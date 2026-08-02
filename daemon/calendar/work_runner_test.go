package calendar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type scheduledActionWatcher struct {
	agent      *classifier.Agent
	hasSession bool
}

type scheduledActionThreadRegistry struct {
	known  bool
	thread string
}

type scheduledActionLaunchRunner struct {
	spawnRoles     []string
	spawnCwds      []string
	spawnCommands  []string
	sendReadyCalls []string
	abortCalls     []string
}

func (r *scheduledActionLaunchRunner) Spawn(role, cwd, command string) (string, error) {
	r.spawnRoles = append(r.spawnRoles, role)
	r.spawnCwds = append(r.spawnCwds, cwd)
	r.spawnCommands = append(r.spawnCommands, command)
	return "claude-scheduled", nil
}

func (r *scheduledActionLaunchRunner) SendWhenReady(sessionID, command, text string) error {
	r.sendReadyCalls = append(r.sendReadyCalls, sessionID+"|"+command+"|"+text)
	return nil
}

func (r *scheduledActionLaunchRunner) Abort(sessionID string) error {
	r.abortCalls = append(r.abortCalls, sessionID)
	return nil
}

func (r *scheduledActionThreadRegistry) HasChatThread(threadID string) (bool, error) {
	r.thread = threadID
	return r.known, nil
}

func (w scheduledActionWatcher) GetAgent(string) *classifier.Agent {
	return w.agent
}

func (w scheduledActionWatcher) HasSession(string) bool {
	return w.hasSession
}

func TestInspectScheduledActionAcceptsOneCurrentTerminalDeliverable(t *testing.T) {
	deliverable := `Three recent AI papers point to a shared shift: inference-time
reasoning is becoming a first-class scaling axis.

The practical recommendation is to budget explicitly for verification and keep
citations attached to intermediate evidence.`
	done := time.Now().UTC()
	runner, item, run, _ := writeScheduledWorkFixture(t, scheduledDeliverableBody("\n\t"+deliverable+"  \n"), func(frontmatter *work.Frontmatter) {
		frontmatter.Done = &done
		frontmatter.Extra["outcome"] = "Digest that must not win."
		frontmatter.Extra["ai_error"] = "Diagnostic that must not win."
	})

	status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
	if !known || status != StatusCompleted || failure != "" {
		t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
	}
	if result != deliverable {
		t.Fatalf("deliverable changed:\n--- got ---\n%s\n--- want ---\n%s", result, deliverable)
	}
}

func TestInspectScheduledActionRejectsMalformedTerminalHandoffs(t *testing.T) {
	validBody := scheduledDeliverableBody("valid result")
	tests := []struct {
		name      string
		body      string
		mutate    func(*work.Frontmatter)
		mutateRun func(*Run)
	}{
		{name: "missing start", body: "result\n" + scheduledDeliverableEnd},
		{name: "missing end", body: scheduledDeliverableStart + "\nresult"},
		{name: "reversed markers", body: scheduledDeliverableEnd + "\nresult\n" + scheduledDeliverableStart},
		{name: "duplicate start", body: scheduledDeliverableStart + scheduledDeliverableStart + "result" + scheduledDeliverableEnd},
		{name: "duplicate end", body: scheduledDeliverableStart + "result" + scheduledDeliverableEnd + scheduledDeliverableEnd},
		{name: "duplicate blocks", body: scheduledDeliverableBody("one") + scheduledDeliverableBody("two")},
		{name: "empty", body: scheduledDeliverableBody(" \n\t ")},
		{name: "placeholder", body: scheduledDeliverableBody(scheduledDeliverablePlaceholder)},
		{name: "invalid UTF-8", body: scheduledDeliverableBody(string([]byte{0xff}))},
		{name: "oversize", body: scheduledDeliverableBody(strings.Repeat("x", maxScheduledDeliverableBytes+1))},
		{name: "wrong Work id", body: validBody, mutate: func(frontmatter *work.Frontmatter) { frontmatter.ID = "other-run" }},
		{name: "wrong Work kind", body: validBody, mutate: func(frontmatter *work.Frontmatter) { frontmatter.Kind = "other" }},
		{name: "wrong Calendar item link", body: validBody, mutate: func(frontmatter *work.Frontmatter) { frontmatter.Extra["calendar_item_id"] = "other-item" }},
		{name: "wrong Calendar run link", body: validBody, mutate: func(frontmatter *work.Frontmatter) { frontmatter.Extra["calendar_run_id"] = "other-run" }},
		{name: "wrong Run Work id", body: validBody, mutateRun: func(run *Run) { run.WorkID = "other-run" }},
		{name: "done and failed conflict", body: validBody, mutate: func(frontmatter *work.Frontmatter) { frontmatter.Status = "failed" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := time.Now().UTC()
			runner, item, run, _ := writeScheduledWorkFixture(t, test.body, func(frontmatter *work.Frontmatter) {
				frontmatter.Done = &done
				frontmatter.Extra["outcome"] = "Outcome fallback must not win."
				frontmatter.Extra["ai_error"] = "AI error fallback must not win."
				if test.mutate != nil {
					test.mutate(frontmatter)
				}
			})
			if test.mutateRun != nil {
				test.mutateRun(&run)
			}
			runner.Watcher = scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateDone, Summary: "Agent Summary fallback must not win."}}

			status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
			if !known || status != StatusFailed || result != "" || strings.TrimSpace(failure) == "" {
				t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
			}
			for _, forbidden := range []string{"Outcome fallback", "AI error fallback", "Agent Summary fallback"} {
				if strings.Contains(failure, forbidden) {
					t.Fatalf("failure used retired fallback %q: %q", forbidden, failure)
				}
			}
		})
	}
}

func TestInspectScheduledActionFailedWorkUsesCalendarOwnedReason(t *testing.T) {
	runner, item, run, _ := writeScheduledWorkFixture(t, scheduledDeliverableBody("must not be returned"), func(frontmatter *work.Frontmatter) {
		frontmatter.Status = "failed"
		frontmatter.Extra["outcome"] = "Outcome fallback must not win."
		frontmatter.Extra["ai_error"] = "AI error fallback must not win."
		frontmatter.Extra["friction"] = "Legacy friction must not win."
	})
	runner.Watcher = scheduledActionWatcher{agent: &classifier.Agent{
		State:   classifier.StateFailed,
		Summary: "Watcher Summary must not win after terminal Work.",
	}}

	status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
	if !known || status != StatusFailed || result != "" || failure != "Linked scheduled Work reported failure." {
		t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
	}
}

func TestInspectScheduledActionReadsCurrentBytesInsteadOfStaleWorkIndex(t *testing.T) {
	runner, item, run, indexed := writeScheduledWorkFixture(t, scheduledDeliverableBody("initial draft"), nil)
	done := time.Now().UTC()
	current := *indexed
	current.Frontmatter = indexed.Frontmatter
	current.Frontmatter.Done = &done
	current.Body = scheduledDeliverableBody("current durable result")
	raw, err := work.SerializeItem(&current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexed.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	stale, ok := runner.Store.GetByID(run.WorkID)
	if !ok || stale.Frontmatter.Done != nil || stale.Body == current.Body {
		t.Fatalf("fixture Work index is not stale: %#v", stale)
	}

	status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
	if !known || status != StatusCompleted || result != "current durable result" || failure != "" {
		t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
	}
}

func TestSchedulerCommitsOneStrictTerminalWorkDeliverableOnce(t *testing.T) {
	deliverable := "First paragraph.\n\nSecond paragraph."
	done := time.Now().UTC()
	calendarStore, scheduler, item, launched := newLinkedScheduledRunFixture(t, scheduledDeliverableBody("\n "+deliverable+" \n"), func(frontmatter *work.Frontmatter) {
		frontmatter.Done = &done
		frontmatter.Extra["outcome"] = "Digest fallback must not win."
	}, scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateDone, Summary: "Summary fallback must not win."}})

	scheduler.Tick(context.Background())
	finished, err := calendarStore.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Revision != launched.Revision+1 || len(finished.Runs) != 1 || finished.Runs[0].Status != StatusCompleted ||
		finished.Runs[0].Result != deliverable || finished.Runs[0].FailureReason != "" {
		t.Fatalf("strict terminal commit = %#v", finished)
	}
	results := calendarStore.ScheduledResults("thread-1", 0)
	if len(results) != 1 || results[0].Body != "**Scheduled result completed**\n\n"+deliverable {
		t.Fatalf("scheduled result projection = %#v", results)
	}

	scheduler.Tick(context.Background())
	repeated, err := calendarStore.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Revision != finished.Revision || len(calendarStore.ScheduledResults("thread-1", 0)) != 1 {
		t.Fatalf("terminal reconciliation was not idempotent: %#v", repeated)
	}
}

func TestSchedulerInvalidTerminalWorkFailsWithoutOutcomeOrSummaryFallback(t *testing.T) {
	done := time.Now().UTC()
	calendarStore, scheduler, item, launched := newLinkedScheduledRunFixture(t, "# No deliverable markers\n", func(frontmatter *work.Frontmatter) {
		frontmatter.Done = &done
		frontmatter.Extra["outcome"] = "Outcome fallback must not win."
		frontmatter.Extra["ai_error"] = "AI error fallback must not win."
	}, scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateDone, Summary: "Summary fallback must not win."}})

	scheduler.Tick(context.Background())
	failed, err := calendarStore.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Revision != launched.Revision+1 || failed.Runs[0].Status != StatusFailed || failed.Runs[0].Result != "" || strings.TrimSpace(failed.Runs[0].FailureReason) == "" {
		t.Fatalf("invalid terminal Work commit = %#v", failed)
	}
	for _, forbidden := range []string{"Outcome fallback", "AI error fallback", "Summary fallback"} {
		if strings.Contains(failed.Runs[0].FailureReason, forbidden) {
			t.Fatalf("invalid terminal Work used %q: %#v", forbidden, failed.Runs[0])
		}
	}
}

func TestInspectScheduledActionAgentDoneCannotManufactureSuccess(t *testing.T) {
	for _, body := range []string{scheduledDeliverableBody("draft only"), "missing markers"} {
		runner, item, run, _ := writeScheduledWorkFixture(t, body, func(frontmatter *work.Frontmatter) {
			frontmatter.Extra["outcome"] = "Outcome fallback must not win."
		})
		runner.Watcher = scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateDone, Summary: "Summary fallback must not win."}}
		status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
		if !known || status != StatusFailed || result != "" || strings.TrimSpace(failure) == "" {
			t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
		}
		if strings.Contains(failure, "Summary fallback") || strings.Contains(failure, "Outcome fallback") {
			t.Fatalf("agent completion manufactured content: %q", failure)
		}
	}
}

func TestInspectScheduledActionUsesSessionExistenceBeforeDeclaringUnobservable(t *testing.T) {
	for _, test := range []struct {
		name       string
		agent      *classifier.Agent
		hasSession bool
		wantKnown  bool
	}{
		{name: "indexed nonterminal agent", agent: &classifier.Agent{State: classifier.StateRunning}, wantKnown: true},
		{name: "watcher index not ready", hasSession: true, wantKnown: true},
		{name: "execution absent", hasSession: false, wantKnown: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, item, run, _ := writeScheduledWorkFixture(t, scheduledDeliverableBody("draft"), nil)
			runner.Watcher = scheduledActionWatcher{agent: test.agent, hasSession: test.hasSession}
			status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
			if known != test.wantKnown || status != StatusRunning || result != "" || failure != "" {
				t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
			}
		})
	}
}

func TestSchedulerRestartReconciliationWaitsForLiveSessionAndFailsTrueAbsenceOnce(t *testing.T) {
	for _, test := range []struct {
		name       string
		hasSession bool
		wantStatus Status
	}{
		{name: "live session before watcher snapshot", hasSession: true, wantStatus: StatusRunning},
		{name: "genuinely absent execution", hasSession: false, wantStatus: StatusFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
			calendarStore, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			calendarStore.now = func() time.Time { return now }
			due := now.Add(-time.Minute)
			item, err := calendarStore.Create(Item{
				ID: "calendar-restart", Title: "Restart handoff", Kind: KindScheduledAction,
				DueAt: &due, Timezone: "UTC", Recurrence: RecurrenceNone,
				ActionInstruction: "Run", SourceThreadID: "thread-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, run, err := calendarStore.Claim(item.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			launched, err := calendarStore.RecordLaunch(item.ID, run.ID, run.ID, "agent-restart")
			if err != nil {
				t.Fatal(err)
			}

			workStore, err := work.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workStore.Write(&work.Item{
				Path: filepath.Join(workStore.Root, "calendar", item.ID+"-"+run.ID+".md"),
				Body: scheduledDeliverableBody("draft"),
				Frontmatter: work.Frontmatter{
					ID: run.ID, Kind: "calendar_action", Created: now,
					Extra: map[string]interface{}{"calendar_item_id": item.ID, "calendar_run_id": run.ID},
				},
			}, time.Time{}); err != nil {
				t.Fatal(err)
			}

			runner := &WorkRunner{Store: workStore, Watcher: scheduledActionWatcher{hasSession: test.hasSession}}
			scheduler := NewScheduler(calendarStore, runner)
			scheduler.now = func() time.Time { return now }
			scheduler.Tick(context.Background())
			first, err := calendarStore.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if first.Status != test.wantStatus || first.Runs[0].Status != test.wantStatus {
				t.Fatalf("first reconciliation = %#v", first)
			}
			if first.Runs[0].Result != "" {
				t.Fatalf("restart reconciliation manufactured result: %#v", first.Runs[0])
			}
			if test.hasSession {
				if first.Revision != launched.Revision {
					t.Fatalf("live session changed revision: got %d want %d", first.Revision, launched.Revision)
				}
			} else if strings.TrimSpace(first.Runs[0].FailureReason) == "" {
				t.Fatalf("absent execution missing failure: %#v", first.Runs[0])
			}
			scheduler.Tick(context.Background())
			second, err := calendarStore.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if second.Revision != first.Revision {
				t.Fatalf("repeated reconciliation changed terminal/running state: %d -> %d", first.Revision, second.Revision)
			}
		})
	}
}

func TestInspectScheduledActionAgentFailureUsesOnlyCompactDiagnostic(t *testing.T) {
	runner, item, run, _ := writeScheduledWorkFixture(t, scheduledDeliverableBody("draft"), nil)
	runner.Watcher = scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateFailed, Summary: "  executor\n failed  "}}
	status, result, failure, known := runner.InspectScheduledAction(context.Background(), item, run)
	if !known || status != StatusFailed || result != "" || failure != "executor failed" {
		t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
	}
}

func TestScheduledWorkBodyRequiresBoundedStructuredDeliverable(t *testing.T) {
	due := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	body := scheduledWorkBody(
		Item{ID: "calendar-1", Title: "Paper review", ActionInstruction: "Summarize the papers", Timezone: "UTC"},
		Run{Title: "Paper review", ScheduledFor: due},
	)
	for _, want := range []string{
		"Write the complete result for the user between the markers below",
		"do not put execution notes or Calendar metadata in this section",
		"at or below 256 KiB",
		scheduledDeliverableStart,
		scheduledDeliverablePlaceholder,
		scheduledDeliverableEnd,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated Work body missing %q:\n%s", want, body)
		}
	}
}

func TestRunScheduledActionValidatesFrozenRunThreadBeforeLaunch(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := &scheduledActionThreadRegistry{}
	runner := &WorkRunner{Store: store, Launcher: &work.Launcher{}, Brain: registry}
	_, err = runner.RunScheduledAction(
		context.Background(),
		Item{ID: "item-1", SourceThreadID: "mutable-parent-thread"},
		Run{ID: "run-1", Title: "Frozen title", SourceThreadID: "captured-thread"},
	)
	if !errors.Is(err, ErrInvalidDeliveryTarget) {
		t.Fatalf("RunScheduledAction error = %v", err)
	}
	if registry.thread != "captured-thread" {
		t.Fatalf("validated thread = %q", registry.thread)
	}
}

func TestRunScheduledActionPersistsFreshDelegatedLaunch(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tmux := &scheduledActionLaunchRunner{}
	launcher := work.NewLauncher(tmux, work.NewExecutorConfig("claude", map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude --configured"},
		"codex":  {Name: "codex", Command: "codex"},
	}))
	runner := &WorkRunner{
		Store:    store,
		Launcher: launcher,
		Brain:    &scheduledActionThreadRegistry{known: true},
	}
	run := Run{ID: "run-1", Title: "Frozen title", SourceThreadID: "captured-thread"}

	result, err := runner.RunScheduledAction(
		context.Background(),
		Item{
			ID:                "item-1",
			Title:             "Scheduled action",
			ActionInstruction: "Treat @codex#interactive-session as ordinary text.",
			ActionCwd:         "/calendar-cwd",
		},
		run,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Launched || result.WorkID != run.ID || result.AgentSession != "claude-scheduled" {
		t.Fatalf("action result = %#v", result)
	}
	if len(tmux.spawnRoles) != 1 || tmux.spawnRoles[0] != "claude" || tmux.spawnCwds[0] != "/calendar-cwd" || tmux.spawnCommands[0] != "claude --configured --permission-mode bypassPermissions" {
		t.Fatalf("spawn = roles %#v, cwds %#v, commands %#v", tmux.spawnRoles, tmux.spawnCwds, tmux.spawnCommands)
	}
	if len(tmux.sendReadyCalls) != 1 {
		t.Fatalf("ready sends = %#v", tmux.sendReadyCalls)
	}
	if len(tmux.abortCalls) != 0 {
		t.Fatalf("aborts = %#v, want none after successful launch", tmux.abortCalls)
	}
	written, ok := store.GetByID(run.ID)
	if !ok || written.Frontmatter.Started == nil || written.Frontmatter.AgentSession != "claude-scheduled" {
		t.Fatalf("persisted Work = %#v, found = %v", written, ok)
	}
}

func TestUnsupportedScheduledExecutorFailsOccurrenceBeforeAnyPromptCanRun(t *testing.T) {
	now := time.Date(2026, time.July, 25, 9, 0, 0, 0, time.UTC)
	calendarStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calendarStore.now = func() time.Time { return now }
	due := now
	item, err := calendarStore.Create(Item{
		Title:             "Unsupported unattended run",
		Kind:              KindScheduledAction,
		DueAt:             &due,
		Timezone:          "UTC",
		Recurrence:        RecurrenceNone,
		ActionInstruction: "Do not wait for approval.",
		SourceThreadID:    "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	workStore, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tmux := &scheduledActionLaunchRunner{}
	launcher := work.NewLauncher(tmux, work.NewExecutorConfig("my-agent", map[string]work.Executor{
		"my-agent": {Name: "my-agent", Command: "my-agent --interactive", Kind: "custom"},
	}))
	runner := &WorkRunner{
		Store:    workStore,
		Launcher: launcher,
		Brain:    &scheduledActionThreadRegistry{known: true},
	}
	scheduler := NewScheduler(calendarStore, runner)
	scheduler.now = func() time.Time { return now }

	finished, err := scheduler.run(context.Background(), item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusFailed || len(finished.Runs) != 1 || finished.Runs[0].Status != StatusFailed {
		t.Fatalf("unsupported scheduled run did not terminate: %#v", finished)
	}
	if finished.Runs[0].Result != "" ||
		!strings.Contains(finished.Runs[0].FailureReason, `executor "my-agent" uses unsupported provider "custom"`) {
		t.Fatalf("unsupported scheduled failure = %#v", finished.Runs[0])
	}
	if finished.SourceThreadID != "thread-1" || scheduler.isLaunching(item.ID) {
		t.Fatalf("delivery or launch lifecycle changed: item=%#v launching=%v", finished, scheduler.isLaunching(item.ID))
	}
	if len(tmux.spawnCommands) != 0 || len(tmux.sendReadyCalls) != 0 || len(tmux.abortCalls) != 0 {
		t.Fatalf("unsupported executor reached an approval-capable process: spawn=%#v send=%#v abort=%#v",
			tmux.spawnCommands, tmux.sendReadyCalls, tmux.abortCalls)
	}
}

func writeScheduledWorkFixture(t *testing.T, body string, mutate func(*work.Frontmatter)) (*WorkRunner, Item, Run, *work.Item) {
	t.Helper()
	root := t.TempDir()
	store, err := work.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := Item{ID: "calendar-1", Title: "Scheduled result", Timezone: "UTC"}
	run := Run{ID: "run-1", WorkID: "run-1", AgentSession: "agent-1", Title: item.Title}
	frontmatter := work.Frontmatter{
		ID:      run.ID,
		Kind:    "calendar_action",
		Created: time.Now().UTC(),
		Extra: map[string]interface{}{
			"calendar_item_id": item.ID,
			"calendar_run_id":  run.ID,
		},
	}
	if mutate != nil {
		mutate(&frontmatter)
	}
	written, err := store.Write(&work.Item{
		Path:        filepath.Join(root, "calendar", item.ID+"-"+run.ID+".md"),
		Body:        body,
		Frontmatter: frontmatter,
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return &WorkRunner{Store: store}, item, run, written
}

func newLinkedScheduledRunFixture(t *testing.T, body string, mutate func(*work.Frontmatter), watcher scheduledActionWatcher) (*Store, *Scheduler, Item, Item) {
	t.Helper()
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	calendarStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calendarStore.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := calendarStore.Create(Item{
		ID: "calendar-result", Title: "Scheduled result", Kind: KindScheduledAction,
		DueAt: &due, Timezone: "UTC", Recurrence: RecurrenceNone,
		ActionInstruction: "Run", SourceThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := calendarStore.Claim(item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	launched, err := calendarStore.RecordLaunch(item.ID, run.ID, run.ID, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	workStore, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	frontmatter := work.Frontmatter{
		ID: run.ID, Kind: "calendar_action", Created: now,
		Extra: map[string]interface{}{"calendar_item_id": item.ID, "calendar_run_id": run.ID},
	}
	if mutate != nil {
		mutate(&frontmatter)
	}
	if _, err := workStore.Write(&work.Item{
		Path: filepath.Join(workStore.Root, "calendar", item.ID+"-"+run.ID+".md"), Body: body, Frontmatter: frontmatter,
	}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	runner := &WorkRunner{Store: workStore, Watcher: watcher}
	scheduler := NewScheduler(calendarStore, runner)
	scheduler.now = func() time.Time { return now }
	return calendarStore, scheduler, item, launched
}

func scheduledDeliverableBody(deliverable string) string {
	return "# Scheduled result\n\n" + scheduledDeliverableStart + "\n" + deliverable + "\n" + scheduledDeliverableEnd + "\n"
}
