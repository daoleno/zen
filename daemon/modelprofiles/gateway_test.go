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
	"time"
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

// TestSetProviderDefaultRetargetsGatewayUpstream is the Settings Provider
// selection regression: changing the default Codex connection must retarget
// the machine-level gateway atomically (the same hook the isolated
// direct-terminal live proof exercises end-to-end).
func TestSetProviderDefaultRetargetsGatewayUpstream(t *testing.T) {
	root := t.TempDir()
	codexConfig := filepath.Join(root, "codex", "config.toml")
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:    filepath.Join(root, "profiles.toml"),
		RoutesPath:      filepath.Join(root, "routes.json"),
		ListenerPath:    filepath.Join(root, "listener.json"),
		GatewayAddr:     "127.0.0.1:0",
		GatewayStateDir: filepath.Join(root, "gateway"),
		CodexConfigPath: codexConfig,
		Lookup:          readyLookup("secret"),
		Verifier:        BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	a := codexResponsesProfile("conn-a", "gpt-5", "gpt-5")
	b := codexResponsesProfile("conn-b", "gpt-5", "gpt-5")
	projA, err := owner.UpsertProfile(a, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	projB, err := owner.UpsertProfile(b, projA.Catalog.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault("codex", "conn-a", "gpt-5", projB.Catalog.Revision); err != nil {
		t.Fatal(err)
	}
	pp, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	setDefaultRevision := pp.Revision
	if _, err := owner.EnableCodexGateway(""); err != nil {
		t.Fatal(err)
	}
	if up, ok := owner.Gateway().Upstream(); !ok || up.ProfileID != "conn-a" {
		t.Fatalf("gateway upstream after enable = %+v ok=%v, want conn-a", up, ok)
	}

	// Settings Provider selection -> default moves to conn-b -> the gateway
	// follows without touching the listener or any running process.
	if _, err := owner.SetProviderDefault("codex", "conn-b", "gpt-5", setDefaultRevision); err != nil {
		t.Fatal(err)
	}
	if up, ok := owner.Gateway().Upstream(); !ok || up.ProfileID != "conn-b" {
		t.Fatalf("gateway upstream after default switch = %+v ok=%v, want conn-b", up, ok)
	}
	if !owner.Gateway().Listening() {
		t.Fatal("gateway stopped listening across the default switch")
	}
}

// TestGatewayUpstreamCompilesAccountConnectionAuth is the live activation
// regression: durable account connections store auth_mode=none (raw form;
// per-client auth semantics compile at use time) with a credential env. The
// machine-level gateway must resolve the default Codex connection through the
// same per-client compile the launch/router path uses, or real requests reach
// the upstream with no credential and fail 401 (observed during guarded live
// activation against a real provider).
func TestGatewayUpstreamCompilesAccountConnectionAuth(t *testing.T) {
	root := t.TempDir()
	codexConfig := filepath.Join(root, "codex", "config.toml")
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:    filepath.Join(root, "profiles.toml"),
		RoutesPath:      filepath.Join(root, "routes.json"),
		ListenerPath:    filepath.Join(root, "listener.json"),
		GatewayAddr:     "127.0.0.1:0",
		GatewayStateDir: filepath.Join(root, "gateway"),
		CodexConfigPath: codexConfig,
		Credentials:     NewMemoryCredentialStore(),
		Lookup:          readyLookup("secret"),
		Verifier:        BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	// Scripted upstream that rejects unauthenticated requests with the exact
	// live failure shape; a correctly authorized broker returns a completion.
	authSeen := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "API key is required in Authorization header", http.StatusUnauthorized)
			return
		}
		authSeen <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp-gw"}`)
	}))
	t.Cleanup(upstream.Close)

	// Durable account connection in the real store shape: custom provider,
	// credential env set, auth_mode stored raw as none (compiled per client).
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-gw-a", Name: "gw A", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL + "/v1", ModelID: "gpt-5",
	}, "secret", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := owner.GetProfile("conn-gw-a")
	if err != nil {
		t.Fatal(err)
	}
	if !isAccountConnection(raw) || normalizeID(raw.AuthMode) != AuthModeNone || normalizeSpace(raw.CredentialEnv) != "ZEN_PROVIDER_API_KEY" {
		t.Fatalf("fixture is not a durable account connection: auth=%q env=%q", raw.AuthMode, raw.CredentialEnv)
	}
	if _, err := owner.SetProviderDefault("codex", "conn-gw-a", "gpt-5", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.EnableCodexGateway(""); err != nil {
		t.Fatal(err)
	}
	up, ok := owner.Gateway().Upstream()
	if !ok {
		t.Fatal("gateway has no upstream after enable")
	}
	if normalizeID(up.AuthMode) != AuthModeBearerEnv {
		t.Fatalf("gateway upstream auth_mode = %q, want bearer_env (per-client compile)", up.AuthMode)
	}
	if normalizeSpace(up.CredentialEnv) != "ZEN_PROVIDER_API_KEY" {
		t.Fatalf("gateway upstream credential_env = %q", up.CredentialEnv)
	}
	if normalizeSpace(up.CredentialRef) == "" {
		t.Fatal("gateway upstream credential_ref is empty")
	}

	// A real gateway request must reach the upstream WITH the stored bearer
	// credential injected (without the fix the raw account connection yields
	// auth_mode=none and the upstream 401s — the live activation failure).
	req, err := http.NewRequest(http.MethodPost, "http://"+owner.Gateway().ActualAddr()+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway request status = %d body=%s", resp.StatusCode, rawBody)
	}
	select {
	case h := <-authSeen:
		if h != "Bearer secret" {
			t.Fatalf("upstream saw Authorization %q, want %q", h, "Bearer secret")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upstream never saw the authorized gateway request")
	}
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
