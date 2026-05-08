package work

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestSessionLogger_CreatesWorkItemForAgent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store)
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
	if !strings.Contains(written.Body, autoBlockStart) || !strings.Contains(written.Body, "Implement the work log") {
		t.Fatalf("body = %q", written.Body)
	}
	if filepath.Base(filepath.Dir(written.Path)) != "zen" {
		t.Fatalf("path = %q", written.Path)
	}
}

func TestSessionLogger_FinalizesFailedAgent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	logger := NewSessionLogger(store)
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

	logger := NewSessionLogger(store)
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
	if strings.Count(second.Body, autoBlockStart) != 1 {
		t.Fatalf("auto block count in body = %q", second.Body)
	}
	if !strings.Contains(second.Body, "second") {
		t.Fatalf("updated auto block missing: %q", second.Body)
	}
	if !strings.Contains(second.Body, "manual note") {
		t.Fatalf("manual note lost: %q", second.Body)
	}
}
