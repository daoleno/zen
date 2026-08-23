package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBotAPIServer struct {
	server  *httptest.Server
	mu      sync.Mutex
	methods []string
	bodies  []map[string]any
	limit   bool
}

func newFakeBotAPIServer(t *testing.T) *fakeBotAPIServer {
	t.Helper()
	fake := &fakeBotAPIServer{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		method := strings.TrimPrefix(request.URL.Path, "/botfixture-token/")
		if method == request.URL.Path {
			t.Errorf("unexpected Bot API path %q", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if request.Body != nil {
			_ = json.NewDecoder(request.Body).Decode(&body)
		}
		fake.mu.Lock()
		fake.methods = append(fake.methods, method)
		fake.bodies = append(fake.bodies, body)
		limited := fake.limit && method == "sendMessage"
		if limited {
			fake.limit = false
		}
		fake.mu.Unlock()

		writer.Header().Set("Content-Type", "application/json")
		if limited {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok":          false,
				"error_code":  429,
				"description": "fixture description must not escape the client",
				"parameters":  map[string]any{"retry_after": 7},
			})
			return
		}
		var result any = true
		switch method {
		case "getMe":
			result = map[string]any{"id": 7001, "is_bot": true, "first_name": "Zen", "username": "zen_fixture_bot"}
		case "getWebhookInfo":
			result = map[string]any{"url": "", "pending_update_count": 0}
		case "getUpdates":
			result = []any{map[string]any{
				"update_id": 12,
				"message": map[string]any{
					"message_id": 44,
					"from":       map[string]any{"id": 99, "is_bot": false, "first_name": "Fixture"},
					"chat":       map[string]any{"id": 99, "type": "private"},
					"text":       "fixture",
				},
			}}
		case "sendMessage":
			result = map[string]any{"message_id": 51, "chat": map[string]any{"id": 99, "type": "private"}}
		case "editMessageText":
			result = map[string]any{"message_id": 51, "chat": map[string]any{"id": 99, "type": "private"}}
		default:
			t.Errorf("unexpected method %q", method)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "result": result})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func TestClientAgainstFakeBotAPIServer(t *testing.T) {
	fake := newFakeBotAPIServer(t)
	client := NewClient(fake.server.URL, fake.server.Client())

	bot, err := client.GetMe(t.Context(), "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	if bot.ID != 7001 || bot.Username != "zen_fixture_bot" {
		t.Fatalf("bot=%+v", bot)
	}
	webhook, err := client.GetWebhookInfo(t.Context(), "fixture-token")
	if err != nil || webhook.URL != "" {
		t.Fatalf("webhook=%+v err=%v", webhook, err)
	}
	updates, err := client.GetUpdates(t.Context(), "fixture-token", 12, 30, []string{"message"})
	if err != nil || len(updates) != 1 || updates[0].UpdateID != 12 {
		t.Fatalf("updates=%+v err=%v", updates, err)
	}
	sent, err := client.SendMessage(t.Context(), "fixture-token", SendRequest{ChatID: 99, Text: "hello", ReplyToMessageID: 44})
	if err != nil || sent.MessageID != 51 {
		t.Fatalf("sent=%+v err=%v", sent, err)
	}
	edited, err := client.EditMessage(t.Context(), "fixture-token", EditRequest{ChatID: 99, MessageID: 51, Text: "updated"})
	if err != nil || edited.MessageID != 51 {
		t.Fatalf("edited=%+v err=%v", edited, err)
	}

	fake.mu.Lock()
	fake.limit = true
	fake.mu.Unlock()
	_, err = client.SendMessage(t.Context(), "fixture-token", SendRequest{ChatID: 99, Text: "limited"})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 429 || apiErr.RetryAfter != 7*time.Second || !apiErr.Retryable {
		t.Fatalf("rate error=%T %+v", err, err)
	}
	if strings.Contains(err.Error(), "fixture description") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("error leaked server or credential data: %q", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := fake.methods; strings.Join(got, ",") != "getMe,getWebhookInfo,getUpdates,sendMessage,editMessageText,sendMessage" {
		t.Fatalf("methods=%v", got)
	}
	updatesBody := fake.bodies[2]
	if updatesBody["offset"] != float64(12) || updatesBody["timeout"] != float64(30) {
		t.Fatalf("getUpdates body=%v", updatesBody)
	}
	sendBody := fake.bodies[3]
	if sendBody["chat_id"] != float64(99) || sendBody["text"] != "hello" {
		t.Fatalf("send body=%v", sendBody)
	}
	if reply, ok := sendBody["reply_parameters"].(map[string]any); !ok || reply["message_id"] != float64(44) {
		t.Fatalf("reply body=%v", sendBody)
	}
}
