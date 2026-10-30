package gateway

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"zcode2api/internal/config"
	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

//go:embed assets/mac_ping_system.json
var macPingSystem []byte

// macPingPayload is the chrome-proven cheap chat: 23mac.har system
// blocks + ping, no tools/thinking. Does NOT run normalizeBody unless
// the debug caller sets normalize=true on a provided body.
func macPingPayload(model, jwt, mid string) []byte {
	if model == "" {
		model = config.DefaultModelID
	}
	if mid == "" {
		mid = store.DeviceMidFor(jwt)
	}
	var sys []any
	_ = json.Unmarshal(macPingSystem, &sys)
	uid, _ := json.Marshal(map[string]string{
		"device_id":    mid,
		"account_uuid": "",
		"session_id":   store.SessionIDFor(mid),
	})
	body := map[string]any{
		"model":      model,
		"max_tokens": 64,
		"stream":     true,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "ping"},
				},
			},
		},
		"system":   sys,
		"metadata": map[string]any{"user_id": string(uid)},
	}
	raw, _ := json.Marshal(body)
	return raw
}

func (s *Server) apiDebugPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID        int64           `json:"id"`
		Beta      string          `json:"beta"`
		Body      json.RawMessage `json:"body"`
		Normalize bool            `json:"normalize"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	accs, _ := s.store.Accounts()
	var acc *store.Account
	for i := range accs {
		if accs[i].ID == req.ID {
			acc = &accs[i]
			break
		}
	}
	if acc == nil || acc.JWT == "" {
		writeJSONError(w, http.StatusNotFound, "no such account")
		return
	}
	payload := macPingPayload(config.DefaultModelID, acc.JWT, acc.DeviceMid)
	if len(req.Body) > 0 && string(req.Body) != "null" {
		if req.Normalize {
			model := config.DefaultModelID
			var peek struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(req.Body, &peek)
			if peek.Model != "" {
				model = peek.Model
			}
			payload = s.normalizeBody(req.Body, model, acc.JWT, acc.DeviceMid)
		} else {
			payload = rewriteDumpMetadata(req.Body, acc.JWT, acc.DeviceMid)
		}
	}
	beta := "mid-conversation-system-2026-04-07"
	if strings.TrimSpace(req.Beta) != "" {
		beta = strings.TrimSpace(req.Beta)
	}
	up := *s.up
	if up.Cfg.LoadSwaps < 2 {
		up.Cfg.LoadSwaps = 2
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	t0 := time.Now()
	extra := http.Header{"anthropic-beta": []string{beta}}
	resp, err := up.PostMessages(ctx, acc.JWT, acc.DeviceMid, payload, extra)
	dt := time.Since(t0).Round(time.Millisecond)
	out := map[string]any{
		"ok":      false,
		"account": maskJWT(acc.JWT),
		"id":      acc.ID,
		"bytes":   len(payload),
		"ms":      dt.Milliseconds(),
	}
	if err != nil {
		out["error"] = err.Error()
		log.Printf("[debug-ping] #%d %s %s %s", acc.ID, maskJWT(acc.JWT), dt, err)
		writeJSON(w, out)
		return
	}
	if resp == nil {
		out["error"] = "nil response"
		writeJSON(w, out)
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	out["status"] = resp.StatusCode
	out["origin_code"] = originCode(raw)
	snip := string(raw)
	if len(snip) > 160 {
		snip = snip[:160]
	}
	out["head"] = snip
	out["ok"] = resp.StatusCode == 200 && !upstream.IsLoadVerdict(resp.StatusCode, raw)
	log.Printf("[debug-ping] #%d %s HTTP %d code=%v %s %s", acc.ID, maskJWT(acc.JWT), resp.StatusCode, out["origin_code"], dt, snip)
	writeJSON(w, out)
}

// rewriteDumpMetadata pins metadata.user_id to the account under test so a
// chatdump replay is not a mismatched-identity 3012.
func rewriteDumpMetadata(body []byte, jwt, mid string) []byte {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	if mid == "" {
		mid = store.DeviceMidFor(jwt)
	}
	uid, _ := json.Marshal(map[string]string{
		"device_id":    mid,
		"account_uuid": "",
		"session_id":   store.SessionIDFor(mid),
	})
	meta, _ := obj["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["user_id"] = string(uid)
	obj["metadata"] = meta
	raw, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return raw
}

func originCode(raw []byte) int {
	var w struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(raw, &w)
	return w.Code
}
