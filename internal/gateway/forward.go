package gateway

import (
	"bytes"
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

	accs, _ := s.store.Accounts()
	maxTry := s.cfg.AccountMaxTry
	if n := len(accs); n < maxTry {
		maxTry = n
	}
	if maxTry < 1 {
		maxTry = 1
	}
	skip := map[int64]bool{}
	extra := r.Header
	for tried := 0; tried < maxTry; tried++ {
		acc, err := s.pickExcept(model, skip)
		if err != nil || acc == nil {
			break
		}
		payload := s.normalizeBody(body, model, acc.JWT)
		log.Printf("POST chat model=%s acc=%s %db", model, maskJWT(acc.JWT), len(payload))
		resp, err := s.up.PostMessages(r.Context(), acc.JWT, acc.DeviceMid, payload, extra)
		if err != nil {
			// every egress load-blocked → the account is "hot": strike it,
			// cool after 3 strikes for 30 minutes
			if strings.Contains(err.Error(), "load-blocked") {
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
		head, _, err := upstream.PeekBody(resp, 512)
		if err != nil {
			resp.Body.Close()
			log.Printf("<< peek %v", err)
			skip[acc.ID] = true
			continue
		}
		if upstream.IsQuotaFamily429(resp.StatusCode, head) {
			log.Printf("<< 429 quota-family %s %s", maskJWT(acc.JWT), upstream.BodySnip(head, 80))
			resp.Body.Close()
			s.markQuotaDead(acc, model)
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

// modelNameMap — the upstream is case-sensitive; lowercase aliases map to
// the billing ids.
var modelNameMap = map[string]string{
	"glm-5.3":       "GLM-5.3",
	"glm-5.3-flash": "GLM-5.3-Flash",
	"glm-5.2":       "GLM-5.2",
	"glm-5-turbo":   "GLM-5-Turbo",
	"glm-turbo":     "GLM-5-Turbo",
	"glm-5.1":       "GLM-5.1",
	"glm-4.7":       "GLM-4.7",
}

// normalizeBody — the canonical body transform:
// model casing, string content bridged to text blocks, the official ZCode
// system blocks + the dynamic currentModel block prepended (content
// inspection rejects requests without the identity blocks — 3012),
// cache_control ephemeral on the last content block of the last
// non-system message, and metadata.user_id = the JWT's sub/user_id as a
// plain string. Works on the ordered document so the client's key order
// (and the python byte layout) survive.
func (s *Server) normalizeBody(body []byte, model, jwt string) []byte {
	obj, err := parseOrdered(body)
	if err != nil {
		return body
	}

	// model: resolve alias/casing to the canonical id
	if m, ok := obj.Get("model"); ok {
		if ms, ok := m.(string); ok && ms != "" {
			if canon := s.catalog.Resolve(ms); canon != "" {
				obj.Set("model", canon)
			} else if mapped, ok2 := modelNameMap[strings.ToLower(ms)]; ok2 {
				obj.Set("model", mapped)
			}
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

	// system: official blocks + dynamic currentModel block, user system after
	// (apply_start_plan_system)
	if blocks, err := loadZcodeSystem(); err == nil && len(blocks) > 0 {
		decodeBlocks := func(list []json.RawMessage) []any {
			arr := make([]any, 0, len(list))
			for _, b := range list {
				if v, err := parseOrdered(b); err == nil {
					arr = append(arr, v)
				}
			}
			return arr
		}
		official := decodeBlocks(blocks)
		if strings.TrimSpace(model) != "" {
			dyn := newOrderedDoc()
			dyn.Set("type", "text")
			dyn.Set("text", "- You are powered by the model named "+model+".")
			cc := newOrderedDoc()
			cc.Set("type", "ephemeral")
			dyn.Set("cache_control", cc)
			official = append(official, dyn)
		}
		sysVal, _ := obj.Get("system")
		switch sys := sysVal.(type) {
		case nil:
			obj.Set("system", official)
		case string:
			if strings.TrimSpace(sys) != "" {
				ub := newOrderedDoc()
				ub.Set("type", "text")
				ub.Set("text", sys)
				official = append(official, ub)
			}
			obj.Set("system", official)
		case []any:
			official = append(official, sys...)
			obj.Set("system", official)
		}
	}

	// cache_control: ephemeral on the last content block of the last
	// non-system message (apply_cache_control)
	applyCacheControlEphemeral(obj)

	// metadata.user_id = the JWT's sub/user_id, plain string (jwt_user_id)
	if uid := store.JWTSub(jwt); uid != "" {
		meta, _ := obj.Get("metadata")
		md, ok := meta.(*orderedDoc)
		if !ok {
			md = newOrderedDoc()
		}
		if cur, _ := md.Get("user_id"); cur != uid {
			md.Set("user_id", uid)
			obj.Set("metadata", md)
		}
	}

	return encodeBody(obj)
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
