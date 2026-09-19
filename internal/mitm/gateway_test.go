package mitm

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kriakiku/potato-network/internal/ca"
	pnruntime "github.com/kriakiku/potato-network/internal/runtime"
)

func TestWriteBadGateway(t *testing.T) {
	var buf strings.Builder
	writeBadGateway(&buf, fmt.Errorf("dial upstream: connection refused"))
	raw := buf.String()
	if !strings.HasPrefix(raw, "HTTP/1.1 502 Bad Gateway") {
		t.Fatalf("status line missing: %q", raw)
	}
	br := bufio.NewReader(strings.NewReader(raw))
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("empty 502 body")
	}
}

// After the client completes TLS with the MITM, an upstream TLS failure must
// yield HTTP 502 on the client connection — not an empty TLS close (Chrome ERR_EMPTY_RESPONSE).
func TestUpstreamTLSFailureAfterClientHandshakeReturns502(t *testing.T) {
	bundle, err := ca.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Accept TCP then close immediately so upstream tls.Client.Handshake fails.
	originLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer originLn.Close()
	go func() {
		for {
			c, err := originLn.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	origHost, origPortStr, _ := net.SplitHostPort(originLn.Addr().String())
	origPort, _ := strconv.Atoi(origPortStr)

	st := pnruntime.New(t.TempDir(), nil, nil)
	p := New(0, bundle, nil, st, nil, true)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyLn.Close()
	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				p.handleTLS(bufio.NewReader(c), c, origHost, origPort)
			}(c)
		}
	}()

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle.CertPEM) {
		t.Fatal("append CA")
	}
	raw, err := net.DialTimeout("tcp", proxyLn.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	tlsClient := tls.Client(raw, &tls.Config{
		ServerName: "fail.example.test",
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	defer tlsClient.Close()
	_ = tlsClient.SetReadDeadline(time.Now().Add(5 * time.Second))

	br := bufio.NewReader(tlsClient)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("expected HTTP 502 response, got read error (empty close?): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("502 body empty")
	}
	if !strings.Contains(string(body), "upstream TLS handshake") {
		t.Fatalf("body=%q", body)
	}
}
