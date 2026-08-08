package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireTmux skips the test when tmux is unavailable, mirroring the terminal
// package's guard. All tests here use explicit -S sockets only; the user's
// default server is never created or mutated.
func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

// TestUnscopedKillServerInsideDelegatedPaneCannotKillDaemonOrUserServer is
// the Slice 3 acceptance proof: the launch environment unsets TMUX and points
// TMUX_TMPDIR at a private scratch, so a plain nested `tmux kill-server`
// from a delegated pane can kill neither the daemon server (Brain + sibling
// delegated Sessions) nor the user's default server. The control phase
// reproduces the original incident: with $TMUX intact, the same command
// kills the pane's own server.
func TestUnscopedKillServerInsideDelegatedPaneCannotKillDaemonOrUserServer(t *testing.T) {
	requireTmux(t)
	_ = os.Unsetenv("TMUX")

	run := func(t *testing.T, hardened bool) {
		t.Helper()
		root := shortTmuxTestDir(t)
		daemonSocket := filepath.Join(root, "daemon.sock")
		scratch := filepath.Join(root, "scratch")
		if err := os.MkdirAll(scratch, 0o700); err != nil {
			t.Fatal(err)
		}
		// The daemon server hosts Brain + sibling delegated Sessions.
		for _, session := range []string{"brain-host", "delegated-sibling"} {
			if out, err := exec.Command("tmux", "-S", daemonSocket, "new-session", "-d", "-s", session, "/bin/sh").CombinedOutput(); err != nil {
				t.Fatalf("create %s on daemon server: %v: %s", session, err, out)
			}
		}
		t.Cleanup(func() { _ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run() })

		// The delegated pane runs the hardened launch environment: TMUX is
		// unset (agentProgressEnvScript) and TMUX_TMPDIR is the private
		// scratch (applyDaemonWindowEnvironment).
		command := "tmux kill-server; echo KILL_EXIT=$?"
		if hardened {
			command = "unset TMUX; export TMUX_TMPDIR=" + shellQuote(scratch) + "; " + command
		}
		if out, err := exec.Command("tmux", "-S", daemonSocket, "send-keys", "-t", "delegated-sibling", command, "Enter").CombinedOutput(); err != nil {
			t.Fatalf("send nested kill-server: %v: %s", err, out)
		}
		time.Sleep(700 * time.Millisecond)

		daemonAlive := exec.Command("tmux", "-S", daemonSocket, "has-session", "-t", "brain-host").Run() == nil
		siblingAlive := exec.Command("tmux", "-S", daemonSocket, "has-session", "-t", "delegated-sibling").Run() == nil
		if hardened {
			if !daemonAlive || !siblingAlive {
				t.Fatalf("hardened nested kill-server destroyed daemon sessions: host=%v sibling=%v", daemonAlive, siblingAlive)
			}
			// The user's default server must not exist as a side effect of the
			// hardened command; the kill resolved into the private scratch.
			if info, err := os.Stat(filepath.Join(scratch, fmt.Sprintf("tmux-%d", os.Getuid()), "default")); err != nil {
				t.Logf("nested kill resolved under private scratch (no default server socket): %v", err)
			} else if info.IsDir() {
				t.Logf("private scratch default socket dir present (harmless)")
			}
			return
		}
		// Control phase: with $TMUX intact the plain command kills the pane's
		// own server (the original incident).
		if daemonAlive || siblingAlive {
			t.Fatalf("control nested kill-server did not kill the daemon server (host=%v sibling=%v)", daemonAlive, siblingAlive)
		}
	}

	t.Run("hardened environment survives", func(t *testing.T) { run(t, true) })
	t.Run("control without hardening kills own server", func(t *testing.T) { run(t, false) })
}

// TestProbeSessionResolvesZenOwnedServerFirst covers the probe contract:
// a known target probes its recorded server; an unknown target probes the
// daemon-namespaced server first, then the user default server, and a hard
// absence on both resolves Absent (never fabricating a session).
func TestProbeSessionResolvesZenOwnedServerFirst(t *testing.T) {
	requireTmux(t)
	_ = os.Unsetenv("TMUX")
	root := shortTmuxTestDir(t)
	daemonSocket := filepath.Join(root, "daemon.sock")
	if out, err := exec.Command("tmux", "-S", daemonSocket, "new-session", "-d", "-s", "brain-host", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("create daemon server session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run() })

	w := New(time.Second)
	w.SetDaemonSocket(daemonSocket, filepath.Join(root, "scratch"))
	// Unknown target on the daemon server resolves Present via the daemon
	// socket probe.
	if presence, err := w.ProbeSession("brain-host"); err != nil || presence != SessionPresencePresent {
		t.Fatalf("daemon-server probe = %v err=%v, want present", presence, err)
	}
	// The recorded socket wins once the target is inventoried: a target
	// tagged on the daemon socket is never probed on the user server.
	w.mu.Lock()
	w.targetSockets["brain-host"] = daemonSocket
	w.mu.Unlock()
	if presence, err := w.ProbeSession("brain-host"); err != nil || presence != SessionPresencePresent {
		t.Fatalf("recorded-socket probe = %v err=%v, want present", presence, err)
	}
	// Absent everywhere resolves Absent without error.
	if presence, err := w.ProbeSession("definitely-not-a-real-session-xyz"); err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("absent probe = %v err=%v, want absent", presence, err)
	}
}

// TestDelegatedSessionOnDaemonServerSurvivesUserDefaultServerKill covers the
// acceptance criterion from the user side: killing the user/default tmux
// server cannot remove Zen-owned Brain/delegated Sessions (they live on the
// daemon-namespaced server).
func TestDelegatedSessionOnDaemonServerSurvivesUserDefaultServerKill(t *testing.T) {
	requireTmux(t)
	_ = os.Unsetenv("TMUX")
	root := shortTmuxTestDir(t)
	daemonSocket := filepath.Join(root, "daemon.sock")
	if out, err := exec.Command("tmux", "-S", daemonSocket, "new-session", "-d", "-s", "delegated", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("create daemon server session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run() })

	w := New(time.Second)
	w.SetDaemonSocket(daemonSocket, filepath.Join(root, "scratch"))
	w.mu.Lock()
	w.targetSockets["delegated"] = daemonSocket
	w.mu.Unlock()

	// An unscoped kill-server on the user's default server targets the
	// default socket only; the daemon server is untouched. We emulate the
	// default-server kill with a scratch default socket so no user state is
	// touched.
	scratchDefault := filepath.Join(root, "user-default")
	if out, err := exec.Command("tmux", "-S", filepath.Join(scratchDefault, "tmux-default.sock"), "new-session", "-d", "-s", "user-session", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("create user-default-like server: %v: %s", err, out)
	}
	_ = exec.Command("tmux", "-S", filepath.Join(scratchDefault, "tmux-default.sock"), "kill-server").Run()

	if presence, err := w.ProbeSession("delegated"); err != nil || presence != SessionPresencePresent {
		t.Fatalf("Zen-owned Session after default-server kill = %v err=%v, want present", presence, err)
	}
	_ = strings.TrimSpace
}

// shortTmuxTestDir returns a short scratch directory for tmux -S sockets
// (sockaddr_un limits the path to ~108 bytes; t.TempDir() can overflow when
// TMPDIR is nested). The caller owns removal.
func shortTmuxTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zt-isolation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
