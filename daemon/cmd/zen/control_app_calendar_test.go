package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/control"
)

func TestCalendarControlToolsCRUDAndResolvedTimeConfirmation(t *testing.T) {
	store, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{calendarStore: store}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	notify := time.Date(2026, time.July, 15, 9, 30, 0, 0, loc)
	resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: &calendar.Item{Title: "提交周报", Kind: calendar.KindReminder, NotifyAt: &notify, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceWeekdays, SourceThreadID: "brain-thread-1"}})
	if !resp.OK || resp.CalendarItem == nil {
		t.Fatalf("create = %#v", resp)
	}
	for _, want := range []string{"2026-07-15 09:30:00 CST", "Asia/Shanghai", "Zen will notify you"} {
		if !strings.Contains(resp.Confirmation, want) {
			t.Fatalf("confirmation %q missing %q", resp.Confirmation, want)
		}
	}
	created := *resp.CalendarItem
	got := app.HandleControlRequest(control.Request{Type: "calendar_get", ID: created.ID})
	if !got.OK || got.CalendarItem.SourceThreadID != "brain-thread-1" {
		t.Fatalf("get = %#v", got)
	}
	created.Title = "提交项目周报"
	updated := app.HandleControlRequest(control.Request{Type: "calendar_update", Revision: created.Revision, CalendarItem: &created})
	if !updated.OK || updated.CalendarItem.Title != "提交项目周报" {
		t.Fatalf("update = %#v", updated)
	}
	conflict := app.HandleControlRequest(control.Request{Type: "calendar_update", Revision: created.Revision, CalendarItem: &created})
	if conflict.OK || conflict.Error.Code != "conflict" {
		t.Fatalf("conflict = %#v", conflict)
	}
	listed := app.HandleControlRequest(control.Request{Type: "calendar_list"})
	if len(listed.CalendarItems) != 1 {
		t.Fatalf("list = %#v", listed)
	}
	cancelled := app.HandleControlRequest(control.Request{Type: "calendar_cancel", ID: created.ID, Revision: updated.CalendarItem.Revision})
	if !cancelled.OK || cancelled.CalendarItem.Status != calendar.StatusCancelled {
		t.Fatalf("cancel = %#v", cancelled)
	}
	if !strings.Contains(cancelled.Confirmation, "will not act on it") {
		t.Fatalf("cancel confirmation = %q", cancelled.Confirmation)
	}
}

type calendarControlRunner struct{}

func (calendarControlRunner) RunScheduledAction(context.Context, calendar.Item, calendar.Run) (calendar.ActionResult, error) {
	return calendar.ActionResult{WorkID: "work-1", AgentSession: "agent-1", Launched: true}, nil
}

func TestCalendarRunControlConfirmationDescribesLaunchNotCompletion(t *testing.T) {
	store, _ := calendar.NewStore(t.TempDir())
	loc, _ := time.LoadLocation("Asia/Shanghai")
	due := time.Now().In(loc).Add(time.Hour)
	item, err := store.Create(calendar.Item{Title: "Prepare report", Kind: calendar.KindScheduledAction, DueAt: &due, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone, ActionInstruction: "Write the report", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{calendarStore: store, calendarScheduler: calendar.NewScheduler(store, calendarControlRunner{})}
	resp := app.HandleControlRequest(control.Request{Type: "calendar_run", ID: item.ID})
	if !resp.OK || resp.CalendarItem.Status != calendar.StatusRunning {
		t.Fatalf("run = %#v", resp)
	}
	for _, want := range []string{"Asia/Shanghai", "launched visible Work", "reconcile it to completed or failed"} {
		if !strings.Contains(resp.Confirmation, want) {
			t.Fatalf("confirmation %q missing %q", resp.Confirmation, want)
		}
	}
	if strings.Contains(strings.ToLower(resp.Confirmation), "completed") && !strings.Contains(resp.Confirmation, "completed or failed") {
		t.Fatalf("confirmation reports false completion: %q", resp.Confirmation)
	}
}

func TestCalendarControlToolRejectsAmbiguousOrMismatchedTime(t *testing.T) {
	store, _ := calendar.NewStore(t.TempDir())
	app := &controlApp{calendarStore: store}
	wrong := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)
	resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: &calendar.Item{Title: "Bad", Kind: calendar.KindDeadline, DueAt: &wrong, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone}})
	if resp.OK || resp.Error == nil || !strings.Contains(resp.Error.Message, "offset does not match") {
		t.Fatalf("response = %#v", resp)
	}
}
