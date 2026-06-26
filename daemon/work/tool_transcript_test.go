package work

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
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

func TestCleanCodexDisplayText_HidesInstructionContextFragments(t *testing.T) {
	value := "## Project Structure & Module Organization\n- Source lives in apps/web/src.\n\n## Build, Test, and Development Commands\n- bun run test\n\n## Agent & Sandbox Releases\n- Public product/API surface uses Agent names.\n\n## Testing Guidelines\n- Tests are colocated with source."
	if got := CleanCodexDisplayText(value); got != "" {
		t.Fatalf("CleanCodexDisplayText() = %q, want empty", got)
	}
}

func TestCleanCodexDisplayText_KeepsContributorGuideRequests(t *testing.T) {
	value := "Generate a file named AGENTS.md that serves as a contributor guide.\n\nRecommended Sections\n\nProject Structure & Module Organization\nBuild, Test, and Development Commands\nCoding Style & Naming Conventions\nTesting Guidelines\nCommit & Pull Request Guidelines"
	got := CleanCodexDisplayText(value)
	if !strings.Contains(got, "Generate a file named AGENTS.md") {
		t.Fatalf("CleanCodexDisplayText() = %q, want contributor guide request", got)
	}
}

func TestMatchCodexTranscriptToAgentStart_UsesNearestCreatedThread(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "newer-other-window", CreatedAtMS: base.Add(90 * time.Second).UnixMilli()},
			Updated: base.Add(5 * time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "this-window", CreatedAtMS: base.Add(3 * time.Second).UnixMilli()},
			Updated: base.Add(2 * time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "old-window", CreatedAtMS: base.Add(-10 * time.Minute).UnixMilli()},
			Updated: base.Add(6 * time.Minute),
		},
	}

	got, ok := matchCodexTranscriptToAgentStart(candidates, base)
	if !ok {
		t.Fatal("expected a transcript match")
	}
	if got.Row.ID != "this-window" {
		t.Fatalf("matched %q, want this-window", got.Row.ID)
	}
}

func TestMatchCodexTranscriptToAgentStart_DoesNotFallBackToOldThread(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "old-window", CreatedAtMS: base.Add(-30 * time.Second).UnixMilli()},
			Updated: base.Add(5 * time.Minute),
		},
	}

	if got, ok := matchCodexTranscriptToAgentStart(candidates, base); ok {
		t.Fatalf("matched %#v, want no match", got)
	}
}

func TestMatchCodexTranscriptToAgentStart_DoesNotUseStaleUpdatedThread(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "created-near-start", CreatedAtMS: base.Add(2 * time.Second).UnixMilli()},
			Updated: base.Add(-1 * time.Second),
		},
	}

	if got, ok := matchCodexTranscriptToAgentStart(candidates, base); ok {
		t.Fatalf("matched %#v, want no match", got)
	}
}

func TestMatchCodexTranscriptToActiveSession_UsesTranscriptUpdatedAfterStart(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "old-ended", CreatedAtMS: base.Add(-30 * time.Minute).UnixMilli()},
			Updated: base.Add(-10 * time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "active-private", CreatedAtMS: base.Add(-30 * time.Second).UnixMilli()},
			Updated: base.Add(12 * time.Minute),
		},
	}

	got, ok := matchCodexTranscriptToActiveSession(candidates, base)
	if !ok {
		t.Fatal("expected active transcript match")
	}
	if got.Row.ID != "active-private" {
		t.Fatalf("matched %q, want active-private", got.Row.ID)
	}
}

func TestMatchCodexTranscriptToActiveSession_DoesNotUseOldCreatedThread(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "old-thread-still-touched", CreatedAtMS: base.Add(-20 * time.Minute).UnixMilli()},
			Updated: base.Add(12 * time.Minute),
		},
	}

	if got, ok := matchCodexTranscriptToActiveSession(candidates, base); ok {
		t.Fatalf("matched %#v, want no match", got)
	}
}

func TestMatchCodexTranscriptToActiveSession_UsesUpdatedWhenCreatedUnknown(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "active-without-created"},
			Updated: base.Add(2 * time.Minute),
		},
	}

	got, ok := matchCodexTranscriptToActiveSession(candidates, base)
	if !ok {
		t.Fatal("expected active transcript match")
	}
	if got.Row.ID != "active-without-created" {
		t.Fatalf("matched %q, want active-without-created", got.Row.ID)
	}
}

func TestLatestUpdatedCodexTranscriptSupportsResume(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "older-resumable", CreatedAtMS: base.Add(-6 * time.Hour).UnixMilli()},
			Updated: base.Add(4 * time.Hour),
		},
		{
			Row:     codexThreadRow{ID: "newer-created-but-stale", CreatedAtMS: base.Add(10 * time.Minute).UnixMilli()},
			Updated: base.Add(30 * time.Minute),
		},
	}

	got := latestUpdatedCodexTranscript(candidates)
	if got.Row.ID != "older-resumable" {
		t.Fatalf("matched %q, want older-resumable", got.Row.ID)
	}
	if !isCodexResumeCommand("codex resume") {
		t.Fatal("codex resume should be detected")
	}
	if isCodexResumeCommand("codex") {
		t.Fatal("plain codex should not be detected as resume")
	}
}

func TestExplicitCodexThreadTitleSkipsFirstUserMessage(t *testing.T) {
	row := codexThreadRow{
		Title:            "感觉 status 命令解析 tty 来展示不太对啊",
		FirstUserMessage: "感觉 status 命令解析 tty 来展示不太对啊",
	}

	if title, ok := explicitCodexThreadTitle(row); ok || title != "" {
		t.Fatalf("explicit title = (%q, %v), want none", title, ok)
	}
}

func TestExplicitCodexThreadTitleKeepsRenamedTitle(t *testing.T) {
	row := codexThreadRow{
		Title:            "Polish Codex session names",
		FirstUserMessage: "为什么首页 Session name 没有跟着 rename 变化",
	}

	title, ok := explicitCodexThreadTitle(row)
	if !ok || title != "Polish Codex session names" {
		t.Fatalf("explicit title = (%q, %v), want renamed title", title, ok)
	}
}

func TestQueryCodexThreadsIncludesTitle(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	dbPath := filepath.Join(t.TempDir(), "state_5.sqlite")
	runSQLite(t, sqlite3, dbPath, `
CREATE TABLE threads (
  id TEXT,
  rollout_path TEXT,
  created_at INTEGER,
  updated_at INTEGER,
  cwd TEXT,
  title TEXT,
  first_user_message TEXT,
  archived INTEGER,
  created_at_ms INTEGER,
  updated_at_ms INTEGER
);
INSERT INTO threads (id, rollout_path, created_at, updated_at, cwd, title, first_user_message, archived, created_at_ms, updated_at_ms)
VALUES ('thread-1', '/tmp/rollout-1.jsonl', 100, 200, '/repo/zen', 'Renamed from Codex', 'First prompt', 0, 100000, 200000);
`)

	rows, err := queryCodexThreads(sqlite3, dbPath, "/repo/zen")
	if err != nil {
		t.Fatalf("queryCodexThreads: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].Title != "Renamed from Codex" {
		t.Fatalf("title = %q, want renamed title", rows[0].Title)
	}
	if rows[0].FirstUserMessage != "First prompt" {
		t.Fatalf("first_user_message = %q, want first prompt", rows[0].FirstUserMessage)
	}
}

func TestQueryCodexThreadsFallsBackWithoutTitleColumn(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	dbPath := filepath.Join(t.TempDir(), "state_5.sqlite")
	runSQLite(t, sqlite3, dbPath, `
CREATE TABLE threads (
  id TEXT,
  rollout_path TEXT,
  created_at INTEGER,
  updated_at INTEGER,
  cwd TEXT,
  archived INTEGER,
  created_at_ms INTEGER,
  updated_at_ms INTEGER
);
INSERT INTO threads (id, rollout_path, created_at, updated_at, cwd, archived, created_at_ms, updated_at_ms)
VALUES ('thread-1', '/tmp/rollout-1.jsonl', 100, 200, '/repo/zen', 0, 100000, 200000);
`)

	rows, err := queryCodexThreads(sqlite3, dbPath, "/repo/zen")
	if err != nil {
		t.Fatalf("queryCodexThreads: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].Title != "" {
		t.Fatalf("title = %q, want empty fallback title", rows[0].Title)
	}
}

func TestBrainCodexTranscriptFallbackUsesLatestUpdated(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "older-brain-thread"},
			Updated: base.Add(time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "latest-brain-thread"},
			Updated: base.Add(10 * time.Minute),
		},
	}

	got, ok := fallbackCodexTranscriptForAgent(candidates, classifier.Agent{
		ID:     "brain-agent-brain-123:@1",
		Name:   "Brain",
		Hidden: true,
	})
	if !ok {
		t.Fatal("expected Brain fallback match")
	}
	if got.Row.ID != "latest-brain-thread" {
		t.Fatalf("matched %q, want latest-brain-thread", got.Row.ID)
	}

	if _, ok := fallbackCodexTranscriptForAgent(candidates, classifier.Agent{
		ID:     "main:@1",
		Name:   "codex",
		Hidden: false,
	}); ok {
		t.Fatal("ordinary Codex agent should not use Brain fallback")
	}
}

func TestBrainCodexTranscriptFallbackDoesNotUseThreadBeforeAgentStart(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row: codexThreadRow{
				ID:          "previous-brain-thread",
				CreatedAtMS: base.Add(-10 * time.Minute).UnixMilli(),
			},
			Updated: base.Add(10 * time.Minute),
		},
	}

	if got, ok := fallbackCodexTranscriptForAgent(candidates, classifier.Agent{
		ID:        "brain-agent-brain-123:@1",
		Name:      "Brain",
		Hidden:    true,
		StartedAt: base,
	}); ok {
		t.Fatalf("matched %#v, want no previous thread match", got)
	}
}

func TestBrainCodexTranscriptFallbackPrefersPostStartThread(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row: codexThreadRow{
				ID:          "previous-brain-thread",
				CreatedAtMS: base.Add(-10 * time.Minute).UnixMilli(),
			},
			Updated: base.Add(10 * time.Minute),
		},
		{
			Row: codexThreadRow{
				ID:          "current-brain-thread",
				CreatedAtMS: base.Add(2 * time.Second).UnixMilli(),
			},
			Updated: base.Add(20 * time.Second),
		},
	}

	got, ok := fallbackCodexTranscriptForAgent(candidates, classifier.Agent{
		ID:        "brain-agent-brain-123:@1",
		Name:      "Brain",
		Hidden:    true,
		StartedAt: base,
	})
	if !ok {
		t.Fatal("expected current Brain fallback match")
	}
	if got.Row.ID != "current-brain-thread" {
		t.Fatalf("matched %q, want current-brain-thread", got.Row.ID)
	}
}

func TestMatchCodexTranscriptToOpenRolloutsUsesNewestOpenFile(t *testing.T) {
	base := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{
			Row:     codexThreadRow{ID: "old-thread"},
			Path:    "/home/user/.codex/sessions/2026/05/21/rollout-old.jsonl",
			Updated: base.Add(2 * time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "new-thread"},
			Path:    "/home/user/.codex/sessions/2026/05/21/rollout-new.jsonl",
			Updated: base.Add(10 * time.Minute),
		},
		{
			Row:     codexThreadRow{ID: "other-process-thread"},
			Path:    "/home/user/.codex/sessions/2026/05/21/rollout-other.jsonl",
			Updated: base.Add(20 * time.Minute),
		},
	}

	got, ok := matchCodexTranscriptToOpenRollouts(candidates, []string{
		"/home/user/.codex/sessions/2026/05/21/rollout-old.jsonl",
		"/home/user/.codex/sessions/2026/05/21/rollout-new.jsonl",
	})
	if !ok {
		t.Fatal("expected an open rollout match")
	}
	if got.Row.ID != "new-thread" {
		t.Fatalf("matched %q, want new-thread", got.Row.ID)
	}
}

func TestParseLsofCodexRolloutPathsFiltersCodexRollouts(t *testing.T) {
	output := strings.Join([]string{
		"p123",
		"n/home/user/.codex/state_5.sqlite",
		"n/home/user/.codex/sessions/2026/05/21/rollout-2026-05-21T08-00-00-old.jsonl",
		"n/home/user/.codex/sessions/2026/05/21/rollout-2026-05-21T08-10-00-new.jsonl (deleted)",
		"n/home/user/tmp/rollout-not-codex.jsonl",
		"n/home/user/.codex/sessions/2026/05/21/not-a-rollout.jsonl",
	}, "\n")

	got := parseLsofCodexRolloutPaths(output)
	want := []string{
		"/home/user/.codex/sessions/2026/05/21/rollout-2026-05-21T08-00-00-old.jsonl",
		"/home/user/.codex/sessions/2026/05/21/rollout-2026-05-21T08-10-00-new.jsonl",
	}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("paths[%d] = %q, want %q; all %#v", index, got[index], want[index], got)
		}
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

func runSQLite(t *testing.T, sqlite3, dbPath, script string) {
	t.Helper()
	out, err := exec.Command(sqlite3, dbPath, script).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite failed: %v%s", err, stderrSuffix(string(out)))
	}
}
