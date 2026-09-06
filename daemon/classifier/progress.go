package classifier

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type WorkerProgress struct {
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

func ValidateProgress(progress WorkerProgress) (WorkerProgress, error) {
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
		return WorkerProgress{}, fmt.Errorf("invalid status %q; valid values are running, done, failed, blocked", progress.Status)
	}
	if !validProgressPhase(progress.Phase) {
		return WorkerProgress{}, fmt.Errorf("invalid phase %q; valid values are starting, reading, planning, working, verifying, reporting", progress.Phase)
	}
	if !validProgressAttention(progress.Attention) {
		return WorkerProgress{}, fmt.Errorf("invalid attention %q; valid values are none, done, blocked, failed, user_input, stale", progress.Attention)
	}
	if progress.TaskClass != "" && !validProgressTaskClass(progress.TaskClass) {
		return WorkerProgress{}, fmt.Errorf("invalid task_class %q; valid values are exploration, mechanical_change, lasting_design", progress.TaskClass)
	}
	if progress.EventKind != "" && !validProgressEventKind(progress.EventKind) {
		return WorkerProgress{}, fmt.Errorf("invalid event_kind %q; valid values are progress, invariant, artifact, risk, needs_judgment, verification, done", progress.EventKind)
	}
	if progress.DetailsJSON != "" && !json.Valid([]byte(progress.DetailsJSON)) {
		return WorkerProgress{}, fmt.Errorf("details_json must be valid JSON")
	}
	if progress.LeaseSeconds < 0 {
		return WorkerProgress{}, fmt.Errorf("lease seconds must be zero or greater")
	}
	return progress, nil
}

func ApplyProgress(worker *Worker, progress WorkerProgress, now time.Time) {
	if worker == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	previousState := worker.State
	previousPhase := worker.Phase
	previousLeaseSeconds := worker.LeaseSeconds
	previousExpectedNextCheckAt := worker.ExpectedNextCheckAt
	worker.State = ProgressState(progress)
	worker.Phase = progress.Phase
	worker.Attention = progress.Attention
	worker.NeedsAttention = ProgressNeedsAttention(progress)
	worker.Summary = truncate(strings.TrimSpace(progress.Summary), 160)
	worker.TaskClass = progress.TaskClass
	worker.EventKind = progress.EventKind
	worker.DetailsJSON = progress.DetailsJSON
	worker.LeaseSeconds = progress.LeaseSeconds
	progressAt := now.UTC()
	worker.LastProgressAt = &progressAt
	if progress.LeaseSeconds > 0 {
		expected := progressAt.Add(time.Duration(progress.LeaseSeconds) * time.Second)
		worker.ExpectedNextCheckAt = &expected
	} else {
		worker.ExpectedNextCheckAt = nil
	}
	if previousState == StateRunning &&
		worker.State == StateRunning &&
		previousPhase == worker.Phase &&
		previousExpectedNextCheckAt != nil &&
		previousExpectedNextCheckAt.After(progressAt) &&
		(worker.ExpectedNextCheckAt == nil || previousExpectedNextCheckAt.After(*worker.ExpectedNextCheckAt)) {
		expected := previousExpectedNextCheckAt.UTC()
		worker.ExpectedNextCheckAt = &expected
		worker.LeaseSeconds = previousLeaseSeconds
	}
	worker.UpdatedAt = progressAt
}

func ProgressState(progress WorkerProgress) WorkerState {
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

func ProgressNeedsAttention(progress WorkerProgress) bool {
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
