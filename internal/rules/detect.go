package rules

import (
	"net/http"
	"strings"
)

// DetectCloudflare reports whether response headers look like Cloudflare edge.
// Keys in h should already be lowercased (as in Eval).
func DetectCloudflare(h map[string]string) bool {
	if h == nil {
		return false
	}
	if strings.TrimSpace(h["cf-ray"]) != "" {
		return true
	}
	if strings.TrimSpace(h["cf-cache-status"]) != "" {
		return true
	}
	return strings.Contains(strings.ToLower(h["server"]), "cloudflare")
}

// DetectCFHit is true when Cloudflare reports a cache hit (cf-cache-status: HIT).
func DetectCFHit(h map[string]string) bool {
	if h == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(h["cf-cache-status"]), "HIT")
}

// CFCacheNone is the counter key for responses that are not Cloudflare.
const CFCacheNone = "NONE"

// CFCacheStatus returns the raw Cloudflare cf-cache-status (uppercased), or
// CFCacheNone when the response is not Cloudflare. CF without a status header
// uses "UNKNOWN".
func CFCacheStatus(h map[string]string) string {
	if !DetectCloudflare(h) {
		return CFCacheNone
	}
	st := strings.ToUpper(strings.TrimSpace(h["cf-cache-status"]))
	if st == "" {
		return "UNKNOWN"
	}
	return st
}

// ClassifyCFCache is kept as an alias of CFCacheStatus for callers.
func ClassifyCFCache(h map[string]string) string {
	return CFCacheStatus(h)
}

// DetectCloudfront reports whether response headers look like Amazon CloudFront.
func DetectCloudfront(h map[string]string) bool {
	if h == nil {
		return false
	}
	if strings.TrimSpace(h["x-amz-cf-id"]) != "" {
		return true
	}
	return strings.Contains(strings.ToLower(h["via"]), "cloudfront")
}

// DetectWebsocket matches MITM upgrade logic: HTTP 101 + Upgrade: websocket on both sides.
func DetectWebsocket(statusCode int, reqH, respH map[string]string) bool {
	if statusCode != http.StatusSwitchingProtocols {
		return false
	}
	return headerHasToken(reqH, "upgrade", "websocket") &&
		headerHasToken(respH, "upgrade", "websocket")
}

func headerHasToken(h map[string]string, key, want string) bool {
	if h == nil {
		return false
	}
	for _, part := range strings.Split(h[strings.ToLower(key)], ",") {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}
