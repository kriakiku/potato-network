package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	_ "github.com/breml/rootcerts" // embed Mozilla CA roots (no system ca-certificates package)

	"github.com/kriakiku/potato-network/internal/api"
	"github.com/kriakiku/potato-network/internal/ca"
	"github.com/kriakiku/potato-network/internal/catalog"
	"github.com/kriakiku/potato-network/internal/config"
	"github.com/kriakiku/potato-network/internal/cronbaseline"
	"github.com/kriakiku/potato-network/internal/croncatalog"
	"github.com/kriakiku/potato-network/internal/dnsfwd"
	"github.com/kriakiku/potato-network/internal/mitm"
	"github.com/kriakiku/potato-network/internal/netstats"
	"github.com/kriakiku/potato-network/internal/rules"
	pnruntime "github.com/kriakiku/potato-network/internal/runtime"
	"github.com/kriakiku/potato-network/internal/shape"
)

// @title						PotatoNetwork API
// @version					1.0
// @description				Last-mile network emulator control plane for Docker sidecars.
// @description
// @description				## Base URL
// @description				`http://<host>:7783` — publish **only** the API port from the PotatoNetwork container. JSON lives under `/v1/…`. Opening `http://<host>:7783/` **302-redirects** to these docs.
// @description
// @description				## Spec downloads
// @description				Machine-readable OpenAPI remains available alongside the Markdown API page:
// @description				- [swagger.json](https://kriakiku.github.io/potato-network/swagger.json)
// @description				- [swagger.yaml](https://kriakiku.github.io/potato-network/swagger.yaml)
// @description
// @description				## Auth
// @description				Optional. Set `POTATONETWORK_API_TOKEN` or `POTATONETWORK_API_TOKEN_FILE`. Send `Authorization: Bearer <token>`. If the token is empty, auth is off. `GET /` and `GET /v1/health` stay public.
// @description				When a token **is** set, the API also answers CORS with allow-all (`Access-Control-Allow-Origin/Methods/Headers: *`) so browser UIs can call with Bearer.
// @description
// @description				## Boot profile
// @description				Default is **passthrough**. Set `POTATONETWORK_PROFILE_COUNTRY` (and optional `POTATONETWORK_PROFILE_TIER`, default `typical`) to apply a country profile at start. Change later with `PUT /v1/profile`.
// @description
// @description				## Profile warnings
// @description				`GET`/`PUT /v1/profile` may include `emulationLimited: true` and `warning` when host CF RTT is already ≥ country CF RTT (last-mile delay clamped to 0). That is a **warning**, not an error — the profile still applies. The process also logs `WARN` on change.
// @description
// @description				## Examples
// @description				```bash
// @description				curl -s -X POST "$API/v1/baseline/probe"
// @description				curl -s -X PUT "$API/v1/profile" -H 'Content-Type: application/json' \
// @description				  -d '{"country":"BD","tier":"typical"}'
// @description				curl -s -X PUT "$API/v1/profile" -H 'Content-Type: application/json' \
// @description				  -d '{"passthrough":true}'
// @description				```
// @description
// @description				Baseline lives in `/data/baseline.json`. Cron: `POTATONETWORK_BASELINE_CRON` (default ~every 3h). Docs home: [PotatoNetwork](https://kriakiku.github.io/potato-network/).
// @contact.name				PotatoNetwork
// @contact.url				https://github.com/kriakiku/potato-network
// @license.name				MIT
// @license.url				https://github.com/kriakiku/potato-network/blob/main/LICENSE
// @host						localhost:7783
// @BasePath					/
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Optional. Format: Bearer followed by the API token (`POTATONETWORK_API_TOKEN`).
func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	uplink, err := shape.ResolveUplink(cfg.Uplink)
	if err != nil {
		log.Fatalf("uplink: %v", err)
	}
	log.Printf("PotatoNetwork uplink=%s data=%s api=%s", uplink, cfg.DataDir, cfg.APIAddr)
	if len(cfg.ShapeExclude) > 0 {
		parts := make([]string, len(cfg.ShapeExclude))
		for i := range cfg.ShapeExclude {
			parts[i] = cfg.ShapeExclude[i].String()
		}
		log.Printf("shape exclude: %s", strings.Join(parts, ", "))
	}
	apiPort := 7783
	if _, portStr, err := net.SplitHostPort(cfg.APIAddr); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			apiPort = p
		}
	}

	cat, err := catalog.NewManager(cfg.DataDir)
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}

	// Upstream from container resolv.conf (Docker 127.0.0.11 or compose `dns:`), then point clients at us.
	dnsUpstream := dnsfwd.DetectUpstream()
	if err := dnsfwd.PointClientsAtLocal(); err != nil {
		log.Printf("WARN: resolv.conf → 127.0.0.1: %v (set dns: [127.0.0.1] on this service)", err)
	}

	sh := shape.New(uplink, apiPort, dnsUpstream, cfg.ShapeExclude)
	st := pnruntime.New(cfg.DataDir, cat, sh)
	if cfg.ProfileCountry != "" {
		p, err := st.ApplyCountryTier(cfg.ProfileCountry, cfg.ProfileTier)
		if err != nil {
			log.Fatalf("boot profile %s/%s: %v", cfg.ProfileCountry, cfg.ProfileTier, err)
		}
		log.Printf("boot profile country=%s tier=%s id=%s", p.Country, p.Tier, p.ID)
	} else if err := st.ClearPassthrough(); err != nil {
		log.Fatalf("boot passthrough: %v", err)
	}

	bundle, err := ca.LoadOrCreate(cfg.DataDir)
	if err != nil {
		log.Fatalf("ca: %v", err)
	}
	log.Printf("CA ready at %s", ca.CertPath(cfg.DataDir))

	stats := netstats.New()

	rulesPath := rules.Path(cfg.DataDir)
	if err := rules.EnsureDefault(rulesPath); err != nil {
		log.Fatalf("rules: %v", err)
	}
	eng := rules.New(rulesPath, st.PathExtraDelayMs, func() rules.ProfileInfo {
		p := st.Profile()
		return rules.ProfileInfo{Country: p.Country, Tier: p.Tier, Passthrough: p.Passthrough}
	}, cfg.PathDelayMaxMs)
	if err := eng.Reload(); err != nil {
		log.Printf("rules compile warning: %v (path delay disabled until fixed)", err)
	}

	dns := dnsfwd.New(dnsUpstream, stats)
	if err := dns.Start(); err != nil {
		log.Fatalf("dns: %v", err)
	}

	proxy := mitm.New(config.MITMPort, bundle, eng, st, stats, cfg.TLSInsecure)
	if err := proxy.Start(); err != nil {
		log.Fatalf("mitm: %v", err)
	}
	if cfg.TLSInsecure {
		log.Printf("WARN TLS insecure: upstream cert verification disabled")
	}
	// InstallBase recreates the nftables table — re-apply API/DNS/exclude marks.
	if err := sh.EnsureExempt(); err != nil {
		log.Fatalf("shape exempt: %v", err)
	}

	cronbaseline.Start(cfg.BaselineCron, st)
	croncatalog.Start(cfg.CatalogCron, cat, cfg.RadarCatalogURL)

	srv := api.New(cfg, st, cat, sh, eng, bundle, stats)
	httpSrv := &http.Server{Addr: cfg.APIAddr, Handler: srv.Handler()}
	go func() {
		log.Printf("API listening %s (auth=%v)", cfg.APIAddr, cfg.APIToken != "")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("shutting down…")
	proxy.Stop()
	dns.Stop()
	_ = sh.Clear()
	_ = httpSrv.Close()
}
