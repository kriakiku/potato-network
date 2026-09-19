// Minimal WebSocket echo for e2e: server on :8765, or -client ws://… one-shot.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/websocket"
)

func main() {
	clientURL := flag.String("client", "", "if set, connect and echo-check then exit")
	addr := flag.String("addr", ":8765", "listen address (server mode)")
	flag.Parse()

	if *clientURL != "" {
		os.Exit(runClient(*clientURL))
	}

	http.Handle("/", websocket.Handler(func(ws *websocket.Conn) {
		_, _ = io.Copy(ws, ws)
	}))
	log.Printf("wsecho listening %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func runClient(url string) int {
	ws, err := websocket.Dial(url, "", "http://127.0.0.1/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		return 1
	}
	defer ws.Close()
	_ = ws.SetDeadline(time.Now().Add(2 * time.Second))

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
