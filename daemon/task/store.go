package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store persists tasks to a JSON file and broadcasts changes.
type Store struct {
	mu         sync.RWMutex
	tasks      map[string]*Task
	nextNumber int
	path       string
	metaPath   string
	events     chan TaskEvent
}

type storeMeta struct {
	NextIssueNumber int `json:"next_issue_number"`
}

func NewStore(dir string) (*Store, error) {
	path := filepath.Join(dir, "tasks.json")
	metaPath := filepath.Join(dir, "meta.json")
	s := &Store{
		tasks:      make(map[string]*Task),
		nextNumber: 1,
		path:       path,
		metaPath:   metaPath,
		events:     make(chan TaskEvent, 64),
	}
	if err := s.loadMeta(); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Events returns a channel that receives task change events.
func (s *Store) Events() <-chan TaskEvent {
	return s.events
}

func (s *Store) Create(title, description, skillID, cwd string, priority int, labels []string, projectID string) (*Task, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	num := s.nextNumber
	s.nextNumber++

	t := &Task{
		ID:          uuid.New().String(),
		Number:      num,
		Title:       title,
		Description: description,
		Status:      StatusBacklog,
		Priority:    priority,
		Labels:      labels,
		ProjectID:   projectID,
		SkillID:     skillID,
		Cwd:         cwd,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.tasks[t.ID] = t
	if err := s.persist(); err != nil {
		delete(s.tasks, t.ID)
		s.nextNumber--
		s.mu.Unlock()
		return nil, err
	}
	if err := s.saveMeta(); err != nil {
		// non-fatal: number will be re-derived on next load
	}
	s.mu.Unlock()

	s.emit(TaskEvent{Type: "task_created", TaskID: t.ID, Task: t})
	return t, nil
}

func (s *Store) Get(id string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cp := *t
		list = append(list, &cp)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

// FindByCurrentRunID returns the task whose current run matches the given run id.
func (s *Store) FindByCurrentRunID(runID string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.CurrentRunID == runID {
			cp := *t
			return &cp
		}
	}
	return nil
}

func (s *Store) Update(id string, fn func(*Task)) (*Task, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("task %s not found", id)
	}
	fn(t)
	t.UpdatedAt = time.Now().UTC()
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := *t
	s.mu.Unlock()

	s.emit(TaskEvent{Type: "task_updated", TaskID: cp.ID, Task: &cp})
	return &cp, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	if _, ok := s.tasks[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("task %s not found", id)
	}
	delete(s.tasks, id)
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.emit(TaskEvent{Type: "task_deleted", TaskID: id})
	return nil
}

func (s *Store) emit(e TaskEvent) {
	select {
	case s.events <- e:
	default:
	}
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read tasks file: %w", err)
	}

	var list []*Task
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse tasks file: %w", err)
	}

	maxNumber := 0
	for _, t := range list {
		if t.Number > maxNumber {
			maxNumber = t.Number
		}
		s.tasks[t.ID] = t
	}
	if maxNumber >= s.nextNumber {
		s.nextNumber = maxNumber + 1
	}
	return nil
}

func (s *Store) persist() error {
	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data, 0o600)
}

func (s *Store) loadMeta() error {
	data, err := os.ReadFile(s.metaPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read meta file: %w", err)
	}
	var m storeMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil // non-fatal
	}
	if m.NextIssueNumber > s.nextNumber {
		s.nextNumber = m.NextIssueNumber
	}
	return nil
}

func (s *Store) saveMeta() error {
	m := storeMeta{NextIssueNumber: s.nextNumber}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.metaPath, data, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
