package brain

import (
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// TestAmbiguousOpenCodeAdmissionNeverTerminalizesAndCompletionIsExactlyOnce
// reproduces the observed failure sequence: an ambiguous initial admission
// used to emit two actionable session.failed Events and pin Work, so the real
// provider completion later had no observable lifecycle transition. The
// corrected sequence must project only nonterminal facts until the
// authoritative turn settlement emits exactly one actionable session.done.
func TestAmbiguousOpenCodeAdmissionNeverTerminalizesAndCompletionIsExactlyOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-zen-opencode:@1"
	item, err := store.CreateWork(Work{
		Title:            "OpenCode completion",
		Objective:        "Emit one completion Event after an ambiguous admission.",
		Status:           WorkOpen,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Start the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	agent := func(state classifier.AgentState, attention string, needsAttention bool, turnID string) *classifier.Agent {
		return &classifier.Agent{
			ID:             sessionID,
			Name:           "OpenCode",
			State:          state,
			Attention:      attention,
			NeedsAttention: needsAttention,
			Delegated:      true,
			PaneAlive:      true,
			Summary:        "Delegated turn running",
		}
	}

	// 1. The ambiguous admission attempt is projected as a nonterminal
	// attempt fact (running, attention none): it must not emit session.failed.
	if woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: sessionID,
		Agent:   agent(classifier.StateRunning, "none", false, ""),
		TurnID:  "",
	}); err != nil {
		t.Fatal(err)
	} else if woke {
		t.Fatal("ambiguous attempt fact woke Brain")
	}

	// 2. Live provider activity projects running for the accepted turn.
	if woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    agent(classifier.StateRunning, "none", false, "turn-1"),
		OldState: string(classifier.StateUnknown),
		NewState: string(classifier.StateRunning),
		TurnID:   "turn-1",
	}); err != nil {
		t.Fatal(err)
	} else if woke {
		t.Fatal("running turn woke Brain")
	}

	// 3. Authoritative settlement: exactly one actionable completion Event.
	if _, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    agent(classifier.StateDone, "none", false, "turn-1"),
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		TurnID:   "turn-1",
	}); err != nil {
		t.Fatal(err)
	}

	// 4. Duplicate completion for the same turn is idempotently suppressed.
	if _, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    agent(classifier.StateDone, "none", false, "turn-1"),
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		TurnID:   "turn-1",
	}); err != nil {
		t.Fatal(err)
	}

	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	done := 0
	actionableDone := 0
	for _, recorded := range events {
		if recorded.Kind == "session.failed" {
			failed++
		}
		if recorded.Kind == "session.done" {
			done++
			if recorded.Actionable {
				actionableDone++
			}
		}
	}
	if failed != 0 {
		t.Fatalf("ambiguous admission emitted %d session.failed Events: %#v", failed, events)
	}
	if done != 1 || actionableDone != 1 {
		t.Fatalf("completion Events = done:%d actionable:%d, want exactly one actionable", done, actionableDone)
	}

	work, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if work.Status == WorkDone || work.Status == WorkCancelled {
		t.Fatalf("Work terminalized: %+v", work)
	}
}

// TestConfirmedFollowUpTurnEstablishesNewEpochAfterEarlierTurnFailure
// verifies that a turn-keyed terminal fact for an older accepted turn cannot
// block the authoritative completion of a confirmed follow-up turn: the
// follow-up establishes a new epoch and its session.done must still emit.
func TestConfirmedFollowUpTurnEstablishesNewEpochAfterEarlierTurnFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-zen-opencode-epoch:@2"
	item, err := store.CreateWork(Work{
		Title:            "Follow-up epoch",
		Objective:        "A later authoritative turn must still complete.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	event := func(state, oldState classifier.AgentState, turnID string) watcher.SessionEvent {
		return watcher.SessionEvent{
			Type:     "agent_state_change",
			AgentID:  sessionID,
			Agent:    &classifier.Agent{ID: sessionID, State: state, Delegated: true, PaneAlive: true, Summary: "summary"},
			OldState: string(oldState),
			NewState: string(state),
			TurnID:   turnID,
		}
	}

	// An older accepted turn fails authoritatively (e.g. bounded start timeout).
	if _, err := service.RouteSessionEvent(event(classifier.StateFailed, classifier.StateRunning, "turn-old")); err != nil {
		t.Fatal(err)
	}
	// The confirmed follow-up establishes a new activity epoch.
	if _, err := service.RouteSessionEvent(event(classifier.StateRunning, classifier.StateFailed, "turn-new")); err != nil {
		t.Fatal(err)
	}
	// Its authoritative completion must still emit one actionable Event.
	if _, err := service.RouteSessionEvent(event(classifier.StateDone, classifier.StateRunning, "turn-new")); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, recorded := range events {
		if recorded.Kind == "session.done" {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("follow-up completion Events = %d, want exactly one: %#v", done, events)
	}
}

// TestSessionEventDedupeKeyCollapsesSameKindAdmissionAttemptEvents verifies
// that repeated lifecycle Events of the same kind for one Session collapse to
// one durable Event when no turn identity separates them, and that a
// turn-keyed Event remains a distinct fact.
func TestSessionEventDedupeKeyCollapsesSameKindAdmissionAttemptEvents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-dedupe:@3"
	item, err := store.CreateWork(Work{
		Title:            "Dedupe",
		Objective:        "Same admission attempt must not duplicate Events.",
		Status:           WorkOpen,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	metadataEvent := func(attention string) watcher.SessionEvent {
		return watcher.SessionEvent{
			Type:    "agent_metadata_change",
			AgentID: sessionID,
			Agent: &classifier.Agent{
				ID: sessionID, State: classifier.StateFailed, Attention: attention,
				NeedsAttention: true, Delegated: true, PaneAlive: true,
			},
		}
	}
	for index := 0; index < 3; index++ {
		if _, err := service.RouteSessionEvent(metadataEvent("failed")); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, recorded := range events {
		if recorded.Kind == "session.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("same-kind admission Events = %d, want exactly one deduplicated: %#v", failed, events)
	}
}
