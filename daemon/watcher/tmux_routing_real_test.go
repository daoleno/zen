package watcher

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/daoleno/zen/daemon/classifier"
)

type sharedTmuxHarness struct {
	w             *Watcher
	root          string
	selected      string
	physical      string
	defaultSocket string
	realTmux      string
	scratch       string
}

func newSharedTmuxHarness(t *testing.T, defaultServer bool) *sharedTmuxHarness {
	t.Helper()
	requireTmux(t)
	root := shortTmuxTestDir(t)
	installIsolatedTmuxShim(t, root)
	defaultSocket := filepath.Join(root, "user-default.sock")
	selected := filepath.Join(root, "inherited.sock")
	physical := selected
	if defaultServer {
		selected = ""
		physical = defaultSocket
	}
	h := &sharedTmuxHarness{
		w:             New(10 * time.Millisecond),
		root:          root,
		selected:      selected,
		physical:      physical,
		defaultSocket: defaultSocket,
		realTmux:      os.Getenv("ZEN_TEST_REAL_TMUX"),
		scratch:       filepath.Join(root, "provider-tmux"),
	}
	if err := os.MkdirAll(h.scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	h.w.SetTmuxServer(h.selected, h.scratch)
	t.Cleanup(func() {
		for _, socket := range []string{filepath.Join(root, "inherited.sock"), defaultSocket} {
			stopHarnessTmuxServer(t, h.realTmux, socket)
		}
	})
	return h
}

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

func shortTmuxTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zt-shared-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func stopHarnessTmuxServer(t *testing.T, realTmux, socket string) {
	t.Helper()
	pidRaw, err := exec.Command(realTmux, "-S", socket, "display-message", "-p", "#{pid}").Output()
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err != nil || pid <= 0 {
		t.Errorf("resolve test tmux pid on %s: %q", socket, pidRaw)
		return
	}
	if out, killErr := exec.Command(realTmux, "-S", socket, "kill-server").CombinedOutput(); killErr != nil {
		t.Errorf("stop test tmux server %d on %s: %v: %s", pid, socket, killErr, out)
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command(realTmux, "-S", socket, "display-message", "-p", "#{pid}").Run() != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("test tmux server %d on %s did not stop", pid, socket)
}

// installIsolatedTmuxShim is a fail-closed firewall for real-tmux tests. Bare
// commands map to a test-owned default socket and explicit sockets must stay
// below the test root, independent of the process's ambient TMUX variables.
func installIsolatedTmuxShim(t *testing.T, root string) {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not available")
	}
	realTmux, err = filepath.Abs(realTmux)
	if err != nil {
		t.Fatal(err)
	}
	shimDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(shimDir, "tmux")
	const shim = `#!/bin/sh
set -eu
real=${ZEN_TEST_REAL_TMUX:?}
allowed=${ZEN_TEST_ALLOWED_TMUX_ROOT:?}
default_socket=${ZEN_TEST_DEFAULT_TMUX_SOCKET:?}
audit=${ZEN_TEST_TMUX_AUDIT:?}
socket=
expect_socket=0
scan_global=1
for arg in "$@"; do
  if [ "$scan_global" -eq 0 ]; then continue; fi
  if [ "$expect_socket" -eq 1 ]; then socket=$arg; expect_socket=0; continue; fi
  case "$arg" in
    -S) expect_socket=1 ;;
    -S*) socket=${arg#-S} ;;
    -L|-L*) echo "tmux test firewall: -L is not allowed" >&2; exit 97 ;;
    -*) ;;
    *) scan_global=0 ;;
  esac
done
if [ "$expect_socket" -eq 1 ]; then
  echo "tmux test firewall: missing -S socket" >&2
  exit 97
fi
if [ -z "$socket" ]; then
  socket=$default_socket
  set -- -S "$socket" "$@"
fi
case "$socket" in
  "$allowed"/*) ;;
  *)
    printf 'REJECT\t%s\t%s\n' "$socket" "$*" >>"$audit"
    echo "tmux test firewall: refusing socket outside test root: $socket" >&2
    exit 97
    ;;
esac
printf '%s\t%s\n' "$socket" "$*" >>"$audit"
exec "$real" "$@"
`
	if err := os.WriteFile(shimPath, []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_REAL_TMUX", realTmux)
	t.Setenv("ZEN_TEST_ALLOWED_TMUX_ROOT", root)
	t.Setenv("ZEN_TEST_DEFAULT_TMUX_SOCKET", filepath.Join(root, "user-default.sock"))
	t.Setenv("ZEN_TEST_TMUX_AUDIT", filepath.Join(root, "tmux-audit.log"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func tmuxHarnessCommand(socket string, args ...string) *exec.Cmd {
	return exec.Command("tmux", append(tmuxSocketArgs(socket), args...)...)
}

func createHarnessPane(t *testing.T, socket, session, command string) string {
	t.Helper()
	if out, err := tmuxHarnessCommand(socket, "new-session", "-d", "-s", session, command).CombinedOutput(); err != nil {
		t.Fatalf("create %s on %q: %v: %s", session, socket, err, out)
	}
	out, err := tmuxHarnessCommand(socket, "display-message", "-p", "-t", session, "#{session_name}:#{window_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func captureHarnessPane(t *testing.T, socket, target string) string {
	t.Helper()
	out, err := tmuxHarnessCommand(socket, "capture-pane", "-t", target, "-p", "-S", "-200").Output()
	if err != nil {
		t.Fatalf("capture %s: %v", target, err)
	}
	return string(out)
}

func waitForHarness(t *testing.T, summary string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", summary)
}

func TestTmuxTestFirewallRejectsAmbientSockets(t *testing.T) {
	requireTmux(t)
	root := shortTmuxTestDir(t)
	installIsolatedTmuxShim(t, root)
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.sock")
	out, err := exec.Command("tmux", "-S", outside, "kill-server").CombinedOutput()
	if err == nil {
		t.Fatalf("firewall accepted out-of-root socket %s", outside)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 97 || !strings.Contains(string(out), "refusing socket") {
		t.Fatalf("firewall rejection = %v: %s", err, out)
	}
}

func TestRealTmuxCustomServerVisibleAndSupportsTwoAttachedClients(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "shared-client", Command: "exec /bin/sh", Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := baseSessionName(target)
	ls, err := exec.Command(h.realTmux, "-S", h.physical, "list-sessions", "-F", "#{session_name}").Output()
	if err != nil || !strings.Contains(string(ls), session) {
		t.Fatalf("ordinary custom-socket list = %q err=%v, want %q", ls, err, session)
	}

	startAttachedTmuxClient(t, h.realTmux, h.physical, session)
	waitForHarness(t, "first attached client", func() bool {
		return attachedClientCount(h.realTmux, h.physical, session) == 1
	})
	startAttachedTmuxClient(t, h.realTmux, h.physical, session)
	waitForHarness(t, "simultaneous second attached client", func() bool {
		return attachedClientCount(h.realTmux, h.physical, session) >= 2
	})
	if err := exec.Command(h.realTmux, "-S", h.physical, "has-session", "-t", target).Run(); err != nil {
		t.Fatalf("second attach disconnected or removed source Session: %v", err)
	}
}

func startAttachedTmuxClient(t *testing.T, realTmux, socket, session string) {
	t.Helper()
	cmd := exec.Command(realTmux, "-S", socket, "-T", "RGB,256", "attach-session", "-t", session)
	cmd.Env = tmuxDetachedClientEnvironment(os.Environ())
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("attach client: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, ptmx)
		close(done)
	}()
	t.Cleanup(func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
}

func tmuxDetachedClientEnvironment(base []string) []string {
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, "TMUX=") || strings.HasPrefix(entry, "TMUX_PANE=") ||
			strings.HasPrefix(entry, "TERM=") || strings.HasPrefix(entry, "COLORTERM=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func attachedClientCount(realTmux, socket, session string) int {
	out, err := exec.Command(realTmux, "-S", socket, "list-clients", "-F", "#{session_name}").Output()
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == session {
			count++
		}
	}
	return count
}

func TestRealTmuxOutsideTmuxUsesOrdinaryDefaultServer(t *testing.T) {
	h := newSharedTmuxHarness(t, true)
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "default-visible", Command: "exec /bin/sh", Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		t.Fatalf("ordinary tmux ls: %v: %s", err, out)
	}
	if !strings.Contains(string(out), baseSessionName(target)) {
		t.Fatalf("ordinary default-server listing %q does not contain %q", out, baseSessionName(target))
	}
	if got := h.w.socketPathFor(target); got != "" {
		t.Fatalf("outside-tmux watcher selected custom socket %q", got)
	}
}

func TestRealTmuxOwnedLifecycleAndAmbientCollisionContainment(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	ambientTarget := createHarnessPane(t, h.selected, "brain-agent-name-collision", "exec /bin/sh")
	if out, err := tmuxHarnessCommand(h.selected, "set-option", "-wg", "@zen_agent_created", "1").CombinedOutput(); err != nil {
		t.Fatalf("set inherited collision marker: %v: %s", err, out)
	}
	originalAmbientName, err := tmuxHarnessCommand(h.selected, "display-message", "-p", "-t", ambientTarget, "#{window_name}").Output()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the strongest stale/name-collision case: the projection contains
	// the ambient target, but the shared server lacks a local ownership marker.
	h.w.registerCreatedSession(ambientTarget, h.root, CreateSessionOptions{Name: "stale collision"}, time.Now())
	if _, err := h.w.CapturePaneContent(ambientTarget); !errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("ambient capture error = %v, want ErrUnownedTmuxTarget", err)
	}
	h.w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	if err := h.w.SendKey(ambientTarget, "a"); err == nil {
		t.Fatal("ambient input succeeded through stale projection")
	}
	if err := h.w.KillSession(ambientTarget); !errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("ambient close error = %v, want ErrUnownedTmuxTarget", err)
	}
	if _, err := h.w.CreateSession(ambientTarget, CreateSessionOptions{Name: "must-not-join"}); !errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("ambient preferred-target create = %v, want ErrUnownedTmuxTarget", err)
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", ambientTarget).Run(); err != nil {
		t.Fatalf("ambient Session did not survive fail-closed operations: %v", err)
	}

	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "name-collision", Command: "exec /bin/sh", Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if presence, err := h.w.ProbeSession(target); err != nil || presence != SessionPresencePresent {
		t.Fatalf("owned probe = %v err=%v", presence, err)
	}
	if presence, err := h.w.ProbeSession(ambientTarget); err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("ambient probe = %v err=%v, want absent from Zen", presence, err)
	}

	// Input and capture use production tmux I/O, with the process-identity seam
	// limited to classifying the test shell as an interactive provider.
	if err := h.w.SendInput(target, "echo ZEN_SHARED_INPUT"); err != nil {
		t.Fatalf("draft input: %v", err)
	}
	if err := h.w.SendKey(target, "Enter"); err != nil {
		t.Fatalf("submit key: %v", err)
	}
	waitForHarness(t, "shared-server input", func() bool {
		text, captureErr := h.w.CapturePaneContent(target)
		return captureErr == nil && strings.Contains(text, "ZEN_SHARED_INPUT")
	})

	// A fresh watcher recovers only the durable local marker from the same
	// server; the ambient convention/global-marker collision stays invisible.
	recovered := New(10 * time.Millisecond)
	recovered.SetTmuxServer(h.selected, h.scratch)
	recovered.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	recovered.poll()
	drainWatcherEvents(recovered)
	if agentByID(recovered.Agents(), target) == nil {
		t.Fatalf("owned target not rediscovered: %#v", recovered.Agents())
	}
	if agentByID(recovered.Agents(), ambientTarget) != nil {
		t.Fatalf("ambient collision was adopted: %#v", recovered.Agents())
	}

	if out, err := tmuxHarnessCommand(h.selected, "set-option", "-w", "-t", target, "remain-on-exit", "on").CombinedOutput(); err != nil {
		t.Fatalf("set remain-on-exit: %v: %s", err, out)
	}
	if err := h.w.SendInput(target, "echo ZEN_SHARED_COMPLETE; exit 0"); err != nil {
		t.Fatal(err)
	}
	if err := h.w.SendKey(target, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitForHarness(t, "owned pane completion", func() bool {
		out, displayErr := tmuxHarnessCommand(h.selected, "display-message", "-p", "-t", target, "#{pane_dead}").Output()
		return displayErr == nil && strings.TrimSpace(string(out)) == "1"
	})
	recovered.poll()
	drainWatcherEvents(recovered)
	agent := agentByID(recovered.Agents(), target)
	if agent == nil || agent.State != classifier.StateDone || agent.PaneAlive {
		t.Fatalf("completed recovered agent = %#v", agent)
	}

	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	resources := &fakeDelegatedResourceManager{boundTarget: target, boundUnit: unit}
	recovered.resources = resources
	if err := recovered.KillSession(target); err != nil {
		t.Fatalf("close owned target: %v", err)
	}
	if len(resources.released) != 1 || resources.released[0] != target+"\t"+unit {
		t.Fatalf("resource releases = %#v", resources.released)
	}
	recovered.poll()
	drainWatcherEvents(recovered)
	if agentByID(recovered.Agents(), target) != nil {
		t.Fatalf("closed target survived cleanup: %#v", recovered.Agents())
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", ambientTarget).Run(); err != nil {
		t.Fatalf("ambient Session was closed during owned cleanup: %v", err)
	}
	ambientName, err := tmuxHarnessCommand(h.selected, "display-message", "-p", "-t", ambientTarget, "#{window_name}").Output()
	if err != nil || string(ambientName) != string(originalAmbientName) {
		t.Fatalf("ambient window renamed: before=%q after=%q err=%v", originalAmbientName, ambientName, err)
	}
}
