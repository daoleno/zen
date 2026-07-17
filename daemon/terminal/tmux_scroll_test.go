package terminal

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type recordedTmuxCommand struct {
	kind string
	args []string
}

type tmuxCommandRecorder struct {
	mu        sync.Mutex
	commands  []recordedTmuxCommand
	output    []byte
	runErr    error
	runErrors []error
}

func (r *tmuxCommandRecorder) run(args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, recordedTmuxCommand{kind: "run", args: append([]string(nil), args...)})
	if len(r.runErrors) > 0 {
		err := r.runErrors[0]
		r.runErrors = r.runErrors[1:]
		return err
	}
	return r.runErr
}

func (r *tmuxCommandRecorder) read(args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, recordedTmuxCommand{kind: "query", args: append([]string(nil), args...)})
	return append([]byte(nil), r.output...), nil
}

type manualTmuxTimer struct {
	stopped bool
	fn      func()
}

func (t *manualTmuxTimer) Stop() bool {
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

type manualTmuxScheduler struct {
	delays []time.Duration
	timers []*manualTmuxTimer
}

func (s *manualTmuxScheduler) after(delay time.Duration, fn func()) tmuxScrollTimer {
	timer := &manualTmuxTimer{fn: fn}
	s.delays = append(s.delays, delay)
	s.timers = append(s.timers, timer)
	return timer
}

func (s *manualTmuxScheduler) fireLatest() {
	for index := len(s.timers) - 1; index >= 0; index-- {
		timer := s.timers[index]
		if timer.stopped {
			continue
		}
		timer.stopped = true
		timer.fn()
		return
	}
}

func newRecordedScrollSession(recorder *tmuxCommandRecorder, scheduler *manualTmuxScheduler) *tmuxSession {
	return &tmuxSession{
		linkedSession:         "zen-view",
		events:                make(chan Event, 8),
		runTmuxCommand:        recorder.run,
		readTmuxCommand:       recorder.read,
		scheduleScrollCommand: scheduler.after,
	}
}

func TestTmuxScrollBatchesCopyModeEntryAndFirstIncrement(t *testing.T) {
	recorder := &tmuxCommandRecorder{}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)

	if err := session.Scroll(-3); err != nil {
		t.Fatalf("Scroll(-3): %v", err)
	}

	want := []recordedTmuxCommand{{
		kind: "run",
		args: []string{
			"copy-mode", "-e", "-t", "zen-view", ";",
			"send-keys", "-t", "zen-view", "-X", "-N", "3", "scroll-up",
		},
	}}
	if !reflect.DeepEqual(recorder.commands, want) {
		t.Fatalf("hot-path commands = %#v, want one batched invocation %#v", recorder.commands, want)
	}
	if len(scheduler.delays) != 1 || scheduler.delays[0] != tmuxScrollReconcileDelay {
		t.Fatalf("reconcile schedule = %v, want [%v]", scheduler.delays, tmuxScrollReconcileDelay)
	}
	if tmuxScrollReconcileDelay > 32*time.Millisecond {
		t.Fatalf("reconcile delay = %v, want <= 32ms and never the old 80-120ms hot-path delay", tmuxScrollReconcileDelay)
	}
}

func TestTmuxScrollHotPathHasNoStateQueryAndCoalescesOneDeferredReconcile(t *testing.T) {
	recorder := &tmuxCommandRecorder{output: []byte("1:7")}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)

	for _, lines := range []int{-2, -4, 3, -1} {
		if err := session.Scroll(lines); err != nil {
			t.Fatalf("Scroll(%d): %v", lines, err)
		}
	}
	for _, command := range recorder.commands {
		if command.kind == "query" {
			t.Fatalf("scroll hot path queried state: %#v", recorder.commands)
		}
	}
	if len(recorder.commands) != 4 {
		t.Fatalf("four incremental batches ran %d commands, want exactly four", len(recorder.commands))
	}

	scheduler.fireLatest()
	queryCount := 0
	for _, command := range recorder.commands {
		if command.kind == "query" {
			queryCount++
		}
	}
	if queryCount != 1 {
		t.Fatalf("deferred state query count = %d, want exactly one", queryCount)
	}

	select {
	case event := <-session.events:
		if event.Type != EventScroll || event.ScrollState.Position != 7 || !event.ScrollState.InCopyMode {
			t.Fatalf("deferred scroll state = %+v", event)
		}
	default:
		t.Fatal("deferred reconciliation emitted no scroll state")
	}
}

func TestTmuxScrollDownUsesExitOnBottomCommand(t *testing.T) {
	recorder := &tmuxCommandRecorder{}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)
	session.inCopyMode = true

	if err := session.Scroll(5); err != nil {
		t.Fatalf("Scroll(5): %v", err)
	}
	want := []string{"send-keys", "-t", "zen-view", "-X", "-N", "5", "scroll-down-and-cancel"}
	if len(recorder.commands) != 1 || !reflect.DeepEqual(recorder.commands[0].args, want) {
		t.Fatalf("down command = %#v, want %#v", recorder.commands, want)
	}
}

func TestTmuxInertialDownBatchAfterBottomIsABenignDeferredReconcile(t *testing.T) {
	recorder := &tmuxCommandRecorder{runErr: testingError("not in a mode")}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)
	session.inCopyMode = true

	if err := session.Scroll(2); err != nil {
		t.Fatalf("post-bottom inertial Scroll(2) = %v, want benign no-op", err)
	}
	if len(recorder.commands) != 1 {
		t.Fatalf("post-bottom inertial command count = %d, want 1", len(recorder.commands))
	}
	if len(scheduler.delays) != 1 {
		t.Fatalf("post-bottom reconcile schedules = %d, want 1", len(scheduler.delays))
	}
}

func TestTmuxFastDirectionReversalReentersAfterNativeBottomExit(t *testing.T) {
	recorder := &tmuxCommandRecorder{
		runErrors: []error{testingError("not in a mode"), nil},
	}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)
	session.inCopyMode = true

	if err := session.Scroll(-2); err != nil {
		t.Fatalf("fast reversal Scroll(-2): %v", err)
	}
	if len(recorder.commands) != 2 {
		t.Fatalf("fast reversal command count = %d, want failed stale step plus one re-entry", len(recorder.commands))
	}
	wantRetry := []string{
		"copy-mode", "-e", "-t", "zen-view", ";",
		"send-keys", "-t", "zen-view", "-X", "-N", "2", "scroll-up",
	}
	if !reflect.DeepEqual(recorder.commands[1].args, wantRetry) {
		t.Fatalf("fast reversal retry = %v, want %v", recorder.commands[1].args, wantRetry)
	}
	if len(scheduler.delays) != 1 {
		t.Fatalf("fast reversal reconcile schedules = %d, want 1", len(scheduler.delays))
	}
}

type testingError string

func (e testingError) Error() string { return string(e) }

func TestTmuxCancelStopsDeferredQueryAndExitsCopyModeOnce(t *testing.T) {
	recorder := &tmuxCommandRecorder{}
	scheduler := &manualTmuxScheduler{}
	session := newRecordedScrollSession(recorder, scheduler)

	if err := session.Scroll(-1); err != nil {
		t.Fatal(err)
	}
	if err := session.CancelScroll(); err != nil {
		t.Fatal(err)
	}
	if err := session.CancelScroll(); err != nil {
		t.Fatal(err)
	}
	scheduler.fireLatest()

	cancelCount := 0
	queryCount := 0
	for _, command := range recorder.commands {
		if reflect.DeepEqual(command.args, []string{
			"if-shell", "-F", "-t", "zen-view", "#{pane_in_mode}",
			"send-keys -t zen-view -X cancel", "",
		}) {
			cancelCount++
		}
		if command.kind == "query" {
			queryCount++
		}
	}
	if cancelCount != 1 || queryCount != 0 {
		t.Fatalf("cancel/query counts = %d/%d, want 1/0; commands=%#v", cancelCount, queryCount, recorder.commands)
	}
}
