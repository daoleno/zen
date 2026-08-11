package brain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// claimResolutionStore builds a store with one Work and one claimed actionable
// Event (the held-claim shape: ClaimedAt set, DeliveredAt/DiscardedAt nil).
func claimResolutionStore(t *testing.T) (*Store, string, WorkEvent) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Held claim resolution",
		Objective:        "Close held delivery claims explicitly.",
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
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
	if err != nil || !ok || claimed.ID != event.ID {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	return store, item.ID, claimed
}

// TestMarkDeliveredClaimClosesHeldClaim explicitly closes a held claim by user
// assertion (C.2.6.1): actor-recorded, idempotent, never time-based.
func TestMarkDeliveredClaimClosesHeldClaim(t *testing.T) {
	store, _, event := claimResolutionStore(t)
	if err := store.MarkDeliveredClaim(event.ID, "user", "visible in host transcript"); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(event.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	row := events[0]
	if row.DeliveredAt == nil || row.Resolution != EventResolutionMarkDelivered ||
		row.ResolvedBy != "user" || row.ResolvedAt == nil {
		t.Fatalf("mark_delivered row = %#v", row)
	}
	// Idempotent: a second resolution on the same row is refused.
	if err := store.MarkDeliveredClaim(event.ID, "user", "again"); err == nil {
		t.Fatal("already-resolved claim accepted a second resolution")
	}
	// ClaimedActionableEvents no longer lists the row.
	claimed, err := store.ClaimedActionableEvents()
	if err != nil || len(claimed) != 0 {
		t.Fatalf("resolved claim still listed: %#v err=%v", claimed, err)
	}
}

// Delivery diagnostics are scheduler audit, not Work-state mutation. They
// must never advance the revision fence carried by the exact Host claim;
// otherwise the provider can accept and execute the Event but its mandated
// typed disposition becomes impossible.
func TestDeliveryDiagnosticDoesNotInvalidateClaimRevision(t *testing.T) {
	store, workID, event := claimResolutionStore(t)
	before, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	note, created, err := store.AppendDeliveryNote(
		workID,
		event.ID,
		"delivery.ambiguous",
		"delivery:"+event.ID+":ambiguous",
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
	claimed, found, err := store.WorkEvent(event.ID)
	if err != nil || !found || claimed.DeliveryWorkRevision != after.Revision {
		t.Fatalf("diagnostic invalidated exact claim fence: event=%+v Work=%+v found=%v err=%v", claimed, after, found, err)
	}
	if note.WorkRevision != after.Revision+1 {
		t.Fatalf("diagnostic revision epoch=%d want next epoch %d", note.WorkRevision, after.Revision+1)
	}
}

// TestDiscardClaimRemovesHeldClaimForever discards a held delivery (C.2.6.2).
func TestDiscardClaimRemovesHeldClaimForever(t *testing.T) {
	store, _, event := claimResolutionStore(t)
	if err := store.DiscardClaim(event.ID, "user", "work is moot"); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListWorkEvents(event.WorkID)
	row := events[0]
	if row.DiscardedAt == nil || row.Resolution != EventResolutionDiscard ||
		row.ResolvedBy != "user" || row.ResolvedAt == nil {
		t.Fatalf("discard row = %#v", row)
	}
	// The discarded row is excluded from claims forever. Its Work receives one
	// fresh level-based reconciliation; the discarded input is never replayed.
	claimed, err := store.ClaimedActionableEvents()
	if err != nil || len(claimed) != 0 {
		t.Fatalf("discarded claim still listed: %#v", claimed)
	}
	reconcile, wasClaimed, err := store.ClaimNextActionableEvent("host")
	if err != nil || !wasClaimed || reconcile.ID == event.ID || reconcile.Kind != "brain.reconcile_required" {
		t.Fatalf("discard reconciliation = %+v claimed=%v err=%v", reconcile, wasClaimed, err)
	}
}

// TestReplayEventCreatesAuditedNewEvent replays a held delivery as a new event
// with a new identity and key (C.2.6.3): the only authorized second wake.
func TestReplayEventCreatesAuditedNewEvent(t *testing.T) {
	store, workID, event := claimResolutionStore(t)
	replay, err := store.ReplayEvent(event.ID, "user", "explicit replay authorization")
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID == event.ID || replay.WorkID != event.WorkID ||
		replay.Kind != event.Kind || replay.ReplayOf != event.ID ||
		!strings.HasPrefix(replay.DedupeKey, "delivery:"+event.ID+":replay:") {
		t.Fatalf("replay row = %#v", replay)
	}
	events, _ := store.ListWorkEvents(workID)
	original := events[0]
	if original.Resolution != EventResolutionReplayed || original.ResolvedBy != "user" {
		t.Fatalf("original row after replay = %#v", original)
	}
	// The replay enters the normal claim pipeline exactly once.
	if _, claimed, err := store.ClaimNextActionableEvent("host"); err != nil || !claimed {
		t.Fatalf("replay not claimable: claimed=%v err=%v", claimed, err)
	}
}

// TestReplayEventIsBoundedToOneReplayOfHeldClaim covers the P1.1 contract:
// replay requires an unresolved held claim, a second replay of the same
// original is rejected, and the resolved original leaves the held set.
func TestReplayEventIsBoundedToOneReplayOfHeldClaim(t *testing.T) {
	store, _, event := claimResolutionStore(t)
	if _, err := store.ReplayEvent(event.ID, "user", "first replay"); err != nil {
		t.Fatal(err)
	}
	// The resolved original is excluded from the held set forever.
	claimed, err := store.ClaimedActionableEvents()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range claimed {
		if row.ID == event.ID {
			t.Fatalf("resolved original still in held set: %#v", claimed)
		}
	}
	// A second replay of the same original is rejected.
	if _, err := store.ReplayEvent(event.ID, "user", "second replay"); err == nil {
		t.Fatal("second replay of the same original was accepted")
	}
	// The single audited replay identity is retained exactly once.
	events, _ := store.ListWorkEvents(event.WorkID)
	replays := 0
	for _, row := range events {
		if row.ReplayOf == event.ID {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("replay rows for %s = %d, want exactly one", event.ID, replays)
	}
}

// TestReplayEventRequiresHeldClaim covers the P1.1 contract: replay without a
// held claim (unclaimed, consumed, discarded, or already resolved) is
// rejected.
func TestReplayEventRequiresHeldClaim(t *testing.T) {
	store, _, event := claimResolutionStore(t)
	// Release the claim: an unclaimed event cannot be replayed.
	if err := store.ReleaseEventClaim(
		event.ID, event.HandlingID, event.WorkID, event.DeliveryHostSessionID, event.ProviderTurnID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplayEvent(event.ID, "user", "no held claim"); err == nil {
		t.Fatal("replay of an unclaimed event was accepted")
	}
	// A discarded claim cannot be replayed.
	store2, _, event2 := claimResolutionStore(t)
	if err := store2.DiscardClaim(event2.ID, "user", "moot"); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.ReplayEvent(event2.ID, "user", "discarded"); err == nil {
		t.Fatal("replay of a discarded claim was accepted")
	}
	// A consumed claim cannot be replayed.
	store3, _, event3 := claimResolutionStore(t)
	if err := store3.MarkDeliveredClaim(event3.ID, "user", "delivered"); err != nil {
		t.Fatal(err)
	}
	if _, err := store3.ReplayEvent(event3.ID, "user", "consumed"); err == nil {
		t.Fatal("replay of a consumed claim was accepted")
	}
}

// TestClaimResolutionRequiresActorAndReason enforces the authorization gate:
// automatic or time-based resolution is prohibited.
func TestClaimResolutionRequiresActorAndReason(t *testing.T) {
	store, _, event := claimResolutionStore(t)
	if err := store.MarkDeliveredClaim(event.ID, "", "reason"); err == nil {
		t.Fatal("resolution without actor accepted")
	}
	if err := store.DiscardClaim(event.ID, "user", ""); err == nil {
		t.Fatal("resolution without reason accepted")
	}
	if _, err := store.ReplayEvent(event.ID, "user", ""); err == nil {
		t.Fatal("replay without reason accepted")
	}
	if err := store.MarkDeliveredClaim("missing-event", "user", "reason"); err == nil {
		t.Fatal("resolution of unknown event accepted")
	}
}

// A held Host claim may already own the Session's sole pending submission.
// An explicit actor resolution must retire that exact transaction in the same
// orchestration replacement; otherwise the replacement Event can be claimed
// but can never prepare its own provider Turn. Late provider evidence for the
// retired transaction must also remain non-adoptable.
func TestClaimResolutionRetiresExactPendingHostSubmissionAtomically(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*Store, WorkEvent) error
	}{
		{name: "mark delivered", apply: func(store *Store, event WorkEvent) error {
			return store.MarkDeliveredClaim(event.ID, "user", "visible in Host transcript")
		}},
		{name: "discard", apply: func(store *Store, event WorkEvent) error {
			return store.DiscardClaim(event.ID, "user", "obsolete delivery")
		}},
		{name: "replay", apply: func(store *Store, event WorkEvent) error {
			_, err := store.ReplayEvent(event.ID, "user", "explicit replay authorization")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, store, original, pending := preparePendingClaimedHostSubmission(t)
			if err := test.apply(store, original); err != nil {
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

			next, claimed, err := reopened.ClaimNextActionableEvent(original.DeliveryHostSessionID)
			if err != nil || !claimed || next.ID == original.ID {
				t.Fatalf("replacement Event claim=%+v claimed=%v err=%v", next, claimed, err)
			}
			candidate := watcher.TurnSubmission{
				WorkID: next.WorkID, SessionID: next.DeliveryHostSessionID,
				ProposedTurnID: next.ProviderTurnID, Receipt: next.ID, ClaimToken: next.HandlingID,
				PayloadSHA256:   pendingSubmissionDigest("replacement Host delivery"),
				ProcessIdentity: "replacement-process", PaneGeneration: "replacement-pane",
				AcceptedAt: lateAt.Add(time.Second), Mode: watcher.TurnSubmissionFresh,
			}
			prepared, created, err := reopened.PrepareTurnSubmission(candidate)
			if err != nil || !created || prepared.State != watcher.TurnSubmissionPending {
				t.Fatalf("retired transaction still gated replacement: submission=%+v created=%v err=%v", prepared, created, err)
			}

			reopenedAgain, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			old, found, err := reopenedAgain.TurnSubmission(original.DeliveryHostSessionID, original.ProviderTurnID)
			if err != nil || !found || old.State != watcher.TurnSubmissionState("retired") {
				t.Fatalf("retired terminal state did not survive second reopen: submission=%+v found=%v err=%v", old, found, err)
			}
			current, found, err := reopenedAgain.PendingTurnSubmission(original.DeliveryHostSessionID)
			if err != nil || !found || current.ProposedTurnID != next.ProviderTurnID {
				t.Fatalf("replacement pending identity after reopen=%+v found=%v err=%v", current, found, err)
			}
		})
	}
}

func TestClaimResolutionRetirementIgnoresUnrelatedPendingSubmission(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@manual-isolation"
	firstWork := createSignalTestWork(t, store, "Resolve first held claim", "brain-agent-worker:@first")
	appendSignalTestEvent(t, store, firstWork, "manual-first")
	secondWork := createSignalTestWork(t, store, "Preserve second pending claim", "brain-agent-worker:@second")
	appendSignalTestEvent(t, store, secondWork, "manual-second")
	first, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || first.WorkID != firstWork.ID {
		t.Fatalf("first claim=%+v ok=%v err=%v", first, ok, err)
	}
	second, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || second.WorkID != secondWork.ID {
		t.Fatalf("second claim=%+v ok=%v err=%v", second, ok, err)
	}
	unrelated, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: second.WorkID, SessionID: hostID, ProposedTurnID: second.ProviderTurnID,
		Receipt: second.ID, ClaimToken: second.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("unrelated provider input"),
		ProcessIdentity: "unrelated-process", PaneGeneration: "unrelated-pane",
		AcceptedAt: time.Date(2026, 8, 11, 1, 40, 0, 0, time.UTC), Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare unrelated submission=%+v created=%v err=%v", unrelated, created, err)
	}
	if err := store.DiscardClaim(first.ID, "user", "retire exact delivery only"); err != nil {
		t.Fatal(err)
	}
	unchanged, found, err := store.TurnSubmission(hostID, unrelated.ProposedTurnID)
	if err != nil || !found || unchanged.State != watcher.TurnSubmissionPending ||
		unchanged.Receipt != unrelated.Receipt || unchanged.PayloadSHA256 != unrelated.PayloadSHA256 {
		t.Fatalf("unrelated pending submission was mutated: submission=%+v found=%v err=%v", unchanged, found, err)
	}
}

func TestClaimResolutionRetirementAndEventMutationShareOneCommit(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*Store, WorkEvent) error
	}{
		{name: "mark delivered", apply: func(store *Store, event WorkEvent) error {
			return store.MarkDeliveredClaim(event.ID, "user", "visible in Host transcript")
		}},
		{name: "discard", apply: func(store *Store, event WorkEvent) error {
			return store.DiscardClaim(event.ID, "user", "obsolete delivery")
		}},
		{name: "replay", apply: func(store *Store, event WorkEvent) error {
			_, err := store.ReplayEvent(event.ID, "user", "explicit replay authorization")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, store, event, _ := preparePendingClaimedHostSubmission(t)
			writes := 0
			write := store.writeOrchestration
			store.writeOrchestration = func(path string, value any) error {
				writes++
				return write(path, value)
			}
			if err := test.apply(store, event); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatalf("actor resolution used %d persistence writes, want one", writes)
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			original, found, err := reopened.WorkEvent(event.ID)
			if err != nil || !found || original.Resolution == "" {
				t.Fatalf("Event resolution missing after reopen: event=%+v found=%v err=%v", original, found, err)
			}
			submission, found, err := reopened.TurnSubmission(event.DeliveryHostSessionID, event.ProviderTurnID)
			if err != nil || !found || submission.State != watcher.TurnSubmissionRetired {
				t.Fatalf("submission retirement missing after reopen: submission=%+v found=%v err=%v", submission, found, err)
			}
		})

		t.Run(test.name+" write failure", func(t *testing.T) {
			root, store, event, _ := preparePendingClaimedHostSubmission(t)
			store.writeOrchestration = func(string, any) error { return errors.New("injected actor-resolution write failure") }
			if err := test.apply(store, event); err == nil {
				t.Fatal("injected persistence failure was ignored")
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			original, found, err := reopened.WorkEvent(event.ID)
			if err != nil || !found || original.Resolution != "" || original.DeliveredAt != nil || original.DiscardedAt != nil {
				t.Fatalf("write failure exposed partial Event resolution: event=%+v found=%v err=%v", original, found, err)
			}
			submission, found, err := reopened.TurnSubmission(event.DeliveryHostSessionID, event.ProviderTurnID)
			if err != nil || !found || submission.State != watcher.TurnSubmissionPending {
				t.Fatalf("write failure exposed partial retirement: submission=%+v found=%v err=%v", submission, found, err)
			}
		})
	}
}

func storeWorkID(t *testing.T, store *Store, sessionID string) string {
	t.Helper()
	item, _, err := store.WorkByOwnerSession(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}
