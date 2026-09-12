package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type liveRichFiles struct {
	*fakeAPI
	client *Client
	token  string
}

func (a *liveRichFiles) GetFile(ctx context.Context, _ string, id string) (File, error) {
	return a.client.GetFile(ctx, a.token, id)
}
func (a *liveRichFiles) DownloadFile(ctx context.Context, _ string, file File) (io.ReadCloser, error) {
	return a.client.DownloadFile(ctx, a.token, file)
}

// Explicit opt-in. One QA topic, <=12 pre-counted write requests. The canonical
// runtime remains the sole poller. This test never edits its state or history.
func TestLiveTelegramRichInteractions(t *testing.T) {
	root := os.Getenv("ZEN_TELEGRAM_LIVE_STATE")
	if root == "" || os.Getenv("ZEN_TELEGRAM_LIVE_RICH") != "1" {
		t.Skip("requires authorized bound-private QA with at most 12 writes")
	}
	type binding struct {
		Enabled      bool  `json:"enabled"`
		BotID        int64 `json:"bot_id"`
		OwnerID      int64 `json:"owner_id"`
		ChatID       int64 `json:"chat_id"`
		BrainTopicID int64 `json:"brain_topic_id"`
	}
	readBinding := func() binding {
		raw, err := os.ReadFile(filepath.Join(root, "telegram", "state.json"))
		if err != nil {
			t.Fatal("binding unavailable")
		}
		var value binding
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("binding invalid")
		}
		return value
	}
	before := readBinding()
	if !before.Enabled || before.OwnerID == 0 || before.OwnerID != before.ChatID || before.BrainTopicID != 76315 {
		t.Fatal("expected enabled bound private bot and primary Brain required")
	}
	raw, err := os.ReadFile(filepath.Join(root, "telegram", "token"))
	if err != nil {
		t.Fatal("credential unavailable")
	}
	token := strings.TrimSpace(string(raw))
	c := NewClient("", nil)
	bot, err := c.GetMe(t.Context(), token)
	if err != nil || bot.ID != before.BotID || !bot.Topics {
		t.Fatal("bot identity/mode mismatch")
	}
	webhook, err := c.GetWebhookInfo(t.Context(), token)
	if err != nil || webhook.URL != "" {
		t.Fatal("webhook mode mismatch")
	}
	var chat Chat
	if err := c.call(t.Context(), token, "getChat", map[string]any{"chat_id": before.ChatID}, &chat); err != nil || chat.ID != before.OwnerID || chat.Type != "private" {
		t.Fatal("private owner mismatch")
	}
	writes := 0
	last := time.Time{}
	count := func(method string) {
		t.Helper()
		if writes >= 12 {
			t.Fatal("live write budget exhausted before network")
		}
		if wait := time.Until(last.Add(1500 * time.Millisecond)); wait > 0 {
			time.Sleep(wait)
		}
		writes++
		last = time.Now()
		t.Logf("pre-network write %d/12: %s", writes, method)
	}
	count("createForumTopic")
	qa, err := c.CreateForumTopic(t.Context(), token, CreateForumTopicRequest{ChatID: before.ChatID, Name: "Zen QA Rich Interactions " + time.Now().UTC().Format("150405")})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created one owned QA topic=%d", qa.MessageThreadID)
	defer func() {
		count("editForumTopic Closed")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.EditForumTopic(ctx, token, EditForumTopicRequest{ChatID: before.ChatID, MessageThreadID: qa.MessageThreadID, Name: "Closed - Zen QA Rich Interactions"}); err != nil {
			t.Errorf("QA rename: %v", err)
		}
		if after := readBinding(); after != before {
			t.Error("canonical bot binding changed")
		}
		t.Logf("total live writes=%d/12; no delete, getUpdates, webhook mutation, bot toggle or provider request", writes)
	}()
	state := durableState{TopicsAvailable: true, BotUsername: bot.Username, BrainTopicID: before.BrainTopicID}
	count("sendMessage Brain navigation")
	nav, err := c.SendMessage(t.Context(), token, SendRequest{ChatID: before.ChatID, MessageThreadID: qa.MessageThreadID, Text: "Zen QA: Brain entry", ReplyMarkup: navigationKeyboard(state, qa.MessageThreadID)})
	if err != nil {
		t.Fatal(err)
	}
	count("pinChatMessage navigation")
	if err := c.PinChatMessage(t.Context(), token, before.ChatID, nav.MessageID); err != nil {
		t.Logf("private message pin unavailable: %v", err)
	} else {
		t.Logf("private navigation message pin accepted; message=%d, Brain topic unchanged=%d", nav.MessageID, before.BrainTopicID)
	}
	f := newMediaFixture(t)
	f.m.api = &liveRichFiles{fakeAPI: f.api.fakeAPI, client: c, token: token}
	sendFixture := func(method, field, name, caption string, data []byte) Message {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("chat_id", fmt.Sprint(before.ChatID))
		_ = writer.WriteField("message_thread_id", fmt.Sprint(qa.MessageThreadID))
		_ = writer.WriteField("caption", caption)
		part, err := writer.CreateFormFile(field, name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = writer.Close()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, c.baseURL+"/bot"+url.PathEscape(token)+"/"+method, &body)
		if err != nil {
			t.Fatal("invalid QA request")
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		count(method + " tiny owned fixture")
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal("QA upload transport unavailable")
		}
		defer resp.Body.Close()
		var result apiEnvelope
		if json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result) != nil || !result.OK {
			t.Fatalf("QA upload rejected (code %d)", result.ErrorCode)
		}
		var message Message
		if json.Unmarshal(result.Result, &message) != nil || message.Chat.ID != before.ChatID || message.MessageThreadID != qa.MessageThreadID {
			t.Fatal("QA reply destination mismatch")
		}
		return message
	}
	doc := sendFixture("sendDocument", "document", "zen-qa-owned.txt", "QA document \U0001f44d", f.files["doc"])
	photo := sendFixture("sendPhoto", "photo", "zen-qa-owned.jpg", "QA photo \u2764\ufe0f", f.files["photo"])
	// Returned bot sends are NOT owner-upload events. Re-envelope only into
	// isolated real Stores to verify the production download/admission path.
	for i, message := range []Message{doc, photo} {
		originalID := message.MessageID
		message.From = &User{ID: 10}
		message.Chat = Chat{ID: 10, Type: "private"}
		message.MessageID = int64(i + 2)
		message.MessageThreadID = 101
		if i == 1 {
			message.MessageThreadID = 76315
		}
		f.apply(t, Update{UpdateID: int64(i + 2), Message: &message})
		row := f.provider.receipts()[fmt.Sprintf("telegram:update:7001:%d", i+2)]
		if row.Body == "" {
			t.Fatal("real Telegram bytes did not reach inert provider")
		}
		envelope := decodeAttachment(t, row.Body)
		if len(envelope.Files) != 1 || !f.m.attachments.Exists(envelope.Files[0]) {
			t.Fatal("download missing")
		}
		data, err := os.ReadFile(envelope.Files[0].Path)
		if err != nil || len(data) == 0 {
			t.Fatal("download bytes missing")
		}
		if i == 0 && !bytes.Equal(data, f.files["doc"]) {
			t.Fatal("document round-trip bytes changed")
		}
		if i == 1 && http.DetectContentType(data) != "image/jpeg" {
			t.Fatal("photo round trip is not a JPEG")
		}
		t.Logf("LIVE upload/getFile/download message=%d bytes=%d type=%s; SYNTHETIC owner envelope -> REAL Store/admission -> INERT provider %s", originalID, len(data), envelope.Files[0].ContentType, row.SessionID)
	}
	reactions := [][]ReactionType{{{Type: "emoji", Emoji: "\U0001f44d"}}, {{Type: "emoji", Emoji: "\U0001f44e"}}, {}}
	for _, reaction := range reactions {
		count("setMessageReaction QA photo")
		if err := c.SetMessageReaction(t.Context(), token, ReactionRequest{ChatID: before.ChatID, MessageID: photo.MessageID, Reaction: reaction}); err != nil {
			t.Logf("private bot reaction unavailable: %v; supported inline-feedback fallback remains", err)
			break
		}
		t.Logf("private bot setMessageReaction accepted; reaction count=%d", len(reaction))
	}
	t.Log("Native owner reaction UPDATE/UI reception not proven: no signed-in client and no second update consumer; administrator-gated Bot API documented fallback tested offline.")
}
