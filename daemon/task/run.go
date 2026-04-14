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

type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusBlocked   RunStatus = "blocked"
	RunStatusFailed    RunStatus = "failed"
	RunStatusDone      RunStatus = "done"
	RunStatusCancelled RunStatus = "cancelled"
)

type Run struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	AttemptNumber int       `json:"attempt_number"`
	Status        RunStatus `json:"status"`

	ExecutionMode  string `json:"execution_mode"`
	ExecutorKind   string `json:"executor_kind,omitempty"`
	ExecutorLabel  string `json:"executor_label,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`

	PromptSnapshot string     `json:"prompt_snapshot,omitempty"`
	Summary        string     `json:"summary,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	WaitingReason  string     `json:"waiting_reason,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RunEvent struct {
	Type  string `json:"type"`
	RunID string `json:"run_id"`
	Run   *Run   `json:"run,omitempty"`
}

type CreateRunOptions struct {
	TaskID         string
	Status         RunStatus
	ExecutionMode  string
	ExecutorKind   string
	ExecutorLabel  string
	AgentSessionID string
	PromptSnapshot string
	Summary        string
}

type RunStore struct {
	mu     sync.RWMutex
	runs   map[string]*Run
	path   string
	events chan RunEvent
}

func NewRunStore(dir string) (*RunStore, error) {
	path := filepath.Join(dir, "runs.json")
	rs := &RunStore{
		runs:   make(map[string]*Run),
		path:   path,
		events: make(chan RunEvent, 64),
	}
	if err := rs.load(); err != nil {
		return nil, err
	}
	return rs, nil
}

func (rs *RunStore) Events() <-chan RunEvent {
	return rs.events
}

func (rs *RunStore) List() []*Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	list := make([]*Run, 0, len(rs.runs))
	for _, run := range rs.runs {
		cp := *run
		list = append(list, &cp)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (rs *RunStore) Get(id string) *Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	run, ok := rs.runs[id]
	if !ok {
		return nil
	}
	cp := *run
	return &cp
}

func (rs *RunStore) Create(opts CreateRunOptions) (*Run, error) {
	if opts.TaskID == "" {
		return nil, fmt.Errorf("task id is required")
	}

	now := time.Now().UTC()
	startedAt := (*time.Time)(nil)
	if opts.Status != "" && opts.Status != RunStatusQueued {
		startedAt = &now
	}
	if opts.Status == "" {
		opts.Status = RunStatusQueued
	}

	rs.mu.Lock()
	attemptNumber := 1
	for _, run := range rs.runs {
		if run.TaskID == opts.TaskID && run.AttemptNumber >= attemptNumber {
			attemptNumber = run.AttemptNumber + 1
		}
	}
	run := &Run{
		ID:             uuid.New().String(),
		TaskID:         opts.TaskID,
		AttemptNumber:  attemptNumber,
		Status:         opts.Status,
		ExecutionMode:  opts.ExecutionMode,
		ExecutorKind:   opts.ExecutorKind,
		ExecutorLabel:  opts.ExecutorLabel,
		AgentSessionID: opts.AgentSessionID,
		PromptSnapshot: opts.PromptSnapshot,
		Summary:        opts.Summary,
		StartedAt:      startedAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	rs.runs[run.ID] = run
	if err := rs.persist(); err != nil {
		delete(rs.runs, run.ID)
		rs.mu.Unlock()
		return nil, err
	}
	cp := *run
	rs.mu.Unlock()

	rs.emit(RunEvent{Type: "run_created", RunID: cp.ID, Run: &cp})
	return &cp, nil
}

func (rs *RunStore) Update(id string, fn func(*Run)) (*Run, error) {
	rs.mu.Lock()
	run, ok := rs.runs[id]
	if !ok {
		rs.mu.Unlock()
		return nil, fmt.Errorf("run %s not found", id)
	}

	prevStatus := run.Status
	fn(run)
	now := time.Now().UTC()
	if run.StartedAt == nil && run.Status != RunStatusQueued {
		run.StartedAt = &now
	}
	if run.EndedAt == nil && isTerminalRunStatus(run.Status) {
		run.EndedAt = &now
	}
	if prevStatus != run.Status && !isTerminalRunStatus(prevStatus) && isTerminalRunStatus(run.Status) && run.EndedAt == nil {
		run.EndedAt = &now
	}
	run.UpdatedAt = now

	if err := rs.persist(); err != nil {
		rs.mu.Unlock()
		return nil, err
	}
	cp := *run
	rs.mu.Unlock()

	eventType := "run_updated"
	switch cp.Status {
	case RunStatusDone:
		eventType = "run_completed"
	case RunStatusFailed:
		eventType = "run_failed"
	case RunStatusCancelled:
		eventType = "run_cancelled"
	}
	rs.emit(RunEvent{Type: eventType, RunID: cp.ID, Run: &cp})
	return &cp, nil
}

func (rs *RunStore) FindActiveByAgentSessionID(agentSessionID string) *Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	var latest *Run
	for _, run := range rs.runs {
		if run.AgentSessionID != agentSessionID {
			continue
		}
		if isTerminalRunStatus(run.Status) {
			continue
		}
		if latest == nil || run.CreatedAt.After(latest.CreatedAt) {
			cp := *run
			latest = &cp
		}
	}
	return latest
}

func (rs *RunStore) emit(event RunEvent) {
	select {
	case rs.events <- event:
	default:
	}
}

func (rs *RunStore) load() error {
	data, err := os.ReadFile(rs.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read runs file: %w", err)
	}
	var list []*Run
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse runs file: %w", err)
	}
	for _, run := range list {
		rs.runs[run.ID] = run
	}
	return nil
}

func (rs *RunStore) persist() error {
	list := make([]*Run, 0, len(rs.runs))
	for _, run := range rs.runs {
		list = append(list, run)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(rs.path, data, 0o600)
}

func isTerminalRunStatus(status RunStatus) bool {
	return status == RunStatusDone || status == RunStatusFailed || status == RunStatusCancelled
}
