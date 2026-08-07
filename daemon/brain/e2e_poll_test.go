package brain

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// e2ePollProbe is a scripted provider probe consumed by the real watcher
// poll: the first observation proves the admitted turn's activity, later
// observations carry the bound terminal. With terminal=false the probe only
// ever reports running activity.
type e2ePollProbe struct {
	mu         sync.Mutex
	calls      int
	acceptedAt time.Time
	terminal   bool
}

func (p *e2ePollProbe) ObserveProviderActivity(classifier.Agent, time.Time) watcher.ProviderActivityObservation {
	p.mu.Lock()
	p.calls++
	call := p.calls
	terminal := p.terminal
	p.mu.Unlock()
	observation := watcher.ProviderActivityObservation{
		ID:              "activity-1",
		Structured:      true,
		FallbackAllowed: true,
		// The provider reads the same durable session the admission tuple
		// came from, so the stream identity matches the recorded tuple.
		AdmissionStream: "stream",
		AdmissionID:     "msg-1",
		AdmissionCursor: 1,
		AdmissionAt:     p.acceptedAt.Add(time.Second),
		InputSHA256:     "payload",
		StartedAt:       p.acceptedAt.Add(time.Second),
	}
	if !terminal || call == 1 {
		observation.Status = "running"
	} else {
		observation.Status = "completed"
		observation.SettledAt = p.acceptedAt.Add(30 * time.Second)
	}
	return observation
}

func (p *e2ePollProbe) ForgetProviderActivity(string) {}

// waitForTurnStatus polls the real store until the canonical turn reaches the
// expected status or the deadline passes.
func waitForTurnStatus(t *testing.T, store *Store, sessionID string, want watcher.TurnStatus) watcher.TurnSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, hasTurn, err := store.Turn(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if hasTurn && snapshot.Status == want {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn did not reach %s; last = %+v hasTurn=%v", want, snapshot, hasTurn)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// runRealWatcher starts the real poll loop with the injected sources and
// returns the cancel function. Events are drained so the poll never blocks.
func runRealWatcher(
	t *testing.T,
	sources watcher.PollSources,
	probe watcher.ProviderActivityProbe,
	ledger watcher.TurnLedger,
) (stop func()) {
	t.Helper()
	w := watcher.New(20 * time.Millisecond)
	w.SetTurnLedger(ledger)
	w.SetProviderActivityProbe(probe)
	restore := w.SetPollSources(sources)
	ctx, cancelCtx := context.WithCancel(context.Background())
	go func() {
		for range w.Events() {
		}
	}()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = w.Run(ctx)
	}()
	return func() {
		cancelCtx()
		// The poll must fully stop before the seam sources are restored,
		// otherwise the poll and the restore race on the package-level
		// inventory functions.
		<-runDone
		restore()
	}
}

// TestE2ERealPollDeadPaneUnknownThenBoundTerminal drives the REAL watcher
// poll against the REAL reducer: a dead pane with an unreadable pane
// identity resolves Unknown (never Failed), and the later bound Provider
// terminal upgrades the turn to Done with exactly one wake of each kind.
func TestE2ERealPollDeadPaneUnknownThenBoundTerminal(t *testing.T) {
	store, service, hostWatcher, sessionID, turnID := e2eStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	e2eAdmission(t, store, sessionID, turnID, at)

	probe := &e2ePollProbe{acceptedAt: at, terminal: true}
	cancel := runRealWatcher(t, watcher.PollSources{
		ListWindows: func() ([]watcher.PollWindow, error) {
			return []watcher.PollWindow{{
				Target: sessionID, Name: "opencode", Cwd: "/repo",
				Command: "opencode", PanePID: 1, Delegated: true,
			}}, nil
		},
		CapturePane: func(string) (string, bool, int) {
			// Dead pane with a non-zero exit and no readable pane identity:
			// the recorded identity cannot be proven, so it resolves
			// Unknown, never Failed.
			return "OpenCode\n", false, 1
		},
		SnapshotProcesses: func() map[int]watcher.PollProcess { return nil },
	}, probe, service)
	defer cancel()

	// Dead pane -> Unknown (one actionable session.uncertain).
	waitForTurnStatus(t, store, sessionID, watcher.TurnUnknown)
	// Late bound Provider terminal -> Done.
	waitForTurnStatus(t, store, sessionID, watcher.TurnDone)

	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	events, _ := store.ListWorkEvents(workItem.ID)
	uncertain := 0
	doneActionable := 0
	for _, event := range events {
		switch {
		case strings.HasSuffix(event.DedupeKey, ":session.uncertain"):
			uncertain++
		case strings.HasSuffix(event.DedupeKey, ":session.done") && event.Actionable:
			doneActionable++
		}
	}
	if uncertain != 1 || doneActionable != 1 {
		t.Fatalf("wake counts: uncertain=%d done=%d events=%#v", uncertain, doneActionable, events)
	}
	// Exactly one wake per kind: the uncertain audit wake and the truthful
	// done wake are each delivered once, and no further dispatch adds more.
	deliveredKinds := map[string]int{}
	for range 4 {
		woke, err := service.DispatchPendingEvent()
		if err != nil {
			t.Fatal(err)
		}
		if !woke {
			break
		}
	}
	for _, call := range hostWatcher.sentCalls {
		for _, kind := range []string{"session.uncertain", "session.done"} {
			if strings.Contains(call.text, `"kind":"`+kind+`"`) {
				deliveredKinds[kind]++
			}
		}
	}
	if deliveredKinds["session.uncertain"] != 1 || deliveredKinds["session.done"] != 1 {
		t.Fatalf("delivered wakes per kind = %#v sends=%#v", deliveredKinds, hostWatcher.sentCalls)
	}
	if len(hostWatcher.sentCalls) != 2 {
		t.Fatalf("host deliveries = %d, want exactly two (one per kind)", len(hostWatcher.sentCalls))
	}
	time.Sleep(100 * time.Millisecond)
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("duplicate dispatch woke=%v err=%v", woke, err)
	}
	if len(hostWatcher.sentCalls) != 2 {
		t.Fatalf("duplicate host deliveries = %d, want two", len(hostWatcher.sentCalls))
	}
}

// TestE2ERealPollPiBlockedFrameNeverWakes drives the Pi pane-Blocked incident
// (A.4.4) through the real watcher poll and the real reducer: the blocked
// frame never produces a needs_input wake for the canonical turn.
func TestE2ERealPollPiBlockedFrameNeverWakes(t *testing.T) {
	store, service, hostWatcher, sessionID, turnID := e2eStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	e2eAdmission(t, store, sessionID, turnID, at)
	// Bound running: the canonical turn is live.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  providerAdmission("stream", "msg-1", 1, "payload", at.Add(2*time.Second)),
		ActivityID: "activity-1",
		StartedAt:  at.Add(2 * time.Second),
		At:         at.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// The exact Pi frame truncated at pane width: the last line ends in `?`.
	content := "┃  ⠼ bun test 2>&1 | grep -E \"(fail)|✗|FAIL\" | head; echo \"exit: $?"
	classified, _ := classifier.Classify(true, strings.Split(content, "\n"), "pi")
	if classified != classifier.StateBlocked {
		t.Fatalf("fixture classification = %s, want blocked (the Pi frame)", classified)
	}

	// Running-only probe: the Pi turn must stay live under the blocked frame.
	probe := &e2ePollProbe{acceptedAt: at, terminal: false}
	cancel := runRealWatcher(t, watcher.PollSources{
		ListWindows: func() ([]watcher.PollWindow, error) {
			return []watcher.PollWindow{{
				Target: sessionID, Name: "pi", Cwd: "/repo",
				Command: "pi", PanePID: 1, Delegated: true,
			}}, nil
		},
		CapturePane: func(string) (string, bool, int) {
			return content, true, -1
		},
		SnapshotProcesses: func() map[int]watcher.PollProcess { return nil },
	}, probe, service)
	defer cancel()
	// Several polls while the blocked-looking frame stays live.
	time.Sleep(200 * time.Millisecond)
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	events, _ := store.ListWorkEvents(workItem.ID)
	for _, event := range events {
		if event.Kind == "session.needs_input" && event.Actionable {
			t.Fatalf("Pi blocked frame produced an actionable needs_input wake: %#v", events)
		}
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("Pi blocked frame woke Brain: woke=%v err=%v", woke, err)
	}
	if len(hostWatcher.sentCalls) != 0 {
		t.Fatalf("Pi blocked frame delivered a wake: %#v", hostWatcher.sentCalls)
	}
	snapshot, _, _ := store.Turn(sessionID)
	if snapshot.Status != watcher.TurnRunning {
		t.Fatalf("Pi turn status = %s, want running", snapshot.Status)
	}
}
