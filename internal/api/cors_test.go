package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kriakiku/potato-network/internal/api"
	"github.com/kriakiku/potato-network/internal/ca"
	"github.com/kriakiku/potato-network/internal/catalog"
	"github.com/kriakiku/potato-network/internal/config"
	"github.com/kriakiku/potato-network/internal/netstats"
	"github.com/kriakiku/potato-network/internal/rules"
	pnruntime "github.com/kriakiku/potato-network/internal/runtime"
	"github.com/kriakiku/potato-network/internal/shape"
)

func TestCORSWhenTokenSet(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, APIAddr: ":0", APIToken: "secret"}
	srv := newTestAPI(t, cfg)

	req := httptest.NewRequest(http.MethodOptions, "/v1/profile", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status=%d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin=%q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "*" {
		t.Fatalf("Allow-Headers=%q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET health missing CORS headers")
	}
}

func TestNoCORSWithoutToken(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, APIAddr: ":0"}
	srv := newTestAPI(t, cfg)

	req := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS without token")
	}
}

func newTestAPI(t *testing.T, cfg config.Config) *api.Server {
	t.Helper()
	cat, err := catalog.NewManager(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	sh := shape.New("lo", 7783, "127.0.0.11:53", nil)
	st := pnruntime.New(cfg.DataDir, cat, sh)
	_ = st.ClearPassthrough()
	bundle, err := ca.LoadOrCreate(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	eng := rules.New(rules.Path(cfg.DataDir), st.PathExtraDelayMs, func() rules.ProfileInfo {
		p := st.Profile()
		return rules.ProfileInfo{Country: p.Country, Tier: p.Tier, Passthrough: p.Passthrough}
	}, cfg.PathDelayMaxMs)
	_ = rules.EnsureDefault(rules.Path(cfg.DataDir))
	_ = eng.Reload()
	return api.New(cfg, st, cat, sh, eng, bundle, netstats.New())
}
