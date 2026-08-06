package brain

import (
	"os"
	"path/filepath"
	"strings"
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
	events := TimelineItemsToConversationEvents(items)
	if len(events) != 2 || events[0].Body != "Remember this across host rotation" || events[1].Body != "Noted." {
		t.Fatalf("conversation events = %#v", events)
	}
}

func TestWorkCardMaterializesOnceChronologically(t *testing.T) {
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
	if item.SourceThreadID != threadID {
		t.Fatalf("frozen thread = %q want %q", item.SourceThreadID, threadID)
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
	card1, materialized, err := store.MaterializeWorkCard(item, first)
	if err != nil || !materialized {
		t.Fatalf("card1 = %#v materialized=%v err=%v", card1, materialized, err)
	}
	card1Again, materialized, err := store.MaterializeWorkCard(item, first)
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
	card2, materialized, err := store.MaterializeWorkCard(item, second)
	if err != nil || !materialized {
		t.Fatalf("card2 = %#v materialized=%v err=%v", card2, materialized, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := TimelineItemsToConversationEvents(items)
	if len(visible) != 2 || visible[0].ID != card1.ID || visible[1].ID != card2.ID {
		t.Fatalf("immutable chronological cards = %#v", visible)
	}
}

func TestWorkEventStaysOnFrozenThreadAfterSwitch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadA := "thread-a"
	threadB := "thread-b"
	if err := store.SetChatState(ChatState{ThreadID: threadA, ThreadIDs: []string{threadA}}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Owned by A",
		Objective:        "Stay on A",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceThreadID != threadA {
		t.Fatalf("create froze %q want %q", item.SourceThreadID, threadA)
	}
	if err := store.SetChatState(ChatState{ThreadID: threadB, ThreadIDs: []string{threadA, threadB}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "after-switch",
		Actionable: false,
		Summary:    "Still belongs to A",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	itemsA, err := store.ThreadTimeline(threadA, 0)
	if err != nil {
		t.Fatal(err)
	}
	itemsB, err := store.ThreadTimeline(threadB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsA) != 1 || itemsA[0].ID != event.ID || itemsA[0].ThreadID != threadA {
		t.Fatalf("thread A timeline = %#v", itemsA)
	}
	if len(itemsB) != 0 {
		t.Fatalf("thread B must stay empty, got %#v", itemsB)
	}
	reloaded, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SourceThreadID != threadA {
		t.Fatalf("SourceThreadID mutated after switch: %#v", reloaded)
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
	if _, _, err := store.MaterializeWorkCard(item, event); err != nil {
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

func TestOrchestrationSchemaV3BindsCurrentThreadWithoutBulkCards(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	threadID := "thread-current"
	chatState := `{"thread_id":"` + threadID + `","thread_ids":["` + threadID + `"]}` + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, "chat_state.json"), []byte(chatState), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "schema_version": 3,
  "migrations": {},
  "brain_work": [{
    "work_id": "work-active",
    "title": "Active legacy",
    "objective": "Bind ownership then accept new cards",
    "status": "waiting",
    "completion_policy": "bounded",
    "created_at": "` + fixed.Format(time.RFC3339Nano) + `",
    "updated_at": "` + fixed.Format(time.RFC3339Nano) + `"
  }, {
    "work_id": "work-calendar",
    "title": "Calendar legacy",
    "objective": "Keep explicit source when already present",
    "status": "running",
    "source_thread_id": "thread-scheduled",
    "completion_policy": "bounded",
    "context_ref": "calendar:item-1:run-1",
    "created_at": "` + fixed.Format(time.RFC3339Nano) + `",
    "updated_at": "` + fixed.Format(time.RFC3339Nano) + `"
  }],
  "brain_work_events": []
}`
	if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), []byte(legacy+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Work("work-active")
	if err != nil {
		t.Fatal(err)
	}
	if active.SourceThreadID != threadID {
		t.Fatalf("active legacy bind = %q want %q", active.SourceThreadID, threadID)
	}
	calendarWork, err := store.Work("work-calendar")
	if err != nil {
		t.Fatal(err)
	}
	if calendarWork.SourceThreadID != "thread-scheduled" {
		t.Fatalf("explicit scheduled source overwritten: %#v", calendarWork)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "orchestration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "terminal_at") {
		t.Fatalf("persisted schema must not retain terminal_at: %s", raw)
	}
	database, migrated, err := decodeOrchestrationDatabase(raw)
	if err != nil || migrated || database.SchemaVersion != orchestrationSchemaVersion {
		t.Fatalf("schema after upgrade incomplete: migrated=%v schema=%d err=%v", migrated, database.SchemaVersion, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("must not bulk-materialize historical cards: %#v", items)
	}

	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		ID: "post-upgrade-card", WorkID: active.ID, Kind: "session.needs_input",
		DedupeKey: "post-upgrade", Actionable: false, Summary: "first post-upgrade event",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	items, err = store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "post-upgrade-card" || items[0].ThreadID != threadID {
		t.Fatalf("post-upgrade card = %#v", items)
	}
	other, err := store.ThreadTimeline("thread-scheduled", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("card leaked onto scheduled thread: %#v", other)
	}
}
