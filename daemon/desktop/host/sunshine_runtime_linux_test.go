package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeSunshineRuntimeConfig(t *testing.T, dir string, cfg sunshineRuntimeConfig) {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sunshine.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_SUNSHINE_CONFIG", path)
}

func TestSunshineRuntimeNotConfiguredStartsNothing(t *testing.T) {
	t.Setenv("ZEN_SUNSHINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	spawned := 0
	snapshot, err := EnsureSunshineRuntime(func(string, []string, string) (SunshineProcess, error) {
		spawned++
		return newFakeSunshineProcess(1), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.Configured || spawned != 0 {
		t.Fatalf("configured=%v spawned=%d", snapshot.Configured, spawned)
	}
	if SunshineConfigured() {
		t.Fatal("unconfigured runtime reported configured")
	}
}

func TestSunshineRuntimeStartsOnceAndStopsIdempotently(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "sunshine")
	writeSunshineRuntimeConfig(t, dir, sunshineRuntimeConfig{
		BinaryPath: "/usr/libexec/zen/sunshine",
		StateDir:   configDir,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		AppID:      1,
	})
	process := newFakeSunshineProcess(4242)
	spawned := 0
	spawner := func(string, []string, string) (SunshineProcess, error) {
		spawned++
		return process, nil
	}
	snapshot, err := EnsureSunshineRuntime(spawner)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !snapshot.Configured || !snapshot.Running || spawned != 1 {
		t.Fatalf("snapshot=%+v spawned=%d", snapshot, spawned)
	}
	if snapshot.HTTPSPort != 47984 {
		t.Fatalf("https port = %d", snapshot.HTTPSPort)
	}
	// Second ensure reuses the running host.
	again, err := EnsureSunshineRuntime(spawner)
	if err != nil || spawned != 1 || !again.Running {
		t.Fatalf("second ensure: %+v spawned=%d err=%v", again, spawned, err)
	}
	if got := SunshineSnapshot(); !got.Running || got.HostKey != "zen-host-1" {
		t.Fatalf("snapshot = %+v", got)
	}
	if err := StopSunshineRuntime(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := StopSunshineRuntime(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if got := SunshineSnapshot(); got.Running {
		t.Fatalf("still running: %+v", got)
	}
}

func TestSunshineRuntimeRevokeRemovesOnlyStateFile(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "sunshine")
	writeSunshineRuntimeConfig(t, dir, sunshineRuntimeConfig{
		BinaryPath: "/usr/libexec/zen/sunshine",
		StateDir:   configDir,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		AppID:      1,
	})
	process := newFakeSunshineProcess(7)
	if _, err := EnsureSunshineRuntime(func(string, []string, string) (SunshineProcess, error) {
		return process, nil
	}); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(configDir, "sunshine_state.json")
	if err := os.WriteFile(stateFile, []byte("pair"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(configDir, "sunshine.crt")
	if err := os.WriteFile(keep, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RevokeSunshineRuntime(context.Background()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("state file still present: %v", err)
	}
	if _, err := os.Lstat(keep); err != nil {
		t.Fatalf("certificate removed: %v", err)
	}
}
