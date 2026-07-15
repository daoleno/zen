package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func TestStoreChatMessagesFiltersByThreadAndLimit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	messages := []ChatMessage{
		{ID: "a1", ThreadID: "thread-a", SessionID: "a", Role: "user", Body: "first", CreatedAt: base},
		{ID: "b1", ThreadID: "thread-b", SessionID: "b", Role: "user", Body: "other", CreatedAt: base.Add(time.Second)},
		{ID: "a2", ThreadID: "thread-a", SessionID: "a", Role: "assistant", Body: "second", CreatedAt: base.Add(2 * time.Second)},
		{ID: "a3", ThreadID: "thread-a", SessionID: "a", Role: "user", Body: "third", CreatedAt: base.Add(3 * time.Second)},
	}
	for _, message := range messages {
		if _, err := store.AppendChatMessage(message); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ChatMessages("thread-a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a2" || got[1].ID != "a3" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDeliverCalendarResultIsIdempotentAndKeepsOldThreadKnown(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-original", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-current", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	result := CalendarResult{
		ID: "calendar_result:item-1:run-1", ThreadID: "thread-original",
		CalendarItemID: "item-1", CalendarRunID: "run-1", Title: "Daily papers",
		Status: "completed", Body: "**Daily papers completed**\n\nThree papers.",
		ScheduledFor: time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 7, 14, 1, 2, 0, 0, time.UTC),
	}
	if _, err := service.DeliverCalendarResult(result); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service = NewService(store, nil, nil)
	if _, err := service.DeliverCalendarResult(result); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ChatMessages("thread-original", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Kind != "calendar_result" || messages[0].CalendarRunID != "run-1" {
		t.Fatalf("messages = %#v", messages)
	}
	results, err := store.ScheduledResults(10)
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
}

func TestDeliverCalendarResultRejectsUnknownThreadWithoutAppending(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	_, err = service.DeliverCalendarResult(CalendarResult{
		ID: "calendar_result:item:run", ThreadID: "missing", Body: "nope",
		ScheduledFor: time.Now(), CreatedAt: time.Now(),
	})
	if !errors.Is(err, ErrChatThreadNotFound) {
		t.Fatalf("err = %v", err)
	}
	results, readErr := store.ScheduledResults(10)
	if readErr != nil || len(results) != 0 {
		t.Fatalf("results = %#v, err = %v", results, readErr)
	}
}

func TestChatMessagesOrderByCanonicalTimeAndIdentityBeforeLimiting(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	if err := store.SetChatState(ChatState{ThreadID: "thread-1", UpdatedAt: baseTime}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []ChatMessage{
		{ID: "later", ThreadID: "thread-1", SessionID: "calendar", Role: "assistant", Body: "later", Kind: "calendar_result", CreatedAt: baseTime.Add(time.Minute)},
		{ID: "same-b", ThreadID: "thread-1", SessionID: "calendar", Role: "assistant", Body: "same b", Kind: "calendar_result", CreatedAt: baseTime},
		{ID: "same-a", ThreadID: "thread-1", SessionID: "calendar", Role: "assistant", Body: "same a", Kind: "calendar_result", CreatedAt: baseTime},
	} {
		if _, err := store.AppendChatMessage(message); err != nil {
			t.Fatal(err)
		}
	}

	messages, err := store.ChatMessages("thread-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{messages[0].ID, messages[1].ID}; fmt.Sprint(got) != "[same-b later]" {
		t.Fatalf("limited messages = %v, want canonical tail", got)
	}
	results, err := store.ScheduledResults(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{results[0].ID, results[1].ID, results[2].ID}; fmt.Sprint(got) != "[same-a same-b later]" {
		t.Fatalf("scheduled results = %v, want time then identity", got)
	}
}

func TestStoreChatThreadIncludesLegacySessionMessages(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	appendLegacyChatMessage(t, store, ChatMessage{
		ID:        "legacy-1",
		SessionID: "old-host",
		Role:      "user",
		Body:      "old chat",
		CreatedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	})

	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ChatMessages(threadID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "old chat" {
		t.Fatalf("messages = %#v", got)
	}
	state, err := store.ChatState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ThreadID != threadID || !containsString(state.SessionIDs, "old-host") {
		t.Fatalf("chat state = %+v", state)
	}
}

func TestServiceRecordUserMessageStoresCursorAndMessage(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	got, err := service.RecordUserMessage("thread-main", "brain-session", "remember this", "\nready\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != "user" || got[0].Body != "remember this" {
		t.Fatalf("messages = %#v", got)
	}
	state, err := store.ChatState("thread-main")
	if err != nil {
		t.Fatal(err)
	}
	if state.LastTranscript != "ready" {
		t.Fatalf("cursor = %q", state.LastTranscript)
	}
	if state.ThreadID != "thread-main" || !containsString(state.SessionIDs, "brain-session") {
		t.Fatalf("chat state = %+v", state)
	}
}

func TestServiceSyncTerminalTranscriptAppendsAssistantDeltaOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	if _, err := service.RecordUserMessage("thread-main", "brain-session", "hello", "Claude ready"); err != nil {
		t.Fatal(err)
	}
	got, err := service.SyncTerminalTranscript("thread-main", "brain-session", "Claude ready\n> hello\nHi there\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("messages = %#v", got)
	}
	if got[1].Role != "assistant" || got[1].Body != "Hi there" {
		t.Fatalf("assistant message = %#v", got[1])
	}

	got, err = service.SyncTerminalTranscript("thread-main", "brain-session", "Claude ready\n> hello\nHi there\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("duplicate assistant message appended: %#v", got)
	}
}

func TestServiceSyncTerminalTranscriptCleansTerminalChrome(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	before := "Claude ready"
	if _, err := service.RecordUserMessage("thread-main", "brain-session", "继续", before); err != nil {
		t.Fatal(err)
	}
	transcript := before + `
> 继续
──── ✶ Transfiguring...

────────────────────────────────────────────
──────────────────────────────────────────── >
──────────────────────── esc to
interrupt

或者你有别的事，直接说就行。

✻ Cogitated for 9s
────────────────────────────────────────────
──────────── ? for
`
	got, err := service.SyncTerminalTranscript("thread-main", "brain-session", transcript)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("messages = %#v", got)
	}
	if got[1].Body != "或者你有别的事，直接说就行。" {
		t.Fatalf("assistant body = %q", got[1].Body)
	}
}

func TestChatMessagesCleansStoredTerminalChrome(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendChatMessage(ChatMessage{
		ID:        "assistant-1",
		ThreadID:  "thread-main",
		SessionID: "brain-session",
		Role:      "assistant",
		Body:      "──── ✶ Transfiguring...\n\n真实回复\n\n✻ Cogitated for 9s",
		CreatedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	got, err := service.ChatMessages("thread-main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "真实回复" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestServiceSyncTerminalTranscriptSeedsInitialCursor(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	got, err := service.SyncTerminalTranscript("thread-main", "brain-session", "\nAgent is ready\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("messages = %#v", got)
	}
	state, err := store.ChatState("thread-main")
	if err != nil {
		t.Fatal(err)
	}
	if state.LastTranscript != "Agent is ready" {
		t.Fatalf("cursor = %q", state.LastTranscript)
	}
}

func TestServiceChatThreadSurvivesHostSessionSwap(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)

	if _, err := service.RecordUserMessage("thread-main", "host-a", "first", "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SyncTerminalTranscript("thread-main", "host-a", "ready\n> first\nreply one"); err != nil {
		t.Fatal(err)
	}
	got, err := service.RecordUserMessage("thread-main", "host-b", "second", "new ready")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("messages = %#v", got)
	}
	if got[0].Body != "first" || got[1].Body != "reply one" || got[2].Body != "second" {
		t.Fatalf("messages = %#v", got)
	}
	state, err := store.ChatState("thread-main")
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(state.SessionIDs, "host-a") || !containsString(state.SessionIDs, "host-b") {
		t.Fatalf("thread did not retain host sessions: %+v", state)
	}
}

func TestServiceContextIncludesCurrentAndRecentMessages(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.currentPath(), []byte("# Current Brain Context\n\n## Active Objective\n\nKeep going.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newChatTestService(store)
	if _, err := service.RecordUserMessage("thread-main", "host-a", "first", "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SyncTerminalTranscript("thread-main", "host-a", "ready\n> first\nreply one"); err != nil {
		t.Fatal(err)
	}

	context, err := service.Context(10)
	if err != nil {
		t.Fatal(err)
	}
	if context.ThreadID != "thread-main" {
		t.Fatalf("thread id = %q", context.ThreadID)
	}
	if !strings.Contains(context.Current, "Keep going.") {
		t.Fatalf("current context = %q", context.Current)
	}
	if len(context.RecentMessages) != 2 {
		t.Fatalf("recent messages = %#v", context.RecentMessages)
	}
	if context.RecentMessages[0].Body != "first" || context.RecentMessages[1].Body != "reply one" {
		t.Fatalf("recent messages = %#v", context.RecentMessages)
	}
	if len(context.Playbooks) != 5 {
		t.Fatalf("playbooks = %#v", context.Playbooks)
	}
}

func newChatTestService(store *Store) *Service {
	tick := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, nil, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude", Kind: "claude", Runtime: work.AgentRuntimeTmux},
		},
	})
	service.now = func() time.Time {
		tick = tick.Add(time.Millisecond)
		return tick
	}
	return service
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func appendLegacyChatMessage(t *testing.T, store *Store, message ChatMessage) {
	t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.messagesPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}
