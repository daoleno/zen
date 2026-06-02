package server

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/work"
)

func TestDecorateBrainSnapshotAddsProviderNeutralWorkAttention(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "failed",
		Source:    "codex",
		Status:    "failed",
		AIUpdated: base.Add(2 * time.Hour),
	})
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "blocked",
		Source:    "claude",
		Status:    "blocked",
		AIUpdated: base.Add(4 * time.Hour),
	})
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "ai-error",
		Source:    "claude",
		AIError:   "agent crashed",
		AIUpdated: base.Add(3 * time.Hour),
	})
	done := base.Add(5 * time.Hour)
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "done",
		Source:    "codex",
		Status:    "failed",
		Done:      &done,
		AIUpdated: base.Add(6 * time.Hour),
	})
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "running",
		Source:    "codex",
		Status:    "running",
		AIUpdated: base.Add(7 * time.Hour),
	})
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "external",
		Source:    "human",
		Status:    "failed",
		AIUpdated: base.Add(8 * time.Hour),
	})
	writeBrainLogWorkItem(t, store, brainLogWorkItem{
		ID:        "task-kind",
		Kind:      "task",
		Source:    "codex",
		Status:    "failed",
		AIUpdated: base.Add(9 * time.Hour),
	})

	srv := &Server{work: store}
	snapshot := brain.Snapshot{
		AttentionQueue: []brain.AttentionQueueItem{
			{
				ID:       "thread:codex:review",
				Kind:     "review_thread",
				Title:    "Review thread",
				ThreadID: "codex:review",
				Updated:  base.Add(10 * time.Hour),
			},
			{
				ID:      "agent:blocked",
				Kind:    "blocked_agent",
				Title:   "Blocked agent",
				AgentID: "blocked",
				Status:  "blocked",
				Updated: base,
			},
		},
	}

	decorated := srv.decorateBrainSnapshot(snapshot)

	got := make([]string, 0, len(decorated.AttentionQueue))
	for _, item := range decorated.AttentionQueue {
		got = append(got, item.ID)
	}
	want := []string{
		"agent:blocked",
		"work:ai-error",
		"work:failed",
		"work:blocked",
		"thread:codex:review",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attention queue order = %#v, want %#v", got, want)
	}

	byID := map[string]brain.AttentionQueueItem{}
	for _, item := range decorated.AttentionQueue {
		byID[item.ID] = item
	}
	if byID["work:ai-error"].Status != "failed" || byID["work:ai-error"].Summary != "agent crashed" {
		t.Fatalf("ai error item = %+v", byID["work:ai-error"])
	}
	if byID["work:blocked"].Status != "blocked" || byID["work:blocked"].Project != "brain" {
		t.Fatalf("blocked item = %+v", byID["work:blocked"])
	}
	for _, excluded := range []string{"work:done", "work:running", "work:external", "work:task-kind"} {
		if _, ok := byID[excluded]; ok {
			t.Fatalf("excluded item %s was added to attention queue: %+v", excluded, byID[excluded])
		}
	}
}

func TestBrainWorkItemNeedsAttentionRules(t *testing.T) {
	done := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		item *work.Item
		want bool
	}{
		{name: "nil", item: nil, want: false},
		{name: "done failed", item: &work.Item{Frontmatter: work.Frontmatter{Status: "failed", Done: &done}}, want: false},
		{name: "blocked", item: &work.Item{Frontmatter: work.Frontmatter{Status: "blocked"}}, want: true},
		{name: "failed", item: &work.Item{Frontmatter: work.Frontmatter{Status: "failed"}}, want: true},
		{name: "ai error", item: &work.Item{Frontmatter: work.Frontmatter{AIError: "tool failed"}}, want: true},
		{name: "running", item: &work.Item{Frontmatter: work.Frontmatter{Status: "running"}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := brainWorkItemNeedsAttention(tt.item); got != tt.want {
				t.Fatalf("brainWorkItemNeedsAttention() = %v, want %v", got, tt.want)
			}
		})
	}
}

type brainLogWorkItem struct {
	ID        string
	Kind      string
	Source    string
	Status    string
	AIError   string
	Done      *time.Time
	AIUpdated time.Time
}

func writeBrainLogWorkItem(t *testing.T, store *work.Store, spec brainLogWorkItem) {
	t.Helper()
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" {
		kind = "brain_log"
	}
	var aiUpdated *time.Time
	if !spec.AIUpdated.IsZero() {
		updated := spec.AIUpdated
		aiUpdated = &updated
	}
	item := &work.Item{
		Path: filepath.Join(store.Root, "brain", spec.ID+".md"),
		Body: "# " + spec.ID + "\n\nBody.\n",
		Frontmatter: work.Frontmatter{
			ID:          spec.ID,
			Kind:        kind,
			Created:     time.Date(2029, 12, 31, 12, 0, 0, 0, time.UTC),
			Title:       spec.ID,
			Summary:     "summary " + spec.ID,
			Status:      spec.Status,
			AgentSource: spec.Source,
			AIError:     spec.AIError,
			AIUpdated:   aiUpdated,
			Done:        spec.Done,
		},
	}
	if _, err := store.Write(item, time.Time{}); err != nil {
		t.Fatal(err)
	}
}
