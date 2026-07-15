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
	if got.Turn == nil || got.Turn.Status != CodexConversationTurnRunning || got.Turn.StartedAt == "" {
		t.Fatalf("turn = %#v, want running provider turn with start time", got.Turn)
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
	if got.Active == nil || *got.Active {
		t.Fatalf("active = %#v, want false after turn_ended", got.Active)
	}
	if got.Turn == nil || got.Turn.Status != CodexConversationTurnCompleted || got.Turn.StartedAt == "" || got.Turn.SettledAt == "" {
		t.Fatalf("turn = %#v, want completed lifecycle", got.Turn)
	}
	if len(got.Events) != 3 {
		t.Fatalf("events len = %d, want 3 (user/assistant/turn_ended): %#v", len(got.Events), got.Events)
	}
	if got.Events[2].Kind != "turn_ended" || got.Events[2].Status != "success" {
		t.Fatalf("turn_ended event = %#v", got.Events[2])
	}
}

func TestParseCursorConversation_IgnoresMidTurnAssistantForLifecycle(t *testing.T) {
	activePath := filepath.Join(t.TempDir(), "active.jsonl")
	writeJSONL(t, activePath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "keep going"}},
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
	if got.Turn == nil || got.Turn.Status != CodexConversationTurnRunning || got.Active == nil || !*got.Active {
		t.Fatalf("expected running turn while tools run without turn_ended: %#v", got)
	}

	endedPath := filepath.Join(t.TempDir(), "ended.jsonl")
	writeJSONL(t, endedPath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "done?"}},
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
	if got.Turn == nil || got.Turn.Status != CodexConversationTurnCompleted || got.Active == nil || *got.Active {
		t.Fatalf("expected completed turn after turn_ended: %#v", got)
	}
}

func TestParseCursorConversation_AppendedRowsKeepStableEventIDs(t *testing.T) {
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

	writeJSONL(t, path, user)
	first, err := loadCachedCursorConversation(path)
	if err != nil {
		t.Fatalf("first cached load: %v", err)
	}
	if len(first.Events) != 1 || first.Turn == nil || first.Turn.Status != CodexConversationTurnRunning || first.Turn.StartedAt == "" {
		t.Fatalf("first snapshot = %#v, want running user turn", first)
	}
	userID := first.Events[0].ID
	turnID := first.Turn.ID
	startedAt := first.Turn.StartedAt

	writeJSONL(t, path, user, assistant)
	second, err := loadCachedCursorConversation(path)
	if err != nil {
		t.Fatalf("second cached load: %v", err)
	}
	if len(second.Events) != 2 || second.Events[0].ID != userID || second.Turn == nil || second.Turn.Status != CodexConversationTurnRunning {
		t.Fatalf("second snapshot changed row identity or ended early: %#v", second)
	}
	if second.Turn.ID != turnID || second.Turn.StartedAt != startedAt {
		t.Fatalf("turn identity/start changed: %#v -> %#v", first.Turn, second.Turn)
	}
	assistantID := second.Events[1].ID
	if second.Events[1].Kind != "assistant_message" || second.Events[1].Partial {
		t.Fatalf("Cursor exposes a complete appended row, not a synthetic partial: %#v", second.Events[1])
	}

	writeJSONL(t, path, user, assistant, ended)
	final, err := loadCachedCursorConversation(path)
	if err != nil {
		t.Fatalf("final cached load: %v", err)
	}
	if len(final.Events) != 3 || final.Events[0].ID != userID || final.Events[1].ID != assistantID {
		t.Fatalf("final snapshot changed existing row identities: %#v", final.Events)
	}
	if final.Turn == nil || final.Turn.ID != turnID || final.Turn.StartedAt != startedAt || final.Turn.Status != CodexConversationTurnCompleted || final.Turn.SettledAt == "" {
		t.Fatalf("turn_ended must settle the same durable turn: %#v", final.Turn)
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
	if got.Active == nil || *got.Active {
		t.Fatalf("active = %#v, want false for completed terminal snapshot content", got.Active)
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

func TestShouldUseTerminalSnapshotConversationFallback_StructuredProvidersNever(t *testing.T) {
	reasons := []string{
		"not_structured_agent",
		"transcript_not_found",
		"transcript_malformed",
		"missing_cwd",
	}
	providers := []string{
		"cursor-agent --force",
		"claude",
		"codex",
		"grok",
	}
	for _, command := range providers {
		for _, reason := range reasons {
			if ShouldUseTerminalSnapshotConversationFallback(
				classifier.Agent{Command: command},
				CodexConversation{Available: false, Reason: reason, Events: []CodexConversationEvent{}},
			) {
				t.Fatalf("%s + %s must not adapt terminal snapshots into Chat", command, reason)
			}
		}
	}
}

func TestStructuredProviderMissingTranscriptStaysEmptyReadyWithoutPaneDump(t *testing.T) {
	now := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	agent := classifier.Agent{
		ID:      "claude-session:@1",
		Name:    "claude",
		Command: "claude",
		Cwd:     "/repo/zen",
		State:   classifier.StateRunning,
	}
	conversation := CodexConversation{
		Available: false,
		Reason:    "transcript_not_found",
		Events:    []CodexConversationEvent{},
	}
	if ShouldUseTerminalSnapshotConversationFallback(agent, conversation) {
		t.Fatal("structured provider must not fall back to terminal snapshot chat events")
	}

	// Even if a helper still builds a snapshot from arbitrary pane text, Chat
	// subscription must not select that path for structured providers.
	snapshot := TerminalSnapshotConversationForAgent(agent, "████ arbitrary startup banner ████\nClaude Code\nThinking\nDone\n", now)
	if len(snapshot.Events) == 0 {
		t.Fatal("helper may still capture pane text for non-Chat uses")
	}
	if ShouldUseTerminalSnapshotConversationFallback(agent, conversation) {
		t.Fatal("Chat must not surface that capture as conversation events")
	}
}
