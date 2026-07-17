package server

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

type recordedPush struct {
	kind      string
	agentID   string
	title     string
	status    string
	threadID  string
	resultID  string
	serverRef string
}

type recordingNotificationPusher struct {
	mu    sync.Mutex
	calls []recordedPush
	err   error
}

func (p *recordingNotificationPusher) SetRegistration(_, serverRef string) {
	p.mu.Lock()
	p.calls = append(p.calls, recordedPush{kind: "registration", serverRef: serverRef})
	p.mu.Unlock()
}

func (p *recordingNotificationPusher) NotifyAgentBlocked(agentID, _, _ string) error {
	return p.record(recordedPush{kind: "blocked", agentID: agentID})
}

func (p *recordingNotificationPusher) NotifyAgentFailed(agentID, _, _ string) error {
	return p.record(recordedPush{kind: "failed", agentID: agentID})
}

func (p *recordingNotificationPusher) NotifyAgentDone(agentID, _, _ string) error {
	return p.record(recordedPush{kind: "done", agentID: agentID})
}

func (p *recordingNotificationPusher) NotifyScheduledResult(title, status, threadID, resultID string) error {
	return p.record(recordedPush{
		kind:     "scheduled_result",
		title:    title,
		status:   status,
		threadID: threadID,
		resultID: resultID,
	})
}

func (p *recordingNotificationPusher) record(call recordedPush) error {
	p.mu.Lock()
	p.calls = append(p.calls, call)
	p.mu.Unlock()
	return p.err
}

func (p *recordingNotificationPusher) snapshot() []recordedPush {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]recordedPush(nil), p.calls...)
}

func TestOrdinaryDelegatedTransitionsEachAttemptOnePush(t *testing.T) {
	for _, state := range []string{"blocked", "failed", "done"} {
		t.Run(state, func(t *testing.T) {
			pusher := &recordingNotificationPusher{}
			srv := &Server{pusher: pusher, active: map[*websocket.Conn]string{}}
			srv.maybeNotifyForSessionEvent(notificationTransition("agent-1", "running", state))
			calls := pusher.snapshot()
			if len(calls) != 1 || calls[0].kind != state || calls[0].agentID != "agent-1" {
				t.Fatalf("calls = %#v", calls)
			}
		})
	}
}

func TestNotificationSuppressionUsesExactAgentViewer(t *testing.T) {
	viewer := &websocket.Conn{}
	pusher := &recordingNotificationPusher{}
	srv := &Server{
		pusher: pusher,
		active: map[*websocket.Conn]string{viewer: "agent-target"},
	}

	srv.maybeNotifyForSessionEvent(notificationTransition("agent-target", "running", "blocked"))
	if calls := pusher.snapshot(); len(calls) != 0 {
		t.Fatalf("exact viewer did not suppress target: %#v", calls)
	}

	srv.active[viewer] = "agent-other"
	srv.maybeNotifyForSessionEvent(notificationTransition("agent-target", "running", "blocked"))
	if calls := pusher.snapshot(); len(calls) != 1 || calls[0].agentID != "agent-target" {
		t.Fatalf("other viewer suppressed target: %#v", calls)
	}
}

func TestRepeatedRealBlockedTransitionsEachAttemptPush(t *testing.T) {
	pusher := &recordingNotificationPusher{}
	srv := &Server{pusher: pusher, active: map[*websocket.Conn]string{}}

	srv.maybeNotifyForSessionEvent(notificationTransition("agent-1", "running", "blocked"))
	srv.maybeNotifyForSessionEvent(notificationTransition("agent-1", "blocked", "running"))
	srv.maybeNotifyForSessionEvent(notificationTransition("agent-1", "running", "blocked"))
	if calls := pusher.snapshot(); len(calls) != 2 || calls[0].kind != "blocked" || calls[1].kind != "blocked" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestNonDelegatedSessionsNeverUseGenericLifecyclePush(t *testing.T) {
	pusher := &recordingNotificationPusher{}
	srv := &Server{pusher: pusher, active: map[*websocket.Conn]string{}}
	for _, state := range []string{"blocked", "failed", "done"} {
		event := notificationTransition("non-delegated", "running", state)
		event.Agent.Delegated = false
		srv.maybeNotifyForSessionEvent(event)
	}
	if calls := pusher.snapshot(); len(calls) != 0 {
		t.Fatalf("non-delegated lifecycle calls = %#v", calls)
	}
}

func TestCalendarTerminalEventAttemptsOneUnsuppressedResultPush(t *testing.T) {
	calendarStore, item, run := newScheduledNotificationFixture(t, "scheduled-agent")
	subID, events := calendarStore.Subscribe()
	defer calendarStore.Unsubscribe(subID)
	if _, err := calendarStore.FinishRun(item.ID, run.ID, "private deliverable", ""); err != nil {
		t.Fatal(err)
	}

	var event calendar.Event
	select {
	case event = <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal Calendar event")
	}
	if event.ScheduledResult == nil {
		t.Fatal("terminal Calendar event has no result")
	}

	pusher := &recordingNotificationPusher{err: errors.New("push unavailable")}
	srv := &Server{
		pusher: pusher,
		active: map[*websocket.Conn]string{&websocket.Conn{}: "scheduled-agent"},
	}
	srv.handleCalendarEvent(event)
	calls := pusher.snapshot()
	if len(calls) != 1 {
		t.Fatalf("result push attempts = %#v", calls)
	}
	call := calls[0]
	result := event.ScheduledResult
	if call.kind != "scheduled_result" || call.title != result.Title ||
		call.status != string(result.Status) || call.threadID != result.ThreadID ||
		call.resultID != result.ID {
		t.Fatalf("result push = %#v, result = %#v", call, result)
	}
}

func TestFailedFinishRunPersistsThenAttemptsOnePushWithoutReplay(t *testing.T) {
	calendarStore, item, run := newScheduledNotificationFixture(t, "scheduled-agent")
	pusher := &recordingNotificationPusher{}
	srv := &Server{pusher: pusher, active: map[*websocket.Conn]string{}}
	srv.SetCalendar(calendarStore, nil)
	defer calendarStore.Unsubscribe(srv.calendarSubID)

	finished, err := calendarStore.FinishRun(item.ID, run.ID, "", "executor failed before publishing a deliverable")
	if err != nil {
		t.Fatal(err)
	}
	event := receiveServerCalendarEvent(t, srv.calendarSub)
	if event.ScheduledResult == nil || event.ScheduledResult.Status != calendar.StatusFailed {
		t.Fatalf("failed terminal event = %#v", event)
	}
	assertNoServerCalendarEvent(t, srv.calendarSub)

	reopened, err := calendar.NewStore(filepath.Dir(calendarStore.Path()))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, finished) || !reflect.DeepEqual(event.Item, finished) {
		t.Fatalf("push event preceded the durable terminal commit\nevent:     %#v\npersisted: %#v\nfinished:  %#v", event.Item, persisted, finished)
	}

	srv.handleCalendarEvent(event)
	calls := pusher.snapshot()
	if len(calls) != 1 || calls[0].kind != "scheduled_result" ||
		calls[0].status != string(calendar.StatusFailed) ||
		calls[0].threadID != run.SourceThreadID ||
		calls[0].resultID != event.ScheduledResult.ID {
		t.Fatalf("failed result push attempts = %#v", calls)
	}

	if _, err := calendarStore.FinishRun(item.ID, run.ID, "", "a later duplicate failure"); err != nil {
		t.Fatal(err)
	}
	assertNoServerCalendarEvent(t, srv.calendarSub)
	if calls := pusher.snapshot(); len(calls) != 1 {
		t.Fatalf("idempotent FinishRun replayed push: %#v", calls)
	}

	reopenedPusher := &recordingNotificationPusher{}
	reopenedServer := &Server{pusher: reopenedPusher, active: map[*websocket.Conn]string{}}
	reopenedServer.SetCalendar(reopened, nil)
	defer reopened.Unsubscribe(reopenedServer.calendarSubID)
	assertNoServerCalendarEvent(t, reopenedServer.calendarSub)
	if calls := reopenedPusher.snapshot(); len(calls) != 0 {
		t.Fatalf("reopen replayed failed result push: %#v", calls)
	}
}

func receiveServerCalendarEvent(t *testing.T, events <-chan calendar.Event) calendar.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Calendar event")
		return calendar.Event{}
	}
}

func assertNoServerCalendarEvent(t *testing.T, events <-chan calendar.Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected Calendar event: %#v", event)
	case <-time.After(10 * time.Millisecond):
	}
}

func notificationTransition(agentID, oldState, newState string) watcher.SessionEvent {
	return watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  agentID,
		OldState: oldState,
		NewState: newState,
		Agent: &classifier.Agent{
			ID:        agentID,
			Name:      "Agent " + agentID,
			State:     classifier.AgentState(newState),
			Summary:   "current summary",
			Delegated: true,
		},
	}
}

func newScheduledNotificationFixture(t *testing.T, agentID string) (*calendar.Store, calendar.Item, calendar.Run) {
	t.Helper()
	store, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(time.Hour)
	item, err := store.Create(calendar.Item{
		ID:                "calendar-item",
		Title:             "Frozen report",
		Kind:              calendar.KindScheduledAction,
		DueAt:             &due,
		Timezone:          "UTC",
		Recurrence:        calendar.RecurrenceNone,
		ActionInstruction: "Produce report",
		SourceThreadID:    "brain-thread-frozen",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordLaunch(item.ID, run.ID, "work-1", agentID); err != nil {
		t.Fatal(err)
	}
	return store, item, run
}
