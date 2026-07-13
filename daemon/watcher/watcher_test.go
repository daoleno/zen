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

func TestActivityProbe_SkipTranscriptWhenPaneDecides(t *testing.T) {
	probe := &countingTranscriptProbe{}
	adapter := classifier.NewCursorActivityAdapterWithTranscript(probe)
	w := New(time.Second)
	w.SetActivityProbe(classifier.NewActivityProbe(adapter))
	agent := classifier.Agent{ID: "a:@1", Command: "cursor-agent", Cwd: "/tmp"}
	got := w.activitySignal(agent, "Cursor Agent\nctrl+c to stop\n", 0, nil)
	if got.State != classifier.StateRunning {
		t.Fatalf("got %#v", got)
	}
	if probe.calls != 0 {
		t.Fatalf("transcript probe calls = %d, want 0 when pane stop marker decides", probe.calls)
	}
}

func TestActivityProbe_UsesInjectedCursorTranscript(t *testing.T) {
	active := true
	probe := &countingTranscriptProbe{active: &active, ok: true}
	adapter := classifier.NewCursorActivityAdapterWithTranscript(probe)
	w := New(time.Second)
	w.SetActivityProbe(classifier.NewActivityProbe(adapter))
	agent := classifier.Agent{ID: "a:@1", Command: "cursor-agent", Cwd: "/tmp"}
	got := w.activitySignal(agent, "Cursor Agent\n→ Add a follow-up\n", 0, nil)
	if got.State != classifier.StateRunning || got.Source != "cursor_transcript_active" {
		t.Fatalf("got %#v", got)
	}
	if probe.calls != 1 {
		t.Fatalf("transcript probe calls = %d, want 1", probe.calls)
	}
}

type countingTranscriptProbe struct {
	calls  int
	active *bool
	ok     bool
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

func (p *countingTranscriptProbe) Active(agent classifier.Agent) (bool, bool) {
	p.calls++
	if p.active == nil {
		return false, p.ok
	}
	return *p.active, p.ok
}
