package calendar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type WorkRunner struct {
	Store    *work.Store
	Launcher *work.Launcher
	Watcher  interface {
		GetAgent(string) *classifier.Agent
	}
}

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
		return StatusCompleted, strings.TrimSpace(linked.Frontmatter.Outcome), "", true
	}
	if status == "failed" {
		failure := strings.TrimSpace(linked.Frontmatter.Friction)
		if failure == "" {
			failure = strings.TrimSpace(linked.Frontmatter.AIError)
		}
		return StatusFailed, strings.TrimSpace(linked.Frontmatter.Outcome), failure, true
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
		return StatusCompleted, strings.TrimSpace(agent.Summary), "", true
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
	cwd := strings.TrimSpace(item.ActionCwd)
	if cwd == "" {
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return ActionResult{}, err
		}
	}
	path := filepath.Join(r.Store.Root, "calendar", item.ID+"-"+run.ID+".md")
	body := fmt.Sprintf("# %s\n\n%s\n\nCalendar item: `%s`\nScheduled for: %s (%s)\n", item.Title, strings.TrimSpace(item.ActionInstruction), item.ID, run.ScheduledFor.In(mustLocation(item.Timezone)).Format("2006-01-02 15:04:05 MST"), item.Timezone)
	created, err := r.Store.Write(&work.Item{Path: path, Project: "calendar", Body: body, Frontmatter: work.Frontmatter{ID: run.ID, Kind: "calendar_action", Created: time.Now().UTC(), Title: item.Title, Extra: map[string]interface{}{"calendar_item_id": item.ID, "calendar_run_id": run.ID}}}, time.Time{})
	if err != nil {
		return ActionResult{}, fmt.Errorf("create visible Work item: %w", err)
	}
	started, err := r.Launcher.Start(created, work.Project{Name: "calendar", Cwd: cwd})
	if err != nil {
		return ActionResult{WorkID: created.ID}, fmt.Errorf("start visible Work item: %w", err)
	}
	written, err := r.Store.Write(started, time.Time{})
	if err != nil {
		return ActionResult{WorkID: created.ID, AgentSession: started.Frontmatter.AgentSession, Launched: true}, fmt.Errorf("persist started Work item: %w", err)
	}
	return ActionResult{WorkID: written.ID, AgentSession: written.Frontmatter.AgentSession, Result: "Visible Work action started.", Launched: true}, nil
}

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
