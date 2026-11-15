// warm.go — account warming for freshly added JWTs (site-registered, no
// manager identity): the pool-admission flow
//
//  1. POST /api/v1/event/report ×2 (app_launch, app_daily_active) — NO
//     Authorization; the 16-field body is keyed by device_mid (upstream
//     dedups daily-active by device_mid+date).
//  2. GET billing/preview and GET marketing/touch?seq=0 → POST billing/claim
//     for every plan_id either list offers that balance does not already
//     have (no name heuristics), preview priority first. Touch plan ids
//     come only from deliveries[].banner.buttons[].action.args.plan_id.
//     After claim code 0 or 1003, one POST marketing/touch/action confirm
//     per campaign_id. 3007 retries once with a re-minted captcha.
//
// No standalone balance call in the sequence; the dashboard quota refresh
// picks the granted state up.
// Events and billing ride the account egress (with a direct retry on
// network failure); the mint is always direct.
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"zcode2api/internal/config"
	"zcode2api/internal/store"
	"zcode2api/internal/util"
)

// WarmReport is the per-account warming outcome (empty step = ok).
type WarmReport struct {
	Account string `json:"account"`
	Events  string `json:"events,omitempty"`
	Claim   string `json:"claim,omitempty"`
	Plans   string `json:"plans,omitempty"`
}

// BuildActivationEvent — the official sendReport 16-field body, field by field. Language/timezone = a real region pair
// (fingerprint.py _LOCALES), same pair the billing headers declare.
func BuildActivationEvent(element, userID, deviceMid, appVersion string) map[string]any {
	return map[string]any{
		"event_id":           util.GenerateUUID(),
		"client_timezone":    config.ClientTimezone,
		"client_language":    "en-US",
		"element_name":       element,
		"event_region":       "app",
		"event_type":         "view",
		"event_text":         "",
		"event_extra_detail": map[string]any{},
		"user_id":            userID,
		"screen_resolution":  "1920x1080",
		"app_version":        appVersion,
		"device_os_category": config.ClientOSCategory,
		"device_os_version":  config.BillingOSVersion,
		"device_mid":         deviceMid,
		"mac_id":             "",
		"marketing_params":   "{}",
	}
}

var activationElements = []string{"app_launch", "app_daily_active"}

func (c *Client) postEvent(ctx context.Context, jwt, element, userID, mid string) error {
	body, _ := json.Marshal(BuildActivationEvent(element, userID, mid, config.BillingClientVersion))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		config.OriginBaseURL+"/api/v1/event/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header = billingHeaders(jwt, mid, config.BillingClientVersion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.egressDo(mid, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("event %s HTTP %d", element, resp.StatusCode)
	}
	return nil
}

// claimOffer is one origin plan_id. Campaign is set when marketing/touch
// named it; preview-only offers leave it empty (no confirm).
type claimOffer struct {
	id, name, campaign string
}

// ClaimOfferedPlans claims every plan the origin offers via billing/preview
// or marketing/touch, skipping ids already on balance. Preview keeps its
// priority order; touch-only ids follow. A preview failure still queries
// touch. Returns claimed names and a joined problem string.
func (c *Client) ClaimOfferedPlans(ctx context.Context, jwt, mid string) (string, string) {
	base := config.OriginBaseURL + "/api/v1/zcode-plan"
	var problems []string
	offers, seen := c.previewOffers(ctx, jwt, mid, base, &problems)
	c.mergeTouchOffers(ctx, jwt, mid, &offers, seen, &problems)

	owned := map[string]bool{}
	if q, err := c.Billing(jwt, mid); err == nil {
		for _, id := range q.PlanIDs {
			owned[strings.ToLower(id)] = true
		}
	}
	var claimed, skipped []string
	confirmed := map[string]bool{}
	for _, p := range offers {
		name := p.name
		if name == "" {
			name = p.id
		}
		if owned[strings.ToLower(p.id)] {
			skipped = append(skipped, name)
			continue
		}
		if err := c.claimPlan(ctx, base, jwt, mid, p.id); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		claimed = append(claimed, name)
		if p.campaign != "" && !confirmed[p.campaign] {
			confirmed[p.campaign] = true
			if err := c.confirmTouch(ctx, jwt, mid, p.campaign); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if len(claimed) == 0 && len(problems) == 0 && len(skipped) > 0 {
		return "", "already have: " + strings.Join(skipped, ", ")
	}
	return strings.Join(claimed, ", "), strings.Join(problems, "; ")
}

func (c *Client) previewOffers(ctx context.Context, jwt, mid, base string, problems *[]string) ([]claimOffer, map[string]int) {
	offers := []claimOffer{}
	seen := map[string]int{}
	status, raw, err := c.getJSON(ctx, http.MethodGet, base+"/billing/preview?app_version="+config.BillingClientVersion+"&platform="+config.ClientPlatform, jwt, mid, nil, nil)
	if err != nil {
		*problems = append(*problems, "preview: "+err.Error())
		return offers, seen
	}
	if status != 200 {
		*problems = append(*problems, fmt.Sprintf("preview HTTP %d: %s", status, BodySnip(raw, 120)))
		return offers, seen
	}
	var pw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Plans []struct {
				PlanID   string `json:"plan_id"`
				Name     string `json:"name"`
				Priority int    `json:"priority"`
			} `json:"plans"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &pw) != nil || pw.Code != 0 {
		*problems = append(*problems, fmt.Sprintf("preview code %d %s", pw.Code, pw.Msg))
		return offers, seen
	}
	sort.SliceStable(pw.Data.Plans, func(i, j int) bool { return pw.Data.Plans[i].Priority > pw.Data.Plans[j].Priority })
	for _, p := range pw.Data.Plans {
		pid := strings.TrimSpace(p.PlanID)
		if pid == "" {
			continue
		}
		key := strings.ToLower(pid)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = len(offers)
		offers = append(offers, claimOffer{id: pid, name: p.Name})
	}
	return offers, seen
}

func (c *Client) mergeTouchOffers(ctx context.Context, jwt, mid string, offers *[]claimOffer, seen map[string]int, problems *[]string) {
	status, raw, err := c.getJSON(ctx, http.MethodGet, config.OriginBaseURL+"/api/v1/marketing/touch?seq=0", jwt, mid, nil, nil)
	if err != nil {
		*problems = append(*problems, "touch: "+err.Error())
		return
	}
	if status != 200 {
		*problems = append(*problems, fmt.Sprintf("touch HTTP %d: %s", status, BodySnip(raw, 120)))
		return
	}
	claims, err := touchPlanClaims(raw)
	if err != nil {
		*problems = append(*problems, "touch: "+err.Error())
		return
	}
	for _, cl := range claims {
		key := strings.ToLower(cl.PlanID)
		if i, ok := seen[key]; ok {
			if (*offers)[i].campaign == "" {
				(*offers)[i].campaign = cl.CampaignID
			}
			continue
		}
		seen[key] = len(*offers)
		*offers = append(*offers, claimOffer{id: cl.PlanID, campaign: cl.CampaignID})
	}
}

// touchClaim is one banner button that carries action.args.plan_id.
type touchClaim struct {
	PlanID     string
	CampaignID string
}

// touchPlanClaims reads plan ids from banner buttons only. background and
// success_popup are not decoded, so a copy of the plan sitting there is
// never claimed. seq=0 is the caller's cursor, not an offer id.
func touchPlanClaims(raw []byte) ([]touchClaim, error) {
	var wrap struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Deliveries []struct {
				CampaignID string `json:"campaign_id"`
				Banner     struct {
					Buttons []struct {
						Action struct {
							Args struct {
								PlanID string `json:"plan_id"`
							} `json:"args"`
						} `json:"action"`
					} `json:"buttons"`
				} `json:"banner"`
			} `json:"deliveries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if wrap.Code != 0 {
		return nil, fmt.Errorf("code %d %s", wrap.Code, wrap.Msg)
	}
	var out []touchClaim
	seen := map[string]bool{}
	for _, d := range wrap.Data.Deliveries {
		cid := strings.TrimSpace(d.CampaignID)
		for _, b := range d.Banner.Buttons {
			pid := strings.TrimSpace(b.Action.Args.PlanID)
			key := strings.ToLower(pid)
			if pid == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, touchClaim{PlanID: pid, CampaignID: cid})
		}
	}
	return out, nil
}

func (c *Client) confirmTouch(ctx context.Context, jwt, mid, campaignID string) error {
	body, _ := json.Marshal(map[string]string{
		"campaign_id": campaignID,
		"action_type": "confirm",
	})
	st, raw, err := c.getJSON(ctx, http.MethodPost, config.OriginBaseURL+"/api/v1/marketing/touch/action", jwt, mid, body, nil)
	if err != nil {
		return fmt.Errorf("touch confirm %s: %w", campaignID, err)
	}
	if st != 200 {
		return fmt.Errorf("touch confirm %s HTTP %d: %s", campaignID, st, BodySnip(raw, 120))
	}
	var cw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(raw, &cw)
	if cw.Code != 0 {
		return fmt.Errorf("touch confirm %s code %d %s", campaignID, cw.Code, cw.Msg)
	}
	return nil
}

// claimPlan: captcha → billing/claim; on 3007
// (captcha rejected) retry once with a freshly minted param. 1003
// already-claimed is a success for warming.
func (c *Client) claimPlan(ctx context.Context, base, jwt, mid, planID string) error {
	body, _ := json.Marshal(map[string]string{"plan_id": planID})
	for attempt := 1; attempt <= 2; attempt++ {
		extra := http.Header{}
		if c.Captcha != nil {
			param, region, err := c.Captcha.Solve(ctx)
			if err != nil {
				return fmt.Errorf("claim %s mint: %w", planID, err)
			}
			if param != "" {
				extra.Set("X-Aliyun-Captcha-Verify-Param", param)
				if region != "" {
					extra.Set("X-Aliyun-Captcha-Verify-Region", region)
				}
			}
		}
		st, raw, err := c.getJSON(ctx, http.MethodPost, base+"/billing/claim", jwt, mid, body, extra)
		if err != nil {
			return fmt.Errorf("claim %s: %w", planID, err)
		}
		if st != 200 && st != 401 && st != 403 {
			return fmt.Errorf("claim %s HTTP %d: %s", planID, st, BodySnip(raw, 120))
		}
		var cw struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(raw, &cw)
		switch {
		case cw.Code == 0 || cw.Code == 1003:
			return nil
		case cw.Code == 3007 && attempt == 1:
			continue // fresh captcha retry
		default:
			return fmt.Errorf("claim %s code %d %s", planID, cw.Code, cw.Msg)
		}
	}
	return fmt.Errorf("claim %s: captcha rejected twice", planID)
}

func (c *Client) getJSON(ctx context.Context, method, url, jwt, mid string, body []byte, extra http.Header) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	h := billingHeaders(jwt, mid, config.BillingClientVersion)
	for k, vs := range extra {
		for _, v := range vs {
			h.Set(k, v)
		}
	}
	req.Header = h
	resp, err := c.egressDo(mid, req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, err
}

// WarmAccount runs the warming sequence for one account (pool admission):
// activation events → preview + marketing/touch → claim. The dashboard
// quota refresh picks the granted state up afterwards.
func (c *Client) WarmAccount(ctx context.Context, jwt, mid string) WarmReport {
	return c.WarmAccountOpts(ctx, jwt, mid, true)
}

// WarmAccountOpts is WarmAccount with an optional claim step. claim=false
// still fires daily-active events but skips billing/claim (Settings toggle
// while chat is live — mass claim burns MIX lines / 3012's the farm).
func (c *Client) WarmAccountOpts(ctx context.Context, jwt, mid string, claim bool) WarmReport {
	rep := WarmReport{Account: mid}
	userID := store.JWTSub(jwt)
	if userID == "" {
		rep.Events = "JWT has no user_id/sub — events skipped"
	} else {
		for _, el := range activationElements {
			if err := c.postEvent(ctx, jwt, el, userID, mid); err != nil {
				rep.Events = err.Error()
				break
			}
		}
	}
	if rep.Events != "" {
		return rep
	}
	if !claim {
		rep.Plans = "warmed (events ok; claim skipped)"
		return rep
	}
	claimed, problems := c.ClaimOfferedPlans(ctx, jwt, mid)
	switch {
	case claimed != "":
		if problems != "" {
			rep.Claim = problems
		}
		rep.Plans = "warmed (events ok; claimed: " + claimed + ")"
	case problems != "":
		rep.Claim = problems
		rep.Plans = "warmed (events ok; claim: " + problems + ")"
	default:
		rep.Plans = "warmed (events ok; no offered plans)"
	}
	return rep
}
