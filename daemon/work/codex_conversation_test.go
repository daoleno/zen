package work

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexConversation_BuildsNativeTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-1",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "<environment_context><cwd>/repo</cwd></environment_context>",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "做一个 native Codex chat render"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "我会先读取 rollout，再保留终端兜底。"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "exec_command",
				"call_id":   "call-test",
				"arguments": `{"cmd":"go test ./daemon/work"}`,
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:05Z",
			"payload": map[string]any{
				"type":              "exec_command_end",
				"call_id":           "call-test",
				"exit_code":         1,
				"aggregated_output": "FAIL\nerror: boom",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:06Z",
			"payload": map[string]any{
				"type":  "custom_tool_call",
				"name":  "apply_patch",
				"input": "*** Begin Patch\n*** Update File: app/app/terminal/TerminalScreenImpl.tsx\n@@\n+chat\n*** End Patch\n",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if !got.Available {
		t.Fatal("conversation should be available")
	}
	if got.SessionID != "codex-1" || got.CWD != "/repo" {
		t.Fatalf("metadata = (%q, %q), want codex-1 /repo", got.SessionID, got.CWD)
	}
	if len(got.Events) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got.Events), got.Events)
	}
	assertEvent(t, got.Events[0], "user_message", "user", "", "做一个 native Codex chat render")
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "我会先读取 rollout")

	command := got.Events[2]
	if command.Kind != "command" || command.Command != "go test ./daemon/work" {
		t.Fatalf("command event = %#v", command)
	}
	if command.Status != "failed" || command.ExitCode == nil || *command.ExitCode != 1 {
		t.Fatalf("command status = %#v", command)
	}
	if !strings.Contains(command.Body, "error: boom") {
		t.Fatalf("command body missing output: %#v", command)
	}

	patch := got.Events[3]
	if patch.Kind != "patch" || len(patch.Files) != 1 || patch.Files[0] != "app/app/terminal/TerminalScreenImpl.tsx" {
		t.Fatalf("patch event = %#v", patch)
	}
	for index, event := range got.Events {
		if event.Seq != index+1 {
			t.Fatalf("event %d seq = %d", index, event.Seq)
		}
		if strings.Contains(event.Body, "environment_context") {
			t.Fatalf("boilerplate leaked: %#v", event)
		}
	}
}

func TestParseCodexConversation_DeNoisesCodexInternalEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-2",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "我会补齐 chat input 和滚动体验。"},
				},
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":    "agent_message",
				"phase":   "final",
				"message": "我会补齐 chat input 和滚动体验。",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type": "thread_goal_updated",
				"goal": map[string]any{
					"status":    "in_progress",
					"objective": "polish native chat render",
				},
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type": "thread_goal_updated",
				"goal": map[string]any{
					"status":    "in_progress",
					"objective": "polish native chat render",
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:05Z",
			"payload": map[string]any{
				"type":    "custom_tool_call",
				"name":    "apply_patch",
				"call_id": "call-patch",
				"input":   "*** Begin Patch\n*** Update File: app/components/terminal/CodexChatSurface.tsx\n@@\n+chat\n*** End Patch\n",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:06Z",
			"payload": map[string]any{
				"type":   "patch_apply_end",
				"status": "success",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:07Z",
			"payload": map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call-patch",
				"output":  "Patch applied successfully",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}
	assertEvent(t, got.Events[0], "assistant_message", "assistant", "", "chat input")
	if got.Events[1].Kind != "patch" || got.Events[1].CallID != "call-patch" {
		t.Fatalf("event[1] = %#v, want patch", got.Events[1])
	}
	for _, event := range got.Events {
		if event.Title == "Goal updated" || event.Title == "Patch applied" || event.Kind == "tool" {
			t.Fatalf("low-signal event leaked: %#v", event)
		}
		if strings.Contains(event.Output, "Patch applied successfully") || strings.Contains(event.Body, "Patch applied successfully") {
			t.Fatalf("apply_patch acknowledgement leaked: %#v", event)
		}
	}
}

func TestParseCodexConversation_RendersToolsAndReasoning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-3",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "reasoning",
				"summary": []map[string]any{
					{"type": "summary_text", "text": "Checking tool rendering"},
					{"type": "summary_text", "text": "Need to cover generic tool output."},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "shell_command",
				"call_id":   "call-shell",
				"arguments": `{"command":["bash","-lc","pwd && git status"],"workdir":"/repo"}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-shell",
				"output":  "Exit code: 0\nWall time: 0 seconds\nOutput:\nok",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "view_image",
				"call_id":   "call-image",
				"arguments": `{"path":"/tmp/screen.png","detail":"original"}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:05Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-image",
				"output":  "image rendered",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:06Z",
			"payload": map[string]any{
				"type":  "custom_tool_call",
				"name":  "browser_click",
				"input": `{"target":"@e3","element":"Send"}`,
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got.Events), got.Events)
	}

	reasoning := got.Events[0]
	if reasoning.Kind != "commentary" || reasoning.Title != "Reasoning" || !strings.Contains(reasoning.Body, "generic tool output") {
		t.Fatalf("reasoning event = %#v", reasoning)
	}

	command := got.Events[1]
	if command.Kind != "command" || command.Command != "pwd && git status" {
		t.Fatalf("legacy command event = %#v", command)
	}
	if command.Status != "done" || command.ExitCode == nil || *command.ExitCode != 0 || !strings.Contains(command.Body, "ok") {
		t.Fatalf("legacy command completion = %#v", command)
	}

	tool := got.Events[2]
	if tool.Kind != "tool" || tool.ToolName != "view_image" || tool.Status != "done" {
		t.Fatalf("tool event = %#v", tool)
	}
	if !strings.Contains(tool.Input, "screen.png") || !strings.Contains(tool.Output, "image rendered") {
		t.Fatalf("tool payload = %#v", tool)
	}

	custom := got.Events[3]
	if custom.Kind != "tool" || custom.ToolName != "browser_click" || custom.Status != "done" || !strings.Contains(custom.Input, "@e3") {
		t.Fatalf("custom tool event = %#v", custom)
	}
}

func TestParseCodexConversation_RendersUpdatePlanAsTodoListEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":    "function_call",
				"name":    "update_plan",
				"call_id": "call-plan",
				"arguments": `{"explanation":"Tracking the UI pass.","plan":[` +
					`{"step":"Study Codex renderer","status":"completed"},` +
					`{"step":"Port plan rows","status":"in_progress"},` +
					`{"step":"Build APK","status":"pending"}]}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-plan",
				"output":  "Plan updated",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	plan := got.Events[0]
	if plan.Kind != "plan" || plan.Title != "Updated Plan" || plan.Explanation != "Tracking the UI pass." {
		t.Fatalf("plan event = %#v", plan)
	}
	if len(plan.Plan) != 3 {
		t.Fatalf("plan steps = %#v", plan.Plan)
	}
	if plan.Plan[0].Status != "completed" || plan.Plan[1].Status != "in_progress" || plan.Plan[2].Status != "pending" {
		t.Fatalf("plan statuses = %#v", plan.Plan)
	}
	if strings.Contains(plan.Output, "Plan updated") || plan.Kind == "tool" {
		t.Fatalf("plan leaked as tool output: %#v", plan)
	}
}

func TestParseCodexConversation_CleansExecCommandOutputEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "functions.exec_command",
				"call_id":   "call-exec",
				"arguments": `{"cmd":"bun test"}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-exec",
				"output":  "Chunk ID: abc123\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 42\nOutput:\nPASS app tests",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	command := got.Events[0]
	if command.Kind != "command" || command.Command != "bun test" {
		t.Fatalf("command event = %#v", command)
	}
	if command.Status != "done" || command.ExitCode == nil || *command.ExitCode != 0 {
		t.Fatalf("command completion = %#v", command)
	}
	if command.Body != "PASS app tests" {
		t.Fatalf("command body = %q, want clean output", command.Body)
	}
}

func TestParseCodexConversation_HidesExecCommandOutputEnvelopeWhenOutputEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "functions.exec_command",
				"call_id":   "call-exec",
				"arguments": `{"cmd":"true"}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-exec",
				"output":  "Chunk ID: abc123\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 0\nOutput:\n",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	command := got.Events[0]
	if command.Status != "done" || command.ExitCode == nil || *command.ExitCode != 0 {
		t.Fatalf("command completion = %#v", command)
	}
	if command.Body != "" {
		t.Fatalf("command body = %q, want no executor metadata", command.Body)
	}
}

func TestParseCodexConversation_CompletesLongRunningExecFromWriteStdinPoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "exec_command",
				"call_id":   "call-build",
				"arguments": `{"cmd":"./gradlew assembleDebug","yield_time_ms":1000}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-build",
				"output":  "Chunk ID: build-1\nWall time: 1.0010 seconds\nProcess running with session ID 98430\nOriginal token count: 3\nOutput:\nstarting",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "write_stdin",
				"call_id":   "call-poll",
				"arguments": `{"session_id":98430,"chars":"","yield_time_ms":30000}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-poll",
				"output":  "Chunk ID: build-2\nWall time: 7.8274 seconds\nProcess exited with code 0\nOriginal token count: 4\nOutput:\nBUILD SUCCESSFUL",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	var command *CodexConversationEvent
	for index := range got.Events {
		if got.Events[index].Kind == "command" {
			command = &got.Events[index]
			break
		}
	}
	if command == nil {
		t.Fatalf("missing command event: %#v", got.Events)
	}
	if command.Status != "done" || command.ExitCode == nil || *command.ExitCode != 0 {
		t.Fatalf("command completion = %#v", command)
	}
	if command.Body != "BUILD SUCCESSFUL" {
		t.Fatalf("command body = %q, want final poll output", command.Body)
	}
	if strings.Contains(command.Body, "Process running") || strings.Contains(command.Body, "Chunk ID") {
		t.Fatalf("command metadata leaked: %#v", command)
	}
}

func TestParseCodexConversation_MarksGenericToolFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "view_image",
				"call_id":   "call-bad-tool",
				"arguments": `{"path":"/tmp/missing.png"}`,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-bad-tool",
				"output":  "Error: file not found",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	if got.Events[0].Kind != "tool" || got.Events[0].ToolName != "view_image" || got.Events[0].Status != "failed" {
		t.Fatalf("tool failure event = %#v", got.Events[0])
	}
}

func TestParseCodexConversation_RetainsEventsAcrossLargeRollout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": strings.Repeat("x", maxCodexConversationRead+1024)},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "latest prompt"},
				},
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}
	assertEvent(t, got.Events[0], "assistant_message", "assistant", "", "xxxxx")
	assertEvent(t, got.Events[1], "user_message", "user", "", "latest prompt")
}

func assertEvent(t *testing.T, event CodexConversationEvent, kind, role, title, bodyPart string) {
	t.Helper()
	if event.Kind != kind || event.Role != role || event.Title != title || !strings.Contains(event.Body, bodyPart) {
		t.Fatalf("event = %#v, want kind=%s role=%s title=%s body~%s", event, kind, role, title, bodyPart)
	}
}
