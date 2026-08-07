package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// This file implements the frozen fault-injection matrix (worklog C.2.10):
// each fault is injected through the real store + reducer and verified
// against the canonical invariants.

func admittedAcceptedTurn(t *testing.T, store *Store, sessionID, turnID string, at time.Time) watcher.TurnAdmission {
	t.Helper()
	admission := providerAdmission("stream", "msg-1", 1, "sha", at.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class:     watcher.EvidenceReceipt,
		Kind:      "admission",
		SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission: admission,
		At:        at.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	return admission
}

// TestFaultCrashBeforeStateCommit: an observation not yet persisted is
// re-applied idempotently after restart; the deterministic FactID makes the
// first application and the replay a single coherent transition.
func TestFaultCrashBeforeStateCommit(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := admittedAcceptedTurn(t, store, sessionID, turnID, at)
	fact := watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  at.Add(3 * time.Second),
		SettledAt:  at.Add(9 * time.Second),
		At:         at.Add(10 * time.Second),
	}
	// Crash before the terminal commit: a fresh store applies it once.
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, changed, err := restarted.ApplyTurnFact(fact)
	if err != nil || !changed || snapshot.Status != watcher.TurnDone {
		t.Fatalf("crash-before-commit apply = (%+v, %v, %v)", snapshot, changed, err)
	}
	// The replay of the same deterministic fact is a no-op.
	if _, changed, err := restarted.ApplyTurnFact(fact); err != nil || changed {
		t.Fatalf("crash-before-commit replay = changed=%v err=%v", changed, err)
	}
}

// TestFaultAmbiguousInputNeverFailed: ambiguous input resolves Admitted →
// Unknown + session.uncertain on identity end, never failed while alive.
func TestFaultAmbiguousInputNeverFailed(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	// The turn stays Admitted through silence and control reports. A dead
	// pane (end-of-identity, never Failed — Round 4 removed the
	// liveness-derived Failed path) resolves Unknown, never Failed.
	snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceLiveness, Kind: "uncertain",
		ProcessDead: true,
		SourceID:    "liveness\x00process-dead",
		SettledAt:   at.Add(20 * time.Second),
		At:          at.Add(21 * time.Second),
	})
	if err != nil || snapshot.Status != watcher.TurnUnknown {
		t.Fatalf("ambiguous input end-of-identity = %+v err=%v, want Unknown", snapshot, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.uncertain"); !found {
		t.Fatal("ambiguous input lost session.uncertain wake")
	}
}

// TestFaultTransientTmuxAbsenceNeverTerminalizes: PaneAbsent is refresh only;
// a later identity re-proof continues the turn.
func TestFaultTransientTmuxAbsenceNeverTerminalizes(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class:     watcher.EvidencePane,
		Kind:      "running",
		PaneAbsent: true,
		SourceID:  "pane\x00absent",
		At:        at.Add(5 * time.Second),
	}); err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("PaneAbsent moved state: (%+v, %v, %v)", snapshot, changed, err)
	}
}

// TestFaultMissingProgressHeartbeatsEmitsStaleOnce: lease expiry wakes Brain
// once per turn and never creates a terminal fact.
func TestFaultMissingProgressHeartbeatsEmitsStaleOnce(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admittedAcceptedTurn(t, store, sessionID, turnID, at)
	now := at.Add(time.Hour)
	store.now = func() time.Time { return now }
	stale := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "stale",
		SourceID: "lease:expiry:" + turnID,
		At:       now,
	}
	if snapshot, _, err := store.ApplyTurnFact(stale); err != nil || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("stale moved canonical status: %+v err=%v", snapshot, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkNeedsInput {
		t.Fatalf("Work after stale = %v", workItem)
	}
	if _, changed, err := store.ApplyTurnFact(stale); err != nil || changed {
		t.Fatalf("stale duplicated: changed=%v err=%v", changed, err)
	}
}

// TestFaultProcessExitNormalAndUnreadableResolveUnknown: normal exit or
// unreadable history resolves Unknown + session.uncertain, never Failed.
func TestFaultProcessExitNormalAndUnreadableResolveUnknown(t *testing.T) {
	for _, name := range []string{"normal-exit", "unreadable-history"} {
		t.Run(name, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			admittedAcceptedTurn(t, store, sessionID, turnID, at)
			sourceID := "liveness\x00process-dead"
			if name == "unreadable-history" {
				sourceID = "liveness\x00process-dead-unreadable"
			}
			snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class: watcher.EvidenceLiveness, Kind: "uncertain",
				ProcessDead: true,
				SourceID:    sourceID,
				SettledAt:   at.Add(20 * time.Second),
				At:          at.Add(21 * time.Second),
			})
			if err != nil || snapshot.Status != watcher.TurnUnknown {
				t.Fatalf("%s = %+v err=%v, want Unknown", name, snapshot, err)
			}
		})
	}
}

// TestFaultFalseTerminalThenTrueCompletion: a false terminal (hint) followed
// by the true bound completion wakes exactly once and flips in place.
func TestFaultFalseTerminalThenTrueCompletion(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := admittedAcceptedTurn(t, store, sessionID, turnID, at)
	// False terminal: an unbound pane-like done fact is a hint only.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00other\x00false-done\x001",
		ActivityID: "false-done",
		StartedAt:  at.Add(3 * time.Second),
		SettledAt:  at.Add(4 * time.Second),
		At:         at.Add(5 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// True bound completion: canonical Done, one actionable wake.
	snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  at.Add(3 * time.Second),
		SettledAt:  at.Add(9 * time.Second),
		At:         at.Add(10 * time.Second),
	})
	if err != nil || snapshot.Status != watcher.TurnDone {
		t.Fatalf("true completion = %+v err=%v", snapshot, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	events, _ := store.ListWorkEvents(workItem.ID)
	actionable := 0
	for _, event := range events {
		if event.Actionable && strings.HasSuffix(event.DedupeKey, ":session.done") {
			actionable++
		}
	}
	if actionable != 1 {
		t.Fatalf("false-then-true completion actionable wakes = %d, want one", actionable)
	}
}

// TestFaultControlDoneWithoutProviderConfirmation: canonical stays Running,
// and lease expiry wakes Brain instead of the hint.
func TestFaultControlDoneWithoutProviderConfirmation(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admittedAcceptedTurn(t, store, sessionID, turnID, at)
	if snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00progress-event-99",
		At:       at.Add(5 * time.Second),
		Summary:  "agent progress --status done",
	}); err != nil || !changed || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("control done moved canonical status: %+v changed=%v err=%v", snapshot, changed, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
	if !found || row.Actionable {
		t.Fatalf("control done row = %+v found=%v, want non-actionable hint", row, found)
	}
	// Lease expiry (not the hint) wakes Brain.
	now := at.Add(time.Hour)
	store.now = func() time.Time { return now }
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "stale",
		SourceID: "lease:expiry:" + turnID,
		At:       now,
	}); err != nil {
		t.Fatal(err)
	}
	if workItem, _, _ = store.WorkByOwnerSession(sessionID); workItem.Status != WorkNeedsInput {
		t.Fatalf("Work after control-done + stale = %v", workItem)
	}
}

// TestFaultSessionReplacedResolvesUnknown: a different live pane identity
// owns the target; the old turn resolves Unknown + session.uncertain.
func TestFaultSessionReplacedResolvesUnknown(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admittedAcceptedTurn(t, store, sessionID, turnID, at)
	snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceLiveness, Kind: "uncertain",
		SessionReplaced: true,
		SourceID:        "liveness\x00session-replaced",
		SettledAt:       at.Add(20 * time.Second),
		At:              at.Add(21 * time.Second),
	})
	if err != nil || snapshot.Status != watcher.TurnUnknown {
		t.Fatalf("session replaced = %+v err=%v", snapshot, err)
	}
}

// TestFaultDuplicatedAndReorderedFacts: out-of-order replay of an already
// applied terminal and a duplicate running fact both dedupe by FactID.
func TestFaultDuplicatedAndReorderedFacts(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := admittedAcceptedTurn(t, store, sessionID, turnID, at)
	terminal := watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  at.Add(3 * time.Second),
		SettledAt:  at.Add(9 * time.Second),
		At:         at.Add(10 * time.Second),
	}
	if _, _, err := store.ApplyTurnFact(terminal); err != nil {
		t.Fatal(err)
	}
	// Duplicated (identical FactID) and reordered (running after done) facts
	// are no-ops.
	if _, changed, err := store.ApplyTurnFact(terminal); err != nil || changed {
		t.Fatalf("duplicate terminal changed state: %v err=%v", changed, err)
	}
	reordered := terminal
	reordered.Kind = "running"
	if _, changed, err := store.ApplyTurnFact(reordered); err != nil || changed {
		t.Fatalf("reordered running changed terminal: %v err=%v", changed, err)
	}
}

// TestFaultMarkerlessAcceptedTurnUnrepresentable: AdmitTurn is the only
// admission path; an input without a durable Admitted record cannot exist.
func TestFaultMarkerlessAcceptedTurnUnrepresentable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// No Work owns the session: admission fails closed as not-submitted.
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID:  "brain-agent-nobody:@1",
		TurnID:     "brain-agent-nobody:@1:turn:1",
		AcceptedAt: time.Now().UTC(),
	}); err == nil || !strings.Contains(err.Error(), "no active Brain Work") {
		t.Fatalf("markerless admission = %v, want fail-closed", err)
	}
}

// TestFaultDeadPaneUnknownThenLateBoundTerminal covers P1.2: a dead pane
// resolves Unknown + one actionable session.uncertain; a later bound Provider
// terminal upgrades canonical status to Done/Failed with exactly one
// actionable wake of that kind; the uncertain row is retained as audit.
func TestFaultDeadPaneUnknownThenLateBoundTerminal(t *testing.T) {
	for _, kind := range []string{"done", "failed"} {
		t.Run(kind, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			admission := admittedAcceptedTurn(t, store, sessionID, turnID, at)

			// Dead pane, no bound terminal readable: Unknown + uncertain.
			if snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class: watcher.EvidenceLiveness, Kind: "uncertain",
				ProcessDead: true,
				SourceID:    "liveness\x00proc-identity-1\x00process-dead",
				SettledAt:   at.Add(20 * time.Second),
				At:          at.Add(21 * time.Second),
			}); err != nil || snapshot.Status != watcher.TurnUnknown {
				t.Fatalf("dead pane = %+v err=%v, want Unknown", snapshot, err)
			}

			// Late bound Provider terminal upgrades Unknown exactly once.
			terminal := watcher.TurnFact{
				SessionID:  sessionID, TurnID: turnID,
				Class:      watcher.EvidenceProvider,
				Kind:       kind,
				SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
				Admission:  admission,
				ActivityID: "activity-1",
				StartedAt:  at.Add(3 * time.Second),
				SettledAt:  at.Add(30 * time.Second),
				At:         at.Add(31 * time.Second),
			}
			snapshot, changed, err := store.ApplyTurnFact(terminal)
			if err != nil || !changed {
				t.Fatalf("late bound terminal = (%+v, %v, %v)", snapshot, changed, err)
			}
			want := watcher.TurnDone
			if kind == "failed" {
				want = watcher.TurnFailed
			}
			if snapshot.Status != want {
				t.Fatalf("late bound terminal status = %s, want %s", snapshot.Status, want)
			}

			workItem, _, _ := store.WorkByOwnerSession(sessionID)
			events, _ := store.ListWorkEvents(workItem.ID)
			uncertain := 0
			terminalActionable := 0
			for _, event := range events {
				switch {
				case strings.HasSuffix(event.DedupeKey, ":session.uncertain"):
					uncertain++
				case strings.HasSuffix(event.DedupeKey, ":session."+kind) && event.Actionable:
					terminalActionable++
				}
			}
			if uncertain != 1 || terminalActionable != 1 {
				t.Fatalf("wake counts: uncertain=%d terminal=%d events=%#v", uncertain, terminalActionable, events)
			}

			// Done/Failed stay immutable: a later running fact is ignored.
			reopen := terminal
			reopen.Kind = "running"
			if _, changed, err := store.ApplyTurnFact(reopen); err != nil || changed {
				t.Fatalf("immutable terminal reopened: changed=%v err=%v", changed, err)
			}
		})
	}
}
