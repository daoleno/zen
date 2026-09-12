package telegram

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

func chromeWire(t *testing.T, m *Manager) *fakeBotAPIServer {
	t.Helper()
	fake := newFakeBotAPIServer(t)
	m.api = NewClient(fake.server.URL, fake.server.Client())
	if err := m.store.replaceToken("fixture-token"); err != nil {
		t.Fatal(err)
	}
	return fake
}

func deliverChrome(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.deliverPending(t.Context(), "fixture-token", maxOutboxRows); err != nil {
		t.Fatal(err)
	}
}

func assertContentOnlyPayloads(t *testing.T, wire *fakeBotAPIServer) {
	t.Helper()
	if len(wire.bodies) == 0 {
		t.Fatal("no Bot API payloads")
	}
	for i, body := range wire.bodies {
		if wire.methods[i] != "sendMessage" && wire.methods[i] != "editMessageText" {
			t.Fatalf("unexpected method %s", wire.methods[i])
		}
		if _, found := body["reply_markup"]; found {
			t.Fatalf("ordinary %s contains reply markup: %+v", wire.methods[i], body)
		}
	}
}

func TestOrdinaryBrainAndSessionPayloadsStayCleanAcrossStreamChunks(t *testing.T) {
	for _, recipient := range []string{"Brain", "Session", "private Session"} {
		t.Run(recipient, func(t *testing.T) {
			m, owner, _, _ := configuredManager(t)
			bindOwner(t, m, 1, 10, 10)
			thread := int64(76315)
			if recipient == "Session" {
				thread = 102
			} else if recipient == "private Session" {
				thread = 0
			}
			if err := m.store.mutate(func(s *durableState) error {
				s.BrainTopicID, s.BrainTopics = 76315, []int64{76315}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			deliverChrome(t, m)
			wire := chromeWire(t, m)
			created := m.now().Add(time.Second)
			for i, text := range []string{"Plain answer", strings.Repeat("x", maxMessageText) + "stream", strings.Repeat("x", maxMessageText) + "stream complete"} {
				var err error
				if recipient == "Brain" {
					owner.timeline = []brain.TimelineItem{{ID: "answer", ThreadID: owner.threadID, Role: "assistant", Body: text, CreatedAt: created}}
					err = m.projectTimeline()
				} else {
					err = m.projectSessionOutput(topicMapping{SessionID: "session-a", MessageThreadID: thread}, brain.SessionProjection{
						Assistant: []brain.SessionAssistantItem{{ID: "answer", Body: text, CreatedAt: created, Partial: i < 2}},
					}, created)
				}
				if err != nil {
					t.Fatal(err)
				}
				deliverChrome(t, m)
			}
			assertContentOnlyPayloads(t, wire)
			if got := strings.Join(wire.methods, ","); got != "sendMessage,editMessageText,sendMessage,editMessageText" {
				t.Fatalf("stream did not send/edit exact chunks: %s", got)
			}
			for i, body := range wire.bodies {
				if wire.methods[i] == "sendMessage" && thread != 0 && body["message_thread_id"] != float64(thread) {
					t.Fatalf("wrong destination: %+v", body)
				}
			}
		})
	}
}

func TestOrdinaryReceiptsStatusesAndErrorsHaveNoMarkup(t *testing.T) {
	f := newMediaFixture(t)
	f.apply(t, f.document(2, 76315, "doc", "Brain file"), f.document(3, 101, "other", "Session file"))
	failed := f.document(4, 102, "doc", "")
	failed.Message.Document.FileSize = maxTelegramFileBytes + 1
	f.apply(t, failed, topicUpdate(5, 76315, "/status"), topicUpdate(6, 101, "/status"))
	for i, outcome := range []brain.ExternalInputDisposition{brain.ExternalInputUncertain, brain.ExternalInputNotSubmitted} {
		input := mediaInput{ID: fmt.Sprintf("outcome-%d", i), SessionID: "session-a", MessageThreadID: 101, Parts: []mediaPart{{UpdateID: int64(20 + i), MessageID: int64(20 + i)}}}
		if err := f.m.finishMedia(input, outcome); err != nil {
			t.Fatal(err)
		}
	}
	input := mediaInput{ID: "download-failure", State: "pending", BrainThreadID: "brain-current", MessageThreadID: 76315, Parts: []mediaPart{{UpdateID: 30, MessageID: 30}}}
	if err := f.m.saveMedia(input); err != nil {
		t.Fatal(err)
	}
	if err := f.m.failMedia(input.ID, "Download unavailable."); err != nil {
		t.Fatal(err)
	}
	f.m.enqueueText("ack:31", strings.Repeat("a", maxMessageText+1), 31)
	f.m.enqueueTopicText("topic-ack:32", "Session unavailable.", 102, 32)
	if err := f.m.store.mutate(func(s *durableState) error {
		enqueueTopicTextLocked(s, "topic:life:session-a:completed", 101, "Session completed.", f.now)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wire := chromeWire(t, f.m)
	deliverChrome(t, f.m)
	assertContentOnlyPayloads(t, wire)
	success, failure := 0, 0
	for _, body := range wire.bodies {
		text := body["text"].(string)
		if strings.HasPrefix(text, "Files received by Zen.") {
			success++
		}
		if strings.Contains(text, "Nothing was forwarded") {
			failure++
		}
	}
	if success != 2 || failure != 2 {
		t.Fatalf("missing file receipts: success=%d failure=%d", success, failure)
	}
}

func TestExplicitInteractionPayloadsRetainExactTopicButtons(t *testing.T) {
	m, owner, _, _ := topicFixture(t)
	createTopicFor(t, m)
	if err := m.store.mutate(func(s *durableState) error {
		s.BotUsername, s.BrainTopicID, s.BrainTopics = "fixture_bot", 76315, []int64{76315}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deliverChrome(t, m)
	wire := chromeWire(t, m)
	thread := topicThreadFor(t, m, "sess-a")
	m.enqueueSessionList(10, 10, thread)
	m.returnToBrain("command:11", thread, 11)
	if got := m.handleCallback(t.Context(), "fixture-token", *navigationCallback(12, 76315, "new").CallbackQuery, 12); got != "callback_confirm" {
		t.Fatal(got)
	}
	for i, id := range []string{"command:10:0", "command:11:0", "callback:12"} {
		row := navigationRow(t, m, id)
		if err := m.deliverOne(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(row.ReplyMarkup)
		var expected any
		_ = json.Unmarshal(encoded, &expected)
		if row.ReplyMarkup == nil || !reflect.DeepEqual(wire.bodies[i]["reply_markup"], expected) || wire.bodies[i]["message_thread_id"] != float64(row.MessageThreadID) {
			t.Fatalf("explicit interaction changed on wire: %+v", wire.bodies[i])
		}
	}
	assertTopicButton(t, navigationRow(t, m, "command:10:0").ReplyMarkup.InlineKeyboard[0][0], thread)
	assertTopicButton(t, navigationRow(t, m, "command:11:0").ReplyMarkup.InlineKeyboard[0][0], 76315)
	if got := navigationRow(t, m, "callback:12").ReplyMarkup.InlineKeyboard; !reflect.DeepEqual(got, [][]InlineKeyboardButton{{{Text: "New Chat", CallbackData: "new_confirm"}, {Text: "Cancel", CallbackData: "brain"}}}) {
		t.Fatalf("reset confirmation changed: %+v", got)
	}
	if owner.newChats != 0 || len(owner.bodies)+len(owner.sessionBodies) != 0 {
		t.Fatal("navigation invoked a provider/reset")
	}
}

func TestLegacyPendingOrdinaryChromeIsStrippedOnlyAtDispatch(t *testing.T) {
	for _, test := range []struct {
		name, id, text, canonical string
		feedback, extra, keep     bool
	}{
		{name: "Brain", id: "assistant:old:0", text: "Brain", canonical: "old", feedback: true},
		{name: "Session", id: "topic:msg:session:old:0", text: "Answer", canonical: "session:old", feedback: true},
		{name: "receipt", id: "media-ack:old", text: "Files received by Zen."},
		{name: "file failure", id: "media-error:old", text: "Nothing was forwarded."},
		{name: "status", id: "command:4:0", text: "Recipient: Brain. Session conversations are in their own topics."},
		{name: "callback error", id: "callback:old:0", text: "Session unavailable."},
		{name: "lifecycle", id: "topic-ack:old:0", text: "Session closed."},
		{name: "welcome", id: "topic:brain:welcome", text: "Brain"},
		{name: "Brain entry", id: "command:4:0", text: "Brain", keep: true},
		{name: "callback Brain entry", id: "callback:4:0", text: "Recipient: Brain.", keep: true},
		{name: "empty chooser", id: "command:4:0", text: sessionListText(nil, true), keep: true},
		{name: "chooser", id: "command:4:0", text: "Delegated Sessions:\n1. Session A", extra: true, keep: true},
		{name: "unknown control", id: "ack:4:0", text: "Notice", extra: true, keep: true},
		{name: "unknown row", id: "unknown:4", text: "Brain", keep: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, owner, _, root := configuredManager(t)
			bindOwner(t, m, 1, 10, 10)
			deliverChrome(t, m)
			keyboard := navigationKeyboard(m.store.snapshot(), 0)
			if test.feedback {
				keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{Text: "\U0001f44d", CallbackData: "feedback:up"}, {Text: "\U0001f44e", CallbackData: "feedback:down"}, {Text: "Clear feedback", CallbackData: "feedback:clear"}})
			}
			if test.extra {
				keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{Text: "Open Session", CallbackData: "session:fixture"}})
			}
			if err := m.store.mutate(func(s *durableState) error {
				enqueue(s, outboxRecord{ID: test.id, Kind: "send", CanonicalID: test.canonical, BrainThreadID: "thread-1", Text: test.text, ReplyMessageID: 40, ReplyMarkup: keyboard, CreatedAt: m.now()})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewManagerWithOptions(root, owner, Options{})
			if err != nil {
				t.Fatal(err)
			}
			wire := chromeWire(t, reopened)
			deliverChrome(t, reopened)
			var expected any
			if test.keep {
				encoded, _ := json.Marshal(keyboard)
				_ = json.Unmarshal(encoded, &expected)
			}
			if got := wire.bodies[0]["reply_markup"]; !reflect.DeepEqual(got, expected) {
				t.Fatalf("stored markup on wire=%+v want=%+v", got, expected)
			}
			row := navigationRow(t, reopened, test.id)
			if row.State != "sent" || row.Text != test.text || row.ReplyMessageID != 40 || (row.ReplyMarkup != nil) != test.keep || reopened.store.snapshot().MessageSources[row.MessageID].BrainThreadID != "thread-1" {
				t.Fatalf("delivery/source changed: %+v", row)
			}
		})
	}
}
