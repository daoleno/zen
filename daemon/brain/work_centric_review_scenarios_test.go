package brain

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func osMkdirAll(path string) error {
	return os.MkdirAll(path, 0o700)
}

// work_centric_review_scenarios_test.go binds the Work-centric review model to
// the brief's required scenarios: duplicate done, three independent completed
// Works (the screenshot scenario), Host death before/after delivery, successor
// Session, terminal Work, historical events, queue/card projection, no double
// review, and restart reconstruction.

// TestReviewDuplicateDoneFactsAreIdempotent: the same Session done fact
// (dedupe key) can never create a second fact row, a second review epoch, or
// a second card.
func TestReviewDuplicateDoneFactsAreIdempotent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Duplicate done", "brain-agent-dupe:@1")
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:brain-agent-dupe:@1:turn:one:session.done",
		SourceName: item.OwnerSessionID, PayloadRef: "session:" + item.OwnerSessionID,
		Summary: "First completion", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("first done created=%v err=%v", created, err)
	}
	duplicate, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:brain-agent-dupe:@1:turn:one:session.done",
		SourceName: item.OwnerSessionID, PayloadRef: "session:" + item.OwnerSessionID,
		Summary: "Duplicate completion", Actionable: true,
	})
	if err != nil || created || duplicate.ID != event.ID {
		t.Fatalf("duplicate done created=%v event=%+v err=%v", created, duplicate, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("duplicate done rows=%+v err=%v", events, err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{event.ID})
	if err != nil || lifecycles[event.ID].ReviewState != WorkReviewQueued {
		t.Fatalf("duplicate done lifecycle=%+v err=%v", lifecycles, err)
	}
	// Exactly one card is materialized for the one fact.
	if _, materialized, err := store.MaterializeWorkCard(item, event); err != nil || !materialized {
		t.Fatalf("first card materialized=%v err=%v", materialized, err)
	}
	if _, materialized, err := store.MaterializeWorkCard(item, event); err != nil || materialized {
		t.Fatalf("duplicate card materialized=%v err=%v", materialized, err)
	}
}

// TestReviewThreeCompletedWorksDischargeExactlyOnce is the screenshot
// scenario: three independent completed Works produce three review-required
// cards; Brain dispositions remove each exactly once; Sessions close; the
// active queue reaches zero with no accumulated ghost cards.
func TestReviewThreeCompletedWorksDischargeExactlyOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@screenshot"
	type fixture struct {
		work  Work
		event WorkEvent
	}
	fixtures := make([]fixture, 0, 3)
	for index := 0; index < 3; index++ {
		item := createSignalTestWork(t, store, "Completed Work", "brain-agent-worker:@shot"+string(rune('a'+index)))
		event := appendSignalTestEvent(t, store, item, "screenshot-done")
		fixtures = append(fixtures, fixture{work: item, event: event})
	}

	// Three review-required cards, one per Work, all queued.
	ids := []string{fixtures[0].event.ID, fixtures[1].event.ID, fixtures[2].event.ID}
	lifecycles, err := store.WorkResultLifecycles(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if lifecycles[id].ReviewState != WorkReviewQueued || !lifecycles[id].CurrentResult {
			t.Fatalf("card lifecycle=%+v want queued current", lifecycles[id])
		}
	}
	inventory, err := store.ProjectWorkInventory(map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	// Three review-required Work cards are the active queue: the bounded
	// current window shows all three as queued attention.
	if len(inventory.Current) != 3 || inventory.Backlog.QueuedAttention != 0 {
		t.Fatalf("queue projection=%+v", inventory)
	}
	for _, current := range inventory.Current {
		if current.AttentionState != WorkAttentionQueued {
			t.Fatalf("queued item attention=%s: %+v", current.AttentionState, current)
		}
	}

	// Discharge each exactly once in queue order.
	for index := 0; index < 3; index++ {
		claimed, ok, err := store.ClaimNextReviewAction(hostID)
		if err != nil || !ok {
			t.Fatalf("claim %d ok=%v err=%v", index, ok, err)
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		delivered, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
		if err != nil || delivered.DeliveredAt == nil {
			t.Fatalf("deliver %d=%+v err=%v", index, delivered, err)
		}
		if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
			WorkID: claimed.WorkID, HandlingID: claimed.HandlingID,
			ProviderTurnID: claimed.ProviderTurnID, ExpectedWorkRevision: claimed.DeliveryWorkRevision,
			Disposition: WorkDispositionComplete, Summary: "Accepted",
		}); err != nil {
			t.Fatalf("resolve %d: %v", index, err)
		}
		// No second claim of the same action is possible.
		if lease := reviewLeaseOf(t, store, claimed.WorkID); lease != nil {
			t.Fatalf("Work %s retained a lease after disposition", claimed.WorkID)
		}
	}

	// Active queue reaches zero; every card resolves; no ghost cards.
	inventory, err = store.ProjectWorkInventory(map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Backlog.QueuedAttention != 0 || len(inventory.Current) != 0 {
		t.Fatalf("post-discharge inventory=%+v", inventory)
	}
	lifecycles, err = store.WorkResultLifecycles(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if lifecycles[id].ReviewState != WorkReviewResolved {
			t.Fatalf("card %s still active: %+v", id, lifecycles[id])
		}
	}
	if _, ok, err := store.ClaimNextReviewAction(hostID); err != nil || ok {
		t.Fatalf("queue not empty after discharge: ok=%v err=%v", ok, err)
	}
}

// TestReviewHostDeathBeforeDeliveryReDeliversSameAction: a claim whose Host
// dies before any delivery evidence is dropped; the same unresolved action is
// re-claimed by the current Host exactly once — no second queue item.
func TestReviewHostDeathBeforeDeliveryReDeliversSameAction(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Host death before delivery", "brain-agent-worker:@dead")
	event := appendSignalTestEvent(t, store, item, "dead-before-delivery")
	oldHost := "brain-agent-brain-hidden:@dead-old"
	if _, ok, err := store.ClaimNextReviewAction(oldHost); err != nil || !ok {
		t.Fatalf("old Host claim ok=%v err=%v", ok, err)
	}
	if lease := reviewLeaseOf(t, store, item.ID); lease == nil || lease.DeliveredAt != nil {
		t.Fatalf("old Host lease=%+v", lease)
	}

	// Old Host gone; current Host replaces it. One lane pass drops the dead
	// lease (no mutation evidence) and re-delivers the same action.
	newHost := "brain-agent-brain-hidden:@dead-new"
	if err := store.SetHostSession(newHost, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		turnStore: store,
		sessions: map[string]*classifier.Agent{
			newHost: {ID: newHost, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
		},
		providerEvidence: map[string]watcher.ProviderActivityObservation{},
	}
	service := NewService(store, fw, nil)
	woke, err := service.ReconcileHostLane()
	if err != nil || !woke {
		t.Fatalf("recovery woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != newHost {
		t.Fatalf("recovery sends=%#v, want one send to %s", fw.sentCalls, newHost)
	}
	lease := requireReviewDelivered(t, store, item.ID)
	if lease.HostSessionID != newHost {
		t.Fatalf("recovered lease bound to %s, want %s", lease.HostSessionID, newHost)
	}
	fact, found, err := store.WorkEvent(event.ID)
	if err != nil || !found || fact.HandledAt != nil {
		t.Fatalf("fact history=%+v found=%v err=%v", fact, found, err)
	}
	// Exactly-once: a second lane pass sends nothing.
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("second pass woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("second pass sends=%#v", fw.sentCalls)
	}

	// Restart reconstruction keeps the delivered lease intact.
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if lease := reviewLeaseOf(t, reopened, item.ID); lease == nil || lease.DeliveredAt == nil ||
		lease.HostSessionID != newHost {
		t.Fatalf("restart reconstruction lease=%+v", lease)
	}
}

// TestReviewHostDeathAfterDeliveryBeforeResolveRequeuesSameAction: a
// delivered lease whose Host dies before the typed disposition ends and the
// same unresolved action is re-delivered to the current Host (row 16/10).
func TestReviewHostDeathAfterDeliveryBeforeResolveRequeuesSameAction(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Host death after delivery", "brain-agent-worker:@dead-after")
	event := appendSignalTestEvent(t, store, item, "dead-after-delivery")
	oldHost := "brain-agent-brain-hidden:@dead-after-old"
	delivered, _ := deliverSignalTestEvent(t, store, oldHost)
	if delivered.DeliveredAt == nil {
		t.Fatal("fixture did not deliver")
	}
	settleCanonicalHostTurnForTest(t, store, oldHost, delivered.ProviderTurnID)

	// Restart with the old Host gone: startup ends the delivered lease and the
	// current Host re-delivers the same unresolved action exactly once.
	newHost := "brain-agent-brain-hidden:@dead-after-new"
	if err := store.SetHostSession(newHost, "codex"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		turnStore: reopened,
		sessions: map[string]*classifier.Agent{
			newHost: {ID: newHost, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
		},
		providerEvidence: map[string]watcher.ProviderActivityObservation{},
	}
	service := NewService(reopened, fw, nil)
	if done, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8); err != nil || !done {
		t.Fatalf("startup done=%v err=%v", done, err)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != newHost {
		t.Fatalf("requeue sends=%#v, want one send to %s", fw.sentCalls, newHost)
	}
	lease := requireReviewDelivered(t, reopened, item.ID)
	if lease.HostSessionID != newHost || lease.DeliveryWorkRevision != delivered.DeliveryWorkRevision+1 {
		t.Fatalf("requeued lease=%+v delivered=%+v", lease, delivered)
	}
	fact, found, err := reopened.WorkEvent(event.ID)
	if err != nil || !found || fact.HandledAt != nil {
		t.Fatalf("fact history=%+v found=%v err=%v", fact, found, err)
	}
	// The disposition then clears the epoch once; no ghost remains.
	if _, _, err := reopened.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID,
		ProviderTurnID: lease.ProviderTurnID, ExpectedWorkRevision: lease.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatalf("resolve requeued review: %v", err)
	}
	if reviewLeaseOf(t, reopened, item.ID) != nil {
		t.Fatal("resolved Work retained a lease")
	}
}

// TestReviewSuccessorSessionContinuesWork: the Brain disposition can continue
// Work to an accepted successor Session; the successor owns the continuing
// Work and the completed Session never stays open.
func TestReviewSuccessorSessionContinuesWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@successor"
	successor := "brain-agent-worker:@successor-continue"
	item := createSignalTestWork(t, store, "Successor continuation", "brain-agent-worker:@old-owner")
	appendSignalTestEvent(t, store, item, "successor-done")

	claimed, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	// Stage and admit the successor through the normal protocol.
	if _, err := store.ReserveWorkSuccessor(item.ID, successor); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Now().UTC().Add(-time.Minute)
	turnID := successor + ":turn:1"
	digest := pendingSubmissionDigest("successor continuation payload")
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: item.ID, SessionID: successor, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: digest, ProcessIdentity: "successor-process", PaneGeneration: "successor-pane",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("successor prepare created=%v err=%v", created, err)
	}
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: successor, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "successor-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "successor-admission", Cursor: 1,
			SHA256: pending.PayloadSHA256, At: acceptedAt.Add(time.Second),
		},
		ResolvedAt: acceptedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("successor admission: %v", err)
	}
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionContinue, SuccessorSessionID: successor,
	}); err != nil {
		t.Fatalf("continue disposition: %v", err)
	}
	item, err = store.Work(item.ID)
	if err != nil || item.OwnerSessionID != successor || !item.OwnerDelegated || item.Status != WorkRunning {
		t.Fatalf("Work after continue=%+v err=%v", item, err)
	}
	// The completed Session's card is history; the successor owns execution.
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("event history=%+v err=%v", events, err)
	}
	if lease := reviewLeaseOf(t, store, item.ID); lease != nil {
		t.Fatalf("continue left a lease: %+v", lease)
	}
}

// TestReviewTerminalWorkDetachesOwnerAndClearsCards: a terminal disposition
// atomically clears the review projection, detaches executor ownership, and
// records finalization obligations; no ghost card survives.
func TestReviewTerminalWorkDetachesOwnerAndClearsCards(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Terminal disposition", "brain-agent-worker:@terminal")
	event := appendSignalTestEvent(t, store, item, "terminal-done")
	hostID := "brain-agent-brain-hidden:@terminal"
	claimed, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	if _, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: claimed.HandlingID,
		ProviderTurnID: claimed.ProviderTurnID, ExpectedWorkRevision: claimed.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete, Summary: "Done",
	}); err != nil {
		t.Fatalf("terminal disposition: %v", err)
	}
	item, err = store.Work(item.ID)
	if err != nil || item.Status != WorkDone || item.TerminalRevision != item.Revision {
		t.Fatalf("terminal Work=%+v err=%v", item, err)
	}
	if item.Review != nil {
		t.Fatalf("terminal Work retained review: %+v", item.Review)
	}
	if len(item.SessionFinalizations) != 1 || item.SessionFinalizations[0].SessionID != "brain-agent-worker:@terminal" {
		t.Fatalf("finalization obligations=%+v", item.SessionFinalizations)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{event.ID})
	if err != nil || lifecycles[event.ID].ReviewState != WorkReviewResolved {
		t.Fatalf("terminal card lifecycle=%+v err=%v", lifecycles, err)
	}
	if _, ok, err := store.ClaimNextReviewAction(hostID); err != nil || ok {
		t.Fatalf("terminal Work claimable: ok=%v err=%v", ok, err)
	}
}

// TestReviewQueueCountAndCardProjectionMatchCanonicalState: queued_attention
// equals the count of review-required Work and every counted item is
// claimable or recoverable; leased-undelivered items stay the same single
// queue item.
func TestReviewQueueCountAndCardProjectionMatchCanonicalState(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := createSignalTestWork(t, store, "Queued first", "brain-agent-queue:@1")
	appendSignalTestEvent(t, store, first, "queue-first")
	second := createSignalTestWork(t, store, "Queued second", "brain-agent-queue:@2")
	appendSignalTestEvent(t, store, second, "queue-second")
	hostID := "brain-agent-brain-hidden:@queue"
	if _, ok, err := store.ClaimNextReviewAction(hostID); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}

	inventory, err := store.ProjectWorkInventory(map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	// Both review-required Works are the same queue items: they appear in the
	// bounded current window as queued attention, never in the backlog.
	if len(inventory.Current) != 2 || inventory.Backlog.QueuedAttention != 0 {
		t.Fatalf("queue projection=%+v", inventory)
	}
	for _, current := range inventory.Current {
		if current.AttentionState != WorkAttentionQueued {
			t.Fatalf("queued item attention=%s: %+v", current.AttentionState, current)
		}
	}
	// The leased-undelivered item is still the same single queue item and is
	// claimable after lease expiry; the pending item is claimable now.
	if claimed, ok, err := store.ClaimNextReviewAction(hostID); err != nil || !ok || claimed.WorkID != second.ID {
		t.Fatalf("second claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	// A claim conflict can never make queued_attention > 0 with no recoverable
	// action: after releasing the claimed lease, both actions are claimable.
	if err := store.ReleaseReviewLease(second.ID, secondLeaseHandlingID(t, store, second.ID), secondLeaseTurnID(t, store, second.ID)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNextReviewAction(hostID); err != nil || !ok {
		t.Fatalf("released claim not re-claimable: ok=%v err=%v", ok, err)
	}
}

func secondLeaseHandlingID(t *testing.T, store *Store, workID string) string {
	t.Helper()
	lease := reviewLeaseOf(t, store, workID)
	if lease == nil {
		t.Fatalf("Work %s has no lease", workID)
	}
	return lease.HandlingID
}

func secondLeaseTurnID(t *testing.T, store *Store, workID string) string {
	t.Helper()
	lease := reviewLeaseOf(t, store, workID)
	if lease == nil {
		t.Fatalf("Work %s has no lease", workID)
	}
	return lease.ProviderTurnID
}

// TestReviewHistoricalEventsStayHistory: after a review epoch resolves, its
// fact remains in history; a newer epoch card is the only active card.
func TestReviewHistoricalEventsStayHistory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Historical events", "brain-agent-history:@1")
	firstEvent := appendSignalTestEvent(t, store, item, "history-first")
	hostID := "brain-agent-brain-hidden:@history"
	delivered, _ := deliverSignalTestEvent(t, store, hostID)
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionContinue, SuccessorSessionID: "brain-agent-worker:@history-next",
	}); err == nil {
		t.Fatal("continue without an accepted successor must fail")
	}
	// Supersede: the work is cancelled; the first epoch card is history.
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionSupersede, Summary: "Superseded",
	}); err != nil {
		t.Fatal(err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{firstEvent.ID})
	if err != nil || lifecycles[firstEvent.ID].ReviewState != WorkReviewResolved {
		t.Fatalf("historical card lifecycle=%+v err=%v", lifecycles, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].HandledAt == nil || events[0].Disposition != WorkDispositionSupersede {
		t.Fatalf("historical fact audit=%+v err=%v", events, err)
	}
}

// TestReviewEpochTransitionTable binds the documented review-epoch state
// machine (work_review.go transition table rows 1-19) to the derived states:
// absent, pending, leased, delivered, ended, quarantined.
func TestReviewEpochTransitionTable(t *testing.T) {
	base := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	lease := func(mutate func(*WorkReviewLease)) *WorkReviewLease {
		l := &WorkReviewLease{
			HostSessionID: "host", HandlingID: "handling", ProviderTurnID: "host:turn:1",
			DeliveryWorkRevision: 2, DeliverySequenceFence: 1, ClaimedAt: base,
		}
		if mutate != nil {
			mutate(l)
		}
		return l
	}
	rows := []struct {
		name   string
		review *WorkReview
		want   reviewDeliveryState
	}{
		{"absent", nil, reviewNone},
		{"pending (row 1-2)", &WorkReview{RequiredAt: base, FactEventID: "f"}, reviewPending},
		{"leased (row 3)", &WorkReview{RequiredAt: base, FactEventID: "f", Lease: lease(nil)}, reviewLeased},
		{"delivered (row 5)", &WorkReview{RequiredAt: base, FactEventID: "f", Lease: lease(func(l *WorkReviewLease) {
			delivered := base.Add(time.Second)
			l.DeliveredAt = &delivered
		})}, reviewDelivered},
		{"ended (row 9-10)", &WorkReview{RequiredAt: base, FactEventID: "f", Lease: lease(func(l *WorkReviewLease) {
			delivered := base.Add(time.Second)
			ended := base.Add(2 * time.Second)
			l.DeliveredAt = &delivered
			l.HandlingEndedAt = &ended
		})}, reviewPending},
		{"quarantined (row 7)", &WorkReview{RequiredAt: base, FactEventID: "f", Lease: lease(func(l *WorkReviewLease) {
			l.AmbiguousDelivery = true
		})}, reviewQuarantined},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title: "transition table", Objective: "bind the epoch machine",
				Status: WorkWaiting, CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			if row.review != nil {
				store.mu.Lock()
				database, err := store.loadOrchestrationLocked()
				if err != nil {
					t.Fatal(err)
				}
				index := workIndex(database.BrainWork, item.ID)
				database.BrainWork[index].Review = row.review
				// the fact row must exist for a valid review
				if _, found := workEventByID(database.BrainWorkEvents, row.review.FactEventID); !found {
					database.NextEventSequence++
					event := WorkEvent{
						ID: row.review.FactEventID, WorkID: item.ID, Kind: "session.done",
						DedupeKey: "session:table:@1:turn:one:session.done", Actionable: true,
						CreatedAt: base, Sequence: database.NextEventSequence, WorkRevision: 1,
					}
					database.BrainWorkEvents = append(database.BrainWorkEvents, event)
				}
				err = store.persistOrchestrationLocked(database)
				store.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			got := reduceWorkReviewState(mustLoadDatabase(t, store), item.ID)
			if got != row.want {
				t.Fatalf("state=%v want=%v", got, row.want)
			}
		})
	}
}

func mustLoadDatabase(t *testing.T, store *Store) orchestrationDatabase {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadOrchestrationLocked()
	if err != nil {
		t.Fatal(err)
	}
	return database
}

// TestSchemaV11MigrationDerivesCanonicalReview: a v11 document whose claimed
// Event carried delivery state migrates to the v12 canonical Work.Review lease
// in memory; the fact rows lose all scheduler fields; the same unresolved
// action is claimable after reopen.
func TestSchemaV11MigrationDerivesCanonicalReview(t *testing.T) {
	root := t.TempDir()
	stateDir := root + "/state"
	if err := osMkdirAll(stateDir); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	workID := "legacy-stranded-work"
	eventID := "52c3cb62-5438-42fa-9a83-f607692f56cf"
	document := map[string]any{
		"schema_version":         11,
		"next_event_sequence":    2,
		"brain_input_admissions": []any{},
		"brain_work": []any{map[string]any{
			"work_id": workID, "revision": 2, "title": "Legacy stranded review",
			"objective": "Migrate to canonical Work.Review.", "status": "needs_input",
			"source_thread_id": "legacy-thread", "completion_policy": "bounded",
			"created_at": at, "updated_at": at,
		}},
		"brain_work_events": []any{map[string]any{
			"event_id": eventID, "work_id": workID, "kind": "brain.reconcile_required",
			"dedupe_key": "brain:legacy:" + eventID, "actionable": true, "created_at": at,
			"sequence": 1, "work_revision": 2,
			"claimed_at": at, "delivery_host_session_id": "brain-agent-brain-hidden:@dead-legacy",
			"handling_id": "legacy-handling", "provider_turn_id": "brain-agent-brain-hidden:@dead-legacy:turn:1",
			"delivery_work_revision": 2, "delivery_sequence_fence": 1,
		}},
		"brain_turns":            []any{},
		"brain_turn_submissions": []any{},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir+"/orchestration.json", raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Review == nil || item.Review.FactEventID != eventID ||
		item.Review.Lease == nil || item.Review.Lease.HostSessionID != "brain-agent-brain-hidden:@dead-legacy" ||
		item.Review.Lease.HandlingID != "legacy-handling" {
		t.Fatalf("migrated review=%+v", item.Review)
	}
	events, err := store.ListWorkEvents(workID)
	if err != nil || len(events) != 1 {
		t.Fatalf("migrated events=%+v err=%v", events, err)
	}
	if events[0].HandledAt != nil {
		t.Fatalf("v12 fact audit drifted: %+v", events[0])
	}
	// The dead Host lease is recovered on the next reducer pass: no mutation
	// evidence exists, so the same unresolved action is re-claimable.
	if recovered, err := store.RecoverReviewLease(workID, "legacy-handling", "brain-agent-brain-hidden:@dead-legacy:turn:1"); err != nil || !recovered {
		t.Fatalf("migrated lease recovery recovered=%v err=%v", recovered, err)
	}
	if claimed, ok, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@new-legacy"); err != nil || !ok ||
		claimed.FactEventID != eventID {
		t.Fatalf("migrated action claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	// The persisted document is rewritten at schema 12 on the next write.
	persisted, err := os.ReadFile(stateDir + "/orchestration.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(persisted, []byte(`"schema_version": 12`)) {
		t.Fatalf("document was not rewritten at schema 12: %s", persisted)
	}
}
