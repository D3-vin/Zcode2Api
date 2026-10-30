package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"zcode2api/internal/captcha"
	"zcode2api/internal/config"
	"zcode2api/internal/proxy"
	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

type Server struct {
	cfg        config.Config
	store      *store.Store
	captcha    captcha.Solver
	sticky     *proxy.StickyProxy
	catalog    *Catalog
	proxies    *proxy.ProxyPool
	up         *upstream.Client
	mu         sync.Mutex
	loginFlows map[string]*loginFlow
	warmOnce   sync.Once
	quotaOnce  sync.Once
	// consecutive-1005 strikes per "accountID:model" — two against a stale
	// quota snapshot bench the account until the period resets
	quota1005 map[string]int
	// session affinity: model → the account serving it until that account
	// dies (quota/flag/cooling) — per-request rotation presented the origin
	// a farm (dozens of identities alternating through one line)
	stickyAcc map[string]int64
	// per-account egress binding: device-mid → its own pool line (random on
	// first use, rebound only when the line stops answering) — one account
	// walks from one IP, like N independent IDE installs
	accLine map[string]*url.URL
	// MIX sticky sessions that just 502/3012'd — skip them when binding.
	hotLine map[string]time.Time
	// chat precheck: last health-probe time per account id (drives the
	// sweep's due ordering; benched accounts re-check on a short cadence);
	// chatOK = last time the account served a client answer — a 3012
	// probe against a just-serving account is intermittent, not a flag
	chatChecked     map[int64]time.Time
	chatOK          map[int64]time.Time
	precheckOnce    sync.Once
	precheckBurstAt time.Time
	// runtime knobs (Settings page): quota monitor interval + captcha pool
	quotaRefreshMin int
	poolMin         int
	poolMax         int
	poolTTL         time.Duration
	chatDump        bool // Settings: dump client+origin bodies to cfg.ChatDump
	autoClaim       bool // Settings: daily warm + Claim all/one may claim plans
}

func NewServer(cfg config.Config) (*Server, error) {
	st, err := store.OpenStore(config.AccountsDBPath)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:         cfg,
		store:       st,
		quota1005:   map[string]int{},
		stickyAcc:   map[string]int64{},
		accLine:     map[string]*url.URL{},
		hotLine:     map[string]time.Time{},
		chatChecked: map[int64]time.Time{},
		chatOK:      map[int64]time.Time{},
		// actual models come from the origin billing (ModelReport/refresh);
		// only the default model bootstraps the list before the first scan
		catalog: NewCatalog([]string{config.DefaultModelID}),
	}
	// THE proxy source: profiles in the store (the dashboard edits them).
	// PROXY_FILE only seeds them once on a fresh database; chat and billing
	// egress always read the live profile set.
	s.proxies = &proxy.ProxyPool{}
	s.sticky = &proxy.StickyProxy{Pool: s.proxies}
	s.seedProfilesFromFile()
	s.rebuildEgress()
	s.restoreAccountLines()
	if n := s.proxies.Len(); n > 0 {
		log.Printf("[proxies] egress pool: %d lines (from proxy profiles)", n)
	} else {
		log.Printf("[proxies] egress pool empty — origin goes direct")
	}
	// mint output goes to the log only — params are single-use and never
	// replayed, so there is no persisted store
	node := captcha.NewNodeSolver(cfg)
	inner := captcha.Solver(node)
	if d3 := captcha.NewD3Solver(cfg); d3.Enabled() {
		d3.PickProxy = func() *url.URL {
			if !cfg.CaptchaD3Proxy {
				return nil
			}
			return s.proxies.Next()
		}
		inner = captcha.NewFallbackSolver(cfg.CaptchaSolver, d3, node)
		fb := "node"
		if cfg.CaptchaSolver == "node" {
			fb = "d3"
		}
		log.Printf("[captcha] solver=%s fallback=%s (D3 %s)", cfg.CaptchaSolver, fb, cfg.CaptchaD3URL)
	} else {
		log.Printf("[captcha] solver=node (happy-dom; CAPTCHA_D3_KEY empty)")
	}
	// pre-solved stock: refill only while a servable account exists AND the
	// origin still requires chat captcha (skip_model_request = chat never
	// consumes params; on-demand sync mint covers rare non-chat needs)
	s.captcha = captcha.NewMintPool(inner, func() bool {
		return !cfg.CaptchaOff && s.store.Active() != nil && !captcha.SkipModelRequest(context.Background())
	},
		s.quotaMin(), s.quotaMax(), s.quotaTTL())
	// keys set from the Settings page are user settings: persisted in the
	// store, they win over env values after restart
	if v := s.store.Setting("admin_password", ""); v != "" {
		s.cfg.AuthToken = v
	}
	if v := s.store.Setting("gateway_key", ""); v != "" {
		s.cfg.GatewayKey = v
	}
	// runtime knobs from stored settings (Settings page overrides live)
	s.quotaRefreshMin = atoiSetting(s.store, "quota_refresh_min", 0)
	s.poolMin = atoiSetting(s.store, "captcha_pool_min", 3)
	s.poolMax = atoiSetting(s.store, "captcha_pool_max", 10)
	if d, err := time.ParseDuration(s.store.Setting("captcha_pool_ttl", "95s")); err == nil && d > 0 {
		s.poolTTL = d
	} else {
		s.poolTTL = 95 * time.Second
	}
	s.chatDump = s.store.Setting("chat_dump", "0") == "1"
	// default on — matches historical warm+claim; turn off while chatting
	s.autoClaim = s.store.Setting("auto_claim", "1") != "0"

	s.up = &upstream.Client{
		Cfg:               cfg,
		Captcha:           s.captcha,
		Sticky:            s.sticky,  // chat failover: shared rotation when a bound line dies
		Pool:              s.proxies, // billing family: round-robin spread
		ProfileForAccount: s.profileForAccountMid,
		OnEgressFail:      s.rebindAccountLine,
	}
	return s, nil
}

func (s *Server) importKeysFile() {
	n := 0
	for _, k := range config.LoadKeyFile(config.KeysFilePath) {
		if _, err := s.store.Add(k, ""); err == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("[pool] +%d from %s", n, config.KeysFilePath)
	}
}

func (s *Server) Close() {
	if s.captcha != nil {
		s.captcha.Close()
	}
	if s.store != nil {
		s.store.Close()
	}
}

func (s *Server) activeAccount() *store.Account {
	return s.store.Active()
}

func reverseAccounts(accs []store.Account) {
	for i, j := 0, len(accs)-1; i < j; i, j = i+1, j-1 {
		accs[i], accs[j] = accs[j], accs[i]
	}
}

func (s *Server) forEachAccount(accs []store.Account, workers int, fn func(store.Account)) {
	if workers < 1 {
		workers = 1
	}
	ch := make(chan store.Account)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range ch {
				fn(a)
			}
		}()
	}
	for _, a := range accs {
		ch <- a
	}
	close(ch)
	wg.Wait()
}

func (s *Server) refreshAllBilling() (checked, dropped int) {
	return s.refreshAllBillingOrder(false, nil)
}

func (s *Server) refreshAllBillingOrder(newest bool, tick func(done, total int, id int64, ok bool, detail string)) (checked, dropped int) {
	accs, err := s.store.Accounts()
	if err != nil {
		return 0, 0
	}
	if newest {
		reverseAccounts(accs)
	}
	n := s.cfg.BillingWorkers
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	var nChk atomic.Int32
	var markMu sync.Mutex
	expiredIDs := []int64{}
	revokedIDs := []int64{}
	order := "oldest-first"
	if newest {
		order = "newest-first"
	}
	log.Printf("[billing] refresh start accounts=%d workers=%d %s", len(accs), n, order)
	total := len(accs)
	if tick != nil {
		tick(0, total, 0, true, "")
	}
	var nDone atomic.Int32
	s.forEachAccount(accs, n, func(a store.Account) {
		// billing rides the account's own device mid: warming and quota
		// scans must present ONE device per account, otherwise the origin
		// serves the scan a device it never saw
		ok := false
		detail := ""
		q, err := s.up.Billing(a.JWT, a.DeviceMid)
		if err != nil {
			log.Printf("[billing] %s: %v", maskJWT(a.JWT), err)
			detail = err.Error()
			// 401 = the token is rejected by the origin: mark, keep the row
			if strings.Contains(fmt.Sprint(err), "HTTP 401") {
				markMu.Lock()
				revokedIDs = append(revokedIDs, a.ID)
				markMu.Unlock()
			}
		} else {
			nChk.Add(1)
			ok = true
			if upstream.IsEmptyGrant(q) {
				markMu.Lock()
				expiredIDs = append(expiredIDs, a.ID)
				markMu.Unlock()
				detail = "empty grant"
			} else {
				s.mu.Lock()
				_ = s.store.UpdateQuota(a.ID, q)
				s.catalog.MergeFromQuota(q)
				s.mu.Unlock()
				detail = fmt.Sprintf("%s rem=%d", q.PlanName, q.Remaining)
				log.Printf("[billing] %s plan=%s rem=%d", maskJWT(a.JWT), q.PlanName, q.Remaining)
			}
		}
		d := int(nDone.Add(1))
		if tick != nil {
			tick(d, total, a.ID, ok, detail)
		}
	})
	dropped = s.store.MarkStatus(expiredIDs, "expired")
	if n := s.store.MarkStatus(revokedIDs, "revoked"); n > 0 {
		log.Printf("[billing] marked %d revoked (401) — hidden, kept in db", n)
	}
	if dropped > 0 {
		log.Printf("[billing] marked %d empty grants expired — hidden, kept in db", dropped)
	}
	return int(nChk.Load()), dropped
}

func (s *Server) pickExcept(model string, skip map[int64]bool) (*store.Account, error) {
	accs, err := s.store.Accounts()
	if err != nil {
		return nil, err
	}
	if len(accs) == 0 {
		return nil, fmt.Errorf("no accounts")
	}
	dead := s.store.DeadSet()
	mkey := upstream.Norm(model)
	servable := func(a store.Account) bool {
		if skip[a.ID] {
			return false
		}
		if a.Status != "" {
			return false // expired or revoked (hidden, kept in db)
		}
		if a.CoolingUntil > time.Now().Unix() {
			return false // risk-control cooldown (strikes)
		}
		if a.DisabledModels != "" {
			for _, dm := range strings.Split(a.DisabledModels, ",") {
				if strings.TrimSpace(dm) == mkey {
					return false // model manually excluded for this account
				}
			}
		}
		if dead[fmt.Sprintf("%d:%s", a.ID, mkey)] {
			return false
		}
		ents := entitlementsOf(a)
		if len(ents) > 0 {
			// the plan was scanned: serve the account only for models it
			// offers, and only while quota remains (a planless/exhausted
			// account is dead weight for this request)
			frac, ok := upstream.FracFromEntitlements(ents, model)
			if !ok {
				return false // plan scanned, this model is not in it
			}
			if frac <= 0 {
				// quota exhausted — dead until the period reset
				s.store.MarkDead(a.ID, mkey, upstream.DeadUntilUnix(ents, model))
				return false
			}
		}
		return true
	}
	// session affinity: keep serving the SAME account per model until it
	// dies (quota exhausted, chat flag, cooldown) — the real-user pattern.
	// Per-request round-robin made the origin see a farm: dozens of
	// identities alternating through one line, every request a new account
	// (live 2026-10-09: each request → next account via one proxy).
	s.mu.Lock()
	stickyID := s.stickyAcc[mkey]
	s.mu.Unlock()
	if stickyID == 0 {
		if act := s.store.Active(); act != nil {
			stickyID = act.ID
		}
	}
	if stickyID != 0 {
		for _, a := range accs {
			if a.ID != stickyID {
				continue
			}
			if servable(a) {
				return &a, nil
			}
			break // the sticky account died — fall through and pick a new one
		}
	}
	// per-model rotation cursor: each model gets its own round-robin
	// start position
	start := s.store.Rotate("m:"+mkey, len(accs))
	order := append(accs[start:], accs[:start]...)
	for _, a := range order {
		a := a
		if !servable(a) {
			continue
		}
		s.mu.Lock()
		s.stickyAcc[mkey] = a.ID
		s.mu.Unlock()
		return &a, nil
	}
	return nil, fmt.Errorf("pool empty for model %s", model)
}

func (s *Server) markQuotaDead(cur *store.Account, model string) {
	if cur == nil {
		return
	}
	mkey := upstream.Norm(model)
	ents := entitlementsOf(*cur)
	frac, ok := upstream.FracFromEntitlements(ents, model)
	if !ok || frac <= 0 {
		until := upstream.DeadUntilUnix(ents, model)
		s.store.MarkDead(cur.ID, mkey, until)
		log.Printf("[pool] exhausted %s model=%s until %d", maskJWT(cur.JWT), model, until)
		return
	}
	// the snapshot claims quota remains, but the origin keeps refusing with
	// 1005. Two in a row ACROSS requests against a rich snapshot is NOT
	// proof of an empty pool (live 2026-10-08: five accounts with rem=5M
	// got benched for a month by what looked like a per-minute limit) —
	// bench briefly and let a targeted refresh decide.
	key := fmt.Sprintf("%d:%s", cur.ID, mkey)
	s.mu.Lock()
	s.quota1005[key]++
	n := s.quota1005[key]
	s.mu.Unlock()
	if n >= 2 {
		s.store.MarkDead(cur.ID, mkey, time.Now().Add(2*time.Minute).Unix())
		log.Printf("[pool] 1005 %s ×%d with quota left — benched 2m, refreshing", maskJWT(cur.JWT), n)
		go s.refreshAndRecheck(*cur, model)
		return
	}
	log.Printf("[pool] 1005 %s still %.0f%% — skip this request, not dead", maskJWT(cur.JWT), frac*100)
}

// refreshAndRecheck pulls a fresh billing snapshot for a 1005-struck
// account: a real zero benches the model until the period resets; quota
// still present means the 1005 was a transient limit — clear the strikes,
// the short bench expires by itself.
func (s *Server) refreshAndRecheck(acc store.Account, model string) {
	q, err := s.up.Billing(acc.JWT, acc.DeviceMid)
	if err != nil {
		log.Printf("[pool] 1005 recheck %s: billing unavailable: %v", maskJWT(acc.JWT), err)
		return
	}
	s.mu.Lock()
	_ = s.store.UpdateQuota(acc.ID, q)
	s.catalog.MergeFromQuota(q)
	s.mu.Unlock()
	if frac, ok := upstream.FracFromEntitlements(q.Entitlements, model); ok && frac <= 0 {
		until := upstream.DeadUntilUnix(q.Entitlements, model)
		s.store.MarkDead(acc.ID, upstream.Norm(model), until)
		log.Printf("[pool] 1005 recheck %s: quota really gone — dead until %d", maskJWT(acc.JWT), until)
		return
	}
	s.quota1005Reset(acc.ID, model)
	log.Printf("[pool] 1005 recheck %s: quota remains — strikes cleared", maskJWT(acc.JWT))
}

// quota1005Reset clears the consecutive-1005 counter: a clean answer from
// the account+model proves the snapshot (and the account) alive.
func (s *Server) quota1005Reset(id int64, model string) {
	key := fmt.Sprintf("%d:%s", id, upstream.Norm(model))
	s.mu.Lock()
	delete(s.quota1005, key)
	s.mu.Unlock()
}

// rebuildCatalogFromStore re-merges the model catalog from the stored
// quota snapshots — zero origin traffic at boot (refreshAllBilling lives
// on in the monitor loop and the Refresh button).
func (s *Server) rebuildCatalogFromStore() {
	accs, err := s.store.Accounts()
	if err != nil {
		return
	}
	n := 0
	s.mu.Lock()
	for _, a := range accs {
		if ents := entitlementsOf(a); len(ents) > 0 {
			s.catalog.MergeFromQuota(store.Quota{Entitlements: ents})
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		log.Printf("[pool] catalog rebuilt from %d stored snapshots", n)
	}
}

func entitlementsOf(a store.Account) []map[string]any {
	if a.QuotaJSON == "" {
		return nil
	}
	var ents []map[string]any
	_ = json.Unmarshal([]byte(a.QuotaJSON), &ents)
	return ents
}

func maskJWT(j string) string {
	if len(j) < 12 {
		return "***"
	}
	return j[:6] + "…" + j[len(j)-4:]
}

// withGatewayAuth guards /v1/* client endpoints: GATEWAY_KEY when set,
// otherwise the shared AUTH_TOKEN (gateway-key semantics).
func (s *Server) withGatewayAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := s.cfg.GatewayKey
		if key == "" {
			key = s.cfg.AuthToken
		}
		got := r.Header.Get("Authorization")
		const p = "Bearer "
		if len(got) > len(p) && got[:len(p)] == p {
			got = got[len(p):]
		}
		if got == "" {
			got = r.Header.Get("x-api-key")
		}
		if got != key {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid gateway key","type":"authentication_error"}}`))
			return
		}
		next(w, r)
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthToken == "" {
			next(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		const p = "Bearer "
		if len(got) > len(p) && got[:len(p)] == p {
			got = got[len(p):]
		}
		if got == "" {
			got = r.Header.Get("x-api-key")
		}
		if got != s.cfg.AuthToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid auth token","type":"authentication_error"}}`))
			return
		}
		next(w, r)
	}
}

func oauthBaseEnv() string      { return os.Getenv("ZCODE_OAUTH_API_BASE") }
func exchangeOriginEnv() string { return os.Getenv("ZCODE_EXCHANGE_ORIGIN") }

func atoiSetting(st *store.Store, key string, def int) int {
	if n, err := strconv.Atoi(st.Setting(key, "")); err == nil && n > 0 {
		return n
	}
	return def
}

func (s *Server) quotaMin() int           { return s.poolMin }
func (s *Server) quotaMax() int           { return s.poolMax }
func (s *Server) quotaTTL() time.Duration { return s.poolTTL }
