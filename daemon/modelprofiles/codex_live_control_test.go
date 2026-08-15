package modelprofiles

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileCodexLiveControlWrapper(t *testing.T) {
	profile := codexResponsesProfile("gw", "gpt-5", "org/m1")
	resolved, err := Compile("codex", profile, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_x/v1",
		CatalogRevision:         1,
		Lookup:                  readyLookup("secret"),
		VerifiedProfileContract: contractFor(profile),
		CodexControlSocket:      "/tmp/zen/codex-ctl-abc.sock",
	})
	if err != nil {
		t.Fatal(err)
	}
	command := resolved.Command
	t.Logf("command=%s", command)
	// Headless app server owns the thread and exposes the control socket.
	if !strings.Contains(command, "codex app-server --listen unix:///tmp/zen/codex-ctl-abc.sock") {
		t.Fatalf("app-server half missing: %q", command)
	}
	// TUI client attaches to the same socket via --remote and execs the shell.
	if !strings.Contains(command, "& exec codex --remote unix:///tmp/zen/codex-ctl-abc.sock") {
		t.Fatalf("tui --remote half missing: %q", command)
	}
	// Model identity: the TUI carries --model; the app server receives the
	// same model as a config override (app-server has no --model flag).
	if !strings.Contains(command, "--model 'gpt-5'") && !strings.Contains(command, `--model gpt-5`) {
		t.Fatalf("tui model flag missing: %q", command)
	}
	if !strings.Contains(command, `--config 'model="gpt-5"'`) {
		t.Fatalf("app-server model config missing: %q", command)
	}
	// Route identity on BOTH halves.
	if strings.Count(command, `model_provider="openai"`) != 2 {
		t.Fatalf("app-server and TUI must share model_provider: %q", command)
	}
	if strings.Count(command, `openai_base_url="http://127.0.0.1:4317/r/rt_x/v1"`) != 2 {
		t.Fatalf("app-server and TUI must share openai_base_url: %q", command)
	}
	// App-server output is redirected to a per-session log so the pane stays
	// TUI-only and the watcher footer regex never sees server logs.
	if !strings.Contains(command, "> /tmp/zen/codex-ctl-abc.log 2>&1") {
		t.Fatalf("app-server log redirect missing: %q", command)
	}
	if resolved.CodexControlSocket != "/tmp/zen/codex-ctl-abc.sock" {
		t.Fatalf("resolved control socket=%q", resolved.CodexControlSocket)
	}
	if err := assertNoUpstreamLeak(command, resolved.Env, profile); err != nil {
		t.Fatal(err)
	}
	if err := assertNoPlaceholderLeak(command, profile); err != nil {
		t.Fatal(err)
	}
}

func TestCompileCodexLiveControlRejectsNativeProtocol(t *testing.T) {
	native := Profile{
		ID: "n", Name: "N", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5",
		AuthMode: AuthModeNone,
	}
	_, err := Compile("codex", native, CompileOptions{
		CatalogRevision:         1,
		VerifiedProfileContract: contractFor(native),
		CodexControlSocket:      "/tmp/zen/codex-ctl-x.sock",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestRouteBindingCodexControlSocketDurableRoundTrip(t *testing.T) {
	profile := codexResponsesProfile("gw", "gpt-5", "org/m1")
	auth := verifiedAuth(profile)
	table := NewRouteTable()
	table.SetLookup(readyLookup("secret"))
	state, err := table.BindLaunch("live-codex", profile, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := table.SetCodexControlSocket("live-codex", "/tmp/zen/codex-ctl-live.sock"); err != nil {
		t.Fatal(err)
	}
	state, _ = table.Get("live-codex")
	if state.Binding.CodexControlSocket != "/tmp/zen/codex-ctl-live.sock" {
		t.Fatalf("binding socket=%q", state.Binding.CodexControlSocket)
	}
	raw, err := EncodeDurableSnapshot(table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	restored := NewRouteTable()
	restored.SetLookup(readyLookup("secret"))
	restored.SetContractVerifier(BuiltinEnvelopeVerifier{})
	if _, err := restored.Restore(mustDecodeDurable(t, raw), BuiltinEnvelopeVerifier{}); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Get("live-codex")
	if !ok || got.Binding.CodexControlSocket != "/tmp/zen/codex-ctl-live.sock" {
		t.Fatalf("restored socket=%q ok=%v", got.Binding.CodexControlSocket, ok)
	}
	// Legacy snapshots without the field decode with an empty socket.
	legacy := strings.Replace(string(raw), `"codex_control_socket": "/tmp/zen/codex-ctl-live.sock",`, "", 1)
	legacyTable := NewRouteTable()
	legacyTable.SetLookup(readyLookup("secret"))
	if _, err := legacyTable.Restore(mustDecodeDurable(t, []byte(legacy)), BuiltinEnvelopeVerifier{}); err != nil {
		t.Fatal(err)
	}
	if got, ok := legacyTable.Get("live-codex"); !ok || got.Binding.CodexControlSocket != "" {
		t.Fatalf("legacy socket=%q", got.Binding.CodexControlSocket)
	}
}

func TestPrepareLaunchAllocatesControlSocketOnlyWithControlDir(t *testing.T) {
	root := t.TempDir()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:    filepath.Join(root, "profiles.toml"),
		RoutesPath:      filepath.Join(root, "routes.json"),
		ListenerPath:    filepath.Join(root, "listener.json"),
		CodexControlDir: filepath.Join(root, "codex-ctl"),
		Lookup:          readyLookup("secret"),
		Verifier:        BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := codexResponsesProfile("gw", "gpt-5", "gpt-5")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "gw", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CodexControlSocket == "" {
		t.Fatal("live-control launch must allocate a control socket")
	}
	if !strings.HasPrefix(plan.Command, "codex app-server --listen unix://") {
		t.Fatalf("launch command must be app-server wrapper: %q", plan.Command)
	}
	if !strings.Contains(plan.Command, "& exec codex --remote unix://") {
		t.Fatalf("launch command must attach TUI via --remote: %q", plan.Command)
	}
	state, ok := owner.Table().Get(plan.ProvisionalID)
	if !ok || state.Binding.CodexControlSocket != plan.CodexControlSocket {
		t.Fatalf("binding socket=%q plan=%q", state.Binding.CodexControlSocket, plan.CodexControlSocket)
	}
	if got := owner.CodexControlSocket(plan.ProvisionalID); got != plan.CodexControlSocket {
		t.Fatalf("accessor=%q", got)
	}
	// The socket survives CommitLaunch (RebindSession preserves binding fields).
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@live"); err != nil {
		t.Fatal(err)
	}
	if got := owner.CodexControlSocket("tmux:@live"); got != plan.CodexControlSocket {
		t.Fatalf("post-commit socket=%q", got)
	}
}

func TestPrepareLaunchWithoutControlDirStaysEmbedded(t *testing.T) {
	root := t.TempDir()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: filepath.Join(root, "profiles.toml"),
		RoutesPath:   filepath.Join(root, "routes.json"),
		ListenerPath: filepath.Join(root, "listener.json"),
		Lookup:       readyLookup("secret"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := codexResponsesProfile("gw", "gpt-5", "gpt-5")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "gw", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CodexControlSocket != "" {
		t.Fatalf("embedded launch must not allocate a socket: %q", plan.CodexControlSocket)
	}
	if strings.Contains(plan.Command, "app-server") || strings.Contains(plan.Command, "--remote") {
		t.Fatalf("embedded launch must stay a plain TUI: %q", plan.Command)
	}
}

func TestPrepareCommitThreadRuntimeCapturesNativeIdentity(t *testing.T) {
	root := t.TempDir()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: filepath.Join(root, "profiles.toml"),
		RoutesPath:   filepath.Join(root, "routes.json"),
		ListenerPath: filepath.Join(root, "listener.json"),
		Lookup:       readyLookup("secret"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	a := codexResponsesProfile("a", "gpt-5", "gpt-5")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID, b.Name, b.ProviderID = "b", "B", "acme-b"
	b.ClientModel, b.Model = "gpt-5.5", "gpt-5.5"
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "tmux:@native-id"); err != nil {
		t.Fatal(err)
	}
	prepared, err := owner.PrepareThreadRuntime("tmux:@native-id", ThreadRuntimeChoice{
		ConnectionID: "b",
		ModelID:      "gpt-5.5",
		Effect:       "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	target := prepared.Target()
	if target.ModelID != "gpt-5.5" || target.Effect != "high" {
		t.Fatalf("target=%#v", target)
	}
	previous := prepared.Previous()
	if previous.ModelID != "gpt-5" || previous.Effect != "" {
		t.Fatalf("previous=%#v (must be the pre-mutation native identity for rollback)", previous)
	}
	// Route not published until Commit.
	if runtime, ok := owner.ThreadRuntime("tmux:@native-id"); !ok || runtime.ModelID != "gpt-5" {
		t.Fatalf("prepare must not publish: %#v", runtime)
	}
	if _, snap, persist, err := owner.CommitThreadRuntime(prepared); err != nil || !persist.Applied {
		t.Fatalf("commit err=%v persist=%#v", err, persist)
	} else if snap.Current == nil || snap.Current.ModelID != "gpt-5.5" {
		t.Fatalf("snapshot=%#v", snap.Current)
	}
	if runtime, ok := owner.ThreadRuntime("tmux:@native-id"); !ok || runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != "high" {
		t.Fatalf("runtime=%#v", runtime)
	}
}

func TestCommitThreadRuntimeFailsClosedOnGenerationChange(t *testing.T) {
	root := t.TempDir()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: filepath.Join(root, "profiles.toml"),
		RoutesPath:   filepath.Join(root, "routes.json"),
		ListenerPath: filepath.Join(root, "listener.json"),
		Lookup:       readyLookup("secret"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	a := codexResponsesProfile("a", "gpt-5", "gpt-5")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID, b.Name, b.ProviderID = "b", "B", "acme-b"
	b.ClientModel, b.Model = "gpt-5.5", "gpt-5.5"
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "tmux:@cas"); err != nil {
		t.Fatal(err)
	}
	prepared, err := owner.PrepareThreadRuntime("tmux:@cas", ThreadRuntimeChoice{
		ConnectionID: "b",
		ModelID:      "gpt-5.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent mutation advances the generation between Prepare and Commit.
	if _, _, _, err := owner.SetThreadRuntime("tmux:@cas", ThreadRuntimeChoice{
		ConnectionID: "b",
		ModelID:      "gpt-5.5",
		Effect:       "low",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, persist, err := owner.CommitThreadRuntime(prepared); err == nil || persist.Applied {
		t.Fatalf("stale commit must fail closed: err=%v persist=%#v", err, persist)
	}
	runtime, ok := owner.ThreadRuntime("tmux:@cas")
	if !ok || runtime.ReasoningEffort != "low" {
		t.Fatalf("runtime must keep the concurrent mutation: %#v", runtime)
	}
}

func mustDecodeDurable(t *testing.T, raw []byte) []SessionRouteState {
	t.Helper()
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	return states
}
