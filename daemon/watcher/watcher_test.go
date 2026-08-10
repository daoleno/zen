package watcher

import (
	"errors"
	"os"
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

func TestResolveOwnedGenerationDeprojectsOwnershipLossBeforeRejecting(t *testing.T) {
	w := New(time.Second)
	sessionID := "brain-agent-ownership-loss:@1"
	identity := testSessionInputIdentity("codex")
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Delegated: true, State: classifier.StateRunning,
		Command: "codex", Attention: "none",
	}
	w.agentOrder = append(w.agentOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(identity)
	input := newFakeSessionInputIO()
	input.paneValue.generation = "pane-current"
	w.sessionInput = newSessionInputOwner(input)

	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: sessionID + ":turn:1", Status: TurnRunning,
		ProcessIdentity: "different-process-generation", PaneGeneration: "pane-current",
	})
	w.SetTurnLedger(ledger)

	if _, err := w.ResolveOwnedGeneration(sessionID); err == nil {
		t.Fatal("mismatched live generation was authorized")
	}
	turn := ledger.snapshot(sessionID)
	if turn.Status != TurnUnknown || turn.ControlState != TurnControlOwnershipLost {
		t.Fatalf("ownership loss was not durably reduced before rejection: %+v", turn)
	}
	projected := w.GetAgent(sessionID)
	if projected == nil || projected.State != classifier.StateUnknown ||
		projected.Attention != "ownership_lost" || !projected.NeedsAttention {
		t.Fatalf("ownership loss was not synchronously deprojected: agent=%+v", projected)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].Kind != "ownership_lost" ||
		ledger.applied[0].Class != EvidenceLiveness {
		t.Fatalf("ownership resolver emitted facts = %+v", ledger.applied)
	}
	if _, err := w.ResolveOwnedGeneration(sessionID); err == nil {
		t.Fatal("named ownership-loss state silently reauthorized a replacement generation")
	}
	if len(ledger.applied) != 1 {
		t.Fatalf("repeated rejection emitted another ownership-loss fact: %+v", ledger.applied)
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

func TestResolveTargetIdentityWaitsForUnknownExpectedExecutable(t *testing.T) {
	shell := targetProcessIdentity{
		Command:         "zsh",
		PanePID:         10,
		PaneStart:       100,
		ForegroundID:    10,
		ForegroundStart: 100,
		ProcessID:       10,
		ProcessStart:    100,
	}
	futureAgent := targetProcessIdentity{
		Command:         "future-agent",
		PanePID:         10,
		PaneStart:       100,
		ForegroundID:    20,
		ForegroundStart: 200,
		ProcessID:       20,
		ProcessStart:    200,
	}
	calls := 0
	got, ok := resolveTargetIdentityWhenReady(
		func(string) (targetProcessIdentity, bool) {
			calls++
			if calls <= 2 {
				return shell, true
			}
			return futureAgent, true
		},
		"session:@1",
		"future-agent --accept-all",
	)
	if !ok || !got.equal(futureAgent) {
		t.Fatalf("resolved identity = (%+v, %v), want future-agent generation", got, ok)
	}
	if calls < 4 {
		t.Fatalf("resolver calls = %d, accepted transient shell before future-agent stabilized", calls)
	}
}

func TestResolveTargetIdentityAcceptsStableUnknownWrapperExecutable(t *testing.T) {
	shell := targetProcessIdentity{
		Command:         "zsh",
		PanePID:         10,
		PaneStart:       100,
		ForegroundID:    10,
		ForegroundStart: 100,
		ProcessID:       10,
		ProcessStart:    100,
	}
	nodeWrapper := targetProcessIdentity{
		Command:         "node",
		PanePID:         10,
		PaneStart:       100,
		ForegroundID:    30,
		ForegroundStart: 300,
		ProcessID:       30,
		ProcessStart:    300,
	}
	calls := 0
	got, ok := resolveTargetIdentityWhenReady(
		func(string) (targetProcessIdentity, bool) {
			calls++
			if calls <= 2 {
				return shell, true
			}
			return nodeWrapper, true
		},
		"session:@2",
		"future-agent --accept-all",
	)
	if !ok || !got.equal(nodeWrapper) {
		t.Fatalf("resolved identity = (%+v, %v), want stable non-shell wrapper generation", got, ok)
	}
	if calls < 4 {
		t.Fatalf("resolver calls = %d, accepted transient shell before wrapper stabilized", calls)
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

func TestUpdateAgentProgressRequiresExactDelegatedSignalIdentity(t *testing.T) {
	w := New(time.Second)
	const sessionID = "brain-agent-signal:@1"
	const turnID = "turn:signal-current"
	w.registerCreatedSession(sessionID, "/repo/zen", CreateSessionOptions{
		Command: "codex", Name: "Signal", Delegated: true,
	}, time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	<-w.Events()
	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: turnID, Status: TurnAccepted,
		AcceptedAt: time.Now().UTC().Add(-time.Second), SignalProtocol: true,
	})
	w.turnLedger = ledger

	for _, test := range []struct {
		name   string
		turnID string
		want   string
	}{
		{name: "missing", want: "turn identity is required"},
		{name: "mismatched", turnID: "turn:previous", want: "does not match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
				TurnID: test.turnID, Status: "done", Phase: "reporting", Attention: "done",
				Summary: "must not apply", ProgressEventID: "rejected-" + test.name,
			}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("identity rejection error = %v", err)
			}
		})
	}
	if agent := w.GetAgent(sessionID); agent.State == classifier.StateDone || agent.Summary == "must not apply" {
		t.Fatalf("rejected signal mutated Session projection: %+v", agent)
	}
	if turn, _, _ := ledger.Turn(sessionID); turn.Status != TurnAccepted {
		t.Fatalf("rejected signal mutated canonical Turn: %+v", turn)
	}

	agent, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
		TurnID: turnID, Status: "done", Phase: "reporting", Attention: "done",
		Summary: "REVIEW_READY", ProgressEventID: "matching-done",
	})
	if err != nil || agent.State != classifier.StateDone || agent.Summary != "REVIEW_READY" {
		t.Fatalf("matching signal projection = %+v err=%v", agent, err)
	}
	<-w.Events()
}

func TestUpdateAgentProgressMapsMatchingUserInputAttentionToBlockedTurn(t *testing.T) {
	w := New(time.Second)
	const sessionID = "brain-agent-user-input:@1"
	const turnID = "turn:user-input"
	w.registerCreatedSession(sessionID, "/repo/zen", CreateSessionOptions{
		Command: "pi", Name: "User input", Delegated: true,
	}, time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	<-w.Events()
	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: turnID, Status: TurnRunning,
		AcceptedAt: time.Now().UTC().Add(-time.Second), SignalProtocol: true,
	})
	w.turnLedger = ledger
	agent, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
		TurnID: turnID, Status: "running", Phase: "working", Attention: "user_input",
		Summary: "Need exact input", ProgressEventID: "matching-user-input",
	})
	if err != nil || agent.State != classifier.StateBlocked || agent.Attention != "user_input" || agent.Summary != "Need exact input" {
		t.Fatalf("user_input projection = %+v err=%v", agent, err)
	}
	turn, _, _ := ledger.Turn(sessionID)
	if turn.Status != TurnBlocked || turn.Attention != "user_input" {
		t.Fatalf("user_input canonical Turn = %+v", turn)
	}
	<-w.Events()
}

func TestRebindDelegatedTurnProjectionClearsOlderStickyFailure(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	<-w.Events()

	// This pre-contract fixture has no prompt-carried identity. Its unscoped
	// failed progress cannot change canonical status, so the Admitted turn keeps
	// the Session projection running.
	ledger := newFakeTurnLedger()
	acceptedAt := time.Now().UTC()
	if err := ledger.AdmitTurn(AdmittedTurn{
		SessionID:  "brain-agent-worker:@1",
		TurnID:     "brain-agent-worker:@1:turn:1",
		AcceptedAt: acceptedAt,
	}); err != nil {
		t.Fatal(err)
	}
	w.turnLedger = ledger

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
	// The unscoped pre-contract report leaves the canonical Admitted Turn and
	// Session projection running, with no sticky failure metadata.
	if failed.State != classifier.StateRunning || failed.Attention != "none" ||
		failed.NeedsAttention || failed.LastProgressAt != nil ||
		failed.TaskClass != "" || failed.EventKind != "" {
		t.Fatalf("failed hint polluted the canonical projection: %#v", failed)
	}

	accepted, err := w.RebindDelegatedTurnProjection("brain-agent-worker:@1")
	if err != nil {
		t.Fatalf("RebindDelegatedTurnProjection returned error: %v", err)
	}
	if accepted.State != classifier.StateRunning || accepted.Attention != "none" || accepted.NeedsAttention {
		t.Fatalf("rebound projection = %#v", accepted)
	}
	if accepted.LastProgressAt != nil || accepted.ExpectedNextCheckAt != nil || accepted.LeaseSeconds != 0 {
		t.Fatalf("rebound projection retained sticky lifecycle progress = %#v", accepted)
	}
	if accepted.TaskClass != "" || accepted.EventKind != "" || accepted.DetailsJSON != "" {
		t.Fatalf("rebound projection retained failure metadata = %#v", accepted)
	}

	select {
	case ev := <-w.Events():
		// The failed hint never flips the canonical projection; the rebind
		// emits the same nonterminal metadata event.
		if ev.Type != "agent_metadata_change" && ev.Type != "agent_state_change" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for rebound projection event")
	}
}

func TestRebindDelegatedTurnProjectionBypassesRecentTurnCache(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 8, 9, 4, 24, 0, 0, time.UTC))
	<-w.Events()

	ledger := newFakeTurnLedger()
	readAt := time.Now().UTC()
	ledger.seed("brain-agent-worker:@1", TurnSnapshot{
		SessionID: "brain-agent-worker:@1", TurnID: "cached-old", Status: TurnDone,
		AcceptedAt: readAt.Add(-time.Minute),
	})
	w.turnLedger = ledger
	if turn, found, err := w.ledgerTurnFor("brain-agent-worker:@1", readAt); err != nil || !found || turn.TurnID != "cached-old" {
		t.Fatalf("seed cache = (%+v, %v, %v)", turn, found, err)
	}
	ledger.seed("brain-agent-worker:@1", TurnSnapshot{
		SessionID: "brain-agent-worker:@1", TurnID: "authoritative-new", Status: TurnAccepted,
		AcceptedAt: readAt.Add(time.Second),
	})

	rebound, err := w.RebindDelegatedTurnProjection("brain-agent-worker:@1")
	if err != nil {
		t.Fatal(err)
	}
	if rebound.State != classifier.StateRunning ||
		w.ledgerTurns["brain-agent-worker:@1"].TurnID != "authoritative-new" {
		t.Fatalf("rebind reused stale cache: agent=%+v cached=%+v", rebound, w.ledgerTurns["brain-agent-worker:@1"])
	}
}

func TestFirstHeartbeatAfterFreshResolveBypassesSupersededTurnCache(t *testing.T) {
	w := New(time.Second)
	const sessionID = "brain-agent-worker:@heartbeat"
	w.registerCreatedSession(sessionID, "/repo/zen", CreateSessionOptions{
		Command: "codex", Name: "Worker",
	}, time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC))
	<-w.Events()

	ledger := newFakeTurnLedger()
	readAt := time.Now().UTC()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: "old-turn", Status: TurnRunning,
		AcceptedAt: readAt.Add(-time.Minute), ActivityID: "old-activity",
	})
	w.turnLedger = ledger
	if cached, found, err := w.ledgerTurnFor(sessionID, readAt); err != nil || !found || cached.TurnID != "old-turn" {
		t.Fatalf("seed old cache = (%+v, %v, %v)", cached, found, err)
	}
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: "fresh-turn", Status: TurnAccepted,
		AcceptedAt: readAt.Add(time.Second), ActivityID: "fresh-activity",
	})

	agent, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
		Status: "running", Phase: "working", Attention: "none",
		Summary: "first fresh heartbeat", ProgressEventID: "heartbeat-fresh-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Events()
	if agent.State != classifier.StateRunning || w.ledgerTurns[sessionID].TurnID != "fresh-turn" {
		t.Fatalf("first heartbeat used superseded cache: agent=%+v cache=%+v", agent, w.ledgerTurns[sessionID])
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.applied) == 0 || ledger.applied[len(ledger.applied)-1].TurnID != "fresh-turn" {
		t.Fatalf("heartbeat fact targeted wrong Turn: %+v", ledger.applied)
	}
}

func TestPreContractTurnProjectionIgnoresUnscopedControlDoneMetadata(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC))
	<-w.Events()

	ledger := newFakeTurnLedger()
	acceptedAt := time.Now().UTC()
	if err := ledger.AdmitTurn(AdmittedTurn{
		SessionID:  "brain-agent-worker:@1",
		TurnID:     "brain-agent-worker:@1:turn:1",
		AcceptedAt: acceptedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.ApplyTurnFact(TurnFact{
		SessionID: "brain-agent-worker:@1",
		TurnID:    "brain-agent-worker:@1:turn:1",
		Class:     EvidenceReceipt,
		Kind:      "admission",
		SourceID:  "receipt\x00brain-agent-worker:@1:turn:1\x00accepted\x00payload",
		Admission: TurnAdmission{Stream: "test", ID: "admission-1", Cursor: 1},
	}); err != nil {
		t.Fatal(err)
	}
	w.turnLedger = ledger

	agent, err := w.UpdateAgentProgress("brain-agent-worker:@1", classifier.AgentProgress{
		Status:       "done",
		Phase:        "reporting",
		Attention:    "done",
		Summary:      "Provider result is ready",
		TaskClass:    "lasting_design",
		EventKind:    "done",
		DetailsJSON:  `{"verification":"complete"}`,
		LeaseSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Events()
	if agent.State != classifier.StateRunning || agent.Attention != "none" || agent.NeedsAttention ||
		agent.Phase != "" || agent.TaskClass != "" || agent.EventKind != "" || agent.DetailsJSON != "" ||
		agent.LastProgressAt != nil || agent.ExpectedNextCheckAt != nil || agent.LeaseSeconds != 0 {
		t.Fatalf("Control done metadata contradicted canonical running turn: %#v", agent)
	}
}

func TestRebindDelegatedTurnProjectionDoesNotOverwriteNewerLifecycleProgress(t *testing.T) {
	w := New(time.Second)
	w.registerCreatedSession("brain-agent-worker:@1", "/repo/zen", CreateSessionOptions{
		Command: "codex",
		Name:    "Worker",
	}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	<-w.Events()
	handoffStartedAt := time.Now().UTC()

	ledger := newFakeTurnLedger()
	if err := ledger.AdmitTurn(AdmittedTurn{
		SessionID:  "brain-agent-worker:@1",
		TurnID:     "brain-agent-worker:@1:turn:1",
		AcceptedAt: handoffStartedAt,
	}); err != nil {
		t.Fatal(err)
	}
	// The turn is accepted (correlated admission) before the progress arrives.
	if _, _, err := ledger.ApplyTurnFact(TurnFact{
		SessionID: "brain-agent-worker:@1",
		TurnID:    "brain-agent-worker:@1:turn:1",
		Class:     EvidenceReceipt,
		Kind:      "admission",
		SourceID:  "receipt\x00brain-agent-worker:@1:turn:1\x00accepted\x00payload",
		Admission: TurnAdmission{Stream: "test", ID: "admission-1", Cursor: 1},
	}); err != nil {
		t.Fatal(err)
	}
	w.turnLedger = ledger

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
	// Control running refreshes the canonical summary from Accepted.
	if progress.Summary != "Running focused tests" {
		t.Fatalf("control summary did not refresh canonical projection: %#v", progress)
	}

	// Rebind after an accepted dispatch projects canonical status; progress
	// lease metadata survives and the canonical summary is preserved.
	accepted, err := w.RebindDelegatedTurnProjection("brain-agent-worker:@1")
	if err != nil {
		t.Fatalf("RebindDelegatedTurnProjection returned error: %v", err)
	}
	if accepted.Summary != "Running focused tests" || accepted.Phase != "verifying" || accepted.EventKind != "verification" {
		t.Fatalf("newer lifecycle progress was overwritten: %#v", accepted)
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

func TestCodexWorkspaceTrustPromptMatchesRequestedWorkingDirectory(t *testing.T) {
	trust := "> You are in /workspace/future\n\n" +
		"  Do you trust the contents of this directory? Working with untrusted contents\n" +
		"  comes with higher risk of prompt injection.\n\n" +
		"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n"
	if !isCodexWorkspaceTrustPrompt("codex --dangerously-bypass-approvals-and-sandbox", trust, "/workspace/future") {
		t.Fatal("unambiguous Codex trust prompt for requested cwd was not detected")
	}
	wrapped := strings.Replace(trust, "/workspace/future", "/workspace/fu\n  ture", 1)
	if !isCodexWorkspaceTrustPrompt("codex", wrapped, "/workspace/future") {
		t.Fatal("hard-wrapped Codex trust path was not reconstructed exactly")
	}
	siblingPrefix := strings.Replace(trust, "/workspace/future", "/workspace/fut", 1)
	if isCodexWorkspaceTrustPrompt("codex", siblingPrefix, "/workspace/future") {
		t.Fatal("Codex trust prompt authorized a sibling path by plain prefix")
	}
	if isCodexWorkspaceTrustPrompt("codex", trust, "/workspace/other") {
		t.Fatal("Codex trust prompt for a different cwd was accepted")
	}
	if isCodexWorkspaceTrustPrompt("future-agent", trust, "/workspace/future") {
		t.Fatal("unknown provider inherited Codex-specific trust handling")
	}
}

func TestCodexWorkspaceTrustPromptAdvancesOnce(t *testing.T) {
	trust := "> You are in /workspace/future\n\n" +
		"  Do you trust the contents of this directory?\n\n" +
		"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n"
	sends := 0
	send := func(key string) error {
		sends++
		if key != "Enter" {
			t.Fatalf("trust key = %q, want Enter", key)
		}
		return nil
	}
	advanced, didAdvance, ok := advanceStartupTrustPromptOnce(
		false,
		"codex",
		trust,
		"/workspace/future",
		func() error { return nil },
		send,
	)
	if !ok || !advanced || !didAdvance || sends != 1 {
		t.Fatalf("first advance = (%v, %v, %v), sends=%d", advanced, didAdvance, ok, sends)
	}
	advanced, didAdvance, ok = advanceStartupTrustPromptOnce(
		advanced,
		"codex",
		trust,
		"/workspace/future",
		func() error { return nil },
		send,
	)
	if !ok || !advanced || didAdvance || sends != 1 {
		t.Fatalf("duplicate advance = (%v, %v, %v), sends=%d", advanced, didAdvance, ok, sends)
	}
}

func TestCodexWorkspaceTrustPromptIdentityChangePreventsAdvance(t *testing.T) {
	trust := "> You are in /workspace/future\n\n" +
		"  Do you trust the contents of this directory?\n\n" +
		"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n"
	sends := 0
	advanced, didAdvance, ok := advanceStartupTrustPromptOnce(
		false,
		"codex",
		trust,
		"/workspace/future",
		func() error { return errors.New("provider generation changed") },
		func(string) error {
			sends++
			return nil
		},
	)
	if ok || advanced || didAdvance || sends != 0 {
		t.Fatalf("identity change advanced trust = (%v, %v, %v), sends=%d", advanced, didAdvance, ok, sends)
	}
}

func TestCodexStartupReadyIgnoresConsumedTrustPromptInScrollback(t *testing.T) {
	content := "> You are in /workspace/future\n\n" +
		"  Do you trust the contents of this directory?\n\n" +
		"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n\n" +
		"│ >_ OpenAI Codex (v0.146.0) │\n" +
		"│ model: gpt-5.6-sol │\n\n" +
		"› Run /review on my current changes\n"
	if !isCodexStartupReady(content) {
		t.Fatal("consumed Codex trust prompt in scrollback blocked the current composer")
	}
	trustOnly := "> You are in /workspace/future\n\n" +
		"  Do you trust the contents of this directory?\n\n" +
		"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n"
	if isCodexStartupReady(trustOnly) {
		t.Fatal("Codex trust prompt without a later ready UI was treated as ready")
	}
}

func TestCodexStartupReadyRecognizesCurrentHeaderlessComposer(t *testing.T) {
	content := strings.Repeat("older completed output that pushed the banner away\n", 90) +
		"\n› Ask Codex to work on something\n" +
		"  gpt-5.6-sol xhigh · 87% left · ~/workspace/zen\n"
	if !isCodexStartupReady(content) {
		t.Fatal("current visible Codex composer/footer should be ready without a historical banner")
	}
}

func TestCodexStartupReadyHeaderlessComposerStillFailsClosedOnBlockingScreens(t *testing.T) {
	idleFooter := "  gpt-5.6-sol xhigh · 87% left · ~/workspace/zen\n"
	for _, test := range []struct {
		name    string
		content string
	}{
		{
			name: "model selection",
			content: idleFooter + "\nSelect a model\n" +
				"› 1. gpt-5.6-sol\n  2. gpt-5.4\n",
		},
		{
			name: "workspace trust",
			content: idleFooter + "\nDo you trust the contents of this directory?\n" +
				"› 1. Yes, continue\n  2. No, quit\n\nPress enter to continue\n",
		},
		{
			name:    "startup",
			content: idleFooter + "\nStarting Codex\n› loading provider state\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if isCodexStartupReady(test.content) {
				t.Fatalf("blocking %s screen was treated as input-ready", test.name)
			}
		})
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
