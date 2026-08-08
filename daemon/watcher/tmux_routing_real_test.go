package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// realTmuxHarness builds two owned custom tmux servers — a daemon-namespaced
// server (Zen-owned) and an explicitly user-tagged server — and drives the
// production watcher against both. Only custom -S sockets are ever touched;
// the user's real default server is never created or mutated.
func realTmuxHarness(t *testing.T) (*Watcher, string, string) {
	t.Helper()
	requireTmux(t)
	root := shortTmuxTestDir(t)
	installIsolatedTmuxShim(t, root)
	daemonSocket := filepath.Join(root, "daemon.sock")
	userSocket := filepath.Join(root, "user.sock")
	defaultSocket := filepath.Join(root, "user-default.sock")
	realTmux := os.Getenv("ZEN_TEST_REAL_TMUX")
	t.Cleanup(func() {
		// Use the already-resolved real binary and explicit test-owned sockets.
		// Cleanup must not depend on PATH, TMUX, or TMUX_TMPDIR, and it must
		// finish before shortTmuxTestDir removes the socket directory.
		for _, socket := range []string{daemonSocket, userSocket, defaultSocket} {
			stopHarnessTmuxServer(t, realTmux, socket)
		}
	})
	// The user-default route remains logically empty in watcher ownership, but
	// the test shim physically maps every bare tmux command to this explicit
	// socket. No ambient TMUX/TMUX_TMPDIR/TMUX_PANE state can therefore route
	// a destructive test command to the tmux server running the test.
	w := New(time.Second)
	w.SetDaemonSocket(daemonSocket, filepath.Join(root, "scratch"))
	// Deterministic identity seam: the panes run /bin/sh, which the
	// production identity proof correctly refuses (shells are not providers).
	// The reviewer-equivalent harness pins the command the way a spawned
	// provider would present it, so every input/admission boundary still
	// resolves the exact pane on the target's own server.
	w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	return w, daemonSocket, userSocket
}

func stopHarnessTmuxServer(t *testing.T, realTmux, socket string) {
	t.Helper()
	pidRaw, err := exec.Command(realTmux, "-S", socket, "display-message", "-p", "#{pid}").Output()
	if err != nil {
		return // The test never created this server, or it already exited.
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err != nil || pid <= 0 {
		t.Errorf("resolve test tmux pid on %s: %q", socket, pidRaw)
		return
	}
	if out, killErr := exec.Command(realTmux, "-S", socket, "kill-server").CombinedOutput(); killErr != nil {
		t.Errorf("stop test tmux server %d on %s: %v: %s", pid, socket, killErr, out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command(realTmux, "-S", socket, "display-message", "-p", "#{pid}").Run() != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The PID came from this test's unique socket immediately before the
	// kill-server request, so a final signal cannot target unrelated state.
	process, findErr := os.FindProcess(pid)
	if findErr == nil {
		_ = process.Kill()
	}
	t.Errorf("test tmux server %d on %s did not stop after kill-server", pid, socket)
}

// installIsolatedTmuxShim is a fail-closed command firewall for real tmux
// tests. Every invocation in the test process — including production watcher
// code and commands typed inside a test pane — is forced onto a socket under
// root. An explicit socket outside root, -L, or an incomplete -S is rejected
// before the real tmux binary is executed.
//
// Merely unsetting TMUX and changing TMUX_TMPDIR is not a sufficient safety
// boundary: one missed environment edge or cleanup command can still resolve
// the ambient server. The executable shim makes the isolation invariant
// independent of ambient process state.
func installIsolatedTmuxShim(t *testing.T, root string) {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not available")
	}
	realTmux, err = filepath.Abs(realTmux)
	if err != nil {
		t.Fatalf("resolve tmux binary: %v", err)
	}
	shimDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatalf("create tmux shim directory: %v", err)
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
  if [ "$scan_global" -eq 0 ]; then
    continue
  fi
  if [ "$expect_socket" -eq 1 ]; then
    socket=$arg
    expect_socket=0
    continue
  fi
  case "$arg" in
    -S) expect_socket=1 ;;
    -S*) socket=${arg#-S} ;;
    -L|-L*)
      echo "tmux test firewall: -L is not allowed" >&2
      exit 97
      ;;
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
		t.Fatalf("write tmux shim: %v", err)
	}
	t.Setenv("ZEN_TEST_REAL_TMUX", realTmux)
	t.Setenv("ZEN_TEST_ALLOWED_TMUX_ROOT", root)
	t.Setenv("ZEN_TEST_DEFAULT_TMUX_SOCKET", filepath.Join(root, "user-default.sock"))
	t.Setenv("ZEN_TEST_TMUX_AUDIT", filepath.Join(root, "tmux-audit.log"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		raw, readErr := os.ReadFile(filepath.Join(root, "tmux-audit.log"))
		if readErr != nil {
			t.Logf("read tmux firewall audit: %v", readErr)
			return
		}
		t.Logf("tmux firewall audit:\n%s", raw)
	})
}

func TestTmuxTestFirewallRejectsOutOfRootSocket(t *testing.T) {
	requireTmux(t)
	root := shortTmuxTestDir(t)
	installIsolatedTmuxShim(t, root)
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.sock")
	out, err := exec.Command("tmux", "-S", outside, "kill-server").CombinedOutput()
	if err == nil {
		t.Fatalf("tmux firewall accepted out-of-root socket %s", outside)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 97 {
		t.Fatalf("tmux firewall error = %v (%s), want exit 97", err, out)
	}
	if !strings.Contains(string(out), "refusing socket outside test root") {
		t.Fatalf("tmux firewall rejection = %q", out)
	}
	// -L is a global socket option that would bypass the -S rewrite; it must
	// fail closed the same way as an out-of-root explicit socket.
	out, err = exec.Command("tmux", "-L", "outside", "kill-server").CombinedOutput()
	if err == nil {
		t.Fatal("tmux firewall accepted -L")
	}
	exitErr, ok = err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 97 {
		t.Fatalf("tmux firewall -L error = %v (%s), want exit 97", err, out)
	}
	if !strings.Contains(string(out), "-L is not allowed") {
		t.Fatalf("tmux firewall -L rejection = %q", out)
	}
}

func tmuxHarnessCommand(socket string, args ...string) *exec.Cmd {
	// Every invocation resolves through the PATH shim, which maps bare
	// commands to the private default socket and rejects any explicit socket
	// outside the test root before the real tmux binary is reached.
	return exec.Command("tmux", append(tmuxSocketArgs(socket), args...)...)
}

func createHarnessPane(t *testing.T, socket, session, command string) string {
	t.Helper()
	if out, err := tmuxHarnessCommand(socket, "new-session", "-d", "-s", session, command).CombinedOutput(); err != nil {
		t.Fatalf("create %s on %q: %v: %s", session, socket, err, out)
	}
	out, err := tmuxHarnessCommand(socket, "display-message", "-p", "-t", session, "#{session_name}:#{window_id}").Output()
	if err != nil {
		t.Fatalf("resolve %s target: %v", session, err)
	}
	return strings.TrimSpace(string(out))
}

func captureHarnessPane(t *testing.T, socket, target string) string {
	t.Helper()
	out, err := tmuxHarnessCommand(socket, "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		t.Fatalf("capture %s on %s: %v", target, socket, err)
	}
	return string(out)
}

// TestRealTmuxBothServersSupportInputCaptureKeysReceiptsAndKill is the
// reviewer-equivalent red/green harness: every production operation works on
// a daemon-namespaced target AND on an explicitly user-tagged target, routed
// through the target's own server; the wrong server fails closed.
func TestRealTmuxBothServersSupportInputCaptureKeysReceiptsAndKill(t *testing.T) {
	w, daemonSocket, userSocket := realTmuxHarness(t)

	daemonTarget := createHarnessPane(t, daemonSocket, "daemon-target", "/bin/sh")
	userTarget := createHarnessPane(t, userSocket, "user-target", "/bin/sh")
	w.registerCreatedSession(daemonSocket, daemonTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "daemon"}, time.Now())
	w.registerCreatedSession(userSocket, userTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "user"}, time.Now())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = exec.Command("tmux", "-S", userSocket, "kill-server").Run()
	})

	for _, tc := range []struct {
		name   string
		target string
		socket string
	}{
		{name: "daemon target", target: daemonTarget, socket: daemonSocket},
		{name: "user-tagged target", target: userTarget, socket: userSocket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Ownership routing: the internal resolver returns the target's
			// own server, never the other server.
			if got := w.socketPathFor(tc.target); got != tc.socket {
				t.Fatalf("socketPathFor(%s) = %q, want %q", tc.target, got, tc.socket)
			}
			// Probe.
			if presence, err := w.ProbeSession(tc.target); err != nil || presence != SessionPresencePresent {
				t.Fatalf("ProbeSession = %v err=%v, want present", presence, err)
			}
			// Capture.
			if err := exec.Command("tmux", "-S", tc.socket, "send-keys", "-t", tc.target, "echo FIRST-"+tc.name, "Enter").Run(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)
			captured, err := w.CapturePaneContent(tc.target)
			if err != nil || !strings.Contains(captured, "FIRST-"+tc.name) {
				t.Fatalf("CapturePaneContent = %q err=%v, want FIRST-%s", captured, err, tc.name)
			}
			// SendKey routes through target ownership.
			if err := w.SendKey(tc.target, "a"); err != nil {
				t.Fatalf("SendKey: %v", err)
			}
			time.Sleep(150 * time.Millisecond)
			if got := captureHarnessPane(t, tc.socket, tc.target); !strings.Contains(got, "a") {
				t.Fatalf("SendKey did not reach %s: %q", tc.name, got)
			}
			// Non-submit draft send (no trailing newline) routes through
			// target ownership too: the literal keystrokes must reach the
			// pane on its own server, not the user default server.
			if err := w.SendInput(tc.target, "echo DRAFT-"+tc.name); err != nil {
				t.Fatalf("SendInput non-submit: %v", err)
			}
			time.Sleep(150 * time.Millisecond)
			if got := captureHarnessPane(t, tc.socket, tc.target); !strings.Contains(got, "DRAFT-"+tc.name) {
				t.Fatalf("non-submit draft did not reach %s: %q", tc.name, got)
			}
			// Input submission with receipt: buffers, queue, paste and the
			// receipt ledger option are all server-local.
			receipt := "receipt-" + tc.name
			result, err := w.SendInputWithReceiptResult(tc.target, "echo GOT-"+tc.name+"; ", receipt)
			if err != nil || result.Outcome != InputAccepted {
				t.Fatalf("SendInputWithReceiptResult = (%+v, %v), want accepted", result, err)
			}
			time.Sleep(200 * time.Millisecond)
			if got := captureHarnessPane(t, tc.socket, tc.target); !strings.Contains(got, "GOT-"+tc.name) {
				t.Fatalf("submitted input did not execute on %s: %q", tc.name, got)
			}
			if outcome, found, err := w.InputReceiptResult(tc.target, receipt); err != nil || !found || outcome.Outcome != InputAccepted {
				t.Fatalf("InputReceiptResult = (%+v, %v, %v), want accepted+found", outcome, found, err)
			}
			// Paste-buffer cleanup on the target's own server.
			if buffers := tmuxListBuffers(t, tc.socket); strings.Contains(buffers, "zen-session-input-") {
				t.Fatalf("paste buffer left on %s: %s", tc.name, buffers)
			}
			// Kill only the target window; the sibling on the same server and
			// the other server's session survive.
			_ = createHarnessPane(t, tc.socket, "sibling-"+tc.name, "/bin/sh")
			otherSocket := userSocket
			if tc.socket == userSocket {
				otherSocket = daemonSocket
			}
			other := createHarnessPane(t, otherSocket, "other-"+tc.name, "/bin/sh")
			if err := w.KillSession(tc.target); err != nil {
				t.Fatalf("KillSession: %v", err)
			}
			if alive := exec.Command("tmux", "-S", tc.socket, "has-session", "-t", "sibling-"+tc.name).Run() == nil; !alive {
				t.Fatalf("KillSession removed a sibling on %s", tc.name)
			}
			if alive := exec.Command("tmux", "-S", otherSocket, "has-session", "-t", other).Run() == nil; !alive {
				t.Fatalf("KillSession on %s removed a Session on the other server", tc.name)
			}
		})
	}
}

func tmuxListBuffers(t *testing.T, socket string) string {
	t.Helper()
	out, err := exec.Command("tmux", "-S", socket, "list-buffers").CombinedOutput()
	if err != nil {
		return "" // no server buffers
	}
	return string(out)
}

// TestRealTmuxWrongSocketFailsClosed drives operations at the wrong server
// and proves they fail without touching the right server's pane.
func TestRealTmuxWrongSocketFailsClosed(t *testing.T) {
	w, daemonSocket, userSocket := realTmuxHarness(t)
	daemonTarget := createHarnessPane(t, daemonSocket, "daemon-target", "/bin/sh")
	userTarget := createHarnessPane(t, userSocket, "user-target", "/bin/sh")
	w.registerCreatedSession(daemonSocket, daemonTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "daemon"}, time.Now())
	w.registerCreatedSession(userSocket, userTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "user"}, time.Now())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = exec.Command("tmux", "-S", userSocket, "kill-server").Run()
	})

	// A user-tagged target must never resolve to the daemon server (P0-1).
	if got := w.socketPathFor(userTarget); got != userSocket {
		t.Fatalf("known user target resolved to %q, want %q", got, userSocket)
	}
	// Identity proof against the wrong server fails closed: the daemon server
	// has no such pane, so no mutation is attempted.
	resolver := func(string) (targetProcessIdentity, bool) {
		return targetProcessIdentity{}, false
	}
	if err := guardTargetIdentity(resolver, daemonTarget, targetProcessIdentity{}); err == nil {
		t.Fatal("identity guard on an unresolvable target succeeded")
	}
	// The readiness trust-prompt advance against the wrong server fails and
	// leaves the pane untouched.
	if ok := waitForInputReadyGuarded(daemonSocket, userTarget, "codex", 500*time.Millisecond, nil); ok {
		t.Fatal("readiness advanced a user pane on the daemon server")
	}
	if got := captureHarnessPane(t, userSocket, userTarget); strings.Contains(got, "ENTERED") {
		t.Fatalf("user pane was mutated through the wrong server: %q", got)
	}
}

// TestRealTmuxSameNameAcrossServersResolvesDeterministically covers the
// same-name ambiguity: the daemon-namespaced (Zen-owned) entry shadows the
// user entry, and operations route to the Zen-owned window only.
func TestRealTmuxSameNameAcrossServersResolvesDeterministically(t *testing.T) {
	w, daemonSocket, _ := realTmuxHarness(t)
	daemonTarget := createHarnessPane(t, daemonSocket, "ambig", "/bin/sh")
	userTarget := createHarnessPane(t, "", "ambig", "/bin/sh")
	// The production inventory tags daemon first and shadows the user entry.
	windows, err := w.listTmuxWindows()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, win := range windows {
		seen[win.target] = win.socket
	}
	if got := seen[daemonTarget]; got != daemonSocket {
		t.Fatalf("inventory tagged %s = %q, want daemon socket", daemonTarget, got)
	}
	// Both servers' first window is @0, so daemonTarget == userTarget: the
	// user entry must be shadowed (exactly one inventory entry, tagged with
	// the daemon socket), never duplicated or user-tagged.
	if userTarget != daemonTarget {
		t.Fatalf("fixture target mismatch: %q vs %q", userTarget, daemonTarget)
	}
	if got := w.socketPathFor(daemonTarget); got != daemonSocket {
		t.Fatalf("ambiguous same-name target resolved to %q, want daemon", got)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = tmuxHarnessCommand("", "kill-server").Run()
	})
}

// TestRealTmuxTrustPromptAdvanceRoutesThroughTargetServer covers the
// Codex/Cursor workspace trust-prompt send-keys: the Enter keypress reaches
// the pane on its own server, and the readiness loop then sees a codex-ready
// frame and returns true.
func TestRealTmuxTrustPromptAdvanceRoutesThroughTargetServer(t *testing.T) {
	w, daemonSocket, userSocket := realTmuxHarness(t)
	// The pane program prints the production codex workspace trust prompt
	// shape (path line, blank line, options) with the pane's own cwd, waits
	// for the Enter, then prints a codex-ready frame.
	script := `printf '> You are in %s

  Do you trust the contents of this directory? Working with untrusted contents
  comes with higher risk of prompt injection.

› 1. Yes, continue
  2. No, quit

  Press enter to continue
' "$PWD"
read line
echo ENTERED
echo '>_ codex'
echo '› ready'
sleep 300`
	daemonTarget := createHarnessPane(t, daemonSocket, "codex-daemon", script)
	userTarget := createHarnessPane(t, userSocket, "codex-user", script)
	w.registerCreatedSession(daemonSocket, daemonTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "daemon"}, time.Now())
	w.registerCreatedSession(userSocket, userTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "user"}, time.Now())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = exec.Command("tmux", "-S", userSocket, "kill-server").Run()
	})
	for _, tc := range []struct {
		name   string
		target string
		socket string
	}{
		{name: "daemon target", target: daemonTarget, socket: daemonSocket},
		{name: "user-tagged target", target: userTarget, socket: userSocket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ok := waitForInputReadyGuarded(tc.socket, tc.target, "codex", 3*time.Second, nil); !ok {
				t.Fatalf("readiness did not advance on %s: %q", tc.name, captureHarnessPane(t, tc.socket, tc.target))
			}
			if got := captureHarnessPane(t, tc.socket, tc.target); !strings.Contains(got, "ENTERED") {
				t.Fatalf("trust prompt Enter did not reach %s: %q", tc.name, got)
			}
		})
	}
}

// TestRealTmuxPollTagsFirstObservationFromTargetServer covers the
// first-poll/tagging order: the very first poll of a poll cycle already
// captures through the target's own server because ownership is tagged before
// the first capture.
func TestRealTmuxPollTagsFirstObservationFromTargetServer(t *testing.T) {
	w, daemonSocket, _ := realTmuxHarness(t)
	daemonTarget := createHarnessPane(t, daemonSocket, "poll-daemon", "/bin/sh")
	// The user session lives on the isolated default server (plain tmux).
	userTarget := createHarnessPane(t, "", "poll-user", "/bin/sh")
	defaultSocketRaw, err := tmuxHarnessCommand("", "display-message", "-p", "-t", userTarget, "#{socket_path}").Output()
	if err != nil {
		t.Fatalf("resolve isolated default socket: %v", err)
	}
	defaultSocket := strings.TrimSpace(string(defaultSocketRaw))
	if defaultSocket == "" {
		t.Fatal("isolated default socket is empty")
	}
	w.registerCreatedSession(daemonSocket, daemonTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "daemon"}, time.Now())
	w.registerCreatedSession("", userTarget, shortTmuxTestDir(t), CreateSessionOptions{Name: "user"}, time.Now())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		// Cleanup is explicit even though the test process has TMUX removed:
		// no destructive command may ever rely on ambient tmux routing.
		_ = exec.Command("tmux", "-S", defaultSocket, "kill-server").Run()
	})
	w.pollNow = fakePollClock([]time.Time{time.Now().UTC()})
	w.poll()
	drainWatcherEvents(w)
	if got := w.SocketPathFor(daemonTarget); got != daemonSocket {
		t.Fatalf("daemon target ownership = %q", got)
	}
	if got := w.SocketPathFor(userTarget); got != "" {
		t.Fatalf("user target ownership = %q, want the user default server", got)
	}
	agent := agentByID(w.Agents(), userTarget)
	if agent == nil {
		t.Fatalf("user target missing from agents: %v", w.Agents())
	}
	_ = classifier.StateRunning
}

// TestRealTmuxUnknownTargetFailsClosedBeforeInventory covers the unknown
// target fallback: ops against a target the watcher has never seen fail
// closed instead of mutating a different server's window.
func TestRealTmuxUnknownTargetFailsClosedBeforeInventory(t *testing.T) {
	w, daemonSocket, userSocket := realTmuxHarness(t)
	userTarget := createHarnessPane(t, userSocket, "lone-user", "/bin/sh")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = exec.Command("tmux", "-S", userSocket, "kill-server").Run()
	})
	// Not tagged: the watcher has never seen this target.
	text, err := w.CapturePaneContent(userTarget)
	if err != nil {
		t.Logf("unknown-target capture failed closed: %v", err)
	}
	if strings.Contains(text, "lone-user") {
		t.Fatalf("unknown target was captured through the wrong server")
	}
	if err := w.SendKey(userTarget, "x"); err == nil {
		t.Fatal("SendKey to an unknown target succeeded instead of failing closed")
	}
	_ = fmt.Sprintf
}
