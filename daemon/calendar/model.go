package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Kind string

const (
	KindEvent           Kind = "event"
	KindReminder        Kind = "reminder"
	KindDeadline        Kind = "deadline"
	KindScheduledAction Kind = "scheduled_action"
)

type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusWaiting   Status = "waiting"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Recurrence string

const (
	RecurrenceNone     Recurrence = "none"
	RecurrenceDaily    Recurrence = "daily"
	RecurrenceWeekly   Recurrence = "weekly"
	RecurrenceWeekdays Recurrence = "weekdays"
)

var (
	ErrNotFound = errors.New("calendar item not found")
	ErrConflict = errors.New("calendar item revision conflict")
	ErrClaimed  = errors.New("calendar occurrence already claimed")
	// ErrInvalidDeliveryTarget marks a captured Brain thread that is not present
	// in the thread registry. Validation happens before Work is launched.
	ErrInvalidDeliveryTarget = errors.New("invalid Brain delivery target")
)

// Item is the canonical durable calendar series. Times are absolute instants;
// Timezone preserves the wall-clock intent used to calculate recurrence.
type Item struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Kind              Kind       `json:"kind"`
	Status            Status     `json:"status"`
	StartAt           *time.Time `json:"start_at,omitempty"`
	EndAt             *time.Time `json:"end_at,omitempty"`
	NotifyAt          *time.Time `json:"notify_at,omitempty"`
	DueAt             *time.Time `json:"due_at,omitempty"`
	Timezone          string     `json:"timezone"`
	Recurrence        Recurrence `json:"recurrence"`
	Notes             string     `json:"notes,omitempty"`
	ActionInstruction string     `json:"action_instruction,omitempty"`
	ActionCwd         string     `json:"action_cwd,omitempty"`
	SourceThreadID    string     `json:"source_thread_id,omitempty"`
	SourceMessageID   string     `json:"source_message_id,omitempty"`
	LinkedWorkID      string     `json:"linked_work_id,omitempty"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	NextAt            time.Time  `json:"next_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	Revision          int64      `json:"revision"`
	Runs              []Run      `json:"runs,omitempty"`
}

type Run struct {
	ID             string     `json:"id"`
	Title          string     `json:"title,omitempty"`
	SourceThreadID string     `json:"source_thread_id,omitempty"`
	ScheduledFor   time.Time  `json:"scheduled_for"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Status         Status     `json:"status"`
	Manual         bool       `json:"manual,omitempty"`
	WorkID         string     `json:"work_id,omitempty"`
	AgentSession   string     `json:"agent_session,omitempty"`
	Result         string     `json:"result,omitempty"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	PreviousStatus Status     `json:"previous_status,omitempty"`
}

func (i Item) Validate() error {
	i.Title = strings.TrimSpace(i.Title)
	if i.Title == "" {
		return fmt.Errorf("title is required")
	}
	switch i.Kind {
	case KindEvent:
		if i.StartAt == nil || i.EndAt == nil {
			return fmt.Errorf("event requires start_at and end_at")
		}
		if !i.EndAt.After(*i.StartAt) {
			return fmt.Errorf("event end_at must be after start_at")
		}
	case KindReminder:
		if i.NotifyAt == nil {
			return fmt.Errorf("reminder requires notify_at")
		}
	case KindDeadline:
		if i.DueAt == nil {
			return fmt.Errorf("deadline requires due_at")
		}
	case KindScheduledAction:
		if i.DueAt == nil {
			return fmt.Errorf("scheduled_action requires due_at")
		}
		if strings.TrimSpace(i.ActionInstruction) == "" {
			return fmt.Errorf("scheduled_action requires action_instruction")
		}
		if strings.TrimSpace(i.SourceThreadID) == "" {
			return fmt.Errorf("scheduled_action requires source_thread_id")
		}
	default:
		return fmt.Errorf("invalid kind %q", i.Kind)
	}
	if strings.TrimSpace(i.Timezone) == "" {
		return fmt.Errorf("timezone is required")
	}
	loc, err := time.LoadLocation(i.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q", i.Timezone)
	}
	if i.Recurrence == "" {
		i.Recurrence = RecurrenceNone
	}
	switch i.Recurrence {
	case RecurrenceNone, RecurrenceDaily, RecurrenceWeekly, RecurrenceWeekdays:
	default:
		return fmt.Errorf("invalid recurrence %q", i.Recurrence)
	}
	for name, value := range map[string]*time.Time{
		"start_at": i.StartAt, "end_at": i.EndAt, "notify_at": i.NotifyAt, "due_at": i.DueAt,
	} {
		if value == nil {
			continue
		}
		_, expected := value.Zone()
		_, actual := value.In(loc).Zone()
		if expected != actual {
			return fmt.Errorf("%s offset does not match timezone %s at that instant", name, i.Timezone)
		}
	}
	return nil
}

func (i Item) TriggerAt() time.Time {
	switch i.Kind {
	case KindEvent:
		if i.StartAt != nil {
			return *i.StartAt
		}
	case KindReminder:
		if i.NotifyAt != nil {
			return *i.NotifyAt
		}
	default:
		if i.DueAt != nil {
			return *i.DueAt
		}
	}
	return time.Time{}
}

func cloneItem(item Item) Item {
	cloneTime := func(value *time.Time) *time.Time {
		if value == nil {
			return nil
		}
		copy := *value
		return &copy
	}
	item.StartAt = cloneTime(item.StartAt)
	item.EndAt = cloneTime(item.EndAt)
	item.NotifyAt = cloneTime(item.NotifyAt)
	item.DueAt = cloneTime(item.DueAt)
	item.CancelledAt = cloneTime(item.CancelledAt)
	item.Runs = append([]Run(nil), item.Runs...)
	for index := range item.Runs {
		item.Runs[index].FinishedAt = cloneTime(item.Runs[index].FinishedAt)
	}
	return item
}
