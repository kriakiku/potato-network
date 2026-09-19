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
