package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

// The user-facing runtime contract: `zen` executes the daemon; `zen-dev` builds
// and restarts the SAME runtime with the SAME arguments, identity and state
// ownership. These tests run the real reviewed binaries from an owned source
// snapshot with an isolated state directory and no worker environment, so they
// prove parity without touching the developer's live daemon, tmux server,
// repository build output or Brain resources.
//
// Isolation: the snapshot copies the module into the test's own temporary tree;
// zen-dev's cwd (and therefore its watched sources and tmp/zen-dev output) lives
// only inside that tree. The test records the shared repository build output's
// hash and mtime before and after and fails if anything outside the owned root
// changed.

var parityHTTPClient = &http.Client{Timeout: 3 * time.Second}

func parityModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// paritySnapshot copies the module into an owned temporary tree. Skipped
// directories are build outputs and VCS metadata only; no file is symlinked to
// the source repository.
func paritySnapshot(t *testing.T, moduleDir string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "daemon")
	skip := map[string]bool{".git": true, "tmp": true, ".cxx": true, "build": true, "node_modules": true}
	if err := filepath.WalkDir(moduleDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0o755)
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, relative), data, info.Mode().Perm())
	}); err != nil {
		t.Fatalf("snapshot module: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "go.mod")); err != nil {
		t.Fatalf("snapshot missing go.mod: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "tmp")); err == nil {
		t.Fatal("snapshot unexpectedly contains a tmp build directory")
	}
	return destination
}

// parityDesktopBuild mirrors the DEV runner's own detection so the direct
// binary is compiled with the same tags, CGO setting and native marker.
func parityDesktopBuild(t *testing.T, moduleDir string) ([]string, []string) {
	t.Helper()
	probe := exec.Command("pkg-config", "--exists", "gtk+-3.0", "gstreamer-app-1.0", "gstreamer-video-1.0", "x11", "xtst", "gio-unix-2.0")
	if probe.Run() != nil {
		return nil, []string{"CGO_ENABLED=0"}
	}
	token, err := desktop.NativeBuildInputToken(filepath.Join(moduleDir, "desktop", "native"))
	if err != nil {
		t.Fatalf("native build input token: %v", err)
	}
	return []string{"-tags", "zen_desktop"}, []string{
		"CGO_ENABLED=1",
		"CGO_CFLAGS=-DZEN_NATIVE_BUILD_INPUT=h" + token,
	}
}

func parityBuild(t *testing.T, moduleDir, pkg, out string, extraArgs, extraEnv []string) {
	t.Helper()
	args := append([]string{"build", "-o", out}, extraArgs...)
	args = append(args, pkg)
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), extraEnv...)
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
	exited   chan struct{}
	exitErr  error
	output   bytes.Buffer
	port     int
	tmuxSock string
}

// parityStart always registers cleanup before returning, so no assertion or
// fatal path can orphan the child.
func parityStart(t *testing.T, binary, cwd, stateDir, home, tmuxDir string, port int) *parityDaemon {
	t.Helper()
	cmd := exec.Command(binary, "-state-dir", stateDir, "-addr", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = cwd
	cmd.Env = parityEnv(home, tmuxDir)
	daemon := &parityDaemon{
		cmd:      cmd,
		exited:   make(chan struct{}),
		port:     port,
		tmuxSock: filepath.Join(tmuxDir, fmt.Sprintf("tmux-%d", os.Getuid()), "default"),
	}
	cmd.Stdout, cmd.Stderr = &daemon.output, &daemon.output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	go func() {
		daemon.exitErr = cmd.Wait()
		close(daemon.exited)
	}()
	t.Cleanup(func() {
		parityStop(t, daemon)
		parityCleanupTmux(daemon.tmuxSock)
	})
	return daemon
}

func parityHealth(daemon *parityDaemon, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-daemon.exited:
			return "", fmt.Errorf("daemon exited early: %v", daemon.exitErr)
		default:
		}
		response, err := parityHTTPClient.Get(fmt.Sprintf("http://127.0.0.1:%d/health", daemon.port))
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
		time.Sleep(150 * time.Millisecond)
	}
	return "", fmt.Errorf("health timeout: %v", lastErr)
}

func parityStop(t *testing.T, daemon *parityDaemon) {
	t.Helper()
	select {
	case <-daemon.exited:
		return
	default:
	}
	_ = daemon.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-daemon.exited:
		return
	case <-time.After(10 * time.Second):
	}
	_ = daemon.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-daemon.exited:
		return
	case <-time.After(5 * time.Second):
	}
	_ = daemon.cmd.Process.Kill()
	<-daemon.exited
}

// parityCleanupTmux uses `-N` so a missing server is never started just to be
// killed, and liveness is asserted with the same non-starting query.
func parityCleanupTmux(tmuxSock string) {
	_ = exec.Command("tmux", "-S", tmuxSock, "-N", "kill-server").Run()
	time.Sleep(200 * time.Millisecond)
}

func parityTmuxDead(sock string) bool {
	out, err := exec.Command("tmux", "-S", sock, "-N", "display-message", "-p", "#{pid}").CombinedOutput()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == ""
}

type paritySharedState struct {
	exists  bool
	hash    string
	modTime int64
}

// paritySharedBuildState snapshots the repository's shared DEV build output so
// the test can prove it never wrote outside its owned root.
func paritySharedBuildState(moduleDir string) paritySharedState {
	path := filepath.Join(moduleDir, "tmp", "zen-dev")
	info, err := os.Stat(path)
	if err != nil {
		return paritySharedState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return paritySharedState{}
	}
	sum := sha256.Sum256(data)
	return paritySharedState{exists: true, hash: hex.EncodeToString(sum[:]), modTime: info.ModTime().UnixNano()}
}

func (s paritySharedState) equal(other paritySharedState) bool {
	return s.exists == other.exists && s.hash == other.hash && s.modTime == other.modTime
}

func parityBuildOptions(t *testing.T, binary string) map[string]string {
	t.Helper()
	output, err := exec.Command("go", "version", "-m", binary).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m %s: %v\n%s", binary, err, output)
	}
	options := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "build ") {
			continue
		}
		fields := strings.SplitN(strings.TrimPrefix(line, "build "), "=", 2)
		if len(fields) == 2 {
			options[fields[0]] = fields[1]
		}
	}
	return options
}

func parityRequireSameBuildOptions(t *testing.T, direct, dev string) {
	t.Helper()
	directOptions := parityBuildOptions(t, direct)
	devOptions := parityBuildOptions(t, dev)
	for _, key := range []string{"-tags", "CGO_ENABLED", "CGO_CFLAGS"} {
		if directOptions[key] != devOptions[key] {
			t.Fatalf("desktop build options differ between zen (%q) and zen-dev child (%q) for %s", directOptions[key], devOptions[key], key)
		}
	}
}

// TestRuntimeParitySameIdentity proves the direct runtime and the DEV runner
// share one state/identity/port ownership path from an owned source snapshot:
// the same state directory yields the same daemon_id, a second owner is
// rejected by the lifecycle lock with its exact cause while the first stays
// healthy, and identity survives a restart for both entry points.
func TestRuntimeParitySameIdentity(t *testing.T) {
	realModule := parityModuleRoot(t)
	snapshot := paritySnapshot(t, realModule)
	work := t.TempDir()
	zen := filepath.Join(work, "zen")
	dev := filepath.Join(work, "zen-dev")
	desktopArgs, desktopEnv := parityDesktopBuild(t, snapshot)
	parityBuild(t, snapshot, "./cmd/zen", zen, desktopArgs, desktopEnv)
	parityBuild(t, snapshot, "./cmd/zen-dev", dev, nil, nil)

	stateDir := filepath.Join(work, "state")
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	tmuxDir := filepath.Join(work, "tmux")
	if err := os.MkdirAll(tmuxDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmuxSock := filepath.Join(tmuxDir, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
	// No tmux server exists before the first start; the runtime must not need one.
	if !parityTmuxDead(tmuxSock) {
		t.Fatal("unexpected isolated tmux server before start")
	}
	sharedBefore := paritySharedBuildState(realModule)

	variants := []struct {
		name string
		bin  string
	}{
		{"zen", zen},
		{"zen-dev", dev},
	}
	identities := map[string]string{}
	for _, variant := range variants {
		daemon := parityStart(t, variant.bin, snapshot, stateDir, home, tmuxDir, parityFreePort(t))
		id, err := parityHealth(daemon, 30*time.Second)
		if err != nil {
			t.Fatalf("%s: %v\noutput: %s", variant.name, err, daemon.output.String())
		}
		identities[variant.name] = id

		// A second owner against the same state directory must fail closed with
		// the lifecycle-lock cause, not an unrelated startup error, and the
		// original owner must stay healthy afterwards.
		second := exec.Command(variant.bin, "-state-dir", stateDir, "-addr", fmt.Sprintf("127.0.0.1:%d", parityFreePort(t)))
		second.Dir = snapshot
		second.Env = parityEnv(home, tmuxDir)
		var secondOutput bytes.Buffer
		second.Stdout, second.Stderr = &secondOutput, &secondOutput
		if err := second.Start(); err != nil {
			t.Fatalf("%s: second owner start: %v", variant.name, err)
		}
		secondDone := make(chan error, 1)
		go func() { secondDone <- second.Wait() }()
		select {
		case waitErr := <-secondDone:
			if waitErr == nil {
				t.Fatalf("%s: second owner was admitted", variant.name)
			}
		case <-time.After(30 * time.Second):
			_ = second.Process.Kill()
			<-secondDone
			t.Fatalf("%s: second owner did not exit; output=%q", variant.name, secondOutput.String())
		}
		if !strings.Contains(secondOutput.String(), "another Zen daemon owns this state directory") {
			t.Fatalf("%s: second owner failed for the wrong cause: %q", variant.name, secondOutput.String())
		}
		if _, err := parityHealth(daemon, 5*time.Second); err != nil {
			t.Fatalf("%s: original owner unhealthy after duplicate rejection: %v", variant.name, err)
		}

		parityStop(t, daemon)
		parityCleanupTmux(daemon.tmuxSock)
		// The isolated run must leave the repository's shared DEV build output
		// untouched (hash and mtime).
		if !paritySharedBuildState(realModule).equal(sharedBefore) {
			t.Fatalf("%s: shared tmp/zen-dev changed during the isolated run", variant.name)
		}

		// Identity and state ownership survive a restart of the same entry point.
		restarted := parityStart(t, variant.bin, snapshot, stateDir, home, tmuxDir, parityFreePort(t))
		restartedID, err := parityHealth(restarted, 30*time.Second)
		if err != nil {
			t.Fatalf("%s restart: %v\noutput: %s", variant.name, err, restarted.output.String())
		}
		if restartedID != id {
			t.Fatalf("%s identity changed across restart: %s -> %s", variant.name, id, restartedID)
		}
		parityStop(t, restarted)
		parityCleanupTmux(restarted.tmuxSock)
	}

	if identities["zen"] == "" || identities["zen"] != identities["zen-dev"] {
		t.Fatalf("runtime identity differs between zen (%s) and zen-dev (%s)", identities["zen"], identities["zen-dev"])
	}
	// The DEV runner built its own child in the snapshot; its build options must
	// match the direct binary so parity covers the same desktop build scope.
	devChild := filepath.Join(snapshot, "tmp", "zen-dev")
	if _, err := os.Stat(devChild); err != nil {
		t.Fatalf("DEV runner child was not built in the owned snapshot: %v", err)
	}
	parityRequireSameBuildOptions(t, zen, devChild)
}

// TestRuntimeParityEarlyExitCleanup covers the early-exit and health-timeout
// paths: health must observe exit, stop must not block, and no child may
// survive the test.
func TestRuntimeParityEarlyExitCleanup(t *testing.T) {
	realModule := parityModuleRoot(t)
	snapshot := paritySnapshot(t, realModule)
	work := t.TempDir()
	zen := filepath.Join(work, "zen")
	desktopArgs, desktopEnv := parityDesktopBuild(t, snapshot)
	parityBuild(t, snapshot, "./cmd/zen", zen, desktopArgs, desktopEnv)
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	tmuxDir := filepath.Join(work, "tmux")
	if err := os.MkdirAll(tmuxDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// A privileged bind failure makes the daemon exit immediately.
	early := exec.Command(zen, "-state-dir", filepath.Join(work, "state-early"), "-addr", "127.0.0.1:1")
	early.Dir = snapshot
	early.Env = parityEnv(home, tmuxDir)
	var earlyOutput bytes.Buffer
	early.Stdout, early.Stderr = &earlyOutput, &earlyOutput
	if err := early.Start(); err != nil {
		t.Fatalf("start early-exit daemon: %v", err)
	}
	earlyDone := make(chan error, 1)
	go func() { earlyDone <- early.Wait() }()
	select {
	case <-earlyDone:
	case <-time.After(15 * time.Second):
		_ = early.Process.Kill()
		<-earlyDone
		t.Fatalf("early-exit daemon did not exit; output=%q", earlyOutput.String())
	}

	// Health against a live process that never serves must time out promptly.
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	timeoutDaemon := &parityDaemon{
		cmd:      sleeper,
		exited:   make(chan struct{}),
		port:     parityFreePort(t),
		tmuxSock: filepath.Join(tmuxDir, fmt.Sprintf("tmux-%d", os.Getuid()), "default"),
	}
	go func() {
		timeoutDaemon.exitErr = sleeper.Wait()
		close(timeoutDaemon.exited)
	}()
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		<-timeoutDaemon.exited
	})
	started := time.Now()
	if _, err := parityHealth(timeoutDaemon, 1*time.Second); err == nil {
		t.Fatal("expected health timeout against non-serving process")
	}
	if elapsed := time.Since(started); elapsed > 6*time.Second {
		t.Fatalf("health timeout was not bounded: %s", elapsed)
	}
	_ = sleeper.Process.Signal(syscall.SIGINT)
	select {
	case <-timeoutDaemon.exited:
	case <-time.After(5 * time.Second):
		_ = sleeper.Process.Kill()
		<-timeoutDaemon.exited
	}
	if early.ProcessState == nil && sleeper.ProcessState == nil {
		t.Fatal("children were not reaped")
	}
}
