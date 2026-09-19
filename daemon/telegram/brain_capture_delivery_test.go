package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pacedPollAPI turns the fake receive endpoint into a bounded long poll so the
// real Manager.Run loop iterates without a busy spin. It also provides the
// optional interaction surface so the pinned Brain entry is not a fixture
// failure.
type pacedPollAPI struct {
	*fakeAPI
	delay time.Duration
}

func (p pacedPollAPI) GetUpdates(ctx context.Context, token string, offset int64, timeout int, allowed []string) ([]Update, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.delay):
	}
	return p.fakeAPI.GetUpdates(ctx, token, offset, timeout, allowed)
}

func (p pacedPollAPI) PinChatMessage(context.Context, string, int64, int64) error {
	return nil
}

func (p pacedPollAPI) SetMessageReaction(context.Context, string, ReactionRequest) error {
	return nil
}

// writeTelegramPiFixture writes the exact provider-native Pi transcript shape.
// The leading public user turn keeps the shared host projection from
// suppressing assistant output, matching a real host transcript.
func writeTelegramPiFixture(t *testing.T, path, sessionID, prompt string, bodies ...string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	now := time.Now().UTC()
	if err := encoder.Encode(map[string]any{
		"type": "session", "version": 3, "id": sessionID,
		"timestamp": now.Format(time.RFC3339Nano), "cwd": filepath.Dir(path),
	}); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(map[string]any{
		"type": "message", "id": sessionID + "-u", "parentId": nil,
		"timestamp": now.Format(time.RFC3339Nano),
		"message":   map[string]any{"role": "user", "content": prompt},
	}); err != nil {
		t.Fatal(err)
	}
	parentID := sessionID + "-u"
	for index, body := range bodies {
		id := sessionID + "-a" + string(rune('0'+index))
		if err := encoder.Encode(map[string]any{
			"type": "message", "id": id, "parentId": parentID,
			"timestamp": now.Add(time.Duration(index+1) * time.Second).Format(time.RFC3339Nano),
			"message": map[string]any{
				"role":       "assistant",
				"content":    []map[string]string{{"type": "text", "text": body}},
				"stopReason": "stop",
			},
		}); err != nil {
			t.Fatal(err)
		}
		parentID = id
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForFakeSent(t *testing.T, api *fakeAPI, body string, timeout time.Duration) SendRequest {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		for _, request := range api.sent {
			if strings.Contains(request.Text, body) {
				api.mu.Unlock()
				return request
			}
		}
		api.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	t.Fatalf("reply %q never reached the fake Bot API; %d sends recorded", body, len(api.sent))
	return SendRequest{}
}

func flushTopicOps(t *testing.T, m *Manager) {
	t.Helper()
	for m.hasDeliverableTopicOp() {
		if err := m.deliverTopicOpOne(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
	}
}

func flushOutbox(t *testing.T, m *Manager) {
	t.Helper()
	for m.hasDeliverableOutbox() {
		if err := m.deliverOne(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTelegramBrainReplyArrivesWithoutAppSubscription is the regression for
// Telegram never receiving canonical Brain replies unless the App was open.
// No server, WebSocket, or App subscription exists in this test process: the
// daemon-owned Host transcript capture plus the real Telegram Run loop must
// deliver provider assistant output to the fake Bot API on their own.
func TestTelegramBrainReplyArrivesWithoutAppSubscription(t *testing.T) {
	m, store, service, provider, api, root := realConversationFixture(t)
	const prompt = "Zen QA only: hello brain"
	const reply = "Zen QA only: canonical reply without any app"
	transcript := filepath.Join(root, "host-transcript.jsonl")
	provider.mu.Lock()
	provider.workers["host:@1"].Command = "pi --session " + transcript
	provider.workers["host:@1"].Cwd = root
	provider.mu.Unlock()
	if err := store.SetHostProviderTranscript("fixture-host-session", transcript, ""); err != nil {
		t.Fatal(err)
	}

	if err := m.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	// Swap the paced receive loop in before any manager goroutine exists, so the
	// test never mutates the transport under a running Manager.
	m.api = pacedPollAPI{fakeAPI: api, delay: 10 * time.Millisecond}
	flushTopicOps(t, m)
	primary := m.store.snapshot().BrainTopicID
	if primary == 0 {
		t.Fatal("fixture Brain topic was not provisioned")
	}
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(2, primary, prompt)); err != nil {
		t.Fatal(err)
	}
	// The provider transcript stamps its public user echo after transport
	// acceptance, exactly like a live provider turn, so the durable admission
	// can claim it.
	writeTelegramPiFixture(t, transcript, "fixture-host-session", prompt, reply)

	captureCtx, cancelCapture := context.WithCancel(t.Context())
	defer cancelCapture()
	go service.RunHostTranscriptCapture(captureCtx)

	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	go func() { _ = m.Run(runCtx) }()

	request := waitForFakeSent(t, api, reply, 20*time.Second)
	if request.ChatID != 10 || request.MessageThreadID != primary {
		t.Fatalf("canonical reply left its Brain topic: %+v", request)
	}
	// The provider user echo must be claimed by the canonical admission, never
	// materialized as a second user row.
	items, err := store.ThreadTimeline("brain-current", 0)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, item := range items {
		if item.Kind == "user_message" && strings.TrimSpace(item.Body) == prompt {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("provider echo duplicated the admitted user row: %d", users)
	}
}

// TestTelegramDelegatedSessionReplyWithoutAppSubscription proves the exact
// delegated Session recipient keeps working with no App subscription: the
// canonical Session projection reads the provider transcript itself and the
// Telegram outbox delivers it to the mapped topic only.
func TestTelegramDelegatedSessionReplyWithoutAppSubscription(t *testing.T) {
	m, _, service, provider, api, root := realConversationFixture(t)
	transcript := filepath.Join(root, "session-a-transcript.jsonl")
	const reply = "Zen QA only: delegated reply without any app"
	provider.mu.Lock()
	provider.workers["session-a"].Command = "pi --session " + transcript
	provider.workers["session-a"].Cwd = root
	provider.mu.Unlock()

	if err := m.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	flushTopicOps(t, m)
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	flushTopicOps(t, m)
	sessionTopic := topicThreadFor(t, m, "session-a")
	if sessionTopic == 0 {
		t.Fatal("fixture Session topic was not provisioned")
	}

	// The transcript appears after the topic boundary, exactly like a live
	// provider turn.
	writeTelegramPiFixture(t, transcript, "fixture-session-a", "Zen QA only: session question", reply)
	projection, err := service.SessionProjection("session-a")
	if err != nil || len(projection.Assistant) == 0 {
		t.Fatalf("real provider transcript projection missing: items=%d err=%v", len(projection.Assistant), err)
	}
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	flushTopicOps(t, m)
	flushOutbox(t, m)

	request := waitForFakeSent(t, api, reply, time.Second)
	if request.ChatID != 10 || request.MessageThreadID != sessionTopic {
		t.Fatalf("delegated reply left its exact Session topic: %+v want topic %d", request, sessionTopic)
	}
}
