package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// ledgerTestStore builds a real store with an active owned Work and an
// admitted canonical turn ready for reducer facts.
func ledgerTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-test:@1"
	_, err = store.CreateWork(Work{
		Title:            "Canonical turn test",
		Objective:        "Exercise the single reducer.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := sessionID + ":turn:1"
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID:       sessionID,
		TurnID:          turnID,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "proc-identity-1",
		PaneGeneration:  "pane-gen-1",
		PayloadSHA256:   "payload-digest",
	}); err != nil {
		t.Fatal(err)
	}
	return store, sessionID, turnID
}

func providerAdmission(stream, id string, cursor uint64, sha string, at time.Time) watcher.TurnAdmission {
	return watcher.TurnAdmission{Stream: stream, ID: id, Cursor: cursor, SHA256: sha, At: at}
}

func turnEvent(t *testing.T, store *Store, workID, dedupeKey string) (WorkEvent, bool) {
	t.Helper()
	events, err := store.ListWorkEvents(workID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.DedupeKey == dedupeKey {
			return event, true
		}
	}
	return WorkEvent{}, false
}

// TestTurnFactIDReplayIsNoOp guards C.3.1: applying the identical
// deterministic fact twice (restart replay, reordered delivery) is a no-op.
func TestTurnFactIDReplayIsNoOp(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	admission := providerAdmission("stream", "msg-1", 1, "sha", time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC))
	fact := watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceReceipt,
		Kind:       "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	}
	snapshot, changed, err := store.ApplyTurnFact(fact)
	if err != nil || !changed || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("first admission apply = (%+v, %v, %v)", snapshot, changed, err)
	}
	replayed, changed, err := store.ApplyTurnFact(fact)
	if err != nil || changed || replayed.Status != watcher.TurnAccepted {
		t.Fatalf("replayed admission apply = (%+v, %v, %v), want no-op", replayed, changed, err)
	}

	// Restart re-read: a fresh store on the same root dedupes identically.
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	replayed, changed, err = restarted.ApplyTurnFact(fact)
	if err != nil || changed || replayed.Status != watcher.TurnAccepted {
		t.Fatalf("restart replay apply = (%+v, %v, %v), want no-op", replayed, changed, err)
	}
}

// TestTurnAmbiguousAdmissionNeverFailedAndAdoptsProviderActivity covers C.6
// and the live OpenCode/Research-B incidents: an ambiguous byte-admission can
// never become false failed while the provider works; the poll adopts the
// provider activity inside the admission window and settles exactly once.
func TestTurnAmbiguousAdmissionNeverFailedAndAdoptsProviderActivity(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)

	// A control failed hint on an Admitted turn is denied outright (C.2.3):
	// canonical status stays Admitted, no row, no wake.
	hint := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "failed",
		SourceID: "control\x00progress-event-1",
		At:       acceptedAt.Add(5 * time.Second),
		Summary:  "Delegated input outcome stayed ambiguous",
	}
	snapshot, changed, err := store.ApplyTurnFact(hint)
	if err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("control failed hint = (%+v, %v, %v), want denied", snapshot, changed, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkRunning {
		t.Fatalf("Work moved off running after hint: %v", workItem)
	}

	// Poll adoption: provider activity started inside the admission window
	// binds the turn; the admission tuple is recorded for later binding.
	activity := watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Cursor:     1,
		Admission:  providerAdmission("stream", "msg-1", 1, "normalized-not-payload", acceptedAt.Add(2*time.Second)),
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(2 * time.Second),
		At:         acceptedAt.Add(3 * time.Second),
		Summary:    "Delegated provider activity running",
	}
	snapshot, changed, err = store.ApplyTurnFact(activity)
	if err != nil || !changed || snapshot.Status != watcher.TurnRunning || !snapshot.HasAdmission {
		t.Fatalf("poll adoption = (%+v, %v, %v)", snapshot, changed, err)
	}
	if snapshot.Admission.ID != "msg-1" || snapshot.Admission.SHA256 != "normalized-not-payload" {
		t.Fatalf("adopted admission tuple = %+v", snapshot.Admission)
	}

	// The bound terminal settles the turn and flips the hint row actionable
	// in place: exactly one actionable wake for (session, turn, failed).
	terminal := watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "failed",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Cursor:     1,
		Admission:  providerAdmission("stream", "msg-1", 1, "normalized-not-payload", acceptedAt.Add(2*time.Second)),
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(2 * time.Second),
		SettledAt:  acceptedAt.Add(30 * time.Second),
		At:         acceptedAt.Add(31 * time.Second),
		Summary:    "Delegated provider failed",
	}
	snapshot, changed, err = store.ApplyTurnFact(terminal)
	if err != nil || !changed || snapshot.Status != watcher.TurnFailed {
		t.Fatalf("bound terminal = (%+v, %v, %v)", snapshot, changed, err)
	}
	workItem, err = store.Work(workItem.ID)
	if err != nil || workItem.Status != WorkWaiting {
		t.Fatalf("Work after failed terminal = %v", workItem)
	}
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.failed")
	if !found || !row.Actionable {
		t.Fatalf("hint row was not flipped actionable: %+v found=%v", row, found)
	}
	actionable := 0
	events, _ := store.ListWorkEvents(workItem.ID)
	for _, event := range events {
		if event.Actionable && strings.HasPrefix(event.DedupeKey, "session:"+sessionID+":turn:") {
			actionable++
		}
	}
	if actionable != 1 {
		t.Fatalf("actionable wakes = %d, want exactly one", actionable)
	}
}

// TestTurnBoundProviderTerminalFlipsUnboundHintInPlace covers C.1.2: an
// unbound provider terminal is a non-actionable hint; the bound terminal of
// the same kind flips the same row actionable — row count never changes.
func TestTurnBoundProviderTerminalFlipsUnboundHintInPlace(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))

	// Correlated admission → Accepted with the recorded tuple.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	// Unbound terminal: hint only, canonical stays Accepted.
	unbound := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00other\x00stale-activity\x001",
		ActivityID: "stale-activity",
		StartedAt:  acceptedAt.Add(3 * time.Second),
		SettledAt:  acceptedAt.Add(9 * time.Second),
		At:         acceptedAt.Add(10 * time.Second),
	}
	snapshot, _, err := store.ApplyTurnFact(unbound)
	if err != nil || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("unbound terminal moved canonical status: %+v err=%v", snapshot, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
	if !found || row.Actionable {
		t.Fatalf("unbound hint row = %+v found=%v, want non-actionable", row, found)
	}

	// Bound terminal: same row flips actionable; row count stays one.
	bound := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Cursor:     1,
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(3 * time.Second),
		SettledAt:  acceptedAt.Add(9 * time.Second),
		At:         acceptedAt.Add(10 * time.Second),
	}
	snapshot, changed, err := store.ApplyTurnFact(bound)
	if err != nil || !changed || snapshot.Status != watcher.TurnDone {
		t.Fatalf("bound terminal = (%+v, %v, %v)", snapshot, changed, err)
	}
	events, _ := store.ListWorkEvents(workItem.ID)
	doneRows := 0
	for _, event := range events {
		if event.DedupeKey == "session:"+sessionID+":turn:"+turnID+":session.done" {
			doneRows++
			if !event.Actionable {
				t.Fatalf("flipped row not actionable: %+v", event)
			}
		}
	}
	if doneRows != 1 {
		t.Fatalf("session.done rows = %d, want exactly one", doneRows)
	}
}

// TestTurnTerminalImmutabilityAndSessionReuse covers C.2.8: a terminal turn
// is immutable; a reused Session's new turn is a new lifecycle boundary and
// old terminal facts are ignored.
func TestTurnTerminalImmutabilityAndSessionReuse(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(3 * time.Second),
		SettledAt:  acceptedAt.Add(9 * time.Second),
		At:         acceptedAt.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	// A later running fact for the terminal turn is ignored.
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-2\x001",
		Admission:  providerAdmission("stream", "msg-2", 2, "sha", acceptedAt.Add(20*time.Second)),
		ActivityID: "activity-2",
		StartedAt:  acceptedAt.Add(20 * time.Second),
		At:         acceptedAt.Add(21 * time.Second),
	})
	if err != nil || changed || snapshot.Status != watcher.TurnDone {
		t.Fatalf("terminal turn mutated: (%+v, %v, %v)", snapshot, changed, err)
	}

	// Session reuse: a new turn is a new lifecycle boundary.
	turn2 := sessionID + ":turn:2"
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID:       sessionID,
		TurnID:          turn2,
		AcceptedAt:      acceptedAt.Add(30 * time.Second),
		ProcessIdentity: "proc-identity-1",
		PaneGeneration:  "pane-gen-1",
		PayloadSHA256:   "payload-digest-2",
	}); err != nil {
		t.Fatal(err)
	}
	admission2 := providerAdmission("stream", "msg-3", 3, "sha-2", acceptedAt.Add(32*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turn2,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turn2 + "\x00accepted\x00payload-digest-2",
		Admission:  admission2,
		ActivityID: "activity-3",
		At:         acceptedAt.Add(32 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// A stale fact for turn 1 must never touch turn 2.
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(2 * time.Second),
		At:         acceptedAt.Add(33 * time.Second),
	}); err != nil || changed {
		t.Fatalf("old turn fact leaked into new turn: changed=%v err=%v", changed, err)
	}
	snapshot, _, _ = store.Turn(sessionID)
	if snapshot.TurnID != turn2 || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("current turn after reuse = %+v", snapshot)
	}
}

// TestTurnLeaseExpiryEmitsStaleOnce covers C.9: missing heartbeats append one
// actionable session.stale per turn; Work moves to needs_input; the clock
// never terminalizes canonical status.
func TestTurnLeaseExpiryEmitsStaleOnce(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	now := acceptedAt.Add(time.Hour)
	store.now = func() time.Time { return now }
	stale := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "stale",
		SourceID: "lease:expiry:" + turnID,
		At:       now,
	}
	snapshot, changed, err := store.ApplyTurnFact(stale)
	if err != nil || !changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("stale fact moved canonical status: (%+v, %v, %v)", snapshot, changed, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkNeedsInput || !strings.Contains(workItem.NextAction, "lease expiry") {
		t.Fatalf("Work after stale = %v", workItem)
	}
	if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.stale"); !found {
		t.Fatal("session.stale row missing")
	}
	// Re-applying the same deterministic stale fact is a no-op (once per turn).
	if _, changed, err := store.ApplyTurnFact(stale); err != nil || changed {
		t.Fatalf("stale re-apply changed state: %v err=%v", changed, err)
	}
	events, _ := store.ListWorkEvents(workItem.ID)
	staleRows := 0
	for _, event := range events {
		if strings.HasSuffix(event.DedupeKey, ":session.stale") {
			staleRows++
		}
	}
	if staleRows != 1 {
		t.Fatalf("session.stale rows = %d, want exactly one", staleRows)
	}
}

// TestTurnLivenessFacts covers CR.3/C.2.1: abnormal exit is final-grade
// Failed; ProcessDead/SessionReplaced without a bound terminal resolve to
// Unknown + one actionable session.uncertain; normal exit never fails.
func TestTurnLivenessFacts(t *testing.T) {
	t.Run("liveness failed fact never decides Failed", func(t *testing.T) {
		// Round 4: the liveness-derived Failed path is removed entirely — no
		// production primitive can attribute a dead pane's exit status to the
		// exact recorded process lifetime. A liveness failed fact is ignored;
		// only a bound Provider terminal may decide Failed.
		store, sessionID, turnID := ledgerTestStore(t)
		acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceReceipt, Kind: "admission",
			SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
			Admission: providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second)),
			At:        acceptedAt.Add(2 * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceLiveness, Kind: "failed",
			AbnormalExit: true,
			SourceID:     "liveness\x00abnormal-exit",
			SettledAt:    acceptedAt.Add(20 * time.Second),
			At:           acceptedAt.Add(21 * time.Second),
		})
		if err != nil || changed || snapshot.Status != watcher.TurnAccepted {
			t.Fatalf("liveness failed fact = (%+v, %v, %v), want ignored", snapshot, changed, err)
		}
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		if workItem.Status != WorkRunning {
			t.Fatalf("Work after liveness failed fact = %v", workItem)
		}
		// The dead pane resolves end-of-identity Unknown + uncertain instead.
		snapshot, _, err = store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceLiveness, Kind: "uncertain",
			ProcessDead: true,
			SourceID:    "liveness\x00process-dead",
			SettledAt:   acceptedAt.Add(20 * time.Second),
			At:          acceptedAt.Add(21 * time.Second),
		})
		if err != nil || snapshot.Status != watcher.TurnUnknown {
			t.Fatalf("dead pane resolution = %+v err=%v, want Unknown", snapshot, err)
		}
	})
	t.Run("normal exit resolves unknown uncertain", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceLiveness, Kind: "uncertain",
			ProcessDead: true,
			SourceID:    "liveness\x00process-dead",
			SettledAt:   acceptedAt.Add(20 * time.Second),
			At:          acceptedAt.Add(21 * time.Second),
		})
		if err != nil || !changed || snapshot.Status != watcher.TurnUnknown {
			t.Fatalf("normal exit = (%+v, %v, %v), want Unknown", snapshot, changed, err)
		}
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		if workItem.Status != WorkNeedsInput ||
			!strings.Contains(workItem.NextAction, "Confirm whether the delegated Session received the prompt") {
			t.Fatalf("Work after Unknown = %v", workItem)
		}
		if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.uncertain"); !found {
			t.Fatal("session.uncertain row missing")
		}
	})
	t.Run("liveness failed fact is ignored on any turn", func(t *testing.T) {
		store, sessionID, turnID := ledgerTestStore(t)
		snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceLiveness, Kind: "failed",
			SourceID: "liveness\x00abnormal-exit",
			At:       time.Now(),
		})
		if err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
			t.Fatalf("liveness failed fact = (%+v, %v, %v), want ignored", snapshot, changed, err)
		}
	})
}

// TestTurnBoundProviderTerminalUpgradesUnknown covers C.2.4: a later readable
// turn-bound Provider terminal upgrades canonical status and derived Work
// from Unknown; the uncertain row is retained as audit history and the new
// kind wakes exactly once.
func TestTurnBoundProviderTerminalUpgradesUnknown(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceLiveness, Kind: "uncertain",
		ProcessDead: true,
		SourceID:    "liveness\x00process-dead",
		SettledAt:   acceptedAt.Add(20 * time.Second),
		At:          acceptedAt.Add(21 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(3 * time.Second),
		SettledAt:  acceptedAt.Add(9 * time.Second),
		At:         acceptedAt.Add(40 * time.Second),
	})
	if err != nil || !changed || snapshot.Status != watcher.TurnDone {
		t.Fatalf("post-Unknown upgrade = (%+v, %v, %v)", snapshot, changed, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkWaiting {
		t.Fatalf("Work after post-Unknown upgrade = %v", workItem)
	}
	events, _ := store.ListWorkEvents(workItem.ID)
	uncertainKept := false
	doneActionable := 0
	for _, event := range events {
		switch {
		case strings.HasSuffix(event.DedupeKey, ":session.uncertain"):
			uncertainKept = true
		case strings.HasSuffix(event.DedupeKey, ":session.done") && event.Actionable:
			doneActionable++
		}
	}
	if !uncertainKept || doneActionable != 1 {
		t.Fatalf("post-Unknown audit rows: uncertainKept=%v doneActionable=%d events=%#v",
			uncertainKept, doneActionable, events)
	}
}

// TestTurnControlAttentionBlocksAndClears covers C.2.3: needs_input comes only
// from Control attention or bound provider facts; pane Blocked never wakes a
// turn-tracked session (Pi incident).
func TestTurnControlAttentionBlocksAndClears(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	// A pane blocked signal is a refresh-grade no-op.
	if snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidencePane, Kind: "blocked",
		SourceID: "pane\x00content-hash",
		At:       acceptedAt.Add(2 * time.Second),
	}); err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("pane blocked moved canonical status: (%+v, %v, %v)", snapshot, changed, err)
	}

	// Control attention user_input → Blocked + actionable needs_input once.
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "attention",
		SourceID: "control\x00progress-event-2",
		At:       acceptedAt.Add(3 * time.Second),
		Summary:  "Awaiting user input",
	}); err != nil || !changed {
		t.Fatalf("control attention apply = changed=%v err=%v", changed, err)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkNeedsInput {
		t.Fatalf("Work after attention = %v", workItem)
	}
	if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.needs_input"); !found {
		t.Fatal("needs_input row missing")
	}

	// Attention cleared via Control running → Running.
	if snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00progress-event-3",
		At:       acceptedAt.Add(4 * time.Second),
	}); err != nil || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("attention clear = %+v err=%v", snapshot, err)
	}
}

// TestTurnControlRunningNeverPromotesAdmitted covers invariant 13: only an
// accepted Receipt or a bound Provider admission/activity proves the input
// began; Control running on an Admitted turn is a no-op.
func TestTurnControlRunningNeverPromotesAdmitted(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00progress-event-4",
		At:       acceptedAt.Add(2 * time.Second),
	})
	if err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("control running promoted Admitted: (%+v, %v, %v)", snapshot, changed, err)
	}
	// A pane running signal likewise cannot promote.
	if snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidencePane, Kind: "running",
		SourceID: "pane\x00content-hash-2",
		At:       acceptedAt.Add(3 * time.Second),
	}); err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("pane running promoted Admitted: (%+v, %v, %v)", snapshot, changed, err)
	}
}

// TestTurnControlDoneHintDeniedBeforeAcceptance covers the C.2.3 gate:
// control terminal reports are hints only for Accepted+ turns at/after
// AcceptedAt; from Admitted they are denied outright.
func TestTurnControlDoneHintDeniedBeforeAcceptance(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00progress-event-5",
		At:       acceptedAt.Add(2 * time.Second),
	})
	if err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("control done on Admitted = (%+v, %v, %v), want denied", snapshot, changed, err)
	}
}

// TestTurnIdenticalHeartbeatsRenewAndRetryDedupes covers the FactID review:
// identical consecutive heartbeats with distinct progress_event_id are
// distinct facts that both renew; a transport retry reusing the ID dedupes.
func TestTurnIdenticalHeartbeatsRenewAndRetryDedupes(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission: admission,
		At:        acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	payload := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00heartbeat-1",
		At:       acceptedAt.Add(3 * time.Second),
		Summary:  "still working",
	}
	if snapshot, changed, err := store.ApplyTurnFact(payload); err != nil || !changed || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("first heartbeat = (%+v, %v, %v)", snapshot, changed, err)
	}
	// Identical payload, distinct progress_event_id: a second distinct fact.
	second := payload
	second.SourceID = "control\x00heartbeat-2"
	second.At = acceptedAt.Add(4 * time.Second)
	if snapshot, changed, err := store.ApplyTurnFact(second); err != nil || !changed || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("second heartbeat = (%+v, %v, %v)", snapshot, changed, err)
	}
	// Transport retry with the same ID dedupes.
	if _, changed, err := store.ApplyTurnFact(payload); err != nil || changed {
		t.Fatalf("retry heartbeat changed state: %v err=%v", changed, err)
	}
}

// TestTurnReceiptAmbiguousThenAcceptedAppliesBothFacts covers the FactID
// review: receipt state is part of the identity, so ambiguous → accepted is a
// distinct fact while same-state retries dedupe.
func TestTurnReceiptAmbiguousThenAcceptedAppliesBothFacts(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	// The ambiguous receipt fact applies no canonical change but is a real
	// durable fact (it documents the ambiguity deterministically).
	ambiguous := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID: "receipt\x00" + turnID + "\x00ambiguous\x00payload-digest",
		At:       acceptedAt.Add(time.Second),
	}
	if snapshot, changed, err := store.ApplyTurnFact(ambiguous); err != nil || changed || snapshot.Status != watcher.TurnAdmitted {
		t.Fatalf("ambiguous receipt = (%+v, %v, %v)", snapshot, changed, err)
	}
	if _, changed, err := store.ApplyTurnFact(ambiguous); err != nil || changed {
		t.Fatalf("ambiguous receipt retry changed state: %v err=%v", changed, err)
	}
	accepted := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second)),
		ActivityID: "activity-1",
		At:         acceptedAt.Add(2 * time.Second),
	}
	if snapshot, changed, err := store.ApplyTurnFact(accepted); err != nil || !changed || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("accepted receipt promotion = (%+v, %v, %v)", snapshot, changed, err)
	}
}

// TestTurnOneProviderRecordDerivesDistinctKinds covers the FactID review:
// kind participates in the base formula, so one native record deriving
// running + done stays distinct and both facts apply.
func TestTurnOneProviderRecordDerivesDistinctKinds(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission: admission,
		At:        acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	base := watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Cursor:     1,
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(3 * time.Second),
	}
	running := base
	running.Kind = "running"
	running.SourceID = "provider\x00" + sessionID + "\x00stream\x00activity-1\x001"
	if snapshot, changed, err := store.ApplyTurnFact(running); err != nil || !changed || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("running from record = (%+v, %v, %v)", snapshot, changed, err)
	}
	done := base
	done.Kind = "done"
	done.SourceID = "provider\x00" + sessionID + "\x00stream\x00activity-1\x001"
	done.SettledAt = acceptedAt.Add(9 * time.Second)
	if snapshot, changed, err := store.ApplyTurnFact(done); err != nil || !changed || snapshot.Status != watcher.TurnDone {
		t.Fatalf("done from same record = (%+v, %v, %v)", snapshot, changed, err)
	}
}

// TestTurnStaleProviderSnapshotCursorGate covers the WAL-lag/stale-snapshot
// fault: a provider fact with an older cursor than the recorded admission can
// never bind or adopt.
func TestTurnStaleProviderSnapshotCursorGate(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission:  providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second)),
		ActivityID: "activity-1",
		At:         acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// Stale snapshot: same stream but an OLDER cursor cannot bind; the fact
	// is attached as a provisional hint and the canonical status stays
	// Accepted — the stale terminal never wakes.
	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00stale-activity\x000",
		Cursor:     0,
		Admission:  providerAdmission("stream", "msg-0", 0, "sha", acceptedAt.Add(-time.Hour)),
		ActivityID: "stale-activity",
		StartedAt:  acceptedAt.Add(-time.Hour),
		SettledAt:  acceptedAt.Add(-30 * time.Minute),
		At:         acceptedAt.Add(5 * time.Second),
	})
	if err != nil || snapshot.Status != watcher.TurnAccepted {
		t.Fatalf("stale snapshot moved canonical status: (%+v, %v, %v)", snapshot, changed, err)
	}
	if len(snapshot.Hints) != 1 || snapshot.Hints[0].Kind != "session.done" {
		t.Fatalf("stale snapshot did not attach a hint: %+v", snapshot.Hints)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if _, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done"); !found {
		t.Fatal("stale snapshot hint row missing")
	}
	row, _ := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
	if row.Actionable {
		t.Fatalf("stale snapshot hint row is actionable: %+v", row)
	}
}

// TestTurnLedgerValidationEnforcesUniqueness covers C.2.5 invariants:
// duplicate (session, turn) rows and duplicate fact IDs are unrepresentable.
func TestTurnLedgerValidationEnforcesUniqueness(t *testing.T) {
	store, sessionID, _ := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	// Duplicate (session, turn) cannot persist.
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	if err == nil {
		database.BrainTurns = append(database.BrainTurns, TurnRecord{
			SessionID:  sessionID,
			TurnID:     sessionID + ":turn:1",
			WorkID:     workItem.ID,
			Status:     watcher.TurnAdmitted,
			AcceptedAt: acceptedAt,
			UpdatedAt:  acceptedAt,
			Facts:      []TurnFactRecord{},
		})
		err = store.persistOrchestrationLocked(database)
	}
	store.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "duplicate session_id/turn_id") {
		t.Fatalf("duplicate turn persisted: %v", err)
	}
}
