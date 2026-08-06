package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func TestThreadTimelineSurvivesEmptyProviderHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_timeline"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	provider := work.CodexConversation{
		Available: true,
		SessionID: "host-a",
		Events: []work.CodexConversationEvent{{
			ID:        "user-1",
			Timestamp: "2026-08-06T01:00:00Z",
			Kind:      "user_message",
			Role:      "user",
			Body:      "Remember this across host rotation",
		}, {
			ID:        "assistant-1",
			Timestamp: "2026-08-06T01:00:01Z",
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      "Noted.",
		}},
	}
	if err := store.MaterializeProviderConversation(threadID, provider); err != nil {
		t.Fatal(err)
	}
	emptyHost := work.CodexConversation{
		Available: true,
		SessionID: "host-b",
		Events:    nil,
	}
	if err := store.MaterializeProviderConversation(threadID, emptyHost); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("timeline len = %d, want 2 after empty host: %#v", len(items), items)
	}
	events := TimelineItemsToConversationEvents(items, false)
	if len(events) != 2 || events[0].Body != "Remember this across host rotation" || events[1].Body != "Noted." {
		t.Fatalf("conversation events = %#v", events)
	}
}

func TestWorkCardMaterializesOnceWithSupersedes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_cards"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Delegated work",
		Objective:        "Do the thing",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "needs-1",
		Actionable: true,
		Summary:    "Need a decision",
		SourceName: "agent-a",
		PayloadRef: "session:sess-a",
	})
	if err != nil || !created {
		t.Fatalf("first event = %#v created=%v err=%v", first, created, err)
	}
	card1, materialized, err := store.MaterializeWorkCard(threadID, item, first)
	if err != nil || !materialized {
		t.Fatalf("card1 = %#v materialized=%v err=%v", card1, materialized, err)
	}
	card1Again, materialized, err := store.MaterializeWorkCard(threadID, item, first)
	if err != nil || materialized || card1Again.ID != card1.ID {
		t.Fatalf("exact-once failed: again=%#v materialized=%v err=%v", card1Again, materialized, err)
	}

	second, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "done-1",
		Actionable: true,
		Summary:    "Finished",
		SourceName: "agent-a",
		PayloadRef: "session:sess-a",
	})
	if err != nil || !created {
		t.Fatalf("second event = %#v created=%v err=%v", second, created, err)
	}
	card2, materialized, err := store.MaterializeWorkCard(threadID, item, second)
	if err != nil || !materialized {
		t.Fatalf("card2 = %#v materialized=%v err=%v", card2, materialized, err)
	}
	if card2.Supersedes != card1.ID {
		t.Fatalf("supersedes = %q, want %q", card2.Supersedes, card1.ID)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := TimelineItemsToConversationEvents(items, false)
	if len(visible) != 1 || visible[0].ID != card2.ID || visible[0].Source != "work_result" {
		t.Fatalf("visible cards = %#v", visible)
	}
	all := TimelineItemsToConversationEvents(items, true)
	if len(all) != 2 {
		t.Fatalf("audit timeline cards = %#v", all)
	}
}

func TestBackfillCurrentWorkCardsDoesNotFloodHistorical(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_backfill"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Historical work",
		Objective:        "Many lifecycle events",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		now = now.Add(time.Minute)
		kind := "session.done"
		if index < 4 {
			kind = "session.needs_input"
		}
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID:     item.ID,
			Kind:       kind,
			DedupeKey:  "hist-" + string(rune('a'+index)),
			Actionable: true,
			Summary:    "historical-" + string(rune('a'+index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	status := WorkDone
	if _, err := store.UpdateWork(item.ID, WorkUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillCurrentWorkCardsOnce(threadID); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillCurrentWorkCardsOnce(threadID); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	cards := 0
	for _, timelineItem := range items {
		if timelineItem.Kind == timelineKindWorkCard {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("backfill cards = %d want 1 (no historical flood): %#v", cards, items)
	}
}

func TestMarkWorkReadClearsTimelineUnread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_read"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Needs input",
		Objective:        "Ask user",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "need",
		Actionable: true,
		Summary:    "Please choose",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MaterializeWorkCard(threadID, item, event); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	if err := service.MarkWorkRead(item.ID); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Unread {
		t.Fatalf("timeline after read = %#v", items)
	}
}

func TestServiceAppendWorkEventMaterializesCard(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_service_card"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Service path",
		Objective:        "Materialize on append",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "svc-need",
		Actionable: false,
		Summary:    "Current needs input",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != event.ID || items[0].EventKind != "session.needs_input" {
		t.Fatalf("service materialize = %#v", items)
	}
}
