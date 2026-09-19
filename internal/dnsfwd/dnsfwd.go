package dnsfwd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kriakiku/potato-network/internal/netstats"
	"github.com/miekg/dns"
)

// Server is a simple DNS forwarder on :53 (no rewrite).
type Server struct {
	upstream string
	stats    *netstats.Store
	udp      *dns.Server
	tcp      *dns.Server
	mu       sync.Mutex
}

func New(upstream string, stats *netstats.Store) *Server {
	return &Server{upstream: normalizeUpstream(upstream), stats: stats}
}

// DetectUpstream reads the first non-loopback nameserver from /etc/resolv.conf
// (Docker embedded DNS is usually 127.0.0.11; compose `dns:` sets custom servers).
// Falls back to Docker embedded DNS 127.0.0.11.
func DetectUpstream() string {
	ns, _ := parseResolv("/etc/resolv.conf")
	for _, h := range ns {
		ip := net.ParseIP(h)
		if ip == nil || ip.IsLoopback() {
			continue // skip 127.0.0.1 / ::1 (ourselves after PointClientsAtLocal)
		}
		return normalizeUpstream(h)
	}
	// Restart in a container that already points at us: prefer Docker's resolver.
	return normalizeUpstream("127.0.0.11")
}

// PointClientsAtLocal rewrites /etc/resolv.conf so the netns (and sidecars) use our :53.
// Preserves search/domain/options; replaces nameserver lines with 127.0.0.1.
func PointClientsAtLocal() error {
	_, keep := parseResolv("/etc/resolv.conf")
	var b strings.Builder
	b.WriteString("nameserver 127.0.0.1\n")
	for _, line := range keep {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write /etc/resolv.conf: %w", err)
	}
	return nil
}

func parseResolv(path string) (nameservers []string, keep []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "nameserver":
			if len(fields) >= 2 {
				nameservers = append(nameservers, fields[1])
			}
		case "search", "domain", "options", "sortlist":
			keep = append(keep, line)
		}
	}
	return nameservers, keep
}

func normalizeUpstream(upstream string) string {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		upstream = "127.0.0.11"
	}
	if _, _, err := net.SplitHostPort(upstream); err != nil {
		upstream = net.JoinHostPort(upstream, "53")
	}
	return upstream
}

func (s *Server) Start() error {
	handler := dns.HandlerFunc(s.serve)
	s.udp = &dns.Server{Addr: ":53", Net: "udp", Handler: handler}
	s.tcp = &dns.Server{Addr: ":53", Net: "tcp", Handler: handler}
	go func() {
		if err := s.udp.ListenAndServe(); err != nil {
			log.Printf("dns udp: %v", err)
		}
	}()
	go func() {
		if err := s.tcp.ListenAndServe(); err != nil {
			log.Printf("dns tcp: %v", err)
		}
	}()
	log.Printf("DNS forwarder :53 → %s", s.upstream)
	return nil
}

func (s *Server) Stop() {
	if s.udp != nil {
		_ = s.udp.Shutdown()
	}
	if s.tcp != nil {
		_ = s.tcp.Shutdown()
	}
}

func (s *Server) Upstream() string { return s.upstream }

func questionDomain(r *dns.Msg) string {
	if r == nil || len(r.Question) == 0 {
		return ""
	}
	return r.Question[0].Name
}

func (s *Server) serve(w dns.ResponseWriter, r *dns.Msg) {
	domain := questionDomain(r)
	c := &dns.Client{Net: "udp", Timeout: 5 * time.Second}
	resp, rtt, err := c.Exchange(r, s.upstream)
	if err != nil {
		c.Net = "tcp"
		resp, rtt, err = c.Exchange(r, s.upstream)
	}
	if err != nil {
		if s.stats != nil {
			s.stats.RecordDNS(domain, rtt, true)
		}
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(m)
		return
	}
	if s.stats != nil {
		s.stats.RecordDNS(domain, rtt, false)
	}
	_ = w.WriteMsg(resp)
}
