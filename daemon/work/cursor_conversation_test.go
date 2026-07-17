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
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning || got.Activity.StartedAt == "" {
		t.Fatalf("turn = %#v, want running provider turn with start time", got.Activity)
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

func TestProviderConversationReaderCursorFindsProjectTranscript(t *testing.T) {
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
		map[string]any{
			"type":      "turn_ended",
			"status":    "success",
			"timestamp": "2026-07-04T07:30:00Z",
		},
	)
	now := time.Now().UTC()
	if err := os.Chtimes(transcriptPath, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	t.Setenv("HOME", home)

	got, err := NewProviderConversationReader().Load(classifier.Agent{
		ID:        "cursor-window:@1",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("ProviderConversationReader.Load: %v", err)
	}
	if !got.Available || got.Source != cursorConversationSource {
		t.Fatalf("conversation = %#v", got)
	}
	if got.Path != transcriptPath || got.SessionID != sessionID || got.CWD != cwd {
		t.Fatalf("metadata = %#v", got)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted || got.Activity.StartedAt == "" || got.Activity.SettledAt == "" {
		t.Fatalf("turn = %#v, want completed lifecycle", got.Activity)
	}
	if len(got.Events) != 3 {
		t.Fatalf("events len = %d, want 3 (user/assistant/turn_ended): %#v", len(got.Events), got.Events)
	}
	if got.Events[2].Kind != "turn_ended" || got.Events[2].Status != "success" {
		t.Fatalf("turn_ended event = %#v", got.Events[2])
	}
}

func TestParseCursorConversation_AssistantRowsDoNotSettleActivity(t *testing.T) {
	activePath := filepath.Join(t.TempDir(), "active.jsonl")
	writeJSONL(t, activePath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "<timestamp>2026-07-04T07:29:00Z</timestamp><user_query>keep going</user_query>"}},
			},
		},
		map[string]any{
			"role": "assistant",
			"message": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Working"},
					{"type": "tool_use", "name": "Shell", "input": map[string]any{"command": "ls"}},
				},
			},
		},
	)
	got, err := parseCursorConversation(activePath)
	if err != nil {
		t.Fatalf("parseCursorConversation: %v", err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("expected running turn while tools run without turn_ended: %#v", got)
	}

	endedPath := filepath.Join(t.TempDir(), "ended.jsonl")
	writeJSONL(t, endedPath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "<timestamp>2026-07-04T07:29:00Z</timestamp><user_query>done?</user_query>"}},
			},
		},
		map[string]any{
			"role": "assistant",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "Yes"}},
			},
		},
		map[string]any{"type": "turn_ended", "status": "success", "timestamp": "2026-07-04T07:30:00Z"},
	)
	got, err = parseCursorConversation(endedPath)
	if err != nil {
		t.Fatalf("parseCursorConversation: %v", err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("expected completed turn after turn_ended: %#v", got)
	}
}

func TestProviderConversationReaderCursorAppendedRowsKeepStableEventIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor-progressive.jsonl")
	user := map[string]any{
		"role": "user",
		"message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "<timestamp>2026-07-04T07:29:00Z</timestamp><user_query>show native rows</user_query>"}},
		},
	}
	assistant := map[string]any{
		"role": "assistant",
		"message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "First structured assistant row."}},
		},
	}
	ended := map[string]any{"type": "turn_ended", "status": "success", "timestamp": "2026-07-04T07:30:00Z"}
	reader := NewProviderConversationReader()

	writeJSONL(t, path, user)
	first, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatalf("first cached load: %v", err)
	}
	if len(first.Events) != 1 || first.Activity == nil || first.Activity.Status != ProviderActivityRunning || first.Activity.StartedAt == "" {
		t.Fatalf("first snapshot = %#v, want running user turn", first)
	}
	userID := first.Events[0].ID
	turnID := first.Activity.ID
	startedAt := first.Activity.StartedAt

	writeJSONL(t, path, user, assistant)
	second, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatalf("second cached load: %v", err)
	}
	if len(second.Events) != 2 || second.Events[0].ID != userID || second.Activity == nil || second.Activity.Status != ProviderActivityRunning {
		t.Fatalf("second snapshot changed row identity or ended early: %#v", second)
	}
	if second.Activity.ID != turnID || second.Activity.StartedAt != startedAt {
		t.Fatalf("turn identity/start changed: %#v -> %#v", first.Activity, second.Activity)
	}
	assistantID := second.Events[1].ID
	if second.Events[1].Kind != "assistant_message" || second.Events[1].Partial {
		t.Fatalf("Cursor exposes a complete appended row, not a synthetic partial: %#v", second.Events[1])
	}

	writeJSONL(t, path, user, assistant, ended)
	final, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatalf("final cached load: %v", err)
	}
	if len(final.Events) != 3 || final.Events[0].ID != userID || final.Events[1].ID != assistantID {
		t.Fatalf("final snapshot changed existing row identities: %#v", final.Events)
	}
	if final.Activity == nil || final.Activity.ID != turnID || final.Activity.StartedAt != startedAt || final.Activity.Status != ProviderActivityCompleted || final.Activity.SettledAt == "" {
		t.Fatalf("turn_ended must settle the same durable turn: %#v", final.Activity)
	}
}
