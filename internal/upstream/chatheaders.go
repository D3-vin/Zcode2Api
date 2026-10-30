package upstream

// chatheaders.go — chat header set from 23mac.har (ZCode 3.16.3 Darwin).
// Do not inject max_tokens / thinking / tools; those are the client's body.

import (
	"net/http"

	"zcode2api/internal/config"
	"zcode2api/internal/store"
	"zcode2api/internal/util"
)

func chatRequestHeaders(jwt, appVer, deviceMid, queryID, traceID string, extra http.Header) [][2]string {
	h := [][2]string{}
	add := func(k, v string) {
		if v != "" {
			h = append(h, [2]string{k, v})
		}
	}
	beta := "mid-conversation-system-2026-04-07"
	if extra != nil {
		if v := extra.Get("anthropic-beta"); v != "" {
			beta = v
		}
	}
	if deviceMid == "" {
		deviceMid = store.DeviceMidFor(jwt)
	}
	if queryID == "" {
		queryID = util.GenerateUUID()
	}
	if traceID == "" {
		traceID = util.GenerateUUID()
	}
	add("content-type", "application/json")
	add("Authorization", "Bearer "+jwt)
	add("anthropic-beta", beta)
	add("anthropic-version", "2023-06-01")
	add("HTTP-Referer", "https://zcode.z.ai")
	add("User-Agent", "ZCode/"+appVer+config.ChatUASuffix)
	add("x-api-key", jwt)
	add("X-ZCode-App-Version", appVer)
	add("X-Title", "Z Code@electron")
	add("X-ZCode-Agent", "glm")
	add("X-Platform", config.ClientPlatform)
	add("X-Release-Channel", "production")
	add("X-Client-Language", "en-US")
	add("X-Client-Timezone", config.ClientTimezone)
	add("X-Os-Category", config.ClientOSCategory)
	add("X-Os-Version", config.ChatOSVersion)
	add("x-query-id", queryID)
	add("x-session-id", store.SessionIDFor(deviceMid))
	add("x-request-id", util.GenerateUUID())
	add("x-zcode-query-source", "main_turn")
	add("x-zcode-session-type", "main")
	add("x-zcode-trace-id", traceID)
	add("Connection", "close")
	return h
}
