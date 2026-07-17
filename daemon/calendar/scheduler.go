package calendar

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const DefaultMissedActionWindow = 15 * time.Minute

type ActionResult struct {
	WorkID, AgentSession string
	Launched             bool
}
type ActionRunner interface {
	RunScheduledAction(context.Context, Item, Run) (ActionResult, error)
}

// ActionInspector reconciles a launched calendar run with its canonical Work
// item/agent lifecycle. A false known result means the linked execution can no
// longer be observed and must never be relaunched implicitly.
type ActionInspector interface {
	InspectScheduledAction(context.Context, Item, Run) (status Status, result, failure string, known bool)
}

type Scheduler struct {
	store        *Store
	runner       ActionRunner
	now          func() time.Time
	interval     time.Duration
	missedWindow time.Duration
	launchMu     sync.Mutex
	launching    map[string]struct{}
}

func NewScheduler(store *Store, runner ActionRunner) *Scheduler {
	return &Scheduler{store: store, runner: runner, now: time.Now, interval: time.Second, missedWindow: DefaultMissedActionWindow, launching: map[string]struct{}{}}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.Tick(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	now := s.now()
	for _, item := range s.store.List() {
		if item.Kind == KindScheduledAction && item.Status == StatusRunning {
			if !s.isLaunching(item.ID) {
				s.reconcile(ctx, item)
			}
			continue
		}
		if item.Status == StatusCancelled || item.Status == StatusCompleted || item.Status == StatusFailed {
			continue
		}
		switch item.Kind {
		case KindReminder, KindDeadline:
			if !item.NextAt.After(now) && item.Status == StatusScheduled {
				status := StatusWaiting
				if item.Recurrence != RecurrenceNone {
					status = StatusCompleted
				}
				_, _ = s.store.SetStatus(item.ID, status, "")
			}
		case KindEvent:
			if item.EndAt != nil && !item.EndAt.After(now) {
				_, _ = s.store.SetStatus(item.ID, StatusCompleted, "")
			} else if !item.NextAt.After(now) && item.Status == StatusScheduled {
				_, _ = s.store.SetStatus(item.ID, StatusRunning, "")
			}
		case KindScheduledAction:
			if item.NextAt.After(now) || item.Status != StatusScheduled || s.isLaunching(item.ID) {
				continue
			}
			if now.Sub(item.NextAt) > s.missedWindow {
				reason := fmt.Sprintf("Missed while daemon was offline (more than %s late); use Run now to start it explicitly.", s.missedWindow)
				s.failOccurrence(ctx, item.ID, reason)
				continue
			}
			go s.run(ctx, item.ID, false)
		}
	}
}

func (s *Scheduler) RunNow(ctx context.Context, id string) (Item, error) {
	return s.run(ctx, id, true)
}

func (s *Scheduler) run(ctx context.Context, id string, manual bool) (Item, error) {
	if s == nil || s.store == nil {
		return Item{}, fmt.Errorf("calendar scheduler is not configured")
	}
	if !s.beginLaunch(id) {
		return Item{}, ErrClaimed
	}
	defer s.endLaunch(id)

	item, run, err := s.store.Claim(id, manual)
	if err != nil {
		return Item{}, err
	}
	if s.runner == nil {
		return s.store.FinishRun(id, run.ID, "", "scheduled action runner is not configured")
	}
	result, runErr := s.runner.RunScheduledAction(ctx, item, run)
	if runErr != nil {
		if result.Launched {
			return s.store.RecordLaunch(id, run.ID, result.WorkID, result.AgentSession)
		}
		return s.complete(item, run, "", strings.TrimSpace(runErr.Error()))
	}
	// Launching visible Work is not task completion. Persist the link and leave
	// both the item and run running until reconciliation observes a terminal Work state.
	return s.store.RecordLaunch(id, run.ID, result.WorkID, result.AgentSession)
}

func (s *Scheduler) failOccurrence(ctx context.Context, id, failure string) {
	item, run, err := s.store.Claim(id, false)
	if err != nil {
		return
	}
	_, _ = s.complete(item, run, "", failure)
}

func (s *Scheduler) complete(item Item, run Run, result, failure string) (Item, error) {
	return s.store.FinishRun(item.ID, run.ID, result, failure)
}

func (s *Scheduler) reconcile(ctx context.Context, item Item) {
	var active *Run
	for idx := len(item.Runs) - 1; idx >= 0; idx-- {
		if item.Runs[idx].Status == StatusRunning {
			active = &item.Runs[idx]
			break
		}
	}
	if active == nil {
		_, _ = s.store.SetStatus(item.ID, StatusFailed, "Running calendar item has no durable run record.")
		return
	}
	inspector, ok := s.runner.(ActionInspector)
	if !ok {
		return
	} // Explicitly remain running when this architecture cannot prove a terminal state.
	status, result, failure, known := inspector.InspectScheduledAction(ctx, item, *active)
	if !known {
		_, _ = s.complete(item, *active, "", "Linked Work/agent is no longer observable after restart; Zen did not relaunch it to avoid duplicate execution.")
		return
	}
	switch status {
	case StatusCompleted:
		_, _ = s.complete(item, *active, result, "")
	case StatusFailed:
		if strings.TrimSpace(failure) == "" {
			failure = "Linked Work/agent failed."
		}
		_, _ = s.complete(item, *active, "", failure)
	}
}

func (s *Scheduler) beginLaunch(id string) bool {
	if s == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	if s.launching == nil {
		s.launching = map[string]struct{}{}
	}
	if _, exists := s.launching[id]; exists {
		return false
	}
	s.launching[id] = struct{}{}
	return true
}

func (s *Scheduler) endLaunch(id string) {
	if s == nil {
		return
	}
	s.launchMu.Lock()
	delete(s.launching, strings.TrimSpace(id))
	s.launchMu.Unlock()
}

func (s *Scheduler) isLaunching(id string) bool {
	if s == nil {
		return false
	}
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	_, exists := s.launching[strings.TrimSpace(id)]
	return exists
}
