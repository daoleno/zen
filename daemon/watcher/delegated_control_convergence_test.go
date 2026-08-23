package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestSubmitDelegatedInputReusesCompletedSessionWithDifferentIdleActivity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Date(2026, 8, 11, 3, 20, 0, 0, time.UTC)
	sessionID := "brain-agent-completed-reuse:@1"
	oldActivityID := "activity-prior-canonical"
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: sessionID + ":turn:1", Status: TurnDone,
		AcceptedAt: now.Add(-time.Hour), ActivityID: oldActivityID,
	})
	payload := "continue with the focused correction"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		{
			ID: "activity-current-idle", Status: "completed",
			StartedAt: now.Add(-time.Minute), SettledAt: now.Add(-time.Second),
			Structured: true,
		},
		{
			ID: "activity-new-follow-up", Status: "running", StartedAt: now.Add(time.Second),
			AdmissionStream: "codex-rollout", AdmissionID: "user-follow-up", AdmissionCursor: 9,
			AdmissionAt: now.Add(time.Second), InputSHA256: digest, Structured: true,
		},
	}}
	w := lifecycleTestWatcher(io, ledger, probe)
	w.sessionInput.now = func() time.Time { return now }
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "codex", Cwd: "/repo/zen", PaneAlive: true,
		Delegated: true, State: classifier.StateDone,
	}

	turnID := sessionID + ":turn:2"
	result, err := w.SubmitDelegatedInput(sessionID, payload, turnID, now)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turnID {
		t.Fatalf("production follow-up = (%+v, %v), want accepted", result, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("provider mutation count=%d, want one", len(io.queues))
	}
	current := ledger.snapshot(sessionID)
	if current.TurnID != turnID || current.Status != TurnAccepted ||
		current.ActivityID != "activity-new-follow-up" {
		t.Fatalf("canonical follow-up = %+v", current)
	}
	for _, fact := range ledger.applied {
		if fact.TurnID == sessionID+":turn:1" && fact.ActivityID == "activity-current-idle" {
			t.Fatalf("unrelated idle activity was adopted into the prior turn: %+v", fact)
		}
	}
}

func TestSubmitDelegatedInputActivityMismatchPreservesControlOwner(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Date(2026, 8, 11, 3, 30, 0, 0, time.UTC)
	sessionID := "brain-agent-activity-mismatch:@1"
	identity := testSessionInputIdentity("codex")
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: sessionID + ":turn:1", Status: TurnRunning,
		AcceptedAt: now.Add(-time.Minute), ActivityID: "activity-canonical",
		ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: io.paneValue.generation,
	})
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{{
		ID: "activity-unowned-running", Status: "running", StartedAt: now.Add(-time.Second),
		Structured: true,
	}}}
	w := New(time.Second)
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "codex", Cwd: "/repo/zen", PaneAlive: true,
		Delegated: true, State: classifier.StateRunning, Attention: "none",
	}
	w.agentOrder = append(w.agentOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(identity)
	w.providerActivityProbe = probe
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	w.sessionInput = owner
	w.turnLedger = ledger
	w.admissionTimeout = func(string) time.Duration { return 0 }

	result, err := w.SubmitDelegatedInput(
		sessionID, "must not mutate the unowned activity", sessionID+":turn:2", now,
	)
	if err == nil || !errors.Is(err, ErrDelegatedInputAdmissionUnavailable) || result.Outcome != InputNotSubmitted {
		t.Fatalf("activity mismatch = (%+v, %v), want definite rejection", result, err)
	}
	if len(io.queues) != 0 {
		t.Fatalf("activity mismatch crossed provider mutation boundary: queues=%d", len(io.queues))
	}
	turn := ledger.snapshot(sessionID)
	if turn.Status != TurnRunning || turn.ControlState != TurnControlOwned {
		t.Fatalf("admission conflict changed durable control owner: %+v", turn)
	}
	projected := w.GetAgent(sessionID)
	if projected == nil || projected.State != classifier.StateRunning ||
		projected.Attention == "ownership_lost" || projected.NeedsAttention {
		t.Fatalf("admission conflict deprojected the live target: %+v", projected)
	}
	if len(ledger.applied) != 0 {
		t.Fatalf("admission conflict emitted lifecycle facts=%+v", ledger.applied)
	}
}

func TestSubmitDelegatedInputActivityMismatchPreservesCompletedOutcome(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Date(2026, 8, 11, 3, 35, 0, 0, time.UTC)
	sessionID := "brain-agent-completed-activity-mismatch:@1"
	identity := testSessionInputIdentity("codex")
	settledAt := now.Add(-time.Minute)
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: sessionID + ":turn:1", Status: TurnDone,
		AcceptedAt: now.Add(-time.Hour), SettledAt: &settledAt, ActivityID: "activity-canonical",
		ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: io.paneValue.generation,
	})
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{{
		ID: "activity-unowned-running", Status: "running", StartedAt: now.Add(-time.Second),
		Structured: true,
	}}}
	w := New(time.Second)
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "codex", Cwd: "/repo/zen", PaneAlive: true,
		Delegated: true, State: classifier.StateDone, Attention: "none",
	}
	w.agentOrder = append(w.agentOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(identity)
	w.providerActivityProbe = probe
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	w.sessionInput = owner
	w.turnLedger = ledger
	w.admissionTimeout = func(string) time.Duration { return 0 }

	result, err := w.SubmitDelegatedInput(
		sessionID, "must not steer unrelated running work", sessionID+":turn:2", now,
	)
	if err == nil || result.Outcome != InputNotSubmitted || len(io.queues) != 0 {
		t.Fatalf("completed mismatch = (%+v, %v), queues=%d", result, err, len(io.queues))
	}
	turn := ledger.snapshot(sessionID)
	if turn.Status != TurnDone || turn.ControlState != TurnControlOwned {
		t.Fatalf("admission conflict changed completed control state: %+v", turn)
	}
	projected := w.GetAgent(sessionID)
	if projected == nil || projected.State != classifier.StateDone ||
		projected.Attention == "ownership_lost" || projected.NeedsAttention {
		t.Fatalf("completed admission conflict deprojected the target: %+v", projected)
	}
}

func TestResolveDelegatedControlIsReadOnlyAcrossProviderActivityChanges(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Date(2026, 8, 11, 3, 37, 0, 0, time.UTC)
	sessionID := "brain-agent-control-surface:@1"
	identity := testSessionInputIdentity("codex")
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: sessionID + ":turn:1", Status: TurnRunning,
		AcceptedAt: now.Add(-time.Hour), ActivityID: "activity-canonical",
		// The canonical Turn intentionally retains an older process identity;
		// read-only inspection must not turn that observation into lifecycle loss.
		ProcessIdentity: "canonical-old-process", PaneGeneration: io.paneValue.generation,
	})
	// A live provider activity with a different ID is deliberately supplied to
	// prove that the read-only resolver does not consume or reconcile it.
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{{
		ID: "activity-unowned-live", Status: "running",
	}}}
	w := New(time.Second)
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "codex", Cwd: "/repo/zen", PaneAlive: true,
		Delegated: true, State: classifier.StateRunning, Attention: "none",
	}
	w.agentOrder = append(w.agentOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(identity)
	w.providerActivityProbe = probe
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	w.sessionInput = owner
	w.turnLedger = ledger

	if _, err := w.ResolveDelegatedControl(sessionID); err != nil {
		t.Fatalf("ResolveDelegatedControl err=%v, want read-only target proof", err)
	}
	turn := ledger.snapshot(sessionID)
	if turn.ControlState != TurnControlOwned || turn.Status != TurnRunning {
		t.Fatalf("read-only control changed canonical turn: %+v", turn)
	}
	agent := w.GetAgent(sessionID)
	if agent == nil || agent.State != classifier.StateRunning || agent.Attention == "ownership_lost" {
		t.Fatalf("read-only control changed agent projection: %+v", agent)
	}
	probe.mu.Lock()
	consumed := probe.stepIdx
	probe.mu.Unlock()
	if consumed != 0 {
		t.Fatalf("read-only control consumed provider activity observation: %d", consumed)
	}
}

func agentStateForTurnStatus(status TurnStatus) classifier.AgentState {
	if status == TurnDone {
		return classifier.StateDone
	}
	return classifier.StateRunning
}
