package classifier

import (
	"strings"
	"testing"
	"time"
)

func TestValidateProgressAcceptsStrictValues(t *testing.T) {
	progress, err := ValidateProgress(AgentProgress{
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
	cases := []AgentProgress{
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
	agent := &Agent{
		ID:      "brain-agent-worker:@1",
		State:   StateRunning,
		Summary: "previous",
	}

	ApplyProgress(agent, AgentProgress{
		Status:       "blocked",
		Phase:        "working",
		Attention:    "user_input",
		Summary:      "Need confirmation",
		TaskClass:    "lasting_design",
		EventKind:    "needs_judgment",
		DetailsJSON:  `{"question":"root design or patch"}`,
		LeaseSeconds: 300,
	}, now)

	if agent.State != StateBlocked {
		t.Fatalf("agent state = %q", agent.State)
	}
	if agent.Phase != "working" || agent.Attention != "user_input" || !agent.NeedsAttention {
		t.Fatalf("agent progress fields = %#v", agent)
	}
	if agent.Summary != "Need confirmation" {
		t.Fatalf("summary = %q", agent.Summary)
	}
	if agent.TaskClass != "lasting_design" || agent.EventKind != "needs_judgment" || agent.DetailsJSON == "" {
		t.Fatalf("semantic progress fields = %#v", agent)
	}
	if agent.LastProgressAt == nil || !agent.LastProgressAt.Equal(now) {
		t.Fatalf("last progress = %#v, want %s", agent.LastProgressAt, now)
	}
	if agent.ExpectedNextCheckAt == nil || !agent.ExpectedNextCheckAt.Equal(now.Add(300*time.Second)) {
		t.Fatalf("next check = %#v", agent.ExpectedNextCheckAt)
	}
}

func TestProgressNeedsAttentionIncludesTerminalStatuses(t *testing.T) {
	for _, progress := range []AgentProgress{
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
	if ProgressNeedsAttention(AgentProgress{Status: "running", Attention: "none"}) {
		t.Fatal("normal running progress should not need attention")
	}
}

func TestApplyProgressTruncatesSummary(t *testing.T) {
	agent := &Agent{}
	ApplyProgress(agent, AgentProgress{
		Status:    "running",
		Phase:     "working",
		Attention: "none",
		Summary:   strings.Repeat("a", 200),
	}, time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC))

	if len(agent.Summary) != 160 {
		t.Fatalf("summary length = %d, want classifier truncate limit 160", len(agent.Summary))
	}
}
