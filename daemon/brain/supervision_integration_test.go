package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// Real Store, Service, admission ledger and card projection; only provider
// transport is fake. No production Session or control socket is used.
func TestReusedWorkerTerminalThroughHostEndFairnessAndPresentation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID, workerID = "host:@integration", "worker:@reused"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Worker{ID: hostID, Hidden: true, State: classifier.StateDone}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{hostID: host}, ownedGenerations: map[string]string{hostID: "host-generation"}, outcomes: map[string]watcher.InputOutcome{}, turnStore: store}
	service := NewService(store, fw, nil)
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{Title: "multi-stage worker", Objective: "review every stage", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "Brain accepts final stage"})
	if err != nil {
		t.Fatal(err)
	}
	signal := func(token, kind string) watcher.TurnProgressResult {
		t.Helper()
		now = now.Add(time.Second)
		result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: workerID, TurnID: token, Class: watcher.EvidenceControl, Kind: kind, SourceID: token + ":" + kind, Summary: token + " " + kind, At: now, LeaseSeconds: 300})
		if err != nil || !result.Matched || !result.Changed {
			t.Fatalf("signal %s/%s: %+v %v", token, kind, result, err)
		}
		return result
	}
	submit := func(token, purposeID string) {
		t.Helper()
		now = now.Add(time.Second)
		purpose := ""
		if purposeID != "" {
			purpose = string(lifecycle.AdmissionPurposeReview)
		}
		result, err := fw.SubmitDelegatedWorkInput(workerID, "scoped "+token, item.ID, token, purpose, purposeID, now)
		if err != nil || result.Outcome != watcher.InputAccepted {
			t.Fatalf("submit %s: %+v %v", token, result, err)
		}
	}
	request := func(id string, lease *WorkReviewLease, disposition WorkDisposition) WorkReviewDispositionRequest {
		return WorkReviewDispositionRequest{WorkID: id, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID, ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: disposition}
	}
	workDeliveries := func() int {
		count := 0
		for _, call := range fw.sentCalls {
			if _, ok := work.ParseCanonicalDirectWorkEventInput(call.text); ok {
				count++
			}
		}
		return count
	}
	submit("stage1", "")
	signal("stage1", "running")
	first, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	signal("stage1", "done")
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("stage1 admission: %v %v", woke, err)
	}
	stage1Lease := requireReviewDelivered(t, store, item.ID)
	submit("stage3", stage1Lease.HandlingID)
	settleCanonicalHostTurnForTest(t, store, hostID, stage1Lease.ProviderTurnID)
	signal("stage3", "running")
	active, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if active.Attempt == nil || active.Attempt.TurnToken != "stage3" || active.Attempt.Generation <= first.Attempt.Generation {
		t.Fatalf("reused ownership=%+v", active.Attempt)
	}
	stale, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: workerID, TurnID: "stage1", Class: watcher.EvidenceControl, Kind: "done", SourceID: "late-stage1", At: now})
	if err != nil || stale.Matched || stale.Changed {
		t.Fatalf("old signal accepted: %+v %v", stale, err)
	}
	if _, err := store.FSM().ReportTurnDone(active.ID, lifecycle.AttemptIdentity{SessionID: workerID, TurnToken: "stage3", Fence: first.Attempt.Generation}, lifecycle.DoneInput{OK: true}); !errors.Is(err, lifecycle.ErrStaleInput) {
		t.Fatalf("wrong fence accepted: %v", err)
	}
	// Model a previously admitted clock exception, as in the live incident.
	const leaseEvent = "old-lease-event"
	if _, err := store.FSM().OpenReviewEvent(active.ID, "lease_expired", "stage3", leaseEvent); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("lease admission: %v %v", woke, err)
	}
	lease := requireReviewDelivered(t, store, item.ID)
	badWait := request(item.ID, lease, WorkDispositionWait)
	badWait.Wake = &WorkWake{Kind: WorkWakeSessionTerminal, Ref: workerID}
	if _, _, err := service.ResolveWorkReview(badWait); !errors.Is(err, ErrWorkAttemptConflict) {
		t.Fatalf("impossible wait: %v", err)
	}
	settleCanonicalHostTurnForTest(t, store, hostID, lease.ProviderTurnID)
	if _, err := service.ObserveHostSessionEvent(watcher.SessionEvent{Type: "worker_state_change", WorkerID: hostID, Worker: host, TurnID: lease.ProviderTurnID, OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone)}); err != nil {
		t.Fatal(err)
	}
	ended, err := store.Work(item.ID)
	if err != nil || ended.Review == nil || ended.Review.Lease == nil || ended.Review.Lease.HandlingEndedAt == nil {
		t.Fatalf("Host end=%+v %v", ended.Review, err)
	}
	if live, err := store.HasLiveDeliveredReview(); err != nil || live {
		t.Fatalf("ended handling holds Host lane: %v %v", live, err)
	}
	before := workDeliveries()
	for range 30 {
		if _, err := service.ReconcileHostLane(); err != nil {
			t.Fatal(err)
		}
	}
	if workDeliveries() != before {
		t.Fatal("failed resolve/Host-end loop sent another LLM input")
	}
	inventory, err := store.ProjectWorkInventory(map[string]bool{workerID: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, current := range inventory.Current {
		if current.ID == item.ID && current.AttentionState == WorkAttentionQueued {
			found = true
		}
	}
	if !found {
		t.Fatalf("unresolved decision not discoverable: %+v", inventory)
	}
	other := createSignalTestWork(t, store, "independent work", "other-worker")
	appendSignalTestEvent(t, store, other, "independent-result")
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("independent Work stranded: %v %v", woke, err)
	}
	otherLease := requireReviewDelivered(t, store, other.ID)
	if _, _, err := service.ResolveWorkReview(request(other.ID, otherLease, WorkDispositionComplete)); err != nil {
		t.Fatal(err)
	}
	settleCanonicalHostTurnForTest(t, store, hostID, otherLease.ProviderTurnID)
	signal("stage3", "done")
	terminal, _ := store.FSM().State(active.ID)
	if terminal.Attempt != nil || terminal.Review == nil || terminal.Review.Reason != "turn_done" || terminal.Review.EventID == leaseEvent {
		t.Fatalf("terminal signal not processed: %+v", terminal)
	}
	old, ok, err := store.WorkEvent(leaseEvent)
	if err != nil || !ok || old.Actionable {
		t.Fatalf("superseded event still actionable: %+v %v", old, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("stage3 terminal not admitted: %v %v", woke, err)
	}
	finalLease := requireReviewDelivered(t, store, item.ID)
	if finalLease.HandlingID == lease.HandlingID {
		t.Fatal("terminal reused stale handling")
	}
	if _, _, err := service.ResolveWorkReview(badWait); err == nil {
		t.Fatal("superseded capability accepted")
	}
	timeline, err := store.ThreadTimeline(item.SourceThreadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := TimelineItemsToConversationEvents(timeline)
	// A client may retain a card from an earlier snapshot even when current
	// timeline projection omits that superseded event.
	cached, ok := timelineItemToConversationEvent(workCardTimelineItem(ended, old, false), 1)
	if !ok {
		t.Fatal("cached card fixture did not convert")
	}
	events = append(events, cached)
	if err := service.AnnotateWorkResultEvents(events); err != nil {
		t.Fatal(err)
	}
	seenOld, seenNew := false, false
	for _, event := range events {
		if event.ID == leaseEvent {
			seenOld = true
			if event.WorkResultCurrent || event.WorkReviewState != string(WorkReviewResolved) {
				t.Fatalf("old card still current: %+v", event)
			}
		}
		if event.ID == terminal.Review.EventID {
			seenNew = true
			if !event.WorkResultCurrent || event.WorkReviewState != string(WorkReviewReviewing) {
				t.Fatalf("new card not reviewing: %+v", event)
			}
		}
	}
	if !seenOld || !seenNew {
		t.Fatalf("card evidence missing: old=%v new=%v", seenOld, seenNew)
	}
	if workDeliveries() != before+2 {
		t.Fatalf("wanted independent and terminal deliveries only: before=%d after=%d", before, workDeliveries())
	}
}
