package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const SchemaVersion = 1

type document struct {
	SchemaVersion int    `json:"schema_version"`
	Items         []Item `json:"items"`
}

type Event struct{ Item Item }

type Store struct {
	path    string
	now     func() time.Time
	mu      sync.RWMutex
	items   map[string]Item
	subs    map[int]chan Event
	nextSub int
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "calendar"), nil
}

func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("calendar root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(root, "calendar.json"), now: time.Now, items: map[string]Item{}, subs: map[int]chan Event{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.persistLocked()
	}
	if err != nil {
		return err
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Pre-v1 development builds used a bare item array. Migrate it once.
		var legacy []Item
		if legacyErr := json.Unmarshal(raw, &legacy); legacyErr != nil {
			return fmt.Errorf("decode calendar store: %w", err)
		}
		doc = document{SchemaVersion: SchemaVersion, Items: legacy}
	}
	if doc.SchemaVersion > SchemaVersion {
		return fmt.Errorf("calendar schema %d is newer than supported schema %d", doc.SchemaVersion, SchemaVersion)
	}
	for _, item := range doc.Items {
		if item.ID == "" {
			continue
		}
		s.items[item.ID] = item
	}
	return s.persistLocked()
}

func (s *Store) Path() string { return s.path }

func (s *Store) Subscribe() (int, <-chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan Event, 32)
	s.subs[id] = ch
	return id, ch
}
func (s *Store) Unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}
func (s *Store) broadcast(item Item) {
	for _, ch := range s.subs {
		select {
		case ch <- Event{Item: cloneItem(item)}:
		default:
		}
	}
}

func (s *Store) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, cloneItem(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextAt.Equal(out[j].NextAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].NextAt.Before(out[j].NextAt)
	})
	return out
}
func (s *Store) Get(id string) (Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return cloneItem(item), nil
}

func (s *Store) Create(item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item = cloneItem(item)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if _, exists := s.items[item.ID]; exists {
		return Item{}, fmt.Errorf("calendar id already exists")
	}
	if item.Recurrence == "" {
		item.Recurrence = RecurrenceNone
	}
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	now := s.now().UTC()
	item.CreatedAt, item.UpdatedAt, item.Revision = now, now, 1
	item.Status, item.NextAt = StatusScheduled, item.TriggerAt()
	item.Runs = nil
	s.items[item.ID] = item
	if err := s.persistLocked(); err != nil {
		delete(s.items, item.ID)
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) Update(item Item, expectedRevision int64) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.items[item.ID]
	if !ok {
		return Item{}, ErrNotFound
	}
	if expectedRevision > 0 && current.Revision != expectedRevision {
		return Item{}, ErrConflict
	}
	if current.Status == StatusRunning {
		return Item{}, fmt.Errorf("running scheduled action cannot be edited")
	}
	current = cloneItem(current)
	item = cloneItem(item)
	if item.Recurrence == "" {
		item.Recurrence = RecurrenceNone
	}
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	item.CreatedAt, item.UpdatedAt, item.Revision = current.CreatedAt, s.now().UTC(), current.Revision+1
	item.Runs = append([]Run(nil), current.Runs...)
	item.Status, item.NextAt = StatusScheduled, item.TriggerAt()
	item.CancelledAt, item.FailureReason = nil, ""
	s.items[item.ID] = item
	if err := s.persistLocked(); err != nil {
		s.items[item.ID] = current
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) Cancel(id string, expectedRevision int64) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if expectedRevision > 0 && item.Revision != expectedRevision {
		return Item{}, ErrConflict
	}
	if item.Status == StatusRunning {
		return Item{}, fmt.Errorf("running scheduled action cannot be cancelled")
	}
	original := cloneItem(item)
	now := s.now().UTC()
	item.Status, item.CancelledAt, item.UpdatedAt = StatusCancelled, &now, now
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) SetStatus(id string, status Status, reason string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	item.Status, item.FailureReason, item.UpdatedAt = status, reason, s.now().UTC()
	item.Revision++
	if status == StatusCompleted && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	}
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) Claim(id string, manual bool) (Item, Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, Run{}, ErrNotFound
	}
	if item.Kind != KindScheduledAction {
		return Item{}, Run{}, fmt.Errorf("run is only valid for scheduled_action")
	}
	if item.Status == StatusRunning {
		return Item{}, Run{}, ErrClaimed
	}
	if item.Status == StatusCancelled {
		return Item{}, Run{}, fmt.Errorf("cancelled action cannot run")
	}
	original := cloneItem(item)
	item = cloneItem(item)
	scheduledFor := item.NextAt
	if manual {
		scheduledFor = s.now().UTC()
	}
	for _, previous := range item.Runs {
		if !manual && previous.ScheduledFor.Equal(scheduledFor) {
			return Item{}, Run{}, ErrClaimed
		}
	}
	now := s.now().UTC()
	run := Run{ID: uuid.NewString(), ScheduledFor: scheduledFor, StartedAt: now, Status: StatusRunning, Manual: manual, PreviousStatus: item.Status}
	item.Runs = append(item.Runs, run)
	item.Status, item.UpdatedAt, item.FailureReason = StatusRunning, now, ""
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, Run{}, err
	}
	s.broadcast(item)
	return cloneItem(item), run, nil
}

func (s *Store) RecordLaunch(id, runID, workID, agentSession, result string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	idx := -1
	for n := range item.Runs {
		if item.Runs[n].ID == runID {
			idx = n
			break
		}
	}
	if idx < 0 {
		return Item{}, fmt.Errorf("calendar run not found")
	}
	if item.Runs[idx].Status != StatusRunning {
		return Item{}, ErrClaimed
	}
	now := s.now().UTC()
	item.Runs[idx].WorkID, item.Runs[idx].AgentSession = workID, agentSession
	item.Runs[idx].Result = result
	item.LinkedWorkID, item.Status, item.FailureReason, item.UpdatedAt = workID, StatusRunning, "", now
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) FinishRun(id, runID, result, failure string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	idx := -1
	for n := range item.Runs {
		if item.Runs[n].ID == runID {
			idx = n
			break
		}
	}
	if idx < 0 {
		return Item{}, fmt.Errorf("calendar run not found")
	}
	if item.Runs[idx].Status != StatusRunning {
		return cloneItem(item), nil
	}
	now := s.now().UTC()
	status := StatusCompleted
	if strings.TrimSpace(failure) != "" {
		status = StatusFailed
	}
	item.Runs[idx].Status, item.Runs[idx].FinishedAt = status, &now
	if strings.TrimSpace(result) != "" {
		item.Runs[idx].Result = result
	}
	item.Runs[idx].FailureReason = failure
	item.Status, item.FailureReason, item.UpdatedAt = status, failure, now
	item.Revision++
	if item.Runs[idx].Manual && item.Runs[idx].PreviousStatus == StatusScheduled && item.NextAt.After(now) {
		item.Status = StatusScheduled
		item.FailureReason = ""
	} else if status == StatusCompleted && item.Runs[idx].Manual && item.Recurrence != RecurrenceNone && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	} else if status == StatusCompleted && !item.Runs[idx].Manual && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	}
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) persistLocked() error {
	doc := document{SchemaVersion: SchemaVersion, Items: make([]Item, 0, len(s.items))}
	for _, item := range s.items {
		doc.Items = append(doc.Items, item)
	}
	sort.Slice(doc.Items, func(i, j int) bool { return doc.Items[i].CreatedAt.Before(doc.Items[j].CreatedAt) })
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".zen-calendar-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return nil
}
