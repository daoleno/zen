package brain

// fresh_signal_lifecycle_test.go is the fresh-state production-path contract
// for the signal system (2026-08-11 user reset). Every test starts from an
// empty current-schema store and walks the one direct lifecycle: create Work,
// append one actionable Event, atomically claim it, admit the exact provider
// turn once, record one typed disposition, and advance or terminalize Work.
// No legacy fixture, migration, or compatibility branch appears anywhere.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// freshHostID is the single Host lane identity used by the fresh-path tests.
const freshHostID = "brain-agent-brain-hidden:@fresh-core"

// TestFreshSignalInitialDelegationLifecycle is the whole fresh lifecycle in
// one pass: create -> append -> claim -> admit the exact provider turn once ->
// deliver -> record one typed disposition -> terminalize. A duplicate
// disposition is rejected, and reopen leaves no replayable signal.
func TestFreshSignalInitialDelegationLifecycle(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Fresh delegation", "brain-agent-fresh-delegation:@1")
	event := appendSignalTestEvent(t, store, item, "fresh-1")
	currentWork, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := store.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || claimed.FactEventID != event.ID {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	if claimed.ClaimedAt == nil || claimed.DeliveryHostSessionID != freshHostID ||
		claimed.HandlingID == "" || claimed.ProviderTurnID == "" ||
		claimed.HandlingID == claimed.ProviderTurnID ||
		claimed.DeliveryWorkRevision != currentWork.Revision || claimed.DeliverySequenceFence == 0 {
		t.Fatalf("claim identity is incomplete: %+v", claimed)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, current, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.DeliveredAt == nil || delivered.DeliveryWorkRevision != current.Revision {
		t.Fatalf("delivery boundary broken: event=%+v Work=%+v", delivered, current)
	}
	fact, found, err := store.WorkEvent(delivered.FactEventID)
	if err != nil || !found || fact.HandledAt != nil {
		t.Fatalf("delivery boundary fact=%+v found=%v err=%v", fact, found, err)
	}
	resolved, terminal, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
		Summary:              "Delegated result accepted.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.HandledAt == nil || resolved.Disposition != WorkDispositionComplete ||
		terminal.Status != WorkDone || terminal.TerminalRevision != terminal.Revision {
		t.Fatalf("disposition did not settle the claimed capability: event=%+v Work=%+v", resolved, terminal)
	}
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); !errors.Is(err, ErrEventHandled) {
		t.Fatalf("duplicate disposition err=%v, want ErrEventHandled", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if replay, claimed, err := reopened.ClaimNextReviewAction(freshHostID); err != nil || claimed {
		t.Fatalf("terminal Work replayed a signal: event=%+v claimed=%v err=%v", replay, claimed, err)
	}
	durable, err := reopened.Work(item.ID)
	if err != nil || durable.Status != WorkDone {
		t.Fatalf("reopened terminal Work=%+v err=%v", durable, err)
	}
}

// TestFreshSignalFollowUpOnActiveWorkAppendsDuringHandling: a follow-up
// signal appended while the Host handling is delivered belongs to the next
// revision epoch, is never consumed by the current disposition, and becomes
// the next claim with the exact revision fence.
func TestFreshSignalFollowUpOnActiveWorkAppendsDuringHandling(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Follow-up on active Work", "brain-agent-followup:@1")
	appendSignalTestEvent(t, store, item, "followup-1")
	delivered, _ := deliverSignalTestEvent(t, store, freshHostID)
	before, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	later, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "review.changed",
		DedupeKey: "review:followup:changed", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append follow-up created=%v err=%v", created, err)
	}
	afterAppend, err := store.Work(item.ID)
	if err != nil || afterAppend.Revision != before.Revision || later.WorkRevision != before.Revision+1 {
		t.Fatalf("follow-up stole the delivered revision epoch: before=%+v after=%+v event=%+v err=%v", before, afterAppend, later, err)
	}
	resolved, terminal, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone {
		t.Fatalf("current disposition event=%+v Work=%+v err=%v", resolved, terminal, err)
	}
	durableLater, found, err := store.WorkEvent(later.ID)
	if err != nil || !found || durableLater.HandledAt != nil || durableLater.WorkRevision != terminal.Revision {
		t.Fatalf("follow-up consumed with the prior epoch: event=%+v found=%v err=%v", durableLater, found, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// The follow-up fact appended during the lease belongs to the next
	// revision epoch (WorkRevision == TerminalRevision): it re-requires the
	// same queue item once, never a duplicate card (row 19).
	next, ok, err := reopened.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || next.FactEventID != later.ID || next.DeliveryWorkRevision != terminal.Revision {
		t.Fatalf("next epoch claim=%+v ok=%v err=%v", next, ok, err)
	}
}

// TestFreshSignalIndependentConcurrentWorks: several independent Works share
// one fair FIFO queue; each is claimed, admitted exactly once, and disposed
// without any cross-Work interference.
func TestFreshSignalIndependentConcurrentWorks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type workFixture struct {
		item  Work
		event WorkEvent
	}
	fixtures := []workFixture{}
	for index := 0; index < 4; index++ {
		item := createSignalTestWork(t, store, "Concurrent Work", "brain-agent-concurrent-"+string(rune('a'+index))+":@1")
		event := appendSignalTestEvent(t, store, item, "concurrent-"+string(rune('a'+index)))
		fixtures = append(fixtures, workFixture{item: item, event: event})
	}
	for index, fixture := range fixtures {
		claimed, ok, err := store.ClaimNextReviewAction(freshHostID)
		if err != nil || !ok || claimed.WorkID != fixture.event.WorkID || claimed.FactEventID != fixture.event.ID {
			t.Fatalf("claim %d = %+v ok=%v err=%v, want Event %s", index, claimed, ok, err, fixture.event.ID)
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		delivered, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
			WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
			ProviderTurnID:       delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision,
			Disposition:          WorkDispositionComplete,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range fixtures {
		durable, err := store.Work(fixture.item.ID)
		if err != nil || durable.Status != WorkDone {
			t.Fatalf("independent Work %s did not terminalize: %+v err=%v", fixture.item.ID, durable, err)
		}
	}
}

// TestFreshSignalDuplicateNotificationIsIdempotent: a duplicate append
// returns the existing row, a duplicate requeue is a no-op, and a replayed
// provider fact is deduped by its deterministic identity.
func TestFreshSignalDuplicateNotificationIsIdempotent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Duplicate notification", "brain-agent-duplicate:@1")
	original := appendSignalTestEvent(t, store, item, "duplicate-1")
	duplicate, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:" + item.OwnerSessionID + ":turn:duplicate-1:session.done",
		PayloadRef: "session:" + item.OwnerSessionID,
		SourceName: item.OwnerSessionID, Actionable: true,
	})
	if err != nil || created || duplicate.ID != original.ID {
		t.Fatalf("duplicate append created=%v event=%+v err=%v", created, duplicate, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("duplicate append added a row: %+v err=%v", events, err)
	}
	delivered, _ := deliverSignalTestEvent(t, store, freshHostID)
	requeued, created, err := store.EndReviewDelivery(
		delivered.WorkID, delivered.HandlingID, delivered.ProviderTurnID,
	)
	if err != nil || !created || requeued.FactEventID != delivered.FactEventID {
		t.Fatalf("first requeue created=%v event=%+v err=%v", created, requeued, err)
	}
	lease := reviewLeaseOf(t, store, item.ID)
	if lease == nil || lease.HandlingEndedAt == nil {
		t.Fatalf("first requeue did not end the lease: %+v", lease)
	}
	// The ended lease makes a second requeue an exact no-op: the same
	// unresolved action is returned, never a duplicate queue item.
	if again, created, err := store.EndReviewDelivery(
		delivered.WorkID, delivered.HandlingID, delivered.ProviderTurnID,
	); err != nil || created || again.FactEventID != delivered.FactEventID {
		t.Fatalf("duplicate requeue created=%v event=%+v err=%v", created, again, err)
	}
	events, err = store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("requeue history=%+v err=%v", events, err)
	}
}

// TestFreshSignalAmbiguousAdmissionNeverReplaysAndDoesNotWedgeUnrelatedWork:
// Work A's exact admission stays ambiguous (held, never released, never
// replayed) while unrelated Work B is claimed and delivered through the
// replacement generation.
func TestFreshSignalAmbiguousAdmissionNeverReplaysAndDoesNotWedgeUnrelatedWork(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(freshHostID, "codex"); err != nil {
		t.Fatal(err)
	}
	workA := createSignalTestWork(t, store, "Ambiguous A", "brain-agent-ambiguous-a:@1")
	eventA := appendSignalTestEvent(t, store, workA, "ambiguous-a")
	workB := createSignalTestWork(t, store, "Unrelated B", "brain-agent-unrelated-b:@1")
	eventB := appendSignalTestEvent(t, store, workB, "unrelated-b")

	claimA, claimed, err := store.ClaimNextReviewAction(freshHostID)
	if err != nil || !claimed || claimA.FactEventID != eventA.ID {
		t.Fatalf("claim A=%+v claimed=%v err=%v", claimA, claimed, err)
	}
	oldAcceptedAt := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	oldPending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimA.WorkID, SessionID: freshHostID, ProposedTurnID: claimA.ProviderTurnID,
		Receipt: claimA.FactEventID, ClaimToken: claimA.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("ambiguous A payload"),
		ProcessIdentity: "old-host-process", PaneGeneration: "old-host-pane",
		AcceptedAt: oldAcceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare A created=%v submission=%+v err=%v", created, oldPending, err)
	}

	// A held claim never blocks the unrelated claim at the store boundary.
	claimB, ok, err := store.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || claimB.FactEventID != eventB.ID {
		t.Fatalf("unrelated claim while A held=%+v ok=%v err=%v", claimB, ok, err)
	}
	// The replacement provider generation delivers B while A's ambiguous
	// transaction stays pending: it is never replayed, never released, and
	// never blocks the unrelated transaction.
	delivery := newCanonicalHostDeliveryWatcher(store, freshHostID)
	delivery.outcomes = map[string]watcher.InputOutcome{claimA.FactEventID: watcher.InputAmbiguous}
	woke, err := NewService(store, delivery, nil).ReconcileHostLane()
	if err != nil || !woke {
		t.Fatalf("replacement lane woke=%v err=%v", woke, err)
	}
	oldDurable, found, err := store.TurnSubmission(freshHostID, claimA.ProviderTurnID)
	if err != nil || !found || oldDurable.State != watcher.TurnSubmissionPending {
		t.Fatalf("ambiguous authority was settled without exact evidence: %+v found=%v err=%v", oldDurable, found, err)
	}
	heldA, found, err := store.WorkEvent(eventA.ID)
	if err != nil || !found || heldA.Resolution != "" {
		t.Fatalf("ambiguous A audit drifted: event=%+v found=%v err=%v", heldA, found, err)
	}
	if leaseA := reviewLeaseOf(t, store, workA.ID); leaseA == nil || leaseA.DeliveredAt != nil {
		t.Fatalf("ambiguous A was replayed or released: %+v", leaseA)
	}
	if leaseB := reviewLeaseOf(t, store, workB.ID); leaseB == nil || leaseB.DeliveredAt == nil {
		t.Fatalf("unrelated B did not proceed: %+v", leaseB)
	}
}

// TestFreshSignalDeliveredCapabilityDoesNotWedgeUnrelatedWork: Work A's
// delivered handling (capability delivered, disposition outstanding) never
// wedges the claim of unrelated Work B; the one Host lane serialization stays
// in the reducer, and a reopen with an ended Host turn requeues A without
// replay so B is admitted first.
func TestFreshSignalDeliveredCapabilityDoesNotWedgeUnrelatedWork(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(freshHostID, "codex"); err != nil {
		t.Fatal(err)
	}
	workA := createSignalTestWork(t, store, "Delivered A", "brain-agent-delivered-a:@1")
	appendSignalTestEvent(t, store, workA, "delivered-a")
	workB := createSignalTestWork(t, store, "Unrelated B", "brain-agent-unrelated-b2:@1")
	eventB := appendSignalTestEvent(t, store, workB, "unrelated-b2")
	deliveredA, _ := deliverSignalTestEvent(t, store, freshHostID)
	if live, err := store.HasLiveDeliveredReview(); err != nil || !live {
		t.Fatalf("delivered handling not live: live=%v err=%v", live, err)
	}
	// The store-level claim boundary is per Work: B is claimable while A's
	// delivered capability is outstanding. The reducer still admits one
	// delivered handling at a time.
	claimB, ok, err := store.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || claimB.FactEventID != eventB.ID {
		t.Fatalf("unrelated claim while A delivered=%+v ok=%v err=%v", claimB, ok, err)
	}

	// Crash while A's delivered capability is outstanding and its Host turn
	// ended: startup reconciliation requeues A without replay, and the
	// unrelated Work B is admitted first.
	hostTurn, found, err := store.Turn(freshHostID)
	if err != nil || !found {
		t.Fatalf("Host Turn=%+v found=%v err=%v", hostTurn, found, err)
	}
	settleCanonicalHostTurnForTest(t, store, freshHostID, hostTurn.TurnID)
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: reopened, sessions: map[string]*classifier.Agent{
		freshHostID: {ID: freshHostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(reopened, fw, nil)
	if complete, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8); err != nil || !complete {
		t.Fatalf("startup complete=%v err=%v", complete, err)
	}
	requeuedA, found, err := reopened.WorkEvent(deliveredA.FactEventID)
	if err != nil || !found {
		t.Fatalf("delivered A fact missing: event=%+v found=%v err=%v", requeuedA, found, err)
	}
	if leaseA := reviewLeaseOf(t, reopened, workA.ID); leaseA == nil || leaseA.HandlingEndedAt == nil {
		t.Fatalf("delivered A was not requeued: %+v", leaseA)
	}
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, `"work_id":"`+workB.ID+`"`) {
		t.Fatalf("unrelated B was not admitted first after reopen: %+v", fw.sentCalls)
	}
	// B's stale pre-crash claim was released by exact receipt reconciliation;
	// the durable Event remains the same and is never duplicated.
	durableB, found, err := reopened.WorkEvent(eventB.ID)
	if err != nil || !found {
		t.Fatalf("B fact missing: event=%+v found=%v err=%v", durableB, found, err)
	}
	if leaseB := reviewLeaseOf(t, reopened, workB.ID); leaseB == nil || leaseB.ClaimedAt.Equal(*claimB.ClaimedAt) {
		t.Fatalf("B claim did not converge: %+v", leaseB)
	}
}

// TestFreshSignalCrashReopenAfterClaimConverges: a claim persists across
// reopen with its exact five-part identity; a proved non-submission releases
// it, an ambiguous receipt holds it without replay, and the eventual
// disposition consumes it once.
func TestFreshSignalCrashReopenAfterClaimConverges(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Crash after claim", "brain-agent-crash-claim:@1")
	event := appendSignalTestEvent(t, store, item, "crash-claim")
	claimed, ok, err := store.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || claimed.FactEventID != event.ID {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	held, found, err := reopened.WorkEvent(event.ID)
	if err != nil || !found {
		t.Fatalf("reopened claim fact missing: event=%+v found=%v err=%v", held, found, err)
	}
	if lease := reviewLeaseOf(t, reopened, item.ID); lease == nil || lease.HandlingID != claimed.HandlingID ||
		lease.ProviderTurnID != claimed.ProviderTurnID || lease.DeliveryWorkRevision != claimed.DeliveryWorkRevision {
		t.Fatalf("reopened claim identity drifted: %+v", lease)
	}
	// Proved non-submission releases the exact claim; the event is claimable
	// again with a fresh capability.
	if err := reopened.ReleaseReviewLease(event.WorkID, claimed.HandlingID, claimed.ProviderTurnID); err != nil {
		t.Fatalf("release proved-unsent claim: %v", err)
	}
	reclaimed, ok, err := reopened.ClaimNextReviewAction(freshHostID)
	if err != nil || !ok || reclaimed.FactEventID != event.ID || reclaimed.HandlingID == claimed.HandlingID {
		t.Fatalf("reclaim=%+v ok=%v err=%v", reclaimed, ok, err)
	}
	resolveClaimedHostTurnForTest(t, reopened, reclaimed)
	delivered, _, err := reopened.ConsumeReviewDelivery(reclaimed.WorkID, reclaimed.HandlingID, reclaimed.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFreshSignalCrashReopenAtDispositionPersistence: a persistence failure
// at the disposition boundary leaves no partial Work/Event/submission state;
// reopen converges and the exact disposition succeeds exactly once.
func TestFreshSignalCrashReopenAtDispositionPersistence(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Crash at disposition", "brain-agent-crash-disposition:@1")
	appendSignalTestEvent(t, store, item, "crash-disposition")
	delivered, _ := deliverSignalTestEvent(t, store, freshHostID)
	request := WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return errors.New("injected disposition persistence failure") }
	if _, _, err := store.ResolveWorkReview(request); err == nil {
		t.Fatal("disposition persistence failure was reported successful")
	}
	store.writeOrchestration = originalWrite
	beforeDisposition, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.Work(item.ID)
	if err != nil || unchanged.Status == WorkDone || unchanged.Revision != beforeDisposition.Revision {
		t.Fatalf("failed disposition mutated Work: %+v err=%v", unchanged, err)
	}
	unchangedEvent, found, err := store.WorkEvent(delivered.FactEventID)
	if err != nil || !found || unchangedEvent.HandledAt != nil || unchangedEvent.Disposition != "" {
		t.Fatalf("failed disposition settled the Event: %+v found=%v err=%v", unchangedEvent, found, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	durable, found, err := reopened.WorkEvent(delivered.FactEventID)
	if err != nil || !found || durable.HandledAt != nil {
		t.Fatalf("reopened delivered handling drifted: %+v found=%v err=%v", durable, found, err)
	}
	if lease := reviewLeaseOf(t, reopened, item.ID); lease == nil || lease.DeliveredAt == nil {
		t.Fatalf("reopened delivered lease drifted: %+v", lease)
	}
	resolved, terminal, err := reopened.ResolveWorkReview(request)
	if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone {
		t.Fatalf("retry disposition event=%+v Work=%+v err=%v", resolved, terminal, err)
	}
	if duplicate, claimed, err := reopened.ClaimNextReviewAction(freshHostID); err != nil || claimed {
		t.Fatalf("settled disposition replayed: event=%+v claimed=%v err=%v", duplicate, claimed, err)
	}
}

// TestFreshSignalExactTypedDispositionsSettleClaimedCapability: each of the
// five typed dispositions advances or terminalizes the Work exactly once and
// leaves no second claimable signal behind.
func TestFreshSignalExactTypedDispositionsSettleClaimedCapability(t *testing.T) {
	for _, test := range []struct {
		name        string
		disposition WorkDisposition
		wantStatus  WorkStatus
		wake        *WorkWake
		successor   string
	}{
		{name: "complete", disposition: WorkDispositionComplete, wantStatus: WorkDone},
		{name: "cancel", disposition: WorkDispositionCancel, wantStatus: WorkCancelled},
		{name: "supersede", disposition: WorkDispositionSupersede, wantStatus: WorkCancelled},
		{name: "wait", disposition: WorkDispositionWait, wantStatus: WorkWaiting,
			wake: &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:wait-thread"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item := createSignalTestWork(t, store, "Typed disposition", "brain-agent-typed-"+test.name+":@1")
			appendSignalTestEvent(t, store, item, "typed-"+test.name)
			delivered, _ := deliverSignalTestEvent(t, store, freshHostID)
			if test.disposition == WorkDispositionWait {
				// A wait requires the source thread identity.
				threadID, err := store.ChatThreadID()
				if err != nil {
					t.Fatal(err)
				}
				test.wake.Ref = "brain-thread:" + threadID
			}
			resolved, terminal, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
				WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          test.disposition,
				Wake:                 test.wake,
				SuccessorSessionID:   test.successor,
			})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.HandledAt == nil || resolved.Disposition != test.disposition ||
				terminal.Status != test.wantStatus {
				t.Fatalf("disposition event=%+v Work=%+v", resolved, terminal)
			}
			if test.wantStatus == WorkWaiting {
				if !workWakeEqual(terminal.Wake, test.wake) {
					t.Fatalf("wait disposition Work=%+v", terminal)
				}
			} else if terminal.TerminalRevision != terminal.Revision {
				t.Fatalf("terminal fence missing: %+v", terminal)
			}
			if replay, claimed, err := store.ClaimNextReviewAction(freshHostID); err != nil || claimed {
				t.Fatalf("settled capability replayed: event=%+v claimed=%v err=%v", replay, claimed, err)
			}
		})
	}
}
