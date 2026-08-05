package brain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const orchestrationSchemaVersion = 3

var (
	ErrWorkNotFound      = errors.New("Brain Work not found")
	ErrWorkConflict      = errors.New("Brain Work already exists")
	ErrWorkOwnerConflict = errors.New("Brain Work already has an owner Session")
	ErrEventClaim        = errors.New("Brain Work event claim is no longer current")
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

// Work is the only durable Brain commitment. It is intentionally small:
// detailed plans and evidence remain in the referenced worklog.
type Work struct {
	ID               string           `json:"work_id"`
	Title            string           `json:"title"`
	Objective        string           `json:"objective"`
	Status           WorkStatus       `json:"status"`
	OwnerSessionID   string           `json:"owner_session_id,omitempty"`
	CompletionPolicy CompletionPolicy `json:"completion_policy"`
	DoneCriteriaRef  string           `json:"done_criteria_ref,omitempty"`
	NextAction       string           `json:"next_action,omitempty"`
	WaitFor          string           `json:"wait_for,omitempty"`
	ContextRef       string           `json:"context_ref,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// WorkEvent is an append-only fact. Event.ID is also its delivery receipt.
// Only Actionable events participate in Brain scheduling. A claimed Event is
// bound to one Host Session and consumed by that Host's identity-bound read.
type WorkEvent struct {
	ID                    string     `json:"event_id"`
	WorkID                string     `json:"work_id"`
	Kind                  string     `json:"kind"`
	DedupeKey             string     `json:"dedupe_key"`
	PayloadRef            string     `json:"payload_ref,omitempty"`
	SourceName            string     `json:"source_name,omitempty"`
	Summary               string     `json:"summary,omitempty"`
	Actionable            bool       `json:"actionable"`
	CreatedAt             time.Time  `json:"created_at"`
	ClaimedAt             *time.Time `json:"claimed_at,omitempty"`
	DeliveryHostSessionID string     `json:"delivery_host_session_id,omitempty"`
	ConsumedAt            *time.Time `json:"consumed_at,omitempty"`
	ReadAt                *time.Time `json:"read_at,omitempty"`
}

type WorkUpdate struct {
	Title            *string
	Objective        *string
	Status           *WorkStatus
	OwnerSessionID   *string
	CompletionPolicy *CompletionPolicy
	DoneCriteriaRef  *string
	NextAction       *string
	WaitFor          *string
	ContextRef       *string
}

type ActiveWork struct {
	ID             string     `json:"work_id"`
	Title          string     `json:"title"`
	Status         WorkStatus `json:"status"`
	OwnerSessionID string     `json:"owner_session_id,omitempty"`
	WaitFor        string     `json:"wait_for,omitempty"`
	UnreadResult   bool       `json:"unread_result"`
}

// WorkResultEvent is a bounded, read-only domain projection. The append-only
// WorkEvent and its Work remain the only durable sources.
type WorkResultEvent struct {
	EventID     string    `json:"event_id"`
	Kind        string    `json:"kind"`
	WorkID      string    `json:"work_id"`
	WorkTitle   string    `json:"work_title"`
	Summary     string    `json:"summary"`
	SessionID   string    `json:"session_id,omitempty"`
	SessionName string    `json:"session_name,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
	Unread      bool      `json:"unread"`

	hasEventSourceName bool
	hasEventSummary    bool
}

type WorkChange struct {
	WorkID string
}

const workResultSummaryRuneLimit = 360

type orchestrationDatabase struct {
	SchemaVersion   int                     `json:"schema_version"`
	Migrations      orchestrationMigrations `json:"migrations"`
	BrainWork       []Work                  `json:"brain_work"`
	BrainWorkEvents []WorkEvent             `json:"brain_work_events"`
}

type orchestrationMigrations struct {
	DelegatedSessionsV1At *time.Time `json:"delegated_sessions_v1_at,omitempty"`
}

type orchestrationV0 struct {
	SchemaVersion int `json:"schema_version"`
}

type legacyOrchestrationDatabase struct {
	SchemaVersion   int                     `json:"schema_version"`
	Migrations      orchestrationMigrations `json:"migrations"`
	BrainWork       []Work                  `json:"brain_work"`
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

func (s *Store) orchestrationPath() string {
	return s.statePath() + string(os.PathSeparator) + "orchestration.json"
}

func (s *Store) ensureOrchestrationDatabase() error {
	raw, err := os.ReadFile(s.orchestrationPath())
	if errors.Is(err, os.ErrNotExist) {
		return s.persistOrchestrationLocked(orchestrationDatabase{
			SchemaVersion:   orchestrationSchemaVersion,
			BrainWork:       []Work{},
			BrainWorkEvents: []WorkEvent{},
		})
	}
	if err != nil {
		return err
	}
	database, migrated, err := decodeOrchestrationDatabase(raw)
	if err != nil {
		return fmt.Errorf("decode Brain orchestration database: %w", err)
	}
	if migrated {
		return s.persistOrchestrationLocked(database)
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
			SchemaVersion:   orchestrationSchemaVersion,
			Migrations:      orchestrationMigrations{},
			BrainWork:       []Work{},
			BrainWorkEvents: []WorkEvent{},
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
			SchemaVersion:   orchestrationSchemaVersion,
			Migrations:      legacy.Migrations,
			BrainWork:       legacy.BrainWork,
			BrainWorkEvents: make([]WorkEvent, 0, len(legacy.BrainWorkEvents)),
		}
		for _, old := range legacy.BrainWorkEvents {
			event := WorkEvent{
				ID: old.ID, WorkID: old.WorkID, Kind: old.Kind,
				DedupeKey: old.DedupeKey, PayloadRef: old.PayloadRef,
				Actionable: old.Actionable, CreatedAt: old.CreatedAt,
				ClaimedAt: old.ClaimedAt, DeliveryHostSessionID: old.DeliveryHostSessionID,
				ConsumedAt: old.ConsumedAt, ReadAt: old.ReadAt,
			}
			if old.DeliveryAcknowledgedAt != nil && event.ConsumedAt == nil {
				event.ConsumedAt = old.DeliveryAcknowledgedAt
			}
			database.BrainWorkEvents = append(database.BrainWorkEvents, event)
		}
		if err := validateOrchestrationDatabase(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, true, nil
	case orchestrationSchemaVersion:
		var database orchestrationDatabase
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if err := ensureSingleJSONValue(decoder, trimmed); err != nil {
			return orchestrationDatabase{}, false, err
		}
		if database.BrainWork == nil || database.BrainWorkEvents == nil {
			return orchestrationDatabase{}, false, fmt.Errorf("brain_work and brain_work_events are required arrays")
		}
		if err := validateOrchestrationDatabase(database); err != nil {
			return orchestrationDatabase{}, false, err
		}
		return database, false, nil
	default:
		return orchestrationDatabase{}, false, fmt.Errorf(
			"unsupported schema_version %d (latest %d)",
			*header.SchemaVersion,
			orchestrationSchemaVersion,
		)
	}
}

func ensureSingleJSONValue(decoder *json.Decoder, raw []byte) error {
	if trailing := bytes.TrimSpace(raw[decoder.InputOffset():]); len(trailing) != 0 {
		return fmt.Errorf("document must contain exactly one JSON value")
	}
	return nil
}

func validateOrchestrationDatabase(database orchestrationDatabase) error {
	workIDs := make(map[string]struct{}, len(database.BrainWork))
	activeOwners := make(map[string]string, len(database.BrainWork))
	for index, item := range database.BrainWork {
		if err := validateWork(item); err != nil {
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
	}
	eventIDs := make(map[string]struct{}, len(database.BrainWorkEvents))
	dedupeKeys := make(map[string]struct{}, len(database.BrainWorkEvents))
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
		key := event.WorkID + "\x00" + event.DedupeKey
		if _, exists := dedupeKeys[key]; exists {
			return fmt.Errorf("brain_work_events[%d]: duplicate dedupe_key %q", index, event.DedupeKey)
		}
		dedupeKeys[key] = struct{}{}
	}
	return nil
}

func validateWork(item Work) error {
	item.ID = strings.TrimSpace(item.ID)
	item.Title = strings.TrimSpace(item.Title)
	item.Objective = strings.TrimSpace(item.Objective)
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
	if event.ClaimedAt == nil && strings.TrimSpace(event.DeliveryHostSessionID) != "" {
		return fmt.Errorf("delivery host requires a claim")
	}
	if event.ClaimedAt != nil && event.ConsumedAt == nil && strings.TrimSpace(event.DeliveryHostSessionID) == "" {
		return fmt.Errorf("unconsumed claim requires delivery_host_session_id")
	}
	if event.ConsumedAt != nil && event.ClaimedAt == nil {
		return fmt.Errorf("consumed event must have a claim")
	}
	return nil
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
	if err := validateOrchestrationDatabase(database); err != nil {
		return err
	}
	return writeJSONFile(s.orchestrationPath(), database)
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
	item.DoneCriteriaRef = strings.TrimSpace(item.DoneCriteriaRef)
	item.NextAction = strings.TrimSpace(item.NextAction)
	item.WaitFor = strings.TrimSpace(item.WaitFor)
	item.ContextRef = strings.TrimSpace(item.ContextRef)
	if item.Status == "" {
		item.Status = WorkOpen
	}
	if item.CompletionPolicy == "" {
		item.CompletionPolicy = CompletionBounded
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	if err := validateWork(item); err != nil {
		return Work{}, err
	}
	return item, nil
}

func (s *Store) CreateWork(item Work) (Work, error) {
	now := s.nowUTC()
	item, err := normalizeWorkForCreate(item, now)
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
		err = s.persistOrchestrationLocked(database)
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
	item, err := normalizeWorkForCreate(item, now)
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
		err = s.persistOrchestrationLocked(database)
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
			item = database.BrainWork[index]
			applyWorkUpdate(&item, update)
			item.UpdatedAt = s.nowUTC()
			if err = validateWork(item); err == nil {
				database.BrainWork[index] = item
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

// AttachWorkOwner is the only delegated-spawn ownership transition. It
// atomically changes an active unowned Work record to one running owner; a
// concurrent incumbent is never replaced.
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
			} else if strings.TrimSpace(item.OwnerSessionID) != "" {
				err = fmt.Errorf("%w: Work %s is owned by %s", ErrWorkOwnerConflict, item.ID, item.OwnerSessionID)
			} else {
				item.OwnerSessionID = ownerSessionID
				item.Status = WorkRunning
				item.NextAction = "Wait for the delegated Session."
				item.WaitFor = "Session " + ownerSessionID
				item.UpdatedAt = s.nowUTC()
				database.BrainWork[index] = item
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
	return Work{}, false, nil
}

// MigrateDelegatedSessionsV1 performs the only legacy ownership adoption.
// Call it once, after Watcher has produced its first authoritative inventory.
// New delegated Sessions are created with Work directly and never use this
// migration or a runtime fallback.
func (s *Store) MigrateDelegatedSessionsV1(sessions []Work) (bool, error) {
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
	event.CreatedAt = s.nowUTC()
	event.ClaimedAt = nil
	event.DeliveryHostSessionID = ""
	event.ConsumedAt = nil
	event.ReadAt = nil
	if err := validateWorkEvent(event); err != nil {
		return WorkEvent{}, false, err
	}

	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err == nil && workIndex(database.BrainWork, event.WorkID) < 0 {
		err = ErrWorkNotFound
	}
	if err == nil {
		for _, current := range database.BrainWorkEvents {
			if current.WorkID == event.WorkID && current.DedupeKey == event.DedupeKey {
				s.mu.Unlock()
				return current, false, nil
			}
		}
		database.BrainWorkEvents = append(database.BrainWorkEvents, event)
		err = s.persistOrchestrationLocked(database)
	}
	s.mu.Unlock()
	if err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(event.WorkID)
	return event, true, nil
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
	index := -1
	for candidate := range database.BrainWorkEvents {
		event := database.BrainWorkEvents[candidate]
		if !workEventSchedulerEligible(database, event) || event.ClaimedAt != nil {
			continue
		}
		if index < 0 ||
			event.CreatedAt.Before(database.BrainWorkEvents[index].CreatedAt) ||
			event.CreatedAt.Equal(database.BrainWorkEvents[index].CreatedAt) && event.ID < database.BrainWorkEvents[index].ID {
			index = candidate
		}
	}
	if index < 0 {
		return WorkEvent{}, false, nil
	}
	now := s.nowUTC()
	database.BrainWorkEvents[index].ClaimedAt = &now
	database.BrainWorkEvents[index].DeliveryHostSessionID = hostSessionID
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

func workEventSchedulerEligible(database orchestrationDatabase, event WorkEvent) bool {
	if !event.Actionable || event.ConsumedAt != nil || event.ReadAt != nil {
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
		if !event.Actionable || event.ClaimedAt == nil || event.ConsumedAt != nil ||
			event.DeliveryHostSessionID != hostSessionID {
			return ErrEventClaim
		}
		event.ClaimedAt = nil
		event.DeliveryHostSessionID = ""
		return s.persistOrchestrationLocked(database)
	}
	return ErrEventClaim
}

// ConsumeClaimedWorkEvent returns and consumes the one Event currently assigned
// to hostSessionID. Host identity is the authorization boundary; Event.ID is
// the stable receipt, so there is no projected delivery token.
func (s *Store) ConsumeClaimedWorkEvent(hostSessionID string) (WorkEvent, Work, bool, error) {
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" {
		return WorkEvent{}, Work{}, false, fmt.Errorf("Host Session is required")
	}
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	workID := ""
	var claimed WorkEvent
	var item Work
	found := false
	if err == nil {
		for index := range database.BrainWorkEvents {
			event := &database.BrainWorkEvents[index]
			if !workEventSchedulerEligible(database, *event) || event.ClaimedAt == nil ||
				event.DeliveryHostSessionID != hostSessionID {
				continue
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
			event.ConsumedAt = &now
			claimed = *event
			item = database.BrainWork[workIndex]
			found = true
			err = s.persistOrchestrationLocked(database)
			break
		}
	}
	s.mu.Unlock()
	if err == nil && workID != "" {
		s.broadcastWorkChange(workID)
	}
	return claimed, item, found, err
}

func workEventIndex(events []WorkEvent, eventID string) int {
	for index := range events {
		if events[index].ID == eventID {
			return index
		}
	}
	return -1
}

// ReconcileMissingWorkOwner atomically detaches the expected missing owner,
// makes the Work non-running, and records the single actionable stale fact.
func (s *Store) ReconcileMissingWorkOwner(workID, ownerSessionID string) (Work, WorkEvent, bool, error) {
	workID = strings.TrimSpace(workID)
	ownerSessionID = strings.TrimSpace(ownerSessionID)
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	var item Work
	var event WorkEvent
	created := false
	if err == nil {
		index := workIndex(database.BrainWork, workID)
		if index < 0 {
			err = ErrWorkNotFound
		} else {
			item = database.BrainWork[index]
			if item.Status == WorkDone || item.Status == WorkCancelled || item.OwnerSessionID != ownerSessionID || ownerSessionID == "" {
				s.mu.Unlock()
				return item, WorkEvent{}, false, nil
			}
			payloadRef := "session:" + ownerSessionID
			lastLifecycleKind := ""
			for _, current := range database.BrainWorkEvents {
				if current.WorkID == item.ID &&
					current.PayloadRef == payloadRef &&
					isSessionLifecycleKind(current.Kind) {
					lastLifecycleKind = current.Kind
				}
			}
			if lastLifecycleKind == "session.done" || lastLifecycleKind == "session.failed" {
				now := s.nowUTC()
				item.OwnerSessionID = ""
				item.WaitFor = ""
				item.UpdatedAt = now
				database.BrainWork[index] = item
				if err = s.persistOrchestrationLocked(database); err != nil {
					s.mu.Unlock()
					return Work{}, WorkEvent{}, false, err
				}
				s.mu.Unlock()
				s.broadcastWorkChange(item.ID)
				return item, WorkEvent{}, false, nil
			}
			now := s.nowUTC()
			item.Status = WorkWaiting
			item.OwnerSessionID = ""
			item.NextAction = "Reconcile the missing delegated Session."
			item.WaitFor = ""
			item.UpdatedAt = now
			database.BrainWork[index] = item

			dedupeKey := "session:" + ownerSessionID + ":missing"
			for _, current := range database.BrainWorkEvents {
				if current.WorkID == item.ID && current.DedupeKey == dedupeKey {
					event = current
					break
				}
			}
			if event.ID == "" {
				digest := sha256.Sum256([]byte(item.ID + "\x00" + dedupeKey))
				event = WorkEvent{
					ID:         fmt.Sprintf("missing-%x", digest[:12]),
					WorkID:     item.ID,
					Kind:       "session.stale",
					DedupeKey:  dedupeKey,
					PayloadRef: "session:" + ownerSessionID,
					Actionable: true,
					CreatedAt:  now,
				}
				database.BrainWorkEvents = append(database.BrainWorkEvents, event)
				created = true
			}
			err = s.persistOrchestrationLocked(database)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return Work{}, WorkEvent{}, false, err
	}
	s.broadcastWorkChange(item.ID)
	return item, event, created, nil
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
			ID:             item.ID,
			Title:          item.Title,
			Status:         item.Status,
			OwnerSessionID: item.OwnerSessionID,
			WaitFor:        item.WaitFor,
			UnreadResult:   hasUnread,
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

func (s *Store) RecentWorkResultEvents(limit int) ([]WorkResultEvent, error) {
	if limit <= 0 {
		return []WorkResultEvent{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return nil, err
	}
	workByID := make(map[string]Work, len(database.BrainWork))
	for _, item := range database.BrainWork {
		workByID[item.ID] = item
	}
	out := make([]WorkResultEvent, 0, min(limit, len(database.BrainWorkEvents)))
	for _, event := range database.BrainWorkEvents {
		if !isProjectedWorkResultEvent(event.Kind) {
			continue
		}
		item, ok := workByID[event.WorkID]
		if !ok {
			continue
		}
		sessionID := strings.TrimSpace(item.OwnerSessionID)
		if payloadSessionID := strings.TrimPrefix(event.PayloadRef, "session:"); payloadSessionID != event.PayloadRef {
			sessionID = strings.TrimSpace(payloadSessionID)
		}
		eventSummary := strings.TrimSpace(event.Summary)
		summary := eventSummary
		if summary == "" {
			summary = strings.TrimSpace(item.Objective)
			if nextAction := strings.TrimSpace(item.NextAction); nextAction != "" {
				summary = nextAction
			}
		}
		summary = compactWorkResultText(summary)
		eventSourceName := strings.TrimSpace(event.SourceName)
		out = append(out, WorkResultEvent{
			EventID:            event.ID,
			Kind:               event.Kind,
			WorkID:             item.ID,
			WorkTitle:          item.Title,
			Summary:            summary,
			SessionID:          sessionID,
			SessionName:        eventSourceName,
			OccurredAt:         event.CreatedAt,
			Unread:             event.ReadAt == nil,
			hasEventSourceName: eventSourceName != "",
			hasEventSummary:    eventSummary != "",
		})
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].OccurredAt.Equal(out[right].OccurredAt) {
			return out[left].EventID < out[right].EventID
		}
		return out[left].OccurredAt.Before(out[right].OccurredAt)
	})
	if len(out) > limit {
		out = append([]WorkResultEvent(nil), out[len(out)-limit:]...)
	}
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
	case "session.done", "session.failed", "session.needs_input", "session.stale":
		return true
	default:
		return false
	}
}

func isResultEvent(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session.done", "session.failed", "session.needs_input", "session.stale",
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
