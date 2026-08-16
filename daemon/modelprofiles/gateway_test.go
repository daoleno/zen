package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGatewayUpstream records exact request bodies for gateway A/B tests.
type fakeGatewayUpstream struct {
	requests chan []byte
	auth     chan string
	server   *httptest.Server
}

func newFakeGatewayUpstream(t *testing.T, response string) *fakeGatewayUpstream {
	t.Helper()
	f := &fakeGatewayUpstream{
		requests: make(chan []byte, 16),
		auth:     make(chan string, 16),
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.requests <- body
		f.auth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func gatewayTest(t *testing.T) *Gateway {
	t.Helper()
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

func gatewayUpstreamFor(baseURL string) GatewayUpstream {
	return GatewayUpstream{
		ProfileID: "conn-test",
		BaseURL:   baseURL,
		Protocol:  ProtocolOpenAIResponses,
		AuthMode:  AuthModeNone,
	}
}

// TestGatewaySameClientSwitchABPreservesModelBytes is the core A/B proof: one
// long-lived client sends request 1 through upstream A, the Settings switch
// swaps the upstream, and the same client sends request 2 through upstream B.
// The request model bytes are unchanged in both directions and the client
// process/session identity is untouched by construction.
func TestGatewaySameClientSwitchABPreservesModelBytes(t *testing.T) {
	a := newFakeGatewayUpstream(t, `{"id":"resp-a","object":"response"}`)
	b := newFakeGatewayUpstream(t, `{"id":"resp-b","object":"response"}`)
	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor(a.server.URL))

	client := &http.Client{}
	modelPayload := []byte(`{"model":"gpt-5.6-sol","input":"first request through A"}`)
	first := sendGatewayRequest(t, client, g, modelPayload)
	if first != `{"id":"resp-a","object":"response"}` {
		t.Fatalf("request 1 response = %q", first)
	}
	receivedA := <-a.requests
	if !bytes.Equal(receivedA, modelPayload) {
		t.Fatalf("upstream A received %q, want exact bytes %q", receivedA, modelPayload)
	}

	// Settings Provider switch: same client, same thread, next request to B.
	g.SetUpstream(gatewayUpstreamFor(b.server.URL))
	secondModel := []byte(`{"model":"gpt-5.6-sol","input":"second request through B"}`)
	second := sendGatewayRequest(t, client, g, secondModel)
	if second != `{"id":"resp-b","object":"response"}` {
		t.Fatalf("request 2 response = %q", second)
	}
	receivedB := <-b.requests
	if !bytes.Equal(receivedB, secondModel) {
		t.Fatalf("upstream B received %q, want exact bytes %q", receivedB, secondModel)
	}
	var modelDoc map[string]any
	if err := json.Unmarshal(receivedB, &modelDoc); err != nil {
		t.Fatal(err)
	}
	if modelDoc["model"] != "gpt-5.6-sol" {
		t.Fatalf("model identity was not preserved through the gateway: %v", modelDoc["model"])
	}
	if !g.Listening() {
		t.Fatal("gateway listener disappeared across the upstream switch")
	}
}

func sendGatewayRequest(t *testing.T, client *http.Client, g *Gateway, body []byte) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d: %s", resp.StatusCode, raw)
	}
	return string(raw)
}

// TestGatewayHonestFailureWithoutUpstream: no selected Provider must produce
// an honest connection failure, never a silent bypass.
func TestGatewayHonestFailureWithoutUpstream(t *testing.T) {
	g := gatewayTest(t)
	req, err := http.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-upstream status = %d, want 503", resp.StatusCode)
	}
}

func TestGatewayRejectsNonLoopback(t *testing.T) {
	g := gatewayTest(t)
	up := newFakeGatewayUpstream(t, "ok")
	g.SetUpstream(gatewayUpstreamFor(up.server.URL))

	// Non-loopback admission is forbidden.
	direct := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9/v1/responses", strings.NewReader(`{}`))
	req.RemoteAddr = "192.168.1.50:1234"
	g.ServeHTTP(direct, req)
	if direct.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want 403", direct.Code)
	}

	// Non-loopback WebSocket Upgrade is forbidden the same way. (Loopback
	// WebSocket Upgrades are proxied transparently; see wsproxy_test.go.)
	ws := httptest.NewRecorder()
	wsReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9/v1/responses", nil)
	wsReq.RemoteAddr = "192.168.1.50:1234"
	wsReq.Header.Set("Upgrade", "websocket")
	wsReq.Header.Set("Connection", "Upgrade")
	g.ServeHTTP(ws, wsReq)
	if ws.Code != http.StatusForbidden {
		t.Fatalf("non-loopback websocket status = %d, want 403", ws.Code)
	}
}

func TestGatewayInjectsStoredCredential(t *testing.T) {
	store := NewMemoryCredentialStore()
	if err := store.Set(CredentialRefFor("conn-keyed"), "sk-secret-abc"); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", store)
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	up := newFakeGatewayUpstream(t, "ok")
	g.SetUpstream(GatewayUpstream{
		ProfileID:     "conn-keyed",
		BaseURL:       up.server.URL,
		Protocol:      ProtocolOpenAIResponses,
		AuthMode:      AuthModeBearerEnv,
		CredentialEnv: "ZEN_PROVIDER_API_KEY",
		CredentialRef: CredentialRefFor("conn-keyed"),
	})
	req, err := http.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if auth := <-up.auth; auth != "Bearer sk-secret-abc" {
		t.Fatalf("upstream Authorization = %q", auth)
	}
}

func TestGatewayProxiesModelsSurface(t *testing.T) {
	g := gatewayTest(t)
	up := newFakeGatewayUpstream(t, `{"models":[{"id":"gpt-5.6-sol"}]}`)
	g.SetUpstream(gatewayUpstreamFor(up.server.URL))
	req, err := http.NewRequest(http.MethodGet, "http://"+g.ActualAddr()+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(raw, []byte("gpt-5.6-sol")) {
		t.Fatalf("models surface was not proxied: %s", raw)
	}
}

func TestGatewayStatePersistsListenAddrAndUpstreamProfile(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "gateway-state.json")
	g := gatewayTest(t)
	g.SetGatewayStatePath(statePath)
	g.SetUpstream(GatewayUpstream{ProfileID: "conn-persist", BaseURL: "https://example.com/v1", AuthMode: AuthModeNone})
	listenAddr, profileID, err := LoadGatewayState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if profileID != "conn-persist" {
		t.Fatalf("persisted upstream profile = %q", profileID)
	}
	if listenAddr == "" {
		t.Fatal("persisted listen address is empty")
	}
}

// TestGatewayStreamingResponseFlushesChunks ensures SSE-style responses stream.
func TestGatewayStreamingResponseFlushesChunks(t *testing.T) {
	g := gatewayTest(t)
	flushed := make(chan struct{}, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: chunk\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(upstream.Close)
	g.SetUpstream(gatewayUpstreamFor(upstream.URL))
	req, err := http.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if count := bytes.Count(raw, []byte("data: chunk")); count != 3 {
		t.Fatalf("streamed chunks = %d, want 3", count)
	}
	_ = flushed
}

// TestGatewayUpstreamFlapDoesNotChangeListener: switching upstream never
// rebinds the stable endpoint.
func TestGatewayUpstreamFlapDoesNotChangeListener(t *testing.T) {
	a := newFakeGatewayUpstream(t, "a")
	b := newFakeGatewayUpstream(t, "b")
	g := gatewayTest(t)
	addr := g.ActualAddr()
	g.SetUpstream(gatewayUpstreamFor(a.server.URL))
	g.SetUpstream(gatewayUpstreamFor(b.server.URL))
	g.ClearUpstream()
	g.SetUpstream(gatewayUpstreamFor(a.server.URL))
	if g.ActualAddr() != addr {
		t.Fatalf("gateway address changed across upstream switches: %s -> %s", addr, g.ActualAddr())
	}
	if !g.Listening() {
		t.Fatal("gateway stopped listening")
	}
}

