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
	mu                  sync.RWMutex
	tasks               map[string]*Task
	nextNumbersByPrefix map[string]int
	path                string
	metaPath            string
	events              chan TaskEvent
}

type storeMeta struct {
	NextIssueNumber          int            `json:"next_issue_number"`
	NextIssueNumbersByPrefix map[string]int `json:"next_issue_numbers_by_prefix,omitempty"`
}

func NewStore(dir string) (*Store, error) {
	path := filepath.Join(dir, "tasks.json")
	metaPath := filepath.Join(dir, "meta.json")
	s := &Store{
		tasks:               make(map[string]*Task),
		nextNumbersByPrefix: map[string]int{DefaultIdentifierPrefix: 1},
		path:                path,
		metaPath:            metaPath,
		events:              make(chan TaskEvent, 64),
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

func (s *Store) Create(title, description string, attachments []Attachment, cwd string, priority int, labels []string, projectID, dueDate, identifierPrefix string) (*Task, error) {
	now := time.Now().UTC()
	normalizedDueDate, err := NormalizeDueDate(dueDate)
	if err != nil {
		return nil, err
	}
	normalizedPrefix := NormalizeIdentifierPrefix(identifierPrefix)

	s.mu.Lock()
	previousNext, hadPreviousNext := s.nextNumbersByPrefix[normalizedPrefix]
	num := s.allocateNumberLocked(normalizedPrefix)

	t := &Task{
		ID:               uuid.New().String(),
		IdentifierPrefix: normalizedPrefix,
		Number:           num,
		Title:            title,
		Description:      description,
		Attachments:      cloneAttachments(attachments),
		Status:           StatusBacklog,
		Priority:         priority,
		Labels:           append([]string(nil), labels...),
		ProjectID:        projectID,
		DueDate:          normalizedDueDate,
		Cwd:              cwd,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	s.tasks[t.ID] = t
	if err := s.persist(); err != nil {
		delete(s.tasks, t.ID)
		if hadPreviousNext {
			s.nextNumbersByPrefix[normalizedPrefix] = previousNext
		} else {
			delete(s.nextNumbersByPrefix, normalizedPrefix)
		}
		s.mu.Unlock()
		return nil, err
	}
	if err := s.saveMeta(); err != nil {
		// non-fatal: number will be re-derived on next load
	}
	s.mu.Unlock()

	cp := cloneTask(t)
	s.emit(TaskEvent{Type: "task_created", TaskID: t.ID, Task: cp})
	return cp, nil
}

func (s *Store) Get(id string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil
	}
	return cloneTask(t)
}

func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, cloneTask(t))
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
			return cloneTask(t)
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
	t.IdentifierPrefix = NormalizeIdentifierPrefix(t.IdentifierPrefix)
	t.UpdatedAt = time.Now().UTC()
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := cloneTask(t)
	s.mu.Unlock()

	s.emit(TaskEvent{Type: "task_updated", TaskID: cp.ID, Task: cp})
	return cp, nil
}

func (s *Store) AddComment(id string, comment TaskComment) (*Task, error) {
	return s.Update(id, func(t *Task) {
		t.Comments = append(t.Comments, comment)
	})
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

func (s *Store) ClearProject(projectID string) ([]*Task, error) {
	s.mu.Lock()
	updated := make([]*Task, 0)

	for _, current := range s.tasks {
		if current.ProjectID != projectID {
			continue
		}

		current.ProjectID = ""
		current.UpdatedAt = time.Now().UTC()
		updated = append(updated, cloneTask(current))
	}

	if len(updated) == 0 {
		s.mu.Unlock()
		return nil, nil
	}

	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()

	for _, current := range updated {
		s.emit(TaskEvent{Type: "task_updated", TaskID: current.ID, Task: current})
	}

	return updated, nil
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
	dirty := false
	maxByPrefix := make(map[string]int)
	for _, t := range list {
		normalizedPrefix := NormalizeIdentifierPrefix(t.IdentifierPrefix)
		if t.IdentifierPrefix != normalizedPrefix {
			t.IdentifierPrefix = normalizedPrefix
			dirty = true
		}
		if t.Number > maxByPrefix[normalizedPrefix] {
			maxByPrefix[normalizedPrefix] = t.Number
		}
		if normalizedPrefix == DefaultIdentifierPrefix && t.Number > maxNumber {
			maxNumber = t.Number
		}
		s.tasks[t.ID] = t
	}
	for prefix, currentMax := range maxByPrefix {
		next := currentMax + 1
		if next > s.nextNumbersByPrefix[prefix] {
			s.nextNumbersByPrefix[prefix] = next
		}
	}
	if maxNumber+1 > s.nextNumbersByPrefix[DefaultIdentifierPrefix] {
		s.nextNumbersByPrefix[DefaultIdentifierPrefix] = maxNumber + 1
	}
	if dirty {
		if err := s.persist(); err != nil {
			return err
		}
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
	for prefix, next := range m.NextIssueNumbersByPrefix {
		normalizedPrefix := NormalizeIdentifierPrefix(prefix)
		if next > s.nextNumbersByPrefix[normalizedPrefix] {
			s.nextNumbersByPrefix[normalizedPrefix] = next
		}
	}
	if m.NextIssueNumber > s.nextNumbersByPrefix[DefaultIdentifierPrefix] {
		s.nextNumbersByPrefix[DefaultIdentifierPrefix] = m.NextIssueNumber
	}
	return nil
}

func (s *Store) saveMeta() error {
	nextByPrefix := make(map[string]int, len(s.nextNumbersByPrefix))
	for prefix, next := range s.nextNumbersByPrefix {
		nextByPrefix[prefix] = next
	}
	m := storeMeta{
		NextIssueNumber:          s.nextNumbersByPrefix[DefaultIdentifierPrefix],
		NextIssueNumbersByPrefix: nextByPrefix,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.metaPath, data, 0o600)
}

func (s *Store) allocateNumberLocked(prefix string) int {
	normalizedPrefix := NormalizeIdentifierPrefix(prefix)
	next := s.nextNumbersByPrefix[normalizedPrefix]
	if next < 1 {
		next = 1
	}
	s.nextNumbersByPrefix[normalizedPrefix] = next + 1
	return next
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

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}

	cp := *task
	if task.Labels != nil {
		cp.Labels = append([]string(nil), task.Labels...)
	}
	if task.Attachments != nil {
		cp.Attachments = cloneAttachments(task.Attachments)
	}
	if task.Comments != nil {
		cp.Comments = make([]TaskComment, len(task.Comments))
		for i, comment := range task.Comments {
			cp.Comments[i] = comment
			if comment.Attachments != nil {
				cp.Comments[i].Attachments = cloneAttachments(comment.Attachments)
			}
		}
	}
	return &cp
}

func cloneAttachments(items []Attachment) []Attachment {
	if items == nil {
		return nil
	}
	return append([]Attachment(nil), items...)
}
