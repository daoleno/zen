package work

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestMatchGrokSessionToAgentStart_UsesNearestCreatedSession(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "newer-other-window",
			CreatedAt: base.Add(90 * time.Second),
			Updated:   base.Add(5 * time.Minute),
		},
		{
			ID:        "this-window",
			CreatedAt: base.Add(3 * time.Second),
			Updated:   base.Add(2 * time.Minute),
		},
		{
			ID:        "old-window",
			CreatedAt: base.Add(-10 * time.Minute),
			Updated:   base.Add(6 * time.Minute),
		},
	}

	got, ok := matchGrokSessionToAgentStart(candidates, base)
	if !ok {
		t.Fatal("expected a grok session match")
	}
	if got.ID != "this-window" {
		t.Fatalf("matched %q, want this-window", got.ID)
	}
}

func TestMatchGrokSessionToAgentStart_DoesNotFallBackToOldSession(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "old-window",
			CreatedAt: base.Add(-30 * time.Second),
			Updated:   base.Add(5 * time.Minute),
		},
	}

	if got, ok := matchGrokSessionToAgentStart(candidates, base); ok {
		t.Fatalf("matched %#v, want no match", got)
	}
}

func TestMatchGrokSessionToActiveSession_UsesSessionUpdatedAfterStart(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "old-ended",
			CreatedAt: base.Add(-30 * time.Minute),
			Updated:   base.Add(-10 * time.Minute),
		},
		{
			ID:        "active-private",
			CreatedAt: base.Add(-30 * time.Second),
			Updated:   base.Add(12 * time.Minute),
		},
	}

	got, ok := matchGrokSessionToActiveSession(candidates, base)
	if !ok {
		t.Fatal("expected active grok session match")
	}
	if got.ID != "active-private" {
		t.Fatalf("matched %q, want active-private", got.ID)
	}
}

func TestFindGrokSession_DoesNotReturnStaleSessionForNewAgent(t *testing.T) {
	homeRoot := t.TempDir()
	home := filepath.Join(homeRoot, "home")
	cwd := "/tmp/zen-grok-fixture"
	sessionID := "stale-grok-session"
	sessionDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeGrokSummary(t, filepath.Join(sessionDir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"created_at": time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano),
	})
	writeJSONL(t, filepath.Join(sessionDir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "old"},
	)

	t.Setenv("HOME", home)
	agentStart := time.Now().UTC()
	got, ok, err := findGrokSession(classifier.Agent{
		Command:   "grok --no-alt-screen --permission-mode bypassPermissions",
		Cwd:       cwd,
		StartedAt: agentStart,
	}, time.Now())
	if err != nil {
		t.Fatalf("findGrokSession: %v", err)
	}
	if ok {
		t.Fatalf("matched stale grok session %#v, want no match for fresh agent", got)
	}
}

func TestFindGrokSession_ResumeCommandMatchesExplicitSessionID(t *testing.T) {
	homeRoot := t.TempDir()
	home := filepath.Join(homeRoot, "home")
	cwd := "/home/daoleno/workspace/pacagent"
	sessionID := "019f2826-12b8-7cc3-a094-a57522b559e6"
	sessionDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	now := time.Now().UTC()
	writeGrokSummary(t, filepath.Join(sessionDir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"updated_at": now.Format(time.RFC3339Nano),
		"created_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
	})

	t.Setenv("HOME", home)
	got, ok, err := findGrokSession(classifier.Agent{
		Command:   "grok --resume " + sessionID,
		Cwd:       cwd,
		StartedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("findGrokSession: %v", err)
	}
	if !ok {
		t.Fatal("expected resume command to match explicit grok session id")
	}
	if got.ID != sessionID || got.Dir != sessionDir {
		t.Fatalf("matched session = (%q, %q), want (%q, %q)", got.ID, got.Dir, sessionID, sessionDir)
	}
}

func TestEncodeGrokSessionCWD(t *testing.T) {
	got := encodeGrokSessionCWD("/home/daoleno/workspace/zen")
	want := "%2Fhome%2Fdaoleno%2Fworkspace%2Fzen"
	if got != want {
		t.Fatalf("encodeGrokSessionCWD = %q, want %q", got, want)
	}
}

func TestAgentToolNameRecognizesGrok(t *testing.T) {
	if got := agentToolName("grok --no-alt-screen --permission-mode bypassPermissions", ""); got != "grok" {
		t.Fatalf("agentToolName = %q, want grok", got)
	}
}

func TestParseGrokConversation_AssistantToolCallsUseUniqueEventIDsBeforeChatTrim(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-tools-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type":    "assistant",
			"content": "Running lookups.",
			"tool_calls": []map[string]any{
				{"id": "call-a", "name": "Grep", "arguments": `{"pattern":"foo"}`},
				{"id": "call-b", "name": "Glob", "arguments": `{"glob_pattern":"**/*.go"}`},
			},
		},
		map[string]any{
			"type":         "tool_result",
			"tool_call_id": "call-a",
			"content":      "found 1",
		},
		map[string]any{
			"type":    "assistant",
			"content": "Done checking.",
		},
	)

	builder := newGrokConversationBuilder(filepath.Base(dir))
	if err := consumeGrokJSONL(filepath.Join(dir, grokChatHistoryFile), builder.consumeChatHistoryLine); err != nil {
		t.Fatalf("consumeGrokJSONL: %v", err)
	}
	seen := map[string]int{}
	for _, event := range builder.events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
}

func TestParseGrokConversation_StreamsNativeChunksAndFinalizesSameEvent(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-stream-1"
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "stream this"},
	)
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	writeJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "old-prompt", "user_message_chunk", map[string]any{"type": "text", "text": "old prompt"}),
		grokUpdateFixture(sessionID, "old-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "old transient output"}),
		grokUpdateFixture(sessionID, "old-prompt", "turn_completed", nil),
		grokUpdateFixture(sessionID, "live-prompt", "user_message_chunk", map[string]any{"type": "text", "text": "stream this"}),
		grokUpdateFixture(sessionID, "live-prompt", "agent_thought_chunk", map[string]any{"type": "text", "text": "Checking **Markdown"}),
		grokUpdateFixture(sessionID, "live-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "Hello"}),
	)

	first, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	firstAssistant := findGrokEvent(t, first.Events, "assistant_message", "Hello")
	if !firstAssistant.Partial || firstAssistant.Status != "running" {
		t.Fatalf("first assistant = %#v", firstAssistant)
	}
	if first.Active == nil || !*first.Active {
		t.Fatalf("first active = %#v", first.Active)
	}
	if eventBodyContains(first.Events, "old transient output") {
		t.Fatalf("completed update history leaked into active tail: %#v", first.Events)
	}
	thought := findGrokEvent(t, first.Events, "commentary", "Checking")
	if !thought.Partial || thought.Status != "running" {
		t.Fatalf("thought = %#v", thought)
	}

	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "live-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": " "}),
		grokUpdateFixture(sessionID, "live-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "world"}),
		grokUpdateFixture(sessionID, "live-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "!"}),
		grokUpdateFixture(sessionID, "live-prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "!"}),
	)
	second, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	secondAssistant := findGrokEvent(t, second.Events, "assistant_message", "Hello world!!")
	if secondAssistant.ID != firstAssistant.ID {
		t.Fatalf("stream id changed: %q -> %q", firstAssistant.ID, secondAssistant.ID)
	}
	if !secondAssistant.Partial {
		t.Fatalf("second assistant finalized before provider turn completion: %#v", secondAssistant)
	}

	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "stream this"},
		map[string]any{"type": "assistant", "content": "Hello world!!"},
	)
	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "live-prompt", "turn_completed", nil),
	)
	final, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("final parse: %v", err)
	}
	finalAssistant := findGrokEvent(t, final.Events, "assistant_message", "Hello world!!")
	if finalAssistant.ID != firstAssistant.ID || finalAssistant.Partial {
		t.Fatalf("final assistant = %#v, first id %q", finalAssistant, firstAssistant.ID)
	}
	if countGrokEvents(final.Events, finalAssistant.ID) != 1 {
		t.Fatalf("final assistant duplicated: %#v", final.Events)
	}
	if final.Active == nil || *final.Active {
		t.Fatalf("final active = %#v", final.Active)
	}

	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "stream this"},
		map[string]any{"type": "assistant", "content": "Hello world!!"},
		map[string]any{"type": "user", "content": "later"},
		map[string]any{"type": "assistant", "content": "ordinary final"},
	)
	later, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("later parse: %v", err)
	}
	if countGrokEvents(later.Events, firstAssistant.ID) != 1 {
		t.Fatalf("streamed final duplicated after later messages: %#v", later.Events)
	}
	findGrokEvent(t, later.Events, "assistant_message", "ordinary final")
}

func TestParseGrokConversation_MatchingActiveHistoryDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-stream-existing"
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "hello"},
		map[string]any{"type": "assistant", "content": "already durable"},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
		grokUpdateFixture(sessionID, "live", "user_message_chunk", map[string]any{"type": "text", "text": "hello"}),
		grokUpdateFixture(sessionID, "live", "agent_message_chunk", map[string]any{"type": "text", "text": "already durable"}),
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	event := findGrokEvent(t, got.Events, "assistant_message", "already durable")
	if event.Partial || countGrokEvents(got.Events, event.ID) != 1 {
		t.Fatalf("matching active history duplicated or marked partial: %#v", got.Events)
	}
}

func TestLoadCachedGrokConversationInvalidatesOnNativeUpdates(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-cache-stream"
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "hello"},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile))
	grokConversationCache.Lock()
	delete(grokConversationCache.byPath, dir)
	grokConversationCache.Unlock()

	first, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if eventBodyContains(first.Events, "first chunk") {
		t.Fatalf("unexpected initial chunk: %#v", first.Events)
	}
	appendJSONL(t, filepath.Join(dir, grokUpdatesFile),
		grokUpdateFixture(sessionID, "live", "user_message_chunk", map[string]any{"type": "text", "text": "hello"}),
		grokUpdateFixture(sessionID, "live", "agent_message_chunk", map[string]any{"type": "text", "text": "first chunk"}),
	)
	second, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	findGrokEvent(t, second.Events, "assistant_message", "first chunk")
}

func TestParseGrokConversation_GroupsNativeStreamsAcrossInterleavedTools(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-native-stream-identity"
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "group native chunks"},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
		grokUpdateFixtureAt(sessionID, "prompt", 100, "user_message_chunk", map[string]any{"type": "text", "text": "group native chunks"}),
		grokUpdateFixtureAt(sessionID, "prompt", 100, "agent_message_chunk", map[string]any{"type": "text", "text": "Before "}),
		grokToolUpdateFixture(sessionID, "prompt", 100, "tool_call", "call-read", "", "Read", nil),
		grokToolUpdateFixture(sessionID, "prompt", 100, "tool_call_update", "call-read", "completed", "Read file", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "file contents"}},
		}),
		grokUpdateFixtureAt(sessionID, "prompt", 100, "agent_message_chunk", map[string]any{"type": "text", "text": "after."}),
		grokUpdateFixtureAt(sessionID, "prompt", 200, "agent_message_chunk", map[string]any{"type": "text", "text": "Second stream"}),
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := findGrokEvent(t, got.Events, "assistant_message", "Before after.")
	second := findGrokEvent(t, got.Events, "assistant_message", "Second stream")
	if first.ID == second.ID || !strings.Contains(first.ID, ":100") || !strings.Contains(second.ID, ":200") {
		t.Fatalf("native stream ids = %q, %q", first.ID, second.ID)
	}
	if first.Partial || !first.Transient || !second.Partial || !second.Transient {
		t.Fatalf("stream lifecycle = first %#v, second %#v", first, second)
	}
	if countGrokEventsByKind(got.Events, "assistant_message") != 2 {
		t.Fatalf("tool interleaving split a native stream: %#v", got.Events)
	}
}

func TestLoadCachedGrokConversation_StreamsSanitizedToolSnapshotsInPlace(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-tool-stream"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	historyPath := filepath.Join(dir, grokChatHistoryFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, historyPath, map[string]any{"type": "user", "content": "run tool"})
	writeJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt", 300, "user_message_chunk", map[string]any{"type": "text", "text": "run tool"}),
		grokToolUpdateFixture(sessionID, "prompt", 300, "tool_call", "call-shell", "", "Shell", nil),
		grokToolUpdateFixture(sessionID, "prompt", 300, "tool_call_update", "call-shell", "in_progress", "Shell", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "\x1b[31mone\x1b[0m"}},
		}),
	)
	grokConversationCache.Lock()
	delete(grokConversationCache.byPath, dir)
	grokConversationCache.Unlock()

	first, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstTool := findGrokToolEvent(t, first.Events, "call-shell")
	if firstTool.Output != "one" || firstTool.Status != "running" || !firstTool.Partial || strings.Contains(firstTool.Output, "\x1b") {
		t.Fatalf("first tool snapshot = %#v", firstTool)
	}

	appendJSONL(t, updatesPath,
		grokToolUpdateFixture(sessionID, "prompt", 300, "tool_call_update", "call-shell", "in_progress", "Shell command", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "one two"}},
		}),
	)
	second, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	secondTool := findGrokToolEvent(t, second.Events, "call-shell")
	if secondTool.ID != firstTool.ID || secondTool.Timestamp != firstTool.Timestamp || secondTool.Output != "one two" || !secondTool.Partial {
		t.Fatalf("second tool snapshot = %#v, first = %#v", secondTool, firstTool)
	}

	appendJSONL(t, updatesPath,
		grokToolUpdateFixture(sessionID, "prompt", 300, "tool_call_update", "call-shell", "completed", "Shell command", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "one two final"}},
		}),
	)
	completed, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	completedTool := findGrokToolEvent(t, completed.Events, "call-shell")
	if completedTool.ID != firstTool.ID || completedTool.Status != "done" || completedTool.Partial || !completedTool.Transient || completedTool.Output != "one two final" {
		t.Fatalf("completed tool = %#v", completedTool)
	}

	writeJSONL(t, historyPath,
		map[string]any{"type": "user", "content": "run tool"},
		map[string]any{"type": "assistant", "content": "", "tool_calls": []map[string]any{{"id": "call-shell", "name": "Shell", "arguments": `{}`}}},
		map[string]any{"type": "tool_result", "tool_call_id": "call-shell", "content": ""},
		map[string]any{"type": "assistant", "content": "Done."},
	)
	appendJSONL(t, updatesPath, grokUpdateFixture(sessionID, "prompt", "turn_completed", nil))
	final, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	finalTool := findGrokToolEvent(t, final.Events, "call-shell")
	if finalTool.ID != firstTool.ID || finalTool.Status != "done" || finalTool.Partial || finalTool.Transient {
		t.Fatalf("canonical tool finalization = %#v", finalTool)
	}
	if final.Active == nil || *final.Active {
		t.Fatalf("final active = %#v", final.Active)
	}
}

func TestLoadCachedGrokConversation_BackgroundTaskFinalizesSameToolAcrossTurnReset(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-background-task"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	historyPath := filepath.Join(dir, grokChatHistoryFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, historyPath,
		map[string]any{"type": "user", "content": "start background task"},
		map[string]any{
			"type":    "assistant",
			"content": "Task started.",
			"tool_calls": []map[string]any{
				{"id": "call-background", "name": "Shell", "arguments": `{"command":"long job"}`},
			},
		},
	)
	backgrounded := grokUpdateFixtureAt(sessionID, "prompt-a", 350, "task_backgrounded", nil)
	backgroundedUpdate := backgrounded["params"].(map[string]any)["update"].(map[string]any)
	backgroundedUpdate["tool_call_id"] = "call-background"
	backgroundedUpdate["task_id"] = "task-uuid"
	backgroundedUpdate["command"] = "long job"
	writeJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt-a", 350, "user_message_chunk", map[string]any{"type": "text", "text": "start background task"}),
		grokToolUpdateFixture(sessionID, "prompt-a", 350, "tool_call", "call-background", "in_progress", "Shell", nil),
		backgrounded,
	)
	grokConversationCache.Lock()
	delete(grokConversationCache.byPath, dir)
	grokConversationCache.Unlock()

	first, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstTool := findGrokToolEvent(t, first.Events, "call-background")
	if firstTool.Status != "running" || !firstTool.Partial {
		t.Fatalf("backgrounded tool = %#v", firstTool)
	}

	completed := grokUpdateFixtureAt(sessionID, "prompt-a", 350, "task_completed", nil)
	completedUpdate := completed["params"].(map[string]any)["update"].(map[string]any)
	exitCode := 0
	completedUpdate["task_snapshot"] = map[string]any{
		"task_id":     "task-uuid",
		"command":     "long job",
		"output":      "\x1b[32mfinished\x1b[0m\u0001",
		"output_file": "/tmp/terminal/call-background.log",
		"exit_code":   exitCode,
		"completed":   true,
		"kind":        "bash",
	}
	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "prompt-a", "turn_completed", nil),
		grokUpdateFixtureAt(sessionID, "prompt-b", 351, "user_message_chunk", map[string]any{"type": "text", "text": "check it"}),
		completed,
		// A delayed provider projection must not reopen the task after its
		// authoritative task_completed event.
		grokToolUpdateFixture(sessionID, "prompt-b", 351, "tool_call_update", "call-background", "in_progress", "Shell", nil),
		grokUpdateFixture(sessionID, "prompt-b", "turn_completed", nil),
	)
	writeJSONL(t, historyPath,
		map[string]any{"type": "user", "content": "start background task"},
		map[string]any{
			"type":    "assistant",
			"content": "Task started.",
			"tool_calls": []map[string]any{
				{"id": "call-background", "name": "Shell", "arguments": `{"command":"long job"}`},
			},
		},
		map[string]any{"type": "user", "content": "check it"},
		map[string]any{"type": "assistant", "content": "It finished."},
	)

	final, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	finalTool := findGrokToolEvent(t, final.Events, "call-background")
	if finalTool.ID != firstTool.ID || finalTool.Status != "done" || finalTool.Partial {
		t.Fatalf("final background tool = %#v, first = %#v", finalTool, firstTool)
	}
	if finalTool.Output != "finished" || strings.Contains(finalTool.Output, "\x1b") || strings.ContainsRune(finalTool.Output, '\x01') {
		t.Fatalf("sanitized background output = %q", finalTool.Output)
	}
	if countGrokEvents(final.Events, firstTool.ID) != 1 {
		t.Fatalf("background task duplicated: %#v", final.Events)
	}
	assertNoGrokRunningProjection(t, final)

	appendJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt-c", 352, "user_message_chunk", map[string]any{"type": "text", "text": "next ordinary turn"}),
		grokUpdateFixtureAt(sessionID, "prompt-c", 352, "agent_message_chunk", map[string]any{"type": "text", "text": "Next turn done."}),
		grokUpdateFixture(sessionID, "prompt-c", "turn_completed", nil),
	)
	writeJSONL(t, historyPath,
		map[string]any{"type": "user", "content": "start background task"},
		map[string]any{
			"type":    "assistant",
			"content": "Task started.",
			"tool_calls": []map[string]any{
				{"id": "call-background", "name": "Shell", "arguments": `{"command":"long job"}`},
			},
		},
		map[string]any{"type": "user", "content": "check it"},
		map[string]any{"type": "assistant", "content": "It finished."},
		map[string]any{"type": "user", "content": "next ordinary turn"},
		map[string]any{"type": "assistant", "content": "Next turn done."},
	)

	afterReset, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	resetTool := findGrokToolEvent(t, afterReset.Events, "call-background")
	if resetTool.ID != firstTool.ID || resetTool.Status != "done" || resetTool.Partial || resetTool.Output != "finished" {
		t.Fatalf("background terminal projection regressed after later turn: %#v", resetTool)
	}
	if countGrokEvents(afterReset.Events, firstTool.ID) != 1 {
		t.Fatalf("background task duplicated after later turn: %#v", afterReset.Events)
	}
	assertNoGrokRunningProjection(t, afterReset)
}

func TestLoadCachedGrokConversation_BackgroundFailureOverridesCanonicalLaunchAckAcrossReset(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-background-failure"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "start failing background task"},
		map[string]any{
			"type":    "assistant",
			"content": "Starting it.",
			"tool_calls": []map[string]any{
				{"id": "call-failing-background", "name": "Shell", "arguments": `{"command":"failing job"}`},
			},
		},
		map[string]any{
			"type":         "tool_result",
			"tool_call_id": "call-failing-background",
			"content":      "Background command started successfully",
		},
		map[string]any{"type": "assistant", "content": "Continuing while it runs."},
	)
	backgrounded := grokUpdateFixtureAt(sessionID, "prompt-a", 360, "task_backgrounded", nil)
	backgroundedUpdate := backgrounded["params"].(map[string]any)["update"].(map[string]any)
	backgroundedUpdate["tool_call_id"] = "call-failing-background"
	backgroundedUpdate["task_id"] = "task-failing-uuid"
	backgroundedUpdate["command"] = "failing job"
	writeJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt-a", 360, "user_message_chunk", map[string]any{"type": "text", "text": "start failing background task"}),
		grokToolUpdateFixture(sessionID, "prompt-a", 360, "tool_call", "call-failing-background", "in_progress", "Shell", nil),
		backgrounded,
	)
	grokConversationCache.Lock()
	delete(grokConversationCache.byPath, dir)
	grokConversationCache.Unlock()

	active, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	activeTool := findGrokToolEvent(t, active.Events, "call-failing-background")
	if activeTool.Status != "running" || !activeTool.Partial {
		t.Fatalf("canonical launch ack hid active background task: %#v", activeTool)
	}

	completed := grokUpdateFixtureAt(sessionID, "prompt-a", 360, "task_completed", nil)
	completedUpdate := completed["params"].(map[string]any)["update"].(map[string]any)
	completedUpdate["task_snapshot"] = map[string]any{
		"task_id":     "task-failing-uuid",
		"command":     "failing job",
		"output":      "\x1b[31mjob failed\x1b[0m",
		"output_file": "/tmp/terminal/call-failing-background.log",
		"exit_code":   7,
		"completed":   true,
		"kind":        "bash",
	}
	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "prompt-a", "turn_completed", nil),
		grokUpdateFixtureAt(sessionID, "prompt-b", 361, "user_message_chunk", map[string]any{"type": "text", "text": "check failure"}),
		completed,
		grokToolUpdateFixture(sessionID, "prompt-b", 361, "tool_call_update", "call-failing-background", "in_progress", "Shell", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "stale partial"}},
		}),
		grokUpdateFixture(sessionID, "prompt-b", "turn_completed", nil),
	)

	failed, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	failedTool := findGrokToolEvent(t, failed.Events, "call-failing-background")
	if failedTool.ID != activeTool.ID || failedTool.Status != "failed" || failedTool.Partial || failedTool.Output != "job failed" {
		t.Fatalf("native background failure did not override launch ack: %#v", failedTool)
	}
	assertNoGrokRunningProjection(t, failed)

	appendJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt-c", 362, "user_message_chunk", map[string]any{"type": "text", "text": "ordinary later turn"}),
		grokToolUpdateFixture(sessionID, "prompt-c", 362, "tool_call_update", "call-failing-background", "in_progress", "Shell", []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "even staler after reset"}},
		}),
		grokUpdateFixtureAt(sessionID, "prompt-c", 362, "agent_message_chunk", map[string]any{"type": "text", "text": "Later turn done."}),
		grokUpdateFixture(sessionID, "prompt-c", "turn_completed", nil),
	)
	afterReset, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	resetTool := findGrokToolEvent(t, afterReset.Events, "call-failing-background")
	if resetTool.ID != activeTool.ID || resetTool.Status != "failed" || resetTool.Partial || resetTool.Output != "job failed" {
		t.Fatalf("background failure regressed after later turn: %#v", resetTool)
	}
	if countGrokEvents(afterReset.Events, activeTool.ID) != 1 {
		t.Fatalf("background failure duplicated after later turn: %#v", afterReset.Events)
	}
	assertNoGrokRunningProjection(t, afterReset)
}

func TestParseGrokConversation_TurnCompletionAndFailureSettleEveryProjection(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		terminal     map[string]any
		toolStatus   string
		statusPhrase string
	}{
		{
			name:       "turn completed",
			terminal:   grokUpdateFixture("grok-settle-turn", "prompt", "turn_completed", nil),
			toolStatus: "done",
		},
		{
			name: "provider failure",
			terminal: func() map[string]any {
				record := grokUpdateFixtureAt("grok-settle-failure", "prompt", 451, "retry_state", nil)
				update := record["params"].(map[string]any)["update"].(map[string]any)
				update["type"] = "failed"
				update["message"] = "provider request failed"
				return record
			}(),
			toolStatus:   "failed",
			statusPhrase: "provider request failed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			sessionID := testCase.terminal["params"].(map[string]any)["sessionId"].(string)
			writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
				"info": map[string]any{"id": sessionID, "cwd": "/repo"},
			})
			writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
				map[string]any{"type": "user", "content": "settle"},
				map[string]any{
					"type":    "assistant",
					"content": "Running a tool.",
					"tool_calls": []map[string]any{
						{"id": "call-live", "name": "Shell", "arguments": `{"command":"work"}`},
					},
				},
				map[string]any{"type": "assistant", "content": "Tool turn finished."},
			)
			writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
				grokUpdateFixtureAt(sessionID, "prompt", 451, "user_message_chunk", map[string]any{"type": "text", "text": "settle"}),
				grokUpdateFixtureAt(sessionID, "prompt", 451, "agent_thought_chunk", map[string]any{"type": "text", "text": "working"}),
				grokUpdateFixtureAt(sessionID, "prompt", 451, "agent_message_chunk", map[string]any{"type": "text", "text": "partial answer"}),
				grokToolUpdateFixture(sessionID, "prompt", 451, "tool_call_update", "call-live", "in_progress", "Shell", []map[string]any{
					{"type": "content", "content": map[string]any{"type": "text", "text": "work output"}},
				}),
				testCase.terminal,
				grokToolUpdateFixture(sessionID, "prompt", 451, "tool_call_update", "call-live", "in_progress", "Shell", nil),
			)

			got, err := parseGrokConversation(dir)
			if err != nil {
				t.Fatal(err)
			}
			tool := findGrokToolEvent(t, got.Events, "call-live")
			if tool.Status != testCase.toolStatus || tool.Partial {
				t.Fatalf("settled tool = %#v", tool)
			}
			if testCase.statusPhrase != "" {
				findGrokEvent(t, got.Events, "status", testCase.statusPhrase)
			}
			assertNoGrokRunningProjection(t, got)
		})
	}
}

func TestParseGrokConversation_FinalizesCanonicalReasoningWithStreamIdentity(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-reasoning-final"
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "think"},
		map[string]any{"type": "reasoning", "summary": []map[string]any{{"type": "summary_text", "text": "First\n\nsecond"}}},
		map[string]any{"type": "assistant", "content": "Final answer"},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
		grokUpdateFixtureAt(sessionID, "prompt", 400, "user_message_chunk", map[string]any{"type": "text", "text": "think"}),
		grokUpdateFixtureAt(sessionID, "prompt", 400, "agent_thought_chunk", map[string]any{"type": "text", "text": "First"}),
		grokUpdateFixtureAt(sessionID, "prompt", 400, "agent_thought_chunk", map[string]any{"type": "text", "text": "\n\nsecond"}),
		grokUpdateFixture(sessionID, "prompt", "turn_completed", nil),
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := findGrokEvent(t, got.Events, "commentary", "First\n\nsecond")
	if !strings.Contains(reasoning.ID, ":stream:prompt:reasoning:400") || reasoning.Partial || reasoning.Status != "done" {
		t.Fatalf("reasoning final = %#v", reasoning)
	}
	if countGrokEvents(got.Events, reasoning.ID) != 1 {
		t.Fatalf("reasoning duplicated: %#v", got.Events)
	}
	if ids := grokEventKinds(got.Events); fmt.Sprint(ids) != "[user_message commentary assistant_message]" {
		t.Fatalf("history order = %v", ids)
	}
}

func TestParseGrokConversation_UpdatesNativeRetryStatusInPlace(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-retry-status"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile), map[string]any{"type": "user", "content": "retry"})
	firstRetry := grokUpdateFixtureAt(sessionID, "prompt", 450, "retry_state", nil)
	firstUpdate := firstRetry["params"].(map[string]any)["update"].(map[string]any)
	firstUpdate["type"] = "retrying"
	firstUpdate["attempt"] = 1
	firstUpdate["max_retries"] = 3
	firstUpdate["reason"] = "provider temporarily unavailable"
	writeJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt", 450, "user_message_chunk", map[string]any{"type": "text", "text": "retry"}),
		firstRetry,
	)

	first, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	status := findGrokEvent(t, first.Events, "status", "temporarily unavailable")
	if status.Status != "running" || !status.Partial || !status.Transient || !strings.Contains(status.Title, "1/3") {
		t.Fatalf("first retry status = %#v", status)
	}

	failedRetry := grokUpdateFixtureAt(sessionID, "prompt", 450, "retry_state", nil)
	failedUpdate := failedRetry["params"].(map[string]any)["update"].(map[string]any)
	failedUpdate["type"] = "failed"
	failedUpdate["message"] = "provider request failed"
	appendJSONL(t, updatesPath, failedRetry)
	failed, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	failedStatus := findGrokEvent(t, failed.Events, "status", "provider request failed")
	if failedStatus.ID != status.ID || failedStatus.Status != "failed" || failedStatus.Partial {
		t.Fatalf("failed retry status = %#v, first = %#v", failedStatus, status)
	}
	if failed.Active == nil || *failed.Active {
		t.Fatalf("failed retry active = %#v", failed.Active)
	}
}

func TestGrokToolUpdateOutput_DropsWrapperOnlyTerminalFallback(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"type":              "Bash",
		"output":            []string{},
		"output_for_prompt": "Exit code: 0\n\nCommand output:\n\n```\n\n```\n\nCommand completed\nShell state: idle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := grokToolUpdateOutput(raw); got != "" {
		t.Fatalf("terminal wrapper leaked into structured output: %q", got)
	}
}

func TestGrokBackgroundTaskTerminalMapping(t *testing.T) {
	zero := 0
	nonzero := 7
	if got := grokBackgroundTaskStatus(&zero, nil, false); got != "done" {
		t.Fatalf("zero exit status = %q", got)
	}
	if got := grokBackgroundTaskStatus(&nonzero, nil, false); got != "failed" {
		t.Fatalf("nonzero exit status = %q", got)
	}
	if got := grokBackgroundTaskStatus(nil, json.RawMessage(`"SIGTERM"`), false); got != "failed" {
		t.Fatalf("signal status = %q", got)
	}
	if got := grokBackgroundTaskStatus(nil, json.RawMessage("null"), true); got != "failed" {
		t.Fatalf("explicit kill status = %q", got)
	}
	if got := grokBackgroundCallID("task-uuid", "/tmp/terminal/call-fallback.log", map[string]int{}); got != "call-fallback" {
		t.Fatalf("output-file call identity = %q", got)
	}
}

func TestLoadCachedGrokConversation_UsesNewestIdentityForRepeatedCompactedReply(t *testing.T) {
	dir := t.TempDir()
	sessionID := "grok-repeated-reply"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	historyPath := filepath.Join(dir, grokChatHistoryFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, historyPath, map[string]any{"type": "user", "content": "first"})
	writeJSONL(t, updatesPath,
		grokUpdateFixtureAt(sessionID, "prompt-a", 501, "user_message_chunk", map[string]any{"type": "text", "text": "first"}),
		grokUpdateFixtureAt(sessionID, "prompt-a", 501, "agent_message_chunk", map[string]any{"type": "text", "text": "Done."}),
	)
	grokConversationCache.Lock()
	delete(grokConversationCache.byPath, dir)
	grokConversationCache.Unlock()
	first, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstReply := findGrokEvent(t, first.Events, "assistant_message", "Done.")
	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "prompt-a", "turn_completed", nil),
		grokUpdateFixtureAt(sessionID, "prompt-b", 502, "user_message_chunk", map[string]any{"type": "text", "text": "second"}),
		grokUpdateFixtureAt(sessionID, "prompt-b", 502, "agent_message_chunk", map[string]any{"type": "text", "text": "Done."}),
		grokUpdateFixture(sessionID, "prompt-b", "turn_completed", nil),
	)
	writeJSONL(t, historyPath,
		map[string]any{"type": "user", "content": "first"},
		map[string]any{"type": "assistant", "content": "Done."},
		map[string]any{"type": "user", "content": "second"},
		map[string]any{"type": "assistant", "content": "Done."},
	)

	got, err := loadCachedGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	replies := grokEventsByKind(got.Events, "assistant_message")
	if len(replies) != 2 {
		t.Fatalf("repeated replies = %#v", got.Events)
	}
	if replies[0].ID != firstReply.ID || !strings.Contains(replies[0].ID, ":prompt-a:assistant:501") {
		t.Fatalf("first finalized identity changed after tracker reset: first=%#v later=%#v", firstReply, replies[0])
	}
	if !strings.Contains(replies[1].ID, ":prompt-b:assistant:502") {
		t.Fatalf("newest repeated reply adopted stale identity: %#v", replies[1])
	}
}

func grokUpdateFixture(sessionID, promptID, kind string, content any) map[string]any {
	return grokUpdateFixtureAt(sessionID, promptID, 1_784_000_000_123, kind, content)
}

func grokUpdateFixtureAt(sessionID, promptID string, streamStart int64, kind string, content any) map[string]any {
	update := map[string]any{"sessionUpdate": kind}
	if content != nil {
		update["content"] = content
	}
	meta := map[string]any{
		"promptId":      promptID,
		"streamStartMs": streamStart,
	}
	if kind == "turn_completed" {
		meta = map[string]any{}
	}
	return map[string]any{
		"timestamp": 1_784_000_000,
		"params": map[string]any{
			"sessionId": sessionID,
			"update":    update,
			"_meta":     meta,
		},
	}
}

func grokToolUpdateFixture(sessionID, promptID string, streamStart int64, kind, callID, status, title string, content any) map[string]any {
	record := grokUpdateFixtureAt(sessionID, promptID, streamStart, kind, content)
	params := record["params"].(map[string]any)
	update := params["update"].(map[string]any)
	update["toolCallId"] = callID
	if status != "" {
		update["status"] = status
	}
	if title != "" {
		update["title"] = title
	}
	return record
}

func appendJSONL(t *testing.T, path string, values ...any) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
}

func findGrokEvent(t *testing.T, events []CodexConversationEvent, kind, body string) CodexConversationEvent {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind && strings.Contains(event.Body, body) {
			return event
		}
	}
	t.Fatalf("missing %s event containing %q: %#v", kind, body, events)
	return CodexConversationEvent{}
}

func eventBodyContains(events []CodexConversationEvent, body string) bool {
	for _, event := range events {
		if strings.Contains(event.Body, body) {
			return true
		}
	}
	return false
}

func countGrokEvents(events []CodexConversationEvent, id string) int {
	count := 0
	for _, event := range events {
		if event.ID == id {
			count++
		}
	}
	return count
}

func countGrokEventsByKind(events []CodexConversationEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func findGrokToolEvent(t *testing.T, events []CodexConversationEvent, callID string) CodexConversationEvent {
	t.Helper()
	for _, event := range events {
		if event.Kind == "tool" && event.CallID == callID {
			return event
		}
	}
	t.Fatalf("missing tool event %q: %#v", callID, events)
	return CodexConversationEvent{}
}

func grokEventKinds(events []CodexConversationEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func grokEventsByKind(events []CodexConversationEvent, kind string) []CodexConversationEvent {
	matched := make([]CodexConversationEvent, 0)
	for _, event := range events {
		if event.Kind == kind {
			matched = append(matched, event)
		}
	}
	return matched
}

func assertNoGrokRunningProjection(t *testing.T, conversation CodexConversation) {
	t.Helper()
	if conversation.Active == nil || *conversation.Active {
		t.Fatalf("conversation active after terminal event: %#v", conversation.Active)
	}
	for _, event := range conversation.Events {
		if event.Partial || event.Status == "running" {
			t.Fatalf("unsettled event after terminal event: %#v", event)
		}
	}
}

func TestParseGrokConversation_AssistantToolCallsUseUniqueEventIDs(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-tools-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type":    "assistant",
			"content": "Running lookups.",
			"tool_calls": []map[string]any{
				{"id": "call-a", "name": "Grep", "arguments": `{"pattern":"foo"}`},
				{"id": "call-b", "name": "Glob", "arguments": `{"glob_pattern":"**/*.go"}`},
			},
		},
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	seen := map[string]int{}
	for _, event := range got.Events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %#v, want assistant message only in chat feed", got.Events)
	}
	if got.Events[0].Kind != "assistant_message" {
		t.Fatalf("event = %#v", got.Events[0])
	}
}

func TestParseGrokConversation_BuildsStructuredTimeline(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-test-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type": "user",
			"content": []map[string]any{
				{"type": "text", "text": "<user_query>\nShip the Grok chat interface\n</user_query>"},
			},
		},
		map[string]any{
			"type":    "assistant",
			"content": "I'll inspect the existing Codex interface first.",
		},
		map[string]any{
			"type": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":        "call-grep",
					"name":      "Grep",
					"arguments": `{"pattern":"codex"}`,
				},
			},
		},
		map[string]any{
			"type":         "tool_result",
			"tool_call_id": "call-grep",
			"content":      "found 3 matches",
		},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
		map[string]any{
			"timestamp": "2026-06-29T10:00:00Z",
			"params": map[string]any{
				"sessionId": "grok-test-1",
				"update": map[string]any{
					"sessionUpdate": "plan",
					"entries": []map[string]any{
						{"content": "Add grok parser", "status": "in_progress"},
						{"content": "Wire app chat surface", "status": "pending"},
					},
				},
			},
		},
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	if !got.Available {
		t.Fatal("conversation should be available")
	}
	if got.SessionID != "grok-test-1" || got.CWD != "/repo" {
		t.Fatalf("metadata = (%q, %q)", got.SessionID, got.CWD)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}

	assertEvent(t, got.Events[0], "user_message", "user", "", "Ship the Grok chat interface")
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "I'll inspect the existing Codex interface first.")
}

func TestLoadCodexConversationForAgent_GrokUnavailableWithoutSession(t *testing.T) {
	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Command: "grok --no-alt-screen",
		Cwd:     filepath.Join(t.TempDir(), "missing-grok-session"),
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "session_not_found" {
		t.Fatalf("conversation = %#v", got)
	}
}

func TestGrokGoalSessionPreservesLatestUserMessages(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}
	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	for _, event := range got.Events {
		if event.Kind == "plan" {
			t.Fatalf("unexpected plan event in grok chat feed: %#v", event)
		}
	}
	userBodies := make([]string, 0, 4)
	for _, event := range got.Events {
		if event.Kind == "user_message" {
			userBodies = append(userBodies, event.Body)
		}
	}
	if len(userBodies) < 2 {
		t.Fatalf("user messages = %#v, want at least 2", userBodies)
	}
	foundVisibleUserMessage := false
	for _, body := range userBodies {
		if !strings.Contains(body, "<summary_content>") && len(strings.TrimSpace(body)) < 2000 {
			foundVisibleUserMessage = true
			break
		}
	}
	if !foundVisibleUserMessage {
		t.Fatalf("user messages missing visible user message: %#v", userBodies)
	}
	latestEvent := got.Events[len(got.Events)-1]
	if latestEvent.Kind == "plan" {
		t.Fatal("latest event should not be a plan checklist")
	}
	if latestEvent.Kind != "assistant_message" {
		t.Fatalf("latest event = %#v, want assistant reply", latestEvent)
	}
	if strings.TrimSpace(latestEvent.Body) == "" {
		t.Fatalf("latest assistant reply empty: %#v", latestEvent)
	}
}

func TestGrokGoalSessionEventMix(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}
	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	counts := map[string]int{}
	for _, event := range got.Events {
		counts[event.Kind]++
	}
	t.Logf("event mix: %#v total=%d", counts, len(got.Events))
	for i := len(got.Events) - 8; i < len(got.Events); i++ {
		if i < 0 {
			continue
		}
		event := got.Events[i]
		body := event.Body
		if len(body) > 60 {
			body = body[:60] + "..."
		}
		t.Logf("tail[%d] kind=%s ts=%q body=%q plan=%d", i, event.Kind, event.Timestamp, body, len(event.Plan))
	}
}

func TestLoadCodexConversationForAgent_GrokGoalSessionHasUniqueEventIDs(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}

	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	seen := map[string]int{}
	for _, event := range got.Events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
	t.Logf("parsed %d unique grok events from goal session", len(got.Events))
}

func TestLoadCodexConversationForAgent_GrokRealSessionFixture(t *testing.T) {
	sourceDir := findLocalGrokSessionDir(t)
	fixtureHome, cwd := installGrokSessionFixture(t, sourceDir)

	t.Setenv("HOME", fixtureHome)
	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Command:   "grok --no-alt-screen --permission-mode bypassPermissions",
		Cwd:       cwd,
		StartedAt: time.Now().Add(-time.Hour),
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if !got.Available {
		t.Fatalf("conversation unavailable: %#v", got)
	}
	if len(got.Events) == 0 {
		t.Fatal("expected parsed grok events")
	}

	hasUser := false
	hasAssistant := false
	hasTool := false
	for _, event := range got.Events {
		switch event.Kind {
		case "user_message":
			hasUser = true
		case "assistant_message":
			hasAssistant = true
		case "tool":
			hasTool = true
		}
	}
	if !hasUser || !hasAssistant || !hasTool {
		t.Fatalf("missing event kinds: user=%v assistant=%v tool=%v events=%#v", hasUser, hasAssistant, hasTool, got.Events)
	}
	t.Logf("parsed %d grok events from fixture", len(got.Events))
}

// requireGrokRealSessionOptIn gates tests that read maintainer ~/.grok data.
// Default `go test ./...` must not inspect local agent session stores.
func requireGrokRealSessionOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv("ZEN_GROK_REAL_SESSION") != "1" {
		t.Skip("set ZEN_GROK_REAL_SESSION=1 to run opt-in ~/.grok integration tests")
	}
}

func findLocalGrokSessionDir(t *testing.T) string {
	t.Helper()
	requireGrokRealSessionOptIn(t)
	source := filepath.Join(os.Getenv("HOME"), ".grok", "sessions", "%2Fhome%2Fdaoleno%2Fworkspace%2Fzen")
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Skipf("real grok sessions unavailable: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(source, entry.Name())
		if _, err := os.Stat(filepath.Join(candidate, grokChatHistoryFile)); err != nil {
			continue
		}
		conversation, err := parseGrokConversation(candidate)
		if err != nil {
			continue
		}
		hasUser := countGrokEventsByKind(conversation.Events, "user_message") > 0
		hasAssistant := countGrokEventsByKind(conversation.Events, "assistant_message") > 0
		hasTool := countGrokEventsByKind(conversation.Events, "tool") > 0
		if hasUser && hasAssistant && hasTool {
			return candidate
		}
	}
	t.Skip("no grok session with structured user, assistant, and tool history found")
	return ""
}

func installGrokSessionFixture(t *testing.T, sourceDir string) (home string, cwd string) {
	t.Helper()
	cwd = "/tmp/zen-grok-fixture"
	homeRoot := t.TempDir()
	home = filepath.Join(homeRoot, "home")
	sessionRoot := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), filepath.Base(sourceDir))
	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, name := range []string{grokChatHistoryFile, grokUpdatesFile} {
		sourcePath := filepath.Join(sourceDir, name)
		if _, err := os.Stat(sourcePath); err != nil {
			continue
		}
		copyFixtureFile(t, sourcePath, filepath.Join(sessionRoot, name))
	}
	writeGrokSummary(t, filepath.Join(sessionRoot, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  filepath.Base(sourceDir),
			"cwd": cwd,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"created_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
	})
	return home, cwd
}

func writeGrokSummary(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func copyFixtureFile(t *testing.T, source, dest string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("Open %s: %v", source, err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		t.Fatalf("Create %s: %v", dest, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("Copy %s: %v", source, err)
	}
}

func TestGrokVisibleUserTextExtractsUserQuery(t *testing.T) {
	got := grokVisibleUserText("<user_query>\nHello Grok\n</user_query>")
	if got != "Hello Grok" {
		t.Fatalf("grokVisibleUserText = %q", got)
	}
}

func TestIsGrokBootstrapUserMessage(t *testing.T) {
	if !isGrokBootstrapUserMessage("<user_info>\nOS Version: linux\n</user_info>") {
		t.Fatal("expected bootstrap detection")
	}
	if isGrokBootstrapUserMessage("Implement the Grok interface") {
		t.Fatal("did not expect bootstrap detection")
	}
}

func TestAgentExecutorInfersGrokProviderAndCapabilities(t *testing.T) {
	cfg := &ExecutorConfig{
		DelegatedExecutor: "claude",
		ByName: map[string]Executor{
			"grok": {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
		},
	}
	executor, ok := cfg.AgentExecutor("grok")
	if !ok {
		t.Fatal("grok executor missing")
	}
	if executor.Provider != AgentProviderGrok {
		t.Fatalf("provider = %q", executor.Provider)
	}
	if !executor.Capabilities.StructuredEvents || executor.Capabilities.NativeThreads {
		t.Fatalf("capabilities = %+v", executor.Capabilities)
	}
}

func TestIsAgentCommandRecognizesGrok(t *testing.T) {
	if !IsAgentCommand("grok --no-alt-screen --permission-mode bypassPermissions") {
		t.Fatal("expected grok command recognition")
	}
}

func TestIsNativeAgentSourceIncludesGrok(t *testing.T) {
	if !IsNativeAgentSource("grok") {
		t.Fatal("expected grok native source")
	}
}

func TestLoadExecutorsIncludesGrokDefault(t *testing.T) {
	cfg, err := LoadExecutors(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	executor, ok := cfg.ByName["grok"]
	if !ok {
		t.Fatal("grok missing from defaults")
	}
	if !strings.Contains(executor.Command, "--no-alt-screen") || !strings.Contains(executor.Command, "bypassPermissions") {
		t.Fatalf("grok command = %q", executor.Command)
	}
	if cfg.DelegatedExecutor != "codex" {
		t.Fatalf("delegated executor changed to %q", cfg.DelegatedExecutor)
	}
}
