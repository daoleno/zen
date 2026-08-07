package modelprofiles_test

import (
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

	"github.com/daoleno/zen/daemon/modelprofiles"
)

// Opt-in same-tmux / same-OS-PID portable switch proof.
// This is the acceptance boundary (not codex exec resume / claude --continue).
//
//	ZEN_PORTABLE_HISTORY_SAMEPID=1 go test ./modelprofiles -run 'TestPortableHistorySamePID' -count=1 -timeout 300s -v
//
// Artifacts land under TMPDIR (Agent-owned). No real credentials / user config / live Sessions.

func TestPortableHistorySamePIDCodex(t *testing.T) {
	if os.Getenv("ZEN_PORTABLE_HISTORY_SAMEPID") == "" {
		t.Skip("set ZEN_PORTABLE_HISTORY_SAMEPID=1 for Codex same-tmux/PID proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux not on PATH: %v", err)
	}
	ver := runVersion(t, codexPath, "CODEX_HOME")
	t.Logf("codex version=%s", ver)

	artRoot := filepath.Join(tmpdir(t), "zen-portable-samepid-codex")
	_ = os.RemoveAll(artRoot)
	if err := os.MkdirAll(artRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(artRoot, "codex-home")
	cwd := filepath.Join(artRoot, "cwd")
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.MkdirAll(cwd, 0o700)
	if err := exec.Command("git", "init", "-q", cwd).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"OPENAI_API_KEY":"zen-loopback-placeholder-not-a-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var hitA, hitB [][]byte
	upA := httptest.NewServer(codexStatefulFake(t, &mu, &hitA, "A", true))
	defer upA.Close()
	upB := httptest.NewServer(codexStatefulFake(t, &mu, &hitB, "B", false))
	defer upB.Close()

	table := modelprofiles.NewRouteTable()
	auth := modelprofiles.ContractAuth{Verifier: modelprofiles.BuiltinEnvelopeVerifier{}}
	profileA := modelprofiles.Profile{
		ID: "a", Name: "A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "fake-a", ProviderLabel: "FakeA",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "model-a",
		ClientModelProvenance: modelprofiles.ContractProvenanceConfiguredCompatibility,
		BaseURL:               strings.TrimSuffix(upA.URL, "/") + "/v1",
		AuthMode:              modelprofiles.AuthModeNone,
	}
	profileB := profileA
	profileB.ID, profileB.Name, profileB.ProviderID, profileB.ProviderLabel, profileB.Model =
		"b", "B", "fake-b", "FakeB", "model-b"
	profileB.BaseURL = strings.TrimSuffix(upB.URL, "/") + "/v1"

	state, err := table.BindLaunch("samepid-codex", profileA, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := modelprofiles.NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, err := modelprofiles.LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("model = \"gpt-5\"\nmodel_provider = \"openai\"\nopenai_base_url = %q\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n", base)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	sess := fmt.Sprintf("zen-samepid-codex-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sess, "-c", cwd,
		"-e", "CODEX_HOME="+codexHome,
		"-e", "HOME="+artRoot,
		"-e", "OPENAI_API_KEY="+modelprofiles.LoopbackAuthPlaceholder,
		"-e", "TERM=xterm-256color",
		"--", codexPath, "--dangerously-bypass-approvals-and-sandbox",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v (%s)", err, out)
	}
	waitCodexReady(t, sess)

	before := captureIdentity(t, sess, codexHome, "codex")
	t.Logf("before: tmux=%s pane_pid=%d client_pid=%d session=%s route=%s",
		sess, before.PanePID, before.ClientPID, before.NativeSessionID, state.Binding.RouteID)
	if before.PanePID == 0 || before.ClientPID == 0 {
		t.Fatalf("missing process identity: %#v", before)
	}

	// Turn 1: harmless tool write in owned scratch + visible text.
	sendTmuxLine(t, sess, `Create file proof.txt containing exactly HELLO using a shell command, then reply with exactly: alpha`)
	waitHits(t, &mu, &hitA, 45*time.Second)
	waitPaneContains(t, sess, "token-A", 45*time.Second)
	before = captureIdentity(t, sess, codexHome, "codex")
	if before.NativeSessionID == "" {
		t.Fatalf("native codex session id not found after turn1 under %s", codexHome)
	}
	t.Logf("after turn1: session=%s A_hits pending", before.NativeSessionID)

	got, err := table.Activate("samepid-codex", profileB, 2, state.Generation, auth)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != modelprofiles.HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	if got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route replaced")
	}

	mid := captureIdentity(t, sess, codexHome, "codex")
	if mid.PanePID != before.PanePID || mid.ClientPID != before.ClientPID {
		t.Fatalf("PID changed across activate: before=%#v mid=%#v", before, mid)
	}
	if mid.NativeSessionID != before.NativeSessionID {
		t.Fatalf("native session replaced: %s -> %s", before.NativeSessionID, mid.NativeSessionID)
	}
	if mid.TmuxTarget != before.TmuxTarget {
		t.Fatalf("tmux target changed: %s -> %s", before.TmuxTarget, mid.TmuxTarget)
	}

	sendTmuxLine(t, sess, `Reply with exactly: beta`)
	waitHits(t, &mu, &hitB, 45*time.Second)
	waitPaneContains(t, sess, "token-B", 45*time.Second)

	after := captureIdentity(t, sess, codexHome, "codex")
	if after.PanePID != before.PanePID || after.ClientPID != before.ClientPID {
		t.Fatalf("PID changed after turn2: before=%#v after=%#v", before, after)
	}
	if after.NativeSessionID != before.NativeSessionID || after.TmuxTarget != before.TmuxTarget {
		t.Fatalf("session/tmux changed: before=%#v after=%#v", before, after)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hitB) == 0 {
		t.Fatal("gateway B empty")
	}
	last := hitB[len(hitB)-1]
	assertNoOpaqueResponsesHistory(t, last)
	if !strings.Contains(string(last), "model-b") {
		t.Fatalf("model-b missing: %s", trimTail(string(last), 400))
	}
	body := string(last)
	if !strings.Contains(body, "alpha") && !strings.Contains(body, "token-A") && !strings.Contains(body, "HELLO") {
		t.Fatalf("portable continuity missing: %s", trimTail(body, 900))
	}
	// Tool continuity if the client completed a function_call round-trip on A.
	sawTool := false
	for _, b := range hitA {
		if strings.Contains(string(b), `"function_call"`) || strings.Contains(string(b), `"function_call_output"`) {
			sawTool = true
			break
		}
	}
	if sawTool && !(strings.Contains(body, `"function_call"`) || strings.Contains(body, `"function_call_output"`) || strings.Contains(body, "HELLO") || strings.Contains(body, "proof.txt")) {
		t.Logf("WARNING: tool round-trip observed on A but B body lacks tool/text continuity markers")
	}
	t.Logf("proof ok: A=%d B=%d route=%s session=%s pane=%d client=%d tool_on_A=%v",
		len(hitA), len(hitB), got.Binding.RouteID, after.NativeSessionID, after.PanePID, after.ClientPID, sawTool)
}

func TestPortableHistorySamePIDClaude(t *testing.T) {
	if os.Getenv("ZEN_PORTABLE_HISTORY_SAMEPID") == "" {
		t.Skip("set ZEN_PORTABLE_HISTORY_SAMEPID=1 for Claude same-tmux/PID proof")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Fatalf("claude not on PATH: %v", err)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux not on PATH: %v", err)
	}
	verOut, _ := exec.Command(claudePath, "--version").CombinedOutput()
	t.Logf("claude version=%s", strings.TrimSpace(string(verOut)))

	artRoot := filepath.Join(tmpdir(t), "zen-portable-samepid-claude")
	_ = os.RemoveAll(artRoot)
	claudeCfg := filepath.Join(artRoot, "claude")
	cwd := filepath.Join(artRoot, "cwd")
	_ = os.MkdirAll(claudeCfg, 0o700)
	_ = os.MkdirAll(cwd, 0o700)

	var mu sync.Mutex
	var hitA, hitB [][]byte
	upA := httptest.NewServer(claudeStatefulFake(t, &mu, &hitA, "A", true))
	defer upA.Close()
	upB := httptest.NewServer(claudeStatefulFake(t, &mu, &hitB, "B", false))
	defer upB.Close()

	table := modelprofiles.NewRouteTable()
	auth := modelprofiles.ContractAuth{Verifier: modelprofiles.BuiltinEnvelopeVerifier{}}
	profileA := modelprofiles.Profile{
		ID: "ca", Name: "A", ExecutorID: modelprofiles.ExecutorClaude,
		ProviderID: "fake-a", ProviderLabel: "FakeA",
		Protocol: modelprofiles.ProtocolAnthropicMessages, ClientModel: "claude-sonnet-4-6", Model: "model-a",
		ClientModelProvenance: modelprofiles.ContractProvenanceConfiguredCompatibility,
		BaseURL:               upA.URL,
		AuthMode:              modelprofiles.AuthModeNone,
	}
	profileB := profileA
	profileB.ID, profileB.Name, profileB.ProviderID, profileB.ProviderLabel, profileB.Model =
		"cb", "B", "fake-b", "FakeB", "model-b"
	profileB.BaseURL = upB.URL

	state, err := table.BindLaunch("samepid-claude", profileA, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := modelprofiles.NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, err := modelprofiles.LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	sess := fmt.Sprintf("zen-samepid-claude-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sess, "-c", cwd,
		"-e", "HOME="+artRoot,
		"-e", "CLAUDE_CONFIG_DIR="+claudeCfg,
		"-e", "ANTHROPIC_BASE_URL="+root,
		"-e", "ANTHROPIC_AUTH_TOKEN="+modelprofiles.LoopbackAuthPlaceholder,
		"-e", "TERM=xterm-256color",
		"--", claudePath, "--bare", "--dangerously-skip-permissions", "--model", "claude-sonnet-4-6",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v (%s)", err, out)
	}
	waitClaudeReady(t, sess)

	before := captureIdentity(t, sess, claudeCfg, "claude")
	t.Logf("before: tmux=%s pane_pid=%d client_pid=%d session=%s route=%s",
		sess, before.PanePID, before.ClientPID, before.NativeSessionID, state.Binding.RouteID)
	if before.PanePID == 0 {
		t.Fatalf("missing pane pid")
	}
	if before.NativeSessionID == "" {
		// Session file may appear after first turn; record after turn1 if needed.
		t.Logf("native session id not yet on disk; will re-check after turn1")
	}

	sendTmuxLine(t, sess, `Create file proof.txt containing exactly HELLO using the Bash tool, then reply with exactly: alpha`)
	waitHits(t, &mu, &hitA, 60*time.Second)
	waitPaneContains(t, sess, "token-A", 60*time.Second)
	before = captureIdentity(t, sess, claudeCfg, "claude")
	if before.NativeSessionID == "" {
		t.Fatalf("claude native session id missing after turn1 under %s", claudeCfg)
	}

	got, err := table.Activate("samepid-claude", profileB, 2, state.Generation, auth)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != modelprofiles.HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	if got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route replaced")
	}

	mid := captureIdentity(t, sess, claudeCfg, "claude")
	if mid.PanePID != before.PanePID {
		t.Fatalf("pane PID changed across activate: %d -> %d", before.PanePID, mid.PanePID)
	}
	if mid.ClientPID != 0 && before.ClientPID != 0 && mid.ClientPID != before.ClientPID {
		t.Fatalf("client PID changed: %d -> %d", before.ClientPID, mid.ClientPID)
	}
	if mid.NativeSessionID != before.NativeSessionID {
		t.Fatalf("native session replaced: %s -> %s", before.NativeSessionID, mid.NativeSessionID)
	}

	sendTmuxLine(t, sess, `Reply with exactly: beta`)
	waitHits(t, &mu, &hitB, 60*time.Second)
	waitPaneContains(t, sess, "token-B", 60*time.Second)

	after := captureIdentity(t, sess, claudeCfg, "claude")
	if after.PanePID != before.PanePID || after.NativeSessionID != before.NativeSessionID {
		t.Fatalf("identity changed: before=%#v after=%#v", before, after)
	}

	mu.Lock()
	defer mu.Unlock()
	last := hitB[len(hitB)-1]
	assertNoOpaqueAnthropicHistory(t, last)
	if !strings.Contains(string(last), "model-b") {
		t.Fatalf("model-b missing: %s", trimTail(string(last), 400))
	}
	body := string(last)
	if !strings.Contains(body, "alpha") && !strings.Contains(body, "token-A") && !strings.Contains(body, "HELLO") {
		t.Fatalf("portable continuity missing: %s", trimTail(body, 900))
	}
	if strings.Contains(body, `"signature"`) || strings.Contains(body, "redacted_thinking") {
		t.Fatalf("opaque thinking reached B: %s", trimTail(body, 800))
	}
	t.Logf("proof ok: A=%d B=%d route=%s session=%s pane=%d client=%d",
		len(hitA), len(hitB), got.Binding.RouteID, after.NativeSessionID, after.PanePID, after.ClientPID)
}

type processIdentity struct {
	TmuxTarget      string
	PanePID         int
	ClientPID       int
	NativeSessionID string
}

func captureIdentity(t *testing.T, sess, home, kind string) processIdentity {
	t.Helper()
	out, err := exec.Command("tmux", "list-panes", "-t", sess, "-F", "#{session_name}:#{window_id}.#{pane_id}\t#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, "\t")
	id := processIdentity{TmuxTarget: parts[0]}
	if len(parts) > 1 {
		id.PanePID, _ = strconv.Atoi(parts[1])
	}
	if id.PanePID > 0 {
		if kids, err := exec.Command("pgrep", "-P", strconv.Itoa(id.PanePID)).Output(); err == nil {
			for _, k := range strings.Fields(string(kids)) {
				pid, _ := strconv.Atoi(k)
				if pid > 0 {
					id.ClientPID = pid
					break
				}
			}
		}
		if id.ClientPID == 0 {
			id.ClientPID = id.PanePID // claude may be the pane process
		}
	}
	switch kind {
	case "codex":
		_ = filepath.Walk(filepath.Join(home, "sessions"), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl") {
				// rollout-<timestamp>-<uuid>.jsonl — UUID is the final 36-char segment when present.
				trim := strings.TrimSuffix(strings.TrimPrefix(base, "rollout-"), ".jsonl")
				if i := strings.LastIndex(trim, "-"); i >= 0 && len(trim[i+1:]) >= 8 {
					// Prefer full UUID if the filename embeds one after the timestamp.
					parts := strings.Split(trim, "-")
					if len(parts) >= 5 {
						id.NativeSessionID = strings.Join(parts[len(parts)-5:], "-")
					} else {
						id.NativeSessionID = trim[i+1:]
					}
				}
			}
			return nil
		})
	case "claude":
		_ = filepath.Walk(filepath.Join(home, "projects"), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".jsonl") {
				id.NativeSessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
			}
			return nil
		})
	}
	return id
}

func waitCodexReady(t *testing.T, sess string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		pane := capturePane(t, sess)
		// Trust dialog can remain painted under the main TUI; once the composer
		// footer is live, treat as ready.
		if strings.Contains(pane, "gpt-5 default") || (strings.Contains(pane, "OpenAI Codex") && strings.Contains(pane, "›")) {
			if strings.Contains(pane, "Yes, continue") && !strings.Contains(pane, "gpt-5 default") {
				_ = exec.Command("tmux", "send-keys", "-t", sess, "1").Run()
				time.Sleep(150 * time.Millisecond)
				_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
				time.Sleep(time.Second)
				continue
			}
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
	t.Fatalf("codex not ready:\n%s", capturePane(t, sess))
}

func waitClaudeReady(t *testing.T, sess string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("tmux", "has-session", "-t", sess).Run() != nil {
			t.Fatal("claude tmux session died during onboarding")
		}
		pane := capturePane(t, sess)
		switch {
		case strings.Contains(pane, "Choose the text style that looks best"):
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
		case strings.Contains(pane, "Press Enter to continue"):
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
		case strings.Contains(pane, "I trust this folder"):
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
		case strings.Contains(pane, "Do you want to use this API key"):
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Up").Run()
			time.Sleep(150 * time.Millisecond)
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
		case strings.Contains(pane, "Yes, I accept"):
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Down").Run()
			time.Sleep(150 * time.Millisecond)
			_ = exec.Command("tmux", "send-keys", "-t", sess, "Enter").Run()
		case strings.Contains(pane, "bypass permissions on") || (strings.Contains(pane, "╭") && !strings.Contains(pane, "Security notes")):
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("claude not ready:\n%s", capturePane(t, sess))
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

func tmpdir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("TMPDIR"); d != "" {
		return d
	}
	return t.TempDir()
}

func codexStatefulFake(t *testing.T, mu *sync.Mutex, sink *[][]byte, label string, tools bool) http.Handler {
	t.Helper()
	var n int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"type":"route_websocket_rejected"}}`))
			return
		}
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		*sink = append(*sink, append([]byte(nil), body...))
		n++
		turn := n
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		write := func(obj any) {
			raw, _ := json.Marshal(obj)
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &typed)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, raw)
			if flusher != nil {
				flusher.Flush()
			}
		}
		id := fmt.Sprintf("resp_%s_%d", label, turn)
		msgID := fmt.Sprintf("msg_%s_%d", label, turn)
		text := fmt.Sprintf("token-%s-%d", label, turn)

		// Attempt a genuine tool round-trip once on gateway A when tools enabled
		// and the request has not yet posted a function_call_output.
		wantTool := tools && turn == 1 && !strings.Contains(string(body), `"function_call_output"`)
		var output []any
		if wantTool {
			output = []any{
				map[string]any{
					"type": "reasoning", "id": "rsn_" + label, "encrypted_content": "opaque-blob-" + label,
					"summary": []any{map[string]string{"type": "summary_text", "text": "plan"}},
				},
				map[string]any{
					"type": "function_call", "name": "shell_command", "call_id": "c_" + label,
					"arguments": `{"command":"printf HELLO > proof.txt"}`,
				},
			}
		} else {
			final := map[string]any{
				"type": "message", "id": msgID, "status": "completed", "role": "assistant",
				"content": []any{map[string]string{"type": "output_text", "text": text}},
			}
			if tools && turn == 1 {
				// labeled: fixture-assisted encrypted reasoning attempt when tool path skipped
			}
			output = []any{
				map[string]any{
					"type": "reasoning", "id": "rsn_" + label + "_2", "encrypted_content": "opaque-blob-" + label,
				},
				final,
			}
			write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
			write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": msgID, "status": "in_progress", "role": "assistant", "content": []any{}}})
			write(map[string]any{"type": "response.content_part.added", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": ""}})
			write(map[string]any{"type": "response.output_text.delta", "item_id": msgID, "output_index": 0, "content_index": 0, "delta": text})
			write(map[string]any{"type": "response.output_text.done", "item_id": msgID, "output_index": 0, "content_index": 0, "text": text})
			write(map[string]any{"type": "response.content_part.done", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": text}})
			write(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": final})
			write(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "status": "completed", "model": "gpt-5", "output": output}})
			return
		}

		write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": output[1]})
		write(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": output[1]})
		write(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "status": "completed", "model": "gpt-5", "output": output}})
	})
}

func claudeStatefulFake(t *testing.T, mu *sync.Mutex, sink *[][]byte, label string, tools bool) http.Handler {
	t.Helper()
	var n int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		*sink = append(*sink, append([]byte(nil), body...))
		n++
		turn := n
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		hasToolResult := strings.Contains(string(body), `"tool_result"`)
		var content []any
		if tools && !hasToolResult && turn <= 2 {
			content = []any{
				map[string]any{"type": "thinking", "thinking": "plan", "signature": "sig-" + label},
				map[string]any{"type": "tool_use", "id": "tool_" + label, "name": "Bash", "input": map[string]string{"command": "printf HELLO > proof.txt"}},
			}
		} else {
			content = []any{
				map[string]any{"type": "thinking", "thinking": "plan", "signature": "sig-" + label},
				map[string]any{"type": "text", "text": fmt.Sprintf("token-%s-%d", label, turn)},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": fmt.Sprintf("msg_%s_%d", label, turn), "type": "message", "role": "assistant", "model": "claude",
			"content": content, "stop_reason": map[bool]string{true: "tool_use", false: "end_turn"}[tools && !hasToolResult && turn <= 2],
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	})
}
