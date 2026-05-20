package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummarizeCodexTranscript_ExtractsWorkflowSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type": "session_meta",
			"payload": map[string]any{
				"id":         "codex-1",
				"cwd":        "/repo",
				"originator": "codex-tui",
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "<environment_context><cwd>/repo</cwd></environment_context>",
			},
		},
		map[string]any{
			"type": "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "修复 Brain 让它读 Codex session"},
				},
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "你咋分析的？感觉还是很浅，重新读 session",
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": "我会改成读取原生 transcript，并把终端输出降级成兜底。",
			},
		},
		map[string]any{
			"type": "response_item",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "exec_command",
				"call_id":   "call-test",
				"arguments": `{"cmd":"go test ./work"}`,
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":              "exec_command_end",
				"call_id":           "call-test",
				"exit_code":         1,
				"aggregated_output": "--- FAIL: TestTranscript\nerror: boom",
			},
		},
		map[string]any{
			"type": "response_item",
			"payload": map[string]any{
				"type":  "custom_tool_call",
				"name":  "apply_patch",
				"input": "*** Begin Patch\n*** Update File: daemon/work/tool_transcript.go\n@@\n+change\n*** End Patch\n",
			},
		},
		map[string]any{
			"type": "response_item",
			"payload": map[string]any{
				"type":  "custom_tool_call",
				"name":  "apply_patch",
				"input": "*** Begin Patch\n*** Update File: daemon/work/tool_transcript.go\n@@\n+second change\n*** End Patch\n",
			},
		},
	)

	got, err := summarizeCodexTranscript(path)
	if err != nil {
		t.Fatalf("summarizeCodexTranscript: %v", err)
	}
	for _, want := range []string{
		"user_turns=2",
		"failures=1",
		"test_runs=1",
		"user_corrections=1",
		"Repeated work surfaces: daemon/work/tool_transcript.go x2",
		"User: 修复 Brain 让它读 Codex session",
		"User: 你咋分析的？感觉还是很浅，重新读 session",
		"Assistant: 我会改成读取原生 transcript",
		"Command exit=1: go test ./work | error: boom",
		"Tool: apply_patch daemon/work/tool_transcript.go",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "environment_context") {
		t.Fatalf("boilerplate leaked into summary:\n%s", got)
	}
}

func TestSummarizeClaudeTranscript_ExtractsWorkflowSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "system",
			"cwd":       "/repo",
			"sessionId": "claude-1",
		},
		map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "这个分析不对，重新读 Codex session"},
				},
			},
		},
		map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "我会直接读取 JSONL，并提取工具链信号。"},
					{"type": "tool_use", "name": "Skill", "input": map[string]any{"skill": "superpowers:brainstorming"}},
					{"type": "tool_use", "name": "AskUserQuestion", "input": map[string]any{"questions": []map[string]any{{"header": "Scope", "question": "分析范围是什么？"}}}},
					{"type": "tool_use", "name": "TaskCreate", "input": map[string]any{"subject": "Read Claude transcript", "description": "Extract native Claude Code workflow signals"}},
					{"type": "tool_use", "name": "TaskUpdate", "input": map[string]any{"taskId": "1", "status": "in_progress"}},
					{"type": "tool_use", "name": "TaskUpdate", "input": map[string]any{"taskId": "1", "status": "completed"}},
					{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "/repo/daemon/work/tool_transcript.go"}},
					{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": "/repo/daemon/work/tool_transcript.go"}},
					{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "go test ./work"}},
				},
			},
		},
		map[string]any{
			"type":       "last-prompt",
			"lastPrompt": "这个分析不对，重新读 Claude Code session",
			"sessionId":  "claude-1",
		},
		map[string]any{
			"type":       "last-prompt",
			"lastPrompt": "这个分析不对，重新读 Claude Code session",
			"sessionId":  "claude-1",
		},
		map[string]any{
			"type":           "permission-mode",
			"permissionMode": "bypassPermissions",
			"sessionId":      "claude-1",
		},
		map[string]any{
			"type": "file-history-snapshot",
			"snapshot": map[string]any{
				"trackedFileBackups": map[string]any{
					"daemon/work/tool_transcript.go": map[string]any{"version": 1},
				},
			},
		},
		map[string]any{
			"type": "attachment",
			"attachment": map[string]any{
				"type": "task_reminder",
			},
		},
		map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "tool_result", "is_error": true, "content": "Error: missing file"},
				},
			},
		},
	)

	got, err := summarizeClaudeTranscript(path)
	if err != nil {
		t.Fatalf("summarizeClaudeTranscript: %v", err)
	}
	for _, want := range []string{
		"user_turns=1",
		"tool_calls=8",
		"failures=1",
		"edits=1",
		"test_runs=1",
		"user_corrections=1",
		"user_clarifications=2",
		"plan_creates=1",
		"plan_updates=2",
		"skill_uses=1",
		"permission_modes=1",
		"file_snapshots=1",
		"hook_events=1",
		"Repeated work surfaces: /repo/daemon/work/tool_transcript.go x2",
		"Repeated user prompt: 这个分析不对，重新读 Claude Code session x2",
		"User: 这个分析不对，重新读 Codex session",
		"Assistant: 我会直接读取 JSONL",
		"Tool: Skill superpowers:brainstorming",
		"Tool: AskUserQuestion questions: Scope",
		"Tool: TaskCreate Read Claude transcript",
		"Tool: TaskUpdate 1 in_progress",
		"Tool: TaskUpdate 1 completed",
		"Tool: Read /repo/daemon/work/tool_transcript.go",
		"Tool: Edit /repo/daemon/work/tool_transcript.go",
		"Tool: Bash go test ./work",
		"Prompt: 这个分析不对，重新读 Claude Code session",
		"Permission mode: bypassPermissions",
		"Task reminder",
		"Tool result failed: Error: missing file",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestTranscriptCWDCandidates_UsesNearestGitRoot(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "daemon", "work")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir): %v", err)
	}

	got := transcriptCWDCandidates(subdir)
	if len(got) != 2 {
		t.Fatalf("candidates = %#v, want subdir and git root", got)
	}
	if got[0] != subdir || got[1] != root {
		t.Fatalf("candidates = %#v, want [%q %q]", got, subdir, root)
	}
}

func TestLoadClaudeTranscript_FallsBackToGitRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	subdir := filepath.Join(repo, "daemon")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir): %v", err)
	}

	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(repo))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir): %v", err)
	}
	path := filepath.Join(projectDir, "session.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "system",
			"cwd":       repo,
			"sessionId": "claude-root",
		},
		map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": "读取根目录 Claude Code session"}},
			},
		},
	)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	got, err := loadClaudeTranscript(subdir, now)
	if err != nil {
		t.Fatalf("loadClaudeTranscript: %v", err)
	}
	if got.Source != "claude" || got.SessionID != "claude-root" || got.Path != path {
		t.Fatalf("transcript = %+v", got)
	}
	if !strings.Contains(got.Excerpt, "读取根目录 Claude Code session") {
		t.Fatalf("excerpt = %q", got.Excerpt)
	}
}

func TestFormatTranscriptForPrompt_IncludesNativeEvidenceHeader(t *testing.T) {
	got := formatTranscriptForPrompt(ToolTranscript{
		Source:    "codex",
		Path:      "/tmp/rollout.jsonl",
		SessionID: "codex-1",
		Updated:   time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
		Excerpt:   "Transcript summary: user_turns=1",
	})
	for _, want := range []string{
		"- Source: codex",
		"- Path: /tmp/rollout.jsonl",
		"- Transcript ID: codex-1",
		"Transcript summary: user_turns=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt transcript missing %q:\n%s", want, got)
		}
	}
}

func writeJSONL(t *testing.T, path string, values ...any) {
	t.Helper()

	var builder strings.Builder
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
