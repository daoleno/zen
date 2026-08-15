package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
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
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/codexctl"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

// TestLiveSameModelEffortConvergence is the end-to-end acceptance proof for
// the same-model effort-only Terminal change: with the real Codex 0.147 app
// server + TUI routed through the Zen loopback router, a native
// thread/settings/update (the exact mutation /model performs) on the SAME
// model changes the TUI footer and native settings, and the next request —
// which carries no model-switch fragment — converges the Zen route binding
// and the Interface WebSocket projection to the same effort, with the request
// forwarded unchanged. No process restart.
//
//	ZEN_CODEX_LIVE_CONTROL=1 go test ./server -run TestLiveSameModelEffortConvergence -count=1 -timeout 420s -v
func TestLiveSameModelEffortConvergence(t *testing.T) {
	if os.Getenv("ZEN_CODEX_LIVE_CONTROL") == "" {
		t.Skip("set ZEN_CODEX_LIVE_CONTROL=1 for the live Codex convergence proof")
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

	artRoot := filepath.Join(os.Getenv("TMPDIR"), "zen-codex-live-effort-converge")
	_ = os.RemoveAll(artRoot)
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
	var hits []string
	upstream := httptest.NewServer(liveConvergeUpstreamFake(t, &mu, &hits))
	defer upstream.Close()

	root := t.TempDir()
	// The app-server control socket must fit the unix SUN_LEN limit; the
	// test tmpdir nests too deeply, so use a short dedicated control dir.
	controlDir := filepath.Join(os.Getenv("TMPDIR"), "zen-live-ctl")
	_ = os.RemoveAll(controlDir)
	t.Cleanup(func() { _ = os.RemoveAll(controlDir) })
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath:    filepath.Join(root, "model-profiles.toml"),
		RoutesPath:      filepath.Join(root, "route-bindings.json"),
		ListenerPath:    filepath.Join(root, "route-listener.json"),
		CodexControlDir: controlDir,
		Lookup:          func(string) (string, bool) { return "key-a", true },
		Verifier:        wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "provider-a", Name: "Provider A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "a", ProviderLabel: "A", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5", Model: "gpt-5", BaseURL: upstream.URL + "/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "A_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	// Sync the connection's model catalog from the provider (the router's
	// GET /v1/models serves it to the Codex app server so the native catalog
	// resolves to this connection's models — no migration prompt).
	if _, err := owner.DiscoverProviderModels(profile.ID, true); err != nil {
		t.Fatalf("discover models: %v", err)
	}
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if launch.CodexControlSocket == "" {
		t.Fatal("live-control launch must allocate a socket")
	}
	const sessionID = "tmux:@live-effort"
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil {
		t.Fatal(err)
	}
	router := httptest.NewServer(owner.RouterHandler())
	defer router.Close()
	base, err := modelprofiles.LoopbackCodexBaseURL(router.Listener.Addr().String(), launch.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	authManager, err := auth.NewManager(filepath.Join(root, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519Generate()
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-live-effort", "phone", hexEncode(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	wsServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer wsServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-live-effort", "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(wsServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	getProjection := func() (model string, effort string) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{
			"type": "get_thread_runtime", "request_id": "gtr-live-effort", "agent_id": sessionID,
		}); err != nil {
			t.Fatal(err)
		}
		payload := readType("thread_runtime")
		runtime, _ := payload["runtime"].(map[string]any)
		model, _ = runtime["model_id"].(string)
		effort, _ = runtime["reasoning_effort"].(string)
		return model, effort
	}

	socketPath := launch.CodexControlSocket
	// Config must exist before the app server starts (it loads config at
	// startup); the TUI reads the same file.
	cfg := fmt.Sprintf("model = \"gpt-5\"\nmodel_provider = \"openai\"\nopenai_base_url = %q\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n", base)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	appLog, err := os.Create(filepath.Join(artRoot, "app-server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer appLog.Close()
	env := []string{
		"CODEX_HOME=" + codexHome,
		"HOME=" + artRoot,
		"OPENAI_API_KEY=zen-loopback-placeholder-not-a-secret",
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
	}
	appServer := exec.Command(codexPath, "app-server", "--listen", "unix://"+socketPath)
	appServer.Env = env
	appServer.Stdout = appLog
	appServer.Stderr = appLog
	if err := appServer.Start(); err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	t.Cleanup(func() {
		_ = appServer.Process.Signal(os.Interrupt)
		waitProcessExitLive(appServer, 5*time.Second)
		_ = appServer.Process.Kill()
		waitProcessExitLive(appServer, 5*time.Second)
	})
	if err := waitSocketErrLive(socketPath, 30*time.Second); err != nil {
		raw, _ := os.ReadFile(appLog.Name())
		t.Fatalf("app-server socket never appeared: %v\nlog:\n%s", err, trimTailLive(string(raw), 2000))
	}

	sess := fmt.Sprintf("zen-codex-live-effort-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sess, "-c", cwd,
		"-e", "CODEX_HOME="+codexHome,
		"-e", "HOME="+artRoot,
		"-e", "OPENAI_API_KEY=zen-loopback-placeholder-not-a-secret",
		"-e", "TERM=xterm-256color",
		"--", codexPath, "--remote", "unix://"+socketPath,
		"--model", "gpt-5", "--config", `model_provider="openai"`,
		"--config", fmt.Sprintf("openai_base_url=%q", base),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v (%s)", err, out)
	}
	waitCodexTUIReadyLive(t, sess, "gpt-5")
	if !appServerAlive(appServer) {
		raw, _ := os.ReadFile(appLog.Name())
		t.Fatalf("app-server died during TUI startup:\n%s", trimTailLive(string(raw), 2000))
	}
	if panePIDLive(t, sess) == 0 {
		raw, _ := os.ReadFile(appLog.Name())
		tuiLogs, _ := filepath.Glob(filepath.Join(artRoot, ".codex", "log", "*"))
		dump := ""
		for _, path := range tuiLogs {
			if raw, err := os.ReadFile(path); err == nil {
				dump += "=== " + path + " ===\n" + trimTailLive(string(raw), 1500) + "\n"
			}
		}
		t.Fatalf("TUI pane is dead:\napp-server log:\n%s\nTUI logs:\n%s", trimTailLive(string(raw), 2000), dump)
	}
	before := panePIDLive(t, sess)
	if before == 0 {
		t.Fatal("missing pane process identity")
	}

	// Turn 1 on the launch model.
	sendTmuxLineLive(t, sess, "Reply with exactly: alpha")
	waitHitsLive(t, &mu, &hits, 1, 90*time.Second)
	mu.Lock()
	firstBody := hits[0]
	mu.Unlock()
	if !strings.Contains(firstBody, `"gpt-5"`) {
		t.Fatalf("first request model mismatch: %s", trimTailLive(firstBody, 400))
	}
	if model, effort := getProjection(); model != "gpt-5" || effort != "" {
		t.Fatalf("initial projection model=%q effort=%q", model, effort)
	}

	ctx, cancel := contextTimeoutLive(40 * time.Second)
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

	// Native same-model effort change (the exact mutation /model performs):
	// same model gpt-5, effort low. No model-switch fragment will appear.
	effort := "low"
	revert, err := client.ApplySettings(ctx, threadID, "gpt-5", &effort, codexctl.Settings{ThreadID: threadID, Model: "gpt-5"}, codexctl.DefaultAckTimeout)
	if err != nil {
		t.Fatalf("apply low effort: %v", err)
	}
	if revert == nil {
		t.Fatal("missing revert closure")
	}
	waitPaneContainsLive(t, sess, "gpt-5 low", 30*time.Second)
	if after := panePIDLive(t, sess); after != before {
		t.Fatalf("process identity changed across effort change: %d -> %d", before, after)
	}
	// Probe the authoritative native settings directly (thread/resume) so the
	// evidence the Router's monitor must see is confirmed.
	probe, err := codexctl.Open(ctx, socketPath, codexctl.DialOptions{})
	if err != nil {
		t.Fatalf("open probe: %v", err)
	}
	probeResume, err := probe.ResumeThread(ctx, threadID)
	probe.Close()
	if err != nil {
		t.Fatalf("probe resume: %v", err)
	}
	t.Logf("native settings probe: model=%q effort=%q", probeResume.Model, probeResume.Effort)
	if probeResume.Model != "gpt-5" || probeResume.Effort != "low" {
		t.Fatalf("native settings probe mismatch: %#v", probeResume)
	}

	// Turn 2: the request must reach the upstream CONVERGED (effort low),
	// not rewritten back to the stale binding.
	sendTmuxLineLive(t, sess, "Reply with exactly: beta")
	waitHitsLive(t, &mu, &hits, 2, 90*time.Second)
	waitPaneContainsLive(t, sess, "token-2", 90*time.Second)
	mu.Lock()
	secondBody := hits[len(hits)-1]
	mu.Unlock()
	if !strings.Contains(secondBody, `"effort":"low"`) {
		// Debug: show the reasoning block + the route state.
		reasoning := ""
		if idx := strings.Index(secondBody, `"reasoning"`); idx >= 0 {
			reasoning = secondBody[idx:]
			if len(reasoning) > 300 {
				reasoning = reasoning[:300]
			}
		}
		runtime, _ := owner.ThreadRuntime(sessionID)
		t.Fatalf("next request must carry the native low effort (converged, not rewritten): reasoning=%q runtime=%#v full=%s", reasoning, runtime, trimTailLive(secondBody, 900))
	}
	if strings.Contains(secondBody, "<model_switch>") {
		t.Fatalf("same-model effort change must not fabricate a model-switch fragment: %s", trimTailLive(secondBody, 500))
	}

	// Route binding and Interface projection agree.
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ModelID != "gpt-5" || runtime.ReasoningEffort != "low" {
		t.Fatalf("route binding after native effort change: %#v", runtime)
	}
	if model, effort := getProjection(); model != "gpt-5" || effort != "low" {
		t.Fatalf("Interface projection after native effort change model=%q effort=%q, want gpt-5/low", model, effort)
	}

	// Native default round-trip: effort back to model default converges to a
	// cleared route effort with the request forwarded without an effort.
	revert, err = client.ApplySettings(ctx, threadID, "gpt-5", nil, codexctl.Settings{ThreadID: threadID, Model: "gpt-5", Effort: "low"}, codexctl.DefaultAckTimeout)
	if err != nil {
		t.Fatalf("apply default effort: %v", err)
	}
	if revert == nil {
		t.Fatal("missing default revert closure")
	}
	if !waitPaneContainsOkLive(t, sess, "gpt-5 default", 30*time.Second) {
		ps, _ := exec.Command("ps", "-eo", "pid,ppid,args").Output()
		lines := []string{}
		for _, line := range strings.Split(string(ps), "\n") {
			if strings.Contains(line, socketPath) || strings.Contains(line, "codex --remote") {
				lines = append(lines, line)
			}
		}
		cwds := ""
		for _, token := range []string{"codex app-server", "codex --remote"} {
			probe, _ := exec.Command("pgrep", "-f", token+" .*"+socketPath).Output()
			for _, pid := range strings.Fields(string(probe)) {
				link, _ := os.Readlink("/proc/" + pid + "/cwd")
				cwds += "pid " + pid + " cwd=" + link + "\n"
			}
		}
		tuiLogs, _ := filepath.Glob(filepath.Join(artRoot, ".codex", "log", "codex-tui.log"))
		logTail := ""
		if len(tuiLogs) > 0 {
			if raw, err := os.ReadFile(tuiLogs[0]); err == nil {
				logTail = trimTailLive(string(raw), 1200)
			}
		}
		pane := capturePaneLive(t, sess)
		paneLines := strings.Split(pane, "\n")
		if len(paneLines) > 15 {
			paneLines = paneLines[len(paneLines)-15:]
		}
		t.Fatalf("pane never showed default footer.\ncodex processes:\n%s\ncwds:\n%stui log:\n%s\npane last lines:\n%s", strings.Join(lines, "\n"), cwds, logTail, strings.Join(paneLines, "\n"))
	}
	sendTmuxLineLive(t, sess, "Reply with exactly: gamma")
	waitHitsLive(t, &mu, &hits, 3, 90*time.Second)
	mu.Lock()
	thirdBody := hits[len(hits)-1]
	mu.Unlock()
	if strings.Contains(thirdBody, `"effort"`) {
		t.Fatalf("default-effort request must carry no explicit effort: %s", trimTailLive(thirdBody, 500))
	}
	if runtime, ok := owner.ThreadRuntime(sessionID); !ok || runtime.ReasoningEffort != "" {
		t.Fatalf("route binding after native default change: %#v", runtime)
	}
	if model, effort := getProjection(); model != "gpt-5" || effort != "" {
		t.Fatalf("projection after native default change model=%q effort=%q", model, effort)
	}
	if after := panePIDLive(t, sess); after != before {
		t.Fatalf("process identity changed across default change: %d -> %d", before, after)
	}

	t.Logf("proof ok: thread=%s pane=%d requests=%d", threadID, before, len(hits))
}

// liveConvergeUpstreamFake serves GET /v1/models and POST /v1/responses (SSE)
// for the routed live session.
func liveConvergeUpstreamFake(t *testing.T, mu *sync.Mutex, sink *[]string) http.Handler {
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
		*sink = append(*sink, string(body))
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

func waitSocketErrLive(socketPath string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("socket %s missing", socketPath)
}

func waitSocketLive(t *testing.T, socketPath string) {
	t.Helper()
	if err := waitSocketErrLive(socketPath, 30*time.Second); err != nil {
		t.Fatalf("app-server control socket never appeared: %v", err)
	}
}

func waitCodexTUIReadyLive(t *testing.T, sess, model string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pane := capturePaneLive(t, sess)
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
	t.Fatalf("codex TUI not ready (model %s):\n%s", model, capturePaneLive(t, sess))
}

func panePIDLive(t *testing.T, sess string) int {
	t.Helper()
	out, err := exec.Command("tmux", "list-panes", "-t", sess, "-F", "#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func sendTmuxLineLive(t *testing.T, sess, line string) {
	t.Helper()
	if err := exec.Command("tmux", "send-keys", "-t", sess, "-l", "--", line).Run(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run(); err != nil {
		t.Fatal(err)
	}
}

func capturePaneLive(t *testing.T, sess string) string {
	t.Helper()
	out, err := exec.Command("tmux", "capture-pane", "-t", sess, "-p", "-S", "-120").Output()
	if err != nil {
		return "(tmux capture error: " + err.Error() + ")"
	}
	return string(out)
}

func waitPaneContainsOkLive(t *testing.T, sess, needle string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(capturePaneLive(t, sess), needle) {
			return true
		}
		time.Sleep(400 * time.Millisecond)
	}
	return false
}

func waitPaneContainsLive(t *testing.T, sess, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(capturePaneLive(t, sess), needle) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q:\n%s", needle, capturePaneLive(t, sess))
}

func waitHitsLive(t *testing.T, mu *sync.Mutex, sink *[]string, want int, d time.Duration) {
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

func trimTailLive(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func appServerAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return cmd.Process.Signal(syscall.Signal(0)) == nil
}

func waitProcessExitLive(cmd *exec.Cmd, d time.Duration) {
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func contextTimeoutLive(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func ed25519Generate() ([]byte, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	return publicKey, privateKey, err
}

func hexEncode(raw []byte) string {
	return hex.EncodeToString(raw)
}
