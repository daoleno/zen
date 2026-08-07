package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

// Deterministic large-conversation synthetic fixture for server-side poll
// phase regression. Body text is synthetic and content-free; timings must not
// depend on message content.

func syntheticLargeConversation() work.CodexConversation {
	const turns = 200
	base := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	events := make([]work.CodexConversationEvent, 0, turns*4)
	seq := 0
	for turn := 0; turn < turns; turn++ {
		seq++
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("evt:user:%d", turn),
			Seq:       seq,
			Timestamp: base.Add(time.Duration(turn*4+1) * time.Second).Format(time.RFC3339Nano),
			Kind:      "user_message",
			Role:      "user",
			Body:      strings.Repeat("u", 2000),
		})
		seq++
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("evt:reasoning:%d", turn),
			Seq:       seq,
			Timestamp: base.Add(time.Duration(turn*4+2) * time.Second).Format(time.RFC3339Nano),
			Kind:      "reasoning",
			Role:      "assistant",
			Body:      strings.Repeat("r", 1000),
			Transient: true,
		})
		seq++
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("evt:text:%d", turn),
			Seq:       seq,
			Timestamp: base.Add(time.Duration(turn*4+2) * time.Second).Format(time.RFC3339Nano),
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      strings.Repeat("a", 4000),
		})
		seq++
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("evt:tool:%d", turn),
			Seq:       seq,
			Timestamp: base.Add(time.Duration(turn*4+3) * time.Second).Format(time.RFC3339Nano),
			Kind:      "tool_call",
			Role:      "assistant",
			ToolName:  "shell",
			CallID:    fmt.Sprintf("call:%d", turn),
			Input:     `{"command":"ls"}`,
			Output:    strings.Repeat("o", 12000),
			Status:    "completed",
		})
	}
	return work.CodexConversation{
		Available: true,
		Source:    "opencode_db",
		Path:      "/repo/.local/share/opencode/opencode.db",
		SessionID: "ses_perf_large",
		CWD:       "/repo/perf",
		Activity: &work.ProviderActivity{
			ID:        "act:last",
			Status:    work.ProviderActivityCompleted,
			StartedAt: base.Format(time.RFC3339Nano),
			SettledAt: base.Add(time.Duration(turns*4) * time.Second).Format(time.RFC3339Nano),
		},
		Events: events,
	}
}

func BenchmarkServerPollPhases(b *testing.B) {
	conversation := syntheticLargeConversation()
	b.Run("sanitize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sanitized := work.SanitizeConversationProjection(conversation)
			if len(sanitized.Events) == 0 {
				b.Fatal("empty")
			}
		}
	})
	// Memoized path: first fingerprint memoizes all per-event fingerprints;
	// subsequent unchanged polls compare the memoized result.
	b.Run("fingerprint-memoized-first", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			memo := codexConversationSubscriptionFingerprintMemoized(conversation, nil, nil)
			if memo.fingerprint == "" {
				b.Fatal("empty")
			}
		}
	})
	b.Run("events-by-id", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			byID := codexConversationEventsByID(conversation.Events)
			if len(byID) == 0 {
				b.Fatal("empty")
			}
		}
	})
	previous := codexConversationEventsByID(conversation.Events)
	next := conversation.Events
	initialMemo := codexConversationSubscriptionFingerprintMemoized(conversation, nil, nil)
	prevSnapshot := &codexConversationSubscriptionSnapshot{
		conversation:      conversation,
		fingerprint:       initialMemo.fingerprint,
		eventsByID:        previous,
		eventFingerprints: initialMemo.eventFingerprints,
		revision:          1,
		readerVersion:     1,
	}
	b.Run("fingerprint-memoized-unchanged", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = codexConversationSubscriptionFingerprintMemoized(conversation, prevSnapshot, nil)
		}
	})
	changed := append(append([]work.CodexConversationEvent{}, conversation.Events[:len(conversation.Events)-1]...), work.CodexConversationEvent{
		ID:        "evt:tool:199",
		Seq:       800,
		Timestamp: conversation.Events[len(conversation.Events)-1].Timestamp,
		Kind:      "tool_call",
		ToolName:  "shell",
		CallID:    "call:199",
		Output:    strings.Repeat("o", 12000) + strings.Repeat("x", 64),
		Status:    "completed",
	})
	b.Run("delta-memoized-one-upsert", func(b *testing.B) {
		changedIDs := []string{"evt:tool:199"}
		nextMemo := codexConversationSubscriptionFingerprintMemoized(
			work.CodexConversation{Events: changed},
			prevSnapshot,
			changedIDs,
		)
		nextByID := codexConversationEventsByID(changed)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			upserts, deletes := codexConversationDeltaMemoized(
				prevSnapshot.eventsByID,
				prevSnapshot.eventFingerprints,
				changed,
				nextMemo.eventFingerprints,
			)
			if len(upserts) != 1 || len(deletes) != 0 {
				b.Fatalf("unexpected delta upserts=%d deletes=%d", len(upserts), len(deletes))
			}
			_ = nextByID
		}
	})
	b.Run("delta-unchanged", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			upserts, deletes := codexConversationDelta(previous, next)
			if len(upserts) != 0 || len(deletes) != 0 {
				b.Fatal("unexpected delta")
			}
		}
	})
	b.Run("snapshot-serialize", func(b *testing.B) {
		payload := codexConversationSnapshotPayload("sub-1", "gen-1", "agent-1", 1, conversation)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := json.Marshal(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("delta-serialize", func(b *testing.B) {
		upserts, _ := codexConversationDelta(previous, changed)
		next := codexConversationSubscriptionSnapshot{conversation: conversation, revision: 2}
		prev := codexConversationSubscriptionSnapshot{conversation: conversation, revision: 1}
		payload := codexConversationDeltaPayload("sub-1", "gen-1", "agent-1", prev, next, upserts, nil)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := json.Marshal(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
}
