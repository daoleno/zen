package classifier

import (
	"strings"
	"time"
)

// MergeProgressAndClassification is the shared session status contract for
// Brain lifecycle progress vs pane classification.
//
// Invariants:
//   - Pane liveness alone never yields Running.
//   - Progress Running requires an active lifecycle lease (ExpectedNextCheckAt
//     still in the future). Provider adapters may still mark Running via
//     ResolveSessionStatus / MergeActivitySignal when this step leaves Unknown.
//   - Alive pane blocked always overrides progress.
//   - Alive pane failed overrides progress only when ExplicitProgressProtectsAgainstPaneFailed
//     is false; dead panes always resolve from classification.
//   - Alive panes with no durable progress signal resolve to classified state
//     (usually Unknown) before provider activity merge.
func MergeProgressAndClassification(worker *Worker, classified WorkerState, classifiedSummary string, now time.Time) (WorkerState, string) {
	if worker == nil {
		return classified, classifiedSummary
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if !worker.PaneAlive {
		return classified, classifiedSummary
	}

	if classified == StateBlocked {
		return classified, classifiedSummary
	}
	if classified == StateFailed && !ExplicitProgressProtectsAgainstPaneFailed(worker, now) {
		return classified, classifiedSummary
	}

	if worker.LastProgressAt == nil {
		return classified, classifiedSummary
	}

	switch worker.State {
	case StateDone, StateFailed, StateBlocked:
		summary := firstNonEmpty(worker.Summary, classifiedSummary)
		return worker.State, summary
	case StateRunning:
		if ProgressLeaseActive(worker, now) {
			summary := firstNonEmpty(worker.Summary, classifiedSummary)
			return StateRunning, summary
		}
		// Lease expired or missing: fall back to classification (usually Unknown).
		return classified, classifiedSummary
	default:
		return classified, classifiedSummary
	}
}

// ExplicitProgressProtectsAgainstPaneFailed reports whether an alive pane's
// heuristic failed text must yield to current explicit progress: an active
// running lease, or sticky done/failed/blocked with LastProgressAt.
func ExplicitProgressProtectsAgainstPaneFailed(worker *Worker, now time.Time) bool {
	if worker == nil || !worker.PaneAlive || worker.LastProgressAt == nil {
		return false
	}
	switch worker.State {
	case StateDone, StateFailed, StateBlocked:
		return true
	case StateRunning:
		return ProgressLeaseActive(worker, now)
	default:
		return false
	}
}

// ProgressLeaseActive reports whether Running progress is still within its lease.
// Running updates without a lease are not treated as durable activity signals.
func ProgressLeaseActive(worker *Worker, now time.Time) bool {
	if worker == nil || worker.State != StateRunning || worker.LastProgressAt == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if worker.ExpectedNextCheckAt == nil {
		return false
	}
	return !now.After(worker.ExpectedNextCheckAt.UTC())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
