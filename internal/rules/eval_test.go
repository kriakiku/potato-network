package rules

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	if err := EnsureDefault(path); err != nil {
		t.Fatal(err)
	}
	e := New(path, nil, nil, 0)
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := e.Source(); got != DefaultScript {
		t.Fatalf("source:\n%s\nwant:\n%s", got, DefaultScript)
	}
	r, err := e.Eval("response", "example.com", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 0 {
		t.Fatalf("default passthrough profile: %+v err=%v", r, err)
	}
	e = New(path, nil, func() ProfileInfo {
		return ProfileInfo{Country: "BD", Tier: "typical"}
	}, 0)
	r, err = e.Eval("response", "example.com", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 0 {
		t.Fatalf("default with country (route cf): %+v err=%v", r, err)
	}
}

func TestEvalLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	if err := os.WriteFile(path, []byte(`{ "delay_ms": 250 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, nil, nil, 0)
	r, err := e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 250 {
		t.Fatalf("%+v err=%v", r, err)
	}
}

func TestEvalPathExtraAndJitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	src := `if lower(header(response, "via")) contains "cloudfront" { { "delay_ms": jitter(route("aws-eu-central-1"), 0) } } else { { "delay_ms": 0 } }`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, func(dest string) int {
		if dest == "aws-eu-central-1" {
			return 120
		}
		return 0
	}, nil, 0)
	r, err := e.Eval("response", "cdn.example.com", "/x", 0, nil, map[string]string{"via": "1.1 cloudfront.net (CloudFront)"})
	if err != nil {
		t.Fatal(err)
	}
	if r.DelayMs != 120 {
		t.Fatalf("%+v", r)
	}
	r, err = e.Eval("response", "cdn.example.com", "/x", 0, nil, map[string]string{"via": "1.1 other"})
	if err != nil || r.DelayMs != 0 {
		t.Fatalf("%+v err=%v", r, err)
	}
}

func TestEvalArithmetic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	if err := os.WriteFile(path, []byte(`{ "delay_ms": route("aws-eu-central-1") / 2 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, func(string) int { return 100 }, nil, 0)
	r, err := e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 50 {
		t.Fatalf("%+v err=%v", r, err)
	}
}

func TestEvalClampMax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	if err := os.WriteFile(path, []byte(`{ "delay_ms": 999999 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, nil, nil, 1000)
	r, err := e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 1000 {
		t.Fatalf("%+v err=%v", r, err)
	}
}

func TestEvalPathExtraPlusJitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	src := `{ "delay_ms": route("aws-eu-central-1") + jitter(150, 0) }`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, func(string) int { return 64 }, nil, 0)
	r, err := e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 214 {
		t.Fatalf("%+v err=%v", r, err)
	}
}

func TestParseResultIgnoresBareNumber(t *testing.T) {
	r := parseResult(250)
	if r.DelayMs != 0 {
		t.Fatalf("bare number should not parse as delay_ms: %+v", r)
	}
}

func TestEvalCountryAndTier(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	src := `if passthrough {
  { "delay_ms": 0 }
} else {
  if country == "BD" && tier == "poor" {
    { "delay_ms": 300 }
  } else {
    if country == "BD" {
      { "delay_ms": 100 }
    } else {
      { "delay_ms": route("aws-eu-central-1") }
    }
  }
}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prof := ProfileInfo{Country: "BD", Tier: "typical"}
	e := New(path, func(string) int { return 64 }, func() ProfileInfo { return prof }, 0)

	r, err := e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 100 {
		t.Fatalf("typical: %+v err=%v", r, err)
	}
	prof = ProfileInfo{Country: "BD", Tier: "poor"}
	r, err = e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 300 {
		t.Fatalf("poor: %+v err=%v", r, err)
	}
	prof = ProfileInfo{Passthrough: true}
	r, err = e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 0 {
		t.Fatalf("passthrough: %+v err=%v", r, err)
	}
	prof = ProfileInfo{Country: "AF", Tier: "typical"}
	r, err = e.Eval("response", "h", "/", 0, nil, nil)
	if err != nil || r.DelayMs != 64 {
		t.Fatalf("other country: %+v err=%v", r, err)
	}
}

func TestEvalCDNAndWebsocketFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.expr")
	src := `if websocket {
  { "delay_ms": 1 }
} else {
  if cf_hit {
    { "delay_ms": 4 }
  } else {
    if cloudflare {
      { "delay_ms": 2 }
    } else {
      if cloudfront {
        { "delay_ms": 3 }
      } else {
        { "delay_ms": status_code }
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(path, nil, nil, 0)

	r, err := e.Eval("response", "h", "/", http.StatusSwitchingProtocols,
		map[string]string{"upgrade": "websocket"},
		map[string]string{"upgrade": "websocket"})
	if err != nil || r.DelayMs != 1 {
		t.Fatalf("websocket: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", http.StatusOK, nil,
		map[string]string{"cf-cache-status": "HIT"})
	if err != nil || r.DelayMs != 4 {
		t.Fatalf("cf_hit: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", http.StatusOK,
		map[string]string{"upgrade": "websocket"},
		map[string]string{"upgrade": "websocket", "cf-ray": "abc"})
	if err != nil || r.DelayMs != 2 {
		t.Fatalf("200+upgrade must not be websocket; cloudflare: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", http.StatusOK, nil,
		map[string]string{"server": "cloudflare"})
	if err != nil || r.DelayMs != 2 {
		t.Fatalf("cloudflare server: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", http.StatusOK, nil,
		map[string]string{"x-amz-cf-id": "xyz"})
	if err != nil || r.DelayMs != 3 {
		t.Fatalf("cloudfront id: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", http.StatusOK, nil,
		map[string]string{"via": "1.1 cloudfront.net (CloudFront)"})
	if err != nil || r.DelayMs != 3 {
		t.Fatalf("cloudfront via: %+v err=%v", r, err)
	}

	r, err = e.Eval("response", "h", "/", 418, nil, nil)
	if err != nil || r.DelayMs != 418 {
		t.Fatalf("status_code fallback: %+v err=%v", r, err)
	}
}
