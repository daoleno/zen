package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// claimResolutionStore builds a store with one Work and one claimed actionable
// Event (the held-claim shape: ClaimedAt set, ConsumedAt/DiscardedAt nil).
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
	claimedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	if err == nil {
		for index := range database.BrainWorkEvents {
			if database.BrainWorkEvents[index].ID == event.ID {
				database.BrainWorkEvents[index].ClaimedAt = &claimedAt
				database.BrainWorkEvents[index].DeliveryHostSessionID = "brain-agent-brain-hidden:@1"
			}
		}
		err = store.persistOrchestrationLocked(database)
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return store, item.ID, event
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
	if row.ConsumedAt == nil || row.Resolution != EventResolutionMarkDelivered ||
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
	// The row is excluded from claims forever; the event never re-dispatches.
	claimed, err := store.ClaimedActionableEvents()
	if err != nil || len(claimed) != 0 {
		t.Fatalf("discarded claim still listed: %#v", claimed)
	}
	if _, claimed, err := store.ClaimNextActionableEvent("host"); err != nil || claimed {
		t.Fatalf("discarded event re-claimed: claimed=%v err=%v", claimed, err)
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
	if err := store.ReleaseEventClaim(event.ID, "brain-agent-brain-hidden:@1"); err != nil {
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

// TestPruneSettledTurnsKeepsHeldAndUncertainRows covers C.12 Phase 3: only
// closed turns whose terminal events were consumed are pruned; held and
// Unknown rows are never pruned.
func TestPruneSettledTurnsKeepsHeldAndUncertainRows(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload-digest",
		Admission: admission,
		At:        acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceProvider, Kind: "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(3 * time.Second),
		SettledAt:  acceptedAt.Add(9 * time.Second),
		At:         acceptedAt.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// The terminal event is NOT consumed yet: nothing is pruned.
	pruned, err := store.PruneSettledTurns(time.Now().UTC())
	if err != nil || pruned != 0 {
		t.Fatalf("prune with unconsumed event = %d err=%v", pruned, err)
	}
	// Consume the terminal event, then the closed turn is pruned.
	events, _ := store.ListWorkEvents(storeWorkID(t, store, sessionID))
	var terminal WorkEvent
	for _, event := range events {
		if strings.HasSuffix(event.DedupeKey, ":session.done") {
			terminal = event
		}
	}
	now := time.Now().UTC()
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	if err == nil {
		for index := range database.BrainWorkEvents {
			if database.BrainWorkEvents[index].ID == terminal.ID {
				database.BrainWorkEvents[index].ClaimedAt = &now
				database.BrainWorkEvents[index].ConsumedAt = &now
			}
		}
		err = store.persistOrchestrationLocked(database)
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	pruned, err = store.PruneSettledTurns(now.Add(time.Hour))
	if err != nil || pruned != 1 {
		t.Fatalf("prune settled turn = %d err=%v", pruned, err)
	}
	if _, hasTurn, err := store.Turn(sessionID); err != nil || hasTurn {
		t.Fatalf("settled turn still present: hasTurn=%v err=%v", hasTurn, err)
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
