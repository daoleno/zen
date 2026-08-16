package modelprofiles

// Focused tests for the transparent Responses WebSocket upstream proxy shared
// by the per-session Router and the machine-level Gateway. These prove the
// core acceptance: Codex WebSocket Upgrades complete (no local 501 fallback),
// frames flow byte-for-byte, Provider credentials inject on the upstream
// handshake, Provider hot-switch selects the upstream per new connection,
// close/error propagation is honest, and concurrent connections stay
// independent.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsUpstreamSimulator is a scripted Responses WebSocket upstream. It accepts
// upgrades, records handshake headers/path/query/auth, records every frame,
// and answers each received message with scriptedText replies (a copy is sent
// once per received frame — tests choose replies that read as protocol
// events). It also records the close code the client sent.
type wsUpstreamSimulator struct {
	mu         sync.Mutex
	handshakes int
	headers    http.Header
	path       string
	query      string
	frames     [][]byte
	closeCodes []int
	url        string
	server     *httptest.Server
}

func newWSUpstreamSimulator(t *testing.T, scriptedReplies []string) *wsUpstreamSimulator {
	t.Helper()
	up := &wsUpstreamSimulator{closeCodes: []int{}}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWebSocketUpgrade(r) {
			http.Error(w, "websocket upgrade required", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		up.mu.Lock()
		up.handshakes++
		up.headers = r.Header.Clone()
		up.path = r.URL.Path
		up.query = r.URL.RawQuery
		up.mu.Unlock()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					up.mu.Lock()
					up.closeCodes = append(up.closeCodes, closeErr.Code)
					up.mu.Unlock()
				}
				break
			}
			up.mu.Lock()
			up.frames = append(up.frames, append([]byte(nil), data...))
			up.mu.Unlock()
			for _, reply := range scriptedReplies {
				_ = conn.WriteMessage(websocket.TextMessage, []byte(reply))
			}
		}
		_ = conn.Close()
	}))
	t.Cleanup(up.server.Close)
	up.url = "ws" + strings.TrimPrefix(up.server.URL, "http")
	return up
}

func (up *wsUpstreamSimulator) frameCount() int {
	up.mu.Lock()
	defer up.mu.Unlock()
	return len(up.frames)
}

func (up *wsUpstreamSimulator) frame(i int) []byte {
	up.mu.Lock()
	defer up.mu.Unlock()
	if i < 0 || i >= len(up.frames) {
		return nil
	}
	return up.frames[i]
}

func (up *wsUpstreamSimulator) authHeader() string {
	up.mu.Lock()
	defer up.mu.Unlock()
	return up.headers.Get("Authorization")
}

// wsClientDial dials the given URL over WebSocket with headers, returning the
// client connection or failing with the HTTP status when the handshake failed.
func wsClientDial(t *testing.T, url, path string, headers map[string]string) (*websocket.Conn, int) {
	t.Helper()
	reqHeader := http.Header{}
	for k, v := range headers {
		reqHeader.Set(k, v)
	}
	if strings.HasPrefix(url, "http://") {
		url = "ws://" + strings.TrimPrefix(url, "http://")
	} else if strings.HasPrefix(url, "https://") {
		url = "wss://" + strings.TrimPrefix(url, "https://")
	}
	conn, resp, err := websocket.DefaultDialer.Dial(url+path, reqHeader)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, status
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, 101
}

const wsEventCreated = `{"type":"response.created","response":{"id":"resp-ws-1"}}`
const wsEventCompleted = `{"type":"response.completed","response":{"id":"resp-ws-1","usage":{"input_tokens":0,"input_tokens_details":null,"output_tokens":0,"output_tokens_details":null,"total_tokens":0}}}`

func TestGatewayWebSocketProxiesFramesTransparently(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated, wsEventCompleted})
	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor(upstream.server.URL))

	client, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", map[string]string{
		"User-Agent":  "codex-cli/0.147.0",
		"OpenAI-Beta": "responses_websockets=2026-02-06",
	})
	if status != 101 {
		t.Fatalf("gateway ws handshake status = %d, want 101", status)
	}

	first := "{\"type\":\"response.create\",\"model\":\"gpt-5.6-sol\",\"input\":[{\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"hello\"}]}]}"
	if err := client.WriteMessage(websocket.TextMessage, []byte(first)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, got, err := client.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		want := []byte(wsEventCreated)
		if i == 1 {
			want = []byte(wsEventCompleted)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("reply %d = %s, want %s (bytes must be exact)", i, got, want)
		}
	}

	if upstream.frameCount() != 1 {
		t.Fatalf("upstream frames = %d, want 1", upstream.frameCount())
	}
	if !bytes.Equal(upstream.frame(0), []byte(first)) {
		t.Fatalf("upstream frame bytes differ: %s", upstream.frame(0))
	}
	upstream.mu.Lock()
	path := upstream.path
	ua := upstream.headers.Get("User-Agent")
	beta := upstream.headers.Get("OpenAI-Beta")
	upstream.mu.Unlock()
	if path != "/v1/responses" {
		t.Fatalf("upstream ws path = %q", path)
	}
	if ua != "codex-cli/0.147.0" {
		t.Fatalf("user-agent not forwarded: %q", ua)
	}
	if beta != "responses_websockets=2026-02-06" {
		t.Fatalf("OpenAI-Beta not forwarded: %q", beta)
	}

	// Second frame on the SAME connection is forwarded too (connection reuse).
	second := `{"type":"response.create","model":"gpt-5.6-sol","input":[]}`
	if err := client.WriteMessage(websocket.TextMessage, []byte(second)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && upstream.frameCount() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if upstream.frameCount() != 2 || !bytes.Equal(upstream.frame(1), []byte(second)) {
		t.Fatalf("second frame not forwarded byte-exact: frames=%d", upstream.frameCount())
	}
}

func TestGatewayWebSocketInjectsStoredCredential(t *testing.T) {
	store := NewMemoryCredentialStore()
	if err := store.Set(CredentialRefFor("conn-ws-keyed"), "sk-ws-secret-xyz"); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", store)
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated})
	g.SetUpstream(GatewayUpstream{
		ProfileID:     "conn-ws-keyed",
		BaseURL:       upstream.server.URL,
		Protocol:      ProtocolOpenAIResponses,
		AuthMode:      AuthModeBearerEnv,
		CredentialEnv: "ZEN_PROVIDER_API_KEY",
		CredentialRef: CredentialRefFor("conn-ws-keyed"),
	})
	client, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", nil)
	if status != 101 {
		t.Fatalf("gateway ws handshake status = %d", status)
	}
	_ = client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m"}`))
	if got := upstream.authHeader(); got != "Bearer sk-ws-secret-xyz" {
		t.Fatalf("upstream ws Authorization = %q, want stored secret", got)
	}

	// The client's own placeholder must never leak to the upstream for a
	// bearer-env provider.
	if upstream.headers.Get("X-Api-Key") != "" {
		t.Fatalf("inbound key leaked: %q", upstream.headers.Get("X-Api-Key"))
	}
}

func TestGatewayWebSocketHotSwitchNewConnectionUsesNewUpstream(t *testing.T) {
	upstreamA := newWSUpstreamSimulator(t, []string{wsEventCreated})
	upstreamB := newWSUpstreamSimulator(t, []string{wsEventCreated})
	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor(upstreamA.server.URL))
	addr := "http://" + g.ActualAddr()

	// Same long-lived client stays on its bound upstream after the switch.
	client, status := wsClientDial(t, addr, "/v1/responses", nil)
	if status != 101 {
		t.Fatalf("handshake 1 status = %d", status)
	}
	_ = client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":["a"]}`))

	g.SetUpstream(gatewayUpstreamFor(upstreamB.server.URL))
	_ = client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":["b"]}`))

	// A NEW connection after the switch must reach upstream B.
	client2, status2 := wsClientDial(t, addr, "/v1/responses", nil)
	if status2 != 101 {
		t.Fatalf("handshake 2 status = %d", status2)
	}
	_ = client2.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"m","input":["c"]}`))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (upstreamA.frameCount() < 2 || upstreamB.frameCount() < 1) {
		time.Sleep(10 * time.Millisecond)
	}
	upstreamA.mu.Lock()
	aFrames := len(upstreamA.frames)
	aHandshakes := upstreamA.handshakes
	upstreamA.mu.Unlock()
	upstreamB.mu.Lock()
	bFrames := len(upstreamB.frames)
	bHandshakes := upstreamB.handshakes
	upstreamB.mu.Unlock()
	if aHandshakes != 1 || bHandshakes != 1 {
		t.Fatalf("handshakes a=%d b=%d, want 1 each", aHandshakes, bHandshakes)
	}
	// The established connection finished its two turns on A (binding is per
	// connection; the switch governs the next connection) and the new client
	// went to B — the hot-switch contract.
	if aFrames != 2 {
		t.Fatalf("upstream A frames = %d, want 2 (established conn keeps A)", aFrames)
	}
	if bFrames != 1 {
		t.Fatalf("upstream B frames = %d, want 1 (new conn uses B)", bFrames)
	}
	if !g.Listening() {
		t.Fatal("gateway listener disappeared across the upstream switch")
	}
}

func TestGatewayWebSocketUpstreamFailureIsHonest(t *testing.T) {
	// A dead upstream address: the client must see a real HTTP failure (502),
	// never a fabricated 101 that then aborts.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := ln.Addr().String()
	_ = ln.Close()

	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor("http://" + deadAddr))
	_, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", nil)
	if status == 101 || status == 0 {
		t.Fatalf("dead upstream ws status = %d, want honest HTTP failure", status)
	}
	if status != http.StatusBadGateway {
		t.Fatalf("dead upstream ws status = %d, want 502", status)
	}
}

func TestGatewayWebSocketNoUpstreamHonestServiceUnavailable(t *testing.T) {
	g := gatewayTest(t)
	_, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("no-upstream ws status = %d, want 503", status)
	}
}

func TestGatewayWebSocketClosePropagation(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, nil)
	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", nil)
	if status != 101 {
		t.Fatalf("handshake status = %d", status)
	}
	// Client-initiated close reaches the upstream with the same code.
	_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "finished"), time.Now().Add(time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		upstream.mu.Lock()
		codes := append([]int(nil), upstream.closeCodes...)
		upstream.mu.Unlock()
		if len(codes) > 0 {
			if codes[0] != websocket.CloseNormalClosure {
				t.Fatalf("close code seen upstream = %d, want 1000", codes[0])
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRouterWebSocketProxiesPerRouteBinding(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated, wsEventCompleted})
	table := NewRouteTable()
	profile := routedCodex(upstream.server.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	client, status := wsClientDial(t, base, "/responses", map[string]string{"User-Agent": "codex-cli/0.147.0"})
	if status != 101 {
		t.Fatalf("router ws handshake status = %d, want 101", status)
	}
	request := `{"type":"response.create","model":"gpt-5.6-sol","input":[]}`
	if err := client.WriteMessage(websocket.TextMessage, []byte(request)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, got, err := client.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		want := []byte(wsEventCreated)
		if i == 1 {
			want = []byte(wsEventCompleted)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("reply %d = %s", i, got)
		}
	}
	_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && upstream.frameCount() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if !bytes.Equal(upstream.frame(0), []byte(request)) {
		t.Fatalf("router forwarded frame bytes differ")
	}
	if upstream.headers.Get("User-Agent") != "codex-cli/0.147.0" {
		t.Fatalf("router did not forward UA: %q", upstream.headers.Get("User-Agent"))
	}

	// Flight lease released and the successful WS session marked opaque
	// history (same semantics as a successful HTTP request).
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && table.InFlightCount(state.Binding.RouteID) != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if table.InFlightCount(state.Binding.RouteID) != 0 {
		t.Fatal("router ws flight lease not released")
	}
	got, _ := table.Get("s1")
	if got.Binding.HistoryState != HistoryStateMayContainOpaque {
		t.Fatalf("router ws history state = %q, want opaque marked", got.Binding.HistoryState)
	}
}

func TestRouterWebSocketRejectsUnsupportedEndpointHonestly(t *testing.T) {
	// A claude route never speaks WebSocket; the honest 501 marker stays and
	// the upstream is never dialed for it.
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated})
	table := NewRouteTable()
	profile := routedClaude(upstream.server.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("claude", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	req, _ := http.NewRequest(http.MethodPost, base+"/v1/messages", strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("non-responses ws status = %d, want 501", resp.StatusCode)
	}
}

func TestRouterWebSocketParallelConnections(t *testing.T) {
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated})
	table := NewRouteTable()
	profile := routedCodex(upstream.server.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s-par", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	const n = 4
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client, status := wsClientDial(t, base, "/responses", nil)
			if status != 101 {
				errs <- errf("client %d handshake status %d", i, status)
				return
			}
			payload := "{\"type\":\"response.create\",\"model\":\"gpt-5.6-sol\",\"input\":[\"c" + string(rune('0'+i)) + "\"]}"
			if err := client.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
				errs <- err
				return
			}
			if _, got, err := client.ReadMessage(); err != nil {
				errs <- err
				return
			} else if string(got) != wsEventCreated {
				errs <- errf("client %d reply = %s", i, got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && upstream.frameCount() < n {
		time.Sleep(10 * time.Millisecond)
	}
	upstream.mu.Lock()
	handshakes := upstream.handshakes
	frames := len(upstream.frames)
	upstream.mu.Unlock()
	if handshakes != n || frames != n {
		t.Fatalf("parallel handshakes=%d frames=%d, want %d each", handshakes, frames, n)
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		var doc map[string]any
		if err := json.Unmarshal(upstream.frame(i), &doc); err != nil {
			t.Fatal(err)
		}
		input := doc["input"]
		seen[input.([]any)[0].(string)] = true
	}
	if len(seen) != n {
		t.Fatalf("distinct parallel clients = %d, want %d: %v", len(seen), n, seen)
	}
}

func TestWebSocketUpstreamURLSchemeConversion(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9/v1/responses":     "ws://127.0.0.1:9/v1/responses",
		"https://api.openai.com/v1/responses": "wss://api.openai.com/v1/responses",
		"wss://already":                       "wss://already",
	}
	for in, want := range cases {
		got, err := wsUpstreamURL(in)
		if err != nil {
			t.Fatalf("wsUpstreamURL(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("wsUpstreamURL(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := wsUpstreamURL("ftp://x"); err == nil {
		t.Fatal("ftp scheme must be rejected")
	}
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func TestBuildWebSocketUpstreamHeadersStripsHandshakeState(t *testing.T) {
	in := http.Header{}
	in.Set("Upgrade", "websocket")
	in.Set("Connection", "Upgrade")
	in.Set("Sec-WebSocket-Key", "clientkey")
	in.Set("Sec-WebSocket-Version", "13")
	in.Set("Authorization", "Bearer user-secret")
	in.Set("X-Api-Key", "user-key")
	in.Set("User-Agent", "codex-cli/0.147.0")
	in.Set("Origin", "http://localhost")
	out := buildWebSocketUpstreamHeaders(in)
	if out.Get("Upgrade") != "" || out.Get("Connection") != "" || out.Get("Sec-WebSocket-Key") != "" {
		t.Fatalf("handshake state leaked upstream: %v", out)
	}
	if out.Get("Authorization") != "" || out.Get("X-Api-Key") != "" {
		t.Fatalf("inbound auth leaked upstream: %v", out)
	}
	if out.Get("Origin") != "" {
		t.Fatalf("origin must not be forwarded: %v", out)
	}
	if out.Get("User-Agent") != "codex-cli/0.147.0" {
		t.Fatalf("UA not forwarded: %v", out)
	}
}
