package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestParseClaudeConversation_BuildsMarkdownThinkingAndTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-session.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "system",
			"cwd":       "/repo/zen",
			"sessionId": "claude-session",
			"uuid":      "sys-1",
			"timestamp": "2026-07-12T01:00:00.000Z",
		},
		map[string]any{
			"type":      "user",
			"cwd":       "/repo/zen",
			"sessionId": "claude-session",
			"uuid":      "user-1",
			"timestamp": "2026-07-12T01:00:01.000Z",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "请修复 **Markdown** 渲染"},
				},
			},
		},
		map[string]any{
			"type":      "assistant",
			"cwd":       "/repo/zen",
			"sessionId": "claude-session",
			"uuid":      "asst-1",
			"timestamp": "2026-07-12T01:00:02.000Z",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "先检查共享 timeline 模型", "signature": "secret-signature"},
					{"type": "text", "text": "我会先读取现有接口，保留 **Markdown**。"},
					{
						"type": "tool_use",
						"id":   "toolu_read_1",
						"name": "Read",
						"input": map[string]any{
							"file_path": "/repo/zen/daemon/work/codex_conversation.go",
						},
					},
					{
						"type": "tool_use",
						"id":   "toolu_bash_1",
						"name": "Bash",
						"input": map[string]any{
							"command":     "go test ./work -run Claude",
							"description": "Run Claude conversation tests",
						},
					},
				},
			},
		},
		map[string]any{
			"type":      "user",
			"cwd":       "/repo/zen",
			"sessionId": "claude-session",
			"uuid":      "user-2",
			"timestamp": "2026-07-12T01:00:03.000Z",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_read_1",
						"content":     "package work",
					},
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_bash_1",
						"content":     "ok",
					},
				},
			},
		},
		map[string]any{
			"type":      "user",
			"isMeta":    true,
			"cwd":       "/repo/zen",
			"sessionId": "claude-session",
			"uuid":      "meta-1",
			"timestamp": "2026-07-12T01:00:04.000Z",
			"message": map[string]any{
				"role":    "user",
				"content": "internal meta should be hidden",
			},
		},
		map[string]any{
			"type":        "assistant",
			"isSidechain": true,
			"cwd":         "/repo/zen",
			"sessionId":   "claude-session",
			"uuid":        "side-1",
			"timestamp":   "2026-07-12T01:00:05.000Z",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "sidechain should be hidden"},
				},
			},
		},
		map[string]any{
			"type":           "permission-mode",
			"permissionMode": "bypassPermissions",
			"sessionId":      "claude-session",
		},
		map[string]any{
			"type": "attachment",
			"attachment": map[string]any{
				"type":   "hook_success",
				"stdout": "secret hook output",
			},
			"sessionId": "claude-session",
		},
	)

	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("parseClaudeConversation: %v", err)
	}
	if !got.Available || got.Source != claudeConversationSource || got.SessionID != "claude-session" {
		t.Fatalf("conversation = %#v", got)
	}
	if got.CWD != "/repo/zen" {
		t.Fatalf("cwd = %q", got.CWD)
	}
	if len(got.Events) != 5 {
		t.Fatalf("events len = %d, want 5: %#v", len(got.Events), got.Events)
	}

	assertEvent(t, got.Events[0], "user_message", "user", "", "请修复 **Markdown** 渲染")
	assertEvent(t, got.Events[1], "commentary", "", "Reasoning", "先检查共享 timeline 模型")
	assertEvent(t, got.Events[2], "assistant_message", "assistant", "", "**Markdown**")
	if got.Events[3].Kind != "tool" || got.Events[3].ToolName != "Read" || got.Events[3].CallID != "toolu_read_1" {
		t.Fatalf("read tool = %#v", got.Events[3])
	}
	if got.Events[3].Status != "done" || got.Events[3].Output != "package work" {
		t.Fatalf("read tool result = %#v", got.Events[3])
	}
	if !strings.Contains(got.Events[3].Input, "codex_conversation.go") {
		t.Fatalf("read tool input = %#v", got.Events[3])
	}
	if got.Events[4].Kind != "command" || got.Events[4].Command != "go test ./work -run Claude" {
		t.Fatalf("bash command = %#v", got.Events[4])
	}
	if got.Events[4].Status != "done" || got.Events[4].Output != "ok" || got.Events[4].ExitCode == nil || *got.Events[4].ExitCode != 0 {
		t.Fatalf("bash result = %#v", got.Events[4])
	}

	for _, event := range got.Events {
		if strings.Contains(event.Body, "secret-signature") ||
			strings.Contains(event.Body, "secret hook") ||
			strings.Contains(event.Body, "sidechain") ||
			strings.Contains(event.Body, "internal meta") ||
			strings.Contains(event.Input, "signature") {
			t.Fatalf("provider-internal content leaked: %#v", event)
		}
	}
}

func TestParseClaudeConversation_StableRefreshIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stable.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type":      "user",
			"sessionId": "stable-session",
			"uuid":      "uuid-user",
			"timestamp": "2026-07-12T02:00:00.000Z",
			"message": map[string]any{
				"role":    "user",
				"content": "hello",
			},
		},
		map[string]any{
			"type":      "assistant",
			"sessionId": "stable-session",
			"uuid":      "uuid-asst",
			"timestamp": "2026-07-12T02:00:01.000Z",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "hi"},
					{"type": "tool_use", "id": "toolu_stable", "name": "Glob", "input": map[string]any{"pattern": "*.go"}},
				},
			},
		},
	)

	first, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	second, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if len(first.Events) != 3 || len(second.Events) != 3 {
		t.Fatalf("event counts = %d %d", len(first.Events), len(second.Events))
	}
	for index := range first.Events {
		if first.Events[index].ID != second.Events[index].ID {
			t.Fatalf("event %d id changed: %q -> %q", index, first.Events[index].ID, second.Events[index].ID)
		}
	}
	if !strings.Contains(first.Events[0].ID, "uuid-user") {
		t.Fatalf("user id = %q", first.Events[0].ID)
	}
	if first.Events[2].ID != "claude-tool:toolu_stable" {
		t.Fatalf("tool id = %q", first.Events[2].ID)
	}
}

func TestParseClaudeConversation_AppendedThinkingAndTextTrackTurnLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progressive.jsonl")
	user := map[string]any{
		"type":      "user",
		"sessionId": "progressive-session",
		"uuid":      "uuid-user",
		"timestamp": "2026-07-15T01:00:00.000Z",
		"message": map[string]any{
			"role":    "user",
			"content": "trace the stream",
		},
	}
	thinking := map[string]any{
		"type":      "assistant",
		"sessionId": "progressive-session",
		"uuid":      "uuid-thinking",
		"timestamp": "2026-07-15T01:00:01.000Z",
		"message": map[string]any{
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Reading the native transcript."},
			},
		},
	}
	answer := map[string]any{
		"type":      "assistant",
		"sessionId": "progressive-session",
		"uuid":      "uuid-answer",
		"timestamp": "2026-07-15T01:00:02.000Z",
		"message": map[string]any{
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "The structured block is visible."},
			},
		},
	}

	writeJSONL(t, path, user)
	userSnapshot, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("user snapshot: %v", err)
	}
	if userSnapshot.Active == nil || !*userSnapshot.Active || len(userSnapshot.Events) != 1 {
		t.Fatalf("user snapshot = %#v, want active turn", userSnapshot)
	}
	userID := userSnapshot.Events[0].ID

	writeJSONL(t, path, user, thinking)
	thinkingSnapshot, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("thinking snapshot: %v", err)
	}
	if thinkingSnapshot.Active == nil || !*thinkingSnapshot.Active {
		t.Fatalf("thinking-only snapshot must stay active while the text record is pending: %#v", thinkingSnapshot.Active)
	}
	if len(thinkingSnapshot.Events) != 2 || thinkingSnapshot.Events[0].ID != userID {
		t.Fatalf("thinking snapshot changed existing identity: %#v", thinkingSnapshot.Events)
	}
	thinkingID := thinkingSnapshot.Events[1].ID
	if thinkingSnapshot.Events[1].Kind != "commentary" || thinkingSnapshot.Events[1].Partial {
		t.Fatalf("Claude thinking is a completed appended block, not a synthetic token stream: %#v", thinkingSnapshot.Events[1])
	}

	writeJSONL(t, path, user, thinking, answer)
	answerSnapshot, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("answer snapshot: %v", err)
	}
	if answerSnapshot.Active == nil || *answerSnapshot.Active {
		t.Fatalf("terminal text record must finish the turn: %#v", answerSnapshot.Active)
	}
	if len(answerSnapshot.Events) != 3 || answerSnapshot.Events[0].ID != userID || answerSnapshot.Events[1].ID != thinkingID {
		t.Fatalf("answer snapshot changed appended event identities: %#v", answerSnapshot.Events)
	}
	if answerSnapshot.Events[2].Kind != "assistant_message" || answerSnapshot.Events[2].Body != "The structured block is visible." || answerSnapshot.Events[2].Partial {
		t.Fatalf("answer event = %#v", answerSnapshot.Events[2])
	}
}

func TestParseClaudeConversation_MalformedLinesAreSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.jsonl")
	content := strings.Join([]string{
		`{not-json`,
		`{"type":"user","sessionId":"m1","uuid":"u1","message":{"role":"user","content":[{"type":"text","text":"visible"}]}}`,
		`{"type":"assistant","sessionId":"m1","uuid":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"x"},{"type":"text","text":"ok"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("parseClaudeConversation: %v", err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %#v", got.Events)
	}
	assertEvent(t, got.Events[0], "user_message", "user", "", "visible")
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "ok")
}

func TestLoadClaudeConversationForAgent_FindsResumeSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/repo/zen"
	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := filepath.Join(projectDir, "old-session.jsonl")
	newPath := filepath.Join(projectDir, "resume-session.jsonl")
	writeJSONL(t, oldPath,
		map[string]any{"type": "system", "cwd": cwd, "sessionId": "old-session"},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": "old-session", "uuid": "old-u",
			"message": map[string]any{"role": "user", "content": "old prompt"},
		},
	)
	writeJSONL(t, newPath,
		map[string]any{"type": "system", "cwd": cwd, "sessionId": "resume-session"},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": "resume-session", "uuid": "new-u",
			"message": map[string]any{"role": "user", "content": "resume prompt"},
		},
	)
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:      "claude",
		Command:   "claude --resume resume-session",
		Cwd:       cwd,
		State:     classifier.StateRunning,
		StartedAt: now.Add(-time.Minute),
	}, now)
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if !got.Available || got.SessionID != "resume-session" || got.Path != newPath {
		t.Fatalf("conversation = %#v", got)
	}
	if len(got.Events) == 0 || !strings.Contains(got.Events[0].Body, "resume prompt") {
		t.Fatalf("events = %#v", got.Events)
	}
	if got.Active == nil || !*got.Active {
		t.Fatalf("active = %#v", got.Active)
	}
}

func TestLoadClaudeConversationForAgent_UnavailableWithoutTranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:    "claude",
		Command: "claude",
		Cwd:     "/repo/missing",
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "transcript_not_found" {
		t.Fatalf("conversation = %#v", got)
	}
}

func TestLoadClaudeConversationForAgent_ProviderSelection(t *testing.T) {
	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:    "claude",
		Command: "claude",
		Cwd:     "",
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "missing_cwd" {
		t.Fatalf("claude missing cwd = %#v", got)
	}

	other, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:    "other",
		Command: "my-agent",
		Cwd:     "/repo",
	}, time.Now())
	if err != nil {
		t.Fatalf("other LoadCodexConversationForAgent: %v", err)
	}
	if other.Available || other.Reason != "not_structured_agent" {
		t.Fatalf("other = %#v", other)
	}
}

func TestClaudeResumeSessionID(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"claude --resume abc-123", "abc-123"},
		{"claude --resume=abc-123", "abc-123"},
		{"/opt/claude -r abc-123", "abc-123"},
		{"claude", ""},
		{"codex --resume abc-123", ""},
	} {
		if got := claudeResumeSessionID(tc.command); got != tc.want {
			t.Fatalf("claudeResumeSessionID(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestParseClaudeConversation_FailedToolResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed-tool.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type": "assistant", "sessionId": "fail-1", "uuid": "a1",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "tool_use", "id": "toolu_fail", "name": "Read", "input": map[string]any{"file_path": "/missing"}},
				},
			},
		},
		map[string]any{
			"type": "user", "sessionId": "fail-1", "uuid": "u1",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "tool_result", "tool_use_id": "toolu_fail", "is_error": true, "content": "Error: missing file"},
				},
			},
		},
	)

	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %#v", got.Events)
	}
	if got.Events[0].Status != "failed" || got.Events[0].Output != "Error: missing file" {
		t.Fatalf("event = %#v", got.Events[0])
	}
	if got.Events[0].ExitCode == nil || *got.Events[0].ExitCode != 1 {
		t.Fatalf("exit = %#v", got.Events[0].ExitCode)
	}
}

func TestLoadClaudeConversationForAgent_AllMalformedUsesTerminalFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/repo/zen"
	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, "broken-session.jsonl")
	content := strings.Join([]string{
		`{not-json`,
		`{"foo":"bar"}`,
		`{"type":"not-a-claude-type","message":{"role":"user","content":"x"}}`,
		`42`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}

	agent := classifier.Agent{
		Name:      "claude",
		Command:   "claude",
		Cwd:       cwd,
		State:     classifier.StateRunning,
		StartedAt: now.Add(-time.Minute),
	}
	got, err := LoadCodexConversationForAgent(agent, now)
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "transcript_malformed" {
		t.Fatalf("conversation = %#v", got)
	}
	if ShouldUseTerminalSnapshotConversationFallback(agent, got) {
		t.Fatal("structured Claude must not dump terminal contents into Chat on malformed transcript")
	}
}

func TestLoadClaudeConversationForAgent_StartedAtSelectsMatchingSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/repo/zen"
	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	now := startedAt.Add(10 * time.Minute)

	matchedPath := filepath.Join(projectDir, "matched-session.jsonl")
	newerUnrelatedPath := filepath.Join(projectDir, "newer-unrelated.jsonl")
	writeJSONL(t, matchedPath,
		map[string]any{
			"type": "system", "cwd": cwd, "sessionId": "matched-session",
			"timestamp": startedAt.Format(time.RFC3339Nano),
		},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": "matched-session", "uuid": "u-matched",
			"timestamp": startedAt.Add(2 * time.Second).Format(time.RFC3339Nano),
			"message":   map[string]any{"role": "user", "content": "matched session prompt"},
		},
	)
	writeJSONL(t, newerUnrelatedPath,
		map[string]any{
			"type": "system", "cwd": cwd, "sessionId": "newer-unrelated",
			"timestamp": startedAt.Add(8 * time.Minute).Format(time.RFC3339Nano),
		},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": "newer-unrelated", "uuid": "u-new",
			"timestamp": startedAt.Add(8*time.Minute + time.Second).Format(time.RFC3339Nano),
			"message":   map[string]any{"role": "user", "content": "newer unrelated prompt"},
		},
	)
	if err := os.Chtimes(matchedPath, startedAt.Add(3*time.Minute), startedAt.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newerUnrelatedPath, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:      "claude",
		Command:   "claude",
		Cwd:       cwd,
		State:     classifier.StateRunning,
		StartedAt: startedAt,
	}, now)
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if !got.Available || got.SessionID != "matched-session" || got.Path != matchedPath {
		t.Fatalf("conversation = %#v", got)
	}
	if len(got.Events) == 0 || !strings.Contains(got.Events[0].Body, "matched session prompt") {
		t.Fatalf("events = %#v", got.Events)
	}
}

func TestLoadClaudeConversationForAgent_AmbiguousSessionsYieldNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/repo/zen"
	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, id := range []string{"sess-a", "sess-b"} {
		path := filepath.Join(projectDir, id+".jsonl")
		writeJSONL(t, path,
			map[string]any{
				"type": "system", "cwd": cwd, "sessionId": id,
				"timestamp": now.Add(-time.Hour).Format(time.RFC3339Nano),
			},
			map[string]any{
				"type": "user", "cwd": cwd, "sessionId": id, "uuid": id + "-u",
				"timestamp": now.Add(-time.Hour + time.Second).Format(time.RFC3339Nano),
				"message":   map[string]any{"role": "user", "content": id + " prompt"},
			},
		)
		if err := os.Chtimes(path, now, now); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Name:      "claude",
		Command:   "claude",
		Cwd:       cwd,
		StartedAt: time.Time{}, // no start anchor and multiple fresh sessions
	}, now)
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "transcript_not_found" {
		t.Fatalf("conversation = %#v", got)
	}
	if ShouldUseTerminalSnapshotConversationFallback(classifier.Agent{Command: "claude"}, got) {
		t.Fatal("structured Claude must not dump terminal contents into Chat when transcript bind is ambiguous")
	}
}

func TestParseClaudeConversation_ToolResultContentArrayAndInternalMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "array-result.jsonl")
	writeJSONL(t, path,
		map[string]any{
			"type": "assistant", "sessionId": "arr-1", "uuid": "a1", "parentUuid": "parent-secret",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "tool_use", "id": "toolu_arr", "name": "Read", "input": map[string]any{"file_path": "/repo/a.go"}},
				},
			},
		},
		map[string]any{
			"type": "user", "sessionId": "arr-1", "uuid": "u1", "parentUuid": "a1",
			"toolUseResult": map[string]any{
				"type":    "internal",
				"payload": map[string]any{"token": "secret-token", "raw": true},
			},
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_arr",
						"content": []map[string]any{
							{"type": "text", "text": "package main"},
							{"type": "text", "text": "func main() {}"},
							{"type": "image", "source": map[string]any{"data": "AAAA", "media_type": "image/png"}},
						},
					},
				},
			},
		},
		map[string]any{
			"type": "assistant", "sessionId": "arr-1", "uuid": "side-1", "isSidechain": true, "parentUuid": "a1",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "sidechain must stay hidden"},
				},
			},
		},
	)

	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %#v", got.Events)
	}
	if got.Events[0].Output != "package main\nfunc main() {}" {
		t.Fatalf("tool output = %#v", got.Events[0])
	}
	raw, err := json.Marshal(got.Events)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, leak := range []string{"secret-token", "parent-secret", "sidechain must stay hidden", "image/png", "AAAA", "toolUseResult"} {
		if strings.Contains(serialized, leak) {
			t.Fatalf("internal metadata leaked %q in %s", leak, serialized)
		}
	}
}
