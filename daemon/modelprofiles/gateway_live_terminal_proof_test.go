package modelprofiles_test

// Opt-in live proof: an ORDINARY Terminal Codex (started directly, not via
// `zen agent spawn`) routes through the machine-level gateway, and a Settings
// Provider switch retargets the same long-lived CLI process to the new
// upstream.
//
// Run (from daemon module root, with the release daemon NOT owning 127.0.0.1:38777
// and tmux + codex + the daemon binary available):
//
//	ZEN_PROOF_ISOLATED_TERMINAL=1 ZEN_DAEMON_BIN=tmp/zen-dev \
//	  go test ./modelprofiles -run TestIsolatedDirectTerminalGatewayProof -count=1 -timeout 240s
//
// Everything runs inside one isolated scratch root: a temporary HOME (sandbox
// Zen state), a temporary CODEX_HOME (projected by the real takeover Enable),
// a dedicated tmux server socket, two scripted 127.0.0.1 upstream providers
// (A then B), and a sandbox daemon with -addr 127.0.0.1:0.
//
// The proof: takeover enabled → the projected native config points plain
// `codex` at the gateway → first prompt reaches upstream A through Zen with
// the exact client model bytes → provider_set_default(B) flips the gateway's
// upstream → the SAME pane/PID sends the second prompt → it reaches upstream
// B, and the rendered replies (A then B) prove the same process/thread
// continued across the switch.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/modelprofiles"
)

func gatewayPortFree() bool {
	ln, err := net.Listen("tcp", "127.0.0.1:38777")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// proofUpstream records every POST /v1/responses body and answers with an SSE
// completion whose text marks which upstream served the turn.
type proofUpstream struct {
	mu       sync.Mutex
	bodyChan chan []byte
	marker   string
	server   *httptest.Server
}

func newProofUpstream(t *testing.T, marker string) *proofUpstream {
	t.Helper()
	up := &proofUpstream{bodyChan: make(chan []byte, 8), marker: marker}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("upstream %s hit: %s %s (upgrade=%v)", marker, r.Method, r.URL.Path, r.Header.Get("Upgrade"))
		switch {
		case strings.HasSuffix(r.URL.Path, "/responses") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			select {
			case up.bodyChan <- body:
			default:
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			events := []string{
				`data: {"type":"response.created","response":{"id":"resp-proof"}}`,
				`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","id":"m1","content":[{"type":"output_text","text":"` + up.marker + `","annotations":[]}]}}`,
				`data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"` + up.marker + `"}`,
				`data: {"type":"response.output_text.done","item_id":"m1","output_index":0,"content_index":0,"text":"` + up.marker + `"}`,
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","id":"m1","content":[{"type":"output_text","text":"` + up.marker + `","annotations":[]}]}}`,
				`data: {"type":"response.completed","response":{"id":"resp-proof","status":"completed","usage":{"input_tokens":1,"input_tokens_details":null,"output_tokens":1,"output_tokens_details":null,"total_tokens":2}}}`,
				"data: [DONE]",
			}
			for _, ev := range events {
				_, _ = io.WriteString(w, ev+"\n\n")
				if flusher != nil {
					flusher.Flush()
				}
			}
		case strings.HasSuffix(r.URL.Path, "/models"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"models":[{"id":"gpt-5.6-sol","display_name":"gpt-5.6-sol"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.server.Close)
	return up
}

func waitBody(t *testing.T, up *proofUpstream, want string) []byte {
	t.Helper()
	select {
	case body := <-up.bodyChan:
		t.Logf("upstream %s received body (%d bytes)", up.marker, len(body))
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("upstream %s request missing %q: %s", up.marker, want, body)
		}
		// Exact model bytes preserved end-to-end (the machine-level contract).
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("upstream %s body not JSON: %v", up.marker, err)
		}
		if doc["model"] != "gpt-5.6-sol" {
			t.Fatalf("upstream %s model = %v, want gpt-5.6-sol", up.marker, doc["model"])
		}
		return body
	case <-time.After(60 * time.Second):
		up.mu.Lock()
		frames := len(up.bodyChan)
		up.mu.Unlock()
		t.Fatalf("timed out waiting for upstream %s (buffered %d)", up.marker, frames)
		return nil
	}
}

// dumpProofState writes the isolated pane + daemon log for failure diagnosis.
func dumpProofState(t *testing.T, tmuxSocket, logPath string) {
	t.Helper()
	if out, err := exec.Command("tmux", "-S", tmuxSocket, "capture-pane", "-t", "zenproof", "-p").Output(); err == nil {
		t.Logf("pane capture:\n%s", trimTo(out, 4000))
	} else {
		t.Logf("pane capture failed: %v", err)
	}
	if raw, err := os.ReadFile(logPath); err == nil {
		t.Logf("daemon log tail:\n%s", trimTo(raw, 4000))
	}
}

func controlCall(t *testing.T, socketPath string, req control.Request) control.Response {
	t.Helper()
	resp, err := control.Call(socketPath, req)
	if err != nil {
		t.Fatalf("control call %s: %v", req.Type, err)
	}
	if !resp.OK {
		msg := ""
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		t.Fatalf("control call %s failed: %s", req.Type, msg)
	}
	return resp
}

func waitControl(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := control.Call(socketPath, control.Request{Type: "codex_gateway_status"}); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("sandbox control socket %s did not come up", socketPath)
}

func TestIsolatedDirectTerminalGatewayProof(t *testing.T) {
	if os.Getenv("ZEN_PROOF_ISOLATED_TERMINAL") == "" {
		t.Skip("set ZEN_PROOF_ISOLATED_TERMINAL=1 to run the isolated direct-terminal proof")
	}
	if !gatewayPortFree() {
		t.Skip("127.0.0.1:38777 is already owned (release daemon running); stop it for the isolated proof")
	}
	daemonBin := strings.TrimSpace(os.Getenv("ZEN_DAEMON_BIN"))
	if daemonBin == "" {
		daemonBin = filepath.Join("..", "..", "tmp", "zen-dev")
	}
	absBin, err := filepath.Abs(daemonBin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absBin); err != nil {
		t.Fatalf("daemon binary %s missing: %v", absBin, err)
	}
	for _, tool := range []string{"tmux", "codex"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}

	sbx := t.TempDir()
	daemonLogPath := filepath.Join(sbx, "daemon.log")
	tmuxSocket := filepath.Join(sbx, "tmux.sock")
	dumped := false
	// kill the isolated tmux server last (cleanups run LIFO, so register it
	// first and let the failure dump run before it)
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", tmuxSocket, "kill-server").Run() })
	t.Cleanup(func() {
		if t.Failed() && !dumped {
			dumped = true
			dumpProofState(t, tmuxSocket, daemonLogPath)
		}
	})
	zenHome := filepath.Join(sbx, "home")
	codexHome := filepath.Join(sbx, "codex-home")
	for _, dir := range []string{zenHome, codexHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh HOME triggers the zsh-newuser-install wizard in the pane shell
	// and blocks the direct codex launch; seed minimal startup files.
	for _, rc := range []string{".zshenv", ".zshrc", ".zprofile"} {
		if err := os.WriteFile(filepath.Join(zenHome, rc), []byte("# zen isolated proof shell\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	upA := newProofUpstream(t, "upstream-A-served")
	upB := newProofUpstream(t, "upstream-B-served")

	// Seed the sandbox Provider catalog: profile A default for codex, profile
	// B switchable at runtime. Mirror the durable Store file shape.
	profiles := "revision = 7\n\n" +
		"[[profiles]]\n" +
		"  id = \"conn-proof-a\"\n  name = \"proof A\"\n  scope = \"account\"\n  client = \"codex\"\n" +
		"  provider_id = \"custom\"\n  provider_label = \"Custom Gateway\"\n" +
		"  base_url = \"" + upA.server.URL + "\"\n  auth_mode = \"none\"\n  credential_env = \"ZEN_PROVIDER_API_KEY\"\n\n" +
		"[[profiles]]\n" +
		"  id = \"conn-proof-b\"\n  name = \"proof B\"\n  scope = \"account\"\n  client = \"codex\"\n" +
		"  provider_id = \"custom\"\n  provider_label = \"Custom Gateway\"\n" +
		"  base_url = \"" + upB.server.URL + "\"\n  auth_mode = \"none\"\n  credential_env = \"ZEN_PROVIDER_API_KEY\"\n\n" +
		"[defaults]\n  codex = \"conn-proof-a\"\n\n" +
		"[default_models]\n  codex = \"gpt-5.6-sol\"\n"
	zenDir := filepath.Join(zenHome, ".zen")
	if err := os.MkdirAll(zenDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zenDir, "model-profiles.toml"), []byte(profiles), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pre-existing (unrelated) Codex config: model/effort the takeover must
	// preserve, plus trust for the scratch cwd so the TUI never prompts.
	codexConfig := "model = \"gpt-5.6-sol\"\nmodel_reasoning_effort = \"medium\"\n" +
		"[projects.\"" + sbx + "\"]\n  trust_level = \"trusted\"\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(codexConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	// Dedicated tmux server so the sandbox daemon's watcher never sees the
	// user's real sessions.
	newTmux := exec.Command("tmux", "-S", tmuxSocket, "new-session", "-d", "-s", "zenproof", "-x", "200", "-y", "50")
	newTmux.Env = proofEnv(zenHome, codexHome, sbx, tmuxSocket)
	if out, err := newTmux.CombinedOutput(); err != nil {
		t.Fatalf("create isolated tmux server: %v: %s", err, out)
	}
	// Sandbox daemon.
	daemonCmd := exec.Command(absBin, "-addr", "127.0.0.1:0", "-state-dir", zenDir)
	daemonCmd.Env = proofEnv(zenHome, codexHome, sbx, tmuxSocket)
	daemonLog, err := os.Create(daemonLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer daemonLog.Close()
	daemonCmd.Stdout = daemonLog
	daemonCmd.Stderr = daemonLog
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start sandbox daemon: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = daemonCmd.Process.Kill()
		}
		_, _ = daemonCmd.Process.Wait()
	})

	socketPath, err := control.DefaultSocketPath(zenDir)
	if err != nil {
		t.Fatal(err)
	}
	waitControl(t, socketPath, 30*time.Second)

	// Truthful status before/after enable.
	statusResp := controlCall(t, socketPath, control.Request{Type: "codex_gateway_status"})
	if statusResp.Gateway == nil || statusResp.Gateway.State != modelprofiles.TakeoverStateInactive {
		state := ""
		if statusResp.Gateway != nil {
			state = statusResp.Gateway.State
		}
		t.Fatalf("pre-enable gateway state = %q, want inactive", state)
	}
	controlCall(t, socketPath, control.Request{Type: "codex_gateway_enable"})
	statusResp = controlCall(t, socketPath, control.Request{Type: "codex_gateway_status"})
	if statusResp.Gateway == nil || statusResp.Gateway.State != modelprofiles.TakeoverStateActive {
		t.Fatalf("post-enable gateway = %+v", statusResp.Gateway)
	}
	projected, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(projected, []byte(modelprofiles.GatewayProviderName)) || !bytes.Contains(projected, []byte(modelprofiles.DefaultGatewayListenAddr)) {
		t.Fatalf("projection missing from %s: %s", codexHome, projected)
	}
	if !bytes.Contains(projected, []byte("gpt-5.6-sol")) {
		t.Fatalf("user model was clobbered by the projection: %s", projected)
	}

	// Direct Terminal Codex (NOT a zen launch): plain `codex` TUI in the pane.
	pane := exec.Command("tmux", "-S", tmuxSocket, "send-keys", "-t", "zenproof",
		"cd "+sbx+" && "+absCodexTUI()+" --dangerously-bypass-approvals-and-sandbox -C "+sbx, "Enter")
	pane.Env = proofEnv(zenHome, codexHome, sbx, tmuxSocket)
	if out, err := pane.CombinedOutput(); err != nil {
		t.Fatalf("launch direct codex: %v: %s", err, out)
	}
	// Answer a first-run trust prompt if it appears (option 1 = yes).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		captured, _ := exec.Command("tmux", "-S", tmuxSocket, "capture-pane", "-t", "zenproof", "-p").Output()
		if bytes.Contains(captured, []byte("Do you trust")) {
			_, _ = exec.Command("tmux", "-S", tmuxSocket, "send-keys", "-t", "zenproof", "1", "Enter").Output()
			break
		}
		if bytes.Contains(captured, []byte("model:")) || bytes.Contains(captured, []byte("model :")) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	panePID := tmuxPanePID(t, tmuxSocket, "zenproof")
	if panePID <= 0 {
		t.Fatal("direct codex pane has no pid")
	}

	// Prompt 1 -> upstream A through Zen. The TUI input pipeline is
	// event-driven; resend Enter until the upstream observes the turn (bounded
	// retry, never duplicates the prompt text).
	submitTurn := func(text string, up *proofUpstream, want string) []byte {
		t.Helper()
		_, _ = exec.Command("tmux", "-S", tmuxSocket, "send-keys", "-t", "zenproof", text, "Enter").Output()
		for attempt := 0; attempt < 5; attempt++ {
			select {
			case body := <-up.bodyChan:
				if !bytes.Contains(body, []byte(want)) {
					t.Fatalf("upstream %s request missing %q: %s", up.marker, want, body)
				}
				return body
			case <-time.After(3 * time.Second):
			}
			_, _ = exec.Command("tmux", "-S", tmuxSocket, "send-keys", "-t", "zenproof", "Enter").Output()
		}
		t.Fatalf("upstream %s never observed the turn %q", up.marker, want)
		return nil
	}
	bodyA := submitTurn("reply with the single word alpha", upA, `"reply with the single word alpha"`)
	if bytes.Contains(bodyA, []byte(modelprofiles.LoopbackAuthPlaceholder)) {
		t.Fatalf("placeholder leaked upstream: %s", bodyA)
	}

	// Settings Provider switch: default codex provider A -> B.
	projResp := controlCall(t, socketPath, control.Request{Type: "provider_list"})
	if projResp.Providers == nil {
		t.Fatal("provider_list returned no projection")
	}
	controlCall(t, socketPath, control.Request{
		Type:       "provider_set_default",
		ExecutorID: modelprofiles.ExecutorCodex,
		ProfileID:  "conn-proof-b",
		ModelID:    "gpt-5.6-sol",
		Revision:   projResp.Providers.Revision,
	})
	statusResp = controlCall(t, socketPath, control.Request{Type: "codex_gateway_status"})
	if statusResp.Gateway == nil || statusResp.Gateway.UpstreamProfileID != "conn-proof-b" {
		t.Fatalf("gateway upstream after switch = %+v", statusResp.Gateway)
	}

	// Prompt 2 in the SAME pane/process -> upstream B.
	bodyB := submitTurn("reply with the single word beta", upB, `"reply with the single word beta"`)
	if bytes.Contains(bodyB, []byte(modelprofiles.LoopbackAuthPlaceholder)) {
		t.Fatalf("placeholder leaked upstream: %s", bodyB)
	}
	if panePID2 := tmuxPanePID(t, tmuxSocket, "zenproof"); panePID2 != panePID {
		t.Fatalf("pane pid changed across the switch: %d -> %d (process must be the same)", panePID, panePID2)
	}

	// The same process rendered both replies (A then B) in its thread. The
	// TUI paints asynchronously after the upstream turn completes, so poll
	// the pane until both markers render (bounded; a stale single-shot
	// capture raced the redraw).
	markers := []string{"upstream-A-served", "upstream-B-served"}
	captured := []byte(nil)
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		captured, _ = exec.Command("tmux", "-S", tmuxSocket, "capture-pane", "-t", "zenproof", "-p").Output()
		all := true
		for _, marker := range markers {
			if !bytes.Contains(captured, []byte(marker)) {
				all = false
				break
			}
		}
		if all {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	for _, marker := range markers {
		if !bytes.Contains(captured, []byte(marker)) {
			t.Fatalf("pane did not render %s: %s", marker, trimTo(captured, 1200))
		}
	}

	// Disable: removes only the Zen-owned projection, keeps the user model.
	controlCall(t, socketPath, control.Request{Type: "codex_gateway_disable"})
	statusResp = controlCall(t, socketPath, control.Request{Type: "codex_gateway_status"})
	if statusResp.Gateway == nil || statusResp.Gateway.State != modelprofiles.TakeoverStateInactive {
		t.Fatalf("post-disable gateway = %+v", statusResp.Gateway)
	}
	after, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if !bytes.Contains(after, []byte("gpt-5.6-sol")) || bytes.Contains(after, []byte(modelprofiles.GatewayProviderName)) {
		t.Fatalf("disable left a broken config: %s", after)
	}

	_ = exec.Command("tmux", "-S", tmuxSocket, "kill-session", "-t", "zenproof").Run()
	stopped = true
	_ = daemonCmd.Process.Signal(os.Interrupt)
}

func trimTo(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func tmuxPanePID(t *testing.T, socket, target string) int {
	t.Helper()
	for i := 0; i < 40; i++ {
		out, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "-t", target, "#{pane_pid}").Output()
		if err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(out))); perr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0
}

func absCodexTUI() string {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "codex"
	}
	return path
}

func proofEnv(zenHome, codexHome, scratch, tmuxSocket string) []string {
	out := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		switch key {
		case "HOME", "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
			"ANTHROPIC_BASE_URL", "TMUX", "ZEN_STATE_DIR", "BUN_INSTALL", "GOROOT", "GOPATH":
			continue
		}
		out = append(out, e)
	}
	return append(out,
		"HOME="+zenHome,
		"CODEX_HOME="+codexHome,
		"TMUX="+tmuxSocket,
		"ZEN_STATE_DIR="+filepath.Join(zenHome, ".zen"),
		fmt.Sprintf("ZEN_PROGRESS_ENV=isolated-proof"),
	)
}
