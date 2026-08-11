package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/google/uuid"
)

const orchestrationSchemaVersion = 11

var (
	ErrWorkNotFound         = errors.New("Brain Work not found")
	ErrWorkConflict         = errors.New("Brain Work already exists")
	ErrWorkOwnerConflict    = errors.New("Brain Work already has an owner Session")
	ErrEventClaim           = errors.New("Brain Work event claim is no longer current")
	ErrEventHandled         = errors.New("Brain Work event is already handled")
	ErrWorkRevisionConflict = errors.New("Brain Work revision is no longer current")
	ErrWorkCloseConflict    = errors.New("Brain Work cannot be operator-closed while signal authority is in flight")
)

type WorkStatus string

const (
	WorkOpen       WorkStatus = "open"
	WorkRunning    WorkStatus = "running"
	WorkWaiting    WorkStatus = "waiting"
	WorkNeedsInput WorkStatus = "needs_input"
	WorkDone       WorkStatus = "done"
	WorkCancelled  WorkStatus = "cancelled"
)

type CompletionPolicy string

const (
	CompletionBounded   CompletionPolicy = "bounded"
	CompletionUntilDone CompletionPolicy = "until_done"
)

type WorkWakeKind string

const (
	WorkWakeSessionTerminal WorkWakeKind = "session_terminal"
	WorkWakeCalendarResult  WorkWakeKind = "calendar_result"
	WorkWakeUserInput       WorkWakeKind = "user_input"
)

// WorkWake is the typed identity of the external producer that owns a true
// wait. WaitFor remains explanatory prose and never schedules Work.
type WorkWake struct {
	Kind WorkWakeKind `json:"kind"`
	Ref  string       `json:"ref"`
}

type BrainInputAdmissionState string

const (
	BrainInputAdmissionPending      BrainInputAdmissionState = "pending"
	BrainInputAdmissionAccepted     BrainInputAdmissionState = "accepted"
	BrainInputAdmissionNotSubmitted BrainInputAdmissionState = "not_submitted"
	BrainInputAdmissionUncertain    BrainInputAdmissionState = "uncertain"
)

// BrainInputAdmission is the restart-enumerable authority for one foreground
// Brain input. Pending is persisted before provider mutation and is a no-replay
// hold. Accepted is committed with every matching user-input Attention in the
// same orchestration replacement. messages.jsonl is only its projection.
type BrainInputAdmission struct {
	RequestID          string                   `json:"request_id"`
	ThreadID           string                   `json:"thread_id"`
	HostSessionID      string                   `json:"host_session_id"`
	HostGeneration     string                   `json:"host_generation"`
	HostTurnID         string                   `json:"host_turn_id"`
	ProviderActivityID string                   `json:"provider_activity_id,omitempty"`
	SessionID          string                   `json:"session_id"`
	DisplayBody        string                   `json:"display_body"`
	BodySHA256         string                   `json:"body_sha256"`
	State              BrainInputAdmissionState `json:"state"`
	CreatedAt          time.Time                `json:"created_at"`
	AcceptedAt         *time.Time               `json:"accepted_at,omitempty"`
	SettledAt          *time.Time               `json:"settled_at,omitempty"`
}

// HostForegroundTurn is the durable admission epoch for one foreground Brain
// response. Multiple accepted steering inputs may share the same provider
// activity, but a terminal boundary closes this exact Host generation and
// turn once. It is not a provider input queue. It is closed only by strong
// exact terminal evidence (matching provider activity identity) inside the
// single Host-lane reducer.
type HostForegroundTurn struct {
	HostSessionID      string    `json:"host_session_id"`
	HostGeneration     string    `json:"host_generation"`
	HostTurnID         string    `json:"host_turn_id"`
	ProviderActivityID string    `json:"provider_activity_id,omitempty"`
	StartedAt          time.Time `json:"started_at"`
}

type WorkProgressMode string

const (
	WorkProgressOwned   WorkProgressMode = "owned"
	WorkProgressWaiting WorkProgressMode = "waiting"
	WorkProgressReady   WorkProgressMode = "ready"
)

type WorkDisposition string

const (
	WorkDispositionContinue  WorkDisposition = "continue"
	WorkDispositionWait      WorkDisposition = "wait"
	WorkDispositionComplete  WorkDisposition = "complete"
	WorkDispositionCancel    WorkDisposition = "cancel"
	WorkDispositionSupersede WorkDisposition = "supersede"
)

type SessionFinalizationState string

const (
	SessionFinalizationPending  SessionFinalizationState = "pending"
	SessionFinalizationFailed   SessionFinalizationState = "failed"
	SessionFinalizationComplete SessionFinalizationState = "complete"
	SessionFinalizationSkipped  SessionFinalizationState = "skipped"
)

// SessionFinalization is the idempotent teardown obligation created by a new
// terminal disposition. Schema upgrades may also add an exact pending
// obligation for delegated provider authority that an older terminal Work
// stranded; existing failed/complete/skipped outcomes are never reset.
type SessionFinalization struct {
	SessionID string                   `json:"session_id"`
	Delegated bool                     `json:"delegated"`
	State     SessionFinalizationState `json:"state"`
	Attempts  uint32                   `json:"attempts,omitempty"`
	LastError string                   `json:"last_error,omitempty"`
	UpdatedAt time.Time                `json:"updated_at"`
}

type PendingSessionFinalization struct {
	WorkID       string
	Finalization SessionFinalization
}

// WorkSuccessorReservation is the one exclusive delegated Session staged by a
// delivered Brain handling. Unlike the rejected Event-local draft, it survives
// Host requeue and restart. ProviderTurnID is filled by canonical admission;
// only the exact handling disposition may promote the reservation to owner.
type WorkSuccessorReservation struct {
	SessionID      string `json:"session_id"`
	ProviderTurnID string `json:"provider_turn_id,omitempty"`
	EventID        string `json:"event_id"`
	HandlingID     string `json:"handling_id"`
}

// Work is the only durable Brain commitment. It is intentionally small:
// detailed plans and evidence remain in the referenced worklog.
//
// SourceThreadID freezes the originating Brain thread at Work creation.
// Later Work Events materialize only into that persisted thread, even if the
// user has since created or switched to another Brain thread.
type Work struct {
	ID                   string                    `json:"work_id"`
	Revision             uint64                    `json:"revision"`
	TerminalRevision     uint64                    `json:"terminal_revision,omitempty"`
	Title                string                    `json:"title"`
	Objective            string                    `json:"objective"`
	Status               WorkStatus                `json:"status"`
	OwnerSessionID       string                    `json:"owner_session_id,omitempty"`
	OwnerDelegated       bool                      `json:"owner_delegated,omitempty"`
	SourceThreadID       string                    `json:"source_thread_id,omitempty"`
	CompletionPolicy     CompletionPolicy          `json:"completion_policy"`
	DoneCriteriaRef      string                    `json:"done_criteria_ref,omitempty"`
	NextAction           string                    `json:"next_action,omitempty"`
	WaitFor              string                    `json:"wait_for,omitempty"`
	Wake                 *WorkWake                 `json:"wake,omitempty"`
	SuccessorReservation *WorkSuccessorReservation `json:"successor_reservation,omitempty"`
	SessionFinalizations []SessionFinalization     `json:"session_finalizations,omitempty"`
	ContextRef           string                    `json:"context_ref,omitempty"`
	CreatedAt            time.Time                 `json:"created_at"`
	UpdatedAt            time.Time                 `json:"updated_at"`
}

// WorkEvent is an append-only fact. Event.ID is also its delivery receipt.
// Only Actionable events participate in Brain scheduling. A claimed Event is
// bound to one Host Session, marked delivered after its exact input is accepted,
// and marked handled only with an atomic typed Work disposition.
// Resolution/ResolvedBy/ResolvedAt/DiscardedAt/ReplayOf are the durable
// actor-recorded audit trail for held delivery claims (C.2.6); they are set
// only by explicit MarkDeliveredClaim/DiscardClaim/ReplayEvent operations,
// never by elapsed time. Read state lives in the timeline projection, never
// on the scheduling fact.
type WorkEvent struct {
	ID                    string          `json:"event_id"`
	WorkID                string          `json:"work_id"`
	Kind                  string          `json:"kind"`
	DedupeKey             string          `json:"dedupe_key"`
	PayloadRef            string          `json:"payload_ref,omitempty"`
	SourceName            string          `json:"source_name,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Actionable            bool            `json:"actionable"`
	CreatedAt             time.Time       `json:"created_at"`
	Sequence              uint64          `json:"sequence"`
	WorkRevision          uint64          `json:"work_revision"`
	ClaimedAt             *time.Time      `json:"claimed_at,omitempty"`
	DeliveryHostSessionID string          `json:"delivery_host_session_id,omitempty"`
	HandlingID            string          `json:"handling_id,omitempty"`
	ProviderTurnID        string          `json:"provider_turn_id,omitempty"`
	DeliveryWorkRevision  uint64          `json:"delivery_work_revision,omitempty"`
	DeliverySequenceFence uint64          `json:"delivery_sequence_fence,omitempty"`
	DeliveredAt           *time.Time      `json:"delivered_at,omitempty"`
	HandlingEndedAt       *time.Time      `json:"handling_ended_at,omitempty"`
	HandledAt             *time.Time      `json:"handled_at,omitempty"`
	Disposition           WorkDisposition `json:"disposition,omitempty"`
	DispositionSummary    string          `json:"disposition_summary,omitempty"`
	CoalescedInto         string          `json:"coalesced_into,omitempty"`
	HistoricalDelivery    bool            `json:"historical_delivery,omitempty"`
	Resolution            string          `json:"resolution,omitempty"`
	ResolvedBy            string          `json:"resolved_by,omitempty"`
	ResolvedAt            *time.Time      `json:"resolved_at,omitempty"`
	DiscardedAt           *time.Time      `json:"discarded_at,omitempty"`
	ReplayOf              string          `json:"replay_of,omitempty"`

	// Decode-only schema <=7 input. New writes never persist presentation
	// state in the scheduler ledger; ensure-time migration moves it into the
	// timeline card before the old field is dropped.
	legacyReadAt *time.Time

	// Review* is a read-time delivery projection. The append-only fact named by
	// ID never changes identity, while a coalesced newer fact can be the latest
	// authoritative result Brain must review through DeliverySequenceFence.
	ReviewKind       string `json:"-"`
	ReviewPayloadRef string `json:"-"`
	ReviewSourceName string `json:"-"`
	ReviewSummary    string `json:"-"`
}

// WorkEventResolution values for held-claim closure (C.2.6).
const (
	EventResolutionMarkDelivered = "mark_delivered"
	EventResolutionDiscard       = "discard"
	EventResolutionReplayed      = "replayed"
)

type WorkUpdate struct {
	Title            *string
	Objective        *string
	Status           *WorkStatus
	OwnerSessionID   *string
	CompletionPolicy *CompletionPolicy
	DoneCriteriaRef  *string
	NextAction       *string
	WaitFor          *string
	Wake             **WorkWake
	ContextRef       *string
}

// WorkCloseRequest is the explicit operator path for terminalizing queued or
// historical Work that has no in-flight Host/provider authority. It is not a
// substitute for ResolveWorkEvent: a claimed delivery, admitted Host handling,
// or unresolved provider submission must still finish through its exact
// capability. Actor and reason are persisted in a non-actionable audit Event.
type WorkCloseRequest struct {
	WorkID           string     `json:"work_id"`
	ExpectedRevision uint64     `json:"expected_work_revision"`
	Status           WorkStatus `json:"status"`
	Actor            string     `json:"actor"`
	Reason           string     `json:"reason"`
}

// WorkEventDispositionRequest is Brain's exact handling transaction. The
// handling identity and expected Work revision came from the delivered compact
// input, preventing an old Host turn from overwriting newer durable state.
type WorkEventDispositionRequest struct {
	EventID              string          `json:"event_id"`
	HandlingID           string          `json:"handling_id"`
	ProviderTurnID       string          `json:"provider_turn_id"`
	ExpectedWorkRevision uint64          `json:"expected_work_revision"`
	Disposition          WorkDisposition `json:"disposition"`
	SuccessorSessionID   string          `json:"successor_session_id,omitempty"`
	Wake                 *WorkWake       `json:"wake,omitempty"`
	NextAction           string          `json:"next_action,omitempty"`
	Summary              string          `json:"summary,omitempty"`
}

type ActiveWork struct {
	ID                   string                    `json:"work_id"`
	Revision             uint64                    `json:"revision"`
	Title                string                    `json:"title"`
	Status               WorkStatus                `json:"status"`
	ProgressMode         WorkProgressMode          `json:"progress_mode"`
	OwnerSessionID       string                    `json:"owner_session_id,omitempty"`
	OwnerDelegated       bool                      `json:"owner_delegated,omitempty"`
	WaitFor              string                    `json:"wait_for,omitempty"`
	Wake                 *WorkWake                 `json:"wake,omitempty"`
	AttentionPending     bool                      `json:"attention_pending"`
	SuccessorReservation *WorkSuccessorReservation `json:"successor_reservation,omitempty"`
	SessionFinalizations []SessionFinalization     `json:"session_finalizations,omitempty"`
	UnreadResult         bool                      `json:"unread_result"`
}

type WorkAttentionState string

const (
	WorkAttentionQueued    WorkAttentionState = "queued"
	WorkAttentionReviewing WorkAttentionState = "reviewing"
)

// CurrentWork is the bounded operational relationship projection. Durable
// Work and result history remain in the Work ledger and are summarized by
// WorkBacklog; they are not execution relationships merely because they are
// unread or once named a Session.
type CurrentWork struct {
	ID                   string                `json:"work_id"`
	Revision             uint64                `json:"revision"`
	Title                string                `json:"title"`
	Status               WorkStatus            `json:"status"`
	ProgressMode         WorkProgressMode      `json:"progress_mode,omitempty"`
	OwnerSessionID       string                `json:"owner_session_id,omitempty"`
	OwnerDelegated       bool                  `json:"owner_delegated,omitempty"`
	WaitFor              string                `json:"wait_for,omitempty"`
	Wake                 *WorkWake             `json:"wake,omitempty"`
	AttentionState       WorkAttentionState    `json:"attention_state,omitempty"`
	SessionFinalizations []SessionFinalization `json:"session_finalizations,omitempty"`
	UnreadResult         bool                  `json:"unread_result"`
}

// WorkBacklog keeps durable history/repair truth explicit without projecting
// every historical row as a current relationship. Category counts are
// intentionally non-exclusive.
type WorkBacklog struct {
	Total             int `json:"total"`
	QueuedAttention   int `json:"queued_attention"`
	HistoricalResults int `json:"historical_results"`
	RepairNeeded      int `json:"repair_needed"`
}

type WorkInventory struct {
	Current []CurrentWork
	Backlog WorkBacklog
}

type WorkReviewState string

const (
	WorkReviewQueued    WorkReviewState = "queued"
	WorkReviewReviewing WorkReviewState = "reviewing"
	WorkReviewResolved  WorkReviewState = "resolved"
)

type WorkResultSessionState string

const (
	WorkResultSessionOpen        WorkResultSessionState = "open"
	WorkResultSessionClosing     WorkResultSessionState = "closing"
	WorkResultSessionFinalized   WorkResultSessionState = "finalized"
	WorkResultSessionCloseFailed WorkResultSessionState = "close_failed"
	WorkResultSessionNotRequired WorkResultSessionState = "not_required"
)

type WorkResultLifecycle struct {
	EventID       string
	ReviewState   WorkReviewState
	SessionState  WorkResultSessionState
	CurrentResult bool
}

type WorkChange struct {
	WorkID string
}

const workResultSummaryRuneLimit = 360

type orchestrationDatabase struct {
	SchemaVersion        int                     `json:"schema_version"`
	NextEventSequence    uint64                  `json:"next_event_sequence"`
	Migrations           orchestrationMigrations `json:"migrations"`
	BrainInputAdmissions []BrainInputAdmission   `json:"brain_input_admissions"`
	HostForegroundTurn   *HostForegroundTurn     `json:"host_foreground_turn,omitempty"`
	BrainWork            []Work                  `json:"brain_work"`
	BrainWorkEvents      []WorkEvent             `json:"brain_work_events"`
	BrainTurns           []TurnRecord            `json:"brain_turns"`
	BrainTurnSubmissions []TurnSubmissionRecord  `json:"brain_turn_submissions"`
}

// workRecord is the on-disk Work shape during decode. Unknown never-released
// fields such as terminal_at are ignored (no DisallowUnknownFields on schema
// 3/4 upgrade). SourceThreadID is required after bind/persist to schema 4.
type workRecord struct {
	ID                   string                    `json:"work_id"`
	Revision             uint64                    `json:"revision"`
	TerminalRevision     uint64                    `json:"terminal_revision,omitempty"`
	Title                string                    `json:"title"`
	Objective            string                    `json:"objective"`
	Status               WorkStatus                `json:"status"`
	OwnerSessionID       string                    `json:"owner_session_id,omitempty"`
	OwnerDelegated       bool                      `json:"owner_delegated,omitempty"`
	SourceThreadID       string                    `json:"source_thread_id,omitempty"`
	CompletionPolicy     CompletionPolicy          `json:"completion_policy"`
	DoneCriteriaRef      string                    `json:"done_criteria_ref,omitempty"`
	NextAction           string                    `json:"next_action,omitempty"`
	WaitFor              string                    `json:"wait_for,omitempty"`
	Wake                 *WorkWake                 `json:"wake,omitempty"`
	SuccessorReservation *WorkSuccessorReservation `json:"successor_reservation,omitempty"`
	SessionFinalizations []SessionFinalization     `json:"session_finalizations,omitempty"`
	ContextRef           string                    `json:"context_ref,omitempty"`
	CreatedAt            time.Time                 `json:"created_at"`
	UpdatedAt            time.Time                 `json:"updated_at"`
}

type orchestrationDatabaseRecord struct {
	SchemaVersion        int                     `json:"schema_version"`
	NextEventSequence    uint64                  `json:"next_event_sequence"`
	Migrations           orchestrationMigrations `json:"migrations"`
	BrainInputAdmissions []BrainInputAdmission   `json:"brain_input_admissions"`
	HostForegroundTurn   *HostForegroundTurn     `json:"host_foreground_turn,omitempty"`
	BrainWork            []workRecord            `json:"brain_work"`
	BrainWorkEvents      []WorkEvent             `json:"brain_work_events"`
	BrainTurns           []TurnRecord            `json:"brain_turns"`
	BrainTurnSubmissions []TurnSubmissionRecord  `json:"brain_turn_submissions"`
}

type orchestrationMigrations struct {
	DelegatedSessionsV1At   *time.Time `json:"delegated_sessions_v1_at,omitempty"`
	TurnLedgerV1At          *time.Time `json:"turn_ledger_v1_at,omitempty"`
	SignalSystemV1StartedAt *time.Time `json:"signal_system_v1_started_at,omitempty"`
	SignalSystemV1Cursor    string     `json:"signal_system_v1_cursor,omitempty"`
	SignalSystemV1At        *time.Time `json:"signal_system_v1_at,omitempty"`
}

type orchestrationV0 struct {
	SchemaVersion int `json:"schema_version"`
}

type legacyOrchestrationDatabase struct {
	SchemaVersion   int                     `json:"schema_version"`
	Migrations      orchestrationMigrations `json:"migrations"`
	BrainWork       []workRecord            `json:"brain_work"`
	BrainWorkEvents []legacyWorkEvent       `json:"brain_work_events"`
}

type legacyWorkEvent struct {
	ID                     string     `json:"event_id"`
	WorkID                 string     `json:"work_id"`
	Kind                   string     `json:"kind"`
	DedupeKey              string     `json:"dedupe_key"`
	PayloadRef             string     `json:"payload_ref,omitempty"`
	Actionable             bool       `json:"actionable"`
	CreatedAt              time.Time  `json:"created_at"`
	ClaimedAt              *time.Time `json:"claimed_at,omitempty"`
	ClaimToken             string     `json:"claim_token,omitempty"`
	DeliveryHostSessionID  string     `json:"delivery_host_session_id,omitempty"`
	DeliveryAcknowledgedAt *time.Time `json:"delivery_acknowledged_at,omitempty"`
	ConsumedAt             *time.Time `json:"consumed_at,omitempty"`
	ReadAt                 *time.Time `json:"read_at,omitempty"`
}

// UnmarshalJSON accepts consumed_at only as a pre-v7 migration input. The
// current writer omits it because delivery and handling are separate facts.
func (event *WorkEvent) UnmarshalJSON(raw []byte) error {
	type durableWorkEvent WorkEvent
	decoded := struct {
		durableWorkEvent
		ConsumedAt *time.Time `json:"consumed_at,omitempty"`
		ReadAt     *time.Time `json:"read_at,omitempty"`
	}{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*event = WorkEvent(decoded.durableWorkEvent)
	event.legacyReadAt = decoded.ReadAt
	if event.DeliveredAt == nil && decoded.ConsumedAt != nil {
		delivered := decoded.ConsumedAt.UTC()
		event.DeliveredAt = &delivered
		event.HistoricalDelivery = true
		ended := delivered
		event.HandlingEndedAt = &ended
	}
	return nil
}

func (s *Store) orchestrationPath() string {
	return s.statePath() + string(os.PathSeparator) + "orchestration.json"
}

func (s *Store) ensureOrchestrationDatabase() error {
	raw, err := os.ReadFile(s.orchestrationPath())
	if errors.Is(err, os.ErrNotExist) {
		return s.persistOrchestrationLocked(orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            []Work{},
			BrainWorkEvents:      []WorkEvent{},
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		})
	}
	if err != nil {
		return err
	}
	database, migrated, err := decodeOrchestrationDatabase(raw)
	if err != nil {
		return fmt.Errorf("decode Brain orchestration database: %w", err)
	}
	threadID, err := s.ChatThreadID()
	if err != nil {
		return err
	}
	bound := bindUnresolvedSourceThreadIDs(&database, threadID)
	readStateMigrated, err := s.migrateLegacyEventReadStateLocked(&database)
	if err != nil {
		return fmt.Errorf("migrate Brain Work read state: %w", err)
	}
	// Per-turn liveness backfill: rows persisted before the per-turn lease
	// existed have a zero deadline. They get a fresh lease minted from their
	// acceptance so an upgrade can never resurrect an old turn's expired
	// lease as an immediate session.stale. Deterministic and idempotent: a
	// deadline already set is never rewritten.
	leaseBackfilled := backfillTurnLeaseDeadlines(&database, s.nowUTC())
	if migrated || bound || leaseBackfilled || readStateMigrated {
		return s.persistOrchestrationLocked(database)
	}
	if err := validateOrchestrationDatabase(database); err != nil {
		return fmt.Errorf("decode Brain orchestration database: %w", err)
	}
	return nil
}

func decodeOrchestrationDatabase(raw []byte) (orchestrationDatabase, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return orchestrationDatabase{}, false, fmt.Errorf("document must be a JSON object")
	}
	var header struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(trimmed, &header); err != nil {
		return orchestrationDatabase{}, false, err
	}
	if header.SchemaVersion == nil {
		return orchestrationDatabase{}, false, fmt.Errorf("schema_version is required")
	}
	switch *header.SchemaVersion {
	case 0:
		var legacy orchestrationV0
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&legacy); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if err := ensureSingleJSONValue(decoder, trimmed); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           orchestrationMigrations{},
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            []Work{},
			BrainWorkEvents:      []WorkEvent{},
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}, true, nil
	case 2:
		var legacy legacyOrchestrationDatabase
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&legacy); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if err := ensureSingleJSONValue(decoder, trimmed); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if legacy.BrainWork == nil || legacy.BrainWorkEvents == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work and brain_work_events are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           legacy.Migrations,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            worksFromRecords(legacy.BrainWork),
			BrainWorkEvents:      make([]WorkEvent, 0, len(legacy.BrainWorkEvents)),
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		for _, old := range legacy.BrainWorkEvents {
			event := WorkEvent{
				ID: old.ID, WorkID: old.WorkID, Kind: old.Kind,
				DedupeKey: old.DedupeKey, PayloadRef: old.PayloadRef,
				Actionable: old.Actionable, CreatedAt: old.CreatedAt,
				ClaimedAt: old.ClaimedAt, DeliveryHostSessionID: old.DeliveryHostSessionID,
				DeliveredAt: old.ConsumedAt, legacyReadAt: old.ReadAt,
			}
			if old.DeliveryAcknowledgedAt != nil && event.DeliveredAt == nil {
				event.DeliveredAt = old.DeliveryAcknowledgedAt
			}
			if event.DeliveredAt != nil {
				event.HistoricalDelivery = true
				ended := event.DeliveredAt.UTC()
				event.HandlingEndedAt = &ended
			}
			database.BrainWorkEvents = append(database.BrainWorkEvents, event)
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case 3:
		var record orchestrationDatabaseRecord
		// Unknown never-released fields (e.g. terminal_at) are ignored.
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work and brain_work_events are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           record.Migrations,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case 4:
		var record orchestrationDatabaseRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work and brain_work_events are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           record.Migrations,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case 5:
		var record orchestrationDatabaseRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work and brain_work_events are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           record.Migrations,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case 6:
		var record orchestrationDatabaseRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil || record.BrainTurnSubmissions == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work, brain_work_events, and brain_turn_submissions are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			Migrations:           record.Migrations,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: record.BrainTurnSubmissions,
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		retirePendingDelegatedSubmissionsForTerminalWork(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case 7:
		// Schema 7 carried the rejected HostAttentionReservation scheduler
		// (attention_reservation, attention_admissions, last_attention_work_id)
		// plus the process-local foreground gate. The Host-lane reducer
		// replaces all of it: the reservation is dropped on decode and the
		// durable gates are the pending Brain input admission and the accepted
		// foreground Host turn. Unknown never-released fields are ignored.
		var record orchestrationDatabaseRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil || record.BrainTurnSubmissions == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work, brain_work_events, and brain_turn_submissions are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			NextEventSequence:    record.NextEventSequence,
			Migrations:           record.Migrations,
			BrainInputAdmissions: record.BrainInputAdmissions,
			HostForegroundTurn:   record.HostForegroundTurn,
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: record.BrainTurnSubmissions,
		}
		upgradeSignalSchema(&database)
		migrateTerminalWorkRevisions(&database)
		retirePendingDelegatedSubmissionsForTerminalWork(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		needsBind := false
		for _, item := range database.BrainWork {
			if strings.TrimSpace(item.SourceThreadID) == "" {
				needsBind = true
				break
			}
		}
		return database, needsBind || *header.SchemaVersion != orchestrationSchemaVersion, nil
	case 8, 9, 10, orchestrationSchemaVersion:
		// Schema 9 added the actor-retired Turn submission state. Schema 10
		// made terminal Work authoritative for delegated provider admission;
		// schema 11 freezes its Event causality boundary as terminal_revision.
		// A pending delegated submission owned by already-terminal
		// Work is retired during the deterministic whole-document upgrade below.
		// Exact Host claim submissions remain governed by their Event handling.
		// The shape is unchanged.
		var record orchestrationDatabaseRecord
		// Ignore unknown never-released fields; bind missing source threads in ensure.
		if err := json.Unmarshal(trimmed, &record); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if record.BrainWork == nil || record.BrainWorkEvents == nil || record.BrainTurnSubmissions == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work, brain_work_events, and brain_turn_submissions are required arrays")
		}
		database := orchestrationDatabase{
			SchemaVersion:        orchestrationSchemaVersion,
			NextEventSequence:    record.NextEventSequence,
			Migrations:           record.Migrations,
			BrainInputAdmissions: record.BrainInputAdmissions,
			HostForegroundTurn:   record.HostForegroundTurn,
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: record.BrainTurnSubmissions,
		}
		upgradeSignalSchema(&database)
		if *header.SchemaVersion < orchestrationSchemaVersion {
			migrateTerminalWorkRevisions(&database)
			retirePendingDelegatedSubmissionsForTerminalWork(&database)
		}
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		needsBind := false
		for _, item := range database.BrainWork {
			if strings.TrimSpace(item.SourceThreadID) == "" {
				needsBind = true
				break
			}
		}
		return database, needsBind || *header.SchemaVersion != orchestrationSchemaVersion, nil
	default:
		return orchestrationDatabase{}, false, fmt.Errorf(
			"unsupported schema_version %d (latest %d)",
			*header.SchemaVersion,
			orchestrationSchemaVersion,
		)
	}
}

// upgradeSignalSchema performs the deterministic, whole-document shape
// conversion needed to decode old rows. The bounded semantic reconciliation
// is separate in MigrateSignalSystemV1.
func upgradeSignalSchema(database *orchestrationDatabase) {
	if database == nil {
		return
	}
	if database.BrainInputAdmissions == nil {
		database.BrainInputAdmissions = []BrainInputAdmission{}
	}
	for index := range database.BrainWork {
		if database.BrainWork[index].Revision == 0 {
			database.BrainWork[index].Revision = 1
		}
	}
	used := map[uint64]bool{}
	for _, event := range database.BrainWorkEvents {
		if event.Sequence > 0 {
			used[event.Sequence] = true
			if event.Sequence > database.NextEventSequence {
				database.NextEventSequence = event.Sequence
			}
		}
	}
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.Sequence == 0 || used[event.Sequence] && duplicateEventSequence(database.BrainWorkEvents, index, event.Sequence) {
			database.NextEventSequence++
			event.Sequence = database.NextEventSequence
		}
		if event.WorkRevision == 0 {
			if workIndex := workIndex(database.BrainWork, event.WorkID); workIndex >= 0 {
				event.WorkRevision = database.BrainWork[workIndex].Revision
			} else {
				event.WorkRevision = 1
			}
		}
	}
}

// migrateTerminalWorkRevisions upgrades the former timestamp-based terminal
// causality rule into one immutable Work revision fence. The migration keeps
// exactly the outstanding Events the older document considered post-terminal,
// then freezes that boundary so later metadata updates cannot change their
// eligibility. Current-schema documents never pass through this repair path.
func migrateTerminalWorkRevisions(database *orchestrationDatabase) {
	if database == nil {
		return
	}
	for workIndex := range database.BrainWork {
		item := &database.BrainWork[workIndex]
		if item.Status != WorkDone && item.Status != WorkCancelled {
			item.TerminalRevision = 0
			continue
		}
		terminalRevision := item.Revision
		for _, event := range database.BrainWorkEvents {
			if event.WorkID != item.ID || !attentionEventOutstanding(event) || event.WorkRevision == 0 ||
				event.CreatedAt.Before(item.UpdatedAt) {
				continue
			}
			if event.WorkRevision < terminalRevision {
				terminalRevision = event.WorkRevision
			}
		}
		item.TerminalRevision = terminalRevision
	}
}

func duplicateEventSequence(events []WorkEvent, current int, sequence uint64) bool {
	for index := range events {
		if index != current && events[index].Sequence == sequence {
			return true
		}
	}
	return false
}

// worksFromRecords copies durable Work fields. SourceThreadID may be empty
// until bindUnresolvedSourceThreadIDs freezes ownership during upgrade.
func worksFromRecords(records []workRecord) []Work {
	out := make([]Work, 0, len(records))
	for _, record := range records {
		out = append(out, Work{
			ID:                   strings.TrimSpace(record.ID),
			Revision:             record.Revision,
			TerminalRevision:     record.TerminalRevision,
			Title:                strings.TrimSpace(record.Title),
			Objective:            strings.TrimSpace(record.Objective),
			Status:               record.Status,
			OwnerSessionID:       strings.TrimSpace(record.OwnerSessionID),
			OwnerDelegated:       record.OwnerDelegated,
			SourceThreadID:       strings.TrimSpace(record.SourceThreadID),
			CompletionPolicy:     record.CompletionPolicy,
			DoneCriteriaRef:      strings.TrimSpace(record.DoneCriteriaRef),
			NextAction:           strings.TrimSpace(record.NextAction),
			WaitFor:              strings.TrimSpace(record.WaitFor),
			Wake:                 cloneWorkWake(record.Wake),
			SuccessorReservation: cloneSuccessorReservation(record.SuccessorReservation),
			SessionFinalizations: cloneSessionFinalizations(record.SessionFinalizations),
			ContextRef:           strings.TrimSpace(record.ContextRef),
			CreatedAt:            record.CreatedAt,
			UpdatedAt:            record.UpdatedAt,
		})
	}
	return out
}

func cloneWorkWake(wake *WorkWake) *WorkWake {
	if wake == nil {
		return nil
	}
	copy := *wake
	copy.Ref = strings.TrimSpace(copy.Ref)
	return &copy
}

func cloneSuccessorReservation(reservation *WorkSuccessorReservation) *WorkSuccessorReservation {
	if reservation == nil {
		return nil
	}
	copy := *reservation
	copy.SessionID = strings.TrimSpace(copy.SessionID)
	copy.ProviderTurnID = strings.TrimSpace(copy.ProviderTurnID)
	copy.EventID = strings.TrimSpace(copy.EventID)
	copy.HandlingID = strings.TrimSpace(copy.HandlingID)
	return &copy
}

func cloneSessionFinalizations(finalizations []SessionFinalization) []SessionFinalization {
	if len(finalizations) == 0 {
		return nil
	}
	out := make([]SessionFinalization, len(finalizations))
	for index, finalization := range finalizations {
		finalization.SessionID = strings.TrimSpace(finalization.SessionID)
		finalization.LastError = strings.TrimSpace(finalization.LastError)
		out[index] = finalization
	}
	return out
}

// bindUnresolvedSourceThreadIDs freezes the current Brain chat thread onto
// every Work that still lacks source_thread_id. Explicit SourceThreadID values
// (including scheduled-action sources already stored on the Work) are kept.
// Historical cards are never bulk-materialized here.
func bindUnresolvedSourceThreadIDs(database *orchestrationDatabase, currentThreadID string) bool {
	if database == nil {
		return false
	}
	currentThreadID = strings.TrimSpace(currentThreadID)
	if currentThreadID == "" {
		return false
	}
	changed := false
	for index := range database.BrainWork {
		if strings.TrimSpace(database.BrainWork[index].SourceThreadID) != "" {
			continue
		}
		database.BrainWork[index].SourceThreadID = currentThreadID
		changed = true
	}
	return changed
}

// backfillTurnLeaseDeadlines mints one fresh upgrade grace from the current
// load/upgrade time for ledger rows persisted before per-turn leases existed.
// It must not derive from an old AcceptedAt: live pre-upgrade turns would
// otherwise load already expired and emit an immediate false session.stale.
// A non-zero deadline is never rewritten, making reload idempotent.
func backfillTurnLeaseDeadlines(database *orchestrationDatabase, upgradeAt time.Time) bool {
	if database == nil {
		return false
	}
	changed := false
	upgradeAt = upgradeAt.UTC()
	if upgradeAt.IsZero() {
		upgradeAt = time.Now().UTC()
	}
	for index := range database.BrainTurns {
		turn := &database.BrainTurns[index]
		if !turn.LeaseDeadline.IsZero() || turn.AcceptedAt.IsZero() {
			continue
		}
		turn.LeaseDeadline = upgradeAt.Add(turnLeaseGrace).UTC()
		changed = true
	}
	return changed
}

func ensureSingleJSONValue(decoder *json.Decoder, raw []byte) error {
	if trailing := bytes.TrimSpace(raw[decoder.InputOffset():]); len(trailing) != 0 {
		return fmt.Errorf("document must contain exactly one JSON value")
	}
	return nil
}

func validateBrainInputAdmissions(admissions []BrainInputAdmission) error {
	identities := make(map[string]struct{}, len(admissions))
	for index, admission := range admissions {
		admission.RequestID = strings.TrimSpace(admission.RequestID)
		admission.ThreadID = strings.TrimSpace(admission.ThreadID)
		admission.HostSessionID = strings.TrimSpace(admission.HostSessionID)
		admission.HostGeneration = strings.TrimSpace(admission.HostGeneration)
		admission.HostTurnID = strings.TrimSpace(admission.HostTurnID)
		admission.ProviderActivityID = strings.TrimSpace(admission.ProviderActivityID)
		admission.SessionID = strings.TrimSpace(admission.SessionID)
		admission.DisplayBody = strings.TrimSpace(admission.DisplayBody)
		admission.BodySHA256 = strings.TrimSpace(admission.BodySHA256)
		if admission.RequestID == "" || admission.ThreadID == "" || admission.HostSessionID == "" ||
			admission.SessionID == "" || admission.DisplayBody == "" {
			return fmt.Errorf("brain_input_admissions[%d]: request, thread, host, session, and display body are required", index)
		}
		if admission.BodySHA256 != AdmissionDigest(admission.DisplayBody) {
			return fmt.Errorf("brain_input_admissions[%d]: display body digest mismatch", index)
		}
		if (admission.HostGeneration == "") != (admission.HostTurnID == "") {
			return fmt.Errorf("brain_input_admissions[%d]: host generation and turn identity must be paired", index)
		}
		if admission.CreatedAt.IsZero() {
			return fmt.Errorf("brain_input_admissions[%d]: created_at is required", index)
		}
		switch admission.State {
		case BrainInputAdmissionPending:
			if admission.AcceptedAt != nil || admission.SettledAt != nil {
				return fmt.Errorf("brain_input_admissions[%d]: pending admission cannot be settled", index)
			}
		case BrainInputAdmissionAccepted:
			if admission.AcceptedAt == nil || admission.AcceptedAt.IsZero() || admission.SettledAt != nil {
				return fmt.Errorf("brain_input_admissions[%d]: accepted admission requires accepted_at", index)
			}
		case BrainInputAdmissionNotSubmitted, BrainInputAdmissionUncertain:
			if admission.AcceptedAt != nil || admission.SettledAt == nil || admission.SettledAt.IsZero() {
				return fmt.Errorf("brain_input_admissions[%d]: terminal admission requires settled_at without accepted_at", index)
			}
		default:
			return fmt.Errorf("brain_input_admissions[%d]: invalid state %q", index, admission.State)
		}
		identity := admission.RequestID + "\x00" + admission.ThreadID
		if _, exists := identities[identity]; exists {
			return fmt.Errorf("brain_input_admissions[%d]: duplicate request_id/thread_id", index)
		}
		identities[identity] = struct{}{}
	}
	return nil
}

func validateOrchestrationDatabase(database orchestrationDatabase) error {
	return validateOrchestrationDatabaseWithSourceThread(database, true)
}

func validateOrchestrationDatabaseLoose(database orchestrationDatabase) error {
	return validateOrchestrationDatabaseWithSourceThread(database, false)
}

func validateOrchestrationDatabaseWithSourceThread(database orchestrationDatabase, requireSourceThread bool) error {
	if err := validateBrainInputAdmissions(database.BrainInputAdmissions); err != nil {
		return err
	}
	workIDs := make(map[string]struct{}, len(database.BrainWork))
	activeOwners := make(map[string]string, len(database.BrainWork))
	for index, item := range database.BrainWork {
		if err := validateWorkWithSourceThread(item, requireSourceThread); err != nil {
			return fmt.Errorf("brain_work[%d]: %w", index, err)
		}
		if _, exists := workIDs[item.ID]; exists {
			return fmt.Errorf("brain_work[%d]: duplicate work_id %q", index, item.ID)
		}
		workIDs[item.ID] = struct{}{}
		owner := strings.TrimSpace(item.OwnerSessionID)
		if owner != "" && item.Status != WorkDone && item.Status != WorkCancelled {
			if existingID := activeOwners[owner]; existingID != "" {
				return fmt.Errorf(
					"brain_work[%d]: owner_session_id %q already owns active Work %q",
					index,
					owner,
					existingID,
				)
			}
			activeOwners[owner] = item.ID
		}
		if reservation := item.SuccessorReservation; reservation != nil {
			sessionID := strings.TrimSpace(reservation.SessionID)
			if existingID := activeOwners[sessionID]; existingID != "" && existingID != item.ID {
				return fmt.Errorf("brain_work[%d]: reserved Session %q already executes active Work %q", index, sessionID, existingID)
			}
			activeOwners[sessionID] = item.ID
		}
	}
	eventIDs := make(map[string]struct{}, len(database.BrainWorkEvents))
	dedupeKeys := make(map[string]struct{}, len(database.BrainWorkEvents))
	sequences := make(map[uint64]string, len(database.BrainWorkEvents))
	for index, event := range database.BrainWorkEvents {
		if err := validateWorkEvent(event); err != nil {
			return fmt.Errorf("brain_work_events[%d]: %w", index, err)
		}
		if _, exists := workIDs[event.WorkID]; !exists {
			return fmt.Errorf("brain_work_events[%d]: unknown work_id %q", index, event.WorkID)
		}
		if _, exists := eventIDs[event.ID]; exists {
			return fmt.Errorf("brain_work_events[%d]: duplicate event_id %q", index, event.ID)
		}
		eventIDs[event.ID] = struct{}{}
		if existingID := sequences[event.Sequence]; existingID != "" {
			return fmt.Errorf("brain_work_events[%d]: sequence %d already belongs to event %q", index, event.Sequence, existingID)
		}
		sequences[event.Sequence] = event.ID
		if event.Sequence > database.NextEventSequence {
			return fmt.Errorf("brain_work_events[%d]: sequence %d exceeds next_event_sequence %d", index, event.Sequence, database.NextEventSequence)
		}
		key := event.WorkID + "\x00" + event.DedupeKey
		if _, exists := dedupeKeys[key]; exists {
			return fmt.Errorf("brain_work_events[%d]: duplicate dedupe_key %q", index, event.DedupeKey)
		}
		dedupeKeys[key] = struct{}{}
	}
	inFlightByWork := map[string]string{}
	globalDelivered := ""
	for index, event := range database.BrainWorkEvents {
		if targetID := strings.TrimSpace(event.CoalescedInto); targetID != "" {
			targetIndex := workEventIndex(database.BrainWorkEvents, targetID)
			if targetIndex < 0 || targetID == event.ID || database.BrainWorkEvents[targetIndex].WorkID != event.WorkID {
				return fmt.Errorf("brain_work_events[%d]: invalid coalesced_into %q", index, targetID)
			}
		}
		if event.Resolution != "" || event.DiscardedAt != nil || event.HandledAt != nil || event.HistoricalDelivery {
			continue
		}
		inFlight := event.ClaimedAt != nil && event.DeliveredAt == nil ||
			event.DeliveredAt != nil && event.HandlingEndedAt == nil
		if !inFlight {
			continue
		}
		if existingID := inFlightByWork[event.WorkID]; existingID != "" {
			return fmt.Errorf("brain_work_events[%d]: Work %q already has in-flight event %q", index, event.WorkID, existingID)
		}
		inFlightByWork[event.WorkID] = event.ID
		if event.DeliveredAt != nil && event.HandlingEndedAt == nil {
			if globalDelivered != "" {
				return fmt.Errorf("brain_work_events[%d]: Host already has live delivered handling %q", index, globalDelivered)
			}
			globalDelivered = event.ID
		}
	}
	if database.Migrations.SignalSystemV1At != nil {
		for index, item := range database.BrainWork {
			if err := validateWorkSignalState(database, item); err != nil {
				return fmt.Errorf("brain_work[%d]: %w", index, err)
			}
		}
	}
	if err := validateTurnLedger(database.BrainTurns, workIDs); err != nil {
		return err
	}
	for index, item := range database.BrainWork {
		reservation := item.SuccessorReservation
		if reservation == nil {
			continue
		}
		if strings.TrimSpace(reservation.EventID) != "" {
			liveBinding := false
			for _, event := range database.BrainWorkEvents {
				if event.ID == reservation.EventID && event.WorkID == item.ID &&
					event.HandlingID == reservation.HandlingID && event.DeliveredAt != nil &&
					event.HandledAt == nil && event.HandlingEndedAt == nil && !event.HistoricalDelivery {
					liveBinding = true
					break
				}
			}
			if !liveBinding {
				return fmt.Errorf("brain_work[%d]: successor reservation is bound to a non-live Host handling", index)
			}
		}
		if strings.TrimSpace(reservation.ProviderTurnID) == "" {
			continue
		}
		found := false
		for _, turn := range database.BrainTurns {
			if turn.WorkID == item.ID && turn.SessionID == reservation.SessionID &&
				turn.TurnID == reservation.ProviderTurnID && !isHostHandlingTurn(database, turn) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("brain_work[%d]: successor reservation does not name a canonical Turn", index)
		}
	}
	if active := database.HostForegroundTurn; active != nil {
		if strings.TrimSpace(active.HostSessionID) == "" || strings.TrimSpace(active.HostGeneration) == "" ||
			strings.TrimSpace(active.HostTurnID) == "" || active.StartedAt.IsZero() {
			return fmt.Errorf("host_foreground_turn requires host session, generation, turn, and started_at")
		}
	}
	if err := validateActiveExecutionOwners(database); err != nil {
		return err
	}
	if err := validateTurnSubmissions(database.BrainTurnSubmissions, workIDs); err != nil {
		return err
	}
	for index, submission := range database.BrainTurnSubmissions {
		if submission.State != watcher.TurnSubmissionPending || strings.TrimSpace(submission.ClaimToken) != "" {
			continue
		}
		workIndex := workIndex(database.BrainWork, submission.WorkID)
		if workIndex >= 0 {
			status := database.BrainWork[workIndex].Status
			if status == WorkDone || status == WorkCancelled {
				return fmt.Errorf("brain_turn_submissions[%d]: terminal Work %q cannot retain pending delegated provider authority", index, submission.WorkID)
			}
		}
	}
	return nil
}

func validateActiveExecutionOwners(database orchestrationDatabase) error {
	activeByWork := map[string]map[string]struct{}{}
	for _, turn := range database.BrainTurns {
		if watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(database, turn) {
			continue
		}
		index := workIndex(database.BrainWork, turn.WorkID)
		if index < 0 {
			continue
		}
		item := database.BrainWork[index]
		if item.Status == WorkDone || item.Status == WorkCancelled {
			continue
		}
		owner := strings.TrimSpace(item.OwnerSessionID)
		reserved := ""
		if item.SuccessorReservation != nil {
			reserved = strings.TrimSpace(item.SuccessorReservation.SessionID)
		}
		if turn.SessionID != owner && turn.SessionID != reserved {
			state := reduceWorkProgressState(database, item)
			// The canonical Turn reducer may explicitly relinquish a blocked or
			// stale execution owner while retaining its exact Turn as lifecycle
			// evidence. Ready attention or a later typed wait then owns progress;
			// exact continue may promote this same active Session again.
			if owner != "" || (!state.Ready && !state.Waiting) {
				return fmt.Errorf("brain_turns: active Session %q is not an owner or reserved successor of Work %q", turn.SessionID, item.ID)
			}
		}
		if activeByWork[item.ID] == nil {
			activeByWork[item.ID] = map[string]struct{}{}
		}
		activeByWork[item.ID][turn.SessionID] = struct{}{}
		if len(activeByWork[item.ID]) > 1 {
			return fmt.Errorf("brain_work: Work %q has more than one active execution Session", item.ID)
		}
	}
	return nil
}

func isHostHandlingTurn(database orchestrationDatabase, turn TurnRecord) bool {
	for _, event := range database.BrainWorkEvents {
		if event.ID == turn.Receipt && event.WorkID == turn.WorkID &&
			event.DeliveryHostSessionID == turn.SessionID && event.ProviderTurnID == turn.TurnID {
			return true
		}
	}
	return false
}

func validateWork(item Work) error {
	return validateWorkWithSourceThread(item, true)
}

func validateWorkWithSourceThread(item Work, requireSourceThread bool) error {
	item.ID = strings.TrimSpace(item.ID)
	item.Title = strings.TrimSpace(item.Title)
	item.Objective = strings.TrimSpace(item.Objective)
	item.SourceThreadID = strings.TrimSpace(item.SourceThreadID)
	if item.ID == "" {
		return fmt.Errorf("work_id is required")
	}
	if item.Title == "" {
		return fmt.Errorf("title is required")
	}
	if item.Objective == "" {
		return fmt.Errorf("objective is required")
	}
	if !validWorkStatus(item.Status) {
		return fmt.Errorf("invalid status %q", item.Status)
	}
	if !validCompletionPolicy(item.CompletionPolicy) {
		return fmt.Errorf("invalid completion_policy %q", item.CompletionPolicy)
	}
	if item.CompletionPolicy == CompletionUntilDone && strings.TrimSpace(item.DoneCriteriaRef) == "" {
		return fmt.Errorf("until_done requires done_criteria_ref")
	}
	if item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
		return fmt.Errorf("created_at and updated_at are required")
	}
	if item.Revision == 0 {
		return fmt.Errorf("revision is required")
	}
	terminal := item.Status == WorkDone || item.Status == WorkCancelled
	if terminal {
		if item.TerminalRevision == 0 || item.TerminalRevision > item.Revision {
			return fmt.Errorf("terminal Work requires terminal_revision within its revision history")
		}
	} else if item.TerminalRevision != 0 {
		return fmt.Errorf("nonterminal Work cannot retain terminal_revision")
	}
	if item.Wake != nil {
		if err := validateWorkWake(item.Wake); err != nil {
			return err
		}
		if item.Status == WorkDone || item.Status == WorkCancelled {
			return fmt.Errorf("terminal Work cannot retain a wake")
		}
	}
	if reservation := item.SuccessorReservation; reservation != nil {
		if strings.TrimSpace(reservation.SessionID) == "" {
			return fmt.Errorf("successor reservation requires session_id")
		}
		bound := strings.TrimSpace(reservation.EventID) != "" || strings.TrimSpace(reservation.HandlingID) != ""
		if bound && (strings.TrimSpace(reservation.EventID) == "" || strings.TrimSpace(reservation.HandlingID) == "") {
			return fmt.Errorf("successor reservation requires both event_id and handling_id")
		}
		if item.Status == WorkDone || item.Status == WorkCancelled {
			return fmt.Errorf("terminal Work cannot retain a successor reservation")
		}
	}
	finalizationIDs := map[string]struct{}{}
	for _, finalization := range item.SessionFinalizations {
		if item.Status != WorkDone && item.Status != WorkCancelled {
			return fmt.Errorf("Session finalizations require terminal Work")
		}
		if strings.TrimSpace(finalization.SessionID) == "" || finalization.UpdatedAt.IsZero() {
			return fmt.Errorf("Session finalization requires session_id and updated_at")
		}
		if _, duplicate := finalizationIDs[finalization.SessionID]; duplicate {
			return fmt.Errorf("duplicate Session finalization %q", finalization.SessionID)
		}
		finalizationIDs[finalization.SessionID] = struct{}{}
		switch finalization.State {
		case SessionFinalizationPending, SessionFinalizationFailed,
			SessionFinalizationComplete, SessionFinalizationSkipped:
		default:
			return fmt.Errorf("invalid Session finalization state %q", finalization.State)
		}
	}
	if requireSourceThread && item.SourceThreadID == "" {
		return fmt.Errorf("source_thread_id is required")
	}
	return nil
}

func validateWorkEvent(event WorkEvent) error {
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("event_id is required")
	}
	if strings.TrimSpace(event.WorkID) == "" {
		return fmt.Errorf("work_id is required")
	}
	if strings.TrimSpace(event.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if strings.TrimSpace(event.DedupeKey) == "" {
		return fmt.Errorf("dedupe_key is required")
	}
	if event.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if event.Sequence == 0 || event.WorkRevision == 0 {
		return fmt.Errorf("sequence and work_revision are required")
	}
	if event.ClaimedAt == nil && strings.TrimSpace(event.DeliveryHostSessionID) != "" {
		return fmt.Errorf("delivery host requires a claim")
	}
	if event.ClaimedAt != nil && event.DeliveredAt == nil && strings.TrimSpace(event.DeliveryHostSessionID) == "" {
		return fmt.Errorf("undelivered claim requires delivery_host_session_id")
	}
	if event.DeliveredAt != nil && event.ClaimedAt == nil {
		return fmt.Errorf("delivered event must have a claim")
	}
	if event.ClaimedAt != nil && event.DeliveredAt == nil && !event.HistoricalDelivery {
		if strings.TrimSpace(event.HandlingID) != "" || strings.TrimSpace(event.ProviderTurnID) != "" {
			if strings.TrimSpace(event.HandlingID) == "" || strings.TrimSpace(event.ProviderTurnID) == "" ||
				event.HandlingID == event.ProviderTurnID || event.DeliveryWorkRevision == 0 || event.DeliverySequenceFence == 0 {
				return fmt.Errorf("live claim requires distinct handling and provider Turn identities, Work revision, and sequence fence")
			}
		}
	}
	if event.DeliveredAt != nil && !event.HistoricalDelivery {
		if strings.TrimSpace(event.HandlingID) == "" || strings.TrimSpace(event.ProviderTurnID) == "" ||
			event.HandlingID == event.ProviderTurnID || event.DeliveryWorkRevision == 0 || event.DeliverySequenceFence == 0 {
			return fmt.Errorf("live delivery requires distinct handling and provider Turn identities, Work revision, and sequence fence")
		}
	}
	if event.HandledAt != nil {
		if event.DeliveredAt == nil && strings.TrimSpace(event.CoalescedInto) == "" || !validWorkDisposition(event.Disposition) {
			return fmt.Errorf("handled event requires delivery and disposition")
		}
	}
	if event.Disposition != "" && event.HandledAt == nil {
		return fmt.Errorf("disposition requires handled_at")
	}
	return nil
}

func validateWorkWake(wake *WorkWake) error {
	if wake == nil {
		return nil
	}
	if strings.TrimSpace(wake.Ref) == "" {
		return fmt.Errorf("typed wake requires ref")
	}
	switch wake.Kind {
	case WorkWakeSessionTerminal, WorkWakeCalendarResult, WorkWakeUserInput:
		return nil
	default:
		return fmt.Errorf("invalid typed wake kind %q", wake.Kind)
	}
}

// SessionTerminalWakeRef is the exact canonical producer identity for one
// Session Turn. A Session name alone is deliberately insufficient because it
// can be reused by later provider Turns.
func SessionTerminalWakeRef(sessionID, turnID string) string {
	return "session:" + strings.TrimSpace(sessionID) + ":turn:" + strings.TrimSpace(turnID)
}

func validateWorkWakeProducer(database orchestrationDatabase, item Work, wake *WorkWake) error {
	if err := validateWorkWake(wake); err != nil {
		return err
	}
	if wake == nil {
		return fmt.Errorf("wait disposition requires a typed wake")
	}
	ref := strings.TrimSpace(wake.Ref)
	switch wake.Kind {
	case WorkWakeUserInput:
		if ref != "brain-thread:"+strings.TrimSpace(item.SourceThreadID) {
			return fmt.Errorf("user_input wake must name the Work source Brain thread")
		}
	case WorkWakeSessionTerminal:
		for _, turn := range database.BrainTurns {
			if watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(database, turn) ||
				ref != SessionTerminalWakeRef(turn.SessionID, turn.TurnID) {
				continue
			}
			producerIndex := workIndex(database.BrainWork, turn.WorkID)
			if producerIndex >= 0 && database.BrainWork[producerIndex].SourceThreadID == item.SourceThreadID {
				return nil
			}
		}
		return fmt.Errorf("session_terminal wake does not name a permitted live canonical Session Turn")
	case WorkWakeCalendarResult:
		calendarIDs := strings.SplitN(strings.TrimPrefix(ref, "calendar:"), ":", 2)
		if len(calendarIDs) != 2 || calendarIDs[0] == "" || calendarIDs[1] == "" {
			return fmt.Errorf("calendar_result wake must name an exact Calendar item/run")
		}
		expectedProducerID := calendarWorkID(calendarIDs[0], calendarIDs[1])
		for _, producer := range database.BrainWork {
			if producer.ID != item.ID && producer.Status != WorkDone && producer.Status != WorkCancelled &&
				producer.ID == expectedProducerID && strings.TrimSpace(producer.ContextRef) == ref &&
				producer.SourceThreadID == item.SourceThreadID {
				return nil
			}
		}
		return fmt.Errorf("calendar_result wake does not name a permitted live canonical Calendar producer")
	}
	return nil
}

func workWakeEqual(left, right *WorkWake) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Kind == right.Kind && strings.TrimSpace(left.Ref) == strings.TrimSpace(right.Ref)
}

func validWorkDisposition(disposition WorkDisposition) bool {
	switch disposition {
	case WorkDispositionContinue, WorkDispositionWait, WorkDispositionComplete,
		WorkDispositionCancel, WorkDispositionSupersede:
		return true
	default:
		return false
	}
}

func validateWorkSignalState(database orchestrationDatabase, item Work) error {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return nil
	}
	_, err := deriveWorkProgressMode(database, item)
	return err
}

type workProgressState struct {
	Owned             bool
	Waiting           bool
	Ready             bool
	LiveCanonicalTurn bool
	OwnerAdmission    bool
}

// eventDeliveryState is the one derived delivery state of the append-only
// Work Event head. It collapses the former attentionObligationState,
// WorkAttentionState, and WorkReviewState mirrors into a single scheduler
// fact: pending (queued for the Host lane), dispatching (claimed, delivery
// receipt outstanding), delivered (awaiting the exact typed disposition), and
// handled (terminal; no longer a head). Fewer than ten persisted transition
// states exist across the lane in total.
type eventDeliveryState uint8

const (
	deliveryNone eventDeliveryState = iota
	deliveryPending
	deliveryDispatching
	deliveryDelivered
)

type attentionObligation struct {
	WorkID      string
	HeadIndex   int
	LatestIndex int
	State       eventDeliveryState
	NeedsRepair bool
}

func (obligation attentionObligation) exists() bool {
	return obligation.HeadIndex >= 0 && obligation.State != deliveryNone
}

func attentionEventOutstanding(event WorkEvent) bool {
	return event.Actionable && event.HandledAt == nil && event.DiscardedAt == nil &&
		event.Resolution == "" && !event.HistoricalDelivery
}

// attentionEventCanObligate excludes durable audit rows that are forbidden
// from ever reaching the Host scheduler. In particular, pre-TurnID delegated
// lifecycle rows stay append-only evidence but cannot become a coalescing
// head for a current turn-scoped fact.
func attentionEventCanObligate(database orchestrationDatabase, event WorkEvent) bool {
	if !attentionEventOutstanding(event) {
		return false
	}
	if isSessionLifecycleKind(event.Kind) && strings.TrimSpace(event.ReplayOf) == "" &&
		!isTurnScopedSessionDedupeKey(event.DedupeKey) && !isCanonicalSessionWakeDedupeKey(event.DedupeKey) {
		return false
	}
	index := workIndex(database.BrainWork, event.WorkID)
	if index < 0 {
		return false
	}
	item := database.BrainWork[index]
	if item.Status != WorkDone && item.Status != WorkCancelled {
		return true
	}
	// A finalization retry is born only after the Work is terminal. Its
	// immutable Session/attempt identity, not the Work's mutable metadata
	// timestamp, is the causality fence. A later title, context, or other
	// terminal metadata update therefore cannot turn the current failed
	// teardown obligation into audit-only history.
	if terminalFinalizationFailureOwnsAttention(item, event) {
		return true
	}
	// TerminalRevision is the immutable causal boundary written by the
	// transition that first terminalized the Work. Later title/context edits
	// advance Revision but never move this fence.
	return item.TerminalRevision != 0 && event.WorkRevision >= item.TerminalRevision
}

func terminalFinalizationFailureOwnsAttention(item Work, event WorkEvent) bool {
	if event.Kind != "brain.finalization_failed" {
		return false
	}
	sessionID := strings.TrimSpace(event.SourceName)
	if sessionID == "" || event.PayloadRef != "session:"+sessionID {
		return false
	}
	for _, finalization := range item.SessionFinalizations {
		if finalization.SessionID == sessionID && finalization.State == SessionFinalizationFailed &&
			event.DedupeKey == finalizationFailureDedupeKey(sessionID, finalization.Attempts) {
			return true
		}
	}
	return false
}

func finalizationFailureDedupeKey(sessionID string, attempt uint32) string {
	return fmt.Sprintf("brain:finalization:%s:attempt:%d", strings.TrimSpace(sessionID), attempt)
}

func reduceAttentionObligations(database orchestrationDatabase) map[string]attentionObligation {
	out := make(map[string]attentionObligation, len(database.BrainWork))
	byID := make(map[string]int, len(database.BrainWorkEvents))
	for index, event := range database.BrainWorkEvents {
		byID[event.ID] = index
	}
	for _, item := range database.BrainWork {
		out[item.ID] = attentionObligation{WorkID: item.ID, HeadIndex: -1, LatestIndex: -1}
	}
	for index, event := range database.BrainWorkEvents {
		if !attentionEventOutstanding(event) {
			continue
		}
		obligation := out[event.WorkID]
		if !attentionEventCanObligate(database, event) {
			// Known audit-only rows are valid durable history. They neither
			// obligate Brain nor create a repair/backlog obligation.
			out[event.WorkID] = obligation
			continue
		}
		if obligation.LatestIndex < 0 || event.Sequence > database.BrainWorkEvents[obligation.LatestIndex].Sequence {
			obligation.LatestIndex = index
		}
		coalesced := false
		if targetID := strings.TrimSpace(event.CoalescedInto); targetID != "" {
			coalesced = attentionCoalescingPathValid(database, byID, event)
		}
		if !coalesced {
			if obligation.HeadIndex < 0 || event.Sequence < database.BrainWorkEvents[obligation.HeadIndex].Sequence {
				if obligation.HeadIndex >= 0 {
					obligation.NeedsRepair = true
				}
				obligation.HeadIndex = index
			} else {
				obligation.NeedsRepair = true
			}
		}
		out[event.WorkID] = obligation
	}
	for workID, obligation := range out {
		if obligation.HeadIndex < 0 {
			continue
		}
		head := database.BrainWorkEvents[obligation.HeadIndex]
		switch {
		case head.DeliveredAt != nil && head.HandlingEndedAt == nil:
			obligation.State = deliveryDelivered
		case head.ClaimedAt != nil:
			obligation.State = deliveryDispatching
		default:
			obligation.State = deliveryPending
		}
		out[workID] = obligation
	}
	return out
}

// attentionCoalescingPathValid requires a coalesced row to reach one eligible
// uncoalesced root in the same Work. Dangling targets and cycles are treated as
// independent roots by the reducer, which keeps Attention schedulable and
// marks the Work for repair instead of silently losing the obligation.
func attentionCoalescingPathValid(database orchestrationDatabase, byID map[string]int, event WorkEvent) bool {
	seen := map[string]bool{event.ID: true}
	targetID := strings.TrimSpace(event.CoalescedInto)
	for targetID != "" {
		if seen[targetID] {
			return false
		}
		seen[targetID] = true
		targetIndex, found := byID[targetID]
		if !found {
			return false
		}
		target := database.BrainWorkEvents[targetIndex]
		if target.WorkID != event.WorkID || !attentionEventCanObligate(database, target) {
			return false
		}
		targetID = strings.TrimSpace(target.CoalescedInto)
	}
	return true
}

func reduceAttentionObligation(database orchestrationDatabase, workID string) attentionObligation {
	if obligation, found := reduceAttentionObligations(database)[strings.TrimSpace(workID)]; found {
		return obligation
	}
	return attentionObligation{WorkID: strings.TrimSpace(workID), HeadIndex: -1, LatestIndex: -1}
}

func workHasLiveCanonicalOwnerTurn(database orchestrationDatabase, item Work) bool {
	ownerID := strings.TrimSpace(item.OwnerSessionID)
	if ownerID == "" {
		return false
	}
	turn, found := currentTurnForSession(database, ownerID)
	if !found || turn.WorkID != item.ID || watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(database, turn) {
		return false
	}
	// A same-Session review correction may already have an accepted successor
	// Turn while the delivered Attention still owns progress. The reservation
	// identifies that exact Turn as staged lifecycle state until continue clears
	// the reservation and transfers execution ownership.
	if reservation := item.SuccessorReservation; reservation != nil &&
		strings.TrimSpace(reservation.SessionID) == ownerID &&
		strings.TrimSpace(reservation.ProviderTurnID) == turn.TurnID {
		return false
	}
	return true
}

func workHasPendingOwnerAdmission(database orchestrationDatabase, item Work) bool {
	ownerID := strings.TrimSpace(item.OwnerSessionID)
	if ownerID == "" || item.SuccessorReservation != nil {
		return false
	}
	for _, submission := range database.BrainTurnSubmissions {
		if submission.WorkID == item.ID && submission.SessionID == ownerID &&
			submission.State == watcher.TurnSubmissionPending && submission.ExistingTurnID == "" {
			return true
		}
	}
	return false
}

// reduceWorkProgressState derives the three progress predicates independently.
// OwnerSessionID text is not authority: only its current canonical nonterminal
// execution Turn or its exact pending initial submission owns progress. A bare
// owner string has no authority.
func reduceWorkProgressState(database orchestrationDatabase, item Work) workProgressState {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return workProgressState{}
	}
	liveOwner := workHasLiveCanonicalOwnerTurn(database, item)
	ownerAdmission := !liveOwner && workHasPendingOwnerAdmission(database, item)
	obligation := reduceAttentionObligation(database, item.ID)
	state := workProgressState{
		Owned:             liveOwner || ownerAdmission,
		Waiting:           item.Wake != nil,
		Ready:             obligation.exists() || obligation.NeedsRepair,
		LiveCanonicalTurn: liveOwner,
		OwnerAdmission:    ownerAdmission,
	}
	// An accepted S1 reservation remains lifecycle-exclusive across Host
	// requeue, but the ready disposition obligation is the progress owner. An
	// unbound reservation is an owner only when no wait or attention exists.
	if !state.Owned && !state.Waiting && !state.Ready {
		if reservation := item.SuccessorReservation; reservation != nil && strings.TrimSpace(reservation.EventID) == "" {
			state.Owned = true
		}
	}
	return state
}

func deriveWorkProgressMode(database orchestrationDatabase, item Work) (WorkProgressMode, error) {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return "", nil
	}
	state := reduceWorkProgressState(database, item)
	// Attention is orthogonal to exact execution ownership. A live canonical
	// delegated turn may own execution while Brain also owes a stale/blocked
	// review. Only execution ownership and a typed external wait are mutually
	// exclusive; Ready becomes the primary progress mode only when neither is
	// present.
	if state.Owned && state.Waiting || !state.Owned && !state.Waiting && !state.Ready {
		return "", fmt.Errorf(
			"nonterminal Work requires exactly one owned, waiting, or ready progress mode (owned=%t waiting=%t ready=%t)",
			state.Owned,
			state.Waiting,
			state.Ready,
		)
	}
	if state.Owned {
		return WorkProgressOwned, nil
	}
	if state.Waiting {
		return WorkProgressWaiting, nil
	}
	return WorkProgressReady, nil
}

func mustDeriveWorkProgressMode(database orchestrationDatabase, item Work) WorkProgressMode {
	mode, _ := deriveWorkProgressMode(database, item)
	return mode
}

// workHasAttentionObligation reports any outstanding or repair-needing
// attention head for the Work, regardless of delivery stage. It drives the
// wire AttentionPending flag only; it never gates scheduling. A delivered
// handling awaiting its typed disposition is the Host lane's stop gate
// (HasLiveDeliveredHandling); worker-side correction admissions stage under
// that exact handling instead of being rejected, and a queued (pending) head
// never gates either path.
func workHasAttentionObligation(database orchestrationDatabase, workID string) bool {
	obligation := reduceAttentionObligation(database, workID)
	return obligation.exists() || obligation.NeedsRepair
}

func workHasInFlightHandling(database orchestrationDatabase, workID string) bool {
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.DeliveredAt != nil && event.HandledAt == nil &&
			event.HandlingEndedAt == nil && !event.HistoricalDelivery {
			return true
		}
	}
	return false
}

// WorkHasInFlightHandling reports whether Work is currently owned by an exact
// delivered Host handling transaction. It is a preflight hint for delegated
// spawn; ReserveWorkSuccessor performs the authoritative check under the Store
// lock.
func (s *Store) WorkHasInFlightHandling(workID string) (bool, error) {
	workID = strings.TrimSpace(workID)
	if s == nil || workID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	return workHasInFlightHandling(database, workID), nil
}

func validWorkStatus(status WorkStatus) bool {
	switch status {
	case WorkOpen, WorkRunning, WorkWaiting, WorkNeedsInput, WorkDone, WorkCancelled:
		return true
	default:
		return false
	}
}

func validCompletionPolicy(policy CompletionPolicy) bool {
	return policy == CompletionBounded || policy == CompletionUntilDone
}

func (s *Store) loadOrchestrationLocked() (orchestrationDatabase, error) {
	raw, err := os.ReadFile(s.orchestrationPath())
	if err != nil {
		return orchestrationDatabase{}, err
	}
	database, _, err := decodeOrchestrationDatabase(raw)
	return database, err
}

func (s *Store) persistOrchestrationLocked(database orchestrationDatabase) error {
	database.SchemaVersion = orchestrationSchemaVersion
	if database.BrainInputAdmissions == nil {
		database.BrainInputAdmissions = []BrainInputAdmission{}
	}
	if database.BrainWork == nil {
		database.BrainWork = []Work{}
	}
	if database.BrainWorkEvents == nil {
		database.BrainWorkEvents = []WorkEvent{}
	}
	if database.BrainTurns == nil {
		database.BrainTurns = []TurnRecord{}
	}
	if database.BrainTurnSubmissions == nil {
		database.BrainTurnSubmissions = []TurnSubmissionRecord{}
	}
	upgradeSignalSchema(&database)
	sort.Slice(database.BrainWork, func(left, right int) bool {
		if database.BrainWork[left].CreatedAt.Equal(database.BrainWork[right].CreatedAt) {
			return database.BrainWork[left].ID < database.BrainWork[right].ID
		}
		return database.BrainWork[left].CreatedAt.Before(database.BrainWork[right].CreatedAt)
	})
	sort.Slice(database.BrainInputAdmissions, func(left, right int) bool {
		if database.BrainInputAdmissions[left].CreatedAt.Equal(database.BrainInputAdmissions[right].CreatedAt) {
			if database.BrainInputAdmissions[left].RequestID == database.BrainInputAdmissions[right].RequestID {
				return database.BrainInputAdmissions[left].ThreadID < database.BrainInputAdmissions[right].ThreadID
			}
			return database.BrainInputAdmissions[left].RequestID < database.BrainInputAdmissions[right].RequestID
		}
		return database.BrainInputAdmissions[left].CreatedAt.Before(database.BrainInputAdmissions[right].CreatedAt)
	})
	sort.Slice(database.BrainWorkEvents, func(left, right int) bool {
		if database.BrainWorkEvents[left].CreatedAt.Equal(database.BrainWorkEvents[right].CreatedAt) {
			return database.BrainWorkEvents[left].ID < database.BrainWorkEvents[right].ID
		}
		return database.BrainWorkEvents[left].CreatedAt.Before(database.BrainWorkEvents[right].CreatedAt)
	})
	sort.Slice(database.BrainTurns, func(left, right int) bool {
		if database.BrainTurns[left].SessionID == database.BrainTurns[right].SessionID {
			return database.BrainTurns[left].TurnID < database.BrainTurns[right].TurnID
		}
		return database.BrainTurns[left].SessionID < database.BrainTurns[right].SessionID
	})
	sort.Slice(database.BrainTurnSubmissions, func(left, right int) bool {
		if database.BrainTurnSubmissions[left].SessionID == database.BrainTurnSubmissions[right].SessionID {
			return database.BrainTurnSubmissions[left].ProposedTurnID < database.BrainTurnSubmissions[right].ProposedTurnID
		}
		return database.BrainTurnSubmissions[left].SessionID < database.BrainTurnSubmissions[right].SessionID
	})
	if err := validateOrchestrationDatabase(database); err != nil {
		return err
	}
	return s.writeOrchestration(s.orchestrationPath(), database)
}

func (s *Store) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func normalizeWorkForCreate(item Work, now time.Time) (Work, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	item.Title = strings.TrimSpace(item.Title)
	item.Objective = strings.TrimSpace(item.Objective)
	item.OwnerSessionID = strings.TrimSpace(item.OwnerSessionID)
	item.SourceThreadID = strings.TrimSpace(item.SourceThreadID)
	item.DoneCriteriaRef = strings.TrimSpace(item.DoneCriteriaRef)
	item.NextAction = strings.TrimSpace(item.NextAction)
	item.WaitFor = strings.TrimSpace(item.WaitFor)
	item.Wake = cloneWorkWake(item.Wake)
	item.SuccessorReservation = nil
	item.SessionFinalizations = nil
	item.ContextRef = strings.TrimSpace(item.ContextRef)
	if item.Status == "" {
		item.Status = WorkOpen
	}
	if item.CompletionPolicy == "" {
		item.CompletionPolicy = CompletionBounded
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	item.Revision = 1
	if item.Status == WorkDone || item.Status == WorkCancelled {
		item.TerminalRevision = item.Revision
	} else {
		item.TerminalRevision = 0
	}
	if err := validateWork(item); err != nil {
		return Work{}, err
	}
	return item, nil
}

func (s *Store) resolveSourceThreadID(item Work) (Work, error) {
	item.SourceThreadID = strings.TrimSpace(item.SourceThreadID)
	if item.SourceThreadID != "" {
		return item, nil
	}
	threadID, err := s.ChatThreadID()
	if err != nil {
		return Work{}, err
	}
	item.SourceThreadID = strings.TrimSpace(threadID)
	if item.SourceThreadID == "" {
		return Work{}, fmt.Errorf("source_thread_id is required")
	}
	return item, nil
}

func (s *Store) CreateWork(item Work) (Work, error) {
	now := s.nowUTC()
	var err error
	item, err = s.resolveSourceThreadID(item)
	if err != nil {
		return Work{}, err
	}
	item, err = normalizeWorkForCreate(item, now)
	if err != nil {
		return Work{}, err
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err == nil {
		for _, current := range database.BrainWork {
			if current.ID == item.ID {
				err = ErrWorkConflict
				break
			}
		}
	}
	if err == nil {
		database.BrainWork = append(database.BrainWork, item)
		itemIndex := workIndex(database.BrainWork, item.ID)
		item, err = ensureInitialAttentionLocked(&database, itemIndex, item, now)
		if err == nil {
			err = s.persistOrchestrationLocked(database)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return Work{}, err
	}
	s.broadcastWorkChange(item.ID)
	return item, nil
}

// EnsureWork creates the deterministic Work once and returns the existing row
// on duplicate calls. The caller must use stable IDs for external occurrences.
func (s *Store) EnsureWork(item Work) (Work, bool, error) {
	if strings.TrimSpace(item.ID) == "" {
		return Work{}, false, fmt.Errorf("deterministic work_id is required")
	}
	now := s.nowUTC()
	var err error
	item, err = s.resolveSourceThreadID(item)
	if err != nil {
		return Work{}, false, err
	}
	item, err = normalizeWorkForCreate(item, now)
	if err != nil {
		return Work{}, false, err
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err == nil {
		for _, current := range database.BrainWork {
			if current.ID == item.ID {
				s.mu.Unlock()
				return current, false, nil
			}
		}
		database.BrainWork = append(database.BrainWork, item)
		itemIndex := workIndex(database.BrainWork, item.ID)
		item, err = ensureInitialAttentionLocked(&database, itemIndex, item, now)
		if err == nil {
			err = s.persistOrchestrationLocked(database)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return Work{}, false, err
	}
	s.broadcastWorkChange(item.ID)
	return item, true, nil
}

func (s *Store) Work(id string) (Work, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return Work{}, err
	}
	for _, item := range database.BrainWork {
		if item.ID == id {
			return item, nil
		}
	}
	return Work{}, ErrWorkNotFound
}

func (s *Store) ListWork() ([]Work, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	out := append([]Work(nil), database.BrainWork...)
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].UpdatedAt.Equal(out[right].UpdatedAt) {
			return out[left].ID < out[right].ID
		}
		return out[left].UpdatedAt.After(out[right].UpdatedAt)
	})
	return out, nil
}

func (s *Store) UpdateWork(id string, update WorkUpdate) (Work, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	var item Work
	if err == nil {
		index := workIndex(database.BrainWork, id)
		if index < 0 {
			err = ErrWorkNotFound
		} else {
			now := s.nowUTC()
			item, err = applyWorkUpdateLocked(&database, index, update, now)
			if err == nil {
				item, err = ensureInitialAttentionLocked(&database, index, item, now)
				if err == nil {
					err = s.persistOrchestrationLocked(database)
				}
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		return Work{}, err
	}
	s.broadcastWorkChange(item.ID)
	return item, nil
}

// CloseWork terminalizes one exact current Work revision without routing an
// artificial reconciliation turn through the Host. A live Host claim remains
// the one fail-closed boundary. Pending delegated provider submissions and
// unaccepted successor reservations are subordinate to the explicit
// actor/revision decision: they are retired atomically so late evidence can
// never revive the Work. The terminal update, Attention settlement, retirement,
// Session finalization obligations, and audit Event share one replacement.
func (s *Store) CloseWork(request WorkCloseRequest) (Work, error) {
	request.WorkID = strings.TrimSpace(request.WorkID)
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.WorkID == "" || request.ExpectedRevision == 0 || request.Actor == "" || request.Reason == "" ||
		(request.Status != WorkDone && request.Status != WorkCancelled) {
		return Work{}, fmt.Errorf("work_id, expected_work_revision, terminal status, actor, and reason are required")
	}

	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	itemIndex := workIndex(database.BrainWork, request.WorkID)
	if itemIndex < 0 {
		s.mu.Unlock()
		return Work{}, ErrWorkNotFound
	}
	item := database.BrainWork[itemIndex]
	if item.Revision != request.ExpectedRevision {
		s.mu.Unlock()
		return Work{}, ErrWorkRevisionConflict
	}
	if item.Status == WorkDone || item.Status == WorkCancelled {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("%w: Work %s is already terminal", ErrWorkCloseConflict, item.ID)
	}
	if eventID, owned := activeHostLaneEvent(database, item.ID); owned {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("%w: Event %s still owns the Host lane", ErrWorkCloseConflict, eventID)
	}
	item.Status = request.Status
	item.Wake = nil
	item.NextAction = ""
	item.WaitFor = ""
	item.SessionFinalizations = terminalSessionFinalizations(database, item, now)
	retirePendingDelegatedSubmissionsForWork(&database, item.ID, now)
	item.SuccessorReservation = nil
	item.Revision++
	item.TerminalRevision = item.Revision
	item.UpdatedAt = now
	database.BrainWork[itemIndex] = item

	settledAt := now.UTC()
	terminalDisposition := WorkDispositionCancel
	if request.Status == WorkDone {
		terminalDisposition = WorkDispositionComplete
	}
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.WorkID != item.ID || !event.Actionable || event.HandledAt != nil || event.DiscardedAt != nil ||
			event.Resolution != "" || event.HistoricalDelivery {
			continue
		}
		switch {
		case event.DeliveredAt != nil && event.HandlingEndedAt != nil:
			// The provider Turn already ended, so this row no longer owns an
			// effect. The operator's exact Work revision now supplies the terminal
			// disposition that the ended handling did not record itself.
			event.HandledAt = &settledAt
			event.Disposition = terminalDisposition
			event.DispositionSummary = request.Reason
		case event.ClaimedAt == nil && event.DeliveredAt == nil:
			event.DiscardedAt = &settledAt
			event.Resolution = EventResolutionDiscard
			event.ResolvedBy = request.Actor
			event.ResolvedAt = &settledAt
		}
	}
	audit := WorkEvent{
		ID: uuid.NewString(), WorkID: item.ID, Kind: "brain.work_closed",
		DedupeKey:  fmt.Sprintf("brain:work-close:%s:%d", request.Status, item.Revision),
		SourceName: request.Actor, Summary: request.Reason, Actionable: false, CreatedAt: now,
	}
	if _, err := appendWorkEventLocked(&database, itemIndex, audit, false); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	item = database.BrainWork[itemIndex]
	s.mu.Unlock()
	s.broadcastWorkChange(item.ID)
	return item, nil
}

func applyWorkUpdateLocked(database *orchestrationDatabase, index int, update WorkUpdate, now time.Time) (Work, error) {
	if database == nil || index < 0 || index >= len(database.BrainWork) {
		return Work{}, ErrWorkNotFound
	}
	original := database.BrainWork[index]
	item := original
	if eventID, owned := activeHostLaneEvent(*database, item.ID); owned {
		return Work{}, fmt.Errorf("%w: Event %s still owns the Host lane", ErrWorkConflict, eventID)
	}
	wasTerminal := item.Status == WorkDone || item.Status == WorkCancelled
	applyWorkUpdate(&item, update)
	if wasTerminal && item.Status != original.Status {
		return Work{}, fmt.Errorf("%w: terminal Work cannot be reopened", ErrWorkConflict)
	}
	becomesTerminal := !wasTerminal && (item.Status == WorkDone || item.Status == WorkCancelled)
	item.UpdatedAt = now
	item.Revision++
	// SourceThreadID is frozen at Create and never rewritten.
	item.SourceThreadID = original.SourceThreadID
	if becomesTerminal {
		item.TerminalRevision = item.Revision
		item.SessionFinalizations = terminalSessionFinalizations(*database, item, now)
		retirePendingDelegatedSubmissionsForWork(database, item.ID, now)
		item.SuccessorReservation = nil
	}
	if err := validateWork(item); err != nil {
		return Work{}, err
	}
	database.BrainWork[index] = item
	return item, nil
}

// activeHostLaneEvent identifies the sole fail-closed boundary shared by
// operator closure and internal Work updates. Once an actionable Event is
// claimed, neither a metadata producer nor an operator may advance the Work
// revision until the exact Host capability is consumed and disposed (or the
// ended handling is explicitly recovered).
func activeHostLaneEvent(database orchestrationDatabase, workID string) (string, bool) {
	for _, event := range database.BrainWorkEvents {
		if event.WorkID != workID || !event.Actionable || event.HandledAt != nil || event.DiscardedAt != nil ||
			event.Resolution != "" || event.HistoricalDelivery {
			continue
		}
		claimInFlight := event.ClaimedAt != nil && event.DeliveredAt == nil
		handlingInFlight := event.DeliveredAt != nil && event.HandlingEndedAt == nil
		if claimInFlight || handlingInFlight {
			return event.ID, true
		}
	}
	return "", false
}

// ReserveWorkSuccessor binds a newly created correction Session to the exact
// delivered handling before provider mutation. Initial ownership is never
// reserved here: PrepareTurnSubmission persists that owner and its canonical
// pending submission atomically. The reservation survives handling requeue and
// restart; only the exact continue disposition may promote it.
func (s *Store) ReserveWorkSuccessor(id, ownerSessionID string) (Work, error) {
	id = strings.TrimSpace(id)
	ownerSessionID = strings.TrimSpace(ownerSessionID)
	if ownerSessionID == "" {
		return Work{}, fmt.Errorf("owner Session is required")
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	var item Work
	if err == nil {
		index := workIndex(database.BrainWork, id)
		if index < 0 {
			err = ErrWorkNotFound
		} else {
			item = database.BrainWork[index]
			if item.Status == WorkDone || item.Status == WorkCancelled {
				err = fmt.Errorf("%w: Work %s is %s", ErrWorkOwnerConflict, item.ID, item.Status)
			} else if executionWorkID := databaseActiveWorkIDForExecutionSession(database, ownerSessionID); executionWorkID != "" && executionWorkID != item.ID {
				err = fmt.Errorf("%w: Session %s already executes Work %s", ErrWorkOwnerConflict, ownerSessionID, executionWorkID)
			} else if handlingIndex := inFlightHandlingEventIndex(database, item.ID); handlingIndex >= 0 {
				handling := database.BrainWorkEvents[handlingIndex]
				reservation := item.SuccessorReservation
				if reservation != nil && strings.TrimSpace(reservation.SessionID) != ownerSessionID {
					err = fmt.Errorf("%w: Work %s already staged successor %s", ErrWorkOwnerConflict, item.ID, reservation.SessionID)
				} else if reservation == nil {
					item.SuccessorReservation = &WorkSuccessorReservation{
						SessionID: ownerSessionID, EventID: handling.ID, HandlingID: handling.HandlingID,
					}
					database.BrainWork[index] = item
					err = s.persistOrchestrationLocked(database)
				}
			} else {
				err = fmt.Errorf("%w: Work %s has no delivered handling for successor %s", ErrWorkOwnerConflict, item.ID, ownerSessionID)
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		return Work{}, err
	}
	s.broadcastWorkChange(item.ID)
	return item, nil
}

func inFlightHandlingEventIndex(database orchestrationDatabase, workID string) int {
	for index, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.DeliveredAt != nil && event.HandledAt == nil &&
			event.HandlingEndedAt == nil && !event.HistoricalDelivery {
			return index
		}
	}
	return -1
}

// RecordSuccessorLaunchFailure owns the successor's post-spawn failure
// lifecycle. A proved non-admission may release the reservation; an ambiguous
// mutation must retain it. In both cases the state transition and actionable
// attention share one orchestration persist.
func (s *Store) RecordSuccessorLaunchFailure(workID, sessionID, failure string, provedNonAdmission bool) (Work, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	failure = strings.TrimSpace(failure)
	if workID == "" || sessionID == "" || failure == "" {
		return Work{}, fmt.Errorf("successor launch failure requires Work, Session, and failure")
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		s.mu.Unlock()
		return Work{}, ErrWorkNotFound
	}
	item := database.BrainWork[index]
	reservation := item.SuccessorReservation
	if reservation == nil || reservation.SessionID != sessionID {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("%w: Work %s does not reserve successor %s", ErrWorkOwnerConflict, workID, sessionID)
	}
	if provedNonAdmission && strings.TrimSpace(reservation.ProviderTurnID) != "" {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("accepted successor Turn cannot be released as non-admitted")
	}
	key := "brain:successor-launch:" + reservation.HandlingID + ":" + sessionID
	eventExists := false
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.DedupeKey == key {
			eventExists = true
			break
		}
	}
	if eventExists && !provedNonAdmission {
		s.mu.Unlock()
		return item, nil
	}
	if provedNonAdmission {
		item.SuccessorReservation = nil
	}
	item.Status = WorkNeedsInput
	item.NextAction = "Resolve the delegated successor launch failure."
	if !provedNonAdmission {
		item.NextAction = "Confirm whether the reserved successor received the prompt; input will not be replayed."
	}
	item.WaitFor = failure
	item.Wake = nil
	item.UpdatedAt = now
	database.BrainWork[index] = item
	if !eventExists {
		event := WorkEvent{
			ID: uuid.NewString(), WorkID: workID, Kind: "brain.successor_launch_failed", DedupeKey: key,
			PayloadRef: "session:" + sessionID, SourceName: sessionID, Summary: failure,
			Actionable: true, CreatedAt: now,
		}
		if _, err := appendWorkEventLocked(&database, index, event, true); err != nil {
			s.mu.Unlock()
			return Work{}, err
		}
	} else {
		item.Revision++
		item.UpdatedAt = now
		database.BrainWork[index] = item
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	item = database.BrainWork[index]
	s.mu.Unlock()
	s.broadcastWorkChange(workID)
	return item, nil
}

func ensureInitialAttentionLocked(database *orchestrationDatabase, itemIndex int, item Work, now time.Time) (Work, error) {
	if database.Migrations.SignalSystemV1At == nil || item.Status == WorkDone || item.Status == WorkCancelled {
		return item, nil
	}
	state := reduceWorkProgressState(*database, item)
	obligation := reduceAttentionObligation(*database, item.ID)
	if !obligation.NeedsRepair && (state.Owned || state.Waiting || obligation.exists()) {
		return item, nil
	}
	if state.Owned && state.Waiting {
		_, err := deriveWorkProgressMode(*database, item)
		return Work{}, err
	}
	if strings.TrimSpace(item.OwnerSessionID) != "" && !state.LiveCanonicalTurn && !state.OwnerAdmission {
		item.OwnerSessionID = ""
		item.OwnerDelegated = false
		item.Revision++
		item.UpdatedAt = now
		database.BrainWork[itemIndex] = item
	}
	event := WorkEvent{
		ID: uuid.NewString(), WorkID: item.ID, Kind: "brain.reconcile_required",
		DedupeKey: "brain:work:" + item.ID + ":initial", SourceName: "brain",
		Summary: "Brain Work requires a durable disposition.", Actionable: true, CreatedAt: now,
	}
	if _, err := appendWorkEventLocked(database, itemIndex, event, false); err != nil {
		return Work{}, err
	}
	return database.BrainWork[itemIndex], nil
}

func settleUndeliveredAttentionForAdmission(database *orchestrationDatabase, workID string, now time.Time) {
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.WorkID != workID || !attentionEventCanObligate(*database, *event) ||
			event.DeliveredAt != nil {
			continue
		}
		discarded := now.UTC()
		event.DiscardedAt = &discarded
		event.Resolution = EventResolutionDiscard
		event.ResolvedBy = "owner_admission"
		event.ResolvedAt = &discarded
	}
}

func applyWorkUpdate(item *Work, update WorkUpdate) {
	if update.Title != nil {
		item.Title = strings.TrimSpace(*update.Title)
	}
	if update.Objective != nil {
		item.Objective = strings.TrimSpace(*update.Objective)
	}
	if update.Status != nil {
		item.Status = *update.Status
	}
	if update.OwnerSessionID != nil {
		item.OwnerSessionID = strings.TrimSpace(*update.OwnerSessionID)
	}
	if update.CompletionPolicy != nil {
		item.CompletionPolicy = *update.CompletionPolicy
	}
	if update.DoneCriteriaRef != nil {
		item.DoneCriteriaRef = strings.TrimSpace(*update.DoneCriteriaRef)
	}
	if update.NextAction != nil {
		item.NextAction = strings.TrimSpace(*update.NextAction)
	}
	if update.WaitFor != nil {
		item.WaitFor = strings.TrimSpace(*update.WaitFor)
	}
	if update.Wake != nil {
		item.Wake = cloneWorkWake(*update.Wake)
	}
	if update.ContextRef != nil {
		item.ContextRef = strings.TrimSpace(*update.ContextRef)
	}
}

func workIndex(items []Work, id string) int {
	for index := range items {
		if items[index].ID == id {
			return index
		}
	}
	return -1
}

func (s *Store) WorkByOwnerSession(sessionID string) (Work, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return Work{}, false, err
	}
	for _, item := range database.BrainWork {
		if item.OwnerSessionID == sessionID && item.Status != WorkDone && item.Status != WorkCancelled {
			return item, true, nil
		}
	}
	// A canonical attention transition relinquishes progress ownership but
	// retains the exact Turn as lifecycle/finalization evidence. Preserve this
	// lookup contract for callers reconciling that Session without restoring
	// OwnerSessionID or treating it as a second progress owner.
	if turn, found := currentTurnForSession(database, sessionID); found {
		if index := workIndex(database.BrainWork, turn.WorkID); index >= 0 {
			item := database.BrainWork[index]
			state := reduceWorkProgressState(database, item)
			if strings.TrimSpace(item.OwnerSessionID) == "" &&
				item.Status != WorkDone && item.Status != WorkCancelled &&
				(state.Ready || state.Waiting) {
				return item, true, nil
			}
		}
	}
	return Work{}, false, nil
}

func databaseActiveWorkIDForExecutionSession(database orchestrationDatabase, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	for _, item := range database.BrainWork {
		if item.SuccessorReservation != nil && item.SuccessorReservation.SessionID == sessionID &&
			item.Status != WorkDone && item.Status != WorkCancelled {
			return item.ID
		}
		if item.OwnerSessionID == sessionID && item.Status != WorkDone && item.Status != WorkCancelled &&
			(workHasLiveCanonicalOwnerTurn(database, item) || workHasPendingOwnerAdmission(database, item)) {
			return item.ID
		}
	}
	return ""
}

// MigrateDelegatedSessionsV1 performs the only legacy ownership adoption.
// Call it once, after Watcher has produced its first authoritative inventory.
// New delegated Sessions are created with Work directly and never use this
// migration or a runtime fallback.
func (s *Store) MigrateDelegatedSessionsV1(sessions []Work) (bool, error) {
	defaultThreadID, err := s.ChatThreadID()
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return false, err
	}
	if database.Migrations.DelegatedSessionsV1At != nil {
		s.mu.Unlock()
		return false, nil
	}
	now := s.nowUTC()
	changedIDs := []string{}
	for _, candidate := range sessions {
		if strings.TrimSpace(candidate.OwnerSessionID) == "" {
			continue
		}
		owned := false
		for _, current := range database.BrainWork {
			if current.OwnerSessionID == strings.TrimSpace(candidate.OwnerSessionID) {
				owned = true
				break
			}
		}
		if owned {
			continue
		}
		candidate.ID = legacySessionWorkID(candidate.OwnerSessionID)
		candidate.OwnerDelegated = true
		if strings.TrimSpace(candidate.SourceThreadID) == "" {
			candidate.SourceThreadID = defaultThreadID
		}
		candidate, err = normalizeWorkForCreate(candidate, now)
		if err != nil {
			s.mu.Unlock()
			return false, err
		}
		database.BrainWork = append(database.BrainWork, candidate)
		changedIDs = append(changedIDs, candidate.ID)
	}
	database.Migrations.DelegatedSessionsV1At = &now
	err = s.persistOrchestrationLocked(database)
	s.mu.Unlock()
	if err != nil {
		return false, err
	}
	for _, id := range changedIDs {
		s.broadcastWorkChange(id)
	}
	return true, nil
}

// MigrateSignalSystemV1 performs the bounded semantic half of schema v7. Old
// consumed events are already decoded as historical delivery facts; each call
// visits at most limit legacy Work rows and gives every silent nonterminal row
// exactly one reconcile-required attention. The durable cursor makes a crash
// resume at the next row without duplicating attention.
func (s *Store) MigrateSignalSystemV1(limit int) (complete bool, processed int, err error) {
	if limit <= 0 {
		return false, 0, fmt.Errorf("signal migration batch limit must be positive")
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return false, 0, err
	}
	if database.Migrations.SignalSystemV1At != nil {
		s.mu.Unlock()
		return true, 0, nil
	}
	if database.Migrations.SignalSystemV1StartedAt == nil {
		started := now.UTC()
		database.Migrations.SignalSystemV1StartedAt = &started
	}
	start := 0
	if cursor := strings.TrimSpace(database.Migrations.SignalSystemV1Cursor); cursor != "" {
		for index, item := range database.BrainWork {
			if item.ID == cursor {
				start = index + 1
				break
			}
		}
	}
	changedIDs := []string{}
	for index := start; index < len(database.BrainWork) && processed < limit; index++ {
		item := database.BrainWork[index]
		revisionBumped := false
		if item.Status != WorkDone && item.Status != WorkCancelled {
			state := reduceWorkProgressState(database, item)
			// A legacy owner link has execution authority only when its exact
			// current canonical non-Host Turn is still nonterminal. Missing and
			// immutable Turns make the link historical, so retire it before
			// deriving the single durable progress mode.
			if strings.TrimSpace(item.OwnerSessionID) != "" && !state.LiveCanonicalTurn && !state.OwnerAdmission {
				item.OwnerSessionID = ""
				item.OwnerDelegated = false
				item.Revision++
				item.UpdatedAt = now
				database.BrainWork[index] = item
				revisionBumped = true
				changedIDs = append(changedIDs, item.ID)
				state = reduceWorkProgressState(database, item)
			}
			obligation := reduceAttentionObligation(database, item.ID)
			if state.Owned && state.Waiting {
				s.mu.Unlock()
				_, stateErr := deriveWorkProgressMode(database, item)
				return false, processed, stateErr
			}
			if obligation.NeedsRepair || !state.Owned && !state.Waiting && !obligation.exists() {
				dedupeKey := "brain:migration:signal-system-v1:" + item.ID
				exists := false
				for _, current := range database.BrainWorkEvents {
					if current.WorkID == item.ID && current.DedupeKey == dedupeKey {
						exists = true
						break
					}
				}
				if !exists {
					event := WorkEvent{
						ID: uuid.NewString(), WorkID: item.ID, Kind: "brain.reconcile_required",
						DedupeKey: dedupeKey, PayloadRef: "work:" + item.ID, SourceName: "brain",
						Summary:    "Legacy nonterminal Work requires a typed disposition.",
						Actionable: true, CreatedAt: now,
					}
					if _, appendErr := appendWorkEventLocked(&database, index, event, !revisionBumped); appendErr != nil {
						s.mu.Unlock()
						return false, processed, appendErr
					}
					changedIDs = append(changedIDs, item.ID)
				}
			}
		}
		database.Migrations.SignalSystemV1Cursor = item.ID
		processed++
	}
	if processed == 0 {
		completedAt := now.UTC()
		database.Migrations.SignalSystemV1At = &completedAt
		complete = true
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return false, processed, err
	}
	s.mu.Unlock()
	for _, workID := range changedIDs {
		s.broadcastWorkChange(workID)
	}
	return complete, processed, nil
}

func (s *Store) WorkByContextRef(contextRef string) (Work, bool, error) {
	contextRef = strings.TrimSpace(contextRef)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return Work{}, false, err
	}
	for _, item := range database.BrainWork {
		if item.ContextRef == contextRef {
			return item, true, nil
		}
	}
	return Work{}, false, nil
}

func normalizeBrainInputAdmission(admission BrainInputAdmission, now time.Time) (BrainInputAdmission, error) {
	admission.RequestID = strings.TrimSpace(admission.RequestID)
	admission.ThreadID = strings.TrimSpace(admission.ThreadID)
	admission.HostSessionID = strings.TrimSpace(admission.HostSessionID)
	admission.HostGeneration = strings.TrimSpace(admission.HostGeneration)
	admission.HostTurnID = strings.TrimSpace(admission.HostTurnID)
	admission.ProviderActivityID = strings.TrimSpace(admission.ProviderActivityID)
	if admission.HostGeneration != "" && admission.HostTurnID == "" {
		admission.HostTurnID = admission.HostSessionID + ":foreground:" + uuid.NewString()
	}
	admission.SessionID = strings.TrimSpace(admission.SessionID)
	admission.DisplayBody = strings.TrimSpace(admission.DisplayBody)
	admission.BodySHA256 = AdmissionDigest(admission.DisplayBody)
	admission.State = BrainInputAdmissionPending
	admission.AcceptedAt = nil
	admission.SettledAt = nil
	if admission.CreatedAt.IsZero() {
		admission.CreatedAt = now.UTC()
	} else {
		admission.CreatedAt = admission.CreatedAt.UTC()
	}
	if err := validateBrainInputAdmissions([]BrainInputAdmission{admission}); err != nil {
		return BrainInputAdmission{}, err
	}
	return admission, nil
}

func brainInputAdmissionIndex(admissions []BrainInputAdmission, requestID, threadID string) int {
	requestID = strings.TrimSpace(requestID)
	threadID = strings.TrimSpace(threadID)
	for index := range admissions {
		if admissions[index].RequestID == requestID && admissions[index].ThreadID == threadID {
			return index
		}
	}
	return -1
}

func sameBrainInputAdmission(left, right BrainInputAdmission) bool {
	return left.RequestID == right.RequestID && left.ThreadID == right.ThreadID &&
		left.HostSessionID == right.HostSessionID && left.SessionID == right.SessionID &&
		left.HostGeneration == right.HostGeneration && left.DisplayBody == right.DisplayBody &&
		left.BodySHA256 == right.BodySHA256
}

func samePersistedBrainInputAdmission(left, right BrainInputAdmission) bool {
	if !sameBrainInputAdmission(left, right) || left.HostTurnID != right.HostTurnID ||
		left.ProviderActivityID != right.ProviderActivityID || left.State != right.State ||
		!left.CreatedAt.Equal(right.CreatedAt) {
		return false
	}
	if (left.AcceptedAt == nil) != (right.AcceptedAt == nil) ||
		(left.SettledAt == nil) != (right.SettledAt == nil) {
		return false
	}
	if left.AcceptedAt != nil && !left.AcceptedAt.Equal(*right.AcceptedAt) {
		return false
	}
	return left.SettledAt == nil || left.SettledAt.Equal(*right.SettledAt)
}

func samePreparedBrainInputAdmission(left, right BrainInputAdmission) bool {
	return sameBrainInputAdmission(left, right) && left.HostTurnID == right.HostTurnID &&
		left.CreatedAt.Equal(right.CreatedAt)
}

// PrepareBrainInputAdmission persists the exact no-replay intent before the
// provider mutation boundary. Duplicate request/thread identities are returned
// without another write and must never cause another provider submission.
func (s *Store) PrepareBrainInputAdmission(candidate BrainInputAdmission) (BrainInputAdmission, bool, error) {
	var err error
	candidate, err = normalizeBrainInputAdmission(candidate, s.nowUTC())
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	if index := brainInputAdmissionIndex(database.BrainInputAdmissions, candidate.RequestID, candidate.ThreadID); index >= 0 {
		existing := database.BrainInputAdmissions[index]
		if !sameBrainInputAdmission(existing, candidate) {
			return BrainInputAdmission{}, false, fmt.Errorf("Brain input admission identity belongs to different input")
		}
		return existing, false, nil
	}
	if candidate.HostGeneration != "" {
		if active := database.HostForegroundTurn; active != nil &&
			active.HostSessionID == candidate.HostSessionID && active.HostGeneration == candidate.HostGeneration {
			candidate.HostTurnID = active.HostTurnID
		} else {
			candidate.HostTurnID = candidate.HostSessionID + ":foreground:" + uuid.NewString()
		}
	}
	database.BrainInputAdmissions = append(database.BrainInputAdmissions, candidate)
	if err := s.persistOrchestrationLocked(database); err != nil {
		return BrainInputAdmission{}, false, err
	}
	return candidate, true, nil
}

// AcceptBrainInputAdmission commits provider acceptance and all matching
// user_input Attentions in one orchestration replacement. If an older direct
// caller has no prepared row, the accepted row is still created atomically
// with its Attentions; the live server always prepares before mutation.
func (s *Store) AcceptBrainInputAdmission(candidate BrainInputAdmission) (BrainInputAdmission, []WorkEvent, bool, error) {
	now := s.nowUTC()
	var err error
	candidate, err = normalizeBrainInputAdmission(candidate, now)
	if err != nil {
		return BrainInputAdmission{}, nil, false, err
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return BrainInputAdmission{}, nil, false, err
	}
	index := brainInputAdmissionIndex(database.BrainInputAdmissions, candidate.RequestID, candidate.ThreadID)
	if index < 0 {
		database.BrainInputAdmissions = append(database.BrainInputAdmissions, candidate)
		index = len(database.BrainInputAdmissions) - 1
	} else if !sameBrainInputAdmission(database.BrainInputAdmissions[index], candidate) {
		s.mu.Unlock()
		return BrainInputAdmission{}, nil, false, fmt.Errorf("Brain input admission identity belongs to different input")
	} else if database.BrainInputAdmissions[index].State == BrainInputAdmissionAccepted {
		existing := database.BrainInputAdmissions[index]
		s.mu.Unlock()
		return existing, nil, false, nil
	} else if database.BrainInputAdmissions[index].State != BrainInputAdmissionPending {
		existing := database.BrainInputAdmissions[index]
		s.mu.Unlock()
		return BrainInputAdmission{}, nil, false, fmt.Errorf("Brain input admission is terminal with state %q", existing.State)
	}
	acceptedAt := now.UTC()
	prepared := database.BrainInputAdmissions[index]
	candidate.CreatedAt = prepared.CreatedAt
	candidate.State = BrainInputAdmissionAccepted
	candidate.AcceptedAt = &acceptedAt
	candidate.SettledAt = nil
	if candidate.HostGeneration != "" {
		turnID := prepared.HostTurnID
		if active := database.HostForegroundTurn; active != nil {
			if active.HostSessionID != candidate.HostSessionID || active.HostGeneration != candidate.HostGeneration {
				s.mu.Unlock()
				return BrainInputAdmission{}, nil, false, fmt.Errorf("foreground Host generation changed before input acceptance")
			}
			turnID = active.HostTurnID
			if candidate.ProviderActivityID != "" && active.ProviderActivityID != "" &&
				candidate.ProviderActivityID != active.ProviderActivityID {
				s.mu.Unlock()
				return BrainInputAdmission{}, nil, false, fmt.Errorf("foreground Host provider turn changed before its terminal boundary")
			}
		} else {
			database.HostForegroundTurn = &HostForegroundTurn{
				HostSessionID: candidate.HostSessionID, HostGeneration: candidate.HostGeneration,
				HostTurnID: turnID, ProviderActivityID: candidate.ProviderActivityID, StartedAt: acceptedAt,
			}
		}
		if database.HostForegroundTurn.ProviderActivityID == "" {
			database.HostForegroundTurn.ProviderActivityID = candidate.ProviderActivityID
		}
		candidate.HostTurnID = turnID
	}
	database.BrainInputAdmissions[index] = candidate
	woken, changedIDs, err := wakeWaitingWorkLocked(
		&database,
		WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + candidate.ThreadID},
		"user.input",
		candidate.RequestID,
		"User input arrived on the waiting Brain thread.",
		now,
	)
	if err != nil {
		s.mu.Unlock()
		return BrainInputAdmission{}, nil, false, err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return BrainInputAdmission{}, nil, false, err
	}
	s.mu.Unlock()
	for _, workID := range changedIDs {
		s.broadcastWorkChange(workID)
	}
	return candidate, woken, true, nil
}

// AbortBrainInputAdmission terminalizes only a pending intent after the
// provider owner proved mutation did not begin. Terminal admission identity is
// retained so a duplicate request cannot cross the mutation boundary again.
func (s *Store) AbortBrainInputAdmission(requestID, threadID string) error {
	requestID = strings.TrimSpace(requestID)
	threadID = strings.TrimSpace(threadID)
	if requestID == "" || threadID == "" {
		return fmt.Errorf("Brain input admission request and thread are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	index := brainInputAdmissionIndex(database.BrainInputAdmissions, requestID, threadID)
	if index < 0 {
		return nil
	}
	if database.BrainInputAdmissions[index].State == BrainInputAdmissionAccepted {
		return fmt.Errorf("accepted Brain input admission cannot be aborted")
	}
	if database.BrainInputAdmissions[index].State != BrainInputAdmissionPending {
		return nil
	}
	settledAt := s.nowUTC()
	database.BrainInputAdmissions[index].State = BrainInputAdmissionNotSubmitted
	database.BrainInputAdmissions[index].SettledAt = &settledAt
	return s.persistOrchestrationLocked(database)
}

// PendingBrainInputAdmissions returns the durable user-input intents whose
// provider mutation outcome still needs exact receipt reconciliation.
func (s *Store) PendingBrainInputAdmissions() ([]BrainInputAdmission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	out := make([]BrainInputAdmission, 0)
	for _, admission := range database.BrainInputAdmissions {
		if admission.State == BrainInputAdmissionPending {
			out = append(out, admission)
		}
	}
	return out, nil
}

// SettleBrainInputAdmission applies one exact terminal receipt outcome. Only a
// pending row may move; accepted/not_submitted/uncertain are monotonic. When an
// old Host identity is already superseded, accepted evidence remains history
// and does not recreate its foreground gate.
func (s *Store) SettleBrainInputAdmission(
	requestID, threadID string,
	state BrainInputAdmissionState,
	createForeground bool,
) (BrainInputAdmission, bool, error) {
	requestID = strings.TrimSpace(requestID)
	threadID = strings.TrimSpace(threadID)
	if requestID == "" || threadID == "" {
		return BrainInputAdmission{}, false, fmt.Errorf("Brain input admission request and thread are required")
	}
	if state != BrainInputAdmissionAccepted && state != BrainInputAdmissionNotSubmitted && state != BrainInputAdmissionUncertain {
		return BrainInputAdmission{}, false, fmt.Errorf("invalid Brain input settlement %q", state)
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return BrainInputAdmission{}, false, err
	}
	index := brainInputAdmissionIndex(database.BrainInputAdmissions, requestID, threadID)
	if index < 0 {
		s.mu.Unlock()
		return BrainInputAdmission{}, false, nil
	}
	admission := database.BrainInputAdmissions[index]
	if admission.State != BrainInputAdmissionPending {
		s.mu.Unlock()
		return admission, false, nil
	}
	changedWorkIDs := []string{}
	if state == BrainInputAdmissionAccepted {
		acceptedAt := now.UTC()
		admission.State = state
		admission.AcceptedAt = &acceptedAt
		admission.SettledAt = nil
		if createForeground && admission.HostGeneration != "" {
			if active := database.HostForegroundTurn; active != nil {
				if active.HostSessionID != admission.HostSessionID || active.HostGeneration != admission.HostGeneration {
					s.mu.Unlock()
					return BrainInputAdmission{}, false, fmt.Errorf("foreground Host generation changed before recovered input acceptance")
				}
				admission.HostTurnID = active.HostTurnID
			} else {
				database.HostForegroundTurn = &HostForegroundTurn{
					HostSessionID:      admission.HostSessionID,
					HostGeneration:     admission.HostGeneration,
					HostTurnID:         admission.HostTurnID,
					ProviderActivityID: admission.ProviderActivityID,
					StartedAt:          acceptedAt,
				}
			}
		}
		_, changedWorkIDs, err = wakeWaitingWorkLocked(
			&database,
			WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + admission.ThreadID},
			"user.input",
			admission.RequestID,
			"User input arrived on the waiting Brain thread.",
			now,
		)
		if err != nil {
			s.mu.Unlock()
			return BrainInputAdmission{}, false, err
		}
	} else {
		settledAt := now.UTC()
		admission.State = state
		admission.AcceptedAt = nil
		admission.SettledAt = &settledAt
		if state == BrainInputAdmissionUncertain {
			_, err = s.appendTimelineItemLocked(TimelineItem{
				ID:        "brain-input-uncertain:" + admission.RequestID,
				ThreadID:  admission.ThreadID,
				SessionID: admission.SessionID,
				Role:      "assistant",
				Kind:      timelineKindAssistantMessage,
				Title:     "Input delivery uncertain",
				Body:      "Zen could not prove whether this message reached the previous Host. It was not replayed automatically.",
				CreatedAt: settledAt,
			})
			if err != nil {
				s.mu.Unlock()
				return BrainInputAdmission{}, false, err
			}
		}
	}
	database.BrainInputAdmissions[index] = admission
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return BrainInputAdmission{}, false, err
	}
	s.mu.Unlock()
	for _, workID := range changedWorkIDs {
		s.broadcastWorkChange(workID)
	}
	return admission, true, nil
}

func (s *Store) BrainInputAdmission(requestID, threadID string) (BrainInputAdmission, bool, error) {
	requestID = strings.TrimSpace(requestID)
	threadID = strings.TrimSpace(threadID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	index := brainInputAdmissionIndex(database.BrainInputAdmissions, requestID, threadID)
	if index < 0 {
		return BrainInputAdmission{}, false, nil
	}
	return database.BrainInputAdmissions[index], true, nil
}

// UnprojectedBrainInputAdmissions returns a bounded restart batch. Projection
// membership is derived from the idempotent timeline identity plus any exact
// provider echo still stranded inside the admission's causal window, so no
// second durable acknowledgement field or repair queue is needed.
func (s *Store) UnprojectedBrainInputAdmissions(limit int) ([]BrainInputAdmission, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("positive Brain input admission limit required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, false, err
	}
	if err := s.ensureAdmissionProvenanceLocked(); err != nil {
		return nil, false, formatAdmissionProvenanceError(err)
	}
	allItems, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return nil, false, err
	}
	out := make([]BrainInputAdmission, 0, limit)
	for _, admission := range database.BrainInputAdmissions {
		if admission.State != BrainInputAdmissionAccepted {
			continue
		}
		canonicalIndex, candidates, err := brainInputAdmissionProjectionState(allItems, admission)
		if err != nil {
			return nil, false, err
		}
		if canonicalIndex >= 0 &&
			(strings.TrimSpace(allItems[canonicalIndex].AdmissionEchoEventID) != "" || len(candidates) == 0) {
			continue
		}
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, admission)
	}
	return out, false, nil
}

func (s *Store) AppendWorkEvent(event WorkEvent) (WorkEvent, bool, error) {
	var err error
	event, err = normalizeWorkEventForAppend(event, s.nowUTC())
	if err != nil {
		return WorkEvent{}, false, err
	}

	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	itemIndex := -1
	if err == nil {
		itemIndex = workIndex(database.BrainWork, event.WorkID)
	}
	if err == nil && itemIndex < 0 {
		err = ErrWorkNotFound
	}
	if err == nil {
		for _, current := range database.BrainWorkEvents {
			if current.WorkID == event.WorkID && current.DedupeKey == event.DedupeKey {
				s.mu.Unlock()
				return current, false, nil
			}
		}
		item := database.BrainWork[itemIndex]
		if event.Actionable && (item.Wake != nil || reduceWorkProgressState(database, item).Owned) {
			// Generic append is an audit/attention operation, not a producer or
			// owner-transition authority. It cannot clear a typed wait or place
			// ready attention over an existing execution owner.
			event.Actionable = false
		}
		event, err = appendWorkEventLocked(&database, itemIndex, event, true)
		if err == nil {
			err = s.persistOrchestrationLocked(database)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(event.WorkID)
	return event, true, nil
}

func normalizeWorkEventForAppend(event WorkEvent, now time.Time) (WorkEvent, error) {
	event.ID = strings.TrimSpace(event.ID)
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	event.WorkID = strings.TrimSpace(event.WorkID)
	event.Kind = strings.TrimSpace(event.Kind)
	event.DedupeKey = strings.TrimSpace(event.DedupeKey)
	event.PayloadRef = strings.TrimSpace(event.PayloadRef)
	event.SourceName = strings.TrimSpace(event.SourceName)
	event.Summary = strings.TrimSpace(event.Summary)
	event.CreatedAt = now.UTC()
	event.Sequence = 0
	event.WorkRevision = 0
	event.ClaimedAt = nil
	event.DeliveryHostSessionID = ""
	event.HandlingID = ""
	event.ProviderTurnID = ""
	event.DeliveryWorkRevision = 0
	event.DeliverySequenceFence = 0
	event.DeliveredAt = nil
	event.HandlingEndedAt = nil
	event.HandledAt = nil
	event.Disposition = ""
	event.DispositionSummary = ""
	event.CoalescedInto = ""
	event.HistoricalDelivery = false
	if isSessionLifecycleKind(event.Kind) && !isTurnScopedSessionDedupeKey(event.DedupeKey) {
		// A delegated lifecycle Event is unrepresentable without the
		// canonical current TurnID: the dedupe key must be turn-scoped
		// (session:<sid>:turn:<tid>:<kind>). Raw-state routing and
		// occurrence-counting keys are deleted.
		return WorkEvent{}, fmt.Errorf("delegated lifecycle event %q requires a canonical turn-scoped dedupe key", event.Kind)
	}
	return event, nil
}

// ApplyProducerTransition ensures or loads a deterministic producer Work and
// commits its state change, canonical event, and every exact typed consumer
// wake in one orchestration replacement. The producer dedupe key is the
// transaction identity: once present, the full transition is a replay no-op.
func (s *Store) ApplyProducerTransition(
	candidate *Work,
	update *WorkUpdate,
	event WorkEvent,
	wake *WorkWake,
	occurrenceID string,
) (Work, WorkEvent, bool, []WorkEvent, error) {
	now := s.nowUTC()
	var err error
	event, err = normalizeWorkEventForAppend(event, now)
	if err != nil {
		return Work{}, WorkEvent{}, false, nil, err
	}
	var normalizedCandidate Work
	if candidate != nil {
		normalizedCandidate, err = s.resolveSourceThreadID(*candidate)
		if err != nil {
			return Work{}, WorkEvent{}, false, nil, err
		}
		normalizedCandidate, err = normalizeWorkForCreate(normalizedCandidate, now)
		if err != nil {
			return Work{}, WorkEvent{}, false, nil, err
		}
		if normalizedCandidate.ID != event.WorkID {
			return Work{}, WorkEvent{}, false, nil, fmt.Errorf("producer candidate work_id does not match event")
		}
	}
	occurrenceID = strings.TrimSpace(occurrenceID)
	if wake != nil {
		wake = cloneWorkWake(wake)
		if err := validateWorkWake(wake); err != nil {
			return Work{}, WorkEvent{}, false, nil, err
		}
		if wake.Kind == WorkWakeSessionTerminal {
			return Work{}, WorkEvent{}, false, nil, fmt.Errorf("session_terminal producer authority belongs to the canonical Turn reducer")
		}
		if occurrenceID == "" {
			return Work{}, WorkEvent{}, false, nil, fmt.Errorf("producer wake requires occurrence identity")
		}
	}

	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, false, nil, err
	}
	itemIndex := workIndex(database.BrainWork, event.WorkID)
	if itemIndex < 0 {
		if candidate == nil {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, false, nil, ErrWorkNotFound
		}
		database.BrainWork = append(database.BrainWork, normalizedCandidate)
		itemIndex = len(database.BrainWork) - 1
	} else if candidate != nil && strings.TrimSpace(database.BrainWork[itemIndex].ContextRef) != strings.TrimSpace(normalizedCandidate.ContextRef) {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, false, nil, fmt.Errorf("%w: producer Work context does not match deterministic candidate", ErrWorkConflict)
	}
	for _, current := range database.BrainWorkEvents {
		if current.WorkID == event.WorkID && current.DedupeKey == event.DedupeKey {
			item := database.BrainWork[itemIndex]
			s.mu.Unlock()
			return item, current, false, nil, nil
		}
	}

	item := database.BrainWork[itemIndex]
	if update != nil {
		item, err = applyWorkUpdateLocked(&database, itemIndex, *update, now)
		if err != nil {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, false, nil, err
		}
	}
	event, err = appendWorkEventLocked(&database, itemIndex, event, update == nil)
	if err != nil {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, false, nil, err
	}
	woken := []WorkEvent{}
	changedIDs := []string{event.WorkID}
	if wake != nil {
		var wakeChanged []string
		wakeSummary := event.Summary
		if wakeSummary == "" {
			wakeSummary = event.Kind
		}
		woken, wakeChanged, err = wakeWaitingWorkLocked(
			&database, *wake, event.Kind, occurrenceID, wakeSummary, now,
		)
		if err != nil {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, false, nil, err
		}
		changedIDs = append(changedIDs, wakeChanged...)
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, false, nil, err
	}
	item = database.BrainWork[itemIndex]
	s.mu.Unlock()
	seen := map[string]struct{}{}
	for _, workID := range changedIDs {
		if _, exists := seen[workID]; exists {
			continue
		}
		seen[workID] = struct{}{}
		s.broadcastWorkChange(workID)
	}
	return item, event, true, woken, nil
}

func appendWorkEventLocked(database *orchestrationDatabase, itemIndex int, event WorkEvent, bumpRevision bool) (WorkEvent, error) {
	return appendWorkEventWithCoalescingLocked(database, itemIndex, event, bumpRevision, true)
}

// appendWorkEventAsAttentionHeadLocked is reserved for transitions that have
// already redirected every prior obligation to this new Event ID. The normal
// append path cannot discover that forward reference before the row exists,
// so allowing it to coalesce would manufacture a two-row cycle and lose the
// Work's schedulable head.
func appendWorkEventAsAttentionHeadLocked(database *orchestrationDatabase, itemIndex int, event WorkEvent, bumpRevision bool) (WorkEvent, error) {
	return appendWorkEventWithCoalescingLocked(database, itemIndex, event, bumpRevision, false)
}

func appendWorkEventWithCoalescingLocked(
	database *orchestrationDatabase,
	itemIndex int,
	event WorkEvent,
	bumpRevision bool,
	autoCoalesce bool,
) (WorkEvent, error) {
	if database == nil || itemIndex < 0 || itemIndex >= len(database.BrainWork) {
		return WorkEvent{}, ErrWorkNotFound
	}
	item := &database.BrainWork[itemIndex]
	if bumpRevision {
		item.Revision++
		item.UpdatedAt = event.CreatedAt.UTC()
	}
	database.NextEventSequence++
	event.Sequence = database.NextEventSequence
	event.WorkRevision = item.Revision
	if autoCoalesce && event.Actionable && strings.TrimSpace(event.CoalescedInto) == "" {
		if obligation := reduceAttentionObligation(*database, event.WorkID); obligation.exists() {
			event.CoalescedInto = database.BrainWorkEvents[obligation.HeadIndex].ID
		}
	}
	if err := validateWorkEvent(event); err != nil {
		return WorkEvent{}, err
	}
	database.BrainWorkEvents = append(database.BrainWorkEvents, event)
	return event, nil
}

func readyAttentionEventID(database orchestrationDatabase, workID string) string {
	obligation := reduceAttentionObligation(database, workID)
	if obligation.exists() && obligation.State == deliveryPending {
		return database.BrainWorkEvents[obligation.HeadIndex].ID
	}
	return ""
}

func wakeWaitingWorkLocked(database *orchestrationDatabase, wake WorkWake, kind, occurrenceID, summary string, now time.Time) ([]WorkEvent, []string, error) {
	recorded := []WorkEvent{}
	changedIDs := []string{}
	for index := range database.BrainWork {
		item := database.BrainWork[index]
		if !workWakeEqual(item.Wake, &wake) {
			continue
		}
		dedupeKey := fmt.Sprintf("wake:%s:%s:%s", wake.Kind, wake.Ref, occurrenceID)
		exists := false
		for _, current := range database.BrainWorkEvents {
			if current.WorkID == item.ID && current.DedupeKey == dedupeKey {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		event := WorkEvent{
			ID: uuid.NewString(), WorkID: item.ID, Kind: kind, DedupeKey: dedupeKey,
			PayloadRef: wake.Ref, SourceName: wake.Ref, Summary: summary,
			Actionable: true, CreatedAt: now,
		}
		// Only provenance-bearing producer transactions call this helper.
		// Clearing the wait before append makes wake satisfaction and ready
		// attention one atomic database replacement without trusting strings in
		// the generic append path.
		item.Wake = nil
		database.BrainWork[index] = item
		var err error
		event, err = appendWorkEventLocked(database, index, event, true)
		if err != nil {
			return nil, nil, err
		}
		recorded = append(recorded, event)
		changedIDs = append(changedIDs, item.ID)
	}
	return recorded, changedIDs, nil
}

func (s *Store) ListWorkEvents(workID string) ([]WorkEvent, error) {
	workID = strings.TrimSpace(workID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	out := []WorkEvent{}
	for _, event := range database.BrainWorkEvents {
		if workID == "" || event.WorkID == workID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (s *Store) ClaimNextActionableEvent(hostSessionID string) (WorkEvent, bool, error) {
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" {
		return WorkEvent{}, false, fmt.Errorf("delivery host Session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return WorkEvent{}, false, err
	}
	// The Host lane owns at most one live delivered handling. A delivered
	// Event awaiting its typed disposition stops every further claim; the
	// same gate lives in the lane reducer and is rechecked here for direct
	// callers.
	for _, event := range database.BrainWorkEvents {
		if event.DeliveredAt != nil && event.HandledAt == nil && event.HandlingEndedAt == nil && !event.HistoricalDelivery {
			return WorkEvent{}, false, nil
		}
	}
	// Fair selection at the boundary: the oldest pending head across Work
	// keys. No counter and no last-admitted mirror are persisted; FIFO by
	// append sequence is the whole fairness contract.
	index := oldestPendingAttentionHeadIndex(database)
	if index < 0 {
		return WorkEvent{}, false, nil
	}
	now := s.nowUTC()
	database.BrainWorkEvents[index].ClaimedAt = &now
	database.BrainWorkEvents[index].DeliveryHostSessionID = hostSessionID
	database.BrainWorkEvents[index].HandlingID = uuid.NewString()
	database.BrainWorkEvents[index].ProviderTurnID = hostSessionID + ":turn:" + uuid.NewString()
	itemIndex := workIndex(database.BrainWork, database.BrainWorkEvents[index].WorkID)
	database.BrainWorkEvents[index].DeliveryWorkRevision = database.BrainWork[itemIndex].Revision
	database.BrainWorkEvents[index].DeliverySequenceFence = database.NextEventSequence
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, false, err
	}
	return authoritativeReviewEvent(database, database.BrainWorkEvents[index]), true, nil
}

// HasLiveDeliveredHandling reports whether one delivered Event still awaits
// its exact typed disposition. The Host lane stops while it is true: the Host
// is mid-review and no new admission may overtake the disposition.
func (s *Store) HasLiveDeliveredHandling() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	for _, event := range database.BrainWorkEvents {
		if event.DeliveredAt != nil && event.HandledAt == nil && event.HandlingEndedAt == nil && !event.HistoricalDelivery {
			return true, nil
		}
	}
	return false, nil
}

// BindHostForegroundActivity persists the exact provider activity identity of
// the accepted foreground Host turn once it is first observed running. The
// binding is the identity anchor for later strong exact terminal evidence; it
// is never overwritten and never inferred from ambient state.
func (s *Store) BindHostForegroundActivity(hostSessionID, hostGeneration, hostTurnID, providerActivityID string) error {
	hostSessionID = strings.TrimSpace(hostSessionID)
	hostGeneration = strings.TrimSpace(hostGeneration)
	hostTurnID = strings.TrimSpace(hostTurnID)
	providerActivityID = strings.TrimSpace(providerActivityID)
	if hostSessionID == "" || hostGeneration == "" || hostTurnID == "" || providerActivityID == "" {
		return fmt.Errorf("foreground Host identity and provider activity are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	active := database.HostForegroundTurn
	if active == nil {
		return nil
	}
	if active.HostSessionID != hostSessionID || active.HostGeneration != hostGeneration || active.HostTurnID != hostTurnID {
		return fmt.Errorf("foreground Host turn identity changed before activity binding")
	}
	if active.ProviderActivityID == providerActivityID {
		return nil
	}
	if active.ProviderActivityID != "" {
		return fmt.Errorf("foreground Host provider activity is already bound")
	}
	active.ProviderActivityID = providerActivityID
	return s.persistOrchestrationLocked(database)
}

// CurrentHostForegroundTurn returns the one durable foreground response epoch,
// or nil when the Host lane is idle. The reducer uses it to recover the same
// admission boundary after daemon reopen; the record itself never authorizes
// replay and is closed only by strong exact terminal evidence.
func (s *Store) CurrentHostForegroundTurn() (*HostForegroundTurn, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	if database.HostForegroundTurn == nil {
		return nil, nil
	}
	copy := *database.HostForegroundTurn
	return &copy, nil
}

// RetireHostForegroundTurn clears only the exact foreground identity observed
// by the Host-lane reducer after the Host binding, pane, or generation was
// proven superseded. A delayed retirement can never clear a newer turn.
func (s *Store) RetireHostForegroundTurn(expected HostForegroundTurn) (bool, error) {
	expected.HostSessionID = strings.TrimSpace(expected.HostSessionID)
	expected.HostGeneration = strings.TrimSpace(expected.HostGeneration)
	expected.HostTurnID = strings.TrimSpace(expected.HostTurnID)
	expected.ProviderActivityID = strings.TrimSpace(expected.ProviderActivityID)
	if expected.HostSessionID == "" || expected.HostGeneration == "" || expected.HostTurnID == "" {
		return false, fmt.Errorf("foreground Host session, generation, and turn are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	active := database.HostForegroundTurn
	if active == nil {
		return false, nil
	}
	if active.HostSessionID != expected.HostSessionID ||
		active.HostGeneration != expected.HostGeneration ||
		active.HostTurnID != expected.HostTurnID ||
		active.ProviderActivityID != expected.ProviderActivityID ||
		!active.StartedAt.Equal(expected.StartedAt) {
		return false, nil
	}
	database.HostForegroundTurn = nil
	if err := s.persistOrchestrationLocked(database); err != nil {
		return false, err
	}
	return true, nil
}

// PendingHostInputAdmission reports whether any Brain input admission for the
// Host is still pending. Pending is persisted before provider mutation and is
// the durable user-steering gate that replaced the process-local
// foregroundInput flag: while it exists, the Host lane must not admit an
// internal Event ahead of the user's message.
func (s *Store) PendingHostInputAdmission(hostSessionID string) (bool, error) {
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	for _, admission := range database.BrainInputAdmissions {
		if admission.State == BrainInputAdmissionPending && admission.HostSessionID == hostSessionID {
			return true, nil
		}
	}
	return false, nil
}

// CloseHostForegroundTurn atomically closes the exact accepted foreground Host
// turn at its terminal boundary. The caller (the serialized Host-lane reducer)
// must already hold strong exact terminal evidence; this method validates the
// exact durable identity so a second close, a late edge, or a mismatched
// generation can never clear a newer turn.
func (s *Store) CloseHostForegroundTurn(hostSessionID, hostGeneration, hostTurnID, providerActivityID string) error {
	hostSessionID = strings.TrimSpace(hostSessionID)
	hostGeneration = strings.TrimSpace(hostGeneration)
	hostTurnID = strings.TrimSpace(hostTurnID)
	providerActivityID = strings.TrimSpace(providerActivityID)
	if hostSessionID == "" || hostGeneration == "" || hostTurnID == "" {
		return fmt.Errorf("foreground Host session, generation, and turn are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	active := database.HostForegroundTurn
	if active == nil {
		return nil
	}
	if active.HostSessionID != hostSessionID || active.HostGeneration != hostGeneration || active.HostTurnID != hostTurnID ||
		(active.ProviderActivityID != "" && providerActivityID != "" && active.ProviderActivityID != providerActivityID) {
		return fmt.Errorf("foreground Host terminal boundary does not match the durable active turn")
	}
	database.HostForegroundTurn = nil
	return s.persistOrchestrationLocked(database)
}

// oldestPendingAttentionHeadIndex selects the one fair pending Work key at the
// admission boundary: the oldest ready head by append sequence. One head per
// Work key, deterministic, and replay-free after reopen.
func oldestPendingAttentionHeadIndex(database orchestrationDatabase) int {
	candidates := attentionCandidateIndexes(database)
	if len(candidates) == 0 {
		return -1
	}
	return candidates[0]
}

// attentionCandidateIndexes returns at most one ready head per Work key. This
// makes fairness a property of Work, not of how many append-only facts one Work
// accumulated while it waited.
func attentionCandidateIndexes(database orchestrationDatabase) []int {
	obligations := reduceAttentionObligations(database)
	out := make([]int, 0, len(obligations))
	for _, obligation := range obligations {
		if obligation.exists() && obligation.State == deliveryPending {
			out = append(out, obligation.HeadIndex)
		}
	}
	sort.Slice(out, func(left, right int) bool {
		leftEvent := database.BrainWorkEvents[out[left]]
		rightEvent := database.BrainWorkEvents[out[right]]
		if leftEvent.Sequence == rightEvent.Sequence {
			return leftEvent.WorkID < rightEvent.WorkID
		}
		return leftEvent.Sequence < rightEvent.Sequence
	})
	return out
}

func authoritativeReviewEvent(database orchestrationDatabase, claimed WorkEvent) WorkEvent {
	latest := claimed
	for _, candidate := range database.BrainWorkEvents {
		if candidate.WorkID != claimed.WorkID || !attentionEventCanObligate(database, candidate) ||
			candidate.Sequence > claimed.DeliverySequenceFence || candidate.HandledAt != nil ||
			candidate.DiscardedAt != nil || candidate.Resolution != "" || candidate.HistoricalDelivery {
			continue
		}
		if candidate.Sequence > latest.Sequence {
			latest = candidate
		}
	}
	claimed.ReviewKind = latest.Kind
	claimed.ReviewPayloadRef = latest.PayloadRef
	claimed.ReviewSourceName = latest.SourceName
	claimed.ReviewSummary = latest.Summary
	return claimed
}

func (s *Store) ClaimedActionableEvents() ([]WorkEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	out := []WorkEvent{}
	for _, event := range database.BrainWorkEvents {
		if workEventSchedulerEligible(database, event) && event.ClaimedAt != nil {
			out = append(out, event)
		}
	}
	return out, nil
}

// LiveHostHandlings returns every delivered handling that still owns the Host
// effect. The schema admits at most one globally, but the bounded API keeps
// startup reconciliation explicit and safe if persisted state is corrupt.
func (s *Store) LiveHostHandlings(limit int) ([]WorkEvent, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("Host handling batch limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, false, err
	}
	out := make([]WorkEvent, 0, limit)
	more := false
	for _, event := range database.BrainWorkEvents {
		if event.DeliveredAt == nil || event.HandledAt != nil || event.HandlingEndedAt != nil || event.HistoricalDelivery {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		out = append(out, event)
	}
	return out, more, nil
}

func workEventSchedulerEligible(database orchestrationDatabase, event WorkEvent) bool {
	if workEventIndex(database.BrainWorkEvents, event.ID) < 0 {
		return attentionEventCanObligate(database, event) && strings.TrimSpace(event.CoalescedInto) == ""
	}
	obligation := reduceAttentionObligation(database, event.WorkID)
	return obligation.exists() && obligation.State != deliveryDelivered &&
		database.BrainWorkEvents[obligation.HeadIndex].ID == event.ID
}

// ReleaseEventClaim atomically makes the exact identity-bound Event claimable
// again only when Session Input proved that provider mutation never started.
// If canonical Host preparation already persisted the exact five-part pending
// submission, that submission is aborted in this same replacement. A resolved
// provider admission is ambiguous and keeps both authorities held.
func (s *Store) ReleaseEventClaim(
	eventID, claimToken, workID, hostSessionID, providerTurnID string,
) error {
	eventID = strings.TrimSpace(eventID)
	claimToken = strings.TrimSpace(claimToken)
	workID = strings.TrimSpace(workID)
	hostSessionID = strings.TrimSpace(hostSessionID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if eventID == "" || claimToken == "" || workID == "" || hostSessionID == "" || providerTurnID == "" {
		return ErrEventClaim
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.ID != eventID {
			continue
		}
		if !event.Actionable || event.ClaimedAt == nil || event.DeliveredAt != nil ||
			event.HandlingID != claimToken || event.WorkID != workID ||
			event.DeliveryHostSessionID != hostSessionID || event.ProviderTurnID != providerTurnID {
			return ErrEventClaim
		}
		submissionIndex := -1
		for candidate := range database.BrainTurnSubmissions {
			submission := database.BrainTurnSubmissions[candidate]
			exact := submission.Receipt == eventID && submission.ClaimToken == claimToken &&
				submission.WorkID == workID && submission.SessionID == hostSessionID &&
				submission.ProposedTurnID == providerTurnID
			if exact {
				submissionIndex = candidate
				break
			}
		}
		if submissionIndex >= 0 {
			submission := &database.BrainTurnSubmissions[submissionIndex]
			switch submission.State {
			case watcher.TurnSubmissionPending:
				abortedAt := s.nowUTC()
				submission.State = watcher.TurnSubmissionAborted
				submission.AbortedAt = &abortedAt
			case watcher.TurnSubmissionAborted:
				// A pre-mutation live attempt may already have persisted its
				// canonical abort before returning InputNotSubmitted. Releasing
				// the still-claimed Event completes that safe held state.
			case watcher.TurnSubmissionResolved:
				return ErrEventClaim
			case watcher.TurnSubmissionRetired:
				return ErrEventClaim
			default:
				return ErrEventClaim
			}
		}
		event.ClaimedAt = nil
		event.DeliveryHostSessionID = ""
		event.HandlingID = ""
		event.ProviderTurnID = ""
		event.DeliveryWorkRevision = 0
		event.DeliverySequenceFence = 0
		return s.persistOrchestrationLocked(database)
	}
	return ErrEventClaim
}

// ConsumeClaimedWorkEvent atomically consumes the exact Event assigned to the
// Host after canonical provider admission accepts that Event.ID receipt. The
// complete claimed Event capability is the authorization boundary; no Work,
// owner, receipt, or provider Turn lookup may substitute for it.
func (s *Store) ConsumeClaimedWorkEvent(
	eventID, claimToken, workID, hostSessionID, providerTurnID string,
) (WorkEvent, Work, error) {
	eventID = strings.TrimSpace(eventID)
	claimToken = strings.TrimSpace(claimToken)
	workID = strings.TrimSpace(workID)
	hostSessionID = strings.TrimSpace(hostSessionID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if eventID == "" || claimToken == "" || workID == "" || hostSessionID == "" || providerTurnID == "" {
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	claimedWorkID := ""
	var claimed WorkEvent
	var item Work
	if err == nil {
		for index := range database.BrainWorkEvents {
			event := &database.BrainWorkEvents[index]
			if event.ID != eventID {
				continue
			}
			if !event.Actionable || event.ClaimedAt == nil || event.DeliveredAt != nil ||
				event.HandlingID != claimToken || event.WorkID != workID ||
				event.DeliveryHostSessionID != hostSessionID || event.ProviderTurnID != providerTurnID {
				err = ErrEventClaim
				break
			}
			if !databaseHasResolvedHostEventAdmission(
				database, eventID, claimToken, workID, hostSessionID, providerTurnID,
			) {
				err = fmt.Errorf("%w: canonical Host admission is missing", ErrEventClaim)
				break
			}
			claimedWorkID = database.BrainWorkEvents[index].WorkID
			workIndex := -1
			for candidate := range database.BrainWork {
				if database.BrainWork[candidate].ID == claimedWorkID {
					workIndex = candidate
					break
				}
			}
			if workIndex < 0 {
				err = ErrWorkNotFound
				break
			}
			now := s.nowUTC()
			deliveredAt := now.UTC()
			event.DeliveredAt = &deliveredAt
			claimed = *event
			item = database.BrainWork[workIndex]
			err = s.persistOrchestrationLocked(database)
			break
		}
		if claimedWorkID == "" && err == nil {
			err = ErrEventClaim
		}
	}
	s.mu.Unlock()
	if err == nil && claimedWorkID != "" {
		s.broadcastWorkChange(claimedWorkID)
	}
	return claimed, item, err
}

// RequeueUnhandledHostAttention ends only the exact admitted Host handling.
// Wrong, reordered, and duplicate provider terminal identities are no-ops. All
// unresolved signals for the Work coalesce into one fresh attention at the FIFO
// tail, while the delivered bytes remain permanently consumed.
func (s *Store) RequeueUnhandledHostAttention(eventID, handlingID, providerTurnID string) (WorkEvent, bool, error) {
	eventID = strings.TrimSpace(eventID)
	handlingID = strings.TrimSpace(handlingID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if eventID == "" || handlingID == "" || providerTurnID == "" {
		return WorkEvent{}, false, nil
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return WorkEvent{}, false, err
	}
	activeIndex := workEventIndex(database.BrainWorkEvents, eventID)
	if activeIndex < 0 {
		return WorkEvent{}, false, nil
	}
	active := &database.BrainWorkEvents[activeIndex]
	if active.DeliveredAt == nil || active.HandledAt != nil || active.HandlingEndedAt != nil || active.HistoricalDelivery ||
		active.HandlingID != handlingID || active.ProviderTurnID != providerTurnID {
		return WorkEvent{}, false, nil
	}
	dedupeKey := "brain:reconcile:handling:" + active.HandlingID
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == active.WorkID && event.DedupeKey == dedupeKey {
			return event, false, nil
		}
	}
	reconcile := WorkEvent{
		ID: uuid.NewString(), WorkID: active.WorkID, Kind: "brain.reconcile_required",
		DedupeKey: dedupeKey, PayloadRef: "work:" + active.WorkID,
		SourceName: "brain", Summary: "The previous Host turn ended without a durable disposition.",
		Actionable: true, CreatedAt: now,
	}
	endedAt := now.UTC()
	active.HandlingEndedAt = &endedAt
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.WorkID == active.WorkID && event.Actionable && event.HandledAt == nil &&
			event.DiscardedAt == nil && !event.HistoricalDelivery {
			event.CoalescedInto = reconcile.ID
		}
	}
	itemIndex := workIndex(database.BrainWork, active.WorkID)
	if reservation := database.BrainWork[itemIndex].SuccessorReservation; reservation != nil &&
		reservation.EventID == active.ID && reservation.HandlingID == active.HandlingID {
		reservation = cloneSuccessorReservation(reservation)
		reservation.EventID = ""
		reservation.HandlingID = ""
		database.BrainWork[itemIndex].SuccessorReservation = reservation
	}
	reconcile, err = appendWorkEventAsAttentionHeadLocked(&database, itemIndex, reconcile, true)
	if err != nil {
		return WorkEvent{}, false, err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(active.WorkID)
	return reconcile, true, nil
}

func (s *Store) WorkEvent(eventID string) (WorkEvent, bool, error) {
	eventID = strings.TrimSpace(eventID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return WorkEvent{}, false, err
	}
	index := workEventIndex(database.BrainWorkEvents, eventID)
	if index < 0 {
		return WorkEvent{}, false, nil
	}
	return database.BrainWorkEvents[index], true, nil
}

// WorkResultLifecycles derives presentation labels from durable authorities;
// cards themselves stay immutable timeline messages.
func (s *Store) WorkResultLifecycles(eventIDs []string) (map[string]WorkResultLifecycle, error) {
	wanted := map[string]bool{}
	for _, eventID := range eventIDs {
		if eventID = strings.TrimSpace(eventID); eventID != "" {
			wanted[eventID] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	latestResult := map[string]WorkEvent{}
	for _, event := range database.BrainWorkEvents {
		if !isProjectedWorkResultEvent(event.Kind) {
			continue
		}
		if current, found := latestResult[event.WorkID]; !found || event.Sequence > current.Sequence {
			latestResult[event.WorkID] = event
		}
	}
	obligations := reduceAttentionObligations(database)
	out := map[string]WorkResultLifecycle{}
	for _, event := range database.BrainWorkEvents {
		if !wanted[event.ID] || !isProjectedWorkResultEvent(event.Kind) {
			continue
		}
		obligation := obligations[event.WorkID]
		outstanding := obligation.exists() && attentionEventCanObligate(database, event)
		reviewState := WorkReviewResolved
		if outstanding {
			reviewState = WorkReviewQueued
			// One derived delivery state feeds every presentation label:
			// pending/dispatching stay queued, delivered becomes reviewing.
			if obligation.State == deliveryDelivered &&
				event.Sequence <= database.BrainWorkEvents[obligation.HeadIndex].DeliverySequenceFence {
				reviewState = WorkReviewReviewing
			}
		}
		sessionState := WorkResultSessionOpen
		if itemIndex := workIndex(database.BrainWork, event.WorkID); itemIndex >= 0 {
			for _, finalization := range database.BrainWork[itemIndex].SessionFinalizations {
				if finalization.SessionID != event.SourceName && finalization.SessionID != strings.TrimPrefix(event.PayloadRef, "session:") {
					continue
				}
				switch finalization.State {
				case SessionFinalizationPending:
					sessionState = WorkResultSessionClosing
				case SessionFinalizationFailed:
					sessionState = WorkResultSessionCloseFailed
				case SessionFinalizationComplete, SessionFinalizationSkipped:
					sessionState = WorkResultSessionFinalized
				}
				break
			}
		}
		if reviewState == WorkReviewResolved && sessionState == WorkResultSessionOpen {
			sessionState = WorkResultSessionNotRequired
		}
		out[event.ID] = WorkResultLifecycle{
			EventID:       event.ID,
			ReviewState:   reviewState,
			SessionState:  sessionState,
			CurrentResult: latestResult[event.WorkID].ID == event.ID,
		}
	}
	return out, nil
}

// ResolveWorkEvent is the single Fact/Attention/Disposition transaction. It
// CASes the exact delivered handling attempt, applies one typed Work outcome,
// and records handling for every coalesced signal through the delivery fence.
func (s *Store) ResolveWorkEvent(request WorkEventDispositionRequest) (WorkEvent, Work, error) {
	request.EventID = strings.TrimSpace(request.EventID)
	request.HandlingID = strings.TrimSpace(request.HandlingID)
	request.ProviderTurnID = strings.TrimSpace(request.ProviderTurnID)
	request.SuccessorSessionID = strings.TrimSpace(request.SuccessorSessionID)
	request.NextAction = strings.TrimSpace(request.NextAction)
	request.Summary = strings.TrimSpace(request.Summary)
	if request.EventID == "" || request.HandlingID == "" || request.ProviderTurnID == "" || request.ExpectedWorkRevision == 0 ||
		!validWorkDisposition(request.Disposition) {
		return WorkEvent{}, Work{}, fmt.Errorf("event_id, handling_id, provider_turn_id, expected_work_revision, and a valid disposition are required")
	}
	if request.Disposition == WorkDispositionWait {
		if err := validateWorkWake(request.Wake); err != nil || request.Wake == nil {
			if err == nil {
				err = fmt.Errorf("wait disposition requires a typed wake")
			}
			return WorkEvent{}, Work{}, err
		}
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, err
	}
	eventIndex := workEventIndex(database.BrainWorkEvents, request.EventID)
	if eventIndex < 0 {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	event := database.BrainWorkEvents[eventIndex]
	if event.HandledAt != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrEventHandled
	}
	if event.DeliveredAt == nil || event.HandlingEndedAt != nil || event.HandlingID != request.HandlingID ||
		event.ProviderTurnID != request.ProviderTurnID {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	itemIndex := workIndex(database.BrainWork, event.WorkID)
	if itemIndex < 0 {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrWorkNotFound
	}
	item := database.BrainWork[itemIndex]
	if event.DeliveryWorkRevision != request.ExpectedWorkRevision || item.Revision != request.ExpectedWorkRevision {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrWorkRevisionConflict
	}
	wasTerminal := item.Status == WorkDone || item.Status == WorkCancelled
	if wasTerminal && request.Disposition != WorkDispositionComplete && request.Disposition != WorkDispositionCancel && request.Disposition != WorkDispositionSupersede {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, fmt.Errorf("terminal Work cannot return to a nonterminal disposition")
	}
	if request.Disposition == WorkDispositionWait {
		if workHasLiveCanonicalOwnerTurn(database, item) {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("%w: wait requires the live canonical owner Turn to settle first", ErrWorkOwnerConflict)
		}
		if err := validateWorkWakeProducer(database, item, request.Wake); err != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, err
		}
	}
	if item.SuccessorReservation != nil && request.Disposition != WorkDispositionContinue &&
		request.Disposition != WorkDispositionComplete && request.Disposition != WorkDispositionCancel &&
		request.Disposition != WorkDispositionSupersede {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, fmt.Errorf("a staged successor Session requires a continue disposition")
	}
	switch request.Disposition {
	case WorkDispositionContinue:
		if request.SuccessorSessionID == "" {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("continue disposition requires successor_session_id")
		}
		successorTurnID := ""
		if reservation := item.SuccessorReservation; reservation != nil {
			boundToDisposition := reservation.EventID == event.ID && reservation.HandlingID == event.HandlingID
			unboundAfterRequeue := strings.TrimSpace(reservation.EventID) == "" && strings.TrimSpace(reservation.HandlingID) == ""
			if reservation.SessionID != request.SuccessorSessionID || reservation.ProviderTurnID == "" ||
				(!boundToDisposition && !unboundAfterRequeue) {
				s.mu.Unlock()
				return WorkEvent{}, Work{}, fmt.Errorf("continue successor does not match the Session reserved for this disposition")
			}
			// Requeue intentionally removes the old Host handling binding while
			// preserving the exclusive accepted successor. The new exact
			// disposition validates that unbound reservation and promotes it in
			// this transaction; consume never binds it to a coincident Host ID.
			successorTurnID = reservation.ProviderTurnID
		}
		if !databaseHasCanonicalAcceptedSuccessor(database, item.ID, request.SuccessorSessionID, successorTurnID) {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("successor Session is not an accepted active non-Host owner of Work")
		}
		item.Status = WorkRunning
		item.OwnerSessionID = request.SuccessorSessionID
		item.OwnerDelegated = true
		item.Wake = nil
		item.WaitFor = "Session " + request.SuccessorSessionID
		item.NextAction = firstNonEmpty(request.NextAction, "Wait for the delegated Session.")
		item.SuccessorReservation = nil
		item.SessionFinalizations = nil
	case WorkDispositionWait:
		// OwnerSessionID without a live canonical owner Turn is historical
		// linkage, not execution authority. Retire it in the same disposition
		// replacement before installing the one typed wait owner.
		item.OwnerSessionID = ""
		item.OwnerDelegated = false
		item.Status = WorkWaiting
		item.Wake = cloneWorkWake(request.Wake)
		item.WaitFor = request.Wake.Ref
		item.NextAction = firstNonEmpty(request.NextAction, "Wait for the named external condition.")
	case WorkDispositionComplete:
		item.Status = WorkDone
		item.Wake = nil
		item.NextAction = request.NextAction
		item.WaitFor = ""
	case WorkDispositionCancel, WorkDispositionSupersede:
		item.Status = WorkCancelled
		item.Wake = nil
		item.NextAction = request.NextAction
		item.WaitFor = ""
	}
	if item.Status == WorkDone || item.Status == WorkCancelled {
		item.SessionFinalizations = terminalSessionFinalizations(database, item, now)
		retirePendingDelegatedSubmissionsForWork(&database, item.ID, now)
		item.SuccessorReservation = nil
	}
	item.Revision++
	if !wasTerminal && (item.Status == WorkDone || item.Status == WorkCancelled) {
		item.TerminalRevision = item.Revision
	}
	item.UpdatedAt = now
	handledAt := now.UTC()
	for index := range database.BrainWorkEvents {
		candidate := &database.BrainWorkEvents[index]
		if candidate.WorkID != item.ID || !attentionEventCanObligate(database, *candidate) || candidate.HandledAt != nil ||
			candidate.DiscardedAt != nil || candidate.HistoricalDelivery ||
			candidate.Sequence > event.DeliverySequenceFence {
			continue
		}
		candidate.HandledAt = &handledAt
		candidate.Disposition = request.Disposition
		candidate.DispositionSummary = request.Summary
	}
	database.BrainWork[itemIndex] = item
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, err
	}
	resolvedEvent := database.BrainWorkEvents[eventIndex]
	s.mu.Unlock()
	s.broadcastWorkChange(item.ID)
	return resolvedEvent, item, nil
}

func databaseHasCanonicalAcceptedSuccessor(database orchestrationDatabase, workID, sessionID, providerTurnID string) bool {
	turn, found := currentTurnForSession(database, sessionID)
	if !found || turn.WorkID != workID ||
		(providerTurnID != "" && turn.TurnID != providerTurnID) ||
		isHostHandlingTurn(database, turn) || !turnHasAdmissionAuthority(turn) {
		return false
	}
	switch turn.Status {
	case watcher.TurnAccepted, watcher.TurnRunning, watcher.TurnBlocked:
		return true
	default:
		return false
	}
}

// turnHasAdmissionAuthority accepts either provider correlation or a Control
// fact that could only have been written after matching the random identity
// carried by this Turn's delegated prompt. SignalProtocol alone is only a
// marker and never sufficient authority.
func turnHasAdmissionAuthority(turn TurnRecord) bool {
	if !turn.Admission.Empty() {
		return true
	}
	if !turn.SignalProtocol {
		return false
	}
	for _, fact := range turn.Facts {
		if fact.Class == watcher.EvidenceControl {
			return true
		}
	}
	return false
}

func terminalSessionFinalizations(database orchestrationDatabase, item Work, now time.Time) []SessionFinalization {
	delegated := map[string]bool{}
	if sessionID := strings.TrimSpace(item.OwnerSessionID); sessionID != "" {
		delegated[sessionID] = item.OwnerDelegated
	}
	if reservation := item.SuccessorReservation; reservation != nil {
		if sessionID := strings.TrimSpace(reservation.SessionID); sessionID != "" {
			delegated[sessionID] = true
		}
	}
	for _, turn := range database.BrainTurns {
		if turn.WorkID == item.ID && strings.TrimSpace(turn.SessionID) != "" && !isHostHandlingTurn(database, turn) {
			delegated[turn.SessionID] = true
		}
	}
	for _, submission := range database.BrainTurnSubmissions {
		// ClaimToken identifies a Host claim submission. The Host is never a
		// delegated teardown target. Only pending delegated transactions add a
		// finalization here: resolved Sessions already appear in BrainTurns, while
		// aborted/previously-retired history carries no live provider authority.
		if submission.WorkID == item.ID && submission.State == watcher.TurnSubmissionPending &&
			strings.TrimSpace(submission.ClaimToken) == "" {
			if sessionID := strings.TrimSpace(submission.SessionID); sessionID != "" {
				delegated[sessionID] = true
			}
		}
	}
	existing := map[string]SessionFinalization{}
	for _, finalization := range item.SessionFinalizations {
		existing[finalization.SessionID] = finalization
	}
	ids := make([]string, 0, len(delegated))
	for sessionID := range delegated {
		ids = append(ids, sessionID)
	}
	sort.Strings(ids)
	out := make([]SessionFinalization, 0, len(ids))
	for _, sessionID := range ids {
		finalization := existing[sessionID]
		if finalization.SessionID == "" || finalization.State == SessionFinalizationFailed {
			attempts := finalization.Attempts
			finalization = SessionFinalization{
				SessionID: sessionID, Delegated: delegated[sessionID], State: SessionFinalizationPending,
				Attempts: attempts, UpdatedAt: now,
			}
		}
		out = append(out, finalization)
	}
	return out
}

// RecordSessionFinalization records one idempotent teardown result. A failure
// atomically appends one actionable retry signal, so terminal cleanup cannot
// disappear into prose or a log line.
func (s *Store) RecordSessionFinalization(workID, sessionID string, state SessionFinalizationState, failure error) (Work, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	if state != SessionFinalizationFailed && state != SessionFinalizationComplete && state != SessionFinalizationSkipped {
		return Work{}, fmt.Errorf("invalid finalization result %q", state)
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		s.mu.Unlock()
		return Work{}, ErrWorkNotFound
	}
	item := database.BrainWork[itemIndex]
	if item.Status != WorkDone && item.Status != WorkCancelled {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("Work has no terminal Session finalization obligation")
	}
	finalizationIndex := -1
	for index := range item.SessionFinalizations {
		if item.SessionFinalizations[index].SessionID == sessionID {
			finalizationIndex = index
			break
		}
	}
	if finalizationIndex < 0 {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("Work has no terminal finalization for Session %s", sessionID)
	}
	finalization := &item.SessionFinalizations[finalizationIndex]
	if finalization.State == SessionFinalizationComplete || finalization.State == SessionFinalizationSkipped {
		s.mu.Unlock()
		return item, nil
	}
	finalization.State = state
	finalization.Attempts++
	finalization.LastError = ""
	if failure != nil {
		finalization.LastError = strings.TrimSpace(failure.Error())
	}
	finalization.UpdatedAt = now
	item.Revision++
	item.UpdatedAt = now
	database.BrainWork[itemIndex] = item
	if state == SessionFinalizationFailed {
		event := WorkEvent{
			ID: uuid.NewString(), WorkID: item.ID, Kind: "brain.finalization_failed",
			DedupeKey:  finalizationFailureDedupeKey(finalization.SessionID, finalization.Attempts),
			PayloadRef: "session:" + finalization.SessionID, SourceName: finalization.SessionID,
			Summary:    "Delegated Session finalization failed: " + finalization.LastError,
			Actionable: true, CreatedAt: now,
		}
		if _, err = appendWorkEventLocked(&database, itemIndex, event, false); err != nil {
			s.mu.Unlock()
			return Work{}, err
		}
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	item = database.BrainWork[itemIndex]
	s.mu.Unlock()
	s.broadcastWorkChange(item.ID)
	return item, nil
}

func (s *Store) PendingSessionFinalizations(limit int) ([]PendingSessionFinalization, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("finalization batch limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, false, err
	}
	out := make([]PendingSessionFinalization, 0, limit)
	more := false
	for _, item := range database.BrainWork {
		for _, finalization := range item.SessionFinalizations {
			if finalization.State != SessionFinalizationPending {
				continue
			}
			if len(out) == limit {
				more = true
				return out, more, nil
			}
			out = append(out, PendingSessionFinalization{WorkID: item.ID, Finalization: finalization})
		}
	}
	return out, more, nil
}

func workEventIndex(events []WorkEvent, eventID string) int {
	for index := range events {
		if events[index].ID == eventID {
			return index
		}
	}
	return -1
}

// ReconcileAbsentWorkOwner retires one stale operational relationship after a
// successful fresh Session inventory proves the named owner is absent. It
// never changes or closes the Session ledger and never replays a pending
// submission; those rows remain audit evidence. Repeated inventories are a
// no-op after the owner link is cleared.
func (s *Store) ReconcileAbsentWorkOwner(workID, sessionID string) (Work, bool, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	if workID == "" || sessionID == "" {
		return Work{}, false, fmt.Errorf("work_id and absent session_id are required")
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, false, err
	}
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		s.mu.Unlock()
		return Work{}, false, ErrWorkNotFound
	}
	item := database.BrainWork[itemIndex]
	if item.Status == WorkDone || item.Status == WorkCancelled || item.OwnerSessionID != sessionID {
		s.mu.Unlock()
		return item, false, nil
	}
	obligation := reduceAttentionObligation(database, workID)
	if obligation.exists() && (obligation.State == deliveryDispatching ||
		obligation.State == deliveryDelivered) {
		// Do not invalidate the immutable Work revision already carried by an
		// admitted or admitting Host turn. Attention, not the absent owner
		// string, is operational during this bounded deferral.
		s.mu.Unlock()
		return item, false, nil
	}

	item.OwnerSessionID = ""
	item.OwnerDelegated = false
	item.Status = WorkNeedsInput
	item.NextAction = "Review the absent Session outcome and choose the next Work disposition."
	item.WaitFor = ""
	item.Wake = nil
	if item.SuccessorReservation != nil && item.SuccessorReservation.SessionID == sessionID {
		item.SuccessorReservation = nil
	}
	database.BrainWork[itemIndex] = item

	if !reduceAttentionObligation(database, workID).exists() {
		event := WorkEvent{
			ID:         uuid.NewString(),
			WorkID:     workID,
			Kind:       "brain.owner_absent",
			DedupeKey:  "brain:owner-absent:" + sessionID,
			PayloadRef: "session:" + sessionID,
			SourceName: sessionID,
			Summary:    "A fresh Session inventory no longer contains the recorded Work owner; its outcome requires review.",
			Actionable: true,
			CreatedAt:  now,
		}
		if _, err = appendWorkEventLocked(&database, itemIndex, event, true); err != nil {
			s.mu.Unlock()
			return Work{}, false, err
		}
	} else {
		item.Revision++
		item.UpdatedAt = now
		database.BrainWork[itemIndex] = item
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, false, err
	}
	item = database.BrainWork[itemIndex]
	s.mu.Unlock()
	s.broadcastWorkChange(workID)
	return item, true, nil
}

func (s *Store) ActiveWork() ([]ActiveWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	unread := map[string]bool{}
	cards, err := s.readTimelineWorkCardsLocked()
	if err != nil {
		return nil, fmt.Errorf("read Work timeline projection: %w", err)
	}
	for _, item := range cards {
		if item.Unread {
			unread[item.WorkID] = true
		}
	}
	out := []ActiveWork{}
	for _, item := range database.BrainWork {
		hasUnread := unread[item.ID]
		if (item.Status == WorkDone || item.Status == WorkCancelled) && !hasUnread {
			continue
		}
		out = append(out, ActiveWork{
			ID:                   item.ID,
			Revision:             item.Revision,
			Title:                item.Title,
			Status:               item.Status,
			OwnerSessionID:       item.OwnerSessionID,
			OwnerDelegated:       item.OwnerDelegated,
			WaitFor:              item.WaitFor,
			Wake:                 cloneWorkWake(item.Wake),
			ProgressMode:         mustDeriveWorkProgressMode(database, item),
			AttentionPending:     workHasAttentionObligation(database, item.ID),
			SuccessorReservation: cloneSuccessorReservation(item.SuccessorReservation),
			SessionFinalizations: cloneSessionFinalizations(item.SessionFinalizations),
			UnreadResult:         hasUnread,
		})
	}
	sort.SliceStable(out, func(left, right int) bool {
		leftIndex := workIndex(database.BrainWork, out[left].ID)
		rightIndex := workIndex(database.BrainWork, out[right].ID)
		leftWork := database.BrainWork[leftIndex]
		rightWork := database.BrainWork[rightIndex]
		if leftWork.UpdatedAt.Equal(rightWork.UpdatedAt) {
			return leftWork.ID < rightWork.ID
		}
		return leftWork.UpdatedAt.After(rightWork.UpdatedAt)
	})
	return out, nil
}

const currentWorkQueuedAttentionLimit = 4

// ProjectWorkInventory separates bounded current operational relationships
// from the durable Work ledger. presentSessions must come from one successful,
// fresh delegated Session inventory; a stored Session string is never enough
// to manufacture an endpoint.
func (s *Store) ProjectWorkInventory(presentSessions map[string]bool) (WorkInventory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return WorkInventory{}, err
	}
	present := map[string]bool{}
	for sessionID, isPresent := range presentSessions {
		if normalized := strings.TrimSpace(sessionID); normalized != "" && isPresent {
			present[normalized] = true
		}
	}

	unread := map[string]bool{}
	hasResult := map[string]bool{}
	for _, event := range database.BrainWorkEvents {
		if isResultEvent(event.Kind) {
			hasResult[event.WorkID] = true
		}
	}
	cards, err := s.readTimelineWorkCardsLocked()
	if err != nil {
		return WorkInventory{}, fmt.Errorf("read Work timeline projection: %w", err)
	}
	for _, item := range cards {
		if item.Unread {
			unread[item.WorkID] = true
		}
	}
	obligations := reduceAttentionObligations(database)
	reviewing := map[string]bool{}
	for workID, obligation := range obligations {
		reviewing[workID] = obligation.exists() && obligation.State == deliveryDelivered
	}
	queued := projectedAttentionWork(database, currentWorkQueuedAttentionLimit)
	queueRank := map[string]int{}
	for index, workID := range queued {
		queueRank[workID] = index
	}

	current := make([]CurrentWork, 0, len(database.BrainWork))
	currentIDs := map[string]bool{}
	repair := map[string]bool{}
	for _, item := range database.BrainWork {
		mode := mustDeriveWorkProgressMode(database, item)
		obligation := obligations[item.ID]
		attentionState := WorkAttentionState("")
		if reviewing[item.ID] {
			attentionState = WorkAttentionReviewing
		} else if _, selected := queueRank[item.ID]; selected {
			attentionState = WorkAttentionQueued
		}
		finalizations := presentFinalizations(item.SessionFinalizations, present)
		include := attentionState != ""
		terminal := item.Status == WorkDone || item.Status == WorkCancelled
		if !terminal {
			switch mode {
			case WorkProgressOwned:
				if item.OwnerDelegated && present[item.OwnerSessionID] {
					include = true
				} else {
					repair[item.ID] = true
				}
			case WorkProgressWaiting:
				if currentWakePresent(item.Wake, present) {
					include = true
				} else {
					repair[item.ID] = true
				}
			case WorkProgressReady:
				// Only the bounded fair queue window is operationally current.
			default:
				repair[item.ID] = true
			}
			if obligation.NeedsRepair {
				repair[item.ID] = true
			}
			if strings.TrimSpace(item.OwnerSessionID) != "" && !present[item.OwnerSessionID] {
				repair[item.ID] = true
			}
		} else {
			for _, finalization := range item.SessionFinalizations {
				if (finalization.State == SessionFinalizationPending || finalization.State == SessionFinalizationFailed) &&
					!present[finalization.SessionID] {
					repair[item.ID] = true
				}
			}
			if len(finalizations) > 0 {
				include = true
			}
		}
		if !include {
			continue
		}
		currentIDs[item.ID] = true
		current = append(current, CurrentWork{
			ID:                   item.ID,
			Revision:             item.Revision,
			Title:                item.Title,
			Status:               item.Status,
			ProgressMode:         mode,
			OwnerSessionID:       currentOwnerSessionID(item, present),
			OwnerDelegated:       item.OwnerDelegated && present[item.OwnerSessionID],
			WaitFor:              item.WaitFor,
			Wake:                 cloneWorkWake(item.Wake),
			AttentionState:       attentionState,
			SessionFinalizations: finalizations,
			UnreadResult:         unread[item.ID],
		})
	}
	sort.SliceStable(current, func(left, right int) bool {
		leftReview := current[left].AttentionState == WorkAttentionReviewing
		rightReview := current[right].AttentionState == WorkAttentionReviewing
		if leftReview != rightReview {
			return leftReview
		}
		leftRank, leftQueued := queueRank[current[left].ID]
		rightRank, rightQueued := queueRank[current[right].ID]
		if leftQueued != rightQueued {
			return leftQueued
		}
		if leftQueued && leftRank != rightRank {
			return leftRank < rightRank
		}
		leftIndex := workIndex(database.BrainWork, current[left].ID)
		rightIndex := workIndex(database.BrainWork, current[right].ID)
		if database.BrainWork[leftIndex].UpdatedAt.Equal(database.BrainWork[rightIndex].UpdatedAt) {
			return current[left].ID < current[right].ID
		}
		return database.BrainWork[leftIndex].UpdatedAt.After(database.BrainWork[rightIndex].UpdatedAt)
	})

	backlog := WorkBacklog{RepairNeeded: len(repair)}
	for _, item := range database.BrainWork {
		if currentIDs[item.ID] {
			continue
		}
		backlog.Total++
		if obligations[item.ID].exists() {
			backlog.QueuedAttention++
		}
		if item.Status == WorkDone || item.Status == WorkCancelled || hasResult[item.ID] {
			backlog.HistoricalResults++
		}
	}
	return WorkInventory{Current: current, Backlog: backlog}, nil
}

func projectedAttentionWork(database orchestrationDatabase, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	projection := database
	projection.BrainWorkEvents = append([]WorkEvent(nil), database.BrainWorkEvents...)
	for len(out) < limit {
		index := oldestPendingAttentionHeadIndex(projection)
		if index < 0 {
			break
		}
		workID := projection.BrainWorkEvents[index].WorkID
		out = append(out, workID)
		claimed := time.Unix(1, int64(len(out))).UTC()
		projection.BrainWorkEvents[index].ClaimedAt = &claimed
		projection.BrainWorkEvents[index].DeliveryHostSessionID = "projection"
	}
	return out
}

func presentFinalizations(finalizations []SessionFinalization, present map[string]bool) []SessionFinalization {
	out := []SessionFinalization{}
	for _, finalization := range finalizations {
		if (finalization.State == SessionFinalizationPending || finalization.State == SessionFinalizationFailed) &&
			present[finalization.SessionID] {
			out = append(out, finalization)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return cloneSessionFinalizations(out)
}

func currentOwnerSessionID(item Work, present map[string]bool) string {
	if item.OwnerDelegated && present[item.OwnerSessionID] {
		return item.OwnerSessionID
	}
	return ""
}

func currentWakePresent(wake *WorkWake, present map[string]bool) bool {
	if wake == nil {
		return false
	}
	switch wake.Kind {
	case WorkWakeUserInput, WorkWakeCalendarResult:
		return true
	case WorkWakeSessionTerminal:
		ref := strings.TrimPrefix(strings.TrimSpace(wake.Ref), "session:")
		marker := strings.Index(ref, ":turn:")
		return marker > 0 && present[ref[:marker]]
	default:
		return false
	}
}

func compactWorkResultText(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	paragraphs := strings.Split(normalized, "\n\n")
	selected := ""
	for _, paragraph := range paragraphs {
		if selected = strings.Join(strings.Fields(paragraph), " "); selected != "" {
			break
		}
	}
	runes := []rune(selected)
	if len(runes) <= workResultSummaryRuneLimit {
		return selected
	}
	return string(runes[:workResultSummaryRuneLimit-1]) + "…"
}

func isProjectedWorkResultEvent(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session.done", "session.failed", "session.needs_input", "session.stale", "session.uncertain":
		return true
	default:
		return false
	}
}

func isResultEvent(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session.done", "session.failed", "session.needs_input", "session.stale",
		"session.uncertain", "delivery.uncertain",
		"calendar.result", "calendar.failure":
		return true
	default:
		return false
	}
}

// MarkWorkRead clears the presentation read state for one Work. Read state
// lives only in the timeline work-card projection; the append-only Event
// fact never carries it, so reading can never mutate scheduler state.
func (s *Store) MarkWorkRead(workID string) error {
	return s.MarkTimelineWorkCardsRead(workID)
}

// readTimelineWorkCardsLocked returns every materialized work card. It is the
// read projection consumed by ActiveWork/ProjectWorkInventory unread labels.
// The caller must hold s.mu.
func (s *Store) readTimelineWorkCardsLocked() ([]TimelineItem, error) {
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]TimelineItem, 0, len(items))
	for _, item := range items {
		if item.Kind == timelineKindWorkCard {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Store) SubscribeWork() (int, <-chan WorkChange) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan WorkChange, 32)
	s.subs[id] = ch
	return id, ch
}

func (s *Store) UnsubscribeWork(id int) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}

func (s *Store) broadcastWorkChange(workID string) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	change := WorkChange{WorkID: workID}
	for _, ch := range s.subs {
		select {
		case ch <- change:
		default:
		}
	}
}
