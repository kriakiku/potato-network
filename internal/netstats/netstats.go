package netstats

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Latency holds aggregate timings.
type Latency struct {
	SumMs int64 `json:"sumMs"`
	MinMs int64 `json:"minMs"`
	MaxMs int64 `json:"maxMs"`
}

// DomainStat is per-domain counters + latency aggregates (DNS/TLS).
type DomainStat struct {
	Domain     string  `json:"domain"`
	Count      int64   `json:"count"`
	ErrorCount int64   `json:"errorCount"`
	LatencyMs  Latency `json:"latencyMs"`
}

// RequestStat is per host+method+path (query stripped) for HTTP or WebSocket.
type RequestStat struct {
	Host   string `json:"host"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path"`
	// Count is completed observations (HTTP responses / WS with first message).
	Count int64 `json:"count"`
	// Started is upgrade/connect attempts (WebSocket); for HTTP equals Count+errors tracked separately.
	Started    int64   `json:"started,omitempty"`
	ErrorCount int64   `json:"errorCount"`
	LatencyMs  Latency `json:"latencyMs"`
}

// Snapshot is returned by GET /v1/stats.
type Snapshot struct {
	DNS         []DomainStat  `json:"dns"`
	TLSClient   []DomainStat  `json:"tlsClient"`
	TLSUpstream []DomainStat  `json:"tlsUpstream"`
	HTTP        []RequestStat `json:"http"`
	WebSocket   []RequestStat `json:"websocket"`
	Events      []Event       `json:"events"`
	SlowHTTP    []HTTPSample  `json:"slowHTTP"`
}

// Event is a wall-clock timeline marker (query stripped).
type Event struct {
	Kind     string `json:"kind"` // "http_start" | "ws_start"
	Host     string `json:"host"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path"`
	AtUnixMs int64  `json:"atUnixMs"`
}

// HTTPSample is one completed HTTP request timing (start → response/error).
type HTTPSample struct {
	Host       string `json:"host"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	DurationMs int64  `json:"durationMs"`
	Failed     bool   `json:"failed"`
	AtUnixMs   int64  `json:"atUnixMs"`
}

const maxEvents = 2000
const maxHTTPSamples = 2000
const slowHTTPTopN = 5

type bucket struct {
	count      int64
	started    int64
	errorCount int64
	sumMs      int64
	minMs      int64
	maxMs      int64
	hasLatency bool
}

// Store accumulates DNS, TLS, HTTP and WebSocket timings for one Potato process.
type Store struct {
	mu          sync.Mutex
	dns         map[string]*bucket
	tlsClient   map[string]*bucket
	tlsUpstream map[string]*bucket
	http        map[string]*bucket // key: host\0method\0path
	websocket   map[string]*bucket // key: host\0path
	events      []Event
	httpSamples []HTTPSample
}

func New() *Store {
	return &Store{
		dns:         map[string]*bucket{},
		tlsClient:   map[string]*bucket{},
		tlsUpstream: map[string]*bucket{},
		http:        map[string]*bucket{},
		websocket:   map[string]*bucket{},
		events:      nil,
		httpSamples: nil,
	}
}

// NormalizeDomain lowercases and strips a trailing dot / optional port.
func NormalizeDomain(domain string) string {
	d := strings.TrimSpace(strings.ToLower(domain))
	d = strings.TrimSuffix(d, ".")
	if h, _, err := net.SplitHostPort(d); err == nil {
		d = h
	}
	return d
}

// StripQuery returns path without ?query or #fragment; empty → "/".
func StripQuery(path string) string {
	p := path
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func httpKey(host, method, path string) string {
	return NormalizeDomain(host) + "\x00" + strings.ToUpper(strings.TrimSpace(method)) + "\x00" + StripQuery(path)
}

func wsKey(host, path string) string {
	return NormalizeDomain(host) + "\x00" + StripQuery(path)
}

func (s *Store) record(m map[string]*bucket, key string, ms int64, failed bool) {
	if key == "" || strings.HasPrefix(key, "\x00") {
		return
	}
	if ms < 0 {
		ms = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := m[key]
	if b == nil {
		b = &bucket{}
		m[key] = b
	}
	b.count++
	if failed {
		b.errorCount++
	}
	if !b.hasLatency {
		b.minMs = ms
		b.maxMs = ms
		b.hasLatency = true
	} else {
		if ms < b.minMs {
			b.minMs = ms
		}
		if ms > b.maxMs {
			b.maxMs = ms
		}
	}
	b.sumMs += ms
}

func (s *Store) bumpStarted(m map[string]*bucket, key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bumpStartedLocked(m, key)
}

func (s *Store) bumpStartedLocked(m map[string]*bucket, key string) {
	b := m[key]
	if b == nil {
		b = &bucket{}
		m[key] = b
	}
	b.started++
}

func (s *Store) appendEventLocked(ev Event) {
	if ev.Host == "" {
		return
	}
	s.events = append(s.events, ev)
	if len(s.events) > maxEvents {
		s.events = s.events[len(s.events)-maxEvents:]
	}
}

func (s *Store) appendHTTPSampleLocked(sample HTTPSample) {
	if sample.Host == "" {
		return
	}
	s.httpSamples = append(s.httpSamples, sample)
	if len(s.httpSamples) > maxHTTPSamples {
		s.httpSamples = s.httpSamples[len(s.httpSamples)-maxHTTPSamples:]
	}
}

// RecordDNS records one upstream DNS exchange for domain (question name).
func (s *Store) RecordDNS(domain string, rtt time.Duration, failed bool) {
	s.record(s.dns, NormalizeDomain(domain), rtt.Milliseconds(), failed)
}

// RecordTLSClient records the MITM server-side handshake (client ↔ Potato).
func (s *Store) RecordTLSClient(domain string, d time.Duration, failed bool) {
	s.record(s.tlsClient, NormalizeDomain(domain), d.Milliseconds(), failed)
}

// RecordTLSUpstream records the origin TLS handshake (Potato ↔ upstream).
func (s *Store) RecordTLSUpstream(domain string, d time.Duration, failed bool) {
	s.record(s.tlsUpstream, NormalizeDomain(domain), d.Milliseconds(), failed)
}

// RecordHTTP records one HTTP request TTFB (query stripped from path).
func (s *Store) RecordHTTP(host, method, path string, ttfb time.Duration, failed bool) {
	ms := ttfb.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	key := httpKey(host, method, path)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Inline aggregate update (same as record) so we can append the sample under one lock.
	if key == "" || strings.HasPrefix(key, "\x00") {
		return
	}
	b := s.http[key]
	if b == nil {
		b = &bucket{}
		s.http[key] = b
	}
	b.count++
	if failed {
		b.errorCount++
	}
	if !b.hasLatency {
		b.minMs = ms
		b.maxMs = ms
		b.hasLatency = true
	} else {
		if ms < b.minMs {
			b.minMs = ms
		}
		if ms > b.maxMs {
			b.maxMs = ms
		}
	}
	b.sumMs += ms
	parts := strings.SplitN(key, "\x00", 3)
	s.appendHTTPSampleLocked(HTTPSample{
		Host:       parts[0],
		Method:     parts[1],
		Path:       parts[2],
		DurationMs: ms,
		Failed:     failed,
		AtUnixMs:   time.Now().UnixMilli(),
	})
}

// RecordHTTPStart appends an http_start timeline event (call when the request begins).
func (s *Store) RecordHTTPStart(host, method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendEventLocked(Event{
		Kind:     "http_start",
		Host:     NormalizeDomain(host),
		Method:   strings.ToUpper(strings.TrimSpace(method)),
		Path:     StripQuery(path),
		AtUnixMs: time.Now().UnixMilli(),
	})
}

// RecordWSStart increments websocket upgrade attempts for host+path (no query).
func (s *Store) RecordWSStart(host, path string) {
	key := wsKey(host, path)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bumpStartedLocked(s.websocket, key)
	s.appendEventLocked(Event{
		Kind:     "ws_start",
		Host:     NormalizeDomain(host),
		Path:     StripQuery(path),
		AtUnixMs: time.Now().UnixMilli(),
	})
}

// RecordWSFirstMessage records time from upgrade start to first tunnel byte.
func (s *Store) RecordWSFirstMessage(host, path string, d time.Duration, failed bool) {
	s.record(s.websocket, wsKey(host, path), d.Milliseconds(), failed)
}

func snapshotDomainMap(m map[string]*bucket) []DomainStat {
	out := make([]DomainStat, 0, len(m))
	for domain, b := range m {
		out = append(out, DomainStat{
			Domain:     domain,
			Count:      b.count,
			ErrorCount: b.errorCount,
			LatencyMs: Latency{
				SumMs: b.sumMs,
				MinMs: b.minMs,
				MaxMs: b.maxMs,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

func snapshotHTTP(m map[string]*bucket) []RequestStat {
	out := make([]RequestStat, 0, len(m))
	for key, b := range m {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		out = append(out, RequestStat{
			Host:       parts[0],
			Method:     parts[1],
			Path:       parts[2],
			Count:      b.count,
			ErrorCount: b.errorCount,
			LatencyMs: Latency{
				SumMs: b.sumMs,
				MinMs: b.minMs,
				MaxMs: b.maxMs,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func snapshotWS(m map[string]*bucket) []RequestStat {
	out := make([]RequestStat, 0, len(m))
	for key, b := range m {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, RequestStat{
			Host:       parts[0],
			Path:       parts[1],
			Count:      b.count,
			Started:    b.started,
			ErrorCount: b.errorCount,
			LatencyMs: Latency{
				SumMs: b.sumMs,
				MinMs: b.minMs,
				MaxMs: b.maxMs,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func topSlowHTTP(samples []HTTPSample, n int) []HTTPSample {
	if n <= 0 || len(samples) == 0 {
		return nil
	}
	cp := make([]HTTPSample, len(samples))
	copy(cp, samples)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].DurationMs != cp[j].DurationMs {
			return cp[i].DurationMs > cp[j].DurationMs
		}
		return cp[i].AtUnixMs < cp[j].AtUnixMs
	})
	if len(cp) > n {
		cp = cp[:n]
	}
	return cp
}

// Snapshot returns a copy of current aggregates.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]Event, len(s.events))
	copy(events, s.events)
	return Snapshot{
		DNS:         snapshotDomainMap(s.dns),
		TLSClient:   snapshotDomainMap(s.tlsClient),
		TLSUpstream: snapshotDomainMap(s.tlsUpstream),
		HTTP:        snapshotHTTP(s.http),
		WebSocket:   snapshotWS(s.websocket),
		Events:      events,
		SlowHTTP:    topSlowHTTP(s.httpSamples, slowHTTPTopN),
	}
}

// Reset clears all aggregates.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dns = map[string]*bucket{}
	s.tlsClient = map[string]*bucket{}
	s.tlsUpstream = map[string]*bucket{}
	s.http = map[string]*bucket{}
	s.websocket = map[string]*bucket{}
	s.events = nil
	s.httpSamples = nil
}
