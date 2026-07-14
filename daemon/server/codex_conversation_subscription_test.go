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
	base := work.CodexConversation{Available: true, SessionID: "agent-session", Events: []work.CodexConversationEvent{{ID: "agent:1", Seq: 1, Kind: "assistant_message", Body: "Earlier"}}}
	got := srv.brainScopedConversation("brain-thread:thread-1", base, time.Now())
	if got.SessionID != "brain-thread:thread-1" || len(got.Events) != 2 {
		t.Fatalf("conversation = %#v", got)
	}
	if result := got.Events[1]; result.ID != "calendar_result:item:run" || result.Source != "calendar_result" || result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
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
