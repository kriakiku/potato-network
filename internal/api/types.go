package api

// ErrorResponse is the standard JSON error body.
type ErrorResponse struct {
	Error string `json:"error" example:"unauthorized"`
}

// HealthResponse is returned by GET /v1/health.
type HealthResponse struct {
	OK      bool   `json:"ok" example:"true"`
	Product string `json:"product" example:"PotatoNetwork"`
	Profile string `json:"profile" example:"passthrough"`
	Shape   string `json:"shape" example:"passthrough"`
}

// ProfilePutRequest is the body for PUT /v1/profile.
type ProfilePutRequest struct {
	Country     string `json:"country" example:"BD"`
	Tier        string `json:"tier" example:"typical"`
	Passthrough bool   `json:"passthrough" example:"false"`
}

// BaselineResponse is returned by baseline GET/PUT/probe.
type BaselineResponse struct {
	HostRtt  map[string]int `json:"hostRtt"`
	ProbedAt string         `json:"probedAt" example:"2026-01-01T00:00:00Z"`
}

// BaselinePutRequest is the body for PUT /v1/baseline.
type BaselinePutRequest struct {
	HostRtt map[string]int `json:"hostRtt"`
}

// CatalogRefreshResponse is returned by POST /v1/catalog/refresh.
type CatalogRefreshResponse struct {
	OK          bool   `json:"ok" example:"true"`
	GeneratedAt string `json:"generatedAt" example:"2026-01-01T00:00:00Z"`
}

// RulesStatus is returned by GET /v1/rules.
type RulesStatus struct {
	Path    string `json:"path" example:"/data/rules.expr"`
	Source  string `json:"source"`
	Compile string `json:"compile"`
	OK      bool   `json:"ok" example:"true"`
}

// StatsResetResponse is returned by POST /v1/stats/reset.
type StatsResetResponse struct {
	OK bool `json:"ok" example:"true"`
}
