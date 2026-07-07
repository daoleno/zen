package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestParseCursorConversation_BuildsMarkdownMessagesAndTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor-session.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": "<timestamp>Saturday, Jul 4, 2026, 3:29 PM (UTC+8)</timestamp>\n<user_query>\n请修复 **Markdown** 渲染\n</user_query>",
					},
				},
			},
		},
		map[string]any{
			"role": "assistant",
			"message": map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": "这里是 **Markdown**：\n\n[REDACTED]\n\n- item one\n- item two",
					},
					{
						"type": "tool_use",
						"name": "Shell",
						"input": map[string]any{
							"command":     "go test ./work",
							"description": "Run work tests",
						},
					},
				},
			},
		},
	)

	got, err := parseCursorConversation(path)
	if err != nil {
		t.Fatalf("parseCursorConversation: %v", err)
	}
	if !got.Available || got.Source != cursorConversationSource || got.SessionID != "cursor-session" {
		t.Fatalf("conversation = %#v", got)
	}
	if len(got.Events) != 3 {
		t.Fatalf("events len = %d, want 3: %#v", len(got.Events), got.Events)
	}
	assertEvent(t, got.Events[0], "user_message", "user", "", "请修复 **Markdown** 渲染")
	if strings.Contains(got.Events[0].Body, "<timestamp>") || strings.Contains(got.Events[0].Body, "<user_query>") {
		t.Fatalf("cursor wrapper leaked into user message: %#v", got.Events[0])
	}
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "**Markdown**")
	if strings.Contains(got.Events[1].Body, "[REDACTED]") {
		t.Fatalf("redacted placeholder leaked into assistant body: %#v", got.Events[1])
	}
	if got.Events[2].Kind != "command" || got.Events[2].Command != "go test ./work" || got.Events[2].Body != "Run work tests" {
		t.Fatalf("command event = %#v", got.Events[2])
	}
	if got.Events[0].Seq >= got.Events[1].Seq || got.Events[1].Seq >= got.Events[2].Seq {
		t.Fatalf("seq order = %d, %d, %d", got.Events[0].Seq, got.Events[1].Seq, got.Events[2].Seq)
	}
}

func TestLoadCursorConversationForAgent_FindsProjectTranscript(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/workspace/pacagent"
	sessionID := "f6df18eb-9674-4298-a25c-16df37c937fc"
	transcriptPath := filepath.Join(
		home,
		cursorProjectDirPrefix,
		encodeCursorProjectDir(cwd),
		cursorTranscriptDir,
		sessionID,
		sessionID+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeJSONL(t, transcriptPath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": "<timestamp>Saturday, Jul 4, 2026, 3:29 PM (UTC+8)</timestamp>\n<user_query>\ncommit and push\n</user_query>",
					},
				},
			},
		},
		map[string]any{
			"role": "assistant",
			"message": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Committed and pushed to `origin/main`."},
				},
			},
		},
	)
	now := time.Now().UTC()
	if err := os.Chtimes(transcriptPath, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	t.Setenv("HOME", home)

	got, err := loadCursorConversationForAgent(classifier.Agent{
		ID:        "cursor-window:@1",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}, now)
	if err != nil {
		t.Fatalf("loadCursorConversationForAgent: %v", err)
	}
	if !got.Available || got.Source != cursorConversationSource {
		t.Fatalf("conversation = %#v", got)
	}
	if got.Path != transcriptPath || got.SessionID != sessionID || got.CWD != cwd {
		t.Fatalf("metadata = %#v", got)
	}
	if got.Active == nil || !*got.Active {
		t.Fatalf("active = %#v, want true", got.Active)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}
}

func TestTerminalSnapshotConversationForAgent_BuildsTerminalStatusFallback(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	got := TerminalSnapshotConversationForAgent(classifier.Agent{
		ID:    "cursor-session:@1",
		Name:  "cursor-agent",
		Cwd:   "/repo",
		State: classifier.StateRunning,
	}, "\n\nThinking\nDone\n", now)

	if !got.Available || got.Reason != "" {
		t.Fatalf("availability = (%v, %q), want available", got.Available, got.Reason)
	}
	if got.Source != "terminal_snapshot" || got.SessionID != "cursor-session:@1" || got.CWD != "/repo" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.Updated == nil || !got.Updated.Equal(now) {
		t.Fatalf("updated = %#v, want %s", got.Updated, now)
	}
	if got.Active == nil || !*got.Active {
		t.Fatalf("active = %#v, want true", got.Active)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	event := got.Events[0]
	if event.Kind != "status" || event.Title != "Terminal snapshot" || event.Status != "done" || !strings.Contains(event.Body, "Thinking\nDone") {
		t.Fatalf("event = %#v, want terminal status snapshot", event)
	}
	if event.Source != "terminal_snapshot" {
		t.Fatalf("event source = %q, want terminal_snapshot", event.Source)
	}
}

func TestTerminalSnapshotConversationForAgent_EmptySnapshotUnavailable(t *testing.T) {
	got := TerminalSnapshotConversationForAgent(classifier.Agent{
		ID:    "cursor-session:@1",
		State: classifier.StateBlocked,
	}, " \n\t\n", time.Unix(0, 0).UTC())

	if got.Available || got.Reason != "terminal_snapshot_empty" {
		t.Fatalf("availability = (%v, %q), want empty snapshot unavailable", got.Available, got.Reason)
	}
	if got.Active == nil || *got.Active {
		t.Fatalf("active = %#v, want false", got.Active)
	}
	if len(got.Events) != 0 {
		t.Fatalf("events = %#v, want none", got.Events)
	}
}

func TestShouldUseTerminalSnapshotConversationFallback_OnlyCursorNotStructured(t *testing.T) {
	notStructured := CodexConversation{
		Available: false,
		Reason:    "not_structured_agent",
		Events:    []CodexConversationEvent{},
	}
	if !ShouldUseTerminalSnapshotConversationFallback(classifier.Agent{
		Command: "cursor-agent --force",
	}, notStructured) {
		t.Fatal("cursor not_structured_agent should use terminal snapshot fallback")
	}
	if ShouldUseTerminalSnapshotConversationFallback(classifier.Agent{
		Command: "codex",
	}, notStructured) {
		t.Fatal("codex not_structured_agent should not use cursor terminal fallback")
	}
	if ShouldUseTerminalSnapshotConversationFallback(classifier.Agent{
		Command: "cursor-agent --force",
	}, CodexConversation{Available: false, Reason: "transcript_not_found"}) {
		t.Fatal("cursor transcript_not_found should keep native sync semantics")
	}
}
