package work

import (
	"strings"
	"testing"
)

func TestHardenCodexDelegatedCommandAppendsFullAuthorization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain codex",
			in:   "codex",
			want: "codex --dangerously-bypass-approvals-and-sandbox",
		},
		{
			name: "codex with no-alt-screen",
			in:   "codex --no-alt-screen",
			want: "codex --no-alt-screen --dangerously-bypass-approvals-and-sandbox",
		},
		{
			name: "already authorized unchanged",
			in:   "codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen",
			want: "codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen",
		},
		{
			name: "absolute path",
			in:   "/opt/codex --no-alt-screen",
			want: "/opt/codex --no-alt-screen --dangerously-bypass-approvals-and-sandbox",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HardenCodexDelegatedCommand(tc.in); got != tc.want {
				t.Fatalf("HardenCodexDelegatedCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Count(HardenCodexDelegatedCommand(tc.in), CodexFullAuthorizationFlag) != 1 {
				t.Fatalf("full authorization flag duplicated for %q", tc.in)
			}
		})
	}
}

func TestHardenCodexDelegatedCommandDefaultsToCodex(t *testing.T) {
	got := HardenCodexDelegatedCommand("")
	if !strings.HasPrefix(got, "codex ") {
		t.Fatalf("empty input should default to codex, got %q", got)
	}
	if !strings.Contains(got, CodexFullAuthorizationFlag) {
		t.Fatalf("default codex command should be hardened: %q", got)
	}
}

func TestHardenClaudeCommandAppendsFullAuthorization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain claude",
			in:   "claude",
			want: "claude --permission-mode bypassPermissions",
		},
		{
			name: "claude with profile",
			in:   "claude --profile my-profile",
			want: "claude --profile my-profile --permission-mode bypassPermissions",
		},
		{
			name: "already authorized dontAsk",
			in:   "claude --permission-mode dontAsk",
			want: "claude --permission-mode dontAsk",
		},
		{
			name: "already authorized bypassPermissions",
			in:   "claude --permission-mode bypassPermissions",
			want: "claude --permission-mode bypassPermissions",
		},
		{
			name: "absolute path",
			in:   "/usr/local/bin/claude --profile test",
			want: "/usr/local/bin/claude --profile test --permission-mode bypassPermissions",
		},
		{
			name: "dangerously-skip-permissions preserved",
			in:   "claude --dangerously-skip-permissions",
			want: "claude --dangerously-skip-permissions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HardenClaudeCommand(tc.in)
			if got != tc.want {
				t.Fatalf("HardenClaudeCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(tc.want, "--permission-mode") {
				permissionModeCount := strings.Count(got, "--permission-mode")
				if permissionModeCount != 1 {
					t.Fatalf("permission mode flag appears %d times for %q", permissionModeCount, tc.in)
				}
			}
			if strings.Contains(tc.in, "--dangerously-skip-permissions") && strings.Contains(got, "--permission-mode") {
				t.Fatalf("dangerously-skip-permissions command should not gain --permission-mode: %q", got)
			}
		})
	}
}

func TestHardenClaudeCommandDefaultsToClaude(t *testing.T) {
	got := HardenClaudeCommand("")
	if !strings.HasPrefix(got, "claude ") {
		t.Fatalf("empty input should default to claude, got %q", got)
	}
	if !strings.Contains(got, ClaudeFullAuthorizationFlag) {
		t.Fatalf("default claude command should be hardened: %q", got)
	}
}

func TestAgentExecutorInfersProviderRuntimeAndCapabilities(t *testing.T) {
	cfg := NewExecutorConfig("claude", map[string]Executor{
		"agent":  {Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "/opt/codex --no-alt-screen"},
		"other":  {Name: "other", Command: "my-agent"},
	})

	codex, ok := cfg.AgentExecutor("codex")
	if !ok {
		t.Fatal("codex executor missing")
	}
	if codex.Provider != "codex" || codex.Runtime != AgentRuntimeTmux {
		t.Fatalf("codex executor = %+v", codex)
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

	claude, ok := cfg.AgentExecutor("claude")
	if !ok {
		t.Fatal("claude executor missing")
	}
	if claude.Provider != "claude" || !claude.Capabilities.InteractiveTTY || !claude.Capabilities.StructuredEvents {
		t.Fatalf("claude executor = %+v", claude)
	}
	if claude.Capabilities.NativeThreads || claude.Capabilities.NativeSearch || claude.Capabilities.NativeResume || claude.Capabilities.NativeGoals {
		t.Fatalf("claude should not claim Codex-native capabilities: %+v", claude.Capabilities)
	}

	cursor, ok := cfg.AgentExecutor("agent")
	if !ok {
		t.Fatal("agent executor missing")
	}
	if cursor.Provider != AgentProviderCursor || !cursor.Capabilities.InteractiveTTY || !cursor.Capabilities.StructuredEvents {
		t.Fatalf("cursor executor = %+v", cursor)
	}
	if cursor.Capabilities.NativeThreads || cursor.Capabilities.NativeSearch {
		t.Fatalf("cursor should not claim unsupported native thread/search capabilities: %+v", cursor.Capabilities)
	}

	other, ok := cfg.AgentExecutor("other")
	if !ok {
		t.Fatal("other executor missing")
	}
	if other.Provider != AgentProviderCustom || !other.Capabilities.InteractiveTTY {
		t.Fatalf("custom executor = %+v", other)
	}
}

func TestInferAgentProviderRecognizesCursorAgent(t *testing.T) {
	for _, value := range []string{
		"cursor",
		"cursor-agent",
		"/home/daoleno/.local/bin/cursor-agent --force",
	} {
		if got := InferAgentProvider(value); got != AgentProviderCursor {
			t.Fatalf("InferAgentProvider(%q) = %q, want %q", value, got, AgentProviderCursor)
		}
	}
	if got := InferAgentProvider("agent"); got != "" {
		t.Fatalf("InferAgentProvider(%q) = %q, want empty", "agent", got)
	}
}

func TestDelegatedAgentExecutorUsesConfiguredDelegatedExecutor(t *testing.T) {
	cfg := NewExecutorConfig("claude", map[string]Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	})

	got, ok := cfg.DelegatedAgentExecutor()
	if !ok {
		t.Fatal("delegated executor missing")
	}
	if got.ID != "claude" || !got.Delegated {
		t.Fatalf("delegated executor = %+v", got)
	}
}

func TestDelegatedAgentExecutorFallsBackToCodexWhenUnset(t *testing.T) {
	cfg := &ExecutorConfig{
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		},
	}

	got, ok := cfg.DelegatedAgentExecutor()
	if !ok {
		t.Fatal("delegated executor missing")
	}
	if got.ID != "codex" || !got.Delegated {
		t.Fatalf("delegated executor = %+v", got)
	}
}

func TestDelegatedAgentExecutorFallsBackToFirstConfiguredExecutor(t *testing.T) {
	cfg := &ExecutorConfig{
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"grok":   {Name: "grok", Command: "grok"},
		},
	}

	got, ok := cfg.DelegatedAgentExecutor()
	if !ok {
		t.Fatal("delegated executor missing")
	}
	if got.ID != "claude" || !got.Delegated {
		t.Fatalf("delegated executor = %+v", got)
	}
}
