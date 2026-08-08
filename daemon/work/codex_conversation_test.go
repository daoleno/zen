package work

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestProviderConversationReaderSelectsOnlyExplicitKnownProvider(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for _, provider := range []string{
		AgentProviderCodex,
		AgentProviderClaude,
		AgentProviderCursor,
		AgentProviderGrok,
	} {
		t.Run(provider, func(t *testing.T) {
			got, err := NewProviderConversationReader().Load(classifier.Agent{
				Name:    "opaque provider",
				Command: "/opt/bin/provider-wrapper",
			}, provider, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.Available || got.Reason != "missing_cwd" {
				t.Fatalf("conversation = %#v", got)
			}
		})
	}

	unknown, err := NewProviderConversationReader().Load(classifier.Agent{
		Name:    "claude",
		Command: "claude",
		Cwd:     "/repo",
	}, "unknown", now)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Available || unknown.Reason != "not_structured_agent" {
		t.Fatalf("unknown provider = %#v", unknown)
	}
}

func TestParseCodexConversation_PreservesLongCompletedAssistantMarkdown(t *testing.T) {
	const suffix = "ZEN_CODEX_SUFFIX_VERTICAL_SLICE_4b1c"
	path := filepath.Join(t.TempDir(), "rollout-long.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-long",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "write a long markdown answer"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": longCompletedAssistantMarkdown(suffix)},
				},
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	assertUncappedAssistantMarkdown(t, got.Events, suffix)
}

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
	if len(patch.FileChanges) != 1 || patch.FileChanges[0].Operation != "update" {
		t.Fatalf("patch file changes = %#v", patch.FileChanges)
	}
	if additions := patch.FileChanges[0].Additions; additions == nil || *additions != 1 {
		t.Fatalf("patch additions = %#v, want 1", additions)
	}
	if deletions := patch.FileChanges[0].Deletions; deletions == nil || *deletions != 0 {
		t.Fatalf("patch deletions = %#v, want 0", deletions)
	}
	previousSeq := 0
	for index, event := range got.Events {
		if event.Seq <= previousSeq {
			t.Fatalf("event %d seq = %d after %d", index, event.Seq, previousSeq)
		}
		previousSeq = event.Seq
		if strings.Contains(event.Body, "environment_context") {
			t.Fatalf("boilerplate leaked: %#v", event)
		}
		if strings.Contains(event.Body, "goal_context") {
			t.Fatalf("goal context leaked: %#v", event)
		}
	}
}

func TestParseCodexConversation_PreservesPatchStatsBeforeRawBodyTruncation(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("*** Begin Patch\n*** Update File: src/ledger/quote.ts\n@@\n")
	for index := 0; index < maxCodexConversationBody; index++ {
		patch.WriteString(" context line\n")
	}
	for index := 0; index < 5; index++ {
		patch.WriteString("-old synthetic line\n")
	}
	for index := 0; index < 9; index++ {
		patch.WriteString("+new synthetic line\n")
	}
	patch.WriteString("*** End Patch\n")

	path := filepath.Join(t.TempDir(), "rollout-patch.jsonl")
	writeJSONL(t, path, map[string]any{
		"type":      "response_item",
		"timestamp": "2026-05-20T10:00:00Z",
		"payload": map[string]any{
			"type":    "custom_tool_call",
			"name":    "apply_patch",
			"call_id": "call-patch-summary",
			"input":   patch.String(),
		},
	})

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %#v, want one patch", got.Events)
	}
	event := got.Events[0]
	if len(event.FileChanges) != 1 {
		t.Fatalf("file changes = %#v", event.FileChanges)
	}
	change := event.FileChanges[0]
	if change.Path != "src/ledger/quote.ts" || change.Operation != "update" {
		t.Fatalf("file change = %#v", change)
	}
	if change.Additions == nil || *change.Additions != 9 || change.Deletions == nil || *change.Deletions != 5 {
		t.Fatalf("file change stats = %#v, want +9 -5", change)
	}
	if strings.Contains(event.Body, "*** End Patch") {
		t.Fatalf("test did not cross raw body truncation boundary: body length %d", len(event.Body))
	}
}

func TestParseCodexConversation_PairsProviderUserAdmissionAndRenderingEchoByRecordOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-dedupe",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "Why does ChatUI flicker?",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:10Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Why does ChatUI flicker?"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:11Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will inspect the list rendering path."},
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
	assertEvent(t, got.Events[0], "user_message", "user", "", "Why does ChatUI flicker?")
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "I will inspect")
	wantAdmission := fmt.Sprintf("%x", sha256.Sum256([]byte("Why does ChatUI flicker?")))
	if got.Events[0].AdmissionSHA256 != wantAdmission {
		t.Fatalf("Codex admission digest = %q, want %q", got.Events[0].AdmissionSHA256, wantAdmission)
	}
}

func TestParseCodexConversation_KeepsIdenticalUserMessagesAcrossTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-identical-turns",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "repeat this",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "turn_complete",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "repeat this",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type": "turn_complete",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	var userEvents []CodexConversationEvent
	for _, event := range got.Events {
		if event.Kind == "user_message" {
			userEvents = append(userEvents, event)
		}
	}
	if len(userEvents) != 2 || userEvents[0].Body != "repeat this" || userEvents[1].Body != "repeat this" {
		t.Fatalf("identical cross-turn user echoes = %#v, want two durable rows", userEvents)
	}
	if userEvents[0].ID == userEvents[1].ID {
		t.Fatalf("identical cross-turn echoes reused identity: %#v", userEvents)
	}
}

func TestParseCodexConversation_HidesContextualUserFragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-context",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "<goal_context>\nContinue working toward the active goal.\n</goal_context>"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "<skills_instructions>\nUse configured skills.\n</skills_instructions>"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "# AGENTS.md instructions for /repo\n\n<INSTRUCTIONS>\nUse repo conventions.\n</INSTRUCTIONS>"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "<user_shell_command>\n<command>\ndate\n</command>\n<result>\nOutput:\nnow\n</result>\n</user_shell_command>"},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:05Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Please keep this visible, even with <xml>inline</xml> text."},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:06Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Visible before\n\n<goal_context>\nHidden goal context.\n</goal_context>\n\nVisible after"},
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
	assertEvent(t, got.Events[0], "user_message", "user", "", "Please keep this visible")
	assertEvent(t, got.Events[1], "user_message", "user", "", "Visible before")
	if strings.Contains(got.Events[1].Body, "goal_context") ||
		strings.Contains(got.Events[1].Body, "Hidden goal context") ||
		!strings.Contains(got.Events[1].Body, "Visible after") {
		t.Fatalf("inline context was not stripped correctly: %#v", got.Events[1])
	}
}

func TestParseCodexConversation_HidesInstructionContextFragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-instructions",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "## Project Structure & Module Organization\n- Keep source in apps/web/src.\n\n## Build, Test, and Development Commands\n- bun run test\n\n## Agent & Sandbox Releases\n- Public product/API surface uses Agent names.\n\n## Testing Guidelines\n- Tests are colocated with source."},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Hi. What do you want to work on in /repo?"},
				},
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
	assertEvent(t, got.Events[0], "user_message", "user", "", "Hi. What do you want to work on")
	if strings.Contains(got.Events[0].Body, "Agent & Sandbox Releases") {
		t.Fatalf("instruction context leaked: %#v", got.Events[0])
	}
}

func TestParseCodexConversation_StripsContextFromNonMessageEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-context-events",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "read_file",
				"call_id":   "call-context-tool",
				"arguments": "<environment_context>\nHidden cwd.\n</environment_context>",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-context-tool",
				"output":  "Visible before\n\n<skills_instructions>\nHidden skill context.\n</skills_instructions>\n\nVisible after",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type":        "plan_update",
				"explanation": "<goal_context>\nHidden goal.\n</goal_context>",
				"plan": []map[string]any{
					{"step": "<permissions instructions>\nHidden permissions.\n</permissions instructions>", "status": "pending"},
					{"step": "Visible step", "status": "in_progress"},
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
	tool := got.Events[0]
	if tool.Kind != "tool" || tool.Input != "" || !strings.Contains(tool.Output, "Visible before") || !strings.Contains(tool.Output, "Visible after") {
		t.Fatalf("tool context cleanup = %#v", tool)
	}
	if strings.Contains(tool.Output, "skills_instructions") || strings.Contains(tool.Output, "Hidden skill context") {
		t.Fatalf("tool context leaked: %#v", tool)
	}
	plan := got.Events[1]
	if plan.Kind != "plan" || plan.Explanation != "" || len(plan.Plan) != 1 || plan.Plan[0].Step != "Visible step" {
		t.Fatalf("plan context cleanup = %#v", plan)
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

func TestParseCodexConversation_RendersCodexErrorAndStreamErrorStatuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":               "stream_error",
				"message":            "Reconnecting... 2/5",
				"additional_details": "Idle timeout waiting for SSE",
				"codex_error_info": map[string]any{
					"response_stream_disconnected": map[string]any{
						"http_status_code": 524,
					},
				},
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "error",
				"message": "unexpected status 429 Too Many Requests: rate limit reached",
				"codex_error_info": map[string]any{
					"http_connection_failed": map[string]any{
						"http_status_code": 429,
					},
				},
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if got.Activity != nil && got.Activity.Status == ProviderActivityRunning {
		t.Fatalf("fatal error left Activity running: %#v", got.Activity)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}
	stream := got.Events[0]
	if stream.Kind != "status" || stream.Title != "Reconnecting... 2/5" || stream.Status != "running" {
		t.Fatalf("stream status = %#v", stream)
	}
	if !strings.Contains(stream.Body, "Idle timeout") {
		t.Fatalf("stream body = %q, want additional details", stream.Body)
	}
	errEvent := got.Events[1]
	if errEvent.Kind != "status" || errEvent.Title != "Codex error" || errEvent.Status != "failed" {
		t.Fatalf("error status = %#v", errEvent)
	}
	if !strings.Contains(errEvent.Body, "429") || !strings.Contains(errEvent.Body, "rate limit") {
		t.Fatalf("error body = %q, want response error text", errEvent.Body)
	}
}

func TestParseCodexConversation_RendersReachedRateLimitSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type": "token_count",
				"rate_limits": map[string]any{
					"limit_id":                "codex",
					"plan_type":               "team",
					"rate_limit_reached_type": "workspace_owner_usage_limit_reached",
					"primary": map[string]any{
						"used_percent":   100,
						"window_minutes": 300,
					},
				},
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
	event := got.Events[0]
	if event.Kind != "status" || event.Title != "Workspace usage limit reached" || event.Status != "failed" {
		t.Fatalf("rate limit status = %#v", event)
	}
	if !strings.Contains(event.Body, "Limit: codex") || !strings.Contains(event.Body, "100% used") {
		t.Fatalf("rate limit body = %q", event.Body)
	}
}

func TestParseCodexConversation_DoesNotEndTurnForNonFatalCodexErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type": "event_msg", "timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type": "task_started",
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":    "error",
				"message": "Cannot steer this active turn.",
				"codex_error_info": map[string]any{
					"active_turn_not_steerable": map[string]any{
						"turn_kind": "review",
					},
				},
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("Activity = %#v, want running for non-fatal error", got.Activity)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
	}
	if got.Events[0].Kind != "status" || got.Events[0].Status != "failed" {
		t.Fatalf("error event = %#v", got.Events[0])
	}
}

func TestParseCodexConversation_RendersResponseItemWebSearchCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":   "web_search_call",
				"id":     "ws-search",
				"status": "completed",
				"action": map[string]any{
					"type":  "search",
					"query": "Hyperliquid fees maker taker schedule",
				},
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
	event := got.Events[0]
	if event.Kind != "web_search" || event.CallID != "ws-search" || event.Status != "done" {
		t.Fatalf("web search event = %#v", event)
	}
	if event.Body != "Hyperliquid fees maker taker schedule" || !strings.Contains(event.Input, `"type": "search"`) {
		t.Fatalf("web search payload = %#v", event)
	}
}

func TestParseCodexConversation_DedupesWebSearchEventMsgAndResponseItemPair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	action := map[string]any{
		"type": "open_page",
		"url":  "https://docs.example.com/guide",
	}
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":    "web_search_end",
				"call_id": "ws-open",
				"query":   "https://docs.example.com/guide",
				"action":  action,
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":   "web_search_call",
				"status": "completed",
				"action": action,
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
	event := got.Events[0]
	if event.Kind != "web_search" || event.CallID != "ws-open" || event.Body != "https://docs.example.com/guide" {
		t.Fatalf("web search event = %#v", event)
	}
}

func TestParseCodexConversation_UpdatesWebSearchBeginWithEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":    "web_search_begin",
				"call_id": "ws-find",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":    "web_search_end",
				"call_id": "ws-find",
				"query":   "'needle' in https://docs.example.com/guide",
				"action": map[string]any{
					"type":    "find_in_page",
					"url":     "https://docs.example.com/guide",
					"pattern": "needle",
				},
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
	event := got.Events[0]
	if event.Kind != "web_search" || event.Status != "done" || event.Body != "'needle' in https://docs.example.com/guide" {
		t.Fatalf("web search event = %#v", event)
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
	if len(got.Events) != 1 {
		t.Fatalf("events len = %d, want latest tail event: %#v", len(got.Events), got.Events)
	}
	assertEvent(t, got.Events[0], "user_message", "user", "", "latest prompt")
}

func TestCodexConversationSeqStableAcrossTrim(t *testing.T) {
	builder := newCodexConversationBuilder("rollout.jsonl")
	for index := 1; index <= maxCodexConversationEvents+2; index++ {
		builder.addEvent(CodexConversationEvent{
			ID:   builder.eventID(index),
			Kind: "assistant_message",
			Role: "assistant",
			Body: fmt.Sprintf("event %d", index),
		})
	}

	got := builder.conversation()
	if len(got.Events) != maxCodexConversationEvents {
		t.Fatalf("events len = %d, want %d", len(got.Events), maxCodexConversationEvents)
	}
	if got.Events[0].Seq != 3 || !strings.HasSuffix(got.Events[0].ID, ":3") {
		t.Fatalf("first event = %#v, want stable seq/id 3 after trim", got.Events[0])
	}
	last := got.Events[len(got.Events)-1]
	if last.Seq != maxCodexConversationEvents+2 {
		t.Fatalf("last seq = %d, want %d", last.Seq, maxCodexConversationEvents+2)
	}
}

func TestParseCodexConversation_KeepsCodexHistoryEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "session_meta",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"id":  "codex-history",
				"cwd": "/repo",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "/status",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":    "history_entry",
				"message": "Model: gpt-5\nApproval: never\nAgents.md: /repo/AGENTS.md",
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want command echo and native status: %#v", len(got.Events), got.Events)
	}
	command := got.Events[0]
	if command.Kind != "user_message" || command.Body != "/status" {
		t.Fatalf("command event = %#v, want durable user echo", command)
	}
	event := got.Events[1]
	if event.Kind != "status" || !strings.Contains(event.Body, "Model: gpt-5") {
		t.Fatalf("history entry event = %#v, want status with native output", event)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("slash command lifecycle = %#v, want completed", got.Activity)
	}
}

func TestParseCodexConversation_RendersAgentReasoningAsRunningCommentary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "agent_reasoning",
				"text": "**Checking context**\n\nReading the current workspace.",
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
	event := got.Events[0]
	if event.Kind != "commentary" || event.Title != "Reasoning" || event.Status != "running" {
		t.Fatalf("reasoning event = %#v, want running commentary", event)
	}
	if !strings.Contains(event.Body, "Checking context") {
		t.Fatalf("reasoning body = %q, want text", event.Body)
	}
}

func TestParseCodexConversation_MergesReasoningUpdatesIntoSingleEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type": "agent_reasoning",
				"text": "Drafting the next step.",
			},
		},
		map[string]any{
			"type":      "event_msg",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type": "agent_reasoning",
				"text": "Drafting the next step.\n\nRefining the plan.",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type": "reasoning",
				"summary": []map[string]any{
					{"type": "summary_text", "text": "Drafting the next step.\n\nRefining the plan.\n\nReady to continue."},
				},
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "Done."},
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
	reasoning := got.Events[0]
	if reasoning.Kind != "commentary" || reasoning.Title != "Reasoning" || reasoning.Status != "done" {
		t.Fatalf("reasoning event = %#v", reasoning)
	}
	if !strings.Contains(reasoning.Body, "Ready to continue.") {
		t.Fatalf("reasoning body = %q, want final summary", reasoning.Body)
	}
	assistant := got.Events[1]
	if assistant.Kind != "assistant_message" || assistant.Body != "Done." {
		t.Fatalf("assistant event = %#v", assistant)
	}
}

func TestParseCodexConversation_ProgressiveReasoningKeepsIdentityUntilFinalized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	firstUpdate := map[string]any{
		"type":      "event_msg",
		"timestamp": "2026-05-20T10:00:01Z",
		"payload": map[string]any{
			"type": "agent_reasoning",
			"text": "Inspecting the provider transcript.",
		},
	}
	secondUpdate := map[string]any{
		"type":      "event_msg",
		"timestamp": "2026-05-20T10:00:02Z",
		"payload": map[string]any{
			"type": "agent_reasoning",
			"text": "Tracing the structured event path.",
		},
	}
	finalUpdate := map[string]any{
		"type":      "response_item",
		"timestamp": "2026-05-20T10:00:03Z",
		"payload": map[string]any{
			"type": "reasoning",
			"summary": []map[string]any{
				{"type": "summary_text", "text": "Inspecting the provider transcript.\n\nTracing the structured event path.\n\nReady."},
			},
		},
	}

	writeJSONL(t, path, firstUpdate)
	first, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if len(first.Events) != 1 || !first.Events[0].Partial || first.Events[0].Status != "running" {
		t.Fatalf("first reasoning update = %#v, want partial running event", first.Events)
	}
	stableID := first.Events[0].ID

	writeJSONL(t, path, firstUpdate, secondUpdate)
	second, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != stableID || !second.Events[0].Partial || second.Events[0].Status != "running" {
		t.Fatalf("second reasoning update = %#v, want same partial event identity %q", second.Events, stableID)
	}
	if !strings.Contains(second.Events[0].Body, "Inspecting the provider transcript") ||
		!strings.Contains(second.Events[0].Body, "Tracing the structured event path") {
		t.Fatalf("second reasoning body = %q", second.Events[0].Body)
	}

	writeJSONL(t, path, firstUpdate, secondUpdate, finalUpdate)
	final, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("final parse: %v", err)
	}
	if len(final.Events) != 1 || final.Events[0].ID != stableID || final.Events[0].Partial || final.Events[0].Status != "done" {
		t.Fatalf("final reasoning update = %#v, want finalized replacement with ID %q", final.Events, stableID)
	}
	if !strings.Contains(final.Events[0].Body, "Ready.") {
		t.Fatalf("final reasoning body = %q", final.Events[0].Body)
	}
}

func TestParseCodexConversation_FinalizesPendingReasoningWhenTurnEnds(t *testing.T) {
	for _, terminalEvent := range []string{"task_complete", "turn_aborted"} {
		t.Run(terminalEvent, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), terminalEvent+".jsonl")
			writeJSONL(t, path,
				map[string]any{
					"type":      "event_msg",
					"timestamp": "2026-05-20T10:00:01Z",
					"payload": map[string]any{
						"type": "task_started",
					},
				},
				map[string]any{
					"type":      "event_msg",
					"timestamp": "2026-05-20T10:00:02Z",
					"payload": map[string]any{
						"type": "agent_reasoning",
						"text": "Checking the final transcript state.",
					},
				},
				map[string]any{
					"type":      "event_msg",
					"timestamp": "2026-05-20T10:00:03Z",
					"payload": map[string]any{
						"type": terminalEvent,
					},
				},
			)

			got, err := parseCodexConversation(path)
			if err != nil {
				t.Fatalf("parseCodexConversation: %v", err)
			}
			wantStatus := ProviderActivityCompleted
			if terminalEvent == "turn_aborted" {
				wantStatus = ProviderActivityInterrupted
			}
			if got.Activity == nil || got.Activity.Status != wantStatus || got.Activity.StartedAt != "2026-05-20T10:00:01Z" || got.Activity.SettledAt != "2026-05-20T10:00:03Z" {
				t.Fatalf("turn = %#v, want stable terminal lifecycle", got.Activity)
			}
			if len(got.Events) != 1 {
				t.Fatalf("events len = %d, want 1: %#v", len(got.Events), got.Events)
			}
			reasoning := got.Events[0]
			if reasoning.Kind != "commentary" || reasoning.Status != "done" {
				t.Fatalf("reasoning event = %#v, want finalized commentary", reasoning)
			}
			if !strings.Contains(reasoning.Body, "final transcript state") {
				t.Fatalf("reasoning body = %q", reasoning.Body)
			}
		})
	}
}

func TestParseCodexConversation_TracksProviderActivityFromNativeLifecycleEvents(t *testing.T) {
	t.Run("running Activity", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "running.jsonl")
		writeJSONL(t, path,
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:00Z",
				"payload": map[string]any{
					"type":    "task_started",
					"turn_id": "turn-running",
				},
			},
		)

		got, err := parseCodexConversation(path)
		if err != nil {
			t.Fatalf("parseCodexConversation: %v", err)
		}
		if got.Activity == nil || got.Activity.Status != ProviderActivityRunning ||
			!strings.Contains(got.Activity.ID, "turn-running") || got.Activity.StartedAt != "2026-05-20T10:00:00Z" {
			t.Fatalf("turn = %#v, want pre-token running lifecycle", got.Activity)
		}
	})

	t.Run("assistant response cannot complete turn when lifecycle end is missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-complete.jsonl")
		writeJSONL(t, path,
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:00Z",
				"payload": map[string]any{
					"type":    "task_started",
					"turn_id": "turn-missing-complete",
				},
			},
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:01Z",
				"payload": map[string]any{
					"type":    "user_message",
					"message": "hi",
				},
			},
			map[string]any{
				"type":      "response_item",
				"timestamp": "2026-05-20T10:00:02Z",
				"payload": map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "Hello."},
					},
				},
			},
		)

		got, err := parseCodexConversation(path)
		if err != nil {
			t.Fatalf("parseCodexConversation: %v", err)
		}
		if got.Activity == nil || got.Activity.Status != ProviderActivityRunning || got.Activity.SettledAt != "" {
			t.Fatalf("assistant rendering metadata settled lifecycle: %#v", got.Activity)
		}
	})

	t.Run("new native turn replaces an interrupted running turn", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "interrupted-then-new.jsonl")
		writeJSONL(t, path,
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:00Z",
				"payload": map[string]any{
					"type":    "task_started",
					"turn_id": "turn-interrupted",
				},
			},
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:05:00Z",
				"payload": map[string]any{
					"type":    "task_started",
					"turn_id": "turn-current",
				},
			},
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:05:03Z",
				"payload": map[string]any{
					"type":    "task_complete",
					"turn_id": "turn-current",
				},
			},
		)

		got, err := parseCodexConversation(path)
		if err != nil {
			t.Fatalf("parseCodexConversation: %v", err)
		}
		if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted ||
			got.Activity.StartedAt != "2026-05-20T10:05:00Z" ||
			got.Activity.SettledAt != "2026-05-20T10:05:03Z" ||
			!strings.Contains(got.Activity.ID, "turn-current") {
			t.Fatalf("current turn Activity = %#v", got.Activity)
		}
	})

	for _, terminalEvent := range []string{"task_complete", "turn_aborted"} {
		t.Run(terminalEvent, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), terminalEvent+".jsonl")
			writeJSONL(t, path,
				map[string]any{
					"type":      "event_msg",
					"timestamp": "2026-05-20T10:00:00Z",
					"payload": map[string]any{
						"type":    "task_started",
						"turn_id": "turn-settled",
					},
				},
				map[string]any{
					"type":      "event_msg",
					"timestamp": "2026-05-20T10:00:03Z",
					"payload": map[string]any{
						"type":    terminalEvent,
						"turn_id": "turn-settled",
					},
				},
			)

			got, err := parseCodexConversation(path)
			if err != nil {
				t.Fatalf("parseCodexConversation: %v", err)
			}
			wantStatus := ProviderActivityCompleted
			if terminalEvent == "turn_aborted" {
				wantStatus = ProviderActivityInterrupted
			}
			if got.Activity == nil || got.Activity.Status != wantStatus || got.Activity.StartedAt != "2026-05-20T10:00:00Z" || got.Activity.SettledAt != "2026-05-20T10:00:03Z" {
				t.Fatalf("turn = %#v", got.Activity)
			}
		})
	}

	t.Run("provider id arriving after user row preserves fallback identity and settles it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "late-provider-id.jsonl")
		writeJSONL(t, path,
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:00Z",
				"payload":   map[string]any{"type": "user_message", "message": "go"},
			},
		)
		before, err := parseCodexConversation(path)
		if err != nil {
			t.Fatal(err)
		}
		if before.Activity == nil || before.Activity.Status != ProviderActivityRunning {
			t.Fatalf("before = %#v", before.Activity)
		}

		writeJSONL(t, path,
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:00Z",
				"payload":   map[string]any{"type": "user_message", "message": "go"},
			},
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:01Z",
				"payload":   map[string]any{"type": "turn_started", "turn_id": "native-turn"},
			},
			map[string]any{
				"type":      "event_msg",
				"timestamp": "2026-05-20T10:00:02Z",
				"payload":   map[string]any{"type": "turn_complete", "turn_id": "native-turn"},
			},
		)
		after, err := parseCodexConversation(path)
		if err != nil {
			t.Fatal(err)
		}
		if after.Activity == nil || after.Activity.ID != before.Activity.ID || after.Activity.StartedAt != before.Activity.StartedAt ||
			after.Activity.Status != ProviderActivityCompleted || after.Activity.SettledAt != "2026-05-20T10:00:02Z" {
			t.Fatalf("late provider correlation changed or failed to settle turn: before=%#v after=%#v", before.Activity, after.Activity)
		}
	})
}

func assertEvent(t *testing.T, event CodexConversationEvent, kind, role, title, bodyPart string) {
	t.Helper()
	if event.Kind != kind || event.Role != role || event.Title != title || !strings.Contains(event.Body, bodyPart) {
		t.Fatalf("event = %#v, want kind=%s role=%s title=%s body~%s", event, kind, role, title, bodyPart)
	}
}
