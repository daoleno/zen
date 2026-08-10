package classifier

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AgentProgress struct {
	// TurnID is the random identity printed in one delegated prompt. It is
	// empty for ordinary/provider-native Sessions that do not participate in
	// the delegated signal contract.
	TurnID       string
	Status       string
	Phase        string
	Attention    string
	Summary      string
	TaskClass    string
	EventKind    string
	DetailsJSON  string
	LeaseSeconds int
	// ProgressEventID is the caller-minted logical event identity: created
	// once per logical progress submission and reused on transport retry, so
	// identical later heartbeats are distinct facts while a retry dedupes.
	// It is audit metadata for the deterministic FactID (C.3.1); the payload
	// hash is never identity.
	ProgressEventID string
}

func ValidateProgress(progress AgentProgress) (AgentProgress, error) {
	progress.TurnID = strings.TrimSpace(progress.TurnID)
	progress.Status = strings.TrimSpace(progress.Status)
	progress.Phase = strings.TrimSpace(progress.Phase)
	progress.Attention = strings.TrimSpace(progress.Attention)
	progress.Summary = truncate(strings.TrimSpace(progress.Summary), 160)
	progress.TaskClass = strings.TrimSpace(progress.TaskClass)
	progress.EventKind = strings.TrimSpace(progress.EventKind)
	progress.DetailsJSON = strings.TrimSpace(progress.DetailsJSON)
	progress.ProgressEventID = strings.TrimSpace(progress.ProgressEventID)

	if !validProgressStatus(progress.Status) {
		return AgentProgress{}, fmt.Errorf("invalid status %q; valid values are running, done, failed, blocked", progress.Status)
	}
	if !validProgressPhase(progress.Phase) {
		return AgentProgress{}, fmt.Errorf("invalid phase %q; valid values are starting, reading, planning, working, verifying, reporting", progress.Phase)
	}
	if !validProgressAttention(progress.Attention) {
		return AgentProgress{}, fmt.Errorf("invalid attention %q; valid values are none, done, blocked, failed, user_input, stale", progress.Attention)
	}
	if progress.TaskClass != "" && !validProgressTaskClass(progress.TaskClass) {
		return AgentProgress{}, fmt.Errorf("invalid task_class %q; valid values are exploration, mechanical_change, lasting_design", progress.TaskClass)
	}
	if progress.EventKind != "" && !validProgressEventKind(progress.EventKind) {
		return AgentProgress{}, fmt.Errorf("invalid event_kind %q; valid values are progress, invariant, artifact, risk, needs_judgment, verification, done", progress.EventKind)
	}
	if progress.DetailsJSON != "" && !json.Valid([]byte(progress.DetailsJSON)) {
		return AgentProgress{}, fmt.Errorf("details_json must be valid JSON")
	}
	if progress.LeaseSeconds < 0 {
		return AgentProgress{}, fmt.Errorf("lease seconds must be zero or greater")
	}
	return progress, nil
}

func ApplyProgress(agent *Agent, progress AgentProgress, now time.Time) {
	if agent == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	previousState := agent.State
	previousPhase := agent.Phase
	previousLeaseSeconds := agent.LeaseSeconds
	previousExpectedNextCheckAt := agent.ExpectedNextCheckAt
	agent.State = ProgressState(progress)
	agent.Phase = progress.Phase
	agent.Attention = progress.Attention
	agent.NeedsAttention = ProgressNeedsAttention(progress)
	agent.Summary = truncate(strings.TrimSpace(progress.Summary), 160)
	agent.TaskClass = progress.TaskClass
	agent.EventKind = progress.EventKind
	agent.DetailsJSON = progress.DetailsJSON
	agent.LeaseSeconds = progress.LeaseSeconds
	progressAt := now.UTC()
	agent.LastProgressAt = &progressAt
	if progress.LeaseSeconds > 0 {
		expected := progressAt.Add(time.Duration(progress.LeaseSeconds) * time.Second)
		agent.ExpectedNextCheckAt = &expected
	} else {
		agent.ExpectedNextCheckAt = nil
	}
	if previousState == StateRunning &&
		agent.State == StateRunning &&
		previousPhase == agent.Phase &&
		previousExpectedNextCheckAt != nil &&
		previousExpectedNextCheckAt.After(progressAt) &&
		(agent.ExpectedNextCheckAt == nil || previousExpectedNextCheckAt.After(*agent.ExpectedNextCheckAt)) {
		expected := previousExpectedNextCheckAt.UTC()
		agent.ExpectedNextCheckAt = &expected
		agent.LeaseSeconds = previousLeaseSeconds
	}
	agent.UpdatedAt = progressAt
}

func ProgressState(progress AgentProgress) AgentState {
	switch progress.Status {
	case "done":
		return StateDone
	case "failed":
		return StateFailed
	case "blocked":
		return StateBlocked
	case "running":
		return StateRunning
	default:
		return StateUnknown
	}
}

func ProgressNeedsAttention(progress AgentProgress) bool {
	switch progress.Status {
	case "done", "blocked", "failed":
		return true
	}
	switch progress.Attention {
	case "done", "blocked", "failed", "user_input", "stale":
		return true
	default:
		return false
	}
}

func validProgressStatus(value string) bool {
	switch value {
	case "running", "done", "failed", "blocked":
		return true
	default:
		return false
	}
}

func validProgressPhase(value string) bool {
	switch value {
	case "starting", "reading", "planning", "working", "verifying", "reporting":
		return true
	default:
		return false
	}
}

func validProgressAttention(value string) bool {
	switch value {
	case "none", "done", "blocked", "failed", "user_input", "stale":
		return true
	default:
		return false
	}
}

func validProgressTaskClass(value string) bool {
	switch value {
	case "exploration", "mechanical_change", "lasting_design":
		return true
	default:
		return false
	}
}

func validProgressEventKind(value string) bool {
	switch value {
	case "progress", "invariant", "artifact", "risk", "needs_judgment", "verification", "done":
		return true
	default:
		return false
	}
}
