package work

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func testDigestProvider(title, summary string) AgentDigestProvider {
	return AgentDigestProviderFunc(func(context.Context, AgentDigestInput) (AgentDigest, error) {
		return AgentDigest{
			Title:    title,
			Summary:  summary,
			Progress: []string{"Edited the work log model"},
			Next:     "Run verification",
			Provider: "test",
		}, nil
	})
}

func testDigestProviderFromAgent() AgentDigestProvider {
	return AgentDigestProviderFunc(func(_ context.Context, input AgentDigestInput) (AgentDigest, error) {
		return AgentDigest{
			Title:    input.Agent.Summary,
			Summary:  "Summary for " + input.Agent.ID,
			Progress: []string{"Useful signal for " + input.Agent.ID},
			Next:     "Continue " + input.Agent.ID,
			Provider: "test",
		}, nil
	})
}

func enableTestTranscript(logger *SessionLogger, source string) {
	logger.loadTranscript = func(agent classifier.Agent, now time.Time) ToolTranscript {
		return ToolTranscript{
			Source:    source,
			Path:      "/tmp/" + source + ".jsonl",
			SessionID: strings.TrimSpace(agent.ID),
			Updated:   now,
			Excerpt:   "Transcript summary: user_turns=1 tool_calls=1\nRecent meaningful events:\n- User: test task\n- Tool: apply_patch daemon/work/session_log.go",
		}
	}
}

func TestSessionLogger_CreatesWorkItemForAgent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Readable work log", "Implemented the work log"))
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	written, err := logger.RecordAgent(&classifier.Agent{
		ID:        "main:@42",
		Name:      "codex (main:@42)",
		Project:   "zen",
		Cwd:       "/home/me/zen",
		Command:   "codex",
		State:     classifier.StateRunning,
		Summary:   "Implement the work log",
		LastLines: []string{"thinking", "working"},
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent: %v", err)
	}
	if written == nil {
		t.Fatal("written = nil")
	}
	if written.Frontmatter.Kind != brainLogKind {
		t.Fatalf("kind = %q", written.Frontmatter.Kind)
	}
	if written.Frontmatter.AgentSource != "codex" {
		t.Fatalf("agent_source = %q", written.Frontmatter.AgentSource)
	}
	if written.Frontmatter.Status != "running" {
		t.Fatalf("status = %q", written.Frontmatter.Status)
	}
	if written.Frontmatter.Done != nil {
		t.Fatalf("done = %v, want nil", written.Frontmatter.Done)
	}
	stored, ok := store.GetByID(written.ID)
	if !ok {
		t.Fatal("stored work item missing")
	}
	if stored.Frontmatter.Title != "zen" {
		t.Fatalf("title = %q", stored.Frontmatter.Title)
	}
	if !strings.Contains(stored.Body, autoBlockStart) || !strings.Contains(stored.Body, "Implemented the work log") || !strings.Contains(stored.Body, "## 2026-05-08") {
		t.Fatalf("body = %q", stored.Body)
	}
	if strings.Contains(stored.Body, "## Recent Output") {
		t.Fatalf("raw output section should not be visible: %q", stored.Body)
	}
	if filepath.Base(filepath.Dir(stored.Path)) != "zen" {
		t.Fatalf("path = %q", stored.Path)
	}
	if filepath.Base(stored.Path) != "brain.md" {
		t.Fatalf("path = %q", stored.Path)
	}
}

func TestSessionLogger_WritesDiagnosticReadout(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, AgentDigestProviderFunc(func(_ context.Context, input AgentDigestInput) (AgentDigest, error) {
		return AgentDigest{
			Title:    "Boundary clarified",
			Outcome:  "Implemented the narrow fix and left release packaging untouched.",
			Readout:  "The agent stayed inside the requested surface instead of broadening the refactor.",
			Signals:  []string{"Used one focused test to lock the behavior.", "Avoided unrelated formatting churn."},
			Friction: "The initial request did not define the release boundary.",
			Cause:    "scope: packaging and product behavior were mixed in one request.",
			Insight:  "Split behavior changes from release chores when assigning agents.",
			Next:     "Ask for the behavior patch first, then request packaging after verification.",
			Provider: "test",
		}, nil
	}))
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	written, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@43",
		Name:    "codex (main:@43)",
		Project: "zen",
		Cwd:     "/home/me/zen",
		Command: "codex",
		State:   classifier.StateRunning,
		Summary: "fixed behavior",
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent: %v", err)
	}
	if written.Frontmatter.Outcome == "" || written.Frontmatter.Insight == "" {
		t.Fatalf("diagnostic frontmatter missing: %+v", written.Frontmatter)
	}
	for _, want := range []string{"#### Outcome", "#### Friction", "#### Cause", "#### Insight"} {
		if !strings.Contains(written.Body, want) {
			t.Fatalf("body missing %q: %q", want, written.Body)
		}
	}
}

func TestSessionLogger_FinalizesFailedAgent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Failed task", "The task failed"))
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	_, err = logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateRunning,
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent(create): %v", err)
	}

	later := now.Add(5 * time.Minute)
	logger.now = func() time.Time { return later }
	written, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateFailed,
		Summary: "error: failed",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(final): %v", err)
	}
	if written.Frontmatter.Status != "failed" {
		t.Fatalf("status = %q", written.Frontmatter.Status)
	}
	if written.Frontmatter.Done != nil {
		t.Fatalf("project log done = %v, want nil", written.Frontmatter.Done)
	}
	if !strings.Contains(written.Body, "The task failed") {
		t.Fatalf("body = %q", written.Body)
	}
}

func TestSessionLogger_ReplacesAutoBlockAndPreservesNotes(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Updated digest", "first"))
	logger.syncDigest = true
	transcriptExcerpt := "Transcript summary: user_turns=1\nRecent meaningful events:\n- User: first"
	logger.loadTranscript = func(agent classifier.Agent, now time.Time) ToolTranscript {
		return ToolTranscript{
			Source:    "codex",
			Path:      "/tmp/codex.jsonl",
			SessionID: strings.TrimSpace(agent.ID),
			Updated:   now,
			Excerpt:   transcriptExcerpt,
		}
	}
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	first, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateRunning,
		Summary: "first",
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent(first): %v", err)
	}

	first.Body += "\nmanual note\n"
	if _, err := store.Write(first, time.Time{}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	later := now.Add(time.Minute)
	logger.now = func() time.Time { return later }
	transcriptExcerpt = "Transcript summary: user_turns=2\nRecent meaningful events:\n- User: second"
	logger.digester = testDigestProvider("Updated digest", "second")
	second, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateBlocked,
		Summary: "second",
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent(second): %v", err)
	}
	stored, ok := store.GetByID(second.ID)
	if !ok {
		t.Fatal("stored work item missing")
	}
	if strings.Count(stored.Body, autoBlockStart) != 1 {
		t.Fatalf("auto block count in body = %q", stored.Body)
	}
	if strings.Count(stored.Body, sessionBlockStart) != 1 {
		t.Fatalf("session block count in body = %q", stored.Body)
	}
	if !strings.Contains(stored.Body, "second") {
		t.Fatalf("updated auto block missing: %q", stored.Body)
	}
	if !strings.Contains(stored.Body, "manual note") {
		t.Fatalf("manual note lost: %q", stored.Body)
	}
}

func TestSessionLogger_UsesOneProjectMarkdownWithNewestDayFirst(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProviderFromAgent())
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")

	day1 := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return day1 }
	first, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@1",
		Name:    "codex (main:@1)",
		Project: "zen",
		Cwd:     "/home/me/zen",
		Command: "codex",
		State:   classifier.StateDone,
		Summary: "First session",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(first): %v", err)
	}

	day2 := day1.Add(24 * time.Hour)
	logger.now = func() time.Time { return day2 }
	second, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@2",
		Name:    "claude (main:@2)",
		Project: "zen",
		Cwd:     "/home/me/zen",
		Command: "claude",
		State:   classifier.StateDone,
		Summary: "Second session",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(second): %v", err)
	}

	if first.Path != second.Path {
		t.Fatalf("paths differ: %q != %q", first.Path, second.Path)
	}
	stored, ok := store.GetByID(second.ID)
	if !ok {
		t.Fatal("stored project log missing")
	}
	if len(store.List()) != 1 {
		t.Fatalf("store items = %d, want 1", len(store.List()))
	}
	if strings.Count(stored.Body, sessionBlockStart) != 2 {
		t.Fatalf("session block count in body = %q", stored.Body)
	}
	newer := strings.Index(stored.Body, "## 2026-05-10")
	older := strings.Index(stored.Body, "## 2026-05-09")
	if newer < 0 || older < 0 || newer > older {
		t.Fatalf("days not newest first: %q", stored.Body)
	}
}

func TestSessionLogger_OrdersBestSessionFirstWithinDay(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, AgentDigestProviderFunc(func(_ context.Context, input AgentDigestInput) (AgentDigest, error) {
		if input.Agent.ID == "main:@low" {
			return AgentDigest{
				Title:    "Low value",
				Summary:  "A short readout",
				Provider: "test",
			}, nil
		}
		return AgentDigest{
			Title:    "High value",
			Summary:  "A stronger readout",
			Progress: []string{"Includes the reusable decision"},
			Next:     "Apply the decision",
			Provider: "test",
		}, nil
	}))
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")

	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }
	_, err = logger.RecordAgent(&classifier.Agent{
		ID:      "main:@low",
		Name:    "codex (main:@low)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateDone,
		Summary: "low",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(low): %v", err)
	}

	logger.now = func() time.Time { return now.Add(5 * time.Minute) }
	written, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@high",
		Name:    "codex (main:@high)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateDone,
		Summary: "high",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(high): %v", err)
	}

	high := strings.Index(written.Body, "### High value")
	low := strings.Index(written.Body, "### Low value")
	if high < 0 || low < 0 || high > low {
		t.Fatalf("best session should appear first within the day: %q", written.Body)
	}
}

func TestSessionLogger_DoesNotWriteFallbackWithoutDigest(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, nil)
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	written, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@pending",
		Name:    "codex (main:@pending)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateRunning,
		Summary: "waiting",
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent: %v", err)
	}
	if written != nil {
		t.Fatalf("fallback work item should not be written: %+v", written)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store should stay empty without an AI digest, got %d item(s)", len(got))
	}
}

func TestSessionLogger_SkipsAgentWithoutNativeTranscript(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Terminal only", "Should not write"))
	logger.syncDigest = true
	logger.loadTranscript = func(classifier.Agent, time.Time) ToolTranscript {
		return ToolTranscript{}
	}
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	written, err := logger.RecordAgent(&classifier.Agent{
		ID:        "main:@terminal-only",
		Name:      "codex (main:@terminal-only)",
		Project:   "zen",
		Command:   "codex",
		State:     classifier.StateRunning,
		Summary:   "working from terminal output",
		LastLines: []string{"terminal output only"},
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent: %v", err)
	}
	if written != nil {
		t.Fatalf("terminal-only agent should not create Brain item: %+v", written)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store should stay empty without native transcript, got %d item(s)", len(got))
	}
}

func TestSessionLogger_BacksOffAfterDigestFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	calls := 0
	logger := NewSessionLogger(store, AgentDigestProviderFunc(func(_ context.Context, input AgentDigestInput) (AgentDigest, error) {
		calls++
		return AgentDigest{}, errors.New("bad prompt")
	}))
	logger.syncDigest = true
	logger.failureBackoff = time.Hour
	enableTestTranscript(logger, "codex")
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }
	agent := &classifier.Agent{
		ID:      "main:@failing",
		Name:    "codex (main:@failing)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateRunning,
		Summary: "same evidence",
	}

	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(first): %v", err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(second): %v", err)
	}
	if calls != 1 {
		t.Fatalf("digest calls = %d, want 1 during failure backoff", calls)
	}

	now = now.Add(time.Hour)
	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(third): %v", err)
	}
	if calls != 2 {
		t.Fatalf("digest calls = %d, want 2 after failure backoff", calls)
	}
}

func TestSessionLogger_SkipsDigestWhenOnlyTerminalSnapshotChanges(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, nil)
	logger.syncDigest = true
	enableTestTranscript(logger, "codex")
	calls := 0
	logger.digester = AgentDigestProviderFunc(func(context.Context, AgentDigestInput) (AgentDigest, error) {
		calls++
		return AgentDigest{
			Title:    "Live readout",
			Summary:  "Native transcript is the durable evidence.",
			Progress: []string{"Terminal repaint should not trigger a second digest."},
			Next:     "Wait for transcript evidence to change.",
			Provider: "test",
		}, nil
	})
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }
	agent := &classifier.Agent{
		ID:        "main:@live",
		Name:      "codex (main:@live)",
		Project:   "zen",
		Command:   "codex",
		State:     classifier.StateRunning,
		Summary:   "working",
		LastLines: []string{"thinking"},
	}

	written, err := logger.RecordAgent(agent, false, true)
	if err != nil {
		t.Fatalf("RecordAgent(first): %v", err)
	}
	if written == nil {
		t.Fatal("written = nil")
	}
	now = now.Add(time.Hour)
	agent.Summary = "terminal changed"
	agent.LastLines = []string{"spinner repaint", "terminal changed"}
	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(second): %v", err)
	}
	if calls != 1 {
		t.Fatalf("digest calls = %d, want 1 when transcript is unchanged", calls)
	}
}

func TestSessionLogger_RedigestWhenNativeTranscriptChanges(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	calls := 0
	excerpt := "Transcript summary: user_turns=1\nRecent meaningful events:\n- User: first"
	logger := NewSessionLogger(store, AgentDigestProviderFunc(func(_ context.Context, input AgentDigestInput) (AgentDigest, error) {
		calls++
		return AgentDigest{
			Title:    "Readout",
			Summary:  "Transcript changed",
			Progress: []string{inputTranscriptMarker(input.Agent.ID, calls)},
			Provider: "test",
		}, nil
	}))
	logger.syncDigest = true
	logger.loadTranscript = func(agent classifier.Agent, now time.Time) ToolTranscript {
		return ToolTranscript{
			Source:    "codex",
			Path:      "/tmp/codex.jsonl",
			SessionID: strings.TrimSpace(agent.ID),
			Updated:   now,
			Excerpt:   excerpt,
		}
	}
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }
	agent := &classifier.Agent{
		ID:      "main:@transcript",
		Name:    "codex (main:@transcript)",
		Project: "zen",
		Command: "codex",
		State:   classifier.StateRunning,
		Summary: "working",
	}

	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(first): %v", err)
	}
	now = now.Add(time.Hour)
	excerpt = "Transcript summary: user_turns=2\nRecent meaningful events:\n- User: changed"
	if _, err := logger.RecordAgent(agent, false, true); err != nil {
		t.Fatalf("RecordAgent(second): %v", err)
	}
	if calls != 2 {
		t.Fatalf("digest calls = %d, want 2 after transcript changes", calls)
	}
}

func inputTranscriptMarker(sessionID string, call int) string {
	return sessionID + " call " + strconv.Itoa(call)
}

func TestSessionLogger_IgnoresPlainTerminalSessions(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Shell output", "Terminal log"))
	logger.syncDigest = true
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	written, err := logger.RecordAgent(&classifier.Agent{
		ID:        "main:@7",
		Name:      "zsh (main:@7)",
		Project:   "zen",
		Cwd:       "/home/me/zen",
		Command:   "zsh",
		State:     classifier.StateRunning,
		Summary:   "npm test",
		LastLines: []string{"npm test", "PASS"},
	}, false, true)
	if err != nil {
		t.Fatalf("RecordAgent: %v", err)
	}
	if written != nil {
		t.Fatalf("plain terminal session should not create work item: %+v", written)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store should stay empty, got %d item(s)", len(got))
	}
}
