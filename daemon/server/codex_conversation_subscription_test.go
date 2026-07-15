package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainScopedConversationIncludesCanonicalCalendarResult(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-1", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	if _, err := service.DeliverCalendarResult(brain.CalendarResult{
		ID: "calendar_result:item:run", ThreadID: "thread-1", CalendarItemID: "item",
		CalendarRunID: "run", Title: "Daily papers", Status: "completed", Body: "Three papers.",
		ScheduledFor: time.Now(), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: service}
	base := work.CodexConversation{Available: true, SessionID: "agent-session", Events: []work.CodexConversationEvent{{ID: "agent:1", Seq: 1, Timestamp: time.Now().Add(-time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "Earlier"}}}
	got := srv.brainScopedConversation("brain-thread:thread-1", base, time.Now())
	if got.SessionID != "brain-thread:thread-1" || len(got.Events) != 2 {
		t.Fatalf("conversation = %#v", got)
	}
	if result := got.Events[1]; result.ID != "calendar_result:item:run" || result.Source != "calendar_result" || result.Kind != "status" || result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBrainScopedConversationPreservesPartialProviderEventWithCalendar(t *testing.T) {
	baseTime := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-1", UpdatedAt: baseTime}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	if _, err := service.DeliverCalendarResult(brain.CalendarResult{
		ID: "calendar_result:item:run", ThreadID: "thread-1", CalendarItemID: "item",
		CalendarRunID: "run", Title: "Daily papers", Status: "completed", Body: "Three papers.",
		ScheduledFor: baseTime, CreatedAt: baseTime.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	active := true
	partial := work.CodexConversationEvent{
		ID:        "grok-session:stream:prompt-1:assistant:1",
		Seq:       200,
		Timestamp: baseTime.Add(2 * time.Minute).Format(time.RFC3339Nano),
		Kind:      "assistant_message",
		Role:      "assistant",
		Body:      "A genuine provider chunk",
		Status:    "running",
		Partial:   true,
		Source:    "grok_updates",
	}
	srv := &Server{brain: service}
	got := srv.brainScopedConversation("brain-thread:thread-1", work.CodexConversation{
		Available: true,
		SessionID: "grok-session",
		Active:    &active,
		Events:    []work.CodexConversationEvent{partial},
	}, baseTime.Add(3*time.Minute))

	if ids := fmt.Sprint(conversationEventIDs(got.Events)); ids != "[calendar_result:item:run grok-session:stream:prompt-1:assistant:1]" {
		t.Fatalf("event order = %s", ids)
	}
	if got.Active == nil || !*got.Active {
		t.Fatalf("active = %#v, want provider turn preserved", got.Active)
	}
	streamed := got.Events[1]
	if streamed.ID != partial.ID || streamed.Body != partial.Body || !streamed.Partial || streamed.Status != "running" {
		t.Fatalf("partial provider event changed during Brain overlay: %#v", streamed)
	}
	if got.Events[0].Partial {
		t.Fatalf("Calendar result must remain finalized: %#v", got.Events[0])
	}
}

func TestBrainScopedConversationOrdersCalendarResultBeforeLaterChat(t *testing.T) {
	baseTime := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-1", UpdatedAt: baseTime}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	if _, err := service.DeliverCalendarResult(brain.CalendarResult{
		ID: "calendar_result:item:run", ThreadID: "thread-1", CalendarItemID: "item",
		CalendarRunID: "run", Title: "Daily Hacker News", Status: "failed",
		Body:         "**Daily Hacker News failed**\n\nLinked Work is no longer observable.",
		ScheduledFor: baseTime, CreatedAt: baseTime.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: service}
	active := false
	base := work.CodexConversation{Available: true, Active: &active, Events: []work.CodexConversationEvent{
		{ID: "user-before", Seq: 40, Timestamp: baseTime.Format(time.RFC3339Nano), Kind: "user_message", Role: "user", Body: "Before"},
		{ID: "assistant-after", Seq: 41, Timestamp: baseTime.Add(2 * time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Role: "assistant", Body: "Later answer"},
	}}

	got := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime.Add(3*time.Minute))
	if ids := conversationEventIDs(got.Events); fmt.Sprint(ids) != "[user-before calendar_result:item:run assistant-after]" {
		t.Fatalf("event order = %v", ids)
	}
	result := got.Events[1]
	if result.Kind != "status" || result.Title != "Daily Hacker News failed" || result.Body != "Linked Work is no longer observable." || result.Status != "failed" {
		t.Fatalf("calendar result presentation = %#v", result)
	}
	if got.Active == nil || *got.Active {
		t.Fatalf("terminal failure left active projection: %#v", got.Active)
	}

	reloaded := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime.Add(4*time.Minute))
	if fmt.Sprint(conversationEventIDs(reloaded.Events)) != fmt.Sprint(conversationEventIDs(got.Events)) ||
		codexConversationEventsFingerprint(reloaded.Events) != codexConversationEventsFingerprint(got.Events) {
		t.Fatalf("reload changed canonical timeline: first=%#v reload=%#v", got.Events, reloaded.Events)
	}
}

func TestBrainScopedConversationOrdersAndDeduplicatesCalendarResultsDeterministically(t *testing.T) {
	baseTime := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-1", UpdatedAt: baseTime}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	for _, result := range []brain.CalendarResult{
		{ID: "calendar_result:item:run-b", ThreadID: "thread-1", Title: "Second", Status: "completed", Body: "second", ScheduledFor: baseTime, CreatedAt: baseTime.Add(time.Minute)},
		{ID: "calendar_result:item:run-a", ThreadID: "thread-1", Title: "First", Status: "completed", Body: "first", ScheduledFor: baseTime, CreatedAt: baseTime.Add(time.Minute)},
		{ID: "calendar_result:item:run-old", ThreadID: "thread-1", Title: "Old", Status: "failed", Body: "old", ScheduledFor: baseTime.Add(-time.Hour), CreatedAt: baseTime.Add(-time.Minute)},
	} {
		if _, err := service.DeliverCalendarResult(result); err != nil {
			t.Fatal(err)
		}
	}
	srv := &Server{brain: service}
	base := work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{
		{ID: "calendar_result:item:run-a", Seq: 99, Timestamp: baseTime.Add(10 * time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "stale duplicate"},
		{ID: "normal", Seq: 100, Timestamp: baseTime.Add(2 * time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "normal"},
	}}

	got := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime)
	want := "[calendar_result:item:run-old calendar_result:item:run-a calendar_result:item:run-b normal]"
	if ids := conversationEventIDs(got.Events); fmt.Sprint(ids) != want {
		t.Fatalf("event order = %v, want %s", ids, want)
	}
	if got.Events[1].Seq != 0 || got.Events[2].Seq != 0 || got.Events[3].Seq != 100 {
		t.Fatalf("merge mutated provider sequence identity: %#v", got.Events)
	}
}

func conversationEventIDs(events []work.CodexConversationEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func TestCodexConversationSubscriptionFingerprintIgnoresUpdatedAt(t *testing.T) {
	firstUpdated := time.Unix(100, 0).UTC()
	secondUpdated := time.Unix(200, 0).UTC()
	base := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Updated:   &firstUpdated,
		Events: []work.CodexConversationEvent{
			{
				ID:   "session-1:10",
				Seq:  10,
				Kind: "assistant_message",
				Role: "assistant",
				Body: "done",
			},
		},
	}
	next := base
	next.Updated = &secondUpdated

	if codexConversationSubscriptionFingerprint(base) != codexConversationSubscriptionFingerprint(next) {
		t.Fatal("updated_at-only changes should not trigger a conversation delta")
	}
}

func TestCodexConversationSubscriptionFingerprintChangesForEventContent(t *testing.T) {
	base := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Events: []work.CodexConversationEvent{
			{
				ID:   "session-1:10",
				Seq:  10,
				Kind: "assistant_message",
				Role: "assistant",
				Body: "done",
			},
		},
	}
	next := base
	next.Events = append([]work.CodexConversationEvent(nil), base.Events...)
	next.Events[0].Body = "done differently"

	if codexConversationSubscriptionFingerprint(base) == codexConversationSubscriptionFingerprint(next) {
		t.Fatal("event content changes should trigger a conversation delta")
	}
}

func TestCodexConversationSubscriptionFingerprintIsCompactForLargeEvents(t *testing.T) {
	conversation := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Events: []work.CodexConversationEvent{
			{
				ID:     "session-1:10",
				Seq:    10,
				Kind:   "tool",
				Output: strings.Repeat("x", 1<<20),
			},
		},
	}

	fingerprint := codexConversationSubscriptionFingerprint(conversation)
	if len(fingerprint) > 64 {
		t.Fatalf("fingerprint len = %d, want compact hash", len(fingerprint))
	}
}

func TestCodexConversationDeltaOnlySendsTrimWindowDifference(t *testing.T) {
	previous := make([]work.CodexConversationEvent, 0, 240)
	for index := 1; index <= 240; index++ {
		previous = append(previous, work.CodexConversationEvent{
			ID:   fmt.Sprintf("session-1:%d", index),
			Seq:  index,
			Kind: "assistant_message",
			Role: "assistant",
			Body: "same",
		})
	}
	next := make([]work.CodexConversationEvent, 0, 240)
	for index := 2; index <= 241; index++ {
		next = append(next, work.CodexConversationEvent{
			ID:   fmt.Sprintf("session-1:%d", index),
			Seq:  index,
			Kind: "assistant_message",
			Role: "assistant",
			Body: "same",
		})
	}

	upserts, deletes := codexConversationDelta(codexConversationEventsByID(previous), next)
	if len(upserts) != 1 || upserts[0].Seq != 241 {
		t.Fatalf("upserts = %#v, want only the newly appended event", upserts)
	}
	if len(deletes) != 1 || deletes[0] != previous[0].ID {
		t.Fatalf("deletes = %#v, want only the trimmed first event", deletes)
	}
}

func TestCodexConversationDeltaFinalizesPartialEventInPlace(t *testing.T) {
	partial := work.CodexConversationEvent{
		ID:      "session-1:stream:answer",
		Seq:     10,
		Kind:    "assistant_message",
		Role:    "assistant",
		Body:    "Complete semantic text",
		Partial: true,
	}
	final := partial
	final.Partial = false

	if codexConversationEventFingerprint(partial) == codexConversationEventFingerprint(final) {
		t.Fatal("partial-only finalization must change the event fingerprint")
	}
	upserts, deletes := codexConversationDelta(
		codexConversationEventsByID([]work.CodexConversationEvent{partial}),
		[]work.CodexConversationEvent{final},
	)
	if len(upserts) != 1 || upserts[0].ID != partial.ID || upserts[0].Partial {
		t.Fatalf("upserts = %#v, want one finalized replacement with the same ID", upserts)
	}
	if len(deletes) != 0 {
		t.Fatalf("deletes = %#v, finalization must not delete and recreate the message", deletes)
	}
	transient := final
	transient.Transient = true
	if codexConversationEventFingerprint(final) == codexConversationEventFingerprint(transient) {
		t.Fatal("transient projection changes must participate in the event fingerprint")
	}
}
