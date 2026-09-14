package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SunshineHost supervises the pinned Sunshine host process that Zen owns.
//
// Sunshine reads its configuration from the positional path passed on the
// command line (src/config.cpp: a bare argument selects the config file; an
// argument starting with "--" is parsed as a command name and main.cpp rejects
// unknown commands). This type therefore launches "<binary> <config> [key=value
// overrides...]" and never uses a "--config" flag.
//
// Isolation is expressed in the configuration itself: the state file
// (upstream "file_state"), credentials, private key, certificate and app list
// are all bound to the private 0700 state directory. An environment variable is
// not counted as isolation proof.
//
// Lifecycle is explicit: Start spawns one process group and reaps it on a
// goroutine, Running reflects the reaped state, Stop reports its real signal
// and wait results and keeps the handle on failure so a retry can signal again,
// and Revoke removes only the Zen-owned state file after the process is gone.
const defaultSunshineStopGrace = 5 * time.Second

type SunshineHostOptions struct {
	// BinaryPath is the reviewed Sunshine executable; must be absolute.
	BinaryPath string
	// StateDir is the Zen-owned private directory; must be absolute and 0700.
	StateDir string
	// ConfigPath is the Sunshine config file; upstream positional argument.
	ConfigPath string
	// StateFilePath maps to upstream "file_state" (pairing/credential state).
	StateFilePath string
	// CredentialsFilePath maps to upstream "credentials_file".
	CredentialsFilePath string
	// PKeyPath maps to upstream "pkey".
	PKeyPath string
	// CertPath maps to upstream "cert".
	CertPath string
	// AppsFilePath maps to upstream "file_apps".
	AppsFilePath string
	// Port is the Sunshine base port; Sunshine validates its own narrower bound.
	Port int
	// StopGrace bounds SIGTERM before SIGKILL. Zero uses the default.
	StopGrace time.Duration
	// SessionEnv is the owner desktop session environment appended to the
	// daemon environment. It is resolved from logind and the owner runtime
	// directory when the daemon was started outside the desktop session (for
	// example over SSH), so the supervised host attaches to the same session
	// instead of creating a replacement one.
	SessionEnv []string
}

// DefaultSunshineHostOptions builds the private layout Zen owns under stateDir.
func DefaultSunshineHostOptions(binaryPath, stateDir string, port int) SunshineHostOptions {
	return SunshineHostOptions{
		BinaryPath:          binaryPath,
		StateDir:            stateDir,
		ConfigPath:          filepath.Join(stateDir, "sunshine.conf"),
		StateFilePath:       filepath.Join(stateDir, "sunshine_state.json"),
		CredentialsFilePath: filepath.Join(stateDir, "sunshine_creds.json"),
		PKeyPath:            filepath.Join(stateDir, "sunshine.key"),
		CertPath:            filepath.Join(stateDir, "sunshine.crt"),
		AppsFilePath:        filepath.Join(stateDir, "apps.json"),
		Port:                port,
	}
}

func (o SunshineHostOptions) stopGrace() time.Duration {
	if o.StopGrace > 0 {
		return o.StopGrace
	}
	return defaultSunshineStopGrace
}

func (o SunshineHostOptions) statePaths() map[string]string {
	return map[string]string{
		"config_path":      o.ConfigPath,
		"state_file_path":  o.StateFilePath,
		"credentials_path": o.CredentialsFilePath,
		"pkey_path":        o.PKeyPath,
		"cert_path":        o.CertPath,
		"apps_file_path":   o.AppsFilePath,
	}
}

func (o SunshineHostOptions) validate() error {
	if o.BinaryPath == "" || !filepath.IsAbs(o.BinaryPath) || filepath.Clean(o.BinaryPath) != o.BinaryPath {
		return errors.New("invalid_sunshine_binary")
	}
	if o.StateDir == "" || !filepath.IsAbs(o.StateDir) || filepath.Clean(o.StateDir) != o.StateDir {
		return errors.New("invalid_sunshine_state_dir")
	}
	seen := make(map[string]string)
	for name, path := range o.statePaths() {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
			strings.ContainsAny(path, "\n#=") {
			return fmt.Errorf("invalid_%s", name)
		}
		if filepath.Dir(path) != o.StateDir {
			return fmt.Errorf("sunshine_%s_outside_state_dir", name)
		}
		if other, exists := seen[path]; exists {
			return fmt.Errorf("sunshine_%s_collides_with_%s", name, other)
		}
		seen[path] = name
	}
	// Sunshine accepts port in [1024 + PORT_HTTPS, 65535 - RTSP_SETUP_PORT];
	// the exact upstream bound is enforced by its own parser at start time.
	if o.Port < 1024 || o.Port > 65000 {
		return errors.New("invalid_sunshine_port")
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

func (o SunshineHostOptions) configBody() string {
	lines := []string{
		"cert = " + o.CertPath,
		"credentials_file = " + o.CredentialsFilePath,
		"file_apps = " + o.AppsFilePath,
		"file_state = " + o.StateFilePath,
		"pkey = " + o.PKeyPath,
		fmt.Sprintf("port = %d", o.Port),
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
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
	if err := os.WriteFile(opts.ConfigPath, []byte(opts.configBody()), 0o600); err != nil {
		return false, fmt.Errorf("write sunshine config: %w", err)
	}
	return true, nil
}

// DefaultSunshineSpawner starts the reviewed binary in its own process group so
// stop signals reach the whole supervised tree and never the daemon's group.
// No guessed environment variables are used for isolation: the config file
// binds every state path. An explicit session env is appended after the daemon
// environment so it wins over an unrelated SSH login environment.
func DefaultSunshineSpawner(binary string, args []string, dir string, env []string) (SunshineProcess, error) {
	command := exec.Command(binary, args...)
	command.Dir = dir
	if len(env) > 0 {
		command.Env = append(os.Environ(), env...)
	}
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
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
	done    chan struct{}
	waitErr error
	exited  bool
}

// defaultSunshineApps is the single Zen-owned desktop application. Sunshine
// normally copies this file from its install prefix on first start, but the
// reviewed launcher is intentionally relocatable and has no shared
// /usr/local/assets tree. Keeping the file in the private state directory
// makes app id 1 (the Desktop route) deterministic without touching a user's
// existing Sunshine installation.
const defaultSunshineApps = `{"env":{},"apps":[{"name":"Desktop","cmd":""}]}
`

func ensureSunshineAppsFile(opts SunshineHostOptions) error {
	if _, err := os.Lstat(opts.AppsFilePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat sunshine apps: %w", err)
	}
	if err := os.WriteFile(opts.AppsFilePath, []byte(defaultSunshineApps), 0o600); err != nil {
		return fmt.Errorf("write sunshine apps: %w", err)
	}
	return nil
}

// StartSunshineHost prepares the private state and starts exactly one process.
func StartSunshineHost(opts SunshineHostOptions, spawner SunshineSpawner) (*SunshineHost, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if _, err := WriteSunshineConfig(opts); err != nil {
		return nil, err
	}
	if err := ensureSunshineAppsFile(opts); err != nil {
		return nil, err
	}
	if spawner == nil {
		spawner = DefaultSunshineSpawner
	}
	args := []string{opts.ConfigPath}
	process, err := spawner(opts.BinaryPath, args, opts.StateDir, opts.SessionEnv)
	if err != nil {
		return nil, err
	}
	host := &SunshineHost{opts: opts, spawner: spawner, process: process, done: make(chan struct{})}
	go host.reap(process)
	return host, nil
}

// reap records the real process exit so Running and Stop are not pointer
// liveness guesses.
func (h *SunshineHost) reap(process SunshineProcess) {
	err := process.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.process != process {
		return
	}
	h.waitErr = err
	h.exited = true
	close(h.done)
}

func (h *SunshineHost) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.process != nil && !h.exited
}

func (h *SunshineHost) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.process == nil {
		return 0
	}
	return h.process.PID()
}

func (h *SunshineHost) waitForExit(done chan struct{}, budget time.Duration, ctx context.Context) bool {
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// Stop terminates the supervised process group within the configured grace
// period. It is idempotent. On a signal or wait failure the handle is kept so a
// retry can signal again instead of silently dropping a live process.
func (h *SunshineHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	process := h.process
	done := h.done
	h.mu.Unlock()
	if process == nil {
		return nil
	}

	signalErr := process.Signal(syscall.SIGTERM)
	if h.waitForExit(done, h.opts.stopGrace(), ctx) {
		h.clear(process)
		return nil
	}

	killErr := process.Signal(syscall.SIGKILL)
	if h.waitForExit(done, h.opts.stopGrace(), ctx) {
		h.clear(process)
		return nil
	}
	if signalErr != nil {
		return fmt.Errorf("stop sunshine: %w", signalErr)
	}
	if killErr != nil {
		return fmt.Errorf("kill sunshine: %w", killErr)
	}
	return errors.New("sunshine_stop_timeout")
}

func (h *SunshineHost) clear(process SunshineProcess) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.process == process {
		h.process = nil
		h.done = nil
		h.waitErr = nil
	}
}

// Revoke stops the supervised host and removes only the Zen-owned upstream
// file_state so the next start requires an explicit re-pair. Certificate,
// private key, credentials and app list are preserved for isolation review.
func (h *SunshineHost) Revoke(ctx context.Context) error {
	stopErr := h.Stop(ctx)
	removeErr := os.Remove(h.opts.StateFilePath)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		if stopErr != nil {
			return fmt.Errorf("revoke sunshine: %v; %w", stopErr, removeErr)
		}
		return fmt.Errorf("revoke sunshine state file: %w", removeErr)
	}
	return stopErr
}

// SunshineStateDirMode is exported for callers that need to assert the private
// directory contract in diagnostics without re-deriving it.
const SunshineStateDirMode os.FileMode = 0o700
