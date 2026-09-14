package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestAuthorizeEnsuresUserUnitAndIsIdempotent(t *testing.T) {
	state := t.TempDir()
	if _, err := auth.NewManager(state); err != nil {
		t.Fatal(err)
	}
	oldConfig := os.Getenv("ZEN_SUNSHINE_CONFIG")
	oldSystemctl := runDesktopUserSystemctl
	oldProbe := probeCanonicalDaemon
	os.Setenv("ZEN_SUNSHINE_CONFIG", filepath.Join(t.TempDir(), "sunshine-not-configured.json"))
	probeCanonicalDaemon = func(string) bool { return false }
	t.Cleanup(func() {
		os.Setenv("ZEN_SUNSHINE_CONFIG", oldConfig)
		runDesktopUserSystemctl = oldSystemctl
		probeCanonicalDaemon = oldProbe
	})
	var calls [][]string
	running := false
	runDesktopUserSystemctl = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "is-active" {
			if running {
				return nil, nil
			}
			return nil, errors.New("inactive")
		}
		if len(args) >= 2 && args[0] == "enable" {
			running = true
		}
		return nil, nil
	}
	if err := RunLinuxCLI([]string{"authorize", "--state-dir", state}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0][0] != "is-active" || calls[1][0] != "enable" || calls[2][0] != "is-active" {
		t.Fatalf("systemctl calls=%v", calls)
	}
	calls = nil
	if err := RunLinuxCLI([]string{"authorize", "--state-dir", state}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0][0] != "is-active" {
		t.Fatalf("repeat calls=%v", calls)
	}
}

func TestAuthorizeReusesRunningCanonicalDaemon(t *testing.T) {
	state := t.TempDir()
	if _, err := auth.NewManager(state); err != nil {
		t.Fatal(err)
	}
	oldConfig, oldSystemctl, oldProbe := os.Getenv("ZEN_SUNSHINE_CONFIG"), runDesktopUserSystemctl, probeCanonicalDaemon
	os.Setenv("ZEN_SUNSHINE_CONFIG", filepath.Join(t.TempDir(), "sunshine-not-configured.json"))
	probeCanonicalDaemon = func(got string) bool { return got == state }
	var calls [][]string
	runDesktopUserSystemctl = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, errors.New("unit missing")
	}
	t.Cleanup(func() {
		os.Setenv("ZEN_SUNSHINE_CONFIG", oldConfig)
		runDesktopUserSystemctl = oldSystemctl
		probeCanonicalDaemon = oldProbe
	})
	if err := RunLinuxCLI([]string{"authorize", "--state-dir", state}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0][0] != "is-active" {
		t.Fatalf("systemctl calls=%v; existing canonical daemon should not be started", calls)
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

func TestStatusCLIReadsAuthorizationReportWithoutSideEffects(t *testing.T) {
	stateDir := t.TempDir()
	devices := `{"devices":[{"id":"dev-1","desktop_scope_version":1},{"id":"dev-2"}]}`
	if err := os.WriteFile(filepath.Join(stateDir, "trusted-devices.json"), []byte(devices), 0600); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "authorization-status.json")
	record := AuthorizationStatus{
		Version: 1, Active: true, Reason: "authorization_active", AuthorizedDevices: 1,
		Inhibitors: InhibitorStatus{Active: true, IdleInhibited: true, SuspendInhibited: true, LockInhibited: true, LogindIdle: "active", LogindSleep: "active", ScreenSaver: "active"},
		UpdatedAt:  time.Now().UTC(), PID: os.Getpid(),
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_DESKTOP_AUTHORIZATION_STATUS", statusPath)

	var human bytes.Buffer
	if err := RunLinuxCLI([]string{"--status", "--state-dir", stateDir}, &human); err != nil {
		t.Fatalf("status: %v", err)
	}
	rendered := human.String()
	if !strings.Contains(rendered, "persisted authorization: 1 device(s)") || !strings.Contains(rendered, "daemon status record: fresh pid=") || !strings.Contains(rendered, "inhibitor: idle=active sleep=active lock=active") {
		t.Fatalf("status output:\n%s", rendered)
	}

	var jsonOut bytes.Buffer
	if err := RunLinuxCLI([]string{"--status", "--json", "--state-dir", stateDir}, &jsonOut); err != nil {
		t.Fatalf("status json: %v", err)
	}
	var report AuthorizationReport
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, jsonOut.String())
	}
	if report.PersistedDevices != 1 || !report.StatusFresh || report.Status.PID != os.Getpid() || !report.Status.Active {
		t.Fatalf("report = %+v", report)
	}

	if err := RunLinuxCLI([]string{"--status", "--install", "--state-dir", stateDir}, &bytes.Buffer{}); err == nil {
		t.Fatal("--status combined with --install was accepted")
	}
	if err := RunLinuxCLI([]string{"--json", "--state-dir", stateDir}, &bytes.Buffer{}); err == nil {
		t.Fatal("--json without --status was accepted")
	}
}

func TestStatusPathWithStateRootMatchesDesktopRecord(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "desktop"), 0700); err != nil {
		t.Fatal(err)
	}
	devices := `{"devices":[{"id":"dev-1","desktop_scope_version":1}]}`
	if err := os.WriteFile(filepath.Join(root, "trusted-devices.json"), []byte(devices), 0600); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(root, "desktop", "authorization-status.json")
	status := AuthorizationStatus{Version: 1, Active: false, Reason: "session_unavailable", Error: "owner_session_locked", Recovery: "sudo loginctl unlock-session <active-session-id>", PID: os.Getpid(), UpdatedAt: time.Now().UTC()}
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_STATE_DIR", root)
	t.Setenv("ZEN_DESKTOP_AUTHORIZATION_STATUS", "")
	var out bytes.Buffer
	if err := RunLinuxCLI([]string{"--status", "--json", "--state-dir", root}, &out); err != nil {
		t.Fatal(err)
	}
	var report AuthorizationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.StatusPath != statusPath || !report.StatusFresh || report.Status.Recovery == "" {
		t.Fatalf("report=%+v", report)
	}
}
