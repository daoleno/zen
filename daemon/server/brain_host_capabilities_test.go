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

func TestBrainSnapshotHostAgentCapabilitiesRoutedHidden(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "brain-agent-brain-hidden:@routed"
	bindRoutedCodexHost(t, owner, hostID)
	hidden := &classifier.Agent{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getAgentOverride = func(id string) *classifier.Agent {
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
	assertHostStaysHiddenFromAgentList(t, srv, hidden)
}

func TestBrainSnapshotHostAgentCapabilitiesManagedNativeReadOnly(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "brain-agent-brain-hidden:@native"
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
	hidden := &classifier.Agent{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getAgentOverride = func(id string) *classifier.Agent {
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
	assertHostStaysHiddenFromAgentList(t, srv, hidden)
}

func TestBrainSnapshotHostAgentCapabilitiesUnmanaged(t *testing.T) {
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)

	hostID := "brain-agent-brain-hidden:@unmanaged"
	hidden := &classifier.Agent{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	srv.getAgentOverride = func(id string) *classifier.Agent {
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
	assertHostStaysHiddenFromAgentList(t, srv, hidden)
}

func TestBrainSnapshotHostAgentCapabilitiesFailClosedMissingAgentOrOwner(t *testing.T) {
	hostID := "brain-agent-brain-hidden:@missing"

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
	srv3.getAgentOverride = func(id string) *classifier.Agent {
		if id == "brain-agent-brain-hidden:@named" {
			return &classifier.Agent{
				ID: id, Name: "Codex Brain", Command: "codex", Hidden: true,
			}
		}
		return nil
	}
	wire3 := mustBrainSnapshotHostWire(t, srv3, "brain-agent-brain-hidden:@named", "codex")
	caps3 := hostCapabilitiesFromWire(t, wire3)
	if caps3.ModelProfileManaged || caps3.ModelProfileActiveSwitch {
		t.Fatalf("name/command must not authorize managed/switch: %#v", caps3)
	}
}

func TestBrainSnapshotHostAgentCapabilitiesSharedWirePath(t *testing.T) {
	// sendBrainSnapshot / broadcastBrainSnapshot / NewChat / executor-switch
	// all serialize through brainSnapshotWire — prove the enrichment lives there.
	owner := startBrainHostCapabilityOwner(t)
	srv := &Server{}
	srv.SetModelProfiles(owner)
	hostID := "brain-agent-brain-hidden:@shared"
	bindRoutedCodexHost(t, owner, hostID)
	srv.getAgentOverride = func(id string) *classifier.Agent {
		if id == hostID {
			return &classifier.Agent{ID: hostID, Name: "Brain", Command: "codex", Hidden: true}
		}
		return nil
	}
	snapshot := brain.Snapshot{
		HostAgent:   &brain.AgentRef{ID: hostID, Name: "Brain", Command: "codex", Hidden: true, Updated: time.Now().UTC()},
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
	hostID := "brain-agent-brain-hidden:@lifecycle"
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
	srv.getAgentOverride = func(id string) *classifier.Agent {
		return bw.GetAgent(id)
	}

	// Reconnect-style projection before watcher discovery: fail closed.
	initial, err := srv.brainSnapshotWire(brain.Snapshot{
		HostAgent: &brain.AgentRef{
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

	hiddenHost := &classifier.Agent{
		ID: hostID, Name: "Brain", Command: "codex", Hidden: true,
		State: classifier.StateRunning,
	}
	// Unrelated Hidden agent noise must not churn brain_snapshot.
	otherHidden := &classifier.Agent{
		ID: "brain-agent-other-hidden:@9", Name: "Other", Command: "codex", Hidden: true,
	}
	if bw.sessions == nil {
		bw.sessions = map[string]*classifier.Agent{}
	}
	bw.sessions[otherHidden.ID] = otherHidden
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "agent_discovered", AgentID: otherHidden.ID, Agent: otherHidden,
	})
	if len(broadcasts) != 0 {
		t.Fatalf("unrelated hidden discovery broadcast=%d", len(broadcasts))
	}

	// Discover current Host: authoritative capability refresh.
	bw.sessions[hostID] = hiddenHost
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "agent_discovered", AgentID: hostID, Agent: hiddenHost,
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
	hostWire, _ := brainPayload["host_agent"].(map[string]any)
	if hidden, _ := hostWire["hidden"].(bool); !hidden {
		t.Fatal("host must remain hidden on capability refresh")
	}
	assertHostStaysHiddenFromAgentList(t, srv, hiddenHost)

	// Output / turn noise must not churn.
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "agent_output", AgentID: hostID, Agent: hiddenHost,
	})
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "agent_state_change", AgentID: hostID, Agent: hiddenHost, NewState: "running",
	})
	srv.handleWatcherEvent(watcher.SessionEvent{
		Type: "agent_metadata_change", AgentID: hostID, Agent: hiddenHost,
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
		Type: "agent_removed", AgentID: hostID, Agent: hiddenHost,
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
	removedHost, _ := removalBrain["host_agent"].(map[string]any)
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
	assertHostStaysHiddenFromAgentList(t, srv, hiddenHost)
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
		HostAgent: &brain.AgentRef{
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

func hostCapabilitiesFromWire(t *testing.T, payload map[string]any) agentSessionWireCapabilities {
	t.Helper()
	hostRaw, ok := payload["host_agent"].(map[string]any)
	if !ok {
		t.Fatalf("host_agent missing: %#v", payload["host_agent"])
	}
	capsRaw, ok := hostRaw["capabilities"]
	if !ok || capsRaw == nil {
		t.Fatalf("host_agent.capabilities missing: %#v", hostRaw)
	}
	raw, err := json.Marshal(capsRaw)
	if err != nil {
		t.Fatal(err)
	}
	var caps agentSessionWireCapabilities
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
	host := payload["host_agent"].(map[string]any)
	if _, ok := host["route"]; ok {
		t.Fatal("host_agent must not embed route snapshot")
	}
	if _, ok := host["binding"]; ok {
		t.Fatal("host_agent must not embed binding")
	}
}

func assertHostStaysHiddenFromAgentList(t *testing.T, srv *Server, host *classifier.Agent) {
	t.Helper()
	visible := &classifier.Agent{ID: "tmux:@visible", Name: "Visible", Command: "codex", Hidden: false}
	list := visibleAgentSessions([]*classifier.Agent{host, visible})
	if len(list) != 1 || list[0].ID != visible.ID {
		t.Fatalf("visible list = %#v", list)
	}
	wired := srv.agentSessionsWire(list)
	if len(wired) != 1 || wired[0].ID != visible.ID {
		t.Fatalf("agent_session_list wire = %#v", wired)
	}
	for _, session := range wired {
		if session.ID == host.ID || session.Hidden {
			t.Fatalf("hidden Brain host must not appear in agent_session_list: %#v", session)
		}
	}
}
