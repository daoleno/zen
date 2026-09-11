package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func requireRealTmux(t *testing.T) string {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not available")
	}
	return realTmux
}

func privateSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zt-view-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestViewBootstrapNeverForksFallbackServer drives the real production view
// bootstrap command against an externally owned socket: absent -> fixture
// bootstrap -> dead -> restart. The client must fail honestly without
// creating a fallback server.
func TestViewBootstrapNeverForksFallbackServer(t *testing.T) {
	realTmux := requireRealTmux(t)
	root := privateSocketDir(t)
	socket := filepath.Join(root, "view.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bootstrap := func() {
		t.Helper()
		if out, err := exec.Command(realTmux, "-S", socket, "new-session", "-d", "-s", "view-keeper", "-x", "80", "-y", "24", "sleep 300").CombinedOutput(); err != nil {
			t.Fatalf("bootstrap fixture server: %v: %s", err, out)
		}
	}
	assertNoServer := func(stage string) {
		t.Helper()
		// Oracle is server liveness, not the socket file: tmux kill-server
		// leaves a stale socket inode behind. -N fails clean without
		// forking; a fallback fork would answer with a PID instead.
		if out, err := exec.Command(realTmux, "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput(); err == nil {
			t.Fatalf("%s: probe answered %q (fallback server forked)", stage, strings.TrimSpace(string(out)))
		}
	}
	serverPid := func() int {
		t.Helper()
		out, err := exec.Command(realTmux, "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
		if err != nil {
			t.Fatalf("server pid probe: %v: %s", err, out)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(out)))
		if parseErr != nil || pid <= 0 {
			t.Fatalf("invalid server pid %q", out)
		}
		return pid
	}
	waitServerGone := func(stage string, pid int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			proc, findErr := os.FindProcess(pid)
			sigErr := proc.Signal(syscall.Signal(0))
			_, probeErr := exec.Command(realTmux, "-S", socket, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
			if (findErr != nil || sigErr != nil) && probeErr != nil {
				return
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("%s: server pid %d still alive or answering", stage, pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Cleanup(func() {
		_ = exec.Command(realTmux, "-S", socket, "kill-server").Run()
	})

	// 1. Absent server: honest failure, nothing forked.
	if out, err := tmuxNewViewSessionCommand(ctx, socket, "zen-view-absent").Output(); err == nil {
		_ = exec.Command(realTmux, "-S", socket, "kill-session", "-t", strings.TrimSpace(string(out))).Run()
		t.Fatal("view bootstrap on absent server succeeded, want unavailable error")
	}
	assertNoServer("absent server")

	// 2. Fixture bootstrap: same production path works.
	bootstrap()
	out, err := tmuxNewViewSessionCommand(ctx, socket, "zen-view-live").Output()
	if err != nil {
		t.Fatalf("view bootstrap on fixture server: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("view bootstrap returned empty window target")
	}
	killTmuxSessionBounded(socket, "zen-view-live")

	// 3. Server dies: next bootstrap fails without fallback.
	pid := serverPid()
	if out, err := exec.Command(realTmux, "-S", socket, "kill-server").CombinedOutput(); err != nil {
		t.Fatalf("kill fixture server: %v: %s", err, out)
	}
	waitServerGone("fixture server exit", pid)
	if out, err := tmuxNewViewSessionCommand(ctx, socket, "zen-view-dead").Output(); err == nil {
		_ = exec.Command(realTmux, "-S", socket, "kill-session", "-t", strings.TrimSpace(string(out))).Run()
		t.Fatal("view bootstrap on dead server succeeded, want unavailable error")
	}
	assertNoServer("dead server")

	// 4. Fixture restart: bootstrap works again.
	bootstrap()
	out, err = tmuxNewViewSessionCommand(ctx, socket, "zen-view-again").Output()
	if err != nil {
		t.Fatalf("view bootstrap after fixture restart: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("view bootstrap after restart returned empty window target")
	}
	killTmuxSessionBounded(socket, "zen-view-again")
}
