package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func TestAdmitUserMessageSurvivesEmptyHostAndDedupesProviderEcho(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_admit"
	hostID := "brain-host:@1"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript("provider-moved", "", ""); err != nil {
		t.Fatal(err)
	}

	service := NewService(store, nil, nil)
	body := "P0 admit durable user before provider echo"
	receipt := "request-admit-1"
	store.now = func() time.Time {
		return time.Date(2026, 8, 6, 4, 39, 59, 0, time.UTC)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, receipt, body, "brain-thread:"+threadID)
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	store.now = func() time.Time {
		return time.Date(2026, 8, 6, 4, 40, 0, 500000000, time.UTC)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}

	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "user_message" || items[0].Body != body {
		t.Fatalf("admission timeline = %#v", items)
	}
	wantID := AdmissionTimelineItemID(receipt)
	if items[0].ID != wantID || !items[0].BrainAdmission || items[0].AdmissionSHA256 != AdmissionDigest(body) {
		t.Fatalf("admission identity = %#v want id=%q brain_admission", items[0], wantID)
	}

	// Host cwd disappears / provider snapshot empty: admission remains.
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-moved",
		Events:    nil,
	}); err != nil {
		t.Fatal(err)
	}
	afterEmpty, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEmpty) != 1 || afterEmpty[0].ID != wantID {
		t.Fatalf("empty host wiped admission: %#v", afterEmpty)
	}

	providerEchoID := "provider-echo-user"
	assistantID := "provider-assistant-1"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-moved",
		Events: []work.CodexConversationEvent{{
			ID:              providerEchoID,
			Timestamp:       "2026-08-06T04:40:00Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:        assistantID,
			Timestamp: "2026-08-06T04:40:01Z",
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      "durable assistant from host transcript",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	finalItems, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalItems) != 2 {
		t.Fatalf("want admission+assistant only, got %#v", finalItems)
	}
	ids := map[string]string{}
	for _, item := range finalItems {
		ids[item.ID] = item.Kind
	}
	if ids[wantID] != "user_message" || ids[assistantID] != "assistant_message" {
		t.Fatalf("final timeline = %#v", finalItems)
	}

	// Restart-style empty read returns both durable rows.
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reopened.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted) != 2 {
		t.Fatalf("restart timeline = %#v", restarted)
	}
	restartIDs := map[string]string{}
	for _, item := range restarted {
		restartIDs[item.ID] = item.Body
	}
	if restartIDs[wantID] != body || restartIDs[assistantID] == "" {
		t.Fatalf("restart timeline = %#v", restarted)
	}
}

func TestAdmitUserMessageUsesScopedThreadNotCurrentGuess(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current := "brain_thread_current"
	target := "brain_thread_target"
	if err := store.SetChatState(ChatState{
		ThreadID:  current,
		ThreadIDs: []string{current, target},
	}); err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@scope"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	receipt := "scoped-request-id"
	body := "display body for scoped thread"
	prepared, created, err := service.PrepareHostUserInput(hostID, receipt, body, "brain-thread:"+target)
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	currentItems, err := store.ThreadTimeline(current, 0)
	if err != nil {
		t.Fatal(err)
	}
	targetItems, err := store.ThreadTimeline(target, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(currentItems) != 0 {
		t.Fatalf("current thread mutated: %#v", currentItems)
	}
	if len(targetItems) != 1 || targetItems[0].ID != receipt || targetItems[0].Body != body {
		t.Fatalf("target admission = %#v", targetItems)
	}
}

func TestHostBoundTranscriptSurvivesCWDDeletion(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_bound"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-host:@bound", "codex"); err != nil {
		t.Fatal(err)
	}

	rolloutDir := filepath.Join(root, "rollouts")
	if err := os.MkdirAll(rolloutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rolloutPath := filepath.Join(rolloutDir, "rollout-host-bound.jsonl")
	writeCodexRolloutFixture(t, rolloutPath, "host-session-bound", []map[string]any{
		{
			"timestamp": "2026-08-06T04:50:00Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "from bound host transcript",
			},
		},
		{
			"timestamp": "2026-08-06T04:50:01Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": "assistant from bound path",
			},
		},
	})

	if err := store.SetHostProviderTranscript("host-session-bound", rolloutPath, root); err != nil {
		t.Fatal(err)
	}

	// Simulate cwd deletion: workspace path still exists for Brain store, but
	// the bound transcript is the only ingestion authority.
	service := NewService(store, nil, nil)
	bound, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Available || bound.Path != rolloutPath {
		t.Fatalf("bound conversation = %#v", bound)
	}
	if err := service.MaterializeProviderConversation(threadID, bound); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("bound materialize = %#v", items)
	}
	foundUser, foundAssistant := false, false
	for _, item := range items {
		switch {
		case item.Kind == "user_message" && item.Body == "from bound host transcript":
			foundUser = true
		case item.Kind == "assistant_message" && item.Body == "assistant from bound path":
			foundAssistant = true
		}
	}
	if !foundUser || !foundAssistant {
		t.Fatalf("missing bound rows: %#v", items)
	}
}

func writeCodexRolloutFixture(t *testing.T, path, sessionID string, events []map[string]any) {
	t.Helper()
	lines := []map[string]any{{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":         sessionID,
			"cwd":        "/deleted/cwd",
			"originator": "codex",
		},
	}}
	lines = append(lines, events...)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}
