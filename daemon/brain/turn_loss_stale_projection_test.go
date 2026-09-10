package brain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

// Live reconciliation 2026-09-11 (Work75ab/Worker194): the coordination steer
// turn was declared lost by lease-expiry escalation while the provider session
// stayed alive, so its BrainTurn rows never terminalized. Brain's scoped review
// follow-up prepared (legitimately queued) but was not yet accepted when the
// delivered turn_lost review resolved continue. ResolveWorkReview committed the
// canonical transition and then failed projecting it: once the open review
// closed, the stale running rows lost their Ready excuse and the presentation
// validator rejected the persist. Canonical revision advanced while the
// presentation kept a ghost open review, and every retry misreported the
// already-closed review.
func TestResolveContinue_ConvergesLostCoordinationWithLiveProvider(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "remote desktop review", Objective: "reconcile lost coordination with live execution",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "all runtime acceptance verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "zen-worker-test:@194"
	const substantive = "turn:substantive-1"
	const coordination = "turn:coordination-2"
	const followUp = "turn:followup-3"
	base := time.Now().UTC().Add(-time.Hour)

	// Substantive execution admitted and running on the provider session.
	first := delegatedSubmissionCandidate(item.ID, sessionID, substantive, "substantive input", base)
	first.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(first); err != nil || !created {
		t.Fatalf("prepare substantive created=%v err=%v", created, err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: substantive, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "substantive-running", At: base.Add(time.Second),
	}); err != nil || !result.Matched {
		t.Fatalf("admit substantive result=%+v err=%v", result, err)
	}

	// Coordination steer supersedes the substantive turn as the live Attempt.
	steer := delegatedSubmissionCandidate(item.ID, sessionID, coordination, "coordination steer", base.Add(time.Minute))
	steer.Mode = watcher.InputAdmissionConditionalSteer
	steer.ExistingTurnID = substantive
	steer.BaselineActivityID = "activity-1"
	steer.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(steer); err != nil || !created {
		t.Fatalf("prepare coordination created=%v err=%v", created, err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: coordination, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "coordination-running", At: base.Add(2 * time.Minute),
	}); err != nil || !result.Matched {
		t.Fatalf("admit coordination result=%+v err=%v", result, err)
	}

	// Lease-expiry escalation declares the coordination turn lost while the
	// provider session stays alive: no terminal turn fact is ever observed,
	// so both BrainTurn rows remain nonterminal projection evidence.
	lost, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || lost.Attempt == nil || lost.Attempt.TurnToken != lifecycle.TurnToken(coordination) {
		t.Fatalf("coordination attempt=%+v err=%v", lost, err)
	}
	if _, err := store.FSM().ReportTurnLost(lost.ID, lifecycle.AttemptIdentity{
		SessionID: lost.Attempt.SessionID, TurnToken: lost.Attempt.TurnToken, Fence: lost.Attempt.Generation,
	}, "lease_expired_escalation"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	lost, err = store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || lost.Attempt != nil || lost.Review == nil || lost.Review.Reason != "turn_lost" {
		t.Fatalf("lost coordination state=%+v err=%v", lost, err)
	}

	// Brain claims, receives, and queues a scoped review follow-up while the
	// loss review is still open. The follow-up is prepared, not yet accepted:
	// transport submission is not provider admission.
	claimAndDeliverTestReview(t, store, "brain-host:@loss")
	follow := delegatedSubmissionCandidate(item.ID, sessionID, followUp, "scoped review", base.Add(3*time.Minute))
	follow.SignalProtocol = true
	follow.ExistingTurnID = coordination
	follow.BaselineActivityID = "activity-1"
	pending, created, err := store.PrepareInputAdmission(follow)
	if err != nil || !created {
		t.Fatalf("prepare follow-up created=%v err=%v", created, err)
	}
	_ = pending
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if admission := state.AdmissionByToken(lifecycle.TurnToken(followUp)); admission == nil ||
		admission.Status != lifecycle.AdmissionPrepared {
		t.Fatalf("follow-up is not legitimately queued: %+v", admission)
	}

	// Resolving the delivered loss review must converge the presentation in
	// the same transaction: no ghost open review, no revision divergence.
	lease := requireReviewDelivered(t, store, item.ID)
	event, projected, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionContinue,
		Summary: "coordination input lost; substantive execution remains live",
	})
	if err != nil {
		t.Fatalf("resolve lost coordination: %v", err)
	}
	if event.HandledAt == nil || event.Actionable || event.Disposition != WorkDispositionContinue {
		t.Fatalf("loss review not closed: %+v", event)
	}
	if projected.Review != nil {
		t.Fatalf("ghost open review survived resolution: %+v", projected.Review)
	}
	state, err = store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Review != nil || state.Revision != projected.Revision {
		t.Fatalf("canonical/presentation diverged: engine=%+v projected=%+v", state, projected)
	}
	for _, turnID := range []string{substantive, coordination} {
		stale, found, err := store.TurnByID(sessionID, turnID)
		if err != nil || !found || watcher.TurnTerminal(stale.Status) {
			t.Fatalf("stale live turn %s must survive as evidence: %+v found=%v err=%v", turnID, stale, found, err)
		}
	}

	// The same delivery capability is idempotent after the commit.
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionContinue,
	}); !errors.Is(err, ErrEventHandled) {
		t.Fatalf("replay of handled review err=%v, want %v", err, ErrEventHandled)
	}

	// The queued follow-up is later admitted by exact provider evidence and
	// owns execution; the stale rows remain audit evidence only.
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: followUp, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "followup-running", At: base.Add(4 * time.Minute),
	}); err != nil || !result.Matched {
		t.Fatalf("admit follow-up result=%+v err=%v", result, err)
	}
	state, err = store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(followUp) {
		t.Fatalf("follow-up attempt=%+v err=%v", state, err)
	}
	projected, err = store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.AttemptSessionID != sessionID || projected.Status != WorkRunning ||
		projected.Review != nil || !strings.Contains(projected.WaitFor, sessionID) {
		t.Fatalf("live execution not projected: %+v", projected)
	}
}
