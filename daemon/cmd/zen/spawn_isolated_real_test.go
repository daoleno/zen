package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// This opt-in test owns its stores, sockets, HOME, and provider transcript.
// It runs production CLI/control/admission/input code without daemon startup's
// scheduler, host bootstrap, gateway takeover, or user-state discovery.
func TestRealCLIIsolatedCodexSpawn(t *testing.T) {
	if os.Getenv("ZEN_TEST_FULL_CODEX_SPAWN") != "1" {
		t.Skip("set ZEN_TEST_FULL_CODEX_SPAWN=1 for one real short Codex spawn")
	}
	if runtime.GOOS != "linux" {
		t.Skip("process lifetime evidence uses Linux procfs")
	}
	cli := os.Getenv("ZEN_TEST_SPAWN_CLI")
	if !filepath.IsAbs(cli) {
		t.Fatal("ZEN_TEST_SPAWN_CLI must name a separately built absolute zen binary")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "zs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	write := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	oldHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	codexSource := os.Getenv("CODEX_HOME")
	if codexSource == "" {
		codexSource = filepath.Join(oldHome, ".codex")
	}
	for _, name := range []string{"config.toml", "auth.json"} {
		data, readErr := os.ReadFile(filepath.Join(codexSource, name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		write(filepath.Join(root, "home", ".codex", name), data, 0600)
	}
	// Suppress the new-user wizard without loading the user's shell startup.
	write(filepath.Join(root, "home", ".zshrc"), nil, 0600)
	for _, key := range []string{"ZEN_AGENT_ID", "ZEN_AGENT_PROGRESS_CMD", "ZEN_WORKER_ID", "ZEN_WORKER_PROGRESS_CMD", "ZEN_STATE_DIR", "ZEN_DELEGATED_EXECUTOR", "ZEN_BRAIN_HOST_EXECUTOR", "TMUX", "TMUX_PANE"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("ZDOTDIR", filepath.Join(root, "home"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "home", ".codex"))
	t.Setenv("ZEN_WORKTREE_ROOT", filepath.Join(root, "worktrees"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	socket := filepath.Join(root, "tmux.sock")
	audit := filepath.Join(root, "tmux-audit")
	// The shim preserves the single tmux queue and its exit status, and never
	// records load-buffer stdin (the prompt) or set-option receipt payloads.
	shim := fmt.Sprintf(`#!/bin/sh
set -u
if [ "$#" -gt 0 ] && [ "$1" != -S ]; then
  case "$1" in -L*|-S*) exit 97 ;; esac
  set -- -S '%s' "$@"
fi
if [ "$#" -lt 3 ] || [ "$1" != -S ] || [ "$2" != '%s' ]; then
  echo 'isolated tmux firewall rejected socket' >&2
  exit 97
fi
err='%s/err.'$$
if [ "$3" = kill-window ]; then
  '%s' -S '%s' capture-pane -p -S - -t "$5" >'%s/failed-pane' 2>&1
  '%s' -S '%s' list-panes -a -F '#{pane_id} #{pane_pid} #{pane_current_command} #{pane_dead}' >'%s/failed-identity' 2>&1
fi
started=$(date +%%s%%N)
stages=
next=0
for arg in "$@"; do
  if [ "$next" = 1 ]; then stages="$stages,$arg"; next=0; fi
  if [ "$arg" = ';' ]; then next=1; fi
done
'%s' "$@" 2>"$err"
rc=$?
printf '%%s\t%%s\t%%s\t%%s\t%%s\n' "$3" "$rc" "$started" "$3$stages" "$(cat "$err")" >>'%s'
cat "$err" >&2
rm -f "$err"
exit "$rc"
`, socket, socket, root, realTmux, socket, root, realTmux, socket, root, realTmux, audit)
	write(filepath.Join(root, "bin", "tmux"), []byte(shim), 0700)
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 140*time.Second)
	t.Cleanup(cancel)
	runTmux := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, realTmux, append([]string{"-S", socket}, args...)...).CombinedOutput()
	}
	// A seed keeps cleanup/query semantics stable even if the target exits.
	if out, err := runTmux("-f", "/dev/null", "new-session", "-d", "-s", "test-seed", "-x", "160", "-y", "60"); err != nil {
		t.Fatalf("private tmux start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		out, killErr := exec.Command(realTmux, "-S", socket, "kill-server").CombinedOutput()
		if killErr != nil {
			t.Errorf("private tmux cleanup: %v: %s", killErr, out)
		}
		if exec.Command(realTmux, "-S", socket, "list-sessions").Run() == nil {
			t.Error("private tmux still running")
		}
	})
	w := watcher.New(500 * time.Millisecond)
	w.SetTmuxServer(socket, filepath.Join(root, "tmux-scratch"))
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	w.SetProviderActivityProbe(newWorkProviderActivityProbe())
	store, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}

	execs := work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	})
	service := brain.NewService(store, w, execs)
	w.SetTurnLedger(service)
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	app := &controlApp{watcher: &isolatedSpawnWatcher{Watcher: w, executable: cli}, brainStore: store, brainService: service, execs: execs, profiles: owner, stateDir: filepath.Join(root, "control")}
	controlPath, err := control.DefaultSocketPath(app.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	server := &control.Server{Path: controlPath, Handler: app}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil {
			t.Errorf("control cleanup: %v", err)
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("unix", controlPath, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private control socket not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	prompt := "This is an isolated transport test. Do not use tools, run commands (including progress), edit files, or delegate. Reply only with the concatenation of ZEN_FULL_SPAWN_ and VERIFIED."
	cmd := exec.CommandContext(ctx, cli, "worker", "spawn", "-state-dir", app.stateDir, "-executor", "codex", "-name", "isolated-cli-proof", "-cwd", cwd, "-prompt", prompt)
	var cliStderr bytes.Buffer
	cmd.Stderr = &cliStderr
	output, cliErr := cmd.Output()
	var resp control.Response
	if err := json.Unmarshal(output, &resp); err != nil || cliErr != nil || !resp.OK || resp.Worker == nil || resp.BrainWork == nil {
		data, _ := os.ReadFile(audit)
		pane, _ := os.ReadFile(filepath.Join(root, "failed-pane"))
		identity, _ := os.ReadFile(filepath.Join(root, "failed-identity"))
		if len(data) > 1500 {
			data = data[len(data)-1500:]
		}
		t.Fatalf("CLI spawn failed: %v decode=%v response=%s stderr=%s; identity=%s pane=%s tmux audit tail=%s", cliErr, err, output, cliStderr.String(), identity, pane, data)
	}
	id := resp.Worker.ID
	closed := false
	t.Logf("CLI exit=0 stderr=%q work=%s session=%s command=%q confirmation=%q", cliStderr.String(), resp.BrainWork.ID, id, resp.Worker.Command, resp.Confirmation)
	if resp.Confirmation != "" || !resp.Worker.Delegated || resp.Worker.Hidden {
		t.Fatalf("not an accepted visible delegated spawn: %+v", resp.Worker)
	}
	t.Cleanup(func() {
		if closed {
			return
		}
		if err := w.KillSession(id); err != nil {
			t.Errorf("target cleanup: %v", err)
		}
	})
	owned, err := w.ResolveOwnedGeneration(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("owned identity=%+v", owned)
	pane, err := runTmux("display-message", "-p", "-t", id, "#{pane_id} #{pane_pid} #{pane_current_command}")
	if err != nil || len(strings.Fields(string(pane))) != 3 {
		t.Fatalf("pane identity: %v %s", err, pane)
	}
	t.Logf("target pane=%s", pane)
	processes := isolatedSpawnProcessTree(t, strings.Fields(string(pane))[1])
	native := false
	for pid, lifetime := range processes {
		executable, _ := os.Readlink("/proc/" + pid + "/exe")
		t.Logf("owned process pid=%s start=%s exe=%s", pid, lifetime, executable)
		if filepath.Base(executable) == "codex" {
			native = true
			env, err := os.ReadFile("/proc/" + pid + "/environ")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range []string{"HOME=" + filepath.Join(root, "home"), "CODEX_HOME=" + filepath.Join(root, "home", ".codex"), "ZEN_WORKER_ID=" + id, "ZEN_STATE_DIR=" + app.stateDir} {
				if !strings.Contains("\x00"+string(env), "\x00"+entry+"\x00") {
					t.Fatalf("native process isolation/identity missing %s", strings.SplitN(entry, "=", 2)[0])
				}
			}
		}
	}
	if !native {
		t.Fatal("no native Codex child in the pane process tree")
	}
	state, err := store.FSM().State(lifecycle.WorkID(resp.BrainWork.ID))
	if err != nil || len(state.Admissions) != 1 {
		t.Fatalf("expected one admission: %v state=%+v", err, state)
	}
	admission := state.AdmissionByToken(state.Attempt.TurnToken)
	query := exec.CommandContext(ctx, cli, "worker", "receipt", "-state-dir", app.stateDir, "-work-id", string(state.ID), "-id", id, "-turn-id", string(admission.TurnToken))
	receiptOutput, err := query.Output()
	if err != nil {
		t.Fatalf("read-only CLI receipt: %v", err)
	}
	var receiptResponse control.Response
	if err := json.Unmarshal(receiptOutput, &receiptResponse); err != nil || !receiptResponse.OK || receiptResponse.WorkerReceipt == nil ||
		receiptResponse.WorkerReceipt.Admission.Status != lifecycle.AdmissionAccepted || !receiptResponse.WorkerReceipt.OwnsAttempt {
		t.Fatalf("exact CLI receipt: decode=%v response=%s", err, receiptOutput)
	}
	for _, admission := range state.Admissions {
		t.Logf("admission=%+v", admission)
		if admission.Status != lifecycle.AdmissionAccepted || admission.SessionID != id || admission.ProcessIdentity != owned.ProcessIdentity || admission.PaneGeneration != owned.PaneGeneration || admission.PreparedAt.IsZero() || admission.SettledAt == nil || admission.SettledAt.Before(admission.PreparedAt) {
			t.Fatalf("not exactly accepted: %+v", admission)
		}
		receipt, found, err := w.InputReceiptResult(id, admission.Receipt)
		if err != nil || !found || receipt.Outcome != watcher.InputAccepted || receipt.Receipt != string(admission.TurnToken) {
			t.Fatalf("receipt mismatch: %+v found=%v err=%v", receipt, found, err)
		}
		t.Logf("confirmed receipt=%+v", receipt)
	}
	deadline = time.Now().Add(75 * time.Second)
	for {
		pane, err := runTmux("capture-pane", "-p", "-S", "-", "-t", id)
		if err != nil {
			t.Fatalf("capture: %v %s", err, pane)
		}
		if strings.Contains(string(pane), "ZEN_FULL_SPAWN_VERIFIED") {
			t.Log("Codex reply: ZEN_FULL_SPAWN_VERIFIED (absent from input)")
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("no short reply: %s", pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	queues := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "send-keys\t") {
			queues++
			t.Logf("single real queue audit: %s", line)
			fields := strings.Split(line, "\t")
			if len(fields) != 5 || fields[1] != "0" || fields[4] != "" || fields[3] != "send-keys,paste-buffer,run-shell,send-keys,delete-buffer" {
				t.Fatalf("queue stderr/exit: %s", line)
			}
			ns, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil || time.Unix(0, ns).Before(admission.PreparedAt) || admission.SettledAt.Before(time.Unix(0, ns)) {
				t.Fatalf("prepared/queue/accepted ordering: %s", line)
			}
		}
	}
	if queues != 1 {
		t.Fatalf("queue submissions=%d, want exactly one", queues)
	}
	if out, err := runTmux("list-buffers", "-F", "#{buffer_name}"); err != nil || len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("buffer cleanup: %v %s", err, out)
	}
	stream := strings.Split(admission.AdmissionStream, "\x00")
	if len(stream) != 3 || !strings.HasPrefix(stream[2], root+string(os.PathSeparator)) {
		t.Fatalf("provider evidence escaped private HOME: %q", admission.AdmissionStream)
	}
	f, err := os.Open(stream[2])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	userMessages, assistantMarkers := 0, 0
	for {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if row.Type == "response_item" {
			if row.Payload.Type == "function_call" || row.Payload.Type == "custom_tool_call" {
				t.Fatal("transport test unexpectedly called a tool")
			}
			if row.Payload.Role == "user" {
				for _, content := range row.Payload.Content {
					if strings.Contains(content.Text, prompt) {
						userMessages++
					}
				}
			}
			if row.Payload.Role == "assistant" {
				for _, content := range row.Payload.Content {
					if strings.TrimSpace(content.Text) == "ZEN_FULL_SPAWN_VERIFIED" {
						assistantMarkers++
					}
				}
			}
		}
	}
	if userMessages != 1 || assistantMarkers != 1 {
		t.Fatalf("rollout user submissions=%d assistant markers=%d", userMessages, assistantMarkers)
	}
	t.Log("private rollout: one user submission, one exact assistant marker, zero tool calls")
	// Kill the owned target before checking its descendants, not the server or
	// any unrelated session. The seed remains until the registered teardown.
	if err := w.KillSession(id); err != nil {
		t.Fatal(err)
	}
	closed = true
	deadline = time.Now().Add(5 * time.Second)
	for {
		alive := 0
		for pid, start := range processes {
			if isolatedSpawnProcessStart(pid) == start {
				alive++
			}
		}
		if alive == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned descendants still alive after close: %d", alive)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Log("target and observed descendants exited; dedicated server/control teardown follows")
	t.Log("PASS: private CLI -> control -> prepared -> one tmux queue -> exact accepted/receipt -> real reply; buffer empty")
}

type isolatedSpawnWatcher struct {
	*watcher.Watcher
	executable string
}

func (w *isolatedSpawnWatcher) CreateSession(target string, options watcher.CreateSessionOptions) (string, error) {
	// The embedded test server is not a CLI binary. Match production's
	// executable provenance rather than recursively invoking the test runner.
	if options.Env == nil {
		options.Env = map[string]string{}
	}
	options.Env["ZEN_WORKER_PROGRESS_CMD"] = w.executable
	return w.Watcher.CreateSession(target, options)
}

func isolatedSpawnProcessStart(pid string) string {
	data, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return ""
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return ""
	}
	return fields[19]
}

func isolatedSpawnProcessTree(t *testing.T, rootPID string) map[string]string {
	t.Helper()
	result := map[string]string{}
	queue := []string{rootPID}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, seen := result[pid]; seen {
			continue
		}
		if start := isolatedSpawnProcessStart(pid); start != "" {
			result[pid] = start
		}
		children, _ := os.ReadFile("/proc/" + pid + "/task/" + pid + "/children")
		queue = append(queue, strings.Fields(string(children))...)
		if len(result) > 256 {
			t.Fatal("unexpectedly large test process tree")
		}
	}
	return result
}
