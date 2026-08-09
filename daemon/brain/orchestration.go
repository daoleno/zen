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

const orchestrationSchemaVersion = 7

var (
	ErrWorkNotFound         = errors.New("Brain Work not found")
	ErrWorkConflict         = errors.New("Brain Work already exists")
	ErrWorkOwnerConflict    = errors.New("Brain Work already has an owner Session")
	ErrEventClaim           = errors.New("Brain Work event claim is no longer current")
	ErrEventHandled         = errors.New("Brain Work event is already handled")
	ErrWorkRevisionConflict = errors.New("Brain Work revision is no longer current")
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
// terminal disposition. Historical terminal Work is never backfilled.
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
// never by elapsed time.
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
	ReadAt                *time.Time      `json:"read_at,omitempty"`
	Resolution            string          `json:"resolution,omitempty"`
	ResolvedBy            string          `json:"resolved_by,omitempty"`
	ResolvedAt            *time.Time      `json:"resolved_at,omitempty"`
	DiscardedAt           *time.Time      `json:"discarded_at,omitempty"`
	ReplayOf              string          `json:"replay_of,omitempty"`
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

type WorkChange struct {
	WorkID string
}

const workResultSummaryRuneLimit = 360

type orchestrationDatabase struct {
	SchemaVersion        int                     `json:"schema_version"`
	NextEventSequence    uint64                  `json:"next_event_sequence"`
	Migrations           orchestrationMigrations `json:"migrations"`
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
	}{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*event = WorkEvent(decoded.durableWorkEvent)
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
	// Per-turn liveness backfill: rows persisted before the per-turn lease
	// existed have a zero deadline. They get a fresh lease minted from their
	// acceptance so an upgrade can never resurrect an old turn's expired
	// lease as an immediate session.stale. Deterministic and idempotent: a
	// deadline already set is never rewritten.
	leaseBackfilled := backfillTurnLeaseDeadlines(&database, s.nowUTC())
	if migrated || bound || leaseBackfilled {
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
				DeliveredAt: old.ConsumedAt, ReadAt: old.ReadAt,
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
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
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
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           []TurnRecord{},
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
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
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: []TurnSubmissionRecord{},
		}
		upgradeSignalSchema(&database)
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
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: record.BrainTurnSubmissions,
		}
		upgradeSignalSchema(&database)
		if err := validateOrchestrationDatabaseLoose(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case orchestrationSchemaVersion:
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
			BrainWork:            worksFromRecords(record.BrainWork),
			BrainWorkEvents:      record.BrainWorkEvents,
			BrainTurns:           record.BrainTurns,
			BrainTurnSubmissions: record.BrainTurnSubmissions,
		}
		upgradeSignalSchema(&database)
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
		return database, needsBind, nil
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

func validateOrchestrationDatabase(database orchestrationDatabase) error {
	return validateOrchestrationDatabaseWithSourceThread(database, true)
}

func validateOrchestrationDatabaseLoose(database orchestrationDatabase) error {
	return validateOrchestrationDatabaseWithSourceThread(database, false)
}

func validateOrchestrationDatabaseWithSourceThread(database orchestrationDatabase, requireSourceThread bool) error {
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
	if err := validateActiveExecutionOwners(database); err != nil {
		return err
	}
	if err := validateTurnSubmissions(database.BrainTurnSubmissions, workIDs); err != nil {
		return err
	}
	return nil
}

func validateActiveExecutionOwners(database orchestrationDatabase) error {
	activeByWork := map[string]map[string]struct{}{}
	for _, turn := range database.BrainTurns {
		if watcher.TurnImmutable(turn.Status) || isHostHandlingTurn(database, turn) {
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
			if watcher.TurnImmutable(turn.Status) || isHostHandlingTurn(database, turn) ||
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

func (state workProgressState) count() int {
	count := 0
	if state.Owned {
		count++
	}
	if state.Waiting {
		count++
	}
	if state.Ready {
		count++
	}
	return count
}

func workHasLiveCanonicalOwnerTurn(database orchestrationDatabase, item Work) bool {
	ownerID := strings.TrimSpace(item.OwnerSessionID)
	if ownerID == "" {
		return false
	}
	turn, found := currentTurnForSession(database, ownerID)
	return found && turn.WorkID == item.ID && !watcher.TurnImmutable(turn.Status) &&
		!isHostHandlingTurn(database, turn)
}

// reduceWorkProgressState derives the three progress predicates independently.
// OwnerSessionID text is not authority: only its current canonical nonterminal
// execution Turn owns progress. The narrow running/no-Turn admission window is
// retained for the existing two-phase delegated launch; migration never treats
// that window as legacy owner authority.
func reduceWorkProgressState(database orchestrationDatabase, item Work) workProgressState {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return workProgressState{}
	}
	liveOwner := workHasLiveCanonicalOwnerTurn(database, item)
	ownerAdmission := !liveOwner && strings.TrimSpace(item.OwnerSessionID) != "" && item.Status == WorkRunning
	state := workProgressState{
		Owned:             liveOwner || ownerAdmission,
		Waiting:           item.Wake != nil,
		Ready:             workHasUnhandledAttention(database, item.ID),
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
	if state.count() != 1 {
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

func workHasUnhandledAttention(database orchestrationDatabase, workID string) bool {
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.Actionable && event.HandledAt == nil &&
			event.DiscardedAt == nil && event.Resolution == "" && !event.HistoricalDelivery {
			return true
		}
	}
	return false
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
// spawn; AttachWorkOwner performs the authoritative check under the Store lock.
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

func applyWorkUpdateLocked(database *orchestrationDatabase, index int, update WorkUpdate, now time.Time) (Work, error) {
	if database == nil || index < 0 || index >= len(database.BrainWork) {
		return Work{}, ErrWorkNotFound
	}
	original := database.BrainWork[index]
	item := original
	wasTerminal := item.Status == WorkDone || item.Status == WorkCancelled
	applyWorkUpdate(&item, update)
	if wasTerminal && item.Status != original.Status {
		return Work{}, fmt.Errorf("%w: terminal Work cannot be reopened", ErrWorkConflict)
	}
	item.UpdatedAt = now
	item.Revision++
	// SourceThreadID is frozen at Create and never rewritten.
	item.SourceThreadID = original.SourceThreadID
	if !wasTerminal && (item.Status == WorkDone || item.Status == WorkCancelled) {
		item.SessionFinalizations = terminalSessionFinalizations(*database, item, now)
		item.SuccessorReservation = nil
	}
	if err := validateWork(item); err != nil {
		return Work{}, err
	}
	database.BrainWork[index] = item
	return item, nil
}

// AttachWorkOwner is the only delegated-spawn ownership transition. It
// atomically changes an active unowned Work record to one running owner. While
// Brain is handling an exact delivered Event, it instead creates the one
// Work-level successor reservation. The reservation survives handling requeue
// and restart; only the exact continue disposition may promote it. A concurrent
// incumbent or successor is never replaced.
func (s *Store) AttachWorkOwner(id, ownerSessionID string) (Work, error) {
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
			} else if item.SuccessorReservation != nil {
				err = fmt.Errorf("%w: Work %s already reserved successor %s", ErrWorkOwnerConflict, item.ID, item.SuccessorReservation.SessionID)
			} else if strings.TrimSpace(item.OwnerSessionID) != "" {
				err = fmt.Errorf("%w: Work %s is owned by %s", ErrWorkOwnerConflict, item.ID, item.OwnerSessionID)
			} else {
				now := s.nowUTC()
				item.OwnerSessionID = ownerSessionID
				item.OwnerDelegated = true
				item.Status = WorkRunning
				item.NextAction = "Wait for the delegated Session."
				item.WaitFor = "Session " + ownerSessionID
				item.Wake = nil
				item.UpdatedAt = now
				item.Revision++
				database.BrainWork[index] = item
				settleUndeliveredAttentionForOwner(&database, item.ID, now)
				err = s.persistOrchestrationLocked(database)
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
	if state.count() == 1 {
		return item, nil
	}
	if state.count() > 1 {
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

func settleUndeliveredAttentionForOwner(database *orchestrationDatabase, workID string, now time.Time) {
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.WorkID != workID || !event.Actionable || event.HandledAt != nil ||
			event.DeliveredAt != nil || event.DiscardedAt != nil {
			continue
		}
		discarded := now.UTC()
		event.DiscardedAt = &discarded
		event.Resolution = EventResolutionDiscard
		event.ResolvedBy = "owner_attachment"
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
		if item.OwnerSessionID == sessionID && item.Status != WorkDone && item.Status != WorkCancelled {
			return item.ID
		}
		if item.SuccessorReservation != nil && item.SuccessorReservation.SessionID == sessionID &&
			item.Status != WorkDone && item.Status != WorkCancelled {
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
			if strings.TrimSpace(item.OwnerSessionID) != "" && !state.LiveCanonicalTurn {
				item.OwnerSessionID = ""
				item.OwnerDelegated = false
				item.Revision++
				item.UpdatedAt = now
				database.BrainWork[index] = item
				revisionBumped = true
				changedIDs = append(changedIDs, item.ID)
				state = reduceWorkProgressState(database, item)
			}
			if state.count() > 1 {
				s.mu.Unlock()
				_, stateErr := deriveWorkProgressMode(database, item)
				return false, processed, stateErr
			}
			if state.count() == 0 {
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
	event.ReadAt = nil
	if isSessionLifecycleKind(event.Kind) && !isTurnScopedSessionDedupeKey(event.DedupeKey) {
		// A delegated lifecycle Event is unrepresentable without the
		// canonical current TurnID: the dedupe key must be turn-scoped
		// (session:<sid>:turn:<tid>:<kind>). Raw-state routing and
		// occurrence-counting keys are deleted.
		return WorkEvent{}, fmt.Errorf("delegated lifecycle event %q requires a canonical turn-scoped dedupe key", event.Kind)
	}
	return event, nil
}

// ApplyProducerTransition commits one producer state change, its canonical
// event, and every exact typed consumer wake in one orchestration replacement.
// The producer dedupe key is the transaction identity: once present, the
// transition and all matching wakes are already durable and replay is a no-op.
func (s *Store) ApplyProducerTransition(
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
		s.mu.Unlock()
		return Work{}, WorkEvent{}, false, nil, ErrWorkNotFound
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
	if event.Actionable {
		if readyID := readyAttentionEventID(*database, event.WorkID); readyID != "" {
			event.CoalescedInto = readyID
		}
	}
	if err := validateWorkEvent(event); err != nil {
		return WorkEvent{}, err
	}
	database.BrainWorkEvents = append(database.BrainWorkEvents, event)
	return event, nil
}

func readyAttentionEventID(database orchestrationDatabase, workID string) string {
	for _, event := range database.BrainWorkEvents {
		if event.WorkID != workID || !event.Actionable || event.HandledAt != nil ||
			event.DiscardedAt != nil || event.HistoricalDelivery || event.CoalescedInto != "" {
			continue
		}
		if event.ClaimedAt == nil && event.DeliveredAt == nil {
			return event.ID
		}
	}
	return ""
}

// WakeWaitingWork atomically projects one external producer fact to every Work
// waiting on that exact typed reference. It is idempotent per Work and source
// occurrence; unrelated waits remain untouched.
func (s *Store) WakeWaitingWork(wake WorkWake, kind, occurrenceID, summary string) ([]WorkEvent, error) {
	wake.Ref = strings.TrimSpace(wake.Ref)
	kind = strings.TrimSpace(kind)
	occurrenceID = strings.TrimSpace(occurrenceID)
	summary = strings.TrimSpace(summary)
	if err := validateWorkWake(&wake); err != nil {
		return nil, err
	}
	if wake.Kind == WorkWakeSessionTerminal {
		return nil, fmt.Errorf("session_terminal producer authority belongs to the canonical Turn reducer")
	}
	if kind == "" || occurrenceID == "" {
		return nil, fmt.Errorf("wake fact requires kind and occurrence identity")
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	recorded, changedIDs, err := wakeWaitingWorkLocked(&database, wake, kind, occurrenceID, summary, now)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if len(recorded) == 0 {
		s.mu.Unlock()
		return recorded, nil
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	for _, workID := range changedIDs {
		s.broadcastWorkChange(workID)
	}
	return recorded, nil
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
	for _, event := range database.BrainWorkEvents {
		if event.DeliveredAt != nil && event.HandledAt == nil && event.HandlingEndedAt == nil && !event.HistoricalDelivery {
			return WorkEvent{}, false, nil
		}
	}
	index := -1
	for candidate := range database.BrainWorkEvents {
		event := database.BrainWorkEvents[candidate]
		if !workEventSchedulerEligible(database, event) || event.ClaimedAt != nil {
			continue
		}
		if index < 0 || event.Sequence < database.BrainWorkEvents[index].Sequence {
			index = candidate
		}
	}
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
	return database.BrainWorkEvents[index], true, nil
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
	// Upgrade safety: legacy actionable delegated lifecycle rows may have
	// unscoped/occurrence-counted keys from before the canonical TurnID gate.
	// They remain durable audit rows but are never eligible for a scheduler
	// claim after upgrade; only the reducer's turn-scoped rows can wake Brain.
	// A user-authorized replay of a held delivery is the one explicit second
	// wake (C.2.6.3) and stays eligible.
	if isSessionLifecycleKind(event.Kind) && strings.TrimSpace(event.ReplayOf) == "" &&
		!isTurnScopedSessionDedupeKey(event.DedupeKey) {
		return false
	}
	if !event.Actionable || event.DeliveredAt != nil || event.HandledAt != nil ||
		event.DiscardedAt != nil || event.HistoricalDelivery || event.CoalescedInto != "" ||
		event.Resolution != "" {
		// Resolved rows (mark_delivered, discard, replay) leave the held set
		// forever; they are never claimed, re-listed, or re-dispatched.
		return false
	}
	for _, other := range database.BrainWorkEvents {
		if other.ID == event.ID || other.WorkID != event.WorkID || other.HandledAt != nil ||
			other.DiscardedAt != nil || other.Resolution != "" {
			continue
		}
		if other.ClaimedAt != nil && other.DeliveredAt == nil ||
			other.DeliveredAt != nil && other.HandlingEndedAt == nil {
			return false
		}
	}
	index := workIndex(database.BrainWork, event.WorkID)
	if index < 0 {
		return false
	}
	item := database.BrainWork[index]
	if item.Status != WorkDone && item.Status != WorkCancelled {
		return true
	}
	// Strictly earlier Events are historical backlog; equality stays eligible
	// for serialized terminal update-then-append under coarse clocks.
	return !event.CreatedAt.Before(item.UpdatedAt)
}

// ReleaseEventClaim atomically makes the exact identity-bound Event claimable
// again only when Session Input proved that provider mutation never started.
func (s *Store) ReleaseEventClaim(eventID, hostSessionID string) error {
	eventID = strings.TrimSpace(eventID)
	hostSessionID = strings.TrimSpace(hostSessionID)
	if eventID == "" || hostSessionID == "" {
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
			event.DeliveryHostSessionID != hostSessionID {
			return ErrEventClaim
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
// Host after Session Input accepts that Event.ID receipt. Event and Host
// identity together are the authorization boundary.
func (s *Store) ConsumeClaimedWorkEvent(eventID, hostSessionID, providerTurnID string) (WorkEvent, Work, error) {
	eventID = strings.TrimSpace(eventID)
	hostSessionID = strings.TrimSpace(hostSessionID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if eventID == "" || hostSessionID == "" || providerTurnID == "" {
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	workID := ""
	var claimed WorkEvent
	var item Work
	if err == nil {
		for index := range database.BrainWorkEvents {
			event := &database.BrainWorkEvents[index]
			if event.ID != eventID {
				continue
			}
			if !event.Actionable || event.ClaimedAt == nil || event.DeliveredAt != nil ||
				event.DeliveryHostSessionID != hostSessionID || event.ProviderTurnID != providerTurnID {
				err = ErrEventClaim
				break
			}
			workID = database.BrainWorkEvents[index].WorkID
			workIndex := -1
			for candidate := range database.BrainWork {
				if database.BrainWork[candidate].ID == workID {
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
			if reservation := database.BrainWork[workIndex].SuccessorReservation; reservation != nil &&
				strings.TrimSpace(reservation.EventID) == "" && strings.TrimSpace(reservation.HandlingID) == "" {
				reservation = cloneSuccessorReservation(reservation)
				reservation.EventID = event.ID
				reservation.HandlingID = event.HandlingID
				database.BrainWork[workIndex].SuccessorReservation = reservation
			}
			claimed = *event
			item = database.BrainWork[workIndex]
			err = s.persistOrchestrationLocked(database)
			break
		}
		if workID == "" && err == nil {
			err = ErrEventClaim
		}
	}
	s.mu.Unlock()
	if err == nil && workID != "" {
		s.broadcastWorkChange(workID)
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
	reconcile, err = appendWorkEventLocked(&database, itemIndex, reconcile, true)
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
	terminal := item.Status == WorkDone || item.Status == WorkCancelled
	if terminal && request.Disposition != WorkDispositionComplete && request.Disposition != WorkDispositionCancel && request.Disposition != WorkDispositionSupersede {
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
		if !databaseHasActiveSuccessor(database, item.ID, request.SuccessorSessionID) {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("successor Session is not an accepted active owner of Work")
		}
		if reservation := item.SuccessorReservation; reservation != nil &&
			(reservation.SessionID != request.SuccessorSessionID || reservation.EventID != event.ID ||
				reservation.HandlingID != event.HandlingID || reservation.ProviderTurnID == "") {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("continue successor does not match the Session staged for this Host turn")
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
		item.SuccessorReservation = nil
	}
	item.Revision++
	item.UpdatedAt = now
	handledAt := now.UTC()
	for index := range database.BrainWorkEvents {
		candidate := &database.BrainWorkEvents[index]
		if candidate.WorkID != item.ID || !candidate.Actionable || candidate.HandledAt != nil ||
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

func databaseHasActiveSuccessor(database orchestrationDatabase, workID, sessionID string) bool {
	for _, turn := range database.BrainTurns {
		if turn.WorkID == workID && turn.SessionID == sessionID &&
			turn.Status != watcher.TurnDone && turn.Status != watcher.TurnFailed && turn.Status != watcher.TurnUnknown {
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
			DedupeKey:  fmt.Sprintf("brain:finalization:%s:attempt:%d", finalization.SessionID, finalization.Attempts),
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

func (s *Store) ActiveWork() ([]ActiveWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	unread := map[string]bool{}
	for _, event := range database.BrainWorkEvents {
		if event.Actionable && event.ReadAt == nil && isResultEvent(event.Kind) {
			unread[event.WorkID] = true
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
			AttentionPending:     workHasUnhandledAttention(database, item.ID),
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

func (s *Store) MarkWorkRead(workID string) error {
	workID = strings.TrimSpace(workID)
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	changed := false
	if err == nil && workIndex(database.BrainWork, workID) < 0 {
		err = ErrWorkNotFound
	}
	if err == nil {
		now := s.nowUTC()
		for index := range database.BrainWorkEvents {
			event := &database.BrainWorkEvents[index]
			if event.WorkID == workID && event.ReadAt == nil && isResultEvent(event.Kind) {
				event.ReadAt = &now
				changed = true
			}
		}
		if changed {
			err = s.persistOrchestrationLocked(database)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if changed {
		s.broadcastWorkChange(workID)
	}
	return nil
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
