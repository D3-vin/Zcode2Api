package gateway

// chatprecheck.go — background chat-health sweep. A per-account WAF chat
// flag (405/3012) is invisible to billing (billing stays 200), so
// pickExcept kept choosing flagged accounts and client requests burned
// their whole ACCOUNT_MAX_TRY budget walking them (live 2026-10-09: ten
// dead accounts in a row → client error). The sweep sends ONE minimal
// real request per account (the user's test_api.py shape, rebound by
// normalizeBody) through the production PostMessages path: 2×3012 on
// different lines (ErrPhaseBlocked) benches the account for
// cfg.PrecheckBench; any HTTP answer restores a benched one. Benched
// accounts re-check on a short cadence, so a lifted flag returns the
// account long before the bench expires. Newly added accounts are left
// alone (they are fresh); a 3012 probe against an account that just
// served a client answer is treated as intermittent, not a flag.

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"time"

	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

const (
	precheckTick     = 10 * time.Minute // baseline sweep cadence
	precheckPerTick  = 5                // accounts per baseline sweep
	precheckBurst    = 50               // event/boot sweep size (verify the whole pool)
	precheckBurstGap = 2 * time.Minute  // min spacing between event bursts
	precheckRecheck  = 30 * time.Minute // re-check cadence for benched accounts
	precheckFreshOk  = 2 * time.Hour    // re-check cadence for healthy accounts
	precheckSkipNew  = 48 * time.Hour   // freshly added accounts are presumed clean
	precheckOKGrace  = 15 * time.Minute // client-200 wins over a 3012 probe within this window
	precheckModel    = "GLM-5.3-Flash"  // cheap family; the verdict is the HTTP status, not the text
)

// precheckTemplate: the minimal client shape (test_api.py style). We only
// need the HTTP status — the body is closed after the first bytes, so the
// generation is aborted almost immediately and the token cost is a few
// units of prompt + a sliver of thinking under max_tokens=64.
const precheckTemplate = `{"model":"GLM-5.3-Flash","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"ping"}]}`

// precheckVerdict classifies a probe result. conclusive=false means
// transport noise (dead lines, mint outage, client-style hangup) — the
// sweep decides nothing on it.
func precheckVerdict(err error, resp *http.Response) (healthy, conclusive bool) {
	switch {
	case errors.Is(err, upstream.ErrPhaseBlocked):
		return false, true // 2×3012 on different lines — the account's own flag
	case err != nil:
		return false, false // load/transport noise — inconclusive
	case resp == nil:
		return false, false // armored (nil,nil) — inconclusive
	default:
		return true, true // any HTTP answer (200/429/1005/…) — the account can chat
	}
}

func (s *Server) chatPrecheckLoop() {
	// boot verification: the pool starts PROVEN, not "unknown until the
	// client trips over a flagged account" (never-checked accounts are due
	// immediately, so one wide sweep covers the whole pool)
	s.chatPrecheckSweep(precheckBurst)
	for {
		time.Sleep(precheckTick)
		s.chatPrecheckSweep(precheckPerTick)
	}
}

// triggerPrecheckBurst re-verifies the pool right after a client request
// hit a 3012 flag — a flagging event usually touches several accounts, and
// the baseline 5-per-10-min pace would leave the client walking the rest
// for hours. Throttled: bursts are at most one per precheckBurstGap.
func (s *Server) triggerPrecheckBurst() {
	s.mu.Lock()
	if time.Since(s.precheckBurstAt) < precheckBurstGap {
		s.mu.Unlock()
		return
	}
	s.precheckBurstAt = time.Now()
	s.mu.Unlock()
	go s.chatPrecheckSweep(precheckBurst)
}

// chatPrecheckSweep probes the most-due accounts (up to limit): never
// checked first, then benched ones past the re-check cadence, then the
// oldest checked.
func (s *Server) chatPrecheckSweep(limit int) {
	accs, err := s.store.Accounts()
	if err != nil {
		return
	}
	now := time.Now()
	type due struct {
		a    store.Account
		last time.Time
	}
	var list []due
	s.mu.Lock()
	for _, a := range accs {
		if a.Status != "" || a.JWT == "" {
			continue
		}
		if a.CreatedAt > 0 && now.Unix()-a.CreatedAt < int64(precheckSkipNew/time.Second) {
			continue // freshly added — presumed clean, left alone
		}
		last := s.chatChecked[a.ID]
		var wait time.Duration
		if last.IsZero() {
			wait = -1 // never checked — always due
		} else if a.CoolingUntil > now.Unix() {
			wait = precheckRecheck
		} else {
			wait = precheckFreshOk
		}
		if wait >= 0 && now.Sub(last) < wait {
			continue
		}
		list = append(list, due{a: a, last: last})
	}
	s.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].last.Before(list[j].last) })
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	benched, probed := 0, 0
	for _, d := range list {
		if s.chatPrecheckOne(d.a) {
			benched++
		}
		probed++
	}
	// every probe in a pass 3012'd: either the whole pool is flagged or the
	// origin is rejecting the template itself — say so loudly either way
	if probed >= 3 && benched == probed {
		log.Printf("[precheck] %d/%d probes chat-blocked in one pass — pool-wide flag or origin rejects the probe shape", benched, probed)
	}
}

// chatPrecheckOne returns true when the account was benched.
func (s *Server) chatPrecheckOne(a store.Account) bool {
	payload := s.normalizeBody([]byte(precheckTemplate), precheckModel, a.JWT, a.DeviceMid)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := s.up.PostMessages(ctx, a.JWT, a.DeviceMid, payload, nil)
	s.mu.Lock()
	s.chatChecked[a.ID] = time.Now()
	okAt := s.chatOK[a.ID]
	s.mu.Unlock()
	if resp != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
	}
	healthy, conclusive := precheckVerdict(err, resp)
	if !conclusive {
		return false
	}
	if healthy {
		if a.CoolingUntil > time.Now().Unix() || a.RiskStrikes > 0 {
			s.store.RiskReset(a.ID)
			log.Printf("[precheck] %s healthy — restored to pool", maskJWT(a.JWT))
		}
		return false
	}
	if time.Since(okAt) < precheckOKGrace {
		// the account served a real client answer minutes ago — the 3012 is
		// intermittent, not a flag; benching it would only shrink the pool
		log.Printf("[precheck] %s 3012 but served a client %ds ago — not benching", maskJWT(a.JWT), int(time.Since(okAt).Seconds()))
		return false
	}
	s.store.SetCooling(a.ID, time.Now().Add(s.cfg.PrecheckBench).Unix())
	log.Printf("[precheck] %s chat-blocked (2×3012) — benched %s", maskJWT(a.JWT), s.cfg.PrecheckBench)
	return true
}
