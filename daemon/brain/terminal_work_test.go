package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// TestTurnFactsNeverRegressTerminalWork is the red-green guard for the
// terminal-Work invariant (C.2.9): WorkDone and WorkCancelled are terminal
// scheduler decisions. Later Session/Turn facts — a delayed liveness
// uncertain after Session removal, a late lease stale, or a late bound
// Provider terminal — may advance the turn record and be retained as
// non-actionable audit, but they must never move the Work's
// status/next_action/wait_for and must never produce an actionable outbox row
// that could wake Brain.
func TestTurnFactsNeverRegressTerminalWork(t *testing.T) {
	t.Helper()
	admission := providerAdmission("stream", "msg-1", 1, "sha", time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC))

	assertTerminalWorkUnchanged := func(t *testing.T, store *Store, before, after Work) {
		t.Helper()
		if after.Status != before.Status ||
			after.NextAction != before.NextAction ||
			after.WaitFor != before.WaitFor {
			t.Fatalf("terminal Work regressed: before=%+v after=%+v", before, after)
		}
	}

	assertNoWake := func(t *testing.T, store *Store) {
		t.Helper()
		if _, claimed, err := store.ClaimNextActionableEvent("brain-agent-host-hidden:@1"); err != nil || claimed {
			t.Fatalf("terminal Work produced a claimable wake: claimed=%v err=%v", claimed, err)
		}
	}

	admitAcceptedTurn := func(t *testing.T, store *Store, sessionID, turnID string) {
		t.Helper()
		acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:      watcher.EvidenceReceipt,
			Kind:       "admission",
			SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
			Admission:  admission,
			ActivityID: "activity-1",
			At:         acceptedAt.Add(2 * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("done + late liveness uncertain stays done with audit only", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		admitAcceptedTurn(t, store, sessionID, turnID)
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		status := WorkDone
		completed, err := store.UpdateWork(workItem.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}

		// Session removed while the canonical turn is still running: the
		// watcher applies the end-of-identity liveness fact (C.2.4) after the
		// explicit completion.
		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:       watcher.EvidenceLiveness,
			Kind:        "uncertain",
			ProcessDead: true,
			SourceID:    "liveness\x00proc-identity-1\x00process-dead",
			SettledAt:   time.Date(2026, 8, 7, 10, 0, 20, 0, time.UTC),
			At:          time.Date(2026, 8, 7, 10, 0, 21, 0, time.UTC),
		})
		if err != nil || !changed || snapshot.Status != watcher.TurnUnknown {
			t.Fatalf("late liveness uncertain = (%+v, %v, %v), want turn audit Unknown", snapshot, changed, err)
		}
		after, err := store.Work(completed.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertTerminalWorkUnchanged(t, store, completed, after)
		row, found := turnEvent(t, store, completed.ID, "session:"+sessionID+":turn:"+turnID+":session.uncertain")
		if !found {
			t.Fatal("late uncertain row missing from outbox audit")
		}
		if row.Actionable {
			t.Fatalf("late uncertain row is actionable for terminal Work: %+v", row)
		}
		assertNoWake(t, store)
	})

	t.Run("done + late lease stale stays done with audit only", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		admitAcceptedTurn(t, store, sessionID, turnID)
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		status := WorkDone
		completed, err := store.UpdateWork(workItem.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}

		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:    watcher.EvidenceControl,
			Kind:     "stale",
			SourceID: "lease:expiry:" + turnID,
			At:       time.Date(2026, 8, 7, 10, 0, 31, 0, time.UTC),
		})
		if err != nil || !changed || snapshot.Status != watcher.TurnAccepted {
			t.Fatalf("late stale = (%+v, %v, %v), want turn audit only", snapshot, changed, err)
		}
		after, err := store.Work(completed.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertTerminalWorkUnchanged(t, store, completed, after)
		row, found := turnEvent(t, store, completed.ID, "session:"+sessionID+":turn:"+turnID+":session.stale")
		if !found {
			t.Fatal("late stale row missing from outbox audit")
		}
		if row.Actionable {
			t.Fatalf("late stale row is actionable for terminal Work: %+v", row)
		}
		assertNoWake(t, store)
	})

	t.Run("cancelled + late facts stay cancelled with audit only", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		admitAcceptedTurn(t, store, sessionID, turnID)
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		status := WorkCancelled
		cancelled, err := store.UpdateWork(workItem.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}

		// Late bound Provider terminal (the reviewer result that was already
		// captured) settles the turn but must not reopen the cancelled Work.
		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:      watcher.EvidenceProvider,
			Kind:       "done",
			SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
			Cursor:     1,
			Admission:  admission,
			ActivityID: "activity-1",
			StartedAt:  time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
			SettledAt:  time.Date(2026, 8, 7, 10, 0, 40, 0, time.UTC),
			At:         time.Date(2026, 8, 7, 10, 0, 41, 0, time.UTC),
		})
		if err != nil || !changed || snapshot.Status != watcher.TurnDone {
			t.Fatalf("late bound terminal = (%+v, %v, %v), want turn audit Done", snapshot, changed, err)
		}
		after, err := store.Work(cancelled.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertTerminalWorkUnchanged(t, store, cancelled, after)
		row, found := turnEvent(t, store, cancelled.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
		if !found {
			t.Fatal("late terminal row missing from outbox audit")
		}
		if row.Actionable {
			t.Fatalf("late terminal row is actionable for terminal Work: %+v", row)
		}
		assertNoWake(t, store)
	})

	t.Run("late provider terminal is recorded as turn-level audit", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		admitAcceptedTurn(t, store, sessionID, turnID)
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		status := WorkDone
		completed, err := store.UpdateWork(workItem.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}

		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:      watcher.EvidenceProvider,
			Kind:       "done",
			SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
			Cursor:     1,
			Admission:  admission,
			ActivityID: "activity-1",
			StartedAt:  time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
			SettledAt:  time.Date(2026, 8, 7, 10, 0, 50, 0, time.UTC),
			At:         time.Date(2026, 8, 7, 10, 0, 51, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
		after, err := store.Work(completed.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertTerminalWorkUnchanged(t, store, completed, after)
		turn, hasTurn, err := store.Turn(sessionID)
		if err != nil || !hasTurn || turn.Status != watcher.TurnDone || turn.SettledAt == nil {
			t.Fatalf("turn audit = (%+v, %v, %v), want settled Done", turn, hasTurn, err)
		}
		database, err := store.loadOrchestrationLocked()
		if err != nil {
			t.Fatal(err)
		}
		for _, recorded := range database.BrainTurns {
			if recorded.SessionID != sessionID || recorded.TurnID != turnID {
				continue
			}
			foundFact := false
			for _, fact := range recorded.Facts {
				if fact.Kind == "done" && fact.Class == watcher.EvidenceProvider {
					foundFact = true
				}
			}
			if !foundFact {
				t.Fatalf("late provider terminal fact not retained on the turn: %+v", recorded.Facts)
			}
		}
		assertNoWake(t, store)
	})

	t.Run("ordinary active Work keeps actionable wakes", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		admitAcceptedTurn(t, store, sessionID, turnID)
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		if workItem.Status != WorkRunning {
			t.Fatalf("active Work not running: %+v", workItem)
		}

		// The same liveness uncertain fact on ACTIVE Work still moves the
		// Work to needs_input and produces the actionable wake.
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class:       watcher.EvidenceLiveness,
			Kind:        "uncertain",
			ProcessDead: true,
			SourceID:    "liveness\x00proc-identity-1\x00process-dead",
			SettledAt:   time.Date(2026, 8, 7, 10, 0, 20, 0, time.UTC),
			At:          time.Date(2026, 8, 7, 10, 0, 21, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
		after, err := store.Work(workItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != WorkNeedsInput ||
			!strings.Contains(after.NextAction, "Confirm whether the delegated Session received the prompt") {
			t.Fatalf("active Work after uncertain = %+v", after)
		}
		row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.uncertain")
		if !found || !row.Actionable {
			t.Fatalf("active Work uncertain row = %+v found=%v, want actionable", row, found)
		}
		if _, claimed, err := store.ClaimNextActionableEvent("brain-agent-host-hidden:@1"); err != nil || !claimed {
			t.Fatalf("active Work wake not claimable: claimed=%v err=%v", claimed, err)
		}
	})
}
