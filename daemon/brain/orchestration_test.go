package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestOrchestrationSchemaV0MigratesDeterministically(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "orchestration.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"schema_version\":0}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	database, migrated, err := decodeOrchestrationDatabase(first)
	if err != nil {
		t.Fatal(err)
	}
	if migrated || database.SchemaVersion != orchestrationSchemaVersion {
		t.Fatalf("database = %#v, migrated=%v", database, migrated)
	}
	if database.BrainWork == nil || len(database.BrainWork) != 0 ||
		database.BrainWorkEvents == nil || len(database.BrainWorkEvents) != 0 {
		t.Fatalf("migrated tables = %#v / %#v", database.BrainWork, database.BrainWorkEvents)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("second open rewrote deterministic migration:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestOrchestrationSchemaV2DropsDeliveryCeremonyDeterministically(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "orchestration.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	hostRaw, err := json.Marshal(hostSessionFile{ID: hostID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "host_session.json"), append(hostRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	legacy := legacyOrchestrationDatabase{
		SchemaVersion: 2,
		BrainWork: []Work{{
			ID:               "work-v1",
			Title:            "Legacy claim",
			Objective:        "Preserve the Event while adding delivery evidence.",
			Status:           WorkWaiting,
			CompletionPolicy: CompletionBounded,
			CreatedAt:        fixed,
			UpdatedAt:        fixed,
		}},
		BrainWorkEvents: []legacyWorkEvent{{
			ID:                    "event-v1",
			WorkID:                "work-v1",
			Kind:                  "session.done",
			DedupeKey:             "session:legacy:done",
			Actionable:            true,
			CreatedAt:             fixed,
			ClaimedAt:             &fixed,
			ClaimToken:            "unbound-v1-token",
			DeliveryHostSessionID: hostID,
		}},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt == nil ||
		events[0].DeliveryHostSessionID != hostID {
		t.Fatalf("v2 claim migration = %#v", events)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("second open rewrote v2 migration:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if bytes.Contains(first, []byte("claim_token")) ||
		bytes.Contains(first, []byte("delivery_acknowledged_at")) {
		t.Fatalf("v2 delivery ceremony survived forward migration:\n%s", first)
	}
}

func TestDelegatedSessionMigrationIsOneWay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return fixed }

	migrated, err := store.MigrateDelegatedSessionsV1([]Work{{
		Title:            "Existing delegated session",
		Objective:        "Preserve existing durable execution ownership.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-existing:@1",
		CompletionPolicy: CompletionBounded,
		WaitFor:          "Session brain-agent-existing:@1",
	}})
	if err != nil || !migrated {
		t.Fatalf("first migration migrated=%v err=%v", migrated, err)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != legacySessionWorkID("brain-agent-existing:@1") {
		t.Fatalf("migrated Work = %#v", items)
	}

	migrated, err = store.MigrateDelegatedSessionsV1([]Work{{
		Title:            "Late legacy session",
		Objective:        "Must not enter through a permanent fallback.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-late:@2",
		CompletionPolicy: CompletionBounded,
	}})
	if err != nil || migrated {
		t.Fatalf("second migration migrated=%v err=%v", migrated, err)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err = reopened.MigrateDelegatedSessionsV1(nil)
	if err != nil || migrated {
		t.Fatalf("reopened migration migrated=%v err=%v", migrated, err)
	}
	items, err = reopened.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OwnerSessionID != "brain-agent-existing:@1" {
		t.Fatalf("one-way migration was replayed: %#v", items)
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
				DedupeKey:  "session:worker:@1:done:42",
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
		WorkID: item.ID, Kind: "session.done", DedupeKey: "session:worker:@1:done", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != event.ID {
		t.Fatalf("claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(event.ID, "different-host:@1"); !errors.Is(err, ErrEventClaim) {
		t.Fatalf("different Host consumed assigned Event: err=%v", err)
	}
	gotEvent, gotWork, err := store.ConsumeClaimedWorkEvent(event.ID, hostID)
	if err != nil || gotEvent.ID != event.ID || gotWork.ID != item.ID || gotEvent.ConsumedAt == nil {
		t.Fatalf("consume event=%#v work=%#v err=%v", gotEvent, gotWork, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(event.ID, hostID); !errors.Is(err, ErrEventClaim) {
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
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID:     item.ID,
			Kind:       "session.stale",
			DedupeKey:  "session:terminal:" + string(status),
			Actionable: true,
		}); err != nil {
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
		DedupeKey:  "session:claimed-then-terminal",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != terminalEvent.ID {
		t.Fatalf("active Event claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	now = base.Add(3*time.Hour + 34*time.Minute)
	status := WorkDone
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
		DedupeKey:  "session:later-active",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err = store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || claimed.ID != activeEvent.ID {
		t.Fatalf("later active Event claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	consumed, consumedWork, err := store.ConsumeClaimedWorkEvent(activeEvent.ID, hostID)
	if err != nil || consumed.ID != activeEvent.ID || consumedWork.ID != active.ID ||
		consumed.ConsumedAt == nil {
		t.Fatalf("consume event=%#v work=%#v err=%v", consumed, consumedWork, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(activeEvent.ID, hostID); !errors.Is(err, ErrEventClaim) {
		t.Fatalf("active Event was consumed more than once: err=%v", err)
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
			DedupeKey:  fmt.Sprintf("terminal-boundary:%s", offset),
			Actionable: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil || !ok || claimed.ID != event.ID {
			t.Fatalf("terminal result offset=%s claim=%#v ok=%v err=%v", offset, claimed, ok, err)
		}
		consumed, consumedWork, err := store.ConsumeClaimedWorkEvent(event.ID, hostID)
		if err != nil || consumed.ID != event.ID || consumedWork.ID != item.ID {
			t.Fatalf("terminal result offset=%s consume=%#v work=%#v err=%v",
				offset, consumed, consumedWork, err)
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
		WorkID: readWork.ID, Kind: "session.done", DedupeKey: "read-result", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNextActionableEvent(hostID); err != nil || !ok {
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
	consumedRead, _, err := store.ConsumeClaimedWorkEvent(readEvent.ID, hostID)
	if err != nil || consumedRead.ConsumedAt == nil {
		t.Fatalf("exact accepted claim was not consumable after card acknowledgement: event=%#v err=%v", consumedRead, err)
	}
	readEvents, err := store.ListWorkEvents(readWork.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(readEvents) != 1 || readEvents[0].ID != readEvent.ID || readEvents[0].ReadAt == nil {
		t.Fatalf("read Event acknowledgement was not preserved: %#v", readEvents)
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
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: unclaimedReadWork.ID, Kind: "session.done", DedupeKey: "read-before-claim", Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkRead(unclaimedReadWork.ID); err != nil {
		t.Fatal(err)
	}
	readBeforeClaim, wasClaimed, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !wasClaimed || readBeforeClaim.WorkID != unclaimedReadWork.ID {
		t.Fatalf("card acknowledgement suppressed Event delivery: event=%#v claimed=%v err=%v", readBeforeClaim, wasClaimed, err)
	}
	if _, _, err := store.ConsumeClaimedWorkEvent(readBeforeClaim.ID, hostID); err != nil {
		t.Fatalf("consume card-acknowledged Event: %v", err)
	}

	terminalEvents, err := store.ListWorkEvents(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(terminalEvents) != 1 || terminalEvents[0].ID != terminalEvent.ID ||
		terminalEvents[0].ClaimedAt == nil || terminalEvents[0].ConsumedAt != nil ||
		terminalEvents[0].ReadAt != nil {
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
		if len(events) != 1 || events[0].ReadAt != nil {
			t.Fatalf("terminal Work Event was not preserved unread: work=%s events=%#v", item.ID, events)
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
	if firstAfterStartingSecond != first {
		t.Fatalf("starting Work C mutated Work A:\nbefore=%#v\nafter=%#v", first, firstAfterStartingSecond)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     second.ID,
		Kind:       "session.done",
		DedupeKey:  "session:c:done",
		Actionable: true,
	}); err != nil {
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

func TestRecentWorkResultEventsAreBoundedDeterministicAndPersistAfterRead(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Ship Brain cards",
		Objective:        "Project durable results into the Brain timeline.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-cards:@1",
		CompletionPolicy: CompletionBounded,
		NextAction:       "Review the delegated result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	appendEvent := func(id, kind, payload string, offset time.Duration) {
		t.Helper()
		now = now.Add(offset)
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			ID: id, WorkID: item.ID, Kind: kind, DedupeKey: id,
			PayloadRef: payload, Actionable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent("event-progress", "session.progress", "session:brain-agent-cards:@1", time.Minute)
	appendEvent("event-b", "session.done", "session:brain-agent-closed:@2", time.Minute)
	appendEvent("event-calendar", "calendar.failure", "calendar-result-id", 0)
	appendEvent("event-c", "session.failed", "session:brain-agent-cards:@1", time.Minute)

	events, err := store.RecentWorkResultEvents(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventID != "event-b" || events[1].EventID != "event-c" {
		t.Fatalf("bounded result events = %#v", events)
	}
	if events[0].SessionID != "brain-agent-closed:@2" ||
		events[1].SessionID != "brain-agent-cards:@1" ||
		events[0].Summary != item.NextAction || !events[0].Unread {
		t.Fatalf("result projection = %#v", events)
	}
	wire, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, internal := range []string{
		"dedupe_key", "payload_ref", "actionable", "claimed_at",
		"delivery_host_session_id", "consumed_at", "read_at",
	} {
		if strings.Contains(string(wire), internal) {
			t.Fatalf("result projection leaked %q: %s", internal, wire)
		}
	}
	if err := store.MarkWorkRead(item.ID); err != nil {
		t.Fatal(err)
	}
	events, err = store.RecentWorkResultEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("read result events disappeared: %#v", events)
	}
	for _, event := range events {
		if event.Unread {
			t.Fatalf("read state not projected: %#v", events)
		}
	}
}

func TestRecentWorkResultEventsExcludeCanonicalCalendarPresentations(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Scheduled review",
		Objective:        "Keep Calendar results in their canonical conversation projection.",
		Status:           WorkDone,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"calendar.result", "calendar.failure"} {
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID: item.ID, Kind: kind, DedupeKey: kind,
			PayloadRef: "scheduled-result-id", Actionable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.RecentWorkResultEvents(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("calendar results duplicated as supplemental result events: %#v", events)
	}
	stored, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("calendar Work Events were mutated: %#v", stored)
	}
}

func TestRecentWorkResultEventsPreserveOccurrenceFactsAcrossWorkMutationAndReload(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Iterate on one Work",
		Objective:        "Original objective.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-iterations:@1",
		CompletionPolicy: CompletionBounded,
		NextAction:       "Original next action.",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, fact := range []struct {
		id      string
		summary string
		source  string
	}{
		{id: "event-first", summary: "First occurrence completed the daemon contract.", source: "Daemon worker"},
		{id: "event-second", summary: "Second occurrence completed the App projection.", source: "App worker"},
	} {
		now = now.Add(time.Minute)
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			ID:         fact.id,
			WorkID:     item.ID,
			Kind:       "session.done",
			DedupeKey:  fmt.Sprintf("done:%d", index),
			PayloadRef: "session:brain-agent-iterations:@1",
			SourceName: fact.source,
			Summary:    fact.summary,
			Actionable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mutatedObjective := "Mutated objective that must not rewrite historical occurrences."
	mutatedNext := "Mutated next action."
	if _, err := store.UpdateWork(item.ID, WorkUpdate{
		Objective:  &mutatedObjective,
		NextAction: &mutatedNext,
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := reloaded.RecentWorkResultEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 ||
		events[0].Summary != "First occurrence completed the daemon contract." ||
		events[0].SessionName != "Daemon worker" ||
		events[1].Summary != "Second occurrence completed the App projection." ||
		events[1].SessionName != "App worker" {
		t.Fatalf("immutable occurrence facts = %#v", events)
	}
}

func TestRecentWorkResultEventsCompactLongUnicodeLegacyFallback(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	longObjective := strings.Repeat("界", 500) +
		"\n\nLifecycle protocol and raw prompt body must not enter the card."
	item, err := store.CreateWork(Work{
		Title:            "Bound the result",
		Objective:        longObjective,
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		ID: "legacy-stale", WorkID: item.ID, Kind: "session.stale",
		DedupeKey: "legacy-stale", Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.RecentWorkResultEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 ||
		!utf8.ValidString(events[0].Summary) ||
		utf8.RuneCountInString(events[0].Summary) != workResultSummaryRuneLimit ||
		!strings.HasSuffix(events[0].Summary, "…") ||
		strings.Contains(events[0].Summary, "Lifecycle protocol") {
		t.Fatalf("compact Unicode fallback = %#v", events)
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

func TestAttachWorkOwnerCompareAndSetHasOneConcurrentWinner(t *testing.T) {
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
			attached, attachErr := store.AttachWorkOwner(item.ID, sessionID)
			if attachErr == nil {
				winners.Add(1)
				winnerMu.Lock()
				winnerID = attached.OwnerSessionID
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
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
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
		if woke, err := service.DispatchPendingEvent(); err != nil || woke {
			t.Fatalf("idle until_done Work woke: woke=%v err=%v", woke, err)
		}
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.progress",
		DedupeKey:  "progress:1",
		Actionable: false,
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("passive event woke: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 0 {
		t.Fatalf("idle scheduler sent %#v", fw.sentCalls)
	}

	actionable, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "done:1",
		PayloadRef: "session:worker:@1",
		SourceName: "Worker One",
		Summary:    "Completed the delegated change.",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || !woke {
		t.Fatalf("actionable event did not wake: woke=%v err=%v", woke, err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
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
	if err != nil || len(events) != 2 || events[1].ID != actionable.ID || events[1].ConsumedAt == nil {
		t.Fatalf("accepted direct Event was not consumed exactly: events=%#v err=%v", events, err)
	}
	cards, err := store.RecentWorkResultEvents(10)
	if err != nil || len(cards) != 1 || cards[0].EventID != actionable.ID ||
		cards[0].WorkTitle != item.Title || cards[0].Summary != actionable.Summary {
		t.Fatalf("durable card projection diverged after direct consumption: cards=%#v err=%v", cards, err)
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

	if woke, err := service.DispatchPendingEvent(); err == nil || woke {
		t.Fatalf("failed send woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt == nil || events[0].ConsumedAt != nil {
		t.Fatalf("failed send claim = %#v", events)
	}
	fw.sendErr = nil
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("uncertain delivery retried: woke=%v err=%v", woke, err)
	}
	retryAt := now.Add(24 * time.Hour)
	store.now = func() time.Time { return retryAt }
	service.now = func() time.Time { return retryAt }
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("elapsed time caused an ambiguous retry: woke=%v err=%v", woke, err)
	}
	events, err = store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].ClaimedAt == nil || events[0].ConsumedAt != nil || len(fw.sentCalls) != 1 {
		t.Fatalf("ambiguous failed send did not remain closed: events=%#v sends=%#v", events, fw.sentCalls)
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
		DedupeKey:  "accepted:persist-failure",
		SourceName: "Worker",
		Summary:    "Provider accepted the direct Event.",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	writeOrchestration := store.writeOrchestration
	writes := 0
	store.writeOrchestration = func(path string, value any) error {
		writes++
		if writes == 2 {
			return errors.New("injected consumed_at persistence failure")
		}
		return writeOrchestration(path, value)
	}

	if woke, dispatchErr := NewService(store, fw, nil).DispatchPendingEvent(); dispatchErr == nil || woke {
		t.Fatalf("persistence failure woke=%v err=%v", woke, dispatchErr)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID ||
		events[0].ClaimedAt == nil || events[0].ConsumedAt != nil ||
		len(fw.sentCalls) != 1 || fw.outcomes[event.ID] != watcher.InputAccepted {
		t.Fatalf("accepted persistence boundary events=%#v sends=%#v outcomes=%#v", events, fw.sentCalls, fw.outcomes)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if woke, dispatchErr := NewService(restarted, fw, nil).DispatchPendingEvent(); dispatchErr != nil || woke {
		t.Fatalf("accepted receipt finalization woke=%v err=%v", woke, dispatchErr)
	}
	events, err = restarted.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID || events[0].ConsumedAt == nil ||
		len(fw.sentCalls) != 1 {
		t.Fatalf("restart did not finalize without resend: events=%#v sends=%#v", events, fw.sentCalls)
	}
	if woke, dispatchErr := NewService(restarted, fw, nil).DispatchPendingEvent(); dispatchErr != nil || woke ||
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
				DedupeKey:  provider + ":definite-pre-mutation",
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
			if woke, dispatchErr := NewService(store, failedWatcher, nil).DispatchPendingEvent(); dispatchErr == nil || woke {
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
			restartedWatcher := &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}
			if woke, dispatchErr := NewService(restarted, restartedWatcher, nil).DispatchPendingEvent(); dispatchErr != nil || !woke {
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
				DedupeKey:  provider + ":ambiguous",
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
			if woke, dispatchErr := NewService(store, failedWatcher, nil).DispatchPendingEvent(); dispatchErr == nil || woke {
				t.Fatalf("ambiguous dispatch woke=%v err=%v", woke, dispatchErr)
			}

			restarted, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			restartedWatcher := &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}
			if woke, dispatchErr := NewService(restarted, restartedWatcher, nil).DispatchPendingEvent(); dispatchErr != nil || woke {
				t.Fatalf("restart replayed ambiguity: woke=%v err=%v", woke, dispatchErr)
			}
			events, err := restarted.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].ID != event.ID || events[0].ClaimedAt == nil ||
				len(restartedWatcher.sentCalls) != 0 {
				t.Fatalf("ambiguous Event did not remain singly claimed: events=%#v sends=%#v",
					events, restartedWatcher.sentCalls)
			}
		})
	}
}

func TestUserSteeringPreemptsUnclaimedWorkEvent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Background result",
		Objective:        "Preserve the result while the user is steering.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "done:user-precedence",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	if !service.NoteUserSteering(hostID) {
		t.Fatal("host user input was not recognized")
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("internal event preempted foreground: woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt != nil || events[0].ConsumedAt != nil {
		t.Fatalf("preempted event was not preserved unclaimed: %#v", events)
	}

	woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  hostID,
		NewState: string(classifier.StateDone),
		Agent:    &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
	})
	if err != nil || !woke {
		t.Fatalf("queued event did not run after foreground turn: woke=%v err=%v", woke, err)
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

	if !service.NoteUserSteering(hostID) {
		t.Fatal("conversation-only Brain input was not recognized")
	}
	service.CancelUserSteering(hostID)
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0] != running {
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
	for _, state := range []classifier.AgentState{
		classifier.StateDone,
		classifier.StateFailed,
		classifier.StateBlocked,
	} {
		t.Run(string(state), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			sessionID := "brain-agent-worker:@2"
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}
			service := NewService(store, fw, nil)
			if _, err := store.CreateWork(Work{
				Title:            "Delegated change",
				Objective:        "Handle one terminal transition.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
			}); err != nil {
				t.Fatal(err)
			}
			agent := &classifier.Agent{
				ID:        sessionID,
				Name:      "Worker",
				Summary:   "Captured at the terminal transition.",
				State:     state,
				Delegated: true,
				UpdatedAt: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
			}
			event := watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  sessionID,
				OldState: string(classifier.StateRunning),
				NewState: string(state),
				Agent:    agent,
			}
			first, err := service.RouteSessionEvent(event)
			if err != nil || !first {
				t.Fatalf("first transition woke=%v err=%v", first, err)
			}
			restartedProjection := event
			restartedAgent := *agent
			restartedAgent.UpdatedAt = agent.UpdatedAt.Add(time.Hour)
			restartedProjection.Agent = &restartedAgent
			restartedProjection.OldState = ""
			second, err := service.RouteSessionEvent(restartedProjection)
			if err != nil || second {
				t.Fatalf("duplicate transition woke=%v err=%v", second, err)
			}
			events, err := store.ListWorkEvents("")
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || len(fw.sentCalls) != 1 ||
				events[0].SourceName != "Worker" ||
				events[0].Summary != "Captured at the terminal transition." {
				t.Fatalf("events=%#v sends=%#v", events, fw.sentCalls)
			}
		})
	}
}

func TestDelegatedSessionDedupeAllowsANewLifecycleEpisode(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	if _, err := store.CreateWork(Work{
		Title:            "Lifecycle episodes",
		Objective:        "Dedupe repeated facts without suppressing a later blocker.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	route := func(oldState, newState classifier.AgentState, updated time.Time) bool {
		t.Helper()
		woke, routeErr := service.RouteSessionEvent(watcher.SessionEvent{
			Type:     "agent_state_change",
			AgentID:  sessionID,
			OldState: string(oldState),
			NewState: string(newState),
			Agent: &classifier.Agent{
				ID:        sessionID,
				State:     newState,
				Delegated: true,
				UpdatedAt: updated,
			},
		})
		if routeErr != nil {
			t.Fatal(routeErr)
		}
		return woke
	}
	if !route(classifier.StateRunning, classifier.StateBlocked, at) {
		t.Fatal("first blocker did not wake")
	}
	if route(classifier.StateBlocked, classifier.StateRunning, at.Add(time.Minute)) {
		t.Fatal("running progress unexpectedly woke")
	}
	if !route(classifier.StateRunning, classifier.StateBlocked, at.Add(2*time.Minute)) {
		t.Fatal("new blocker episode was over-deduplicated")
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || len(fw.sentCalls) != 2 {
		t.Fatalf("events=%#v sends=%#v", events, fw.sentCalls)
	}
	actionable := []WorkEvent{}
	for _, event := range events {
		if event.Actionable {
			actionable = append(actionable, event)
		}
	}
	if len(actionable) != 2 || actionable[0].ConsumedAt == nil || actionable[1].ConsumedAt == nil {
		t.Fatalf("turn-keyed Events were not each consumed exactly: %#v", actionable)
	}
	firstInput := decodeDirectWorkEventInput(t, fw.sentCalls[0].text)
	secondInput := decodeDirectWorkEventInput(t, fw.sentCalls[1].text)
	if firstInput.EventID != actionable[0].ID || secondInput.EventID != actionable[1].ID ||
		firstInput.EventID == secondInput.EventID {
		t.Fatalf("turn deliveries=(%#v, %#v), Events=%#v", firstInput, secondInput, actionable)
	}
}

func TestDelegatedSessionReconciliationKeepsLiveOverdueSessionNonActionable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
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
	if _, err := store.CreateWork(Work{
		Title:            "Leased work",
		Objective:        "Wait for a healthy lease.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatal(err)
	}
	progressAt := now.Add(-time.Minute)
	nextCheck := now.Add(time.Minute)
	agent := &classifier.Agent{
		ID:                  sessionID,
		State:               classifier.StateRunning,
		Delegated:           true,
		PaneAlive:           true,
		ProcessID:           4242,
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &nextCheck,
		UpdatedAt:           progressAt,
	}

	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("healthy lease polled Brain: events=%#v sends=%#v", events, fw.sentCalls)
	}

	expired := now.Add(-time.Second)
	agent.ExpectedNextCheckAt = &expired
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err = store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("healthy overdue Session became actionable: events=%#v sends=%#v", events, fw.sentCalls)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 ||
		!strings.Contains(items[0].WaitFor, "is live; progress lease overdue") ||
		items[0].NextAction != "Wait for authoritative delegated Session state." {
		t.Fatalf("healthy overdue Work = %#v", items)
	}
}

func TestDelegatedSessionReconciliationStalesObservedMissingOrDeadExactlyOnce(t *testing.T) {
	for _, test := range []struct {
		name      string
		reconcile func(*Service, *classifier.Agent)
	}{
		{
			name: "missing after healthy inventory",
			reconcile: func(service *Service, _ *classifier.Agent) {
				service.ReconcileDelegatedSessions(nil)
				service.ReconcileDelegatedSessions(nil)
			},
		},
		{
			name: "dead process and pane",
			reconcile: func(service *Service, agent *classifier.Agent) {
				expired := time.Date(2026, 8, 3, 9, 59, 0, 0, time.UTC)
				dead := *agent
				dead.State = classifier.StateUnknown
				dead.PaneAlive = false
				dead.ProcessID = 0
				dead.ExpectedNextCheckAt = &expired
				service.ReconcileDelegatedSessions([]*classifier.Agent{&dead})
				service.ReconcileDelegatedSessions([]*classifier.Agent{&dead})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
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
				Objective:        "Wake only when the delegated Session is absent or dead.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			progressAt := now.Add(-time.Minute)
			nextCheck := now.Add(time.Minute)
			agent := &classifier.Agent{
				ID:                  sessionID,
				State:               classifier.StateRunning,
				Delegated:           true,
				PaneAlive:           true,
				ProcessID:           4242,
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &nextCheck,
				UpdatedAt:           progressAt,
			}
			service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
			test.reconcile(service, agent)

			got, err := store.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.OwnerSessionID != "" ||
				got.Status != WorkWaiting ||
				len(events) != 1 ||
				events[0].Kind != "session.stale" ||
				!events[0].Actionable ||
				len(fw.sentCalls) != 1 {
				t.Fatalf("reconciled Work=%#v Events=%#v sends=%#v", got, events, fw.sentCalls)
			}
		})
	}
}

func TestDelegatedSessionRemovalKeepsSingleTerminalFailureWithoutFollowupStale(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-removed:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
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
	live := &classifier.Agent{
		ID:        sessionID,
		State:     classifier.StateRunning,
		Delegated: true,
		PaneAlive: true,
		ProcessID: 4242,
	}
	service.ReconcileDelegatedSessions([]*classifier.Agent{live})
	removed := *live
	removed.State = classifier.StateRemoved
	removed.PaneAlive = false
	removed.ProcessID = 0
	if woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_removed",
		AgentID:  sessionID,
		Agent:    &removed,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateRemoved),
	}); err != nil || !woke {
		t.Fatalf("removed terminal woke=%v err=%v", woke, err)
	}
	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)

	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "session.failed" || len(fw.sentCalls) != 1 {
		t.Fatalf("removed terminal Events=%#v sends=%#v", events, fw.sentCalls)
	}
}

func TestDelegatedSessionRemovalAfterDoneDoesNotCreateFalseFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-completed:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
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
	done := &classifier.Agent{
		ID:        sessionID,
		State:     classifier.StateDone,
		Delegated: true,
		Summary:   "Accepted result",
	}
	if woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    done,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
	}); err != nil || !woke {
		t.Fatalf("done terminal woke=%v err=%v", woke, err)
	}

	removed := *done
	removed.State = classifier.StateRemoved
	if woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_removed",
		AgentID:  sessionID,
		Agent:    &removed,
		NewState: string(classifier.StateRemoved),
	}); err != nil || woke {
		t.Fatalf("completed cleanup woke=%v err=%v", woke, err)
	}

	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "session.done" || len(fw.sentCalls) != 1 {
		t.Fatalf("completed cleanup Events=%#v sends=%#v", events, fw.sentCalls)
	}
}

func TestTerminalLifecycleSuppressesMissingOwnerStaleAcrossReopen(t *testing.T) {
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
				Status:           WorkWaiting,
				OwnerSessionID:   ownerID,
				CompletionPolicy: CompletionBounded,
				NextAction:       test.nextAction,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, created, err := store.AppendWorkEvent(WorkEvent{
				WorkID:     item.ID,
				Kind:       test.kind,
				DedupeKey:  "session:" + ownerID + ":turn:one:" + test.kind,
				PayloadRef: "session:" + ownerID,
				Actionable: true,
			}); err != nil || !created {
				t.Fatalf("append terminal Event created=%v err=%v", created, err)
			}

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(reopened, &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}, nil)
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
				got.OwnerSessionID != "" ||
				got.Status != WorkWaiting ||
				got.NextAction != test.nextAction {
				t.Fatalf("reconciled Work=%#v Events=%#v", got, events)
			}
		})
	}
}

func TestFirstAuthoritativeInventoryReconcilesMissingOwnerExactlyOnce(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	hostID := "brain-agent-brain-hidden:@1"
	ownerID := "brain-agent-missing:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Missing owner",
		Objective:        "Surface a Session missing after restart.",
		Status:           WorkRunning,
		OwnerSessionID:   ownerID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
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
	if got.OwnerSessionID != "" || got.Status != WorkWaiting ||
		len(events) != 1 || events[0].Kind != "session.stale" || !events[0].Actionable ||
		len(fw.sentCalls) != 1 {
		t.Fatalf("first reconciliation Work=%#v Events=%#v sends=%#v", got, events, fw.sentCalls)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reopened, fw, nil)
	restarted.now = func() time.Time { return now.Add(time.Minute) }
	restarted.ReconcileDelegatedSessions(nil)
	events, err = reopened.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(fw.sentCalls) != 1 {
		t.Fatalf("restart duplicated missing-owner result: Events=%#v sends=%#v", events, fw.sentCalls)
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
			if got != item || len(events) != 0 {
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
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
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
	if len(events) != 3 || events[0].Kind != "calendar.launched" ||
		events[1].Kind != "session.done" || events[1].Actionable ||
		events[2].Kind != "calendar.result" || events[2].PayloadRef != result.ID {
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
