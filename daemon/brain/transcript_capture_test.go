package brain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

// writePiTranscriptFixture writes the exact provider-native Pi transcript shape
// the work package parses. Only the transcript IO is a fixture; capture,
// parsing, and durable materialization below are production code. The leading
// public user turn matches real host transcripts: private host bootstrap turns
// and their output are suppressed by the shared projection.
func writePiTranscriptFixture(t *testing.T, path, sessionID, prompt string, bodies ...string) {
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

func waitForTimelineAssistant(t *testing.T, store *Store, threadID string, want int) []TimelineItem {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []TimelineItem
	for time.Now().Before(deadline) {
		items, err := store.ThreadTimeline(threadID, 0)
		if err != nil {
			t.Fatal(err)
		}
		assistant := make([]TimelineItem, 0, len(items))
		for _, item := range items {
			if item.Kind == timelineKindAssistantMessage {
				assistant = append(assistant, item)
			}
		}
		last = assistant
		if len(assistant) == want {
			return assistant
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("assistant rows = %d, want %d (%+v)", len(last), want, last)
	return nil
}

// TestHostTranscriptCaptureMaterializesWithoutSubscriber is the regression for
// Telegram Brain replies that never appeared while no App WebSocket
// subscription was open. Nothing in this test opens a subscription: the
// daemon-owned capture loop must append provider assistant rows to the durable
// canonical timeline on its own, exactly once per provider event.
func TestHostTranscriptCaptureMaterializesWithoutSubscriber(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		threadID = "brain_thread_capture"
		hostID   = "zen-worker-brain-capture:@1"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "pi"); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(t.TempDir(), "host-session.jsonl")
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply")
	if err := store.SetHostProviderTranscript("capture-session", transcript, ""); err != nil {
		t.Fatal(err)
	}
	watcher := &fakeWatcher{sessions: map[string]*classifier.Worker{
		hostID: {
			ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateRunning,
			Command: "pi --session " + transcript, Cwd: t.TempDir(),
		},
	}}
	service := NewService(store, watcher, nil)
	if items, err := store.ThreadTimeline(threadID, 0); err != nil || len(items) != 0 {
		t.Fatalf("fixture already materialized timeline rows: %d err=%v", len(items), err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go service.RunHostTranscriptCapture(ctx)
	waitForTimelineAssistant(t, store, threadID, 1)

	// A later provider row must appear without any subscriber or manual call.
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply", "second captured reply")
	waitForTimelineAssistant(t, store, threadID, 2)

	// Repeated passes never duplicate a durable provider row.
	for range 3 {
		if err := service.captureHostTranscript(work.NewProviderConversationReader()); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range items {
		if item.Kind != timelineKindAssistantMessage {
			continue
		}
		if ids[item.ID] {
			t.Fatalf("duplicate captured row %q", item.ID)
		}
		ids[item.ID] = true
		if !strings.HasPrefix(item.ID, "capture-session") {
			t.Fatalf("captured row lost provider identity: %+v", item)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("captured assistant rows = %d, want 2", len(ids))
	}
}
