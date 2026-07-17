package calendar

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestRunProjectionFreezesPresentationAcrossItemEditAndRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	claimedAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return claimedAt }
	due := claimedAt.Add(time.Hour)
	item, err := store.Create(Item{
		ID: "item-1", Title: "Original title", Kind: KindScheduledAction,
		DueAt: &due, Timezone: "UTC", Recurrence: RecurrenceNone,
		ActionInstruction: "Build it", SourceThreadID: "thread-original",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if run.Title != "Original title" || run.SourceThreadID != "thread-original" {
		t.Fatalf("claimed Run did not freeze presentation: %#v", run)
	}
	finishedAt := claimedAt.Add(2 * time.Minute)
	store.now = func() time.Time { return finishedAt }
	finished, err := store.FinishRun(item.ID, run.ID, "Durable answer.", "")
	if err != nil {
		t.Fatal(err)
	}
	finished.Title = "Renamed series"
	finished.SourceThreadID = "thread-retargeted"
	store.now = func() time.Time { return finishedAt.Add(time.Minute) }
	if _, err := store.Update(finished, finished.Revision); err != nil {
		t.Fatal(err)
	}

	want := []ScheduledResult{{
		ID:             "calendar_result:item-1:" + run.ID,
		ThreadID:       "thread-original",
		Body:           "**Original title completed**\n\nDurable answer.",
		CreatedAt:      finishedAt,
		Status:         StatusCompleted,
		Title:          "Original title",
		CalendarItemID: "item-1",
		CalendarRunID:  run.ID,
		ScheduledFor:   claimedAt,
	}}
	if got := store.ScheduledResults("thread-original", 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("projection after edit = %#v, want %#v", got, want)
	}
	if got := store.ScheduledResults("thread-retargeted", 0); len(got) != 0 {
		t.Fatalf("mutable parent thread changed historical projection: %#v", got)
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if got := store.ScheduledResults("thread-original", 10); !reflect.DeepEqual(got, want) {
			t.Fatalf("repeated projection changed: %#v", got)
		}
	}
	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.ScheduledResults("thread-original", 10); !reflect.DeepEqual(got, want) {
		t.Fatalf("restart projection = %#v, want %#v", got, want)
	}
	assertCalendarFixtureUnchanged(t, store.Path(), raw, before)
}

func TestScheduledResultsFilterOrderLimitAndFormatting(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	finishedAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return finishedAt }

	type outcome struct {
		itemID, title, thread, result, failure string
	}
	outcomes := []outcome{
		{itemID: "item-b", title: "Completed job", thread: "thread-1", result: "Complete durable result."},
		{itemID: "item-a", title: "Failed job", thread: "thread-1", failure: strings.Repeat(" noisy \n failure ", 40)},
		{itemID: "item-c", title: "Other thread", thread: "thread-2", result: "Other result"},
	}
	for _, outcome := range outcomes {
		due := finishedAt.Add(time.Hour)
		item, err := store.Create(Item{
			ID: outcome.itemID, Title: outcome.title, Kind: KindScheduledAction,
			DueAt: &due, Timezone: "UTC", Recurrence: RecurrenceNone,
			ActionInstruction: "Run", SourceThreadID: outcome.thread,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, run, err := store.Claim(item.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishRun(item.ID, run.ID, outcome.result, outcome.failure); err != nil {
			t.Fatal(err)
		}
	}

	threadOne := store.ScheduledResults("thread-1", 0)
	if len(threadOne) != 2 {
		t.Fatalf("thread results = %#v", threadOne)
	}
	ids := []string{threadOne[0].ID, threadOne[1].ID}
	wantIDs := append([]string(nil), ids...)
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("equal-time IDs not deterministic: %v", ids)
	}
	if limited := store.ScheduledResults("thread-1", 1); len(limited) != 1 || limited[0].ID != threadOne[1].ID {
		t.Fatalf("newest limit = %#v", limited)
	}
	if other := store.ScheduledResults("thread-2", 0); len(other) != 1 || other[0].Title != "Other thread" {
		t.Fatalf("exact thread filter = %#v", other)
	}

	byTitle := map[string]ScheduledResult{}
	for _, result := range threadOne {
		byTitle[result.Title] = result
	}
	if got := byTitle["Completed job"].Body; got != "**Completed job completed**\n\nComplete durable result." {
		t.Fatalf("completion body = %q", got)
	}
	failed := byTitle["Failed job"]
	if !strings.HasPrefix(failed.Body, "**Failed job failed**\n\n") || strings.Contains(failed.Body, "\n failure") {
		t.Fatalf("failed body was not compacted: %q", failed.Body)
	}
	stored, err := store.Get("item-a")
	if err != nil {
		t.Fatal(err)
	}
	if got := []rune(stored.Runs[0].FailureReason); len(got) != maxScheduledFailureRunes || !strings.HasSuffix(string(got), "...") {
		t.Fatalf("canonical failure length/suffix = %d %q", len(got), string(got))
	}
}

func TestRecordLaunchThenEmptySuccessfulFinishIsRejectedWithoutMutation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := store.Create(Item{
		ID: "item-empty-result", Title: "No deliverable", Kind: KindScheduledAction,
		DueAt: &due, Timezone: "UTC", Recurrence: RecurrenceNone,
		ActionInstruction: "Run", SourceThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	launched, err := store.RecordLaunch(item.ID, run.ID, "work-1", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := launched.Runs[0].Result; got != "" {
		t.Fatalf("launch polluted terminal result: %q", got)
	}
	if got := store.ScheduledResults("thread-1", 0); len(got) != 0 {
		t.Fatalf("running launch projected as a result: %#v", got)
	}

	beforeRaw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(item.ID, run.ID, "", ""); err == nil {
		t.Fatal("empty completion was accepted")
	}
	after, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, launched) {
		t.Fatalf("empty completion changed state\nbefore: %#v\nafter:  %#v", launched, after)
	}
	assertCalendarFixtureUnchanged(t, store.Path(), beforeRaw, beforeInfo)
	if results := store.ScheduledResults("thread-1", 0); len(results) != 0 {
		t.Fatalf("invalid completion projected: %#v", results)
	}
}

func TestScheduledResultsSkipMalformedTerminalRunsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	finishedAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	scheduledFor := finishedAt.Add(-time.Hour)
	item := Item{
		ID: "item-1", Kind: KindScheduledAction,
		Runs: []Run{
			{ID: "missing-snapshot", Title: "Incomplete", SourceThreadID: "thread-1", Status: StatusCompleted, FinishedAt: &finishedAt, Result: "hidden"},
			{ID: "empty-success", Title: "Empty", SourceThreadID: "thread-1", ScheduledFor: scheduledFor, Status: StatusCompleted, FinishedAt: &finishedAt},
			{ID: "empty-failure", Title: "Empty failure", SourceThreadID: "thread-1", ScheduledFor: scheduledFor, Status: StatusFailed, FinishedAt: &finishedAt},
			{ID: "ambiguous", Title: "Ambiguous", SourceThreadID: "thread-1", ScheduledFor: scheduledFor, Status: StatusFailed, FinishedAt: &finishedAt, Result: "result", FailureReason: "failure"},
			{ID: "success", Title: "Valid success", SourceThreadID: "thread-1", ScheduledFor: scheduledFor, Status: StatusCompleted, FinishedAt: &finishedAt, Result: "durable result"},
			{ID: "failure", Title: "Valid failure", SourceThreadID: "thread-1", ScheduledFor: scheduledFor, Status: StatusFailed, FinishedAt: &finishedAt, FailureReason: "compact failure"},
		},
	}
	raw, err := json.Marshal(document{SchemaVersion: SchemaVersion, Items: []Item{item}})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path, before := writeCalendarFixture(t, root, raw)
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got := store.ScheduledResults("thread-1", 0)
	if len(got) != 2 || got[0].Body != "**Valid failure failed**\n\ncompact failure" || got[1].Body != "**Valid success completed**\n\ndurable result" {
		t.Fatalf("terminal projections = %#v", got)
	}
	gotRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotRaw, raw) {
		t.Fatalf("projection rewrote malformed fixture: got %q want %q", gotRaw, raw)
	}
	assertCalendarFixtureUnchanged(t, path, raw, before)
}

func TestScheduledResultProjectionRejectsMalformedTerminalPayloads(t *testing.T) {
	finishedAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	scheduledFor := finishedAt.Add(-time.Hour)
	base := Run{
		ID: "run-1", Title: "Result", SourceThreadID: "thread-1",
		ScheduledFor: scheduledFor, FinishedAt: &finishedAt,
	}
	tests := []struct {
		name   string
		mutate func(*Run)
	}{
		{name: "empty success", mutate: func(run *Run) { run.Status = StatusCompleted }},
		{name: "placeholder success", mutate: func(run *Run) { run.Status, run.Result = StatusCompleted, scheduledDeliverablePlaceholder }},
		{name: "oversize success", mutate: func(run *Run) {
			run.Status, run.Result = StatusCompleted, strings.Repeat("x", maxScheduledDeliverableBytes+1)
		}},
		{name: "invalid UTF-8 success", mutate: func(run *Run) { run.Status, run.Result = StatusCompleted, string([]byte{0xff}) }},
		{name: "success with failure", mutate: func(run *Run) { run.Status, run.Result, run.FailureReason = StatusCompleted, "result", "failure" }},
		{name: "empty failure", mutate: func(run *Run) { run.Status = StatusFailed }},
		{name: "failure with result", mutate: func(run *Run) { run.Status, run.Result, run.FailureReason = StatusFailed, "result", "failure" }},
		{name: "noncompact failure", mutate: func(run *Run) { run.Status, run.FailureReason = StatusFailed, "  failure  " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := base
			test.mutate(&run)
			if result, ok := scheduledResultFromRun("item-1", run); ok {
				t.Fatalf("malformed Run projected: %#v", result)
			}
		})
	}
}
