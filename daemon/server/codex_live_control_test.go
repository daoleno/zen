package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/codexctl"
	"github.com/daoleno/zen/daemon/modelprofiles"
)

// fakeLiveControl records native thread-settings applications exactly like the
// real codexctl client would surface them through the LiveControl seam.
type fakeLiveControl struct {
	mu          sync.Mutex
	applied     []nativeApply
	reverts     []nativeApply
	closeCount  int
	resolveErr  error
	applyErr    error
	revertErr   error
	resolvedID  string
	dialCount   int
	socketPaths []string
}

type nativeApply struct {
	ThreadID string
	Model    string
	Effort   string
}

func (f *fakeLiveControl) ResolveThread(ctx context.Context, cwd string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	if f.resolvedID != "" {
		return f.resolvedID, nil
	}
	return "thread-native", nil
}

func (f *fakeLiveControl) ApplySettings(ctx context.Context, threadID, model string, effort *string, previous codexctl.Settings, ackTimeout time.Duration) (func(ctx context.Context) error, error) {
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	effortValue := ""
	if effort != nil {
		effortValue = *effort
	}
	f.mu.Lock()
	f.applied = append(f.applied, nativeApply{ThreadID: threadID, Model: model, Effort: effortValue})
	f.mu.Unlock()
	return func(ctx context.Context) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.revertErr != nil {
			return f.revertErr
		}
		f.reverts = append(f.reverts, nativeApply{ThreadID: threadID, Model: previous.Model, Effort: previous.Effort})
		return nil
	}, nil
}

func (f *fakeLiveControl) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return nil
}

func (f *fakeLiveControl) appliedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

func (f *fakeLiveControl) lastApplied() nativeApply {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 {
		return nativeApply{}
	}
	return f.applied[len(f.applied)-1]
}

func (f *fakeLiveControl) revertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reverts)
}

func (f *fakeLiveControl) lastRevert() nativeApply {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reverts) == 0 {
		return nativeApply{}
	}
	return f.reverts[len(f.reverts)-1]
}

// startLiveProfileOwner starts a profile owner whose managed Codex launches
// allocate live-control sockets.
func startLiveProfileOwner(t *testing.T) *modelprofiles.Owner {
	t.Helper()
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath:    filepath.Join(root, "model-profiles.toml"),
		RoutesPath:      filepath.Join(root, "route-bindings.json"),
		ListenerPath:    filepath.Join(root, "route-listener.json"),
		CodexControlDir: filepath.Join(root, "codex-ctl"),
		Lookup:          func(string) (string, bool) { return "ready", true },
		Verifier:        wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func bindLiveCodexSession(t *testing.T, owner *modelprofiles.Owner, sessionID string) {
	t.Helper()
	profileA := modelprofiles.Profile{
		ID: "provider-a", Name: "Provider A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "a", ProviderLabel: "A", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.4", Model: "gpt-5.4", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "A_KEY",
	}
	if _, err := owner.UpsertProfile(profileA, 0, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profileA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if launch.CodexControlSocket == "" {
		t.Fatal("expected live-control socket allocation")
	}
	if _, _, persist, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil || !persist.Applied {
		t.Fatalf("commit launch err=%v persist=%#v", err, persist)
	}
}

func TestSetThreadRuntimeLiveNativeFirstThenRouteCommit(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@live-native-first"
	bindLiveCodexSession(t, owner, sessionID)
	target := modelprofiles.Profile{
		ID: "provider-b", Name: "Provider B", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "b", ProviderLabel: "B", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.5", Model: "gpt-5.5", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "B_KEY",
	}
	if _, err := owner.UpsertProfile(target, 1, true); err != nil {
		t.Fatal(err)
	}

	live := &fakeLiveControl{}
	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		live.mu.Lock()
		live.dialCount++
		live.socketPaths = append(live.socketPaths, socketPath)
		live.mu.Unlock()
		return live, nil
	}
	killCalled := false
	inputCalled := false
	srv.killSessionOverride = func(string) error {
		killCalled = true
		return errors.New("must not kill")
	}
	srv.sendInputOverride = func(string, string) error {
		inputCalled = true
		return errors.New("must not send resume input")
	}

	before, ok := owner.Table().Get(sessionID)
	if !ok {
		t.Fatal("missing route before switch")
	}
	_, persist, err := srv.SetThreadRuntime(sessionID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: target.ID,
		ModelID:      target.Model,
		Effect:       "high",
	})
	if err != nil || !persist.Applied {
		t.Fatalf("switch err=%v persist=%#v", err, persist)
	}
	if killCalled || inputCalled {
		t.Fatalf("runtime switch touched live process via terminal: kill=%v input=%v", killCalled, inputCalled)
	}
	// Native applied exactly the resolved client model + effort.
	applied := live.lastApplied()
	if applied.Model != "gpt-5.5" || applied.Effort != "high" {
		t.Fatalf("native applied=%#v", applied)
	}
	if live.revertCount() != 0 {
		t.Fatalf("revert must not run on success: %d", live.revertCount())
	}
	// Route projection agrees with the native thread.
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ConnectionID != target.ID || runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != "high" {
		t.Fatalf("runtime=%#v", runtime)
	}
	after, ok := owner.Table().Get(sessionID)
	if !ok {
		t.Fatal("live Session route was removed")
	}
	if after.Binding.RouteID != before.Binding.RouteID || after.Binding.SessionID != before.Binding.SessionID {
		t.Fatalf("runtime switch replaced Session identity: before=%#v after=%#v", before.Binding, after.Binding)
	}
	if after.Binding.CodexControlSocket == "" {
		t.Fatal("live-control socket lost on switch")
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.dialCount != 1 || len(live.socketPaths) != 1 || live.socketPaths[0] != after.Binding.CodexControlSocket {
		t.Fatalf("dial count=%d paths=%v", live.dialCount, live.socketPaths)
	}
}

func TestSetThreadRuntimeLiveNativeFailureKeepsOldState(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@live-native-fail"
	bindLiveCodexSession(t, owner, sessionID)
	target := modelprofiles.Profile{
		ID: "provider-b", Name: "Provider B", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "b", ProviderLabel: "B", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.5", Model: "gpt-5.5", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "B_KEY",
	}
	if _, err := owner.UpsertProfile(target, 1, true); err != nil {
		t.Fatal(err)
	}

	live := &fakeLiveControl{applyErr: errors.New("native apply rejected")}
	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		return live, nil
	}
	_, persist, err := srv.SetThreadRuntime(sessionID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: target.ID,
		ModelID:      target.Model,
	})
	if err == nil || persist.Applied {
		t.Fatalf("native failure must not publish route: persist=%#v err=%v", persist, err)
	}
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ConnectionID != "provider-a" || runtime.ModelID != "gpt-5.4" {
		t.Fatalf("native failure must keep old route: %#v", runtime)
	}
	if live.revertCount() != 0 {
		t.Fatal("no native change was applied; revert must not run")
	}
}

func TestSetThreadRuntimeLiveDialFailureKeepsOldState(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@live-dial-fail"
	bindLiveCodexSession(t, owner, sessionID)
	target := modelprofiles.Profile{
		ID: "provider-b", Name: "Provider B", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "b", ProviderLabel: "B", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.5", Model: "gpt-5.5", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "B_KEY",
	}
	if _, err := owner.UpsertProfile(target, 1, true); err != nil {
		t.Fatal(err)
	}
	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		return nil, errors.New("socket gone")
	}
	_, persist, err := srv.SetThreadRuntime(sessionID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: target.ID,
		ModelID:      target.Model,
	})
	if err == nil || persist.Applied {
		t.Fatalf("dial failure must not publish route: persist=%#v err=%v", persist, err)
	}
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ModelID != "gpt-5.4" {
		t.Fatalf("dial failure must keep old route: %#v", runtime)
	}
}

func TestSetThreadRuntimeLiveRouteCommitFailureRevertsNative(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@live-commit-fail"
	bindLiveCodexSession(t, owner, sessionID)
	target := modelprofiles.Profile{
		ID: "provider-b", Name: "Provider B", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "b", ProviderLabel: "B", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.5", Model: "gpt-5.5", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "B_KEY",
	}
	if _, err := owner.UpsertProfile(target, 1, true); err != nil {
		t.Fatal(err)
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_write" {
			return errors.New("injected route commit failure")
		}
		return nil
	})

	live := &fakeLiveControl{}
	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		return live, nil
	}
	_, persist, err := srv.SetThreadRuntime(sessionID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: target.ID,
		ModelID:      target.Model,
		Effect:       "high",
	})
	owner.RoutesFile().SetPersistHook(nil)
	if err == nil || persist.Applied {
		t.Fatalf("route commit failure must surface: persist=%#v err=%v", persist, err)
	}
	// Native was applied first, then reverted to the previous identity.
	applied := live.lastApplied()
	if applied.Model != "gpt-5.5" || applied.Effort != "high" {
		t.Fatalf("native applied=%#v", applied)
	}
	if live.revertCount() != 1 {
		t.Fatalf("revert must run exactly once after failed route commit: %d", live.revertCount())
	}
	reverted := live.lastRevert()
	if reverted.Model != "gpt-5.4" || reverted.Effort != "" {
		t.Fatalf("native revert must restore previous identity: %#v", reverted)
	}
	// Route stays on the previous binding.
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ConnectionID != "provider-a" || runtime.ModelID != "gpt-5.4" {
		t.Fatalf("failed commit must keep old route: %#v", runtime)
	}
}

func TestSetThreadRuntimeLegacyEmbeddedNoLiveControl(t *testing.T) {
	// An owner without a control dir produces embedded launches (no socket);
	// SetThreadRuntime must stay route-only and never dial.
	owner := startProfileOwner(t)
	const sessionID = "tmux:@embedded"
	profileA := modelprofiles.Profile{
		ID: "provider-a", Name: "Provider A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "a", ProviderLabel: "A", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.4", Model: "gpt-5.4", BaseURL: "https://gateway.example/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "A_KEY",
	}
	if _, err := owner.UpsertProfile(profileA, 0, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profileA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if launch.CodexControlSocket != "" {
		t.Fatalf("embedded launch must have no socket: %q", launch.CodexControlSocket)
	}
	if _, _, persist, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil || !persist.Applied {
		t.Fatalf("commit err=%v persist=%#v", err, persist)
	}
	target := profileA
	target.ID = "provider-b"
	target.Name = "Provider B"
	target.ClientModel = "gpt-5.5"
	target.Model = "gpt-5.5"
	if _, err := owner.UpsertProfile(target, 1, true); err != nil {
		t.Fatal(err)
	}
	live := &fakeLiveControl{}
	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		live.mu.Lock()
		live.dialCount++
		live.mu.Unlock()
		return live, nil
	}
	killCalled := false
	inputCalled := false
	srv.killSessionOverride = func(string) error {
		killCalled = true
		return errors.New("must not kill")
	}
	srv.sendInputOverride = func(string, string) error {
		inputCalled = true
		return errors.New("must not send input")
	}
	_, persist, err := srv.SetThreadRuntime(sessionID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: target.ID,
		ModelID:      target.Model,
	})
	if err != nil || !persist.Applied {
		t.Fatalf("embedded switch err=%v persist=%#v", err, persist)
	}
	if killCalled || inputCalled {
		t.Fatalf("embedded switch touched terminal: kill=%v input=%v", killCalled, inputCalled)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.dialCount != 0 {
		t.Fatalf("embedded session must not dial live control: %d", live.dialCount)
	}
}
