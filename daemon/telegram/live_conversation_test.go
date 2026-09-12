package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
)

// A bounded outbound transport, never a poller. Input/callback envelopes and
// provider IO are fixtures; only the two newly created QA topics may be written.
type boundedConversationAPI struct {
	*Client
	t       *testing.T
	chatID  int64
	token   string
	topics  map[int64]bool
	count   int
	last    time.Time
	sent    []Message
	created int
}

func (a *boundedConversationAPI) mutation(chatID, topic int64) {
	a.t.Helper()
	if chatID != a.chatID || (topic != 0 && !a.topics[topic]) || a.count >= 18 {
		a.t.Fatal("live QA destination or operation budget violation")
	}
	if wait := time.Until(a.last.Add(1500 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	a.count++
	a.last = time.Now()
}

func (a *boundedConversationAPI) GetUpdates(context.Context, string, int64, int, []string) ([]Update, error) {
	a.t.Fatal("live QA must never poll")
	return nil, fmt.Errorf("poll prohibited")
}
func (a *boundedConversationAPI) AnswerCallbackQuery(context.Context, string, string, string) error {
	return nil // Synthetic callback IDs cannot be answered by the real Bot API.
}
func (a *boundedConversationAPI) SendChatAction(context.Context, string, ChatActionRequest) error {
	return nil
}
func (a *boundedConversationAPI) CreateForumTopic(ctx context.Context, token string, req CreateForumTopicRequest) (ForumTopic, error) {
	if !strings.HasPrefix(req.Name, "Zen QA ") || a.created >= 2 {
		a.t.Fatal("live QA may create only two clearly labeled fixture topics")
	}
	a.mutation(req.ChatID, 0)
	a.created++
	result, err := a.Client.CreateForumTopic(ctx, a.token, req)
	if err == nil {
		a.topics[result.MessageThreadID] = true
		a.t.Logf("Created isolated QA topic=%d", result.MessageThreadID)
	}
	return result, err
}
func (a *boundedConversationAPI) SendMessage(ctx context.Context, token string, req SendRequest) (Message, error) {
	if req.MessageThreadID == 0 {
		a.t.Fatal("live QA may not send to General")
	}
	a.mutation(req.ChatID, req.MessageThreadID)
	result, err := a.Client.SendMessage(ctx, a.token, req)
	if err == nil {
		if result.Chat.ID != a.chatID || result.MessageThreadID != req.MessageThreadID {
			a.t.Fatal("Telegram returned a different reply destination")
		}
		a.sent = append(a.sent, result)
		a.t.Logf("Telegram accepted exact destination topic=%d message=%d keyboard=%t", result.MessageThreadID, result.MessageID, req.ReplyMarkup != nil)
	}
	return result, err
}
func (a *boundedConversationAPI) EditMessage(context.Context, string, EditRequest) (Message, error) {
	a.t.Fatal("live QA does not edit messages")
	return Message{}, fmt.Errorf("message edit prohibited")
}
func (a *boundedConversationAPI) EditForumTopic(ctx context.Context, token string, req EditForumTopicRequest) error {
	if !strings.HasPrefix(req.Name, "Closed") || !a.topics[req.MessageThreadID] {
		a.t.Fatal("live QA may only mark its own topics Closed")
	}
	a.mutation(req.ChatID, req.MessageThreadID)
	err := a.Client.EditForumTopic(ctx, a.token, req)
	if err == nil {
		a.t.Logf("Retained QA topic=%d with Closed label", req.MessageThreadID)
	}
	return err
}
func (a *boundedConversationAPI) CloseForumTopic(context.Context, string, ForumTopicIDRequest) error {
	a.t.Fatal("private close prohibited")
	return fmt.Errorf("close prohibited")
}
func (a *boundedConversationAPI) ReopenForumTopic(context.Context, string, ForumTopicIDRequest) error {
	a.t.Fatal("private reopen prohibited")
	return fmt.Errorf("reopen prohibited")
}
func (a *boundedConversationAPI) DeleteForumTopic(context.Context, string, ForumTopicIDRequest) error {
	a.t.Fatal("history deletion prohibited")
	return fmt.Errorf("delete prohibited")
}

func TestLiveTelegramConversationWithIsolatedBrainStore(t *testing.T) {
	root := os.Getenv("ZEN_TELEGRAM_LIVE_STATE")
	if root == "" || os.Getenv("ZEN_TELEGRAM_LIVE_CONVERSATION") != "1" {
		t.Skip("requires explicit authorization for two bound private QA topics")
	}
	readState := func() durableState {
		raw, err := os.ReadFile(filepath.Join(root, "telegram", "state.json"))
		var state durableState
		if err != nil || json.Unmarshal(raw, &state) != nil {
			t.Fatal("bound state unavailable")
		}
		return state
	}
	before := readState()
	if !before.Enabled || before.OwnerID == 0 || before.ChatID != before.OwnerID || before.BrainTopicID == 0 {
		t.Fatal("enabled verified private binding required")
	}
	secret, err := os.ReadFile(filepath.Join(root, "telegram", "token"))
	if err != nil {
		t.Fatal("configured credential unavailable")
	}
	token := strings.TrimSpace(string(secret))
	client := NewClient("", nil)
	bot, err := client.GetMe(t.Context(), token)
	if err != nil || bot.ID != before.BotID || !bot.Topics || !bot.UserTopics {
		t.Fatal("bot identity or mode mismatch")
	}
	webhook, err := client.GetWebhookInfo(t.Context(), token)
	if err != nil || webhook.URL != "" {
		t.Fatal("unexpected webhook mode")
	}
	var chat Chat
	if err := client.call(t.Context(), token, "getChat", map[string]any{"chat_id": before.ChatID}, &chat); err != nil || chat.ID != before.OwnerID || chat.Type != "private" {
		t.Fatal("private owner mismatch")
	}

	m, store, service, provider, _, fixtureRoot := realConversationFixture(t)
	api := &boundedConversationAPI{Client: client, t: t, chatID: before.ChatID, token: token, topics: map[int64]bool{}}
	m.api = api
	stamp := time.Now().UTC().Format("150405")
	transcript := filepath.Join(fixtureRoot, "qa-provider.jsonl")
	provider.mu.Lock()
	delete(provider.workers, "session-b")
	provider.workers["session-a"].Name = "Zen QA Session " + stamp
	provider.workers["session-a"].Cwd = fixtureRoot
	provider.workers["session-a"].Command = "pi --session " + transcript
	provider.mu.Unlock()
	if err := m.store.mutate(func(s *durableState) error {
		*s = newDurableState()
		s.Enabled, s.TopicsAvailable, s.UsersCreateTopics = true, true, true
		s.BotID, s.BotUsername = bot.ID, bot.Username
		s.ChatID, s.OwnerID = before.ChatID, before.OwnerID
		s.DeliveryStartedAt = time.Now().UTC()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	if err := m.store.mutate(func(s *durableState) error { s.TopicOps[0].Label = "Zen QA Brain " + stamp; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.projectSessionTopics(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	flush := func() {
		t.Helper()
		for m.hasDeliverableTopicOp() {
			if err := m.deliverTopicOpOne(t.Context(), token); err != nil {
				t.Fatal(err)
			}
		}
		for m.hasDeliverableOutbox() {
			if err := m.deliverOne(t.Context(), token); err != nil {
				t.Fatal(err)
			}
		}
		for _, row := range m.store.snapshot().Outbox {
			if row.State != "sent" {
				t.Fatalf("QA output not delivered: id=%s state=%s", row.ID, row.State)
			}
		}
	}
	flush()
	primary, sessionTopic := m.store.snapshot().BrainTopicID, topicThreadFor(t, m, "session-a")
	input := func(id, topic int64, body string) {
		t.Helper()
		update := topicUpdate(id, topic, body)
		update.Message.MessageID = 0 // Synthetic input has no Telegram message to quote.
		update.Message.Chat.ID, update.Message.From.ID = before.ChatID, before.OwnerID
		if err := m.handleUpdate(t.Context(), token, update); err != nil {
			t.Fatal(err)
		}
	}
	answer := func(id, body string) {
		t.Helper()
		if _, err := store.AppendTimelineItem(brain.TimelineItem{ID: id, ThreadID: "brain-current", SessionID: "host:@1", Kind: "assistant_message", Role: "assistant", Body: body, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if err := m.projectTimeline(); err != nil {
			t.Fatal(err)
		}
	}
	input(2, primary, "Zen QA only: fixture Brain message")
	input(3, primary, "Zen QA only: same-conversation follow-up")
	answer("qa-first", "**Zen QA only**: two fixture inputs admitted to the same Brain conversation. No model call.")
	input(4, primary, "/sessions")
	flush()
	callback := navigationCallback(5, primary, "session:"+digestText("session-a"))
	callback.CallbackQuery.From.ID, callback.CallbackQuery.Message.Chat.ID = before.OwnerID, before.ChatID
	callback.CallbackQuery.Message.MessageID = 0
	if err := m.handleUpdate(t.Context(), token, callback); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "Zen QA only", Objective: "Isolated exact Session fixture", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := store.PrepareInputAdmission(watcher.InputAdmission{WorkID: item.ID, SessionID: "session-a", ProposedTurnID: "turn:qa", Receipt: "qa-busy", PayloadSHA256: brain.AdmissionDigest("qa"), PaneGeneration: "qa-generation", ProcessIdentity: "qa-process", AcceptedAt: time.Now().UTC(), Mode: watcher.InputAdmissionFresh})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveInputAdmission(watcher.InputAdmissionResolution{SessionID: "session-a", ProposedTurnID: admission.ProposedTurnID, Receipt: admission.Receipt, PayloadSHA256: admission.PayloadSHA256, ActivityID: "qa-activity", ResolvedAt: time.Now().UTC(), Admission: watcher.TurnAdmission{Stream: "fixture", ID: "qa-input", Cursor: 1, SHA256: admission.PayloadSHA256, At: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	input(6, sessionTopic, "Zen QA only: exact Session input")
	input(7, sessionTopic, "Zen QA only: busy Session follow-up")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	file, err := os.Create(transcript)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, record := range []any{
		map[string]any{"type": "session", "version": 3, "id": "qa-session", "timestamp": now, "cwd": fixtureRoot},
		map[string]any{"type": "message", "id": "qa-answer", "timestamp": now, "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": "Zen QA only: exact Session input and busy follow-up received by inert provider fixture."}}, "stopReason": "stop"}},
	} {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	projection, err := service.SessionProjection("session-a")
	if err != nil || len(projection.Assistant) != 1 {
		t.Fatalf("real provider transcript projection missing: items=%d err=%v", len(projection.Assistant), err)
	}
	if err := m.projectSessionTopics(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	input(8, sessionTopic, "/status")
	input(9, sessionTopic, "/brain")
	input(10, primary, "Zen QA only: back to same Brain")
	answer("qa-back", "Zen QA only: back to the same isolated Brain conversation; no new chat or Work created.")
	flush()
	for id, expected := range map[int64]string{2: "host:@1", 3: "host:@1", 6: "session-a", 7: "session-a", 10: "host:@1"} {
		if got := provider.receipts()[fmt.Sprintf("telegram:update:%d:%d", bot.ID, id)].SessionID; got != expected {
			t.Fatalf("fixture admission target %d = %s, want %s", id, got, expected)
		}
	}
	thread, _ := store.ChatThreadID()
	if thread != "brain-current" || provider.created != 0 {
		t.Fatal("fixture canonical Brain reset or Session replaced")
	}
	provider.mu.Lock()
	delete(provider.workers, "session-a")
	provider.mu.Unlock()
	if err := m.projectSessionTopics(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	input(11, sessionTopic, "Zen QA only: stale destination must reject")
	flush()
	mapping, found := topicMappingByThread(m.store.snapshot(), sessionTopic)
	if !found || mapping.State != topicStateStale || m.store.snapshot().Processed["11"].Disposition != "topic_stale" {
		t.Fatal("exact Session tombstone missing")
	}
	if err := api.EditForumTopic(t.Context(), token, EditForumTopicRequest{ChatID: before.ChatID, MessageThreadID: primary, Name: "Closed - Zen QA Brain " + stamp}); err != nil {
		t.Fatal(err)
	}
	after := readState()
	if before.BotID != after.BotID || before.OwnerID != after.OwnerID || before.BrainTopicID != after.BrainTopicID || !reflect.DeepEqual(before.BrainTopics, after.BrainTopics) || !before.DeliveryStartedAt.Equal(after.DeliveryStartedAt) {
		t.Fatal("production identity or delivery boundary changed during fixture QA")
	}
	t.Logf("PASS real Brain Store + real Bot API output: QA Brain=%d Session=%d sends=%d mutations=%d. Inbound/callback/provider are fixtures; production Brain topic=%d unchanged. No polling, model execution, deletion or live state writes.", primary, sessionTopic, len(api.sent), api.count, before.BrainTopicID)
}
