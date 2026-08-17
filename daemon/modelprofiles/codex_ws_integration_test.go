package modelprofiles

// Opt-in live proof: a real installed `codex` CLI streams over the transparent
// Responses WebSocket proxy (machine-level Gateway and per-session Router) and
// NEVER emits the "Falling back from WebSockets to HTTPS transport" warning.
//
// Run (from daemon module root):
//
//	ZEN_CODEX_WS_INTEGRATION=1 go test ./modelprofiles -run TestCodexWSUpstreamProxyLive -count=1 -timeout 120s
//
// Uses a temporary CODEX_HOME and a scripted 127.0.0.1 Responses WebSocket
// upstream that speaks the protocol events (response.created, output_item,
// output_text.delta/.done, response.completed). No real credentials, no user
// config, no network beyond loopback. The upstream records whether any
// non-WebSocket POST /v1/responses request arrived (it must not for the WS
// session) and whether the WS handshake carried the injected provider auth.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsLiveUpstream is the scripted Responses WebSocket upstream for the live
// proof: it reads the first frame, answers with a complete turn (created,
// output_item.added, content_part.added, output_text.delta/done,
// output_item.done, completed), then keeps reading until the CLI closes.
type wsLiveUpstream struct {
	mu         sync.Mutex
	handshakes int
	postHits   int // non-WebSocket /v1/responses requests (fallback must not happen)
	path       string
	frames     int
	server     *httptest.Server
	url        string
}

func newWSLiveUpstream(t *testing.T) *wsLiveUpstream {
	t.Helper()
	up := &wsLiveUpstream{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/responses") && !isWebSocketUpgrade(r) {
			up.mu.Lock()
			up.postHits++
			up.mu.Unlock()
			http.Error(w, "post not expected", http.StatusTeapot)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		up.mu.Lock()
		up.handshakes++
		up.path = r.URL.Path
		up.mu.Unlock()
		var requestID string
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			up.mu.Lock()
			up.frames++
			up.mu.Unlock()
			var doc map[string]any
			_ = json.Unmarshal(data, &doc)
			if doc["type"] == "response.create" {
				if v, ok := doc["model"].(string); ok {
					requestID = v
				}
			}
			respID := "resp-live-ws-1"
			events := []map[string]any{
				{"type": "response.created", "response": map[string]any{"id": respID}},
				{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{
					"id": "msg-1", "type": "message", "role": "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
				}},
				{"type": "response.content_part.added", "item_id": "msg-1", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": ""}},
				{"type": "response.output_text.delta", "item_id": "msg-1", "output_index": 0, "content_index": 0, "delta": "ok"},
				{"type": "response.output_text.done", "item_id": "msg-1", "output_index": 0, "content_index": 0, "text": "ok"},
				{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{
					"id": "msg-1", "type": "message", "role": "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": "ok", "annotations": []any{}}},
				}},
				{"type": "response.completed", "response": map[string]any{
					"id":     respID,
					"status": "completed",
					"usage":  map[string]any{"input_tokens": 1, "input_tokens_details": nil, "output_tokens": 1, "output_tokens_details": nil, "total_tokens": 2},
				}},
			}
			for _, ev := range events {
				raw, _ := json.Marshal(ev)
				if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
					return
				}
			}
			_ = requestID
		}
		_ = conn.Close()
	}))
	t.Cleanup(up.server.Close)
	up.url = "ws" + strings.TrimPrefix(up.server.URL, "http")
	return up
}

func TestCodexWSUpstreamProxyLive(t *testing.T) {
	if os.Getenv("ZEN_CODEX_WS_INTEGRATION") == "" {
		t.Skip("set ZEN_CODEX_WS_INTEGRATION=1 to run the Codex WebSocket live proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	version := strings.TrimSpace(runCodex(t, codexPath, []string{"--version"}, nil, ""))
	t.Logf("codex --version => %s", version)
	if !strings.Contains(version, "codex-cli") {
		t.Fatalf("unexpected codex version output: %q", version)
	}

	t.Run("gateway", func(t *testing.T) {
		upstream := newWSLiveUpstream(t)
		g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
		if err := g.Listen(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = g.Close() })
		g.SetUpstream(gatewayUpstreamFor(upstream.server.URL))

		home := t.TempDir()
		codexHome := filepath.Join(home, "codex-home")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		config := "model_provider = \"zen-gateway\"\nmodel = \"gpt-4o\"\n" +
			"[model_providers.zen-gateway]\n" +
			"name = \"zen-gateway\"\n" +
			"base_url = \"http://" + g.ActualAddr() + "/v1\"\n" +
			"wire_api = \"responses\"\n" +
			"requires_openai_auth = false\n" +
			"supports_websockets = true\n"
		if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}

		out := runCodex(t, codexPath,
			[]string{"exec", "--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"-c", `model_provider="zen-gateway"`,
				"-c", `model="gpt-4o"`,
				"reply with the single word ok"},
			append(scrubEnv(os.Environ(), "CODEX_HOME"), "CODEX_HOME="+codexHome, "HOME="+home),
			home,
		)
		if strings.Contains(out, "Falling back from WebSockets") {
			t.Fatalf("codex warned about WebSocket fallback:\n%s", trimTail(out, 1600))
		}
		if !strings.Contains(out, "ok") {
			t.Fatalf("codex did not print the streamed reply, tail=%q", trimTail(out, 600))
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			upstream.mu.Lock()
			h, p, f := upstream.handshakes, upstream.path, upstream.frames
			upstream.mu.Unlock()
			if h >= 1 && p == "/v1/responses" && f >= 1 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		upstream.mu.Lock()
		h, p, f, posts := upstream.handshakes, upstream.path, upstream.frames, upstream.postHits
		upstream.mu.Unlock()
		if h < 1 {
			t.Fatalf("gateway live: no WebSocket handshake reached the upstream")
		}
		if p != "/v1/responses" {
			t.Fatalf("gateway live: upstream path = %q", p)
		}
		if f < 1 {
			t.Fatalf("gateway live: no request frame reached the upstream")
		}
		if posts != 0 {
			t.Fatalf("gateway live: codex fell back to HTTPS POST (%d hits)", posts)
		}
	})

	t.Run("router", func(t *testing.T) {
		upstream := newWSLiveUpstream(t)
		table := NewRouteTable()
		profile := routedCodex(upstream.server.URL, "gpt-4o", "gpt-4o")
		state, err := table.BindLaunch("live-ws-router", profile, 1, verifiedAuth(profile))
		if err != nil {
			t.Fatal(err)
		}
		router := NewRouter(table)
		srv := httptest.NewServer(router.Handler())
		defer srv.Close()
		base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

		home := t.TempDir()
		codexHome := filepath.Join(home, "codex-home")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		out := runCodex(t, codexPath,
			[]string{"exec", "--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"-c", `model_provider="openai"`,
				"-c", "openai_base_url=" + tomlString(base),
				"-c", `model="gpt-4o"`,
				"reply with the single word ok"},
			append(scrubEnv(os.Environ(), "CODEX_HOME", "OPENAI_API_KEY"),
				"CODEX_HOME="+codexHome,
				"OPENAI_API_KEY="+LoopbackAuthPlaceholder,
				"HOME="+home,
			),
			home,
		)
		if strings.Contains(out, "Falling back from WebSockets") {
			t.Fatalf("codex warned about WebSocket fallback:\n%s", trimTail(out, 1600))
		}
		if !strings.Contains(out, "ok") {
			t.Fatalf("codex did not print the streamed reply, tail=%q", trimTail(out, 600))
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			upstream.mu.Lock()
			h, f := upstream.handshakes, upstream.frames
			upstream.mu.Unlock()
			if h >= 1 && f >= 1 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		upstream.mu.Lock()
		h, f, posts := upstream.handshakes, upstream.frames, upstream.postHits
		upstream.mu.Unlock()
		if h < 1 {
			t.Fatalf("router live: no WebSocket handshake reached the upstream")
		}
		if f < 1 {
			t.Fatalf("router live: no request frame reached the upstream")
		}
		if posts != 0 {
			t.Fatalf("router live: codex fell back to HTTPS POST (%d hits)", posts)
		}
	})
}

func TestCodexWSImmediateTerminationLive(t *testing.T) {
	if os.Getenv("ZEN_CODEX_WS_INTEGRATION") == "" {
		t.Skip("set ZEN_CODEX_WS_INTEGRATION=1 to run the Codex WebSocket live proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	for _, mode := range []wsCompletionCloseMode{wsCompletionNormalClose, wsCompletionTCPEOF} {
		t.Run(string(mode), func(t *testing.T) {
			upstream := newWSCompletionUpstream(t, mode, 1)
			gateway := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
			if err := gateway.Listen(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = gateway.Close() })
			gateway.SetUpstream(gatewayUpstreamFor(upstream.server.URL))

			home := t.TempDir()
			codexHome := filepath.Join(home, "codex-home")
			if err := os.MkdirAll(codexHome, 0o700); err != nil {
				t.Fatal(err)
			}
			config := "model_provider = \"zen-gateway\"\nmodel = \"gpt-4o\"\n" +
				"[model_providers.zen-gateway]\n" +
				"name = \"zen-gateway\"\n" +
				"base_url = \"http://" + gateway.ActualAddr() + "/v1\"\n" +
				"wire_api = \"responses\"\n" +
				"requires_openai_auth = false\n" +
				"supports_websockets = true\n" +
				"request_max_retries = 0\n" +
				"stream_max_retries = 0\n"
			if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}

			out := runCodex(t, codexPath,
				[]string{"exec", "--skip-git-repo-check",
					"--dangerously-bypass-approvals-and-sandbox",
					"-c", `model_provider="zen-gateway"`,
					"-c", `model="gpt-4o"`,
					"reply with no text"},
				append(scrubEnv(os.Environ(), "CODEX_HOME"), "CODEX_HOME="+codexHome, "HOME="+home),
				home,
			)
			if strings.Contains(out, "Falling back from WebSockets") || strings.Contains(out, "stream disconnected before completion") {
				t.Fatalf("codex rejected completed stream followed by %s:\n%s", mode, trimTail(out, 2000))
			}
			if upstream.seenTurns() != 2 {
				t.Fatalf("upstream turns = %d, want 2 (prewarm + actual)", upstream.seenTurns())
			}
		})
	}
}

func TestCodexWSHandshakeRejectionFallsBackSilentlyLive(t *testing.T) {
	if os.Getenv("ZEN_CODEX_WS_INTEGRATION") == "" {
		t.Skip("set ZEN_CODEX_WS_INTEGRATION=1 to run the Codex WebSocket live proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	var wsHits, postHits int
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			mu.Lock()
			wsHits++
			mu.Unlock()
			http.Error(w, "websocket unsupported", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		postHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp-fallback"}},
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{
				"id": "msg-fallback", "type": "message", "role": "assistant", "content": []any{},
			}},
			{"type": "response.content_part.added", "item_id": "msg-fallback", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": ""}},
			{"type": "response.output_text.delta", "item_id": "msg-fallback", "output_index": 0, "content_index": 0, "delta": "ok"},
			{"type": "response.output_text.done", "item_id": "msg-fallback", "output_index": 0, "content_index": 0, "text": "ok"},
			{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{
				"id": "msg-fallback", "type": "message", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "ok", "annotations": []any{}}},
			}},
			{"type": "response.completed", "response": map[string]any{
				"id": "resp-fallback", "status": "completed",
				"usage": map[string]any{"input_tokens": 1, "input_tokens_details": nil, "output_tokens": 1, "output_tokens_details": nil, "total_tokens": 2},
			}},
		} {
			raw, _ := json.Marshal(event)
			_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	gateway := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := gateway.Listen(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	gateway.SetUpstream(gatewayUpstreamFor(upstream.URL))

	home := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "model_provider = \"zen-gateway\"\nmodel = \"gpt-4o\"\n" +
		"[model_providers.zen-gateway]\n" +
		"name = \"zen-gateway\"\n" +
		"base_url = \"http://" + gateway.ActualAddr() + "/v1\"\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = false\n" +
		"supports_websockets = true\n" +
		"request_max_retries = 0\n" +
		"stream_max_retries = 0\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runCodex(t, codexPath,
		[]string{"exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "reply with the single word ok"},
		append(scrubEnv(os.Environ(), "CODEX_HOME"), "CODEX_HOME="+codexHome, "HOME="+home), home)
	if strings.Contains(out, "Falling back from WebSockets") {
		t.Fatalf("Codex 0.147 exposed capability fallback warning:\n%s", trimTail(out, 2000))
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("Codex did not complete over HTTPS fallback:\n%s", trimTail(out, 2000))
	}
	mu.Lock()
	defer mu.Unlock()
	if wsHits < 1 || postHits < 1 {
		t.Fatalf("upstream hits websocket=%d post=%d, want both", wsHits, postHits)
	}
}

// runCodex executes the installed codex CLI with the given env/dir; combined
// output is returned and a fatal error is raised on non-zero exit.
func runCodex(t *testing.T, codexPath string, args []string, env []string, dir string) string {
	t.Helper()
	cmd := exec.Command(codexPath, args...)
	if env != nil {
		cmd.Env = env
	}
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex %v failed: %v\n%s", args, err, trimTail(string(out), 1600))
	}
	return string(out)
}

func trimTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func scrubEnv(env []string, drop ...string) []string {
	ban := map[string]struct{}{}
	for _, k := range drop {
		ban[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if _, ok := ban[key]; ok {
			continue
		}
		out = append(out, e)
	}
	return out
}
