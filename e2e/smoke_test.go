//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	defaultAPI       = "http://127.0.0.1:7783"
	defaultContainer = "potatonetwork-e2e"
)

func apiBase() string {
	if v := strings.TrimSpace(os.Getenv("POTATONETWORK_E2E_API")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultAPI
}

func containerName() string {
	if v := strings.TrimSpace(os.Getenv("POTATONETWORK_E2E_CONTAINER")); v != "" {
		return v
	}
	return defaultContainer
}

func TestE2E_APIHealth(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	code, body := apiGET(t, "/v1/health")
	if code != 200 {
		t.Fatalf("health status=%d body=%s", code, body)
	}
	var h map[string]any
	if err := json.Unmarshal([]byte(body), &h); err != nil {
		t.Fatal(err)
	}
	if h["ok"] != true {
		t.Fatalf("health not ok: %s", body)
	}
}

func TestE2E_APIExemptUnderShape(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	putBaseline(t)
	putProfile(t, "AF", "typical") // high CF RTT → measurable last-mile

	// Control-plane must stay fast while shaping is on.
	start := time.Now()
	code, _ := apiGET(t, "/v1/health")
	elapsed := time.Since(start)
	if code != 200 {
		t.Fatalf("health status=%d", code)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("API health took %s under shape (want <500ms) — likely not exempt", elapsed)
	}
}

func TestE2E_ProfileAppliesLossAndDelayFields(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	putBaseline(t)
	putProfile(t, "AF", "poor")

	code, body := apiGET(t, "/v1/profile")
	if code != 200 {
		t.Fatalf("profile status=%d body=%s", code, body)
	}
	var p struct {
		Country     string  `json:"country"`
		Tier        string  `json:"tier"`
		DelayMs     int     `json:"delayMs"`
		LossPercent float64 `json:"lossPercent"`
		Passthrough bool    `json:"passthrough"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Passthrough || p.Country != "AF" || p.Tier != "poor" {
		t.Fatalf("unexpected profile: %+v", p)
	}
	if p.DelayMs < 10 {
		t.Fatalf("DelayMs=%d too small for AF/poor with baseline cf=5", p.DelayMs)
	}
	if p.LossPercent <= 0 {
		t.Fatalf("LossPercent=%v want >0", p.LossPercent)
	}
}

func TestE2E_PathDelayLiteral(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	writeRules(t, `{ "delay_ms": 250 }`)
	assertPathDelayApprox(t, 250, 60)
}

func TestE2E_PathDelayPathExtra(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	// AF/typical + hostRtt cf=5 → route(aws-eu-central-1) = (235-106)/2 = 64
	writeRules(t, `{ "delay_ms": route("aws-eu-central-1") }`)
	assertPathDelayApprox(t, 64, 40)
}

func TestE2E_PathDelayPathExtraHalf(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	writeRules(t, `{ "delay_ms": route("aws-eu-central-1") / 2 }`)
	assertPathDelayApprox(t, 32, 30)
}

func TestE2E_PathDelayJitterAroundFixed(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	// jitter(200, 0) is deterministic
	writeRules(t, `{ "delay_ms": jitter(200, 0) }`)
	assertPathDelayApprox(t, 200, 50)
}

func TestE2E_PathDelayPathExtraPlusJitter(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	// 64 + jitter(150, 0) = 214 exactly
	writeRules(t, `{ "delay_ms": route("aws-eu-central-1") + jitter(150, 0) }`)
	assertPathDelayApprox(t, 214, 50)
}

func TestE2E_PathDelayPathExtraPlusJitterSpread(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	// 64 + (150±40) → [174, 254]; assert mid-range with wide slack
	writeRules(t, `{ "delay_ms": route("aws-eu-central-1") + jitter(150, 40) }`)
	assertPathDelayApprox(t, 214, 90)
}

func TestE2E_ShapeExcludeBypassesPathDelay(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitOrigin(t, 30*time.Second)
	ensureExcludeLoopbackIP(t, "10.66.66.66")

	writeRules(t, `{ "delay_ms": 250 }`)
	putPassthrough(t)
	putBaseline(t)
	putProfile(t, "AF", "typical")

	excluded := httpTTFBURL(t, "http://10.66.66.66/")
	control := httpTTFBURL(t, "http://127.0.0.1/")
	t.Logf("excluded TTFB=%s control TTFB=%s", excluded, control)

	// Excluded IP skips MITM redirect → no rules path delay.
	if excluded > 150*time.Millisecond {
		t.Fatalf("excluded IP TTFB %s too slow (want ≤150ms; SHAPE_EXCLUDE should skip MITM)", excluded)
	}
	// Control still goes through MITM + delay_ms 250.
	if control < 180*time.Millisecond {
		t.Fatalf("control TTFB %s too fast (want ≥180ms with delay_ms 250)", control)
	}
}

func TestE2E_WebSocketEchoViaMITM(t *testing.T) {
	waitHealthy(t, 60*time.Second)
	waitWSEcho(t, 30*time.Second)
	putPassthrough(t)

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	start := time.Now()
	out, err := exec.Command(
		"docker", "run", "--rm",
		"--network", "container:"+containerName(),
		"potatonetwork-wsecho:e2e",
		"-client", "ws://127.0.0.1:8765/",
	).CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("wsecho client: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("unexpected client output: %s", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("websocket echo took %s (want <2s)", elapsed)
	}
	t.Logf("websocket echo ok in %s", elapsed)
}

func waitWSEcho(t *testing.T, timeout time.Duration) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := exec.Command(
			"docker", "run", "--rm",
			"--network", "container:"+containerName(),
			"potatonetwork-wsecho:e2e",
			"-client", "ws://127.0.0.1:8765/",
		).CombinedOutput()
		last = strings.TrimSpace(string(out))
		if err == nil && strings.Contains(last, "ok") {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("wsecho :8765 not ready within %s: last=%q", timeout, last)
}

func ensureExcludeLoopbackIP(t *testing.T, ip string) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	cname := containerName()
	// Idempotent: ignore "File exists".
	out, err := exec.Command("docker", "exec", cname, "ip", "addr", "add", ip+"/32", "dev", "lo").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "File exists") {
		t.Fatalf("ip addr add %s/32: %v\n%s", ip, err, out)
	}
}

func assertPathDelayApprox(t *testing.T, wantMs, slackMs int) {
	t.Helper()
	putPassthrough(t)
	fast := httpTTFB(t)
	t.Logf("passthrough TTFB=%s", fast)

	putBaseline(t)
	putProfile(t, "AF", "typical") // enables MITM; loopback skips netem

	slow := httpTTFB(t)
	t.Logf("shaped TTFB=%s (want ~+%dms ±%d)", slow, wantMs, slackMs)

	delta := slow - fast
	minExtra := time.Duration(wantMs-slackMs) * time.Millisecond
	maxExtra := time.Duration(wantMs+slackMs+150) * time.Millisecond // +150 for TLS/handshake noise
	if delta < minExtra {
		t.Fatalf("extra TTFB %s too small (want ≥%s; fast=%s slow=%s)", delta, minExtra, fast, slow)
	}
	if delta > maxExtra {
		t.Fatalf("extra TTFB %s too large (want ≤%s; fast=%s slow=%s)", delta, maxExtra, fast, slow)
	}
}

func writeRules(t *testing.T, src string) {
	t.Helper()
	path := filepath.Join(e2eDataDir(t), "rules.expr")
	prev, _ := os.ReadFile(path)
	t.Cleanup(func() {
		_ = os.WriteFile(path, prev, 0o644)
	})
	src = strings.TrimSpace(src) + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force distinct mtime on coarse FS and wait until the process reloads.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, body := apiGETQuiet("/v1/rules")
		if code == 200 {
			var st struct {
				Source  string `json:"source"`
				Compile string `json:"compile"`
				OK      bool   `json:"ok"`
			}
			if json.Unmarshal([]byte(body), &st) == nil && st.OK && strings.TrimSpace(st.Source) == strings.TrimSpace(src) {
				return
			}
			if st.Compile != "" {
				t.Fatalf("rules compile error: %s\nsource=%q", st.Compile, src)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("rules.expr not reloaded with expected source within 5s:\n%s", src)
}

func e2eDataDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	candidates := []string{
		filepath.Join(wd, "testdata", "e2e", "data"),
		filepath.Join(wd, "..", "testdata", "e2e", "data"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	t.Fatalf("testdata/e2e/data not found from cwd=%s", wd)
	return ""
}

func waitOrigin(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := exec.Command(
			"docker", "run", "--rm",
			"--network", "container:"+containerName(),
			"curlimages/curl:8.5.0",
			"-sS", "--max-time", "3",
			"http://127.0.0.1/",
		).CombinedOutput()
		last = strings.TrimSpace(string(out))
		if err == nil && strings.Contains(last, "potato-e2e-origin") {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("local origin :80 not ready within %s: last=%q", timeout, last)
}

func waitHealthy(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		code, body := apiGETQuiet("/v1/health")
		last = fmt.Sprintf("%d %s", code, body)
		if code == 200 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("API not healthy within %s: last=%s", timeout, last)
}

func apiGET(t *testing.T, path string) (int, string) {
	t.Helper()
	code, body := apiGETQuiet(path)
	return code, body
}

func apiGETQuiet(path string) (int, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiBase() + path)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b)
}

func apiJSON(t *testing.T, method, path string, payload any) (int, string) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiBase()+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b)
}

func putBaseline(t *testing.T) {
	t.Helper()
	code, body := apiJSON(t, http.MethodPut, "/v1/baseline", map[string]any{
		"hostRtt": map[string]int{"cf": 5},
	})
	if code != 200 {
		t.Fatalf("PUT baseline status=%d body=%s", code, body)
	}
}

func putProfile(t *testing.T, country, tier string) {
	t.Helper()
	code, body := apiJSON(t, http.MethodPut, "/v1/profile", map[string]any{
		"country": country,
		"tier":    tier,
	})
	if code != 200 {
		t.Fatalf("PUT profile status=%d body=%s", code, body)
	}
	// Allow qdisc / nft to settle.
	time.Sleep(500 * time.Millisecond)
}

func putPassthrough(t *testing.T) {
	t.Helper()
	code, body := apiJSON(t, http.MethodPut, "/v1/profile", map[string]any{
		"passthrough": true,
	})
	if code != 200 {
		t.Fatalf("PUT passthrough status=%d body=%s", code, body)
	}
	time.Sleep(300 * time.Millisecond)
}

func originURL() string {
	if v := strings.TrimSpace(os.Getenv("POTATONETWORK_E2E_ORIGIN")); v != "" {
		return v
	}
	return "http://127.0.0.1/"
}

func httpTTFB(t *testing.T) time.Duration {
	t.Helper()
	return httpTTFBURL(t, originURL())
}

func httpTTFBURL(t *testing.T, url string) time.Duration {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	cname := containerName()
	out, err := exec.Command(
		"docker", "run", "--rm",
		"--network", "container:"+cname,
		"curlimages/curl:8.5.0",
		"-sS", "-o", "/dev/null",
		"-w", "%{time_starttransfer}",
		"--connect-timeout", "15",
		"--max-time", "60",
		url,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("curl in netns %s: %v\n%s", url, out, err)
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("parse TTFB %q: %v", out, err)
	}
	return time.Duration(sec * float64(time.Second))
}
