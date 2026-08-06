package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainAdmissionClearsPendingPathAcrossCWDLossAndProviderEcho(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-admission-p0"
	hostID := "brain-agent-host:@p0"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}

	hostHome := filepath.Join(root, "host-home")
	rolloutPath := filepath.Join(hostHome, ".codex", "sessions", "rollout-host.jsonl")
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeServerCodexRollout(t, rolloutPath, "provider-session-p0",
		"accepted user body for pending clear",
		"assistant reply after cwd vanished",
	)
	if err := store.SetHostProviderTranscript("provider-session-p0", rolloutPath, hostHome); err != nil {
		t.Fatal(err)
	}

	service := brain.NewService(store, nil, nil)
	srv := &Server{brain: service}

	receipt := "app-request-pending-1"
	body := "accepted user body for pending clear"
	scope := "brain-thread:" + threadID
	if err := service.AdmitHostUserInput(hostID, receipt, body, scope); err != nil {
		t.Fatal(err)
	}

	// Live agent conversation is unavailable because cwd moved to trash.
	live := work.CodexConversation{
		Available: false,
		Reason:    "transcript_not_found",
		Events:    nil,
	}
	got := srv.brainScopedConversation("brain-thread:"+threadID, live, time.Now().UTC())
	if !got.Available {
		t.Fatalf("scoped unavailable: %#v", got)
	}

	admissionID := brain.AdmissionTimelineItemID(receipt)
	ids := map[string]bool{}
	userCount := 0
	for _, event := range got.Events {
		ids[event.ID] = true
		if event.Kind == "user_message" {
			userCount++
			if event.Body != body {
				t.Fatalf("user body = %q", event.Body)
			}
		}
	}
	if !ids[admissionID] {
		t.Fatalf("missing admission id %q in %#v", admissionID, conversationEventIDs(got.Events))
	}
	if !idsContainAssistant(got.Events, "assistant reply after cwd vanished") {
		t.Fatalf("missing assistant from bound transcript: %#v", got.Events)
	}
	if userCount != 1 {
		t.Fatalf("provider echo duplicated user rows: count=%d events=%#v", userCount, got.Events)
	}

	// Empty/restart read of a fresh Server still returns both durable rows.
	reopened, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Server{brain: brain.NewService(reopened, nil, nil)}
	again := restarted.brainScopedConversation("brain-thread:"+threadID, work.CodexConversation{
		Available: false,
		Events:    nil,
	}, time.Now().UTC())
	if !again.Available || !idsContain(again.Events, admissionID) ||
		!idsContainAssistant(again.Events, "assistant reply after cwd vanished") {
		t.Fatalf("restart scoped = %#v", again.Events)
	}
}

func TestBrainAdmissionOverlaySuppressesEchoesOneToOneNotByBodySet(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-overlay-one-to-one"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	body := "identical overlay body"
	if _, err := store.AdmitUserMessage(threadID, "host", "receipt-overlay-a", body); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitUserMessage(threadID, "host", "receipt-overlay-b", body); err != nil {
		t.Fatal(err)
	}

	srv := &Server{brain: brain.NewService(store, nil, nil)}
	provider := work.CodexConversation{
		Available: true,
		SessionID: "provider-overlay",
		Events: []work.CodexConversationEvent{{
			ID:              "provider-overlay:1",
			Timestamp:       "2026-08-06T06:00:00Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: brain.AdmissionDigest(body),
		}, {
			ID:              "provider-overlay:2",
			Timestamp:       "2026-08-06T06:00:01Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: brain.AdmissionDigest(body),
		}, {
			ID:              "provider-overlay:3",
			Timestamp:       "2026-08-06T06:00:02Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: brain.AdmissionDigest(body),
		}, {
			ID:        "provider-overlay:4",
			Timestamp: "2026-08-06T06:00:03Z",
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      "ok",
		}},
	}
	got := srv.brainScopedConversation("brain-thread:"+threadID, provider, time.Now().UTC())
	if !got.Available {
		t.Fatalf("unavailable: %#v", got)
	}
	userIDs := []string{}
	for _, event := range got.Events {
		if event.Kind == "user_message" {
			userIDs = append(userIDs, event.ID)
		}
	}
	// Two admissions + one remaining same-body Terminal/provider input.
	if len(userIDs) != 3 {
		t.Fatalf("user ids = %#v events=%#v", userIDs, conversationEventIDs(got.Events))
	}
	want := map[string]bool{
		"receipt-overlay-a":  true,
		"receipt-overlay-b":  true,
		"provider-overlay:3": true,
	}
	for _, id := range userIDs {
		if !want[id] {
			t.Fatalf("unexpected user id %q in %#v", id, userIDs)
		}
	}
	if idsContain(got.Events, "provider-overlay:1") || idsContain(got.Events, "provider-overlay:2") {
		t.Fatalf("echoes leaked into overlay: %#v", conversationEventIDs(got.Events))
	}
}

func idsContain(events []work.CodexConversationEvent, id string) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}

func idsContainAssistant(events []work.CodexConversationEvent, body string) bool {
	for _, event := range events {
		if event.Kind == "assistant_message" && event.Body == body {
			return true
		}
	}
	return false
}

func writeServerCodexRollout(t *testing.T, path, sessionID, userBody, assistantBody string) {
	t.Helper()
	lines := []map[string]any{
		{
			"timestamp": "2026-08-06T04:55:00Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id":         sessionID,
				"cwd":        "/gone/cwd",
				"originator": "codex",
			},
		},
		{
			"timestamp": "2026-08-06T04:55:01Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": userBody,
			},
		},
		{
			"timestamp": "2026-08-06T04:55:02Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": assistantBody,
			},
		},
	}
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
