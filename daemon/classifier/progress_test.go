package classifier

import (
	"strings"
	"testing"
	"time"
)

func TestValidateProgressAcceptsStrictValues(t *testing.T) {
	progress, err := ValidateProgress(WorkerProgress{
		Status:       "running",
		Phase:        "working",
		Attention:    "none",
		Summary:      "Adding close guard",
		TaskClass:    "lasting_design",
		EventKind:    "invariant",
		DetailsJSON:  `{"invariants":["state is durable"]}`,
		LeaseSeconds: 900,
	})
	if err != nil {
		t.Fatalf("ValidateProgress returned error: %v", err)
	}
	if progress.Status != "running" || progress.Phase != "working" || progress.Attention != "none" {
		t.Fatalf("progress = %#v", progress)
	}
	if progress.Summary != "Adding close guard" || progress.LeaseSeconds != 900 {
		t.Fatalf("progress metadata = %#v", progress)
	}
	if progress.TaskClass != "lasting_design" || progress.EventKind != "invariant" || progress.DetailsJSON == "" {
		t.Fatalf("semantic progress metadata = %#v", progress)
	}
}

func TestValidateProgressRejectsAliasesAndCamelCaseLeaseIsNotAField(t *testing.T) {
	cases := []WorkerProgress{
		{Status: "completed", Phase: "working", Attention: "none"},
		{Status: "running", Phase: "coding", Attention: "none"},
		{Status: "running", Phase: "working", Attention: "waiting"},
		{Status: "RUNNING", Phase: "working", Attention: "none"},
		{Status: "running", Phase: "working", Attention: "none", LeaseSeconds: -1},
		{Status: "running", Phase: "working", Attention: "none", TaskClass: "bugfix"},
		{Status: "running", Phase: "working", Attention: "none", EventKind: "checkpoint"},
		{Status: "running", Phase: "working", Attention: "none", DetailsJSON: `{"broken"`},
	}

	for _, progress := range cases {
		if got, err := ValidateProgress(progress); err == nil {
			t.Fatalf("ValidateProgress(%#v) = %#v, want error", progress, got)
		}
	}
}

func TestApplyProgressUpdatesLifecycleFields(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	worker := &Worker{
		ID:      "zen-worker-worker:@1",
		State:   StateRunning,
		Summary: "previous",
	}

	ApplyProgress(worker, WorkerProgress{
		Status:       "blocked",
		Phase:        "working",
		Attention:    "user_input",
		Summary:      "Need confirmation",
		TaskClass:    "lasting_design",
		EventKind:    "needs_judgment",
		DetailsJSON:  `{"question":"root design or patch"}`,
		LeaseSeconds: 300,
	}, now)

	if worker.State != StateBlocked {
		t.Fatalf("agent state = %q", worker.State)
	}
	if worker.Phase != "working" || worker.Attention != "user_input" || !worker.NeedsAttention {
		t.Fatalf("worker progress fields = %#v", worker)
	}
	if worker.Summary != "Need confirmation" {
		t.Fatalf("summary = %q", worker.Summary)
	}
	if worker.TaskClass != "lasting_design" || worker.EventKind != "needs_judgment" || worker.DetailsJSON == "" {
		t.Fatalf("semantic progress fields = %#v", worker)
	}
	if worker.LastProgressAt == nil || !worker.LastProgressAt.Equal(now) {
		t.Fatalf("last progress = %#v, want %s", worker.LastProgressAt, now)
	}
	if worker.ExpectedNextCheckAt == nil || !worker.ExpectedNextCheckAt.Equal(now.Add(300*time.Second)) {
		t.Fatalf("next check = %#v", worker.ExpectedNextCheckAt)
	}
}

func TestApplyProgressDoesNotShortenActiveLeaseWithinRunningPhase(t *testing.T) {
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	longDeadline := start.Add(900 * time.Second)
	worker := &Worker{
		State:               StateRunning,
		Phase:               "working",
		LeaseSeconds:        900,
		ExpectedNextCheckAt: &longDeadline,
	}

	ApplyProgress(worker, WorkerProgress{
		Status:       "running",
		Phase:        "working",
		Attention:    "none",
		Summary:      "Routine progress",
		LeaseSeconds: 300,
	}, start.Add(time.Minute))

	if worker.ExpectedNextCheckAt == nil || !worker.ExpectedNextCheckAt.Equal(longDeadline) {
		t.Fatalf("routine progress shortened active lease to %#v, want %s", worker.ExpectedNextCheckAt, longDeadline)
	}
	if worker.LeaseSeconds != 900 {
		t.Fatalf("effective lease seconds = %d, want preserved 900", worker.LeaseSeconds)
	}

	ApplyProgress(worker, WorkerProgress{
		Status:       "running",
		Phase:        "verifying",
		Attention:    "none",
		Summary:      "Phase changed",
		LeaseSeconds: 300,
	}, start.Add(2*time.Minute))
	wantPhaseDeadline := start.Add(7 * time.Minute)
	if worker.ExpectedNextCheckAt == nil || !worker.ExpectedNextCheckAt.Equal(wantPhaseDeadline) {
		t.Fatalf("new phase deadline = %#v, want %s", worker.ExpectedNextCheckAt, wantPhaseDeadline)
	}
	if worker.LeaseSeconds != 300 {
		t.Fatalf("new phase lease seconds = %d, want 300", worker.LeaseSeconds)
	}
}

func TestProgressNeedsAttentionIncludesTerminalStatuses(t *testing.T) {
	for _, progress := range []WorkerProgress{
		{Status: "done", Attention: "none"},
		{Status: "failed", Attention: "none"},
		{Status: "blocked", Attention: "none"},
		{Status: "running", Attention: "user_input"},
		{Status: "running", Attention: "stale"},
	} {
		if !ProgressNeedsAttention(progress) {
			t.Fatalf("ProgressNeedsAttention(%#v) = false, want true", progress)
		}
	}
	if ProgressNeedsAttention(WorkerProgress{Status: "running", Attention: "none"}) {
		t.Fatal("normal running progress should not need attention")
	}
}

func TestApplyProgressTruncatesSummary(t *testing.T) {
	worker := &Worker{}
	ApplyProgress(worker, WorkerProgress{
		Status:    "running",
		Phase:     "working",
		Attention: "none",
		Summary:   strings.Repeat("a", 200),
	}, time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC))

	if len(worker.Summary) != 160 {
		t.Fatalf("summary length = %d, want classifier truncate limit 160", len(worker.Summary))
	}
}
