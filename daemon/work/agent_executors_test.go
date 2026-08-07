package work

import (
	"errors"
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
		{"env value containing flag unchanged", "env NOTE=--dangerously-bypass-approvals-and-sandbox codex", "env NOTE=--dangerously-bypass-approvals-and-sandbox codex"},
		{"option value containing flag unchanged", "codex --note=--dangerously-bypass-approvals-and-sandbox", "codex --note=--dangerously-bypass-approvals-and-sandbox"},
		{"equivalent aliases still gain literal flag", "codex -a never -s danger-full-access", "codex -a never -s danger-full-access --dangerously-bypass-approvals-and-sandbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HardenCodexDelegatedCommand(tc.in); got != tc.want {
				t.Fatalf("HardenCodexDelegatedCommand(%q) = %q, want %q", tc.in, got, tc.want)
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
		{"env value containing option unchanged", "env MODE=--permission-mode VALUE=bypassPermissions claude", "env MODE=--permission-mode VALUE=bypassPermissions claude"},
		{"quoted equals", `claude "--permission-mode=bypassPermissions"`, `claude "--permission-mode=bypassPermissions"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HardenClaudeCommand(tc.in)
			if got != tc.want {
				t.Fatalf("HardenClaudeCommand(%q) = %q, want %q", tc.in, got, tc.want)
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

func TestScheduledActionCommandRecognizesExactUnattendedArgv(t *testing.T) {
	tests := []struct {
		name, executorID, command, kind, want string
	}{
		{"Codex env spoof", "codex", "env NOTE=--dangerously-bypass-approvals-and-sandbox codex", "", "env NOTE=--dangerously-bypass-approvals-and-sandbox codex --dangerously-bypass-approvals-and-sandbox"},
		{"Codex aliases", "codex", "codex -a never -s danger-full-access", "", "codex -a never -s danger-full-access"},
		{"Codex attached aliases", "codex", "codex -anever -sdanger-full-access", "", "codex -anever -sdanger-full-access"},
		{"Codex attached approval", "codex", "codex -anever", "", "codex -anever --sandbox danger-full-access"},
		{"Codex attached sandbox", "codex", "codex -sdanger-full-access", "", "codex -sdanger-full-access --ask-for-approval never"},
		{"Codex quoted equals", "codex", `env NOTE='not an option' -- codex "--ask-for-approval=never" '--sandbox=danger-full-access'`, "", `env NOTE='not an option' -- codex "--ask-for-approval=never" '--sandbox=danger-full-access'`},
		{"Codex quoted hash", "codex", `codex '# configured-note'`, "", `codex '# configured-note' --dangerously-bypass-approvals-and-sandbox`},
		{"Codex escaped hash", "codex", `env PROFILE=calendar -- codex \#configured-note`, "", `env PROFILE=calendar -- codex \#configured-note --dangerously-bypass-approvals-and-sandbox`},
		{"Codex partial approval", "codex", "codex -a=never", "", "codex -a=never --sandbox danger-full-access"},
		{"Codex partial sandbox", "codex", "codex -s=danger-full-access", "", "codex -s=danger-full-access --ask-for-approval never"},
		{"Codex before terminator", "codex", `codex -a never -s danger-full-access -- "literal prompt"`, "", `codex -a never -s danger-full-access -- "literal prompt"`},
		{"Claude quoted equals", "claude", `claude "--permission-mode=bypassPermissions"`, "", `claude "--permission-mode=bypassPermissions"`},
		{"Cursor substrings", "cursor", "env FORCE=--force cursor-agent --forceful --sandbox=disabled --distrust --approve-mcps-extra", "cursor", "env FORCE=--force cursor-agent --forceful --sandbox=disabled --distrust --approve-mcps-extra --force --trust --approve-mcps"},
		{"Cursor quoted exact", "cursor", `cursor-agent '--yolo' "--sandbox=disabled" '--trust' "--approve-mcps"`, "cursor", `cursor-agent '--yolo' "--sandbox=disabled" '--trust' "--approve-mcps"`},
		{"Grok env spoof", "grok", "env APPROVAL=--always-approve grok --sandbox=off", "", "env APPROVAL=--always-approve grok --sandbox=off --permission-mode bypassPermissions"},
		{"Grok quoted equals", "grok", `grok '--permission-mode=bypassPermissions' "--sandbox=off"`, "", `grok '--permission-mode=bypassPermissions' "--sandbox=off"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := Executor{Name: test.executorID, Command: test.command, Kind: test.kind}
			got, err := ScheduledActionCommand(test.executorID, executor)
			if err != nil {
				t.Fatalf("ScheduledActionCommand returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("ScheduledActionCommand() = %q, want %q", got, test.want)
			}
			if executor.Command != test.command {
				t.Fatalf("ordinary command mutated: got %q, want %q", executor.Command, test.command)
			}
		})
	}
}

func TestScheduledActionCommandRejectsExplicitNonUnattendedModes(t *testing.T) {
	tests := []struct {
		name, executorID, command, kind string
	}{
		{"Codex separate approval", "codex", "codex --ask-for-approval on-request", ""},
		{"Codex alias approval", "codex", "codex -a untrusted", ""},
		{"Codex attached approval", "codex", "codex -aon-request -sdanger-full-access", ""},
		{"Codex equals sandbox", "codex", "codex --sandbox=workspace-write", ""},
		{"Codex attached sandbox", "codex", "codex -anever -sworkspace-write", ""},
		{"Codex quoted alias sandbox", "codex", `codex "-s" "read-only"`, ""},
		{"Codex shell chain", "codex", "codex && other --dangerously-bypass-approvals-and-sandbox", ""},
		{"Codex quoted substitution", "codex", `codex "$(other --dangerously-bypass-approvals-and-sandbox)"`, ""},
		{"Codex argv terminator", "codex", "codex -- --dangerously-bypass-approvals-and-sandbox", ""},
		{"Claude permission", "claude", "claude --permission-mode dontAsk", ""},
		{"Claude quoted equals", "claude", `claude "--permission-mode=dontAsk"`, ""},
		{"Cursor auto review", "agent", "cursor-agent --auto-review --sandbox disabled", "cursor"},
		{"Cursor equals plan", "agent", "cursor-agent --mode=plan", "cursor"},
		{"Cursor quoted ask", "agent", `cursor-agent "--mode" "ask"`, "cursor"},
		{"Cursor restricted sandbox", "agent", "cursor-agent --sandbox=enabled", "cursor"},
		{"Grok permission", "grok", "grok --permission-mode default", ""},
		{"Grok quoted sandbox", "grok", `grok "--sandbox=strict"`, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ScheduledActionCommand(test.executorID, Executor{Name: test.executorID, Command: test.command, Kind: test.kind})
			if !errors.Is(err, ErrScheduledActionUnattended) {
				t.Fatalf("error = %v, want ErrScheduledActionUnattended", err)
			}
		})
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

func TestProfileClientExecutorSeparatesAliasFromCanonicalClient(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{name: "codex alias id", in: []string{"primary", "codex --flag"}, want: AgentProviderCodex},
		{name: "codex kind", in: []string{"codex", "env X=1 -- mybin"}, want: AgentProviderCodex},
		{name: "claude wrapper", in: []string{"desk", "claude --permission-mode bypassPermissions"}, want: AgentProviderClaude},
		{name: "custom raw", in: []string{"custom", "my-custom-agent"}, want: ""},
		{name: "cursor unsupported", in: []string{"cursor-agent --force"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProfileClientExecutor(tc.in...); got != tc.want {
				t.Fatalf("ProfileClientExecutor(%v)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
	ae := NewAgentExecutor("primary", Executor{Name: "primary", Command: "codex", Kind: "codex"})
	if ae.ID != "primary" || ae.ProfileClientExecutor() != AgentProviderCodex {
		t.Fatalf("AgentExecutor=%+v hint=%q", ae, ae.ProfileClientExecutor())
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
