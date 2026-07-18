package classifier

import (
	"testing"
	"time"
)

func TestMergeProgressAndClassification_OrdinaryShellStaysUnknown(t *testing.T) {
	agent := &Agent{
		PaneAlive: true,
		State:     StateUnknown,
	}
	classified, summary := Classify(true, []string{"$ echo hi", "hi", "$"}, "")
	got, _ := MergeProgressAndClassification(agent, classified, summary, time.Now().UTC())
	if got != StateUnknown {
		t.Fatalf("state = %q, want unknown for ordinary shell", got)
	}
	if got == StateRunning {
		t.Fatal("ordinary alive pane must never resolve to running")
	}
}

func TestMergeProgressAndClassification_ActiveLeaseKeepsRunning(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-30 * time.Second)
	leaseUntil := now.Add(270 * time.Second)
	agent := &Agent{
		PaneAlive:           true,
		State:               StateRunning,
		Summary:             "Working on fix",
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &leaseUntil,
		LeaseSeconds:        300,
	}
	got, summary := MergeProgressAndClassification(agent, StateUnknown, "shell noise", now)
	if got != StateRunning {
		t.Fatalf("state = %q, want running under active lease", got)
	}
	if summary != "Working on fix" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestMergeProgressAndClassification_ExpiredLeaseFallsToUnknown(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-10 * time.Minute)
	leaseUntil := now.Add(-1 * time.Minute)
	agent := &Agent{
		PaneAlive:           true,
		State:               StateRunning,
		Summary:             "Still claimed running",
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &leaseUntil,
		LeaseSeconds:        300,
	}
	// Fresh pane churn after expiry must not keep Running.
	classified, summary := Classify(true, []string{"compiling...", "done.", "$"}, "")
	got, _ := MergeProgressAndClassification(agent, classified, summary, now)
	if got != StateUnknown {
		t.Fatalf("state = %q, want unknown after lease expiry", got)
	}
	if got == StateRunning {
		t.Fatal("expired running lease must not stay running")
	}
}

func TestMergeProgressAndClassification_LeaseBoundaryInclusive(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-5 * time.Minute)
	agent := &Agent{
		PaneAlive:           true,
		State:               StateRunning,
		Summary:             "On the wire",
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &now, // exactly now: still active (!After)
		LeaseSeconds:        300,
	}
	got, _ := MergeProgressAndClassification(agent, StateUnknown, "noise", now)
	if got != StateRunning {
		t.Fatalf("state = %q, want running when now == ExpectedNextCheckAt", got)
	}
	oneNsLater := now.Add(time.Nanosecond)
	got, _ = MergeProgressAndClassification(agent, StateUnknown, "noise", oneNsLater)
	if got != StateUnknown {
		t.Fatalf("state = %q, want unknown one ns after lease end", got)
	}
}

func TestMergeProgressAndClassification_RunningWithoutLeaseIsNotDurable(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now
	agent := &Agent{
		PaneAlive:      true,
		State:          StateRunning,
		Summary:        "No lease",
		LastProgressAt: &progressAt,
		LeaseSeconds:   0,
	}
	got, _ := MergeProgressAndClassification(agent, StateUnknown, "idle", now)
	if got != StateUnknown {
		t.Fatalf("state = %q, want unknown when running progress has no lease", got)
	}
}

func TestMergeProgressAndClassification_DelegatedDoneSticksWhileAlive(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-2 * time.Minute)
	agent := &Agent{
		PaneAlive:      true,
		State:          StateDone,
		Summary:        "Delegated work finished",
		Delegated:      true,
		LastProgressAt: &progressAt,
	}
	classified, summary := Classify(true, []string{"$ ", "done output", "$"}, "")
	got, gotSummary := MergeProgressAndClassification(agent, classified, summary, now)
	if got != StateDone {
		t.Fatalf("state = %q, want done for completed delegated session", got)
	}
	if gotSummary != "Delegated work finished" {
		t.Fatalf("summary = %q", gotSummary)
	}
}

func TestMergeProgressAndClassification_FailedProgressSticks(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	agent := &Agent{
		PaneAlive:      true,
		State:          StateFailed,
		Summary:        "Agent failed",
		LastProgressAt: &progressAt,
	}
	got, _ := MergeProgressAndClassification(agent, StateUnknown, "prompt", now)
	if got != StateFailed {
		t.Fatalf("state = %q, want failed", got)
	}
}

func TestMergeProgressAndClassification_BlockedProgressSticks(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	agent := &Agent{
		PaneAlive:      true,
		State:          StateBlocked,
		Summary:        "Need a decision",
		Attention:      "user_input",
		LastProgressAt: &progressAt,
	}
	got, summary := MergeProgressAndClassification(agent, StateUnknown, "shell prompt", now)
	if got != StateBlocked {
		t.Fatalf("state = %q, want blocked", got)
	}
	if summary != "Need a decision" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestMergeProgressAndClassification_PriorityTable(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	activeLease := now.Add(2 * time.Minute)
	expiredLease := now.Add(-time.Minute)

	tests := []struct {
		name       string
		agent      *Agent
		classified AgentState
		want       AgentState
	}{
		{
			name: "classify blocked overrides active running lease",
			agent: &Agent{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "working",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &activeLease,
			},
			classified: StateBlocked,
			want:       StateBlocked,
		},
		{
			name: "active running lease outranks classify failed",
			agent: &Agent{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "working",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &activeLease,
			},
			classified: StateFailed,
			want:       StateRunning,
		},
		{
			name: "classify blocked overrides sticky done",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateDone,
				Summary:        "Was done",
				LastProgressAt: &progressAt,
			},
			classified: StateBlocked,
			want:       StateBlocked,
		},
		{
			name: "sticky done outranks classify failed",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateDone,
				Summary:        "Was done",
				LastProgressAt: &progressAt,
			},
			classified: StateFailed,
			want:       StateDone,
		},
		{
			name: "classify unknown keeps sticky done",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateDone,
				Summary:        "Delegated finished",
				LastProgressAt: &progressAt,
			},
			classified: StateUnknown,
			want:       StateDone,
		},
		{
			name: "classify unknown keeps sticky failed",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateFailed,
				Summary:        "Boom",
				LastProgressAt: &progressAt,
			},
			classified: StateUnknown,
			want:       StateFailed,
		},
		{
			name: "expired running + classify unknown => unknown",
			agent: &Agent{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "stale running",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &expiredLease,
			},
			classified: StateUnknown,
			want:       StateUnknown,
		},
		{
			name: "alive pane with no progress stays classified unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
			},
			classified: StateUnknown,
			want:       StateUnknown,
		},
		{
			name: "dead pane uses classified done",
			agent: &Agent{
				PaneAlive:           false,
				State:               StateRunning,
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &activeLease,
			},
			classified: StateDone,
			want:       StateDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := MergeProgressAndClassification(tt.agent, tt.classified, "detail", now)
			if got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeProgressAndClassification_ClassifyBlockedOverridesDone(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	agent := &Agent{
		PaneAlive:      true,
		State:          StateDone,
		Summary:        "Was done",
		LastProgressAt: &progressAt,
	}
	got, _ := MergeProgressAndClassification(agent, StateBlocked, "Do you want to proceed? (Y/n)", now)
	if got != StateBlocked {
		t.Fatalf("state = %q, want blocked override", got)
	}
}

// Grok always-approve chrome must classify as Unknown so an active progress
// lease can keep the session Running instead of being wiped as blocked.
func TestResolveSessionStatus_GrokAlwaysApproveChromeKeepsRunningLease(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-30 * time.Second)
	leaseUntil := now.Add(270 * time.Second)
	agent := &Agent{
		PaneAlive:           true,
		State:               StateRunning,
		Summary:             "Reading delegated lifecycle",
		Command:             "grok",
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &leaseUntil,
		LeaseSeconds:        300,
		Delegated:           true,
	}
	lines := []string{
		"│ ❯                                                                        │",
		"╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
		"Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+x:shortcuts",
	}
	classified, classifiedSummary := Classify(true, lines, agent.Command)
	if classified != StateUnknown {
		t.Fatalf("classified = %q (%q), want unknown for always-approve chrome", classified, classifiedSummary)
	}
	got, summary := ResolveSessionStatus(agent, classified, classifiedSummary, now, ActivitySignal{})
	if got != StateRunning {
		t.Fatalf("resolved = %q, want running under lease", got)
	}
	if summary != "Reading delegated lifecycle" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestProgressLeaseActive(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	agent := &Agent{State: StateRunning, LastProgressAt: &progressAt, ExpectedNextCheckAt: &future}
	if !ProgressLeaseActive(agent, now) {
		t.Fatal("expected active lease")
	}
	agent.ExpectedNextCheckAt = &past
	if ProgressLeaseActive(agent, now) {
		t.Fatal("expected expired lease")
	}
	agent.ExpectedNextCheckAt = nil
	if ProgressLeaseActive(agent, now) {
		t.Fatal("missing lease must not be active")
	}
}
