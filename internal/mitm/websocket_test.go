package mitm

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

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
		tunnel(cProxy, cbr, uProxy, ubr)
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

func TestWebSocketUpgradeTunnel(t *testing.T) {
	originLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer originLn.Close()
	mux := http.NewServeMux()
	mux.Handle("/", websocket.Handler(func(ws *websocket.Conn) {
		_, _ = io.Copy(ws, ws)
	}))
	go http.Serve(originLn, mux)

	origHost, origPortStr, _ := net.SplitHostPort(originLn.Addr().String())
	origPort, _ := strconv.Atoi(origPortStr)

	st := pnruntime.New(t.TempDir(), nil, nil)
	p := New(0, nil, nil, st, false)

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

	wsURL := "ws://" + proxyLn.Addr().String() + "/"
	ws, err := websocket.Dial(wsURL, "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()
	_ = ws.SetDeadline(time.Now().Add(3 * time.Second))

	if err := websocket.Message.Send(ws, "ping"); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := websocket.Message.Receive(ws, &got); err != nil {
		t.Fatal(err)
	}
	if got != "ping" {
		t.Fatalf("echo=%q", got)
	}
}
