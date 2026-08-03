package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestBuildWindowCommandForShellInjectsAgentProgressEnv(t *testing.T) {
	got := buildWindowCommandForShellWithOptions("/bin/zsh", "codex --dangerously-bypass-approvals-and-sandbox", true)
	for _, want := range []string{
		"ZEN_AGENT_ID",
		"ZEN_AGENT_PROGRESS_CMD",
		"tmux display-message",
		"codex --dangerously-bypass-approvals-and-sandbox",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress shell command missing %q:\n%s", want, got)
		}
	}
	// The injected assignment must not embed a space-separated command string
	// (that breaks under zsh, which does not word-split unquoted variables).
	if strings.Contains(got, `ZEN_AGENT_PROGRESS_CMD="zen agent progress"`) ||
		strings.Contains(got, "ZEN_AGENT_PROGRESS_CMD=zen agent progress") {
		t.Fatalf("ZEN_AGENT_PROGRESS_CMD must be a single executable token:\n%s", got)
	}
}

func TestZenExecutablePathPrefersCurrentExecutable(t *testing.T) {
	got := ZenExecutablePath()
	if got == "" {
		t.Fatal("ZenExecutablePath() returned empty string")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable() unavailable: %v", err)
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		t.Skip("os.Executable() returned empty path")
	}
	if got != exe {
		t.Fatalf("ZenExecutablePath() = %q, want current executable %q (must not fall back to a PATH lookup)", got, exe)
	}
}

func TestAgentProgressEnvScriptAssignsSingleToken(t *testing.T) {
	script := agentProgressEnvScript()
	if !strings.Contains(script, "ZEN_AGENT_PROGRESS_CMD=") {
		t.Fatalf("script missing ZEN_AGENT_PROGRESS_CMD assignment:\n%s", script)
	}
	// The assignment must not embed a space-separated command string.
	if strings.Contains(script, "zen agent progress") {
		t.Fatalf("script must not embed space-separated command:\n%s", script)
	}
	// The injected value must be the current executable's path (shell-quoted),
	// not a stale "zen" resolved via PATH.
	if exe, err := os.Executable(); err == nil {
		if exe = strings.TrimSpace(exe); exe != "" {
			if !strings.Contains(script, shellQuote(exe)) {
				t.Fatalf("script does not assign the current executable path %q:\n%s", exe, script)
			}
		}
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

func TestDetectAgentProcessRecognizesCursorAgent(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	gotCommand, gotStarted, gotPID := detectAgentProcess("zsh", 100, map[int]processInfo{
		100: {
			pid:       100,
			comm:      "zsh",
			args:      "zsh",
			startedAt: now.Add(-2 * time.Minute),
		},
		101: {
			pid:       101,
			ppid:      100,
			comm:      "cursor-agent",
			args:      "cursor-agent --force --sandbox disabled",
			startedAt: now.Add(-30 * time.Second),
		},
	}, now)
	if gotCommand != "cursor-agent" || gotPID != 101 || !gotStarted.Equal(now.Add(-30*time.Second)) {
		t.Fatalf("detectAgentProcess = %q %s %d", gotCommand, gotStarted, gotPID)
	}
}

func TestNewTmuxSessionNameUsesAgentPrefix(t *testing.T) {
	got := newTmuxSessionName(CreateSessionOptions{Name: "Brain Codex"})
	if !strings.HasPrefix(got, "brain-agent-brain-codex-") {
		t.Fatalf("newTmuxSessionName() = %q", got)
	}
}

func TestBuildNewSessionArgsCreatesDetachedSession(t *testing.T) {
	got := buildNewSessionArgs("brain-agent-codex-123", "/repo/zen", CreateSessionOptions{
		Name: "brain-codex",
	}, "exec '/bin/zsh' -i -l -c 'codex'")
	wantPrefix := []string{
		"new-session",
		"-d",
		"-P",
		"-F",
		"#{session_name}:#{window_id}",
		"-s",
		"brain-agent-codex-123",
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("buildNewSessionArgs() = %v", got)
	}
	if got[len(got)-1] != "exec '/bin/zsh' -i -l -c 'codex'" {
		t.Fatalf("last arg = %q", got[len(got)-1])
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
	if agent.State != classifier.StateUnknown || !agent.StartedAt.Equal(startedAt) {
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

func TestRegisterCreatedSessionMarksHiddenAgent(t *testing.T) {
	w := New(time.Second)
	startedAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	w.registerCreatedSession("main:@43", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Brain",
		Hidden:  true,
	}, startedAt)

	agent := w.GetAgent("main:@43")
	if agent == nil {
		t.Fatal("expected created session to be registered")
	}
	if !agent.Hidden || !w.hidden["main:@43"] {
		t.Fatalf("expected hidden agent, got agent hidden=%v registry=%v", agent.Hidden, w.hidden["main:@43"])
	}
	if agent.Delegated {
		t.Fatal("hidden Brain host should not be marked delegated")
	}
}

func TestRegisterCreatedSessionMarksVisibleBrainSpawnAsDelegated(t *testing.T) {
	w := New(time.Second)
	startedAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	w.registerCreatedSession("brain-agent-verify-123:@44", "/repo/zen", CreateSessionOptions{
		Command:   "codex",
		Name:      "Verify",
		Delegated: true,
	}, startedAt)

	agent := w.GetAgent("brain-agent-verify-123:@44")
	if agent == nil {
		t.Fatal("expected created session to be registered")
	}
	if !agent.Delegated {
		t.Fatal("visible brain-agent session should be marked delegated")
	}
}

func TestRegisterCreatedSessionDoesNotInferDelegatedFromName(t *testing.T) {
	w := New(time.Second)

	w.registerCreatedSession("brain-agent-user-owned:@44", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "User owned",
	}, time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC))

	agent := w.GetAgent("brain-agent-user-owned:@44")
	if agent == nil {
		t.Fatal("expected created session to be registered")
	}
	if agent.Delegated {
		t.Fatal("session name alone should not mark an agent delegated")
	}
}

func TestAllowedTmuxKeyIncludesCodexPickerShortcuts(t *testing.T) {
	for _, key := range []string{"1", "2", "3", "9", "y", "p", "A", "Enter", "Escape"} {
		if !allowedTmuxKey(key) {
			t.Fatalf("expected key %q to be allowed", key)
		}
	}
	if allowedTmuxKey("0") {
		t.Fatal("did not expect 0 to be allowed")
	}
}

func TestAgentMetadataChangedDetectsNameChange(t *testing.T) {
	agent := &classifier.Agent{
		Name:      "Codex (main:@42)",
		Project:   "zen",
		Cwd:       "/repo/zen",
		Command:   "codex",
		ProcessID: 123,
	}
	previous := agentMetadataSnapshotFor(agent)

	agent.Name = "Investigate rename sync (main:@42)"

	if !agentMetadataChanged(previous, agent) {
		t.Fatal("expected metadata change after agent name changed")
	}
}

func TestAgentMetadataChangedIgnoresStateOnlyChange(t *testing.T) {
	agent := &classifier.Agent{
		Name:      "Codex (main:@42)",
		Project:   "zen",
		Cwd:       "/repo/zen",
		Command:   "codex",
		State:     classifier.StateRunning,
		Summary:   "running",
		ProcessID: 123,
	}
	previous := agentMetadataSnapshotFor(agent)

	agent.State = classifier.StateBlocked
	agent.Summary = "waiting"
	agent.UpdatedAt = time.Now()

	if agentMetadataChanged(previous, agent) {
		t.Fatal("state-only updates should not count as metadata changes")
	}
}

func TestAgentMetadataChangedDetectsProgressAttentionChange(t *testing.T) {
	agent := &classifier.Agent{
		Name:           "Worker (brain-agent-worker:@1)",
		State:          classifier.StateRunning,
		Phase:          "working",
		Attention:      "none",
		LeaseSeconds:   300,
		NeedsAttention: false,
	}
	previous := agentMetadataSnapshotFor(agent)

	agent.Attention = "user_input"
	agent.NeedsAttention = true
	progressAt := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	agent.LastProgressAt = &progressAt

	if !agentMetadataChanged(previous, agent) {
		t.Fatal("expected progress attention change to count as metadata")
	}
}

func TestUpdateAgentProgressUpdatesAgentAndEmitsStateEvent(t *testing.T) {
	w := New(time.Second)
	startedAt := time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, startedAt)
	<-w.Events()

	agent, err := w.UpdateAgentProgress("brain-agent-worker:@1", classifier.AgentProgress{
		Status:       "done",
		Phase:        "reporting",
		Attention:    "done",
		Summary:      "Finished verification",
		TaskClass:    "mechanical_change",
		EventKind:    "verification",
		DetailsJSON:  `{"command":"go test ./..."}`,
		LeaseSeconds: 300,
	})
	if err != nil {
		t.Fatalf("UpdateAgentProgress returned error: %v", err)
	}
	if agent.State != classifier.StateDone || agent.Phase != "reporting" || agent.Attention != "done" {
		t.Fatalf("agent progress = %#v", agent)
	}
	if !agent.NeedsAttention || agent.LastProgressAt == nil || agent.ExpectedNextCheckAt == nil {
		t.Fatalf("agent progress metadata = %#v", agent)
	}
	if agent.TaskClass != "mechanical_change" || agent.EventKind != "verification" || agent.DetailsJSON == "" {
		t.Fatalf("agent semantic metadata = %#v", agent)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "agent_state_change" || ev.OldState != "unknown" || ev.NewState != "done" {
			t.Fatalf("event = %#v", ev)
		}
		if ev.Agent == nil || ev.Agent.Summary != "Finished verification" || ev.Agent.EventKind != "verification" {
			t.Fatalf("event agent = %#v", ev.Agent)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress event")
	}
}

func TestUpdateAgentProgressRejectsUnknownAgent(t *testing.T) {
	w := New(time.Second)
	if _, err := w.UpdateAgentProgress("missing:@1", classifier.AgentProgress{
		Status:    "running",
		Phase:     "working",
		Attention: "none",
	}); err == nil {
		t.Fatal("expected missing agent error")
	}
}

func TestSettleAgentInputAcceptedClearsOlderStickyFailure(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	<-w.Events()

	failed, err := w.UpdateAgentProgress("brain-agent-worker:@1", classifier.AgentProgress{
		Status:    "failed",
		Phase:     "starting",
		Attention: "failed",
		Summary:   "Initial delegated prompt was not submitted",
		TaskClass: "lasting_design",
		EventKind: "risk",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Events()
	if failed.LastProgressAt == nil {
		t.Fatal("failed progress did not record its timestamp")
	}

	accepted, err := w.SettleAgentInputAccepted(
		"brain-agent-worker:@1",
		failed.LastProgressAt.Add(time.Nanosecond),
		"working",
		"Delegated Codex input accepted",
	)
	if err != nil {
		t.Fatalf("SettleAgentInputAccepted returned error: %v", err)
	}
	if accepted.State != classifier.StateRunning || accepted.Attention != "none" || accepted.NeedsAttention {
		t.Fatalf("accepted handoff = %#v", accepted)
	}
	if accepted.LastProgressAt != nil || accepted.ExpectedNextCheckAt != nil || accepted.LeaseSeconds != 0 {
		t.Fatalf("accepted handoff retained sticky lifecycle progress = %#v", accepted)
	}
	if accepted.TaskClass != "" || accepted.EventKind != "" || accepted.DetailsJSON != "" {
		t.Fatalf("accepted handoff retained failure metadata = %#v", accepted)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "agent_state_change" || ev.OldState != "failed" || ev.NewState != "running" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for accepted handoff event")
	}
}

func TestSettleAgentInputAcceptedDoesNotOverwriteNewerLifecycleProgress(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	<-w.Events()
	handoffStartedAt := time.Now().UTC()

	progress, err := w.UpdateAgentProgress("brain-agent-worker:@1", classifier.AgentProgress{
		Status:       "running",
		Phase:        "verifying",
		Attention:    "none",
		Summary:      "Running focused tests",
		TaskClass:    "lasting_design",
		EventKind:    "verification",
		LeaseSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Events()
	if progress.LastProgressAt == nil || progress.LastProgressAt.Before(handoffStartedAt) {
		t.Fatalf("test progress timestamp = %#v, handoff start = %s", progress.LastProgressAt, handoffStartedAt)
	}

	accepted, err := w.SettleAgentInputAccepted(
		"brain-agent-worker:@1",
		handoffStartedAt,
		"starting",
		"Initial delegated prompt accepted by Codex",
	)
	if err != nil {
		t.Fatalf("SettleAgentInputAccepted returned error: %v", err)
	}
	if accepted.Summary != "Running focused tests" || accepted.Phase != "verifying" || accepted.EventKind != "verification" {
		t.Fatalf("newer lifecycle progress was overwritten: %#v", accepted)
	}
	select {
	case ev := <-w.Events():
		t.Fatalf("settlement emitted an event after newer progress won: %#v", ev)
	default:
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

func TestSendInputChunksLongText(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}

	session := fmt.Sprintf("zen-watcher-send-input-%d-%d", os.Getpid(), time.Now().UnixNano())
	outputPath := filepath.Join(t.TempDir(), "input.txt")
	text := strings.Repeat("long input ", 3000)
	want := text + "\r"

	command := fmt.Sprintf("stty raw -echo; dd of=%q bs=1 count=%d 2>/dev/null", outputPath, len(want))
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", session, command).CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	time.Sleep(100 * time.Millisecond)
	if resolved, known := resolveTargetProcessCommand(session); !known || isCodexCommand(resolved) {
		t.Fatalf("authoritative non-Codex target identity = %q, known=%v", resolved, known)
	}
	if err := New(time.Second).SendInput(session, text+"\n"); err != nil {
		t.Fatalf("SendInput returned error: %v", err)
	}

	got := readFileWithMinSize(t, outputPath, len(want), 3*time.Second)
	if got != want {
		t.Fatalf("tmux input mismatch: got %d bytes, want %d bytes", len(got), len(want))
	}
}

func TestSplitStringByMaxBytesKeepsUTF8RunesIntact(t *testing.T) {
	got := splitStringByMaxBytes("ab你cd好", 4)
	want := []string{"ab", "你c", "d好"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitStringByMaxBytes() = %#v, want %#v", got, want)
	}
	for _, chunk := range got {
		if !utf8.ValidString(chunk) {
			t.Fatalf("invalid UTF-8 chunk %q", chunk)
		}
		if len(chunk) > 4 {
			t.Fatalf("chunk %q has %d bytes, want <= 4", chunk, len(chunk))
		}
	}
}

func TestCodexInputReadyRequiresPrompt(t *testing.T) {
	starting := "╭────╮\n│ >_ OpenAI Codex │\n│ model: loading │\n╰────╯\n"
	if isAgentInputReady("codex", starting) {
		t.Fatal("Codex loading screen should not be input-ready")
	}

	loadingWithPrompt := starting + "\n› Find and fix a bug in @filename\n"
	if isAgentInputReady("codex", loadingWithPrompt) {
		t.Fatal("Codex prompt should not be input-ready while model is loading")
	}

	ready := "╭────╮\n│ >_ OpenAI Codex │\n│ model: gpt-5.5 xhigh │\n╰────╯\n\n› Find and fix a bug in @filename\n"
	if !isAgentInputReady("codex", ready) {
		t.Fatal("Codex prompt should be input-ready")
	}
}

func TestCodexStartupContinuePromptIsNotInputReady(t *testing.T) {
	continuePrompt := "╭────╮\n│ >_ OpenAI Codex │\n╰────╯\n\nPress enter to continue\n"
	if !isCodexStartupContinuePrompt("codex", continuePrompt) {
		t.Fatal("Codex startup continue prompt should be detected")
	}
	if isAgentInputReady("codex", continuePrompt) {
		t.Fatal("Codex startup continue prompt should not be treated as task input-ready")
	}
}

func TestCodexInputReadyIgnoresStaleLoadingInScrollback(t *testing.T) {
	content := "╭────╮\n│ >_ OpenAI Codex │\n│ model: loading │\n╰────╯\n\n› Improve documentation in @filename\n\n" +
		"╭────╮\n│ >_ OpenAI Codex │\n│ model: gpt-5.5 medium │\n╰────╯\n\n› Improve documentation in @filename\n"
	if !isAgentInputReady("codex", content) {
		t.Fatal("current Codex prompt should be ready even when scrollback contains an older loading screen")
	}
}

func TestCodexInputReadyAcceptsComposerDuringProviderOwnedMCPStartup(t *testing.T) {
	for _, status := range []string{
		"• Starting MCP servers (0/3): context7, playwright",
		"• Booting MCP server: codex_apps (16s • esc to interrupt)",
	} {
		content := "╭────╮\n│ >_ OpenAI Codex (v0.144.6) │\n│ model: gpt-5.6-sol medium │\n╰────╯\n" +
			status + "\n\n› Find and fix a bug in @filename\n"
		if !isAgentInputReady("codex", content) {
			t.Fatalf("provider composer should own readiness during optional MCP startup:\n%s", content)
		}
	}
}

func TestCodexInputReadyWithExplicitCommandAfterLongScrollbackWithoutHeader(t *testing.T) {
	content := strings.Repeat("completed delegated output line\n", 1100) +
		"\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"

	if !isAgentInputReady("codex", content) {
		t.Fatal("explicit Codex command should make a headerless idle composer ready")
	}
}

func TestCodexInputReadyWithExplicitCommandRejectsHeaderlessUnsafeStates(t *testing.T) {
	longScrollback := strings.Repeat("completed delegated output line\n", 1100)
	tests := []struct {
		name    string
		content string
	}{
		{name: "model loading", content: longScrollback +
			"\n│ model: loading │\n\n› Find and fix a bug in @filename\n"},
		{name: "startup continue", content: longScrollback +
			"\nDo you trust the contents of this directory?\n› 1. Yes, continue\n  Press enter to continue\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if isAgentInputReady("codex", test.content) {
				t.Fatalf("headerless Codex %s state should not be input-ready", test.name)
			}
		})
	}
}

func TestCodexContentInferenceStillRequiresIdentity(t *testing.T) {
	headerless := strings.Repeat("arbitrary terminal output\n", 100) + "\n› idle-looking text\n"
	if needsInputReadinessWait("", headerless) {
		t.Fatal("headerless arbitrary content should not be inferred as Codex")
	}
	if !needsInputReadinessWait("", "│ >_ OpenAI Codex │\n› Find and fix a bug in @filename\n") {
		t.Fatal("OpenAI Codex pane identity should still enable content-inferred readiness checks")
	}
}

func TestCursorAgentInputReadyRequiresComposerPrompt(t *testing.T) {
	starting := "Cursor Agent\nv2026.07.01-41b2de7\nTip: Use /mcp to connect Cursor to your tools and data sources.\n"
	if isAgentInputReady("cursor-agent --force --sandbox disabled", starting) {
		t.Fatal("Cursor Agent startup screen should not be input-ready")
	}

	ready := starting + "\n\nComposer 2.5 Fast           Run Everything\n~/workspace/zen · main\n"
	if !isAgentInputReady("cursor-agent --force --sandbox disabled", ready) {
		t.Fatal("Cursor Agent composer prompt should be input-ready")
	}
}

func TestCursorWorkspaceTrustPromptIsNotInputReady(t *testing.T) {
	trust := "╭────╮\n⚠ Workspace Trust Required\n\nCursor Agent can execute code and access files in this directory.\nDo you trust the contents of this directory?\n\n▶ [a] Trust this workspace\n  [q] Quit\n╰────╯\n"
	if !isCursorWorkspaceTrustPrompt("cursor-agent --force --sandbox disabled", trust) {
		t.Fatal("Cursor Agent workspace trust prompt should be detected")
	}
	if isAgentInputReady("cursor-agent --force --sandbox disabled", trust) {
		t.Fatal("Cursor Agent workspace trust prompt should not be treated as task input-ready")
	}
}

func TestCursorAgentInputReadyIgnoresStaleStartupInScrollback(t *testing.T) {
	content := "Cursor Agent\nv2026.07.01\nTip: loading\n\n" +
		"Cursor Agent\nv2026.07.01\n\nComposer 2.5 Fast           Run Everything\n~/workspace/zen · main\n"
	if !isAgentInputReady("cursor-agent --force --sandbox disabled", content) {
		t.Fatal("current Cursor Agent prompt should be ready even when scrollback contains older startup text")
	}
}

func TestCursorAgentCommandNeedsInputReadinessWait(t *testing.T) {
	if !needsInputReadinessWait("cursor-agent --force --sandbox disabled", "") {
		t.Fatal("Cursor Agent should wait for composer readiness")
	}
	if !isCursorAgentCommand("/home/me/bin/cursor-agent --force") {
		t.Fatal("absolute Cursor Agent path should be detected")
	}
}

func TestGrokInputReadyRequiresComposerAndChrome(t *testing.T) {
	starting := "Starting Grok...\nLoading model\n"
	if isAgentInputReady("grok", starting) {
		t.Fatal("Grok startup without composer should not be input-ready")
	}

	chromeOnly := "Grok 4.5 (high) · always-approve\nShift+Tab:mode  │  Ctrl+c:cancel\n"
	if isAgentInputReady("grok", chromeOnly) {
		t.Fatal("Grok chrome without composer prompt should not be input-ready")
	}

	ready := "" +
		"  ╭──────────────────────────────────────────────────────────────────────────╮\n" +
		"  │ ❯                                                                        │\n" +
		"  ╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯\n" +
		"\n" +
		"  Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+g:send to bg  │  Ctrl+x:shortcuts\n"
	if !isAgentInputReady("grok", ready) {
		t.Fatal("Grok composer + chrome should be input-ready")
	}

	// Legacy keybinding chrome used in older captures.
	legacyReady := "│ ❯\nEnter:send\nGrok 4.5\n"
	if !isAgentInputReady("grok", legacyReady) {
		t.Fatal("Grok Enter:send chrome with composer should be input-ready")
	}
}

func TestGrokCommandNeedsInputReadinessWait(t *testing.T) {
	if !needsInputReadinessWait("grok", "") {
		t.Fatal("Grok should wait for composer readiness")
	}
	if !isGrokCommand("/home/me/bin/grok --yolo") {
		t.Fatal("absolute Grok path should be detected")
	}
	if !needsInputReadinessWait("", "╰───── Grok 4.5 (high) · always-approve ─╯\n│ ❯") {
		t.Fatal("Grok pane content should trigger readiness wait even without command")
	}
}

func TestClaudeInputReadyRequiresAllThreeIndicators(t *testing.T) {
	const bypassFooter = "  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"
	const manualFooter = "⏸ manual mode on · ? for shortcuts · ← for agents"
	// Exact untouched ready pane from configured-Claude probe @224.
	readyLive := "" +
		" ▐▛███▜▌   Claude Code v2.1.214\n" +
		"▝▜█████▛▘  Haiku 4.5 · API Usage Billing\n" +
		"  ▘▘ ▝▝    ~/workspace/zen\n" +
		"\n\n" +
		"────────────────────────────────────────\n" +
		"❯\u00a0\n" +
		"────────────────────────────────────────\n" +
		bypassFooter

	// Startup screen: has version but not composer or mode footer.
	startup := "Claude Code v2.1.214\nLoading...\n"
	if isAgentInputReady("claude", startup) {
		t.Fatal("Claude startup without composer should not be input-ready")
	}

	// Safety screen: has version and footer but no empty composer line.
	safety := "Claude Code v2.1.214\nPermissions request\n" + bypassFooter + "\n"
	if isAgentInputReady("claude", safety) {
		t.Fatal("Claude safety screen without empty composer should not be input-ready")
	}

	// Nonempty draft: has version, composer glyph, and footer but composer is not empty.
	draft := "Claude Code v2.1.214\nSome text in the composer\n❯ more draft\n" + bypassFooter + "\n"
	if isAgentInputReady("claude", draft) {
		t.Fatal("Claude with nonempty draft should not be input-ready")
	}

	// NBSP after ❯ plus draft text is still a nonempty composer.
	draftNBSP := "Claude Code v2.1.214\n❯\u00a0typed draft\n" + bypassFooter + "\n"
	if isAgentInputReady("claude", draftNBSP) {
		t.Fatal("Claude with NBSP-padded nonempty draft should not be input-ready")
	}

	// Arbitrary Claude mention with ctrl should not match without all three.
	arbitrary := "Claude can ctrl+c to exit\nSome content\n"
	if isAgentInputReady("claude", arbitrary) {
		t.Fatal("Claude with arbitrary content and ctrl mention should not be input-ready")
	}

	if !isAgentInputReady("claude", readyLive) {
		t.Fatal("exact @224 Claude ready pane with NBSP composer should be input-ready")
	}

	// Ready state: version + empty NBSP composer + mode footer (manual mode).
	readyManual := "Claude Code v2.1.214\nMessages here\n\n❯\u00a0\n" + manualFooter + "\n"
	if !isAgentInputReady("claude", readyManual) {
		t.Fatal("Claude with empty NBSP composer and manual mode footer should be input-ready")
	}

	// Version number can vary and must not pin major version 2.
	readyV2150 := "Claude Code v2.1.50\n\n❯\u00a0\n" + manualFooter + "\n"
	if !isAgentInputReady("claude", readyV2150) {
		t.Fatal("Claude v2.1.50 with ready state should be input-ready")
	}
	readyV3 := "Claude Code v3.0.1\n\n❯\u00a0\n" + bypassFooter + "\n"
	if !isAgentInputReady("claude", readyV3) {
		t.Fatal("Claude v3.0.1 with NBSP empty composer should be input-ready")
	}
}

func TestClaudeCommandDetection(t *testing.T) {
	if !needsInputReadinessWait("claude", "") {
		t.Fatal("Claude command should require readiness wait")
	}
	if !isClaudeCommand("claude") {
		t.Fatal("bare claude should be detected")
	}
	if !isClaudeCommand("cc --profile test") {
		t.Fatal("claude alias cc should be detected")
	}
	if !isClaudeCommand("/usr/local/bin/claude --add-dir /tmp") {
		t.Fatal("absolute path claude should be detected")
	}
}

func TestProviderCommandDetectionDirectAndEnvWrapped(t *testing.T) {
	const zenPathWrap = "env PATH='/opt/zen/bin':$PATH"
	// Exact Host form from withZenCLIOnPath(shellQuote(dir)): quoted dir may
	// contain spaces, with :$PATH and other PATH entries appended outside quotes.
	const zenPathWrapSpaced = "env PATH='/Applications/Zen CLI/bin':$PATH:/home/daoleno/.local/bin:/usr/bin"
	tests := []struct {
		name    string
		command string
		codex   bool
		cursor  bool
		grok    bool
		claude  bool
	}{
		{name: "direct codex", command: "codex", codex: true},
		{name: "direct codex with flags", command: "codex --dangerously-bypass-approvals-and-sandbox", codex: true},
		{name: "env-wrapped codex", command: zenPathWrap + " codex --dangerously-bypass-approvals-and-sandbox", codex: true},
		{name: "env-wrapped absolute codex", command: zenPathWrap + " /usr/local/bin/codex", codex: true},
		{name: "env-wrapped spaced PATH codex", command: zenPathWrapSpaced + " codex --dangerously-bypass-approvals-and-sandbox", codex: true},

		{name: "direct cursor-agent", command: "cursor-agent --force --sandbox disabled", cursor: true},
		{name: "env-wrapped cursor-agent", command: zenPathWrap + " cursor-agent --force --sandbox disabled", cursor: true},
		{name: "env-wrapped absolute cursor-agent", command: zenPathWrap + " /home/me/bin/cursor-agent --force", cursor: true},
		{name: "env-wrapped spaced PATH cursor-agent", command: zenPathWrapSpaced + " cursor-agent --force --sandbox disabled", cursor: true},

		{name: "direct grok", command: "grok --no-alt-screen", grok: true},
		{name: "direct grok prefix", command: "grok-cli --yolo", grok: true},
		{name: "env-wrapped grok", command: zenPathWrap + " grok --no-alt-screen", grok: true},
		{name: "env-wrapped absolute grok", command: zenPathWrap + " /home/me/bin/grok --yolo", grok: true},
		{name: "env-wrapped spaced PATH grok", command: zenPathWrapSpaced + " grok --no-alt-screen", grok: true},

		{name: "direct claude", command: "claude --permission-mode bypassPermissions", claude: true},
		{name: "direct cc alias", command: "cc --profile test", claude: true},
		{name: "env-wrapped claude", command: zenPathWrap + " claude --permission-mode bypassPermissions", claude: true},
		{name: "env-wrapped absolute claude", command: zenPathWrap + " /usr/local/bin/claude --add-dir /tmp", claude: true},
		{name: "env-wrapped spaced PATH claude", command: zenPathWrapSpaced + " claude --permission-mode bypassPermissions", claude: true},
		{name: "env with dashdash then claude", command: "env PATH='/opt/zen/bin':$PATH -- claude", claude: true},
		{name: "env with multiple assignments", command: "env FOO=1 PATH='/opt/zen/bin':$PATH claude", claude: true},

		{name: "empty", command: ""},
		{name: "env without executable", command: "env PATH='/opt/zen/bin':$PATH"},
		{name: "env spaced PATH without executable", command: zenPathWrapSpaced},
		{name: "env only", command: "env"},
		{name: "custom executor", command: "/usr/local/bin/my-agent --flag"},
		{name: "custom mentions claude later", command: "my-agent --provider claude"},
		{name: "custom mentions codex later", command: "runner exec codex"},
		{name: "env-wrapped custom", command: zenPathWrap + " my-agent --flag"},
		{name: "env-wrapped spaced PATH custom", command: zenPathWrapSpaced + " my-agent --flag"},
		{name: "shell not provider", command: "zsh -c claude"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCodexCommand(tc.command); got != tc.codex {
				t.Fatalf("isCodexCommand(%q) = %v, want %v", tc.command, got, tc.codex)
			}
			if got := isCursorAgentCommand(tc.command); got != tc.cursor {
				t.Fatalf("isCursorAgentCommand(%q) = %v, want %v", tc.command, got, tc.cursor)
			}
			if got := isGrokCommand(tc.command); got != tc.grok {
				t.Fatalf("isGrokCommand(%q) = %v, want %v", tc.command, got, tc.grok)
			}
			if got := isClaudeCommand(tc.command); got != tc.claude {
				t.Fatalf("isClaudeCommand(%q) = %v, want %v", tc.command, got, tc.claude)
			}
		})
	}
}

func TestSendInputWhenReadyClaudeSendsLargeBodyExactlyOnce(t *testing.T) {
	binDir := t.TempDir()
	logDir := t.TempDir()
	readyPath := filepath.Join(logDir, "ready.txt")
	chunksPath := filepath.Join(logDir, "chunks.bin")
	literalCountPath := filepath.Join(logDir, "literal-count.txt")
	entersPath := filepath.Join(logDir, "enters.txt")
	ready := "" +
		" ▐▛███▜▌   Claude Code v2.1.214\n" +
		"▝▜█████▛▘  Haiku 4.5 · API Usage Billing\n" +
		"  ▘▘ ▝▝    ~/workspace/zen\n" +
		"\n\n" +
		"────────────────────────────────────────\n" +
		"❯\u00a0\n" +
		"────────────────────────────────────────\n" +
		"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"
	if err := os.WriteFile(readyPath, []byte(ready), 0o600); err != nil {
		t.Fatalf("write ready fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
ready=%q
chunks=%q
literal_count=%q
enters=%q
case "$1" in
  capture-pane)
    cat "$ready" || exit 1
    exit 0
    ;;
  list-panes)
    printf '0\n'
    exit 0
    ;;
  send-keys)
    shift
    literal=0
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -l)
          literal=1
          shift
          ;;
        -t)
          shift
          [ "$#" -gt 0 ] && shift
          ;;
        --)
          shift
          if [ "$literal" = 1 ]; then
            printf '%%s' "$*" >> "$chunks"
            printf '1\n' >> "$literal_count"
          fi
          exit 0
          ;;
        Enter)
          printf 'Enter\n' >> "$enters"
          exit 0
          ;;
        *)
          shift
          ;;
      esac
    done
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`, readyPath, chunksPath, literalCountPath, entersPath)
	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	targetCommandResolverMu.Lock()
	previousResolver := targetCommandResolver
	targetCommandResolver = func(target string) (string, bool) {
		return "claude --permission-mode bypassPermissions", target == "claude-ready:@1"
	}
	targetCommandResolverMu.Unlock()
	t.Cleanup(func() {
		targetCommandResolverMu.Lock()
		targetCommandResolver = previousResolver
		targetCommandResolverMu.Unlock()
	})

	body := strings.Repeat("zen-claude-brief-", 80)
	if len(body) <= tmuxSendInputChunkBytes {
		t.Fatalf("test body len=%d, want > %d", len(body), tmuxSendInputChunkBytes)
	}
	wantLiteralSends := (len(body) + tmuxSendInputChunkBytes - 1) / tmuxSendInputChunkBytes
	if wantLiteralSends < 2 {
		t.Fatalf("expected multi-chunk body, wantLiteralSends=%d", wantLiteralSends)
	}

	if err := SendInputWhenReady("claude-ready:@1", "env PATH='/Applications/Zen CLI/bin':$PATH:/usr/bin claude --permission-mode bypassPermissions", body+"\n"); err != nil {
		t.Fatalf("SendInputWhenReady: %v", err)
	}

	gotChunks, err := os.ReadFile(chunksPath)
	if err != nil {
		t.Fatalf("read chunks: %v", err)
	}
	if string(gotChunks) != body {
		t.Fatalf("literal chunks = %q (%d bytes), want body exactly once (%d bytes)", gotChunks, len(gotChunks), len(body))
	}

	gotLiteralCount, err := os.ReadFile(literalCountPath)
	if err != nil {
		t.Fatalf("read literal count: %v", err)
	}
	literalSends := len(strings.Split(strings.TrimSpace(string(gotLiteralCount)), "\n"))
	if literalSends != wantLiteralSends {
		t.Fatalf("literal send-keys -l count = %d, want %d", literalSends, wantLiteralSends)
	}

	gotEnters, err := os.ReadFile(entersPath)
	if err != nil {
		t.Fatalf("read enters: %v", err)
	}
	enterLines := strings.Split(strings.TrimSpace(string(gotEnters)), "\n")
	if len(enterLines) != 1 || enterLines[0] != "Enter" {
		t.Fatalf("Enter sends = %q, want exactly one Enter", gotEnters)
	}
}

func TestCursorAgentUsesLongerSubmitDelay(t *testing.T) {
	if got := tmuxSubmitDelay("cursor-agent --force --sandbox disabled"); got < 350*time.Millisecond {
		t.Fatalf("Cursor Agent submit delay = %s, want at least 350ms", got)
	}
	if got := tmuxSubmitDelay("codex"); got != 120*time.Millisecond {
		t.Fatalf("Codex submit delay = %s", got)
	}
	if got := tmuxSubmitDelay("grok"); got < 250*time.Millisecond {
		t.Fatalf("Grok submit delay = %s, want at least 250ms", got)
	}
	if got := tmuxSubmitDelay("claude"); got < 200*time.Millisecond {
		t.Fatalf("Claude submit delay = %s, want at least 200ms", got)
	}
}

func TestGenericWatcherSendInputKeepsLegacyDelayWhileExplicitReadyKeepsProviderDelay(t *testing.T) {
	binDir := t.TempDir()
	tmuxPath := filepath.Join(binDir, "tmux")
	script := `#!/bin/sh
case "$1" in
  capture-pane)
    printf 'Claude Code v2.1.214\nCursor Agent\nrun everything\nGrok 1\n❯ \nbypass permissions on\nEnter: send\n'
    ;;
  list-panes)
    printf '0\n'
    ;;
esac
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	previousSleep := tmuxSubmitSleep
	var requested []time.Duration
	tmuxSubmitSleep = func(delay time.Duration) {
		requested = append(requested, delay)
	}
	targetCommandResolverMu.Lock()
	previousResolver := targetCommandResolver
	targetCommandResolverMu.Unlock()
	t.Cleanup(func() {
		tmuxSubmitSleep = previousSleep
		targetCommandResolverMu.Lock()
		targetCommandResolver = previousResolver
		targetCommandResolverMu.Unlock()
	})

	tests := []struct {
		name          string
		command       string
		explicitDelay time.Duration
	}{
		{name: "Claude", command: "claude --permission-mode bypassPermissions", explicitDelay: 250 * time.Millisecond},
		{name: "Cursor", command: "cursor-agent --force", explicitDelay: 400 * time.Millisecond},
		{name: "Grok", command: "grok --no-alt-screen", explicitDelay: 300 * time.Millisecond},
		{name: "custom", command: "my-agent --interactive", explicitDelay: 120 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "delay-" + strings.ToLower(tc.name) + ":@1"
			resolver := func(target string) (string, bool) {
				return tc.command, target == sessionID
			}
			w := New(time.Second)
			w.targetCommandResolver = resolver
			requested = nil
			if err := w.SendInput(sessionID, "heartbeat\n"); err != nil {
				t.Fatalf("generic SendInput: %v", err)
			}
			if !reflect.DeepEqual(requested, []time.Duration{120 * time.Millisecond}) {
				t.Fatalf("generic requested delays=%v want [120ms]", requested)
			}

			targetCommandResolverMu.Lock()
			targetCommandResolver = resolver
			targetCommandResolverMu.Unlock()
			requested = nil
			if err := SendInputWhenReady(sessionID, tc.command, "explicit ready\n"); err != nil {
				t.Fatalf("explicit-ready SendInput: %v", err)
			}
			if !reflect.DeepEqual(requested, []time.Duration{tc.explicitDelay}) {
				t.Fatalf("explicit-ready requested delays=%v want [%s]", requested, tc.explicitDelay)
			}
		})
	}
}

func TestUnknownCommandDoesNotWaitForInputReady(t *testing.T) {
	if !isAgentInputReady("zsh", "") {
		t.Fatal("unknown commands should be treated as immediately ready")
	}
}

func readFileWithMinSize(t *testing.T, path string, minSize int, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var data []byte
	for {
		current, err := os.ReadFile(path)
		if err == nil {
			data = current
			if len(data) >= minSize {
				return string(data)
			}
		}
		if time.Now().After(deadline) {
			if len(data) > 0 {
				return string(data)
			}
			t.Fatalf("timed out waiting for %s to reach %d bytes", path, minSize)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestChangedPaneLinesReturnsTailWhenContentRepaintsSameLine(t *testing.T) {
	got := changedPaneLines("› hello\nThinking", "› hello\nThinking longer")
	want := []string{"› hello", "Thinking longer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedPaneLines() = %#v, want %#v", got, want)
	}
}

func TestChangedPaneLinesReturnsOnlyAppendedLines(t *testing.T) {
	got := changedPaneLines("one\ntwo", "one\ntwo\nthree")
	want := []string{"three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedPaneLines() = %#v, want %#v", got, want)
	}
}

func TestDetectAgentProcessPrefersCodexChildStartTime(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	codexStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: codexStarted, comm: "codex", args: "codex"},
	}

	command, startedAt, pid := detectAgentProcess("codex", 10, processes, codexStarted.Add(5*time.Second))
	if command != "codex" || !startedAt.Equal(codexStarted) || pid != 20 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want codex child start %s pid 20", command, startedAt, pid, codexStarted)
	}
}

func TestForegroundCodexAuthorityRejectsHigherScoringBackgroundProvider(t *testing.T) {
	started := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, pgid: 20, tpgid: 20, startedAt: started.Add(time.Second), comm: "codex", args: "codex --no-alt-screen"},
		30: {pid: 30, ppid: 10, pgid: 30, tpgid: 20, startedAt: started.Add(2 * time.Second), comm: "grok", args: "grok --resume background-session"},
	}

	command, processStarted, pid, ok := foregroundTargetProcess(10, processes)
	if !ok || command != "codex" || pid != 20 || !processStarted.Equal(processes[20].startedAt) {
		t.Fatalf(
			"foreground authority = (%q, %s, %d, %t), want actual foreground Codex pid 20",
			command,
			processStarted,
			pid,
			ok,
		)
	}
}

func TestForegroundCodexWrapperAuthorityRejectsBackgroundNativeClaude(t *testing.T) {
	started := time.Date(2026, 8, 3, 12, 30, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {
			pid:       20,
			ppid:      10,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(time.Second),
			comm:      "node",
			args:      "node /home/user/.local/bin/codex --no-alt-screen",
		},
		30: {pid: 30, ppid: 10, pgid: 30, tpgid: 20, startedAt: started.Add(2 * time.Second), comm: "claude", args: "claude --dangerously-skip-permissions"},
	}

	command, processStarted, pid, ok := foregroundTargetProcess(10, processes)
	if !ok || command != "codex" || pid != 20 || !processStarted.Equal(processes[20].startedAt) {
		t.Fatalf(
			"foreground wrapper authority = (%q, %s, %d, %t), want actual foreground Codex wrapper pid 20",
			command,
			processStarted,
			pid,
			ok,
		)
	}
}

func TestForegroundProviderAuthorityRequiresPaneLineage(t *testing.T) {
	started := time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 40, startedAt: started, comm: "zsh", args: "zsh"},
		40: {pid: 40, ppid: 1, pgid: 40, tpgid: 40, startedAt: started.Add(time.Second), comm: "codex", args: "codex"},
	}
	if command, processStarted, pid, ok := foregroundTargetProcess(10, processes); ok {
		t.Fatalf(
			"unrelated foreground process authorized target = (%q, %s, %d)",
			command,
			processStarted,
			pid,
		)
	}
}

func TestForegroundProviderAuthorityFollowsOpaqueForegroundWrapper(t *testing.T) {
	started := time.Date(2026, 8, 3, 13, 30, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {
			pid:       20,
			ppid:      10,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(time.Second),
			comm:      "bash",
			args:      "bash /opt/provider-wrapper",
		},
		21: {
			pid:       21,
			ppid:      20,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(2 * time.Second),
			comm:      "codex",
			args:      "codex --no-alt-screen",
		},
		30: {
			pid:       30,
			ppid:      10,
			pgid:      30,
			tpgid:     20,
			startedAt: started.Add(3 * time.Second),
			comm:      "grok",
			args:      "grok --resume background-session",
		},
	}

	command, processStarted, pid, ok := foregroundTargetProcess(10, processes)
	if !ok || command != "codex" || pid != 21 || !processStarted.Equal(processes[21].startedAt) {
		t.Fatalf(
			"opaque foreground wrapper authority = (%q, %s, %d, %t), want Codex child pid 21",
			command,
			processStarted,
			pid,
			ok,
		)
	}
	authority, ok := resolveForegroundTargetProcess(10, processes)
	if !ok ||
		authority.foreground.pid != 20 ||
		!authority.foreground.startedAt.Equal(processes[20].startedAt) ||
		authority.provider.pid != 21 ||
		!authority.provider.startedAt.Equal(processes[21].startedAt) {
		t.Fatalf("opaque wrapper bound authority = %#v, ok=%t", authority, ok)
	}
}

func TestForegroundProviderAuthorityRejectsConflictingSameGroupProviders(t *testing.T) {
	started := time.Date(2026, 8, 3, 14, 0, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {
			pid:       20,
			ppid:      10,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(time.Second),
			comm:      "bash",
			args:      "bash /opt/provider-wrapper",
		},
		21: {
			pid:       21,
			ppid:      20,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(2 * time.Second),
			comm:      "codex",
			args:      "codex",
		},
		22: {
			pid:       22,
			ppid:      20,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(3 * time.Second),
			comm:      "claude",
			args:      "claude",
		},
	}
	if authority, ok := resolveForegroundTargetProcess(10, processes); ok {
		t.Fatalf("conflicting same-PGID Providers authorized target: %#v", authority)
	}
}

func TestForegroundProviderAuthorityRejectsIndeterminateSameFamilySiblings(t *testing.T) {
	started := time.Date(2026, 8, 3, 14, 15, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {
			pid:       20,
			ppid:      10,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(time.Second),
			comm:      "bash",
			args:      "bash /opt/provider-wrapper",
		},
		21: {
			pid:       21,
			ppid:      20,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(2 * time.Second),
			comm:      "node",
			args:      "node /opt/codex/bin/codex --no-alt-screen",
		},
		22: {
			pid:       22,
			ppid:      20,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(3 * time.Second),
			comm:      "codex",
			args:      "codex --no-alt-screen",
		},
	}
	if authority, ok := resolveForegroundTargetProcess(10, processes); ok {
		t.Fatalf("indeterminate same-family siblings authorized target: %#v", authority)
	}
}

func TestForegroundProviderAuthorityKeepsPlainShellGeneric(t *testing.T) {
	started := time.Date(2026, 8, 3, 14, 30, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, pgid: 10, tpgid: 20, startedAt: started, comm: "zsh", args: "zsh"},
		20: {
			pid:       20,
			ppid:      10,
			pgid:      20,
			tpgid:     20,
			startedAt: started.Add(time.Second),
			comm:      "bash",
			args:      "bash",
		},
	}
	authority, ok := resolveForegroundTargetProcess(10, processes)
	if !ok ||
		authority.command != "bash" ||
		authority.foreground.pid != 20 ||
		authority.provider.pid != 20 {
		t.Fatalf("plain foreground shell authority = %#v, ok=%t", authority, ok)
	}
}

func TestAgentCommandFromProcessRejectsShellLaunchHint(t *testing.T) {
	process := processInfo{
		pid:  10,
		ppid: 1,
		comm: "zsh",
		args: "zsh -lc codex --no-alt-screen",
	}
	if command := agentCommandFromProcess(process); command != "" {
		t.Fatalf("shell launch hint was treated as actual provider process: %q", command)
	}
}

func TestParseProcessSnapshotUsesStableAbsoluteStartTime(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	first := parseProcessSnapshot([]byte(
		"3887666 3887534 3887666 3887666 Tue Jul 21 00:45:09 2026 codex /opt/codex --sandbox\n",
	), location)
	second := parseProcessSnapshot([]byte(
		"3887666 3887534 3887666 3887666 Tue Jul 21 00:45:09 2026 codex /opt/codex --sandbox\n",
	), location)

	wantStartedAt := time.Date(2026, 7, 21, 0, 45, 9, 0, location)
	for name, snapshot := range map[string]map[int]processInfo{
		"first":  first,
		"second": second,
	} {
		process, ok := snapshot[3887666]
		if !ok {
			t.Fatalf("%s snapshot omitted process", name)
		}
		if !process.startedAt.Equal(wantStartedAt) {
			t.Fatalf("%s start = %s, want %s", name, process.startedAt, wantStartedAt)
		}
		if process.ppid != 3887534 ||
			process.pgid != 3887666 ||
			process.tpgid != 3887666 ||
			process.comm != "codex" ||
			process.args != "/opt/codex --sandbox" {
			t.Fatalf("%s process = %#v", name, process)
		}
	}
}

func TestDetectAgentProcessPreservesCodexResumeIntent(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	codexStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: codexStarted, comm: "node", args: "node /home/user/.local/bin/codex --dangerously-bypass-approvals-and-sandbox resume"},
	}

	command, startedAt, pid := detectAgentProcess("node", 10, processes, codexStarted.Add(5*time.Second))
	if command != "codex resume" || !startedAt.Equal(codexStarted) || pid != 20 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want codex resume child start %s pid 20", command, startedAt, pid, codexStarted)
	}
}

func TestDetectAgentProcessPreservesGrokResumeIntent(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	grokStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: grokStarted, comm: "grok", args: "grok --resume 019f2826-12b8-7cc3-a094-a57522b559e6"},
	}

	command, startedAt, pid := detectAgentProcess("grok", 10, processes, grokStarted.Add(5*time.Second))
	if command != "grok --resume 019f2826-12b8-7cc3-a094-a57522b559e6" || !startedAt.Equal(grokStarted) || pid != 20 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want grok resume session child start %s pid 20", command, startedAt, pid, grokStarted)
	}
}

func TestDetectAgentProcessPrefersGrokChildStartTime(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	grokStarted := shellStarted.Add(30 * time.Minute)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: grokStarted, comm: "grok", args: "grok --no-alt-screen"},
	}

	command, startedAt, pid := detectAgentProcess("grok", 10, processes, grokStarted.Add(5*time.Second))
	if command != "grok" || !startedAt.Equal(grokStarted) || pid != 20 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want grok child start %s pid 20", command, startedAt, pid, grokStarted)
	}
}

func TestDetectAgentProcessPrefersNativeCodexChildOverNodeWrapper(t *testing.T) {
	shellStarted := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	wrapperStarted := shellStarted.Add(30 * time.Minute)
	nativeStarted := wrapperStarted.Add(time.Second)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: shellStarted, comm: "zsh", args: "zsh"},
		20: {pid: 20, ppid: 10, startedAt: wrapperStarted, comm: "node", args: "node /home/user/.local/bin/codex --dangerously-bypass-approvals-and-sandbox"},
		30: {pid: 30, ppid: 20, startedAt: nativeStarted, comm: "codex", args: "/home/user/.local/share/codex/codex --dangerously-bypass-approvals-and-sandbox"},
	}

	command, startedAt, pid := detectAgentProcess("node", 10, processes, nativeStarted.Add(5*time.Second))
	if command != "codex" || !startedAt.Equal(nativeStarted) || pid != 30 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want native codex start %s pid 30", command, startedAt, pid, nativeStarted)
	}
}

func TestDetectAgentProcessUsesFallbackForCodexWithoutProcessMatch(t *testing.T) {
	fallbackAt := time.Date(2026, 5, 21, 8, 30, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, startedAt: fallbackAt.Add(-2 * time.Hour), comm: "zsh", args: "zsh"},
	}

	command, startedAt, pid := detectAgentProcess("codex", 10, processes, fallbackAt)
	if command != "codex" || !startedAt.Equal(fallbackAt) || pid != 10 {
		t.Fatalf("detectAgentProcess() = (%q, %s, %d), want codex fallback %s pid 10", command, startedAt, pid, fallbackAt)
	}
}

func TestCursorToolChildActive_IgnoresCodeModeHost(t *testing.T) {
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, comm: "cursor-agent", args: "node .../cursor-agent/versions/x/index.js"},
		20: {pid: 20, ppid: 10, comm: "node", args: "node .../code-mode-host"},
	}
	if cursorToolChildActive(10, processes) {
		t.Fatal("code-mode-host must not count as tool activity")
	}
}

func TestCursorToolChildActive_DetectsShellWorker(t *testing.T) {
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, comm: "cursor-agent", args: "node .../cursor-agent/versions/x/index.js"},
		20: {pid: 20, ppid: 10, comm: "npm", args: "npm exec @playwright/mcp@latest"},
		40: {pid: 40, ppid: 10, comm: "zsh", args: "zsh -c sandbox tool"},
	}
	if !cursorToolChildActive(10, processes) {
		t.Fatal("shell worker under cursor-agent should count as tool activity")
	}
}

func TestCursorActivitySignal_StopMarker(t *testing.T) {
	w := New(time.Second)
	w.SetActivityProbe(classifier.NewActivityProbe(classifier.NewCursorActivityAdapter()))
	agent := classifier.Agent{Command: "cursor-agent", Cwd: "/tmp"}
	got := w.activitySignal(agent, "Cursor Agent\n→ Add a follow-up\nctrl+c to stop\n", 0, nil)
	if got.State != classifier.StateRunning {
		t.Fatalf("got %#v", got)
	}
}

func TestActivityProbe_IdleProviderPanesWithoutProcessFactsStayUnknown(t *testing.T) {
	w := New(time.Second)
	w.SetActivityProbe(classifier.DefaultActivityProbe())

	tests := []struct {
		command string
		pane    string
	}{
		{command: "codex", pane: "OpenAI Codex\n› "},
		{command: "claude", pane: "Claude Code\n❯ "},
		{command: "cursor-agent", pane: "Cursor Agent\n→ Add a follow-up"},
		{command: "grok", pane: "Grok\n❯ "},
	}
	for _, testCase := range tests {
		agent := classifier.Agent{ID: testCase.command, Command: testCase.command, Cwd: "/tmp", PaneAlive: true, State: classifier.StateUnknown}
		signal := w.activitySignal(agent, testCase.pane, 0, nil)
		state, _ := classifier.ResolveSessionStatus(&agent, classifier.StateUnknown, "Session idle", time.Now().UTC(), signal)
		if state != classifier.StateUnknown {
			t.Fatalf("%s state = %q from signal %#v, want unknown", testCase.command, state, signal)
		}
	}
}

func TestAgentsPreserveFirstSeenOrderAcrossMutableUpdates(t *testing.T) {
	w := New(time.Second)
	w.agents["a"] = &classifier.Agent{ID: "a", Summary: "first"}
	w.agents["b"] = &classifier.Agent{ID: "b", Summary: "second"}
	w.agentOrder = []string{"a", "b"}

	w.agents["a"].UpdatedAt = time.Now().Add(time.Hour)
	w.agents["a"].Summary = "new activity"
	w.agents["b"].State = classifier.StateRunning

	got := w.Agents()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("Agents() order = %#v, want [a b]", got)
	}

	delete(w.agents, "a")
	w.compactAgentOrderLocked()
	w.agents["c"] = &classifier.Agent{ID: "c"}
	w.agentOrder = append(w.agentOrder, "c")
	got = w.Agents()
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("Agents() after remove/add = %#v, want [b c]", got)
	}
}

func TestClassifyPaneAndApplyProgressInvalidation(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-30 * time.Second)
	activeLease := now.Add(270 * time.Second)
	expiredLease := now.Add(-time.Minute)
	waitFalsePositive := []string{"waiting for BUILD SUCCESSFUL|BUILD FAILED|FAILURE:", "ctrl+c to stop"}
	genuineFail := []string{"FAILED: 3 tests failed"}

	tests := []struct {
		name        string
		agent       *classifier.Agent
		alive       bool
		lines       []string
		want        classifier.AgentState
		wantSummary string
		wantCleared bool
		checkSticky bool
	}{
		{
			name: "1 wait/search false-positive keeps lease",
			agent: &classifier.Agent{
				State: classifier.StateRunning, Summary: "Rebuilding Android APK",
				LastProgressAt: &progressAt, ExpectedNextCheckAt: &activeLease, LeaseSeconds: 300,
			},
			alive: true, lines: waitFalsePositive, want: classifier.StateRunning, wantSummary: "Rebuilding Android APK",
		},
		{
			name:  "2 genuine failed without progress",
			agent: &classifier.Agent{State: classifier.StateUnknown},
			alive: true, lines: genuineFail, want: classifier.StateFailed, wantCleared: true,
		},
		{
			name: "3 expired lease clears so failed cannot stick",
			agent: &classifier.Agent{
				State: classifier.StateRunning, Summary: "stale",
				LastProgressAt: &progressAt, ExpectedNextCheckAt: &expiredLease, LeaseSeconds: 300,
			},
			alive: true, lines: genuineFail, want: classifier.StateFailed, wantCleared: true, checkSticky: true,
		},
		{
			name:  "4 sticky done retained",
			agent: &classifier.Agent{State: classifier.StateDone, Summary: "Was done", LastProgressAt: &progressAt},
			alive: true, lines: genuineFail, want: classifier.StateDone, wantSummary: "Was done",
		},
		{
			name:  "4b sticky failed retained",
			agent: &classifier.Agent{State: classifier.StateFailed, Summary: "explicit failed", LastProgressAt: &progressAt},
			alive: true, lines: genuineFail, want: classifier.StateFailed, wantSummary: "explicit failed",
		},
		{
			name:  "4c sticky blocked retained",
			agent: &classifier.Agent{State: classifier.StateBlocked, Summary: "explicit blocked", LastProgressAt: &progressAt},
			alive: true, lines: genuineFail, want: classifier.StateBlocked, wantSummary: "explicit blocked",
		},
		{
			name: "5 blocked clears running progress",
			agent: &classifier.Agent{
				State: classifier.StateRunning, Summary: "working",
				LastProgressAt: &progressAt, ExpectedNextCheckAt: &activeLease, LeaseSeconds: 300,
			},
			alive: true, lines: []string{"Do you want to proceed? (Y/n)"}, want: classifier.StateBlocked, wantCleared: true,
		},
		{
			name: "6 dead pane failed clears lease",
			agent: &classifier.Agent{
				State: classifier.StateRunning, Summary: "claimed",
				LastProgressAt: &progressAt, ExpectedNextCheckAt: &activeLease, LeaseSeconds: 300,
			},
			alive: false, lines: genuineFail, want: classifier.StateFailed, wantCleared: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified, summary := classifyPaneAndApplyProgressInvalidation(tt.agent, tt.alive, tt.lines, now)
			got, gotSummary := classifier.ResolveSessionStatus(tt.agent, classified, summary, now, classifier.ActivitySignal{})
			if got != tt.want {
				t.Fatalf("state = %q, want %q (classified=%q)", got, tt.want, classified)
			}
			if tt.wantSummary != "" && gotSummary != tt.wantSummary {
				t.Fatalf("summary = %q, want %q", gotSummary, tt.wantSummary)
			}
			if cleared := tt.agent.LastProgressAt == nil; cleared != tt.wantCleared {
				t.Fatalf("cleared = %v, want %v", cleared, tt.wantCleared)
			}
			if tt.checkSticky {
				tt.agent.State = got
				nextClassified, nextSummary := classifyPaneAndApplyProgressInvalidation(tt.agent, true, []string{"$"}, now)
				next, _ := classifier.ResolveSessionStatus(tt.agent, nextClassified, nextSummary, now, classifier.ActivitySignal{})
				if next != classifier.StateUnknown {
					t.Fatalf("follow-up = %q, want unknown", next)
				}
			}
		})
	}
}
