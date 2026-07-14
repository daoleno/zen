package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func localTime(t *testing.T, zone string, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func reminder(at time.Time) Item {
	return Item{Title: "Take a breath", Kind: KindReminder, NotifyAt: &at, Timezone: "America/New_York", Recurrence: RecurrenceNone}
}

func TestStoreCreateUpdateCancelAndRevisionConflict(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := localTime(t, "America/New_York", 2026, time.July, 14, 9, 30)
	created, err := store.Create(reminder(at))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != StatusScheduled || created.NextAt != at || created.Revision != 1 {
		t.Fatalf("created = %#v", created)
	}
	created.Title = "Breathe slowly"
	updated, err := store.Update(created, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Title != "Breathe slowly" {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := store.Update(created, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	cancelled, err := store.Cancel(created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancelled = %#v", cancelled)
	}
}

func TestStoreMigratesBareArrayAndPreservesRunningClaimForReconciliation(t *testing.T) {
	root := t.TempDir()
	at := localTime(t, "America/New_York", 2026, time.July, 14, 9, 30)
	started := at.Add(-time.Minute)
	item := Item{ID: "action-1", Title: "Ship it", Kind: KindScheduledAction, Status: StatusRunning, DueAt: &at, NextAt: at, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Run tests", CreatedAt: started, UpdatedAt: started, Revision: 1, Runs: []Run{{ID: "run-1", ScheduledFor: at, StartedAt: started, Status: StatusRunning}}}
	raw, _ := json.Marshal([]Item{item})
	if err := os.WriteFile(filepath.Join(root, "calendar.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusRunning || recovered.Runs[0].Status != StatusRunning {
		t.Fatalf("recovered = %#v", recovered)
	}
	persisted, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(persisted, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %d", doc.SchemaVersion)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestStoreEnforcesPrivateDirectoryAndFileModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "calendar")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("modes directory=%o file=%o", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

func TestValidationRequiresKindSpecificTimeAndMatchingTimezoneOffset(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(Item{Title: "No end", Kind: KindEvent, Timezone: "UTC"}); err == nil {
		t.Fatal("expected event validation error")
	}
	wrong := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.FixedZone("wrong", 0))
	if _, err := store.Create(reminder(wrong)); err == nil {
		t.Fatal("expected timezone offset validation error")
	}
	if _, err := store.Create(Item{Title: "Empty action", Kind: KindScheduledAction, DueAt: &wrong, Timezone: "UTC", Recurrence: RecurrenceNone}); err == nil {
		t.Fatal("expected action instruction validation error")
	}
	if _, err := store.Create(Item{Title: "Untargeted action", Kind: KindScheduledAction, DueAt: &wrong, Timezone: "UTC", Recurrence: RecurrenceNone, ActionInstruction: "Run it"}); err == nil || !strings.Contains(err.Error(), "source_thread_id") {
		t.Fatalf("target validation error = %v", err)
	}
}

func TestNextOccurrencePreservesWallClockAcrossDSTAndWeekdays(t *testing.T) {
	beforeDST := localTime(t, "America/New_York", 2026, time.March, 7, 9, 0)
	next, ok := NextOccurrence(beforeDST, RecurrenceDaily, "America/New_York")
	if !ok {
		t.Fatal("missing recurrence")
	}
	if next.Hour() != 9 || next.Day() != 8 {
		t.Fatalf("next = %v", next)
	}
	_, oldOffset := beforeDST.Zone()
	_, newOffset := next.Zone()
	if oldOffset == newOffset {
		t.Fatalf("expected DST offset change: %v -> %v", beforeDST, next)
	}
	friday := localTime(t, "America/New_York", 2026, time.July, 17, 9, 0)
	monday, _ := NextOccurrence(friday, RecurrenceWeekdays, "America/New_York")
	if monday.Weekday() != time.Monday || monday.Day() != 20 {
		t.Fatalf("weekday next = %v", monday)
	}
}

func TestResolveLocalDateTimeRequiresExplicitDSTChoice(t *testing.T) {
	normal, err := ResolveLocalDateTime("2026-07-14", "18:20", "Asia/Shanghai", "")
	if err != nil || normal.UTC().Format(time.RFC3339) != "2026-07-14T10:20:00Z" {
		t.Fatalf("normal = %v, err=%v", normal, err)
	}
	if _, err := ResolveLocalDateTime("2026-03-08", "02:30", "America/New_York", ""); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("gap error = %v", err)
	}
	if _, err := ResolveLocalDateTime("2026-11-01", "01:30", "America/New_York", ""); err == nil || !strings.Contains(err.Error(), "occurs twice") {
		t.Fatalf("ambiguity error = %v", err)
	}
	first, err := ResolveLocalDateTime("2026-11-01", "01:30", "America/New_York", "first")
	if err != nil || first.UTC().Format(time.RFC3339) != "2026-11-01T05:30:00Z" {
		t.Fatalf("first = %v, err=%v", first, err)
	}
	second, err := ResolveLocalDateTime("2026-11-01", "01:30", "America/New_York", "second")
	if err != nil || second.UTC().Format(time.RFC3339) != "2026-11-01T06:30:00Z" {
		t.Fatalf("second = %v, err=%v", second, err)
	}
}

func TestNextOccurrenceSkipsGapAndKeepsAmbiguousOffset(t *testing.T) {
	beforeGap := localTime(t, "America/New_York", 2026, time.March, 7, 2, 30)
	afterGap, ok := NextOccurrence(beforeGap, RecurrenceDaily, "America/New_York")
	if !ok || afterGap.Day() != 9 || afterGap.Hour() != 2 || afterGap.Minute() != 30 {
		t.Fatalf("gap occurrence = %v, ok=%v", afterGap, ok)
	}

	beforeRepeat := localTime(t, "America/New_York", 2026, time.October, 31, 1, 30)
	repeated, ok := NextOccurrence(beforeRepeat, RecurrenceDaily, "America/New_York")
	if !ok {
		t.Fatal("missing repeated occurrence")
	}
	_, previousOffset := beforeRepeat.Zone()
	_, repeatedOffset := repeated.Zone()
	if repeatedOffset != previousOffset {
		t.Fatalf("offset changed across ambiguity: %v -> %v", beforeRepeat, repeated)
	}
}

func TestRecurringEventPreservesIndependentEndWallClockAcrossDST(t *testing.T) {
	start := localTime(t, "America/New_York", 2026, time.March, 7, 1, 30)
	end := localTime(t, "America/New_York", 2026, time.March, 7, 3, 30)
	item := Item{Kind: KindEvent, StartAt: &start, EndAt: &end, Timezone: "America/New_York", Recurrence: RecurrenceDaily}
	if !advanceItem(&item) {
		t.Fatal("event did not advance")
	}
	// March 8 skips 02:00, so the absolute duration is one hour. The user's
	// independent wall-clock end remains 03:30 instead of shifting to 04:30.
	if item.StartAt.Day() != 8 || item.StartAt.Hour() != 1 || item.EndAt.Day() != 8 || item.EndAt.Hour() != 3 || item.EndAt.Minute() != 30 {
		t.Fatalf("advanced event = %v to %v", item.StartAt, item.EndAt)
	}
}

func TestRecurringEventSkipsOccurrenceWhenEndFallsInDSTGap(t *testing.T) {
	start := localTime(t, "America/New_York", 2026, time.March, 7, 1, 30)
	end := localTime(t, "America/New_York", 2026, time.March, 7, 2, 30)
	item := Item{Kind: KindEvent, StartAt: &start, EndAt: &end, Timezone: "America/New_York", Recurrence: RecurrenceDaily}
	if !advanceItem(&item) {
		t.Fatal("event did not advance")
	}
	if item.StartAt.Day() != 9 || item.EndAt.Day() != 9 || item.StartAt.Hour() != 1 || item.EndAt.Hour() != 2 {
		t.Fatalf("gap event = %v to %v", item.StartAt, item.EndAt)
	}
}

type fakeRunner struct {
	mu            sync.Mutex
	calls         int
	block         chan struct{}
	inspectStatus Status
	inspectKnown  bool
	result        ActionResult
	err           error
}

type retryingDeliveryRunner struct {
	deliveries int
}

func (r *retryingDeliveryRunner) RunScheduledAction(context.Context, Item, Run) (ActionResult, error) {
	return ActionResult{WorkID: "work-1", AgentSession: "agent-1", Launched: true}, nil
}

func (r *retryingDeliveryRunner) InspectScheduledAction(context.Context, Item, Run) (Status, string, string, bool) {
	return StatusCompleted, "durable result", "", true
}

func (r *retryingDeliveryRunner) DeliverScheduledAction(context.Context, Item, Run, Status, string, string) error {
	r.deliveries++
	if r.deliveries == 1 {
		return errors.New("temporary Brain write failure")
	}
	return nil
}

func (r *fakeRunner) RunScheduledAction(_ context.Context, _ Item, _ Run) (ActionResult, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.block != nil {
		<-r.block
	}
	if r.result.WorkID == "" {
		r.result = ActionResult{WorkID: "work-1", AgentSession: "agent-1", Result: "started", Launched: true}
	}
	return r.result, r.err
}

func TestPersistenceFailureRollsBackEveryStateTransition(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	reminderAt := now.Add(time.Hour)
	rem, err := store.Create(reminder(reminderAt))
	if err != nil {
		t.Fatal(err)
	}
	actionAt := now.Add(2 * time.Hour)
	action, err := store.Create(Item{Title: "Atomic action", Kind: KindScheduledAction, DueAt: &actionAt, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Run it", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(action.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	assertUnchanged := func(id string, before Item) {
		t.Helper()
		after, getErr := store.Get(id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("state changed after persistence failure\nbefore: %#v\nafter:  %#v", before, after)
		}
	}

	// Renaming the temporary file over a directory fails after the temp file has
	// been synced, exercising the rollback path without permission assumptions.
	store.path = root
	if _, err := store.Cancel(rem.ID, rem.Revision); err == nil {
		t.Fatal("expected cancel persistence failure")
	}
	assertUnchanged(rem.ID, rem)
	updated := rem
	updated.Title = "Must not leak into memory"
	if _, err := store.Update(updated, rem.Revision); err == nil {
		t.Fatal("expected update persistence failure")
	}
	assertUnchanged(rem.ID, rem)
	if _, err := store.SetStatus(rem.ID, StatusWaiting, "must roll back"); err == nil {
		t.Fatal("expected status persistence failure")
	}
	assertUnchanged(rem.ID, rem)

	claimed, err := store.Get(action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordLaunch(action.ID, run.ID, "work-1", "agent-1", "started"); err == nil {
		t.Fatal("expected launch persistence failure")
	}
	assertUnchanged(action.ID, claimed)
	if _, err := store.FinishRun(action.ID, run.ID, "done", ""); err == nil {
		t.Fatal("expected finish persistence failure")
	}
	assertUnchanged(action.ID, claimed)
}

func TestPartialLaunchPersistenceFailureStaysRunningForReconciliation(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(time.Hour)
	item, _ := store.Create(Item{Title: "Launch", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Launch work", SourceThreadID: "thread-1"})
	runner := &fakeRunner{
		result: ActionResult{WorkID: "work-1", AgentSession: "agent-1", Result: "agent launched", Launched: true},
		err:    errors.New("write started Work frontmatter"),
	}
	scheduler := NewScheduler(store, runner)
	got, err := scheduler.RunNow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning || len(got.Runs) != 1 || got.Runs[0].Status != StatusRunning {
		t.Fatalf("partial launch was treated as terminal: %#v", got)
	}
	if got.Runs[0].WorkID != "work-1" || got.Runs[0].AgentSession != "agent-1" || !strings.Contains(got.Runs[0].Result, "Launch succeeded") {
		t.Fatalf("partial launch evidence missing: %#v", got.Runs[0])
	}
}

func (r *fakeRunner) InspectScheduledAction(_ context.Context, _ Item, _ Run) (Status, string, string, bool) {
	return r.inspectStatus, "finished", "", r.inspectKnown
}

func TestSchedulerClaimsOnceAndBoundedCatchUp(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := localTime(t, "America/New_York", 2026, time.July, 14, 9, 30)
	store.now = func() time.Time { return now }
	due := now.Add(-5 * time.Minute)
	action, err := store.Create(Item{Title: "Run report", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Generate report", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{block: make(chan struct{}), inspectStatus: StatusCompleted, inspectKnown: true}
	scheduler := NewScheduler(store, runner)
	scheduler.now = func() time.Time { return now }
	for range 4 {
		scheduler.Tick(context.Background())
	}
	deadline := time.Now().Add(time.Second)
	for {
		runner.mu.Lock()
		calls := runner.calls
		runner.mu.Unlock()
		if calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls = %d", calls)
		}
		time.Sleep(time.Millisecond)
	}
	close(runner.block)
	deadline = time.Now().Add(time.Second)
	for {
		scheduler.Tick(context.Background())
		current, _ := store.Get(action.ID)
		if current.Status == StatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %s", current.Status)
		}
		time.Sleep(time.Millisecond)
	}
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}

	missedDue := now.Add(-DefaultMissedActionWindow - time.Second)
	missed, err := store.Create(Item{Title: "Old action", Kind: KindScheduledAction, DueAt: &missedDue, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Do old work", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Tick(context.Background())
	got, _ := store.Get(missed.ID)
	if got.Status != StatusFailed || got.FailureReason == "" {
		t.Fatalf("missed = %#v", got)
	}
}

func TestRestartReconciliationNeverRelaunchesUnknownWork(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, _ := store.Create(Item{Title: "Deploy", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Deploy", SourceThreadID: "thread-1"})
	_, run, err := store.Claim(item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordLaunch(item.ID, run.ID, "work-1", "agent-gone", "launched"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{inspectKnown: false}
	scheduler := NewScheduler(store, runner)
	scheduler.now = func() time.Time { return now }
	scheduler.Tick(context.Background())
	got, _ := store.Get(item.ID)
	if runner.calls != 0 {
		t.Fatalf("relaunched %d times", runner.calls)
	}
	if got.Status != StatusFailed || got.FailureReason == "" {
		t.Fatalf("got = %#v", got)
	}
}

func TestSchedulerRetriesDeliveryWithoutRelaunchingOccurrence(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := store.Create(Item{Title: "Deliver", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Deliver", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &retryingDeliveryRunner{}
	scheduler := NewScheduler(store, runner)
	if _, err := scheduler.RunNow(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	scheduler.Tick(context.Background())
	first, _ := store.Get(item.ID)
	if first.Status != StatusRunning || runner.deliveries != 1 {
		t.Fatalf("first = %#v, deliveries = %d", first, runner.deliveries)
	}
	scheduler.Tick(context.Background())
	finished, _ := store.Get(item.ID)
	if finished.Status != StatusCompleted || runner.deliveries != 2 || len(finished.Runs) != 1 {
		t.Fatalf("finished = %#v, deliveries = %d", finished, runner.deliveries)
	}
}

func TestRunNowDoesNotConsumeAStillFutureScheduledOccurrence(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(time.Hour)
	item, err := store.Create(Item{Title: "Future deploy", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Deploy", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(item.ID, run.ID, "manual work finished", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(item.ID)
	if got.Status != StatusScheduled || !got.NextAt.Equal(due) {
		t.Fatalf("got = %#v", got)
	}
}

func TestFailedRunNowDoesNotConsumeAStillFutureScheduledOccurrence(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(time.Hour)
	item, err := store.Create(Item{Title: "Future deploy", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceWeekly, ActionInstruction: "Deploy", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(item.ID, run.ID, "", "manual attempt failed"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(item.ID)
	if got.Status != StatusScheduled || !got.NextAt.Equal(due) || got.Runs[0].Status != StatusFailed {
		t.Fatalf("got = %#v", got)
	}
}

func TestFailedRecurringOccurrenceAdvancesAndRemainsRecorded(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := store.Create(Item{Title: "Daily report", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceDaily, ActionInstruction: "Report", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.FinishRun(item.ID, run.ID, "", "executor failed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusScheduled || !got.NextAt.After(now) || got.Runs[0].Status != StatusFailed || got.FailureReason != "executor failed" {
		t.Fatalf("got = %#v", got)
	}
}

func TestSchedulerReminderDeadlineEventAndRecurringAction(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	notify := now.Add(-time.Minute)
	rem, _ := store.Create(reminder(notify))
	start := now.Add(-time.Hour)
	end := now.Add(-time.Minute)
	event, _ := store.Create(Item{Title: "Focus", Kind: KindEvent, StartAt: &start, EndAt: &end, Timezone: "America/New_York", Recurrence: RecurrenceDaily})
	runner := &fakeRunner{}
	scheduler := NewScheduler(store, runner)
	scheduler.now = func() time.Time { return now }
	scheduler.Tick(context.Background())
	gotReminder, _ := store.Get(rem.ID)
	if gotReminder.Status != StatusWaiting {
		t.Fatalf("reminder status = %s", gotReminder.Status)
	}
	recurringAt := now.Add(-48 * time.Hour)
	recurring, _ := store.Create(Item{Title: "Daily", Kind: KindReminder, NotifyAt: &recurringAt, Timezone: "America/New_York", Recurrence: RecurrenceDaily})
	scheduler.Tick(context.Background())
	gotRecurring, _ := store.Get(recurring.ID)
	if gotRecurring.Status != StatusScheduled || !gotRecurring.NextAt.After(now) || gotRecurring.NextAt.Hour() != recurringAt.Hour() {
		t.Fatalf("recurring = %#v", gotRecurring)
	}
	gotEvent, _ := store.Get(event.ID)
	if gotEvent.Status != StatusScheduled || gotEvent.StartAt.Day() != start.Day()+1 {
		t.Fatalf("event = %#v", gotEvent)
	}
}
