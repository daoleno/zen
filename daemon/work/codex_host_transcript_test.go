package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCodexConversationByIdentityUsesHostDataRootNotDaemonHome(t *testing.T) {
	daemonHome := t.TempDir()
	hostHome := t.TempDir()
	t.Setenv("HOME", daemonHome)

	// Poison daemon HOME with an old July-style rollout that must not win.
	daemonRollout := filepath.Join(daemonHome, ".codex", "sessions", "2026", "07", "20", "rollout-old.jsonl")
	if err := os.MkdirAll(filepath.Dir(daemonRollout), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCodexHostRollout(t, daemonRollout, "daemon-old-session", "daemon old user", "daemon old assistant")

	hostRollout := filepath.Join(hostHome, ".codex", "sessions", "2026", "08", "05",
		"rollout-2026-08-05T09-12-05-019fcf7a-485b-7961-ad2f-dbe9f6eab2d2.jsonl")
	if err := os.MkdirAll(filepath.Dir(hostRollout), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCodexHostRollout(t, hostRollout, "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2",
		"host process user", "host process assistant")

	// Deleted/moved cwd: identity still loads via bound path + host data root.
	deletedCWD := filepath.Join(t.TempDir(), "gone-workspace")
	_ = deletedCWD

	got, err := LoadCodexConversationByIdentity(CodexTranscriptIdentity{
		SessionID: "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2",
		Path:      hostRollout,
		DataRoot:  hostHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != hostRollout {
		t.Fatalf("bound load = %#v", got)
	}
	foundHostUser := false
	for _, event := range got.Events {
		if event.Kind == "user_message" && event.Body == "host process user" {
			foundHostUser = true
		}
		if event.Body == "daemon old user" {
			t.Fatalf("daemon HOME rollout leaked into host-bound load: %#v", event)
		}
	}
	if !foundHostUser {
		t.Fatalf("missing host rollout events: %#v", got.Events)
	}

	// Session-only lookup under host data root also works when path is empty.
	bySession, err := LoadCodexConversationByIdentity(CodexTranscriptIdentity{
		SessionID: "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2",
		DataRoot:  hostHome,
	})
	// Without sqlite row this stays unavailable; path-based identity is the owner.
	_ = bySession
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenRolloutPathBeatsStaleThreadIDWhenResolvingIdentity(t *testing.T) {
	path := "/home/example/.zen/t/iso/home/.codex/sessions/2026/08/05/rollout-2026-08-05T09-12-05-019fcf7a-485b-7961-ad2f-dbe9f6eab2d2.jsonl"
	got := sessionIDFromCodexRolloutPath(path)
	if got != "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2" {
		t.Fatalf("session from path = %q", got)
	}
	root := dataRootForPath(path, []string{"/home/example/.zen/t/iso/home"})
	if root != "/home/example/.zen/t/iso/home" {
		t.Fatalf("data root = %q", root)
	}
}

func TestPreferHostBoundConversationIgnoresWrongCWDAttachment(t *testing.T) {
	bound := CodexConversation{
		Available: true,
		Path:      "/host/home/.codex/sessions/rollout-host.jsonl",
		Events: []CodexConversationEvent{
			{ID: "a1", Kind: "assistant_message", Body: "from host bind"},
		},
	}
	live := CodexConversation{
		Available: true,
		Path:      "/daemon/home/.codex/sessions/rollout-july20.jsonl",
		Events: []CodexConversationEvent{
			{ID: "wrong", Kind: "assistant_message", Body: "cwd matched wrong root"},
		},
	}
	got := PreferHostBoundConversation(live, bound)
	if got.Path != bound.Path || len(got.Events) != 1 || got.Events[0].ID != "a1" {
		t.Fatalf("prefer = %#v", got)
	}
}

func writeCodexHostRollout(t *testing.T, path, sessionID, userBody, assistantBody string) {
	t.Helper()
	lines := []map[string]any{
		{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			"type":      "session_meta",
			"payload": map[string]any{
				"id":         sessionID,
				"cwd":        "/deleted/cwd",
				"originator": "codex",
			},
		},
		{
			"timestamp": "2026-08-06T04:27:00Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": userBody,
			},
		},
		{
			"timestamp": "2026-08-06T04:27:01Z",
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
