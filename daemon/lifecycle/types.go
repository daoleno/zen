// Package lifecycle implements the canonical delegated-Work lifecycle state
// machine. Current rows and append-only audit Events commit in one transaction
// image; startup reads current rows directly and never replays another
// authority. See docs/work-lifecycle.md for the invariants.
package lifecycle

import (
	"encoding/json"
	"errors"
	"time"
)

// WorkID identifies the aggregate root.
type WorkID string

// TurnToken is the canonical identity of one admitted provider turn. It is
// minted once per turn and carried by every turn-scoped command and event.
type TurnToken string

// Status is the reduced Work lifecycle status.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusWaiting   Status = "waiting"
	StatusBlocked   Status = "blocked"
	StatusDone      Status = "done"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether the status ends the Work lifecycle.
func (s Status) Terminal() bool {
	return s == StatusDone || s == StatusCancelled
}

// Policy selects the completion rule applied when a turn settles ok.
type Policy string

const (
	PolicyBounded   Policy = "bounded"
	PolicyUntilDone Policy = "until_done"
)

// Event kinds (canonical log vocabulary).
type Kind string

const (
	KWorkDefined            Kind = "work.defined"
	KWorkAmended            Kind = "work.amended"
	KWorkCancelled          Kind = "work.cancelled"
	KWorkCompleted          Kind = "work.completed"
	KAdmissionPrepared      Kind = "admission.prepared"
	KAdmissionRearmed       Kind = "admission.rearmed"
	KAdmissionAmbiguous     Kind = "admission.ambiguous"
	KAdmissionAccepted      Kind = "admission.accepted"
	KAdmissionAborted       Kind = "admission.aborted"
	KTurnAdmitted           Kind = "turn.admitted"
	KTurnHeartbeat          Kind = "turn.heartbeat"
	KTurnProgress           Kind = "turn.progress"
	KTurnDone               Kind = "turn.done"
	KTurnRelinquished       Kind = "turn.relinquished"
	KTurnLost               Kind = "turn.lost"
	KLeaseExpired           Kind = "turn.lease_expired"
	KWakeSet                Kind = "wake.set"
	KWakeCleared            Kind = "wake.cleared"
	KReviewOpened           Kind = "review.opened"
	KReviewClaimed          Kind = "review.claimed"
	KReviewDelivered        Kind = "review.delivered"
	KReviewReleased         Kind = "review.released"
	KReviewDeliveryResolved Kind = "review.delivery_resolved"
	KReviewResolved         Kind = "review.resolved"
)

// Wake kinds identify the external producer of a true wait.
type WakeKind string

const (
	WakeSessionTerminal WakeKind = "session_terminal"
	WakeCalendarResult  WakeKind = "calendar_result"
	WakeUserInput       WakeKind = "user_input"
	WakeDueRetry        WakeKind = "due_retry"
)

// Review dispositions.
type Disposition string

const (
	DispositionContinue Disposition = "continue"
	DispositionWait     Disposition = "wait"
	DispositionComplete Disposition = "complete"
	DispositionCancel   Disposition = "cancel"
)

// Event is one immutable fact in the canonical log. Seq is assigned by the log;
// SourceID provides producer-stable dedupe within a Work stream.
type Event struct {
	Seq       uint64    `json:"seq"`
	WorkID    WorkID    `json:"work_id"`
	Kind      Kind      `json:"kind"`
	TurnToken TurnToken `json:"turn_token,omitempty"`
	Fence     uint64    `json:"fence,omitempty"`
	SourceID  string    `json:"source_id,omitempty"`
	At        time.Time `json:"at"`
	Payload   any       `json:"payload,omitempty"`
}

// UnmarshalJSON decodes Payload into the concrete type selected by Kind so
// replay reduces over typed payloads, not generic maps.
func (e *Event) UnmarshalJSON(data []byte) error {
	raw := struct {
		Seq       uint64          `json:"seq"`
		WorkID    WorkID          `json:"work_id"`
		Kind      Kind            `json:"kind"`
		TurnToken TurnToken       `json:"turn_token,omitempty"`
		Fence     uint64          `json:"fence,omitempty"`
		SourceID  string          `json:"source_id,omitempty"`
		At        time.Time       `json:"at"`
		Payload   json.RawMessage `json:"payload,omitempty"`
	}{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Seq, e.WorkID, e.Kind = raw.Seq, raw.WorkID, raw.Kind
	e.TurnToken, e.Fence, e.SourceID, e.At = raw.TurnToken, raw.Fence, raw.SourceID, raw.At
	if len(raw.Payload) > 0 && string(raw.Payload) != "null" {
		p, err := decodePayload(e.Kind, raw.Payload)
		if err != nil {
			return err
		}
		e.Payload = p
	}
	return nil
}

func decodePayload(kind Kind, raw json.RawMessage) (any, error) {
	switch kind {
	case KWorkDefined:
		var p DefinedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KWorkAmended:
		var p AmendedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KWorkCancelled:
		var p CancelledPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KWorkCompleted:
		var p CancelledPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KAdmissionPrepared, KAdmissionRearmed:
		var p AdmissionPreparedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KAdmissionAmbiguous:
		var p AdmissionAmbiguousPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KAdmissionAccepted:
		var p AdmissionAcceptedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KAdmissionAborted:
		var p AdmissionAbortedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnAdmitted:
		var p AdmittedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnHeartbeat:
		var p HeartbeatPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnProgress:
		var p ProgressPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnDone:
		var p DonePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnRelinquished:
		var p RelinquishedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KTurnLost:
		var p LostPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KLeaseExpired:
		var p ExpiredPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KWakeSet:
		var p WakePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KWakeCleared:
		var p WakeClearedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewOpened:
		var p ReviewOpenedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewClaimed:
		var p ReviewClaimedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewDelivered:
		var p ReviewDeliveredPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewReleased:
		var p ReviewReleasedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewDeliveryResolved:
		var p ReviewDeliveryResolvedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case KReviewResolved:
		var p ReviewResolvedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, nil
	}
}

// Payloads. Only the fields the reducer needs are canonical; prose is evidence.

type DefinedPayload struct {
	Title           string `json:"title"`
	Objective       string `json:"objective"`
	Policy          Policy `json:"policy"`
	DoneCriteriaRef string `json:"done_criteria_ref,omitempty"`
	SourceThreadID  string `json:"source_thread_id,omitempty"`
}

type AmendedPayload struct {
	Title           *string `json:"title,omitempty"`
	Objective       *string `json:"objective,omitempty"`
	DoneCriteriaRef *string `json:"done_criteria_ref,omitempty"`
	NextAction      *string `json:"next_action,omitempty"`
}

type CancelledPayload struct {
	Actor  string `json:"actor,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// AdmissionMode records whether accepted provider input creates a fresh Turn
// or steers the exact currently-owned provider activity.
type AdmissionMode string

const (
	AdmissionFresh            AdmissionMode = "fresh"
	AdmissionConditionalSteer AdmissionMode = "conditional_steer"
)

// AdmissionStatus is a tagged transport-boundary state. Prepared and
// ambiguous are active; accepted and aborted are terminal. Ambiguous is never
// replayable and can only be settled from explicit provider evidence.
type AdmissionStatus string

const (
	AdmissionPrepared  AdmissionStatus = "prepared"
	AdmissionAmbiguous AdmissionStatus = "ambiguous"
	AdmissionAccepted  AdmissionStatus = "accepted"
	AdmissionAborted   AdmissionStatus = "aborted"
)

type AdmissionPreparedPayload struct {
	SessionID          string           `json:"session_id"`
	Receipt            string           `json:"receipt"`
	ClaimToken         string           `json:"claim_token,omitempty"`
	PayloadSHA256      string           `json:"payload_sha256"`
	ProcessIdentity    string           `json:"process_identity"`
	PaneGeneration     string           `json:"pane_generation"`
	Mode               AdmissionMode    `json:"mode"`
	ExistingTurnToken  TurnToken        `json:"existing_turn_token,omitempty"`
	BaselineActivityID string           `json:"baseline_activity_id,omitempty"`
	SignalProtocol     bool             `json:"signal_protocol,omitempty"`
	AttemptedAt        time.Time        `json:"attempted_at"`
	TranscriptProvider string           `json:"transcript_provider,omitempty"`
	TranscriptFlag     string           `json:"transcript_flag,omitempty"`
	TranscriptPath     string           `json:"transcript_path,omitempty"`
	Purpose            AdmissionPurpose `json:"purpose,omitempty"`
	PurposeID          string           `json:"purpose_id,omitempty"`
}

type AdmissionAmbiguousPayload struct {
	Reason string `json:"reason,omitempty"`
}

type AdmissionAcceptedPayload struct {
	ActivityID      string    `json:"activity_id"`
	AdmissionStream string    `json:"admission_stream"`
	AdmissionID     string    `json:"admission_id"`
	AdmissionCursor uint64    `json:"admission_cursor"`
	AdmissionSHA256 string    `json:"admission_sha256"`
	AdmissionAt     time.Time `json:"admission_at"`
	ResultTurnToken TurnToken `json:"result_turn_token"`
}

type AdmissionAbortedPayload struct {
	Reason string `json:"reason,omitempty"`
}

type AdmittedPayload struct {
	SessionID  string    `json:"session_id"`
	Delegated  bool      `json:"delegated"`
	FollowUpOf TurnToken `json:"follow_up_of,omitempty"`
}

type HeartbeatPayload struct {
	LeaseSeconds int `json:"lease_seconds"`
}

type ProgressPayload struct {
	Note string `json:"note,omitempty"`
}

// DonePayload settles one turn. Final is the completion-authority flag: only
// a reporter whose evidence class affirms the done criteria (or a bounded
// signal-protocol worker terminal) may complete the Work outright; every
// other terminal settles review-ready and leaves completion to Brain's typed
// disposition. until_done only denies implicit completion (I7).
type DonePayload struct {
	OK          bool   `json:"ok"`
	Summary     string `json:"summary,omitempty"`
	CriteriaMet bool   `json:"criteria_met,omitempty"`
	Final       bool   `json:"final,omitempty"`
}

// RelinquishedPayload settles a reviewed turn without applying the Work
// completion rule. It is used only by atomic review acceptance when a named
// next Attempt is ready to execute.
type RelinquishedPayload struct {
	Reason string `json:"reason"`
}

type LostPayload struct {
	Reason string `json:"reason,omitempty"`
}

type ExpiredPayload struct {
	Deadline time.Time `json:"deadline"`
}

type WakePayload struct {
	WakeKind      WakeKind   `json:"wake_kind"`
	Ref           string     `json:"ref"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

type WakeClearedPayload struct {
	WakeKind   WakeKind `json:"wake_kind"`
	Ref        string   `json:"ref"`
	Occurrence string   `json:"occurrence,omitempty"`
}

type AdmissionPurpose string

const (
	AdmissionPurposeReview AdmissionPurpose = "review"
)

type ReviewOpenedPayload struct {
	EventID string `json:"event_id"`
	Reason  string `json:"reason"`
	Ref     string `json:"ref,omitempty"`
}

type ReviewResolvedPayload struct {
	EventID       string      `json:"event_id"`
	Disposition   Disposition `json:"disposition"`
	Actor         string      `json:"actor,omitempty"`
	WakeKind      WakeKind    `json:"wake_kind,omitempty"`
	WakeRef       string      `json:"wake_ref,omitempty"`
	NextAttemptAt *time.Time  `json:"next_attempt_at,omitempty"`
}

type ReviewClaimedPayload struct {
	EventID       string    `json:"event_id"`
	HostSessionID string    `json:"host_session_id"`
	HandlerID     string    `json:"handler_id"`
	HandlerToken  TurnToken `json:"handler_token"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ReviewDeliveredPayload struct {
	EventID      string    `json:"event_id"`
	HandlerToken TurnToken `json:"handler_token"`
}

type ReviewReleasedPayload struct {
	EventID      string    `json:"event_id"`
	HandlerToken TurnToken `json:"handler_token"`
}

// ReviewDeliveryResolvedPayload records an explicit actor judgment for an
// ambiguous, not-yet-confirmed Host delivery. The canonical log is the audit;
// no parallel event or submission row participates in the transition.
type ReviewDeliveryResolvedPayload struct {
	EventID      string    `json:"event_id"`
	HandlerToken TurnToken `json:"handler_token"`
	Action       string    `json:"action"`
	Actor        string    `json:"actor"`
	Reason       string    `json:"reason"`
}

// Reduced state.

// AttemptIdentity is the exact capability required to mutate an Attempt.
// All three fields must match; none is inferred from another field.
type AttemptIdentity struct {
	SessionID string
	TurnToken TurnToken
	Fence     uint64
}

// Attempt is the single active execution of a Work. Identity is exactly
// (SessionID, TurnToken, Generation); inputs presenting anything else are stale.
type Attempt struct {
	SessionID     string    `json:"session_id"`
	Delegated     bool      `json:"delegated"`
	Generation    uint64    `json:"generation"`
	TurnToken     TurnToken `json:"turn_token"`
	FollowUpOf    TurnToken `json:"follow_up_of,omitempty"`
	AdmittedAt    time.Time `json:"admitted_at"`
	LeaseDeadline time.Time `json:"lease_deadline"`
	LeaseEpoch    uint64    `json:"lease_epoch"`
}

// WakeState is the typed external producer a waiting Work parks on.
type WakeState struct {
	Kind          WakeKind   `json:"kind"`
	Ref           string     `json:"ref"`
	Since         time.Time  `json:"since"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

// ReviewState is the stable attention obligation Brain must disposition.
type ReviewState struct {
	EventID  string         `json:"event_id"`
	Reason   string         `json:"reason"` // turn_done | turn_failed | lease_expired | turn_lost
	Ref      string         `json:"ref,omitempty"`
	OpenedAt time.Time      `json:"opened_at"`
	Handler  *ReviewHandler `json:"handler,omitempty"`
}

// ReviewHandler is the single Brain handling lease for the open Event.
type ReviewHandler struct {
	HostSessionID  string     `json:"host_session_id"`
	HandlerID      string     `json:"handler_id"`
	HandlerToken   TurnToken  `json:"handler_token"`
	ClaimedAt      time.Time  `json:"claimed_at"`
	ClaimExpiresAt time.Time  `json:"claim_expires_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

// AdmissionState is the canonical pre-provider-mutation transaction owned by
// the Work aggregate. Transport receipts are evidence only; no projection or
// tmux option may authorize a Turn.
type AdmissionState struct {
	TurnToken          TurnToken        `json:"turn_token"`
	SessionID          string           `json:"session_id"`
	Receipt            string           `json:"receipt"`
	ClaimToken         string           `json:"claim_token,omitempty"`
	PayloadSHA256      string           `json:"payload_sha256"`
	ProcessIdentity    string           `json:"process_identity"`
	PaneGeneration     string           `json:"pane_generation"`
	Mode               AdmissionMode    `json:"mode"`
	ExistingTurnToken  TurnToken        `json:"existing_turn_token,omitempty"`
	BaselineActivityID string           `json:"baseline_activity_id,omitempty"`
	SignalProtocol     bool             `json:"signal_protocol,omitempty"`
	TranscriptProvider string           `json:"transcript_provider,omitempty"`
	TranscriptFlag     string           `json:"transcript_flag,omitempty"`
	TranscriptPath     string           `json:"transcript_path,omitempty"`
	Purpose            AdmissionPurpose `json:"purpose,omitempty"`
	PurposeID          string           `json:"purpose_id,omitempty"`
	Status             AdmissionStatus  `json:"status"`
	PreparedAt         time.Time        `json:"prepared_at"`
	AttemptedAt        time.Time        `json:"attempted_at"`
	SettledAt          *time.Time       `json:"settled_at,omitempty"`
	Reason             string           `json:"reason,omitempty"`
	ActivityID         string           `json:"activity_id,omitempty"`
	AdmissionStream    string           `json:"admission_stream,omitempty"`
	AdmissionID        string           `json:"admission_id,omitempty"`
	AdmissionCursor    uint64           `json:"admission_cursor,omitempty"`
	AdmissionAt        time.Time        `json:"admission_at,omitempty"`
	ResultTurnToken    TurnToken        `json:"result_turn_token,omitempty"`
}

// State is the full reduced aggregate. Never mutated outside Reduce.
type State struct {
	ID              WorkID          `json:"id"`
	Revision        uint64          `json:"revision"`
	Status          Status          `json:"status"`
	Title           string          `json:"title"`
	Objective       string          `json:"objective"`
	Policy          Policy          `json:"policy"`
	DoneCriteriaRef string          `json:"done_criteria_ref,omitempty"`
	SourceThreadID  string          `json:"source_thread_id,omitempty"`
	NextAction      string          `json:"next_action,omitempty"`
	LastSummary     string          `json:"last_summary,omitempty"`
	Fence           uint64          `json:"fence"`
	Attempt         *Attempt        `json:"attempt,omitempty"`
	Wake            *WakeState      `json:"wake,omitempty"`
	Review          *ReviewState    `json:"review,omitempty"`
	Admission       *AdmissionState `json:"admission,omitempty"`
	SeenSources     map[string]bool `json:"seen_sources,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	TerminalAt      *time.Time      `json:"terminal_at,omitempty"`
}

// Clone returns a complete deep copy, including the internal source-dedupe
// index. Use cloneView for operational reads that do not inspect that index.
func (s *State) Clone() *State {
	return s.clone(true)
}

func (s *State) cloneView() *State {
	return s.clone(false)
}

func (s *State) clone(includeSeenSources bool) *State {
	if s == nil {
		return nil
	}
	out := *s
	if s.Attempt != nil {
		o := *s.Attempt
		out.Attempt = &o
	}
	if s.Wake != nil {
		w := *s.Wake
		if s.Wake.NextAttemptAt != nil {
			next := *s.Wake.NextAttemptAt
			w.NextAttemptAt = &next
		}
		out.Wake = &w
	}
	if s.Review != nil {
		r := *s.Review
		if s.Review.Handler != nil {
			h := *s.Review.Handler
			r.Handler = &h
		}
		out.Review = &r
	}
	if s.Admission != nil {
		admission := *s.Admission
		if s.Admission.SettledAt != nil {
			settled := *s.Admission.SettledAt
			admission.SettledAt = &settled
		}
		out.Admission = &admission
	}
	out.SeenSources = nil
	if includeSeenSources && len(s.SeenSources) > 0 {
		out.SeenSources = make(map[string]bool, len(s.SeenSources))
		for k, v := range s.SeenSources {
			out.SeenSources[k] = v
		}
	}
	if s.TerminalAt != nil {
		t := *s.TerminalAt
		out.TerminalAt = &t
	}
	return &out
}

// CurrentTurn returns the sole current Attempt, if any.
func (s *State) CurrentTurn() *Attempt { return s.Attempt }

// Admission returns the immutable transaction identified by its proposed
// Turn token.
func (s *State) AdmissionByToken(token TurnToken) *AdmissionState {
	if s.Admission != nil && s.Admission.TurnToken == token {
		return s.Admission
	}
	return nil
}

// ActiveAdmission returns the sole unresolved transport transaction.
func (s *State) ActiveAdmission() *AdmissionState {
	if s.Admission != nil && (s.Admission.Status == AdmissionPrepared || s.Admission.Status == AdmissionAmbiguous) {
		return s.Admission
	}
	return nil
}

// Errors returned by command validation. ErrStaleInput is deliberately
// idempotent-flavored: callers treat it as "already handled".
var (
	ErrUnknownWork      = errors.New("lifecycle: unknown work")
	ErrWorkExists       = errors.New("lifecycle: work already exists")
	ErrTerminal         = errors.New("lifecycle: work is terminal")
	ErrStaleInput       = errors.New("lifecycle: stale fence or turn token")
	ErrAttemptActive    = errors.New("lifecycle: Work already has an active Attempt")
	ErrRevisionConflict = errors.New("lifecycle: revision conflict")
	ErrNotWaiting       = errors.New("lifecycle: work is not waiting on that wake")
	ErrNoOpenReview     = errors.New("lifecycle: no open actionable Event")
	ErrReviewLease      = errors.New("lifecycle: review lease mismatch")
	ErrInvalidCommand   = errors.New("lifecycle: invalid command")
)
