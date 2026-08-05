package watcher

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const delegatedTurnOption = "zen_delegated_turn"
const delegatedTurnSchema = 1
const delegatedTurnGenericQuietWindow = 30 * time.Second

type delegatedTurnStatus string

const (
	delegatedTurnAmbiguous  delegatedTurnStatus = "ambiguous"
	delegatedTurnDispatched delegatedTurnStatus = "dispatched"
	delegatedTurnRunning    delegatedTurnStatus = "running"
	delegatedTurnIdle       delegatedTurnStatus = "idle"
	delegatedTurnDone       delegatedTurnStatus = "done"
	delegatedTurnFailed     delegatedTurnStatus = "failed"
)

// delegatedTurnRecord is durable metadata on the existing Session. It is not a
// scheduler object: the current accepted input receipt remains the identity,
// and Work/Event remain the only orchestration owners.
type delegatedTurnRecord struct {
	SchemaVersion    int                 `json:"schema_version"`
	ID               string              `json:"id"`
	Status           delegatedTurnStatus `json:"status"`
	AcceptedAt       time.Time           `json:"accepted_at"`
	ProcessIdentity  string              `json:"process_identity"`
	PaneBaseline     string              `json:"pane_baseline,omitempty"`
	ProviderActivity string              `json:"provider_activity,omitempty"`
	ComposerObserved bool                `json:"composer_observed,omitempty"`
	IdleSince        *time.Time          `json:"idle_since,omitempty"`
	Summary          string              `json:"summary,omitempty"`
	SettledAt        *time.Time          `json:"settled_at,omitempty"`
}

type delegatedTurnObservation struct {
	Provider     ProviderActivityObservation
	Pane         classifier.ActivitySignal
	PaneIdentity string
	Live         bool
	Now          time.Time
	StartTimeout time.Duration
}

// ProviderActivityObservation is a provider-neutral view of daemon/work's
// native Activity. Watcher consumes it through an injected probe so the work
// package remains the single parser and lifecycle truth owner.
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
}

type ProviderActivityProbe interface {
	ObserveProviderActivity(agent classifier.Agent, now time.Time) ProviderActivityObservation
	ForgetProviderActivity(agentID string)
}

func providerActivityForDelegatedTurn(
	agent classifier.Agent,
	now time.Time,
	turn delegatedTurnRecord,
	found bool,
	turnErr error,
	probe ProviderActivityProbe,
) (ProviderActivityObservation, bool) {
	if turnErr != nil || !found || delegatedTurnTerminal(turn.Status) {
		return ProviderActivityObservation{}, false
	}
	if probe == nil {
		return ProviderActivityObservation{FallbackAllowed: true}, true
	}
	return probe.ObserveProviderActivity(agent, now), true
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

func validateDelegatedTurn(turn delegatedTurnRecord) error {
	if turn.SchemaVersion != delegatedTurnSchema {
		return fmt.Errorf("unsupported delegated turn schema %d", turn.SchemaVersion)
	}
	if strings.TrimSpace(turn.ID) == "" || turn.AcceptedAt.IsZero() ||
		strings.TrimSpace(turn.ProcessIdentity) == "" {
		return fmt.Errorf("delegated turn identity is incomplete")
	}
	if strings.TrimSpace(turn.PaneBaseline) == "" {
		return fmt.Errorf("delegated turn post-dispatch pane baseline is missing")
	}
	switch turn.Status {
	case delegatedTurnAmbiguous, delegatedTurnDispatched, delegatedTurnRunning,
		delegatedTurnIdle, delegatedTurnDone, delegatedTurnFailed:
	default:
		return fmt.Errorf("invalid delegated turn status %q", turn.Status)
	}
	if (turn.Status == delegatedTurnDone || turn.Status == delegatedTurnFailed) != (turn.SettledAt != nil) {
		return fmt.Errorf("delegated turn terminal timestamp does not match status")
	}
	return nil
}

func delegatedTurnPaneIdentity(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func decodeDelegatedTurn(value string) (delegatedTurnRecord, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return delegatedTurnRecord{}, false, nil
	}
	var turn delegatedTurnRecord
	if err := json.Unmarshal([]byte(value), &turn); err != nil {
		return delegatedTurnRecord{}, false, fmt.Errorf("decode delegated turn: %w", err)
	}
	if err := validateDelegatedTurn(turn); err != nil {
		return delegatedTurnRecord{}, false, err
	}
	return turn, true, nil
}

func reduceDelegatedTurn(turn delegatedTurnRecord, observation delegatedTurnObservation) (delegatedTurnRecord, bool) {
	if delegatedTurnTerminal(turn.Status) {
		return turn, false
	}
	now := observation.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !observation.Live {
		turn.Status = delegatedTurnFailed
		turn.Summary = "Delegated provider process or pane is no longer live"
		turn.ComposerObserved = false
		turn.IdleSince = nil
		turn.SettledAt = &now
		return turn, true
	}
	if provider := observation.Provider; provider.ID != "" &&
		!provider.StartedAt.IsZero() &&
		!provider.StartedAt.Before(turn.AcceptedAt) {
		switch strings.TrimSpace(provider.Status) {
		case "running":
			if turn.Status == delegatedTurnRunning &&
				turn.ProviderActivity == provider.ID &&
				turn.IdleSince == nil {
				return turn, false
			}
			turn.Status = delegatedTurnRunning
			turn.ProviderActivity = provider.ID
			turn.ComposerObserved = false
			turn.IdleSince = nil
			return turn, true
		case "completed":
			turn.Status = delegatedTurnDone
		case "failed", "interrupted", "cancelled":
			turn.Status = delegatedTurnFailed
		default:
			return turn, false
		}
		turn.ProviderActivity = provider.ID
		turn.ComposerObserved = false
		turn.IdleSince = nil
		if provider.SettledAt.IsZero() {
			turn.SettledAt = &now
		} else {
			settledAt := provider.SettledAt.UTC()
			turn.SettledAt = &settledAt
		}
		if turn.Status == delegatedTurnDone {
			turn.Summary = "Delegated turn completed"
		} else {
			turn.Summary = "Delegated provider activity " + strings.TrimSpace(provider.Status)
		}
		return turn, true
	}
	if (turn.Status == delegatedTurnDispatched || turn.Status == delegatedTurnAmbiguous) &&
		observation.StartTimeout > 0 &&
		observation.Pane.State != classifier.StateRunning &&
		!now.Before(turn.AcceptedAt.Add(observation.StartTimeout)) {
		wasAmbiguous := turn.Status == delegatedTurnAmbiguous
		turn.Status = delegatedTurnFailed
		if wasAmbiguous {
			turn.Summary = "Delegated input outcome stayed ambiguous; provider start was not observed"
		} else {
			turn.Summary = "Delegated input was dispatched, but provider start was not observed"
		}
		turn.ComposerObserved = false
		turn.IdleSince = nil
		turn.SettledAt = &now
		return turn, true
	}
	if observation.Provider.Structured && !observation.Provider.FallbackAllowed {
		return turn, false
	}
	switch observation.Pane.State {
	case classifier.StateRunning:
		if turn.Status == delegatedTurnAmbiguous &&
			observation.Pane.Source == "generic_pane_activity" &&
			strings.TrimSpace(observation.PaneIdentity) != "" &&
			observation.PaneIdentity != turn.PaneBaseline {
			// The first generic post-boundary change may be Zen's own composer
			// mutation. Persist it as the new ambiguity baseline; only a later
			// distinct post-boundary pane change can prove provider work.
			if !turn.ComposerObserved {
				turn.PaneBaseline = observation.PaneIdentity
				turn.ComposerObserved = true
				return turn, true
			}
		}
		if turn.Status == delegatedTurnRunning && turn.IdleSince == nil {
			return turn, false
		}
		turn.Status = delegatedTurnRunning
		turn.ComposerObserved = false
		turn.IdleSince = nil
		return turn, true
	case classifier.StateUnknown:
		if strings.TrimSpace(observation.Pane.Source) == "" ||
			(turn.Status != delegatedTurnRunning && turn.Status != delegatedTurnIdle) {
			return turn, false
		}
		if turn.Status == delegatedTurnRunning {
			turn.Status = delegatedTurnIdle
			turn.IdleSince = &now
			return turn, true
		}
		if turn.IdleSince == nil {
			turn.IdleSince = &now
			return turn, true
		}
		if now.Before(turn.IdleSince.Add(delegatedTurnGenericQuietWindow)) {
			return turn, false
		}
		turn.Status = delegatedTurnDone
		turn.Summary = "Delegated turn completed"
		turn.SettledAt = &now
		return turn, true
	default:
		return turn, false
	}
}

func delegatedTurnFallbackPaneActivity(
	turn delegatedTurnRecord,
	provider ProviderActivityObservation,
	activity classifier.ActivitySignal,
	contentChanged bool,
) classifier.ActivitySignal {
	if provider.Structured && !provider.FallbackAllowed {
		return activity
	}
	if activity.State == classifier.StateRunning ||
		activity.State == classifier.StateBlocked ||
		activity.State == classifier.StateFailed {
		return activity
	}
	if contentChanged {
		return classifier.ActivitySignal{
			State:  classifier.StateRunning,
			Source: "generic_pane_activity",
		}
	}
	if turn.Status == delegatedTurnRunning || turn.Status == delegatedTurnIdle {
		return classifier.ActivitySignal{
			State:  classifier.StateUnknown,
			Source: "generic_pane_stable",
		}
	}
	return classifier.ActivitySignal{}
}

func delegatedTurnTerminal(status delegatedTurnStatus) bool {
	return status == delegatedTurnDone || status == delegatedTurnFailed
}

func settleDelegatedTurnFromProgress(
	turn delegatedTurnRecord,
	state classifier.AgentState,
	summary string,
	at time.Time,
) (delegatedTurnRecord, bool) {
	if turn.Status != delegatedTurnRunning && turn.Status != delegatedTurnIdle {
		return turn, false
	}
	if at.Before(turn.AcceptedAt) {
		return turn, false
	}
	switch state {
	case classifier.StateDone:
		turn.Status = delegatedTurnDone
	case classifier.StateFailed:
		turn.Status = delegatedTurnFailed
	default:
		return turn, false
	}
	at = at.UTC()
	turn.SettledAt = &at
	turn.ComposerObserved = false
	turn.IdleSince = nil
	turn.Summary = strings.TrimSpace(summary)
	return turn, true
}

func (owner *sessionInputOwner) observeDelegatedTurn(
	sessionID string,
	expectedTurnID string,
	observation delegatedTurnObservation,
	resolver func(string) (targetProcessIdentity, bool),
) (delegatedTurnRecord, bool, error) {
	var result delegatedTurnRecord
	found := false
	err := owner.serialized(sessionID, func() error {
		current, ok := resolver(sessionID)
		pane := owner.io.pane(sessionID)
		target := pane.paneID
		if target == "" {
			target = sessionID
		}
		turn, exists, err := owner.io.delegatedTurn(target)
		if err != nil || !exists {
			return err
		}
		found = true
		result = turn
		if turn.ID != strings.TrimSpace(expectedTurnID) {
			// Inventory and the serialized Session input owner crossed a new
			// dispatch/steering boundary. Reconcile the new marker next poll.
			return nil
		}
		if !ok || delegatedTurnIdentity(current) != turn.ProcessIdentity {
			observation.Live = false
		}
		next, changed := reduceDelegatedTurn(turn, observation)
		if !changed {
			return nil
		}
		if err := owner.io.writeDelegatedTurn(target, next); err != nil {
			return err
		}
		confirmed, exists, err := owner.io.delegatedTurn(target)
		if err != nil {
			return err
		}
		if !exists || confirmed.ID != next.ID || confirmed.Status != next.Status ||
			confirmed.ProviderActivity != next.ProviderActivity {
			return fmt.Errorf("delegated turn readback did not match written lifecycle projection")
		}
		result = confirmed
		return nil
	})
	return result, found, err
}

func (owner *sessionInputOwner) settleDelegatedTurnFromProgress(
	sessionID, turnID string,
	next delegatedTurnRecord,
) (delegatedTurnRecord, error) {
	var confirmed delegatedTurnRecord
	err := owner.serialized(sessionID, func() error {
		pane := owner.io.pane(sessionID)
		target := pane.paneID
		if target == "" {
			target = sessionID
		}
		current, exists, err := owner.io.delegatedTurn(target)
		if err != nil {
			return fmt.Errorf("read delegated turn for progress: %w", err)
		}
		if !exists || current.ID != strings.TrimSpace(turnID) {
			return fmt.Errorf("delegated turn changed before terminal progress could settle it")
		}
		if err := owner.io.writeDelegatedTurn(target, next); err != nil {
			return fmt.Errorf("write delegated turn from terminal progress: %w", err)
		}
		confirmed, exists, err = owner.io.delegatedTurn(target)
		if err != nil {
			return fmt.Errorf("confirm delegated turn from terminal progress: %w", err)
		}
		if !exists || confirmed.ID != next.ID || confirmed.Status != next.Status {
			return fmt.Errorf("terminal progress delegated turn readback did not match")
		}
		return nil
	})
	return confirmed, err
}
