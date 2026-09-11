package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

// TestReconcileDelegatedSessionsObservationGapKeepsLiveTurn covers the restart
// reconciliation observation gap: when the selected tmux server is unreadable,
// an owner that is absent from the inventory snapshot must remain owned and the
// canonical Turn must not become Unknown.
func TestReconcileDelegatedSessionsObservationGapKeepsLiveTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "live across observation gap", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "worker-turn", AcceptedAt: base,
	})
	fw := &fakeWatcher{turnStore: store, probeErr: errors.New("tmux server unavailable")}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return base }

	// The inventory gap omits the worker and the server cannot be read.
	service.ReconcileDelegatedSessions(nil)

	turn, found, err := store.Turn("worker")
	if err != nil || !found {
		t.Fatalf("canonical turn found=%v err=%v", found, err)
	}
	if turn.Status != watcher.TurnAdmitted {
		t.Fatalf("observation gap ended the live turn: %+v", turn)
	}
	st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if st.Attempt == nil || st.Attempt.TurnToken != lifecycle.TurnToken("worker-turn") {
		t.Fatalf("observation gap released the live attempt: %+v", st.Attempt)
	}
}

// TestApplyTurnFactTransientLivenessUncertainNeverEndsTurn covers the reducer
// defence-in-depth: an EvidenceLiveness uncertain fact without positive
// disappearance (ProcessDead/SessionReplaced) is audit only and never ends a
// live Turn, while positive evidence still resolves Unknown.
func TestApplyTurnFactTransientLivenessUncertainNeverEndsTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "transient liveness", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "transient-turn", AcceptedAt: base,
	})

	snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "worker", TurnID: "transient-turn", Class: watcher.EvidenceLiveness,
		Kind: "uncertain", PaneAbsent: true,
		SourceID: "liveness\x00worker\x00pane-absent", At: base, Summary: "pane absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed || snapshot.Status == watcher.TurnUnknown {
		t.Fatalf("transient liveness ended the live turn: %+v changed=%v", snapshot, changed)
	}

	snapshot, changed, err = store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "worker", TurnID: "transient-turn", Class: watcher.EvidenceLiveness,
		Kind: "uncertain", ProcessDead: true,
		SourceID: "liveness\x00worker\x00process-dead", At: base.Add(time.Second), Summary: "process dead",
	})
	if err != nil || !changed || snapshot.Status != watcher.TurnUnknown {
		t.Fatalf("positive liveness evidence did not resolve Unknown: %+v changed=%v err=%v", snapshot, changed, err)
	}
}

// TestReconcileDelegatedSessionsProvenAbsenceEndsTurn covers the true-dead
// path: a reachable server that no longer owns the target still resolves
// end-of-identity Unknown and releases the attempt.
func TestReconcileDelegatedSessionsProvenAbsenceEndsTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "truly gone", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "worker-turn", AcceptedAt: base,
	})
	// nil sessions => ProbeSession Absent => ResolveDelegatedAbsence true.
	fw := &fakeWatcher{turnStore: store}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return base }

	service.ReconcileDelegatedSessions(nil)

	turn, found, err := store.Turn("worker")
	if err != nil || !found {
		t.Fatalf("canonical turn found=%v err=%v", found, err)
	}
	if turn.Status != watcher.TurnUnknown {
		t.Fatalf("proven absence did not resolve Unknown: %+v", turn)
	}
	st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if st.Attempt != nil {
		t.Fatalf("proven absence did not release the attempt: %+v", st.Attempt)
	}
}

// TestBusySignalFollowUpSteersActiveAttemptThroughRealStore reproduces the
// 02:03 coordination-send failure against the real Brain Store/FSM: a Fresh
// admission is rejected while the same Work owns an active Attempt, but the
// generic conditional-steer signal contract steers that exact Attempt, the new
// Turn becomes canonical, and the predecessor's late done signal cannot finish
// the new execution.
func TestBusySignalFollowUpSteersActiveAttemptThroughRealStore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "busy steer", "worker")
	sessionID := "worker"
	oldTurn := "worker-turn-1"

	if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: oldTurn, Receipt: oldTurn,
		PayloadSHA256: pendingSubmissionDigest("old"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("prepare active turn created=%v err=%v", created, err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: oldTurn, Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00" + oldTurn, At: base.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	running, found, err := store.Turn(sessionID)
	if err != nil || !found || running.TurnID != oldTurn || running.Status != watcher.TurnRunning {
		t.Fatalf("active turn = %+v found=%v err=%v", running, found, err)
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(oldTurn) {
		t.Fatalf("no active attempt for %s: %+v", oldTurn, state.Attempt)
	}

	// The exact production failure: a Fresh admission cannot cross an active
	// Attempt on the same Work.
	if _, _, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: "worker-turn-2-fresh", Receipt: "worker-turn-2-fresh",
		PayloadSHA256: pendingSubmissionDigest("fresh"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base.Add(2 * time.Second), Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}); err == nil {
		t.Fatal("fresh admission was accepted while the Work had an active Attempt")
	}

	// The generic conditional-steer/signal contract steers the active Attempt.
	newTurn := "worker-turn-2"
	if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: newTurn, Receipt: newTurn,
		PayloadSHA256: pendingSubmissionDigest("new"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base.Add(3 * time.Second), Mode: watcher.InputAdmissionConditionalSteer,
		ExistingTurnID: oldTurn, BaselineActivityID: "observed-activity", SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("conditional steer prepare created=%v err=%v", created, err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: newTurn, Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00" + newTurn, At: base.Add(4 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != newTurn {
		t.Fatalf("steer did not promote the new turn: %+v found=%v err=%v", current, found, err)
	}
	state, _ = store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(newTurn) {
		t.Fatalf("active attempt not superseded by steer: %+v", state.Attempt)
	}

	// A late predecessor done signal cannot complete the new execution.
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: oldTurn, Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00" + oldTurn + "\x00done", At: base.Add(5 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	after, found, err := store.Turn(sessionID)
	if err != nil || !found || after.TurnID != newTurn || watcher.TurnImmutable(after.Status) {
		t.Fatalf("stale predecessor signal completed the new execution: %+v found=%v err=%v", after, found, err)
	}
}
