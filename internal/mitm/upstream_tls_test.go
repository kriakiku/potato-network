package mitm

import (
	"net"
	"testing"
	"time"
)

func TestDialUpstreamTLSCloudflare(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	raw, err := d.Dial("tcp", "lobby.winfinity.live:443")
	if err != nil {
		t.Skipf("dial: %v", err)
	}
	defer raw.Close()
	tlsConn, err := dialUpstreamTLS(raw, "lobby.winfinity.live", false)
	if err != nil {
		t.Fatalf("upstream TLS: %v", err)
	}
	defer tlsConn.Close()
	if got := tlsConn.ConnectionState().NegotiatedProtocol; got != "" && got != "http/1.1" {
		t.Fatalf("ALPN=%q want http/1.1 or empty", got)
	}
}
