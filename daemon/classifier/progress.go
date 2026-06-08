package classifier

import (
	"fmt"
	"strings"
	"time"
)

type AgentProgress struct {
	Status       string
	Phase        string
	Attention    string
	Summary      string
	LeaseSeconds int
}

func ValidateProgress(progress AgentProgress) (AgentProgress, error) {
	progress.Status = strings.TrimSpace(progress.Status)
	progress.Phase = strings.TrimSpace(progress.Phase)
	progress.Attention = strings.TrimSpace(progress.Attention)
	progress.Summary = truncate(strings.TrimSpace(progress.Summary), 160)

	if !validProgressStatus(progress.Status) {
		return AgentProgress{}, fmt.Errorf("invalid status %q; valid values are running, done, failed, blocked", progress.Status)
	}
	if !validProgressPhase(progress.Phase) {
		return AgentProgress{}, fmt.Errorf("invalid phase %q; valid values are starting, reading, planning, working, verifying, reporting", progress.Phase)
	}
	if !validProgressAttention(progress.Attention) {
		return AgentProgress{}, fmt.Errorf("invalid attention %q; valid values are none, done, blocked, failed, user_input, stale", progress.Attention)
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
	agent.State = ProgressState(progress)
	agent.Phase = progress.Phase
	agent.Attention = progress.Attention
	agent.NeedsAttention = ProgressNeedsAttention(progress)
	agent.Summary = truncate(strings.TrimSpace(progress.Summary), 160)
	agent.LeaseSeconds = progress.LeaseSeconds
	progressAt := now.UTC()
	agent.LastProgressAt = &progressAt
	if progress.LeaseSeconds > 0 {
		expected := progressAt.Add(time.Duration(progress.LeaseSeconds) * time.Second)
		agent.ExpectedNextCheckAt = &expected
	} else {
		agent.ExpectedNextCheckAt = nil
	}
	agent.UpdatedAt = progressAt
	if previousState != agent.State {
		agent.StateVersion++
	}
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
