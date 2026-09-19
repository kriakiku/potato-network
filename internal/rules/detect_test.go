package rules

import (
	"net/http"
	"testing"
)

func TestDetectCloudflare(t *testing.T) {
	if DetectCloudflare(nil) {
		t.Fatal("nil")
	}
	if !DetectCloudflare(map[string]string{"cf-ray": "abc-SJC"}) {
		t.Fatal("cf-ray")
	}
	if !DetectCloudflare(map[string]string{"cf-cache-status": "HIT"}) {
		t.Fatal("cf-cache-status")
	}
	if !DetectCloudflare(map[string]string{"server": "cloudflare"}) {
		t.Fatal("server")
	}
	if DetectCloudflare(map[string]string{"server": "nginx"}) {
		t.Fatal("nginx")
	}
}

func TestDetectCFHit(t *testing.T) {
	if DetectCFHit(nil) {
		t.Fatal("nil")
	}
	if !DetectCFHit(map[string]string{"cf-cache-status": "HIT"}) {
		t.Fatal("HIT")
	}
	if !DetectCFHit(map[string]string{"cf-cache-status": "hit"}) {
		t.Fatal("hit lower")
	}
	if DetectCFHit(map[string]string{"cf-cache-status": "MISS"}) {
		t.Fatal("MISS")
	}
	if DetectCFHit(map[string]string{"cf-cache-status": "DYNAMIC"}) {
		t.Fatal("DYNAMIC")
	}
	if DetectCFHit(map[string]string{"cf-ray": "abc"}) {
		t.Fatal("cf-ray alone")
	}
}

func TestDetectCloudfront(t *testing.T) {
	if DetectCloudfront(nil) {
		t.Fatal("nil")
	}
	if !DetectCloudfront(map[string]string{"via": "1.1 cloudfront.net (CloudFront)"}) {
		t.Fatal("via")
	}
	if !DetectCloudfront(map[string]string{"x-amz-cf-id": "xyz"}) {
		t.Fatal("x-amz-cf-id")
	}
	if DetectCloudfront(map[string]string{"via": "1.1 other"}) {
		t.Fatal("other via")
	}
}

func TestClassifyCFCache(t *testing.T) {
	cases := []struct {
		h    map[string]string
		want string
	}{
		{nil, CFCacheNone},
		{map[string]string{"server": "nginx"}, CFCacheNone},
		{map[string]string{"cf-cache-status": "HIT"}, "HIT"},
		{map[string]string{"cf-cache-status": "stale", "cf-ray": "x"}, "STALE"},
		{map[string]string{"cf-cache-status": "UPDATING"}, "UPDATING"},
		{map[string]string{"cf-cache-status": "REVALIDATED"}, "REVALIDATED"},
		{map[string]string{"cf-cache-status": "DYNAMIC"}, "DYNAMIC"},
		{map[string]string{"cf-cache-status": "MISS"}, "MISS"},
		{map[string]string{"cf-cache-status": "EXPIRED"}, "EXPIRED"},
		{map[string]string{"cf-cache-status": "BYPASS"}, "BYPASS"},
		{map[string]string{"cf-ray": "abc-SJC"}, "UNKNOWN"},
	}
	for _, tc := range cases {
		if got := CFCacheStatus(tc.h); got != tc.want {
			t.Fatalf("%v: got %q want %q", tc.h, got, tc.want)
		}
	}
}

func TestDetectWebsocket(t *testing.T) {
	req := map[string]string{"upgrade": "websocket"}
	resp := map[string]string{"upgrade": "WebSocket"}
	if !DetectWebsocket(http.StatusSwitchingProtocols, req, resp) {
		t.Fatal("101 upgrade")
	}
	if DetectWebsocket(http.StatusOK, req, resp) {
		t.Fatal("200 should be false")
	}
	if DetectWebsocket(http.StatusSwitchingProtocols, map[string]string{}, resp) {
		t.Fatal("missing req upgrade")
	}
}
