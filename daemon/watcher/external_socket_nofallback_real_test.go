package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestExternalSocketNeverForksFallbackServer drives the real production
// CreateSession path against an externally owned socket through its full
// lifecycle: absent -> fixture bootstrap -> dead -> fixture restart. The
// production client must fail honestly when the server is gone and must never
// create a fallback server in its own process tree.
func TestExternalSocketNeverForksFallbackServer(t *testing.T) {
	requireTmux(t)
	root := shortTmuxTestDir(t)
	installIsolatedTmuxShim(t, root)
	socket := filepath.Join(root, "external.sock")
	other := filepath.Join(root, "unrelated.sock")
	scratch := filepath.Join(root, "provider-tmux")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	w := New(10 * time.Millisecond)
	w.SetTmuxServer(socket, scratch)
	t.Cleanup(func() {
		stopHarnessTmuxServer(t, os.Getenv("ZEN_TEST_REAL_TMUX"), socket)
		stopHarnessTmuxServer(t, os.Getenv("ZEN_TEST_REAL_TMUX"), other)
	})

	create := func() (string, error) {
		return w.CreateSession("", CreateSessionOptions{
			Name: "nofallback", Command: "exec /bin/sh", Detached: true,
		})
	}
	assertNoServer := func(stage string) {
		t.Helper()
		// Oracle is server liveness, not the socket file: tmux kill-server
		// leaves a stale socket inode behind. -N fails clean without
		// forking; a fallback fork would answer with a PID instead.
		if out, err := exec.Command("tmux", "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput(); err == nil {
			t.Fatalf("%s: probe answered %q (fallback server forked)", stage, strings.TrimSpace(string(out)))
		}
	}
	waitServerGone := func(stage string, pid int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			proc, findErr := os.FindProcess(pid)
			sigErr := proc.Signal(syscall.Signal(0))
			_, probeErr := exec.Command("tmux", "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
			if (findErr != nil || sigErr != nil) && probeErr != nil {
				return
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("%s: server pid %d still alive or answering", stage, pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	serverPid := func() int {
		t.Helper()
		out, err := exec.Command("tmux", "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
		if err != nil {
			t.Fatalf("server pid probe: %v: %s", err, out)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(out)))
		if parseErr != nil || pid <= 0 {
			t.Fatalf("invalid server pid %q", out)
		}
		return pid
	}

	// 1. Absent server: honest failure, nothing forked.
	if target, err := create(); err == nil {
		_ = w.KillSession(target)
		t.Fatal("CreateSession on absent server succeeded, want unavailable error")
	}
	assertNoServer("absent server")

	// 2. Fixture-owned bootstrap (raw, explicit): production creation works.
	bootstrapHarnessServer(t, socket)
	target, err := create()
	if err != nil {
		t.Fatalf("CreateSession on fixture server: %v", err)
	}
	if err := w.KillSession(target); err != nil {
		t.Fatalf("close owned target: %v", err)
	}

	// 3. Unrelated second server is preserved throughout.
	bootstrapHarnessServer(t, other)
	otherPane := createHarnessPane(t, other, "unrelated-keep", "exec /bin/sh")
	_ = otherPane

	// 4. Server dies: next production spawn fails without fallback.
	pid := serverPid()
	if out, err := exec.Command("tmux", "-S", socket, "kill-server").CombinedOutput(); err != nil {
		t.Fatalf("kill fixture server: %v: %s", err, out)
	}
	waitServerGone("fixture server exit", pid)
	if target, err := create(); err == nil {
		_ = w.KillSession(target)
		t.Fatal("CreateSession on dead server succeeded, want unavailable error")
	}
	assertNoServer("dead server")

	// 5. Fixture restarts server: creation works again, unrelated intact.
	bootstrapHarnessServer(t, socket)
	target, err = create()
	if err != nil {
		t.Fatalf("CreateSession after fixture restart: %v", err)
	}
	if err := w.KillSession(target); err != nil {
		t.Fatalf("close recreated target: %v", err)
	}
	if out, err := exec.Command("tmux", "-S", other, "display-message", "-p", "-t", "unrelated-keep", "#{session_name}").Output(); err != nil || strings.TrimSpace(string(out)) != "unrelated-keep" {
		t.Fatalf("unrelated server disturbed: %q err=%v", out, err)
	}

	// 6. After-last-session removal: explicit kill of every session exits the
	// server (default exit-empty), and production creation fails again.
	pid = serverPid()
	if out, err := exec.Command("tmux", "-S", socket, "kill-session", "-t", "harness-keeper").CombinedOutput(); err != nil {
		t.Fatalf("kill keeper session: %v: %s", err, out)
	}
	waitServerGone("last-session server exit", pid)
	if target, err := create(); err == nil {
		_ = w.KillSession(target)
		t.Fatal("CreateSession after last-session exit succeeded, want unavailable error")
	}
	assertNoServer("after last-session exit")

	// 7. No live user server mutation: the ambient default socket is untouched
	// by every step above (all commands carried explicit -S under the root).
	if _, err := os.Stat("/tmp/tmux-1000/default"); err != nil {
		t.Fatalf("live default socket disturbed: %v", err)
	}
}
