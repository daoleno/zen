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
func MergeProgressAndClassification(agent *Agent, classified AgentState, classifiedSummary string, now time.Time) (AgentState, string) {
	if agent == nil {
		return classified, classifiedSummary
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if !agent.PaneAlive {
		return classified, classifiedSummary
	}

	if classified == StateBlocked {
		return classified, classifiedSummary
	}
	if classified == StateFailed && !ExplicitProgressProtectsAgainstPaneFailed(agent, now) {
		return classified, classifiedSummary
	}

	if agent.LastProgressAt == nil {
		return classified, classifiedSummary
	}

	switch agent.State {
	case StateDone, StateFailed, StateBlocked:
		summary := firstNonEmpty(agent.Summary, classifiedSummary)
		return agent.State, summary
	case StateRunning:
		if ProgressLeaseActive(agent, now) {
			summary := firstNonEmpty(agent.Summary, classifiedSummary)
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
func ExplicitProgressProtectsAgainstPaneFailed(agent *Agent, now time.Time) bool {
	if agent == nil || !agent.PaneAlive || agent.LastProgressAt == nil {
		return false
	}
	switch agent.State {
	case StateDone, StateFailed, StateBlocked:
		return true
	case StateRunning:
		return ProgressLeaseActive(agent, now)
	default:
		return false
	}
}

// ProgressLeaseActive reports whether Running progress is still within its lease.
// Running updates without a lease are not treated as durable activity signals.
func ProgressLeaseActive(agent *Agent, now time.Time) bool {
	if agent == nil || agent.State != StateRunning || agent.LastProgressAt == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if agent.ExpectedNextCheckAt == nil {
		return false
	}
	return !now.After(agent.ExpectedNextCheckAt.UTC())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
