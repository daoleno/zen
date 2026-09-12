package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeSunshineProcess struct {
	mu         sync.Mutex
	pid        int
	signals    []syscall.Signal
	waitCh     chan error
	waitOnce   sync.Once
	ignoreTerm bool
}

func newFakeSunshineProcess(pid int, ignoreTerm bool) *fakeSunshineProcess {
	return &fakeSunshineProcess{pid: pid, waitCh: make(chan error, 1), ignoreTerm: ignoreTerm}
}

func (p *fakeSunshineProcess) Signal(signal syscall.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, signal)
	ignore := p.ignoreTerm && signal != syscall.SIGKILL
	p.mu.Unlock()
	if !ignore {
		p.waitOnce.Do(func() { p.waitCh <- nil })
	}
	return nil
}

func (p *fakeSunshineProcess) Wait() error { return <-p.waitCh }

func (p *fakeSunshineProcess) PID() int { return p.pid }

func (p *fakeSunshineProcess) observedSignals() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.signals...)
}

func sunshineTestOptions(t *testing.T) SunshineHostOptions {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "sunshine")
	return SunshineHostOptions{
		BinaryPath:       "/usr/libexec/zen/sunshine",
		StateDir:         stateDir,
		ConfigPath:       filepath.Join(stateDir, "sunshine.conf"),
		PairingStatePath: filepath.Join(stateDir, "pairing.state"),
		Port:             47989,
		StopGrace:        50 * time.Millisecond,
	}
}

func TestSunshineOptionsValidation(t *testing.T) {
	base := SunshineHostOptions{
		BinaryPath:       "/usr/libexec/zen/sunshine",
		StateDir:         "/var/lib/zen/sunshine",
		ConfigPath:       "/var/lib/zen/sunshine/sunshine.conf",
		PairingStatePath: "/var/lib/zen/sunshine/pairing.state",
		Port:             47989,
	}
	cases := map[string]func(o *SunshineHostOptions){
		"relative binary":       func(o *SunshineHostOptions) { o.BinaryPath = "sunshine" },
		"relative state dir":    func(o *SunshineHostOptions) { o.StateDir = "sunshine" },
		"config outside state":  func(o *SunshineHostOptions) { o.ConfigPath = "/etc/sunshine.conf" },
		"pairing outside state": func(o *SunshineHostOptions) { o.PairingStatePath = "/etc/pairing.state" },
		"colliding paths":       func(o *SunshineHostOptions) { o.ConfigPath = o.PairingStatePath },
		"zero port":             func(o *SunshineHostOptions) { o.Port = 0 },
		"high port":             func(o *SunshineHostOptions) { o.Port = 70000 },
		"empty extra arg":       func(o *SunshineHostOptions) { o.ExtraArgs = []string{""} },
	}
	for name, mutate := range cases {
		opts := base
		mutate(&opts)
		if err := opts.validate(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
	if err := base.validate(); err != nil {
		t.Fatalf("valid options rejected: %v", err)
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
	body, err := os.ReadFile(opts.ConfigPath)
	if err != nil || string(body) != "port = 47989\n" {
		t.Fatalf("config body = %q err=%v", body, err)
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

func TestStartPassesPrivateConfigAndWorkingDir(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(4242, false)
	var gotBinary string
	var gotArgs []string
	var gotDir string
	host, err := StartSunshineHost(opts, func(binary string, args []string, dir string) (SunshineProcess, error) {
		gotBinary, gotArgs, gotDir = binary, append([]string(nil), args...), dir
		return process, nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if gotBinary != opts.BinaryPath {
		t.Fatalf("binary = %q", gotBinary)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "--config" || gotArgs[1] != opts.ConfigPath {
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
	_, err := StartSunshineHost(opts, func(string, []string, string) (SunshineProcess, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected spawner error")
	}
}

func TestStopIsBoundedAndIdempotent(t *testing.T) {
	opts := sunshineTestOptions(t)
	process := newFakeSunshineProcess(7, false)
	host, err := StartSunshineHost(opts, func(string, []string, string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := host.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("stop took %v", elapsed)
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
	process := newFakeSunshineProcess(9, true)
	host, err := StartSunshineHost(opts, func(string, []string, string) (SunshineProcess, error) {
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

func TestRevokeStopsAndRemovesOnlyZenPairingState(t *testing.T) {
	opts := sunshineTestOptions(t)
	userFile := filepath.Join(opts.StateDir, "user.conf")
	process := newFakeSunshineProcess(11, false)
	host, err := StartSunshineHost(opts, func(string, []string, string) (SunshineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.PairingStatePath, []byte("pair"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("user"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := host.Revoke(context.Background()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := os.Lstat(opts.PairingStatePath); !os.IsNotExist(err) {
		t.Fatalf("pairing state still present: %v", err)
	}
	if _, err := os.Lstat(userFile); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
	if host.Running() {
		t.Fatal("process still running after revoke")
	}
	if err := host.Revoke(context.Background()); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
}
