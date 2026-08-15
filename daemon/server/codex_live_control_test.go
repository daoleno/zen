package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/codexctl"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
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

func TestSetThreadRuntimeLegacyEmbeddedCodexRejectedHonestly(t *testing.T) {
	// A pre-feature embedded Codex session (no app-server socket) cannot adopt
	// native synchronization without restarting the Codex process. The
	// Interface mutation must be rejected — never silently route-only — and
	// the projection must not advertise hot switching.
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
	if err == nil || persist.Applied {
		t.Fatalf("embedded codex switch must be rejected: persist=%#v err=%v", persist, err)
	}
	if !strings.Contains(err.Error(), "live-control") {
		t.Fatalf("rejection must name the migration limitation: %v", err)
	}
	if killCalled || inputCalled {
		t.Fatalf("embedded switch touched terminal: kill=%v input=%v", killCalled, inputCalled)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.dialCount != 0 {
		t.Fatalf("embedded session must not dial live control: %d", live.dialCount)
	}
	// The projection must not claim hot switching for the embedded session.
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || runtime.ModelID != "gpt-5.4" {
		t.Fatalf("rejected switch must keep the old runtime: %#v", runtime)
	}
	if runtime.HotSwitchable {
		t.Fatalf("embedded codex session must not advertise hot switching: %#v", runtime)
	}
	if caps := owner.SessionRouteCapabilities(sessionID); caps.ActiveSwitch {
		t.Fatalf("embedded codex session must not advertise active switch: %#v", caps)
	}
}

func TestSetThreadRuntimeLiveSessionAdvertisesHotSwitch(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@hot-switch-advert"
	bindLiveCodexSession(t, owner, sessionID)
	runtime, ok := owner.ThreadRuntime(sessionID)
	if !ok || !runtime.HotSwitchable {
		t.Fatalf("live-control codex session must advertise hot switching: %#v", runtime)
	}
	if caps := owner.SessionRouteCapabilities(sessionID); !caps.ActiveSwitch {
		t.Fatalf("live-control codex session must advertise active switch: %#v", caps)
	}
}

// TestTerminalModelSwitchConvergesInterfaceProjection proves the native
// Terminal /model -> Interface convergence end-to-end: a request carrying
// Codex's reserved model-switch signal (model + effort, including the native
// default effort "none") updates the authoritative route binding, and the
// Interface's get_thread_runtime WebSocket projection observes exactly the
// same model/effort.
func TestTerminalModelSwitchConvergesInterfaceProjection(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-converge", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startLiveProfileOwner(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)

	var upstreamBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBodies = append(upstreamBodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","object":"response","output":[]}`))
	}))
	t.Cleanup(upstream.Close)

	profile := modelprofiles.Profile{
		ID: "provider-a", Name: "Provider A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "a", ProviderLabel: "A", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.4", Model: "gpt-5.4", BaseURL: upstream.URL + "/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "A_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "tmux:@converge"); err != nil {
		t.Fatal(err)
	}
	base, err := modelprofiles.LoopbackCodexBaseURL(owner.ListenAddr(), launch.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-converge", "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	getProjection := func() (model string, effort string) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{
			"type": "get_thread_runtime", "request_id": "gtr-1", "agent_id": "tmux:@converge",
		}); err != nil {
			t.Fatal(err)
		}
		payload := readType("thread_runtime")
		runtime, _ := payload["runtime"].(map[string]any)
		model, _ = runtime["model_id"].(string)
		effort, _ = runtime["reasoning_effort"].(string)
		return model, effort
	}
	post := func(body string) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, base+"/responses", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+modelprofiles.LoopbackAuthPlaceholder)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		responseBody, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.StatusCode, responseBody)
		}
	}

	if model, effort := getProjection(); model != "gpt-5.4" || effort != "" {
		t.Fatalf("initial projection model=%q effort=%q", model, effort)
	}

	// Native /model switch: model gpt-5.5 with explicit high effort.
	post(`{"model":"gpt-5.5","reasoning":{"effort":"high"},"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"<model_switch>\nThe user was previously using a different model. Please continue the conversation according to the following instructions:\n\nUse the newly selected model.\n</model_switch>"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	if model, effort := getProjection(); model != "gpt-5.5" || effort != "high" {
		t.Fatalf("projection after native switch model=%q effort=%q, want gpt-5.5/high", model, effort)
	}
	if runtime, ok := owner.ThreadRuntime("tmux:@converge"); !ok || runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != "high" {
		t.Fatalf("authoritative route after native switch: %#v", runtime)
	}
	if len(upstreamBodies) != 1 || !strings.Contains(upstreamBodies[0], `"gpt-5.5"`) {
		t.Fatalf("upstream must receive the switched model: %#v", upstreamBodies)
	}

	// Native /model switch to default effort: Codex 0.147 carries
	// reasoning.effort "none". The route effort must clear and the Interface
	// projection must agree (no explicit effort), with the request forwarded
	// without a stale effort.
	post(`{"model":"gpt-5.5","reasoning":{"effort":"none"},"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"<model_switch>\nThe user was previously using a different model. Please continue the conversation according to the following instructions:\n\nUse the newly selected model.\n</model_switch>"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	if model, effort := getProjection(); model != "gpt-5.5" || effort != "" {
		t.Fatalf("projection after native default switch model=%q effort=%q, want gpt-5.5/default", model, effort)
	}
	if runtime, ok := owner.ThreadRuntime("tmux:@converge"); !ok || runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != "" {
		t.Fatalf("authoritative route after native default switch: %#v", runtime)
	}
	if len(upstreamBodies) != 2 {
		t.Fatalf("upstream bodies=%d", len(upstreamBodies))
	}
	if strings.Contains(upstreamBodies[1], `"effort"`) {
		t.Fatalf("default-effort switch must not forward a stale effort: %s", upstreamBodies[1])
	}
}

// TestTeardownAgentSessionCleansCodexControlArtifacts proves normal Session
// teardown (kill_agent) kills any remaining Codex app-server via its recorded
// pid and removes the daemon-owned socket/pid/log artifacts.
func TestTeardownAgentSessionCleansCodexControlArtifacts(t *testing.T) {
	owner := startLiveProfileOwner(t)
	const sessionID = "tmux:@teardown-cleanup"
	bindLiveCodexSession(t, owner, sessionID)
	socketPath := owner.CodexControlSocket(sessionID)
	if socketPath == "" {
		t.Fatal("live session must have a control socket")
	}
	// Fabricate the app-server artifacts exactly as the launch wrapper does.
	appServer := exec.Command("sleep", "60")
	appServer.Args = []string{"codex app-server --listen unix://" + socketPath, "60"}
	if err := appServer.Start(); err != nil {
		t.Fatal(err)
	}
	appServerDone := make(chan struct{})
	go func() {
		_, _ = appServer.Process.Wait()
		close(appServerDone)
	}()
	t.Cleanup(func() {
		_ = appServer.Process.Kill()
		<-appServerDone
	})
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := modelprofiles.CodexControlPidPath(socketPath)
	logPath := modelprofiles.CodexControlLogPath(socketPath)
	for path, content := range map[string]string{
		socketPath: "stale-socket",
		pidPath:    strconv.Itoa(appServer.Process.Pid),
		logPath:    "stale-log",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{}
	srv.SetModelProfiles(owner)
	srv.killSessionOverride = func(string) error { return nil }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	result := srv.teardownAgentSession(sessionID)
	if result.Err != nil {
		t.Fatalf("teardown: %v", result.Err)
	}
	select {
	case <-appServerDone:
	case <-time.After(6 * time.Second):
		t.Fatal("app-server survived Session teardown")
	}
	for _, path := range []string{socketPath, pidPath, logPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %s not removed after teardown", path)
		}
	}
	if _, ok := owner.Table().Get(sessionID); ok {
		t.Fatal("route must be released after teardown")
	}
}

// TestTerminalEffortOnlyChangeConvergesInterfaceProjection proves the
// same-model effort-only Terminal change converges end-to-end: the request
// (no model-switch fragment) is checked against the authoritative native
// settings snapshot, the route binding converges, and the Interface
// get_thread_runtime projection observes exactly the same effort — the
// request is never rewritten back to the stale binding.
func TestTerminalEffortOnlyChangeConvergesInterfaceProjection(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-effort-converge", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startLiveProfileOwner(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		return &fakeLiveControl{}, nil
	}
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)

	var upstreamBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBodies = append(upstreamBodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","object":"response","output":[]}`))
	}))
	t.Cleanup(upstream.Close)

	profile := modelprofiles.Profile{
		ID: "provider-a", Name: "Provider A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "a", ProviderLabel: "A", Protocol: modelprofiles.ProtocolOpenAIResponses,
		ClientModel: "gpt-5.4", Model: "gpt-5.4", BaseURL: upstream.URL + "/v1",
		AuthMode: modelprofiles.AuthModeBearerEnv, CredentialEnv: "A_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "tmux:@effort-converge"); err != nil {
		t.Fatal(err)
	}
	// Interface sets explicit high first; the native thread then moves to low
	// (same model, no reserved fragment).
	if _, persist, err := srv.SetThreadRuntime("tmux:@effort-converge", modelprofiles.ThreadRuntimeChoice{
		ConnectionID: profile.ID, ModelID: "gpt-5.4", Effect: "high",
	}); err != nil || !persist.Applied {
		t.Fatalf("interface high err=%v persist=%#v", err, persist)
	}
	owner.SetNativeSettingsLookup(func(routeID string) (codexctl.NativeSettings, bool) {
		return codexctl.NativeSettings{ThreadID: "t-native", Model: "gpt-5.4", Effort: "low"}, true
	})
	base, err := modelprofiles.LoopbackCodexBaseURL(owner.ListenAddr(), launch.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-effort-converge", "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	getProjection := func() (model string, effort string) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{
			"type": "get_thread_runtime", "request_id": "gtr-effort", "agent_id": "tmux:@effort-converge",
		}); err != nil {
			t.Fatal(err)
		}
		payload := readType("thread_runtime")
		runtime, _ := payload["runtime"].(map[string]any)
		model, _ = runtime["model_id"].(string)
		effort, _ = runtime["reasoning_effort"].(string)
		return model, effort
	}

	if model, effort := getProjection(); model != "gpt-5.4" || effort != "high" {
		t.Fatalf("initial projection model=%q effort=%q", model, effort)
	}
	// Terminal effort-only change: same model, effort low, no fragment.
	request, err := http.NewRequest(http.MethodPost, base+"/responses", strings.NewReader(`{"model":"gpt-5.4","reasoning":{"effort":"low"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+modelprofiles.LoopbackAuthPlaceholder)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, responseBody)
	}
	if model, effort := getProjection(); model != "gpt-5.4" || effort != "low" {
		t.Fatalf("projection after effort-only change model=%q effort=%q, want gpt-5.4/low", model, effort)
	}
	if runtime, ok := owner.ThreadRuntime("tmux:@effort-converge"); !ok || runtime.ReasoningEffort != "low" {
		t.Fatalf("authoritative route after effort-only change: %#v", runtime)
	}
	if len(upstreamBodies) != 1 || !strings.Contains(upstreamBodies[0], `"effort":"low"`) {
		t.Fatalf("upstream must receive the converged low effort: %#v", upstreamBodies)
	}
}
