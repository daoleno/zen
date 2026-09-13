package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeSunshineProcess struct {
	mu         sync.Mutex
	pid        int
	signals    []syscall.Signal
	waitCh     chan struct{}
	waitErr    error
	ignoreTerm bool
	signalFail bool
}

func newFakeSunshineProcess(pid int) *fakeSunshineProcess {
	return &fakeSunshineProcess{pid: pid, waitCh: make(chan struct{})}
}

func (p *fakeSunshineProcess) Signal(signal syscall.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, signal)
	ignore := p.ignoreTerm && signal != syscall.SIGKILL
	fail := p.signalFail
	p.mu.Unlock()
	if fail {
		return errors.New("signal_failed")
	}
	if !ignore {
		p.exitNow()
	}
	return nil
}

func (p *fakeSunshineProcess) Wait() error {
	<-p.waitCh
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *fakeSunshineProcess) PID() int { return p.pid }

func (p *fakeSunshineProcess) exitNow() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.waitCh:
	default:
		close(p.waitCh)
	}
}

func (p *fakeSunshineProcess) observedSignals() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.signals...)
}

func sunshineTestOptions(t *testing.T) SunshineHostOptions {
	t.Helper()
	opts := DefaultSunshineHostOptions("/usr/libexec/zen/sunshine", filepath.Join(t.TempDir(), "sunshine"), 47989)
	opts.StopGrace = 50 * time.Millisecond
	return opts
}

func TestDefaultSunshineOptionsBindAllPathsToPrivateState(t *testing.T) {
	stateDir := "/var/lib/zen/sunshine"
	opts := DefaultSunshineHostOptions("/usr/libexec/zen/sunshine", stateDir, 47989)
	if err := opts.validate(); err != nil {
		t.Fatalf("default options rejected: %v", err)
	}
	for name, path := range opts.statePaths() {
		if filepath.Dir(path) != stateDir {
			t.Fatalf("%s = %q is outside %q", name, path, stateDir)
		}
	}
	body := opts.configBody()
	for _, key := range []string{"cert = ", "credentials_file = ", "file_apps = ", "file_state = ", "pkey = ", "port = 47989"} {
		if !strings.Contains(body, key) {
			t.Fatalf("config body missing %q: %q", key, body)
		}
	}
}

func TestSunshineOptionsValidation(t *testing.T) {
	base := DefaultSunshineHostOptions("/usr/libexec/zen/sunshine", "/var/lib/zen/sunshine", 47989)
	cases := map[string]func(o *SunshineHostOptions){
		"relative binary": func(o *SunshineHostOptions) { o.BinaryPath = "sunshine" },
		"relative state":  func(o *SunshineHostOptions) { o.StateDir = "sunshine" },
		"config outside":  func(o *SunshineHostOptions) { o.ConfigPath = "/etc/sunshine.conf" },
		"state outside":   func(o *SunshineHostOptions) { o.StateFilePath = "/etc/state.json" },
		"pkey outside":    func(o *SunshineHostOptions) { o.PKeyPath = "/etc/pkey" },
		"cert outside":    func(o *SunshineHostOptions) { o.CertPath = "/etc/cert" },
		"apps outside":    func(o *SunshineHostOptions) { o.AppsFilePath = "/etc/apps.json" },
		"credentials out": func(o *SunshineHostOptions) { o.CredentialsFilePath = "/etc/creds.json" },
		"collision":       func(o *SunshineHostOptions) { o.CertPath = o.PKeyPath },
		"port low":        func(o *SunshineHostOptions) { o.Port = 80 },
		"port high":       func(o *SunshineHostOptions) { o.Port = 65535 },
		"newline in path": func(o *SunshineHostOptions) { o.CertPath = o.StateDir + "/ce\nrt" },
	}
	for name, mutate := range cases {
		opts := base
		mutate(&opts)
		if err := opts.validate(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestSunshineStateDirAndConfigArePrivateAndNeverOverwritten(t *testing.T) {
	opts := sunshineTestOptions(t)
	created, err := WriteSunshineConfig(opts)
	if err != nil || !created {
		t.Fatalf("write config: created=%v err=%v", created, err)
	}
	info, err := os.Lstat(opts.StateDir)
	if err != nil || info.Mode().Perm() != SunshineStateDirMode {
		t.Fatalf("state dir mode = %v err=%v", info.Mode().Perm(), err)
	}
	configInfo, err := os.Lstat(opts.ConfigPath)
	if err != nil || configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v err=%v", configInfo.Mode().Perm(), err)
	}
	body, _ := os.ReadFile(opts.ConfigPath)
	if string(body) != opts.configBody() {
		t.Fatalf("config body = %q want %q", body, opts.configBody())
	}

	// An administrator-reviewed config must never be rewritten.
	if err := os.WriteFile(opts.ConfigPath, []byte("port = 1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = WriteSunshineConfig(opts)
	if err != nil || created {
		t.Fatalf("second write: created=%v err=%v", created, err)
	}
	body, _ = os.ReadFile(opts.ConfigPath)
	if string(body) != "port = 1234\n" {
		t.Fatalf("config overwritten: %q", body)
	}
}

func TestUnsafeSunshineStateDirRejected(t *testing.T) {
	opts := sunshineTestOptions(t)
	if err := os.MkdirAll(opts.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSunshineStateDir(opts); err == nil {
		t.Fatal("expected unsafe state dir rejection")
	}
}

func TestStartUsesPositionalConfigPathAndPrivateWorkingDir(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(4242)
	var gotBinary string
	var gotArgs []string
	var gotDir string
	host, err := StartSunshineHost(opts, func(binary string, args []string, dir string, env []string) (SunshineProcess, error) {
		gotBinary, gotArgs, gotDir = binary, append([]string(nil), args...), dir
		return process, nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if gotBinary != opts.BinaryPath {
		t.Fatalf("binary = %q", gotBinary)
	}
	// Upstream config.cpp reads a bare path as the config; "--config" would be
	// parsed as an unknown command by main.cpp.
	if len(gotArgs) != 1 || gotArgs[0] != opts.ConfigPath {
		t.Fatalf("args = %q", gotArgs)
	}
	if gotDir != opts.StateDir {
		t.Fatalf("dir = %q", gotDir)
	}
	if !host.Running() || host.PID() != 4242 {
		t.Fatalf("running=%v pid=%d", host.Running(), host.PID())
	}
	_ = host.Stop(context.Background())
}

func TestSpawnerErrorSurfaces(t *testing.T) {
	opts := sunshineTestOptions(t)
	_, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected spawner error")
	}
}

func TestRunningReflectsRealProcessExit(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(7)
	host, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	process.exitNow()
	deadline := time.Now().Add(2 * time.Second)
	for host.Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if host.Running() {
		t.Fatal("running still true after process exit")
	}
	// Stop after an exit reports success instead of discarding a live handle.
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("stop after exit: %v", err)
	}
}

func TestStopIsBoundedAndIdempotent(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(7)
	host, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if signals := process.observedSignals(); len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v", signals)
	}
	if host.Running() || host.PID() != 0 {
		t.Fatalf("still running: %v pid=%d", host.Running(), host.PID())
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestStopEscalatesWhenTermIsIgnored(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(9)
	process.ignoreTerm = true
	host, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	signals := process.observedSignals()
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v", signals)
	}
}

func TestFailedStopKeepsHandleForRetry(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(13)
	process.ignoreTerm = true
	process.signalFail = true
	host, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err == nil {
		t.Fatal("expected stop failure")
	}
	if !host.Running() {
		t.Fatal("failed stop discarded the live handle")
	}
	process.mu.Lock()
	process.signalFail = false
	process.ignoreTerm = false
	process.mu.Unlock()
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("retry stop: %v", err)
	}
	if host.Running() {
		t.Fatal("still running after retry")
	}
}

func TestRevokeRemovesOnlyUpstreamStateFile(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(11)
	host, err := StartSunshineHost(opts, func(string, []string, string, []string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	preserved := []string{opts.PKeyPath, opts.CertPath, opts.CredentialsFilePath, opts.AppsFilePath, opts.ConfigPath}
	for _, path := range preserved {
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(opts.StateFilePath, []byte("pair"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := host.Revoke(context.Background()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := os.Lstat(opts.StateFilePath); !os.IsNotExist(err) {
		t.Fatalf("file_state still present: %v", err)
	}
	for _, path := range preserved {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("unrelated file removed: %s: %v", path, err)
		}
	}
	if host.Running() {
		t.Fatal("process still running after revoke")
	}
	if err := host.Revoke(context.Background()); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
}
