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

func TestWorkSendReusesCompletedSessionWithoutResolve(t *testing.T) {
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Reuse one delegated Session", Objective: "Continue the next reviewed stage in place.",
		CompletionPolicy: brain.CompletionUntilDone, DoneCriteriaRef: "all reviewed stages complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "zen-worker-reusable:@500"
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
	fw := newFakeControlWatcher()
	fw.turnStore = store
	fw.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Name: "Reusable", Command: "codex", Delegated: true, State: classifier.StateDone,
	}
	app := &controlApp{watcher: fw, brainStore: store}
	request := control.Request{
		Type: "worker_send", WorkerID: sessionID, Text: "Implement the reviewed second stage.", Submit: true,
		WorkID: item.ID,
	}
	response := app.HandleControlRequest(request)
	if !response.OK || response.TurnID == "" {
		t.Fatalf("follow-up response=%+v", response)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Attempt == nil || string(state.Attempt.TurnToken) != response.TurnID || state.Review != nil {
		t.Fatalf("follow-up not active: %+v %v", state, err)
	}
	result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: response.TurnID, Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "report-second-stage", Summary: "Verified second stage",
		At: time.Now().UTC().Add(time.Second),
	})
	if err != nil || !result.Matched || !result.Changed {
		t.Fatalf("Worker report=%+v %v", result, err)
	}
	current, _ := store.Work(item.ID)
	if current.Status == brain.WorkDone || current.Review == nil {
		t.Fatalf("runtime accepted result for Brain: %+v", current)
	}
	response = app.HandleControlRequest(control.Request{
		Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"status"},
		BrainWork: &brain.Work{Status: brain.WorkDone},
	})
	if !response.OK || response.BrainWork.Status != brain.WorkDone {
		t.Fatalf("Brain acceptance=%+v", response)
	}
}
