package work

import (
	"context"
	"path/filepath"
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

func TestSessionLogger_CreatesWorkItemForAgent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store, testDigestProvider("Readable work log", "Implemented the work log"))
	logger.syncDigest = true
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
	if written.Frontmatter.AgentSession != "main:@42" {
		t.Fatalf("agent_session = %q", written.Frontmatter.AgentSession)
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
	if stored.Frontmatter.Title != "Readable work log" {
		t.Fatalf("title = %q", stored.Frontmatter.Title)
	}
	if !strings.Contains(stored.Body, autoBlockStart) || !strings.Contains(stored.Body, "Implemented the work log") {
		t.Fatalf("body = %q", stored.Body)
	}
	if strings.Contains(stored.Body, "## Recent Output") {
		t.Fatalf("raw output section should not be visible: %q", stored.Body)
	}
	if filepath.Base(filepath.Dir(stored.Path)) != "zen" {
		t.Fatalf("path = %q", stored.Path)
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
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	_, err = logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
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
		State:   classifier.StateFailed,
		Summary: "error: failed",
	}, true, true)
	if err != nil {
		t.Fatalf("RecordAgent(final): %v", err)
	}
	if written.Frontmatter.Status != "failed" {
		t.Fatalf("status = %q", written.Frontmatter.Status)
	}
	if written.Frontmatter.Done == nil || !written.Frontmatter.Done.Equal(later) {
		t.Fatalf("done = %v, want %v", written.Frontmatter.Done, later)
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
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	first, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
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
	logger.digester = testDigestProvider("Updated digest", "second")
	second, err := logger.RecordAgent(&classifier.Agent{
		ID:      "main:@42",
		Name:    "codex (main:@42)",
		Project: "zen",
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
	if !strings.Contains(stored.Body, "second") {
		t.Fatalf("updated auto block missing: %q", stored.Body)
	}
	if !strings.Contains(stored.Body, "manual note") {
		t.Fatalf("manual note lost: %q", stored.Body)
	}
}
