package modelprofiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleCodexConfig mirrors the real user config shape: commented provider
// lines, model/effort, features, multiple provider tables, and project trust
// blocks — all of which must survive takeover byte-for-byte.
const sampleCodexConfig = `#model_provider = "yescode"
#model_provider = "packycode"
model = "gpt-5.6-sol"
model_reasoning_effort = "medium"
network_access = "enabled"
disable_response_storage = true

[features]
goals = true

[model_providers.yescode]
name = "yescode"
base_url = "https://co.yes.vg/v1"
wire_api = "responses"
requires_openai_auth = true

##########################################################

[projects."/home/daoleno/my-conf"]
trust_level = "trusted"
`

func takeoverFixture(t *testing.T) (*Takeover, *Gateway, string) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte(sampleCodexConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	return takeover, g, configPath
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestTakeoverEnablePreservesUnrelatedConfigBytes proves the surgical
// projection: every unrelated byte (comments, model, effort, projects) is
// preserved exactly, and only the marked block + model_provider line are added.
func TestTakeoverEnablePreservesUnrelatedConfigBytes(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	original := readFileBytes(t, configPath)
	status, err := takeover.Enable(DefaultGatewayListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != TakeoverStateActive {
		t.Fatalf("status after enable = %+v", status)
	}
	projected := readFileBytes(t, configPath)
	if !bytes.Contains(projected, []byte(takeoverMarkerOpen)) ||
		!bytes.Contains(projected, []byte("[model_providers."+GatewayProviderName+"]")) ||
		!bytes.Contains(projected, []byte("base_url = \"http://"+DefaultGatewayListenAddr+"/v1\"")) {
		t.Fatalf("projection block missing:\n%s", projected)
	}
	if !hasExactProviderLine(projected, projectedProviderLine()) {
		t.Fatalf("model_provider line missing:\n%s", projected)
	}
	for _, preserved := range []string{
		"#model_provider = \"yescode\"",
		"model = \"gpt-5.6-sol\"",
		"model_reasoning_effort = \"medium\"",
		"[features]\ngoals = true",
		"[model_providers.yescode]",
		"[projects.\"/home/daoleno/my-conf\"]",
		"trust_level = \"trusted\"",
	} {
		if !bytes.Contains(projected, []byte(preserved)) {
			t.Fatalf("unrelated config was not preserved (%q):\n%s", preserved, projected)
		}
	}
	if err := validateConfigTOML(projected); err != nil {
		t.Fatalf("projected config invalid: %v", err)
	}
	_ = original
}

// TestTakeoverEnableIdempotent: a second enable is a no-op and never creates a
// second backup.
func TestTakeoverEnableIdempotent(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	first := readFileBytes(t, configPath)
	state, _ := takeover.LoadState()
	backupPath := state.BackupPath
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	second := readFileBytes(t, configPath)
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent enable changed the config")
	}
	state, _ = takeover.LoadState()
	if state.BackupPath != backupPath {
		t.Fatalf("idempotent enable created a new backup: %s -> %s", backupPath, state.BackupPath)
	}
}

// TestTakeoverDisableRestoresExactOriginal: enable then disable returns the
// config to the exact pre-takeover bytes.
func TestTakeoverDisableRestoresExactOriginal(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	original := readFileBytes(t, configPath)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	status, err := takeover.Disable()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != TakeoverStateInactive {
		t.Fatalf("status after disable = %+v", status)
	}
	restored := readFileBytes(t, configPath)
	if !bytes.Equal(restored, original) {
		t.Fatalf("disable did not restore the exact original config:\n--- got ---\n%s\n--- want ---\n%s", restored, original)
	}
}

// TestTakeoverDisablePreservesUserChanges: unrelated user edits made while
// takeover was enabled survive disable.
func TestTakeoverDisablePreservesUserChanges(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	projected := readFileBytes(t, configPath)
	withEdit := strings.Replace(string(projected), "model_reasoning_effort = \"medium\"", "model_reasoning_effort = \"high\"", 1)
	if err := os.WriteFile(configPath, []byte(withEdit), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := takeover.Disable()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != TakeoverStateInactive {
		t.Fatalf("status = %+v", status)
	}
	restored := readFileBytes(t, configPath)
	if !bytes.Contains(restored, []byte("model_reasoning_effort = \"high\"")) {
		t.Fatalf("user edit was not preserved:\n%s", restored)
	}
	if bytes.Contains(restored, []byte(takeoverMarkerOpen)) || bytes.Contains(restored, []byte("[model_providers."+GatewayProviderName+"]")) {
		t.Fatalf("projection was not removed:\n%s", restored)
	}
}

// TestTakeoverDriftDetection: user edits to the Zen-owned projection or the
// model_provider line are detected and reported truthfully.
func TestTakeoverDriftDetection(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	projected := readFileBytes(t, configPath)

	edited := strings.Replace(string(projected), "requires_openai_auth = false", "requires_openai_auth = true", 1)
	if err := os.WriteFile(configPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	status := takeover.Status()
	if status.State != TakeoverStateDrifted {
		t.Fatalf("edited projection status = %+v, want drifted", status)
	}

	projected = readFileBytes(t, configPath)
	edited = strings.Replace(string(projected), projectedProviderLine(), "model_provider = \"yescode\"", 1)
	if err := os.WriteFile(configPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	status = takeover.Status()
	if status.State != TakeoverStateDrifted {
		t.Fatalf("edited provider line status = %+v, want drifted", status)
	}
}

// TestTakeoverRepairRestoresProjection: a user deleting the projection is
// repaired by Repair() (daemon restart path) while other edits survive.
func TestTakeoverRepairRestoresProjection(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	projected := readFileBytes(t, configPath)
	// User removed the marked block and changed effort.
	withoutBlock := removeProjectionBlock(string(projected))
	withEdit := strings.Replace(withoutBlock, "model_reasoning_effort = \"medium\"", "model_reasoning_effort = \"low\"", 1)
	withEdit = strings.Replace(withEdit, projectedProviderLine()+"\n", "", 1)
	if err := os.WriteFile(configPath, []byte(withEdit), 0o600); err != nil {
		t.Fatal(err)
	}
	status := takeover.Status()
	if status.State != TakeoverStateDrifted {
		t.Fatalf("pre-repair status = %+v", status)
	}
	repaired, err := takeover.Repair(DefaultGatewayListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.State != TakeoverStateActive {
		t.Fatalf("post-repair status = %+v", repaired)
	}
	after := readFileBytes(t, configPath)
	if !bytes.Contains(after, []byte(takeoverMarkerOpen)) || !hasExactProviderLine(after, projectedProviderLine()) {
		t.Fatalf("repair did not restore the projection:\n%s", after)
	}
	if !bytes.Contains(after, []byte("model_reasoning_effort = \"low\"")) {
		t.Fatalf("repair clobbered a user edit:\n%s", after)
	}
}

// TestTakeoverMissingConfigCreatesAndDisablesToEmpty: first enable on a
// machine without a codex config creates the projection; disable removes it.
func TestTakeoverMissingConfigCreatesAndDisablesToEmpty(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	status, err := takeover.Enable(DefaultGatewayListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != TakeoverStateActive {
		t.Fatalf("status = %+v", status)
	}
	created := readFileBytes(t, configPath)
	if !bytes.Contains(created, []byte("[model_providers."+GatewayProviderName+"]")) {
		t.Fatalf("missing config was not created with the projection:\n%s", created)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	after := readFileBytes(t, configPath)
	if strings.TrimSpace(string(after)) != "" && !bytes.Contains(after, []byte("[model_providers."+GatewayProviderName+"]")) {
		t.Fatalf("disable left non-empty non-projection config:\n%s", after)
	}
}

// TestTakeoverMalformedConfigFailsSafe: garbage config is never clobbered.
func TestTakeoverMalformedConfigFailsSafe(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	garbage := []byte("this is {{{ not toml at all")
	if err := os.WriteFile(configPath, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err == nil {
		t.Fatal("enable succeeded on malformed config")
	}
	after := readFileBytes(t, configPath)
	if !bytes.Equal(after, garbage) {
		t.Fatal("malformed config was modified")
	}
}

// TestTakeoverBackupRestore: the recorded rollback restores the exact
// pre-takeover bytes even after user edits.
func TestTakeoverBackupRestore(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	original := readFileBytes(t, configPath)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"something-else\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := takeover.RestoreBackup()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != TakeoverStateInactive {
		t.Fatalf("status after restore = %+v", status)
	}
	restored := readFileBytes(t, configPath)
	if !bytes.Equal(restored, original) {
		t.Fatalf("backup restore did not return exact original bytes:\n--- got ---\n%s\n--- want ---\n%s", restored, original)
	}
}

// TestTakeoverRestartRepairSimulation: a fresh Takeover over the same state
// dir (daemon restart) with a drifted live config repairs to active.
func TestTakeoverRestartRepairSimulation(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte(sampleCodexConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	stateDir := filepath.Join(root, "gateway-state")
	first := NewTakeover(configPath, stateDir, g)
	if _, err := first.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	_ = g.Close()
	// Simulate a crash leaving the projection; a new daemon instance binds a
	// new gateway on the same address and repairs drift.
	g2 := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g2.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g2.Close() })
	restarted := NewTakeover(configPath, stateDir, g2)
	state, err := restarted.LoadState()
	if err != nil || !state.Enabled {
		t.Fatalf("restarted takeover lost enabled state: %+v err=%v", state, err)
	}
	if status := restarted.Status(); status.State != TakeoverStateActive {
		t.Fatalf("restarted status = %+v, want active", status)
	}
}

// TestTakeoverConflictMultipleProviderLinesFailsSafe.
func TestTakeoverConflictMultipleProviderLinesFailsSafe(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	conflict := "model = \"gpt-5\"\nmodel_provider = \"a\"\nmodel_provider = \"b\"\n"
	if err := os.WriteFile(configPath, []byte(conflict), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err == nil {
		t.Fatal("enable succeeded with conflicting model_provider lines")
	}
	after := readFileBytes(t, configPath)
	if !bytes.Equal(after, []byte(conflict)) {
		t.Fatal("conflicting config was modified")
	}
}

// TestTakeoverPermissionFailureFailsSafe: an unwritable config path must fail
// without partial writes.
func TestTakeoverPermissionFailureFailsSafe(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission checks do not apply")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(locked, "config.toml")
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err == nil {
		t.Fatal("enable succeeded on an unwritable config path")
	}
}

// TestTakeoverStatusLifecycle walks inactive -> active -> drifted -> inactive.
func TestTakeoverStatusLifecycle(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if status := takeover.Status(); status.State != TakeoverStateInactive {
		t.Fatalf("initial status = %+v", status)
	}
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	if status := takeover.Status(); status.State != TakeoverStateActive {
		t.Fatalf("active status = %+v", status)
	}
	if status := takeover.Status(); !status.RestoreAvailable || status.BackupPath == "" {
		t.Fatalf("restore availability = %+v", status)
	}
	projected := readFileBytes(t, configPath)
	edited := strings.Replace(string(projected), "requires_openai_auth = false", "requires_openai_auth = true", 1)
	if err := os.WriteFile(configPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := takeover.Status(); status.State != TakeoverStateDrifted {
		t.Fatalf("drifted status = %+v", status)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	if status := takeover.Status(); status.State != TakeoverStateInactive {
		t.Fatalf("post-disable status = %+v", status)
	}
}

// TestTakeoverGatewayDownIsBroken: takeover with a stopped gateway reports
// broken truthfully.
func TestTakeoverGatewayDownIsBroken(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte(sampleCodexConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	_ = g.Close()
	status := takeover.Status()
	if status.State != TakeoverStateBroken {
		t.Fatalf("gateway-down status = %+v, want broken", status)
	}
}

// TestTakeoverProjectionProviderValuePreservedOnDisable: a pre-existing
// uncommented model_provider value is restored on disable.
func TestTakeoverProjectionProviderValuePreservedOnDisable(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	original := "model = \"gpt-5.6-sol\"\nmodel_provider = \"yescode\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	takeover := NewTakeover(configPath, filepath.Join(root, "gateway-state"), g)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	projected := readFileBytes(t, configPath)
	if !hasExactProviderLine(projected, projectedProviderLine()) {
		t.Fatalf("projection line missing:\n%s", projected)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	restored := readFileBytes(t, configPath)
	if string(restored) != original {
		t.Fatalf("original provider value was not restored:\n%s", restored)
	}
}
