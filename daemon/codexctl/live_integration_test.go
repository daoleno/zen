package codexctl_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/codexctl"
)

// Opt-in live native thread-settings proof against the installed Codex 0.147
// app server + TUI in tmux. This is the acceptance boundary for the native
// model/effort synchronization work:
//
//	ZEN_CODEX_LIVE_CONTROL=1 go test ./codexctl -run TestLiveNativeThreadSettings -count=1 -timeout 420s -v
//
// It proves, with one real process tree: same tmux/process identity across the
// mutation; the native thread setting changed (TUI footer + next request body
// model/effort, including Codex's native <model_switch> signal); the
// thread/settings/updated acknowledgement matched; and a rejected native
// mutation leaves the thread untouched. Artifacts land under TMPDIR
// (Agent-owned). No real credentials / user config / live Sessions.
func TestLiveNativeThreadSettings(t *testing.T) {
	if os.Getenv("ZEN_CODEX_LIVE_CONTROL") == "" {
		t.Skip("set ZEN_CODEX_LIVE_CONTROL=1 for the live Codex app-server proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux not on PATH: %v", err)
	}
	if out, err := exec.Command(codexPath, "--version").CombinedOutput(); err != nil {
		t.Fatalf("codex --version: %v", err)
	} else {
		t.Logf("codex version=%s", strings.TrimSpace(string(out)))
	}

	artRoot := filepath.Join(os.Getenv("TMPDIR"), "zen-codex-live-ctl")
	_ = os.RemoveAll(artRoot)
	if err := os.MkdirAll(artRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(artRoot, "codex-home")
	cwd := filepath.Join(artRoot, "cwd")
	for _, dir := range []string{codexHome, cwd} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "init", "-q", cwd).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"OPENAI_API_KEY":"zen-loopback-placeholder-not-a-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var hits [][]byte
	upstream := httptest.NewServer(liveUpstreamFake(t, &mu, &hits))
	defer upstream.Close()

	cfg := fmt.Sprintf("model = \"gpt-5\"\nmodel_provider = \"openai\"\nopenai_base_url = %q\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n", upstream.URL+"/v1")
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join(artRoot, "codex-ctl.sock")
	env := []string{
		"CODEX_HOME=" + codexHome,
		"HOME=" + artRoot,
		"OPENAI_API_KEY=zen-loopback-placeholder-not-a-secret",
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
	}
	appServer := exec.Command(codexPath, "app-server", "--listen", "unix://"+socketPath)
	appServer.Env = env
	appServer.Stdout = io.Discard
	appServer.Stderr = io.Discard
	if err := appServer.Start(); err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	t.Cleanup(func() {
		_ = appServer.Process.Kill()
		_, _ = appServer.Process.Wait()
	})
	waitSocket(t, socketPath)

	sess := fmt.Sprintf("zen-codex-live-ctl-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sess, "-c", cwd,
		"-e", "CODEX_HOME="+codexHome,
		"-e", "HOME="+artRoot,
		"-e", "OPENAI_API_KEY=zen-loopback-placeholder-not-a-secret",
		"-e", "TERM=xterm-256color",
		"--", codexPath, "--remote", "unix://"+socketPath,
		"--model", "gpt-5", "--config", `model_provider="openai"`,
		"--config", fmt.Sprintf("openai_base_url=%q", upstream.URL+"/v1"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v (%s)", err, out)
	}
	waitCodexTUIReady(t, sess, "gpt-5")
	before := panePID(t, sess)
	if before == 0 {
		t.Fatal("missing pane process identity")
	}

	// Turn 1 on the launch model.
	sendTmuxLine(t, sess, "Reply with exactly: alpha")
	waitHits(t, &mu, &hits, 1, 60*time.Second)
	waitPaneContains(t, sess, "token-1", 60*time.Second)
	mu.Lock()
	firstBody := string(hits[0])
	mu.Unlock()
	assertBodyModelEffort(t, firstBody, "gpt-5", "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := codexctl.Open(ctx, socketPath, codexctl.DialOptions{})
	if err != nil {
		t.Fatalf("open live control: %v", err)
	}
	defer client.Close()

	threadID, err := client.ResolveThread(ctx, cwd)
	if err != nil {
		t.Fatalf("resolve thread: %v", err)
	}
	t.Logf("resolved native thread=%s", threadID)
	if threadID == "" {
		t.Fatal("empty native thread id")
	}

	// Native mutation: model gpt-5.5 + effort high, ack = applied notification.
	effort := "high"
	revert, err := client.ApplySettings(ctx, threadID, "gpt-5.5", &effort, codexctl.Settings{ThreadID: threadID, Model: "gpt-5"}, codexctl.DefaultAckTimeout)
	if err != nil {
		t.Fatalf("apply settings: %v", err)
	}
	if revert == nil {
		t.Fatal("missing revert closure")
	}

	// Same tmux/process identity: the TUI process must not have restarted.
	after := panePID(t, sess)
	if after != before {
		t.Fatalf("process identity changed across native mutation: %d -> %d", before, after)
	}
	// TUI/native thread state changed: the footer now shows the new model.
	waitPaneContains(t, sess, "gpt-5.5", 30*time.Second)

	// Next request uses the exact selected model+effort natively.
	sendTmuxLine(t, sess, "Reply with exactly: beta")
	waitHits(t, &mu, &hits, 2, 60*time.Second)
	waitPaneContains(t, sess, "token-2", 60*time.Second)
	mu.Lock()
	secondBody := string(hits[len(hits)-1])
	mu.Unlock()
	assertBodyModelEffort(t, secondBody, "gpt-5.5", "high")
	// Codex's native model-switch developer fragment is present — the same
	// signal Zen's router uses to converge the Interface projection.
	if !strings.Contains(secondBody, "<model_switch>") || !strings.Contains(secondBody, "previously using a different model") {
		t.Fatalf("native model_switch signal missing from next request: %s", trimTail(secondBody, 600))
	}

	// Rejected native mutation keeps the thread untouched: an unknown thread id
	// is rejected by the app server before any settings change.
	beforeHits := len(hits)
	if _, err := client.ApplySettings(ctx, "00000000-0000-0000-0000-000000000000", "gpt-5.5", nil, codexctl.Settings{}, 5*time.Second); err == nil {
		t.Fatal("unknown thread id must be rejected natively")
	}
	sendTmuxLine(t, sess, "Reply with exactly: gamma")
	waitHits(t, &mu, &hits, beforeHits+1, 60*time.Second)
	mu.Lock()
	thirdBody := string(hits[len(hits)-1])
	mu.Unlock()
	assertBodyModelEffort(t, thirdBody, "gpt-5.5", "high")

	t.Logf("proof ok: thread=%s pane=%d requests=%d", threadID, after, len(hits))
}

// liveUpstreamFake serves GET /v1/models (thread model resolution) and
// POST /v1/responses (SSE replies) for the isolated app server.
func liveUpstreamFake(t *testing.T, mu *sync.Mutex, sink *[][]byte) http.Handler {
	t.Helper()
	var n int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5","object":"model"},{"id":"gpt-5.5","object":"model"}]}`))
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		mu.Lock()
		*sink = append(*sink, append([]byte(nil), body...))
		n++
		turn := n
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		id := fmt.Sprintf("resp_%d", turn)
		msgID := fmt.Sprintf("msg_%d", turn)
		text := fmt.Sprintf("token-%d", turn)
		write := func(obj map[string]any) {
			raw, _ := json.Marshal(obj)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", obj["type"], raw)
			if flusher != nil {
				flusher.Flush()
			}
		}
		final := map[string]any{
			"type": "message", "id": msgID, "status": "completed", "role": "assistant",
			"content": []any{map[string]string{"type": "output_text", "text": text}},
		}
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": final})
		write(map[string]any{"type": "response.content_part.added", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": text}})
		write(map[string]any{"type": "response.output_text.delta", "item_id": msgID, "output_index": 0, "content_index": 0, "delta": text})
		write(map[string]any{"type": "response.output_text.done", "item_id": msgID, "output_index": 0, "content_index": 0, "text": text})
		write(map[string]any{"type": "response.content_part.done", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": text}})
		write(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": final})
		write(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "status": "completed", "output": []any{final}}})
	})
}

func assertBodyModelEffort(t *testing.T, body, model, effort string) {
	t.Helper()
	var payload struct {
		Model     string `json:"model"`
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode request body: %v (%s)", err, trimTail(body, 300))
	}
	if payload.Model != model {
		t.Fatalf("request model=%q want %q", payload.Model, model)
	}
	if payload.Reasoning.Effort != effort {
		t.Fatalf("request effort=%q want %q", payload.Reasoning.Effort, effort)
	}
}

func waitSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("app-server control socket never appeared at %s", socketPath)
}

func waitCodexTUIReady(t *testing.T, sess, model string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pane := capturePane(t, sess)
		if strings.Contains(pane, model) && strings.Contains(pane, "›") {
			return
		}
		if strings.Contains(pane, "Yes, continue") || strings.Contains(pane, "trust the contents") {
			_ = exec.Command("tmux", "send-keys", "-t", sess, "1").Run()
			time.Sleep(150 * time.Millisecond)
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
			time.Sleep(time.Second)
			continue
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("codex TUI not ready (model %s):\n%s", model, capturePane(t, sess))
}

func panePID(t *testing.T, sess string) int {
	t.Helper()
	out, err := exec.Command("tmux", "list-panes", "-t", sess, "-F", "#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func sendTmuxLine(t *testing.T, sess, line string) {
	t.Helper()
	if err := exec.Command("tmux", "send-keys", "-t", sess, "-l", "--", line).Run(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run(); err != nil {
		t.Fatal(err)
	}
}

func capturePane(t *testing.T, sess string) string {
	t.Helper()
	out, err := exec.Command("tmux", "capture-pane", "-t", sess, "-p", "-S", "-120").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func waitPaneContains(t *testing.T, sess, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(capturePane(t, sess), needle) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q:\n%s", needle, capturePane(t, sess))
}

func waitHits(t *testing.T, mu *sync.Mutex, sink *[][]byte, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*sink)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("upstream hits = %d, want %d", len(*sink), want)
}

func trimTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
