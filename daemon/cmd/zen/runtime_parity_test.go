package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The user-facing runtime contract: `zen` executes the daemon; `zen-dev` builds
// and restarts the SAME runtime with the SAME arguments, identity and state
// ownership. These tests boot the real reviewed binaries with an isolated
// state directory and no worker environment, so they prove parity without
// touching the developer's live daemon, tmux server or Brain resources.

func parityModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func parityBuild(t *testing.T, root, pkg, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, output)
	}
}

func parityFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func parityEnv(home, tmuxDir string) []string {
	env := []string{
		"HOME=" + home,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/bin:/bin:/usr/lib/go/bin",
		"TMPDIR=" + home,
		"TMUX=",
		"TMUX_TMPDIR=" + tmuxDir,
		"XDG_RUNTIME_DIR=" + home + "/run",
		"LANG=C.UTF-8",
	}
	// The DEV runner shells out to `go build`; point Go at the real user
	// caches so read-only module cache directories never land in the isolated
	// HOME and temporary tree.
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOFLAGS", "GOPROXY"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	if realHome, err := os.UserHomeDir(); err == nil {
		env = append(env,
			"GOPATH="+filepath.Join(realHome, "go"),
			"GOMODCACHE="+filepath.Join(realHome, "go", "pkg", "mod"),
			"GOCACHE="+filepath.Join(realHome, ".cache", "go-build"),
		)
	}
	return env
}

type parityDaemon struct {
	cmd      *exec.Cmd
	done     chan error
	port     int
	tmuxSock string
}

func parityStart(t *testing.T, binary, cwd, stateDir, home, tmuxDir string, port int) *parityDaemon {
	t.Helper()
	cmd := exec.Command(binary, "-state-dir", stateDir, "-addr", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = cwd
	cmd.Env = parityEnv(home, tmuxDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	daemon := &parityDaemon{cmd: cmd, done: make(chan error, 1), port: port, tmuxSock: filepath.Join(tmuxDir, "tmux-1000", "default")}
	go func() { daemon.done <- cmd.Wait() }()
	return daemon
}

func parityHealth(t *testing.T, daemon *parityDaemon, timeout time.Duration) (string, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", daemon.port))
		if err == nil {
			var payload struct {
				DaemonID string `json:"daemon_id"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&payload)
			response.Body.Close()
			if decodeErr == nil && payload.DaemonID != "" {
				return payload.DaemonID, nil
			}
			lastErr = fmt.Errorf("invalid health payload: %v", decodeErr)
		} else {
			lastErr = err
		}
		select {
		case exitErr := <-daemon.done:
			return "", fmt.Errorf("daemon exited early: %v", exitErr)
		default:
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", fmt.Errorf("health timeout: %v", lastErr)
}

func parityStop(t *testing.T, daemon *parityDaemon) {
	t.Helper()
	_ = daemon.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-daemon.done:
	case <-time.After(10 * time.Second):
		_ = daemon.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-daemon.done:
		case <-time.After(5 * time.Second):
			_ = daemon.cmd.Process.Kill()
			<-daemon.done
		}
	}
}

// parityCleanupTmux kills any tmux server the daemon's Brain feature may have
// started under the isolated socket. The socket inode can remain after
// kill-server, so liveness is asserted with the `-N` query, never file bytes.
func parityCleanupTmux(tmuxSock string) {
	_ = exec.Command("tmux", "-S", tmuxSock, "kill-server").Run()
	time.Sleep(200 * time.Millisecond)
}

func parityTmuxDead(sock string) bool {
	out, err := exec.Command("tmux", "-S", sock, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == ""
}

// TestRuntimeParitySameIdentity proves the direct runtime and the DEV runner
// share one state/identity/port ownership path: the same state directory
// yields the same daemon_id, a second owner is rejected by the lifecycle lock,
// and identity survives a restart for both entry points.
func TestRuntimeParitySameIdentity(t *testing.T) {
	root := parityModuleRoot(t)
	work := t.TempDir()
	zen := filepath.Join(work, "zen")
	dev := filepath.Join(work, "zen-dev")
	parityBuild(t, root, "./cmd/zen", zen)
	parityBuild(t, root, "./cmd/zen-dev", dev)

	stateDir := filepath.Join(work, "state")
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	tmuxDir := filepath.Join(work, "tmux")
	if err := os.MkdirAll(tmuxDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// No tmux server exists before the first start; the runtime must not need one.
	if !parityTmuxDead(filepath.Join(tmuxDir, "tmux-1000", "default")) {
		t.Fatal("unexpected isolated tmux server before start")
	}

	variants := []struct {
		name string
		bin  string
	}{
		{"zen", zen},
		{"zen-dev", dev},
	}
	identities := map[string]string{}
	for _, variant := range variants {
		daemon := parityStart(t, variant.bin, root, stateDir, home, tmuxDir, parityFreePort(t))
		id, err := parityHealth(t, daemon, 30*time.Second)
		if err != nil {
			parityStop(t, daemon)
			parityCleanupTmux(daemon.tmuxSock)
			t.Fatalf("%s: %v", variant.name, err)
		}
		identities[variant.name] = id

		// A second owner against the same state directory must fail closed.
		second := exec.Command(variant.bin, "-state-dir", stateDir, "-addr", fmt.Sprintf("127.0.0.1:%d", parityFreePort(t)))
		second.Dir = root
		second.Env = parityEnv(home, tmuxDir)
		if err := second.Start(); err != nil {
			t.Fatalf("%s: second owner start: %v", variant.name, err)
		}
		secondErr := make(chan error, 1)
		go func() { secondErr <- second.Wait() }()
		select {
		case waitErr := <-secondErr:
			if waitErr == nil {
				t.Fatalf("%s: second owner was admitted", variant.name)
			}
		case <-time.After(30 * time.Second):
			_ = second.Process.Kill()
			t.Fatalf("%s: second owner did not exit", variant.name)
		}

		parityStop(t, daemon)
		parityCleanupTmux(daemon.tmuxSock)

		// Identity and state ownership survive a restart of the same entry point.
		restarted := parityStart(t, variant.bin, root, stateDir, home, tmuxDir, parityFreePort(t))
		restartedID, err := parityHealth(t, restarted, 30*time.Second)
		if err != nil {
			parityStop(t, restarted)
			parityCleanupTmux(restarted.tmuxSock)
			t.Fatalf("%s restart: %v", variant.name, err)
		}
		if restartedID != id {
			parityStop(t, restarted)
			parityCleanupTmux(restarted.tmuxSock)
			t.Fatalf("%s identity changed across restart: %s -> %s", variant.name, id, restartedID)
		}
		parityStop(t, restarted)
		parityCleanupTmux(restarted.tmuxSock)
	}

	if identities["zen"] == "" || identities["zen"] != identities["zen-dev"] {
		t.Fatalf("runtime identity differs between zen (%s) and zen-dev (%s)", identities["zen"], identities["zen-dev"])
	}
}
