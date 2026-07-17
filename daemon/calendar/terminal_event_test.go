package calendar

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFinishRunEmitsOneCommittedScheduledResultEvent(t *testing.T) {
	store, item, run := newTerminalEventFixture(t)
	subID, events := store.Subscribe()
	defer store.Unsubscribe(subID)

	finished, err := store.FinishRun(item.ID, run.ID, "Durable result.", "")
	if err != nil {
		t.Fatal(err)
	}
	event := receiveCalendarEvent(t, events)
	if !reflect.DeepEqual(event.Item, finished) {
		t.Fatalf("event item differs from committed item\nevent: %#v\ncommit: %#v", event.Item, finished)
	}
	if event.ScheduledResult == nil {
		t.Fatal("terminal commit emitted no scheduled result")
	}
	result := event.ScheduledResult
	if result.ID != "calendar_result:"+item.ID+":"+run.ID ||
		result.ThreadID != "thread-frozen" ||
		result.Title != "Frozen title" ||
		result.Status != StatusCompleted {
		t.Fatalf("scheduled result = %#v", result)
	}

	reopened, err := NewStore(filepath.Dir(store.Path()))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, finished) {
		t.Fatalf("event preceded durable commit\npersisted: %#v\nfinished:  %#v", persisted, finished)
	}

	if _, err := store.FinishRun(item.ID, run.ID, "different valid result", ""); err != nil {
		t.Fatal(err)
	}
	assertNoCalendarEvent(t, events)

	reopenSubID, reopenEvents := reopened.Subscribe()
	defer reopened.Unsubscribe(reopenSubID)
	assertNoCalendarEvent(t, reopenEvents)
}

func TestFinishRunFailurePathsEmitNoEventOrMutation(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Store, Item, Run) error
	}{
		{
			name: "invalid payload",
			run: func(store *Store, item Item, run Run) error {
				_, err := store.FinishRun(item.ID, run.ID, "", "")
				return err
			},
		},
		{
			name: "unknown run conflict",
			run: func(store *Store, item Item, _ Run) error {
				_, err := store.FinishRun(item.ID, "unknown-run", "result", "")
				return err
			},
		},
		{
			name: "persistence failure",
			run: func(store *Store, item Item, run Run) error {
				path := store.path
				store.path = filepath.Dir(path)
				_, err := store.FinishRun(item.ID, run.ID, "result", "")
				store.path = path
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, item, run := newTerminalEventFixture(t)
			before, err := store.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			subID, events := store.Subscribe()
			defer store.Unsubscribe(subID)

			if err := test.run(store, item, run); err == nil {
				t.Fatal("expected FinishRun error")
			}
			after, err := store.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("failed finish changed memory\nbefore: %#v\nafter:  %#v", before, after)
			}
			afterRaw, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterRaw, raw) {
				t.Fatal("failed finish changed durable bytes")
			}
			assertNoCalendarEvent(t, events)
		})
	}
}

func TestNonterminalCalendarEventHasNoScheduledResult(t *testing.T) {
	store, item, run := newTerminalEventFixture(t)
	subID, events := store.Subscribe()
	defer store.Unsubscribe(subID)

	if _, err := store.RecordLaunch(item.ID, run.ID, "work-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	event := receiveCalendarEvent(t, events)
	if event.ScheduledResult != nil {
		t.Fatalf("nonterminal event carried result: %#v", event.ScheduledResult)
	}
}

func newTerminalEventFixture(t *testing.T) (*Store, Item, Run) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 17, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := store.Create(Item{
		ID:                "item-1",
		Title:             "Frozen title",
		Kind:              KindScheduledAction,
		DueAt:             &due,
		Timezone:          "UTC",
		Recurrence:        RecurrenceNone,
		ActionInstruction: "Produce the result",
		SourceThreadID:    "thread-frozen",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	return store, item, run
}

func receiveCalendarEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Calendar event")
		return Event{}
	}
}

func assertNoCalendarEvent(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected Calendar event: %#v", event)
	case <-time.After(10 * time.Millisecond):
	}
}
