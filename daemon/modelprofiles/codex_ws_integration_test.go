package modelprofiles_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
)

// Opt-in Codex WebSocket→POST fallback probe against a local Zen-shaped route.
//
// Run (from daemon module root):
//
//	ZEN_CODEX_WS_INTEGRATION=1 go test ./modelprofiles -run TestCodexWSFallbackIntegration -count=1 -timeout 60s
//
// Requires an installed `codex` on PATH. Uses a temporary CODEX_HOME and the
// fixed non-secret loopback placeholder — never real credentials, user config,
// or network beyond 127.0.0.1. Observed on this workstation: codex-cli 0.146.1
// receives Upgrade→501 then POSTs /v1/responses. Future Codex versions must
// re-run this probe; do not treat the result as a permanent product guarantee.
func TestCodexWSFallbackIntegration(t *testing.T) {
	if os.Getenv("ZEN_CODEX_WS_INTEGRATION") == "" {
		t.Skip("set ZEN_CODEX_WS_INTEGRATION=1 to run Codex WS fallback probe")
	}
	tmpRoot := t.TempDir()
	codexHome := filepath.Join(tmpRoot, "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	verCmd := exec.Command(codexPath, "--version")
	verCmd.Env = append(scrubEnv(os.Environ(), "CODEX_HOME"), "CODEX_HOME="+codexHome, "HOME="+tmpRoot)
	verOut, err := verCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex --version: %v (%s)", err, verOut)
	}
	version := strings.TrimSpace(string(verOut))
	t.Logf("codex --version => %s", version)

	var mu sync.Mutex
	events := []map[string]any{}
	add := func(ev map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		up := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		add(map[string]any{
			"method": r.Method, "path": r.URL.Path, "upgrade": up,
			"connection": r.Header.Get("Connection"), "body_len": len(body),
		})
		if up {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"type":"route_websocket_rejected","message":"request failed"}}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/responses") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"resp_fake","object":"response","status":"completed","output":[]}`))
			return
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	base := "http://" + ln.Addr().String() + "/v1"
	cmd := exec.Command(codexPath, "exec", "--skip-git-repo-check",
		"-c", `model_provider="openai"`,
		"-c", `openai_base_url="`+base+`"`,
		"-c", `model="gpt-4o"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"reply with the single word ok",
	)
	cmd.Env = append(scrubEnv(os.Environ(), "CODEX_HOME", "OPENAI_API_KEY"),
		"CODEX_HOME="+codexHome,
		"OPENAI_API_KEY="+modelprofiles.LoopbackAuthPlaceholder,
		"HOME="+tmpRoot,
	)
	cmd.Dir = tmpRoot
	out, err := cmd.CombinedOutput()
	t.Logf("codex exit=%v output_tail=%q", err, trimTail(string(out), 800))

	deadline := time.Now().Add(2 * time.Second)
	var sawWS501, sawPOST bool
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, ev := range events {
			if ev["upgrade"] == true && ev["method"] == "GET" {
				sawWS501 = true
			}
			if ev["upgrade"] == false && ev["method"] == "POST" && strings.HasSuffix(ev["path"].(string), "/responses") {
				if n, _ := ev["body_len"].(int); n > 0 {
					sawPOST = true
				}
			}
		}
		snapshot, _ := json.Marshal(events)
		mu.Unlock()
		if sawWS501 && sawPOST {
			t.Logf("events=%s", snapshot)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !sawWS501 {
		t.Fatalf("expected WebSocket Upgrade hit (501 path); version=%s events=%v", version, events)
	}
	if !sawPOST {
		t.Fatalf("expected POST /v1/responses after WS reject; version=%s events=%v", version, events)
	}
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
