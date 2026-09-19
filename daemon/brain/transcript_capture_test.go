package brain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func captureFixture(t *testing.T) (*Store, *Service, *fakeWatcher) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "zen-worker-brain-capture:@1"
	if err := store.SetChatState(ChatState{ThreadID: "brain_thread_capture"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "pi"); err != nil {
		t.Fatal(err)
	}
	watcher := &fakeWatcher{sessions: map[string]*classifier.Worker{}}
	service := NewService(store, watcher, nil)
	return store, service, watcher
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
		assistant := assistantItems(items)
		last = assistant
		if len(assistant) == want {
			return assistant
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("assistant rows = %d, want %d (%+v)", len(last), want, last)
	return nil
}

func assistantItems(items []TimelineItem) []TimelineItem {
	out := make([]TimelineItem, 0, len(items))
	for _, item := range items {
		if item.Kind == timelineKindAssistantMessage {
			out = append(out, item)
		}
	}
	return out
}

// TestHostTranscriptCaptureMaterializesWithoutSubscriber is the regression for
// Telegram Brain replies that never appeared while no App WebSocket
// subscription was open. Nothing in this test opens a subscription: the
// daemon-owned capture pass must append provider assistant rows to the durable
// canonical timeline on its own, exactly once per provider event, and an
// unchanged source must not repeat store work.
func TestHostTranscriptCaptureMaterializesWithoutSubscriber(t *testing.T) {
	store, service, watcher := captureFixture(t)
	const (
		threadID = "brain_thread_capture"
		hostID   = "zen-worker-brain-capture:@1"
	)
	transcript := filepath.Join(t.TempDir(), "host-session.jsonl")
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply")
	if err := store.SetHostProviderTranscript("capture-session", transcript, ""); err != nil {
		t.Fatal(err)
	}
	watcher.sessions[hostID] = &classifier.Worker{
		ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateRunning,
		Command: "pi --session " + transcript, Cwd: t.TempDir(),
	}
	if items, err := store.ThreadTimeline(threadID, 0); err != nil || len(items) != 0 {
		t.Fatalf("fixture already materialized timeline rows: %d err=%v", len(items), err)
	}

	reader := work.NewProviderConversationReader()
	state := &hostTranscriptCaptureState{}
	captured, err := service.captureHostTranscript(reader, state)
	if err != nil || !captured {
		t.Fatalf("first capture: captured=%v err=%v", captured, err)
	}
	waitForTimelineAssistant(t, store, threadID, 1)

	// An unchanged source whose binding still matches the checkpoint performs
	// no store work at all.
	captured, err = service.captureHostTranscript(reader, state)
	if err != nil || captured {
		t.Fatalf("unchanged source took store work: captured=%v err=%v", captured, err)
	}

	// A later provider row must appear without any subscriber or manual store
	// call.
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply", "second captured reply")
	captured, err = service.captureHostTranscript(reader, state)
	if err != nil || !captured {
		t.Fatalf("changed capture: captured=%v err=%v", captured, err)
	}
	waitForTimelineAssistant(t, store, threadID, 2)

	// Repeated unchanged passes never duplicate a durable provider row.
	for range 3 {
		captured, err = service.captureHostTranscript(reader, state)
		if err != nil || captured {
			t.Fatalf("repeat pass: captured=%v err=%v", captured, err)
		}
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range assistantItems(items) {
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

	// The production loop still wires the same pass: a new provider row lands
	// without an explicit capture call.
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply", "second captured reply", "third captured reply")
	captureCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go service.RunHostTranscriptCapture(captureCtx)
	waitForTimelineAssistant(t, store, threadID, 3)
}

// switchingWatcher simulates NewChat/Host replacement racing one capture read:
// the first worker lookup switches the store's thread and host binding, exactly
// between the capture pass's thread read and its store append.
type switchingWatcher struct {
	*fakeWatcher
	once     sync.Once
	switchFn func()
}

func (w *switchingWatcher) GetWorker(id string) *classifier.Worker {
	w.once.Do(w.switchFn)
	return w.fakeWatcher.GetWorker(id)
}

// TestHostTranscriptCaptureDiscardsReadAcrossThreadAndHostSwitch proves a
// switch that races the read can never write the new Host output into the old
// thread, and that the pass re-reads the current binding and retries instead
// of advancing its checkpoint for the stale binding.
func TestHostTranscriptCaptureDiscardsReadAcrossThreadAndHostSwitch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		oldThread = "brain_thread_before_switch"
		newThread = "brain_thread_after_switch"
		oldHost   = "zen-worker-brain-switch-old:@1"
		newHost   = "zen-worker-brain-switch-new:@1"
	)
	oldTranscript := filepath.Join(t.TempDir(), "old.jsonl")
	newTranscript := filepath.Join(t.TempDir(), "new.jsonl")
	writePiTranscriptFixture(t, oldTranscript, "switch-session-old", "old question", "old reply")
	writePiTranscriptFixture(t, newTranscript, "switch-session-new", "new question", "new reply")
	if err := store.SetChatState(ChatState{ThreadID: oldThread}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(oldHost, "pi"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript("switch-session-old", oldTranscript, ""); err != nil {
		t.Fatal(err)
	}
	base := &fakeWatcher{sessions: map[string]*classifier.Worker{
		oldHost: {ID: oldHost, Name: "Brain", Hidden: true, State: classifier.StateRunning, Command: "pi --session " + oldTranscript, Cwd: t.TempDir()},
		newHost: {ID: newHost, Name: "Brain", Hidden: true, State: classifier.StateRunning, Command: "pi --session " + newTranscript, Cwd: t.TempDir()},
	}}
	watcher := &switchingWatcher{fakeWatcher: base, switchFn: func() {
		if err := store.SetChatState(ChatState{ThreadID: newThread}); err != nil {
			t.Error(err)
		}
		if err := store.SetHostSession(newHost, "pi"); err != nil {
			t.Error(err)
		}
		if err := store.SetHostProviderTranscript("switch-session-new", newTranscript, ""); err != nil {
			t.Error(err)
		}
	}}
	service := NewService(store, watcher, nil)
	state := &hostTranscriptCaptureState{}
	captured, err := service.captureHostTranscript(work.NewProviderConversationReader(), state)
	if err != nil || !captured {
		t.Fatalf("switch retry did not capture: captured=%v err=%v", captured, err)
	}
	if state.binding.ThreadID != newThread {
		t.Fatalf("checkpoint thread = %q, want %q", state.binding.ThreadID, newThread)
	}
	oldItems, err := store.ThreadTimeline(oldThread, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(assistantItems(oldItems)) != 0 {
		t.Fatalf("stale read wrote into the old thread: %+v", oldItems)
	}
	newItems, err := store.ThreadTimeline(newThread, 0)
	if err != nil {
		t.Fatal(err)
	}
	assistant := assistantItems(newItems)
	if len(assistant) != 1 || !strings.Contains(assistant[0].Body, "new reply") {
		t.Fatalf("new thread capture = %+v", assistant)
	}
}

// TestHostTranscriptCaptureWriteFailureKeepsCheckpoint proves a failed store
// append never advances the unchanged-source checkpoint: the same source is
// retried after the write path recovers instead of being skipped.
func TestHostTranscriptCaptureWriteFailureKeepsCheckpoint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure injection requires a non-root test user")
	}
	store, service, watcher := captureFixture(t)
	const (
		threadID = "brain_thread_capture"
		hostID   = "zen-worker-brain-capture:@1"
	)
	transcript := filepath.Join(t.TempDir(), "host-session.jsonl")
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply")
	if err := store.SetHostProviderTranscript("capture-session", transcript, ""); err != nil {
		t.Fatal(err)
	}
	watcher.sessions[hostID] = &classifier.Worker{
		ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateRunning,
		Command: "pi --session " + transcript, Cwd: t.TempDir(),
	}
	reader := work.NewProviderConversationReader()
	state := &hostTranscriptCaptureState{}
	captured, err := service.captureHostTranscript(reader, state)
	if err != nil || !captured {
		t.Fatalf("first capture: captured=%v err=%v", captured, err)
	}
	appliedRevision := state.revision

	messages := store.messagesPath()
	if err := os.Chmod(messages, 0o400); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(messages, 0o600) }()
	writePiTranscriptFixture(t, transcript, "capture-session", "public fixture question", "first captured reply", "second captured reply")
	if _, err := service.captureHostTranscript(reader, state); err == nil {
		t.Fatal("materialize into a read-only timeline unexpectedly succeeded")
	}
	if state.revision != appliedRevision {
		t.Fatalf("checkpoint advanced after a failed write: %q -> %q", appliedRevision, state.revision)
	}
	if err := os.Chmod(messages, 0o600); err != nil {
		t.Fatal(err)
	}
	captured, err = service.captureHostTranscript(reader, state)
	if err != nil || !captured {
		t.Fatalf("retry after restored write: captured=%v err=%v", captured, err)
	}
	waitForTimelineAssistant(t, store, threadID, 2)
}
