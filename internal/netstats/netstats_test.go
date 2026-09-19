package netstats

import (
	"strings"
	"testing"
	"time"
)

func TestRecordAndSnapshot(t *testing.T) {
	s := New()
	s.RecordDNS("Example.COM.", 10*time.Millisecond, false)
	s.RecordDNS("example.com", 30*time.Millisecond, true)
	s.RecordTLSUpstream("cdn.example.com", 50*time.Millisecond, false)
	s.RecordTLSClient("cdn.example.com", 5*time.Millisecond, false)

	snap := s.Snapshot()
	if len(snap.DNS) != 1 {
		t.Fatalf("dns len=%d want 1", len(snap.DNS))
	}
	d := snap.DNS[0]
	if d.Domain != "example.com" || d.Count != 2 || d.ErrorCount != 1 {
		t.Fatalf("dns unexpected: %+v", d)
	}
	if d.LatencyMs.MinMs != 10 || d.LatencyMs.MaxMs != 30 || d.LatencyMs.SumMs != 40 {
		t.Fatalf("dns latency unexpected: %+v", d.LatencyMs)
	}
	if len(snap.TLSUpstream) != 1 || snap.TLSUpstream[0].Count != 1 {
		t.Fatalf("tls upstream: %+v", snap.TLSUpstream)
	}

	s.Reset()
	if len(s.Snapshot().DNS) != 0 {
		t.Fatal("expected empty after reset")
	}
}

func TestHTTPAndWebSocketStripQuery(t *testing.T) {
	s := New()
	s.RecordHTTP("API.Example.com:443", "get", "/v1/x?session=abc&t=1", 40*time.Millisecond, false)
	s.RecordHTTP("api.example.com", "GET", "/v1/x?other=9", 60*time.Millisecond, false)
	s.RecordHTTP("api.example.com", "GET", "/v1/y", 10*time.Millisecond, false)

	s.RecordWSStart("ws.example.com", "/socket?token=secret")
	s.RecordWSStart("ws.example.com", "/socket?token=other")
	s.RecordWSFirstMessage("ws.example.com", "/socket?token=secret", 100*time.Millisecond, false)

	s.RecordHTTPStart("api.example.com", "GET", "/v1/x?q=1")
	s.RecordHTTPStart("api.example.com", "POST", "/v1/y")

	snap := s.Snapshot()
	if len(snap.HTTP) != 2 {
		t.Fatalf("http len=%d want 2: %+v", len(snap.HTTP), snap.HTTP)
	}
	var x *RequestStat
	for i := range snap.HTTP {
		if snap.HTTP[i].Path == "/v1/x" {
			x = &snap.HTTP[i]
		}
	}
	if x == nil || x.Count != 2 || x.Host != "api.example.com" || x.Method != "GET" {
		t.Fatalf("http /v1/x unexpected: %+v", x)
	}
	if x.LatencyMs.SumMs != 100 {
		t.Fatalf("http latency sum=%d", x.LatencyMs.SumMs)
	}

	if len(snap.WebSocket) != 1 {
		t.Fatalf("ws len=%d: %+v", len(snap.WebSocket), snap.WebSocket)
	}
	ws := snap.WebSocket[0]
	if ws.Path != "/socket" || ws.Started != 2 || ws.Count != 1 {
		t.Fatalf("ws unexpected: %+v", ws)
	}

	if len(snap.Events) < 4 {
		t.Fatalf("events len=%d want >=4: %+v", len(snap.Events), snap.Events)
	}
	kinds := map[string]int{}
	for _, ev := range snap.Events {
		kinds[ev.Kind]++
		if strings.Contains(ev.Path, "?") {
			t.Fatalf("event path still has query: %+v", ev)
		}
	}
	if kinds["ws_start"] != 2 || kinds["http_start"] != 2 {
		t.Fatalf("event kinds: %+v", kinds)
	}
}

func TestStripQuery(t *testing.T) {
	if got := StripQuery("/a?b=1#c"); got != "/a" {
		t.Fatalf("got %q", got)
	}
	if got := StripQuery(""); got != "/" {
		t.Fatalf("got %q", got)
	}
}

func TestSlowHTTPTop5(t *testing.T) {
	s := New()
	// Durations 10..70 ms — top 5 should be 70,60,50,40,30
	for i := 1; i <= 7; i++ {
		path := "/asset/" + strings.Repeat("x", i)
		s.RecordHTTP("cdn.example.com", "GET", path+"?q=1", time.Duration(i*10)*time.Millisecond, false)
	}
	s.RecordHTTP("api.example.com", "POST", "/v1/slow?tok=x", 100*time.Millisecond, true)
	// WS must not appear in slowHTTP
	s.RecordWSFirstMessage("ws.example.com", "/socket", 500*time.Millisecond, false)

	snap := s.Snapshot()
	if len(snap.SlowHTTP) != 5 {
		t.Fatalf("slowHTTP len=%d want 5: %+v", len(snap.SlowHTTP), snap.SlowHTTP)
	}
	if snap.SlowHTTP[0].DurationMs != 100 || snap.SlowHTTP[0].Method != "POST" {
		t.Fatalf("rank1 unexpected: %+v", snap.SlowHTTP[0])
	}
	if snap.SlowHTTP[0].Path != "/v1/slow" {
		t.Fatalf("query not stripped: %+v", snap.SlowHTTP[0])
	}
	if snap.SlowHTTP[0].Failed != true {
		t.Fatalf("expected failed=true: %+v", snap.SlowHTTP[0])
	}
	for i := 1; i < len(snap.SlowHTTP); i++ {
		if snap.SlowHTTP[i].DurationMs > snap.SlowHTTP[i-1].DurationMs {
			t.Fatalf("not sorted desc: %+v", snap.SlowHTTP)
		}
		if strings.Contains(snap.SlowHTTP[i].Path, "?") {
			t.Fatalf("query in path: %+v", snap.SlowHTTP[i])
		}
	}
	for _, sample := range snap.SlowHTTP {
		if sample.Host == "ws.example.com" {
			t.Fatalf("WS leaked into slowHTTP: %+v", sample)
		}
	}

	s.Reset()
	if len(s.Snapshot().SlowHTTP) != 0 {
		t.Fatal("slowHTTP not cleared on reset")
	}
}
