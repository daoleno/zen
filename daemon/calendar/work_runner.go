package calendar

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type WorkRunner struct {
	Store    *work.Store
	Launcher *work.Launcher
	Watcher  interface {
		GetAgent(string) *classifier.Agent
	}
	Brain *brain.Service
}

const (
	scheduledDeliverableStart       = "<!-- zen:scheduled-deliverable:start -->"
	scheduledDeliverableEnd         = "<!-- zen:scheduled-deliverable:end -->"
	scheduledDeliverablePlaceholder = "Replace this line with the complete user-facing deliverable."
	maxScheduledDeliverableBytes    = 256 * 1024
)

func (r *WorkRunner) InspectScheduledAction(_ context.Context, _ Item, run Run) (Status, string, string, bool) {
	if r == nil || r.Store == nil || strings.TrimSpace(run.WorkID) == "" {
		return StatusRunning, "", "", false
	}
	linked, ok := r.Store.GetByID(run.WorkID)
	if !ok {
		return StatusRunning, "", "", false
	}
	status := strings.ToLower(strings.TrimSpace(linked.Frontmatter.Status))
	if linked.Frontmatter.Done != nil || status == "done" {
		return StatusCompleted, scheduledActionResult(linked, ""), "", true
	}
	if status == "failed" {
		failure := strings.TrimSpace(linked.Frontmatter.Friction)
		if failure == "" {
			failure = strings.TrimSpace(linked.Frontmatter.AIError)
		}
		return StatusFailed, scheduledActionResult(linked, ""), failure, true
	}
	if r.Watcher == nil || strings.TrimSpace(run.AgentSession) == "" {
		return StatusRunning, "", "", true
	}
	agent := r.Watcher.GetAgent(run.AgentSession)
	if agent == nil {
		return StatusRunning, "", "", false
	}
	switch agent.State {
	case classifier.StateDone:
		return StatusCompleted, scheduledActionResult(linked, agent.Summary), "", true
	case classifier.StateFailed, classifier.StateRemoved:
		return StatusFailed, "", strings.TrimSpace(agent.Summary), true
	default:
		return StatusRunning, "", "", true
	}
}

func (r *WorkRunner) RunScheduledAction(_ context.Context, item Item, run Run) (ActionResult, error) {
	if r == nil || r.Store == nil || r.Launcher == nil {
		return ActionResult{}, fmt.Errorf("work execution is not configured")
	}
	if r.Brain == nil {
		return ActionResult{}, fmt.Errorf("Brain result delivery is not configured")
	}
	known, err := r.Brain.HasChatThread(item.SourceThreadID)
	if err != nil {
		return ActionResult{}, fmt.Errorf("validate Brain result thread: %w", err)
	}
	if !known {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrInvalidDeliveryTarget, strings.TrimSpace(item.SourceThreadID))
	}
	cwd := strings.TrimSpace(item.ActionCwd)
	if cwd == "" {
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return ActionResult{}, err
		}
	}
	path := filepath.Join(r.Store.Root, "calendar", item.ID+"-"+run.ID+".md")
	body := scheduledWorkBody(item, run)
	created, err := r.Store.Write(&work.Item{Path: path, Project: "calendar", Body: body, Frontmatter: work.Frontmatter{ID: run.ID, Kind: "calendar_action", Created: time.Now().UTC(), Title: item.Title, Extra: map[string]interface{}{"calendar_item_id": item.ID, "calendar_run_id": run.ID}}}, time.Time{})
	if err != nil {
		return ActionResult{}, fmt.Errorf("create visible Work item: %w", err)
	}
	started, err := r.Launcher.StartDedicated(created, work.Project{Name: "calendar", Cwd: cwd})
	if err != nil {
		return ActionResult{WorkID: created.ID}, fmt.Errorf("start visible Work item: %w", err)
	}
	written, err := r.Store.Write(started, time.Time{})
	if err != nil {
		return ActionResult{WorkID: created.ID, AgentSession: started.Frontmatter.AgentSession, Launched: true}, fmt.Errorf("persist started Work item: %w", err)
	}
	return ActionResult{WorkID: written.ID, AgentSession: written.Frontmatter.AgentSession, Result: "Visible Work action started.", Launched: true}, nil
}

func scheduledWorkBody(item Item, run Run) string {
	return fmt.Sprintf(`# %s

## Instructions

%s

## User-facing deliverable

Write the complete result for the user between the markers below. Replace the
placeholder entirely. Preserve useful paragraphs, lists, links, and citations;
do not put execution notes or Calendar metadata in this section. Keep the
deliverable at or below 256 KiB.

%s
%s
%s

## Calendar metadata

Calendar item: `+"`%s`"+`
Scheduled for: %s (%s)
`, item.Title, strings.TrimSpace(item.ActionInstruction), scheduledDeliverableStart, scheduledDeliverablePlaceholder, scheduledDeliverableEnd, item.ID, run.ScheduledFor.In(mustLocation(item.Timezone)).Format("2006-01-02 15:04:05 MST"), item.Timezone)
}

func scheduledActionResult(item *work.Item, legacySummary string) string {
	if item != nil {
		if deliverable, ok := extractScheduledDeliverable(item.Body); ok {
			return deliverable
		}
		if outcome := strings.TrimSpace(item.Frontmatter.Outcome); outcome != "" {
			return outcome
		}
	}
	return strings.TrimSpace(legacySummary)
}

func extractScheduledDeliverable(body string) (string, bool) {
	start := strings.Index(body, scheduledDeliverableStart)
	if start < 0 {
		return "", false
	}
	start += len(scheduledDeliverableStart)
	endOffset := strings.Index(body[start:], scheduledDeliverableEnd)
	if endOffset < 0 {
		return "", false
	}
	deliverable := strings.TrimSpace(body[start : start+endOffset])
	if deliverable == "" || deliverable == scheduledDeliverablePlaceholder {
		return "", false
	}
	return limitUTF8Bytes(deliverable, maxScheduledDeliverableBytes), true
}

func limitUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (r *WorkRunner) DeliverScheduledAction(_ context.Context, item Item, run Run, status Status, result, failure string) error {
	if r == nil || r.Brain == nil {
		return fmt.Errorf("Brain result delivery is not configured")
	}
	body := strings.TrimSpace(result)
	if status == StatusFailed {
		reason := compactFailure(failure)
		if reason == "" {
			reason = "Scheduled Work failed."
		}
		body = fmt.Sprintf("**%s failed**\n\n%s", strings.TrimSpace(item.Title), reason)
	} else {
		if body == "" {
			body = "Scheduled Work completed."
		}
		body = fmt.Sprintf("**%s completed**\n\n%s", strings.TrimSpace(item.Title), body)
	}
	_, err := r.Brain.DeliverCalendarResult(brain.CalendarResult{
		ID:             "calendar_result:" + item.ID + ":" + run.ID,
		ThreadID:       item.SourceThreadID,
		CalendarItemID: item.ID,
		CalendarRunID:  run.ID,
		Title:          item.Title,
		Status:         string(status),
		Body:           body,
		ScheduledFor:   run.ScheduledFor,
		CreatedAt:      time.Now().UTC(),
	})
	if errors.Is(err, brain.ErrChatThreadNotFound) {
		return fmt.Errorf("%w: %v", ErrInvalidDeliveryTarget, err)
	}
	return err
}

func compactFailure(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > 240 {
		return string(runes[:237]) + "..."
	}
	return value
}

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
