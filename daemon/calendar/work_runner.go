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

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type WorkRunner struct {
	Store    *work.Store
	Launcher *work.Launcher
	Watcher  interface {
		GetAgent(string) *classifier.Agent
		HasSession(string) bool
	}
	Brain interface {
		HasChatThread(string) (bool, error)
	}
}

const (
	scheduledDeliverableStart       = "<!-- zen:scheduled-deliverable:start -->"
	scheduledDeliverableEnd         = "<!-- zen:scheduled-deliverable:end -->"
	scheduledDeliverablePlaceholder = "Replace this line with the complete user-facing deliverable."
	maxScheduledDeliverableBytes    = 256 * 1024
)

var errScheduledWorkIdentity = errors.New("linked scheduled Work identity mismatch")

func (r *WorkRunner) InspectScheduledAction(_ context.Context, item Item, run Run) (Status, string, string, bool) {
	if r == nil || r.Store == nil || strings.TrimSpace(run.WorkID) == "" {
		return StatusRunning, "", "", false
	}
	linked, readErr := r.readCurrentScheduledWork(item, run)
	if errors.Is(readErr, errScheduledWorkIdentity) {
		return StatusFailed, "", "Linked scheduled Work does not belong to this Calendar occurrence.", true
	}
	if readErr == nil {
		status := strings.ToLower(strings.TrimSpace(linked.Frontmatter.Status))
		if linked.Frontmatter.Done != nil && status == "failed" {
			return StatusFailed, "", "Linked scheduled Work has conflicting done and failed state.", true
		}
		if linked.Frontmatter.Done != nil || status == "done" {
			deliverable, err := extractScheduledDeliverable(linked.Body)
			if err != nil {
				return StatusFailed, "", compactFailure("Linked scheduled Work deliverable is invalid: " + err.Error()), true
			}
			return StatusCompleted, deliverable, "", true
		}
		if status == "failed" {
			return StatusFailed, "", "Linked scheduled Work reported failure.", true
		}
	}
	if r.Watcher == nil {
		if readErr == nil {
			return StatusRunning, "", "", true
		}
		return StatusRunning, "", "", false
	}
	agentSession := strings.TrimSpace(run.AgentSession)
	if agentSession == "" {
		return StatusRunning, "", "", false
	}
	agent := r.Watcher.GetAgent(agentSession)
	if agent == nil {
		if r.Watcher.HasSession(agentSession) {
			return StatusRunning, "", "", true
		}
		return StatusRunning, "", "", false
	}
	switch agent.State {
	case classifier.StateDone:
		if readErr != nil {
			return StatusFailed, "", "Linked agent completed, but its current scheduled Work is unavailable or invalid.", true
		}
		return StatusFailed, "", "Linked agent completed before producing a valid terminal scheduled Work deliverable.", true
	case classifier.StateFailed, classifier.StateRemoved:
		failure := compactFailure(agent.Summary)
		if failure == "" {
			failure = "Linked agent failed before producing a terminal scheduled Work deliverable."
		}
		return StatusFailed, "", failure, true
	default:
		return StatusRunning, "", "", true
	}
}

func (r *WorkRunner) readCurrentScheduledWork(item Item, run Run) (*work.Item, error) {
	itemID := strings.TrimSpace(item.ID)
	runID := strings.TrimSpace(run.ID)
	workID := strings.TrimSpace(run.WorkID)
	if itemID == "" || runID == "" || workID == "" || workID != runID {
		return nil, errScheduledWorkIdentity
	}
	path := filepath.Join(r.Store.Root, "calendar", itemID+"-"+runID+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read linked scheduled Work: %w", err)
	}
	linked, err := work.ParseFile(path, raw, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("parse linked scheduled Work: %w", err)
	}
	calendarItemID, itemOK := scheduledWorkMetadata(linked.Frontmatter.Extra, "calendar_item_id")
	calendarRunID, runOK := scheduledWorkMetadata(linked.Frontmatter.Extra, "calendar_run_id")
	if strings.TrimSpace(linked.ID) != runID || strings.TrimSpace(linked.Frontmatter.Kind) != "calendar_action" ||
		!itemOK || calendarItemID != itemID || !runOK || calendarRunID != runID {
		return nil, errScheduledWorkIdentity
	}
	return linked, nil
}

func scheduledWorkMetadata(extra map[string]interface{}, key string) (string, bool) {
	value, ok := extra[key].(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func (r *WorkRunner) RunScheduledAction(_ context.Context, item Item, run Run) (ActionResult, error) {
	if r == nil || r.Store == nil || r.Launcher == nil {
		return ActionResult{}, fmt.Errorf("work execution is not configured")
	}
	if r.Brain == nil {
		return ActionResult{}, fmt.Errorf("Brain thread registry is not configured")
	}
	known, err := r.Brain.HasChatThread(run.SourceThreadID)
	if err != nil {
		return ActionResult{}, fmt.Errorf("validate Brain result thread: %w", err)
	}
	if !known {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrInvalidDeliveryTarget, strings.TrimSpace(run.SourceThreadID))
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
	created, err := r.Store.Write(&work.Item{Path: path, Project: "calendar", Body: body, Frontmatter: work.Frontmatter{ID: run.ID, Kind: "calendar_action", Created: time.Now().UTC(), Title: run.Title, Extra: map[string]interface{}{"calendar_item_id": item.ID, "calendar_run_id": run.ID}}}, time.Time{})
	if err != nil {
		return ActionResult{}, fmt.Errorf("create visible Work item: %w", err)
	}
	started, err := r.Launcher.StartDedicated(created, cwd)
	if err != nil {
		return ActionResult{WorkID: created.ID}, fmt.Errorf("start visible Work item: %w", err)
	}
	written, err := r.Store.Write(started, time.Time{})
	if err != nil {
		return ActionResult{WorkID: created.ID, AgentSession: started.Frontmatter.AgentSession, Launched: true}, fmt.Errorf("persist started Work item: %w", err)
	}
	return ActionResult{WorkID: written.ID, AgentSession: written.Frontmatter.AgentSession, Launched: true}, nil
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
`, run.Title, strings.TrimSpace(item.ActionInstruction), scheduledDeliverableStart, scheduledDeliverablePlaceholder, scheduledDeliverableEnd, item.ID, run.ScheduledFor.In(mustLocation(item.Timezone)).Format("2006-01-02 15:04:05 MST"), item.Timezone)
}

func extractScheduledDeliverable(body string) (string, error) {
	startCount := strings.Count(body, scheduledDeliverableStart)
	if startCount != 1 {
		return "", fmt.Errorf("expected exactly one start marker, got %d", startCount)
	}
	endCount := strings.Count(body, scheduledDeliverableEnd)
	if endCount != 1 {
		return "", fmt.Errorf("expected exactly one end marker, got %d", endCount)
	}
	start := strings.Index(body, scheduledDeliverableStart) + len(scheduledDeliverableStart)
	end := strings.Index(body, scheduledDeliverableEnd)
	if end < start {
		return "", fmt.Errorf("end marker precedes start marker")
	}
	return normalizeScheduledResult(body[start:end])
}

func normalizeScheduledResult(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("result must be valid UTF-8")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("result must be nonempty")
	}
	if value == scheduledDeliverablePlaceholder {
		return "", fmt.Errorf("result still contains the placeholder")
	}
	if len(value) > maxScheduledDeliverableBytes {
		return "", fmt.Errorf("result exceeds %d bytes", maxScheduledDeliverableBytes)
	}
	return value, nil
}

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
