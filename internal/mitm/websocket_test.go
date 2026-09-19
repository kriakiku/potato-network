package mitm

import (
	"bufio"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
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

func TestIsWebSocketUpgrade(t *testing.T) {
	req := &http.Request{Header: http.Header{"Upgrade": []string{"websocket"}}}
	ok := &http.Response{StatusCode: http.StatusSwitchingProtocols, Header: http.Header{"Upgrade": []string{"WebSocket"}}}
	if !isWebSocketUpgrade(req, ok) {
		t.Fatal("expected websocket upgrade")
	}
	bad := &http.Response{StatusCode: 200, Header: http.Header{"Upgrade": []string{"websocket"}}}
	if isWebSocketUpgrade(req, bad) {
		t.Fatal("200 should not be upgrade")
	}
}

func TestBufferResponseBody(t *testing.T) {
	html := "<html><body>ok</body></html>"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(html)),
		// Chunked-style: no Content-Length
	}
	if err := bufferResponseBody(resp); err != nil {
		t.Fatal(err)
	}
	if resp.ContentLength != int64(len(html)) {
		t.Fatalf("ContentLength=%d", resp.ContentLength)
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(html)) {
		t.Fatalf("header %q", resp.Header.Get("Content-Length"))
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != html {
		t.Fatalf("body=%q", got)
	}
}

func TestTunnelLeftover(t *testing.T) {
	cApp, cProxy := net.Pipe()
	uOrigin, uProxy := net.Pipe()
	defer cApp.Close()
	defer cProxy.Close()
	defer uOrigin.Close()
	defer uProxy.Close()

	ubr := bufio.NewReader(io.MultiReader(
		strings.NewReader("from-up"),
		uProxy,
	))
	cbr := bufio.NewReader(cProxy)

	done := make(chan struct{})
	go func() {
		tunnel(cProxy, cbr, uProxy, ubr, nil)
		close(done)
	}()

	buf := make([]byte, 7)
	if _, err := io.ReadFull(cApp, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "from-up" {
		t.Fatalf("leftover=%q", buf)
	}
	if _, err := cApp.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf = make([]byte, 4)
	if _, err := io.ReadFull(uOrigin, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got=%q", buf)
	}
	_ = cApp.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel did not exit")
	}
}

func TestTunnelClosesPeer(t *testing.T) {
	cApp, cProxy := net.Pipe()
	uOrigin, uProxy := net.Pipe()
	defer cApp.Close()
	defer uOrigin.Close()

	done := make(chan struct{})
	go func() {
		tunnel(cProxy, bufio.NewReader(cProxy), uProxy, bufio.NewReader(uProxy), nil)
		close(done)
	}()

	_ = cApp.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel did not exit after client close")
	}
	// Upstream side should be closed so reads fail promptly.
	_ = uOrigin.SetDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := uOrigin.Read(buf); err == nil {
		t.Fatal("expected read error on closed upstream peer")
	}
}

func TestWebSocketUpgradeTunnelHTTP(t *testing.T) {
	originLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer originLn.Close()
	go serveWSEcho(t, originLn, nil)

	origHost, origPortStr, _ := net.SplitHostPort(originLn.Addr().String())
	origPort, _ := strconv.Atoi(origPortStr)

	st := pnruntime.New(t.TempDir(), nil, nil)
	p := New(0, nil, nil, st, nil, false)

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
				br := bufio.NewReader(c)
				p.handleHTTP(br, c, origHost, origPort)
			}(c)
		}
	}()

	wsEchoRoundTrip(t, "ws", proxyLn.Addr().String(), "")
}

func TestWebSocketUpgradeTunnelTLS(t *testing.T) {
	bundle, err := ca.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leafKey, leaf, err := bundle.TLSCertificate("ws.example.test")
	if err != nil {
		t.Fatal(err)
	}
	originCert := &tls.Certificate{
		Certificate: [][]byte{leaf.Raw},
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}

	originLn, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{*originCert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer originLn.Close()
	go serveWSEcho(t, originLn, nil)

	origHost, origPortStr, _ := net.SplitHostPort(originLn.Addr().String())
	origPort, _ := strconv.Atoi(origPortStr)

	st := pnruntime.New(t.TempDir(), nil, nil)
	p := New(0, bundle, nil, st, nil, true) // insecure: origin uses Potato-minted leaf without system trust

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
				// Pretend REDIRECT delivered a TLS ClientHello destined for origin:443.
				br := bufio.NewReader(c)
				// Client will speak TLS to us; forge SNI via peek — inject by wrapping
				// with a TLS client that sets ServerName, then handleTLS peeks SNI from
				// the real ClientHello. Use handleTLS with orig IP/port.
				p.handleTLS(br, c, origHost, origPort)
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
		ServerName: "ws.example.test",
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	defer tlsClient.Close()
	wsEchoRoundTripConn(t, tlsClient, "ws.example.test")
}

// serveWSEcho accepts HTTP(S) connections and echoes WebSocket payloads as raw frames.
func serveWSEcho(t *testing.T, ln net.Listener, _ *tls.Config) {
	t.Helper()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			br := bufio.NewReader(c)
			req, err := http.ReadRequest(br)
			if err != nil {
				return
			}
			if !headerHasToken(req.Header, "Upgrade", "websocket") {
				_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
				return
			}
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := wsAcceptKey(key)
			resp := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
			if _, err := io.WriteString(c, resp); err != nil {
				return
			}
			// Echo: read masked client frames, write unmasked server frames.
			for {
				b0, err := br.ReadByte()
				if err != nil {
					return
				}
				b1, err := br.ReadByte()
				if err != nil {
					return
				}
				masked := b1&0x80 != 0
				n := int(b1 & 0x7f)
				if n == 126 {
					var ext [2]byte
					if _, err := io.ReadFull(br, ext[:]); err != nil {
						return
					}
					n = int(ext[0])<<8 | int(ext[1])
				} else if n == 127 {
					return // not needed for tests
				}
				var mask [4]byte
				if masked {
					if _, err := io.ReadFull(br, mask[:]); err != nil {
						return
					}
				}
				payload := make([]byte, n)
				if _, err := io.ReadFull(br, payload); err != nil {
					return
				}
				if masked {
					for i := range payload {
						payload[i] ^= mask[i%4]
					}
				}
				opcode := b0 & 0x0f
				if opcode == 0x8 { // close
					return
				}
				out := []byte{0x80 | opcode, byte(len(payload))}
				out = append(out, payload...)
				if _, err := c.Write(out); err != nil {
					return
				}
			}
		}(c)
	}
}

func wsEchoRoundTrip(t *testing.T, scheme, addr, hostHeader string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if hostHeader == "" {
		hostHeader = addr
	}
	wsEchoRoundTripConn(t, c, hostHeader)
}

func wsEchoRoundTripConn(t *testing.T, c net.Conn, host string) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		host, key,
	)
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	payload := []byte("ping")
	mask := [4]byte{1, 2, 3, 4}
	frame := []byte{0x81, byte(0x80 | len(payload))}
	frame = append(frame, mask[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, masked...)
	if _, err := c.Write(frame); err != nil {
		t.Fatal(err)
	}

	b0, err := br.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	b1, err := br.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	n := int(b1 & 0x7f)
	got := make([]byte, n)
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatal(err)
	}
	if (b0&0x0f) != 0x1 || string(got) != "ping" {
		t.Fatalf("echo opcode=%d got=%q", b0&0x0f, got)
	}
}

func wsAcceptKey(clientKey string) string {
	// SHA1(key + GUID) base64 — minimal copy of RFC6455 for tests.
	const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	sum := sha1.Sum([]byte(clientKey + guid))
	return base64.StdEncoding.EncodeToString(sum[:])
}
