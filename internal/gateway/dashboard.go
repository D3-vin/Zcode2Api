package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"zcode2api/internal/config"
	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

// handleDashboard serves the embedded SPA (webui/) — "/" renders the
// Account Pool page, other paths fall through to the file handler.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	serveWebUI(w, r)
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	active := s.activeAccount()
	jwt := ""
	if active != nil {
		jwt = maskJWT(active.JWT)
	}
	proxies := 0
	if s.proxies != nil {
		proxies = s.proxies.Len()
	}
	models := []string{}
	modelsMeta := []map[string]any{}
	for _, m := range s.catalog.List() {
		models = append(models, m.ID)
		modelsMeta = append(modelsMeta, map[string]any{
			"id":             m.ID,
			"name":           m.Name,
			"context_window": m.ContextWindow,
			"max_completion": m.MaxCompletion,
		})
	}
	writeJSON(w, map[string]any{
		"running":       true,
		"port":          s.cfg.Port,
		"accounts":      s.store.Count(),
		"dead_models":   s.store.DeadCount(),
		"proxies":       proxies,
		"active_jwt":    jwt,
		"default_model": config.DefaultModelID, // hardwired (reference parity: refs have no such setting)
		"version":       config.AppVersion,
		"models":        models,
		"models_meta":   modelsMeta,
	})
}

func (s *Server) accountsJSON() []map[string]any {
	accs, _ := s.store.Accounts()
	out := make([]map[string]any, 0, len(accs))
	for _, a := range accs {
		ents := entitlementsOf(a)
		out = append(out, map[string]any{
			"id":              a.ID,
			"jwt":             maskJWT(a.JWT),
			"label":           a.Label,
			"active":          a.Active,
			"status":          a.Status,
			"plan":            a.PlanName,
			"expires_at":      a.ExpiresAt,
			"entitlements":    ents,
			"risk_strikes":    a.RiskStrikes,
			"cooling_until":   a.CoolingUntil,
			"disabled_models": a.DisabledModels,
		})
	}
	return out
}

func (s *Server) apiAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"accounts": s.accountsJSON()})
}

type scanJob struct {
	id       int64
	jwt, mid string
}

// scanQuotas fills the accounts' quota snapshots in parallel (the bulk-add
// background tail; same worker cap as the quota refresh button).
func (s *Server) scanQuotas(jobs []scanJob) {
	if len(jobs) == 0 {
		return
	}
	n := s.cfg.BillingWorkers
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if q, err := s.up.Billing(j.jwt, j.mid); err == nil && !upstream.IsEmptyGrant(q) {
				s.mu.Lock()
				_ = s.store.UpdateQuota(j.id, q)
				s.catalog.MergeFromQuota(q)
				s.mu.Unlock()
			}
		}()
	}
	wg.Wait()
	log.Printf("[pool] bulk-add quota scans done: %d accounts", len(jobs))
}

func (s *Server) apiAccountsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		JWT   string `json:"jwt"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.JWT == "" {
		http.Error(w, `{"error":"jwt required"}`, http.StatusBadRequest)
		return
	}
	// bulk: newline-separated JWTs in one field — each lands as its own
	// account (admin paste pattern). The form responds as soon as the rows
	// exist; the per-account billing scans continue in the background
	// (quotas fill in, the monitor re-checks anyway).
	lines := strings.FieldsFunc(body.JWT, func(r rune) bool { return r == 10 || r == 13 })
	if len(lines) > 1 {
		var jobs []scanJob
		added, skipped := 0, 0
		for _, ln := range lines {
			if ln = strings.TrimSpace(ln); ln == "" {
				continue
			}
			if a, err := s.store.Add(ln, body.Label); err != nil {
				skipped++
			} else {
				added++
				jobs = append(jobs, scanJob{id: a.ID, jwt: a.JWT, mid: a.DeviceMid})
			}
		}
		go s.scanQuotas(jobs)
		writeJSON(w, map[string]any{"ok": true, "added": added, "skipped": skipped, "accounts": s.accountsJSON()})
		return
	}
	a, err := s.store.Add(body.JWT, body.Label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if q, err := s.up.Billing(a.JWT, a.DeviceMid); err == nil {
		if upstream.IsEmptyGrant(q) {
			// keep the row: mark expired and hide it (never auto-delete)
			s.store.MarkStatus([]int64{a.ID}, "expired")
			writeJSON(w, map[string]any{"ok": true, "note": "empty grant — marked expired (hidden)", "accounts": s.accountsJSON()})
			return
		}
		_ = s.store.UpdateQuota(a.ID, q)
		s.catalog.MergeFromQuota(q)
	}
	writeJSON(w, map[string]any{"ok": true, "id": a.ID})
}

func (s *Server) apiAccountsRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.store.Remove(body.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) apiAccountsDeleteExpired(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	n, err := s.store.DeleteExpired()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": n, "accounts": s.accountsJSON()})
}

func (s *Server) apiAccountsSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	accs, _ := s.store.Accounts()
	var found *store.Account
	for i := range accs {
		if accs[i].ID == body.ID {
			found = &accs[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"no such account"}`, http.StatusNotFound)
		return
	}
	if found.Status != "" {
		http.Error(w, `{"error":"account is `+found.Status+`"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.Select(body.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// drop per-model stickies so the next chat request lands on this account
	s.mu.Lock()
	s.stickyAcc = map[string]int64{}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "accounts": s.accountsJSON()})
}

func (s *Server) apiAccountsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path, cleanup, err := importFile(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	if cleanup != "" {
		defer os.Remove(cleanup)
	}
	res, err := s.ImportManagerDB(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "import": res, "accounts": s.accountsJSON()})
}

// apiAccountsClaimAll is the dashboard "Claim all": preview → claim every
// offered plan for every account (no activation events — warm owns those).
func (s *Server) apiAccountsClaimAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	okClaim := s.autoClaim
	s.mu.Unlock()
	if !okClaim {
		writeJSONError(w, http.StatusConflict, "auto-claim is off (Settings) — enable it to claim plans")
		return
	}
	var body struct {
		Newest bool `json:"newest"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	accs, err := s.store.Accounts()
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if body.Newest {
		reverseAccounts(accs)
	}
	n := s.cfg.BillingWorkers
	if n < 1 {
		n = 1
	}
	if !s.cfg.CaptchaOff && s.cfg.CaptchaMinters > 0 && n > s.cfg.CaptchaMinters {
		n = s.cfg.CaptchaMinters
	}
	if n > 32 {
		n = 32
	}
	work := accs[:0]
	for _, a := range accs {
		if a.JWT != "" {
			work = append(work, a)
		}
	}
	order := "oldest-first"
	if body.Newest {
		order = "newest-first"
	}
	log.Printf("[claim] all start accounts=%d workers=%d %s", len(work), n, order)
	emit := beginNDJSON(w)
	emit(massEvt{Event: "start", Kind: "claim", Total: len(work)})
	var nDone atomic.Int32
	s.forEachAccount(work, n, func(a store.Account) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		claimed, problems := s.up.ClaimOfferedPlans(ctx, a.JWT, a.DeviceMid)
		cancel()
		logClaim("all", a.ID, a.JWT, claimed, problems)
		ok := claimed != "" || strings.HasPrefix(problems, "already have:")
		detail := claimed
		if detail == "" {
			detail = problems
		}
		if detail == "" {
			detail = "nothing offered"
		}
		emit(massEvt{Event: "tick", Kind: "claim", Done: int(nDone.Add(1)), Total: len(work), ID: a.ID, OK: ok, Detail: clip(fmt.Sprintf("#%d %s", a.ID, detail), 96)})
	})
	log.Printf("[claim] all done")
	emit(massEvt{Event: "done", Kind: "claim", Done: len(work), Total: len(work), OK: true})
}

// apiAccountsClaimOne claims the offered plans of a single account.
func (s *Server) apiAccountsClaimOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	okClaim := s.autoClaim
	s.mu.Unlock()
	if !okClaim {
		writeJSONError(w, http.StatusConflict, "auto-claim is off (Settings) — enable it to claim plans")
		return
	}
	var body struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	accs, _ := s.store.Accounts()
	for _, a := range accs {
		if a.ID != body.ID {
			continue
		}
		log.Printf("[claim] one #%d %s start", a.ID, maskJWT(a.JWT))
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		claimed, problems := s.up.ClaimOfferedPlans(ctx, a.JWT, a.DeviceMid)
		cancel()
		logClaim("one", a.ID, a.JWT, claimed, problems)
		msg := claimed
		if msg == "" {
			msg = problems
		}
		if msg == "" {
			msg = "nothing offered"
		}
		writeJSON(w, map[string]any{"ok": true, "claim": msg})
		return
	}
	log.Printf("[claim] one #%d not found", body.ID)
	http.Error(w, `{"error":"no such account"}`, http.StatusNotFound)
}

func logClaim(kind string, id int64, jwt, claimed, problems string) {
	who := fmt.Sprintf("%s #%d %s", kind, id, maskJWT(jwt))
	switch {
	case claimed != "" && problems != "":
		log.Printf("[claim] %s claimed: %s; also: %s", who, claimed, problems)
	case claimed != "":
		log.Printf("[claim] %s claimed: %s", who, claimed)
	case strings.HasPrefix(problems, "already have:"):
		log.Printf("[claim] %s skip %s", who, problems)
	case problems != "":
		log.Printf("[claim] %s failed: %s", who, problems)
	default:
		log.Printf("[claim] %s nothing offered", who)
	}
}

func (s *Server) apiQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Newest bool `json:"newest"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	emit := beginNDJSON(w)
	emit(massEvt{Event: "start", Kind: "check"})
	checked, dropped := s.refreshAllBillingOrder(body.Newest, func(done, total int, id int64, ok bool, detail string) {
		label := ""
		if id != 0 {
			label = clip(fmt.Sprintf("#%d %s", id, detail), 96)
		}
		emit(massEvt{Event: "tick", Kind: "check", Done: done, Total: total, ID: id, OK: ok, Detail: label})
	})
	emit(massEvt{Event: "done", Kind: "check", Checked: checked, Dropped: dropped, OK: true})
}

// apiAccountsExport backs the pool up as JSON (device identities included).
func (s *Server) apiMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, monitor())
}

func (s *Server) apiAccountsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"accounts": s.store.ExportAccounts()})
}

// apiAccountsImportJSON restores a backup (JSON body or multipart file).
func (s *Server) apiAccountsImportJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Accounts) == 0 {
		http.Error(w, `{"error":"json body {accounts:[...]} required"}`, http.StatusBadRequest)
		return
	}
	added, skipped := s.store.ImportAccounts(payload.Accounts)
	writeJSON(w, map[string]any{"ok": true, "added": added, "skipped": skipped, "accounts": s.accountsJSON()})
}

// apiAccountsEdit: name / token / egress profile binding / disabled models.
func (s *Server) apiAccountsEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID             int64    `json:"id"`
		Name           string   `json:"name"`
		Token          string   `json:"token"`
		Egress         string   `json:"egress"` // "direct" | "pool" | profile id
		DisabledModels []string `json:"disabled_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if body.Name != "" {
		s.store.SetLabel(body.ID, body.Name)
	}
	if body.Token != "" {
		s.store.SetToken(body.ID, body.Token)
	}
	switch body.Egress {
	case "direct":
		s.unbindAccount(body.ID)
	case "pool", "":
		s.bindAccountPool(body.ID)
	default:
		var pid int64
		if _, err := fmt.Sscanf(body.Egress, "%d", &pid); err == nil && pid > 0 {
			s.bindAccount(body.ID, pid)
		}
	}
	if body.DisabledModels != nil {
		norms := make([]string, 0, len(body.DisabledModels))
		for _, m := range body.DisabledModels {
			if n := upstream.Norm(m); n != "" {
				norms = append(norms, n)
			}
		}
		s.store.SetDisabledModels(body.ID, strings.Join(norms, ","))
	}
	writeJSON(w, map[string]any{"ok": true, "accounts": s.accountsJSON()})
}

// apiAccountsRefreshOne refreshes a single account's quota.
func (s *Server) apiAccountsRefreshOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	accs, _ := s.store.Accounts()
	for _, a := range accs {
		if a.ID != body.ID {
			continue
		}
		q, err := s.up.Billing(a.JWT, a.DeviceMid)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = s.store.UpdateQuota(a.ID, q)
		s.catalog.MergeFromQuota(q)
		writeJSON(w, map[string]any{"ok": true, "accounts": s.accountsJSON()})
		return
	}
	http.Error(w, `{"error":"no such account"}`, http.StatusNotFound)
}

// apiAccountsToggle hides/shows an account in dispatch.
func (s *Server) apiAccountsToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID      int64 `json:"id"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if body.Enabled {
		s.store.SetStatusByID(body.ID, "")
	} else {
		s.store.SetStatusByID(body.ID, "disabled")
	}
	writeJSON(w, map[string]any{"ok": true, "accounts": s.accountsJSON()})
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{
			"quota_refresh_min": s.store.Setting("quota_refresh_min", "0"),
			"captcha_pool_min":  s.store.Setting("captcha_pool_min", "3"),
			"captcha_pool_max":  s.store.Setting("captcha_pool_max", "10"),
			"captcha_pool_ttl":  s.store.Setting("captcha_pool_ttl", "95s"),
			"gateway_key_set":   s.cfg.GatewayKey != "",
			"chat_dump":         s.chatDumpPath() != "",
			"chat_dump_path":    s.cfg.ChatDump,
			"auto_claim": func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.autoClaim
			}(),
		})
		return
	}
	var body struct {
		AdminPassword   string `json:"admin_password"`
		GatewayKey      string `json:"gateway_key"`
		QuotaRefreshMin int    `json:"quota_refresh_min"`
		CaptchaPoolMin  int    `json:"captcha_pool_min"`
		CaptchaPoolMax  int    `json:"captcha_pool_max"`
		CaptchaPoolTTL  string `json:"captcha_pool_ttl"`
		ChatDump        *bool  `json:"chat_dump"`
		AutoClaim       *bool  `json:"auto_claim"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.AdminPassword != "" {
		s.cfg.AuthToken = body.AdminPassword // user setting: persisted, wins over env
		s.store.SetSetting("admin_password", body.AdminPassword)
	}
	if body.GatewayKey != "" {
		s.cfg.GatewayKey = body.GatewayKey // user setting: persisted, wins over env
		s.store.SetSetting("gateway_key", body.GatewayKey)
	}
	if body.QuotaRefreshMin >= 0 {
		s.quotaRefreshMin = body.QuotaRefreshMin
		s.store.SetSetting("quota_refresh_min", fmt.Sprint(body.QuotaRefreshMin))
	}
	if body.CaptchaPoolMin > 0 {
		s.poolMin = body.CaptchaPoolMin
		s.store.SetSetting("captcha_pool_min", fmt.Sprint(body.CaptchaPoolMin))
	}
	if body.CaptchaPoolMax > 0 {
		s.poolMax = body.CaptchaPoolMax
		s.store.SetSetting("captcha_pool_max", fmt.Sprint(body.CaptchaPoolMax))
	}
	if body.CaptchaPoolTTL != "" {
		if d, err := time.ParseDuration(body.CaptchaPoolTTL); err == nil && d > 0 {
			s.poolTTL = d
			s.store.SetSetting("captcha_pool_ttl", body.CaptchaPoolTTL)
		}
	}
	if body.ChatDump != nil {
		s.mu.Lock()
		s.chatDump = *body.ChatDump
		s.mu.Unlock()
		v := "0"
		if *body.ChatDump {
			v = "1"
		}
		s.store.SetSetting("chat_dump", v)
	}
	if body.AutoClaim != nil {
		s.mu.Lock()
		s.autoClaim = *body.AutoClaim
		s.mu.Unlock()
		v := "0"
		if *body.AutoClaim {
			v = "1"
		}
		s.store.SetSetting("auto_claim", v)
	}
	writeJSON(w, map[string]any{"ok": true})
}

type massEvt struct {
	Event   string `json:"event"`
	Kind    string `json:"kind,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	ID      int64  `json:"id,omitempty"`
	OK      bool   `json:"ok,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Checked int    `json:"checked,omitempty"`
	Dropped int    `json:"dropped,omitempty"`
}

func beginNDJSON(w http.ResponseWriter) func(massEvt) {
	var mu sync.Mutex
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	return func(ev massEvt) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n"))
		if fl != nil {
			fl.Flush()
		}
	}
}

func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
