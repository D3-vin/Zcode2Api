package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"zcode2api/internal/captcha"
	"zcode2api/internal/config"
	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.routeAnthropic(w, r, body, false)
}

func (s *Server) routeAnthropic(w http.ResponseWriter, r *http.Request, body []byte, openai bool) {
	body = s.injectDefaults(body)
	model := upstream.ParseModel(body)
	if model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	// client aliases (claude-* probes, lowercase glm-*) resolve to the real
	// billing id ONCE, before account picking/dead-marks/body building
	if mapped, ok := modelNameMap[strings.ToLower(model)]; ok {
		model = mapped
	}
	model = s.catalog.Resolve(model)

	accs, _ := s.store.Accounts()
	maxTry := s.cfg.AccountMaxTry
	if n := len(accs); n < maxTry {
		maxTry = n
	}
	if maxTry < 1 {
		maxTry = 1
	}
	// hold-retry: the whole walk failed but the client is still with us — it
	// must NOT see the failure (agent clients face-plant after a handful of
	// 529s and need a manual reload). Keep waiting for the pool to recover
	// (the precheck re-check restores benched accounts) and walk again, up
	// to REQUEST_HOLD (0 = legacy fail-fast). While everything is benched a
	// re-walk costs nothing — pickExcept refuses without origin traffic.
	holdUntil := time.Now().Add(s.cfg.RequestHold)
	dumped := false
walk:
	for {
		skip := map[int64]bool{}
		extra := r.Header
		for tried := 0; tried < maxTry; tried++ {
			acc, err := s.pickExcept(model, skip)
			if err != nil || acc == nil {
				break
			}
			payload := s.normalizeBody(body, model, acc.JWT, acc.DeviceMid)
			if !dumped {
				dumpChatBodies(s.chatDumpPath(), r, body, payload, maskJWT(acc.JWT))
				dumped = true
			}
			log.Printf("POST chat model=%s acc=%s %db", model, maskJWT(acc.JWT), len(payload))
			resp, err := s.up.PostMessages(r.Context(), acc.JWT, acc.DeviceMid, payload, extra)
			if err != nil {
				// the client hung up: nothing downstream can succeed — no strike,
				// no account switch, stop quietly (the write to a dead
				// connection is a no-op anyway)
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || r.Context().Err() != nil {
					log.Printf("<< client gone %s", maskJWT(acc.JWT))
					return
				}
				// a WAF phase block is tied to the account identity — cool it
				// LONG (RISK_COOLDOWN, default 6h) so rotation reaches fresh
				// accounts fast; re-checking cooled ones is a separate concern
				if errors.Is(err, upstream.ErrPhaseBlocked) {
					s.store.RiskStrike(acc.ID, 1, s.cfg.RiskCooldown)
					log.Printf("[pool] %s phase-cooled %s", maskJWT(acc.JWT), s.cfg.RiskCooldown)
					// a flag usually isn't alone — re-verify the pool NOW so the
					// next client request goes straight to a proven account
					s.triggerPrecheckBurst()
				} else if errors.Is(err, upstream.ErrLoadBlocked) {
					// every egress load-blocked (no verdict pair — dead lines or
					// mint outage): strike, cool after 3 strikes for 30 minutes
					if n := s.store.RiskStrike(acc.ID, 3, 30*time.Minute); n >= 3 {
						log.Printf("[pool] %s risk-cooled 30m (strikes=%d)", maskJWT(acc.JWT), n)
					}
				}
				if errors.Is(err, captcha.ErrCaptchaUnavailable) {
					log.Printf("<< captcha unavailable %s: %v", maskJWT(acc.JWT), err)
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "5")
					if openai {
						writeOpenAIError(w, http.StatusServiceUnavailable, "captcha solve failed; retry later")
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					w.Write(upstream.CaptchaRequiredJSON())
					return
				}
				log.Printf("<< origin err %v", err)
				skip[acc.ID] = true
				continue
			}
			if resp == nil {
				// armored 2026-10-08: a live (nil,nil) from PostMessages killed
				// this request with a panic — treat it as an origin error, skip
				// the account, keep the client alive (the ret() tracer in
				// PostMessages names the culprit line in the log)
				log.Printf("<< BUG guard: PostMessages (nil,nil) acc=%s — skipping account", maskJWT(acc.JWT))
				skip[acc.ID] = true
				continue
			}
			head, _, err := upstream.PeekBody(resp, 512)
			if err != nil {
				resp.Body.Close()
				log.Printf("<< peek %v", err)
				skip[acc.ID] = true
				continue
			}
			// a 405 is never a legitimate client answer: if the verdict
			// classifier let one through (truncated head etc.), fail over
			// to the next account instead of relaying it — the client must
			// not learn about our account switches
			if resp.StatusCode == http.StatusMethodNotAllowed {
				resp.Body.Close()
				log.Printf("<< 405 passthrough guard %s — switching account", maskJWT(acc.JWT))
				skip[acc.ID] = true
				continue
			}
			if resp.StatusCode >= 500 && resp.StatusCode <= 504 {
				resp.Body.Close()
				log.Printf("<< %d passthrough guard %s — switching account", resp.StatusCode, maskJWT(acc.JWT))
				skip[acc.ID] = true
				continue
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				if upstream.IsQuotaFamily429(resp.StatusCode, head) || upstream.IsQuotaError(head) {
					log.Printf("<< 429 quota-family %s %s", maskJWT(acc.JWT), upstream.BodySnip(head, 80))
					resp.Body.Close()
					s.markQuotaDead(acc, model)
				} else {
					log.Printf("<< 429 %s %s — switching account", maskJWT(acc.JWT), upstream.BodySnip(head, 80))
					resp.Body.Close()
				}
				skip[acc.ID] = true
				continue
			}
			if upstream.IsQuotaError(head) {
				log.Printf("<< 1005 %s %s", maskJWT(acc.JWT), upstream.BodySnip(head, 80))
				resp.Body.Close()
				s.markQuotaDead(acc, model)
				skip[acc.ID] = true
				continue
			}
			if upstream.IsCaptchaError(head) {
				// a challenge makes the current token batch untrustworthy —
				// drain the pre-solved stock
				if inv, ok := s.captcha.(interface{ Invalidate() }); ok {
					inv.Invalidate()
				}
				log.Printf("<< 3007 captcha %s %s", maskJWT(acc.JWT), upstream.BodySnip(head, 80))
				resp.Body.Close()
				skip[acc.ID] = true
				continue
			}
			_ = s.store.Select(acc.ID)
			s.quota1005Reset(acc.ID, model)
			s.mu.Lock()
			s.chatOK[acc.ID] = time.Now()
			s.mu.Unlock()
			if acc.RiskStrikes > 0 || acc.CoolingUntil > 0 {
				s.store.RiskReset(acc.ID) // a clean answer clears the strikes
			}
			log.Printf("<< %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), upstream.BodySnip(head, 120))

			reqStart := time.Now()
			scn := &usageScanner{}
			// tee the body so usage (input/output/cache tokens) is accounted
			// while the response streams to the client unchanged; the wrapped
			// body starts with head, so the writers stream from it alone
			resp.Body = &teeReadCloser{
				Reader: io.TeeReader(io.MultiReader(bytes.NewReader(head), resp.Body), scn),
				Closer: resp.Body,
			}
			if openai {
				s.writeOpenAIFromOrigin(w, resp, head, resp.Body, model, ParseStream(body))
			} else {
				copyOrigin(w, resp, head)
			}
			LogOK(model, maskJWT(acc.JWT), resp.StatusCode, ParseStream(body), 0, time.Since(reqStart), scn.Usage())
			return
		}
		if s.cfg.RequestHold <= 0 || time.Now().After(holdUntil) {
			break walk
		}
		select {
		case <-r.Context().Done():
			// the client hung up while we held — quiet exit, no bytes wasted
			return
		case <-time.After(15 * time.Second):
		}
	}
	if openai {
		writeOpenAIError(w, 529, "Overloaded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "0")
	w.WriteHeader(529)
	w.Write(upstream.OverloadedJSON())
}

func (s *Server) injectDefaults(body []byte) []byte {
	obj, err := parseOrdered(body)
	if err != nil {
		return body
	}
	dm := config.DefaultModelID
	m, _ := obj.Get("model")
	ms, _ := m.(string)
	if strings.TrimSpace(ms) == "" && dm != "" {
		ms = dm
	}
	// the origin receives the canonical billing casing; any client casing resolves
	if ms != "" {
		obj.Set("model", s.catalog.Resolve(ms))
	}
	// upstream legal max is 131072 — bigger budgets get 400 code 1210
	// (upstream hard limit; above → 400/1210)
	if n, ok := obj.Get("max_tokens"); ok {
		if num, isNum := n.(json.Number); isNum {
			if v, err := num.Int64(); err == nil && v > 131072 {
				obj.Set("max_tokens", json.Number("131072"))
			}
		}
	}
	// thinking: pure passthrough (the client's own config or the model default
	// applies)
	return encodeBody(obj)
}

// encodeBody serializes like the reference: python json.dumps(body,
// ensure_ascii=False) — spaced separators, raw UTF-8, client key order.
func encodeBody(obj *orderedDoc) []byte {
	return obj.MarshalPython()
}

// modelNameMap — client-side aliases to the real billing ids. Names the
// origin never serves (Claude Code probes claude-*, clients send lowercase
// glm-*) MUST resolve here before the catalog lookup, or the origin rejects
// the request as an unknown model.
var modelNameMap = map[string]string{
	"glm-5.3":                 "GLM-5.3",
	"glm-5.3-flash":           "GLM-5.3-Flash",
	"glm-5.2":                 "GLM-5.2",
	"glm-5-turbo":             "GLM-5-Turbo",
	"glm-turbo":               "GLM-5-Turbo",
	"glm-5.1":                 "GLM-5.1",
	"glm-4.7":                 "GLM-4.7",
	"zcode glm 5.3":           "GLM-5.3",
	"zcode glm 5.3 flash":     "GLM-5.3-Flash",
	"claude-fable-5-1":        "GLM-5.3",
	"fable":                   "GLM-5.3",
	"claude-opus-4-8":         "GLM-5.3",
	"claude-opus-5-5":         "GLM-5.3",
	"opus":                    "GLM-5.3",
	"claude-sonnet-4-6":       "GLM-5.3",
	"claude-sonnet-5-5":       "GLM-5.3-Flash",
	"sonnet":                  "GLM-5.3-Flash",
	"claude-3-5-haiku-latest": "GLM-5.3-Flash",
	"claude-haiku-5-5":        "GLM-5.3-Flash",
	"haiku":                   "GLM-5.3-Flash",
}

// normalizeBody — the canonical body transform:
// model casing, string content bridged to text blocks, the official ZCode
// system blocks + the dynamic currentModel block prepended (content
// inspection rejects requests without the identity blocks — 3012),
// cache_control ephemeral on the last content block of the last
// non-system message, and metadata.user_id = the Mac JSON blob
// (device_id / account_uuid / session_id). Does not rewrite max_tokens/tools.
// thinking/output_config.effort: Claude → GLM dictionary (see remapClaudeThinking).
func (s *Server) normalizeBody(body []byte, model, jwt, deviceMid string) []byte {
	obj, err := parseOrdered(body)
	if err != nil {
		return body
	}

	// model: client aliases first (claude-* / lowercase glm-* — the catalog
	// can't know them), then the catalog normalizes casing of real ids
	if m, ok := obj.Get("model"); ok {
		if ms, ok := m.(string); ok && ms != "" {
			if mapped, ok2 := modelNameMap[strings.ToLower(ms)]; ok2 {
				ms = mapped
			}
			obj.Set("model", s.catalog.Resolve(ms))
		}
	}

	// messages: plain-string content must become a text block
	if mv, ok := obj.Get("messages"); ok {
		if msgs, ok := mv.([]any); ok {
			for i, m := range msgs {
				doc, ok := m.(*orderedDoc)
				if !ok {
					continue
				}
				if c, ok := doc.Get("content"); ok {
					if cs, ok := c.(string); ok {
						block := newOrderedDoc()
						block.Set("type", "text")
						block.Set("text", cs)
						doc.Set("content", []any{block})
						msgs[i] = doc
					}
				}
			}
			obj.Set("messages", msgs)
		}
	}

	// system: exactly the 3 HAR identity blocks. A 4th block 3012's
	// (billing / Claude / title — live 2026-10-29). Editing text inside
	// the last block is WAF-ok (same matrix): drop the canned opener and
	// splice client system there so rules sit in system[], not a user-fold.
	clientSys := takeClientSystemTexts(obj)
	if blocks, err := loadZcodeSystem(); err == nil && len(blocks) > 0 {
		official := make([]any, 0, len(blocks))
		for _, b := range blocks {
			if v, err := parseOrdered(b); err == nil {
				official = append(official, v)
			}
		}
		spliceClientIntoLastSystem(official, clientSys)
		obj.Set("system", official)
	}

	// cache_control: ephemeral on the last content block of the last
	// non-system message (apply_cache_control)
	applyCacheControlEphemeral(obj)

	// 23mac.har: metadata.user_id is a JSON string, not the JWT sub.
	if deviceMid == "" {
		deviceMid = store.DeviceMidFor(jwt)
	}
	uid := `{"device_id":"` + deviceMid + `","account_uuid":"","session_id":"` + store.SessionIDFor(deviceMid) + `"}`
	meta, _ := obj.Get("metadata")
	md, ok := meta.(*orderedDoc)
	if !ok {
		md = newOrderedDoc()
	}
	md.Set("user_id", uid)
	obj.Set("metadata", md)

	remapClaudeThinking(obj)
	return encodeBody(obj)
}

// takeClientSystemTexts pulls text out of the client's system field and
// clears it so the Mac identity is the only system on the wire.
func takeClientSystemTexts(obj *orderedDoc) []string {
	sysVal, ok := obj.Get("system")
	if !ok || sysVal == nil {
		return nil
	}
	var texts []string
	switch sys := sysVal.(type) {
	case string:
		if t := strings.TrimSpace(sys); t != "" {
			texts = append(texts, t)
		}
	case []any:
		for _, item := range sys {
			switch b := item.(type) {
			case *orderedDoc:
				if t, ok := b.Get("text"); ok {
					if s, ok := t.(string); ok {
						if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "x-anthropic-billing-header:") {
							texts = append(texts, s)
						}
					}
				}
			case map[string]any:
				if s, ok := b["text"].(string); ok {
					if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "x-anthropic-billing-header:") {
						texts = append(texts, s)
					}
				}
			case string:
				if s := strings.TrimSpace(b); s != "" {
					texts = append(texts, s)
				}
			}
		}
	}
	return texts
}

const zcodeCannedOpener = "Before your first tool call, say in a sentence what you're about to do; while working, give brief updates when you find something load-bearing or change direction."
const zcodeLeadOutcome = "Lead with the outcome. Your first sentence after finishing should answer \"what happened\" or \"what did you find\" — the thing the user would ask for if they said \"just give me the TLDR.\" Supporting detail and reasoning come after, for readers who want them."

// spliceClientIntoLastSystem keeps len(system)==3 (WAF) but puts client
// rules inside the last HAR block. Also strips the ZCode canned opener
// that made every reply start the same way (live ping 200, 2026-10-29).
func spliceClientIntoLastSystem(official []any, texts []string) {
	if len(official) == 0 {
		return
	}
	last, ok := official[len(official)-1].(*orderedDoc)
	if !ok {
		return
	}
	raw, _ := last.Get("text")
	s, _ := raw.(string)
	s = strings.ReplaceAll(s, zcodeCannedOpener, "")
	s = strings.ReplaceAll(s, zcodeLeadOutcome, "")
	if len(texts) > 0 {
		s += "\n\n# Client instructions\n" + strings.Join(texts, "\n\n")
	}
	last.Set("text", s)
}

// remapClaudeThinking maps Claude Code effort/thinking onto the GLM wire
// seen in 23mac.har (thinking.type=enabled, output_config.effort=max).
//
// ponytail: medium→high and xhigh→max are a UI-scale guess — ZCode HARs
// only captured effort=max. Sniff official ZCode at Low/High/Max and
// replace this table if the tokens differ.
func remapClaudeThinking(obj *orderedDoc) {
	if tv, ok := obj.Get("thinking"); ok {
		if doc, ok := tv.(*orderedDoc); ok {
			if t, ok := doc.Get("type"); ok {
				if s, ok := t.(string); ok && strings.EqualFold(s, "adaptive") {
					doc.Set("type", "enabled")
					obj.Set("thinking", doc)
				}
			}
		}
	}
	oc, ok := obj.Get("output_config")
	if !ok {
		return
	}
	doc, ok := oc.(*orderedDoc)
	if !ok {
		return
	}
	ev, ok := doc.Get("effort")
	if !ok {
		return
	}
	s, ok := ev.(string)
	if !ok {
		return
	}
	if m := mapGLMEffort(s); m != s {
		doc.Set("effort", m)
		obj.Set("output_config", doc)
	}
}

func mapGLMEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low":
		return "low"
	case "medium":
		return "high"
	case "high":
		return "high"
	case "xhigh", "extra":
		return "max"
	case "max":
		return "max"
	default:
		return v
	}
}

// applyCacheControlEphemeral: append
// cache_control {"ephemeral"} to the last block of the last non-system
// message (idempotent).
func applyCacheControlEphemeral(obj *orderedDoc) {
	mv, ok := obj.Get("messages")
	if !ok {
		return
	}
	msgs, ok := mv.([]any)
	if !ok || len(msgs) == 0 {
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		doc, ok := msgs[i].(*orderedDoc)
		if !ok {
			continue
		}
		if role, _ := doc.Get("role"); role == "system" {
			continue
		}
		cv, _ := doc.Get("content")
		switch content := cv.(type) {
		case string:
			block := newOrderedDoc()
			block.Set("type", "text")
			block.Set("text", content)
			cc := newOrderedDoc()
			cc.Set("type", "ephemeral")
			block.Set("cache_control", cc)
			doc.Set("content", []any{block})
		case []any:
			if len(content) == 0 {
				return
			}
			if last, ok := content[len(content)-1].(*orderedDoc); ok {
				if _, has := last.Get("cache_control"); !has {
					cc := newOrderedDoc()
					cc.Set("type", "ephemeral")
					last.Set("cache_control", cc)
				}
			}
		}
		return
	}
}

func ParseStream(body []byte) bool {
	var obj struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &obj)
	return obj.Stream
}

// copyOrigin streams the origin response to the client unchanged. resp.Body
// must already start with head (the peek buffer is re-attached by the caller),
// so the whole thing streams end-to-end and flushes as it goes — a relayed
// SSE must reach the client chunk-by-chunk, not sit in the 4KB server buffer.
func copyOrigin(w http.ResponseWriter, resp *http.Response, head []byte) {
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		if upstream.IsSSE(head) {
			ct = "text/event-stream; charset=utf-8"
		} else {
			ct = "application/json"
		}
	}
	w.Header().Set("Content-Type", ct)
	if upstream.IsSSE(head) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(&flushWriter{w: w}, resp.Body)
}

// flushWriter flushes the ResponseWriter after every write.
type flushWriter struct{ w http.ResponseWriter }

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fl, ok := fw.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

type teeReadCloser struct {
	io.Reader
	io.Closer
}
