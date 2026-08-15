package modelprofiles

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CodexControlPidPath returns the daemon-owned pid file for a Session's Codex
// app-server control socket. The launch wrapper writes the app-server PID here
// so Session teardown can kill an orphaned app server and startup can sweep
// stale artifacts.
func CodexControlPidPath(socketPath string) string {
	return strings.TrimSpace(socketPath) + ".pid"
}

// CodexControlLogPath returns the per-session app-server log path.
func CodexControlLogPath(socketPath string) string {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return ""
	}
	return strings.TrimSuffix(socketPath, filepath.Ext(socketPath)) + ".log"
}

// CleanupCodexControlArtifacts terminates a Session's Codex app-server (via
// the recorded pid, verified against the process cmdline to guard against pid
// reuse) and removes the daemon-owned socket, pid, and log files. Idempotent
// and safe to call after the Session is confirmed dead.
func CleanupCodexControlArtifacts(socketPath string) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil
	}
	pidPath := CodexControlPidPath(socketPath)
	if raw, err := os.ReadFile(pidPath); err == nil {
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && pid > 0 && codexAppServerProcess(pid, socketPath) {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			for i := 0; i < 30 && processAlive(pid); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if processAlive(pid) && codexAppServerProcess(pid, socketPath) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
	_ = os.Remove(pidPath)
	_ = os.Remove(socketPath)
	_ = os.Remove(CodexControlLogPath(socketPath))
	return nil
}

// SweepCodexControlArtifacts removes stale daemon-owned app-server artifacts
// for a restored Session binding: when the recorded app-server pid is dead or
// no longer matches this socket, the socket/pid/log files are daemon-owned
// leftovers and are removed. A live matching app server is left untouched so
// the restored Session keeps its native control surface after a daemon
// restart.
func SweepCodexControlArtifacts(socketPath string) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return
	}
	pidPath := CodexControlPidPath(socketPath)
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil || pid <= 0 {
		removeCodexControlFiles(socketPath)
		return
	}
	if !processAlive(pid) || !codexAppServerProcess(pid, socketPath) {
		removeCodexControlFiles(socketPath)
	}
}

func removeCodexControlFiles(socketPath string) {
	_ = os.Remove(CodexControlPidPath(socketPath))
	_ = os.Remove(socketPath)
	_ = os.Remove(CodexControlLogPath(socketPath))
}

// processAlive reports whether pid exists (signal 0 probe).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// codexAppServerProcess verifies that pid is the codex app-server for the
// given control socket by inspecting its command line. This guards kill/sweep
// against pid reuse.
func codexAppServerProcess(pid int, socketPath string) bool {
	args := processCmdline(pid)
	if args == "" {
		return false
	}
	return strings.Contains(args, "app-server") && strings.Contains(args, socketPath)
}

// processCmdline returns the process command line (Linux /proc; portable ps
// fallback). Empty when unreadable.
func processCmdline(pid int) string {
	if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil {
		return strings.ReplaceAll(string(raw), "\x00", " ")
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
