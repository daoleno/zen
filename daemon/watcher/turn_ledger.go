package watcher

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// This file defines the frozen provider-neutral per-turn canonical protocol
// vocabulary (worklog 2026-08-07-zen-agent-event-reliability, Research C.2).
//
// Exactly one canonical owner exists for every accepted delegated turn: a
// durable ledger record in the Brain lifecycle store. Every lifecycle
// transition is applied by one reducer (brain.Store.ApplyTurnFact) over that
// record; Work status, append-only Events, and the Session projection are derived
// from canonical status only. The watcher is provider-neutral: brain.Store
// implements TurnLedger structurally (brain imports watcher, never the
// reverse).

// EvidenceClass is the strictly ordered evidence lattice
// Absent < Pane < Control < Receipt < Liveness < Provider.
type EvidenceClass string

const (
	EvidenceAbsent   EvidenceClass = "absent"
	EvidencePane     EvidenceClass = "pane"
	EvidenceControl  EvidenceClass = "control"
	EvidenceReceipt  EvidenceClass = "receipt"
	EvidenceLiveness EvidenceClass = "liveness"
	EvidenceProvider EvidenceClass = "provider"
)

// TurnStatus is the canonical per-turn lifecycle status. Done, Failed, and
// Unknown are terminal for scheduling; Done/Failed are immutable, Unknown may
// be upgraded by a later turn-bound Provider terminal (C.2.4). Ownership loss
// is deliberately NOT a status: it is only TurnControlState=ownership_lost, a
// control capability state orthogonal to the provider outcome.
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

// TurnControlState is orthogonal to provider lifecycle outcome. Ownership loss
// gates only a non-immutable turn whose provider outcome may still be live or
// unknown. Once Done/Failed is immutable, later liveness loss is audit evidence
// and the next turn must establish its own independent control capability.
type TurnControlState string

const (
	TurnControlOwned         TurnControlState = ""
	TurnControlOwnershipLost TurnControlState = "ownership_lost"
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

// TurnHint is an attached provisional terminal report (Control or unbound
// Provider). Hints never change canonical status and never wake.
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
	// Structured Control progress context is presentation metadata attached to
	// the exact fact. It never participates in lifecycle or FactID authority.
	Phase       string
	Attention   string
	EventKind   string
	DetailsJSON string
	// CriteriaMet is accepted only from an exact signal-owned Control done
	// report. Provider terminal state never sets completion criteria.
	CriteriaMet bool
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
	// reducer and persists turn + derived Work + audit Event atomically.
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

// InputAdmissionState is the durable state of one delegated input
// transaction. Pending submissions are owned exclusively by the Brain Turn
// Ledger; the tmux receipt is only a transport replay fence.
type InputAdmissionState string

const (
	InputAdmissionPending  InputAdmissionState = "pending"
	InputAdmissionResolved InputAdmissionState = "resolved"
	InputAdmissionAborted  InputAdmissionState = "aborted"
	// InputAdmissionRetired is a local-control terminal state for a submission
	// whose provider authority was explicitly ended: either an actor resolved
	// its Host delivery obligation or a causally newer exact provider generation
	// replaced it. Unlike Aborted it does not claim that provider mutation was
	// impossible; unlike Resolved it never authorizes a canonical Turn.
	InputAdmissionRetired InputAdmissionState = "retired"
)

// InputAdmissionMode records how a provider activity observed before
// mutation may own the input after confirmation.
type InputAdmissionMode string

const (
	InputAdmissionFresh            InputAdmissionMode = "fresh"
	InputAdmissionConditionalSteer InputAdmissionMode = "conditional_steer"
)

// InputAdmission is the provider-neutral projection of the ledger-owned
// pending submission row. It is deliberately separate from TurnSnapshot: a
// pending candidate is never the current canonical Turn.
type InputAdmission struct {
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
	Mode               InputAdmissionMode
	ExistingTurnID     string
	BaselineActivityID string
	// SignalProtocol is persisted before provider mutation only when this
	// submission's ProposedTurnID was included in the delegated prompt.
	SignalProtocol     bool
	Purpose            string
	PurposeID          string
	State              InputAdmissionState
	ResolvedTurnID     string
	ResolvedActivityID string
	ResolvedAdmission  TurnAdmission
}

// InputAdmissionResolution is the exact provider admission that resolves a
// pending submission. Admission.SHA256 must equal the pending payload digest;
// a provider admission for different bytes can never claim the candidate.
type InputAdmissionResolution struct {
	SessionID      string
	ProposedTurnID string
	Receipt        string
	PayloadSHA256  string
	ActivityID     string
	Admission      TurnAdmission
	ResolvedAt     time.Time
}

// InputAdmissionLedger is the transactional delegated-input half of the
// canonical Turn Ledger. Prepare persists one exact input transaction before
// provider mutation without replacing the current Turn; several distinct
// pending transactions may coexist for one Session, and each resolves,
// aborts, or retires only from its own exact evidence. Resolve atomically
// either records steering on the existing exact activity or promotes the
// proposed Turn; Abort is a permanent, non-adoptable terminal state.
type InputAdmissionLedger interface {
	PrepareInputAdmission(submission InputAdmission) (InputAdmission, bool, error)
	InputAdmission(sessionID, proposedTurnID string) (InputAdmission, bool, error)
	PendingInputAdmissions(sessionID string) ([]InputAdmission, error)
	ResolveInputAdmission(resolution InputAdmissionResolution) (InputAdmission, error)
	AbortInputAdmission(sessionID, proposedTurnID, receipt, payloadSHA256 string) (InputAdmission, error)
}

// InputAdmissionAmbiguityLedger is implemented by lifecycle authorities that
// durably distinguish a started-but-unconfirmed provider mutation from a
// merely prepared transaction.
type InputAdmissionAmbiguityLedger interface {
	MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason string) error
}

// AdmittedTurn is the exact identity of a provider Turn whose owning claim is
// already canonical. Live delegated input uses InputAdmissionLedger; it never
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
