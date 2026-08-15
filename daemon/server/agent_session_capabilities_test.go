package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/work"
)

func TestAgentSessionWireAdvertisesKnownStructuredProvider(t *testing.T) {
	srv := &Server{}
	for _, command := range []string{"codex", "pi --session /tmp/x.jsonl", "opencode --auto"} {
		wire := srv.agentSessionWire(&classifier.Agent{ID: command + "-1", Command: command})
		if wire == nil || !wire.Capabilities.StructuredEvents {
			t.Fatalf("%s capabilities = %#v", command, wire)
		}
	}
}

func TestAgentSessionWireUsesConfiguredExecutorCapabilityForGenericCommand(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"future": {
			Name:    "future",
			Command: "future-agent --structured",
			Kind:    work.AgentProviderGrok,
		},
	}}}
	agent := &classifier.Agent{ID: "future-1", Command: "/opt/bin/future-agent"}
	wire := srv.agentSessionWire(agent)
	if wire == nil || !wire.Capabilities.StructuredEvents {
		t.Fatalf("configured generic capabilities = %#v", wire)
	}
	provider := srv.structuredProviderForAgent(agent)
	if provider != work.AgentProviderGrok {
		t.Fatalf("configured provider = %q, want grok", provider)
	}
	conversation, err := work.NewProviderConversationReader().Load(*agent, provider, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Reason != "missing_cwd" {
		t.Fatalf("configured wrapper did not reach grok loader: %#v", conversation)
	}

	payload, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"id":"future-1"`) ||
		!strings.Contains(string(payload), `"structured_events":true`) ||
		!strings.Contains(string(payload), `"model_profile_managed":false`) ||
		!strings.Contains(string(payload), `"model_profile_active_switch":false`) {
		t.Fatalf("agent wire payload = %s", payload)
	}
}

func TestAgentSessionWireDoesNotAdvertisePlainShell(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"future": {
			Name:    "future",
			Command: "future-agent --structured",
			Kind:    work.AgentProviderGrok,
		},
	}}}
	wire := srv.agentSessionWire(&classifier.Agent{ID: "shell-1", Command: "zsh"})
	if wire == nil || wire.Capabilities.StructuredEvents {
		t.Fatalf("plain shell capabilities = %#v", wire)
	}
}

func TestAgentSessionWireDoesNotInferStructuredProviderFromShellTitle(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"codex": {
			Name:    "codex",
			Command: "codex",
			Kind:    work.AgentProviderCodex,
		},
	}}}
	for _, name := range []string{"Codex notes", "Claude research", "Cursor Agent scratch", "Grok shell"} {
		wire := srv.agentSessionWire(&classifier.Agent{
			ID:      "shell-title",
			Name:    name,
			Command: "zsh",
		})
		if wire == nil || wire.Capabilities.StructuredEvents {
			t.Fatalf("plain shell titled %q capabilities = %#v", name, wire)
		}
	}
}

func TestAgentSessionWireModelProfileCapabilitiesFromRouteTable(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	srv := &Server{}
	srv.SetModelProfiles(owner)

	// Non-routed: no binding — even Codex-named sessions stay unauthorized.
	bypass := srv.agentSessionWire(&classifier.Agent{ID: "shell:@1", Name: "Codex", Command: "codex"})
	if bypass.Capabilities.ModelProfileManaged || bypass.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("non-routed must not authorize: %#v", bypass.Capabilities)
	}

	routedProfile := modelprofiles.Profile{
		ID: "codex-routed", Name: "Routed", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(routedProfile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, routedProfile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@routed"); err != nil {
		t.Fatal(err)
	}
	routed := srv.agentSessionWire(&classifier.Agent{ID: "tmux:@routed", Name: "zsh notes", Command: "zsh"})
	// This owner has no live-control dir: the routed Codex session is a
	// pre-feature embedded session — managed but NOT active-switchable (a
	// switch could never reach the native thread without a restart).
	if !routed.Capabilities.ModelProfileManaged || routed.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("embedded routed capabilities %#v", routed.Capabilities)
	}
	// A live-control launch (control socket) advertises active switching.
	liveOwner := startLiveProfileOwner(t)
	liveSrv := &Server{}
	liveSrv.SetModelProfiles(liveOwner)
	liveProfile := routedProfile
	liveProfile.ID = "codex-live"
	liveProfile.ClientModel = "gpt-5"
	liveProfile.Model = "gpt-5"
	if _, err := liveOwner.UpsertProfile(liveProfile, 0, true); err != nil {
		t.Fatal(err)
	}
	livePlan, err := liveOwner.PrepareLaunch(modelprofiles.ExecutorCodex, liveProfile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if livePlan.CodexControlSocket == "" {
		t.Fatal("live-control owner must allocate a socket")
	}
	if _, _, _, err := liveOwner.CommitLaunch(livePlan.ProvisionalID, "tmux:@live-routed"); err != nil {
		t.Fatal(err)
	}
	liveRouted := liveSrv.agentSessionWire(&classifier.Agent{ID: "tmux:@live-routed", Name: "zsh notes", Command: "zsh"})
	if !liveRouted.Capabilities.ModelProfileManaged || !liveRouted.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("live-control routed capabilities %#v", liveRouted.Capabilities)
	}
	// Must not infer from command/name — zsh command with route still authorized by table only.
	if routed.Capabilities.StructuredEvents {
		t.Fatal("zsh must not gain structured_events from route presence")
	}

	nativeProfile := modelprofiles.Profile{
		ID: "codex-native", Name: "Native", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: modelprofiles.ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		AuthMode:              modelprofiles.AuthModeNone,
	}
	if _, err := owner.UpsertProfile(nativeProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	nativePlan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, nativeProfile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(nativePlan.ProvisionalID, "tmux:@native"); err != nil {
		t.Fatal(err)
	}
	native := srv.agentSessionWire(&classifier.Agent{ID: "tmux:@native", Name: "Codex", Command: "codex"})
	if !native.Capabilities.ModelProfileManaged || native.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("native managed=true switch=false: %#v", native.Capabilities)
	}

	anthropicProfile := modelprofiles.Profile{
		ID: "claude-main", Name: "Claude", ExecutorID: modelprofiles.ExecutorClaude,
		ProviderID: "anthropic", ProviderLabel: "Anthropic",
		Protocol: modelprofiles.ProtocolAnthropicMessages, ClientModel: "claude-sonnet-4-6", Model: "claude-sonnet-4-6",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.anthropic.com",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ANTHROPIC_API_KEY",
	}
	if _, err := owner.UpsertProfile(anthropicProfile, 2, true); err != nil {
		t.Fatal(err)
	}
	anthropicPlan, err := owner.PrepareLaunch(modelprofiles.ExecutorClaude, anthropicProfile.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(anthropicPlan.ProvisionalID, "tmux:@anthropic"); err != nil {
		t.Fatal(err)
	}
	anthropic := srv.agentSessionWire(&classifier.Agent{ID: "tmux:@anthropic", Name: "notes", Command: "zsh"})
	if !anthropic.Capabilities.ModelProfileManaged || !anthropic.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("anthropic managed=true switch=true: %#v", anthropic.Capabilities)
	}

	// List projection matches per-session wire.
	list := srv.agentSessionsWire([]*classifier.Agent{
		{ID: "tmux:@routed", Command: "zsh"},
		{ID: "tmux:@native", Command: "codex"},
		{ID: "tmux:@anthropic", Command: "zsh"},
		{ID: "shell:@1", Command: "codex"},
	})
	if len(list) != 4 {
		t.Fatalf("list len=%d", len(list))
	}
	if !list[0].Capabilities.ModelProfileManaged || list[0].Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("list responses (embedded codex) must be managed-only: %#v", list[0].Capabilities)
	}
	if !list[1].Capabilities.ModelProfileManaged || list[1].Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("list native %#v", list[1].Capabilities)
	}
	if !list[2].Capabilities.ModelProfileManaged || !list[2].Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("list anthropic %#v", list[2].Capabilities)
	}
	if list[3].Capabilities.ModelProfileManaged || list[3].Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("list ordinary %#v", list[3].Capabilities)
	}

	// Restart projection must match (route table reload).
	profiles, routes := owner.Store().Path(), owner.RoutesFile().Path()
	listener := filepath.Join(root, "route-listener.json")
	addr := owner.ListenAddr()
	_ = owner.Close()
	owner2, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: func(string) (string, bool) { return "ready", true }, Verifier: wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.ListenAddr() != addr {
		t.Fatalf("listener %q -> %q", addr, owner2.ListenAddr())
	}
	srv2 := &Server{}
	srv2.SetModelProfiles(owner2)
	restarted := srv2.agentSessionWire(&classifier.Agent{ID: "tmux:@routed", Command: "zsh"})
	// The embedded (socket-less) binding survives restart as managed-only —
	// never advertising a switch that could not reach the native thread.
	if !restarted.Capabilities.ModelProfileManaged || restarted.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("restart routed (embedded) must be managed-only: %#v", restarted.Capabilities)
	}
	restartNative := srv2.agentSessionWire(&classifier.Agent{ID: "tmux:@native", Command: "codex"})
	if !restartNative.Capabilities.ModelProfileManaged || restartNative.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("restart native %#v", restartNative.Capabilities)
	}
	restartAnthropic := srv2.agentSessionWire(&classifier.Agent{ID: "tmux:@anthropic", Command: "zsh"})
	if !restartAnthropic.Capabilities.ModelProfileManaged || !restartAnthropic.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("restart anthropic %#v", restartAnthropic.Capabilities)
	}
}
