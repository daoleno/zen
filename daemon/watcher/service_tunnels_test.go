package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func tunnelFixture(t *testing.T) (*Watcher, SessionService, *httptest.Server, *atomic.Bool) {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ws" {
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			kind, data, err := connection.ReadMessage()
			if err == nil {
				_ = connection.WriteMessage(kind, data)
			}
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(writer, "ZEN_TUNNEL_FIXTURE")
	}))
	t.Cleanup(origin.Close)
	port := origin.Listener.Addr().(*net.TCPAddr).Port
	live := &atomic.Bool{}
	live.Store(true)
	service := SessionService{ID: "fixture", PID: os.Getpid(), Port: port, Binds: []string{"127.0.0.1"}, Generation: ""}
	watcher := &Watcher{listSocketsFn: func() ([]listeningSocket, error) {
		if !live.Load() {
			return nil, nil
		}
		return []listeningSocket{{pid: service.PID, port: port, bind: "127.0.0.1", inode: "fixture-1"}}, nil
	}}
	sockets, _ := watcher.listeningSocketsForServices()
	service.Generation = serviceInstanceGeneration(service, sockets)
	return watcher, service, origin, live
}

func startFixtureTunnel(t *testing.T, w *Watcher, service SessionService) (*quickTunnel, *serviceTunnels) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tunnel := &quickTunnel{state: ServiceTunnel{Status: "starting", Generation: service.Generation}, cancel: cancel, done: make(chan struct{})}
	owner := w.tunnelOwner()
	owner.items[service.ID] = tunnel
	go w.runServiceTunnel(ctx, owner, tunnel, service)
	t.Cleanup(func() {
		cancel()
		select {
		case <-tunnel.done:
		case <-time.After(6 * time.Second):
			t.Error("tunnel cleanup timed out")
		}
	})
	return tunnel, owner
}
func waitTunnel(t *testing.T, tunnel *quickTunnel, owner *serviceTunnels) ServiceTunnel {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		owner.mu.Lock()
		state := tunnel.state
		owner.mu.Unlock()
		if state.Status == "running" || state.Status == "failed" || state.Status == "stopped" {
			return state
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("tunnel did not settle")
	return ServiceTunnel{}
}

func TestQuickTunnelOriginLifetimeAndStopDoNotKillOrigin(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "cloudflared")
	if err := os.WriteFile(binary, []byte(`#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = --url ]; then printf '%s' "$2" > "$ZEN_TUNNEL_TEST_PROXY"; fi
  shift
done
printf '%s\n' '{"message":"https://fixture-test.trycloudflare.com"}' '{"message":"Registered tunnel connection"}'
exec sleep 60
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	proxyFile := filepath.Join(directory, "proxy-url")
	t.Setenv("ZEN_TUNNEL_TEST_PROXY", proxyFile)
	w, service, origin, live := tunnelFixture(t)
	tunnel, owner := startFixtureTunnel(t, w, service)
	if state := waitTunnel(t, tunnel, owner); state.Status != "running" {
		t.Fatalf("state=%+v", state)
	}
	proxyBytes, err := os.ReadFile(proxyFile)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL := string(proxyBytes)
	proxyResponse, err := http.Get(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(proxyResponse.Body)
	proxyResponse.Body.Close()
	if string(body) != "ZEN_TUNNEL_FIXTURE" {
		t.Fatalf("proxy HTTP body=%q", body)
	}
	connection, _, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second}).Dial("ws"+strings.TrimPrefix(proxyURL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := connection.WriteMessage(websocket.TextMessage, []byte("LOCAL_WS")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := connection.ReadMessage()
	connection.Close()
	if err != nil || string(echo) != "LOCAL_WS" {
		t.Fatalf("proxy WebSocket=%q %v", echo, err)
	}
	live.Store(false)
	select {
	case <-tunnel.done:
	case <-time.After(4 * time.Second):
		t.Fatal("origin loss did not stop tunnel")
	}
	owner.mu.Lock()
	state := tunnel.state
	owner.mu.Unlock()
	if state.URL != "" || state.Status != "failed" {
		t.Fatalf("stale URL: %+v", state)
	}
	response, err := http.Get(origin.URL)
	if err != nil {
		t.Fatal("stopping tunnel killed origin", err)
	}
	response.Body.Close()
}

func TestQuickTunnelLogOnlyAcceptsStructuredCloudflareURLs(t *testing.T) {
	if quickTunnelLogURL([]byte(`{"message":"Your tunnel: https://hello-world.trycloudflare.com"}`)) != "https://hello-world.trycloudflare.com" {
		t.Fatal("missing native URL")
	}
	for _, line := range []string{`https://bad.trycloudflare.com`, `{"message":"https://example.com"}`, `{"error":"https://bad.trycloudflare.com"}`} {
		if quickTunnelLogURL([]byte(line)) != "" {
			t.Fatal("accepted unrelated output")
		}
	}
	if !privateService(SessionService{Process: "zen serve"}) || !privateService(SessionService{Command: "zen dsh-session"}) {
		t.Fatal("control service publishable")
	}
}

func TestQuickTunnelLiveHTTPAndWebSocket(t *testing.T) {
	if os.Getenv("ZEN_QUICK_TUNNEL_SMOKE") != "1" {
		t.Skip("explicit public-fixture smoke only")
	}
	w, service, origin, _ := tunnelFixture(t)
	tunnel, owner := startFixtureTunnel(t, w, service)
	state := waitTunnel(t, tunnel, owner)
	if state.Status != "running" {
		t.Fatalf("Cloudflare start: %+v", state)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	dialer := (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	if os.Getenv("ZEN_TUNNEL_SMOKE_DOH") == "1" {
		// Test-only DNS isolation: preserve the public hostname for TLS and HTTP.
		publicHost, _ := url.Parse(state.URL)
		request, _ := http.NewRequest(http.MethodGet, "https://1.1.1.1/dns-query?type=A&name="+url.QueryEscape(publicHost.Hostname()), nil)
		request.Header.Set("Accept", "application/dns-json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var answer struct {
			Status int
			Answer []struct {
				Type int
				Data string
			}
		}
		err = json.NewDecoder(response.Body).Decode(&answer)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		address := ""
		for _, record := range answer.Answer {
			if record.Type == 1 && net.ParseIP(record.Data) != nil {
				address = record.Data
				break
			}
		}
		if address == "" {
			t.Fatalf("Cloudflare DoH has no public A record: status=%d", answer.Status)
		}
		dialer = func(ctx context.Context, network, target string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(target)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address, port))
		}
		client.Transport = &http.Transport{DialContext: dialer}
		t.Log("Public hostname resolved through Cloudflare DoH; system DNS proof remains unavailable")
	}
	var body []byte
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		response, err := client.Get(state.URL)
		lastErr = err
		if err == nil {
			body, _ = io.ReadAll(io.LimitReader(response.Body, 1024))
			response.Body.Close()
			if string(body) == "ZEN_TUNNEL_FIXTURE" {
				break
			}
		}
		time.Sleep(time.Second)
	}
	if string(body) != "ZEN_TUNNEL_FIXTURE" {
		t.Fatalf("public HTTP failed: %v body=%q", lastErr, body)
	}
	connection, _, err := (&websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: dialer}).Dial("wss"+strings.TrimPrefix(state.URL, "https")+"/ws", nil)
	if err != nil {
		t.Fatal("public WebSocket", err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := connection.WriteMessage(websocket.TextMessage, []byte("ZEN_WS")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := connection.ReadMessage()
	if err != nil || string(echo) != "ZEN_WS" {
		t.Fatalf("WebSocket echo=%q err=%v", echo, err)
	}
	tunnel.cancel()
	select {
	case <-tunnel.done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop timed out")
	}
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal("origin stopped with tunnel", err)
	}
	response.Body.Close()
	t.Log("Controlled public HTTP and WebSocket passed; tunnel stopped; origin remained alive. Quick Tunnels do not support SSE.")
}

func TestQuickTunnelGenerationChangesOnSamePIDPortReuse(t *testing.T) {
	service := SessionService{PID: os.Getpid(), Port: 12345}
	old := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1", inode: "1"}})
	replacement := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1", inode: "2"}})
	if old == "" || old == replacement {
		t.Fatal("listener replacement retained tunnel ownership")
	}
	if value := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1"}}); value != "" {
		t.Fatal("missing listener identity accepted")
	}
}

func TestQuickTunnelStoppingFinishedOperationRemainsStopped(t *testing.T) {
	w := &Watcher{}
	done := make(chan struct{})
	close(done)
	owner := w.tunnelOwner()
	owner.items["service"] = &quickTunnel{state: ServiceTunnel{Status: "failed", Generation: "old", Error: "ended"}, cancel: func() {}, done: done}
	state, err := w.ServiceTunnelAction("service", "old", "stop")
	if err != nil || state.Status != "stopped" || state.URL != "" {
		t.Fatalf("stop=%+v %v", state, err)
	}
}
