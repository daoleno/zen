package modelprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSProxyReaderTerminalResultDoesNotBecomeClosedChannelZeroValues(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
		close(closed)
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	read := startWSProxyReader(conn, stop)
	<-closed
	item := <-read
	if item.err == nil {
		t.Fatal("reader terminal result has nil error")
	}
	select {
	case item, ok := <-read:
		t.Fatalf("reader channel remained selectable after terminal result: ok=%t item=%+v", ok, item)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWSProxyTurnCallbackExactlyOnce(t *testing.T) {
	for _, mode := range []wsCompletionCloseMode{wsCompletionKeepOpen, wsCompletionEarlyClose} {
		t.Run(string(mode), func(t *testing.T) {
			upstream := newWSCompletionUpstream(t, mode, 1)
			var calls atomic.Int32
			var successful atomic.Bool
			resolve := func(turn bool) (wsProxyTarget, error) {
				targetURL, err := wsUpstreamURL(upstream.server.URL + "/v1/responses")
				if err != nil {
					return wsProxyTarget{}, err
				}
				target := wsProxyTarget{key: "one", url: targetURL}
				if turn {
					target.done = func(ok bool) {
						calls.Add(1)
						successful.Store(ok)
					}
				}
				return target, nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyWebSocketToUpstream(context.Background(), w, r, resolve, nil)
			}))
			defer server.Close()
			client, status := wsClientDial(t, server.URL, "/v1/responses", nil)
			if status != http.StatusSwitchingProtocols {
				t.Fatalf("handshake status = %d", status)
			}
			if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)); err != nil {
				t.Fatal(err)
			}
			for {
				_, payload, err := client.ReadMessage()
				if err != nil || bytes.Contains(payload, []byte(`"type":"response.completed"`)) {
					break
				}
			}
			_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && calls.Load() == 0 {
				time.Sleep(10 * time.Millisecond)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("callback calls = %d, want exactly 1", got)
			}
			wantSuccess := mode == wsCompletionKeepOpen
			if got := successful.Load(); got != wantSuccess {
				t.Fatalf("callback success = %t, want %t", got, wantSuccess)
			}
			time.Sleep(50 * time.Millisecond)
			if got := calls.Load(); got != 1 {
				t.Fatalf("callback called again after teardown: %d", got)
			}
		})
	}
}

type wsCompletionCloseMode string

const (
	wsCompletionKeepOpen    wsCompletionCloseMode = "keep_open"
	wsCompletionNormalClose wsCompletionCloseMode = "normal_close"
	wsCompletionTCPEOF      wsCompletionCloseMode = "tcp_eof"
	wsCompletionEarlyClose  wsCompletionCloseMode = "early_close"
)

type wsCompletionUpstream struct {
	server *httptest.Server
	mode   wsCompletionCloseMode
	turns  int
	mu     sync.Mutex
	seen   int
}

func newWSCompletionUpstream(t *testing.T, mode wsCompletionCloseMode, turns int) *wsCompletionUpstream {
	t.Helper()
	up := &wsCompletionUpstream{mode: mode, turns: turns}
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(*http.Request) bool { return true },
		EnableCompression: true,
		WriteBufferSize:   32,
	}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.EnableWriteCompression(true)
		for turn := 0; turn < turns; turn++ {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			up.mu.Lock()
			up.seen++
			up.mu.Unlock()
			responseID := fmt.Sprintf("resp-%d", turn+1)
			created, _ := json.Marshal(map[string]any{
				"type":     "response.created",
				"response": map[string]any{"id": responseID},
			})
			if err := conn.WriteMessage(websocket.TextMessage, created); err != nil {
				return
			}
			if mode == wsCompletionEarlyClose {
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "early"), time.Now().Add(time.Second))
				return
			}
			completed, _ := json.Marshal(map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":     responseID,
					"status": "completed",
					"usage": map[string]any{
						"input_tokens": 0, "input_tokens_details": nil,
						"output_tokens": 0, "output_tokens_details": nil,
						"total_tokens": 0,
					},
				},
			})
			writer, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			mid := len(completed) / 2
			_, _ = writer.Write(completed[:mid])
			_, _ = writer.Write(completed[mid:])
			if err := writer.Close(); err != nil {
				return
			}
			if turn+1 == turns {
				switch mode {
				case wsCompletionNormalClose:
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
					return
				case wsCompletionTCPEOF:
					_ = conn.UnderlyingConn().Close()
					return
				}
			}
		}
	}))
	t.Cleanup(up.server.Close)
	return up
}

func (up *wsCompletionUpstream) seenTurns() int {
	up.mu.Lock()
	defer up.mu.Unlock()
	return up.seen
}

func TestGatewayWebSocketForwardsCompletedBeforeImmediateTermination(t *testing.T) {
	for _, mode := range []wsCompletionCloseMode{wsCompletionNormalClose, wsCompletionTCPEOF} {
		t.Run(string(mode), func(t *testing.T) {
			upstream := newWSCompletionUpstream(t, mode, 1)
			gateway := gatewayTest(t)
			gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
			client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
			if status != http.StatusSwitchingProtocols {
				t.Fatalf("handshake status = %d", status)
			}
			if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
				t.Fatal(err)
			}
			var gotCompleted bool
			for !gotCompleted {
				_, payload, err := client.ReadMessage()
				if err != nil {
					break
				}
				if bytes.Contains(payload, []byte(`"type":"response.completed"`)) {
					gotCompleted = true
				}
			}
			if !gotCompleted {
				t.Fatal("response.completed was not forwarded before termination")
			}
		})
	}
}

func TestGatewayWebSocketReconnectsLatestProviderAfterCompletedUpstreamTermination(t *testing.T) {
	for _, mode := range []wsCompletionCloseMode{wsCompletionNormalClose, wsCompletionTCPEOF} {
		t.Run(string(mode), func(t *testing.T) {
			upstreamA := newWSCompletionUpstream(t, mode, 1)
			upstreamB := newWSCompletionUpstream(t, wsCompletionKeepOpen, 1)
			gateway := gatewayTest(t)
			gateway.SetUpstream(gatewayUpstreamFor(upstreamA.server.URL))
			client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
			if status != http.StatusSwitchingProtocols {
				t.Fatalf("handshake status = %d", status)
			}

			writeAndReadCompleted := func(label string) {
				t.Helper()
				payload := []byte(`{"type":"response.create","model":"m","input":["` + label + `"]}`)
				if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
					t.Fatal(err)
				}
				for {
					_, reply, err := client.ReadMessage()
					if err != nil {
						t.Fatalf("%s: downstream closed before response.completed: %v", label, err)
					}
					if bytes.Contains(reply, []byte(`"type":"response.completed"`)) {
						return
					}
				}
			}

			writeAndReadCompleted("a")
			gateway.SetUpstream(gatewayUpstreamFor(upstreamB.server.URL))
			writeAndReadCompleted("b")
			if upstreamA.seenTurns() != 1 || upstreamB.seenTurns() != 1 {
				t.Fatalf("turns A=%d B=%d, want 1 each", upstreamA.seenTurns(), upstreamB.seenTurns())
			}
		})
	}
}

func TestGatewayWebSocketImmediateCloseCompletionStress(t *testing.T) {
	const iterations = 100
	for iteration := 0; iteration < iterations; iteration++ {
		upstream := newWSCompletionUpstream(t, wsCompletionNormalClose, 1)
		gateway := gatewayTest(t)
		gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
		client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
		if status != http.StatusSwitchingProtocols {
			t.Fatalf("iteration %d: handshake status = %d", iteration, status)
		}
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		var gotCompleted bool
		for !gotCompleted {
			_, payload, err := client.ReadMessage()
			if err != nil {
				break
			}
			if bytes.Contains(payload, []byte(`"type":"response.completed"`)) {
				gotCompleted = true
			}
		}
		if !gotCompleted {
			t.Fatalf("iteration %d: response.completed was lost", iteration)
		}
		_ = gateway.Close()
	}
}

func TestGatewayWebSocketSupportsSequentialTurnsBeforeClose(t *testing.T) {
	upstream := newWSCompletionUpstream(t, wsCompletionNormalClose, 3)
	gateway := gatewayTest(t)
	gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d", status)
	}
	for turn := 0; turn < 3; turn++ {
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
			t.Fatal(err)
		}
		for {
			_, payload, err := client.ReadMessage()
			if err != nil {
				t.Fatalf("turn %d: %v", turn+1, err)
			}
			if bytes.Contains(payload, []byte(`"type":"response.completed"`)) {
				break
			}
		}
	}
	if upstream.seenTurns() != 3 {
		t.Fatalf("upstream turns = %d, want 3", upstream.seenTurns())
	}
}

func TestGatewayWebSocketPropagatesUpstreamCloseBeforeCompleted(t *testing.T) {
	upstream := newWSCompletionUpstream(t, wsCompletionEarlyClose, 1)
	gateway := gatewayTest(t)
	gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d", status)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
		t.Fatal(err)
	}
	var gotCompleted bool
	for {
		_, payload, err := client.ReadMessage()
		if err != nil {
			break
		}
		gotCompleted = gotCompleted || bytes.Contains(payload, []byte(`"type":"response.completed"`))
	}
	if gotCompleted {
		t.Fatal("premature upstream close fabricated response.completed")
	}
}

func TestGatewayWebSocketClientCancelClosesActiveUpstream(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, nil)
	gateway := gatewayTest(t)
	gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d", status)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && upstream.frameCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cancel"), time.Now().Add(time.Second))
	for time.Now().Before(deadline) {
		upstream.mu.Lock()
		closed := len(upstream.closeCodes) > 0
		upstream.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("active upstream did not observe client cancellation")
}

func TestGatewayWebSocketShutdownClosesDownstream(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, nil)
	gateway := gatewayTest(t)
	gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d", status)
	}
	if err := gateway.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := client.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseGoingAway {
		t.Fatalf("shutdown close = %v, want 1001", err)
	}
}

func TestWSConnRegistryRejectsConnectionThatFinishesUpgradeAfterShutdown(t *testing.T) {
	registry := newWSConnRegistry()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upgraded := make(chan *websocket.Conn, 1)
	admit := make(chan struct{})
	admitted := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			admitted <- false
			return
		}
		upgraded <- conn
		<-admit
		admitted <- registry.add(conn)
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	<-upgraded
	registry.closeAll(websocket.CloseGoingAway, "shutdown")
	close(admit)
	if <-admitted {
		t.Fatal("connection admitted after registry shutdown")
	}

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = client.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseGoingAway {
		t.Fatalf("late admission close = %v, want 1001", err)
	}
}

func TestGatewayWebSocketListenAfterCloseUsesFreshRegistry(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, nil)
	gateway := gatewayTest(t)
	gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	if err := gateway.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Listen(); err != nil {
		t.Fatal(err)
	}

	client, status := wsClientDial(t, "http://"+gateway.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d", status)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":[]}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if upstream.frameCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("restarted gateway did not admit a WebSocket turn")
}
