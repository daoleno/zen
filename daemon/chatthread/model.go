// Package chatthread defines the provider-neutral canonical Chat thread domain.
//
// The package is deliberately isolated from daemon transports, provider
// adapters, and production registration. It projects a closed set of normalized
// facts into a deterministic Thread snapshot and can durably own explicitly
// opted-in v2 Threads.
package chatthread

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidArgument   = errors.New("invalid chat thread argument")
	ErrIDConflict        = errors.New("chat thread ID conflicts with immutable payload")
	ErrFactKeyConflict   = errors.New("provider fact key conflicts with prior payload")
	ErrInvalidTransition = errors.New("invalid chat thread transition")
	ErrAmbiguousRetry    = errors.New("ambiguous delivery cannot be retried")
	ErrDuplicatePosition = errors.New("duplicate canonical position")
	ErrDuplicateOrdinal  = errors.New("duplicate input ordinal")
	ErrEventBeforeCause  = errors.New("event precedes its causal submission")
	ErrTerminalReopen    = errors.New("terminal execution activity cannot reopen")
	ErrInvariant         = errors.New("chat thread invariant violation")
)

type ThreadID string
type SubmissionID string
type ExecutionID string
type EventID string
type ProviderFactKey string
type DispatchAttemptID string
type WriterEpoch string
type WriterSequence uint64
type Position uint64
type Revision uint64
type EventRevision uint64
type InputOrdinal uint64

type SubmissionOrigin string

const (
	OriginApp              SubmissionOrigin = "app"
	OriginProviderExternal SubmissionOrigin = "provider_external"
)

type DeliveryState string

const (
	DeliveryQueued     DeliveryState = "queued"
	DeliveryDelivering DeliveryState = "delivering"
	DeliveryAmbiguous  DeliveryState = "ambiguous"
	DeliveryDelivered  DeliveryState = "delivered"
)

type ActivityState string

const (
	ActivityRunning     ActivityState = "running"
	ActivityCompleted   ActivityState = "completed"
	ActivityFailed      ActivityState = "failed"
	ActivityInterrupted ActivityState = "interrupted"
	ActivityCancelled   ActivityState = "cancelled"
)

type EventKind string

const (
	EventAssistant EventKind = "assistant"
	EventTool      EventKind = "tool"
	EventPlan      EventKind = "plan"
	EventStatus    EventKind = "status"
)

// Thread is the complete materialized domain projection. Submission and Event
// positions share one sequence; Activity lifecycle never participates in that
// visible ordering.
type Thread struct {
	ID                  ThreadID            `json:"thread_id"`
	Revision            Revision            `json:"revision"`
	NextPosition        Position            `json:"next_position"`
	CurrentExecutionID  ExecutionID         `json:"current_execution_id,omitempty"`
	QueuedSubmissionIDs []SubmissionID      `json:"queued_submission_ids"`
	Submissions         []Submission        `json:"submissions"`
	ExecutionActivities []ExecutionActivity `json:"execution_activities"`
	Events              []ThreadEvent       `json:"events"`
}

// SubmissionPayload is immutable content. Its values participate only in
// same-ID conflict detection; they are never used to find or merge a record.
type SubmissionPayload struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachment_ids"`
}

type Submission struct {
	ID               SubmissionID      `json:"submission_id"`
	Position         Position          `json:"position"`
	Origin           SubmissionOrigin  `json:"origin"`
	WriterEpoch      WriterEpoch       `json:"writer_epoch,omitempty"`
	WriterSequence   WriterSequence    `json:"writer_seq,omitempty"`
	AcceptedRevision Revision          `json:"accepted_revision,omitempty"`
	AcceptedAt       *time.Time        `json:"accepted_at,omitempty"`
	Payload          SubmissionPayload `json:"payload"`
	Delivery         DeliveryState     `json:"delivery"`
	DispatchAttempt  DispatchAttemptID `json:"dispatch_attempt_id,omitempty"`
	ExecutionID      ExecutionID       `json:"execution_id,omitempty"`
	InputOrdinal     InputOrdinal      `json:"input_ordinal,omitempty"`
	AdmissionFactKey ProviderFactKey   `json:"admission_fact_key,omitempty"`
}

type ExecutionActivity struct {
	ID                      ExecutionID     `json:"execution_id"`
	State                   ActivityState   `json:"state"`
	InputCount              InputOrdinal    `json:"input_count"`
	ConsumedThroughPosition Position        `json:"consumed_through_position,omitempty"`
	StartFactKey            ProviderFactKey `json:"start_fact_key"`
	TerminalFactKey         ProviderFactKey `json:"terminal_fact_key,omitempty"`
	TerminalReason          string          `json:"terminal_reason,omitempty"`
}

type ThreadEvent struct {
	ID                       EventID       `json:"event_id"`
	Position                 Position      `json:"position"`
	Revision                 EventRevision `json:"revision"`
	ExecutionID              ExecutionID   `json:"execution_id"`
	CausalSubmissionID       SubmissionID  `json:"causal_submission_id"`
	CausalSubmissionPosition Position      `json:"causal_submission_position"`
	Kind                     EventKind     `json:"kind"`
	Final                    bool          `json:"final"`
	Payload                  string        `json:"payload"`
}

// AcceptSubmissionCommand admits one stable user intent and allocates its
// canonical position. Reusing the ID with the same immutable fields is a no-op.
type AcceptSubmissionCommand struct {
	SubmissionID SubmissionID
	Origin       SubmissionOrigin
	WriterEpoch  WriterEpoch
	WriterSeq    WriterSequence
	AcceptedAt   time.Time
	Payload      SubmissionPayload
}

// BeginDeliveryCommand records the sole authorized external dispatch attempt.
// A different attempt is rejected once delivery is ambiguous.
type BeginDeliveryCommand struct {
	SubmissionID SubmissionID
	AttemptID    DispatchAttemptID
}

// ProviderFact is a deliberately closed normalized adapter boundary. The
// unexported marker prevents callers from extending it into a generic event bus.
type ProviderFact interface {
	providerFact()
}

type ActivityStartedFact struct {
	Key         ProviderFactKey
	ExecutionID ExecutionID
}

func (ActivityStartedFact) providerFact() {}

type DeliveryAmbiguousFact struct {
	Key          ProviderFactKey
	SubmissionID SubmissionID
	AttemptID    DispatchAttemptID
}

func (DeliveryAmbiguousFact) providerFact() {}

type InputAdmittedFact struct {
	Key          ProviderFactKey
	ExecutionID  ExecutionID
	SubmissionID SubmissionID
	Ordinal      InputOrdinal
}

func (InputAdmittedFact) providerFact() {}

type EventUpsertFact struct {
	Key                ProviderFactKey
	EventID            EventID
	ExecutionID        ExecutionID
	CausalSubmissionID SubmissionID
	Kind               EventKind
	Final              bool
	Payload            string
}

func (EventUpsertFact) providerFact() {}

type ActivityTerminalFact struct {
	Key           ProviderFactKey
	ExecutionID   ExecutionID
	TerminalState ActivityState
	Reason        string
}

func (ActivityTerminalFact) providerFact() {}

type ApplyResult struct {
	Revision Revision
	Changed  bool
}

// StateDigest returns a stable digest of the complete visible Thread state.
func StateDigest(thread Thread) string {
	canonical := cloneThread(thread)
	encoded, _ := json.Marshal(canonical)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func cloneThread(thread Thread) Thread {
	clone := thread
	clone.QueuedSubmissionIDs = append([]SubmissionID{}, thread.QueuedSubmissionIDs...)
	clone.Submissions = append([]Submission{}, thread.Submissions...)
	for index := range clone.Submissions {
		clone.Submissions[index].Payload.AttachmentIDs = append(
			[]string{},
			thread.Submissions[index].Payload.AttachmentIDs...,
		)
		if thread.Submissions[index].AcceptedAt != nil {
			acceptedAt := *thread.Submissions[index].AcceptedAt
			clone.Submissions[index].AcceptedAt = &acceptedAt
		}
	}
	clone.ExecutionActivities = append([]ExecutionActivity{}, thread.ExecutionActivities...)
	clone.Events = append([]ThreadEvent{}, thread.Events...)
	return clone
}

func validSubmissionOrigin(origin SubmissionOrigin) bool {
	return origin == OriginApp || origin == OriginProviderExternal
}

func validDeliveryState(state DeliveryState) bool {
	switch state {
	case DeliveryQueued, DeliveryDelivering, DeliveryAmbiguous, DeliveryDelivered:
		return true
	default:
		return false
	}
}

func validActivityState(state ActivityState) bool {
	switch state {
	case ActivityRunning, ActivityCompleted, ActivityFailed, ActivityInterrupted, ActivityCancelled:
		return true
	default:
		return false
	}
}

func terminalActivityState(state ActivityState) bool {
	return validActivityState(state) && state != ActivityRunning
}

func validEventKind(kind EventKind) bool {
	switch kind {
	case EventAssistant, EventTool, EventPlan, EventStatus:
		return true
	default:
		return false
	}
}
