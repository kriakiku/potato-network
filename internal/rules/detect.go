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
