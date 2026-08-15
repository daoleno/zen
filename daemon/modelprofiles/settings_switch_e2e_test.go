package modelprofiles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func startSettingsSwitchOwner(t *testing.T, root string) *Owner {
	t.Helper()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
		RoutesPath:    filepath.Join(root, "route-bindings.json"),
		ListenerPath:  filepath.Join(root, "route-listener.json"),
		DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
		Lookup:        func(string) (string, bool) { return "", false },
		Verifier:      BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner.SetCredentialStore(creds)
	return owner
}

// TestSettingsDefaultDoesNotMutateCurrentRuntime proves Settings owns only the
// future-thread default. The acknowledged current runtime remains sendable and
// both independent values restore after restart.
func TestSettingsDefaultDoesNotMutateCurrentRuntime(t *testing.T) {
	root := t.TempDir()
	upstreamA := newE2EUpstream(t, nil)
	upstreamB := newE2EUpstream(t, nil)
	start := func() *Owner {
		owner, err := StartOwner(OwnerConfig{
			ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
			RoutesPath:    filepath.Join(root, "route-bindings.json"),
			ListenerPath:  filepath.Join(root, "route-listener.json"),
			DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
			Lookup:        func(string) (string, bool) { return "", false },
			Verifier:      BuiltinEnvelopeVerifier{},
		})
		if err != nil {
			t.Fatal(err)
		}
		creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		owner.SetCredentialStore(creds)
		return owner
	}

	owner := start()
	connAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", upstreamA.server.URL+"/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := connAProjection.Connections[0]
	connBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", upstreamB.server.URL+"/v1", "gpt-5.5"), "key-b", connAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, connBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"gpt-5.4"},
		connB.ID: {"gpt-5.5"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, connB.ID, "", owner.Catalog().Revision); err == nil {
		t.Fatal("different Provider default accepted without an atomic model")
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "thread-1"); err != nil {
		t.Fatal(err)
	}
	routeID := launch.State.Binding.RouteID

	if _, err := owner.SetProviderDefault(ClientCodex, connB.ID, "gpt-5.5", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	selection, ok := owner.ThreadRuntime("thread-1")
	if !ok || selection.ConnectionID != connA.ID || selection.ModelID != "gpt-5.4" {
		t.Fatalf("Settings mutated current runtime: %#v", selection)
	}
	router := httptest.NewServer(owner.router.Handler())
	postLoopback(t, router.Listener.Addr().String(), routeID, "gpt-5.4")
	if got, _ := upstreamA.last(); got.auth != "Bearer key-a" || got.model != "gpt-5.4" {
		t.Fatalf("current runtime stopped sending after Settings change: %#v", got)
	}
	router.Close()
	_ = owner.Close()

	restored := start()
	t.Cleanup(func() { _ = restored.Close() })
	if def := restored.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "gpt-5.5" {
		t.Fatalf("future default not restored: %#v", def)
	}
	selection, ok = restored.ThreadRuntime("thread-1")
	if !ok || selection.ConnectionID != connA.ID || selection.ModelID != "gpt-5.4" {
		t.Fatalf("acknowledged runtime not restored: %#v", selection)
	}
}

func TestSwitchProviderRetargetsSessionsAndPreservesInFlightRequests(t *testing.T) {
	root := t.TempDir()
	hold := make(chan struct{})
	upstreamA := newE2EUpstream(t, hold)
	upstreamB := newE2EUpstream(t, nil)
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", upstreamA.server.URL+"/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := connAProjection.Connections[0]
	connBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", upstreamB.server.URL+"/v1", "gpt-5.4"), "key-b", connAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, connBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"gpt-5.4", "gpt-5.5"},
		connB.ID: {"gpt-5.4", "gpt-5.5"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}

	launch1, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch1.ProvisionalID, "thread-1"); err != nil {
		t.Fatal(err)
	}
	launch2, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch2.ProvisionalID, "thread-2"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("thread-1", ThreadRuntimeChoice{
		ConnectionID: connA.ID,
		ModelID:      "gpt-5.4",
		Effect:       ReasoningEffortHigh,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("thread-2", ThreadRuntimeChoice{
		ConnectionID: connA.ID,
		ModelID:      "gpt-5.5",
		Effect:       ReasoningEffortLow,
	}); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []SessionLaunchPlan{launch1, launch2} {
		for key, value := range plan.Env {
			if strings.Contains(value, "key-a") || strings.Contains(value, "key-b") {
				t.Fatalf("child environment %s exposed a Provider credential", key)
			}
		}
	}

	router := httptest.NewServer(owner.router.Handler())
	t.Cleanup(router.Close)
	var holdOnce sync.Once
	releaseHold := func() { holdOnce.Do(func() { close(hold) }) }
	t.Cleanup(releaseHold)
	route1 := launch1.State.Binding.RouteID
	route2 := launch2.State.Binding.RouteID
	base1, err := LoopbackCodexBaseURL(router.Listener.Addr().String(), route1)
	if err != nil {
		t.Fatal(err)
	}

	if sel, ok := owner.ThreadRuntime("thread-1"); !ok || sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("session-1 should start with effect high before switch: %#v", sel)
	}

	requestDone := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, base1+"/responses", strings.NewReader(`{"model":"gpt-5.4","reasoning":{"effort":"high"},"input":[]}`))
		if err != nil {
			requestDone <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			requestDone <- err
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			requestDone <- fmt.Errorf("held request status=%d", resp.StatusCode)
			return
		}
		requestDone <- nil
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := upstreamA.last(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("held request never reached the upstream")
		}
		time.Sleep(10 * time.Millisecond)
	}

	rev := owner.Catalog().Revision
	proj, err := owner.SwitchProvider(ClientCodex, connB.ID, rev)
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.Defaults[ClientCodex]; got.ConnectionID != connB.ID || got.ModelID != "gpt-5.4" {
		t.Fatalf("switch default not preserved: %#v", got)
	}

	selection1, ok := owner.ThreadRuntime("thread-1")
	if !ok || selection1.ConnectionID != connB.ID || selection1.ModelID != "gpt-5.4" || selection1.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("session-1 not retargeted with model/effect preserved: %#v", selection1)
	}
	selection2, ok := owner.ThreadRuntime("thread-2")
	if !ok || selection2.ConnectionID != connB.ID || selection2.ModelID != "gpt-5.5" || selection2.ReasoningEffort != ReasoningEffortLow {
		t.Fatalf("session-2 not retargeted with model/effect preserved: %#v", selection2)
	}
	state1, ok := owner.Table().Get("thread-1")
	if !ok || state1.Binding.HistoryState != HistoryStateMayContainOpaque || state1.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("in-flight old-provider history was not durably guarded: %#v", state1.Binding)
	}

	// Every switched Session crosses the real Router boundary. Thread 1 omits
	// its acknowledged effect; thread 2 sends an older model and omits its
	// acknowledged effect. Both payloads must normalize to the atomically
	// switched per-Session bindings, never mutate them.
	postLoopback(t, router.Listener.Addr().String(), route1, "gpt-5.4")
	postLoopback(t, router.Listener.Addr().String(), route2, "gpt-5.4")
	newProviderCalls := upstreamB.snapshot()
	if len(newProviderCalls) != 2 {
		t.Fatalf("new provider calls=%#v want one per switched Session", newProviderCalls)
	}
	if got := newProviderCalls[0]; got.auth != "Bearer key-b" || got.model != "gpt-5.4" || got.effort != ReasoningEffortHigh {
		t.Fatalf("thread-1 did not preserve model/effect on new provider: %#v", got)
	}
	if got := newProviderCalls[1]; got.auth != "Bearer key-b" || got.model != "gpt-5.5" || got.effort != ReasoningEffortLow {
		t.Fatalf("thread-2 did not preserve model/effect on new provider: %#v", got)
	}
	routeFile, err := os.ReadFile(filepath.Join(root, "route-bindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(routeFile, []byte("key-a")) || bytes.Contains(routeFile, []byte("key-b")) {
		t.Fatal("durable route file exposed a Provider credential")
	}

	releaseHold()
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if got, ok := upstreamA.last(); !ok || got.auth != "Bearer key-a" || got.model != "gpt-5.4" || got.effort != ReasoningEffortHigh {
		t.Fatalf("in-flight request changed across switch: %#v", got)
	}

	_ = owner.Close()
	restored := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = restored.Close() })
	if def := restored.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "gpt-5.4" {
		t.Fatalf("future default not restored after switch: %#v", def)
	}
	selection1, ok = restored.ThreadRuntime("thread-1")
	if !ok || selection1.ConnectionID != connB.ID || selection1.ModelID != "gpt-5.4" || selection1.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("session-1 not restored after switch: %#v", selection1)
	}
	selection2, ok = restored.ThreadRuntime("thread-2")
	if !ok || selection2.ConnectionID != connB.ID || selection2.ModelID != "gpt-5.5" || selection2.ReasoningEffort != ReasoningEffortLow {
		t.Fatalf("session-2 not restored after switch: %#v", selection2)
	}
	restoredState1, ok := restored.Table().Get("thread-1")
	if !ok || restoredState1.Binding.HistoryState != HistoryStateMayContainOpaque || restoredState1.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("in-flight history guard not restored: %#v", restoredState1.Binding)
	}
}

func TestSwitchProviderIgnoresTargetDiscoveryAndPreservesModels(t *testing.T) {
	root := t.TempDir()
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connectionAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", "https://alpha.example/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionA := connectionAProjection.Connections[0]
	connectionBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", "https://beta.example/v1", "gpt-5.4"), "key-b", connectionAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionB := connectionByName(t, connectionBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connectionA.ID: {"gpt-5.4", "gpt-5.5"},
		connectionB.ID: {"gpt-5.4"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connectionA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{"validation-thread-1", "validation-thread-2"} {
		launch, err := owner.PrepareLaunch(ExecutorCodex, connectionA.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := owner.SetThreadRuntime("validation-thread-2", ThreadRuntimeChoice{
		ConnectionID: connectionA.ID,
		ModelID:      "gpt-5.5",
		Effect:       ReasoningEffortLow,
	}); err != nil {
		t.Fatal(err)
	}
	before := captureProviderSwitchSnapshot(owner)
	if _, err := owner.SwitchProvider(ClientCodex, connectionB.ID, before.Revision); err != nil {
		t.Fatalf("provider-only switch must ignore target discovery: %v", err)
	}
	after := captureProviderSwitchSnapshot(owner)
	if after.Defaults[ClientCodex] != connectionB.ID || after.DefaultModels[ClientCodex] != before.DefaultModels[ClientCodex] {
		t.Fatalf("default model was not preserved: before=%#v after=%#v", before, after)
	}
	for _, sessionID := range []string{"validation-thread-1", "validation-thread-2"} {
		runtime, ok := owner.ThreadRuntime(sessionID)
		if !ok || runtime.ConnectionID != connectionB.ID {
			t.Fatalf("runtime %s not retargeted: %#v", sessionID, runtime)
		}
	}
	if runtime, _ := owner.ThreadRuntime("validation-thread-2"); runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != ReasoningEffortLow {
		t.Fatalf("preserved runtime changed: %#v", runtime)
	}
	if _, err := os.Stat(filepath.Join(root, providerSwitchJournalFileName)); !os.IsNotExist(err) {
		t.Fatalf("completed switch left a transaction journal: %v", err)
	}
}

func TestSwitchProviderCredentialFailureChangesNothing(t *testing.T) {
	root := t.TempDir()
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connectionAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", "https://alpha.example/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionA := connectionAProjection.Connections[0]
	connectionBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", "https://beta.example/v1", "gpt-5.4"), "key-b", connectionAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionB := connectionByName(t, connectionBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connectionA.ID: {"gpt-5.4"},
		connectionB.ID: {"gpt-5.4"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connectionA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{"credential-thread-1", "credential-thread-2"} {
		launch, err := owner.PrepareLaunch(ExecutorCodex, connectionA.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.ClearProviderCredential(connectionB.ID); err != nil {
		t.Fatal(err)
	}
	before := captureProviderSwitchSnapshot(owner)
	if _, err := owner.SwitchProvider(ClientCodex, connectionB.ID, before.Revision); !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("switch error=%v want missing target credential", err)
	}
	assertProviderSwitchSnapshot(t, captureProviderSwitchSnapshot(owner), before)
	if _, err := os.Stat(filepath.Join(root, providerSwitchJournalFileName)); !os.IsNotExist(err) {
		t.Fatalf("credential validation failure wrote a transaction journal: %v", err)
	}
}

func TestSwitchProviderBlocksNewRouteAdmissionUntilWholeTransactionPublishes(t *testing.T) {
	root := t.TempDir()
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connectionAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", "https://alpha.example/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionA := connectionAProjection.Connections[0]
	connectionBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", "https://beta.example/v1", "gpt-5.4"), "key-b", connectionAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionB := connectionByName(t, connectionBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connectionA.ID: {"gpt-5.4"},
		connectionB.ID: {"gpt-5.4"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connectionA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, connectionA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "admission-thread"); err != nil {
		t.Fatal(err)
	}

	enteredRoutePersist := make(chan struct{})
	releaseRoutePersist := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRoutePersist) }) }
	t.Cleanup(release)
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_write" {
			close(enteredRoutePersist)
			<-releaseRoutePersist
		}
		return nil
	})
	t.Cleanup(func() { owner.RoutesFile().SetPersistHook(nil) })

	switchDone := make(chan error, 1)
	go func() {
		_, switchErr := owner.SwitchProvider(ClientCodex, connectionB.ID, owner.Catalog().Revision)
		switchDone <- switchErr
	}()
	select {
	case <-enteredRoutePersist:
	case <-time.After(2 * time.Second):
		t.Fatal("Provider switch never reached route persistence")
	}
	if connectionID, modelID := owner.store.ClientDefault(ClientCodex); connectionID != connectionA.ID || modelID != "gpt-5.4" {
		t.Fatalf("default published before route transaction: connection=%q model=%q", connectionID, modelID)
	}

	type flightResult struct {
		binding RouteBinding
		token   string
		err     error
	}
	flightDone := make(chan flightResult, 1)
	go func() {
		binding, token, flightErr := owner.Table().BeginRouteFlight(launch.State.Binding.RouteID)
		flightDone <- flightResult{binding: binding, token: token, err: flightErr}
	}()
	select {
	case result := <-flightDone:
		t.Fatalf("route admission escaped the atomic switch boundary: %#v", result)
	case <-time.After(50 * time.Millisecond):
	}

	release()
	if err := <-switchDone; err != nil {
		t.Fatal(err)
	}
	result := <-flightDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer func() { _ = owner.Table().EndRouteFlight(result.binding.RouteID, result.token, false) }()
	if result.binding.ProfileID != connectionB.ID || result.binding.ClientModel != "gpt-5.4" {
		t.Fatalf("new admission did not observe the complete switched binding: %#v", result.binding)
	}
	if connectionID, modelID := owner.store.ClientDefault(ClientCodex); connectionID != connectionB.ID || modelID != "gpt-5.4" {
		t.Fatalf("default did not publish with route transaction: connection=%q model=%q", connectionID, modelID)
	}
}

func TestTerminalModelSwitchQueuedBehindGlobalSwitchUsesNewProvider(t *testing.T) {
	root := t.TempDir()
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connectionAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", "https://alpha.example/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionA := connectionAProjection.Connections[0]
	connectionBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", "https://beta.example/v1", "gpt-5.4"), "key-b", connectionAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionB := connectionByName(t, connectionBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connectionA.ID: {"gpt-5.4", "gpt-5.5"},
		connectionB.ID: {"gpt-5.4", "gpt-5.5"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connectionA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, connectionA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "queued-terminal-model-switch"); err != nil {
		t.Fatal(err)
	}

	enteredRoutePersist := make(chan struct{})
	releaseRoutePersist := make(chan struct{})
	var persistOnce sync.Once
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_write" {
			persistOnce.Do(func() {
				close(enteredRoutePersist)
				<-releaseRoutePersist
			})
		}
		return nil
	})
	t.Cleanup(func() { owner.RoutesFile().SetPersistHook(nil) })
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRoutePersist) }) }
	t.Cleanup(release)

	switchDone := make(chan error, 1)
	go func() {
		_, switchErr := owner.SwitchProvider(ClientCodex, connectionB.ID, owner.Catalog().Revision)
		switchDone <- switchErr
	}()
	select {
	case <-enteredRoutePersist:
	case <-time.After(2 * time.Second):
		t.Fatal("Provider switch never reached route persistence")
	}

	terminalDone := make(chan error, 1)
	go func() {
		terminalDone <- owner.ApplyTerminalModelSwitch(launch.State.Binding.RouteID, "gpt-5.5", ReasoningEffortHigh, true)
	}()
	select {
	case err := <-terminalDone:
		t.Fatalf("Terminal model switch escaped the global switch transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	release()
	if err := <-switchDone; err != nil {
		t.Fatal(err)
	}
	if err := <-terminalDone; err != nil {
		t.Fatal(err)
	}
	runtime, ok := owner.ThreadRuntime("queued-terminal-model-switch")
	if !ok || runtime.ConnectionID != connectionB.ID || runtime.ModelID != "gpt-5.5" || runtime.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("queued Terminal switch escaped onto the stale Provider: %#v", runtime)
	}
	state, ok := owner.Table().Get("queued-terminal-model-switch")
	if !ok || state.Binding.RouteID != launch.State.Binding.RouteID {
		t.Fatalf("queued Terminal switch replaced route identity: %#v", state.Binding)
	}
}

func TestSwitchProviderRollsBackOnRoutePersistFailure(t *testing.T) {
	root := t.TempDir()
	upstreamA := newE2EUpstream(t, nil)
	upstreamB := newE2EUpstream(t, nil)
	owner := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	connAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", upstreamA.server.URL+"/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := connAProjection.Connections[0]
	connBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", upstreamB.server.URL+"/v1", "gpt-5.4"), "key-b", connAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, connBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"gpt-5.4"},
		connB.ID: {"gpt-5.4"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	launch1, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch1.ProvisionalID, "thread-rollback-1"); err != nil {
		t.Fatal(err)
	}
	launch2, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch2.ProvisionalID, "thread-rollback-2"); err != nil {
		t.Fatal(err)
	}

	beforeDefault := owner.MustProjectForTest(t).Defaults[ClientCodex]
	beforeRuntime1, ok := owner.ThreadRuntime("thread-rollback-1")
	if !ok {
		t.Fatal("missing thread-rollback-1 before switch")
	}
	beforeRuntime2, ok := owner.ThreadRuntime("thread-rollback-2")
	if !ok {
		t.Fatal("missing thread-rollback-2 before switch")
	}

	failing := false
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if !failing && phase == "before_rename" {
			failing = true
			return errors.New("injected route persist failure")
		}
		return nil
	})
	t.Cleanup(func() { owner.RoutesFile().SetPersistHook(nil) })

	if _, err := owner.SwitchProvider(ClientCodex, connB.ID, owner.Catalog().Revision); err == nil {
		t.Fatal("switch provider unexpectedly succeeded")
	}

	if got := owner.MustProjectForTest(t).Defaults[ClientCodex]; got != beforeDefault {
		t.Fatalf("default changed after rollback: before=%#v after=%#v", beforeDefault, got)
	}
	if after, ok := owner.ThreadRuntime("thread-rollback-1"); !ok || after.ConnectionID != beforeRuntime1.ConnectionID || after.ModelID != beforeRuntime1.ModelID {
		t.Fatalf("runtime-1 changed after rollback: before=%#v after=%#v", beforeRuntime1, after)
	}
	if after, ok := owner.ThreadRuntime("thread-rollback-2"); !ok || after.ConnectionID != beforeRuntime2.ConnectionID || after.ModelID != beforeRuntime2.ModelID {
		t.Fatalf("runtime-2 changed after rollback: before=%#v after=%#v", beforeRuntime2, after)
	}

	_ = owner.Close()
	restored := startSettingsSwitchOwner(t, root)
	t.Cleanup(func() { _ = restored.Close() })
	if got := restored.MustProjectForTest(t).Defaults[ClientCodex]; got != beforeDefault {
		t.Fatalf("default not restored on restart after rollback: before=%#v after=%#v", beforeDefault, got)
	}
	if after, ok := restored.ThreadRuntime("thread-rollback-1"); !ok || after.ConnectionID != beforeRuntime1.ConnectionID || after.ModelID != beforeRuntime1.ModelID {
		t.Fatalf("runtime-1 not restored on restart: before=%#v after=%#v", beforeRuntime1, after)
	}
	if after, ok := restored.ThreadRuntime("thread-rollback-2"); !ok || after.ConnectionID != beforeRuntime2.ConnectionID || after.ModelID != beforeRuntime2.ModelID {
		t.Fatalf("runtime-2 not restored on restart: before=%#v after=%#v", beforeRuntime2, after)
	}
}
