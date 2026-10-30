package captcha

// captchasolver.go — captcha scene/region/prefix config: live from the
// origin client/configs (5-minute cache), cn fallback ("旧值 sgp 已过期" —
// sgp is expired, the live-measured region is cn). The client/configs
// fetch below is THE single fetch of that endpoint in the codebase: the
// captcha section parsed here, the model metadata parsed by the gateway
// catalog (models.go). Consumed by the node solver (nodesolver.go);
// earlier mint backends live in git history.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"zcode2api/internal/config"
)

var ErrCaptchaUnavailable = errors.New("captcha solver unavailable")

// defaultCaptchaCfg — both references' fallback.
var defaultCaptchaCfg = captchaCfg{Enabled: true, Prefix: "no8xfe", Region: "cn", SceneID: "11xygtvd"}

type captchaCfg struct {
	Enabled bool
	Prefix  string
	Region  string
	SceneID string
	// SkipModelRequest: the origin lifted the captcha requirement for
	// chat requests (client/configs, observed 2026-10-08 with IDE 3.14.4:
	// POST /messages → 200 with no captcha headers at all).
	SkipModelRequest bool
}

// SkipModelRequest reports the origin's skip_model_request flag (live
// client/configs, cached): when true, chat requests carry no captcha
// headers — the origin does not require them. Unknown config (fetch
// failed) reports false: keep minting, the safe default.
func SkipModelRequest(ctx context.Context) bool {
	return fetchCaptchaCfg(ctx).SkipModelRequest
}

// ClientConfigs fetches the origin client/configs document (5-minute
// cache, direct egress, no auth — the endpoint is public). Returns nil
// when the fetch fails and no cached copy exists.
func ClientConfigs(ctx context.Context) []byte {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if time.Since(cfgAt) < 5*time.Minute && cfgRaw != nil {
		return cfgRaw
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		config.OriginBaseURL+"/api/v1/client/configs?app_version="+url.QueryEscape(config.ClientVersion)+"&platform="+url.QueryEscape(config.ClientPlatform), nil)
	if err != nil {
		return cfgRaw
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}
	if resp, err := client.Do(req); err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) > 0 {
			cfgRaw, cfgAt = body, time.Now()
		}
	}
	return cfgRaw
}

var (
	cfgMu  sync.Mutex
	cfgRaw []byte
	cfgAt  time.Time
)

// ResetConfigsCache drops the cached client/configs document — tests use
// it to control the skip_model_request state deterministically.
func ResetConfigsCache() {
	cfgMu.Lock()
	cfgRaw, cfgAt = nil, time.Time{}
	cfgMu.Unlock()
}

// fetchCaptchaCfg parses the captcha section of client/configs, falling
// back to the baked defaults when there is no data (dead egress, mitm
// CA, ...).
func fetchCaptchaCfg(ctx context.Context) captchaCfg {
	c := defaultCaptchaCfg
	body := ClientConfigs(ctx)
	if body == nil {
		return c
	}
	var wrap struct {
		Code int `json:"code"`
		Data struct {
			Configs struct {
				Captcha struct {
					Enabled          *bool  `json:"enabled"`
					Prefix           string `json:"prefix"`
					Region           string `json:"region"`
					SceneID          string `json:"sceneId"`
					SkipModelRequest bool   `json:"skip_model_request"`
				} `json:"captcha"`
			} `json:"configs"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wrap) != nil || wrap.Code != 0 {
		return c
	}
	cc := wrap.Data.Configs.Captcha
	if cc.Prefix != "" {
		c = captchaCfg{Enabled: cc.Enabled == nil || *cc.Enabled, Prefix: cc.Prefix, Region: cc.Region, SceneID: cc.SceneID, SkipModelRequest: cc.SkipModelRequest}
	}
	return c
}
