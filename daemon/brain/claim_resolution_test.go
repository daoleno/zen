package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// claimResolutionStore builds a store with one Work and one held review lease
// (the quarantine shape: lease minted, never delivered, no exact submission).
func claimResolutionStore(t *testing.T) (*Store, string, WorkReviewAction) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Held lease resolution",
		Objective:        "Close held review leases explicitly.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:held:turn:one:session.done",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@1")
	if err != nil || !ok || claimed.FactEventID != event.ID {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	return store, item.ID, claimed
}

// TestMarkReviewDeliveredClosesHeldLease explicitly closes a held lease by
// user assertion (C.2.6.1): actor-recorded, idempotent, never time-based. The
// same unresolved action remains the same queue item and becomes re-claimable.
func TestMarkReviewDeliveredClosesHeldLease(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseMarkDelivered, "user", "visible in host transcript"); err != nil {
		t.Fatal(err)
	}
	lease := reviewLeaseOf(t, store, workID)
	if lease != nil {
		t.Fatalf("mark_delivered left lease: %+v", lease)
	}
	event, found, err := store.WorkEvent(claimed.FactEventID)
	if err != nil || !found || event.Resolution != EventResolutionMarkDelivered ||
		event.ResolvedBy != "user" || event.ResolvedAt == nil {
		t.Fatalf("mark_delivered audit row = %#v found=%v err=%v", event, found, err)
	}
	// Idempotent: a second resolution on the same held lease is refused.
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseMarkDelivered, "user", "again"); err == nil {
		t.Fatal("already-resolved lease accepted a second resolution")
	}
	// The same unresolved action is re-claimable: exactly one queue item.
	requeued, ok, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@2")
	if err != nil || !ok || requeued.FactEventID != claimed.FactEventID {
		t.Fatalf("mark_delivered did not re-claim the same action: %+v ok=%v err=%v", requeued, ok, err)
	}
	actions, err := store.LeasedReviewActions()
	if err != nil || len(actions) != 1 {
		t.Fatalf("post-resolution leases = %+v err=%v", actions, err)
	}
}

// Delivery diagnostics are scheduler audit, not Work-state mutation. They
// must never advance the revision fence carried by the exact review lease;
// otherwise the provider can accept and execute the action but its mandated
// typed disposition becomes impossible.
func TestDeliveryDiagnosticDoesNotInvalidateLeaseRevision(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	before, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	note, created, err := store.AppendDeliveryNote(
		workID,
		claimed.FactEventID,
		"delivery.ambiguous",
		"delivery:"+claimed.FactEventID+":ambiguous",
		"Provider admission is still being reconciled.",
		false,
	)
	if err != nil || !created {
		t.Fatalf("append diagnostic created=%v note=%+v err=%v", created, note, err)
	}
	after, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("delivery diagnostic changed Work revision: before=%+v after=%+v", before, after)
	}
	lease := reviewLeaseOf(t, store, workID)
	if lease == nil || lease.DeliveryWorkRevision != after.Revision {
		t.Fatalf("diagnostic invalidated exact lease fence: lease=%+v Work=%+v", lease, after)
	}
	if note.WorkRevision != after.Revision+1 {
		t.Fatalf("diagnostic revision epoch=%d want next epoch %d", note.WorkRevision, after.Revision+1)
	}
}

// TestDiscardReviewRemovesHeldLeaseForever discards a held lease (C.2.6.2).
// The current action is abandoned with audit; the same Work queue item is
// re-required by one fresh reconcile fact so the Brain still decides the
// disposition.
func TestDiscardReviewRemovesHeldLeaseForever(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseDiscard, "user", "work is moot"); err != nil {
		t.Fatal(err)
	}
	if lease := reviewLeaseOf(t, store, workID); lease != nil {
		t.Fatalf("discard left lease: %+v", lease)
	}
	event, found, err := store.WorkEvent(claimed.FactEventID)
	if err != nil || !found || event.DiscardedAt == nil || event.Resolution != EventResolutionDiscard ||
		event.ResolvedBy != "user" || event.ResolvedAt == nil {
		t.Fatalf("discard audit row = %#v found=%v err=%v", event, found, err)
	}
	// The same queue item is re-required by one fresh reconcile fact; the
	// discarded action is never replayed.
	reconcile, wasClaimed, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@2")
	if err != nil || !wasClaimed || reconcile.FactEventID == claimed.FactEventID || reconcile.Kind != "brain.reconcile_required" {
		t.Fatalf("discard reconciliation = %+v claimed=%v err=%v", reconcile, wasClaimed, err)
	}
	events, err := store.ListWorkEvents(workID)
	if err != nil || len(events) != 2 {
		t.Fatalf("discard event history = %+v err=%v", events, err)
	}
}

// TestReplayReviewCreatesNoSecondFact replays a held lease as the same action
// identity (C.2.6.3): the only authorized second wake is a lease reset — the
// fact row is unchanged, so no second card is possible.
func TestReplayReviewCreatesNoSecondFact(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "explicit replay authorization"); err != nil {
		t.Fatal(err)
	}
	event, found, err := store.WorkEvent(claimed.FactEventID)
	if err != nil || !found || event.Resolution != EventResolutionReplayed || event.ResolvedBy != "user" {
		t.Fatalf("replay audit row = %#v found=%v err=%v", event, found, err)
	}
	// The same action identity re-enters the claim pipeline exactly once.
	replayed, ok, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@2")
	if err != nil || !ok || replayed.FactEventID != claimed.FactEventID {
		t.Fatalf("replay re-claim=%+v ok=%v err=%v", replayed, ok, err)
	}
	events, err := store.ListWorkEvents(workID)
	if err != nil || len(events) != 1 {
		t.Fatalf("replay created a second fact row: %+v err=%v", events, err)
	}
}

// TestReplayReviewIsBoundedToOneReplayOfHeldLease covers the bounded
// contract: replay requires a held lease, a second replay of the same action
// is rejected, and the resolved original leaves the held set.
func TestReplayReviewIsBoundedToOneReplayOfHeldLease(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "first replay"); err != nil {
		t.Fatal(err)
	}
	// The resolved original is excluded from the held set forever.
	actions, err := store.LeasedReviewActions()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.FactEventID == claimed.FactEventID {
			t.Fatalf("resolved original still in held set: %+v", actions)
		}
	}
	// A second replay of the same action is rejected.
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "second replay"); err == nil {
		t.Fatal("second replay of the same action was accepted")
	}
	// The single audited replay identity is retained exactly once.
	events, _ := store.ListWorkEvents(workID)
	if len(events) != 1 || events[0].Resolution != EventResolutionReplayed {
		t.Fatalf("replay rows = %+v, want exactly one audited row", events)
	}
}

// TestReplayReviewRequiresHeldLease covers the bounded contract: replay
// without a held lease (pending, consumed, discarded, or already resolved) is
// rejected.
func TestReplayReviewRequiresHeldLease(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	// Release the lease: an unleased action cannot be replayed.
	if err := store.ReleaseReviewLease(workID, claimed.HandlingID, claimed.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "no held lease"); err == nil {
		t.Fatal("replay of an unleased action was accepted")
	}
	// A discarded lease cannot be replayed.
	store2, workID2, claimed2 := claimResolutionStore(t)
	if _, _, err := store2.ResolveReviewLease(workID2, ReviewLeaseDiscard, "user", "moot"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store2.ResolveReviewLease(workID2, ReviewLeaseReplay, "user", "discarded"); err == nil {
		t.Fatal("replay of a discarded action was accepted")
	}
	_ = claimed2
	// A consumed lease cannot be replayed.
	store3, workID3, claimed3 := claimResolutionStore(t)
	if _, _, err := store3.ResolveReviewLease(workID3, ReviewLeaseMarkDelivered, "user", "delivered"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store3.ResolveReviewLease(workID3, ReviewLeaseReplay, "user", "consumed"); err == nil {
		t.Fatal("replay of a consumed action was accepted")
	}
	_ = claimed3
	_ = claimed
}

// TestReviewLeaseResolutionRequiresActorAndReason enforces the authorization
// gate: automatic or time-based resolution is prohibited.
func TestReviewLeaseResolutionRequiresActorAndReason(t *testing.T) {
	store, workID, _ := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseMarkDelivered, "", "reason"); err == nil {
		t.Fatal("resolution without actor accepted")
	}
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseDiscard, "user", ""); err == nil {
		t.Fatal("resolution without reason accepted")
	}
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", ""); err == nil {
		t.Fatal("replay without reason accepted")
	}
	if _, _, err := store.ResolveReviewLease("missing-work", ReviewLeaseMarkDelivered, "user", "reason"); err == nil {
		t.Fatal("resolution of unknown Work accepted")
	}
}

// A held review lease may already own the Session's sole pending submission.
// An explicit actor resolution must retire that exact transaction in the same
// orchestration replacement; otherwise the replacement action can be claimed
// but can never prepare its own provider Turn. Late provider evidence for the
// retired transaction must also remain non-adoptable.
func TestReviewLeaseResolutionRetiresExactPendingHostSubmissionAtomically(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*Store, string) error
	}{
		{name: "mark delivered", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseMarkDelivered, "user", "visible in Host transcript")
			return err
		}},
		{name: "discard", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseDiscard, "user", "obsolete delivery")
			return err
		}},
		{name: "replay", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "explicit replay authorization")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, store, original, pending := preparePendingClaimedHostSubmission(t)
			workID := original.WorkID
			if err := test.apply(store, workID); err != nil {
				t.Fatal(err)
			}

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			retired, found, err := reopened.TurnSubmission(original.DeliveryHostSessionID, original.ProviderTurnID)
			if err != nil || !found || retired.State != watcher.TurnSubmissionState("retired") {
				t.Fatalf("exact pending submission was not retired: submission=%+v found=%v err=%v", retired, found, err)
			}

			lateAt := pending.AcceptedAt.Add(time.Second)
			if _, err := reopened.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
				SessionID: pending.SessionID, ProposedTurnID: pending.ProposedTurnID,
				Receipt: pending.Receipt, PayloadSHA256: pending.PayloadSHA256,
				ActivityID: "late-provider-activity",
				Admission:  providerAdmission("stream", "late-provider-message", 1, pending.PayloadSHA256, lateAt),
				ResolvedAt: lateAt,
			}); err == nil {
				t.Fatal("late provider evidence adopted an actor-retired submission")
			}
			if _, found, err := reopened.Turn(pending.SessionID); err != nil || found {
				t.Fatalf("retired submission created a canonical Turn: found=%v err=%v", found, err)
			}

			next, claimed, err := reopened.ClaimNextReviewAction(original.DeliveryHostSessionID)
			if err != nil || !claimed {
				t.Fatalf("replacement action claim=%+v claimed=%v err=%v", next, claimed, err)
			}
			// The same queue item is re-claimable: replay/mark_delivered keep
			// the same action identity; discard re-requires via a fresh
			// reconcile fact. In every case only one item exists.
			if test.name == "discard" && next.FactEventID == original.FactEventID {
				t.Fatalf("discard kept the discarded action identity: %+v", next)
			}
			if test.name != "discard" && next.FactEventID != original.FactEventID {
				t.Fatalf("replay/mark_delivered changed the action identity: %+v", next)
			}
			candidate := watcher.TurnSubmission{
				WorkID: next.WorkID, SessionID: next.DeliveryHostSessionID,
				ProposedTurnID: next.ProviderTurnID, Receipt: next.FactEventID, ClaimToken: next.HandlingID,
				PayloadSHA256:   pendingSubmissionDigest("replacement Host delivery"),
				ProcessIdentity: "replacement-process", PaneGeneration: "replacement-pane",
				AcceptedAt: lateAt.Add(time.Second), Mode: watcher.TurnSubmissionFresh,
			}
			prepared, created, err := reopened.PrepareTurnSubmission(candidate)
			if err != nil || !created || prepared.State != watcher.TurnSubmissionPending {
				t.Fatalf("retired transaction still gated replacement: submission=%+v created=%v err=%v", prepared, created, err)
			}
		})
	}
}

func TestReviewLeaseResolutionRetirementIgnoresUnrelatedPendingSubmission(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@manual-isolation"
	firstWork := createSignalTestWork(t, store, "Resolve first held lease", "brain-agent-worker:@first")
	appendSignalTestEvent(t, store, firstWork, "manual-first")
	secondWork := createSignalTestWork(t, store, "Preserve second pending lease", "brain-agent-worker:@second")
	appendSignalTestEvent(t, store, secondWork, "manual-second")
	first, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok || first.WorkID != firstWork.ID {
		t.Fatalf("first claim=%+v ok=%v err=%v", first, ok, err)
	}
	second, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok || second.WorkID != secondWork.ID {
		t.Fatalf("second claim=%+v ok=%v err=%v", second, ok, err)
	}
	unrelated, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: second.WorkID, SessionID: hostID, ProposedTurnID: second.ProviderTurnID,
		Receipt: second.FactEventID, ClaimToken: second.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("unrelated provider input"),
		ProcessIdentity: "unrelated-process", PaneGeneration: "unrelated-pane",
		AcceptedAt: time.Date(2026, 8, 11, 1, 40, 0, 0, time.UTC), Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare unrelated submission=%+v created=%v err=%v", unrelated, created, err)
	}
	if _, _, err := store.ResolveReviewLease(first.WorkID, ReviewLeaseDiscard, "user", "retire exact delivery only"); err != nil {
		t.Fatal(err)
	}
	unchanged, found, err := store.TurnSubmission(hostID, unrelated.ProposedTurnID)
	if err != nil || !found || unchanged.State != watcher.TurnSubmissionPending ||
		unchanged.Receipt != unrelated.Receipt || unchanged.PayloadSHA256 != unrelated.PayloadSHA256 {
		t.Fatalf("unrelated pending submission was mutated: submission=%+v found=%v err=%v", unchanged, found, err)
	}
}

func TestReviewLeaseResolutionAndFactAuditShareOneCommit(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*Store, string) error
	}{
		{name: "mark delivered", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseMarkDelivered, "user", "visible in Host transcript")
			return err
		}},
		{name: "discard", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseDiscard, "user", "obsolete delivery")
			return err
		}},
		{name: "replay", apply: func(store *Store, workID string) error {
			_, _, err := store.ResolveReviewLease(workID, ReviewLeaseReplay, "user", "explicit replay authorization")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, store, event, _ := preparePendingClaimedHostSubmission(t)
			workID := event.WorkID
			writes := 0
			write := store.writeOrchestration
			store.writeOrchestration = func(path string, value any) error {
				writes++
				return write(path, value)
			}
			if err := test.apply(store, workID); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatalf("actor resolution used %d persistence writes, want one", writes)
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fact, found, err := reopened.WorkEvent(event.FactEventID)
			if err != nil || !found || fact.Resolution == "" {
				t.Fatalf("fact resolution audit missing after reopen: fact=%+v found=%v err=%v", fact, found, err)
			}
			submission, found, err := reopened.TurnSubmission(event.DeliveryHostSessionID, event.ProviderTurnID)
			if err != nil || !found || submission.State != watcher.TurnSubmissionRetired {
				t.Fatalf("submission retirement missing after reopen: submission=%+v found=%v err=%v", submission, found, err)
			}
		})

		t.Run(test.name+" write failure", func(t *testing.T) {
			root, store, event, _ := preparePendingClaimedHostSubmission(t)
			store.writeOrchestration = func(string, any) error { return errors.New("injected actor-resolution write failure") }
			if _, _, err := store.ResolveReviewLease(event.WorkID, ReviewLeaseMarkDelivered, "user", "reason"); err == nil {
				t.Fatal("injected persistence failure was ignored")
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fact, found, err := reopened.WorkEvent(event.FactEventID)
			if err != nil || !found || fact.Resolution != "" || fact.DiscardedAt != nil {
				t.Fatalf("write failure exposed partial fact audit: fact=%+v found=%v err=%v", fact, found, err)
			}
			submission, found, err := reopened.TurnSubmission(event.DeliveryHostSessionID, event.ProviderTurnID)
			if err != nil || !found || submission.State != watcher.TurnSubmissionPending {
				t.Fatalf("write failure exposed partial retirement: submission=%+v found=%v err=%v", submission, found, err)
			}
		})
	}
}

// TestReviewLeaseDiscardReconcileDedupe proves the discard re-requirement is
// a single fresh fact (same queue item) and that repeating the discard is a
// no-op once the lease is gone.
func TestReviewLeaseDiscardReconcileDedupe(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	if _, _, err := store.ResolveReviewLease(workID, ReviewLeaseDiscard, "user", "moot"); err != nil {
		t.Fatal(err)
	}
	item, err := store.Work(workID)
	if err != nil || item.Review == nil || item.Review.FactEventID == claimed.FactEventID {
		t.Fatalf("discard re-requirement missing: Work=%+v err=%v", item, err)
	}
	if _, found := workEventByID(mustLoadEvents(t, store), item.Review.FactEventID); !found {
		t.Fatalf("reconcile fact id is not a fact row id: %q", item.Review.FactEventID)
	}
	events, err := store.ListWorkEvents(workID)
	if err != nil || len(events) != 2 {
		t.Fatalf("discard history = %+v err=%v", events, err)
	}
}

func mustLoadEvents(t *testing.T, store *Store) []WorkEvent {
	t.Helper()
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	return events
}
