package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestSessionActivityAdvancedDecision(t *testing.T) {
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	runningTurn := TurnSnapshot{
		TurnID:     "turn-1",
		Status:     TurnRunning,
		AcceptedAt: base,
	}
	doneTurn := runningTurn
	doneTurn.Status = TurnDone
	doneTurn.SettledAt = &base

	cases := []struct {
		name           string
		contentChanged bool
		oldState       classifier.AgentState
		newState       classifier.AgentState
		previousTurn   TurnSnapshot
		hadPrevious    bool
		turn           TurnSnapshot
		hasTurn        bool
		want           bool
	}{
		{
			name:           "content change advances",
			contentChanged: true,
			want:           true,
		},
		{
			name:     "state transition advances",
			oldState: classifier.StateUnknown,
			newState: classifier.StateDone,
			want:     true,
		},
		{
			name:     "no-op observation preserves",
			oldState: classifier.StateDone,
			newState: classifier.StateDone,
			want:     false,
		},
		{
			name:     "new delegated turn advances",
			oldState: classifier.StateRunning,
			newState: classifier.StateRunning,
			turn:     runningTurn,
			hasTurn:  true,
			want:     true,
		},
		{
			name:         "same turn same status preserves",
			oldState:     classifier.StateRunning,
			newState:     classifier.StateRunning,
			previousTurn: runningTurn,
			hadPrevious:  true,
			turn:         runningTurn,
			hasTurn:      true,
			want:         false,
		},
		{
			name:         "turn id change advances",
			oldState:     classifier.StateRunning,
			newState:     classifier.StateRunning,
			previousTurn: runningTurn,
			hadPrevious:  true,
			turn:         func() TurnSnapshot { t := runningTurn; t.TurnID = "turn-2"; return t }(),
			hasTurn:      true,
			want:         true,
		},
		{
			name:         "turn status change advances",
			oldState:     classifier.StateRunning,
			newState:     classifier.StateRunning,
			previousTurn: runningTurn,
			hadPrevious:  true,
			turn:         doneTurn,
			hasTurn:      true,
			want:         true,
		},
		{
			name:         "turn removed preserves",
			oldState:     classifier.StateRunning,
			newState:     classifier.StateRunning,
			previousTurn: runningTurn,
			hadPrevious:  true,
			hasTurn:      false,
			want:         false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionActivityAdvanced(
				tc.contentChanged,
				tc.oldState,
				tc.newState,
				tc.previousTurn,
				tc.hadPrevious,
				tc.turn,
				tc.hasTurn,
			)
			if got != tc.want {
				t.Fatalf("sessionActivityAdvanced() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionDiscoveryActivityTimePriority(t *testing.T) {
	started := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	accepted := time.Date(2026, 8, 7, 9, 30, 0, 0, time.UTC)
	progress := time.Date(2026, 8, 7, 9, 45, 0, 0, time.UTC)
	provider := time.Date(2026, 8, 7, 9, 50, 0, 0, time.UTC)
	settled := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)

	baseAgent := &classifier.Agent{StartedAt: started}
	baseTurn := TurnSnapshot{
		TurnID:     "turn-1",
		Status:     TurnRunning,
		AcceptedAt: accepted,
	}

	cases := []struct {
		name     string
		agent    *classifier.Agent
		turn     TurnSnapshot
		hasTurn  bool
		provider ProviderActivityObservation
		want     time.Time
	}{
		{
			name:  "falls back to process start time",
			agent: baseAgent,
			want:  started,
		},
		{
			name:  "no provable source stays unavailable",
			agent: &classifier.Agent{},
			want:  time.Time{},
		},
		{
			name:    "turn acceptance beats process start",
			agent:   baseAgent,
			turn:    baseTurn,
			hasTurn: true,
			want:    accepted,
		},
		{
			name:    "last progress beats turn acceptance",
			agent:   &classifier.Agent{StartedAt: started, LastProgressAt: &progress},
			turn:    baseTurn,
			hasTurn: true,
			want:    progress,
		},
		{
			name:     "authoritative provider activity beats progress",
			agent:    &classifier.Agent{StartedAt: started, LastProgressAt: &progress},
			turn:     baseTurn,
			hasTurn:  true,
			provider: ProviderActivityObservation{StartedAt: provider},
			want:     provider,
		},
		{
			name:  "turn settlement is the latest provable activity",
			agent: baseAgent,
			turn: func() TurnSnapshot {
				turn := baseTurn
				turn.Status = TurnDone
				turn.SettledAt = &settled
				return turn
			}(),
			hasTurn: true,
			want:    settled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionDiscoveryActivityTime(tc.agent, tc.turn, tc.hasTurn, tc.provider)
			if !got.Equal(tc.want) {
				t.Fatalf("sessionDiscoveryActivityTime() = %v, want %v", got, tc.want)
			}
		})
	}
}

// installFakePollSeams points poll at deterministic tmux/process sources.
// contentByTarget maps window target to captured pane text; all panes are
// reported alive. processes is the fake process table used to derive each
// Session's StartedAt. It returns a restore function.
func installFakePollSeams(
	windows []tmuxWindow,
	contentByTarget map[string]string,
	processes map[int]processInfo,
) func() {
	previousList := listTmuxWindowsFunc
	previousCapture := capturePaneContentFunc
	previousSnapshot := snapshotProcessesFunc
	listTmuxWindowsFunc = func() ([]tmuxWindow, error) { return windows, nil }
	capturePaneContentFunc = func(target string) (string, bool, int) {
		return contentByTarget[target], true, -1
	}
	snapshotProcessesFunc = func() map[int]processInfo { return processes }
	return func() {
		listTmuxWindowsFunc = previousList
		capturePaneContentFunc = previousCapture
		snapshotProcessesFunc = previousSnapshot
	}
}

// fakeProcess is a single claude process-table entry with a fixed start time.
func fakeProcess(panePID int, startedAt time.Time) processInfo {
	return processInfo{
		pid:       panePID,
		ppid:      1,
		pgid:      panePID,
		tpgid:     panePID,
		startedAt: startedAt,
		comm:      "claude",
		args:      "claude",
	}
}

// fakePollClock returns a poll clock that hands out times in ascending order.
func fakePollClock(steps []time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		value := steps[index%len(steps)]
		index++
		return value
	}
}

func drainWatcherEvents(w *Watcher) {
	for {
		select {
		case <-w.Events():
		default:
			return
		}
	}
}

func testWindows() []tmuxWindow {
	return []tmuxWindow{
		{target: "sess-a:@1", name: "alpha", cwd: "/repo/a", command: "claude", panePID: 111},
		{target: "sess-b:@2", name: "beta", cwd: "/repo/b", command: "claude", panePID: 222},
	}
}

const contentA = "Claude Code\nworking on task alpha\n❯ \n"
const contentB = "Claude Code\nworking on task beta\n❯ \n"

var sessionAStarted = time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
var sessionBStarted = time.Date(2026, 8, 7, 7, 30, 0, 0, time.UTC)

func liveSessionProcesses() map[int]processInfo {
	return map[int]processInfo{
		111: fakeProcess(111, sessionAStarted),
		222: fakeProcess(222, sessionBStarted),
	}
}

func TestPollRediscoveredSessionsKeepRealActivityTimes(t *testing.T) {
	// Fresh watcher (daemon restart / rebuild): two already-alive Sessions with
	// different process start times are discovered by the same first poll.
	// Their activity time must come from their own provable process start, not
	// the shared discovery poll instant.
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	})
	restore := installFakePollSeams(testWindows(), map[string]string{
		"sess-a:@1": contentA,
		"sess-b:@2": contentB,
	}, liveSessionProcesses())
	defer restore()

	w.poll()

	agents := w.Agents()
	agentA := agentByID(agents, "sess-a:@1")
	agentB := agentByID(agents, "sess-b:@2")
	if agentA == nil || agentB == nil {
		t.Fatalf("agents = %#v", agents)
	}
	if !agentA.UpdatedAt.Equal(sessionAStarted) {
		t.Fatalf("sess-a activity time = %v, want provable process start %v", agentA.UpdatedAt, sessionAStarted)
	}
	if !agentB.UpdatedAt.Equal(sessionBStarted) {
		t.Fatalf("sess-b activity time = %v, want provable process start %v", agentB.UpdatedAt, sessionBStarted)
	}
	if agentA.UpdatedAt.Equal(agentB.UpdatedAt) {
		t.Fatalf("rediscovered sessions share one activity time: %v", agentA.UpdatedAt)
	}
	// The observation clock must never leak into the activity time.
	if agentA.UpdatedAt.Equal(agentA.LastSeenAt) || agentB.UpdatedAt.Equal(agentB.LastSeenAt) {
		t.Fatalf("observation time leaked into activity time: a %v/%v b %v/%v",
			agentA.UpdatedAt, agentA.LastSeenAt, agentB.UpdatedAt, agentB.LastSeenAt)
	}
	if !agentA.LastSeenAt.Equal(time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC)) ||
		!agentB.LastSeenAt.Equal(time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC)) {
		t.Fatalf("observation times = a:%v b:%v", agentA.LastSeenAt, agentB.LastSeenAt)
	}
}

func TestPollNoopPreservesUpdatedAtAndOrder(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 4, 0, time.UTC),
	})
	restore := installFakePollSeams(testWindows(), map[string]string{
		"sess-a:@1": contentA,
		"sess-b:@2": contentB,
	}, liveSessionProcesses())
	defer restore()

	w.poll()
	drainWatcherEvents(w)

	first := w.Agents()
	if len(first) != 2 {
		t.Fatalf("agents after first poll = %d, want 2", len(first))
	}
	firstA := agentByID(first, "sess-a:@1")
	firstB := agentByID(first, "sess-b:@2")
	if firstA == nil || firstB == nil {
		t.Fatalf("agents = %#v", first)
	}
	if !firstA.UpdatedAt.Equal(sessionAStarted) || !firstB.UpdatedAt.Equal(sessionBStarted) {
		t.Fatalf("discovery seeded from process start = a:%v b:%v, want a:%v b:%v",
			firstA.UpdatedAt, firstB.UpdatedAt, sessionAStarted, sessionBStarted)
	}
	if firstA.UpdatedAt.Equal(firstB.UpdatedAt) {
		t.Fatalf("sessions observed in the same poll must keep distinct activity times: %v == %v", firstA.UpdatedAt, firstB.UpdatedAt)
	}

	w.poll()

	second := w.Agents()
	if len(second) != 2 {
		t.Fatalf("agents after no-op poll = %d, want 2", len(second))
	}
	if second[0].ID != first[0].ID || second[1].ID != first[1].ID {
		t.Fatalf("no-op poll reordered sessions: before %#v after %#v", agentIDs(first), agentIDs(second))
	}
	secondA := agentByID(second, "sess-a:@1")
	secondB := agentByID(second, "sess-b:@2")
	if !secondA.UpdatedAt.Equal(firstA.UpdatedAt) || !secondB.UpdatedAt.Equal(firstB.UpdatedAt) {
		t.Fatalf("no-op poll mutated activity times: a %v -> %v, b %v -> %v",
			firstA.UpdatedAt, secondA.UpdatedAt, firstB.UpdatedAt, secondB.UpdatedAt)
	}
	if !secondA.LastSeenAt.After(firstA.UpdatedAt) || !secondB.LastSeenAt.After(firstB.UpdatedAt) {
		t.Fatalf("no-op poll must still advance the observation time: %#v %#v", secondA, secondB)
	}

	select {
	case event := <-w.Events():
		t.Fatalf("no-op poll emitted event: %#v", event)
	default:
	}
}

func TestHiddenHostProviderTerminalEmitsActivityChangeWithoutPaneOutput(t *testing.T) {
	w := New(time.Second)
	started := time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC)
	settled := started.Add(time.Second)
	w.providerActivityProbe = &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		{ID: "host-activity", Status: "running", StartedAt: started, Structured: true},
		{ID: "host-activity", Status: "completed", StartedAt: started, SettledAt: settled, Structured: true},
	}}
	windows := []tmuxWindow{{
		target: "brain-agent-brain-provider-boundary:@1", name: "Brain", cwd: "/brain",
		command: "codex", panePID: 111, hidden: true,
	}}
	restore := installFakePollSeams(windows, map[string]string{
		windows[0].target: contentA,
	}, map[int]processInfo{111: fakeProcess(111, sessionAStarted)})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	w.poll()

	events := collectWatcherEvents(w)
	if len(events) != 1 || events[0].Type != "provider_activity_change" || events[0].AgentID != windows[0].target {
		t.Fatalf("terminal provider boundary events = %#v", events)
	}
}

func TestPollContentChangeAdvancesOnlyAffectedSession(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 4, 0, time.UTC),
	})
	restore := installFakePollSeams(testWindows(), map[string]string{
		"sess-a:@1": contentA,
		"sess-b:@2": contentB,
	}, liveSessionProcesses())
	defer restore()

	w.poll()
	drainWatcherEvents(w)

	before := w.Agents()
	beforeB := agentByID(before, "sess-b:@2")

	restore()
	restore = installFakePollSeams(testWindows(), map[string]string{
		"sess-a:@1": contentA + "new provider output\n",
		"sess-b:@2": contentB,
	}, liveSessionProcesses())
	defer restore()

	w.poll()

	after := w.Agents()
	afterA := agentByID(after, "sess-a:@1")
	afterB := agentByID(after, "sess-b:@2")
	if !afterA.UpdatedAt.Equal(time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC)) {
		t.Fatalf("content change advanced %s to the poll activity instant %v, want 10:00:03", "sess-a:@1", afterA.UpdatedAt)
	}
	if !afterB.UpdatedAt.Equal(beforeB.UpdatedAt) {
		t.Fatalf("unaffected session activity time advanced: %v -> %v", beforeB.UpdatedAt, afterB.UpdatedAt)
	}

	events := collectWatcherEvents(w)
	outputEvents := 0
	stateEvents := 0
	for _, event := range events {
		switch event.Type {
		case "agent_output":
			if event.AgentID != "sess-a:@1" {
				t.Fatalf("agent_output for wrong session: %#v", event)
			}
			outputEvents++
		case "agent_state_change", "agent_metadata_change":
			stateEvents++
		}
	}
	if outputEvents != 1 {
		t.Fatalf("agent_output events = %d, want 1: %#v", outputEvents, events)
	}
	if stateEvents != 0 {
		t.Fatalf("unexpected state/metadata events: %#v", events)
	}
}

func TestPollStateTransitionAdvancesUpdatedAtWithoutContentChange(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 4, 0, time.UTC),
	})
	windows := testWindows()
	restore := installFakePollSeams(windows, map[string]string{
		"sess-a:@1": "Claude Code\nidle prompt\n❯ \n",
		"sess-b:@2": contentB,
	}, liveSessionProcesses())
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	before := w.Agents()
	beforeB := agentByID(before, "sess-b:@2")

	// Pane dies: same captured lines, but liveness flips. Classification moves
	// sess-a to done with no content change.
	previousCapture := capturePaneContentFunc
	capturePaneContentFunc = func(target string) (string, bool, int) {
		content, alive, deadStatus := previousCapture(target)
		if target == "sess-a:@1" {
			return content, false, 1
		}
		return content, alive, deadStatus
	}

	w.poll()

	after := w.Agents()
	afterA := agentByID(after, "sess-a:@1")
	afterB := agentByID(after, "sess-b:@2")
	if afterA == nil || afterA.State != classifier.StateDone {
		t.Fatalf("sess-a state = %v, want done", stateOf(afterA))
	}
	if !afterA.UpdatedAt.Equal(time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC)) {
		t.Fatalf("state transition advanced %s to %v, want 10:00:03", "sess-a:@1", afterA.UpdatedAt)
	}
	if !afterB.UpdatedAt.Equal(beforeB.UpdatedAt) {
		t.Fatalf("unaffected session activity time advanced: %v -> %v", beforeB.UpdatedAt, afterB.UpdatedAt)
	}

	events := collectWatcherEvents(w)
	stateEvents := 0
	for _, event := range events {
		if event.Type == "agent_state_change" && event.AgentID == "sess-a:@1" {
			stateEvents++
		}
	}
	if stateEvents != 1 {
		t.Fatalf("state change events = %d, want 1: %#v", stateEvents, events)
	}
}

func TestPollTurnSettlementSeedsActivityAndRepeatsPreserveIt(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 4, 0, time.UTC),
	})
	settledAt := time.Date(2026, 8, 7, 9, 59, 0, 0, time.UTC)
	ledger := newFakeTurnLedger()
	ledger.seed("brain-agent-worker:@1", TurnSnapshot{
		SessionID:  "brain-agent-worker:@1",
		TurnID:     "turn-1",
		Status:     TurnDone,
		AcceptedAt: time.Date(2026, 8, 7, 9, 58, 0, 0, time.UTC),
		SettledAt:  &settledAt,
		Summary:    "Finished verification",
	})
	w.turnLedger = ledger
	windows := []tmuxWindow{
		{target: "brain-agent-worker:@1", name: "worker", cwd: "/repo/zen", command: "claude", panePID: 333},
	}
	restore := installFakePollSeams(windows, map[string]string{
		"brain-agent-worker:@1": "Claude Code\nFinished verification\n",
	}, map[int]processInfo{
		333: fakeProcess(333, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)),
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)

	first := agentByID(w.Agents(), "brain-agent-worker:@1")
	if first == nil {
		t.Fatalf("delegated session missing after first poll")
	}
	if first.State != classifier.StateDone {
		t.Fatalf("delegated session state = %v, want done", first.State)
	}
	if !first.UpdatedAt.Equal(settledAt) {
		t.Fatalf("discovery seeded activity time = %v, want turn settlement %v (not the poll instant)", first.UpdatedAt, settledAt)
	}
	firstActivity := first.UpdatedAt

	// A repeated poll with the identical settled turn is a no-op.
	w.poll()

	second := agentByID(w.Agents(), "brain-agent-worker:@1")
	if !second.UpdatedAt.Equal(firstActivity) {
		t.Fatalf("no-op poll with settled turn mutated activity time: %v -> %v", firstActivity, second.UpdatedAt)
	}
	if !second.LastSeenAt.After(firstActivity) {
		t.Fatalf("no-op poll must still advance observation time: %#v", second)
	}
	if second.State != classifier.StateDone {
		t.Fatalf("delegated session state after no-op = %v, want done", second.State)
	}

	select {
	case event := <-w.Events():
		t.Fatalf("no-op poll emitted event: %#v", event)
	default:
	}
}

func agentByID(agents []*classifier.Agent, id string) *classifier.Agent {
	for _, agent := range agents {
		if agent.ID == id {
			return agent
		}
	}
	return nil
}

func agentIDs(agents []*classifier.Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return ids
}

func stateOf(agent *classifier.Agent) classifier.AgentState {
	if agent == nil {
		return ""
	}
	return agent.State
}

func collectWatcherEvents(w *Watcher) []SessionEvent {
	var events []SessionEvent
	for {
		select {
		case event := <-w.Events():
			events = append(events, event)
		default:
			return events
		}
	}
}
