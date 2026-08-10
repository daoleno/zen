package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// openCodeLedgerFlow drives one canonical OpenCode turn through the single
// reducer: durable admission, correlated receipt, live provider activity, and
// a bound provider terminal. It mirrors the production watcher/adapter path.
func openCodeLedgerFlow(t *testing.T, store *Store, workID, sessionID, turnID string, acceptedAt time.Time, terminal watcher.TurnFact) {
	t.Helper()
	bootstrapAdmittedTurnFixture(t, store, workID, watcher.AdmittedTurn{
		SessionID:       sessionID,
		TurnID:          turnID,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "opencode-proc-" + turnID,
		PaneGeneration:  "pane-" + turnID,
		PayloadSHA256:   "payload-" + turnID,
	})
	admission := providerAdmission("opencode\x00db\x00"+sessionID, "msg-"+turnID, 1, "sha-"+turnID, acceptedAt)
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceReceipt,
		Kind:       "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-" + turnID,
		Admission:  admission,
		ActivityID: "activity-" + turnID,
		At:         acceptedAt.Add(time.Second),
		Summary:    "Delegated input accepted",
	}); err != nil || !changed {
		t.Fatalf("admission apply = (%v, %v)", changed, err)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "running",
		SourceID:   "provider\x00" + sessionID + "\x00opencode\x00msg-" + turnID + "\x001",
		Cursor:     1,
		Admission:  admission,
		ActivityID: "activity-" + turnID,
		StartedAt:  acceptedAt.Add(2 * time.Second),
		At:         acceptedAt.Add(3 * time.Second),
		Summary:    "Delegated provider activity running",
	}); err != nil || !changed {
		t.Fatalf("provider running apply = (%v, %v)", changed, err)
	}
	terminal.SessionID = sessionID
	terminal.TurnID = turnID
	terminal.Admission = admission
	terminal.ActivityID = "activity-" + turnID
	terminal.Cursor = 1
	if terminal.SourceID == "" {
		terminal.SourceID = "provider\x00" + sessionID + "\x00opencode\x00msg-" + turnID + "\x001"
	}
	if _, changed, err := store.ApplyTurnFact(terminal); err != nil || !changed {
		t.Fatalf("provider terminal apply = (%v, %v)", changed, err)
	}
}

// TestAmbiguousOpenCodeAdmissionNeverTerminalizesAndCompletionIsExactlyOnce
// covers the live incidents: an ambiguous admission fact can never emit
// session.failed while the provider works; the correlated turn completes with
// exactly one actionable session.done; replay is a no-op.
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
	acceptedAt := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID:  sessionID,
		TurnID:     turnID,
		AcceptedAt: acceptedAt,
	})

	// 1. The ambiguous admission attempt is a control failed self-report on
	// an Admitted turn: denied outright (C.2.3) — never failed, never a row.
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "failed",
		SourceID: "control\x00attempt-1",
		At:       acceptedAt.Add(time.Second),
		Summary:  "Delegated input outcome stayed ambiguous; provider start was not observed",
	})
	if err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("ambiguous control failed = (%+v, %v, %v), want denied", snapshot, changed, err)
	}

	// 2-4. Correlated admission, live provider activity, bound terminal.
	openCodeLedgerFlow(t, store, item.ID, sessionID, turnID, acceptedAt, watcher.TurnFact{
		Class:     watcher.EvidenceProvider,
		Kind:      "done",
		SettledAt: acceptedAt.Add(30 * time.Second),
		Summary:   "Delegated provider completed the turn",
	})

	// 5. Duplicate completion for the same turn is idempotently suppressed.
	admission := providerAdmission("opencode\x00db\x00"+sessionID, "msg-"+turnID, 1, "sha-"+turnID, acceptedAt)
	replayed, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "done",
		SourceID:   "provider\x00" + sessionID + "\x00opencode\x00msg-" + turnID + "\x001",
		Cursor:     1,
		Admission:  admission,
		ActivityID: "activity-" + turnID,
		SettledAt:  acceptedAt.Add(30 * time.Second),
		Summary:    "Delegated provider completed the turn",
	})
	if err != nil || changed || replayed.Status != watcher.TurnDone {
		t.Fatalf("replayed terminal = (%+v, %v, %v), want no-op", replayed, changed, err)
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
// verifies that a terminal fact for an older accepted turn cannot block the
// authoritative completion of a confirmed follow-up turn: each canonical turn
// is its own immutable lifecycle boundary with its own wake.
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
	base := time.Date(2026, 8, 9, 7, 0, 0, 0, time.UTC)

	// An older accepted turn fails authoritatively (bound provider terminal).
	openCodeLedgerFlow(t, store, item.ID, sessionID, sessionID+":turn:old", base, watcher.TurnFact{
		Class: watcher.EvidenceProvider, Kind: "failed",
		SettledAt: base.Add(time.Minute),
		Summary:   "Delegated provider failed the turn",
	})
	// The confirmed follow-up establishes a new activity epoch and completes.
	openCodeLedgerFlow(t, store, item.ID, sessionID, sessionID+":turn:new", base.Add(2*time.Minute), watcher.TurnFact{
		Class: watcher.EvidenceProvider, Kind: "done",
		SettledAt: base.Add(3 * time.Minute),
		Summary:   "Delegated provider completed the turn",
	})
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	failed := 0
	for _, recorded := range events {
		if recorded.Kind == "session.done" && recorded.Actionable {
			done++
		}
		if recorded.Kind == "session.failed" && recorded.Actionable {
			failed++
		}
	}
	if done != 1 || failed != 1 {
		t.Fatalf("epoch Events done=%d failed=%d, want exactly one each: %#v", done, failed, events)
	}
}

// TestFollowUpToDoneSessionReopensTurnAndNotifiesExactlyOnce verifies the
// reusable-Session contract: a follow-up submitted after the previous turn
// settled done establishes a new turn epoch, its authoritative completion
// emits exactly one new actionable session.done, and replayed terminal facts
// never add a duplicate or lose the notification.
func TestFollowUpToDoneSessionReopensTurnAndNotifiesExactlyOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-zen-opencode-followup-done:@1"
	item, err := store.CreateWork(Work{
		Title:            "Follow-up reopen",
		Objective:        "A follow-up turn after done must notify exactly once.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)
	openCodeLedgerFlow(t, store, item.ID, sessionID, sessionID+":turn:1", base, watcher.TurnFact{
		Class: watcher.EvidenceProvider, Kind: "done",
		SettledAt: base.Add(time.Minute),
		Summary:   "Delegated provider completed the turn",
	})
	openCodeLedgerFlow(t, store, item.ID, sessionID, sessionID+":turn:2", base.Add(2*time.Minute), watcher.TurnFact{
		Class: watcher.EvidenceProvider, Kind: "done",
		SettledAt: base.Add(3 * time.Minute),
		Summary:   "Delegated provider completed the turn",
	})

	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	actionableDone := 0
	for _, recorded := range events {
		if recorded.Kind == "session.done" {
			done++
			if recorded.Actionable {
				actionableDone++
			}
		}
	}
	if done != 2 || actionableDone != 2 {
		t.Fatalf("session.done Events = %d (actionable %d), want exactly two actionable (one per turn): %#v", done, actionableDone, events)
	}
}
