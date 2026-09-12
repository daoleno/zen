package telegram

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only. Two bounded requests in a previously verified QA
// topic; no polling, history reads/replay, provider input or state mutation.
func TestLiveTelegramLocalFileFormatting(t *testing.T) {
	root := os.Getenv("ZEN_TELEGRAM_LIVE_STATE")
	if root == "" {
		t.Skip("requires authorized bound private QA topic")
	}
	topic, err := strconv.ParseInt(os.Getenv("ZEN_TELEGRAM_LIVE_QA_TOPIC"), 10, 64)
	if err != nil || topic <= 1 {
		t.Fatal("exact QA topic required")
	}
	raw, err := os.ReadFile(filepath.Join(root, "telegram", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state durableState
	if json.Unmarshal(raw, &state) != nil || state.OwnerID == 0 || state.OwnerID != state.ChatID {
		t.Fatal("verified private owner binding required")
	}
	secret, err := os.ReadFile(filepath.Join(root, "telegram", "token"))
	if err != nil {
		t.Fatal("credential unavailable")
	}
	token := strings.TrimSpace(string(secret))
	client := NewClient("", nil)
	bot, err := client.GetMe(t.Context(), token)
	if err != nil || bot.ID != state.BotID || !bot.Topics {
		t.Fatal("bot identity/mode mismatch")
	}
	webhook, err := client.GetWebhookInfo(t.Context(), token)
	if err != nil || webhook.URL != "" {
		t.Fatal("unexpected webhook mode")
	}
	var chat Chat
	if err := client.call(t.Context(), token, "getChat", map[string]any{"chat_id": state.ChatID}, &chat); err != nil || chat.Type != "private" || chat.ID != state.OwnerID {
		t.Fatal("private owner mismatch")
	}
	time.Sleep(1100 * time.Millisecond)
	_, err = client.SendMessage(t.Context(), token, SendRequest{ChatID: state.ChatID, MessageThreadID: topic, Text: "fixture.go", Entities: []MessageEntity{{Type: "text_link", Offset: 0, Length: 10, URL: "/workspace/fixture.go"}}})
	var rejection *APIError
	if !errors.As(err, &rejection) || rejection.Code != 400 {
		t.Fatal("local-file URL did not reproduce definite rejection")
	}
	t.Log("Telegram rejected local-file URL entity with HTTP 400; raw API description withheld")
	time.Sleep(1100 * time.Millisecond)
	rendered := renderMarkdown("**Zen QA**: [fixture.go](/workspace/fixture.go)")
	message, err := client.SendMessage(t.Context(), token, SendRequest{ChatID: state.ChatID, MessageThreadID: topic, Text: rendered.Text, Entities: rendered.Entities})
	if err != nil || message.MessageThreadID != topic {
		t.Fatal("corrected formatter did not deliver to exact QA topic")
	}
	t.Logf("Production formatter delivered synthetic file-link receipt: topic=%d message=%d; no agent input", topic, message.MessageID)
}
