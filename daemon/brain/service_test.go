package brain

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type fakeWatcher struct {
	agents    []*classifier.Agent
	sessions  map[string]*classifier.Agent
	created   []createdCall
	sentCalls []sentCall
	killed    []string
	sendErr   error
	captures  map[string]string
	receipts  map[string]string
}

type createdCall struct {
	id   string
	opts watcher.CreateSessionOptions
}

type sentCall struct {
	sessionID string
	text      string
}

func TestDelegatedTurnEventIdentitySettlesExactlyOnceAcrossDuplicateAndRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-unknown:@1"
	item, err := store.CreateWork(Work{
		Title:            "Unknown provider lifecycle",
		Objective:        "Settle through the provider-neutral Session boundary.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &classifier.Agent{
		ID:        sessionID,
		Name:      "Unknown provider",
		State:     classifier.StateDone,
		Summary:   "Fallback result",
		Delegated: true,
		PaneAlive: true,
	}
	event := watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    agent,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		TurnID:   "unknown-turn-1",
	}
	service := NewService(store, &fakeWatcher{}, nil)
	if _, err := service.RouteSessionEvent(event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RouteSessionEvent(event); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, recorded := range events {
		if recorded.Kind == "session.done" {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("duplicate fallback completion Events = %d, events=%#v", done, events)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reopened, &fakeWatcher{}, nil)
	if _, err := restarted.RouteSessionEvent(event); err != nil {
		t.Fatal(err)
	}
	events, err = reopened.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	done = 0
	for _, recorded := range events {
		if recorded.Kind == "session.done" {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("restart replayed fallback completion: done=%d events=%#v", done, events)
	}
}

func TestDelegatedTurnDeadProcessFailureEventIsExactlyOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-unknown-dead:@1"
	item, err := store.CreateWork(Work{
		Title:            "Unknown dead provider",
		Objective:        "Report provider loss once.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &classifier.Agent{
		ID: sessionID, Name: "Unknown dead provider",
		State: classifier.StateFailed, Summary: "Provider process is no longer live",
		Delegated: true,
	}
	event := watcher.SessionEvent{
		Type: "agent_state_change", AgentID: sessionID, Agent: agent,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateFailed),
		TurnID: "unknown-dead-turn",
	}
	service := NewService(store, &fakeWatcher{}, nil)
	if _, err := service.RouteSessionEvent(event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RouteSessionEvent(event); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, recorded := range events {
		if recorded.Kind == "session.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("dead provider failure Events = %d, events=%#v", failed, events)
	}
}

func TestDelegatedTerminalFactSurvivesRestartAndDeadPaneProjection(t *testing.T) {
	for _, test := range []struct {
		name          string
		doneTurnID    string
		failureTurnID string
	}{
		{name: "turn keyed", doneTurnID: "turn-one", failureTurnID: "turn-one"},
		{name: "restart projection loses turn ID", doneTurnID: "turn-one"},
		{name: "legacy without turn ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			sessionID := "brain-agent-terminal:@1"
			item, err := store.CreateWork(Work{
				Title:            "Terminal monotonicity",
				Objective:        "Keep the accepted terminal fact immutable.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
				NextAction:       "Wait for the delegated Session.",
				WaitFor:          "Session " + sessionID,
			})
			if err != nil {
				t.Fatal(err)
			}
			done := watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  sessionID,
				OldState: string(classifier.StateRunning),
				NewState: string(classifier.StateDone),
				TurnID:   test.doneTurnID,
				Agent: &classifier.Agent{
					ID:        sessionID,
					State:     classifier.StateDone,
					Summary:   "Accepted result",
					Delegated: true,
				},
			}
			if _, err := NewService(store, &fakeWatcher{}, nil).RouteSessionEvent(done); err != nil {
				t.Fatal(err)
			}

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			deadPane := done
			deadPane.OldState = ""
			deadPane.NewState = string(classifier.StateFailed)
			deadPane.TurnID = test.failureTurnID
			deadPane.Agent = &classifier.Agent{
				ID:        sessionID,
				State:     classifier.StateFailed,
				Summary:   "Delegated provider process or pane is no longer live",
				Delegated: true,
			}
			if _, err := NewService(reopened, &fakeWatcher{}, nil).RouteSessionEvent(deadPane); err != nil {
				t.Fatal(err)
			}

			got, err := reopened.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := reopened.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			doneCount := 0
			failedCount := 0
			for _, event := range events {
				switch event.Kind {
				case "session.done":
					doneCount++
				case "session.failed":
					failedCount++
				}
			}
			if doneCount != 1 || failedCount != 0 ||
				got.Status != WorkWaiting ||
				got.NextAction != "Review the delegated Session result." {
				t.Fatalf("Work=%#v Events=%#v", got, events)
			}
		})
	}
}

func TestDelegatedTerminalFactAllowsLaterAcceptedTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-terminal:@1"
	item, err := store.CreateWork(Work{
		Title:            "Later accepted turn",
		Objective:        "Allow a new lifecycle after a new running boundary.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	route := func(state classifier.AgentState, turnID, summary string) {
		t.Helper()
		if _, routeErr := service.RouteSessionEvent(watcher.SessionEvent{
			Type:     "agent_state_change",
			AgentID:  sessionID,
			NewState: string(state),
			TurnID:   turnID,
			Agent: &classifier.Agent{
				ID:        sessionID,
				State:     state,
				Summary:   summary,
				Delegated: true,
			},
		}); routeErr != nil {
			t.Fatal(routeErr)
		}
	}
	route(classifier.StateDone, "turn-one", "First accepted result")
	route(classifier.StateRunning, "turn-two", "Second turn observed running")
	route(classifier.StateFailed, "turn-two", "Second turn failed")

	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	doneCount := 0
	failedCount := 0
	for _, event := range events {
		switch event.Kind {
		case "session.done":
			doneCount++
		case "session.failed":
			failedCount++
		}
	}
	if doneCount != 1 || failedCount != 1 ||
		got.Status != WorkWaiting ||
		got.NextAction != "Inspect the delegated Session failure." {
		t.Fatalf("Work=%#v Events=%#v", got, events)
	}
}

func TestDelegatedTerminalFactAllowsLaterDispatchedTurnToFailWithoutRunning(t *testing.T) {
	for _, test := range []struct {
		name        string
		priorTurnID string
	}{
		{name: "keyed prior terminal", priorTurnID: "turn-one"},
		{name: "legacy prior terminal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sessionID := "brain-agent-terminal:@1"
			item, err := store.CreateWork(Work{
				Title:            "Later dispatched turn",
				Objective:        "Allow a bounded no-start failure for a new accepted turn.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(store, &fakeWatcher{}, nil)
			route := func(eventType string, state classifier.AgentState, turnID, summary string) {
				t.Helper()
				if _, routeErr := service.RouteSessionEvent(watcher.SessionEvent{
					Type:     eventType,
					AgentID:  sessionID,
					NewState: string(state),
					TurnID:   turnID,
					Agent: &classifier.Agent{
						ID:        sessionID,
						State:     state,
						Summary:   summary,
						Delegated: true,
					},
				}); routeErr != nil {
					t.Fatal(routeErr)
				}
			}
			route("agent_state_change", classifier.StateDone, test.priorTurnID, "Prior accepted result")
			route("agent_metadata_change", classifier.StateUnknown, "turn-two", "New turn dispatched")
			route("agent_state_change", classifier.StateFailed, "turn-two", "Provider start was not observed")

			got, err := store.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			doneCount := 0
			progressCount := 0
			failedCount := 0
			for _, event := range events {
				switch event.Kind {
				case "session.done":
					doneCount++
				case "session.progress":
					progressCount++
				case "session.failed":
					failedCount++
				}
			}
			if doneCount != 1 || progressCount != 1 || failedCount != 1 ||
				got.Status != WorkWaiting ||
				got.NextAction != "Inspect the delegated Session failure." {
				t.Fatalf("Work=%#v Events=%#v", got, events)
			}
		})
	}
}

func TestDelegatedSameTurnProgressBeforeTerminalDoesNotReopenIt(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-terminal:@1"
	item, err := store.CreateWork(Work{
		Title:            "Same turn progress",
		Objective:        "Keep one turn terminal after its earlier dispatch Event.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	route := func(eventType string, state classifier.AgentState, summary string) {
		t.Helper()
		if _, routeErr := service.RouteSessionEvent(watcher.SessionEvent{
			Type:     eventType,
			AgentID:  sessionID,
			NewState: string(state),
			TurnID:   "turn-one",
			Agent: &classifier.Agent{
				ID:        sessionID,
				State:     state,
				Summary:   summary,
				Delegated: true,
			},
		}); routeErr != nil {
			t.Fatal(routeErr)
		}
	}
	route("agent_metadata_change", classifier.StateUnknown, "Turn dispatched")
	route("agent_state_change", classifier.StateDone, "Accepted result")
	route("agent_state_change", classifier.StateFailed, "Restart saw a dead pane")

	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedCount := 0
	for _, event := range events {
		if event.Kind == "session.failed" {
			failedCount++
		}
	}
	if failedCount != 0 ||
		got.Status != WorkWaiting ||
		got.NextAction != "Review the delegated Session result." {
		t.Fatalf("Work=%#v Events=%#v", got, events)
	}
}

func (w *fakeWatcher) Agents() []*classifier.Agent {
	out := make([]*classifier.Agent, 0, len(w.agents))
	for _, agent := range w.agents {
		cp := *agent
		out = append(out, &cp)
	}
	return out
}

func (w *fakeWatcher) GetAgent(id string) *classifier.Agent {
	if w.sessions != nil {
		if agent, ok := w.sessions[id]; ok {
			cp := *agent
			return &cp
		}
	}
	return nil
}

func (w *fakeWatcher) HasSession(target string) bool {
	if w.sessions == nil {
		return false
	}
	_, ok := w.sessions[target]
	return ok
}

func (w *fakeWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.sessions == nil {
		w.sessions = map[string]*classifier.Agent{}
	}
	id := "brain-agent-" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(opts.Name), " ", "-"))
	if opts.Hidden {
		id += "-hidden"
	}
	id += fmt.Sprintf(":@%d", len(w.created)+1)
	agent := &classifier.Agent{
		ID:        id,
		Name:      opts.Name + " (" + id + ")",
		Cwd:       opts.Cwd,
		Command:   opts.Command,
		State:     classifier.StateRunning,
		Summary:   "Session starting",
		Hidden:    opts.Hidden,
		Delegated: opts.Delegated && !opts.Hidden,
	}
	w.created = append(w.created, createdCall{id: id, opts: opts})
	w.sessions[id] = agent
	w.agents = append(w.agents, agent)
	return id, nil
}

func (w *fakeWatcher) SendInput(sessionID, text string) error {
	w.sentCalls = append(w.sentCalls, sentCall{sessionID: sessionID, text: text})
	if w.sendErr == nil {
		if w.captures == nil {
			w.captures = map[string]string{}
		}
		w.captures[sessionID] += text
	}
	return w.sendErr
}

func (w *fakeWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	return w.SendInput(sessionID, text)
}

func (w *fakeWatcher) SendInputWithReceiptResult(sessionID, text, receipt string) (watcher.InputResult, error) {
	if w.receipts != nil && w.receipts[sessionID] == receipt {
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
	}
	if err := w.SendInput(sessionID, text); err != nil {
		return watcher.InputResult{Outcome: watcher.InputOutcomeFromError(err), Receipt: receipt}, err
	}
	if w.receipts == nil {
		w.receipts = map[string]string{}
	}
	w.receipts[sessionID] = receipt
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
}

func (w *fakeWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	if w.sessions != nil {
		delete(w.sessions, sessionID)
	}
	nextAgents := w.agents[:0]
	for _, agent := range w.agents {
		if agent.ID != sessionID {
			nextAgents = append(nextAgents, agent)
		}
	}
	w.agents = nextAgents
	return nil
}

func (w *fakeWatcher) CapturePaneContent(sessionID string) (string, error) {
	return w.captures[sessionID], nil
}

func TestEnrichWorkResultEventsUsesLiveSessionWithoutErasingWorkFallback(t *testing.T) {
	events := []WorkResultEvent{
		{
			EventID:   "live-event",
			SessionID: "brain-agent-live:@1",
			Summary:   "Review the durable Work result.",
		},
		{
			EventID:   "closed-event",
			SessionID: "brain-agent-closed:@2",
			Summary:   "Inspect the durable Work fallback.",
		},
	}
	enrichWorkResultEvents(events, []AgentRef{{
		ID:      "brain-agent-live:@1",
		Name:    "Brain card worker",
		Summary: "Finished the focused implementation and tests.",
	}})

	if events[0].SessionName != "Brain card worker" ||
		events[0].Summary != "Finished the focused implementation and tests." {
		t.Fatalf("live result event = %#v", events[0])
	}
	if events[1].SessionName != "" ||
		events[1].Summary != "Inspect the durable Work fallback." {
		t.Fatalf("closed result event lost fallback = %#v", events[1])
	}
}

func TestEnrichWorkResultEventsCompactsLegacyLiveSummary(t *testing.T) {
	events := []WorkResultEvent{{
		EventID:   "legacy-event",
		SessionID: "brain-agent-live:@1",
		Summary:   "Work fallback.",
	}}
	enrichWorkResultEvents(events, []AgentRef{{
		ID:      "brain-agent-live:@1",
		Summary: strings.Repeat("结", 500),
	}})

	if len(events) != 1 ||
		utf8.RuneCountInString(events[0].Summary) != workResultSummaryRuneLimit ||
		!strings.HasSuffix(events[0].Summary, "…") {
		t.Fatalf("legacy live summary = %#v", events)
	}
}

func TestServiceSnapshotExposesWorkResultEvents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Expose the result",
		Objective:        "Project the durable occurrence in Brain snapshots.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range recentWorkResultEventLimit + 1 {
		now = now.Add(time.Minute)
		eventID := fmt.Sprintf("event-%02d", index)
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			ID: eventID, WorkID: item.ID, Kind: "session.done",
			DedupeKey: eventID, Summary: "Snapshot projection completed.",
			Actionable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := NewService(store, nil, nil).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ResultEvents) != recentWorkResultEventLimit ||
		snapshot.ResultEvents[0].EventID != "event-01" ||
		snapshot.ResultEvents[len(snapshot.ResultEvents)-1].EventID != "event-20" ||
		snapshot.ResultEvents[0].Summary != "Snapshot projection completed." {
		t.Fatalf("snapshot result events = %#v", snapshot.ResultEvents)
	}
}

func TestServiceSnapshotCreatesHiddenHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("snapshot should create exactly one host session, got %#v", fw.created)
	}
	if !fw.created[0].opts.Hidden || !fw.created[0].opts.Detached {
		t.Fatalf("host session options = %+v", fw.created[0].opts)
	}
	if snapshot.Workspace != store.WorkspacePath() {
		t.Fatalf("workspace = %q, want %q", snapshot.Workspace, store.WorkspacePath())
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != fw.created[0].id {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.Provider != "codex" || snapshot.HostExecutor.Runtime != work.AgentRuntimeTmux {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if len(snapshot.Executors) == 0 || !snapshot.Executors[0].Host {
		t.Fatalf("executors = %#v", snapshot.Executors)
	}
	if len(fw.sentCalls) == 0 || fw.sentCalls[0].sessionID != fw.created[0].id {
		t.Fatalf("expected bootstrap prompt to be sent to host, got %#v", fw.sentCalls)
	}
	if !strings.Contains(fw.created[0].opts.Command, codexFullAuthorizationFlag) {
		t.Fatalf("built-in Brain command should bypass Codex approvals and sandbox: %q", fw.created[0].opts.Command)
	}
}

func TestServiceSnapshotReusesMatchingHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	first, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected existing codex host to be reused, got %#v", fw.created)
	}
	command := fw.created[0].opts.Command
	if !strings.Contains(command, codexFullAuthorizationFlag) {
		t.Fatalf("codex Brain host should bypass approvals and sandbox: %q", command)
	}
	if strings.Count(command, codexFullAuthorizationFlag) != 1 {
		t.Fatalf("codex full authorization flag duplicated: %q", command)
	}
	if first.HostAgent == nil || second.HostAgent == nil || first.HostAgent.ID != second.HostAgent.ID {
		t.Fatalf("host agents = %#v / %#v", first.HostAgent, second.HostAgent)
	}
}

func TestServiceSnapshotAndContextDoNotMutateThreadRegistryForHost(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})
	first, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.HostAgent == nil {
		t.Fatal("initial Snapshot did not create the host fixture")
	}

	raw := []byte("{\"thread_id\":\"thread-current\",\"thread_ids\":[\"thread-old\",\"thread-current\"]}\n")
	path, before := writeChatStateFixture(t, root, raw)
	second, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if second.HostAgent == nil || second.HostAgent.ID != first.HostAgent.ID {
		t.Fatalf("host agents = %#v / %#v", first.HostAgent, second.HostAgent)
	}
	assertChatStateFixtureUnchanged(t, path, raw, before)
	context, err := service.Context()
	if err != nil {
		t.Fatal(err)
	}
	if context.ThreadID != "thread-current" || context.HostAgent == nil || context.HostAgent.ID != first.HostAgent.ID {
		t.Fatalf("context = %#v", context)
	}
	assertChatStateFixtureUnchanged(t, path, raw, before)
}

// Grok always-approve chrome previously false-positive blocked sessions. Blocked
// status must not by itself replace the Brain host on Snapshot/foreground re-entry.
func TestServiceSnapshotReusesGrokHostEvenWhenClassifiedBlocked(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-reuse:@29"
	if err := store.SetHostSession(hostID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {
				ID:      hostID,
				Name:    "Brain (" + hostID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "grok",
				State:   classifier.StateBlocked,
				Summary: "╰───── Grok 4.5 (high) · always-approve ─╯",
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[hostID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex"},
		},
	})
	// Prefer the recorded grok host executor for this Snapshot path.
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "grok")

	first, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("blocked chrome must not replace host, created %#v", fw.created)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("blocked chrome must not kill host, killed %#v", fw.killed)
	}
	if first.HostAgent == nil || first.HostAgent.ID != hostID {
		t.Fatalf("first host = %#v", first.HostAgent)
	}
	if second.HostAgent == nil || second.HostAgent.ID != hostID {
		t.Fatalf("second host = %#v", second.HostAgent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != hostID || hostSession.ExecutorID != "grok" {
		t.Fatalf("host session = %+v", hostSession)
	}
}

// Host replacement on re-entry is driven by a missing tmux target, not by status.
func TestServiceSnapshotReplacesHostWhenTmuxSessionMissing(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	// HasSession false: no sessions map entry and no agent list entry.
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok": {Name: "grok", Command: "grok", Kind: "grok"},
		},
	})
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "grok")

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected replacement host when tmux target missing, got %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID == oldID {
		t.Fatalf("host session id should be replaced, still %q", hostSession.ID)
	}
	audit, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), hostReplaceReasonMissingTmux) {
		t.Fatalf("expected missing_tmux audit, got %s", audit)
	}
	if !strings.Contains(string(audit), oldID) {
		t.Fatalf("audit should name previous host, got %s", audit)
	}
}

// Empty executor_id must not default to codex and kill a live grok Brain host on
// Snapshot (foreground reconnect / brain_snapshot). This is a documented
// replacement footgun independent of Grok blocked-chrome classification.
func TestServiceSnapshotAdoptsLiveHostProviderWhenExecutorIDEmpty(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-live-grok:@42"
	// Record id only — empty executor_id (legacy / partial write).
	if err := store.SetHostSessionID(hostID); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {
				ID:      hostID,
				Name:    "Brain (" + hostID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "grok",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[hostID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("must not kill live grok host when executor_id empty, killed %#v", fw.killed)
	}
	if len(fw.created) != 0 {
		t.Fatalf("must not create replacement, created %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != hostID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "grok" {
		t.Fatalf("host executor = %#v, want grok adopted from live host", snapshot.HostExecutor)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != hostID || hostSession.ExecutorID != "grok" {
		t.Fatalf("host session should persist adopted executor, got %+v", hostSession)
	}
}

// When the recorded host is gone but another matching Brain host is still alive,
// rebind instead of spawning a blank session (preserves continuity when ids drift).
func TestServiceSnapshotRebindsAliveHostWhenRecordedTargetMissing(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@292"
	aliveID := "brain-agent-brain-alive:@300"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID:      aliveID,
				Name:    "Brain (" + aliveID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "codex --dangerously-bypass-approvals-and-sandbox",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("should rebind alive host, not create: %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != aliveID {
		t.Fatalf("host agent = %#v, want rebound %s", snapshot.HostAgent, aliveID)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != aliveID {
		t.Fatalf("host session = %+v, want rebound alive id", hostSession)
	}
	audit, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("expected recovered_alive_host audit, got %s", audit)
	}
}

func TestServiceSnapshotAuditsProviderMismatchReplacement(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old-grok:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {
				ID:      oldID,
				Name:    "Brain",
				Cwd:     store.WorkspacePath(),
				Command: "grok",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})
	// Explicit env switch to codex while a grok host is still alive.
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "codex")

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("expected provider mismatch kill, killed %#v", fw.killed)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected codex replacement host, created %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	audit, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), hostReplaceReasonProviderMismatch) {
		t.Fatalf("expected provider_mismatch audit, got %s", audit)
	}
}

func TestServiceSnapshotFallsBackToCodexNotDelegatedExecutor(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("claude", map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	}))

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "codex" {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if len(fw.created) != 1 || !strings.HasPrefix(fw.created[0].opts.Command, "codex") {
		t.Fatalf("created host = %#v", fw.created)
	}
}

func TestServiceSnapshotHonorsHostExecutorOverride(t *testing.T) {
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "claude")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex":  {Name: "codex", Command: "codex"},
		"claude": {Name: "claude", Command: "claude"},
	}))

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "claude" || snapshot.HostExecutor.Provider != "claude" {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	command := fw.created[0].opts.Command
	if !strings.HasPrefix(command, "claude") || !strings.Contains(command, "--permission-mode bypassPermissions") || !strings.Contains(command, " --add-dir ") {
		t.Fatalf("host command = %q", command)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ExecutorID != "claude" {
		t.Fatalf("host executor id = %q", hostSession.ExecutorID)
	}
	if !strings.Contains(fw.sentCalls[0].text, "Host executor: claude") {
		t.Fatalf("bootstrap prompt did not include host executor metadata:\n%s", fw.sentCalls[0].text)
	}
}

func TestServiceBootstrapPromptDefaultsToAutonomousScheduling(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("bootstrap sends = %#v", fw.sentCalls)
	}
	prompt := fw.sentCalls[0].text
	assertCalendarPromptContract(t, prompt, "Do not infer Calendar items from unrelated messages")
	for _, want := range []string{
		"Delegated executor: codex",
		"Host Executor runs Brain chat, planning, delegation, review, and final synthesis.",
		"Delegated Executor runs delegated agents and ordinary non-Brain sessions unless the user explicitly asks for a different executor for that session",
		"Brain is the user's scheduler",
		"proactively create or reuse a visible delegated agent session",
		"Brain is the orchestrator, not the execution pool",
		"Delegate a subtask only when it can be named clearly",
		"Run independent delegated subtasks in parallel when that reduces elapsed time",
		"Delegated agents should not invent the overall plan",
		"Review delegated results before integrating them",
		"For a single larger task, prefer reusing the same delegated agent session",
		"Managed worktree root:",
		"Use the repository supplied by the user as the default workspace, even when it is dirty",
		"$ZEN_WORKTREE_ROOT",
		"TMPDIR/TMP/TEMP",
		"$ZEN_BUILD_TMPDIR",
		"Never hard-code OS-global temp paths",
		"Zen CLI quick reference",
		"only sessions with delegated=true are Brain-owned",
		"agent spawn -name",
		"agent capture -id",
		"agent send -id",
		"agent close -id",
		"Delegated agent lifecycle",
		"Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true",
		"Keep orchestration principles in Markdown, prompts, and agent instructions",
		"Treat an Active work event message as one claimed actionable delta",
		"consolidate options and a recommendation",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("bootstrap prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Only create or ask for a visible delegated agent session when the user explicitly asks") {
		t.Fatalf("bootstrap prompt still requires explicit delegation:\n%s", prompt)
	}
	if strings.Contains(prompt, "creates a visible delegated agent with the current Brain executor as executor") {
		t.Fatalf("bootstrap prompt still routes delegated agents to the current Brain executor:\n%s", prompt)
	}
	for _, unexpected := range []string{
		"resource admission is a ceiling",
		"smallest useful frontier",
		"Resource-Aware Scheduling",
		"do not launch work outside Zen's owned lifecycle",
		"safe concurrent headroom",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("bootstrap prompt should not include %q:\n%s", unexpected, prompt)
		}
	}
}

func TestServiceBootstrapPromptReferencesMemoryWithoutEmbeddingIt(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	memorySecret := "MEMORY_SECRET_SHOULD_NOT_BE_IN_BOOTSTRAP"
	profileSecret := "PROFILE_SECRET_SHOULD_NOT_BE_IN_BOOTSTRAP"
	if err := os.WriteFile(store.memoryPath(), []byte("# Brain Memory\n\n"+memorySecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.profileNotesPath(), []byte("# Brain Profile\n\n"+profileSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Memory, memorySecret) || !strings.Contains(snapshot.Profile, profileSecret) {
		t.Fatalf("snapshot should still expose stored memory/profile: %#v", snapshot)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("bootstrap sends = %#v", fw.sentCalls)
	}
	prompt := fw.sentCalls[0].text
	for _, want := range []string{
		"Treat this bootstrap as a map, not the full context",
		"read memory.md/profile.md on demand",
		"repairs product-owned standard Brain workspace blocks",
		"zen brain context --json",
		"zen brain playbooks --json",
		"progressive disclosure",
		"playbooks/",
		"current.md",
		"memory.md",
		"profile.md",
		"policies/delegation.md",
		"policies/engine.md",
		"policies/handoff.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("bootstrap prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unexpected := range []string{
		memorySecret,
		profileSecret,
		"Current memory:",
		"Current profile notes:",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("bootstrap prompt should not include %q:\n%s", unexpected, prompt)
		}
	}
}

func TestServiceSetHostExecutorPersistsAndStartsSelectedHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex":  {Name: "codex", Command: "codex"},
		"claude": {Name: "claude", Command: "claude"},
	}))

	snapshot, err := service.SetHostExecutor("claude")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "claude" {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == "" {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if len(fw.created) != 1 || !strings.HasPrefix(fw.created[0].opts.Command, "claude") {
		t.Fatalf("created = %#v", fw.created)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ExecutorID != "claude" || hostSession.ID != fw.created[0].id {
		t.Fatalf("host session = %+v", hostSession)
	}
}

func TestServiceSetHostExecutorHandsOffExistingThread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldHostID := "brain-agent-brain-old:@1"
	if err := store.SetHostSession(oldHostID, "grok"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{
		ThreadID:  "thread-main",
		ThreadIDs: []string{"thread-history", "thread-main"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.currentPath(), []byte("# Current Brain Context\n\n## Active Objective\n\nPreserve handoff objective.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHostID: {
				ID:      oldHostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "grok --no-alt-screen --permission-mode bypassPermissions",
				Hidden:  true,
			},
		},
	}
	service := NewService(store, fw, work.NewExecutorConfig("grok", map[string]work.Executor{
		"grok":  {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions", Kind: "grok", Runtime: work.AgentRuntimeTmux},
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
	}))
	registryRaw, err := os.ReadFile(store.ChatStatePath())
	if err != nil {
		t.Fatal(err)
	}
	registryInfo, err := os.Stat(store.ChatStatePath())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.SetHostExecutor("codex")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldHostID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldHostID {
		t.Fatalf("killed = %#v", fw.killed)
	}
	if len(fw.sentCalls) != 2 {
		t.Fatalf("sent calls = %#v", fw.sentCalls)
	}
	handoff := fw.sentCalls[1].text
	for _, want := range []string{
		"Brain host executor handoff:",
		"Previous host executor: grok",
		"Current host executor: codex",
		"Delegated executor: grok",
		"Read current.md in the Brain workspace before continuing.",
		"Preserve handoff objective.",
		"Host Executor runs Brain chat, planning, delegation, review, and final synthesis.",
		"Delegated Executor runs delegated agents and ordinary non-Brain sessions unless the user explicitly asks for a different executor for that session.",
		"Brain keeps decomposition, ordering, judgment, result review, and final synthesis.",
		"Delegated agents are scoped execution sessions",
		"Run independent subtasks in parallel when useful",
		"Inspect delegated results before integrating them.",
	} {
		if !strings.Contains(handoff, want) {
			t.Fatalf("handoff missing %q:\n%s", want, handoff)
		}
	}
	assertChatStateFixtureUnchanged(t, store.ChatStatePath(), registryRaw, registryInfo)
	state, err := store.ChatState("thread-main")
	if err != nil {
		t.Fatal(err)
	}
	if state.ThreadID != "thread-main" || len(state.ThreadIDs) != 2 ||
		state.ThreadIDs[0] != "thread-history" || state.ThreadIDs[1] != "thread-main" {
		t.Fatalf("thread registry = %#v", state)
	}
}

func TestServiceHousekeepingRepairsWorkspaceAndReportsDelegatedAgents(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.currentPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.policyPath("engine.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.policyPath("delegation.md"), []byte("# Old Delegation\n\nKeep delegated notes.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.policyPath("handoff.md"), []byte("# Old Handoff\n\nKeep handoff notes.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	delegatedID := "brain-agent-worker:@1"
	fw := &fakeWatcher{
		agents: []*classifier.Agent{
			{
				ID:        delegatedID,
				Name:      "Worker",
				State:     classifier.StateRunning,
				Cwd:       "/repo",
				Command:   "codex",
				Delegated: true,
			},
		},
	}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
	}))

	report, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ChangedPaths) == 0 {
		t.Fatalf("expected repaired workspace report: %+v", report)
	}
	for _, want := range []string{"current.md", "policies/delegation.md", "policies/engine.md", "policies/handoff.md"} {
		if !containsString(report.ChangedPaths, want) {
			t.Fatalf("changed paths %v missing %q", report.ChangedPaths, want)
		}
	}
	if !pathExists(store.currentPath()) || !pathExists(store.policyPath("engine.md")) {
		t.Fatalf("housekeeping did not backfill current/policy files")
	}
	delegation, err := os.ReadFile(store.policyPath("delegation.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Keep delegated notes.",
		"## Orchestrator / Delegation Model",
		"Review delegated output before integrating it",
	} {
		if !strings.Contains(string(delegation), want) {
			t.Fatalf("delegation policy missing %q:\n%s", want, delegation)
		}
	}
	engine, err := os.ReadFile(store.policyPath("engine.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(engine), "Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.") {
		t.Fatalf("engine policy was not backfilled:\n%s", engine)
	}
	handoff, err := os.ReadFile(store.policyPath("handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Keep handoff notes.",
		"## Rules",
		"Treat a host executor switch as a host replacement, not a new conversation.",
	} {
		if !strings.Contains(string(handoff), want) {
			t.Fatalf("handoff policy missing %q:\n%s", want, handoff)
		}
	}
	if len(report.OpenDelegatedAgents) != 1 || report.OpenDelegatedAgents[0].ID != delegatedID {
		t.Fatalf("delegated agents = %#v", report.OpenDelegatedAgents)
	}
	if len(report.RecommendedNextSteps) == 0 {
		t.Fatalf("expected recommended next steps: %+v", report)
	}
}

func TestServiceNewChatReplacesHostAndStartsFreshThread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldHostID := "old-host"
	oldThreadID := "thread-old"
	if err := store.SetHostSession(oldHostID, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{
		ThreadID:  oldThreadID,
		ThreadIDs: []string{"thread-history", oldThreadID},
	}); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHostID: {
				ID:      oldHostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "claude --add-dir '" + store.WorkspacePath() + "'",
				Hidden:  true,
			},
		},
	}
	service := NewService(store, fw, work.NewExecutorConfig("claude", map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude", Kind: "claude", Runtime: work.AgentRuntimeTmux},
	}))

	snapshot, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldHostID {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	created := fw.created[0]
	if !created.opts.Hidden || !created.opts.Detached || created.opts.Name != "Brain" {
		t.Fatalf("created host = %+v", created.opts)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != created.id {
		t.Fatalf("host agent = %#v created=%#v", snapshot.HostAgent, created)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "claude" {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if snapshot.ChatThreadID == "" || snapshot.ChatThreadID == oldThreadID {
		t.Fatalf("chat thread = %q, old = %q", snapshot.ChatThreadID, oldThreadID)
	}
	state, err := store.ChatState("")
	if err != nil {
		t.Fatal(err)
	}
	if state.ThreadID != snapshot.ChatThreadID || len(state.ThreadIDs) != 3 ||
		state.ThreadIDs[0] != "thread-history" || state.ThreadIDs[1] != oldThreadID ||
		state.ThreadIDs[2] != snapshot.ChatThreadID {
		t.Fatalf("new Chat thread registry = %#v", state)
	}
	known, err := store.HasChatThread(oldThreadID)
	if err != nil || !known {
		t.Fatalf("old thread known = %t, err = %v", known, err)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != created.id || hostSession.ExecutorID != "claude" {
		t.Fatalf("host session = %+v", hostSession)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != created.id {
		t.Fatalf("bootstrap sends = %#v", fw.sentCalls)
	}
	bootstrap := fw.sentCalls[0].text
	for _, want := range []string{
		"Brain is the orchestrator, not the execution pool",
		"Delegate a subtask only when it can be named clearly",
		"Run independent delegated subtasks in parallel when that reduces elapsed time",
		"Use the repository supplied by the user as the default workspace, even when it is dirty",
		"$ZEN_WORKTREE_ROOT",
		"TMPDIR/TMP/TEMP",
		"$ZEN_BUILD_TMPDIR",
		"Never hard-code OS-global temp paths",
		"Review delegated results before integrating them",
	} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("new chat bootstrap missing %q:\n%s", want, bootstrap)
		}
	}
	for _, unexpected := range []string{
		"resource admission is a ceiling",
		"smallest useful frontier",
		"do not launch work outside Zen's owned lifecycle",
	} {
		if strings.Contains(bootstrap, unexpected) {
			t.Fatalf("new chat bootstrap should not include %q:\n%s", unexpected, bootstrap)
		}
	}
}

func TestServiceSetHostExecutorRejectsUnknownAdapter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))

	if _, err := service.SetHostExecutor("claude"); err == nil {
		t.Fatal("expected unknown executor error")
	}
}

func TestServiceSnapshotSeesLiveDelegatedExecutorSwitch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execs := work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
		"grok":  {Name: "grok", Command: "grok --live", Kind: "grok", Runtime: work.AgentRuntimeTmux},
	})
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{}}
	service := NewService(store, fw, execs)

	before, err := service.Context()
	if err != nil {
		t.Fatal(err)
	}
	if before.DelegatedExecutor == nil || before.DelegatedExecutor.ID != "codex" {
		t.Fatalf("before delegated = %#v", before.DelegatedExecutor)
	}

	if err := execs.SetDelegatedExecutor("grok"); err != nil {
		t.Fatalf("SetDelegatedExecutor: %v", err)
	}

	after, err := service.Context()
	if err != nil {
		t.Fatal(err)
	}
	if after.DelegatedExecutor == nil || after.DelegatedExecutor.ID != "grok" {
		t.Fatalf("after delegated = %#v", after.DelegatedExecutor)
	}
	// Host executor is independent of delegated selection.
	if after.HostExecutor == nil || after.HostExecutor.ID == "" {
		t.Fatalf("host executor missing after delegated switch: %#v", after.HostExecutor)
	}
}

func TestServiceSnapshotReplacesMismatchedHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old:@1"
	if err := store.SetHostSessionID(oldID); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {
				ID:      oldID,
				Name:    "Brain (" + oldID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "claude",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected mismatched host to be replaced, got %#v", fw.created)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("expected mismatched host to be killed, got %#v", fw.killed)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
}

func TestServiceSnapshotPreservesCodexHostWithoutFullAuthorization(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "old-brain-host:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {
				ID:      oldID,
				Name:    "Brain (" + oldID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "codex --no-alt-screen -C '" + store.WorkspacePath() + "'",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("expected existing host to be preserved, killed %#v", fw.killed)
	}
	if len(fw.created) != 0 {
		t.Fatalf("expected no replacement host, got %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != oldID || hostSession.ExecutorID != "codex" {
		t.Fatalf("host session = %+v", hostSession)
	}
}

func TestServiceSnapshotFiltersHiddenHostFromVisibleAgents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		agents: []*classifier.Agent{{
			ID:      "main:@1",
			Name:    "Codex (main:@1)",
			State:   classifier.StateRunning,
			Summary: "working",
		}},
	}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].ID != "main:@1" {
		t.Fatalf("visible agents = %#v", snapshot.Agents)
	}
	if snapshot.HostAgent == nil || !snapshot.HostAgent.Hidden {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
}

func TestStoreUsesStateAndWorkspaceDirectories(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := filepath.Join(root, "workspace")
	if store.WorkspacePath() != wantWorkspace {
		t.Fatalf("workspace path = %q, want %q", store.WorkspacePath(), wantWorkspace)
	}
	if pathExists(retiredResultLogPath(root)) {
		t.Fatalf("fresh Brain store created retired result log")
	}
	if !pathExists(filepath.Join(root, "state", "reminders.json")) {
		t.Fatalf("missing state reminders file")
	}
	if !pathExists(filepath.Join(root, "workspace", "memory.md")) {
		t.Fatalf("missing workspace memory file")
	}
	if !pathExists(filepath.Join(root, "workspace", "current.md")) {
		t.Fatalf("missing workspace current file")
	}
	for _, policy := range []string{"delegation.md", "engine.md", "handoff.md"} {
		if !pathExists(filepath.Join(root, "workspace", "policies", policy)) {
			t.Fatalf("missing workspace policy file %s", policy)
		}
	}
	for _, playbook := range seedPlaybookFilenames() {
		if !pathExists(filepath.Join(root, "workspace", "playbooks", playbook)) {
			t.Fatalf("missing workspace playbook file %s", playbook)
		}
	}
	instructions, err := os.ReadFile(filepath.Join(root, "workspace", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertCalendarPromptContract(t, string(instructions), "Do not extract Calendar items automatically from unrelated chat")
	if !strings.Contains(string(instructions), "Keep a human-readable handoff projection in current.md; database Work/Event state is authoritative") {
		t.Fatalf("workspace instructions do not describe current.md:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Use policies/ for stable Brain orchestration rules") {
		t.Fatalf("workspace instructions do not describe policies:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Use playbooks/ for provider-neutral operating playbooks") {
		t.Fatalf("workspace instructions do not describe playbooks:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Brain is the user's scheduler") {
		t.Fatalf("workspace instructions do not describe scheduler behavior:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Brain is the orchestrator, not the execution pool") {
		t.Fatalf("workspace instructions do not describe orchestrator behavior:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Delegate only clean subtasks with one concern") {
		t.Fatalf("workspace instructions do not describe scoped delegated briefs:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "inspect their reports before integrating results") {
		t.Fatalf("workspace instructions do not describe delegated result review:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "For a single larger task, prefer reusing the same delegated agent session") {
		t.Fatalf("workspace instructions do not describe delegated session reuse:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Keep orchestration principles in Markdown, prompts, and agent instructions") {
		t.Fatalf("workspace instructions do not describe prompt-first orchestration:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Treat an Active work event message as one claimed actionable delta") {
		t.Fatalf("workspace instructions do not describe Work event handling:\n%s", instructions)
	}
	for _, want := range []string{"zen brain context --json", "zen brain playbooks --json", "zen agent list --json", "zen agent spawn -name", "zen agent capture -id", "zen agent send -id", "zen agent close -id"} {
		if !strings.Contains(string(instructions), want) {
			t.Fatalf("workspace instructions missing %q:\n%s", want, instructions)
		}
	}
	if !strings.Contains(string(instructions), "Keep delegated agent lifecycle ownership") {
		t.Fatalf("workspace instructions missing lifecycle ownership:\n%s", instructions)
	}
	for _, want := range []string{
		"$ZEN_WORKTREE_ROOT",
		"TMPDIR/TMP/TEMP",
		"$ZEN_BUILD_TMPDIR",
		"Never hard-code OS-global temp paths",
	} {
		if !strings.Contains(string(instructions), want) {
			t.Fatalf("workspace instructions missing %q:\n%s", want, instructions)
		}
	}
	if !strings.Contains(string(instructions), "Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true") {
		t.Fatalf("workspace instructions missing external session guard:\n%s", instructions)
	}
	if strings.Contains(string(instructions), "only when the user asks Brain to delegate real work") {
		t.Fatalf("workspace instructions still require explicit delegation:\n%s", instructions)
	}
}

func TestStoreContextAndHousekeepingDoNotCreateOrReadRetiredResultLog(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			root := t.TempDir()
			path := retiredResultLogPath(root)
			var before os.FileInfo
			want := []byte("not current Brain state\n")
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, want, 0o640); err != nil {
					t.Fatal(err)
				}
				var err error
				before, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
			}

			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(store, nil, nil)
			if _, err := service.Context(); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Housekeeping(); err != nil {
				t.Fatal(err)
			}

			if !existing {
				if pathExists(path) {
					t.Fatal("Brain context/housekeeping created retired result log")
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) || after.Mode() != before.Mode() ||
				!after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
				t.Fatalf("retired result log changed: bytes=%q mode=%v mtime=%v", got, after.Mode(), after.ModTime())
			}
		})
	}
}

func retiredResultLogPath(root string) string {
	return filepath.Join(root, "state", "messages.jsonl")
}

func TestStorePreservesUnmarkedWorkspaceInstructionsBeforeCanonicalBlock(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	staleInstructions := `# Brain Workspace

Custom local note.

- Only create or ask for a visible delegated agent session when the user explicitly asks you to delegate real work.
`
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(staleInstructions), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	instructions := string(raw)
	if !strings.HasPrefix(instructions, staleInstructions) {
		t.Fatalf("workspace instructions changed unmarked existing bytes:\n%s", instructions)
	}
	for _, want := range []string{
		managedStartMarker(brainAgentsManagedID),
		"## Brain Orchestration Rules",
		"## Brain Communication Rules",
		"## Executor Rules",
		"## Zen CLI",
		managedEndMarker(brainAgentsManagedID),
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("workspace instructions missing %q:\n%s", want, instructions)
		}
	}
}

func TestServiceHousekeepingRepairsCalendarContractWithoutOverwritingUserContent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	customInstructions := "# My Brain Rules\n\nKeep this user-authored rule.\n"
	if err := os.WriteFile(store.workspaceInstructionsPath(), []byte(customInstructions), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))

	report, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.ChangedPaths, "AGENTS.md") {
		t.Fatalf("calendar instruction repair was not reported: %+v", report)
	}
	raw, err := os.ReadFile(store.workspaceInstructionsPath())
	if err != nil {
		t.Fatal(err)
	}
	instructions := string(raw)
	if !strings.Contains(instructions, "Keep this user-authored rule.") {
		t.Fatalf("housekeeping overwrote user content:\n%s", instructions)
	}
	assertCalendarPromptContract(t, instructions, "Do not extract Calendar items automatically from unrelated chat")
}

func assertCalendarPromptContract(t *testing.T, value, noAutoExtractionMarker string) {
	t.Helper()
	for _, want := range []string{
		"calendar list/get/create/update/cancel/run",
		"explicit time intent",
		"event, reminder, and deadline are passive Calendar records",
		"scheduled_action launches delegated execution",
		"current Brain thread_id from ",
		"brain context --json and pass that exact value",
		"pass that exact value as -source-thread (source_thread_id)",
		"Never invent, omit, or silently retarget this thread",
		"canonical full result, or a concise failure, returns idempotently to that captured Brain thread",
		"unread state and notifications are projections",
		"A recurring series continues after a failed occurrence",
		"local YYYY-MM-DD date, HH:MM wall time, and IANA timezone",
		"DST fall-back",
		"first or second; never guess",
		"After create, update, or run",
		"resolved local date",
		"recurrence/effect",
		"result destination from the command confirmation",
		noAutoExtractionMarker,
	} {
		if !strings.Contains(value, want) {
			t.Fatalf("Calendar prompt contract missing %q:\n%s", want, value)
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
