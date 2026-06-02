package brain

import (
	"encoding/json"
	"os"
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

func TestStoreThreadMetadataPinsThread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.SetThreadPinned("codex:thread-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ThreadID != "codex:thread-1" || !meta.Pinned {
		t.Fatalf("metadata = %+v", meta)
	}
	got, err := store.ThreadMetadata("codex:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Pinned {
		t.Fatalf("stored metadata = %+v", got)
	}
	metadata, err := store.ThreadMetadataMap([]string{"codex:thread-1", "codex:thread-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !metadata["codex:thread-1"].Pinned || metadata["codex:thread-2"].Pinned {
		t.Fatalf("metadata map = %#v", metadata)
	}
	review, err := store.SetThreadReviewState("codex:thread-2", "review")
	if err != nil {
		t.Fatal(err)
	}
	if review.ReviewState != "needs_review" {
		t.Fatalf("review metadata = %+v", review)
	}
	if _, err := store.SetThreadReviewState("codex:thread-3", "reviewing"); err != nil {
		t.Fatal(err)
	}
	metadata, err = store.ThreadMetadataMap([]string{"codex:thread-1", "codex:thread-2"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata["codex:thread-2"].ReviewState != "needs_review" {
		t.Fatalf("metadata map = %#v", metadata)
	}
	attention, err := store.AttentionSummary()
	if err != nil {
		t.Fatal(err)
	}
	if attention.Pinned != 1 || attention.NeedsReview != 1 || attention.Reviewing != 1 || attention.ReviewQueue != 2 || attention.Pressure != "review" {
		t.Fatalf("attention = %+v", attention)
	}
	if !attention.CanStartAgent || attention.AvailableAgentSlots != 3 || attention.MaxInFlightAgents != 3 || attention.ReviewQueueLimit != 4 {
		t.Fatalf("attention capacity = %+v", attention)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Attention.ReviewQueue != 2 || snapshot.Attention.Pinned != 1 || snapshot.Attention.Pressure != "review" {
		t.Fatalf("snapshot attention = %+v", snapshot.Attention)
	}
	if !snapshot.Attention.CanStartAgent || snapshot.Attention.AvailableAgentSlots != 3 {
		t.Fatalf("snapshot attention capacity = %+v", snapshot.Attention)
	}
	if len(snapshot.AttentionQueue) != 2 {
		t.Fatalf("snapshot attention queue = %#v", snapshot.AttentionQueue)
	}
	if snapshot.AttentionQueue[0].Kind != "review_thread" || snapshot.AttentionQueue[0].ThreadID != "codex:thread-3" || snapshot.AttentionQueue[0].ReviewState != "reviewing" {
		t.Fatalf("first queue item = %+v", snapshot.AttentionQueue[0])
	}
	if snapshot.AttentionQueue[1].Kind != "review_thread" || snapshot.AttentionQueue[1].ThreadID != "codex:thread-2" || snapshot.AttentionQueue[1].ReviewState != "needs_review" {
		t.Fatalf("second queue item = %+v", snapshot.AttentionQueue[1])
	}
	if _, err := store.SetThreadPinned("codex:thread-1", false); err != nil {
		t.Fatal(err)
	}
	got, err = store.ThreadMetadata("codex:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pinned {
		t.Fatalf("unpinned metadata = %+v", got)
	}
	if _, err := store.SetThreadReviewState("codex:thread-2", ""); err != nil {
		t.Fatal(err)
	}
	got, err = store.ThreadMetadata("codex:thread-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewState != "" {
		t.Fatalf("cleared review metadata = %+v", got)
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
