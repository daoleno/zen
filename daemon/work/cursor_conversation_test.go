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
	now := time.Now().UTC()
	startedAt := now.Add(-30 * time.Second)
	settledAt := now.Add(-5 * time.Second)
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
						"text": "<timestamp>" + startedAt.Format(time.RFC3339Nano) + "</timestamp>\n<user_query>\ncommit and push\n</user_query>",
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
			"timestamp": settledAt.Format(time.RFC3339Nano),
		},
	)
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
	if got.Activity.StartedAt != startedAt.Format(time.RFC3339Nano) || got.Activity.SettledAt != settledAt.Format(time.RFC3339Nano) {
		t.Fatalf("turn chronology = %#v, want start %s before settlement %s", got.Activity, startedAt, settledAt)
	}
	if len(got.Events) != 3 {
		t.Fatalf("events len = %d, want 3 (user/assistant/turn_ended): %#v", len(got.Events), got.Events)
	}
	if got.Events[2].Kind != "turn_ended" || got.Events[2].Status != "success" {
		t.Fatalf("turn_ended event = %#v", got.Events[2])
	}
}

func TestProviderConversationReaderFreshCursorDoesNotBorrowAnotherCursorSession(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/workspace/zen"
	now := time.Date(2026, 7, 17, 10, 50, 11, 0, time.UTC)
	oldSessionID := "7822da8a-bd40-4022-98f6-701edd2307c8"
	oldTranscriptPath := filepath.Join(
		home,
		cursorProjectDirPrefix,
		encodeCursorProjectDir(cwd),
		cursorTranscriptDir,
		oldSessionID,
		oldSessionID+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(oldTranscriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeJSONL(t, oldTranscriptPath,
		map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": "<timestamp>Thursday, Jul 16, 2026, 11:31 AM (UTC+8)</timestamp>\n<user_query>old Brain rollout</user_query>",
				}},
			},
		},
		map[string]any{
			"role": "assistant",
			"message": map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": "old provider-owned history",
				}},
			},
		},
	)
	oldUpdatedAt := now.Add(-31*time.Hour - 16*time.Minute)
	if err := os.Chtimes(oldTranscriptPath, oldUpdatedAt, oldUpdatedAt); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	t.Setenv("HOME", home)

	resumed, err := NewProviderConversationReader().Load(classifier.Agent{
		ID:        "cursor-window:@old",
		Command:   "cursor-agent --resume " + oldSessionID,
		Cwd:       cwd,
		StartedAt: now,
		State:     classifier.StateRunning,
	}, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("resume load: %v", err)
	}
	if !resumed.Available || resumed.SessionID != oldSessionID {
		t.Fatalf("explicit native resume did not select its transcript: %#v", resumed)
	}

	freshReader := NewProviderConversationReader()
	freshAgent := classifier.Agent{
		ID:        "cursor-window:@fresh",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now,
		State:     classifier.StateRunning,
	}
	fresh, err := freshReader.Load(freshAgent, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("fresh load: %v", err)
	}
	if fresh.Available || fresh.Reason != "transcript_not_found" ||
		fresh.SessionID != "" || fresh.Path != "" || fresh.Activity != nil ||
		len(fresh.Events) != 0 {
		t.Fatalf("fresh Cursor borrowed another session: %#v", fresh)
	}

	newSessionID := "cursor-session-created-for-fresh-agent"
	newTranscriptPath := filepath.Join(
		home,
		cursorProjectDirPrefix,
		encodeCursorProjectDir(cwd),
		cursorTranscriptDir,
		newSessionID,
		newSessionID+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(newTranscriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll new transcript: %v", err)
	}
	writeJSONL(t, newTranscriptPath, map[string]any{
		"role": "user",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "<timestamp>" + now.Add(2*time.Second).Format(time.RFC3339Nano) + "</timestamp>\n<user_query>fresh request</user_query>",
			}},
		},
	})
	newUpdatedAt := now.Add(3 * time.Second)
	if err := os.Chtimes(newTranscriptPath, newUpdatedAt, newUpdatedAt); err != nil {
		t.Fatalf("Chtimes new transcript: %v", err)
	}
	attached, err := freshReader.Load(freshAgent, AgentProviderCursor, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("fresh transcript load: %v", err)
	}
	if !attached.Available || attached.SessionID != newSessionID ||
		attached.Path != newTranscriptPath || len(attached.Events) != 1 ||
		strings.Contains(attached.Events[0].Body, "old Brain rollout") {
		t.Fatalf("fresh Cursor did not bind only its newly-created transcript: %#v", attached)
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

func TestProviderConversationReaderCursorResolvesHiddenWorkspaceViaTrustedMarker(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/.zen/worktrees/zen/terminal-native-scroll-perf"
	encodedName := encodeCursorProjectDir(cwd)
	actualName := "home-daoleno-zen-worktrees-zen-terminal-native-scroll-perf"
	if encodedName == actualName {
		t.Fatalf("fixture requires Cursor private name to differ from encodeCursorProjectDir: %q", encodedName)
	}

	now := time.Now().UTC()
	sessionID := "eea683cc-bef4-470d-a11f-473992ff5338"
	transcriptPath := writeCursorHiddenWorkspaceTranscript(t, home, cwd, actualName, sessionID, "recover interface", now)
	if _, err := os.Stat(filepath.Join(home, cursorProjectDirPrefix, encodedName)); !os.IsNotExist(err) {
		t.Fatalf("encoded project dir must be absent before marker fallback: err=%v", err)
	}
	t.Setenv("HOME", home)

	got, err := NewProviderConversationReader().Load(classifier.Agent{
		ID:        "brain-agent-terminal-native-scroll-recovery-3:@14",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Available || got.Reason != "" || got.SessionID != sessionID || got.Path != transcriptPath || got.CWD != cwd {
		t.Fatalf("hidden workspace did not attach via .workspace-trusted: %#v", got)
	}
	if len(got.Events) == 0 {
		t.Fatalf("attached conversation has no events: %#v", got)
	}
}

func TestProviderConversationReaderCursorMarkerFallbackRejectsUnsafeAndForeignSources(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/.zen/worktrees/zen/git-diff-performance"
	now := time.Now().UTC()
	startedAt := now.Add(-time.Minute)
	t.Setenv("HOME", home)

	ownSession := "6c472972-9e28-4480-9bac-d24573b38ff8"
	ownPath := writeCursorHiddenWorkspaceTranscript(
		t, home, cwd, "home-daoleno-zen-worktrees-zen-git-diff-performance", ownSession, "own session", now,
	)

	writeCursorHiddenWorkspaceTranscript(
		t, home, "/home/daoleno/.zen/worktrees/zen/other-workspace",
		"mismatched-marker", "mismatched-session", "foreign workspace", now,
	)

	writeCursorRejectedMarker(t, home, "malformed-marker", []byte("{not-json"))
	writeCursorRejectedMarker(t, home, "oversized-marker", []byte(
		`{"workspacePath":"`+cwd+`","pad":"`+strings.Repeat("x", cursorWorkspaceTrustedMaxBytes)+`"}`,
	))

	staleSession := "stale-session"
	stalePath := writeCursorHiddenWorkspaceTranscript(
		t, home, cwd, "stale-transcript-project", staleSession, "stale transcript",
		now.Add(-cursorTranscriptAge-time.Hour),
	)

	otherSession := "other-agent-session"
	writeCursorHiddenWorkspaceTranscript(
		t, home, cwd, "other-session-project", otherSession, "different session", now.Add(-2*time.Hour),
	)

	reader := NewProviderConversationReader()
	agent := classifier.Agent{
		ID:        "brain-agent-git-diff-performance-recovery-3:@31",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: startedAt,
		State:     classifier.StateRunning,
	}
	got, err := reader.Load(agent, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Available || got.SessionID != ownSession || got.Path != ownPath {
		t.Fatalf("expected exact owned marker transcript, got %#v", got)
	}
	if got.SessionID == staleSession || got.Path == stalePath || strings.Contains(conversationBodies(got), "stale transcript") {
		t.Fatalf("borrowed stale transcript: %#v", got)
	}
	if got.SessionID == otherSession || strings.Contains(conversationBodies(got), "different session") {
		t.Fatalf("borrowed different-session transcript: %#v", got)
	}
	if strings.Contains(conversationBodies(got), "foreign workspace") {
		t.Fatalf("borrowed mismatched marker transcript: %#v", got)
	}
}

func TestProviderConversationReaderCursorProjectRootCacheIsSubscriptionLocal(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/.zen/worktrees/zen/cache-probe"
	otherCWD := "/home/daoleno/.zen/worktrees/zen/cache-other"
	now := time.Now().UTC()
	t.Setenv("HOME", home)

	firstSession := "cache-session-one"
	firstPath := writeCursorHiddenWorkspaceTranscript(t, home, cwd, "private-cache-probe", firstSession, "first transcript", now)
	reader := NewProviderConversationReader()
	agent := classifier.Agent{
		ID:        "cursor-cache:@1",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}

	first, err := reader.Load(agent, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if !first.Available || first.SessionID != firstSession || first.Path != firstPath {
		t.Fatalf("first attach failed: %#v", first)
	}
	if reader.cursorProjectRootsCWD != filepath.Clean(cwd) || len(reader.cursorProjectRoots) != 1 {
		t.Fatalf("expected subscription-local project root cache, got cwd=%q roots=%#v", reader.cursorProjectRootsCWD, reader.cursorProjectRoots)
	}
	cachedRoot := reader.cursorProjectRoots[0].dir

	if err := os.RemoveAll(filepath.Dir(firstPath)); err != nil {
		t.Fatalf("RemoveAll first transcript: %v", err)
	}
	secondSession := "cache-session-two"
	secondPath := filepath.Join(cachedRoot, cursorTranscriptDir, secondSession, secondSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(secondPath), 0o755); err != nil {
		t.Fatalf("MkdirAll second: %v", err)
	}
	writeCursorUserTranscript(t, secondPath, "second transcript", now.Add(3*time.Second))

	second, err := reader.Load(agent, AgentProviderCursor, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if !second.Available || second.SessionID != secondSession || second.Path != secondPath {
		t.Fatalf("same-CWD poll did not reselect new transcript: %#v", second)
	}
	if reader.cursorProjectRootsCWD != filepath.Clean(cwd) || len(reader.cursorProjectRoots) != 1 || reader.cursorProjectRoots[0].dir != cachedRoot {
		t.Fatalf("same-CWD poll must reuse project-root cache only: %#v", reader.cursorProjectRoots)
	}

	otherSession := "cache-session-other"
	otherPath := writeCursorHiddenWorkspaceTranscript(t, home, otherCWD, "private-cache-other", otherSession, "other cwd", now.Add(5*time.Second))
	otherAgent := agent
	otherAgent.Cwd = otherCWD
	other, err := reader.Load(otherAgent, AgentProviderCursor, now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("other CWD Load: %v", err)
	}
	if !other.Available || other.SessionID != otherSession || other.Path != otherPath {
		t.Fatalf("CWD change did not re-resolve project root: %#v", other)
	}
	if reader.cursorProjectRootsCWD != filepath.Clean(otherCWD) {
		t.Fatalf("CWD change must invalidate prior project-root cache: %#v", reader.cursorProjectRoots)
	}
	for _, root := range reader.cursorProjectRoots {
		if root.dir == cachedRoot {
			t.Fatalf("changed CWD retained prior project root %q", cachedRoot)
		}
	}
}

func TestProviderConversationReaderCursorForeignDirectRootDoesNotHideMarkerOwner(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/.zen/worktrees/zen/marker-owner"
	foreignCWD := "/home/daoleno/.zen/worktrees/zen/other-workspace"
	now := time.Now().UTC()
	t.Setenv("HOME", home)

	encodedName := encodeCursorProjectDir(cwd)
	hashedName := "home-daoleno-zen-worktrees-zen-marker-owner"
	if encodedName == hashedName {
		t.Fatalf("fixture requires hashed private name to differ from encode: %q", encodedName)
	}

	// Colliding legacy-encoded directory exists but its marker belongs elsewhere.
	foreignSession := "foreign-direct-session"
	foreignPath := writeCursorHiddenWorkspaceTranscript(
		t, home, foreignCWD, encodedName, foreignSession, "foreign colliding root", now,
	)

	ownSession := "exact-marker-owner-session"
	ownPath := writeCursorHiddenWorkspaceTranscript(t, home, cwd, hashedName, ownSession, "exact marker owner", now)

	got, err := NewProviderConversationReader().Load(classifier.Agent{
		ID:        "cursor-marker-owner:@1",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Available || got.SessionID != ownSession || got.Path != ownPath {
		t.Fatalf("foreign direct root hid exact marker owner: %#v", got)
	}
	if got.SessionID == foreignSession || got.Path == foreignPath || strings.Contains(conversationBodies(got), "foreign colliding root") {
		t.Fatalf("borrowed foreign-marked direct root: %#v", got)
	}
}

func TestProviderConversationReaderCursorProjectRootCacheRetriesAndDropsDeadRoots(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/.zen/worktrees/zen/root-replace"
	now := time.Now().UTC()
	t.Setenv("HOME", home)

	reader := NewProviderConversationReader()
	agent := classifier.Agent{
		ID:        "cursor-root-replace:@1",
		Command:   "cursor-agent --force --sandbox disabled",
		Cwd:       cwd,
		StartedAt: now.Add(-time.Minute),
		State:     classifier.StateRunning,
	}

	missing, err := reader.Load(agent, AgentProviderCursor, now)
	if err != nil {
		t.Fatalf("empty Load: %v", err)
	}
	if missing.Available || missing.Reason != "transcript_not_found" {
		t.Fatalf("expected not_found before marker/transcript: %#v", missing)
	}
	if reader.cursorProjectRootsCWD != "" || len(reader.cursorProjectRoots) != 0 {
		t.Fatalf("empty resolution must not be cached: cwd=%q roots=%#v", reader.cursorProjectRootsCWD, reader.cursorProjectRoots)
	}

	oldSession := "old-root-session"
	oldPath := writeCursorHiddenWorkspaceTranscript(t, home, cwd, "private-root-old", oldSession, "old root", now.Add(2*time.Second))
	first, err := reader.Load(agent, AgentProviderCursor, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if !first.Available || first.SessionID != oldSession || first.Path != oldPath {
		t.Fatalf("retry after marker/transcript failed: %#v", first)
	}
	oldRoot := reader.cursorProjectRoots[0].dir

	if err := os.RemoveAll(oldRoot); err != nil {
		t.Fatalf("RemoveAll old root: %v", err)
	}
	newSession := "replacement-root-session"
	newPath := writeCursorHiddenWorkspaceTranscript(t, home, cwd, "private-root-new", newSession, "replacement root", now.Add(4*time.Second))

	replaced, err := reader.Load(agent, AgentProviderCursor, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("replacement Load: %v", err)
	}
	if !replaced.Available || replaced.SessionID != newSession || replaced.Path != newPath {
		t.Fatalf("dead cached root pinned old source: %#v", replaced)
	}
	if len(reader.cursorProjectRoots) != 1 || reader.cursorProjectRoots[0].dir == oldRoot {
		t.Fatalf("expected re-resolved replacement root, got %#v", reader.cursorProjectRoots)
	}
	if strings.Contains(conversationBodies(replaced), "old root") {
		t.Fatalf("replacement retained old transcript body: %#v", replaced)
	}
}

func writeCursorWorkspaceTrusted(t *testing.T, projectDir, workspacePath string) {
	t.Helper()
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll project: %v", err)
	}
	payload := []byte(`{"trustedAt":"2026-07-18T00:00:00.000Z","workspacePath":` + mustJSONString(workspacePath) + `}`)
	if err := os.WriteFile(filepath.Join(projectDir, cursorWorkspaceTrustedMarker), payload, 0o644); err != nil {
		t.Fatalf("WriteFile .workspace-trusted: %v", err)
	}
}

func writeCursorRejectedMarker(t *testing.T, home, projectName string, marker []byte) {
	t.Helper()
	projectDir := filepath.Join(home, cursorProjectDirPrefix, projectName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll rejected marker project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, cursorWorkspaceTrustedMarker), marker, 0o644); err != nil {
		t.Fatalf("WriteFile rejected marker: %v", err)
	}
}

func writeCursorUserTranscript(t *testing.T, path, body string, updated time.Time) {
	t.Helper()
	writeJSONL(t, path, map[string]any{
		"role": "user",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "<timestamp>" + updated.Add(-time.Second).Format(time.RFC3339Nano) + "</timestamp>\n<user_query>" + body + "</user_query>",
			}},
		},
	})
	if err := os.Chtimes(path, updated, updated); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

func writeCursorHiddenWorkspaceTranscript(
	t *testing.T,
	home, cwd, privateName, sessionID, body string,
	updated time.Time,
) string {
	t.Helper()
	projectDir := filepath.Join(home, cursorProjectDirPrefix, privateName)
	path := filepath.Join(projectDir, cursorTranscriptDir, sessionID, sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeCursorWorkspaceTrusted(t, projectDir, cwd)
	writeCursorUserTranscript(t, path, body, updated)
	return path
}

func mustJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func conversationBodies(conversation CodexConversation) string {
	var parts []string
	for _, event := range conversation.Events {
		parts = append(parts, event.Body)
	}
	return strings.Join(parts, "\n")
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
