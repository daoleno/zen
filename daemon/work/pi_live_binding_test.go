package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// TestPiLiveBindingSurvivesRefreshReconnectAndLateGrowth reproduces the real
// launch-to-reader gap at the reader boundary: the owned --session path in the
// agent command must deterministically bind the exact transcript across late
// file creation, incremental growth, a fresh reader (reconnect/reload), and
// repeated loads (watcher refresh). Event IDs must stay stable across loads so
// the server delta path never re-sends settled history.
func TestPiLiveBindingSurvivesRefreshReconnectAndLateGrowth(t *testing.T) {
	dir := t.TempDir()
	owned := filepath.Join(dir, "owned.jsonl")
	agent := classifier.Agent{
		ID:        "agent-pi",
		Cwd:       "/repo",
		Command:   "pi --session " + owned,
		StartedAt: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
	}

	reader := NewProviderConversationReader()
	now := time.Now().UTC()

	// Late file creation: before the first flush the owned path is an honest
	// missing transcript, never a fallback to the shared per-CWD store.
	first, err := reader.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Available || first.Reason != "transcript_not_found" {
		t.Fatalf("late creation load = %+v, want transcript_not_found", first)
	}

	// First flush: user text only.
	writePiLiveFixture(t, owned, "/repo", "sess-live-1", []string{
		piLiveUserLine("u1", "", "2026-08-07T10:00:01.000Z", "first user text"),
	})
	second, err := reader.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Available || second.SessionID != "sess-live-1" || len(second.Events) != 1 ||
		second.Events[0].Kind != "user_message" || second.Events[0].Body != "first user text" {
		t.Fatalf("first flush = %+v", second)
	}
	firstEventID := second.Events[0].ID

	// Growth: assistant thinking + text + tool call (toolUse) append new
	// events; the settled history keeps its exact IDs.
	appendPiLiveLines(t, owned, []string{
		piLiveAssistantLine("a1", "u1", "2026-08-07T10:00:02.000Z", "second text", "toolUse", `{"type":"thinking","thinking":"planning"},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}`),
	})
	third, err := reader.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Available || third.SessionID != "sess-live-1" {
		t.Fatalf("growth load = %+v", third)
	}
	if third.Events[0].ID != firstEventID {
		t.Fatalf("user event id changed after growth: %q -> %q", firstEventID, third.Events[0].ID)
	}
	var tool *CodexConversationEvent
	for i := range third.Events {
		if third.Events[i].Kind == "tool_call" {
			tool = &third.Events[i]
		}
	}
	if tool == nil || tool.ToolName != "bash" || tool.Status != "running" || !tool.Partial {
		t.Fatalf("tool call event = %#v", tool)
	}
	toolID := tool.ID

	// Tool result merges into the same tool event (stable ID, completed) and
	// the final stop settles the lifecycle.
	appendPiLiveLines(t, owned, []string{
		piLiveToolResultLine("r1", "a1", "2026-08-07T10:00:03.000Z", "call_1", "bash", "tool output ok", false),
		piLiveAssistantLine("a2", "r1", "2026-08-07T10:00:04.000Z", "final text", "stop", `{"type":"text","text":"final text"}`),
	})
	fourth, err := reader.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !fourth.Available || fourth.Activity == nil || fourth.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("settled lifecycle = %+v", fourth.Activity)
	}
	tool = nil
	for i := range fourth.Events {
		if fourth.Events[i].Kind == "tool_call" {
			tool = &fourth.Events[i]
		}
	}
	if tool == nil || tool.ID != toolID || tool.Status != "completed" || tool.Partial ||
		tool.Output != "tool output ok" {
		t.Fatalf("tool result did not merge into stable tool event: %#v", tool)
	}
	hasFinal := false
	for _, event := range fourth.Events {
		if event.Kind == "assistant_message" && event.Body == "final text" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatalf("final assistant text missing: %#v", fourth.Events)
	}

	// Reconnect/reload: a brand-new reader binds the exact same transcript
	// from the agent command alone, without any reader-owned pin memory.
	fresh := NewProviderConversationReader()
	freshConversation, err := fresh.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !freshConversation.Available || freshConversation.SessionID != "sess-live-1" ||
		len(freshConversation.Events) != len(fourth.Events) {
		t.Fatalf("fresh reader binding = %+v", freshConversation)
	}
	for i := range fourth.Events {
		if freshConversation.Events[i].ID != fourth.Events[i].ID ||
			freshConversation.Events[i].Body != fourth.Events[i].Body {
			t.Fatalf("fresh reader event %d diverged: %#v vs %#v", i, freshConversation.Events[i], fourth.Events[i])
		}
	}
	if freshConversation.Activity == nil || freshConversation.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("fresh reader lifecycle = %+v", freshConversation.Activity)
	}

	// Repeated loads on the same reader (watcher refresh) keep the identity
	// and never renumber events.
	again, err := reader.Load(agent, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Available || again.SessionID != "sess-live-1" || len(again.Events) != len(fourth.Events) {
		t.Fatalf("repeat load = %+v", again)
	}
}

// TestPiSameCWDSessionsNeverCrossBind pins the fail-closed isolation rule at
// the reader boundary: two same-CWD Pi Sessions with distinct owned paths must
// deterministically bind their own transcripts on the same reader and on fresh
// readers, and the shared per-CWD store must never pick up owned transcripts.
func TestPiSameCWDSessionsNeverCrossBind(t *testing.T) {
	dir := t.TempDir()
	ownedA := filepath.Join(dir, "a.jsonl")
	ownedB := filepath.Join(dir, "b.jsonl")
	writePiLiveFixture(t, ownedA, "/repo", "sess-a", []string{
		piLiveUserLine("u1", "", "2026-08-07T10:00:01.000Z", "session a text"),
		piLiveAssistantLine("a1", "u1", "2026-08-07T10:00:02.000Z", "session a reply", "stop", `{"type":"text","text":"session a reply"}`),
	})
	writePiLiveFixture(t, ownedB, "/repo", "sess-b", []string{
		piLiveUserLine("u1", "", "2026-08-07T10:00:01.000Z", "session b text"),
		piLiveAssistantLine("a1", "u1", "2026-08-07T10:00:02.000Z", "session b reply", "stop", `{"type":"text","text":"session b reply"}`),
	})
	agentA := classifier.Agent{ID: "agent-a", Cwd: "/repo", Command: "pi --session " + ownedA}
	agentB := classifier.Agent{ID: "agent-b", Cwd: "/repo", Command: "pi --session " + ownedB}
	now := time.Now().UTC()

	reader := NewProviderConversationReader()
	gotA, err := reader.Load(agentA, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotA.Available || gotA.SessionID != "sess-a" {
		t.Fatalf("agent A bound = %+v", gotA)
	}
	gotB, err := reader.Load(agentB, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotB.Available || gotB.SessionID != "sess-b" || conversationContainsBody(gotB, "session a text") {
		t.Fatalf("agent B bound = %+v", gotB)
	}
	// Switching back must rebind A without leaking B.
	gotABack, err := reader.Load(agentA, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotABack.Available || gotABack.SessionID != "sess-a" || conversationContainsBody(gotABack, "session b text") {
		t.Fatalf("agent A rebind leaked B: %+v", gotABack)
	}

	// Fresh readers (reconnect/reload) bind the exact same owned transcripts.
	freshB := NewProviderConversationReader()
	gotBfresh, err := freshB.Load(agentB, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotBfresh.Available || gotBfresh.SessionID != "sess-b" {
		t.Fatalf("fresh reader B = %+v", gotBfresh)
	}

	// The shared per-CWD store never sees the owned transcripts: an unowned
	// "pi" launch with an empty shared directory stays transcript_not_found.
	agentDir := filepath.Join(dir, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	unowned := classifier.Agent{ID: "agent-unowned", Cwd: "/repo", Command: "pi"}
	gotUnowned, err := NewProviderConversationReader().Load(unowned, AgentProviderPi, now)
	if err != nil {
		t.Fatal(err)
	}
	if gotUnowned.Available || gotUnowned.Reason != "transcript_not_found" {
		t.Fatalf("unowned load must not see owned transcripts: %+v", gotUnowned)
	}
}

// TestPiLiveConversationProjectsAllShapes covers every live Pi content shape:
// user text, reasoning, assistant text, tool calls, tool results (success and
// error), bash command execution, and the running/completed lifecycle with
// stable IDs in parent-chain order.
func TestPiLiveConversationProjectsAllShapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shapes.jsonl")
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-shapes","timestamp":"2026-08-07T10:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-07T10:00:01.000Z","message":{"role":"user","content":"run everything"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-07T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning text"},{"type":"text","text":"starting work"},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo ok"}},{"type":"toolCall","id":"call_2","name":"read","arguments":{"path":"/repo/a"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"r1","parentId":"a1","timestamp":"2026-08-07T10:00:03.000Z","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false}}`,
		`{"type":"message","id":"r2","parentId":"r1","timestamp":"2026-08-07T10:00:04.000Z","message":{"role":"toolResult","toolCallId":"call_2","toolName":"read","content":[{"type":"text","text":"boom"}],"isError":true}}`,
		`{"type":"message","id":"b1","parentId":"r2","timestamp":"2026-08-07T10:00:05.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_3","name":"bash","arguments":{"command":"sleep 5"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"r3","parentId":"b1","timestamp":"2026-08-07T10:00:06.000Z","message":{"role":"toolResult","toolCallId":"call_3","toolName":"bash","content":[{"type":"text","text":"ran"}],"isError":false}}`,
		`{"type":"message","id":"bx1","parentId":"r3","timestamp":"2026-08-07T10:00:07.000Z","message":{"role":"bashexecution","command":"sleep 5","exitCode":0,"cancelled":false,"content":[{"type":"text","text":"ran"}]}}`,
		`{"type":"message","id":"a2","parentId":"bx1","timestamp":"2026-08-07T10:00:08.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	byID := map[string]CodexConversationEvent{}
	var bashTool, failedTool *CodexConversationEvent
	for i := range conversation.Events {
		event := conversation.Events[i]
		kinds = append(kinds, event.Kind)
		if _, dup := byID[event.ID]; dup {
			t.Fatalf("duplicate event id %q", event.ID)
		}
		byID[event.ID] = event
		switch {
		case event.Kind == "tool_call" && event.CallID == "call_1":
			bashTool = &conversation.Events[i]
		case event.Kind == "tool_call" && event.CallID == "call_2":
			failedTool = &conversation.Events[i]
		}
	}
	wantKinds := []string{
		"user_message", "reasoning", "assistant_message",
		"tool_call", "tool_call", "tool_call", "command_execution", "assistant_message",
	}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}
	if bashTool == nil || bashTool.Status != "completed" || bashTool.Partial || bashTool.Output != "ok" {
		t.Fatalf("successful tool = %#v", bashTool)
	}
	if failedTool == nil || failedTool.Status != "failed" || failedTool.Partial || failedTool.Output != "boom" {
		t.Fatalf("failed tool = %#v", failedTool)
	}
	var execution *CodexConversationEvent
	for i := range conversation.Events {
		if conversation.Events[i].Kind == "command_execution" {
			execution = &conversation.Events[i]
		}
	}
	if execution == nil || execution.Command != "sleep 5" || execution.Status != "completed" ||
		execution.ExitCode == nil || *execution.ExitCode != 0 {
		t.Fatalf("bash execution = %#v", execution)
	}
	if conversation.Activity == nil || conversation.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("lifecycle = %+v", conversation.Activity)
	}
	if !strings.Contains(conversation.Events[0].Body, "run everything") ||
		!strings.Contains(conversation.Events[1].Body, "reasoning text") ||
		conversation.Events[1].Transient != true ||
		!strings.Contains(conversation.Events[2].Body, "starting work") ||
		!strings.Contains(conversation.Events[len(conversation.Events)-1].Body, "done") {
		t.Fatalf("event bodies = %#v", conversation.Events)
	}
}

// TestPiLiveInterruptedTurnSettlesRunningTool pins the interrupted lifecycle:
// an aborted turn cancels still-running tools and settles the Activity, so the
// Interface never leaves a tool permanently running.
func TestPiLiveInterruptedTurnSettlesRunningTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupt.jsonl")
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-interrupt","timestamp":"2026-08-07T10:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-07T10:00:01.000Z","message":{"role":"user","content":"go"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-07T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_9","name":"bash","arguments":{"command":"long task"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"a2","parentId":"a1","timestamp":"2026-08-07T10:00:03.000Z","message":{"role":"assistant","content":[],"stopReason":"aborted"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var tool *CodexConversationEvent
	for i := range conversation.Events {
		if conversation.Events[i].Kind == "tool_call" {
			tool = &conversation.Events[i]
		}
	}
	if tool == nil || tool.Status != "cancelled" || tool.Partial {
		t.Fatalf("interrupted tool = %#v", tool)
	}
	if conversation.Activity == nil || conversation.Activity.Status != ProviderActivityInterrupted {
		t.Fatalf("interrupted lifecycle = %+v", conversation.Activity)
	}
}

// piLiveUserLine builds a version-3 Pi user message line.
func piLiveUserLine(id, parentID, timestamp, text string) string {
	return `{"type":"message","id":"` + id + `","parentId":` + piLiveParent(parentID) +
		`,"timestamp":"` + timestamp + `","message":{"role":"user","content":"` + text + `"}}`
}

// piLiveAssistantLine builds a version-3 Pi assistant line with an explicit
// content block list; when text is non-empty a text block is added.
func piLiveAssistantLine(id, parentID, timestamp, text, stopReason string, extraBlock string) string {
	blocks := extraBlock
	if text != "" {
		comma := ""
		if blocks != "" {
			comma = ","
		}
		blocks += comma + `{"type":"text","text":"` + text + `"}`
	}
	if blocks == "" {
		blocks = "[]"
	} else {
		blocks = "[" + blocks + "]"
	}
	return `{"type":"message","id":"` + id + `","parentId":` + piLiveParent(parentID) +
		`,"timestamp":"` + timestamp + `","message":{"role":"assistant","content":` + blocks + `,"stopReason":"` + stopReason + `"}}`
}

// piLiveToolResultLine builds a version-3 Pi toolResult message line.
func piLiveToolResultLine(id, parentID, timestamp, callID, toolName, output string, isError bool) string {
	return `{"type":"message","id":"` + id + `","parentId":` + piLiveParent(parentID) +
		`,"timestamp":"` + timestamp + `","message":{"role":"toolResult","toolCallId":"` + callID + `","toolName":"` + toolName +
		`","content":[{"type":"text","text":"` + output + `"}],"isError":` + boolString(isError) + `}}`
}

// piLiveParent renders a parentId JSON value (null or a quoted id).
func piLiveParent(parentID string) string {
	if strings.TrimSpace(parentID) == "" {
		return "null"
	}
	return `"` + parentID + `"`
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func writePiLiveFixture(t *testing.T, path, cwd, sessionID string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	header := `{"type":"session","version":3,"id":"` + sessionID + `","timestamp":"2026-08-07T09:59:00.000Z","cwd":"` + cwd + `"}`
	content := append([]string{header}, lines...)
	if err := os.WriteFile(path, []byte(strings.Join(content, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendPiLiveLines(t *testing.T, path string, lines []string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}
