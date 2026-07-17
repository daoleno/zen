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

func TestQueryCodexThreadsUsesMinimalSourceSchema(t *testing.T) {
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
	if row := rows[0]; row.ID != "thread-1" || row.RolloutPath != "/tmp/rollout-1.jsonl" ||
		row.CreatedAt != 100 || row.CreatedAtMS != 100000 {
		t.Fatalf("minimal source row = %#v", row)
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

func TestFindCodexTranscriptAllowsStaleOpenRollout(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := filepath.Join(t.TempDir(), "freeride")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd): %v", err)
	}
	rolloutPath := filepath.Join(home, ".codex", "sessions", "2026", "06", "22", "rollout-2026-06-22T16-07-10-019eee5e-7dec-71b1-bc2b-adcb2bad1c4c.jsonl")
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(rollout): %v", err)
	}
	writeJSONL(t, rolloutPath,
		map[string]any{
			"type": "session_meta",
			"payload": map[string]any{
				"id":         "019eee5e-7dec-71b1-bc2b-adcb2bad1c4c",
				"cwd":        cwd,
				"originator": "codex-tui",
			},
		},
		map[string]any{
			"type": "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "Improve documentation in @filename",
			},
		},
	)

	now := time.Date(2026, 6, 27, 2, 30, 0, 0, time.Local)
	stale := now.Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(rolloutPath, stale, stale); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(db): %v", err)
	}
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
`)
	insert := `INSERT INTO threads (id, rollout_path, created_at, updated_at, cwd, archived, created_at_ms, updated_at_ms)
VALUES ('019eee5e-7dec-71b1-bc2b-adcb2bad1c4c', ` + sqlString(rolloutPath) + `, 1782115630, 1782116683, ` + sqlString(cwd) + `, 0, 1782115630572, 1782116683915);`
	runSQLite(t, sqlite3, dbPath, insert)

	file, err := os.Open(rolloutPath)
	if err != nil {
		t.Fatalf("Open rollout: %v", err)
	}
	defer file.Close()

	got, ok, err := findCodexTranscript(classifier.Agent{
		ID:        "brain-agent-brain-1781166359611353356:@3747",
		Name:      "node (brain-agent-brain-1781166359611353356:@3747)",
		Cwd:       cwd,
		Command:   "codex",
		ProcessID: os.Getpid(),
		StartedAt: now.Add(-5 * 24 * time.Hour),
	}, now)
	if err != nil {
		t.Fatalf("findCodexTranscript: %v", err)
	}
	if !ok {
		t.Fatal("expected stale rollout to match while current process has it open")
	}
	if got.Path != rolloutPath || got.Row.ID != "019eee5e-7dec-71b1-bc2b-adcb2bad1c4c" {
		t.Fatalf("matched %+v, want rollout %q", got, rolloutPath)
	}
}

func TestFreshCodexTranscriptCandidatesFiltersStaleNonOpenCandidates(t *testing.T) {
	now := time.Date(2026, 6, 27, 2, 30, 0, 0, time.UTC)
	candidates := []codexTranscriptCandidate{
		{Row: codexThreadRow{ID: "stale"}, Updated: now.Add(-5 * 24 * time.Hour)},
		{Row: codexThreadRow{ID: "fresh"}, Updated: now.Add(-time.Hour)},
	}

	got := freshCodexTranscriptCandidates(candidates, now)
	if len(got) != 1 || got[0].Row.ID != "fresh" {
		t.Fatalf("fresh candidates = %#v, want only fresh", got)
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
