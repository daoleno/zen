package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestProviderConversationReaderOwnsOneIndependentSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor.jsonl")
	writeCursorReaderTranscript(t, path, "first source")

	reader := NewProviderConversationReader()
	first, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) == 0 || len(unchanged.Events) == 0 || &first.Events[0] != &unchanged.Events[0] {
		t.Fatalf("unchanged source was reparsed instead of reused: first=%#v unchanged=%#v", first.Events, unchanged.Events)
	}

	independent, err := NewProviderConversationReader().loadCursorConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(independent.Events) == 0 || &first.Events[0] == &independent.Events[0] {
		t.Fatal("independent readers shared retained parsed state")
	}

	appendJSONL(t, path, map[string]any{
		"role": "assistant",
		"message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "appended reply"}},
		},
	})
	appended, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(appended.Events) != 2 || !strings.Contains(appended.Events[1].Body, "appended reply") {
		t.Fatalf("append did not advance reader state: %#v", appended.Events)
	}

	writeCursorReaderTranscript(t, path, "truncated replacement")
	forceReaderFixtureModTime(t, path, time.Now().Add(2*time.Second))
	truncated, err := reader.loadCursorConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(truncated.Events) != 1 || !strings.Contains(truncated.Events[0].Body, "truncated replacement") ||
		conversationContainsBody(truncated, "first source") || conversationContainsBody(truncated, "appended reply") {
		t.Fatalf("truncate retained prior source state: %#v", truncated.Events)
	}

	replacedPath := filepath.Join(dir, "replace.jsonl")
	writeCursorReaderTranscript(t, replacedPath, "same stamp old")
	oldInfo, err := os.Stat(replacedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.loadCursorConversation(replacedPath); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement.jsonl")
	writeCursorReaderTranscript(t, replacement, "same stamp new")
	forceReaderFixtureModTime(t, replacement, oldInfo.ModTime())
	replacementInfo, err := os.Stat(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if replacementInfo.Size() != oldInfo.Size() || !replacementInfo.ModTime().Equal(oldInfo.ModTime()) {
		t.Fatalf("replacement precondition differs: old=(%d,%s) new=(%d,%s)", oldInfo.Size(), oldInfo.ModTime(), replacementInfo.Size(), replacementInfo.ModTime())
	}
	if err := os.Rename(replacement, replacedPath); err != nil {
		t.Fatal(err)
	}
	replaced, err := reader.loadCursorConversation(replacedPath)
	if err != nil {
		t.Fatal(err)
	}
	if conversationContainsBody(replaced, "same stamp old") || !conversationContainsBody(replaced, "same stamp new") {
		t.Fatalf("same-stamp path replacement retained prior inode state: %#v", replaced.Events)
	}

	newPath := filepath.Join(dir, "new-source.jsonl")
	writeCursorReaderTranscript(t, newPath, "selected new path")
	selected, err := reader.loadCursorConversation(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !conversationContainsBody(selected, "selected new path") || conversationContainsBody(selected, "same stamp new") {
		t.Fatalf("selected path inherited prior source: %#v", selected.Events)
	}
}

func TestProviderConversationReaderGrokTrackersAreIndependent(t *testing.T) {
	dir := t.TempDir()
	sessionID := "independent-grok-readers"
	updatesPath := filepath.Join(dir, grokUpdatesFile)
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{"id": sessionID, "cwd": "/repo"},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "initial"},
	)
	writeJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "prompt", "user_message_chunk", map[string]any{"type": "text", "text": "initial"}),
	)

	firstReader := NewProviderConversationReader()
	secondReader := NewProviderConversationReader()
	if _, err := firstReader.loadGrokConversation(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := secondReader.loadGrokConversation(dir); err != nil {
		t.Fatal(err)
	}
	firstTracker := firstReader.source.grokUpdates
	secondTracker := secondReader.source.grokUpdates
	if firstTracker == nil || secondTracker == nil || firstTracker == secondTracker {
		t.Fatalf("reader-owned Grok trackers are not independent: first=%p second=%p", firstTracker, secondTracker)
	}
	secondOffset := secondTracker.offset

	appendJSONL(t, updatesPath,
		grokUpdateFixture(sessionID, "prompt", "agent_message_chunk", map[string]any{"type": "text", "text": "first reader advanced"}),
	)
	advanced, err := firstReader.loadGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !conversationContainsBody(advanced, "first reader advanced") {
		t.Fatalf("first reader did not advance: %#v", advanced.Events)
	}
	if secondTracker.offset != secondOffset {
		t.Fatalf("advancing first reader mutated second tracker offset: got %d, want %d", secondTracker.offset, secondOffset)
	}
	if conversationContainsBody(secondReader.source.conversation, "first reader advanced") {
		t.Fatalf("advancing first reader mutated second retained conversation: %#v", secondReader.source.conversation.Events)
	}

	second, err := secondReader.loadGrokConversation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !conversationContainsBody(second, "first reader advanced") {
		t.Fatalf("second reader did not independently advance: %#v", second.Events)
	}
}

func TestProviderConversationReaderSelectsNewSessionAndProviderWithoutPriorState(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/provider-reader"
	now := time.Now().UTC()
	t.Setenv("HOME", home)

	oldCursorPath := cursorReaderTranscriptPath(home, cwd, "cursor-old")
	writeCursorReaderTranscript(t, oldCursorPath, "cursor old session")
	forceReaderFixtureModTime(t, oldCursorPath, now.Add(-time.Minute))

	reader := NewProviderConversationReader()
	agent := classifier.Agent{ID: "agent", Cwd: cwd, Command: "cursor-agent --resume cursor-old"}
	oldCursor, err := reader.Load(agent, AgentProviderCursor, now)
	if err != nil {
		t.Fatal(err)
	}
	if oldCursor.SessionID != "cursor-old" || !conversationContainsBody(oldCursor, "cursor old session") {
		t.Fatalf("initial Cursor source = %#v", oldCursor)
	}

	newCursorPath := cursorReaderTranscriptPath(home, cwd, "cursor-new")
	writeCursorReaderTranscript(t, newCursorPath, "cursor new session")
	forceReaderFixtureModTime(t, newCursorPath, now.Add(time.Second))
	agent.Command = "cursor-agent --resume cursor-new"
	newCursor, err := reader.Load(agent, AgentProviderCursor, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if newCursor.SessionID != "cursor-new" || !conversationContainsBody(newCursor, "cursor new session") ||
		conversationContainsBody(newCursor, "cursor old session") {
		t.Fatalf("new-session selection retained prior Cursor state: %#v", newCursor)
	}

	claudePath := claudeReaderTranscriptPath(home, cwd, "claude-session")
	writeClaudeReaderTranscript(t, claudePath, cwd, "claude-session", "claude provider")
	forceReaderFixtureModTime(t, claudePath, now)
	claudeAgent := agent
	claudeAgent.Command = "claude --resume claude-session"
	claudeConversation, err := reader.Load(claudeAgent, AgentProviderClaude, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claudeConversation.SessionID != "claude-session" || !conversationContainsBody(claudeConversation, "claude provider") ||
		conversationContainsBody(claudeConversation, "cursor new session") {
		t.Fatalf("provider switch retained prior Cursor state: %#v", claudeConversation)
	}
}

func TestProviderConversationReaderResumeSwitchCannotReusePriorSession(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/provider-reader-resume"
	now := time.Now().UTC()
	t.Setenv("HOME", home)
	for _, fixture := range []struct {
		id   string
		body string
	}{
		{id: "resume-one", body: "first resumed session"},
		{id: "resume-two", body: "second resumed session"},
	} {
		path := claudeReaderTranscriptPath(home, cwd, fixture.id)
		writeClaudeReaderTranscript(t, path, cwd, fixture.id, fixture.body)
		forceReaderFixtureModTime(t, path, now)
	}

	reader := NewProviderConversationReader()
	agent := classifier.Agent{ID: "agent", Cwd: cwd, Command: "claude --resume resume-one"}
	first, err := reader.Load(agent, AgentProviderClaude, now)
	if err != nil {
		t.Fatal(err)
	}
	agent.Command = "claude --resume resume-two"
	second, err := reader.Load(agent, AgentProviderClaude, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != "resume-one" || second.SessionID != "resume-two" ||
		!conversationContainsBody(second, "second resumed session") || conversationContainsBody(second, "first resumed session") {
		t.Fatalf("resume switch reused prior session: first=%#v second=%#v", first, second)
	}
}

func writeCursorReaderTranscript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, path, map[string]any{
		"role": "user",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "<timestamp>2026-07-17T00:00:00Z</timestamp><user_query>" + body + "</user_query>",
			}},
		},
	})
}

func writeClaudeReaderTranscript(t *testing.T, path, cwd, sessionID, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, path,
		map[string]any{
			"type": "system", "cwd": cwd, "sessionId": sessionID,
			"uuid": "system-" + sessionID, "timestamp": "2026-07-17T00:00:00Z",
		},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": sessionID,
			"uuid": "user-" + sessionID, "timestamp": "2026-07-17T00:00:01Z",
			"message": map[string]any{"role": "user", "content": body},
		},
	)
}

func cursorReaderTranscriptPath(home, cwd, sessionID string) string {
	return filepath.Join(home, cursorProjectDirPrefix, encodeCursorProjectDir(cwd), cursorTranscriptDir, sessionID, sessionID+".jsonl")
}

func claudeReaderTranscriptPath(home, cwd, sessionID string) string {
	return filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd), sessionID+".jsonl")
}

func forceReaderFixtureModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func conversationContainsBody(conversation CodexConversation, value string) bool {
	for _, event := range conversation.Events {
		if strings.Contains(event.Body, value) {
			return true
		}
	}
	return false
}
