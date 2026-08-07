package watcher

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// This file defines the frozen provider-neutral per-turn canonical protocol
// vocabulary (worklog 2026-08-07-zen-agent-event-reliability, Research C.2).
//
// Exactly one canonical owner exists for every accepted delegated turn: a
// durable ledger record in the Brain orchestration store. Every lifecycle
// transition is applied by one reducer (brain.Store.ApplyTurnFact) over that
// record; Work status, outbox events, and the Session projection are derived
// from canonical status only. The watcher is provider-neutral: brain.Store
// implements TurnLedger structurally (brain imports watcher, never the
// reverse).

// EvidenceClass is the strictly ordered evidence lattice
// Absent < Legacy < Pane < Control < Receipt < Liveness < Provider.
type EvidenceClass string

const (
	EvidenceAbsent    EvidenceClass = "absent"
	EvidenceLegacy    EvidenceClass = "legacy"
	EvidencePane      EvidenceClass = "pane"
	EvidenceControl   EvidenceClass = "control"
	EvidenceReceipt   EvidenceClass = "receipt"
	EvidenceLiveness  EvidenceClass = "liveness"
	EvidenceProvider  EvidenceClass = "provider"
)

// TurnStatus is the canonical per-turn lifecycle status. Done, Failed, and
// Unknown are terminal for scheduling; Done/Failed are immutable, Unknown may
// be upgraded by a later turn-bound Provider terminal (C.2.4).
type TurnStatus string

const (
	TurnAdmitted TurnStatus = "admitted"
	TurnAccepted TurnStatus = "accepted"
	TurnRunning  TurnStatus = "running"
	TurnBlocked  TurnStatus = "blocked"
	TurnDone     TurnStatus = "done"
	TurnFailed   TurnStatus = "failed"
	TurnUnknown  TurnStatus = "unknown"
)

// TurnTerminal reports whether the canonical status is terminal for
// scheduling (immutable Done/Failed, or Unknown awaiting a bound fact).
func TurnTerminal(status TurnStatus) bool {
	switch status {
	case TurnDone, TurnFailed, TurnUnknown:
		return true
	default:
		return false
	}
}

// TurnAdmission is the durable admission tuple bound to a turn. It proves the
// provider admitted the exact input (stream identity + monotone cursor +
// payload digest), never settlement.
type TurnAdmission struct {
	Stream string    `json:"stream,omitempty"`
	ID     string    `json:"id,omitempty"`
	Cursor uint64    `json:"cursor,omitempty"`
	SHA256 string    `json:"sha256,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

// Empty reports whether no admission tuple is recorded on the turn.
func (a TurnAdmission) Empty() bool {
	return strings.TrimSpace(a.Stream) == "" ||
		strings.TrimSpace(a.ID) == "" ||
		a.Cursor == 0
}

// TurnHint is an attached provisional terminal report (Control, Legacy, or
// unbound Provider). Hints never change canonical status and never wake.
type TurnHint struct {
	Kind    string        `json:"kind"`
	Class   EvidenceClass `json:"evidence_class"`
	At      time.Time     `json:"at,omitempty"`
	Summary string        `json:"summary,omitempty"`
}

// TurnSnapshot is the canonical per-turn projection the watcher consumes. It
// is a pure read of the durable ledger record; it is never written directly.
type TurnSnapshot struct {
	SessionID     string
	TurnID        string
	Status        TurnStatus
	AcceptedAt    time.Time
	SettledAt     *time.Time
	Summary       string
	Attention     string
	ActivityID    string
	Admission     TurnAdmission
	HasAdmission  bool
	Hints         []TurnHint
	PaneGeneration string
	UpdatedAt     time.Time
}

// TurnFact is one provider-neutral observation applied to the canonical turn
// by the single reducer. FactID identity is deterministic (TurnFactID);
// re-applying the same fact after restart or reorder is a no-op.
type TurnFact struct {
	SessionID string
	TurnID    string
	Class     EvidenceClass
	// Kind is the derived fact kind: admission, running, attention, done,
	// failed, uncertain, or stale. One native record deriving multiple kinds
	// yields distinct facts because kind is part of the base FactID formula.
	Kind string
	// Bound marks a provider fact that the caller believes is turn-bound.
	// The reducer re-verifies binding against the recorded admission tuple /
	// activity identity and the admission window; unbound provider terminals
	// become provisional hints.
	Bound bool
	// SourceID is the stable source event identity for FactID derivation
	// (C.3.1). No wall-clock observation time and no per-run UUID may appear.
	SourceID string
	// Cursor is the monotone provider cursor for stale-snapshot gating.
	Cursor uint64
	// Admission carries the provider-native admission tuple when the fact
	// correlates (or adopts) the turn's accepted input.
	Admission TurnAdmission
	// ActivityID is the provider-native activity identity bound to the turn.
	ActivityID string
	StartedAt  time.Time
	SettledAt  time.Time
	At         time.Time
	Summary    string
	// Liveness proof fields (EvidenceLiveness only).
	AbnormalExit   bool // authoritative abnormal exit (nonzero dead status / signal)
	ProcessDead    bool // positive identity disappearance
	SessionReplaced bool
	PaneAbsent     bool // transient absence; never terminal
}

// TurnFactID is the frozen deterministic fact identity:
//
//	FactID = sha256(sessionID NUL turnID NUL class NUL kind NUL stableSourceEventID)
//
// No component may contain wall-clock observation time or per-run UUIDs, so
// restart re-read and reordered replay dedupe identically.
func TurnFactID(sessionID, turnID string, class EvidenceClass, kind, stableSourceEventID string) string {
	raw := strings.Join([]string{
		strings.TrimSpace(sessionID),
		strings.TrimSpace(turnID),
		string(class),
		strings.TrimSpace(kind),
		strings.TrimSpace(stableSourceEventID),
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum[:])
}

// TurnFactIDFor returns the deterministic identity for a concrete fact using
// its class/kind/source identity.
func (f TurnFact) TurnFactIDFor() string {
	return TurnFactID(f.SessionID, f.TurnID, f.Class, f.Kind, f.SourceID)
}

// TurnLedger is the provider-neutral canonical per-turn ledger interface. It
// is implemented by brain.Store (via brain.Service) and consumed by the
// watcher; the watcher never holds a competing lifecycle state machine.
type TurnLedger interface {
	// Turn returns the canonical snapshot for the session, or found=false
	// when the session has no accepted-turn ledger record.
	Turn(sessionID string) (TurnSnapshot, bool, error)
	// ApplyTurnFact applies exactly one observation through the single
	// reducer and persists turn + derived Work + outbox event atomically.
	// A replayed or reordered fact (same deterministic FactID) is a no-op.
	ApplyTurnFact(fact TurnFact) (TurnSnapshot, bool, error)
}

// TurnLedgerAdmitter is the pre-dispatch half of the ledger contract: the
// Admitted record must be durable before any provider mutation can begin, so
// a markerless accepted input is unrepresentable (C.2 invariant 2).
type TurnLedgerAdmitter interface {
	// AdmitTurn durably records the Admitted turn before the submit queue
	// runs. It fails closed (NotSubmitted) when no durable identity can be
	// established.
	AdmitTurn(admitted AdmittedTurn) error
}

// AdmittedTurn is the durable pre-dispatch turn identity.
type AdmittedTurn struct {
	SessionID       string
	TurnID          string
	Receipt         string
	AcceptedAt      time.Time
	ProcessIdentity string
	PaneGeneration  string
	PayloadSHA256   string
}

// LegacyDelegatedTurnMarker is one pre-protocol tmux @zen_delegated_turn
// option read by the one-shot migration. After the migration imports it as an
// attached hint, the option is unset and all later writes go to the ledger.
type LegacyDelegatedTurnMarker struct {
	Target string
	Raw    string
}

// LegacyDelegatedTurn is the decoded pre-protocol marker used by the one-shot
// migration. Only the fields that can seed the canonical ledger are read.
type LegacyDelegatedTurn struct {
	ID              string
	Status          string
	AcceptedAt      time.Time
	ProcessIdentity string
	Summary         string
	SettledAt       *time.Time
}

// DecodeLegacyDelegatedTurn decodes a raw @zen_delegated_turn option for the
// one-shot migration. Schema/validation errors are surfaced so the migration
// can leave the marker quarantined rather than importing garbage.
func DecodeLegacyDelegatedTurn(raw string) (LegacyDelegatedTurn, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return LegacyDelegatedTurn{}, false, nil
	}
	var legacy struct {
		ID              string     `json:"id"`
		Status          string     `json:"status"`
		AcceptedAt      time.Time  `json:"accepted_at"`
		ProcessIdentity string     `json:"process_identity"`
		Summary         string     `json:"summary,omitempty"`
		SettledAt       *time.Time `json:"settled_at,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return LegacyDelegatedTurn{}, false, fmt.Errorf("decode legacy delegated turn: %w", err)
	}
	return LegacyDelegatedTurn{
		ID:              strings.TrimSpace(legacy.ID),
		Status:          strings.TrimSpace(legacy.Status),
		AcceptedAt:      legacy.AcceptedAt,
		ProcessIdentity: strings.TrimSpace(legacy.ProcessIdentity),
		Summary:         strings.TrimSpace(legacy.Summary),
		SettledAt:       legacy.SettledAt,
	}, true, nil
}
