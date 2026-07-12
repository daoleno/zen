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
//   - Progress outcomes done/failed/blocked persist while the pane is alive
//     unless classification reports blocked or failed (which wins).
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

	// Pane-derived attention/failure always overrides sticky progress.
	if classified == StateBlocked || classified == StateFailed {
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
