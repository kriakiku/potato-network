// Dual-purpose e2e origin on :80 (MITM-redirected):
//   GET /              → potato-e2e-origin (path-delay / shape-exclude tests)
//   GET /sleep/{ms}    → sleep then respond (slowHTTP e2e)
//   WebSocket /        → echo (MITM tunnel tests)
//   -client ws://…     → one-shot client for e2e harness
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

func main() {
	clientURL := flag.String("client", "", "if set, connect and echo-check then exit")
	addr := flag.String("addr", ":80", "listen address (server mode)")
	flag.Parse()

	if *clientURL != "" {
		os.Exit(runClient(*clientURL))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sleep/", handleSleep)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if isWSUpgrade(r) {
			websocket.Handler(func(ws *websocket.Conn) {
				_, _ = io.Copy(ws, ws)
			}).ServeHTTP(w, r)
			return
		}
		_, _ = io.WriteString(w, "potato-e2e-origin")
	})
	log.Printf("e2e origin listening %s (http+ws)", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// GET /sleep/{ms} sleeps then returns a short body (for slowHTTP top-N e2e).
func handleSleep(w http.ResponseWriter, r *http.Request) {
	if isWSUpgrade(r) {
		http.Error(w, "websocket not supported on /sleep", http.StatusBadRequest)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/sleep/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.Error(w, "usage: /sleep/{ms}", http.StatusBadRequest)
		return
	}
	ms, err := strconv.Atoi(rest)
	if err != nil || ms < 0 {
		http.Error(w, "invalid ms", http.StatusBadRequest)
		return
	}
	if ms > 10_000 {
		ms = 10_000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	_, _ = fmt.Fprintf(w, "slept-%d", ms)
}

func isWSUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func runClient(url string) int {
	ws, err := websocket.Dial(url, "", "http://127.0.0.1/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		return 1
	}
	defer ws.Close()
	_ = ws.SetDeadline(time.Now().Add(5 * time.Second))

	const payload = "ping"
	if err := websocket.Message.Send(ws, payload); err != nil {
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		return 1
	}
	var got string
	if err := websocket.Message.Receive(ws, &got); err != nil {
		fmt.Fprintf(os.Stderr, "recv: %v\n", err)
		return 1
	}
	if got != payload {
		fmt.Fprintf(os.Stderr, "echo mismatch: got %q want %q\n", got, payload)
		return 1
	}
	fmt.Println("ok")
	return 0
}
