package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestHostOwnershipLossIdleAutomaticallyDeliversCanonicalEventOnce(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID     = "brain-host:@ownership-lost-idle"
		oldTurnID  = "turn-host-history"
		workerID   = "worker:@completed"
		workerTurn = "turn-worker-completed"
		threadID   = "thread-host-recovery"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	oldWork, err := store.CreateWork(Work{
		Title: "Historical Host turn", Objective: "Retain no-replay audit evidence.",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	oldAccepted := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	bootstrapAdmittedTurnFixture(t, store, oldWork.ID, watcher.AdmittedTurn{
		SessionID: hostID, TurnID: oldTurnID, AcceptedAt: oldAccepted,
		ProcessIdentity: "host-process", PaneGeneration: "host-pane", PayloadSHA256: "old-payload",
	})
	if lost, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: hostID, TurnID: oldTurnID, Class: watcher.EvidenceLiveness,
		Kind: "ownership_lost", SessionReplaced: true, SourceID: "host-history-ownership-lost",
		At: oldAccepted.Add(time.Minute), Summary: "historical Host Turn ownership was lost",
	}); err != nil || !changed || lost.Status != watcher.TurnUnknown || lost.ControlState != watcher.TurnControlOwnershipLost {
		t.Fatalf("historical loss changed=%v turn=%+v err=%v", changed, lost, err)
	}
	oldState, err := store.FSM().State(lifecycle.WorkID(oldWork.ID))
	if err != nil || oldState.Review == nil {
		t.Fatalf("historical review state=%+v err=%v", oldState, err)
	}
	if _, err := store.FSM().ResolveReview(
		lifecycle.WorkID(oldWork.ID), oldState.Review.EventID,
		lifecycle.ResolveReviewInput{Disposition: lifecycle.DispositionComplete, Actor: "test"},
	); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(oldWork.ID); err != nil {
		t.Fatal(err)
	}

	completed, err := store.CreateWork(Work{
		Title: "Completed delegated attempt", Objective: "Deliver one canonical completion Event.",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	workerAccepted := oldAccepted.Add(2 * time.Minute)
	bootstrapAdmittedTurnFixture(t, store, completed.ID, watcher.AdmittedTurn{
		SessionID: workerID, TurnID: workerTurn, AcceptedAt: workerAccepted,
		ProcessIdentity: "worker-process", PaneGeneration: "worker-pane", PayloadSHA256: "worker-payload",
	})
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: workerID, TurnID: workerTurn, Class: watcher.EvidenceProvider,
		Kind: "done", Bound: true, SourceID: "provider-worker-completed", ActivityID: "worker-activity",
		StartedAt: workerAccepted, SettledAt: workerAccepted.Add(time.Minute), At: workerAccepted.Add(time.Minute),
		Summary: "bounded attempt complete",
	}); err != nil || !changed {
		t.Fatalf("completed provider fact changed=%v err=%v", changed, err)
	}
	before, err := store.FSM().State(lifecycle.WorkID(completed.ID))
	if err != nil || before.Review == nil {
		t.Fatalf("canonical pending state=%+v err=%v", before, err)
	}
	eventID := before.Review.EventID
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
		ownedGenerations: map[string]string{hostID: "stable-host-generation"},
		outcomes:         map[string]watcher.InputOutcome{}, turnStore: store,
	}
	service := NewService(store, fw, nil)
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("idle Host automatic delivery woke=%v err=%v", woke, err)
	}
	after, err := store.FSM().State(lifecycle.WorkID(completed.ID))
	if err != nil || after.Review == nil || after.Review.EventID != eventID ||
		after.Review.Handler == nil || after.Review.Handler.DeliveredAt == nil {
		t.Fatalf("delivered canonical state=%+v err=%v", after, err)
	}
	deliveredRevision := after.Revision
	for i := 0; i < 32; i++ {
		if woke, err := service.ReconcileHostLane(); err != nil || woke {
			t.Fatalf("stable reconcile %d woke=%v err=%v", i, woke, err)
		}
	}
	after, _ = store.FSM().State(lifecycle.WorkID(completed.ID))
	if after.Revision != deliveredRevision || len(fw.sentCalls) != 1 {
		t.Fatalf("stable reconciliation churned: revision=%d want=%d sends=%d", after.Revision, deliveredRevision, len(fw.sentCalls))
	}
	items, err := store.ThreadTimeline(threadID, 0)
	matchingCards := 0
	for _, timelineItem := range items {
		if timelineItem.WorkID == completed.ID && timelineItem.ID == eventID {
			matchingCards++
		}
	}
	if err != nil || matchingCards != 1 {
		t.Fatalf("canonical card identity=%+v event=%s matches=%d err=%v", items, eventID, matchingCards, err)
	}
	historical, found, err := store.TurnByID(hostID, oldTurnID)
	if err != nil || !found || historical.Status != watcher.TurnUnknown || historical.ControlState != watcher.TurnControlOwnershipLost {
		t.Fatalf("historical audit was rewritten: found=%v turn=%+v err=%v", found, historical, err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopened
	if woke, err := NewService(reopened, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("reload reconcile woke=%v err=%v", woke, err)
	}
	reloaded, _ := reopened.FSM().State(lifecycle.WorkID(completed.ID))
	if reloaded.Revision != deliveredRevision || len(fw.sentCalls) != 1 {
		t.Fatalf("reload churned: revision=%d want=%d sends=%d", reloaded.Revision, deliveredRevision, len(fw.sentCalls))
	}
}
