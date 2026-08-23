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

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/google/uuid"
)

const presentationSchemaVersion = 13

var (
	ErrWorkNotFound         = errors.New("Brain Work not found")
	ErrWorkConflict         = errors.New("Brain Work already exists")
	ErrWorkAttemptConflict  = errors.New("Brain Work already has an active Attempt")
	ErrEventClaim           = errors.New("Brain Work event claim is no longer current")
	ErrEventHandled         = errors.New("Brain Work event is already handled")
	ErrWorkRevisionConflict = errors.New("Brain Work revision is no longer current")
	ErrWorkCloseConflict    = errors.New("Brain Work cannot be operator-closed while signal authority is in flight")
	// ErrSchedulerStateReset is returned when the lifecycle document is not
	// the current scheduler schema. Scheduler state is disposable by design:
	// no migration exists, so the state directory must be archived/reset.
	ErrSchedulerStateReset = errors.New("Brain scheduler state requires a reset (schema is not current)")
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
	WorkWakeDueRetry        WorkWakeKind = "due_retry"
)

// WorkWake is the typed identity of the external producer that owns a true
// wait. WaitFor remains explanatory prose and never schedules Work.
type WorkWake struct {
	Kind          WorkWakeKind `json:"kind"`
	Ref           string       `json:"ref"`
	NextAttemptAt *time.Time   `json:"next_attempt_at,omitempty"`
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
// same lifecycle replacement. messages.jsonl is only its projection.
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

// HostForegroundTurn is the durable admission event for one foreground Brain
// response. StartedAt is the persisted pre-provider-mutation Prepare boundary,
// not the later accepted-state write: provider Activity may naturally begin
// between those two commits. Multiple accepted steering inputs may share the
// same provider activity, but a terminal boundary closes this exact Host
// generation and turn once. It is not a provider input queue. It is closed
// only by strong exact terminal evidence (matching provider activity identity)
// inside the single Host-lane reducer.
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
	WorkDispositionContinue WorkDisposition = "continue"
	WorkDispositionWait     WorkDisposition = "wait"
	WorkDispositionComplete WorkDisposition = "complete"
	WorkDispositionCancel   WorkDisposition = "cancel"
)

// Work is the only durable Brain commitment. It is intentionally small:
// detailed plans and evidence remain in the referenced worklog.
//
// SourceThreadID freezes the originating Brain thread at Work creation.
// Later Work Events materialize only into that persisted thread, even if the
// user has since created or switched to another Brain thread.
//
// Review is the canonical Brain review obligation (see work_review.go): it is
// the only scheduler truth. Session status and Event delivery never
// independently own scheduler state.
type Work struct {
	ID               string           `json:"work_id"`
	Revision         uint64           `json:"revision"`
	TerminalRevision uint64           `json:"terminal_revision,omitempty"`
	Title            string           `json:"title"`
	Objective        string           `json:"objective"`
	Status           WorkStatus       `json:"status"`
	AttemptSessionID string           `json:"attempt_session_id,omitempty"`
	AttemptDelegated bool             `json:"attempt_delegated,omitempty"`
	SourceThreadID   string           `json:"source_thread_id,omitempty"`
	CompletionPolicy CompletionPolicy `json:"completion_policy"`
	DoneCriteriaRef  string           `json:"done_criteria_ref,omitempty"`
	NextAction       string           `json:"next_action,omitempty"`
	WaitFor          string           `json:"wait_for,omitempty"`
	Wake             *WorkWake        `json:"wake,omitempty"`
	Review           *WorkReview      `json:"review,omitempty"`
	ContextRef       string           `json:"context_ref,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// WorkEvent is an append-only fact and at most a one-shot wake/delivery
// signal. It never carries claim, lease, or delivery state: Work.Review is the
// only scheduler truth (I1). Event.ID is the delivery receipt identity and
// the review event anchor. Resolution/ResolvedBy/ResolvedAt/DiscardedAt are
// the durable actor-recorded audit trail for explicit lease closures
// (mark_delivered, discard, replay); HandledAt/Disposition/DispositionSummary
// are the Brain disposition audit. Both are written only with the atomic
// transition that closes the event; elapsed time never writes them.
type WorkEvent struct {
	ID                 string          `json:"event_id"`
	WorkID             string          `json:"work_id"`
	Kind               string          `json:"kind"`
	DedupeKey          string          `json:"dedupe_key"`
	PayloadRef         string          `json:"payload_ref,omitempty"`
	SourceName         string          `json:"source_name,omitempty"`
	Summary            string          `json:"summary,omitempty"`
	Actionable         bool            `json:"actionable"`
	CreatedAt          time.Time       `json:"created_at"`
	Sequence           uint64          `json:"sequence"`
	WorkRevision       uint64          `json:"work_revision"`
	CoalescedInto      string          `json:"coalesced_into,omitempty"`
	HandledAt          *time.Time      `json:"handled_at,omitempty"`
	Disposition        WorkDisposition `json:"disposition,omitempty"`
	DispositionSummary string          `json:"disposition_summary,omitempty"`
	Resolution         string          `json:"resolution,omitempty"`
	ResolvedBy         string          `json:"resolved_by,omitempty"`
	ResolvedAt         *time.Time      `json:"resolved_at,omitempty"`
	DiscardedAt        *time.Time      `json:"discarded_at,omitempty"`
}

// WorkEventResolution values for actor lease closure audit (the C.2.6 path,
// now Work-scoped: the fact row records who closed the lease and how).
const (
	EventResolutionMarkDelivered = "mark_delivered"
	EventResolutionDiscard       = "discard"
	EventResolutionReplayed      = "replayed"
)

type WorkUpdate struct {
	Title            *string
	Objective        *string
	Status           *WorkStatus
	AttemptSessionID *string
	CompletionPolicy *CompletionPolicy
	DoneCriteriaRef  *string
	NextAction       *string
	WaitFor          *string
	Wake             **WorkWake
	ContextRef       *string
}

// WorkCloseRequest is the explicit operator path for terminalizing queued or
// historical Work that has no in-flight Host/provider authority. It is not a
// substitute for ResolveWorkReview: a claimed delivery, admitted Host handling,
// or unresolved provider submission must still finish through its exact
// capability. Actor and reason are persisted in a non-actionable audit Event.
type WorkCloseRequest struct {
	WorkID           string     `json:"work_id"`
	ExpectedRevision uint64     `json:"expected_work_revision"`
	Status           WorkStatus `json:"status"`
	Actor            string     `json:"actor"`
	Reason           string     `json:"reason"`
}

type ActiveWork struct {
	ID               string           `json:"work_id"`
	Revision         uint64           `json:"revision"`
	Title            string           `json:"title"`
	Status           WorkStatus       `json:"status"`
	ProgressMode     WorkProgressMode `json:"progress_mode"`
	AttemptSessionID string           `json:"attempt_session_id,omitempty"`
	AttemptDelegated bool             `json:"attempt_delegated,omitempty"`
	WaitFor          string           `json:"wait_for,omitempty"`
	Wake             *WorkWake        `json:"wake,omitempty"`
	AttentionPending bool             `json:"attention_pending"`
	UnreadResult     bool             `json:"unread_result"`
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
	ID               string             `json:"work_id"`
	Revision         uint64             `json:"revision"`
	Title            string             `json:"title"`
	Status           WorkStatus         `json:"status"`
	ProgressMode     WorkProgressMode   `json:"progress_mode,omitempty"`
	AttemptSessionID string             `json:"attempt_session_id,omitempty"`
	AttemptDelegated bool               `json:"attempt_delegated,omitempty"`
	WaitFor          string             `json:"wait_for,omitempty"`
	Wake             *WorkWake          `json:"wake,omitempty"`
	AttentionState   WorkAttentionState `json:"attention_state,omitempty"`
	UnreadResult     bool               `json:"unread_result"`
}

// WorkBacklog keeps durable history/repair truth explicit without projecting
// every historical row as a current relationship. Category counts are
// intentionally non-exclusive.
type WorkBacklog struct {
	Total             int `json:"total"`
	QueuedAttention   int `json:"queued_attention"`
	HistoricalResults int `json:"historical_results"`
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

type presentationDatabase struct {
	SchemaVersion        int                   `json:"schema_version"`
	NextEventSequence    uint64                `json:"next_event_sequence"`
	BrainInputAdmissions []BrainInputAdmission `json:"brain_input_admissions"`
	HostForegroundTurn   *HostForegroundTurn   `json:"host_foreground_turn,omitempty"`
	BrainWork            []Work                `json:"brain_work"`
	BrainWorkEvents      []WorkEvent           `json:"brain_work_events"`
	BrainTurns           []TurnRecord          `json:"brain_turns"`
}

// workRecord is the on-disk Work shape during decode. Unknown never-released
// fields are ignored. SourceThreadID is required after Create/bind/persist.
type workRecord struct {
	ID               string           `json:"work_id"`
	Revision         uint64           `json:"revision"`
	TerminalRevision uint64           `json:"terminal_revision,omitempty"`
	Title            string           `json:"title"`
	Objective        string           `json:"objective"`
	Status           WorkStatus       `json:"status"`
	AttemptSessionID string           `json:"attempt_session_id,omitempty"`
	AttemptDelegated bool             `json:"attempt_delegated,omitempty"`
	SourceThreadID   string           `json:"source_thread_id,omitempty"`
	CompletionPolicy CompletionPolicy `json:"completion_policy"`
	DoneCriteriaRef  string           `json:"done_criteria_ref,omitempty"`
	NextAction       string           `json:"next_action,omitempty"`
	WaitFor          string           `json:"wait_for,omitempty"`
	Wake             *WorkWake        `json:"wake,omitempty"`
	Review           *WorkReview      `json:"review,omitempty"`
	ContextRef       string           `json:"context_ref,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

type presentationDatabaseRecord struct {
	SchemaVersion        int                   `json:"schema_version"`
	NextEventSequence    uint64                `json:"next_event_sequence"`
	BrainInputAdmissions []BrainInputAdmission `json:"brain_input_admissions"`
	HostForegroundTurn   *HostForegroundTurn   `json:"host_foreground_turn,omitempty"`
	BrainWork            []workRecord          `json:"brain_work"`
	BrainWorkEvents      []WorkEvent           `json:"brain_work_events"`
	BrainTurns           []TurnRecord          `json:"brain_turns"`
}

func (s *Store) presentationPath() string {
	return s.statePath() + string(os.PathSeparator) + "presentation.json"
}

func (s *Store) ensurePresentationDatabase() error {
	raw, err := os.ReadFile(s.presentationPath())
	if errors.Is(err, os.ErrNotExist) {
		return s.persistPresentationLocked(presentationDatabase{
			SchemaVersion:        presentationSchemaVersion,
			BrainInputAdmissions: []BrainInputAdmission{},
			BrainWork:            []Work{},
			BrainWorkEvents:      []WorkEvent{},
			BrainTurns:           []TurnRecord{},
		})
	}
	if err != nil {
		return err
	}
	if _, err := decodePresentationDatabase(raw); err != nil {
		return fmt.Errorf("decode Brain presentation database: %w", err)
	}
	return nil
}

// decodePresentationDatabase accepts only the current projection schema.
// Older scheduler documents must reset; legacy fact rows are never promoted
// back into writable lifecycle authority.
func decodePresentationDatabase(raw []byte) (presentationDatabase, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return presentationDatabase{}, fmt.Errorf("document must be a JSON object")
	}
	var header struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(trimmed, &header); err != nil {
		return presentationDatabase{}, err
	}
	if header.SchemaVersion == nil {
		return presentationDatabase{}, fmt.Errorf("schema_version is required")
	}
	version := *header.SchemaVersion
	if version != presentationSchemaVersion {
		return presentationDatabase{}, fmt.Errorf(
			"%w: schema_version %d (expected %d)",
			ErrSchedulerStateReset,
			version,
			presentationSchemaVersion,
		)
	}
	var record presentationDatabaseRecord
	if err := json.Unmarshal(trimmed, &record); err != nil {
		return presentationDatabase{}, err
	}
	if record.BrainWork == nil || record.BrainWorkEvents == nil || record.BrainInputAdmissions == nil {
		return presentationDatabase{}, fmt.Errorf("brain_input_admissions, brain_work, and brain_work_events are required arrays")
	}
	database := presentationDatabase{
		SchemaVersion:        presentationSchemaVersion,
		NextEventSequence:    record.NextEventSequence,
		HostForegroundTurn:   record.HostForegroundTurn,
		BrainWork:            worksFromRecords(record.BrainWork),
		BrainWorkEvents:      record.BrainWorkEvents,
		BrainTurns:           record.BrainTurns,
		BrainInputAdmissions: record.BrainInputAdmissions,
	}
	if err := validatePresentationDatabase(database); err != nil {
		return presentationDatabase{}, err
	}
	return database, nil
}

// worksFromRecords copies durable Work fields.
func worksFromRecords(records []workRecord) []Work {
	out := make([]Work, 0, len(records))
	for _, record := range records {
		out = append(out, Work{
			ID:               strings.TrimSpace(record.ID),
			Revision:         record.Revision,
			TerminalRevision: record.TerminalRevision,
			Title:            strings.TrimSpace(record.Title),
			Objective:        strings.TrimSpace(record.Objective),
			Status:           record.Status,
			AttemptSessionID: strings.TrimSpace(record.AttemptSessionID),
			AttemptDelegated: record.AttemptDelegated,
			SourceThreadID:   strings.TrimSpace(record.SourceThreadID),
			CompletionPolicy: record.CompletionPolicy,
			DoneCriteriaRef:  strings.TrimSpace(record.DoneCriteriaRef),
			NextAction:       strings.TrimSpace(record.NextAction),
			WaitFor:          strings.TrimSpace(record.WaitFor),
			Wake:             cloneWorkWake(record.Wake),
			Review:           cloneWorkReview(record.Review),
			ContextRef:       strings.TrimSpace(record.ContextRef),
			CreatedAt:        record.CreatedAt,
			UpdatedAt:        record.UpdatedAt,
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
	if wake.NextAttemptAt != nil {
		next := wake.NextAttemptAt.UTC()
		copy.NextAttemptAt = &next
	}
	return &copy
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

func validatePresentationDatabase(database presentationDatabase) error {
	return validatePresentationDatabaseWithSourceThread(database, true)
}

func validatePresentationDatabaseWithSourceThread(database presentationDatabase, requireSourceThread bool) error {
	if err := validateBrainInputAdmissions(database.BrainInputAdmissions); err != nil {
		return err
	}
	workIDs := make(map[string]struct{}, len(database.BrainWork))
	activeAttemptSessions := make(map[string]string, len(database.BrainWork))
	for index, item := range database.BrainWork {
		if err := validateWorkWithSourceThread(item, requireSourceThread); err != nil {
			return fmt.Errorf("brain_work[%d]: %w", index, err)
		}
		if _, exists := workIDs[item.ID]; exists {
			return fmt.Errorf("brain_work[%d]: duplicate work_id %q", index, item.ID)
		}
		workIDs[item.ID] = struct{}{}
		attemptSessionID := strings.TrimSpace(item.AttemptSessionID)
		if attemptSessionID != "" && item.Status != WorkDone && item.Status != WorkCancelled {
			if existingID := activeAttemptSessions[attemptSessionID]; existingID != "" {
				return fmt.Errorf(
					"brain_work[%d]: attempt_session_id %q already owns active Work %q",
					index,
					attemptSessionID,
					existingID,
				)
			}
			activeAttemptSessions[attemptSessionID] = item.ID
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
	}
	for index, item := range database.BrainWork {
		if err := validateWorkReview(database, item); err != nil {
			return fmt.Errorf("brain_work[%d]: %w", index, err)
		}
		if review := item.Review; review != nil && review.Lease != nil && review.Lease.HandlingEndedAt == nil {
			if existingID := inFlightByWork[item.ID]; existingID != "" {
				return fmt.Errorf("brain_work[%d]: Work %q already has an in-flight review lease", index, item.ID)
			}
			inFlightByWork[item.ID] = review.EventID
			if review.Lease.DeliveredAt != nil {
				if globalDelivered != "" {
					return fmt.Errorf("brain_work[%d]: Host already has live delivered review %q", index, globalDelivered)
				}
				globalDelivered = item.ID
			}
		}
	}
	for index, item := range database.BrainWork {
		if err := validateWorkSignalState(database, item); err != nil {
			return fmt.Errorf("brain_work[%d]: %w", index, err)
		}
	}
	if err := validateTurnLedger(database.BrainTurns, workIDs); err != nil {
		return err
	}
	if active := database.HostForegroundTurn; active != nil {
		if strings.TrimSpace(active.HostSessionID) == "" || strings.TrimSpace(active.HostGeneration) == "" ||
			strings.TrimSpace(active.HostTurnID) == "" || active.StartedAt.IsZero() {
			return fmt.Errorf("host_foreground_turn requires host session, generation, turn, and started_at")
		}
	}
	if err := validateActiveAttempts(database); err != nil {
		return err
	}
	return nil
}

func validateActiveAttempts(database presentationDatabase) error {
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
		attemptSessionID := strings.TrimSpace(item.AttemptSessionID)
		if turn.SessionID != attemptSessionID {
			state := reduceWorkProgressState(database, item)
			// The canonical Turn reducer may explicitly relinquish a blocked or
			// stale Attempt while retaining its exact Turn as lifecycle evidence.
			// Ready attention, a typed wait, or another canonical Attempt
			// then owns progress; exact continue may promote this same active
			// Session again.
			relinquished := workTurnHasRelinquishmentEvidence(database, item.ID, turn)
			if !relinquished && !state.Ready && (attemptSessionID != "" || !state.Waiting) {
				return fmt.Errorf("brain_turns: active Session %q is not the active Attempt of Work %q", turn.SessionID, item.ID)
			}
			// Once ready attention, a typed wait, or explicit Session-result
			// evidence has replaced execution authority, another Turn is durable
			// lifecycle evidence only. An unrelated Turn beside an active Attempt still
			// fails above unless its own result Event proves relinquishment.
			continue
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

func workTurnHasRelinquishmentEvidence(database presentationDatabase, workID string, turn TurnRecord) bool {
	workID = strings.TrimSpace(workID)
	sessionID := strings.TrimSpace(turn.SessionID)
	if workID == "" || sessionID == "" {
		return false
	}
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && strings.TrimSpace(event.SourceName) == sessionID &&
			isProjectedWorkResultEvent(event.Kind) &&
			strings.TrimSpace(event.DedupeKey) == sessionTurnEventDedupeKey(sessionID, turn.TurnID, event.Kind) {
			return true
		}
	}
	// A delegated Session is reusable. Once that same Session has a later
	// accepted Turn for the Work, the older nonterminal row is historical
	// lifecycle evidence even if the provider never emitted a terminal fact for
	// it. The exact newer admission is sufficient: a result from merely an older
	// Turn must not relinquish a later rogue Turn.
	for _, candidate := range database.BrainTurns {
		if candidate.WorkID == workID &&
			strings.TrimSpace(candidate.SessionID) == sessionID &&
			candidate.TurnID != turn.TurnID && candidate.AcceptedAt.After(turn.AcceptedAt) {
			return true
		}
	}
	return false
}

func isHostHandlingTurn(database presentationDatabase, turn TurnRecord) bool {
	if turn.HostHandling {
		return true
	}
	// A Host handling Turn's Receipt names the review-event fact it delivered.
	// The receipt identity is durable even after the lease ends or is
	// replaced, so a Host Turn is never misread as delegated execution.
	fact, found := workEventByID(database.BrainWorkEvents, turn.Receipt)
	if !found || fact.WorkID != turn.WorkID {
		return false
	}
	itemIndex := workIndex(database.BrainWork, turn.WorkID)
	if itemIndex < 0 {
		return false
	}
	review := database.BrainWork[itemIndex].Review
	if review != nil && review.Lease != nil && review.EventID == turn.Receipt &&
		review.Lease.HostSessionID == turn.SessionID && review.Lease.ProviderTurnID == turn.TurnID {
		return true
	}
	// The fact row carries no claim state (I1); a Turn whose Receipt names a
	// fact and whose session ever acted as the delivery host is Host-side
	// lifecycle. ClaimToken-bearing submissions are Host submissions.
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
	if event.HandledAt != nil {
		if !validWorkDisposition(event.Disposition) {
			return fmt.Errorf("handled event requires a disposition")
		}
	}
	if event.Disposition != "" && event.HandledAt == nil {
		return fmt.Errorf("disposition requires handled_at")
	}
	if event.Resolution != "" {
		if event.ResolvedBy == "" || event.ResolvedAt == nil {
			return fmt.Errorf("resolved event requires resolved_by and resolved_at")
		}
	}
	if event.DiscardedAt != nil {
		if event.Resolution != EventResolutionDiscard || event.ResolvedAt == nil {
			return fmt.Errorf("discarded event requires discard resolution audit")
		}
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
		if wake.NextAttemptAt != nil {
			return fmt.Errorf("%s wake cannot set next_attempt_at", wake.Kind)
		}
		return nil
	case WorkWakeDueRetry:
		if wake.NextAttemptAt == nil || wake.NextAttemptAt.IsZero() {
			return fmt.Errorf("due_retry wake requires next_attempt_at")
		}
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

func validateWorkWakeProducer(database presentationDatabase, item Work, wake *WorkWake, now time.Time) error {
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
	case WorkWakeDueRetry:
		if !wake.NextAttemptAt.After(now) {
			return fmt.Errorf("due_retry next_attempt_at must be in the future")
		}
	}
	return nil
}

func workWakeEqual(left, right *WorkWake) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Kind != right.Kind || strings.TrimSpace(left.Ref) != strings.TrimSpace(right.Ref) {
		return false
	}
	if left.NextAttemptAt == nil || right.NextAttemptAt == nil {
		return left.NextAttemptAt == nil && right.NextAttemptAt == nil
	}
	return left.NextAttemptAt.Equal(*right.NextAttemptAt)
}

func validWorkDisposition(disposition WorkDisposition) bool {
	switch disposition {
	case WorkDispositionContinue, WorkDispositionWait, WorkDispositionComplete, WorkDispositionCancel:
		return true
	default:
		return false
	}
}

func validateWorkSignalState(database presentationDatabase, item Work) error {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return nil
	}
	// Silent nonterminal Work (created before its first signal, or between
	// the Work row and its appended Event) is legal. What is unrepresentable
	// is dual progress authority: a typed wait beside a live canonical owner
	// or pending owner admission.
	state := reduceWorkProgressState(database, item)
	if state.Owned && state.Waiting {
		return fmt.Errorf("nonterminal Work cannot be simultaneously owned and waiting")
	}
	return nil
}

type workProgressState struct {
	Owned             bool
	Waiting           bool
	Ready             bool
	LiveCanonicalTurn bool
}

// reviewDeliveryState is the derived delivery stage of the canonical review
// obligation: pending (claimable), leased (claimed, delivery in flight),
// delivered (awaiting the exact typed disposition), or quarantined (mutation
// evidence while the lease Host is gone; explicit actor resolution only).
type reviewDeliveryState uint8

const (
	reviewNone reviewDeliveryState = iota
	reviewPending
	reviewLeased
	reviewDelivered
	reviewQuarantined
)

func reduceWorkReviewState(database presentationDatabase, workID string) reviewDeliveryState {
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		return reviewNone
	}
	review := database.BrainWork[itemIndex].Review
	if review == nil {
		return reviewNone
	}
	lease := review.Lease
	if lease == nil {
		return reviewPending
	}
	if lease.AmbiguousDelivery {
		return reviewQuarantined
	}
	if lease.HandlingEndedAt != nil {
		// Ended without a disposition: the same unresolved action is
		// re-claimable (rows 9-10).
		return reviewPending
	}
	if lease.DeliveredAt != nil {
		return reviewDelivered
	}
	return reviewLeased
}

func workHasActiveCanonicalAttempt(database presentationDatabase, item Work) bool {
	attemptSessionID := strings.TrimSpace(item.AttemptSessionID)
	if attemptSessionID == "" {
		return false
	}
	turn, found := currentTurnForSession(database, attemptSessionID)
	if !found || turn.WorkID != item.ID || watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(database, turn) {
		return false
	}
	return true
}

// reduceWorkProgressState derives the three progress predicates independently.
// AttemptSessionID text is not authority: only its current canonical nonterminal
// execution Turn or its exact pending initial submission carries progress. A
// bare Session string has no authority.
func reduceWorkProgressState(database presentationDatabase, item Work) workProgressState {
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return workProgressState{}
	}
	activeAttempt := workHasActiveCanonicalAttempt(database, item)
	hasReview := item.Review != nil
	state := workProgressState{
		Owned:             activeAttempt,
		Waiting:           item.Wake != nil,
		Ready:             hasReview,
		LiveCanonicalTurn: activeAttempt,
	}
	return state
}

func deriveWorkProgressMode(database presentationDatabase, item Work) (WorkProgressMode, error) {
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

func mustDeriveWorkProgressMode(database presentationDatabase, item Work) WorkProgressMode {
	mode, _ := deriveWorkProgressMode(database, item)
	return mode
}

// workHasReviewObligation reports the canonical review obligation regardless
// of delivery stage. It drives the wire AttentionPending flag only; it never
// gates scheduling. A delivered review awaiting its typed disposition is the
// Host lane's stop gate; worker-side correction admissions stage under that
// exact handling instead of being rejected, and a pending review never gates
// either path.
func workHasReviewObligation(database presentationDatabase, workID string) bool {
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		return false
	}
	return database.BrainWork[itemIndex].Review != nil
}

// workHasDeliveredReview reports whether Work is currently owned by an exact
// delivered review awaiting its typed disposition.
func workHasDeliveredReview(database presentationDatabase, workID string) bool {
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		return false
	}
	return reviewDeliveredAwaitingDisposition(database.BrainWork[itemIndex].Review)
}

// WorkHasDeliveredReview reports whether Work is currently owned by an exact
// delivered review handling. Admission preparation performs the authoritative
// capability check at the lifecycle transaction boundary.
func (s *Store) WorkHasDeliveredReview(workID string) (bool, error) {
	workID = strings.TrimSpace(workID)
	if s == nil || workID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return false, err
	}
	return workHasDeliveredReview(database, workID), nil
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

func (s *Store) loadPresentationLocked() (presentationDatabase, error) {
	raw, err := os.ReadFile(s.presentationPath())
	if err != nil {
		return presentationDatabase{}, err
	}
	return decodePresentationDatabase(raw)
}

func (s *Store) persistPresentationLocked(database presentationDatabase) error {
	database.SchemaVersion = presentationSchemaVersion
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
	if err := validatePresentationDatabase(database); err != nil {
		return err
	}
	return s.writePresentation(s.presentationPath(), database)
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
	// Ownership is admitted only by Lifecycle.AdmitTurn. Caller-provided owner
	// fields are caller input and never enter the canonical projection.
	item.AttemptSessionID = ""
	item.AttemptDelegated = false
	item.SourceThreadID = strings.TrimSpace(item.SourceThreadID)
	item.DoneCriteriaRef = strings.TrimSpace(item.DoneCriteriaRef)
	item.NextAction = strings.TrimSpace(item.NextAction)
	item.WaitFor = strings.TrimSpace(item.WaitFor)
	item.Wake = cloneWorkWake(item.Wake)
	item.ContextRef = strings.TrimSpace(item.ContextRef)
	// Definition creates an unowned aggregate. All lifecycle status changes,
	// including the first running state, are projections of Lifecycle commands.
	item.Status = WorkOpen
	if item.CompletionPolicy == "" {
		item.CompletionPolicy = CompletionBounded
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	item.Revision = 1
	item.TerminalRevision = 0
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
	database, err := s.loadPresentationLocked()
	if err == nil {
		for _, current := range database.BrainWork {
			if current.ID == item.ID {
				err = ErrWorkConflict
				break
			}
		}
	}
	if err == nil {
		// Canonical aggregate first: the engine is the only writer of
		// delegated-Work lifecycle state. The lifecycle row is a derived
		// read model refreshed from engine state.
		if err = s.fsmDefine(item); err == nil {
			database.BrainWork = append(database.BrainWork, item)
			err = s.persistPresentationLocked(database)
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
	database, err := s.loadPresentationLocked()
	if err == nil {
		for _, current := range database.BrainWork {
			if current.ID == item.ID {
				s.mu.Unlock()
				return current, false, nil
			}
		}
		if err = s.fsmDefine(item); err == nil {
			database.BrainWork = append(database.BrainWork, item)
			err = s.persistPresentationLocked(database)
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
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
	var item Work
	if err == nil {
		index := workIndex(database.BrainWork, id)
		if index < 0 {
			err = ErrWorkNotFound
		} else {
			now := s.nowUTC()
			item, err = s.applyWorkUpdateViaFSMLocked(&database, index, update, now)
			if err == nil {
				err = s.persistPresentationLocked(database)
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

// applyWorkUpdateViaFSMLocked routes operator updates through the canonical
// engine. Lifecycle fields (status/owner/wait) are engine commands; prose
// fields are amendments. Hand-set owners are unrepresentable: ownership is
// admitted only through canonical turn admission.
func (s *Store) applyWorkUpdateViaFSMLocked(database *presentationDatabase, index int, update WorkUpdate, now time.Time) (Work, error) {
	item := database.BrainWork[index]
	if eventID, owned := activeHostLaneEvent(*database, item.ID); owned {
		// The delivered review capability froze its exact revision fence:
		// neither metadata nor terminal transitions may advance the aggregate
		// while that handling is in flight (I4).
		return Work{}, fmt.Errorf("%w: Event %s still owns the Host lane", ErrWorkConflict, eventID)
	}
	if item.Status == WorkDone || item.Status == WorkCancelled {
		if update.Status != nil && *update.Status != item.Status {
			return Work{}, fmt.Errorf("%w: terminal Work cannot be reopened", ErrWorkConflict)
		}
	}
	if update.AttemptSessionID != nil && strings.TrimSpace(*update.AttemptSessionID) != "" {
		return Work{}, fmt.Errorf("%w: Attempt Sessions are admitted only through canonical turn admission", ErrWorkAttemptConflict)
	}

	// Status transitions.
	if update.Status != nil {
		switch *update.Status {
		case WorkCancelled:

			if _, err := s.fsm.Cancel(lifecycle.WorkID(item.ID), 0, "operator", "work update"); err != nil {
				return Work{}, err
			}
		case WorkWaiting:
			var wake *WorkWake
			if update.Wake != nil {
				wake = cloneWorkWake(*update.Wake)
			}
			waitFor := ""
			if update.WaitFor != nil {
				waitFor = *update.WaitFor
			}
			if wake == nil {
				wake = &WorkWake{Kind: WorkWakeUserInput, Ref: firstNonEmpty(waitFor, "operator:"+item.ID)}
			}
			if _, err := s.fsm.SetWait(lifecycle.WorkID(item.ID), lifecycle.WakeKind(wake.Kind), wake.Ref); err != nil {
				return Work{}, err
			}
		case WorkDone:
			if _, err := s.fsm.Complete(lifecycle.WorkID(item.ID), 0, "operator", "work update"); err != nil {
				return Work{}, err
			}
		default:
			return Work{}, fmt.Errorf("%w: status %q is derived; only done/cancelled/waiting are operator-settable", ErrWorkConflict, string(*update.Status))
		}
	} else if update.Wake != nil && *update.Wake != nil {
		wake := cloneWorkWake(*update.Wake)
		if _, err := s.fsm.SetWait(lifecycle.WorkID(item.ID), lifecycle.WakeKind(wake.Kind), wake.Ref); err != nil {
			return Work{}, err
		}
	} else if update.Wake != nil && *update.Wake == nil {
		st, err := s.fsmState(item.ID)
		if err != nil {
			return Work{}, err
		}
		if st.Wake != nil {
			if _, err := s.fsm.ClearWait(lifecycle.WorkID(item.ID), st.Wake.Kind, st.Wake.Ref, "operator"); err != nil {
				return Work{}, err
			}
		}
	}

	// Prose amendments. Terminal aggregates accept no further events.
	title, objective, dcr, nextAction := update.Title, update.Objective, update.DoneCriteriaRef, update.NextAction
	if title != nil || objective != nil || dcr != nil || nextAction != nil {
		if _, err := s.fsm.Amend(lifecycle.WorkID(item.ID), 0, title, objective, dcr, nextAction); err != nil && err != lifecycle.ErrTerminal {
			return Work{}, err
		}
	}

	if err := s.fsmSyncWorkLocked(database, item.ID, now); err != nil {
		return Work{}, err
	}
	updated := database.BrainWork[index]
	// ContextRef remains presentation metadata on the read model.
	if update.ContextRef != nil {
		updated.ContextRef = *update.ContextRef
		database.BrainWork[index] = updated
	}
	return updated, nil
}

// CloseWork terminalizes one exact current Work revision without routing an
// artificial reconciliation turn through the Host. A live Host claim remains
// the one fail-closed boundary. Pending delegated provider submissions are
// subordinate to the explicit
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
	database, err := s.loadPresentationLocked()
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
	// Snapshot any projected review event before canonical close: the engine
	// clears it, and the audit row below still needs its settlement marking.
	reviewBeforeClose := item.Review
	// Canonical terminalization: the engine is the only writer of Work status.
	// The operator CAS above already pinned the exact projected revision, so
	// the engine command carries no second revision pin (the projected
	// revision and the canonical event count are different sequences).
	var fsmErr error
	if request.Status == WorkDone {
		_, fsmErr = s.fsm.Complete(lifecycle.WorkID(item.ID), 0, request.Actor, request.Reason)
	} else {
		_, fsmErr = s.fsm.Cancel(lifecycle.WorkID(item.ID), 0, request.Actor, request.Reason)
	}
	if fsmErr != nil {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("%w: canonical close rejected: %v", ErrWorkCloseConflict, fsmErr)
	}
	if err := s.fsmSyncWorkLocked(&database, item.ID, now); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	item = database.BrainWork[itemIndex]
	settledAt := now.UTC()
	terminalDisposition := WorkDispositionCancel
	if request.Status == WorkDone {
		terminalDisposition = WorkDispositionComplete
	}
	// The operator's exact Work revision supplies the terminal disposition for
	// the review event (ended or undelivered) and the canonical review
	// obligation is cleared atomically (row 15).
	if review := reviewBeforeClose; review != nil {
		eventIndex := workEventIndex(database.BrainWorkEvents, review.EventID)
		if eventIndex >= 0 {
			event := &database.BrainWorkEvents[eventIndex]
			if event.HandledAt == nil && event.DiscardedAt == nil && event.Resolution == "" {
				event.HandledAt = &settledAt
				event.Disposition = terminalDisposition
				event.DispositionSummary = request.Reason
			}
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
	if err := s.persistPresentationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	item = database.BrainWork[itemIndex]
	s.mu.Unlock()
	s.broadcastWorkChange(item.ID)
	return item, nil
}

// activeHostLaneEvent identifies the sole fail-closed boundary shared by
// operator closure and internal Work updates. Once a review lease is in
// flight, neither a metadata producer nor an operator may advance the Work
// revision until the exact Host capability is consumed and disposed (or the
// ended handling is explicitly recovered).
func activeHostLaneEvent(database presentationDatabase, workID string) (string, bool) {
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		return "", false
	}
	review := database.BrainWork[itemIndex].Review
	if review == nil || review.Lease == nil {
		return "", false
	}
	if review.Lease.HandlingEndedAt == nil {
		return review.EventID, true
	}
	return "", false
}

func workHostLaneOwned(database presentationDatabase, workID string) bool {
	_, owned := activeHostLaneEvent(database, workID)
	return owned
}

func workIndex(items []Work, id string) int {
	for index := range items {
		if items[index].ID == id {
			return index
		}
	}
	return -1
}

func (s *Store) WorkByAttemptSession(sessionID string) (Work, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return Work{}, false, err
	}
	for _, item := range database.BrainWork {
		if item.AttemptSessionID == sessionID && item.Status != WorkDone && item.Status != WorkCancelled {
			return item, true, nil
		}
	}
	// A canonical attention transition relinquishes progress ownership but
	// retains the exact Turn as lifecycle/finalization evidence. Preserve this
	// lookup contract for callers reconciling that Session without restoring
	// AttemptSessionID or treating it as a second progress owner.
	if turn, found := currentTurnForSession(database, sessionID); found {
		if index := workIndex(database.BrainWork, turn.WorkID); index >= 0 {
			item := database.BrainWork[index]
			state := reduceWorkProgressState(database, item)
			if strings.TrimSpace(item.AttemptSessionID) == "" &&
				item.Status != WorkDone && item.Status != WorkCancelled &&
				(state.Ready || state.Waiting) {
				return item, true, nil
			}
		}
	}
	return Work{}, false, nil
}

func databaseActiveWorkIDForExecutionSession(database presentationDatabase, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	for _, item := range database.BrainWork {
		if item.AttemptSessionID == sessionID && item.Status != WorkDone && item.Status != WorkCancelled &&
			workHasActiveCanonicalAttempt(database, item) {
			return item.ID
		}
	}
	return ""
}

func (s *Store) WorkByContextRef(contextRef string) (Work, bool, error) {
	contextRef = strings.TrimSpace(contextRef)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
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
//
// One exception is the retry contract: an admission durably settled as
// NotSubmitted (the provider provably never mutated) is the same logical input
// retried. It is re-armed in place (same request id, same payload identity)
// with the caller's current host generation so the retry may cross the
// mutation boundary again. Accepted and Uncertain are monotonic and are never
// re-armed; a different payload with the same identity still fails closed.
func (s *Store) PrepareBrainInputAdmission(candidate BrainInputAdmission) (BrainInputAdmission, bool, error) {
	var err error
	candidate, err = normalizeBrainInputAdmission(candidate, s.nowUTC())
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	if index := brainInputAdmissionIndex(database.BrainInputAdmissions, candidate.RequestID, candidate.ThreadID); index >= 0 {
		existing := database.BrainInputAdmissions[index]
		samePayload := existing.RequestID == candidate.RequestID && existing.ThreadID == candidate.ThreadID &&
			existing.HostSessionID == candidate.HostSessionID && existing.SessionID == candidate.SessionID &&
			existing.DisplayBody == candidate.DisplayBody && existing.BodySHA256 == candidate.BodySHA256
		if !samePayload {
			return BrainInputAdmission{}, false, fmt.Errorf("Brain input admission identity belongs to different input")
		}
		if existing.State == BrainInputAdmissionNotSubmitted {
			// Re-arm the exact never-mutated intent. Host generation/turn are
			// ambient session identity, not payload identity: the retry adopts
			// the caller's current generation so a replaced host pane does not
			// permanently strand the same logical input.
			rearmed := existing
			rearmed.State = BrainInputAdmissionPending
			rearmed.SettledAt = nil
			rearmed.HostGeneration = candidate.HostGeneration
			rearmed.HostTurnID = candidate.HostTurnID
			if candidate.HostGeneration != "" {
				if active := database.HostForegroundTurn; active != nil &&
					active.HostSessionID == candidate.HostSessionID && active.HostGeneration == candidate.HostGeneration {
					rearmed.HostTurnID = active.HostTurnID
				}
			}
			database.BrainInputAdmissions[index] = rearmed
			if err := s.persistPresentationLocked(database); err != nil {
				return BrainInputAdmission{}, false, err
			}
			return rearmed, true, nil
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
	if err := s.persistPresentationLocked(database); err != nil {
		return BrainInputAdmission{}, false, err
	}
	return candidate, true, nil
}

// AcceptBrainInputAdmission commits provider acceptance and all matching
// user_input Attentions in one lifecycle replacement. If an older direct
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
	database, err := s.loadPresentationLocked()
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
				HostTurnID: turnID, ProviderActivityID: candidate.ProviderActivityID, StartedAt: prepared.CreatedAt.UTC(),
			}
		}
		if database.HostForegroundTurn.ProviderActivityID == "" {
			database.HostForegroundTurn.ProviderActivityID = candidate.ProviderActivityID
		}
		candidate.HostTurnID = turnID
	}
	database.BrainInputAdmissions[index] = candidate
	woken, changedIDs, err := s.wakeWaitingWorkLocked(
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
	if err := s.persistPresentationLocked(database); err != nil {
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
	database, err := s.loadPresentationLocked()
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
	return s.persistPresentationLocked(database)
}

// PendingBrainInputAdmissions returns the durable user-input intents whose
// provider mutation outcome still needs exact receipt reconciliation.
func (s *Store) PendingBrainInputAdmissions() ([]BrainInputAdmission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
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
					StartedAt:          admission.CreatedAt.UTC(),
				}
			}
		}
		_, changedWorkIDs, err = s.wakeWaitingWorkLocked(
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
	if err := s.persistPresentationLocked(database); err != nil {
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
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, false, err
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
	database, err := s.loadPresentationLocked()
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
			if syncErr := s.fsmSyncWorkLocked(&database, item.ID, event.CreatedAt); syncErr == nil {
				event.WorkRevision = database.BrainWork[itemIndex].Revision
			}
		}
		if err == nil {
			err = s.persistPresentationLocked(database)
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
	event.HandledAt = nil
	event.Disposition = ""
	event.DispositionSummary = ""
	event.CoalescedInto = ""
	event.Resolution = ""
	event.ResolvedBy = ""
	event.ResolvedAt = nil
	event.DiscardedAt = nil
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
// wake in one lifecycle replacement. The producer dedupe key is the
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
	database, err := s.loadPresentationLocked()
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
		if err := s.fsmDefine(normalizedCandidate); err != nil {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, false, nil, err
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
		item, err = s.applyWorkUpdateViaFSMLocked(&database, itemIndex, *update, now)
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
		woken, wakeChanged, err = s.wakeWaitingWorkLocked(
			&database, *wake, event.Kind, occurrenceID, wakeSummary, now,
		)
		if err != nil {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, false, nil, err
		}
		changedIDs = append(changedIDs, wakeChanged...)
	}
	if err := s.persistPresentationLocked(database); err != nil {
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

func appendWorkEventLocked(database *presentationDatabase, itemIndex int, event WorkEvent, bumpRevision bool) (WorkEvent, error) {
	if database == nil || itemIndex < 0 || itemIndex >= len(database.BrainWork) {
		return WorkEvent{}, ErrWorkNotFound
	}
	item := &database.BrainWork[itemIndex]
	hostLaneOwned := workHostLaneOwned(*database, item.ID)
	if bumpRevision && !hostLaneOwned {
		item.Revision++
		item.UpdatedAt = event.CreatedAt.UTC()
	}
	database.NextEventSequence++
	event.Sequence = database.NextEventSequence
	event.WorkRevision = item.Revision
	if hostLaneOwned {
		event.WorkRevision++
	}
	if err := validateWorkEvent(event); err != nil {
		return WorkEvent{}, err
	}
	database.BrainWorkEvents = append(database.BrainWorkEvents, event)
	return event, nil
}

func (s *Store) wakeWaitingWorkLocked(database *presentationDatabase, wake WorkWake, kind, occurrenceID, summary string, now time.Time) ([]WorkEvent, []string, error) {
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
		if _, err := s.fsm.ClearWait(lifecycle.WorkID(item.ID), lifecycle.WakeKind(wake.Kind), wake.Ref, occurrenceID); err != nil {
			return nil, nil, err
		}
		var err error
		event, err = appendWorkEventLocked(database, index, event, true)
		if err != nil {
			return nil, nil, err
		}
		if err := s.fsmSyncWorkLocked(database, item.ID, now); err != nil {
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
	database, err := s.loadPresentationLocked()
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

// ClaimNextReviewAction claims the next review-required Work for the Host:
// the oldest event birth (RequiredAt) with no in-flight lease and no
// quarantine. Claiming mints a disposable lease carrying the exact capability
// and the frozen Work revision fence. The same unresolved action is
// re-claimable after lease expiry (Host death or ended delivery), and never
// creates a second queue item.
func (s *Store) ClaimNextReviewAction(hostSessionID string) (WorkReviewAction, bool, error) {
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" {
		return WorkReviewAction{}, false, fmt.Errorf("delivery host Session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return WorkReviewAction{}, false, err
	}
	// Canonical claim: the engine owns handler leases. Oldest open event
	// without a handler wins; fairness is a property of Work.
	handlerToken := hostSessionID + ":turn:" + uuid.NewString()
	type candidate struct {
		id       string
		openedAt time.Time
	}
	candidates := []candidate{}
	for _, st := range s.fsm.ListStates() {
		if st.Review == nil || st.Review.Handler != nil || st.Status.Terminal() {
			continue
		}
		candidates = append(candidates, candidate{id: string(st.ID), openedAt: st.Review.OpenedAt})
	}
	if len(candidates) == 0 {
		return WorkReviewAction{}, false, nil
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].openedAt.Equal(candidates[right].openedAt) {
			return candidates[left].id < candidates[right].id
		}
		return candidates[left].openedAt.Before(candidates[right].openedAt)
	})
	var claimed *lifecycle.State
	for _, c := range candidates {
		st, err := s.fsm.ClaimReview(lifecycle.WorkID(c.id), hostSessionID, lifecycle.TurnToken(handlerToken))
		if err != nil {
			continue // lost a race or event closed; try next
		}
		claimed = st
		break
	}
	if claimed == nil {
		return WorkReviewAction{}, false, nil
	}
	itemIndex := workIndex(database.BrainWork, string(claimed.ID))
	if itemIndex < 0 {
		return WorkReviewAction{}, false, ErrWorkNotFound
	}
	if err := s.fsmSyncWorkLocked(&database, string(claimed.ID), s.nowUTC()); err != nil {
		return WorkReviewAction{}, false, err
	}
	item := &database.BrainWork[itemIndex]
	if err := s.persistPresentationLocked(database); err != nil {
		return WorkReviewAction{}, false, err
	}
	action, found := reviewActionFromReview(database, item.Review)
	return action, found, nil
}

// HasLiveDeliveredReview reports whether one delivered review still awaits its
// exact typed disposition. The Host lane stops while it is true: the Host is
// mid-review and no new admission may overtake the disposition.
func (s *Store) HasLiveDeliveredReview() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return false, err
	}
	return reviewDeliveredInFlightIndex(database) >= 0, nil
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
	database, err := s.loadPresentationLocked()
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
	return s.persistPresentationLocked(database)
}

// CurrentHostForegroundTurn returns the one durable foreground response event,
// or nil when the Host lane is idle. The reducer uses it to recover the same
// admission boundary after daemon reopen; the record itself never authorizes
// replay and is closed only by strong exact terminal evidence.
func (s *Store) CurrentHostForegroundTurn() (*HostForegroundTurn, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
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
	if err := s.persistPresentationLocked(database); err != nil {
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
	database, err := s.loadPresentationLocked()
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
	database, err := s.loadPresentationLocked()
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
	return s.persistPresentationLocked(database)
}

// LeasedReviewActions returns every review lease currently held (claimed or
// delivered), the dispatching surface the reducer reconciles against the
// receipt ledger.
func (s *Store) LeasedReviewActions() ([]WorkReviewAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, err
	}
	out := []WorkReviewAction{}
	for _, item := range database.BrainWork {
		review := item.Review
		if review == nil || review.Lease == nil {
			continue
		}
		if action, found := reviewActionFromReview(database, review); found {
			out = append(out, action)
		}
	}
	return out, nil
}

// LiveReviewHandlings returns every delivered review lease that still owns the
// Host effect. The schema admits at most one globally, but the bounded API
// keeps startup reconciliation explicit and safe if persisted state is
// corrupt.
func (s *Store) LiveReviewHandlings(limit int) ([]WorkReviewAction, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("Host handling batch limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, false, err
	}
	out := make([]WorkReviewAction, 0, limit)
	more := false
	for _, item := range database.BrainWork {
		review := item.Review
		if !reviewDeliveredAwaitingDisposition(review) {
			continue
		}
		action, found := reviewActionFromReview(database, review)
		if !found {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		out = append(out, action)
	}
	return out, more, nil
}

// ReleaseReviewLease atomically makes the exact lease claimable again only
// when Session Input proved that provider mutation never started. If canonical
// Host preparation already persisted the exact five-part pending submission,
// that submission is aborted in this same replacement. A resolved provider
// admission is ambiguous and keeps the lease held.
func (s *Store) ReleaseReviewLease(workID, claimToken, providerTurnID string) error {
	workID = strings.TrimSpace(workID)
	claimToken = strings.TrimSpace(claimToken)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if workID == "" || claimToken == "" || providerTurnID == "" {
		return ErrEventClaim
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return err
	}
	itemIndex := reviewLeaseByCapability(database, workID, claimToken, providerTurnID)
	if itemIndex < 0 {
		return ErrEventClaim
	}
	item := &database.BrainWork[itemIndex]
	review := item.Review
	lease := review.Lease
	if lease.DeliveredAt != nil || lease.HandlingEndedAt != nil {
		return ErrEventClaim
	}
	if _, err := s.fsm.ReleaseReview(lifecycle.WorkID(workID), lifecycle.TurnToken(providerTurnID)); err != nil {
		return err
	}
	if err := s.fsmSyncWorkLocked(&database, workID, s.nowUTC()); err != nil {
		return err
	}
	return s.persistPresentationLocked(database)
}

// ConsumeReviewDelivery atomically marks the exact review lease delivered.
// The complete lease capability is the authorization boundary; delivery proof
// is the canonical handler lease itself (the Lifecycle-admission receipt check
// was removed with the fact-receipt event identity).
func (s *Store) ConsumeReviewDelivery(workID, claimToken, providerTurnID string) (WorkReviewAction, Work, error) {
	workID = strings.TrimSpace(workID)
	claimToken = strings.TrimSpace(claimToken)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if workID == "" || claimToken == "" || providerTurnID == "" {
		return WorkReviewAction{}, Work{}, ErrEventClaim
	}
	s.mu.Lock()
	database, err := s.loadPresentationLocked()
	var action WorkReviewAction
	var item Work
	if err == nil {
		itemIndex := reviewLeaseByCapability(database, workID, claimToken, providerTurnID)
		if itemIndex < 0 {
			err = ErrEventClaim
		} else {
			review := database.BrainWork[itemIndex].Review
			lease := review.Lease
			if lease.DeliveredAt != nil || lease.HandlingEndedAt != nil {
				err = ErrEventClaim
			} else if _, fsmErr := s.fsm.MarkReviewDelivered(lifecycle.WorkID(workID), lifecycle.TurnToken(providerTurnID)); fsmErr != nil {
				err = fmt.Errorf("%w: canonical delivery rejected: %v", ErrEventClaim, fsmErr)
			} else {
				if syncErr := s.fsmSyncWorkLocked(&database, workID, s.nowUTC()); syncErr != nil {
					err = syncErr
				} else {
					item = database.BrainWork[itemIndex]
					action, _ = reviewActionFromReview(database, item.Review)
					err = s.persistPresentationLocked(database)
				}
			}
		}
	}
	s.mu.Unlock()
	if err == nil && item.ID != "" {
		s.broadcastWorkChange(item.ID)
	}
	return action, item, err
}

// EndReviewDelivery ends exactly the admitted review lease without a typed
// disposition (Host turn ended or Host died after delivery). The delivered
// bytes remain permanently consumed; the unresolved Work action stays the same
// review event and becomes re-claimable — no new queue item is created (I7).
// An audit note records the ended handling.
func (s *Store) EndReviewDelivery(workID, handlingID, providerTurnID string) (WorkReviewAction, bool, error) {
	workID = strings.TrimSpace(workID)
	handlingID = strings.TrimSpace(handlingID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if workID == "" || handlingID == "" || providerTurnID == "" {
		return WorkReviewAction{}, false, nil
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return WorkReviewAction{}, false, err
	}
	itemIndex := reviewLeaseByCapability(database, workID, handlingID, providerTurnID)
	if itemIndex < 0 {
		return WorkReviewAction{}, false, nil
	}
	review := database.BrainWork[itemIndex].Review
	lease := review.Lease
	if lease.DeliveredAt == nil || lease.HandlingEndedAt != nil {
		action, _ := reviewActionFromReview(database, review)
		return action, false, nil
	}
	dedupeKey := "brain:reconcile:handling:" + lease.HandlingID
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.DedupeKey == dedupeKey {
			action, _ := reviewActionFromReview(database, review)
			return action, false, nil
		}
	}
	// Canonical: drop the handler lease so the event is claimable again.
	if _, releaseErr := s.fsm.ReleaseReview(lifecycle.WorkID(workID), lifecycle.TurnToken(providerTurnID)); releaseErr != nil {
		return WorkReviewAction{}, false, releaseErr
	}
	if syncErr := s.fsmSyncWorkLocked(&database, workID, now); syncErr != nil {
		return WorkReviewAction{}, false, syncErr
	}
	audit := WorkEvent{
		ID: uuid.NewString(), WorkID: workID, Kind: "brain.reconcile_required",
		DedupeKey: dedupeKey, PayloadRef: "work:" + workID,
		SourceName: "brain", Summary: "The previous Host turn ended without a durable disposition.",
		Actionable: false, CreatedAt: now,
	}
	if _, err := appendWorkEventLocked(&database, itemIndex, audit, true); err != nil {
		return WorkReviewAction{}, false, err
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return WorkReviewAction{}, false, err
	}
	action, _ := reviewActionFromReview(database, database.BrainWork[itemIndex].Review)
	s.broadcastWorkChange(workID)
	return action, true, nil
}

func (s *Store) WorkEvent(eventID string) (WorkEvent, bool, error) {
	eventID = strings.TrimSpace(eventID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return WorkEvent{}, false, err
	}
	index := workEventIndex(database.BrainWorkEvents, eventID)
	if index < 0 {
		return WorkEvent{}, false, nil
	}
	return database.BrainWorkEvents[index], true, nil
}

// WorkResultLifecycles derives presentation labels from canonical Work state;
// cards themselves stay immutable timeline messages. A card is active (queued
// or reviewing) iff its fact is the current review action (I7); older event
// cards are history. "Session open" requires a live delegated owner (I9).
func (s *Store) WorkResultLifecycles(eventIDs []string) (map[string]WorkResultLifecycle, error) {
	wanted := map[string]bool{}
	for _, eventID := range eventIDs {
		if eventID = strings.TrimSpace(eventID); eventID != "" {
			wanted[eventID] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
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
	out := map[string]WorkResultLifecycle{}
	for requestedID := range wanted {
		event := WorkEvent{}
		workID := strings.TrimPrefix(requestedID, "work:")
		if workID != requestedID {
			event = latestResult[workID]
		} else {
			for _, candidate := range database.BrainWorkEvents {
				if candidate.ID == requestedID && isProjectedWorkResultEvent(candidate.Kind) {
					event = candidate
					break
				}
			}
			workID = event.WorkID
		}
		if event.ID == "" || workID == "" {
			continue
		}
		itemIndex := workIndex(database.BrainWork, workID)
		item := Work{}
		if itemIndex >= 0 {
			item = database.BrainWork[itemIndex]
		}
		reviewState := WorkReviewResolved
		review := item.Review
		if review != nil && (requestedID == "work:"+workID || review.EventID == event.ID) {
			reviewState = WorkReviewQueued
			if reviewDeliveredAwaitingDisposition(review) {
				reviewState = WorkReviewReviewing
			}
		}
		sessionState := WorkResultSessionNotRequired
		if itemIndex >= 0 {
			sessionID := firstNonEmpty(event.SourceName, strings.TrimPrefix(event.PayloadRef, "session:"))
			if item.Status == WorkDone || item.Status == WorkCancelled {
				sessionState = WorkResultSessionFinalized
			} else if sessionID != "" && item.AttemptSessionID == sessionID &&
				!sessionHasImmutableTurn(database, sessionID) {
				// "Session open" reflects a live delegated owner only: a completed
				// Session (immutable canonical Turn) or a terminal Work is never
				// shown as open.
				sessionState = WorkResultSessionOpen
			} else if sessionHasImmutableTurn(database, sessionID) {
				// The Session completed/closed; teardown is owed or done. It is
				// never shown as open.
				sessionState = WorkResultSessionClosing
			}
		}
		out[requestedID] = WorkResultLifecycle{
			EventID:       requestedID,
			ReviewState:   reviewState,
			SessionState:  sessionState,
			CurrentResult: requestedID == "work:"+workID || latestResult[workID].ID == event.ID,
		}
	}
	return out, nil
}

func sessionHasImmutableTurn(database presentationDatabase, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	turn, found := currentTurnForSession(database, sessionID)
	return found && watcher.TurnImmutable(turn.Status)
}

// ResolveWorkReview is the single Work/Disposition transaction. It CASes the
// exact delivered review lease, applies one typed Work outcome, audits the
// event fact, and atomically clears the canonical review obligation. Facts
// appended during the lease (sequence > fence) re-require a fresh event in the
// same replacement.
func (s *Store) ResolveWorkReview(request WorkReviewDispositionRequest) (WorkEvent, Work, error) {
	request.WorkID = strings.TrimSpace(request.WorkID)
	request.HandlingID = strings.TrimSpace(request.HandlingID)
	request.ProviderTurnID = strings.TrimSpace(request.ProviderTurnID)
	request.NextSessionID = strings.TrimSpace(request.NextSessionID)
	request.NextTurnToken = strings.TrimSpace(request.NextTurnToken)
	request.NextAction = strings.TrimSpace(request.NextAction)
	request.Summary = strings.TrimSpace(request.Summary)
	if request.WorkID == "" || request.HandlingID == "" || request.ProviderTurnID == "" || request.ExpectedWorkRevision == 0 ||
		!validWorkDisposition(request.Disposition) {
		return WorkEvent{}, Work{}, fmt.Errorf("work_id, handling_id, provider_turn_id, expected_work_revision, and a valid disposition are required")
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
	database, err := s.loadPresentationLocked()
	if err != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, err
	}
	itemIndex := reviewLeaseByCapability(database, request.WorkID, request.HandlingID, request.ProviderTurnID)
	if itemIndex < 0 {
		// A stale capability after the event already committed reads as
		// handled; a capability that never existed reads as a claim conflict.
		if itemIndex2 := workIndex(database.BrainWork, request.WorkID); itemIndex2 >= 0 &&
			database.BrainWork[itemIndex2].Revision > request.ExpectedWorkRevision {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, ErrEventHandled
		}
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	review := database.BrainWork[itemIndex].Review
	lease := review.Lease
	item := database.BrainWork[itemIndex]
	eventIndex := workEventIndex(database.BrainWorkEvents, review.EventID)
	event := WorkEvent{}
	if eventIndex >= 0 {
		event = database.BrainWorkEvents[eventIndex]
		if event.HandledAt != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, ErrEventHandled
		}
	} else {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, fmt.Errorf("review Event %q is missing", review.EventID)
	}
	if lease.DeliveredAt == nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, ErrEventClaim
	}
	// The canonical event identity plus the exact handler capability replace
	// a projected revision freeze: engine events during a delivered handling
	// (heartbeats, progress) legitimately advance the aggregate, and a stale
	// disposition is an idempotent no-op against a superseded event.
	_ = request.ExpectedWorkRevision
	wasTerminal := item.Status == WorkDone || item.Status == WorkCancelled
	if wasTerminal && request.Disposition != WorkDispositionComplete && request.Disposition != WorkDispositionCancel {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, fmt.Errorf("terminal Work cannot return to a nonterminal disposition")
	}
	if request.Disposition == WorkDispositionWait {
		if workHasActiveCanonicalAttempt(database, item) {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("%w: wait requires the active canonical Attempt to settle first", ErrWorkAttemptConflict)
		}
		if err := validateWorkWakeProducer(database, item, request.Wake, now); err != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, err
		}
	}
	// Canonical dispositions: the engine is the only writer of Work status,
	// ownership, and wait state. The projected row below is refreshed from
	// canonical state after the event closes.
	nextAction := request.NextAction
	switch request.Disposition {
	case WorkDispositionContinue:
		if request.NextSessionID == "" || request.NextTurnToken == "" {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("continue disposition requires exact next_session_id and next_turn_token")
		}
		canonical, stateErr := s.fsmState(item.ID)
		if stateErr != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, stateErr
		}
		nextToken := lifecycle.TurnToken(request.NextTurnToken)
		admission := canonical.AdmissionByToken(nextToken)
		if admission == nil || admission.Status != lifecycle.AdmissionAccepted ||
			admission.SessionID != request.NextSessionID || admission.Purpose != lifecycle.AdmissionPurposeReview ||
			admission.PurposeID != lease.HandlingID {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("%w: named next Attempt Turn has no accepted admission for this review handling", ErrWorkAttemptConflict)
		}
		st, stErr := s.fsm.AcceptReviewFollowUp(
			lifecycle.WorkID(item.ID), fsmEventID(item), request.NextSessionID,
			nextToken,
		)
		if stErr != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, stErr
		}
		if st.Attempt == nil || st.Attempt.SessionID != request.NextSessionID ||
			st.Attempt.TurnToken != nextToken {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, fmt.Errorf("%w: atomic next Attempt admission failed: attempt=%+v", ErrWorkAttemptConflict, st.Attempt)
		}
	case WorkDispositionWait:
		if err := s.fsmResolveReview(item.ID, lifecycle.DispositionWait, request.Wake); err != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, err
		}
	case WorkDispositionComplete:
		if err := s.fsmResolveReview(item.ID, lifecycle.DispositionComplete, nil); err != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, err
		}
	case WorkDispositionCancel:
		if err := s.fsmResolveReview(item.ID, lifecycle.DispositionCancel, nil); err != nil {
			s.mu.Unlock()
			return WorkEvent{}, Work{}, err
		}
	}
	// Refresh the whole projected row from canonical state; disposition prose
	// is applied on top as presentation.
	if err := s.fsmSyncWorkLocked(&database, item.ID, now); err != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, err
	}
	item = database.BrainWork[itemIndex]
	item.NextAction = firstNonEmpty(nextAction, item.NextAction)
	handledAt := now.UTC()
	event.Actionable = false
	event.HandledAt = &handledAt
	event.Disposition = request.Disposition
	event.DispositionSummary = request.Summary
	if eventIndex >= 0 {
		database.BrainWorkEvents[eventIndex] = event
	}
	database.BrainWork[itemIndex] = item
	if err := s.persistPresentationLocked(database); err != nil {
		s.mu.Unlock()
		return WorkEvent{}, Work{}, err
	}
	resolvedEvent := event
	s.mu.Unlock()
	s.broadcastWorkChange(item.ID)
	return resolvedEvent, item, nil
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

func workEventIndex(events []WorkEvent, eventID string) int {
	for index := range events {
		if events[index].ID == eventID {
			return index
		}
	}
	return -1
}

// ReconcileAbsentWorkAttempt reports process loss against the exact canonical
// owner token. Inventory is observation only; Lifecycle releases the owner and
// opens the review event atomically. Repeated inventories are no-ops.
func (s *Store) ReconcileAbsentWorkAttempt(workID, sessionID string) (Work, bool, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	if workID == "" || sessionID == "" {
		return Work{}, false, fmt.Errorf("work_id and absent session_id are required")
	}
	state, err := s.fsmState(workID)
	if errors.Is(err, lifecycle.ErrUnknownWork) {
		return Work{}, false, ErrWorkNotFound
	}
	if err != nil {
		return Work{}, false, err
	}
	if state.Attempt == nil || state.Attempt.SessionID != sessionID || state.Status.Terminal() {
		item, readErr := s.Work(workID)
		return item, false, readErr
	}
	identity := lifecycle.AttemptIdentity{SessionID: state.Attempt.SessionID, TurnToken: state.Attempt.TurnToken, Fence: state.Attempt.Generation}
	if _, err := s.fsm.ReportTurnLost(state.ID, identity, "process_loss"); err != nil {
		return Work{}, false, err
	}
	if err := s.SyncWorkProjection(workID); err != nil {
		return Work{}, false, err
	}
	item, err := s.Work(workID)
	if err != nil {
		return Work{}, false, err
	}
	s.broadcastWorkChange(workID)
	return item, true, nil
}

func (s *Store) ActiveWork() ([]ActiveWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
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
			ID:               item.ID,
			Revision:         item.Revision,
			Title:            item.Title,
			Status:           item.Status,
			AttemptSessionID: item.AttemptSessionID,
			AttemptDelegated: item.AttemptDelegated,
			WaitFor:          item.WaitFor,
			Wake:             cloneWorkWake(item.Wake),
			ProgressMode:     mustDeriveWorkProgressMode(database, item),
			AttentionPending: workHasReviewObligation(database, item.ID),
			UnreadResult:     hasUnread,
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
	database, err := s.loadPresentationLocked()
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
	reviewing := map[string]bool{}
	for _, item := range database.BrainWork {
		if item.Review != nil && reviewDeliveredAwaitingDisposition(item.Review) {
			reviewing[item.ID] = true
		}
	}
	queued := projectedAttentionWork(database, currentWorkQueuedAttentionLimit)
	queueRank := map[string]int{}
	for index, workID := range queued {
		queueRank[workID] = index
	}

	current := make([]CurrentWork, 0, len(database.BrainWork))
	currentIDs := map[string]bool{}
	for _, item := range database.BrainWork {
		mode := mustDeriveWorkProgressMode(database, item)
		attentionState := WorkAttentionState("")
		if reviewing[item.ID] {
			attentionState = WorkAttentionReviewing
		} else if _, selected := queueRank[item.ID]; selected {
			attentionState = WorkAttentionQueued
		}
		include := attentionState != ""
		terminal := item.Status == WorkDone || item.Status == WorkCancelled
		if !terminal {
			switch mode {
			case WorkProgressOwned:
				include = include || item.AttemptDelegated && present[item.AttemptSessionID]
			case WorkProgressWaiting:
				include = include || currentWakePresent(item.Wake, present)
			case WorkProgressReady:
				// Only the bounded fair queue window is operationally current.
			}
		}
		if !include {
			continue
		}
		currentIDs[item.ID] = true
		current = append(current, CurrentWork{
			ID:               item.ID,
			Revision:         item.Revision,
			Title:            item.Title,
			Status:           item.Status,
			ProgressMode:     mode,
			AttemptSessionID: currentAttemptSessionID(item, present),
			AttemptDelegated: item.AttemptDelegated && present[item.AttemptSessionID],
			WaitFor:          item.WaitFor,
			Wake:             cloneWorkWake(item.Wake),
			AttentionState:   attentionState,
			UnreadResult:     unread[item.ID],
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

	backlog := WorkBacklog{}
	for _, item := range database.BrainWork {
		if currentIDs[item.ID] {
			continue
		}
		backlog.Total++
		if item.Review != nil {
			backlog.QueuedAttention++
		}
		if item.Status == WorkDone || item.Status == WorkCancelled || hasResult[item.ID] {
			backlog.HistoricalResults++
		}
	}
	return WorkInventory{Current: current, Backlog: backlog}, nil
}

// projectedAttentionWork returns the bounded queue window of review-required
// Work, oldest event birth first. Every returned Work is claimable or
// recoverable (I6): leased-undelivered items are still the same single queue
// item, delivered items sort first as reviewing.
func projectedAttentionWork(database presentationDatabase, limit int) []string {
	if limit <= 0 {
		return nil
	}
	type candidate struct {
		workID     string
		requiredAt time.Time
		delivered  bool
	}
	candidates := make([]candidate, 0, len(database.BrainWork))
	for _, item := range database.BrainWork {
		review := item.Review
		if review == nil {
			continue
		}
		candidates = append(candidates, candidate{
			workID:     item.ID,
			requiredAt: review.RequiredAt,
			delivered:  reviewDeliveredAwaitingDisposition(review),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].delivered != candidates[right].delivered {
			return candidates[left].delivered
		}
		if candidates[left].requiredAt.Equal(candidates[right].requiredAt) {
			return candidates[left].workID < candidates[right].workID
		}
		return candidates[left].requiredAt.Before(candidates[right].requiredAt)
	})
	out := make([]string, 0, limit)
	for _, candidate := range candidates {
		if len(out) == limit {
			break
		}
		out = append(out, candidate.workID)
	}
	return out
}

func currentAttemptSessionID(item Work, present map[string]bool) string {
	if item.AttemptDelegated && present[item.AttemptSessionID] {
		return item.AttemptSessionID
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
