package telegram

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

type interactionFixtureAPI struct {
	*fakeAPI
	pins     [][2]int64
	pinError error
}

func (a *interactionFixtureAPI) SetMessageReaction(context.Context, string, ReactionRequest) error {
	return nil
}
func (a *interactionFixtureAPI) PinChatMessage(_ context.Context, _ string, chat, message int64) error {
	a.pins = append(a.pins, [2]int64{chat, message})
	return a.pinError
}

func TestReactionBDDFeedbackIsMessageScopedNeverProviderInput(t *testing.T) {
	f := newMediaFixture(t)
	f.apply(t, f.document(2, 101, "doc", "A"), f.document(3, 102, "other", "B"))
	before := f.provider.receipts()
	reaction := func(updateID, messageID int64, emoji string) Update {
		r := &MessageReactionUpdated{Chat: Chat{ID: 10, Type: "private"}, MessageID: messageID, User: &User{ID: 10}, Date: f.now.Unix()}
		if emoji != "" {
			r.NewReaction = []ReactionType{{Type: "emoji", Emoji: emoji}}
		}
		return Update{UpdateID: updateID, MessageReaction: r}
	}
	add := reaction(4, 2, "\U0001f44d")
	f.apply(t, add, add, reaction(5, 2, "\u2764\ufe0f"), reaction(6, 3, "\U0001f44d"), reaction(7, 2, ""))
	s := f.m.store.snapshot()
	if len(s.Feedback[2].Reactions) != 0 || s.Feedback[2].Source.SessionID != "session-a" || s.Feedback[3].Source.SessionID != "session-b" {
		t.Fatalf("reaction attribution/change/removal=%+v", s.Feedback)
	}
	unknown := reaction(8, 999, "\U0001f44d")
	foreign := reaction(9, 3, "\U0001f44e")
	foreign.MessageReaction.User.ID = 11
	actor := reaction(10, 3, "\U0001f44e")
	actor.MessageReaction.ActorChat = &Chat{ID: 10}
	f.apply(t, unknown, foreign, actor)
	if len(f.m.store.snapshot().Feedback) != 2 {
		t.Fatal("unknown or non-owner feedback admitted")
	}
	f.reopen(t)
	f.apply(t, add)
	callback := func(id int64, data string) Update {
		return Update{UpdateID: id, CallbackQuery: &CallbackQuery{ID: fmt.Sprint(id), From: &User{ID: 10}, Message: &Message{MessageID: 3, MessageThreadID: 101, Chat: Chat{ID: 10, Type: "private"}}, Data: data}}
	}
	f.apply(t, callback(11, "feedback:down"), callback(12, "feedback:clear"))
	s = f.m.store.snapshot()
	if s.Feedback[3].Native || len(s.Feedback[3].Reactions) != 0 || s.Feedback[3].Source.SessionID != "session-b" || s.Feedback[3].Source.MessageThreadID != 102 {
		t.Fatal("callback metadata retargeted message feedback")
	}
	if !reflect.DeepEqual(before, f.provider.receipts()) {
		t.Fatal("reaction/feedback created provider input")
	}
	thread, _ := f.store.ChatThreadID()
	if thread != "brain-current" {
		t.Fatal("feedback changed canonical Brain")
	}
}

func TestBrainEntryReusesPrimaryPinsMessageAndSurvivesRestart(t *testing.T) {
	m, owner, api, root := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	a := &interactionFixtureAPI{fakeAPI: api}
	m.api = a
	if err := m.store.mutate(func(s *durableState) error {
		s.BrainTopicID = 76315
		s.BrainTopics = []int64{76315}
		s.TopicsAvailable = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.ensureBrainEntry(); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.deliverPending(t.Context(), "fixture-token", 8); err != nil {
		t.Fatal(err)
	}
	s := m.store.snapshot()
	if s.BrainEntryState != "pinned" || len(a.pins) != 1 || a.pins[0][1] != s.BrainEntryMessageID {
		t.Fatalf("entry=%s pins=%v", s.BrainEntryState, a.pins)
	}
	var entry *outboxRecord
	for i := range s.Outbox {
		if s.Outbox[i].ID == "brain-entry:v1" {
			entry = &s.Outbox[i]
		}
	}
	if entry == nil || entry.MessageThreadID != 76315 || !strings.Contains(entry.ReplyMarkup.InlineKeyboard[0][0].URL, "/76315?thread=76315") {
		t.Fatalf("wrong Brain entry: %+v", entry)
	}
	reopened, err := NewManagerWithOptions(root, owner, Options{API: a})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.ensureBrainEntry(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.deliverPending(t.Context(), "fixture-token", 8); err != nil {
		t.Fatal(err)
	}
	if len(a.pins) != 1 || len(reopened.store.snapshot().TopicOps) != len(s.TopicOps) {
		t.Fatal("restart duplicated navigation/topic")
	}
}

func TestBrainEntryPinRejectionDoesNotClaimTopicPinOrResend(t *testing.T) {
	m, _, api, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	if err := m.store.mutate(func(s *durableState) error { s.TopicsAvailable = false; return nil }); err != nil {
		t.Fatal(err)
	}
	a := &interactionFixtureAPI{fakeAPI: api, pinError: &APIError{Code: 400}}
	m.api = a
	if err := m.ensureBrainEntry(); err != nil {
		t.Fatal(err)
	}
	_ = m.deliverPending(t.Context(), "fixture-token", 8)
	_ = m.deliverPending(t.Context(), "fixture-token", 8)
	if m.store.snapshot().BrainEntryState != "sent" || len(a.pins) != 1 {
		t.Fatal("pin rejection lied or retried")
	}
}

func TestAssistantFeedbackHasExactCanonicalSourceAfterDelivery(t *testing.T) {
	m, owner, _, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	owner.timeline = []brain.TimelineItem{{ID: "future", ThreadID: owner.threadID, Role: "assistant", Body: "Answer", CreatedAt: m.now().Add(time.Second)}}
	if err := m.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := m.deliverPending(t.Context(), "fixture-token", 8); err != nil {
		t.Fatal(err)
	}
	s := m.store.snapshot()
	var source messageSource
	for _, row := range s.Outbox {
		if row.CanonicalID == "future" {
			source = s.MessageSources[row.MessageID]
		}
	}
	if source.BrainThreadID != owner.threadID || source.BrainThreadID == "" {
		t.Fatalf("assistant attribution missing: %+v", source)
	}
}

func TestUnicodeGraphemeChunkingKeepsEmojiAndUTF16Entities(t *testing.T) {
	for _, emoji := range []string{"\U0001f469\u200d\U0001f4bb", "\u2764\ufe0f", "\U0001f1e8\U0001f1f3", "\U0001f44d\U0001f3fd"} {
		text := strings.Repeat("x", 4095) + emoji + "tail"
		chunks := chunkRichText(richText{Text: text, Entities: []MessageEntity{{Type: "custom_emoji", Offset: 4095, Length: utf16Len(emoji), CustomEmojiID: "fixture"}}}, 4096)
		joined := ""
		found := false
		for _, chunk := range chunks {
			joined += chunk.Text
			if utf16Len(chunk.Text) > 4096 {
				t.Fatal("oversized chunk")
			}
			if strings.Contains(chunk.Text, emoji) {
				found = true
				if len(chunk.Entities) != 1 || chunk.Entities[0].Length != utf16Len(emoji) || chunk.Entities[0].CustomEmojiID != "fixture" {
					t.Fatalf("invalid UTF16 entity: %+v", chunk)
				}
			}
		}
		if joined != text || !found {
			t.Fatal("emoji was altered/split")
		}
		if got := renderMarkdown("**" + emoji + "**"); got.Text != emoji || got.Entities[0].Length != utf16Len(emoji) {
			t.Fatalf("markdown changed emoji: %+v", got)
		}
	}
	if chunks := chunkRichText(richText{Text: "a" + strings.Repeat("\u0301", 5000)}, 4096); len(chunks) != 1 {
		t.Fatal("pathological grapheme was corrupted")
	}
}
