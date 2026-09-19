package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kriakiku/potato-network/internal/ca"
	"github.com/kriakiku/potato-network/internal/catalog"
	"github.com/kriakiku/potato-network/internal/config"
	"github.com/kriakiku/potato-network/internal/netstats"
	"github.com/kriakiku/potato-network/internal/rules"
	pnruntime "github.com/kriakiku/potato-network/internal/runtime"
	"github.com/kriakiku/potato-network/internal/shape"
)

type Server struct {
	cfg     config.Config
	state   *pnruntime.State
	catalog *catalog.Manager
	shape   *shape.Manager
	rules   *rules.Engine
	ca      *ca.Bundle
	stats   *netstats.Store
	mux     *http.ServeMux
}

func New(cfg config.Config, st *pnruntime.State, cat *catalog.Manager, sh *shape.Manager, eng *rules.Engine, bundle *ca.Bundle, stats *netstats.Store) *Server {
	if stats == nil {
		stats = netstats.New()
	}
	s := &Server{cfg: cfg, state: st, catalog: cat, shape: sh, rules: eng, ca: bundle, stats: stats, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.cors(s.auth(s.mux)) }

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/profile", s.handleProfile)
	s.mux.HandleFunc("/v1/baseline", s.handleBaseline)
	s.mux.HandleFunc("/v1/baseline/probe", s.handleBaselineProbe)
	s.mux.HandleFunc("/v1/catalog", s.handleCatalog)
	s.mux.HandleFunc("/v1/catalog/refresh", s.handleCatalogRefresh)
	s.mux.HandleFunc("/v1/ca.pem", s.handleCA)
	s.mux.HandleFunc("/v1/rules", s.handleRules)
	s.mux.HandleFunc("/v1/stats", s.handleStats)
	s.mux.HandleFunc("/v1/stats/reset", s.handleStatsReset)
}

// cors allows any origin/method/header when an API token is configured
// (browser clients can then call with Authorization: Bearer …).
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIToken != "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Docs redirect + health are public.
		if r.URL.Path == "/" || r.URL.Path == "/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		tok := s.cfg.APIToken
		if tok == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) != tok {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleRoot redirects browsers to the published API docs.
//
//	@Summary		Redirect to docs
//	@Description	302 to GitHub Pages API docs
//	@Tags			system
//	@Produce		json
//	@Success		302	{string}	string	"Location: docs URL"
//	@Failure		404	{object}	ErrorResponse
//	@Router			/ [get]
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	http.Redirect(w, r, config.DocsURL, http.StatusFound)
}

// handleHealth returns liveness and a short profile / shape summary.
//
//	@Summary		Health check
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/v1/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, HealthResponse{
		OK:      true,
		Product: "PotatoNetwork",
		Profile: s.state.StatusSummary(),
		Shape:   s.shape.Status(),
	})
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getProfile(w, r)
	case http.MethodPut:
		s.putProfile(w, r)
	default:
		w.WriteHeader(405)
	}
}

// getProfile returns the active last-mile profile.
//
//	@Summary		Get profile
//	@Tags			profile
//	@Produce		json
//	@Success		200	{object}	profiles.Profile
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/profile [get]
func (s *Server) getProfile(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.state.Profile())
}

// putProfile applies a country/tier profile or clears to passthrough.
//
//	@Summary		Set profile
//	@Tags			profile
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ProfilePutRequest	true	"Country+tier or passthrough"
//	@Success		200		{object}	profiles.Profile
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/profile [put]
func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	var body ProfilePutRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Passthrough || (body.Country == "" && body.Tier == "") {
		if err := s.state.ClearPassthrough(); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, s.state.Profile())
		return
	}
	if body.Tier == "" {
		body.Tier = "typical"
	}
	p, err := s.state.ApplyCountryTier(body.Country, body.Tier)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleBaseline(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getBaseline(w, r)
	case http.MethodPut:
		s.putBaseline(w, r)
	default:
		w.WriteHeader(405)
	}
}

// getBaseline returns host RTT baseline and last probe time.
//
//	@Summary		Get baseline
//	@Tags			baseline
//	@Produce		json
//	@Success		200	{object}	BaselineResponse
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/baseline [get]
func (s *Server) getBaseline(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, BaselineResponse{
		HostRtt:  s.state.HostRtt(),
		ProbedAt: formatTime(s.state.ProbedAt()),
	})
}

// putBaseline sets host RTT values without probing.
//
//	@Summary		Set baseline
//	@Tags			baseline
//	@Accept			json
//	@Produce		json
//	@Param			body	body		BaselinePutRequest	true	"Host RTT map"
//	@Success		200		{object}	BaselineResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/baseline [put]
func (s *Server) putBaseline(w http.ResponseWriter, r *http.Request) {
	var body BaselinePutRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(body.HostRtt) == 0 {
		writeErr(w, 400, "hostRtt required")
		return
	}
	s.state.SetHostRtt(body.HostRtt)
	writeJSON(w, 200, BaselineResponse{
		HostRtt:  s.state.HostRtt(),
		ProbedAt: formatTime(s.state.ProbedAt()),
	})
}

// handleBaselineProbe runs a TCP RTT probe to catalog destinations.
//
//	@Summary		Probe baseline
//	@Tags			baseline
//	@Produce		json
//	@Success		200	{object}	BaselineResponse
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/baseline/probe [post]
func (s *Server) handleBaselineProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	rtt := s.state.ProbeBaseline()
	writeJSON(w, 200, BaselineResponse{
		HostRtt:  rtt,
		ProbedAt: formatTime(s.state.ProbedAt()),
	})
}

// handleCatalog returns the full country catalog.
//
//	@Summary		Get catalog
//	@Tags			catalog
//	@Produce		json
//	@Success		200	{object}	catalog.Catalog
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/catalog [get]
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, 200, s.catalog.Get())
}

// handleCatalogRefresh pulls the catalog from the configured URL.
//
//	@Summary		Refresh catalog
//	@Tags			catalog
//	@Produce		json
//	@Success		200	{object}	CatalogRefreshResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/catalog/refresh [post]
func (s *Server) handleCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if err := s.catalog.RefreshFromURL(s.cfg.RadarCatalogURL); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, CatalogRefreshResponse{
		OK:          true,
		GeneratedAt: s.catalog.Get().GeneratedAt,
	})
}

// handleCA downloads the MITM root CA PEM.
//
//	@Summary		Download CA certificate
//	@Tags			ca
//	@Produce		application/x-pem-file
//	@Success		200	{string}	string	"PEM certificate"
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/ca.pem [get]
func (s *Server) handleCA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=\"potatonetwork-ca.pem\"")
	_, _ = w.Write(s.ca.CertPEM)
}

// handleRules returns rules.expr path, source, and compile status.
//
//	@Summary		Rules status
//	@Tags			rules
//	@Produce		json
//	@Success		200	{object}	RulesStatus
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/rules [get]
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	_ = s.rules.Reload()
	writeJSON(w, 200, RulesStatus{
		Path:    s.rules.Path(),
		Source:  s.rules.Source(),
		Compile: s.rules.CompileErr(),
		OK:      s.rules.CompileErr() == "",
	})
}

// handleStats returns DNS + TLS aggregates collected since boot or last reset.
//
//	@Summary		Get DNS/TLS stats
//	@Tags			stats
//	@Produce		json
//	@Success		200	{object}	netstats.Snapshot
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/stats [get]
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, 200, s.stats.Snapshot())
}

// handleStatsReset clears DNS/TLS aggregates.
//
//	@Summary		Reset DNS/TLS stats
//	@Tags			stats
//	@Produce		json
//	@Success		200	{object}	StatsResetResponse
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/stats/reset [post]
func (s *Server) handleStatsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	s.stats.Reset()
	writeJSON(w, 200, StatsResetResponse{OK: true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
