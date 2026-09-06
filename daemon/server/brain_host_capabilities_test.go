package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainSnapshotHostWorkerCapabilitiesRoutedHidden(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "zen-worker-brain-hidden:@routed"
	bindRoutedCodexHost(t, owner, hostID)
	hidden := &classifier.Worker{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == hostID {
			return hidden
		}
		return nil
	}

	wire := mustBrainSnapshotHostWire(t, srv, hostID, "codex")
	caps := hostCapabilitiesFromWire(t, wire)
	if !caps.ModelProfileManaged || !caps.ModelProfileActiveSwitch || !caps.StructuredEvents {
		t.Fatalf("routed hidden host capabilities = %#v", caps)
	}
	assertNoRouteOrSecretLeak(t, wire)
	assertHostStaysHiddenFromWorkerList(t, srv, hidden)
}

func TestBrainSnapshotHostWorkerCapabilitiesManagedNativeReadOnly(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "zen-worker-brain-hidden:@native"
	profile := modelprofiles.Profile{
		ID: "codex-native", Name: "Native", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: modelprofiles.ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		AuthMode:              modelprofiles.AuthModeNone,
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, hostID); err != nil {
		t.Fatal(err)
	}
	hidden := &classifier.Worker{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == hostID {
			return hidden
		}
		return nil
	}

	wire := mustBrainSnapshotHostWire(t, srv, hostID, "codex")
	caps := hostCapabilitiesFromWire(t, wire)
	if !caps.ModelProfileManaged || caps.ModelProfileActiveSwitch || !caps.StructuredEvents {
		t.Fatalf("native managed read-only capabilities = %#v", caps)
	}
	assertNoRouteOrSecretLeak(t, wire)
	assertHostStaysHiddenFromWorkerList(t, srv, hidden)
}

func TestBrainSnapshotHostWorkerCapabilitiesUnmanaged(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "zen-worker-brain-hidden:@unmanaged"
	hidden := &classifier.Worker{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == hostID {
			return hidden
		}
		return nil
	}

	wire := mustBrainSnapshotHostWire(t, srv, hostID, "codex")
	caps := hostCapabilitiesFromWire(t, wire)
	if caps.ModelProfileManaged || caps.ModelProfileActiveSwitch {
		t.Fatalf("unmanaged host must not authorize profile actions: %#v", caps)
	}
	if !caps.StructuredEvents {
		t.Fatalf("live codex host still advertises structured_events: %#v", caps)
	}
	assertNoRouteOrSecretLeak(t, wire)
	assertHostStaysHiddenFromWorkerList(t, srv, hidden)
}

func TestBrainSnapshotBroadcastDoesNotAdmitHostActivation(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID   = "zen-worker-brain-hidden:@busy-broadcast"
		threadID = "brain-thread-client-data-restored"
	)
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	fw := &brainServiceTestWatcher{sessions: map[string]*classifier.Worker{
		hostID: {
			ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
			State: classifier.StateRunning, Summary: "Processing current provider turn",
		},
	}}
	service := brain.NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))
	srv := &Server{brain: service}
	var broadcast map[string]any
	srv.brainSnapshotBroadcastHook = func(payload map[string]any) {
		broadcast = payload
	}

	srv.broadcastBrainSnapshot()

	if broadcast == nil || broadcast["type"] != "brain_snapshot" {
		t.Fatalf("brain snapshot was not broadcast: %#v", broadcast)
	}
	payload, ok := broadcast["brain"].(map[string]any)
	if !ok || payload["chat_thread_id"] != threadID {
		t.Fatalf("brain client data payload = %#v", broadcast["brain"])
	}
	if fw.readyInputCalls != 0 || fw.receiptInputCalls != 0 {
		t.Fatalf("snapshot broadcast mutated Host input: ready=%d receipt=%d", fw.readyInputCalls, fw.receiptInputCalls)
	}
	activation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if activation.SessionID != "" {
		t.Fatalf("snapshot broadcast falsely marked activation: %+v", activation)
	}
}

func TestBrainSnapshotHostWorkerCapabilitiesFailClosedMissingWorkerOrOwner(t *testing.T) {
	hostID := "zen-worker-brain-hidden:@missing"

	// No watcher agent and no profile owner.
	srv := &Server{}
	wire := mustBrainSnapshotHostWire(t, srv, hostID, "codex")
	caps := hostCapabilitiesFromWire(t, wire)
	if caps.StructuredEvents || caps.ModelProfileManaged || caps.ModelProfileActiveSwitch {
		t.Fatalf("missing agent/owner must fail closed: %#v", caps)
	}

	// Profile owner present but watcher agent missing: still fail closed.
	owner := startBrainHostCapabilityOwner(t)
	bindRoutedCodexHost(t, owner, hostID)
	srv2 := &Server{}
	srv2.SetModelProfiles(owner)
	wire2 := mustBrainSnapshotHostWire(t, srv2, hostID, "codex")
	caps2 := hostCapabilitiesFromWire(t, wire2)
	if caps2.StructuredEvents || caps2.ModelProfileManaged || caps2.ModelProfileActiveSwitch {
		t.Fatalf("missing watcher agent must fail closed even with route: %#v", caps2)
	}

	// Name/command must never authorize without a route, even when agent is live.
	srv3 := &Server{}
	srv3.SetModelProfiles(owner)
	srv3.getWorkerOverride = func(id string) *classifier.Worker {
		if id == "zen-worker-brain-hidden:@named" {
			return &classifier.Worker{
				ID: id, Name: "Codex Brain", Command: "codex", Hidden: true,
			}
		}
		return nil
	}
	wire3 := mustBrainSnapshotHostWire(t, srv3, "zen-worker-brain-hidden:@named", "codex")
	caps3 := hostCapabilitiesFromWire(t, wire3)
	if caps3.ModelProfileManaged || caps3.ModelProfileActiveSwitch {
		t.Fatalf("name/command must not authorize managed/switch: %#v", caps3)
	}
}

func TestBrainSnapshotHostWorkerCapabilitiesSharedWirePath(t *testing.T) {
	// sendBrainSnapshot / broadcastBrainSnapshot / NewChat / executor-switch
	// all serialize through brainSnapshotWire — prove the enrichment lives there.
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)
	hostID := "zen-worker-brain-hidden:@shared"
	bindRoutedCodexHost(t, owner, hostID)
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == hostID {
			return &classifier.Worker{ID: hostID, Name: "Brain", Command: "codex", Hidden: true}
		}
		return nil
	}
	snapshot := brain.Snapshot{
		HostWorker:  &brain.WorkerRef{ID: hostID, Name: "Brain", Command: "codex", Hidden: true, Updated: time.Now().UTC()},
		GeneratedAt: time.Now().UTC(),
	}
	wire, err := srv.brainSnapshotWire(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	caps := hostCapabilitiesFromWire(t, wire.(map[string]any))
	if !caps.ModelProfileManaged || !caps.ModelProfileActiveSwitch {
		t.Fatalf("shared brainSnapshotWire path capabilities = %#v", caps)
	}
}

func TestHiddenHostDiscoveryRefreshesBrainSnapshotCapabilities(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	hostID := "zen-worker-brain-hidden:@lifecycle"
	bindRoutedCodexHost(t, owner, hostID)

	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	bw := &killTrackingWatcher{}
	service := brain.NewService(store, bw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	srv := &Server{brain: service}
	srv.SetModelProfiles(owner)
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		return bw.GetWorker(id)
	}

	// Reconnect-style projection before watcher discovery: fail closed.
	initial, err := srv.brainSnapshotWire(brain.Snapshot{
		HostWorker: &brain.WorkerRef{
			ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
			Updated: time.Now().UTC(),
		},
		GeneratedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	initialCaps := hostCapabilitiesFromWire(t, initial.(map[string]any))
	if initialCaps.ModelProfileManaged || initialCaps.ModelProfileActiveSwitch || initialCaps.StructuredEvents {
		t.Fatalf("missing watcher agent must stay false: %#v", initialCaps)
	}

	var broadcasts []map[string]any
	srv.brainSnapshotBroadcastHook = func(payload map[string]any) {
		broadcasts = append(broadcasts, payload)
	}

	hiddenHost := &classifier.Worker{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	// Unrelated Hidden agent noise must not churn brain_snapshot.
	otherHidden := &classifier.Worker{
		ID: "zen-worker-other-hidden:@9", Name: "Other", Command: "codex", Hidden: true,
	}
	if bw.sessions == nil {
		bw.sessions = map[string]*classifier.Worker{}
	}
	bw.sessions[otherHidden.ID] = otherHidden
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_discovered", WorkerID: otherHidden.ID, Worker: otherHidden,
	})
	if len(broadcasts) != 0 {
		t.Fatalf("unrelated hidden discovery broadcast=%d", len(broadcasts))
	}

	// Discover current Host: authoritative capability refresh.
	bw.sessions[hostID] = hiddenHost
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_discovered", WorkerID: hostID, Worker: hiddenHost,
	})
	if len(broadcasts) != 1 {
		t.Fatalf("current host discovery broadcasts=%d", len(broadcasts))
	}
	brainPayload, ok := broadcasts[0]["brain"].(map[string]any)
	if !ok || broadcasts[0]["type"] != "brain_snapshot" {
		t.Fatalf("broadcast=%#v", broadcasts[0])
	}
	discoveredCaps := hostCapabilitiesFromWire(t, brainPayload)
	if !discoveredCaps.ModelProfileManaged || !discoveredCaps.ModelProfileActiveSwitch || !discoveredCaps.StructuredEvents {
		t.Fatalf("discovered host capabilities = %#v", discoveredCaps)
	}
	hostWire, _ := brainPayload["host_worker"].(map[string]any)
	if hidden, _ := hostWire["hidden"].(bool); !hidden {
		t.Fatal("host must remain hidden on capability refresh")
	}
	assertHostStaysHiddenFromWorkerList(t, srv, hiddenHost)

	// Output / turn noise must not churn.
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_output", WorkerID: hostID, Worker: hiddenHost,
	})
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_state_change", WorkerID: hostID, Worker: hiddenHost, NewState: "running",
	})
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_metadata_change", WorkerID: hostID, Worker: hiddenHost,
	})
	if len(broadcasts) != 1 {
		t.Fatalf("output/turn noise churned brain_snapshot: broadcasts=%d", len(broadcasts))
	}

	// Removal of the current Host refreshes capabilities only (projection).
	// It must fail-closed managed=false and must not drive ensureHostAgent
	// continuity (no CreateSession / route transfer / host binding rewrite).
	beforeHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	beforeCreated := bw.created
	routeBefore, routeOK := owner.Table().Get(hostID)
	if !routeOK {
		t.Fatal("expected route on recorded host before removal")
	}
	delete(bw.sessions, hostID)
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "worker_removed", WorkerID: hostID, Worker: hiddenHost,
	})
	if len(broadcasts) != 2 {
		t.Fatalf("host removal broadcasts=%d, want 2", len(broadcasts))
	}
	removalBrain, ok := broadcasts[1]["brain"].(map[string]any)
	if !ok || broadcasts[1]["type"] != "brain_snapshot" {
		t.Fatalf("removal broadcast=%#v", broadcasts[1])
	}
	removedCaps := hostCapabilitiesFromWire(t, removalBrain)
	if removedCaps.ModelProfileManaged || removedCaps.ModelProfileActiveSwitch || removedCaps.StructuredEvents {
		t.Fatalf("removed host must fail-closed capabilities: %#v", removedCaps)
	}
	removedHost, _ := removalBrain["host_worker"].(map[string]any)
	if id, _ := removedHost["id"].(string); id != hostID {
		t.Fatalf("removal payload must keep recorded host id %q, got %#v", hostID, removedHost)
	}
	if bw.created != beforeCreated {
		t.Fatalf("removal projection must not CreateSession: before=%d after=%d", beforeCreated, bw.created)
	}
	if len(bw.killed) != 0 {
		t.Fatalf("removal projection must not kill: %#v", bw.killed)
	}
	afterHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHost, afterHost) {
		t.Fatalf("host/provider binding mutated by projection:\nbefore=%s\nafter=%s", beforeHost, afterHost)
	}
	routeAfter, routeOK := owner.Table().Get(hostID)
	if !routeOK {
		t.Fatal("removal projection must not drop route binding")
	}
	if routeAfter.Binding.RouteID != routeBefore.Binding.RouteID || routeAfter.Binding.SessionID != hostID {
		t.Fatalf("route mutated: before=%+v after=%+v", routeBefore.Binding, routeAfter.Binding)
	}
	assertHostStaysHiddenFromWorkerList(t, srv, hiddenHost)
}

func startBrainHostCapabilityOwner(t *testing.T) *modelprofiles.Owner {
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

func bindRoutedCodexHost(t *testing.T, owner *modelprofiles.Owner, sessionID string) {
	t.Helper()
	profile := modelprofiles.Profile{
		ID: "codex-routed-host", Name: "Routed", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	rev := owner.Catalog().Revision
	if _, err := owner.UpsertProfile(profile, rev, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, sessionID); err != nil {
		t.Fatal(err)
	}
}

func mustBrainSnapshotHostWire(t *testing.T, srv *Server, hostID, command string) map[string]any {
	t.Helper()
	snapshot := brain.Snapshot{
		HostWorker: &brain.WorkerRef{
			ID: hostID, Name: "Brain", Command: command, Hidden: true,
			Status: string(classifier.StateRunning), Updated: time.Now().UTC(),
		},
		GeneratedAt: time.Now().UTC(),
	}
	wire, err := srv.brainSnapshotWire(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := wire.(map[string]any)
	if !ok {
		t.Fatalf("wire type %T", wire)
	}
	return payload
}

func hostCapabilitiesFromWire(t *testing.T, payload map[string]any) workerSessionWireCapabilities {
	t.Helper()
	hostRaw, ok := payload["host_worker"].(map[string]any)
	if !ok {
		t.Fatalf("host_worker missing: %#v", payload["host_worker"])
	}
	capsRaw, ok := hostRaw["capabilities"]
	if !ok || capsRaw == nil {
		t.Fatalf("host_worker.capabilities missing: %#v", hostRaw)
	}
	raw, err := json.Marshal(capsRaw)
	if err != nil {
		t.Fatal(err)
	}
	var caps workerSessionWireCapabilities
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	return caps
}

func assertNoRouteOrSecretLeak(t *testing.T, payload map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, banned := range []string{
		`"route_id"`, `"route_protocol"`, `"provisional"`,
		`"credential_env"`, `"ACME_KEY"`, `"api_key"`, `"Authorization"`,
		`"listen_addr"`, `"pending:`,
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("brain_snapshot leaked %s: %s", banned, body)
		}
	}
	host := payload["host_worker"].(map[string]any)
	if _, ok := host["route"]; ok {
		t.Fatal("host_worker must not embed route snapshot")
	}
	if _, ok := host["binding"]; ok {
		t.Fatal("host_worker must not embed binding")
	}
}

func assertHostStaysHiddenFromWorkerList(t *testing.T, srv *Server, host *classifier.Worker) {
	t.Helper()
	visible := &classifier.Worker{ID: "tmux:@visible", Name: "Visible", Command: "codex", Hidden: false}
	list := visibleWorkerSessions([]*classifier.Worker{host, visible})
	if len(list) != 1 || list[0].ID != visible.ID {
		t.Fatalf("visible list = %#v", list)
	}
	wired := srv.workerSessionsWire(list)
	if len(wired) != 1 || wired[0].ID != visible.ID {
		t.Fatalf("worker_session_list wire = %#v", wired)
	}
	for _, session := range wired {
		if session.ID == host.ID || session.Hidden {
			t.Fatalf("hidden Brain host must not appear in worker_session_list: %#v", session)
		}
	}
}
