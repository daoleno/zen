package main

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestReviewAuthorizedSendReusesCompletedSessionBeforeTypedContinue(t *testing.T) {
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Reuse one delegated Session", Objective: "Continue the next reviewed stage in place.",
		CompletionPolicy: brain.CompletionUntilDone, DoneCriteriaRef: "all reviewed stages complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "brain-agent-reusable:@500"
	oldTurnID := admitControlWorkOwner(t, store, item.ID, sessionID)
	oldTurn, found, err := store.Turn(sessionID)
	if err != nil || !found {
		t.Fatalf("old Turn found=%v err=%v", found, err)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: oldTurnID, Class: watcher.EvidenceProvider,
		Kind: "done", Bound: true, SourceID: "provider-reusable-done",
		Admission: oldTurn.Admission, ActivityID: oldTurn.ActivityID,
		StartedAt: oldTurn.AcceptedAt.Add(time.Second), SettledAt: oldTurn.AcceptedAt.Add(2 * time.Second),
		At: oldTurn.AcceptedAt.Add(2 * time.Second), Summary: "first stage complete",
	}); err != nil || !changed {
		t.Fatalf("terminal fact changed=%v err=%v", changed, err)
	}
	claimed, ok, err := store.ClaimNextReviewAction("brain-host:@review")
	if err != nil || !ok {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	resolveControlHostClaim(t, store, claimed)
	if _, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	delivered, err := store.Work(item.ID)
	if err != nil || delivered.Review == nil || delivered.Review.Lease == nil {
		t.Fatalf("delivered Work=%+v err=%v", delivered, err)
	}
	lease := delivered.Review.Lease
	fw := newFakeControlWatcher()
	fw.turnStore = store
	fw.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Name: "Reusable", Command: "codex", Delegated: true, State: classifier.StateDone,
	}
	app := &controlApp{watcher: fw, brainStore: store}
	request := control.Request{
		Type: "agent_send", AgentID: sessionID, Text: "Implement the reviewed second stage.", Submit: true,
		WorkID: item.ID, EventID: delivered.Review.EventID,
		HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		Revision: int64(lease.DeliveryWorkRevision), TurnID: "turn:review-reuse-next",
	}
	response := app.HandleControlRequest(request)
	if !response.OK || response.TurnID != request.TurnID {
		t.Fatalf("review-authorized response=%+v", response)
	}
	prepared, found, err := store.InputAdmission(sessionID, request.TurnID)
	if err != nil || !found || prepared.State != watcher.InputAdmissionResolved ||
		prepared.Purpose != string(lifecycle.AdmissionPurposeReview) || prepared.PurposeID != lease.HandlingID {
		t.Fatalf("review admission found=%v admission=%+v err=%v", found, prepared, err)
	}
	beforeContinue, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || beforeContinue.Attempt != nil || beforeContinue.Review == nil {
		t.Fatalf("accepted preparation activated early: state=%+v err=%v", beforeContinue, err)
	}
	beforeDuplicateRevision := beforeContinue.Revision
	beforeDuplicateSends := len(fw.submitted)
	duplicate := app.HandleControlRequest(request)
	if !duplicate.OK || duplicate.TurnID != request.TurnID {
		t.Fatalf("duplicate preparation response=%+v", duplicate)
	}
	afterDuplicate, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if afterDuplicate.Revision != beforeDuplicateRevision || len(fw.submitted) != beforeDuplicateSends {
		t.Fatalf("duplicate preparation churned revision/sends: %d->%d, %d->%d",
			beforeDuplicateRevision, afterDuplicate.Revision, beforeDuplicateSends, len(fw.submitted))
	}
	_, continued, err := store.ResolveWorkReview(brain.WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: brain.WorkDispositionContinue,
		NextSessionID: sessionID, NextTurnToken: request.TurnID, NextAction: "Run the reviewed second stage.",
	})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt == nil || state.Attempt.SessionID != sessionID || state.Attempt.TurnToken != lifecycle.TurnToken(request.TurnID) ||
		continued.AttemptSessionID != sessionID || state.Review != nil {
		t.Fatalf("typed continue did not activate exact Attempt: state=%+v Work=%+v", state, continued)
	}
}
