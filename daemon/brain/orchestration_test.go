package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestOrchestrationSchemaV11RejectsTerminalWorkPendingSubmission(t *testing.T) {
	at := time.Date(2026, 8, 11, 4, 15, 0, 0, time.UTC)
	record := orchestrationDatabaseRecord{
		SchemaVersion:        orchestrationSchemaVersion,
		BrainInputAdmissions: []BrainInputAdmission{},
		BrainWork: []workRecord{{
			ID: "work-invalid-terminal-pending", Revision: 1, TerminalRevision: 1, Title: "invalid terminal Work",
			Objective: "reject current-schema corruption", Status: WorkCancelled,
			SourceThreadID: "brain-thread-invalid", CompletionPolicy: CompletionBounded,
			CreatedAt: at.Add(-time.Hour), UpdatedAt: at,
		}},
		BrainWorkEvents: []WorkEvent{}, BrainTurns: []TurnRecord{},
		BrainTurnSubmissions: []TurnSubmissionRecord{{
			SessionID: "brain-agent-invalid:@1", ProposedTurnID: "turn:invalid",
			WorkID: "work-invalid-terminal-pending", Receipt: "turn:invalid",
			PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: "invalid-process",
			PaneGeneration: "invalid-generation", AcceptedAt: at, Mode: watcher.TurnSubmissionFresh,
			State: watcher.TurnSubmissionPending, CreatedAt: at,
		}},
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeOrchestrationDatabase(raw); err == nil ||
		!strings.Contains(err.Error(), "terminal Work") {
		t.Fatalf("current schema accepted terminal pending authority: %v", err)
	}
}

func TestTimelineCorruptionFailsWorkReadProjectionClosed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: "valid-card", ThreadID: threadID, SessionID: "worker:@1",
		Role: "assistant", Body: "valid before corruption", Kind: timelineKindWorkCard,
		WorkID: "work-corrupt-timeline", EventKind: "session.done", Unread: true,
	}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.messagesPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{not-json}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActiveWork(); err == nil || !strings.Contains(err.Error(), "decode timeline line 2") {
		t.Fatalf("ActiveWork corruption error=%v", err)
	}
	if _, err := store.ProjectWorkInventory(nil); err == nil || !strings.Contains(err.Error(), "decode timeline line 2") {
		t.Fatalf("ProjectWorkInventory corruption error=%v", err)
	}
	if _, err := store.ThreadTimeline(threadID, 0); err == nil || !strings.Contains(err.Error(), "decode timeline line 2") {
		t.Fatalf("ThreadTimeline corruption error=%v", err)
	}
}

func TestWorkEventDedupeAndClaimAreAtomic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Atomic event",
		Objective:        "Consume one external fact at most once.",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	var created atomic.Int32
	var appendErrors atomic.Int32
	var appendWG sync.WaitGroup
	for range 32 {
		appendWG.Add(1)
		go func() {
			defer appendWG.Done()
			_, wasCreated, appendErr := store.AppendWorkEvent(WorkEvent{
				WorkID:     item.ID,
				Kind:       "session.done",
				DedupeKey:  "session:worker:@1:turn:42:session.done",
				Actionable: true,
			})
			if appendErr != nil {
				appendErrors.Add(1)
			}
			if wasCreated {
				created.Add(1)
			}
		}()
	}
	appendWG.Wait()
	if appendErrors.Load() != 0 || created.Load() != 1 {
		t.Fatalf("append errors=%d created=%d", appendErrors.Load(), created.Load())
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("deduplicated events = %#v", events)
	}

	var claimed atomic.Int32
	var claimErrors atomic.Int32
	var claimWG sync.WaitGroup
	for range 32 {
		claimWG.Add(1)
		go func() {
			defer claimWG.Done()
			_, ok, claimErr := store.ClaimNextActionableEvent("brain-agent-host-hidden:@1")
			if claimErr != nil {
				claimErrors.Add(1)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	claimWG.Wait()
	if claimErrors.Load() != 0 || claimed.Load() != 1 {
		t.Fatalf("claim errors=%d claimed=%d", claimErrors.Load(), claimed.Load())
	}

	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.ClaimNextActionableEvent("brain-agent-host-hidden:@1"); err != nil || ok {
		t.Fatalf("durable claim replayed after restart: ok=%v err=%v", ok, err)
	}
}

func TestClaimedEventIsIdentityBoundConsumedOnceWithoutReplay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	item, err := store.CreateWork(Work{
		Title:            "Identity-bound delivery",
		Objective:        "Expose one assigned Event to its Host exactly once.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done", DedupeKey: "session:worker:@1:turn:one:session.done", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != event.ID {
		t.Fatalf("claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	if _, _, err := store.ConsumeClaimedWorkEvent(event.ID, claimed.HandlingID, claimed.WorkID, "different-host:@1", claimed.ProviderTurnID); !errors.Is(err, ErrEventClaim) {
		t.Fatalf("different Host consumed assigned Event: err=%v", err)
	}
	for name, claim := range map[string]struct {
		eventID, token, workID, providerTurnID string
	}{
		"event":         {eventID: "different-event", token: claimed.HandlingID, workID: claimed.WorkID, providerTurnID: claimed.ProviderTurnID},
		"claim token":   {eventID: event.ID, token: "different-claim-token", workID: claimed.WorkID, providerTurnID: claimed.ProviderTurnID},
		"Work":          {eventID: event.ID, token: claimed.HandlingID, workID: "different-work", providerTurnID: claimed.ProviderTurnID},
		"provider Turn": {eventID: event.ID, token: claimed.HandlingID, workID: claimed.WorkID, providerTurnID: "different-provider-turn"},
	} {
		if _, _, err := store.ConsumeClaimedWorkEvent(
			claim.eventID, claim.token, claim.workID, hostID, claim.providerTurnID,
		); !errors.Is(err, ErrEventClaim) {
			t.Fatalf("different %s consumed assigned Event: err=%v", name, err)
		}
	}
	gotEvent, gotWork, err := store.ConsumeClaimedWorkEvent(event.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID)
	if err != nil || gotEvent.ID != event.ID || gotWork.ID != item.ID || gotEvent.DeliveredAt == nil {
		t.Fatalf("consume event=%#v work=%#v err=%v", gotEvent, gotWork, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(event.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID); !errors.Is(err, ErrEventClaim) {
		t.Fatalf("consumed Event replayed: err=%v", err)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.ClaimNextActionableEvent(hostID); err != nil || ok {
		t.Fatalf("consumed Event reclaimed after restart: ok=%v err=%v", ok, err)
	}
}

func TestWorkEventSchedulerEligibilityFollowsTerminalLifecycleBoundary(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	base := time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC)
	now := base
	store.now = func() time.Time { return now }

	terminalWorks := []Work{}
	for _, status := range []WorkStatus{WorkDone, WorkCancelled} {
		item, err := store.CreateWork(Work{
			Title:            "Terminal history",
			Objective:        "Retain an unread historical result without waking Brain.",
			Status:           WorkWaiting,
			CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		now = base.Add(10 * time.Minute)
		terminalEvent, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID:     item.ID,
			Kind:       "session.stale",
			DedupeKey:  "session:terminal:" + string(status) + ":turn:one:session.stale",
			Actionable: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		// The unread projection is the materialized timeline work card; the
		// append-only Event fact never carries read state.
		if _, _, err := store.MaterializeWorkCard(item, terminalEvent); err != nil {
			t.Fatal(err)
		}
		now = base.Add(3*time.Hour + 34*time.Minute)
		item, err = store.UpdateWork(item.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}
		terminalWorks = append(terminalWorks, item)
		now = base
	}
	if event, claimed, err := store.ClaimNextActionableEvent(hostID); err != nil || claimed {
		t.Fatalf("pre-terminal unclaimed Event was claimable: event=%#v claimed=%v err=%v", event, claimed, err)
	}

	terminal, err := store.CreateWork(Work{
		Title:            "Claim before completion",
		Objective:        "Stop scheduling a claim after its Work becomes terminal.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = base.Add(10 * time.Minute)
	terminalEvent, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     terminal.ID,
		Kind:       "session.stale",
		DedupeKey:  "session:claimed:turn:one:session.stale",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MaterializeWorkCard(terminal, terminalEvent); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != terminalEvent.ID {
		t.Fatalf("active Event claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	now = base.Add(3*time.Hour + 34*time.Minute)
	status := WorkDone
	if _, err := store.UpdateWork(terminal.ID, WorkUpdate{Status: &status}); !errors.Is(err, ErrWorkConflict) {
		t.Fatalf("terminal update bypassed held Host claim: %v", err)
	}
	if err := store.DiscardClaim(claimed.ID, "test", "explicitly retire the undelivered historical claim"); err != nil {
		t.Fatal(err)
	}
	terminal, err = store.UpdateWork(terminal.ID, WorkUpdate{Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	terminalWorks = append(terminalWorks, terminal)
	blockers, err := store.ClaimedActionableEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Fatalf("pre-terminal claimed Event remained a scheduling blocker: %#v", blockers)
	}

	now = base.Add(4 * time.Hour)
	active, err := store.CreateWork(Work{
		Title:            "Later active Work",
		Objective:        "Deliver the next eligible active Work Event.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	activeEvent, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     active.ID,
		Kind:       "session.done",
		DedupeKey:  "session:later:turn:one:session.done",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err = store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != activeEvent.ID {
		t.Fatalf("later active Event claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	consumed, consumedWork, err := store.ConsumeClaimedWorkEvent(activeEvent.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID)
	if err != nil || consumed.ID != activeEvent.ID || consumedWork.ID != active.ID ||
		consumed.DeliveredAt == nil {
		t.Fatalf("consume event=%#v work=%#v err=%v", consumed, consumedWork, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(activeEvent.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID); !errors.Is(err, ErrEventClaim) {
		t.Fatalf("active Event was consumed more than once: err=%v", err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: consumed.ID, HandlingID: consumed.HandlingID, ProviderTurnID: consumed.ProviderTurnID,
		ExpectedWorkRevision: consumed.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatalf("end active Host handling: %v", err)
	}

	for _, offset := range []time.Duration{0, time.Minute} {
		now = base.Add(5 * time.Hour)
		item, err := store.CreateWork(Work{
			Title:            "Terminal boundary result",
			Objective:        "Deliver a terminal result created at or after the transition.",
			Status:           WorkWaiting,
			CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		status := WorkDone
		item, err = store.UpdateWork(item.ID, WorkUpdate{Status: &status})
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(offset)
		event, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID:     item.ID,
			Kind:       "calendar.result",
			DedupeKey:  fmt.Sprintf("session:boundary:turn:%s:session.done", offset),
			Actionable: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil || !ok || claimed.ID != event.ID {
			t.Fatalf("terminal result offset=%s claim=%#v ok=%v err=%v", offset, claimed, ok, err)
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		consumed, consumedWork, err := store.ConsumeClaimedWorkEvent(event.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID)
		if err != nil || consumed.ID != event.ID || consumedWork.ID != item.ID {
			t.Fatalf("terminal result offset=%s consume=%#v work=%#v err=%v",
				offset, consumed, consumedWork, err)
		}
		if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: consumed.ID, HandlingID: consumed.HandlingID, ProviderTurnID: consumed.ProviderTurnID,
			ExpectedWorkRevision: consumed.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
		}); err != nil {
			t.Fatalf("end terminal Host handling offset=%s err=%v", offset, err)
		}
	}

	now = base.Add(6 * time.Hour)
	readWork, err := store.CreateWork(Work{
		Title:            "Acknowledged result",
		Objective:        "Do not schedule an Event after it is marked read.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	readEvent, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: readWork.ID, Kind: "session.done", DedupeKey: "session:read:turn:one:session.done", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MaterializeWorkCard(readWork, readEvent); err != nil {
		t.Fatal(err)
	}
	readClaim, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("Event to acknowledge was not claimed: ok=%v err=%v", ok, err)
	}
	now = now.Add(time.Minute)
	if err := store.MarkWorkRead(readWork.ID); err != nil {
		t.Fatal(err)
	}
	if blockers, err := store.ClaimedActionableEvents(); err != nil || len(blockers) != 1 ||
		blockers[0].ID != readEvent.ID {
		t.Fatalf("card acknowledgement changed the exact delivery claim: blockers=%#v err=%v", blockers, err)
	}
	resolveClaimedHostTurnForTest(t, store, readClaim)
	consumedRead, _, err := store.ConsumeClaimedWorkEvent(readEvent.ID, readClaim.HandlingID, readClaim.WorkID, hostID, readClaim.ProviderTurnID)
	if err != nil || consumedRead.DeliveredAt == nil {
		t.Fatalf("exact accepted claim was not consumable after card acknowledgement: event=%#v err=%v", consumedRead, err)
	}
	readEvents, err := store.ListWorkEvents(readWork.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(readEvents) != 1 || readEvents[0].ID != readEvent.ID ||
		readEvents[0].ClaimedAt == nil || readEvents[0].DeliveredAt == nil {
		t.Fatalf("card acknowledgement mutated the exact delivery claim: %#v", readEvents)
	}
	projectedAfterRead, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range projectedAfterRead {
		if item.ID == readWork.ID && item.UnreadResult {
			t.Fatalf("card acknowledgement did not clear the read projection: %+v", item)
		}
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: consumedRead.ID, HandlingID: consumedRead.HandlingID, ProviderTurnID: consumedRead.ProviderTurnID,
		ExpectedWorkRevision: consumedRead.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatalf("end card-acknowledged Host handling: %v", err)
	}
	unclaimedReadWork, err := store.CreateWork(Work{
		Title:            "Acknowledged before claim",
		Objective:        "Keep card acknowledgement separate from Event delivery.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	readBeforeEvent, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: unclaimedReadWork.ID, Kind: "session.done", DedupeKey: "session:read-before:turn:one:session.done", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MaterializeWorkCard(unclaimedReadWork, readBeforeEvent); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkRead(unclaimedReadWork.ID); err != nil {
		t.Fatal(err)
	}
	readBeforeClaim, wasClaimed, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !wasClaimed || readBeforeClaim.WorkID != unclaimedReadWork.ID {
		t.Fatalf("card acknowledgement suppressed Event delivery: event=%#v claimed=%v err=%v", readBeforeClaim, wasClaimed, err)
	}
	resolveClaimedHostTurnForTest(t, store, readBeforeClaim)
	if _, _, err := store.ConsumeClaimedWorkEvent(readBeforeClaim.ID, readBeforeClaim.HandlingID, readBeforeClaim.WorkID, hostID, readBeforeClaim.ProviderTurnID); err != nil {
		t.Fatalf("consume card-acknowledged Event: %v", err)
	}

	terminalEvents, err := store.ListWorkEvents(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(terminalEvents) != 2 || terminalEvents[0].ID != terminalEvent.ID ||
		terminalEvents[0].ClaimedAt == nil || terminalEvents[0].DeliveredAt != nil ||
		terminalEvents[0].Resolution != EventResolutionDiscard || terminalEvents[0].DiscardedAt == nil ||
		terminalEvents[1].Kind != "brain.reconcile_required" || terminalEvents[1].ClaimedAt != nil {
		t.Fatalf("terminal Event history changed: %#v", terminalEvents)
	}
	projected, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	projectedByID := map[string]ActiveWork{}
	for _, item := range projected {
		projectedByID[item.ID] = item
	}
	for _, item := range terminalWorks {
		events, err := store.ListWorkEvents(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		wantEvents := 1
		if item.ID == terminal.ID {
			wantEvents = 2
		}
		if len(events) != wantEvents {
			t.Fatalf("terminal Work Event history changed: work=%s events=%#v", item.ID, events)
		}
		if !projectedByID[item.ID].UnreadResult {
			t.Fatalf("terminal Work lost unread projection: work=%s projection=%#v", item.ID, projected)
		}
	}
}

func TestActiveWorkProjectsMultipleItemsAndUnreadResults(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateWork(Work{
		Title:            "Work A",
		Objective:        "Keep A running.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-a:@1",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateWork(Work{
		Title:            "Work C",
		Objective:        "Start C independently.",
		Status:           WorkDone,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstAfterStartingSecond, err := store.Work(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstAfterStartingSecond, first) {
		t.Fatalf("starting Work C mutated Work A:\nbefore=%#v\nafter=%#v", first, firstAfterStartingSecond)
	}
	secondEvent, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     second.ID,
		Kind:       "session.done",
		DedupeKey:  "session:c:turn:one:session.done",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The unread projection is the materialized timeline work card.
	if _, _, err := store.MaterializeWorkCard(second, secondEvent); err != nil {
		t.Fatal(err)
	}

	active, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("active Work = %#v", active)
	}
	byID := map[string]ActiveWork{}
	for _, item := range active {
		byID[item.ID] = item
	}
	if byID[first.ID].Status != WorkRunning || byID[first.ID].UnreadResult {
		t.Fatalf("Work A projection = %#v", byID[first.ID])
	}
	if byID[second.ID].Status != WorkDone || !byID[second.ID].UnreadResult {
		t.Fatalf("Work C projection = %#v", byID[second.ID])
	}
	if err := store.MarkWorkRead(second.ID); err != nil {
		t.Fatal(err)
	}
	active, err = store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("read terminal Work should leave Active projection: %#v", active)
	}
}

func TestOneSessionCannotOwnTwoActiveWorkRecords(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := "brain-agent-worker:@1"
	first, err := store.CreateWork(Work{
		Title:            "First",
		Objective:        "Keep one canonical Session owner.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Second",
		Objective:        "Must not duplicate active Session ownership.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	}); err == nil {
		t.Fatal("duplicate active Session ownership was accepted")
	}
	status := WorkDone
	if _, err := store.UpdateWork(first.ID, WorkUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Successor",
		Objective:        "Reuse the Session after prior Work is terminal.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatalf("terminal Work should release owner uniqueness: %v", err)
	}
}

func TestPrepareTurnSubmissionOwnerAdmissionHasOneConcurrentWinner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Single owner",
		Objective:        "Attach exactly one delegated Session.",
		Status:           WorkOpen,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	var winners atomic.Int32
	var winnerMu sync.Mutex
	winnerID := ""
	var wg sync.WaitGroup
	for index := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessionID := fmt.Sprintf("brain-agent-worker:@%d", index+1)
			turnID := sessionID + ":turn:1"
			attached, created, attachErr := store.PrepareTurnSubmission(watcher.TurnSubmission{
				WorkID: item.ID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
				PayloadSHA256:   pendingSubmissionDigest("concurrent owner payload"),
				ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
				AcceptedAt: time.Date(2026, 8, 10, 7, 0, 0, 0, time.UTC),
				Mode:       watcher.TurnSubmissionFresh,
			})
			if attachErr == nil {
				if !created {
					t.Errorf("winning owner admission was not newly created: %+v", attached)
					return
				}
				winners.Add(1)
				winnerMu.Lock()
				winnerID = attached.SessionID
				winnerMu.Unlock()
				return
			}
			if !errors.Is(attachErr, ErrWorkOwnerConflict) {
				t.Errorf("attach error = %v", attachErr)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("CAS winners = %d", winners.Load())
	}
	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerSessionID != winnerID || got.Status != WorkRunning {
		t.Fatalf("attached Work=%#v winner=%q", got, winnerID)
	}
	if pendingList, err := store.PendingTurnSubmissions(winnerID); err != nil || len(pendingList) != 1 || pendingList[0].WorkID != item.ID {
		t.Fatalf("winning owner lacks pending submission: submissions=%+v err=%v", pendingList, err)
	}
}

func TestDispatchRequiresActionableEventEvenForUntilDoneWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Invalid completion",
		Objective:        "Must name evidence.",
		CompletionPolicy: CompletionUntilDone,
	}); err == nil {
		t.Fatal("until_done Work without done_criteria_ref was accepted")
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Verified completion",
		Objective:        "Continue only when a real fact arrives.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef:  "worklog/verified.md#done",
		NextAction:       "Wait for evidence.",
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 20 {
		if woke, err := service.ReconcileHostLane(); err != nil || woke {
			t.Fatalf("idle until_done Work woke: woke=%v err=%v", woke, err)
		}
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.progress",
		DedupeKey:  "session:progress:turn:one:session.progress",
		Actionable: false,
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("passive event woke: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 0 {
		t.Fatalf("idle scheduler sent %#v", fw.sentCalls)
	}

	actionable, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:worker:@1:turn:one:session.done",
		PayloadRef: "session:worker:@1",
		SourceName: "Worker One",
		Summary:    "Completed the delegated change.",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("actionable event did not wake: woke=%v err=%v", woke, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("consumed event replayed: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("sends = %#v", fw.sentCalls)
	}
	if fw.receipts[hostID] != actionable.ID {
		t.Fatalf("delivery = %#v receipt=%q, want Event.ID receipt", fw.sentCalls[0], fw.receipts[hostID])
	}
	for _, required := range []string{
		"<zen_work_event>", actionable.ID, item.ID, item.Title, actionable.Kind, actionable.PayloadRef,
	} {
		if required != "" && !strings.Contains(fw.sentCalls[0].text, required) {
			t.Fatalf("direct delivery omitted %q: %q", required, fw.sentCalls[0].text)
		}
	}
	for _, forbidden := range []string{
		item.Objective, "delivery_token", "ZEN_TX", "PATH_B64URL", "PAYLOAD_SHA256", "zen brain " + "event",
	} {
		if forbidden != "" && strings.Contains(fw.sentCalls[0].text, forbidden) {
			t.Fatalf("direct delivery leaked %q: %q", forbidden, fw.sentCalls[0].text)
		}
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 2 || events[1].ID != actionable.ID || events[1].DeliveredAt == nil {
		t.Fatalf("accepted direct Event was not consumed exactly: events=%#v err=%v", events, err)
	}
}

func TestDispatchAmbiguousSendRetainsExactClaimWithoutReplay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		turnStore: store,
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
		sendErr: &watcher.InputSubmissionError{
			Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
			Cause:  os.ErrDeadlineExceeded,
		},
	}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Retry delivery",
		Objective:        "Do not lose a failed host send.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "external.changed",
		DedupeKey:  "external:send-failure",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	if woke, err := service.ReconcileHostLane(); err == nil || woke {
		t.Fatalf("failed send woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt == nil || events[0].DeliveredAt != nil {
		t.Fatalf("failed send claim = %#v", events)
	}
	// The tmux receipt ledger proves the mutation may have begun: the claim
	// is held forever, never released by elapsed time, never replayed.
	fw.setReceiptOutcome(events[0].ID, watcher.InputAmbiguous)
	fw.sendErr = nil
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("uncertain delivery retried: woke=%v err=%v", woke, err)
	}
	retryAt := now.Add(24 * time.Hour)
	store.now = func() time.Time { return retryAt }
	service.now = func() time.Time { return retryAt }
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("elapsed time caused an ambiguous retry: woke=%v err=%v", woke, err)
	}
	events, err = store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	original := WorkEvent{}
	for _, event := range events {
		if event.ID == events[0].ID || event.DedupeKey == "external:send-failure" {
			original = event
		}
	}
	if original.ClaimedAt == nil || original.DeliveredAt != nil || len(fw.sentCalls) != 1 {
		t.Fatalf("ambiguous failed send did not remain closed: events=%#v sends=%#v", events, fw.sentCalls)
	}
	// The held claim is surfaced as a deduped delivery.ambiguous note.
	noteFound := false
	for _, event := range events {
		if event.Kind == "delivery.ambiguous" && !event.Actionable {
			noteFound = true
		}
	}
	if !noteFound {
		t.Fatalf("ambiguous claim missing delivery.ambiguous note: %#v", events)
	}
}

func TestAcceptedReceiptFinalizesConsumptionAfterPersistenceFailureAndRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Recover accepted consumption",
		Objective:        "Finalize durable Event consumption without a second provider turn.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Review the accepted result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:accepted:turn:one:session.done",
		SourceName: "Worker",
		Summary:    "Provider accepted the direct Event.",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	writeOrchestration := store.writeOrchestration
	writes := 0
	store.writeOrchestration = func(path string, value any) error {
		writes++
		if writes == 4 {
			return errors.New("injected consumed_at persistence failure")
		}
		return writeOrchestration(path, value)
	}

	if woke, dispatchErr := NewService(store, fw, nil).ReconcileHostLane(); dispatchErr == nil || woke {
		t.Fatalf("persistence failure woke=%v err=%v", woke, dispatchErr)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID ||
		events[0].ClaimedAt == nil || events[0].DeliveredAt != nil ||
		len(fw.sentCalls) != 1 || fw.outcomes[event.ID] != watcher.InputAccepted {
		t.Fatalf("accepted persistence boundary events=%#v sends=%#v outcomes=%#v", events, fw.sentCalls, fw.outcomes)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if woke, dispatchErr := NewService(restarted, fw, nil).ReconcileHostLane(); dispatchErr != nil || woke {
		t.Fatalf("accepted receipt finalization woke=%v err=%v", woke, dispatchErr)
	}
	events, err = restarted.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID || events[0].DeliveredAt == nil ||
		len(fw.sentCalls) != 1 {
		t.Fatalf("restart did not finalize without resend: events=%#v sends=%#v", events, fw.sentCalls)
	}
	if woke, dispatchErr := NewService(restarted, fw, nil).ReconcileHostLane(); dispatchErr != nil || woke ||
		len(fw.sentCalls) != 1 {
		t.Fatalf("finalized Event replayed: woke=%v err=%v sends=%#v", woke, dispatchErr, fw.sentCalls)
	}
}

func TestDispatchDefinitePreMutationFailureReleasesSameEventAcrossRestart(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			if err := store.SetHostSession(hostID, provider); err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title:            "Recover exact control event",
				Objective:        "Release only a definitely unsent claim.",
				Status:           WorkWaiting,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			event, _, err := store.AppendWorkEvent(WorkEvent{
				WorkID:     item.ID,
				Kind:       "session.done",
				DedupeKey:  "session:" + provider + ":turn:one:session.done",
				Actionable: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			failedWatcher := &fakeWatcher{
				sessions: map[string]*classifier.Agent{
					hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
				},
				sendErr: &watcher.InputSubmissionError{
					Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted},
					Cause:  errors.New("target generation changed before queue start"),
				},
			}
			if woke, dispatchErr := NewService(store, failedWatcher, nil).ReconcileHostLane(); dispatchErr == nil || woke {
				t.Fatalf("definite failure woke=%v err=%v", woke, dispatchErr)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].ID != event.ID || events[0].ClaimedAt != nil {
				t.Fatalf("exact Event was not released: %#v", events)
			}

			restarted, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			restartedWatcher := &fakeWatcher{turnStore: restarted, sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}
			if woke, dispatchErr := NewService(restarted, restartedWatcher, nil).ReconcileHostLane(); dispatchErr != nil || !woke {
				t.Fatalf("restart dispatch woke=%v err=%v", woke, dispatchErr)
			}
			if len(restartedWatcher.sentCalls) != 1 ||
				restartedWatcher.receipts[hostID] != event.ID {
				t.Fatalf("restart sent %#v receipts=%#v, want same Event.ID %q",
					restartedWatcher.sentCalls, restartedWatcher.receipts, event.ID)
			}
		})
	}
}

func TestDispatchAmbiguousClaimNeverReplaysAfterRestartForCodexAndClaude(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			if err := store.SetHostSession(hostID, provider); err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title:            "Retain ambiguous event",
				Objective:        "Never create a second provider turn.",
				Status:           WorkWaiting,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			event, _, err := store.AppendWorkEvent(WorkEvent{
				WorkID:     item.ID,
				Kind:       "session.done",
				DedupeKey:  "session:" + provider + ":turn:one:session.done",
				Actionable: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			failedWatcher := &fakeWatcher{
				sessions: map[string]*classifier.Agent{
					hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
				},
				sendErr: &watcher.InputSubmissionError{
					Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
					Cause:  errors.New("tmux queue started before connection loss"),
				},
			}
			if woke, dispatchErr := NewService(store, failedWatcher, nil).ReconcileHostLane(); dispatchErr == nil || woke {
				t.Fatalf("ambiguous dispatch woke=%v err=%v", woke, dispatchErr)
			}

			restarted, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			// The tmux receipt ledger proves the mutation may have begun:
			// the claim is held forever, surfaced as a deduped
			// delivery.ambiguous note, and never replayed (C.2.7).
			restartedWatcher := &fakeWatcher{
				turnStore: restarted,
				sessions: map[string]*classifier.Agent{
					hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
				},
			}
			restartedWatcher.setReceiptOutcome(event.ID, watcher.InputAmbiguous)
			if woke, dispatchErr := NewService(restarted, restartedWatcher, nil).ReconcileHostLane(); dispatchErr != nil || woke {
				t.Fatalf("restart replayed ambiguity: woke=%v err=%v", woke, dispatchErr)
			}
			events, err := restarted.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 2 || events[0].ID != event.ID || events[0].ClaimedAt == nil ||
				events[0].DeliveredAt != nil ||
				len(restartedWatcher.sentCalls) != 0 {
				t.Fatalf("ambiguous Event did not remain singly held: events=%#v sends=%#v",
					events, restartedWatcher.sentCalls)
			}
			note := events[1]
			if note.Kind != "delivery.ambiguous" || note.Actionable ||
				note.DedupeKey != "delivery:"+event.ID+":ambiguous" {
				t.Fatalf("ambiguous delivery note = %#v", note)
			}
		})
	}
}

func TestDispatchClaimWithAbsentReceiptReleasesAndRedispatches(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Absent receipt releases",
		Objective:        "Host receipts are written before the host mutates.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:absent:turn:one:session.done",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Claim once with a send failure that leaves no receipt (provably never
	// submitted), then restart with an empty receipt ledger: the claim is
	// released immediately and the event becomes dispatchable again.
	failedWatcher := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
		sendErr: errors.New("tmux queue did not start"),
	}
	if _, dispatchErr := NewService(store, failedWatcher, nil).ReconcileHostLane(); dispatchErr == nil {
		t.Fatal("failed dispatch did not error")
	}
	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restartedWatcher := &fakeWatcher{turnStore: restarted, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	if woke, dispatchErr := NewService(restarted, restartedWatcher, nil).ReconcileHostLane(); dispatchErr != nil || !woke {
		t.Fatalf("absent-receipt restart dispatch woke=%v err=%v", woke, dispatchErr)
	}
	if len(restartedWatcher.sentCalls) != 1 ||
		restartedWatcher.receipts[hostID] != event.ID {
		t.Fatalf("absent-receipt restart sent %#v receipts=%#v, want re-dispatch of %q",
			restartedWatcher.sentCalls, restartedWatcher.receipts, event.ID)
	}
}

func TestUserSteeringCannotOvertakeIdleBoundaryEvent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Background result",
		Objective:        "Deliver the Event before the user message mutates the provider.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:user:turn:one:session.done",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Steering enters the lane before the user message is prepared: the
	// pending internal Event is admitted at the idle boundary exactly once
	// and cannot be overtaken by the user's input.
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatal("host user input was not recognized")
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("idle-boundary delivery=%d, want exact-once: %#v", len(fw.sentCalls), fw.sentCalls)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt == nil || events[0].DeliveredAt == nil {
		t.Fatalf("idle-boundary Event was not delivered exactly once: %#v", events)
	}

	// The prepared user message becomes the durable pending admission; the
	// delivered Event awaits its typed disposition, so the lane stops and
	// never replays it.
	if _, created, err := service.PrepareHostUserInput(hostID, "foreground-user-1", "continue", ""); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("in-flight user message or delivered handling woke the lane: woke=%v err=%v", woke, err)
	}
	woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  hostID,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		TurnID:   "foreground-provider-turn",
		Agent:    &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
	})
	if err != nil || woke {
		t.Fatalf("terminal edge replayed the delivered Event: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("sends = %#v", fw.sentCalls)
	}
}

func TestConversationOnlyUserSteeringDoesNotCreateOrPauseWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	running, err := store.CreateWork(Work{
		Title:            "Work A",
		Objective:        "Continue in the background.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-a:@2",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatal("conversation-only Brain input was not recognized")
	}
	service.CancelUserSteering(hostID)
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !reflect.DeepEqual(items[0], running) {
		t.Fatalf("conversation-only input changed durable Work: %#v", items)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("conversation-only input created Events: %#v", events)
	}
}

func TestDelegatedSessionTransitionsDedupeToOneTurn(t *testing.T) {
	// Canonical contract: one actionable wake per (session, turn, kind) from
	// the single reducer; replayed identical facts are no-ops.
	for _, test := range []struct {
		name string
		kind string
		fact func(store *Store, sessionID, turnID string, admission watcher.TurnAdmission)
	}{
		{
			name: "done",
			kind: "session.done",
			fact: func(store *Store, sessionID, turnID string, admission watcher.TurnAdmission) {
				applyProviderTerminal(t, store, sessionID, turnID, "done", admission)
			},
		},
		{
			name: "failed",
			kind: "session.failed",
			fact: func(store *Store, sessionID, turnID string, admission watcher.TurnAdmission) {
				applyProviderTerminal(t, store, sessionID, turnID, "failed", admission)
			},
		},
		{
			name: "blocked",
			kind: "session.needs_input",
			fact: func(store *Store, sessionID, turnID string, admission watcher.TurnAdmission) {
				applyControlAttention(t, store, sessionID, turnID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			sessionID := "brain-agent-worker:@2"
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title:            "Delegated change",
				Objective:        "Handle one terminal transition.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			turnID := sessionID + ":turn:1"
			bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
				SessionID: sessionID, TurnID: turnID, AcceptedAt: now,
			})
			admission := providerAdmission("stream", "msg-1", 1, "sha", now)
			applyReceiptAdmission(t, store, sessionID, turnID, admission)
			test.fact(store, sessionID, turnID, admission)
			// Re-apply the identical fact: deterministic FactID no-op.
			test.fact(store, sessionID, turnID, admission)

			events, err := store.ListWorkEvents("")
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].Kind != test.kind {
				t.Fatalf("events=%#v want exactly one %s", events, test.kind)
			}
		})
	}
}

func TestDelegatedSessionDedupeAllowsANewLifecycleEpisode(t *testing.T) {
	// Canonical contract: a blocked attention fact wakes exactly once per
	// (session, turn, kind); repeated identical facts dedupe; a later bound
	// terminal for the same turn is its own kind and still wakes once.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Lifecycle episodes",
		Objective:        "Dedupe repeated facts without suppressing a later blocker.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: now,
	})
	admission := providerAdmission("stream", "msg-1", 1, "sha", now)
	applyReceiptAdmission(t, store, sessionID, turnID, admission)

	// First blocker wakes exactly once; repeated attention facts dedupe.
	applyControlAttention(t, store, sessionID, turnID)
	applyControlAttention(t, store, sessionID, turnID)
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	actionable := []WorkEvent{}
	for _, event := range events {
		if event.Kind == "session.needs_input" {
			actionable = append(actionable, event)
		}
	}
	if len(actionable) != 1 {
		t.Fatalf("attention wakes = %d, want exactly one per turn: %#v", len(actionable), events)
	}
	// The same turn's bound terminal is a distinct kind and wakes once.
	applyProviderTerminal(t, store, sessionID, turnID, "done", admission)
	events, err = store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, event := range events {
		if event.Kind == "session.done" && event.Actionable {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("done wakes = %d, want exactly one: %#v", done, events)
	}
}

func applyReceiptAdmission(t *testing.T, store *Store, sessionID, turnID string, admission watcher.TurnAdmission) {
	t.Helper()
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceReceipt,
		Kind:       "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         admission.At.Add(time.Second),
	}); err != nil || !changed {
		t.Fatalf("receipt admission apply = (%v, %v)", changed, err)
	}
}

func applyControlAttention(t *testing.T, store *Store, sessionID, turnID string) {
	t.Helper()
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "attention",
		SourceID:     "control\x00attention-" + turnID,
		LeaseSeconds: 300,
		Summary:      "Resolve the delegated Session request.",
	}); err != nil {
		t.Fatal(err)
	}
}

func applyProviderTerminal(t *testing.T, store *Store, sessionID, turnID, kind string, admission watcher.TurnAdmission) {
	t.Helper()
	_, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       kind,
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00msg-1\x001",
		Cursor:     1,
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  admission.At.Add(2 * time.Second),
		SettledAt:  admission.At.Add(30 * time.Second),
		Summary:    "Delegated provider " + kind + " the turn",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReconcileStaleUsesPerTurnLeaseNotAgentLease(t *testing.T) {
	// Slice 1 contract: session.stale is a property of the CURRENT turn's own
	// lease. A newly admitted turn mints a fresh lease; the agent's shared
	// lease fields (possibly expired from an older turn) can never stale it.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	store.now = func() time.Time { return now }
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Leased work",
		Objective:        "Wait for the current turn's own lease.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := now.Add(-time.Minute)
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
	})

	// The agent's shared lease is long expired (inherited from an older
	// turn), but the current turn's own deadline is fresh: no stale.
	expired := now.Add(-time.Hour)
	agent := &classifier.Agent{
		ID:                  sessionID,
		State:               classifier.StateRunning,
		Delegated:           true,
		PaneAlive:           true,
		ProcessID:           4242,
		LastProgressAt:      &expired,
		ExpectedNextCheckAt: &expired,
		UpdatedAt:           expired,
	}
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("fresh per-turn lease was staled by the old agent lease: events=%#v sends=%#v", events, fw.sentCalls)
	}

	// The current turn's own deadline passes: exactly one actionable stale,
	// deduped across repeated reconciles.
	staleAt := now.Add(turnLeaseGrace).Add(time.Second)
	store.now = func() time.Time { return staleAt }
	service.now = func() time.Time { return staleAt }
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err = store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	stale := 0
	for _, event := range events {
		if event.Kind == "session.stale" {
			stale++
		}
	}
	if stale != 1 || len(fw.sentCalls) != 1 {
		t.Fatalf("per-turn lease expiry stale=%d sends=%#v events=%#v", stale, fw.sentCalls, events)
	}

	// A control heartbeat renews the current turn's lease; a later reconcile
	// adds nothing.
	renewAt := now.Add(turnLeaseGrace).Add(2 * time.Second)
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00heartbeat-1", LeaseSeconds: 300,
		At: renewAt,
	}); err != nil {
		t.Fatal(err)
	}
	laterAt := now.Add(turnLeaseGrace).Add(3 * time.Second)
	store.now = func() time.Time { return laterAt }
	service.now = func() time.Time { return laterAt }
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err = store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	stale = 0
	for _, event := range events {
		if event.Kind == "session.stale" {
			stale++
		}
	}
	if stale != 1 {
		t.Fatalf("renewed lease re-staled the turn: %#v", events)
	}
}

func TestReconcileDeadOrAbsentTurnOwnedSessions(t *testing.T) {
	// Canonical path: an absent session resolves exactly one actionable
	// session.uncertain; a dead-pane session is owned by watcher liveness and
	// is never staled from the clock.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Authoritative liveness",
		Objective:        "Wake only from the canonical ledger record.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := now.Add(-2 * time.Hour)
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
		ProcessIdentity: "proc-1",
	})
	// Expire the turn's own lease so any unguarded path would stale.
	service.now = func() time.Time { return now }
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00heartbeat-1", LeaseSeconds: 1,
		At: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	// Dead process and pane: reconcile must not stale (watcher liveness owns
	// end-of-identity); no event, no wake.
	dead := &classifier.Agent{
		ID: sessionID, State: classifier.StateRunning, Delegated: true,
		PaneAlive: false, ProcessID: 0,
	}
	service.ReconcileDelegatedSessions([]*classifier.Agent{dead})
	service.ReconcileDelegatedSessions([]*classifier.Agent{dead})
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("dead-pane reconcile woke: events=%#v sends=%#v", events, fw.sentCalls)
	}

	// Absent after a successful inventory: exactly one actionable
	// session.uncertain, deduped across repeated reconciles.
	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)
	events, err = store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	uncertain := 0
	for _, event := range events {
		if event.Kind == "session.uncertain" {
			uncertain++
		}
	}
	if uncertain != 1 || len(fw.sentCalls) != 1 {
		t.Fatalf("absent reconcile uncertain=%d sends=%#v events=%#v", uncertain, fw.sentCalls, events)
	}
	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != WorkNeedsInput || got.OwnerSessionID != "" || got.OwnerDelegated {
		t.Fatalf("absent reconcile Work=%#v", got)
	}
}

func TestDelegatedSessionRemovalKeepsSingleTerminalFailureWithoutFollowupStale(t *testing.T) {
	// A bound provider failed terminal wakes exactly once; later removal and
	// reconcile cycles add no stale and never reopen the immutable turn.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-removed:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Removed delegated Session",
		Objective:        "Preserve the authoritative terminal failure.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := now.Add(-time.Hour)
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
	})
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt)
	applyReceiptAdmission(t, store, sessionID, turnID, admission)
	applyProviderTerminal(t, store, sessionID, turnID, "failed", admission)

	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	stale := 0
	for _, event := range events {
		if event.Kind == "session.failed" {
			failed++
		}
		if event.Kind == "session.stale" {
			stale++
		}
	}
	if failed != 1 || stale != 0 || len(fw.sentCalls) != 1 {
		t.Fatalf("removed terminal failed=%d stale=%d Events=%#v sends=%#v", failed, stale, events, fw.sentCalls)
	}
}

func TestDelegatedSessionRemovalAfterDoneDoesNotCreateFalseFailure(t *testing.T) {
	// A bound provider done terminal is immutable: removal and reconcile
	// cycles never produce a failure or stale row.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-completed:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Completed delegated Session",
		Objective:        "Keep cleanup distinct from execution failure.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := now.Add(-time.Hour)
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
	})
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt)
	applyReceiptAdmission(t, store, sessionID, turnID, admission)
	applyProviderTerminal(t, store, sessionID, turnID, "done", admission)

	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	failed := 0
	stale := 0
	for _, event := range events {
		switch event.Kind {
		case "session.done":
			done++
		case "session.failed":
			failed++
		case "session.stale":
			stale++
		}
	}
	if done != 1 || failed != 0 || stale != 0 || len(fw.sentCalls) != 1 {
		t.Fatalf("completed cleanup done=%d failed=%d stale=%d Events=%#v sends=%#v", done, failed, stale, events, fw.sentCalls)
	}
}

func TestTerminalLifecycleSuppressesMissingOwnerStaleAcrossReopen(t *testing.T) {
	// A canonical terminal turn is immutable and its terminal Event was
	// derived at fact-apply time: restart reconcile never appends stale and
	// never detaches or rewrites the terminal Work (the owner binding is
	// durable).
	for _, test := range []struct {
		kind       string
		nextAction string
	}{
		{kind: "session.done", nextAction: "Review the delegated Session result."},
		{kind: "session.failed", nextAction: "Inspect the delegated Session failure."},
	} {
		t.Run(test.kind, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			ownerID := "brain-agent-terminal:@2"
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title:            "Terminal delegated Session",
				Objective:        "Keep terminal lifecycle monotonic after cleanup.",
				Status:           WorkRunning,
				OwnerSessionID:   ownerID,
				CompletionPolicy: CompletionBounded,
				NextAction:       "Wait for the delegated Session.",
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			turnID := ownerID + ":turn:1"
			bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
				SessionID: ownerID, TurnID: turnID, AcceptedAt: now,
			})
			admission := providerAdmission("stream", "msg-1", 1, "sha", now)
			applyReceiptAdmission(t, store, ownerID, turnID, admission)
			terminalKind := "done"
			if test.kind == "session.failed" {
				terminalKind = "failed"
			}
			applyProviderTerminal(t, store, ownerID, turnID, terminalKind, admission)

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(reopened, &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}, nil)
			service.now = func() time.Time { return now.Add(time.Hour) }
			service.ReconcileDelegatedSessions(nil)
			service.ReconcileDelegatedSessions(nil)

			got, err := reopened.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := reopened.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			stale := 0
			terminal := 0
			for _, event := range events {
				if event.Kind == "session.stale" {
					stale++
				}
				if event.Kind == test.kind {
					terminal++
				}
			}
			if terminal != 1 || stale != 0 ||
				got.OwnerSessionID != "" || got.OwnerDelegated ||
				got.Status != WorkNeedsInput ||
				got.NextAction != "Review the absent Session outcome and choose the next Work disposition." {
				t.Fatalf("reconciled Work=%#v Events=%#v", got, events)
			}
		})
	}
}

func TestFirstAuthoritativeInventoryReconcilesMissingOwnerExactlyOnce(t *testing.T) {
	// A successful fresh inventory retires a delegated Work owner even when its
	// initial provider Turn never became canonical. It preserves uncertainty as
	// one review obligation and never fabricates a lifecycle terminal.
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 17, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	hostID := "brain-agent-brain-hidden:@1"
	ownerID := "brain-agent-missing:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Missing owner",
		Objective:        "An absent owner cannot remain operational.",
		Status:           WorkRunning,
		OwnerSessionID:   ownerID,
		OwnerDelegated:   true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)

	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerSessionID != "" || got.OwnerDelegated || got.Status != WorkNeedsInput ||
		len(events) != 1 || events[0].Kind != "brain.owner_absent" ||
		events[0].ClaimedAt != nil || len(fw.sentCalls) != 0 {
		t.Fatalf("markerless missing-owner reconcile Work=%#v Events=%#v sends=%#v", got, events, fw.sentCalls)
	}
}

func TestFirstAuthoritativeInventoryDoesNotManageNonDelegatedOwner(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprintf("hidden=%v", hidden), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ownerID := "user-session:@2"
			item, err := store.CreateWork(Work{
				Title:            "Foreign owner",
				Objective:        "Do not manage a non-delegated Session.",
				Status:           WorkRunning,
				OwnerSessionID:   ownerID,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(store, &fakeWatcher{}, nil)
			service.ReconcileDelegatedSessions([]*classifier.Agent{{
				ID:        ownerID,
				State:     classifier.StateRunning,
				Hidden:    hidden,
				Delegated: false,
			}})
			service.ReconcileDelegatedSessions(nil)

			got, err := store.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, item) || len(events) != 0 {
				t.Fatalf("non-delegated owner was managed: Work=%#v Events=%#v", got, events)
			}
		})
	}
}

func TestNonDelegatedSessionCannotBeClaimedByBrain(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  "user-session:@1",
		NewState: string(classifier.StateDone),
		Agent: &classifier.Agent{
			ID:        "user-session:@1",
			State:     classifier.StateDone,
			Delegated: false,
		},
	})
	if err != nil || woke {
		t.Fatalf("non-delegated Session routed: woke=%v err=%v", woke, err)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || len(events) != 0 {
		t.Fatalf("non-delegated Session created scheduler state: Work=%#v Events=%#v", items, events)
	}
}

func TestCalendarScheduledActionProjectsIdempotentlyWithoutOwningDelivery(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	scheduledFor := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	sourceThreadID := "brain-thread-immutable"
	item := calendar.Item{
		ID:                "calendar-item-1",
		Title:             "Morning report",
		Kind:              calendar.KindScheduledAction,
		ActionInstruction: "Generate the report.",
		SourceThreadID:    sourceThreadID,
		Runs: []calendar.Run{{
			ID:             "calendar-run-1",
			Title:          "Morning report",
			SourceThreadID: sourceThreadID,
			ScheduledFor:   scheduledFor,
			Status:         calendar.StatusRunning,
			AgentSession:   "brain-agent-calendar:@2",
		}},
	}

	if woke, err := service.RouteCalendarEvent(calendar.Event{Item: item}); err != nil || woke {
		t.Fatalf("launch projection woke=%v err=%v", woke, err)
	}
	sessionDone := watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  item.Runs[0].AgentSession,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		Agent: &classifier.Agent{
			ID:        item.Runs[0].AgentSession,
			State:     classifier.StateDone,
			Delegated: true,
			UpdatedAt: scheduledFor.Add(30 * time.Second),
		},
	}
	now = scheduledFor.Add(30 * time.Second)
	if woke, err := service.RouteSessionEvent(sessionDone); err != nil || woke {
		t.Fatalf("Calendar-owned raw Session result woke=%v err=%v", woke, err)
	}
	item.Runs[0].Status = calendar.StatusCompleted
	finished := scheduledFor.Add(time.Minute)
	item.Runs[0].FinishedAt = &finished
	result := &calendar.ScheduledResult{
		ID:             "calendar-result-1",
		ThreadID:       sourceThreadID,
		Body:           "Canonical Calendar result",
		CreatedAt:      finished,
		Status:         calendar.StatusCompleted,
		Title:          item.Title,
		CalendarItemID: item.ID,
		CalendarRunID:  item.Runs[0].ID,
		ScheduledFor:   scheduledFor,
	}
	terminal := calendar.Event{Item: item, ScheduledResult: result}
	now = finished
	if woke, err := service.RouteCalendarEvent(terminal); err != nil || !woke {
		t.Fatalf("result projection woke=%v err=%v", woke, err)
	}
	if woke, err := service.RouteCalendarEvent(terminal); err != nil || woke {
		t.Fatalf("duplicate Calendar result woke=%v err=%v", woke, err)
	}
	sessionDone.OldState = string(classifier.StateDone)
	sessionDone.NewState = string(classifier.StateRunning)
	sessionDone.Agent.State = classifier.StateRunning
	sessionDone.Agent.Summary = "Late Session monitoring progress"
	if woke, err := service.RouteSessionEvent(sessionDone); err != nil || woke {
		t.Fatalf("late Calendar Session progress woke=%v err=%v", woke, err)
	}

	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != WorkDone ||
		items[0].OwnerSessionID != "brain-agent-calendar:@2" {
		t.Fatalf("Calendar Work = %#v", items)
	}
	// The raw Calendar Session result is not routed (no canonical turn): only
	// the idempotent Calendar projection rows exist.
	if len(events) != 2 || events[0].Kind != "calendar.launched" ||
		events[1].Kind != "calendar.result" || events[1].PayloadRef != result.ID {
		t.Fatalf("Calendar Events = %#v", events)
	}
	if terminal.Item.SourceThreadID != sourceThreadID ||
		terminal.Item.Runs[0].SourceThreadID != sourceThreadID ||
		terminal.ScheduledResult.ThreadID != sourceThreadID {
		t.Fatalf("Brain projection retargeted Calendar delivery: %#v", terminal)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("Calendar result turns = %#v", fw.sentCalls)
	}
}

// TestLegacyUnscopedLifecycleRowsNeverSchedulerEligible covers upgrade
// safety: actionable delegated lifecycle rows persisted before the canonical
// TurnID gate (occurrence-counted or bare-session dedupe keys) remain durable
// audit rows but are never scheduler-eligible; only the reducer's turn-scoped
// rows can wake Brain, and non-lifecycle rows keep their eligibility.
func TestLegacyUnscopedLifecycleRowsNeverSchedulerEligible(t *testing.T) {
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	database := orchestrationDatabase{
		BrainWork: []Work{{ID: "work-1", Status: WorkWaiting}},
	}
	legacy := WorkEvent{
		WorkID:     "work-1",
		Kind:       "session.done",
		DedupeKey:  "session:agent-1:session.done:1",
		Actionable: true,
		CreatedAt:  base,
	}
	if workEventSchedulerEligible(database, legacy) {
		t.Fatal("legacy unscoped lifecycle row became scheduler-eligible")
	}
	scoped := legacy
	scoped.DedupeKey = "session:agent-1:turn:turn-1:session.done"
	if !workEventSchedulerEligible(database, scoped) {
		t.Fatal("turn-scoped lifecycle row is not scheduler-eligible")
	}
	// The canonical ledger shape embeds the Session ID inside the TurnID
	// (turnID = sessionID+":turn:N"); the key still contains exactly one
	// scope marker and must stay eligible.
	embedded := legacy
	embedded.DedupeKey = "session:brain-agent-worker:@2:turn:brain-agent-worker:@2:turn:1:session.stale"
	if !workEventSchedulerEligible(database, embedded) {
		t.Fatal("embedded-turnID lifecycle row is not scheduler-eligible")
	}
	// A user-authorized replay of a held lifecycle delivery is the one
	// explicit second wake and stays eligible despite its non-turn-scoped key.
	replay := legacy
	replay.DedupeKey = "delivery:event-1:replay:nonce"
	replay.ReplayOf = "event-1"
	if !workEventSchedulerEligible(database, replay) {
		t.Fatal("authorized replay lost scheduler eligibility")
	}
	plain := legacy
	plain.Kind = "provider.changed"
	plain.DedupeKey = "provider:agent-1:changed"
	if !workEventSchedulerEligible(database, plain) {
		t.Fatal("non-lifecycle row lost scheduler eligibility")
	}
}
