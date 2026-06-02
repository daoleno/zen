package work

import "testing"

func TestAgentAdapterInfersProviderRuntimeAndCapabilities(t *testing.T) {
	cfg := &ExecutorConfig{
		Default: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "/opt/codex --no-alt-screen"},
			"other":  {Name: "other", Command: "my-agent"},
		},
	}

	codex, ok := cfg.AgentAdapter("codex")
	if !ok {
		t.Fatal("codex adapter missing")
	}
	if codex.Provider != "codex" || codex.Runtime != AgentRuntimeTmux {
		t.Fatalf("codex adapter = %+v", codex)
	}
	if !codex.Capabilities.NativeThreads ||
		!codex.Capabilities.NativeSearch ||
		!codex.Capabilities.NativeWorktrees ||
		!codex.Capabilities.NativeFork ||
		!codex.Capabilities.NativeResume ||
		!codex.Capabilities.NativeGoals ||
		!codex.Capabilities.InteractiveTTY ||
		!codex.Capabilities.StructuredEvents {
		t.Fatalf("codex capabilities = %+v", codex.Capabilities)
	}
	if codex.Capabilities.NativePinning {
		t.Fatalf("codex should not claim unverified native pinning: %+v", codex.Capabilities)
	}

	claude, ok := cfg.AgentAdapter("claude")
	if !ok {
		t.Fatal("claude adapter missing")
	}
	if claude.Provider != "claude" || !claude.Capabilities.InteractiveTTY {
		t.Fatalf("claude adapter = %+v", claude)
	}
	if claude.Capabilities.NativeThreads || claude.Capabilities.NativeSearch || claude.Capabilities.NativeResume || claude.Capabilities.NativeGoals {
		t.Fatalf("claude should not claim Codex-native capabilities: %+v", claude.Capabilities)
	}

	other, ok := cfg.AgentAdapter("other")
	if !ok {
		t.Fatal("other adapter missing")
	}
	if other.Provider != AgentProviderCustom || !other.Capabilities.InteractiveTTY {
		t.Fatalf("custom adapter = %+v", other)
	}
}

func TestDefaultAgentAdapterUsesConfiguredDefault(t *testing.T) {
	cfg := &ExecutorConfig{
		Default: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		},
	}

	got, ok := cfg.DefaultAgentAdapter()
	if !ok {
		t.Fatal("default adapter missing")
	}
	if got.ID != "claude" || !got.Preferred {
		t.Fatalf("default adapter = %+v", got)
	}
}
