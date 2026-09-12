package host

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/nativebind"
)

func TestPlanPrintsWithoutInstalling(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "desktop-host.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"hostId":"fixture","ownerUid":1000,"seat":"seat0"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if err := RunLinuxCLI([]string{"--plan", "--config", config}, &stderr); err != nil {
		t.Fatal(err)
	}
	rendered := stderr.String()
	if !strings.Contains(rendered, "not applied") || !strings.Contains(rendered, "/usr/libexec/zen/zen desktop-host") {
		t.Fatalf("plan missing review content:\n%s", rendered)
	}
	if err := RunLinuxCLI([]string{"--plan", "--install", "--config", config}, &stderr); err == nil {
		t.Fatal("plan combined with install")
	}
}

func TestInitializeConfigIsIdempotentAndRefusesReviewedChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "desktop-host.json")
	want := HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"}
	created, err := InitializeConfig(path, want)
	if err != nil || !created {
		t.Fatalf("first init: created=%v err=%v", created, err)
	}
	created, err = InitializeConfig(path, want)
	if err != nil || created {
		t.Fatalf("same init: created=%v err=%v", created, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	changed := want
	changed.HostID = "other"
	if _, err := InitializeConfig(path, changed); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("changed init error=%v", err)
	}
}

func TestInitConfigCLIUsesCanonicalIdentity(t *testing.T) {
	if !nativebind.NativeLinked {
		t.Skip("desktop host init requires the zen_desktop build")
	}
	previous := inspectInitHost
	inspectInitHost = func() error { return nil }
	t.Cleanup(func() { inspectInitHost = previous })
	state := t.TempDir()
	config := filepath.Join(t.TempDir(), "desktop-host.json")
	manager, err := auth.NewManager(state)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunLinuxCLI([]string{"--init-config", "--state-dir", state, "--config", config}, &output); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadHostConfig(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != manager.DaemonID() || got.OwnerUID == 0 || got.Seat != "seat0" {
		t.Fatalf("config=%+v manager=%s", got, manager.DaemonID())
	}
	if !strings.Contains(output.String(), "--plan") || !strings.Contains(output.String(), "--binary-source") {
		t.Fatalf("output missing next commands:\n%s", output.String())
	}
}

func TestInitializeConfigRejectsSymlinkAndPublicFile(t *testing.T) {
	config := HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if _, err := InitializeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeConfig(symlink, config); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeConfig(path, config); err == nil {
		t.Fatal("public config accepted")
	}
}

func TestInitMissingIdentityDoesNotCreateState(t *testing.T) {
	if !nativebind.NativeLinked {
		t.Skip("native build required")
	}
	state := filepath.Join(t.TempDir(), "missing")
	err := RunLinuxCLI([]string{"--init-config", "--state-dir", state}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "existing daemon identity required") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("setup created a new identity directory")
	}
}

func TestInitShellQuote(t *testing.T) {
	if shellQuote("a'b$HOME`id`") != "'a'\"'\"'b$HOME`id`'" {
		t.Fatal("unsafe command quoting")
	}
}

func TestInitUnsupportedHostLeavesConfigAbsent(t *testing.T) {
	if !nativebind.NativeLinked {
		t.Skip("native build required")
	}
	state := t.TempDir()
	if _, err := auth.NewManager(state); err != nil {
		t.Fatal(err)
	}
	previous := inspectInitHost
	inspectInitHost = func() error { return errors.New("Wayland lock/login is unsupported") }
	t.Cleanup(func() { inspectInitHost = previous })
	err := RunLinuxCLI([]string{"--init-config", "--state-dir", state}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "desktop-host.json")); !os.IsNotExist(err) {
		t.Fatal("unsupported host got a config")
	}
}

func TestInitConcurrentDifferentIdentitiesCannotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-host.json")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			<-start
			_, err := InitializeConfig(path, HostConfig{Version: 1, HostID: id, OwnerUID: 1000, Seat: "seat0"})
			results <- err
		}(id)
	}
	close(start)
	one, two := <-results, <-results
	if (one == nil) == (two == nil) {
		t.Fatalf("exactly one init must win: %v / %v", one, two)
	}
}
