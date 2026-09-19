package rules

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// DefaultScript: no path delay (route("cf") is 0); keeps passthrough explicit.
const DefaultScript = `if passthrough {
  { "delay_ms": 0 }
} else {
  { "delay_ms": route("cf") }
}`

// DefaultMaxDelayMs caps a single path-delay sleep (overridable via ENV).
const DefaultMaxDelayMs = 60_000

// PathExtraFunc returns one-way path-extra delay in ms for a catalog destination id.
type PathExtraFunc func(dest string) int

// ProfileFunc returns the active last-mile profile snapshot for expr bindings.
type ProfileFunc func() ProfileInfo

// ProfileInfo is exposed to rules.expr as country / tier / passthrough.
type ProfileInfo struct {
	Country     string
	Tier        string
	Passthrough bool
}

// Result is the evaluated path-delay policy. New fields can be added later without
// changing the expr return shape convention (a map of named values).
type Result struct {
	DelayMs int `expr:"delay_ms"`
}

type Engine struct {
	mu         sync.RWMutex
	path       string
	mtime      time.Time
	program    *vm.Program
	err        error
	source     string
	pathExtra  PathExtraFunc
	profile    ProfileFunc
	maxDelayMs int
}

func New(path string, pathExtra PathExtraFunc, profile ProfileFunc, maxDelayMs int) *Engine {
	if maxDelayMs <= 0 {
		maxDelayMs = DefaultMaxDelayMs
	}
	if pathExtra == nil {
		pathExtra = func(string) int { return 0 }
	}
	if profile == nil {
		profile = func() ProfileInfo { return ProfileInfo{Passthrough: true} }
	}
	e := &Engine{path: path, pathExtra: pathExtra, profile: profile, maxDelayMs: maxDelayMs}
	_ = e.Reload()
	return e
}

func Path(dataDir string) string {
	return dataDir + "/rules.expr"
}

func EnsureDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(DefaultScript+"\n"), 0o644)
}

func (e *Engine) Path() string { return e.path }

func (e *Engine) Source() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

func (e *Engine) CompileErr() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *Engine) MaxDelayMs() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.maxDelayMs
}

func (e *Engine) Reload() error {
	_ = EnsureDefault(e.path)
	info, err := os.Stat(e.path)
	if err != nil {
		e.mu.Lock()
		e.err = err
		e.program = nil
		e.mu.Unlock()
		return err
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		return err
	}
	src := strings.TrimSpace(string(data))
	if src == "" {
		src = DefaultScript
	}
	prog, err := expr.Compile(src, expr.Env(e.compileEnv()), expr.AsAny())
	e.mu.Lock()
	defer e.mu.Unlock()
	e.source = src
	e.mtime = info.ModTime()
	if err != nil {
		e.err = err
		e.program = nil
		return err
	}
	e.err = nil
	e.program = prog
	return nil
}

func (e *Engine) ReloadIfChanged() {
	info, err := os.Stat(e.path)
	if err != nil {
		return
	}
	e.mu.RLock()
	same := info.ModTime().Equal(e.mtime)
	e.mu.RUnlock()
	if !same {
		_ = e.Reload()
	}
}

func (e *Engine) compileEnv() map[string]any {
	return map[string]any{
		"phase":        "",
		"host":         "",
		"path":         "",
		"request":      map[string]string{},
		"response":     map[string]string{},
		"country":      "",
		"tier":         "",
		"passthrough":  false,
		"status_code":  0,
		"cloudflare":   false,
		"cloudfront":   false,
		"websocket":    false,
		"header":       headerFn,
		"match":        matchFn,
		"lower":        strings.ToLower,
		"route":        e.routeFn,
		"jitter":       jitterFn,
	}
}

func (e *Engine) routeFn(dest string) int {
	e.mu.RLock()
	fn := e.pathExtra
	e.mu.RUnlock()
	if fn == nil || dest == "" || dest == "cf" {
		return 0
	}
	return fn(dest)
}

func (e *Engine) currentProfile() ProfileInfo {
	e.mu.RLock()
	fn := e.profile
	e.mu.RUnlock()
	if fn == nil {
		return ProfileInfo{Passthrough: true}
	}
	return fn()
}

// Eval runs the script and returns a clamped Result (today: delay_ms).
// statusCode is the origin HTTP status (used for websocket / status_code bindings).
func (e *Engine) Eval(phase, host, path string, statusCode int, reqH, respH map[string]string) (Result, error) {
	e.ReloadIfChanged()
	e.mu.RLock()
	prog := e.program
	cerr := e.err
	maxMs := e.maxDelayMs
	e.mu.RUnlock()
	if prog == nil {
		if cerr != nil {
			return Result{}, fmt.Errorf("rules compile: %w", cerr)
		}
		return Result{}, nil
	}
	p := e.currentProfile()
	req := lowerKeys(reqH)
	resp := lowerKeys(respH)
	env := e.compileEnv()
	env["phase"] = phase
	env["host"] = host
	env["path"] = path
	env["request"] = req
	env["response"] = resp
	env["country"] = p.Country
	env["tier"] = p.Tier
	env["passthrough"] = p.Passthrough
	env["status_code"] = statusCode
	env["cloudflare"] = DetectCloudflare(resp)
	env["cloudfront"] = DetectCloudfront(resp)
	env["websocket"] = DetectWebsocket(statusCode, req, resp)
	out, err := expr.Run(prog, env)
	if err != nil {
		return Result{}, err
	}
	r := parseResult(out)
	r.DelayMs = ClampDelayMs(r.DelayMs, maxMs)
	return r, nil
}

func parseResult(out any) Result {
	v, ok := out.(map[string]any)
	if !ok {
		return Result{}
	}
	return Result{DelayMs: asInt(v["delay_ms"])}
}

// ClampDelayMs enforces ≥0 and ≤maxMs (default 60s). Over-max logs a WARN once per call.
func ClampDelayMs(ms, maxMs int) int {
	if ms < 0 {
		return 0
	}
	if maxMs <= 0 {
		maxMs = DefaultMaxDelayMs
	}
	if ms > maxMs {
		log.Printf("WARN path delay %dms exceeds max %dms; clamping", ms, maxMs)
		return maxMs
	}
	return ms
}

// jitterFn implements expr jitter(base_ms, jitter_ms) → base ± uniform jitter, ≥0.
func jitterFn(base, spread any) int {
	return applyJitter(asInt(base), asInt(spread))
}

func applyJitter(base, jitter int) int {
	if base < 0 {
		base = 0
	}
	if jitter <= 0 {
		return base
	}
	delta := rand.IntN(2*jitter+1) - jitter
	out := base + delta
	if out < 0 {
		return 0
	}
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func lowerKeys(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[strings.ToLower(k)] = v
	}
	return out
}

func headerFn(h map[string]string, name string) string {
	if h == nil {
		return ""
	}
	return h[strings.ToLower(name)]
}

func matchFn(re, s string) bool {
	ok, err := matchRegex(re, s)
	if err != nil {
		return false
	}
	return ok
}
