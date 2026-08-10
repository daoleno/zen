package brain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func pendingSubmissionDigest(payload string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
}

func pendingSubmissionTestStore(t *testing.T) (*Store, string, time.Time) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-pending:@1"
	if _, err := store.CreateWork(Work{
		Title: "Pending submission", Objective: "Exercise the canonical input transaction.",
		Status: WorkRunning, OwnerSessionID: sessionID,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatal(err)
	}
	return store, sessionID, time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
}

func prepareInitialSubmission(t *testing.T, store *Store, sessionID, turnID, payload string, at time.Time) watcher.TurnSubmission {
	t.Helper()
	workID := ""
	if item, found, err := store.WorkByOwnerSession(sessionID); err != nil {
		t.Fatal(err)
	} else if found {
		workID = item.ID
	} else if items, err := store.ListWork(); err != nil {
		t.Fatal(err)
	} else if len(items) == 1 {
		// Proved non-submission atomically releases the initial owner while the
		// same Work remains the explicit target for a later fresh attempt.
		workID = items[0].ID
	}
	submission, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256:   pendingSubmissionDigest(payload),
		ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
		AcceptedAt: at, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare submission = (%+v, %v, %v)", submission, created, err)
	}
	return submission
}

func prepareSignalSubmission(t *testing.T, store *Store, sessionID, turnID, payload string, at time.Time) watcher.TurnSubmission {
	t.Helper()
	workID := ""
	if item, found, err := store.WorkByOwnerSession(sessionID); err != nil {
		t.Fatal(err)
	} else if found {
		workID = item.ID
	} else if items, err := store.ListWork(); err != nil {
		t.Fatal(err)
	} else if len(items) == 1 {
		workID = items[0].ID
	}
	submission, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pendingSubmissionDigest(payload), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", AcceptedAt: at, Mode: watcher.TurnSubmissionFresh,
		SignalProtocol: true,
	})
	if err != nil || !created {
		t.Fatalf("prepare signal submission = (%+v, %v, %v)", submission, created, err)
	}
	return submission
}

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
		SessionID: sessionID, TurnID: turnID,
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
		Class:      watcher.EvidencePane,
		Kind:       "running",
		PaneAbsent: true,
		SourceID:   "pane\x00absent",
		At:         at.Add(5 * time.Second),
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
		SessionID: sessionID, TurnID: turnID,
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

func TestFaultSignalTurnProviderTerminalNeverTerminalizes(t *testing.T) {
	// C.2.10 required regression: for a signal-protocol Turn, provider
	// completed/error observations are transport/liveness evidence only.
	// Even a bound provider failure (the recoverable Pi error shape) must not
	// move canonical status or create an actionable terminal; only exact
	// Control done/failed is semantic terminal authority.
	store, sessionID, at := pendingSubmissionTestStore(t)
	store.now = func() time.Time { return at }
	turnID := "turn:signal-provider-terminal"
	pending := prepareSignalSubmission(t, store, sessionID, turnID, "signal payload", at)

	// The exact Control running signal creates the canonical turn from the
	// pending submission; only then can provider evidence attach to it.
	admitted, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00progress-event-1", At: at.Add(time.Second),
		Summary:      "worker accepted",
		LeaseSeconds: 300,
	})
	if err != nil || !admitted.Owned || !admitted.Matched || !admitted.Changed ||
		admitted.Turn.Status != watcher.TurnRunning {
		t.Fatalf("control running admission = (%+v, %v)", admitted, err)
	}
	// The provider adapter enriches the same Turn with its exact tuple; the
	// later provider terminal fact is therefore BOUND to the turn.
	tuple := watcher.TurnAdmission{
		Stream: "provider", ID: "admission-activity-1", Cursor: 1,
		SHA256: pending.PayloadSHA256, At: at.Add(2 * time.Second),
	}
	enriched, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: pending.Receipt,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "activity-1",
		Admission: tuple, ResolvedAt: at.Add(2 * time.Second),
	})
	if err != nil || enriched.ResolvedActivityID != "activity-1" {
		t.Fatalf("provider tuple enrichment = (%+v, %v)", enriched, err)
	}

	// The BOUND provider failure attaches as a provisional hint: canonical
	// status stays Running, Work stays owned, and no actionable terminal
	// Event exists.
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "failed",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  tuple,
		ActivityID: "activity-1",
		StartedAt:  at.Add(3 * time.Second),
		SettledAt:  at.Add(4 * time.Second),
		At:         at.Add(5 * time.Second),
		Summary:    "upstream HTTP 503",
	})
	if err != nil || !changed || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("bound provider failure moved signal canonical status: %+v changed=%v err=%v", snapshot, changed, err)
	}
	workItem, err := store.Work(pending.WorkID)
	if err != nil || workItem.OwnerSessionID != sessionID || workItem.Status != WorkRunning {
		t.Fatalf("provider failure deprojected signal Work: %+v err=%v", workItem, err)
	}
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.failed")
	if !found || row.Actionable {
		t.Fatalf("provider failure created an actionable terminal: %+v found=%v", row, found)
	}

	// The provider recovers and continues inside the same user turn: running
	// evidence refreshes the exact turn without any terminal.
	recovered, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x002",
		Admission:  tuple,
		ActivityID: "activity-1",
		StartedAt:  at.Add(3 * time.Second),
		At:         at.Add(6 * time.Second),
		Summary:    "provider resumed tool calls",
	})
	if err != nil || !changed || recovered.Status != watcher.TurnRunning {
		t.Fatalf("recovered provider running moved signal canonical status: %+v changed=%v err=%v", recovered, changed, err)
	}

	// Only the exact Control terminal is semantic.
	result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00progress-event-100", At: at.Add(7 * time.Second),
		Summary: "REVIEW_READY: exact control completion",
	})
	if err != nil || !result.Owned || !result.Matched || !result.Changed || result.Turn.Status != watcher.TurnDone {
		t.Fatalf("exact control done = (%+v, %v)", result, err)
	}
	doneRow, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
	if !found || !doneRow.Actionable {
		t.Fatalf("control done event = %+v found=%v", doneRow, found)
	}
	if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.failed"); !found {
		t.Fatal("provider failure audit hint disappeared")
	}
}

func TestFaultMatchingControlDoneAtomicallyAdmitsAndSettlesAcrossRestart(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	store.now = func() time.Time { return at }
	turnID := "turn:signal-fault"
	pending := prepareSignalSubmission(t, store, sessionID, turnID, "signal payload", at)
	fact := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00progress-event-99", At: at.Add(5 * time.Second),
		Summary: "REVIEW_READY: control-only completion",
	}

	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return errors.New("injected signal commit failure") }
	if _, err := store.ApplyDelegatedTurnProgress(fact); err == nil {
		t.Fatal("signal persistence failure was reported successful")
	}
	store.writeOrchestration = originalWrite
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := restarted.Turn(sessionID); err != nil || found {
		t.Fatalf("failed atomic signal exposed Turn: found=%v err=%v", found, err)
	}
	stillPending, found, err := restarted.TurnSubmission(sessionID, turnID)
	if err != nil || !found || stillPending.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed atomic signal lost pending row: submission=%+v found=%v err=%v", stillPending, found, err)
	}
	if stillPending.PayloadSHA256 != pending.PayloadSHA256 {
		t.Fatalf("failed atomic signal changed pending identity: %+v", stillPending)
	}

	restarted.now = func() time.Time { return at.Add(5 * time.Second) }
	result, err := restarted.ApplyDelegatedTurnProgress(fact)
	if err != nil || !result.Owned || !result.Matched || !result.Changed || result.Turn.Status != watcher.TurnDone {
		t.Fatalf("restart matching done = (%+v, %v)", result, err)
	}
	resolved, found, err := restarted.TurnSubmission(sessionID, turnID)
	if err != nil || !found || resolved.State != watcher.TurnSubmissionResolved ||
		resolved.ResolvedTurnID != turnID || resolved.ResolvedActivityID != "" || !resolved.ResolvedAdmission.Empty() {
		t.Fatalf("control resolution = (%+v, %v, %v)", resolved, found, err)
	}
	workItem, err := restarted.Work(pending.WorkID)
	if err != nil || workItem.Status != WorkWaiting || workItem.NextAction != "Review the delegated Session result." {
		t.Fatalf("control terminal Work = %+v err=%v", workItem, err)
	}
	row, found := turnEvent(t, restarted, pending.WorkID, "session:"+sessionID+":turn:"+turnID+":session.done")
	if !found || !row.Actionable || row.Summary != fact.Summary {
		t.Fatalf("control terminal event = %+v found=%v", row, found)
	}
	provider := watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "provider-late",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "provider-late-admission", Cursor: 1,
			SHA256: pending.PayloadSHA256, At: at.Add(6 * time.Second),
		}, ResolvedAt: at.Add(6 * time.Second),
	}
	if converged, err := restarted.ResolveTurnSubmission(provider); err != nil ||
		converged.ResolvedActivityID != provider.ActivityID {
		t.Fatalf("late provider convergence = (%+v, %v)", converged, err)
	}
	if _, changed, err := restarted.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceProvider, Kind: "done",
		SourceID: "provider\x00late-terminal", Admission: provider.Admission,
		ActivityID: provider.ActivityID, StartedAt: at.Add(6 * time.Second),
		SettledAt: at.Add(7 * time.Second), At: at.Add(7 * time.Second),
	}); err != nil || changed {
		t.Fatalf("late provider terminal duplicated control outcome: changed=%v err=%v", changed, err)
	}
	if replay, err := restarted.ApplyDelegatedTurnProgress(fact); err != nil ||
		!replay.Owned || !replay.Matched || replay.Changed || replay.Turn.Status != watcher.TurnDone {
		t.Fatalf("restart replay = (%+v, %v)", replay, err)
	}
	claimed, ok, err := restarted.ClaimNextActionableEvent("brain-agent-brain-hidden:@signal")
	if err != nil || !ok || claimed.ID != row.ID {
		t.Fatalf("canonical Brain wake = event=%+v ok=%v err=%v", claimed, ok, err)
	}
	if duplicate, ok, err := restarted.ClaimNextActionableEvent("brain-agent-brain-hidden:@signal"); err != nil || ok {
		t.Fatalf("duplicate Brain wake = event=%+v ok=%v err=%v", duplicate, ok, err)
	}
}

func TestFaultDelegatedSignalRejectsMissingMismatchedAndPrePreparationIdentities(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	store.now = func() time.Time { return at }
	turnID := "turn:identity-gate"
	pending := prepareSignalSubmission(t, store, sessionID, turnID, "identity payload", at)

	writes := 0
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(path string, value any) error {
		writes++
		return originalWrite(path, value)
	}
	for _, test := range []struct {
		name   string
		turnID string
		at     time.Time
	}{
		{name: "missing", at: at.Add(time.Second)},
		{name: "mismatched", turnID: "turn:forged", at: at.Add(time.Second)},
		{name: "before preparation", turnID: turnID, at: at.Add(-time.Nanosecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
				SessionID: sessionID, TurnID: test.turnID, Class: watcher.EvidenceControl,
				Kind: "done", SourceID: "control\x00rejected-" + test.name, At: test.at,
			})
			if err != nil || !result.Owned || result.Matched || result.Changed {
				t.Fatalf("rejected identity = (%+v, %v)", result, err)
			}
		})
	}
	store.writeOrchestration = originalWrite
	if writes != 0 {
		t.Fatalf("rejected identities attempted %d durable writes", writes)
	}
	if _, found, err := store.Turn(sessionID); err != nil || found {
		t.Fatalf("rejected identities promoted a Turn: found=%v err=%v", found, err)
	}
	stillPending, found, err := store.TurnSubmission(sessionID, turnID)
	if err != nil || !found || stillPending.State != watcher.TurnSubmissionPending ||
		stillPending.PayloadSHA256 != pending.PayloadSHA256 {
		t.Fatalf("rejected identities mutated pending: (%+v, %v, %v)", stillPending, found, err)
	}
	events, err := store.ListWorkEvents(pending.WorkID)
	if err != nil || len(events) != 0 {
		t.Fatalf("rejected identities emitted events: %+v err=%v", events, err)
	}
}

func TestFaultConcurrentMatchingTerminalRetriesReduceExactlyOnce(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	store.now = func() time.Time { return at }
	turnID := "turn:terminal-race"
	pending := prepareSignalSubmission(t, store, sessionID, turnID, "race payload", at)
	fact := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00same-logical-terminal", At: at.Add(time.Second), Summary: "race done",
	}
	const attempts = 24
	results := make(chan watcher.TurnProgressResult, attempts)
	errors := make(chan error, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := store.ApplyDelegatedTurnProgress(fact)
			results <- result
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	changed := 0
	for result := range results {
		if !result.Owned || !result.Matched || result.Turn.Status != watcher.TurnDone {
			t.Fatalf("racing terminal result = %+v", result)
		}
		if result.Changed {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("racing terminal durable changes = %d, want one", changed)
	}
	events, err := store.ListWorkEvents(pending.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, event := range events {
		if event.Kind == "session.done" && event.Actionable {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("racing terminal events = %+v", events)
	}
}

func TestFaultReusedSessionRejectsPreviousTurnAndConvergesProviderEvidence(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	store.now = func() time.Time { return at }
	firstTurnID := "turn:reuse-first"
	first := prepareSignalSubmission(t, store, sessionID, firstTurnID, "first prompt", at)
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: firstTurnID, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "control\x00first-running", At: at.Add(time.Second),
	}); err != nil || !result.Matched || result.Turn.Status != watcher.TurnRunning {
		t.Fatalf("first signal admission = (%+v, %v)", result, err)
	}
	providerAdmission := watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: firstTurnID, Receipt: firstTurnID,
		PayloadSHA256: first.PayloadSHA256, ActivityID: "native-reused-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "provider-first", Cursor: 1,
			SHA256: first.PayloadSHA256, At: at.Add(2 * time.Second),
		}, ResolvedAt: at.Add(2 * time.Second),
	}
	if resolved, err := store.ResolveTurnSubmission(providerAdmission); err != nil ||
		resolved.ResolvedActivityID != providerAdmission.ActivityID {
		t.Fatalf("provider convergence = (%+v, %v)", resolved, err)
	}

	secondAt := at.Add(time.Minute)
	store.now = func() time.Time { return secondAt }
	secondTurnID := "turn:reuse-second"
	second, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: first.WorkID, SessionID: sessionID, ProposedTurnID: secondTurnID, Receipt: secondTurnID,
		PayloadSHA256: pendingSubmissionDigest("second prompt"), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", AcceptedAt: secondAt,
		Mode: watcher.TurnSubmissionConditionalSteer, ExistingTurnID: firstTurnID,
		BaselineActivityID: providerAdmission.ActivityID, SignalProtocol: true,
	})
	if err != nil || !created {
		t.Fatalf("prepare reused Session = (%+v, %v, %v)", second, created, err)
	}
	if stale, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: firstTurnID, Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "control\x00late-first", At: secondAt.Add(time.Second),
	}); err != nil || !stale.Owned || stale.Matched || stale.Changed {
		t.Fatalf("previous-turn signal = (%+v, %v)", stale, err)
	}
	if current, _, _ := store.Turn(sessionID); current.TurnID != firstTurnID || current.Status != watcher.TurnRunning {
		t.Fatalf("previous-turn signal mutated current before admission: %+v", current)
	}
	if admitted, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: secondTurnID, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "control\x00second-running", At: secondAt.Add(2 * time.Second),
	}); err != nil || !admitted.Matched || admitted.Turn.TurnID != secondTurnID || admitted.Turn.Status != watcher.TurnRunning {
		t.Fatalf("second signal admission = (%+v, %v)", admitted, err)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: firstTurnID, Class: watcher.EvidenceProvider,
		Kind: "done", SourceID: "provider\x00late-first", Admission: providerAdmission.Admission,
		ActivityID: providerAdmission.ActivityID, StartedAt: at, SettledAt: secondAt,
		At: secondAt.Add(3 * time.Second),
	}); err != nil || changed {
		t.Fatalf("late provider terminal changed newer Turn: changed=%v err=%v", changed, err)
	}
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != secondTurnID || current.Status != watcher.TurnRunning {
		t.Fatalf("current reused Turn = (%+v, %v, %v)", current, found, err)
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
		SessionID: sessionID, TurnID: turnID,
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

// TestFaultMarkerlessAcceptedTurnUnrepresentable: even the legacy bootstrap
// path requires an owning Work; live input is stricter through pending resolve.
func TestFaultMarkerlessAcceptedTurnUnrepresentable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// No Work owns the session: admission fails closed as not-submitted.
	turnID := "brain-agent-nobody:@1:turn:1"
	if _, _, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		SessionID: "brain-agent-nobody:@1", ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", AcceptedAt: time.Now().UTC(), Mode: watcher.TurnSubmissionFresh,
	}); err == nil || !strings.Contains(err.Error(), "no active Brain Work owns delegated Session") {
		t.Fatalf("markerless admission = %v, want fail-closed", err)
	}
}

func TestFaultPendingSubmissionPrepareCrashAndAbortAreFailClosed(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	payload := "first payload"
	turnID := sessionID + ":turn:first"
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return errors.New("injected prepare persistence failure") }
	if _, _, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pendingSubmissionDigest(payload), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", AcceptedAt: at,
		Mode: watcher.TurnSubmissionFresh,
	}); err == nil {
		t.Fatal("prepare persistence failure was accepted")
	}
	store.writeOrchestration = originalWrite
	if _, found, err := store.TurnSubmission(sessionID, turnID); err != nil || found {
		t.Fatalf("failed prepare survived = found=%v err=%v", found, err)
	}
	if _, found, err := store.Turn(sessionID); err != nil || found {
		t.Fatalf("failed prepare created current Turn = found=%v err=%v", found, err)
	}

	prepared := prepareInitialSubmission(t, store, sessionID, turnID, payload, at)
	store.writeOrchestration = func(string, any) error { return errors.New("injected abort persistence failure") }
	if _, err := store.AbortTurnSubmission(sessionID, turnID, turnID, prepared.PayloadSHA256); err == nil {
		t.Fatal("abort persistence failure was reported successful")
	}
	store.writeOrchestration = originalWrite
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	stillPending, found, err := restarted.TurnSubmission(sessionID, turnID)
	if err != nil || !found || stillPending.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed abort changed durable state = (%+v, %v, %v)", stillPending, found, err)
	}
	if _, err := restarted.AbortTurnSubmission(sessionID, turnID, turnID, prepared.PayloadSHA256); err != nil {
		t.Fatal(err)
	}
	restarted, err = NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	afterAbort, err := restarted.Work(prepared.WorkID)
	if err != nil || afterAbort.OwnerSessionID != "" || afterAbort.OwnerDelegated {
		t.Fatalf("committed abort retained naked initial owner: Work=%+v err=%v", afterAbort, err)
	}
	events, err := restarted.ListWorkEvents(prepared.WorkID)
	if err != nil || countUnhandledEventKind(events, "brain.submission_not_admitted") != 1 {
		t.Fatalf("committed abort lost retry Attention: events=%+v err=%v", events, err)
	}
	resolution := watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: prepared.PayloadSHA256, ActivityID: "activity-first",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-first", Cursor: 1,
			SHA256: prepared.PayloadSHA256, At: at.Add(time.Second),
		},
	}
	if _, err := restarted.ResolveTurnSubmission(resolution); err == nil || !strings.Contains(err.Error(), "never be adopted") {
		t.Fatalf("aborted submission was adoptable: %v", err)
	}
	if _, found, err := restarted.Turn(sessionID); err != nil || found {
		t.Fatalf("aborted submission created phantom Turn = found=%v err=%v", found, err)
	}

	// A different payload with a different proposed Turn is a new transaction;
	// the permanently aborted row neither adopts nor blocks it.
	next := prepareInitialSubmission(t, restarted, sessionID, sessionID+":turn:second", "different payload", at.Add(time.Minute))
	resolution.ProposedTurnID = next.ProposedTurnID
	resolution.Receipt = next.Receipt
	resolution.PayloadSHA256 = next.PayloadSHA256
	resolution.ActivityID = "activity-second"
	resolution.Admission.ID = "admission-second"
	resolution.Admission.Cursor = 2
	resolution.Admission.SHA256 = next.PayloadSHA256
	resolution.Admission.At = at.Add(time.Minute + time.Second)
	resolved, err := restarted.ResolveTurnSubmission(resolution)
	if err != nil || resolved.ResolvedTurnID != next.ProposedTurnID {
		t.Fatalf("different payload retry = (%+v, %v)", resolved, err)
	}
}

func TestFaultPendingSubmissionResolveIsAtomicAcrossCrashAndDigestMismatch(t *testing.T) {
	store, sessionID, at := pendingSubmissionTestStore(t)
	pending := prepareInitialSubmission(t, store, sessionID, sessionID+":turn:1", "payload", at)
	if pending.ProcessIdentity != "process-identity" || pending.PaneGeneration != "pane-generation" ||
		pending.Receipt != pending.ProposedTurnID {
		t.Fatalf("pending submission lost transport identity: %+v", pending)
	}
	resolution := watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: pending.ProposedTurnID, Receipt: pending.Receipt,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "activity-new",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-new", Cursor: 1,
			SHA256: pending.PayloadSHA256, At: at.Add(time.Second),
		},
	}

	mismatch := resolution
	mismatch.Admission.SHA256 = pendingSubmissionDigest("different provider bytes")
	if _, err := store.ResolveTurnSubmission(mismatch); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("digest mismatch claimed pending submission: %v", err)
	}
	if _, found, _ := store.Turn(sessionID); found {
		t.Fatal("digest mismatch promoted a Turn")
	}

	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return errors.New("injected post-mutation resolve persistence failure") }
	if _, err := store.ResolveTurnSubmission(resolution); err == nil {
		t.Fatal("resolve persistence failure was reported successful")
	}
	store.writeOrchestration = originalWrite
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := restarted.Turn(sessionID); found {
		t.Fatal("failed atomic resolve exposed a fresh Turn")
	}
	stillPending, found, err := restarted.TurnSubmission(sessionID, pending.ProposedTurnID)
	if err != nil || !found || stillPending.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed atomic resolve lost pending row = (%+v, %v, %v)", stillPending, found, err)
	}
	resolved, err := restarted.ResolveTurnSubmission(resolution)
	if err != nil || resolved.State != watcher.TurnSubmissionResolved || resolved.ResolvedTurnID != pending.ProposedTurnID {
		t.Fatalf("restart resolve = (%+v, %v)", resolved, err)
	}
	if resolved.ResolvedActivityID != resolution.ActivityID ||
		resolved.ResolvedAdmission.SHA256 != pending.PayloadSHA256 {
		t.Fatalf("resolved submission lost exact provider proof: %+v", resolved)
	}
	current, found, err := restarted.Turn(sessionID)
	if err != nil || !found || current.TurnID != pending.ProposedTurnID || current.Status != watcher.TurnAccepted {
		t.Fatalf("restart current Turn = (%+v, %v, %v)", current, found, err)
	}
	if duplicate, err := restarted.ResolveTurnSubmission(resolution); err != nil || duplicate.ResolvedTurnID != pending.ProposedTurnID {
		t.Fatalf("repeat resolve = (%+v, %v)", duplicate, err)
	}
	database, err := restarted.loadOrchestrationLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(database.BrainTurns) != 1 {
		t.Fatalf("repeat resolve duplicated Turns: %+v", database.BrainTurns)
	}
}

func TestFaultPendingSubmissionSameActivitySteersDifferentActivityPromotes(t *testing.T) {
	for _, test := range []struct {
		name              string
		confirmedActivity string
		wantFresh         bool
	}{
		{name: "same activity steers", confirmedActivity: "activity-running"},
		{name: "different activity promotes", confirmedActivity: "activity-new", wantFresh: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, sessionID, at := pendingSubmissionTestStore(t)
			item, found, err := store.WorkByOwnerSession(sessionID)
			if err != nil || !found {
				t.Fatalf("pending submission Work found=%v err=%v", found, err)
			}
			oldTurnID := sessionID + ":turn:old"
			bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
				SessionID: sessionID, TurnID: oldTurnID, AcceptedAt: at.Add(-time.Minute),
				ProcessIdentity: "old-process", PaneGeneration: "old-pane",
			})
			oldAdmission := watcher.TurnAdmission{
				Stream: "provider", ID: "old-admission", Cursor: 1,
				SHA256: pendingSubmissionDigest("old"), At: at.Add(-time.Minute + time.Second),
			}
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: oldTurnID, Class: watcher.EvidenceReceipt,
				Kind: "admission", SourceID: "old-receipt", Admission: oldAdmission,
				ActivityID: "activity-running", At: at.Add(-time.Minute + time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: oldTurnID, Class: watcher.EvidenceProvider,
				Kind: "running", SourceID: "old-running", Admission: oldAdmission,
				ActivityID: "activity-running", StartedAt: at.Add(-time.Minute), At: at,
			}); err != nil {
				t.Fatal(err)
			}
			payload := "follow-up"
			proposed := sessionID + ":turn:new"
			pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
				SessionID: sessionID, ProposedTurnID: proposed, Receipt: proposed,
				PayloadSHA256:   pendingSubmissionDigest(payload),
				ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
				AcceptedAt: at, Mode: watcher.TurnSubmissionConditionalSteer,
				ExistingTurnID: oldTurnID, BaselineActivityID: "activity-running",
			})
			if err != nil || !created {
				t.Fatalf("prepare conditional = (%+v, %v, %v)", pending, created, err)
			}
			if current, _, _ := store.Turn(sessionID); current.TurnID != oldTurnID {
				t.Fatalf("pending replaced running Turn: %+v", current)
			}
			resolved, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
				SessionID: sessionID, ProposedTurnID: proposed, Receipt: proposed,
				PayloadSHA256: pending.PayloadSHA256, ActivityID: test.confirmedActivity,
				Admission: watcher.TurnAdmission{
					Stream: "provider", ID: "new-admission", Cursor: 2,
					SHA256: pending.PayloadSHA256, At: at.Add(time.Second),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantTurnID := oldTurnID
			if test.wantFresh {
				wantTurnID = proposed
			}
			if resolved.ResolvedTurnID != wantTurnID {
				t.Fatalf("resolved TurnID=%s want %s", resolved.ResolvedTurnID, wantTurnID)
			}
			current, found, err := store.Turn(sessionID)
			if err != nil || !found || current.TurnID != wantTurnID {
				t.Fatalf("current after resolve = (%+v, %v, %v)", current, found, err)
			}
		})
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
				SessionID: sessionID, TurnID: turnID,
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
