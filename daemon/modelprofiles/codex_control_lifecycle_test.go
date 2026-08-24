package modelprofiles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// spawnFakeAppServer starts a short-lived process whose command line looks
// like a codex app-server bound to socketPath (Linux /proc cmdline check).
// The returned channel closes when the process has been reaped (kill-0 alone
// stays true for zombies).
func spawnFakeAppServer(t *testing.T, socketPath string) (int, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, "sleep", "60")
	cmd.Args = []string{"codex app-server --listen unix://" + socketPath, "60"}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Start does not guarantee that process inspection observes the exec'd
	// command line yet. Do not invoke cleanup before its PID-reuse guard can
	// identify this child as the fake app-server.
	ready := time.NewTicker(time.Millisecond)
	defer ready.Stop()
	for !codexAppServerProcess(cmd.Process.Pid, socketPath) {
		select {
		case <-done:
			t.Fatal("fake app-server exited before its command line was observable")
		case <-ready.C:
		}
	}
	return cmd.Process.Pid, done
}

func waitReaped(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("app-server process not reaped")
	}
}

func writeControlArtifacts(t *testing.T, socketPath string, pid int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexControlPidPath(socketPath), []byte(strings.TrimSpace(strings.Join([]string{intString(pid)}, ""))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexControlLogPath(socketPath), []byte("logs"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func intString(v int) string {
	return strconv.Itoa(v)
}

func TestCleanupCodexControlArtifactsKillsAppServerAndRemovesFiles(t *testing.T) {
	root := t.TempDir()
	socketPath := filepath.Join(root, "codex-ctl-abc.sock")
	pid, done := spawnFakeAppServer(t, socketPath)
	writeControlArtifacts(t, socketPath, pid)
	if err := CleanupCodexControlArtifacts(socketPath); err != nil {
		t.Fatal(err)
	}
	// The app-server process was terminated (SIGTERM -> sleep exits).
	waitReaped(t, done)
	for _, path := range []string{socketPath, CodexControlPidPath(socketPath), CodexControlLogPath(socketPath)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %s not removed", path)
		}
	}
}

func TestCleanupCodexControlArtifactsSkipsUnrelatedLivePid(t *testing.T) {
	root := t.TempDir()
	socketPath := filepath.Join(root, "codex-ctl-abc.sock")
	// A live pid whose cmdline does NOT match the socket must not be killed.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	writeControlArtifacts(t, socketPath, cmd.Process.Pid)
	if err := CleanupCodexControlArtifacts(socketPath); err != nil {
		t.Fatal(err)
	}
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("unrelated live pid must not be killed")
	}
	// Files are still removed (the session is dead; artifacts are daemon-owned).
	for _, path := range []string{socketPath, CodexControlPidPath(socketPath), CodexControlLogPath(socketPath)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %s not removed", path)
		}
	}
}

func TestSweepCodexControlArtifactsKeepsLiveAppServerAndRemovesStale(t *testing.T) {
	root := t.TempDir()
	liveSocket := filepath.Join(root, "codex-ctl-live.sock")
	livePid, liveDone := spawnFakeAppServer(t, liveSocket)
	writeControlArtifacts(t, liveSocket, livePid)

	staleSocket := filepath.Join(root, "codex-ctl-stale.sock")
	stalePid, staleDone := spawnFakeAppServer(t, staleSocket)
	writeControlArtifacts(t, staleSocket, stalePid)
	// Point the stale session's pidfile at a definitely-dead pid.
	if err := os.WriteFile(CodexControlPidPath(staleSocket), []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}

	SweepCodexControlArtifacts(liveSocket)
	SweepCodexControlArtifacts(staleSocket)

	// The live app server keeps its socket/pid/log.
	if _, err := os.Stat(liveSocket); err != nil {
		t.Fatalf("live socket removed: %v", err)
	}
	if _, err := os.Stat(CodexControlPidPath(liveSocket)); err != nil {
		t.Fatalf("live pidfile removed: %v", err)
	}
	// The stale session's files are swept.
	for _, path := range []string{staleSocket, CodexControlPidPath(staleSocket), CodexControlLogPath(staleSocket)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale artifact %s not swept", path)
		}
	}
	if !processAlive(livePid) {
		t.Fatal("live app server must survive the sweep")
	}
	_ = stalePid
	_ = staleDone
	_ = liveDone
}
