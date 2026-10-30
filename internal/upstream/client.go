// Package upstream — the zcode.z.ai origin side: chat transport, billing
// fetch, verdict classification. The gateway (internal/gateway) drives it.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"zcode2api/internal/captcha"
	"zcode2api/internal/config"
	"zcode2api/internal/proxy"
	"zcode2api/internal/store"
	"zcode2api/internal/util"
)

const (
	messagesPath = "/api/v1/zcode-plan/anthropic/v1/messages"
	balancePath  = "/api/v1/zcode-plan/billing/balance"
)

// Client is the origin-facing half of the server: it mints captcha params
// (direct from this host), sends chat requests over the sticky egress pool
// and fetches billing.
type Client struct {
	Cfg     config.Config
	Captcha captcha.Solver
	Sticky  *proxy.StickyProxy // chat egress: one line holds traffic until it fails
	Pool    *proxy.ProxyPool   // billing-family egress: round-robin spread
	// ProfileForAccount returns a per-account bound proxy URL (dashboard
	// proxy profiles / auto per-account binding); nil = fall through to the pool.
	ProfileForAccount func(accDeviceMid string) *url.URL
	// OnEgressFail reports that the account's swap-0 egress line failed at
	// CONNECT level (dead proxy) — the gateway rebinds the account to a
	// fresh line. Verdict-level failures are NOT reported (the line answers).
	OnEgressFail func(accDeviceMid string, dead *url.URL)

	egMu   sync.Mutex
	egress map[string]*http.Client // billing-family clients, per egress URL

	// dead-line circuit breaker (billing egress): url → benched-until.
	// A line that just failed is skipped for lineCooldown so scans stop
	// paying its timeout over and over; entries expire lazily.
	breakMu sync.Mutex
	benched map[string]time.Time
}

// lineCooldown: how long a failed billing line stays out of rotation.
const lineCooldown = 2 * time.Minute

func (c *Client) benchLine(u *url.URL) {
	if u == nil {
		return
	}
	c.breakMu.Lock()
	if c.benched == nil {
		c.benched = map[string]time.Time{}
	}
	c.benched[u.String()] = time.Now().Add(lineCooldown)
	c.breakMu.Unlock()
}

func (c *Client) lineBenched(u *url.URL) bool {
	if u == nil {
		return false
	}
	c.breakMu.Lock()
	defer c.breakMu.Unlock()
	until, ok := c.benched[u.String()]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(c.benched, u.String())
		return false
	}
	return true
}

// pickLiveLine returns the next pool line, preferring non-benched ones;
// when every line is benched it degrades to the plain round-robin pick
// (a possibly-dead line beats refusing to scan at all).
func (c *Client) pickLiveLine() *url.URL {
	if c.Pool == nil {
		return nil
	}
	u := c.Pool.Next()
	for i := 0; i < 5 && u != nil && c.lineBenched(u); i++ {
		u = c.Pool.Next()
	}
	return u
}

// egressURL picks the billing-family egress: the bound dashboard profile
// if any, else the NEXT live pool line (round-robin, skipping benched).
// A 46-account scan must spread across the pool — one sticky line hits
// the provider's per-line connection cap ("Not Enough Connections").
// Empty pool = nil = direct.
func (c *Client) egressURL(mid string) *url.URL {
	if c.ProfileForAccount != nil {
		if u := c.ProfileForAccount(mid); u != nil {
			return u
		}
	}
	if c.Pool != nil {
		return c.pickLiveLine()
	}
	return nil
}

// clientFor returns the cached billing-family client for an egress URL
// (nil = direct).
func (c *Client) clientFor(u *url.URL) *http.Client {
	key := "direct"
	if u != nil {
		key = u.String()
	}
	c.egMu.Lock()
	defer c.egMu.Unlock()
	if cl := c.egress[key]; cl != nil {
		return cl
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil // direct must be truly direct (no env mitm fallback)
	if u != nil {
		tr.Proxy = http.ProxyURL(u)
	}
	cl := &http.Client{Timeout: 15 * time.Second, Transport: tr}
	if c.egress == nil {
		c.egress = map[string]*http.Client{}
	}
	c.egress[key] = cl
	return cl
}

// egressDo runs a billing-family request on the account's egress. A failed
// proxy line is BENCHED (2m breaker) and the request rotates to the next
// live line — never the home IP (user rule 2026-10-08: with a pool
// configured, everything always rides a proxy). An empty pool keeps the
// single direct attempt (empty-pool semantics).
func deadBillingStatus(code int) bool {
	switch code {
	case 502, 503, 504, 522, 524:
		return true
	}
	return false
}

// deadChatStatus: MIX/origin dropped the POST (empty 5xx). 23mac.har and
// the manager both retry these — they are not a model answer. 502 is NOT
// a 3012 WAF verdict.
func deadChatStatus(code int) bool {
	switch code {
	case 500, 502, 503, 504:
		return true
	}
	return false
}

func (c *Client) egressDo(mid string, req *http.Request) (*http.Response, error) {
	u := c.egressURL(mid)
	for try := 1; ; try++ {
		resp, err := c.clientFor(u).Do(req)
		if err == nil && resp != nil && deadBillingStatus(resp.StatusCode) {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
			resp = nil
		}
		if err == nil {
			return resp, nil
		}
		if u == nil {
			return nil, err // no pool configured: the single direct attempt failed
		}
		c.benchLine(u) // stop paying this line's timeout for a while
		if try == 1 && c.OnEgressFail != nil {
			c.OnEgressFail(mid, u)
		}
		if try >= 3 {
			return nil, fmt.Errorf("egress %s failed after %d lines: %w", u.Host, try, err)
		}
		if c.Pool == nil {
			return nil, fmt.Errorf("egress %s failed, no pool to rotate to: %w", u.Host, err)
		}
		next := c.pickLiveLine()
		if next == nil || next.String() == u.String() {
			return nil, fmt.Errorf("egress %s failed, no live alternative line: %w", u.Host, err)
		}
		if req.GetBody != nil {
			if body, berr := req.GetBody(); berr == nil {
				req.Body = body
			}
		}
		log.Printf("[billing] egress %s failed: %v — benched %s, rotating to %s", u.Host, err, lineCooldown, next.Host)
		u = next
	}
}

// IsLoadVerdict reports an upstream LOAD verdict (manager gateway
// LOAD_CODES = {3007, 3009, 3010, 3012}): rotate the egress and retry.
// Status from resp.StatusCode, business code parsed from the body JSON.
func IsLoadVerdict(status int, bodyHead []byte) bool {
	// 529 is always retryable: the manager gateway answers 529 {"overloaded"}
	// after rotating its own DC proxy (gateway.py retryable_error_response),
	// expecting the client to repeat — the IDE's AI SDK does; so do we.
	if status == 529 {
		return true
	}
	if status != 405 && status != 429 && status != 503 {
		return false
	}
	switch originJSONCode(bodyHead) {
	case 3007, 3009, 3010, 3012:
		return true
	}
	return false
}

func billingHeaders(jwt, mid, appVer string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+jwt)
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "*")
	h.Set("User-Agent", "ZCode/"+appVer)
	h.Set("HTTP-Referer", "https://zcode.z.ai")
	h.Set("X-Title", "Z Code@electron")
	h.Set("X-ZCode-App-Version", appVer)
	// os.release() semantics (fingerprint.py pools), NOT a marketing name
	h.Set("X-Os-Category", config.ClientOSCategory)
	h.Set("X-Os-Version", config.BillingOSVersion)
	h.Set("X-Platform", config.ClientPlatform)
	h.Set("X-Release-Channel", "production")
	h.Set("X-Client-Language", "en-US")
	h.Set("X-Client-Timezone", config.ClientTimezone)
	h.Set("X-Device-Mid", mid)
	h.Set("x-request-id", util.GenerateUUID())
	h.Set("sec-fetch-mode", "cors")
	return h
}

// Billing fetches the account's plan/quota snapshot (billing rides the
// round-robin pool proxy when configured, direct otherwise).
func (c *Client) Billing(jwt, mid string) (store.Quota, error) {
	u := config.OriginBaseURL + balancePath + "?app_version=" + config.BillingClientVersion
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return store.Quota{}, err
	}
	req.Header = billingHeaders(jwt, mid, config.BillingClientVersion)
	resp, err := c.egressDo(mid, req)
	if err != nil {
		return store.Quota{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return store.Quota{}, err
	}
	if resp.StatusCode != 200 {
		return store.Quota{}, fmt.Errorf("billing HTTP %d %s", resp.StatusCode, BodySnip(raw, 200))
	}
	var wrap struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return store.Quota{}, err
	}
	if wrap.Code != 0 {
		return store.Quota{}, fmt.Errorf("billing code %d", wrap.Code)
	}
	return quotaFromBalance(wrap.Data), nil
}

func quotaFromBalance(data map[string]any) store.Quota {
	q := store.Quota{PlanName: "Free"}
	if data == nil {
		return q
	}
	seen := map[string]bool{}
	addPlan := func(id string) {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		q.PlanIDs = append(q.PlanIDs, id)
	}
	plans, _ := data["plans"].([]any)
	if len(plans) > 0 {
		if p, ok := plans[0].(map[string]any); ok {
			if n := Str(p["name"]); n != "" {
				q.PlanName = n
			}
			q.ExpiresAt = unixDate(p["ends_at"])
			if q.ExpiresAt == "" {
				q.ExpiresAt = unixDate(p["expires_at"])
			}
		}
	}
	for _, raw := range plans {
		if p, ok := raw.(map[string]any); ok {
			addPlan(Str(p["plan_id"]))
		}
	}
	bals, _ := data["balances"].([]any)
	for _, raw := range bals {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		q.Balances = append(q.Balances, b)
		addPlan(Str(b["plan_id"]))
		q.Entitlements = append(q.Entitlements, map[string]any{
			"name":       firstNonEmpty(Str(b["show_name"]), Str(b["entitlement_id"])),
			"remaining":  AsInt(b["remaining_units"]),
			"used":       AsInt(b["used_units"]),
			"limit":      AsInt(b["total_units"]),
			"period_end": unixInt(b["period_end"]),
		})
		q.Remaining += AsInt(b["remaining_units"])
		q.Used += AsInt(b["used_units"])
		q.Limit += AsInt(b["total_units"])
	}
	return q
}

func unixDate(v any) string {
	n := unixInt(v)
	if n > 0 {
		return time.Unix(n, 0).UTC().Format("2006-01-02")
	}
	if s, ok := v.(string); ok && s != "" {
		return s[:min(10, len(s))]
	}
	return ""
}

func unixInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		n := int64(t)
		if n > 1e12 {
			n /= 1000
		}
		return n
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseRetryAfter parses the Retry-After header (delta-seconds form).
func parseRetryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// reattach rebuilds the response with the peeked head re-attached.
func reattach(head []byte, rest io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(strings.NewReader(string(head)), rest), rest}
}

// ErrLoadBlocked: the account's egress budget is spent on load verdicts
// (3012/3007/3009/3010/529) — the gateway strikes the account and fails
// over to the next one instead of failing the client.
var ErrLoadBlocked = errors.New("no egress accepted the request (load-blocked)")

// ErrPhaseBlocked: TWO load verdicts back-to-back on different lines — a
// WAF phase block tied to this account's identity, not one bad proxy. The
// gateway cools such accounts for a LONG time (RISK_COOLDOWN) so rotation
// reaches fresh accounts fast.
var ErrPhaseBlocked = fmt.Errorf("waf phase: %w", ErrLoadBlocked)

// PostMessages sends one chat request: up to LOAD_SWAPS+1 egress attempts
// (sticky proxy, rotating on load verdicts and dead proxies), a fresh
// single-use captcha param per attempt (minted direct from this host).
func (c *Client) PostMessages(ctx context.Context, jwt, deviceMid string, body []byte, extra http.Header) (*http.Response, error) {
	// (nil, nil) is forbidden by every audited return path, yet a live
	// panic trace (2026-10-08 13:11, current build) saw one reach
	// routeAnthropic. ret() names the exact return line if it ever happens
	// again; the gateway also nil-guards.
	ret := func(r *http.Response, e error) (*http.Response, error) {
		if r == nil && e == nil {
			log.Printf("[egress] BUG: PostMessages (nil, nil), ctx.Err=%v — trace follows:", ctx.Err())
			debug.PrintStack()
		}
		return r, e
	}
	loadSwaps := c.Cfg.LoadSwaps
	var exclude *url.URL
	busy := 0        // 3010 admission-limit busy-retries (same egress)
	raRetries := 0   // plain-429 Retry-After in-place retries
	deadTries := 0   // 502/5xx in-place retries (Mac: same query/trace, new request-id)
	wafVerdicts := 0 // 3012 identity verdicts — the only phase-block proof
	queryID := util.GenerateUUID()
	traceID := util.GenerateUUID()
	for swap := 0; swap <= loadSwaps; swap++ {
		if ctx.Err() != nil {
			return ret(nil, ctx.Err())
		}
	retry:
		var proxyURL *url.URL
		if c.ProfileForAccount != nil {
			proxyURL = c.ProfileForAccount(deviceMid)
			if swap > 0 && exclude != nil && proxyURL != nil && proxyURL.String() == exclude.String() {
				proxyURL = nil // still the dead sid — fall through to sticky
			}
		}
		if proxyURL == nil && c.Sticky != nil {
			if swap == 0 {
				proxyURL = c.Sticky.Next()
			} else {
				proxyURL = c.Sticky.Rotate(exclude)
			}
			if proxyURL == nil && swap > 0 {
				break // nothing left to rotate to
			}
		} else if proxyURL == nil && swap > 0 {
			break // no pool: single direct attempt
		}
		if proxyURL != nil {
			exclude = proxyURL
		}
		egress := egressLabel(proxyURL)

		// 23mac.har: chat has no captcha headers. Claim still mints.
		target, err := url.Parse(config.OriginBaseURL + messagesPath)
		if err != nil {
			return ret(nil, err)
		}
		headers := chatRequestHeaders(jwt, config.ClientVersion, deviceMid, queryID, traceID, extra)
		log.Printf("[egress] chat via %s (swap %d)", egress, swap)
		resp, err := sendChat(ctx, proxyURL, target, headers, body, 15*time.Second)
		if err != nil {
			// trust ONLY the request context (see the mint branch note): a
			// dead-proxy dial timeout matches DeadlineExceeded and must
			// ROTATE, not read as a client hang-up
			if ctx.Err() != nil {
				return ret(nil, ctx.Err()) // client hung up — no rotation, no strike
			}
			// connect-level failure (dead proxy, refused CONNECT): rotate
			log.Printf("[egress] connect failed via %s — rotating (%d/%d)", egress, swap+1, loadSwaps)
			if swap == 0 && c.OnEgressFail != nil {
				c.OnEgressFail(deviceMid, proxyURL)
			}
			continue
		}
		// load verdict (manager LOAD_CODES semantics): the STATUS comes from
		// resp.StatusCode, the business code from the body JSON — 3012 (edge)
		// and 3007 (app captcha check) are different problems.
		head, rest, perr := PeekBody(resp, 256)

		// 23mac.har: 502 → same x-query-id / x-zcode-trace-id, new
		// x-request-id, 200. Manager treats 5xx as load (rotate, never
		// pass through). We do Mac in-place first, then sid swap.
		if deadChatStatus(resp.StatusCode) {
			resp.Body.Close()
			if deadTries < 2 {
				deadTries++
				log.Printf("[egress] HTTP %d via %s — retry same query (%d/2)", resp.StatusCode, egress, deadTries)
				select {
				case <-ctx.Done():
					return ret(nil, ctx.Err())
				case <-time.After(time.Second):
				}
				goto retry
			}
			deadTries = 0
			if c.OnEgressFail != nil {
				c.OnEgressFail(deviceMid, proxyURL)
			}
			if swap == loadSwaps {
				log.Printf("[egress] HTTP %d via %s — every egress blocked (%d/%d)", resp.StatusCode, egress, swap+1, loadSwaps)
				return ret(nil, ErrLoadBlocked)
			}
			log.Printf("[egress] HTTP %d via %s — rotating sid (%d/%d)", resp.StatusCode, egress, swap+1, loadSwaps)
			select {
			case <-ctx.Done():
				return ret(nil, ctx.Err())
			case <-time.After(time.Second):
			}
			continue
		}

		// plain 429 with Retry-After: honor it (cap 120s), retry in place up
		// to 5 times on the SAME egress
		if perr == nil && resp.StatusCode == http.StatusTooManyRequests &&
			!IsQuotaFamily429(resp.StatusCode, head) && !IsAdmissionLimit(resp.StatusCode, head) {
			if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 && raRetries < 5 {
				raRetries++
				if ra > 120*time.Second {
					ra = 120 * time.Second
				}
				resp.Body.Close()
				log.Printf("[egress] 429 via %s — Retry-After %s (%d/5)", egress, ra, raRetries)
				select {
				case <-ctx.Done():
					return ret(nil, ctx.Err())
				case <-time.After(ra):
				}
				goto retry
			}
		}

		// 3010 admission limit: the account stays usable — busy-retry 1s/2s
		// on the SAME egress (the official client's cadence)
		if perr == nil && IsAdmissionLimit(resp.StatusCode, head) && busy < 2 {
			resp.Body.Close()
			busy++
			log.Printf("[egress] 3010 admission limit via %s — busy-retry in %ds (%d/2)", egress, busy, busy)
			select {
			case <-ctx.Done():
				return ret(nil, ctx.Err())
			case <-time.After(time.Duration(busy) * time.Second):
			}
			goto retry
		}

		// quota-family 429: the account's model is exhausted — hand the
		// response up; the gateway marks the model dead and switches
		// accounts (no egress rotation helps here)
		if perr == nil && IsQuotaFamily429(resp.StatusCode, head) {
			resp.Body = reattach(head, rest)
			log.Printf("[egress] quota-family 429 (code %d) via %s — account switch", originJSONCode(head), egress)
			return ret(resp, nil)
		}

		if perr == nil && IsLoadVerdict(resp.StatusCode, head) {
			resp.Body.Close()
			// only the WAF identity verdict (405/3012 "unusual activity")
			// proves a phase block tied to this account. 3009/3010/529 are
			// load throttles that pass in seconds — an account serving 200s
			// between two 3009s must NOT bench for RISK_COOLDOWN (seen live
			// 2026-10-08 14:24: six 200s, then 3009 3009 → wrongly cooled 6h)
			if originJSONCode(head) == 3012 {
				wafVerdicts++
				if c.OnEgressFail != nil {
					c.OnEgressFail(deviceMid, proxyURL)
				}
			}
			if wafVerdicts >= 2 {
				log.Printf("[egress] load verdict (HTTP %d code %d) via %s — %d WAF verdicts in a row: phase block, switching account", resp.StatusCode, originJSONCode(head), egress, wafVerdicts)
				if c.Sticky != nil {
					// the next account must not start on the line this one died on
					c.Sticky.Rotate(proxyURL)
				}
				return ret(nil, ErrPhaseBlocked)
			}
			if swap == loadSwaps {
				log.Printf("[egress] load verdict (HTTP %d code %d) via %s — every egress blocked (%d/%d)", resp.StatusCode, originJSONCode(head), egress, swap+1, loadSwaps)
				return ret(nil, ErrLoadBlocked)
			}
			log.Printf("[egress] load verdict (HTTP %d code %d) via %s — retrying (%d/%d)", resp.StatusCode, originJSONCode(head), egress, swap+1, loadSwaps)
			// IDE cadence: a short pause keeps us from hammering mid-rotation.
			select {
			case <-ctx.Done():
				return ret(nil, ctx.Err())
			case <-time.After(time.Second):
			}
			continue
		}
		// leftover 429 (no Retry-After, unknown body code): never pass
		// through — rotate like a load throttle, gateway switches accounts
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if swap == loadSwaps {
				log.Printf("[egress] 429 via %s — every egress blocked (%d/%d)", egress, swap+1, loadSwaps)
				return ret(nil, ErrLoadBlocked)
			}
			log.Printf("[egress] 429 via %s — retrying (%d/%d)", egress, swap+1, loadSwaps)
			select {
			case <-ctx.Done():
				return ret(nil, ctx.Err())
			case <-time.After(time.Second):
			}
			continue
		}
		resp.Body = reattach(head, rest)
		return ret(resp, nil)
	}
	return ret(nil, ErrLoadBlocked)
}
