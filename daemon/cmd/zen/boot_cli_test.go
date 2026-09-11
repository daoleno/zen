package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/control"
)

const (
	bootIsActive     = "systemctl --user is-active zen.service"
	bootIsEnabled    = "systemctl --user is-enabled zen.service"
	bootMainPID      = "systemctl --user show zen.service -p MainPID --value"
	bootFragment     = "systemctl --user show zen.service -p FragmentPath --value"
	bootControlGroup = "systemctl --user show zen.service -p ControlGroup --value"
	bootReload       = "systemctl --user daemon-reload"
	bootEnable       = "systemctl --user enable zen.service"
	bootEnableNow    = "systemctl --user enable --now zen.service"
	bootRestart      = "systemctl --user restart zen.service"
	bootStop         = "systemctl --user stop zen.service"
	bootDisable      = "systemctl --user disable zen.service"
)

type fakeBootRunner struct {
	mu        sync.Mutex
	calls     [][]string
	responses map[string]string
	failures  map[string]bool
	handlers  map[string]func()
	sequences map[string][]string
}

func (f *fakeBootRunner) run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	key := strings.Join(call, " ")
	f.mu.Lock()
	f.calls = append(f.calls, call)
	handler := f.handlers[key]
	sequenceValue := ""
	hasSequence := false
	if values := f.sequences[key]; len(values) > 0 {
		sequenceValue = values[0]
		f.sequences[key] = values[1:]
		hasSequence = true
	}
	f.mu.Unlock()
	if handler != nil {
		handler()
	}
	if hasSequence {
		return []byte(sequenceValue), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures[key] {
		return []byte(f.responses[key]), errors.New("fake failure")
	}
	return []byte(f.responses[key]), nil
}

func (f *fakeBootRunner) set(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[key] = value
}

func (f *fakeBootRunner) fail(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[key] = true
	f.responses[key] = value
}

func (f *fakeBootRunner) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if strings.Join(call, " ") == key {
			count++
		}
	}
	return count
}

func (f *fakeBootRunner) called(key string) bool {
	return f.count(key) > 0
}

func (f *fakeBootRunner) on(key string, handler func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[key] = handler
}

// push queues one response ahead of the static response for key.
func (f *fakeBootRunner) push(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sequences[key] = append(f.sequences[key], value)
}

func (f *fakeBootRunner) setSequence(key string, values ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sequences[key] = append([]string(nil), values...)
}

func (f *fakeBootRunner) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func bootTestRunner() *fakeBootRunner {
	return &fakeBootRunner{
		responses: map[string]string{},
		failures:  map[string]bool{},
		handlers:  map[string]func(){},
		sequences: map[string][]string{},
	}
}

func newBootTestEnvironment(t *testing.T) (bootConfig, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("HOME", home)
	binary := filepath.Join(home, "zen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(home, "work dir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return bootConfig{
		Binary:   binary,
		StateDir: filepath.Join(home, ".zen"),
		Addr:     bootDefaultAddr,
		WorkDir:  workDir,
		PathEnv:  "/usr/local/bin:/usr/bin:/bin",
		Home:     home,
	}, home
}

// bootWriteIdentity writes a real daemon identity file and returns its daemon id.
func bootWriteIdentity(t *testing.T, stateDir string) string {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"private_key_hex": hex.EncodeToString(private)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "identity.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])
}

func bootHoldStateLock(t *testing.T, stateDir string) {
	t.Helper()
	lock, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("hold state lock: acquired=%t err=%v", acquired, err)
	}
	t.Cleanup(func() { _ = lock.Close() })
}

func bootStartHealthServer(t *testing.T, daemonID string) string {
	t.Helper()
	return bootStartHealthServerOn(t, "127.0.0.1:0", daemonID)
}

func bootStartHealthServerOn(t *testing.T, addr, daemonID string) string {
	t.Helper()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":    "ok",
			"daemon_id": daemonID,
		})
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

func bootFreeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func bootCurrentCgroup(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 3 && strings.TrimSpace(fields[2]) != "" {
			return strings.TrimSpace(fields[2])
		}
	}
	t.Fatal("no cgroup path for the test process")
	return ""
}

func bootWriteInstalledUnit(t *testing.T, config bootConfig) (string, string) {
	t.Helper()
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootWriteFileAtomic(path, []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
	metadataPath := bootMetadataPath(path)
	if err := writeBootMetadata(metadataPath, bootMetadataFor(config)); err != nil {
		t.Fatal(err)
	}
	return path, metadataPath
}

// bootInstallEnvironment returns a config whose state is owned by a live
// identity/health/state-lock fixture and a fake runner that reports the unit as
// active with this test process as MainPID, so install verification succeeds.
func bootInstallEnvironment(t *testing.T) (bootConfig, *fakeBootRunner) {
	t.Helper()
	config, _ := newBootTestEnvironment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config.Binary = executable
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootStartHealthServer(t, daemonID)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "active")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	runner.set(bootIsEnabled, "enabled")
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	runner.set(bootFragment, path)
	runner.set(bootControlGroup, bootCurrentCgroup(t))
	runner.set("loginctl show-user "+currentUserName()+" -p Linger --value", "yes")
	return config, runner
}

// bootFreshInstallEnvironment models a first install: the unit is inactive and
// nothing owns the state until `systemctl --user enable --now` starts the
// daemon (the fake lock acquisition stands in for that daemon).
func bootFreshInstallEnvironment(t *testing.T) (bootConfig, *fakeBootRunner) {
	t.Helper()
	config, _ := newBootTestEnvironment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config.Binary = executable
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootFreeLoopbackAddr(t)

	runner := bootTestRunner()
	runner.set(bootIsActive, "active")
	runner.push(bootIsActive, "inactive")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	runner.set(bootIsEnabled, "enabled")
	runner.set(bootFragment, mustBootUnitPath(t))
	runner.set(bootControlGroup, bootCurrentCgroup(t))
	runner.set("loginctl show-user "+currentUserName()+" -p Linger --value", "yes")
	var lock *control.LifecycleLock
	runner.on(bootEnableNow, func() {
		bootStartHealthServerOn(t, config.Addr, daemonID)
		acquired, acquiredOK, err := control.TryAcquireLifecycleLock(config.StateDir)
		if err != nil || !acquiredOK {
			return
		}
		lock = acquired
	})
	t.Cleanup(func() {
		if lock != nil {
			_ = lock.Close()
		}
	})
	return config, runner
}

func TestResolveBootConfigDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")

	config, err := parseBootConfig("zen boot install", nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.StateDir != filepath.Join(home, ".zen") {
		t.Fatalf("state dir = %s", config.StateDir)
	}
	if config.Addr != bootDefaultAddr {
		t.Fatalf("addr = %s", config.Addr)
	}
	if config.WorkDir != work {
		t.Fatalf("work dir = %s", config.WorkDir)
	}
	if config.Home != home {
		t.Fatalf("home = %s", config.Home)
	}
	if !filepath.IsAbs(config.Binary) {
		t.Fatalf("binary is not absolute: %s", config.Binary)
	}
	if config.PathEnv != "/usr/local/bin:/usr/bin:/bin" {
		t.Fatalf("path = %s", config.PathEnv)
	}
}

func TestResolveBootConfigRelativePathsAndHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	if err := os.MkdirAll(filepath.Join(work, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "bin", "zen"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	config, err := parseBootConfig("zen boot install", []string{
		"-binary", "./bin/zen",
		"-state-dir", "state",
		"-work-dir", ".",
		"-addr", "127.0.0.1:1234",
		"-path-env", "./bin:~/.local/bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Binary != filepath.Join(work, "bin", "zen") {
		t.Fatalf("binary = %s", config.Binary)
	}
	if config.StateDir != filepath.Join(work, "state") {
		t.Fatalf("state dir = %s", config.StateDir)
	}
	if config.WorkDir != work {
		t.Fatalf("work dir = %s", config.WorkDir)
	}
	wantPath := filepath.Join(work, "bin") + string(os.PathListSeparator) + filepath.Join(home, ".local", "bin")
	if config.PathEnv != wantPath {
		t.Fatalf("path = %s, want %s", config.PathEnv, wantPath)
	}
}

func TestParseBootConfigRejectsLANWithAddr(t *testing.T) {
	if _, err := parseBootConfig("zen boot install", []string{"-lan", "-addr", "127.0.0.1:9999"}); err == nil {
		t.Fatal("lan plus explicit addr was accepted")
	}
}

func TestParseBootConfigRejectsInvalidAddress(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "127.0.0.1:", "no-port"} {
		if _, err := parseBootConfig("zen boot install", []string{"-addr", value}); err == nil {
			t.Fatalf("invalid address %q was accepted", value)
		}
	}
}

func TestRenderBootUnitRejectsControlCharacters(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.StateDir = "/tmp/state\nwith-newline"
	if _, err := renderBootUnit(config); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("newline in state dir not rejected: %v", err)
	}
	config.StateDir = "/tmp/state"
	config.PathEnv = "/usr/bin\n"
	if _, err := renderBootUnit(config); err == nil {
		t.Fatal("newline in PATH not rejected")
	}
}

func TestSystemdEscaping(t *testing.T) {
	execCases := []struct{ in, want string }{
		{"plain", `"plain"`},
		{"with space", `"with space"`},
		{"", `""`},
		{"dollar$sign", `"dollar$$sign"`},
		{"percent%sign", `"percent%%sign"`},
		{`back\slash`, `"back\\slash"`},
		{`quote"sign`, `"quote\"sign"`},
		{`mix $%\"' end`, `"mix $$%%\\\"' end"`},
		{"-leading-dash", `"-leading-dash"`},
	}
	for _, item := range execCases {
		if got := systemdExecArgument(item.in); got != item.want {
			t.Errorf("systemdExecArgument(%q) = %q, want %q", item.in, got, item.want)
		}
	}
	pathCases := []struct{ in, want string }{
		{"plain path", "plain path"},
		{"percent%sign", "percent%%sign"},
		{"dollar$sign", "dollar$sign"},
		{`back\slash`, `back\slash`},
		{`quote"sign`, `quote"sign`},
	}
	for _, item := range pathCases {
		if got := systemdPathValue(item.in); got != item.want {
			t.Errorf("systemdPathValue(%q) = %q, want %q", item.in, got, item.want)
		}
	}
	envCases := []struct{ in, want string }{
		{"plain", "plain"},
		{"with space", "with space"},
		{"percent%sign", "percent%%sign"},
		{"dollar$sign", "dollar$sign"},
		{`back\slash`, `back\\slash`},
		{`quote"sign`, `quote\"sign`},
	}
	for _, item := range envCases {
		if got := systemdEnvironmentValue(item.in); got != item.want {
			t.Errorf("systemdEnvironmentValue(%q) = %q, want %q", item.in, got, item.want)
		}
	}
}

func TestRenderBootUnitContract(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.WorkDir = "/tmp/work %dir"
	config.StateDir = `/tmp/state %$"\ dir`
	config.PathEnv = `/opt/go bin:/usr/bin/%s`
	config.Binary = `/opt/zen bin/zen%$"\ file`
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		bootManagedMarker,
		"[Unit]",
		"[Service]",
		"Type=simple",
		`WorkingDirectory=/tmp/work %%dir`,
		`ExecStart="/opt/zen bin/zen%%$$\"\\ file" -state-dir "/tmp/state %%$$\"\\ dir" -addr "127.0.0.1:9876"`,
		`Environment="HOME=` + strings.ReplaceAll(config.Home, `%`, `%%`) + `"`,
		`Environment="PATH=/opt/go bin:/usr/bin/%%s"`,
		"Restart=on-failure",
		"RestartSec=5",
		"KillMode=process",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, line+"\n") {
			t.Fatalf("unit missing %q:\n%s", line, unit)
		}
	}
	if strings.Contains(unit, "tmux") || strings.Contains(unit, "zen-tmux") {
		t.Fatalf("boot unit must not manage tmux:\n%s", unit)
	}
}

func TestRenderBootUnitHasNoOrderingCycle(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	inUnitSection := false
	for _, line := range strings.Split(unit, "\n") {
		switch line {
		case "[Unit]":
			inUnitSection = true
			continue
		case "[Service]":
			inUnitSection = false
		}
		if !inUnitSection {
			continue
		}
		for _, directive := range []string{"After=", "Before=", "Wants=", "Requires="} {
			if strings.HasPrefix(line, directive) && strings.Contains(line, "default.target") {
				t.Fatalf("unit orders against its install target: %q", line)
			}
		}
	}
	if !strings.Contains(unit, "WantedBy=default.target\n") {
		t.Fatalf("unit is not enabled into default.target:\n%s", unit)
	}
}

func TestRenderBootUnitLAN(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.LAN = true
	config.Addr = bootLANAddr
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `-state-dir "`+config.StateDir+`" -lan`) {
		t.Fatalf("LAN unit does not render -lan:\n%s", unit)
	}
	if strings.Contains(unit, "-addr") {
		t.Fatalf("LAN unit must not also pass -addr:\n%s", unit)
	}
}

func TestBootRenderedUnitParsesWithSystemdAnalyze(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("user units are Linux-only")
	}
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	base := t.TempDir()
	binary := filepath.Join(base, "zen bin dir", "zen")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(base, "work %dir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := bootConfig{
		Binary:   binary,
		StateDir: filepath.Join(base, `state %$"\ dir`),
		Addr:     bootDefaultAddr,
		WorkDir:  workDir,
		PathEnv:  filepath.Join(base, "go bin") + ":/usr/bin:/bin",
		Home:     filepath.Join(base, `home %$"\ dir`),
	}
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, bootServiceName)
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(analyze, "verify", path).CombinedOutput()
	if err != nil {
		t.Fatalf("systemd-analyze verify rejected the rendered unit: %v\n%s\nunit:\n%s", err, output, unit)
	}
	if strings.Contains(string(output), "bad unit file setting") {
		t.Fatalf("systemd-analyze reported a bad setting:\n%s\nunit:\n%s", output, unit)
	}
	// The same escaping rules must hold for the LAN form.
	lanUnit, err := renderBootUnit(bootConfig{
		Binary:   binary,
		StateDir: config.StateDir,
		Addr:     bootLANAddr,
		LAN:      true,
		WorkDir:  workDir,
		PathEnv:  config.PathEnv,
		Home:     config.Home,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(lanUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(analyze, "verify", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd-analyze verify rejected the LAN unit: %v\n%s\nunit:\n%s", err, output, lanUnit)
	}
}

func TestBootInstallWritesManagedUnitAndEnables(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	var out bytes.Buffer
	if err := bootInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != unit {
		t.Fatalf("unit file does not match the rendered contract:\n%s", data)
	}
	metadata, err := readBootMetadata(bootMetadataPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.same(config) {
		t.Fatalf("metadata does not record the installed config: %+v", metadata)
	}
	if !runner.called(bootReload) {
		t.Fatal("daemon-reload not called")
	}
	if !runner.called(bootEnableNow) {
		t.Fatal("enable --now not called")
	}
	if !strings.Contains(out.String(), "Installed and started") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Address: "+config.Addr) {
		t.Fatalf("output does not report the installed address: %s", out.String())
	}
}

func TestBootInstallIdempotentDoesNotRestart(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	if err := bootInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	var out bytes.Buffer
	if err := bootInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if runner.called(bootRestart) {
		t.Fatal("idempotent reinstall restarted a healthy daemon")
	}
	if runner.called(bootReload) {
		t.Fatal("idempotent reinstall reloaded systemd without a unit change")
	}
	if runner.called(bootEnableNow) {
		t.Fatal("idempotent reinstall used enable --now")
	}
	if !runner.called(bootEnable) {
		t.Fatal("idempotent reinstall did not ensure the unit is enabled")
	}
	if !strings.Contains(out.String(), "Already installed and running") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestBootInstallChangedCommandRestarts(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	if err := bootInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	config.WorkDir = filepath.Join(config.Home, "other work")
	if err := os.MkdirAll(config.WorkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bootInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootRestart) {
		t.Fatal("changed command did not restart the live unit")
	}
	if !runner.called(bootReload) {
		t.Fatal("changed unit was not reloaded")
	}
	if !strings.Contains(out.String(), "Updated and restarted") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	metadata, err := readBootMetadata(bootMetadataPath(mustBootUnitPath(t)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WorkDir != config.WorkDir {
		t.Fatalf("metadata kept the old working directory: %+v", metadata)
	}
}

func TestBootInstallEnvironmentChangeRestarts(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	if err := bootInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	config.PathEnv = filepath.Join(config.Home, "other bin")
	if err := os.MkdirAll(config.PathEnv, 0o755); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	var out bytes.Buffer
	if err := bootInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootRestart) {
		t.Fatal("PATH change did not restart the live daemon")
	}
	if !strings.Contains(out.String(), "Updated and restarted") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	metadata, err := readBootMetadata(bootMetadataPath(mustBootUnitPath(t)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PathEnv != config.PathEnv {
		t.Fatalf("metadata kept the old PATH: %+v", metadata)
	}
}

func TestBootInstallRefusesForeignUnit(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := bootTestRunner()
	if err := bootInstall(config, runner, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("foreign unit not refused: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Description=user unit") {
		t.Fatal("foreign unit was modified")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("foreign unit triggered systemd calls: %v", runner.calls)
	}
}

func TestBootInstallRefusesRoot(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	original := bootEffectiveUID
	bootEffectiveUID = func() int { return 0 }
	defer func() { bootEffectiveUID = original }()
	err := bootInstall(config, bootTestRunner(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "refusing root") {
		t.Fatalf("root install not refused: %v", err)
	}
	if _, statErr := os.Stat(mustBootUnitPath(t)); !os.IsNotExist(statErr) {
		t.Fatal("root install wrote a unit")
	}
}

func TestBootInstallRefusesLiveStateOwner(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "owned by a running process") {
		t.Fatalf("live state owner not refused: %v", err)
	}
	if _, statErr := os.Stat(mustBootUnitPath(t)); !os.IsNotExist(statErr) {
		t.Fatal("refused install wrote a unit")
	}
	if runner.called(bootEnableNow) || runner.called(bootRestart) || runner.called(bootStop) {
		t.Fatalf("refused install started or stopped units: %v", runner.calls)
	}
}

func TestBootInstallRefusesBusyAddress(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config.Addr = listener.Addr().String()
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	err = bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("busy address not refused: %v", err)
	}
	if _, statErr := os.Stat(mustBootUnitPath(t)); !os.IsNotExist(statErr) {
		t.Fatal("refused install wrote a unit")
	}
}

func TestBootInstallVerificationFailureStopsOwnUnit(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	// The installed binary is a shell script, so the active process (this
	// test binary) cannot be the installed owner.
	config.Binary = filepath.Join(home, "zen")
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootStartHealthServer(t, daemonID)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "active")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	runner.set(bootIsEnabled, "enabled")
	runner.set(bootFragment, mustBootUnitPath(t))
	runner.set(bootControlGroup, bootCurrentCgroup(t))
	runner.set("loginctl show-user "+currentUserName()+" -p Linger --value", "yes")
	previousTimeout, previousInterval := bootVerifyTimeout, bootVerifyInterval
	bootVerifyTimeout, bootVerifyInterval = 400*time.Millisecond, 50*time.Millisecond
	defer func() { bootVerifyTimeout, bootVerifyInterval = previousTimeout, previousInterval }()

	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "did not become the verified owner") {
		t.Fatalf("verification failure not reported: %v", err)
	}
	if !runner.called(bootRestart) {
		t.Fatal("changed unit was not started before verification")
	}
	if !runner.called(bootStop) {
		t.Fatal("failed verification left the unit running instead of stopping its own unit")
	}
	if _, statErr := os.Stat(mustBootUnitPath(t)); statErr != nil {
		t.Fatal("failed install removed the recoverable unit")
	}
}

func TestBootInstallRefusesInaccessibleContext(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the user access checks")
	}
	t.Run("non executable binary", func(t *testing.T) {
		config, home := newBootTestEnvironment(t)
		binary := filepath.Join(home, "plain")
		if err := os.WriteFile(binary, []byte("not executable"), 0o644); err != nil {
			t.Fatal(err)
		}
		config.Binary = binary
		err := bootInstall(config, bootTestRunner(), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "not executable") {
			t.Fatalf("non-executable binary not refused: %v", err)
		}
	})
	t.Run("inaccessible working directory", func(t *testing.T) {
		config, _ := newBootTestEnvironment(t)
		if err := os.Chmod(config.WorkDir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(config.WorkDir, 0o755) })
		err := bootInstall(config, bootTestRunner(), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "working directory is not accessible") {
			t.Fatalf("inaccessible working directory not refused: %v", err)
		}
	})
}

func TestBootInstallRefusesBusyNewAddressOnUpdate(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	if err := bootInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config.Addr = listener.Addr().String()
	runner.reset()
	err = bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("busy new address not refused: %v", err)
	}
	if runner.called(bootRestart) {
		t.Fatal("refused update restarted the live unit")
	}
	metadata, err := readBootMetadata(bootMetadataPath(mustBootUnitPath(t)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Addr == config.Addr {
		t.Fatal("refused update replaced the installed address")
	}
}

func TestBootInstallFailsFastWhenUnitDoesNotStart(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	runner.set(bootIsActive, "failed")
	started := time.Now()
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "did not become the verified owner") {
		t.Fatalf("failed unit not reported: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("verification waited %s for a failed unit", elapsed)
	}
	if !runner.called(bootStop) {
		t.Fatal("failed verification did not stop the owned unit")
	}
}

func TestBootInstallRefusesActiveUnitWithManualOwner(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "active")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	// The active unit runs in another cgroup, so the manual lock holder in
	// this test process must not be attributed to it.
	runner.set(bootControlGroup, "/user.slice/user-1000.slice/user@1000.service/app.slice/manual-owner.service")
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "outside zen.service") {
		t.Fatalf("active unit with manual state owner not refused: %v", err)
	}
	if runner.called(bootEnableNow) || runner.called(bootRestart) || runner.called(bootStop) {
		t.Fatalf("refused install touched units: %v", runner.calls)
	}
	if _, statErr := os.Stat(mustBootUnitPath(t)); !os.IsNotExist(statErr) {
		t.Fatal("refused install wrote a unit")
	}
}

func TestBootInstallFailsWhenFragmentPathUnknown(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	runner.set(bootFragment, "")
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no FragmentPath") {
		t.Fatalf("unknown FragmentPath not treated as failure: %v", err)
	}
	if runner.called(bootEnableNow) || runner.called(bootStop) {
		t.Fatalf("unknown FragmentPath started or stopped the unit: %v", runner.calls)
	}
}

func TestBootInstallRefusesUnknownMainPID(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	runner.set(bootMainPID, "0")
	previousTimeout, previousInterval := bootVerifyTimeout, bootVerifyInterval
	bootVerifyTimeout, bootVerifyInterval = 400*time.Millisecond, 50*time.Millisecond
	defer func() { bootVerifyTimeout, bootVerifyInterval = previousTimeout, previousInterval }()
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "did not become the verified owner") || !strings.Contains(err.Error(), "no readable main process") {
		t.Fatalf("unknown MainPID not reported: %v", err)
	}
	if !runner.called(bootStop) {
		t.Fatal("failed verification left the unit running")
	}
}

func TestBootInstallRequiresStableOwner(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	// Preflight inactive, first verification active, confirmation failed: the
	// installer must not accept a one-sample success.
	runner.setSequence(bootIsActive, "inactive", "active", "failed")
	previousTimeout, previousInterval := bootVerifyTimeout, bootVerifyInterval
	bootVerifyTimeout, bootVerifyInterval = 2*time.Second, 50*time.Millisecond
	defer func() { bootVerifyTimeout, bootVerifyInterval = previousTimeout, previousInterval }()
	err := bootInstall(config, runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "did not become the verified owner") {
		t.Fatalf("transient owner reported as success: %v", err)
	}
	if !runner.called(bootStop) {
		t.Fatal("failed verification left the unit running")
	}
}

func TestBootInstallDryRunWritesNothing(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.DryRun = true
	var out bytes.Buffer
	if err := bootInstall(config, bootTestRunner(), &out); err != nil {
		t.Fatal(err)
	}
	path, _ := bootUnitPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a unit")
	}
	if _, err := os.Stat(bootMetadataPath(path)); !os.IsNotExist(err) {
		t.Fatal("dry run wrote metadata")
	}
	if !strings.Contains(out.String(), bootManagedMarker) {
		t.Fatalf("dry run did not print the unit: %s", out.String())
	}
}

func TestBootStatusUsesInstalledConfigurationAndOwner(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config.Binary = executable
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootStartHealthServer(t, daemonID)
	// A custom loopback port far from the 127.0.0.1:9876 invocation default.
	if strings.Contains(config.Addr, ":9876") {
		t.Skip("unexpected default port in fixture")
	}
	bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)

	runner := bootTestRunner()
	runner.set(bootIsEnabled, "enabled")
	runner.set(bootIsActive, "active")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	runner.set(bootControlGroup, bootCurrentCgroup(t))
	runner.set("loginctl show-user "+currentUserName()+" -p Linger --value", "yes")

	var out bytes.Buffer
	if err := bootStatus(runner, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"address: " + config.Addr,
		"state directory: " + config.StateDir,
		"binary: " + config.Binary,
		"binary sha256: ",
		"service: active",
		"main pid: " + strconv.Itoa(os.Getpid()),
		"ownership: state directory is owned by zen.service (PID " + strconv.Itoa(os.Getpid()) + ")",
		"health: ok daemon_id=" + daemonID,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), bootDefaultAddr) {
		t.Fatalf("status used the invocation default instead of the installed address:\n%s", out.String())
	}
}

func TestBootStatusAttributesForeignHealth(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootWriteIdentity(t, config.StateDir)
	config.Addr = bootStartHealthServer(t, "not-the-installed-daemon")
	bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.set(bootIsEnabled, "enabled")
	runner.set(bootIsActive, "active")
	runner.set(bootMainPID, strconv.Itoa(os.Getpid()))
	runner.set("loginctl show-user "+currentUserName()+" -p Linger --value", "yes")

	var out bytes.Buffer
	if err := bootStatus(runner, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mismatch:") {
		t.Fatalf("foreign health was not attributed:\n%s", out.String())
	}
}

func TestBootStatusMissingAndForeignUnit(t *testing.T) {
	_, _ = newBootTestEnvironment(t)
	var out bytes.Buffer
	if err := bootStatus(bootTestRunner(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Fatalf("missing unit not reported: %s", out.String())
	}
	path := mustBootUnitPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := bootStatus(bootTestRunner(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not managed") {
		t.Fatalf("foreign unit not reported: %s", out.String())
	}
}

func TestBootUninstallRemovesOnlyManagedUnit(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, metadataPath := bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	var out bytes.Buffer
	if err := bootUninstall(runner, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("managed unit was not removed")
	}
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatal("managed metadata was not removed")
	}
	if !runner.called(bootStop) || !runner.called(bootDisable) || !runner.called(bootReload) {
		t.Fatalf("uninstall calls incomplete: %v", runner.calls)
	}
	stopIndex, disableIndex := -1, -1
	runner.mu.Lock()
	for index, call := range runner.calls {
		switch strings.Join(call, " ") {
		case bootStop:
			stopIndex = index
		case bootDisable:
			disableIndex = index
		}
	}
	runner.mu.Unlock()
	if stopIndex < 0 || disableIndex < 0 || stopIndex > disableIndex {
		t.Fatalf("uninstall must stop before disabling: stop=%d disable=%d", stopIndex, disableIndex)
	}
	if !strings.Contains(out.String(), "Removed") {
		t.Fatalf("unexpected output: %s", out.String())
	}

	// A foreign unit must never be removed.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bootUninstall(runner, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("foreign unit not refused on uninstall: %v", err)
	}
}

func TestBootUninstallStopFailureRetainsConfiguration(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, metadataPath := bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.fail(bootStop, "Failed to stop zen.service: access denied")
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
		t.Fatalf("stop failure not reported: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("stop failure removed the unit")
	}
	if _, statErr := os.Stat(metadataPath); statErr != nil {
		t.Fatal("stop failure removed the metadata")
	}
	if runner.called(bootDisable) {
		t.Fatal("stop failure still disabled the unit")
	}
}

// bootUseProcFixture points the ownership scan at an owned procfs tree that
// exposes only the listed PIDs (as symlinks to the real /proc entries). The
// scan then cannot be perturbed by unrelated host processes whose
// /proc/<pid>/fd is unreadable, while each exposed PID still resolves its real
// cgroup, fd links and fdinfo lock records.
func bootUseProcFixture(t *testing.T, pids ...int) {
	t.Helper()
	root := t.TempDir()
	for _, pid := range pids {
		if err := os.Symlink("/proc/"+strconv.Itoa(pid), filepath.Join(root, strconv.Itoa(pid))); err != nil {
			t.Fatal(err)
		}
	}
	previous := bootProcRoot
	bootProcRoot = root
	t.Cleanup(func() { bootProcRoot = previous })
}

func TestBootUninstallStillRunningRetainsConfiguration(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, _ := bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.set(bootIsActive, "active")
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "still active after stop") {
		t.Fatalf("still-running unit not reported: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("still-running uninstall removed the unit")
	}
	if runner.called(bootDisable) {
		t.Fatal("still-running unit was disabled")
	}
}

func TestBootUninstallDisableFailureRetainsConfiguration(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, _ := bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	runner.fail(bootDisable, "Failed to disable unit")
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
		t.Fatalf("disable failure not reported: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("disable failure removed the unit")
	}
}

func TestBootUninstallRetainsOwnLeftoverDaemon(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, metadataPath := bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	runner.set(bootControlGroup, bootCurrentCgroup(t))
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "still owns state directory") || !strings.Contains(err.Error(), "configuration retained") {
		t.Fatalf("own leftover daemon not reported: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("own leftover daemon removed the unit")
	}
	if _, statErr := os.Stat(metadataPath); statErr != nil {
		t.Fatal("own leftover daemon removed the metadata")
	}
	if runner.called(bootDisable) {
		t.Fatal("own leftover daemon still disabled the unit")
	}
}

func TestBootUninstallRetainsUnknownOwner(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootUseProcFixture(t, os.Getpid())
	path, metadataPath := bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ControlGroup is unknown") {
		t.Fatalf("unattributable state owner not retained: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("unattributable owner removed the unit")
	}
	if _, statErr := os.Stat(metadataPath); statErr != nil {
		t.Fatal("unattributable owner removed the metadata")
	}
}

func TestBootUninstallOutsideOwnerRemovesWithNote(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootUseProcFixture(t, os.Getpid())
	path, metadataPath := bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	runner.set(bootControlGroup, "/user.slice/user-1000.slice/user@1000.service/app.slice/manual-owner.service")
	var out bytes.Buffer
	if err := bootUninstall(runner, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unit was not removed")
	}
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatal("metadata was not removed")
	}
	if !strings.Contains(out.String(), "owned by a process outside zen.service") {
		t.Fatalf("outside owner note missing: %s", out.String())
	}
}

func TestBootStateLockPIDRequiresRealLock(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootHoldStateLock(t, config.StateDir)
	lockPath, err := bootLifecycleLockPath(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	// A real process in the same cgroup opens the lock file without flocking.
	opener := exec.Command("/bin/sh", "-c", "exec 9>>\"$0\"; sleep 30", lockPath)
	opener.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := opener.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-opener.Process.Pid, syscall.SIGKILL)
		_, _ = opener.Process.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Readlink("/proc/" + strconv.Itoa(opener.Process.Pid) + "/fd/9"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	bootUseProcFixture(t, os.Getpid(), opener.Process.Pid)

	pid, err := bootStateLockPID(config.StateDir, bootCurrentCgroup(t))
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("cgroup scan reported %d, want the flock holder %d", pid, os.Getpid())
	}
	all, err := bootStateLockPID(config.StateDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if all != os.Getpid() {
		t.Fatalf("global scan reported %d, want the flock holder %d", all, os.Getpid())
	}
	// The opener must never be reported, even though it is in the cgroup and
	// has the lock file open.
	openerHeld, err := bootPIDHoldsFileLock(opener.Process.Pid, lockPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if openerHeld {
		t.Fatal("a process that only opened the lock file was reported as the holder")
	}
	holderHeld, err := bootPIDHoldsFileLock(os.Getpid(), lockPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if !holderHeld {
		t.Fatal("the real flock holder was not detected")
	}
}

func TestBootUninstallRetainsConfigurationWithoutMetadata(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, metadataPath := bootWriteInstalledUnit(t, config)
	if err := os.Remove(metadataPath); err != nil {
		t.Fatal(err)
	}
	runner := bootTestRunner()
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
		t.Fatalf("missing metadata not retained: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("missing metadata removed the unit")
	}
	if runner.called(bootStop) || runner.called(bootDisable) {
		t.Fatalf("missing metadata touched the unit: %v", runner.calls)
	}
}

func TestBootUninstallRetainsInvalidMetadata(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, metadataPath := bootWriteInstalledUnit(t, config)
	if err := os.WriteFile(metadataPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := bootTestRunner()
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
		t.Fatalf("invalid metadata not retained: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("invalid metadata removed the unit")
	}
}

func TestBootUninstallRetainsOnUnreadableOwnershipScan(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, _ := bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	previous := bootProcRoot
	bootProcRoot = filepath.Join(t.TempDir(), "missing")
	defer func() { bootProcRoot = previous }()
	err := bootUninstall(runner, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
		t.Fatalf("unreadable ownership scan did not retain configuration: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("unreadable ownership scan removed the unit")
	}
}

func TestBootStateLockPIDStrictPermissionIsUnknown(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	group := "/user.slice/user-1000.slice/user@1000.service/app.slice/zen.service"
	root := t.TempDir()
	processDir := filepath.Join(root, "4242")
	if err := os.MkdirAll(processDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "cgroup"), []byte("0::"+group+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(processDir, "fd"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(processDir, "fd"), 0o755) })
	previous := bootProcRoot
	bootProcRoot = root
	defer func() { bootProcRoot = previous }()

	if _, err := bootStateLockPID(config.StateDir, group); err == nil {
		t.Fatal("unreadable process in the matched cgroup was treated as no owner")
	}
}

// TestBootStateLockPIDStrictProvenForeignIsSkipped models an undedicated host
// where an unrelated root process shares the cgroup the unit reports (for
// example the root cgroup on a CI runner). The unit runs as the installing
// user, so a proven-foreign process is skipped as an observation-attribution
// preference instead of failing the whole scan closed; an inherited-descriptor
// caveat is documented on the production helper.
func TestBootStateLockPIDStrictProvenForeignIsSkipped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root owns every process; no foreign UID exists")
	}
	previous := bootProcRoot
	bootProcRoot = "/proc"
	defer func() { bootProcRoot = previous }()
	if bootProcessOwnerKind(1) != bootProcessOwnerForeignUID {
		t.Skip("/proc/1 is not a foreign-UID process on this host")
	}
	if !bootSkipInspectionError(1, os.ErrPermission, true) {
		t.Fatal("proven-foreign process must be skippable in strict cgroup mode")
	}
	// An unknown owner stays unresolved: permission errors are not
	// automatically foreign.
	if bootSkipInspectionError(1<<30, os.ErrPermission, true) {
		t.Fatal("unknown process owner must not be skipped in strict cgroup mode")
	}
}

// TestBootStateLockPIDZombieIsSkipped covers an ambient zombie in the scanned
// cgroup (for example a dead tunnel child the parent never reaped). A zombie
// holds no descriptors and can never be the state lock owner, so the scan must
// skip it instead of failing closed with an unreadable /proc/<pid>/fd.
func TestBootStateLockPIDZombieIsSkipped(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	bootHoldStateLock(t, config.StateDir)
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if bootPIDZombie(cmd.Process.Pid) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !bootPIDZombie(cmd.Process.Pid) {
		t.Fatal("child did not become a zombie")
	}
	bootUseProcFixture(t, os.Getpid(), cmd.Process.Pid)
	if !bootSkipInspectionError(cmd.Process.Pid, os.ErrPermission, true) {
		t.Fatal("zombie process must be skippable in strict cgroup mode")
	}
	pid, err := bootStateLockPID(config.StateDir, bootCurrentCgroup(t))
	if err != nil {
		t.Fatalf("zombie turned the strict scan into unresolved ownership: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("scan reported %d, want the real flock holder %d", pid, os.Getpid())
	}
}

func TestBootUninstallRetainsHeldButUnattributable(t *testing.T) {
	t.Run("matched cgroup inspection denied", func(t *testing.T) {
		config, _ := newBootTestEnvironment(t)
		path, metadataPath := bootWriteInstalledUnit(t, config)
		bootHoldStateLock(t, config.StateDir)
		group := "/user.slice/user-1000.slice/user@1000.service/app.slice/zen.service"
		root := t.TempDir()
		processDir := filepath.Join(root, "4242")
		if err := os.MkdirAll(processDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(processDir, "cgroup"), []byte("0::"+group+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(processDir, "fd"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(processDir, "fd"), 0o755) })
		previous := bootProcRoot
		bootProcRoot = root
		defer func() { bootProcRoot = previous }()

		runner := bootTestRunner()
		runner.set(bootIsActive, "inactive")
		runner.set(bootControlGroup, group)
		err := bootUninstall(runner, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "unit and configuration retained") {
			t.Fatalf("denied inspection did not retain configuration: %v", err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatal("denied inspection removed the unit")
		}
		if _, statErr := os.Stat(metadataPath); statErr != nil {
			t.Fatal("denied inspection removed the metadata")
		}
		if runner.called(bootDisable) {
			t.Fatal("denied inspection disabled the unit")
		}
	})

	t.Run("held but no attributable holder", func(t *testing.T) {
		config, _ := newBootTestEnvironment(t)
		path, metadataPath := bootWriteInstalledUnit(t, config)
		bootHoldStateLock(t, config.StateDir)
		previous := bootProcRoot
		bootProcRoot = t.TempDir()
		defer func() { bootProcRoot = previous }()

		runner := bootTestRunner()
		runner.set(bootIsActive, "inactive")
		err := bootUninstall(runner, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "could not be attributed") {
			t.Fatalf("unattributable held lock did not retain configuration: %v", err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatal("unattributable held lock removed the unit")
		}
		if _, statErr := os.Stat(metadataPath); statErr != nil {
			t.Fatal("unattributable held lock removed the metadata")
		}
	})
}

func TestBootFDLockRecordParsing(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"acquired exclusive whole file", "pos:\t0\nlock:\t1: FLOCK  ADVISORY  WRITE 123 08:01:42 0 EOF\n", true},
		{"shared read lock", "lock:\t1: FLOCK  ADVISORY  READ 123 08:01:42 0 EOF\n", false},
		{"posix record lock", "lock:\t1: POSIX  ADVISORY  WRITE 123 08:01:42 0 EOF\n", false},
		{"pending marker", "lock:\t1: -> FLOCK  ADVISORY  WRITE 123 08:01:42 0 EOF\n", false},
		{"partial range", "lock:\t1: FLOCK  ADVISORY  WRITE 123 08:01:42 5 EOF\n", false},
		{"open without lock", "pos:\t0\nflags:\t0100000\n", false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fdinfo")
			if err := os.WriteFile(path, []byte(item.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := bootFDLockRecord(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != item.want {
				t.Fatalf("bootFDLockRecord = %t, want %t", got, item.want)
			}
		})
	}
}

func TestBootProcessOwnerUnknownIsNotForeign(t *testing.T) {
	root := t.TempDir()
	previous := bootProcRoot
	bootProcRoot = root
	defer func() { bootProcRoot = previous }()

	if got := bootProcessOwnerForPath(filepath.Join(root, "4242")); got != bootProcessOwnerUnknown {
		t.Fatalf("missing process path classified as %d, want unknown", got)
	}
	if got := bootProcessOwnerForPath(root); got != bootProcessOwnerSameUID {
		t.Fatalf("test-owned path classified as %d, want same UID", got)
	}
	if bootSkipInspectionError(4242, os.ErrPermission, false) {
		t.Fatal("permission error with unknown owner was skipped as foreign")
	}
	if bootSkipInspectionError(4242, os.ErrPermission, true) {
		t.Fatal("strict permission error was skipped")
	}
	if !bootSkipInspectionError(4242, os.ErrNotExist, false) {
		t.Fatal("exited process was not skipped")
	}
}

func TestBootPIDInCgroupUnknownOwnerFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "4242"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := bootProcRoot
	bootProcRoot = root
	defer func() { bootProcRoot = previous }()
	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	if _, err := bootPIDInCgroup(4242, "/user.slice/test.service"); err == nil {
		t.Fatal("permission error with an unknown process owner was treated as foreign")
	}
}

func TestBootLingerFailureReportsExactOperatorCommand(t *testing.T) {
	config, runner := bootFreshInstallEnvironment(t)
	name := currentUserName()
	runner.set("loginctl show-user "+name+" -p Linger --value", "no")
	runner.fail("loginctl enable-linger "+name, "Interactive authentication required.")
	var out bytes.Buffer
	err := bootInstall(config, runner, &out)
	if err == nil || !strings.Contains(err.Error(), "sudo loginctl enable-linger "+name) {
		t.Fatalf("linger failure did not report the exact operator command: %v", err)
	}
	if !strings.Contains(out.String(), "sudo loginctl enable-linger "+name) {
		t.Fatalf("operator command missing from output: %s", out.String())
	}
	if !runner.called(bootEnableNow) {
		t.Fatal("unit was not installed and started before the linger failure")
	}
}

func mustBootUnitPath(t *testing.T) string {
	t.Helper()
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}
