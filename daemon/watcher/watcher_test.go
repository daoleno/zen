package watcher

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildWindowCommandForShellStartsInteractiveLoginShell(t *testing.T) {
	got := buildWindowCommandForShell("/bin/zsh", "")
	want := "exec '/bin/zsh' -i -l"
	if got != want {
		t.Fatalf("buildWindowCommandForShell() = %q, want %q", got, want)
	}
}

func TestBuildWindowCommandForShellWrapsCommandInInteractiveLoginShell(t *testing.T) {
	got := buildWindowCommandForShell("/bin/zsh", "codex --dangerously-bypass-approvals-and-sandbox")
	want := "exec '/bin/zsh' -i -l -c 'codex --dangerously-bypass-approvals-and-sandbox'"
	if got != want {
		t.Fatalf("buildWindowCommandForShell(command) = %q, want %q", got, want)
	}
}

func TestTmuxWindowEnvironmentPreservesUsefulEnvAndSkipsTmuxManagedKeys(t *testing.T) {
	got := tmuxWindowEnvironment([]string{
		"OPENAI_API_KEY=test-key",
		"PATH=/usr/local/bin:/usr/bin",
		"TMUX=/tmp/tmux-1000/default,123,0",
		"TMUX_PANE=%1",
		"PWD=/tmp/demo",
		"TERM=screen-256color",
		"LANG=en_US.UTF-8",
	})
	want := []string{
		"LANG=en_US.UTF-8",
		"OPENAI_API_KEY=test-key",
		"PATH=/usr/local/bin:/usr/bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tmuxWindowEnvironment() = %v, want %v", got, want)
	}
}

func TestBaseSessionNameHandlesStableWindowIDs(t *testing.T) {
	got := baseSessionName("main:@3198")
	if got != "main" {
		t.Fatalf("baseSessionName() = %q, want %q", got, "main")
	}
}

func TestFormatAgentNamePrefersWindowNameAndKeepsTargetSuffix(t *testing.T) {
	got := formatAgentName("Implement issue titles", "main:@42")
	want := "Implement issue titles (main:@42)"
	if got != want {
		t.Fatalf("formatAgentName() = %q, want %q", got, want)
	}
}

func TestFormatAgentNameFallsBackToTargetWhenWindowNameMissing(t *testing.T) {
	got := formatAgentName("", "main:@42")
	want := "main:@42"
	if got != want {
		t.Fatalf("formatAgentName() = %q, want %q", got, want)
	}
}

func TestSplitTmuxInputTreatsTrailingNewlineAsSubmit(t *testing.T) {
	body, submit := splitTmuxInput("/status\n")
	if body != "/status" || !submit {
		t.Fatalf("splitTmuxInput() = (%q, %v), want /status submit", body, submit)
	}
}

func TestSplitTmuxInputPreservesInternalNewlines(t *testing.T) {
	body, submit := splitTmuxInput("line one\nline two\n")
	if body != "line one\nline two" || !submit {
		t.Fatalf("splitTmuxInput() = (%q, %v), want multiline body submit", body, submit)
	}
}

func TestSplitTmuxInputCanSendTextWithoutSubmit(t *testing.T) {
	body, submit := splitTmuxInput("draft")
	if body != "draft" || submit {
		t.Fatalf("splitTmuxInput() = (%q, %v), want draft without submit", body, submit)
	}
}

func TestDetectAgentProcessPrefersCodexChildStartTime(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	codexStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: codexStarted, comm: "codex", args: "codex"},
	}

	command, startedAt := detectAgentProcess("codex", 10, processes, codexStarted.Add(5*time.Second))
	if command != "codex" || !startedAt.Equal(codexStarted) {
		t.Fatalf("detectAgentProcess() = (%q, %s), want codex child start %s", command, startedAt, codexStarted)
	}
}

func TestDetectAgentProcessUsesFallbackForCodexWithoutProcessMatch(t *testing.T) {
	fallbackAt := time.Date(2026, 5, 21, 8, 30, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: fallbackAt.Add(-2 * time.Hour), comm: "zsh", args: "zsh"},
	}

	command, startedAt := detectAgentProcess("codex", 10, processes, fallbackAt)
	if command != "codex" || !startedAt.Equal(fallbackAt) {
		t.Fatalf("detectAgentProcess() = (%q, %s), want codex fallback %s", command, startedAt, fallbackAt)
	}
}
