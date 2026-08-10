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
	EvidenceAbsent   EvidenceClass = "absent"
	EvidenceLegacy   EvidenceClass = "legacy"
	EvidencePane     EvidenceClass = "pane"
	EvidenceControl  EvidenceClass = "control"
	EvidenceReceipt  EvidenceClass = "receipt"
	EvidenceLiveness EvidenceClass = "liveness"
	EvidenceProvider EvidenceClass = "provider"
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
	// TurnOwnershipLost is a named recoverable terminal state: the exact
	// provider/pane generation can no longer be controlled. A later bound
	// provider terminal may still resolve its outcome, but commands reject only
	// after this state is durably projected.
	TurnOwnershipLost TurnStatus = "ownership_lost"
)

// TurnControlState is orthogonal to provider lifecycle outcome. A completed
// Turn remains completed if its tmux/provider control identity later becomes
// unsafe; commands still fail closed from the durable ownership_lost state.
type TurnControlState string

const (
	TurnControlOwned         TurnControlState = ""
	TurnControlOwnershipLost TurnControlState = "ownership_lost"
)

// TurnTerminal reports whether the canonical status is terminal for
// scheduling (immutable Done/Failed, or Unknown awaiting a bound fact).
func TurnTerminal(status TurnStatus) bool {
	switch status {
	case TurnDone, TurnFailed, TurnUnknown, TurnOwnershipLost:
		return true
	default:
		return false
	}
}

// TurnImmutable reports whether the provider outcome is globally final:
// Done and Failed can never be rewritten, while orthogonal control ownership
// may still be lost. Unknown is still probed so a later turn-bound Provider
// terminal can upgrade it (C.2.4).
func TurnImmutable(status TurnStatus) bool {
	return status == TurnDone || status == TurnFailed
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
	SessionID      string
	TurnID         string
	Status         TurnStatus
	AcceptedAt     time.Time
	SettledAt      *time.Time
	Summary        string
	Attention      string
	ControlState   TurnControlState
	ActivityID     string
	Admission      TurnAdmission
	HasAdmission   bool
	Hints          []TurnHint
	PaneGeneration string
	// ProcessIdentity is the recorded provider process identity at admission;
	// it anchors deterministic liveness FactIDs (never observation time).
	ProcessIdentity string
	// TranscriptBinding is the provider-native transcript identity recorded at
	// admission (Pi owned session flag/path). It is restored on rediscovery so
	// provider evidence survives daemon restart; the tmux option is only an
	// advisory cache.
	TranscriptBinding TranscriptBinding
	// LeaseDeadline is the turn's own expected-next-check time: minted fresh
	// at admission and extended only by this turn's Control lease facts. It is
	// the sole basis for session.stale, so an old turn's expired lease can
	// never make a newer turn stale.
	LeaseDeadline time.Time
	// SignalProtocol marks a Turn whose exact random identity was carried in
	// its delegated prompt. Only these Turns accept lifecycle authority from
	// matching Control progress; pre-upgrade/provider-native Turns do not.
	SignalProtocol bool
	UpdatedAt      time.Time
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
	AbnormalExit    bool // authoritative abnormal exit (nonzero dead status / signal)
	ProcessDead     bool // positive identity disappearance
	SessionReplaced bool
	PaneAbsent      bool // transient absence; never terminal

	// LeaseSeconds is the caller-declared progress lease for Control facts:
	// the reducer extends the turn's own per-turn liveness deadline by this
	// many seconds from the fact time. Zero never extends the lease.
	LeaseSeconds int
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
	// ApplyDelegatedTurnProgress atomically matches one Control fact to the
	// signal identity carried by the current delegated prompt. A match may
	// promote the exact pending submission and reduce the fact in the same
	// persistence transaction. Owned distinguishes a delegated signal
	// contract from an ordinary/provider-native Session; Matched is false for
	// missing, mismatched, stale, or previous-turn identities.
	ApplyDelegatedTurnProgress(fact TurnFact) (TurnProgressResult, error)
}

type TurnProgressResult struct {
	Turn    TurnSnapshot
	Owned   bool
	Matched bool
	Changed bool
}

// TurnSubmissionState is the durable state of one delegated input
// transaction. Pending submissions are owned exclusively by the Brain Turn
// Ledger; the tmux receipt is only a transport replay fence.
type TurnSubmissionState string

const (
	TurnSubmissionPending  TurnSubmissionState = "pending"
	TurnSubmissionResolved TurnSubmissionState = "resolved"
	TurnSubmissionAborted  TurnSubmissionState = "aborted"
)

// TurnSubmissionMode records how a provider activity observed before
// mutation may own the input after confirmation.
type TurnSubmissionMode string

const (
	TurnSubmissionFresh            TurnSubmissionMode = "fresh"
	TurnSubmissionConditionalSteer TurnSubmissionMode = "conditional_steer"
)

// TurnSubmission is the provider-neutral projection of the ledger-owned
// pending submission row. It is deliberately separate from TurnSnapshot: a
// pending candidate is never the current canonical Turn.
type TurnSubmission struct {
	WorkID         string
	SessionID      string
	ProposedTurnID string
	Receipt        string
	// ClaimToken is the exact Brain Work Event claim capability for a Host
	// submission. It is empty for delegated submissions. Receipt, WorkID,
	// SessionID, ProposedTurnID, and ClaimToken must match one claimed Event.
	ClaimToken         string
	PayloadSHA256      string
	ProcessIdentity    string
	PaneGeneration     string
	AcceptedAt         time.Time
	TranscriptBinding  TranscriptBinding
	Mode               TurnSubmissionMode
	ExistingTurnID     string
	BaselineActivityID string
	// SignalProtocol is persisted before provider mutation only when this
	// submission's ProposedTurnID was included in the delegated prompt.
	SignalProtocol     bool
	State              TurnSubmissionState
	ResolvedTurnID     string
	ResolvedActivityID string
	ResolvedAdmission  TurnAdmission
}

// TurnSubmissionResolution is the exact provider admission that resolves a
// pending submission. Admission.SHA256 must equal the pending payload digest;
// a provider admission for different bytes can never claim the candidate.
type TurnSubmissionResolution struct {
	SessionID      string
	ProposedTurnID string
	Receipt        string
	PayloadSHA256  string
	ActivityID     string
	Admission      TurnAdmission
	ResolvedAt     time.Time
}

// TurnSubmissionLedger is the transactional delegated-input half of the
// canonical Turn Ledger. Prepare persists before provider mutation without
// replacing the current Turn. Resolve atomically either records steering on
// the existing exact activity or promotes the proposed Turn; Abort is a
// permanent, non-adoptable terminal state.
type TurnSubmissionLedger interface {
	PrepareTurnSubmission(submission TurnSubmission) (TurnSubmission, bool, error)
	TurnSubmission(sessionID, proposedTurnID string) (TurnSubmission, bool, error)
	PendingTurnSubmission(sessionID string) (TurnSubmission, bool, error)
	ResolveTurnSubmission(resolution TurnSubmissionResolution) (TurnSubmission, error)
	AbortTurnSubmission(sessionID, proposedTurnID, receipt, payloadSHA256 string) (TurnSubmission, error)
}

// AdmittedTurn is the exact identity of a provider Turn whose owning claim is
// already canonical. Live delegated input uses TurnSubmissionLedger; it never
// creates an Admitted Turn before mutation.
type AdmittedTurn struct {
	SessionID       string
	TurnID          string
	Receipt         string
	AcceptedAt      time.Time
	ProcessIdentity string
	PaneGeneration  string
	PayloadSHA256   string
	// TranscriptBinding is the provider-native transcript identity known at
	// admission time (currently the Zen-owned Pi session flag/path from the
	// launch command). It is persisted with the ledger so provider evidence
	// survives daemon restart without tmux option state.
	TranscriptBinding TranscriptBinding
}

// TranscriptBinding is the provider-native transcript identity recorded at
// admission. The equivalent tmux window option (@zen_agent_pi_session) is
// only an advisory cache for sessions without a ledger record.
type TranscriptBinding struct {
	Provider string `json:"provider,omitempty"`
	PiFlag   string `json:"pi_flag,omitempty"`
	PiPath   string `json:"pi_path,omitempty"`
}

// Empty reports whether no transcript binding is recorded.
func (b TranscriptBinding) Empty() bool {
	return strings.TrimSpace(b.Provider) == "" ||
		(strings.TrimSpace(b.PiFlag) == "" && strings.TrimSpace(b.PiPath) == "")
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
