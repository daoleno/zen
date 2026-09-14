package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SunshineRuntime is the production entry point for the supervised Sunshine
// host. It is explicitly configured (ZEN_SUNSHINE_BINARY plus a Zen-owned
// state dir) and never guesses a binary or touches a personal Sunshine install.
// The capability endpoint only advertises Moonlight when this runtime reports a
// configured host, so existing servers keep the old route until Sunshine is
// explicitly enabled.
type sunshineRuntimeConfig struct {
	BinaryPath    string `json:"binary_path"`
	StateDir      string `json:"state_dir"`
	HostKey       string `json:"host_key"`
	HTTPPort      int    `json:"http_port"`
	HTTPSPort     int    `json:"https_port"`
	AppID         int    `json:"app_id"`
	WebUIUsername string `json:"web_ui_username"`
	WebUIPassword string `json:"web_ui_password"`
}

var (
	sunshineRuntimeMu        sync.Mutex
	sunshineRuntimeHost      *SunshineHost
	sunshineRuntimeOptions   SunshineHostOptions
	sunshineRuntimeCfg       sunshineRuntimeConfig
	sunshineRuntimeActiveCfg sunshineRuntimeConfig
	sunshineRuntimeError     string
)

// SunshineConfigPath is the Zen-owned explicit configuration file.
func SunshineConfigPath() string {
	if override := os.Getenv("ZEN_SUNSHINE_CONFIG"); override != "" {
		return override
	}
	return filepath.Join(ZenStateDir(), "sunshine.json")
}

// ZenStateDir returns the private Zen desktop state directory.
func ZenStateDir() string {
	if override := os.Getenv("ZEN_STATE_DIR"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/zen-desktop"
	}
	return filepath.Join(home, ".zen", "desktop")
}

func loadSunshineRuntimeConfig() (sunshineRuntimeConfig, error) {
	var cfg sunshineRuntimeConfig
	path := SunshineConfigPath()
	body, err := os.ReadFile(path)
	if err != nil && os.Getenv("ZEN_SUNSHINE_CONFIG") == "" {
		// The reviewed desktop candidate keeps its runtime versioned under
		// ~/.local/lib/zen/remote-desktop-*/.  Discover that explicit layout
		// automatically while retaining ~/.zen/desktop as the canonical path
		// when it exists.  No arbitrary system Sunshine installation is used.
		if discovered := discoverSunshineConfig(); discovered != "" {
			path = discovered
			body, err = os.ReadFile(path)
		}
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return cfg, err
	}
	if binary := os.Getenv("ZEN_SUNSHINE_BINARY"); binary != "" {
		cfg.BinaryPath = binary
	}
	return cfg, nil
}

func discoverSunshineConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	paths, err := filepath.Glob(filepath.Join(home, ".local", "lib", "zen", "remote-desktop-*", "sunshine.json"))
	if err != nil || len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	for i := len(paths) - 1; i >= 0; i-- {
		info, statErr := os.Lstat(paths[i])
		if statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 {
			return paths[i]
		}
	}
	return ""
}

// SunshineConfigured reports whether the operator explicitly configured the
// supervised Sunshine host.
func SunshineConfigured() bool {
	_, err := loadSunshineRuntimeConfig()
	return err == nil
}

// SunshineAdminFromRuntime builds the authenticated, certificate-pinned admin
// client used for per-device enrollment and removal. While a host is running,
// the immutable started configuration is used instead of a newer disk config.
func SunshineAdminFromRuntime() (*SunshineAdmin, error) {
	sunshineRuntimeMu.Lock()
	cfg := sunshineRuntimeCfg
	if sunshineRuntimeHost != nil && sunshineRuntimeHost.Running() {
		cfg = sunshineRuntimeActiveCfg
	}
	sunshineRuntimeMu.Unlock()
	if cfg.BinaryPath == "" {
		var err error
		cfg, err = loadSunshineRuntimeConfig()
		if err != nil {
			return nil, err
		}
	}
	if !strings.HasPrefix(cfg.StateDir, "/") {
		return nil, errors.New("invalid_sunshine_state_dir")
	}
	return NewSunshineAdmin(
		fmt.Sprintf("https://127.0.0.1:%d", cfg.HTTPPort+1),
		cfg.WebUIUsername,
		cfg.WebUIPassword,
		filepath.Join(cfg.StateDir, "sunshine.crt"),
	)
}

// SunshineStateFilePath is the Zen-owned upstream state file (file_state).
func SunshineStateFilePath() string {
	sunshineRuntimeMu.Lock()
	stateDir := sunshineRuntimeCfg.StateDir
	if sunshineRuntimeHost != nil && sunshineRuntimeHost.Running() {
		stateDir = sunshineRuntimeActiveCfg.StateDir
	}
	sunshineRuntimeMu.Unlock()
	if stateDir == "" {
		if cfg, err := loadSunshineRuntimeConfig(); err == nil && cfg.StateDir != "" {
			stateDir = cfg.StateDir
		}
	}
	if stateDir == "" {
		stateDir = ZenStateDir()
	}
	return filepath.Join(stateDir, "sunshine_state.json")
}

// SunshineAvailable requires a running host, a constructible pinned admin
// client and a readable Zen-owned state file. Enrollment readiness is reported
// separately so availability never implies a verified per-device enrollment.
func SunshineAvailable() bool {
	snapshot := SunshineSnapshot()
	if !snapshot.Configured || !snapshot.Running {
		return false
	}
	admin, err := SunshineAdminFromRuntime()
	if err != nil || admin == nil {
		return false
	}
	if _, err := os.Stat(SunshineStateFilePath()); err != nil {
		return false
	}
	return true
}

// SunshineAvailabilityReason reports the last supervised-host start failure so
// the capability endpoint explains why Moonlight is unavailable instead of
// claiming a generic binding problem.
func SunshineAvailabilityReason() string {
	sunshineRuntimeMu.Lock()
	defer sunshineRuntimeMu.Unlock()
	if sunshineRuntimeError != "" {
		return sunshineRuntimeError
	}
	return "admin_binding_unavailable"
}

// EnsureSunshineRuntime is the production caller: it starts the supervised
// Sunshine host once when explicit configuration exists. The spawner is
// injectable for tests; production passes nil for the default spawner. When the
// daemon was started outside the owner desktop session (for example over SSH),
// the owner session is discovered from logind and its environment is passed to
// the supervised process so it attaches to the same KDE session.
func EnsureSunshineRuntime(ctx context.Context, spawner SunshineSpawner) (SunshineRuntimeSnapshot, error) {
	cfg, err := loadSunshineRuntimeConfig()
	if err != nil {
		return SunshineRuntimeSnapshot{}, nil
	}
	sunshineRuntimeMu.Lock()
	defer sunshineRuntimeMu.Unlock()

	if sunshineRuntimeHost != nil && sunshineRuntimeHost.Running() {
		// The running process keeps its started identity; a config edit on disk
		// is not published as the running host until a restart.
		return sunshineSnapshotLocked(), nil
	}
	sunshineRuntimeCfg = cfg

	stateFile := filepath.Join(cfg.StateDir, "sunshine_state.json")
	// Sunshine derives HTTPS (base-5), HTTP (base) and RTSP (base+21) ports
	// from one base port; the config carries only that base.
	opts := DefaultSunshineHostOptions(cfg.BinaryPath, cfg.StateDir, cfg.HTTPPort)
	opts.StateFilePath = stateFile
	opts.CredentialsFilePath = filepath.Join(cfg.StateDir, "sunshine_creds.json")
	opts.PKeyPath = filepath.Join(cfg.StateDir, "sunshine.key")
	opts.CertPath = filepath.Join(cfg.StateDir, "sunshine.crt")
	opts.AppsFilePath = filepath.Join(cfg.StateDir, "apps.json")
	sessionEnv, sessionErr := sessionEnvironmentForHost(uint32(os.Getuid()))
	if sessionErr != nil {
		sunshineRuntimeError = sessionErr.Error()
		return sunshineSnapshotLocked(), fmt.Errorf("start sunshine runtime: %w", sessionErr)
	}
	opts.SessionEnv = sessionEnv
	host, err := StartSunshineHost(opts, spawner)
	if err != nil {
		sunshineRuntimeError = err.Error()
		return sunshineSnapshotLocked(), fmt.Errorf("start sunshine runtime: %w", err)
	}
	sunshineRuntimeHost = host
	sunshineRuntimeOptions = opts
	sunshineRuntimeActiveCfg = cfg
	sunshineRuntimeError = ""
	return sunshineSnapshotLocked(), nil
}

// sessionEnvironmentForHost returns the owner desktop session environment when
// this process is not already running inside that session. A daemon that
// already carries the display environment keeps it unchanged.
func sessionEnvironmentForHost(uid uint32) ([]string, error) {
	if strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "" || strings.TrimSpace(os.Getenv("DISPLAY")) != "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := discoverHostSession(ctx, uid)
	if err != nil {
		return nil, err
	}
	return session.Environment(), nil
}

// discoverHostSession is injectable so unit tests do not depend on the
// developer's live desktop session.
var discoverHostSession = DiscoverOwnerSession

// StopSunshineRuntime stops the supervised host. Idempotent.
func StopSunshineRuntime(ctx context.Context) error {
	sunshineRuntimeMu.Lock()
	host := sunshineRuntimeHost
	sunshineRuntimeMu.Unlock()
	if host == nil {
		return nil
	}
	if err := host.Stop(ctx); err != nil {
		// Keep the handle so a retry can signal the still-running process.
		return err
	}
	sunshineRuntimeMu.Lock()
	if sunshineRuntimeHost == host {
		sunshineRuntimeHost = nil
		sunshineRuntimeActiveCfg = sunshineRuntimeConfig{}
	}
	sunshineRuntimeMu.Unlock()
	return nil
}

// SunshineSnapshot reports the configured/running state for capability
// advertisement without starting anything.
func SunshineSnapshot() SunshineRuntimeSnapshot {
	sunshineRuntimeMu.Lock()
	defer sunshineRuntimeMu.Unlock()
	if sunshineRuntimeCfg.HostKey == "" {
		if cfg, err := loadSunshineRuntimeConfig(); err == nil {
			sunshineRuntimeCfg = cfg
		}
	}
	return sunshineSnapshotLocked()
}

func sunshineSnapshotLocked() SunshineRuntimeSnapshot {
	running := sunshineRuntimeHost != nil && sunshineRuntimeHost.Running()
	cfg := sunshineRuntimeCfg
	if running {
		cfg = sunshineRuntimeActiveCfg
	}
	if cfg.HostKey == "" && cfg.BinaryPath == "" {
		return SunshineRuntimeSnapshot{}
	}
	return SunshineRuntimeSnapshot{
		Configured: true,
		Running:    running,
		HostKey:    cfg.HostKey,
		HTTPPort:   cfg.HTTPPort,
		HTTPSPort:  cfg.HTTPPort - 5,
		AppID:      cfg.AppID,
	}
}

// RevokeSunshineRuntime stops the host and removes only the Zen-owned pairing
// state file.
func RevokeSunshineRuntime(ctx context.Context) error {
	sunshineRuntimeMu.Lock()
	host := sunshineRuntimeHost
	opts := sunshineRuntimeOptions
	sunshineRuntimeMu.Unlock()
	if host == nil {
		if opts.StateFilePath == "" {
			return nil
		}
		err := os.Remove(opts.StateFilePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := host.Revoke(ctx); err != nil {
		// Keep the handle: a failed revoke must remain retryable.
		return err
	}
	sunshineRuntimeMu.Lock()
	if sunshineRuntimeHost == host {
		sunshineRuntimeHost = nil
		sunshineRuntimeActiveCfg = sunshineRuntimeConfig{}
	}
	sunshineRuntimeMu.Unlock()
	return nil
}
