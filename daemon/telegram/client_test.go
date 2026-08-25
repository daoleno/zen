package telegram

import (
	"encoding/json"
	"fmt"
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
		case "sendChatAction":
			result = true
		case "createForumTopic":
			result = map[string]any{"message_thread_id": 42, "name": body["name"]}
		case "editForumTopic", "closeForumTopic", "reopenForumTopic", "deleteForumTopic":
			result = true
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
	entity := MessageEntity{Type: "bold", Offset: 0, Length: 5}
	sent, err := client.SendMessage(t.Context(), "fixture-token", SendRequest{ChatID: 99, Text: "hello", Entities: []MessageEntity{entity}, ReplyToMessageID: 44})
	if err != nil || sent.MessageID != 51 {
		t.Fatalf("sent=%+v err=%v", sent, err)
	}
	edited, err := client.EditMessage(t.Context(), "fixture-token", EditRequest{ChatID: 99, MessageID: 51, Text: "updated", Entities: []MessageEntity{{Type: "italic", Offset: 0, Length: 7}}})
	if err != nil || edited.MessageID != 51 {
		t.Fatalf("edited=%+v err=%v", edited, err)
	}
	if err := client.SendChatAction(t.Context(), "fixture-token", ChatActionRequest{ChatID: 99, Action: "typing"}); err != nil {
		t.Fatal(err)
	}

	fake.mu.Lock()
	fake.limit = true
	fake.mu.Unlock()
	_, err = client.SendMessage(t.Context(), "fixture-token", SendRequest{ChatID: 99, Text: "limited"})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 429 || apiErr.RetryAfter != 7*time.Second || !apiErr.Retryable {
		t.Fatalf("rate error=%T %+v", err, err)
	}
	if apiErr.description != "fixture description must not escape the client" {
		t.Fatalf("internal description=%q", apiErr.description)
	}
	if strings.Contains(err.Error(), "fixture description") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("error leaked server or credential data: %q", err)
	}
	for _, public := range []string{fmt.Sprint(apiErr), fmt.Sprintf("%v", apiErr), fmt.Sprintf("%+v", apiErr), fmt.Sprintf("%#v", apiErr)} {
		if strings.Contains(public, "fixture description") || public != "Telegram Bot API request failed" {
			t.Fatalf("formatted error leaked internal description: %q", public)
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := fake.methods; strings.Join(got, ",") != "getMe,getWebhookInfo,getUpdates,sendMessage,editMessageText,sendChatAction,sendMessage" {
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
	if entities, ok := sendBody["entities"].([]any); !ok || len(entities) != 1 {
		t.Fatalf("send entities=%v", sendBody["entities"])
	}
	editBody := fake.bodies[4]
	if entities, ok := editBody["entities"].([]any); !ok || len(entities) != 1 {
		t.Fatalf("edit entities=%v", editBody["entities"])
	}
	actionBody := fake.bodies[5]
	if actionBody["chat_id"] != float64(99) || actionBody["action"] != "typing" {
		t.Fatalf("chat action body=%v", actionBody)
	}
	if sendBody["message_thread_id"] != nil {
		t.Fatalf("General send unexpectedly carried message_thread_id: %v", sendBody["message_thread_id"])
	}
}

func TestClientTopicMethodsAndThreadAwareSend(t *testing.T) {
	fake := newFakeBotAPIServer(t)
	client := NewClient(fake.server.URL, fake.server.Client())

	topic, err := client.CreateForumTopic(t.Context(), "fixture-token", CreateForumTopicRequest{ChatID: 99, Name: "Session one"})
	if err != nil || topic.MessageThreadID != 42 || topic.Name != "Session one" {
		t.Fatalf("create topic=%+v err=%v", topic, err)
	}
	if err := client.EditForumTopic(t.Context(), "fixture-token", EditForumTopicRequest{ChatID: 99, MessageThreadID: 42, Name: "Session one renamed"}); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseForumTopic(t.Context(), "fixture-token", ForumTopicIDRequest{ChatID: 99, MessageThreadID: 42}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReopenForumTopic(t.Context(), "fixture-token", ForumTopicIDRequest{ChatID: 99, MessageThreadID: 42}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteForumTopic(t.Context(), "fixture-token", ForumTopicIDRequest{ChatID: 99, MessageThreadID: 42}); err != nil {
		t.Fatal(err)
	}
	sent, err := client.SendMessage(t.Context(), "fixture-token", SendRequest{ChatID: 99, MessageThreadID: 42, Text: "topic text", ReplyToMessageID: 7})
	if err != nil || sent.MessageID != 51 {
		t.Fatalf("topic send=%+v err=%v", sent, err)
	}
	if err := client.SendChatAction(t.Context(), "fixture-token", ChatActionRequest{ChatID: 99, MessageThreadID: 42, Action: "typing"}); err != nil {
		t.Fatal(err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := strings.Join(fake.methods, ","); got != "createForumTopic,editForumTopic,closeForumTopic,reopenForumTopic,deleteForumTopic,sendMessage,sendChatAction" {
		t.Fatalf("methods=%v", got)
	}
	createBody := fake.bodies[0]
	if createBody["chat_id"] != float64(99) || createBody["name"] != "Session one" {
		t.Fatalf("createForumTopic body=%v", createBody)
	}
	editBody := fake.bodies[1]
	if editBody["message_thread_id"] != float64(42) || editBody["name"] != "Session one renamed" {
		t.Fatalf("editForumTopic body=%v", editBody)
	}
	for _, index := range []int{2, 3, 4} {
		if fake.bodies[index]["message_thread_id"] != float64(42) {
			t.Fatalf("topic id body=%v", fake.bodies[index])
		}
	}
	sendBody := fake.bodies[5]
	if sendBody["chat_id"] != float64(99) || sendBody["message_thread_id"] != float64(42) || sendBody["text"] != "topic text" {
		t.Fatalf("topic send body=%v", sendBody)
	}
	reply, ok := sendBody["reply_parameters"].(map[string]any)
	if !ok || reply["message_id"] != float64(7) {
		t.Fatalf("topic reply body=%v", sendBody)
	}
	actionBody := fake.bodies[6]
	if actionBody["chat_id"] != float64(99) || actionBody["message_thread_id"] != float64(42) || actionBody["action"] != "typing" {
		t.Fatalf("topic action body=%v", actionBody)
	}
}

func TestFormattingRejectionClassificationIsNarrow(t *testing.T) {
	if !formattingRejected(&APIError{Code: 400, description: "Bad Request: can't parse entities: offset"}) {
		t.Fatal("definite entity rejection was not classified")
	}
	for _, err := range []error{
		&APIError{Code: 400, description: "Bad Request: message is not modified"},
		&APIError{Code: 429, Retryable: true, description: "can't parse entities"},
		&APIError{Code: 500, Retryable: true, description: "can't parse entities"},
	} {
		if formattingRejected(err) {
			t.Fatalf("non-format error classified as formatting: %+v", err)
		}
	}
}
