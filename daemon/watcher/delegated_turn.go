package watcher

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// delegatedTurnOption is the pre-protocol tmux @zen_delegated_turn marker.
// After the one-shot migration it is never written again; the canonical
// ledger owns turn lifecycle state.
const delegatedTurnOption = "zen_delegated_turn"

// ProviderProbeState distinguishes a successful read (possibly with no new
// fact) from a provably unlocatable or unreadable transcript source. Only
// loss states may drive the bounded provider-evidence session.uncertain;
// "no new fact yet" is never a loss.
type ProviderProbeState string

const (
	// ProbeStateOK means the probe read the provider source successfully;
	// the observation may carry no new fact (healthy, no evidence yet).
	ProbeStateOK ProviderProbeState = ""
	// ProbeStateUnlocatable means the transcript source cannot be found
	// (missing session file, missing rollout, absent session row).
	ProbeStateUnlocatable ProviderProbeState = "unlocatable"
	// ProbeStateUnreadable means the source exists but cannot be read or
	// parsed (stat/open/sqlite/parse failure, WAL lag past the reader guard).
	ProbeStateUnreadable ProviderProbeState = "unreadable"
)

// Loss reports whether the probe state is a bounded evidence loss (as opposed
// to a successful read with no new fact).
func (s ProviderProbeState) Loss() bool {
	return s == ProbeStateUnlocatable || s == ProbeStateUnreadable
}

// ProviderActivityObservation is a provider-neutral view of daemon/work's
// native Activity. Watcher consumes it through an injected probe so the work
// package remains the single parser and lifecycle truth owner. It is
// diagnostic evidence only: the canonical per-turn reducer (brain.Store) is
// the only lifecycle state machine.
type ProviderActivityObservation struct {
	ID              string
	Status          string
	StartedAt       time.Time
	SettledAt       time.Time
	AdmissionStream string
	AdmissionID     string
	AdmissionCursor uint64
	AdmissionAt     time.Time
	InputSHA256     string
	Structured      bool
	FallbackAllowed bool
	// ProbeState is the channel-health classification of this observation:
	// OK (read succeeded, possibly no new fact) vs unlocatable/unreadable.
	ProbeState ProviderProbeState
}

type ProviderActivityProbe interface {
	ObserveProviderActivity(agent classifier.Agent, now time.Time) ProviderActivityObservation
	ForgetProviderActivity(agentID string)
}

func delegatedTurnIdentity(identity targetProcessIdentity) string {
	raw := fmt.Sprintf(
		"%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d",
		identity.Command,
		identity.PanePID,
		identity.PaneStart,
		identity.ForegroundID,
		identity.ForegroundStart,
		identity.ProcessID,
		identity.ProcessStart,
	)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum[:])
}

func delegatedTurnPaneIdentity(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

// providerFactSourceID derives the stable provider source identity for the
// frozen deterministic FactID formula (C.3.1): the adapter's native durable
// event/message identity plus its monotone cursor. No wall-clock observation
// time and no per-run UUID appear, so restart re-read and reordered replay
// dedupe identically.
func providerFactSourceID(sessionID string, observation ProviderActivityObservation) string {
	return fmt.Sprintf(
		"provider\x00%s\x00%s\x00%s\x00%d",
		strings.TrimSpace(sessionID),
		firstNonEmptyString(observation.AdmissionStream, "stream"),
		firstNonEmptyString(observation.ID, observation.AdmissionID),
		observation.AdmissionCursor,
	)
}

func admissionFromObservation(observation ProviderActivityObservation) TurnAdmission {
	admissionAt := observation.AdmissionAt
	if admissionAt.IsZero() {
		// Adapters without a dedicated admission timestamp anchor the window
		// on the activity start: the input began before the activity.
		admissionAt = observation.StartedAt
	}
	return TurnAdmission{
		Stream: strings.TrimSpace(observation.AdmissionStream),
		ID:     strings.TrimSpace(observation.AdmissionID),
		Cursor: observation.AdmissionCursor,
		SHA256: strings.TrimSpace(observation.InputSHA256),
		At:     admissionAt.UTC(),
	}
}
