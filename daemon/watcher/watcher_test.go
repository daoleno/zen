package watcher

import (
	"reflect"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
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

func TestCreatedSessionNameFallsBackToCommandExecutable(t *testing.T) {
	got := createdSessionName(CreateSessionOptions{
		Command: "/usr/local/bin/codex --model gpt-5",
	})
	if got != "codex" {
		t.Fatalf("createdSessionName() = %q, want codex", got)
	}
}

func TestRegisterCreatedSessionSeedsAgentSnapshotAndEvent(t *testing.T) {
	w := New(time.Second)
	startedAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	w.registerCreatedSession("main:@42", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Codex follow-up",
	}, startedAt)

	agent := w.GetAgent("main:@42")
	if agent == nil {
		t.Fatal("expected created session to be registered")
	}
	if agent.Name != "Codex follow-up (main:@42)" {
		t.Fatalf("agent name = %q", agent.Name)
	}
	if agent.Cwd != "/repo/zen" || agent.Project != "zen" || agent.Command != "codex" {
		t.Fatalf("agent metadata = cwd %q project %q command %q", agent.Cwd, agent.Project, agent.Command)
	}
	if agent.State != classifier.StateRunning || !agent.StartedAt.Equal(startedAt) {
		t.Fatalf("agent state/start = %q %s", agent.State, agent.StartedAt)
	}
	if _, ok := w.prevContent["main:@42"]; !ok {
		t.Fatal("expected initial content marker for first poll update")
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "agent_discovered" || ev.AgentID != "main:@42" || ev.Agent == nil {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for created session event")
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

func TestDetectAgentProcessPreservesCodexResumeIntent(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	codexStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: codexStarted, comm: "node", args: "node /home/user/.local/bin/codex --dangerously-bypass-approvals-and-sandbox resume"},
	}

	command, startedAt := detectAgentProcess("node", 10, processes, codexStarted.Add(5*time.Second))
	if command != "codex resume" || !startedAt.Equal(codexStarted) {
		t.Fatalf("detectAgentProcess() = (%q, %s), want codex resume child start %s", command, startedAt, codexStarted)
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
