package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SunshineRuntime is the production entry point for the supervised Sunshine
// host. It is explicitly configured (ZEN_SUNSHINE_BINARY plus a Zen-owned
// state dir) and never guesses a binary or touches a personal Sunshine install.
// The capability endpoint only advertises Moonlight when this runtime reports a
// configured host, so existing servers keep the old route until Sunshine is
// explicitly enabled.
type SunshineRuntimeSnapshot struct {
	Configured bool   `json:"configured"`
	Running    bool   `json:"running"`
	HostKey    string `json:"host_key"`
	HTTPPort   int    `json:"http_port"`
	HTTPSPort  int    `json:"https_port"`
	AppID      int    `json:"app_id"`
}

type sunshineRuntimeConfig struct {
	BinaryPath string `json:"binary_path"`
	StateDir   string `json:"state_dir"`
	HostKey    string `json:"host_key"`
	HTTPPort   int    `json:"http_port"`
	HTTPSPort  int    `json:"https_port"`
	AppID      int    `json:"app_id"`
}

var (
	sunshineRuntimeMu      sync.Mutex
	sunshineRuntimeHost    *SunshineHost
	sunshineRuntimeOptions SunshineHostOptions
	sunshineRuntimeCfg     sunshineRuntimeConfig
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
	body, err := os.ReadFile(SunshineConfigPath())
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

// SunshineConfigured reports whether the operator explicitly configured the
// supervised Sunshine host.
func SunshineConfigured() bool {
	_, err := loadSunshineRuntimeConfig()
	return err == nil
}

// EnsureSunshineRuntime is the production caller: it starts the supervised
// Sunshine host once when explicit configuration exists. The spawner is
// injectable for tests; production passes nil for the default spawner.
func EnsureSunshineRuntime(spawner SunshineSpawner) (SunshineRuntimeSnapshot, error) {
	cfg, err := loadSunshineRuntimeConfig()
	if err != nil {
		return SunshineRuntimeSnapshot{}, nil
	}
	sunshineRuntimeMu.Lock()
	defer sunshineRuntimeMu.Unlock()

	sunshineRuntimeCfg = cfg
	if sunshineRuntimeHost != nil && sunshineRuntimeHost.Running() {
		return sunshineSnapshotLocked(), nil
	}

	stateFile := filepath.Join(cfg.StateDir, "sunshine_state.json")
	// Sunshine derives HTTPS (base-5), HTTP (base) and RTSP (base+21) ports
	// from one base port; the config carries only that base.
	opts := DefaultSunshineHostOptions(cfg.BinaryPath, cfg.StateDir, cfg.HTTPPort)
	opts.StateFilePath = stateFile
	opts.CredentialsFilePath = filepath.Join(cfg.StateDir, "sunshine_creds.json")
	opts.PKeyPath = filepath.Join(cfg.StateDir, "sunshine.key")
	opts.CertPath = filepath.Join(cfg.StateDir, "sunshine.crt")
	opts.AppsFilePath = filepath.Join(cfg.StateDir, "apps.json")
	host, err := StartSunshineHost(opts, spawner)
	if err != nil {
		return sunshineSnapshotLocked(), fmt.Errorf("start sunshine runtime: %w", err)
	}
	sunshineRuntimeHost = host
	sunshineRuntimeOptions = opts
	return sunshineSnapshotLocked(), nil
}

// StopSunshineRuntime stops the supervised host. Idempotent.
func StopSunshineRuntime(ctx context.Context) error {
	sunshineRuntimeMu.Lock()
	host := sunshineRuntimeHost
	sunshineRuntimeHost = nil
	sunshineRuntimeMu.Unlock()
	if host == nil {
		return nil
	}
	return host.Stop(ctx)
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
	cfg := sunshineRuntimeCfg
	if cfg.HostKey == "" && cfg.BinaryPath == "" {
		return SunshineRuntimeSnapshot{}
	}
	return SunshineRuntimeSnapshot{
		Configured: true,
		Running:    sunshineRuntimeHost != nil && sunshineRuntimeHost.Running(),
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
	sunshineRuntimeHost = nil
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
	return host.Revoke(ctx)
}
