package captcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zcode2api/internal/config"
	"zcode2api/internal/util"
)

type D3Solver struct {
	cfg       config.Config
	PickProxy func() *url.URL
	http      *http.Client
}

func NewD3Solver(cfg config.Config) *D3Solver {
	return &D3Solver{
		cfg: cfg,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{Proxy: nil}, // D3 API itself is direct
		},
	}
}

func (d *D3Solver) Enabled() bool {
	return d != nil && strings.TrimSpace(d.cfg.CaptchaD3Key) != ""
}

func (d *D3Solver) Close() {}

func (d *D3Solver) Solve(ctx context.Context) (string, string, error) {
	if !d.Enabled() {
		return "", "", fmt.Errorf("%w: no CAPTCHA_D3_KEY", ErrCaptchaUnavailable)
	}
	cc := fetchCaptchaCfg(ctx)
	if !cc.Enabled {
		return "", "", fmt.Errorf("%w: captcha disabled by origin config", ErrCaptchaUnavailable)
	}
	tries := d.cfg.CaptchaRetries
	if tries < 1 {
		tries = 1
	}
	if tries > 3 {
		tries = 3 // D3 is remote; leftover budget belongs to the node fallback
	}
	var last error
	for attempt := 1; attempt <= tries; attempt++ {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		log.Printf("[captcha] claim solver=d3 attempt %d/%d", attempt, tries)
		param, err := d.solveOnce(ctx, cc)
		if err == nil {
			log.Printf("[captcha] d3 minted param %s (%d bytes, region %s, attempt %d/%d)", util.TruncateToken(param), len(param), cc.Region, attempt, tries)
			return param, cc.Region, nil
		}
		last = err
		log.Printf("[captcha] d3 attempt %d/%d failed: %v", attempt, tries, err)
	}
	return "", "", fmt.Errorf("%w: %v", ErrCaptchaUnavailable, last)
}

func (d *D3Solver) solveOnce(ctx context.Context, cc captchaCfg) (string, error) {
	base := strings.TrimRight(d.cfg.CaptchaD3URL, "/")
	if base == "" {
		base = "d3"
	}
	task := map[string]any{
		"type":       "aliyun",
		"websiteURL": strings.TrimRight(config.OriginBaseURL, "/") + "/",
		"websiteKey": cc.SceneID,
		"prefix":     cc.Prefix,
		"region":     cc.Region,
	}
	if d.cfg.CaptchaD3Proxy && d.PickProxy != nil {
		if u := d.PickProxy(); u != nil {
			host, port, user, pass := splitProxy(u)
			if host != "" && port != "" {
				task["proxyType"] = "http"
				task["proxyAddress"] = host
				task["proxyPort"] = port
				if user != "" {
					task["proxyLogin"] = user
					task["proxyPassword"] = pass
				}
			}
		}
	}
	var created struct {
		ErrorID          int    `json:"errorId"`
		TaskID           string `json:"taskId"`
		ErrorDescription string `json:"errorDescription"`
	}
	if err := d.postJSON(ctx, base+"/createTask", map[string]any{
		"clientKey": d.cfg.CaptchaD3Key,
		"task":      task,
	}, &created); err != nil {
		return "", err
	}
	if created.ErrorID != 0 || created.TaskID == "" {
		return "", fmt.Errorf("createTask: id=%d %s", created.ErrorID, created.ErrorDescription)
	}
	deadline := time.Now().Add(d.cfg.CaptchaTimeout)
	if d.cfg.CaptchaTimeout <= 0 {
		deadline = time.Now().Add(40 * time.Second)
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
		var got struct {
			ErrorID          int             `json:"errorId"`
			Status           string          `json:"status"`
			ErrorDescription string          `json:"errorDescription"`
			Solution         json.RawMessage `json:"solution"`
		}
		if err := d.postJSON(ctx, base+"/getTaskResult", map[string]any{
			"clientKey": d.cfg.CaptchaD3Key,
			"taskId":    created.TaskID,
		}, &got); err != nil {
			return "", err
		}
		st := strings.ToLower(strings.TrimSpace(got.Status))
		if st == "processing" {
			continue
		}
		if st == "ready" {
			raw := d3SolutionValue(got.Solution)
			param := valueToParam(raw, cc.SceneID)
			if !ReadyCaptchaHeader(param) {
				return "", fmt.Errorf("d3 ready but param not usable")
			}
			return param, nil
		}
		if got.ErrorID != 0 || st == "failed" || st == "error" {
			return "", fmt.Errorf("getTaskResult: %s %s", st, got.ErrorDescription)
		}
	}
	return "", fmt.Errorf("d3 timeout")
}

func (d *D3Solver) postJSON(ctx context.Context, url string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

func d3SolutionValue(sol json.RawMessage) string {
	if len(sol) == 0 {
		return ""
	}
	var obj struct {
		Value json.RawMessage `json:"value"`
		Token string          `json:"token"`
	}
	if json.Unmarshal(sol, &obj) == nil {
		if s := unwrapJSONString(obj.Value); s != "" {
			return s
		}
		if obj.Token != "" {
			return obj.Token
		}
	}
	return unwrapJSONString(sol)
}

func unwrapJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func valueToParam(raw, scene string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		return text
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(text), &parsed) != nil {
		return text
	}
	token, _ := parsed["securityToken"].(string)
	cid, _ := parsed["certifyId"].(string)
	sid, _ := parsed["sceneId"].(string)
	if result, ok := parsed["Result"].(map[string]any); ok {
		if token == "" {
			token, _ = result["securityToken"].(string)
		}
		if cid == "" {
			cid, _ = result["certifyId"].(string)
		}
	}
	if sid == "" {
		sid = scene
	}
	if token == "" || cid == "" || sid == "" {
		return text
	}
	payload, _ := json.Marshal(map[string]any{
		"certifyId":     cid,
		"sceneId":       sid,
		"isSign":        true,
		"securityToken": token,
	})
	return base64.StdEncoding.EncodeToString(payload)
}

func splitProxy(u *url.URL) (host, port, user, pass string) {
	if u == nil {
		return
	}
	host = u.Hostname()
	port = u.Port()
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	return
}

var _ Solver = (*D3Solver)(nil)
