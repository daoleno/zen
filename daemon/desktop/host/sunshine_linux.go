package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// SunshineHost supervises the pinned Sunshine host process that Zen owns.
//
// The daemon never edits a user's personal Sunshine installation: Zen writes
// only its own private state directory (mode 0700) and starts the reviewed
// binary with that private config. Every process action goes through a spawner
// so tests can exercise the contract without touching a real host, and the
// stop/revoke paths are bounded and idempotent because revoke is the product's
// authority boundary: once Zen revokes, the supervised process is gone.
const defaultSunshineStopGrace = 5 * time.Second

type SunshineProcess interface {
	Signal(signal syscall.Signal) error
	Wait() error
	PID() int
}

// SunshineSpawner starts the supervised host process. The default spawner uses
// exec with a private process group; tests inject a fake.
type SunshineSpawner func(binary string, args []string, dir string) (SunshineProcess, error)

type SunshineHostOptions struct {
	// BinaryPath is the reviewed Sunshine executable; must be absolute.
	BinaryPath string
	// StateDir is the Zen-owned private directory; must be absolute.
	StateDir string
	// ConfigPath is the Sunshine config inside StateDir; must be absolute.
	ConfigPath string
	// PairingStatePath holds the Zen-owned pairing state, inside StateDir.
	PairingStatePath string
	// Port is the Sunshine control port.
	Port int
	// ExtraArgs are appended after the private config argument.
	ExtraArgs []string
	// StopGrace bounds SIGTERM before SIGKILL. Zero uses the default.
	StopGrace time.Duration
}

func (o SunshineHostOptions) stopGrace() time.Duration {
	if o.StopGrace > 0 {
		return o.StopGrace
	}
	return defaultSunshineStopGrace
}

func (o SunshineHostOptions) validate() error {
	for name, path := range map[string]string{
		"sunshine_binary":             o.BinaryPath,
		"sunshine_state_dir":          o.StateDir,
		"sunshine_config_path":        o.ConfigPath,
		"sunshine_pairing_state_path": o.PairingStatePath,
	} {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("invalid_%s", name)
		}
	}
	if filepath.Dir(o.ConfigPath) != o.StateDir || filepath.Dir(o.PairingStatePath) != o.StateDir {
		return errors.New("sunshine_path_outside_state_dir")
	}
	if o.ConfigPath == o.PairingStatePath {
		return errors.New("sunshine_config_pairing_collision")
	}
	if o.Port <= 0 || o.Port > 65535 {
		return errors.New("invalid_sunshine_port")
	}
	for _, arg := range o.ExtraArgs {
		if arg == "" {
			return errors.New("invalid_sunshine_arg")
		}
	}
	return nil
}

// EnsureSunshineStateDir creates and validates the private Zen-owned directory.
func EnsureSunshineStateDir(opts SunshineHostOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}
	info, err := os.Lstat(opts.StateDir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(opts.StateDir, 0o700); err != nil {
			return fmt.Errorf("create sunshine state dir: %w", err)
		}
		info, err = os.Lstat(opts.StateDir)
	}
	if err != nil {
		return fmt.Errorf("stat sunshine state dir: %w", err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o700 || owner.Uid != uint32(os.Getuid()) {
		return errors.New("unsafe_sunshine_state_dir")
	}
	return nil
}

// WriteSunshineConfig writes the private config once. An existing file is never
// overwritten because its content may have been reviewed by an administrator.
func WriteSunshineConfig(opts SunshineHostOptions) (created bool, err error) {
	if err := EnsureSunshineStateDir(opts); err != nil {
		return false, err
	}
	if _, statErr := os.Lstat(opts.ConfigPath); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf("stat sunshine config: %w", statErr)
	}
	body := fmt.Sprintf("port = %d\n", opts.Port)
	if err := os.WriteFile(opts.ConfigPath, []byte(body), 0o600); err != nil {
		return false, fmt.Errorf("write sunshine config: %w", err)
	}
	return true, nil
}

// DefaultSunshineSpawner starts the reviewed binary in its own process group so
// stop signals reach the whole supervised tree and never the daemon's group.
func DefaultSunshineSpawner(binary string, args []string, dir string) (SunshineProcess, error) {
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "SUNSHINE_STATE_DIR="+dir)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	// Setpgid keeps stop signals inside the supervised tree. No Pdeathsig:
	// Go may deliver it when the spawning thread exits, which is not the
	// daemon lifetime; orphan cleanup is owned by the Zen service unit.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start sunshine: %w", err)
	}
	return &execSunshineProcess{command: command}, nil
}

type execSunshineProcess struct {
	command *exec.Cmd
}

func (p *execSunshineProcess) Signal(signal syscall.Signal) error {
	if p.command.Process == nil {
		return errors.New("sunshine_process_not_started")
	}
	// Negative pid signals the whole process group created by Setpgid.
	return syscall.Kill(-p.command.Process.Pid, signal)
}

func (p *execSunshineProcess) Wait() error {
	return p.command.Wait()
}

func (p *execSunshineProcess) PID() int {
	if p.command.Process == nil {
		return 0
	}
	return p.command.Process.Pid
}

type SunshineHost struct {
	mu      sync.Mutex
	opts    SunshineHostOptions
	spawner SunshineSpawner
	process SunshineProcess
}

// StartSunshineHost prepares the private state and starts exactly one process.
func StartSunshineHost(opts SunshineHostOptions, spawner SunshineSpawner) (*SunshineHost, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if _, err := WriteSunshineConfig(opts); err != nil {
		return nil, err
	}
	if spawner == nil {
		spawner = DefaultSunshineSpawner
	}
	args := append([]string{"--config", opts.ConfigPath}, opts.ExtraArgs...)
	process, err := spawner(opts.BinaryPath, args, opts.StateDir)
	if err != nil {
		return nil, err
	}
	return &SunshineHost{opts: opts, spawner: spawner, process: process}, nil
}

func (h *SunshineHost) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.process != nil
}

func (h *SunshineHost) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.process == nil {
		return 0
	}
	return h.process.PID()
}

// Stop terminates the supervised process group within the configured grace
// period. It is idempotent and never touches an unowned process.
func (h *SunshineHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	process := h.process
	h.process = nil
	h.mu.Unlock()
	if process == nil {
		return nil
	}

	signalErr := process.Signal(syscall.SIGTERM)
	waitResult := make(chan error, 1)
	go func() { waitResult <- process.Wait() }()

	timer := time.NewTimer(h.opts.stopGrace())
	defer timer.Stop()
	select {
	case err := <-waitResult:
		if err != nil && signalErr != nil {
			return fmt.Errorf("stop sunshine: %w", err)
		}
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	_ = process.Signal(syscall.SIGKILL)
	select {
	case <-waitResult:
		return nil
	case <-time.After(h.opts.stopGrace()):
		return errors.New("sunshine_stop_timeout")
	}
}

// Revoke stops the supervised host and removes the Zen-owned pairing state so
// the next start requires an explicit re-pair. The user's own Sunshine data is
// never touched because only paths inside the private state directory are used.
func (h *SunshineHost) Revoke(ctx context.Context) error {
	stopErr := h.Stop(ctx)
	removeErr := os.Remove(h.opts.PairingStatePath)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		if stopErr != nil {
			return fmt.Errorf("revoke sunshine: %v; %w", stopErr, removeErr)
		}
		return fmt.Errorf("revoke sunshine pairing state: %w", removeErr)
	}
	return stopErr
}

// SunshineStateDirMode is exported for callers that need to assert the private
// directory contract in diagnostics without re-deriving it.
const SunshineStateDirMode os.FileMode = 0o700
