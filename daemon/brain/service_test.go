package brain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type fakeWatcher struct {
	agents           []*classifier.Agent
	sessions         map[string]*classifier.Agent
	created          []createdCall
	sentCalls        []sentCall
	killed           []string
	sendErr          error
	createErr        error
	killErr          error
	killLeavesLive   bool
	probeErr         error
	probeErrByID     map[string]error
	createHook       func()
	captures         map[string]string
	receipts         map[string]string
	outcomes         map[string]watcher.InputOutcome
	turnStore        *Store
	providerEvidence map[string]watcher.ProviderActivityObservation
	providerProbeErr map[string]error
	ownedGenerations map[string]string
}

type createdCall struct {
	id   string
	opts watcher.CreateSessionOptions
}

type sentCall struct {
	sessionID string
	text      string
}

func TestRouteSessionEventWithoutCanonicalTurnNeverCreatesLifecycleEvents(t *testing.T) {
	// Slice 1 contract: a delegated lifecycle Work Event is unrepresentable
	// without the current canonical TurnID. Raw classifier/pane state (the
	// legacy sessionEventProjection path) must never project done/failed/
	// needs_input/stale events, no matter how terminal the pane looks.
	for _, test := range []struct {
		name  string
		event watcher.SessionEvent
	}{
		{
			name: "state done",
			event: watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  "brain-agent-markerless:@1",
				OldState: string(classifier.StateRunning),
				NewState: string(classifier.StateDone),
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateDone,
					Summary: "Session starting", Delegated: true, PaneAlive: true,
				},
			},
		},
		{
			name: "state failed",
			event: watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  "brain-agent-markerless:@1",
				OldState: string(classifier.StateRunning),
				NewState: string(classifier.StateFailed),
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateFailed,
					Summary: "Session starting", Delegated: true, PaneAlive: true,
				},
			},
		},
		{
			name: "state blocked",
			event: watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  "brain-agent-markerless:@1",
				OldState: string(classifier.StateRunning),
				NewState: string(classifier.StateBlocked),
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateBlocked,
					Summary: "Session starting", Delegated: true, PaneAlive: true,
				},
			},
		},
		{
			name: "metadata attention failed",
			event: watcher.SessionEvent{
				Type:    "agent_metadata_change",
				AgentID: "brain-agent-markerless:@1",
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateRunning,
					Attention: "failed", NeedsAttention: true,
					Summary: "Session starting", Delegated: true, PaneAlive: true,
				},
			},
		},
		{
			name: "metadata attention user input",
			event: watcher.SessionEvent{
				Type:    "agent_metadata_change",
				AgentID: "brain-agent-markerless:@1",
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateRunning,
					Attention: "user_input", NeedsAttention: true,
					Summary: "Resolve the delegated Session request.", Delegated: true, PaneAlive: true,
				},
			},
		},
		{
			name: "removed",
			event: watcher.SessionEvent{
				Type:     "agent_removed",
				AgentID:  "brain-agent-markerless:@1",
				OldState: string(classifier.StateRunning),
				NewState: string(classifier.StateRemoved),
				Agent: &classifier.Agent{
					ID: "brain-agent-markerless:@1", State: classifier.StateRemoved,
					Summary: "Session starting", Delegated: true,
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sessionID := strings.TrimSpace(test.event.AgentID)
			item, err := store.CreateWork(Work{
				Title:            "Markerless delegated session",
				Objective:        "No lifecycle began without a canonical turn.",
				Status:           WorkRunning,
				AttemptSessionID: sessionID,
				CompletionPolicy: CompletionBounded,
				NextAction:       "Wait for the delegated Session.",
				WaitFor:          "Session " + sessionID,
			})
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(store, &fakeWatcher{}, nil)
			for attempt := 0; attempt < 2; attempt++ {
				if woke, routeErr := service.RouteSessionEvent(test.event); routeErr != nil || woke {
					t.Fatalf("route woke=%v err=%v", woke, routeErr)
				}
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 0 {
				t.Fatalf("markerless session produced lifecycle Events=%#v", events)
			}
			got, err := store.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != item.Status || got.AttemptSessionID != "" || got.AttemptDelegated {
				t.Fatalf("markerless Work mutated: %#v", got)
			}
		})
	}
}

func TestReconcileDelegatedSessionsWithoutTurnNeverRoutesRawState(t *testing.T) {
	// Slice 1 contract: heartbeat reconciliation reads the current ledger
	// record only. A Work-owning delegated Session with no canonical turn is
	// never staled, never failed, and never gets Work text rewritten from raw
	// pane/process/classifier state (the false "Session starting" wake class).
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	sessionID := "brain-agent-markerless:@1"
	item, err := store.CreateWork(Work{
		Title:            "Markerless delegated session",
		Objective:        "No lifecycle began without a canonical turn.",
		Status:           WorkRunning,
		AttemptSessionID: sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	agents := []*classifier.Agent{{
		ID: sessionID, State: classifier.StateFailed, Summary: "Session starting",
		Delegated: true, PaneAlive: true,
		ExpectedNextCheckAt: &now, // long-expired cross-turn lease must not stale a turnless session
	}}
	service := NewService(store, &fakeWatcher{sessions: map[string]*classifier.Agent{agents[0].ID: agents[0]}}, nil)
	service.ReconcileDelegatedSessions(agents)
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("markerless session reconcile produced Events=%#v", events)
	}
	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != item.Status || got.AttemptSessionID != "" || got.NextAction != item.NextAction {
		t.Fatalf("markerless Work text mutated by raw state: %#v", got)
	}
}

func TestRouteSessionEventWithCanonicalTurnRedispatchesOnly(t *testing.T) {
	// Canonical-turn sessions route only to delivery: the reducer already
	// derived Work + Events atomically. A raw "failed" state on a live
	// canonical turn never creates a second lifecycle row.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	sessionID := "brain-agent-canonical:@1"
	item, err := store.CreateWork(Work{
		Title:            "Canonical delegated session",
		Objective:        "The ledger owns lifecycle.",
		Status:           WorkRunning,
		AttemptSessionID: sessionID,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID:  sessionID,
		TurnID:     "turn-one",
		AcceptedAt: now,
	})
	service := NewService(store, &fakeWatcher{}, nil)
	agent := &classifier.Agent{
		ID: sessionID, State: classifier.StateFailed, Summary: "Session starting",
		Delegated: true, PaneAlive: true,
	}
	if _, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  sessionID,
		Agent:    agent,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateFailed),
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "session.failed" {
			t.Fatalf("raw failed state created a canonical failure row: %#v", events)
		}
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
	presence, err := w.ProbeSession(target)
	return err == nil && presence == watcher.SessionPresencePresent
}

func (w *fakeWatcher) ProbeSession(target string) (watcher.SessionPresence, error) {
	if w.probeErrByID != nil {
		if err, ok := w.probeErrByID[target]; ok && err != nil {
			return watcher.SessionPresenceUnknown, err
		}
	}
	if w.probeErr != nil {
		return watcher.SessionPresenceUnknown, w.probeErr
	}
	if w.sessions == nil {
		return watcher.SessionPresenceAbsent, nil
	}
	if _, ok := w.sessions[target]; ok {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}

func (w *fakeWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.createErr != nil {
		return "", w.createErr
	}
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
	if w.createHook != nil {
		w.createHook()
	}
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
	if w.outcomes != nil && w.outcomes[receipt] == watcher.InputAccepted {
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
	}
	if err := w.SendInput(sessionID, text); err != nil {
		outcome := watcher.InputOutcomeFromError(err)
		if outcome == watcher.InputAmbiguous {
			if w.outcomes == nil {
				w.outcomes = map[string]watcher.InputOutcome{}
			}
			w.outcomes[receipt] = outcome
		}
		return watcher.InputResult{Outcome: outcome, Receipt: receipt}, err
	}
	if w.receipts == nil {
		w.receipts = map[string]string{}
	}
	if w.outcomes == nil {
		w.outcomes = map[string]watcher.InputOutcome{}
	}
	w.receipts[sessionID] = receipt
	w.outcomes[receipt] = watcher.InputAccepted
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
}

func (w *fakeWatcher) SubmitDelegatedInput(sessionID, payload, turnID string, _ time.Time) (watcher.InputResult, error) {
	w.sentCalls = append(w.sentCalls, sentCall{sessionID: sessionID, text: payload})
	if w.sendErr != nil {
		return watcher.InputResult{Outcome: watcher.InputOutcomeFromError(w.sendErr), Receipt: turnID, TurnID: turnID}, w.sendErr
	}
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: turnID, TurnID: turnID}, nil
}

func (w *fakeWatcher) SubmitDelegatedWorkInput(
	sessionID, payload, workID, turnID, purpose, purposeID string, acceptedAt time.Time,
) (watcher.InputResult, error) {
	w.sentCalls = append(w.sentCalls, sentCall{sessionID: sessionID, text: payload})
	if w.sendErr != nil {
		return watcher.InputResult{Outcome: watcher.InputOutcomeFromError(w.sendErr), Receipt: turnID, TurnID: turnID}, w.sendErr
	}
	if w.turnStore != nil {
		pending, _, err := w.turnStore.PrepareInputAdmission(watcher.InputAdmission{
			WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
			PayloadSHA256: pendingSubmissionDigest(payload), ProcessIdentity: "delegated-process",
			PaneGeneration: "delegated-pane", AcceptedAt: acceptedAt.UTC(), Mode: watcher.InputAdmissionFresh,
			SignalProtocol: true, Purpose: purpose, PurposeID: purposeID,
		})
		if err != nil {
			return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: turnID, TurnID: turnID}, err
		}
		resolvedAt := acceptedAt.Add(time.Millisecond).UTC()
		if _, err := w.turnStore.ResolveInputAdmission(watcher.InputAdmissionResolution{
			SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID, PayloadSHA256: pending.PayloadSHA256,
			ActivityID: "delegated-activity-" + turnID,
			Admission:  watcher.TurnAdmission{Stream: "provider", ID: "delegated-admission-" + turnID, Cursor: 1, SHA256: pending.PayloadSHA256, At: resolvedAt},
			ResolvedAt: resolvedAt,
		}); err != nil {
			return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: turnID, TurnID: turnID}, err
		}
	}
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: turnID, TurnID: turnID}, nil
}

func (w *fakeWatcher) SubmitBrainHostInput(
	sessionID, payload, eventID, claimToken, workID, providerTurnID string,
	acceptedAt time.Time,
) (watcher.InputResult, error) {
	if w.turnStore != nil && w.sendErr == nil {
		// A re-delivery of the same action identity is a fresh delivery attempt:
		// the transport receipt ledger is re-written per attempt, never reused.
		if w.outcomes != nil {
			delete(w.outcomes, eventID)
		}
		existingTurnID := ""
		if current, found, err := w.turnStore.Turn(sessionID); err != nil {
			return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID}, err
		} else if found {
			existingTurnID = current.TurnID
		}
		pending, created, err := w.turnStore.PrepareInputAdmission(watcher.InputAdmission{
			WorkID: workID, SessionID: sessionID, ProposedTurnID: providerTurnID,
			Receipt: eventID, ClaimToken: claimToken,
			PayloadSHA256:   pendingSubmissionDigest(payload),
			ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
			AcceptedAt: acceptedAt.UTC(), Mode: watcher.InputAdmissionFresh, ExistingTurnID: existingTurnID,
		})
		if err != nil {
			return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID}, err
		}
		if !created {
			return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID},
				fmt.Errorf("Host submission was not freshly prepared")
		}
		result, err := w.SendInputWithReceiptResult(sessionID, payload, eventID)
		result.TurnID = providerTurnID
		if err != nil {
			return result, err
		}
		resolvedAt := acceptedAt.Add(time.Millisecond).UTC()
		resolved, err := w.turnStore.ResolveInputAdmission(watcher.InputAdmissionResolution{
			SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: eventID,
			PayloadSHA256: pending.PayloadSHA256, ActivityID: "host-activity-" + providerTurnID,
			Admission: watcher.TurnAdmission{
				Stream: "provider", ID: "host-admission-" + providerTurnID, Cursor: 1,
				SHA256: pending.PayloadSHA256, At: resolvedAt,
			},
			ResolvedAt: resolvedAt,
		})
		if err != nil {
			result.Outcome = watcher.InputAmbiguous
			return result, err
		}
		result.Outcome = watcher.InputAccepted
		result.TurnID = resolved.ResolvedTurnID
		return result, nil
	}
	result, err := w.SendInputWithReceiptResult(sessionID, payload, eventID)
	result.TurnID = providerTurnID
	return result, err
}

func (w *fakeWatcher) InputReceiptResult(_ string, receipt string) (watcher.InputResult, bool, error) {
	outcome, found := w.outcomes[receipt]
	return watcher.InputResult{Outcome: outcome, Receipt: receipt}, found, nil
}

func (w *fakeWatcher) setReceiptOutcome(receipt string, outcome watcher.InputOutcome) {
	if w.outcomes == nil {
		w.outcomes = map[string]watcher.InputOutcome{}
	}
	w.outcomes[receipt] = outcome
}

func (w *fakeWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	if w.killLeavesLive && w.killErr != nil {
		return w.killErr
	}
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
	if w.killErr != nil {
		return w.killErr
	}
	return nil
}

func (w *fakeWatcher) CapturePaneContent(sessionID string) (string, error) {
	return w.captures[sessionID], nil
}

func (w *fakeWatcher) ProbeProviderEvidence(sessionID string) (watcher.ProviderActivityObservation, bool, error) {
	if err := w.providerProbeErr[sessionID]; err != nil {
		return watcher.ProviderActivityObservation{}, false, err
	}
	observation, found := w.providerEvidence[sessionID]
	return observation, found, nil
}

func (w *fakeWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	if generation := strings.TrimSpace(w.ownedGenerations[sessionID]); generation != "" {
		return watcher.OwnedGeneration{SessionID: sessionID, Generation: generation}, nil
	}
	agent := w.GetAgent(sessionID)
	if agent == nil {
		return watcher.OwnedGeneration{}, fmt.Errorf("Session %s is unavailable", sessionID)
	}
	generation := AdmissionDigest(fmt.Sprintf("%s\x00%d\x00%d", sessionID, agent.ProcessID, agent.StartedAt.UnixNano()))
	return watcher.OwnedGeneration{SessionID: sessionID, Generation: generation}, nil
}

func (w *fakeWatcher) ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	return w.ResolveOwnedGeneration(sessionID)
}

func TestHostInputAdmissionLiveCriticalSectionAndRestartSettlement(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID    = "brain-host:@input-critical-section"
		threadID  = "thread-input-critical-section"
		requestID = "request-input-critical-section"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "host-generation-one"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	service := NewService(store, fw, nil)
	prepared, created, err := service.PrepareHostUserInput(
		hostID, requestID, "do not race the provider call", "brain-thread:"+threadID,
	)
	if err != nil || !created || prepared.State != BrainInputAdmissionPending {
		t.Fatalf("prepare created=%v admission=%+v err=%v", created, prepared, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("live in-flight reconcile woke=%v err=%v", woke, err)
	}
	if current, found, err := store.BrainInputAdmission(requestID, threadID); err != nil ||
		!found || current.State != BrainInputAdmissionPending {
		t.Fatalf("live attempt was prematurely settled: found=%v admission=%+v err=%v", found, current, err)
	}

	// A process restart erases only the critical-section marker. The durable
	// intent and exact preserved generation remain, so an absent receipt proves
	// non-submission and terminalizes the request without provider replay.
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopened
	if woke, err := NewService(reopened, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("restart settlement woke=%v err=%v", woke, err)
	}
	settled, found, err := reopened.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || settled.State != BrainInputAdmissionNotSubmitted || settled.SettledAt == nil {
		t.Fatalf("restart settlement found=%v admission=%+v err=%v", found, settled, err)
	}

	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopenedAgain
	if woke, err := NewService(reopenedAgain, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("second restart woke=%v err=%v", woke, err)
	}
	// The same logical input retried after a proven non-mutation is re-armed
	// (same identity, same payload, current generation): the retry may cross
	// the mutation boundary again, and no provider input is replayed by the
	// reconciliation itself.
	duplicate, duplicateCreated, err := NewService(reopenedAgain, fw, nil).PrepareHostUserInput(
		hostID, requestID, "do not race the provider call", "brain-thread:"+threadID,
	)
	if err != nil || !duplicateCreated || duplicate.State != BrainInputAdmissionPending {
		t.Fatalf("same-input retry re-arm created=%v admission=%+v err=%v", duplicateCreated, duplicate, err)
	}
	if duplicate.RequestID != requestID || duplicate.BodySHA256 != AdmissionDigest("do not race the provider call") {
		t.Fatalf("re-armed retry lost the logical input identity: %+v", duplicate)
	}
	if duplicate.HostGeneration != "host-generation-one" {
		t.Fatalf("re-armed retry did not adopt the current host generation: %+v", duplicate)
	}
	if len(fw.sentCalls) != 0 {
		t.Fatalf("restart settlement replayed provider input: %+v", fw.sentCalls)
	}
}

func TestHostInputAdmissionDifferentPayloadNeverRearmsSameIdentity(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID    = "brain-host:@input-edited-retry"
		threadID  = "thread-input-edited-retry"
		requestID = "request-input-edited-retry"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "host-generation-one"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	service := NewService(store, fw, nil)
	prepared, created, err := service.PrepareHostUserInput(
		hostID, requestID, "original message", "brain-thread:"+threadID,
	)
	if err != nil || !created || prepared.State != BrainInputAdmissionPending {
		t.Fatalf("prepare created=%v admission=%+v err=%v", created, prepared, err)
	}
	if err := service.AbortHostUserInput(prepared.RequestID, prepared.ThreadID); err != nil {
		t.Fatalf("abort: %v", err)
	}
	settled, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || settled.State != BrainInputAdmissionNotSubmitted {
		t.Fatalf("abort settlement found=%v admission=%+v err=%v", found, settled, err)
	}
	// An edited payload under the same identity is a different logical input:
	// it must fail closed and never re-arm the original row.
	if _, _, err := service.PrepareHostUserInput(
		hostID, requestID, "edited message", "brain-thread:"+threadID,
	); err == nil || !strings.Contains(err.Error(), "belongs to different input") {
		t.Fatalf("edited payload under same identity did not fail closed: %v", err)
	}
	retained, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || retained.State != BrainInputAdmissionNotSubmitted {
		t.Fatalf("failed edited retry mutated the original row: found=%v admission=%+v err=%v", found, retained, err)
	}
}

func TestHostInputAdmissionRestartUsesExactAcceptedReceipt(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID    = "brain-host:@input-accepted"
		threadID  = "thread-input-accepted"
		requestID = "request-input-accepted"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "host-generation-accepted"},
		outcomes:         map[string]watcher.InputOutcome{requestID: watcher.InputAccepted},
	}
	service := NewService(store, fw, nil)
	if _, created, err := service.PrepareHostUserInput(
		hostID, requestID, "accepted before process loss", "brain-thread:"+threadID,
	); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopened
	if woke, err := NewService(reopened, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("accepted restart reconcile woke=%v err=%v", woke, err)
	}
	accepted, found, err := reopened.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || accepted.State != BrainInputAdmissionAccepted || accepted.AcceptedAt == nil {
		t.Fatalf("accepted receipt found=%v admission=%+v err=%v", found, accepted, err)
	}
	active, err := reopened.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID != accepted.HostTurnID ||
		active.HostGeneration != "host-generation-accepted" {
		t.Fatalf("recovered foreground=%+v err=%v", active, err)
	}
	items, err := reopened.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].Body != "accepted before process loss" {
		t.Fatalf("recovered timeline=%+v err=%v", items, err)
	}

	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopenedAgain
	if woke, err := NewService(reopenedAgain, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("accepted second restart woke=%v err=%v", woke, err)
	}
	items, err = reopenedAgain.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("accepted receipt replayed projection: items=%+v err=%v", items, err)
	}
	if len(fw.sentCalls) != 0 {
		t.Fatalf("accepted receipt was replayed to provider: %+v", fw.sentCalls)
	}
}

func TestHostInputAdmissionReplacementBecomesUncertainAndFreesLane(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID    = "brain-host:@input-replaced"
		threadID  = "thread-input-replaced"
		requestID = "request-input-replaced"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "host-generation-old"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	service := NewService(store, fw, nil)
	if _, created, err := service.PrepareHostUserInput(
		hostID, requestID, "outcome lost with old pane", "brain-thread:"+threadID,
	); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	item, err := store.CreateWork(Work{
		Title: "Unrelated ready Work", Objective: "Dispatch after the stale input gate converges.",
		Status: WorkWaiting, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "review.changed", DedupeKey: "replaced-input:unrelated", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append Event created=%v err=%v", created, err)
	}
	if _, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), event.Kind, event.ID, event.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	fw.ownedGenerations[hostID] = "host-generation-replacement"
	recovered, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = recovered
	if woke, err := NewService(recovered, fw, nil).ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("replacement convergence woke=%v err=%v", woke, err)
	}
	settled, found, err := recovered.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || settled.State != BrainInputAdmissionUncertain || settled.SettledAt == nil {
		t.Fatalf("uncertain settlement found=%v admission=%+v err=%v", found, settled, err)
	}
	events, err := recovered.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID || !reviewLeaseDelivered(t, recovered, item.ID) {
		t.Fatalf("unrelated review did not dispatch: events=%+v err=%v", events, err)
	}
	items, err := recovered.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != "brain-input-uncertain:"+requestID {
		t.Fatalf("uncertain diagnostic=%+v err=%v", items, err)
	}

	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = reopened
	if woke, err := NewService(reopened, fw, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("uncertain reopen woke=%v err=%v", woke, err)
	}
	items, err = reopened.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("uncertain diagnostic replayed: items=%+v err=%v", items, err)
	}
}

func TestHostBindingReplacementRetiresExactForegroundAndDispatches(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		oldHost  = "brain-host:@foreground-old"
		newHost  = "brain-host:@foreground-new"
		threadID = "thread-foreground-host-replacement"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(oldHost, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHost: {ID: oldHost, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{
			oldHost: "old-host-generation",
			newHost: "new-host-generation",
		},
		outcomes:  map[string]watcher.InputOutcome{},
		turnStore: store,
	}
	service := NewService(store, fw, nil)
	oldAdmission, created, err := service.PrepareHostUserInput(
		oldHost, "old-host-input", "finish on the old Host", "brain-thread:"+threadID,
	)
	if err != nil || !created {
		t.Fatalf("old prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(oldAdmission); err != nil {
		t.Fatal(err)
	}
	oldForeground, err := store.CurrentHostForegroundTurn()
	if err != nil || oldForeground == nil || oldForeground.HostSessionID != oldHost {
		t.Fatalf("old foreground=%+v err=%v", oldForeground, err)
	}

	item, err := store.CreateWork(Work{
		Title: "Ready after Host replacement", Objective: "Dispatch without the old foreground wedge.",
		Status: WorkWaiting, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "review.changed", DedupeKey: "host-replacement:ready", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append ready Event created=%v err=%v", created, err)
	}
	if _, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), event.Kind, event.ID, event.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	delete(fw.sessions, oldHost)
	fw.sessions[newHost] = &classifier.Agent{ID: newHost, Hidden: true, State: classifier.StateRunning}
	if err := store.SetHostSession(newHost, "codex"); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("replacement reconcile woke=%v err=%v", woke, err)
	}
	if active, err := store.CurrentHostForegroundTurn(); err != nil || active != nil {
		t.Fatalf("old foreground survived replacement: active=%+v err=%v", active, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("replacement event history: events=%+v err=%v", events, err)
	}
	if lease := requireReviewDelivered(t, store, item.ID); lease.HostSessionID != newHost {
		t.Fatalf("replacement did not dispatch on new Host: lease=%+v", lease)
	}
	auditRaw, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(bytes.TrimSpace(auditRaw), []byte{'\n'}) + 1; lines != 1 ||
		!bytes.Contains(auditRaw, []byte(`"reason":"host_binding_replaced"`)) {
		t.Fatalf("foreground retirement audit=%q", auditRaw)
	}

	// A new foreground may begin even while the newly delivered card awaits a
	// disposition. A delayed old terminal edge and delayed old CAS are both
	// fenced by exact Host/generation/turn identity.
	newAdmission, created, err := service.PrepareHostUserInput(
		newHost, "new-host-input", "continue on the replacement Host", "brain-thread:"+threadID,
	)
	if err != nil || !created {
		t.Fatalf("new prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(newAdmission); err != nil {
		t.Fatal(err)
	}
	newForeground, err := store.CurrentHostForegroundTurn()
	if err != nil || newForeground == nil || newForeground.HostSessionID != newHost {
		t.Fatalf("new foreground=%+v err=%v", newForeground, err)
	}
	if retired, err := store.RetireHostForegroundTurn(*oldForeground); err != nil || retired {
		t.Fatalf("delayed old retirement retired=%v err=%v", retired, err)
	}
	if woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "agent_state_change", AgentID: oldHost,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: "old-provider-turn",
		Agent:  &classifier.Agent{ID: oldHost, Hidden: true, State: classifier.StateDone},
	}); err != nil || woke {
		t.Fatalf("delayed old terminal woke=%v err=%v", woke, err)
	}
	stillActive, err := store.CurrentHostForegroundTurn()
	if err != nil || stillActive == nil || stillActive.HostTurnID != newForeground.HostTurnID {
		t.Fatalf("delayed old terminal cleared new foreground: active=%+v err=%v", stillActive, err)
	}
	auditAfter, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil || !bytes.Equal(auditRaw, auditAfter) {
		t.Fatalf("retirement audit replayed: before=%q after=%q err=%v", auditRaw, auditAfter, err)
	}
}

func TestHostGenerationReplacementRetiresForegroundAndAllowsNextTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID   = "brain-host:@foreground-generation"
		threadID = "thread-foreground-generation"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "generation-one"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	service := NewService(store, fw, nil)
	first, created, err := service.PrepareHostUserInput(
		hostID, "generation-one-input", "first pane", "brain-thread:"+threadID,
	)
	if err != nil || !created {
		t.Fatalf("first prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(first); err != nil {
		t.Fatal(err)
	}
	fw.ownedGenerations[hostID] = "generation-two"
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("generation replacement woke=%v err=%v", woke, err)
	}
	if active, err := store.CurrentHostForegroundTurn(); err != nil || active != nil {
		t.Fatalf("superseded generation remained active: active=%+v err=%v", active, err)
	}

	second, created, err := service.PrepareHostUserInput(
		hostID, "generation-two-input", "replacement pane", "brain-thread:"+threadID,
	)
	if err != nil || !created || second.HostGeneration != "generation-two" {
		t.Fatalf("second prepare created=%v admission=%+v err=%v", created, second, err)
	}
	if err := service.AdmitHostUserInput(second); err != nil {
		t.Fatal(err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID != second.HostTurnID ||
		active.HostGeneration != "generation-two" {
		t.Fatalf("replacement generation foreground=%+v err=%v", active, err)
	}
	auditRaw, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil || bytes.Count(bytes.TrimSpace(auditRaw), []byte{'\n'}) != 0 ||
		!bytes.Contains(auditRaw, []byte(`"reason":"host_generation_replaced"`)) {
		t.Fatalf("generation retirement audit=%q err=%v", auditRaw, err)
	}
}

func TestServiceSnapshotHasNoResultEventsChannel(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title:            "Expose the result",
		Objective:        "Cards live in the timeline, not the snapshot.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		ID: "event-01", WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:event:turn:one:session.done", Summary: "Done.",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := NewService(store, nil, nil).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "result_events") {
		t.Fatalf("snapshot must not carry result_events: %s", raw)
	}
	if strings.Contains(string(raw), `"active_work"`) ||
		!strings.Contains(string(raw), `"current_work"`) ||
		!strings.Contains(string(raw), `"work_backlog"`) {
		t.Fatalf("snapshot did not separate current relationships from durable backlog: %s", raw)
	}
	items, err := store.ThreadTimeline(snapshot.ChatThreadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("snapshot must not reproject audit events into timeline: %#v", items)
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

// missing_tmux with a bound provider session must native-resume (not blank),
// atomically persist the resume token, and audit resume_launched (tmux launch
// only — not provider acceptance).
func TestServiceSnapshotResumesProviderSessionWhenTmuxMissing(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected one resume launch, got %#v", fw.created)
	}
	command := fw.created[0].opts.Command
	token, present, err := work.ProviderResumeToken(work.AgentProviderCodex, command)
	if err != nil || !present || token != providerSessionID {
		t.Fatalf("resume command = %q token=(%q,%v,%v)", command, token, present, err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID == oldID || hostSession.ProviderSessionID != providerSessionID || hostSession.TranscriptPath != transcriptPath {
		t.Fatalf("atomic binding = %+v", hostSession)
	}
	audit, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("expected resume_launched audit, got %s", audit)
	}
}

func TestServiceSnapshotResumesCodexFromTranscriptPathOnly(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	derived := "019fd717-589c-7a11-9966-917f43dc336a"
	path := "/tmp/rollout-2026-08-06T20-40-59-" + derived + ".jsonl"
	if err := store.SetHostSession("dead:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript("", path, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ProviderSessionID != derived || host.TranscriptPath != path {
		t.Fatalf("path-derived binding = %+v", host)
	}
	token, present, err := work.ProviderResumeToken(work.AgentProviderCodex, fw.created[0].opts.Command)
	if err != nil || !present || token != derived {
		t.Fatalf("command=%q token=%q err=%v", fw.created[0].opts.Command, token, err)
	}
}

// Snapshot → ensureHostAgent never calls NewChat/SetChatState; chat_state bytes
// stay identical. thread_ids is cumulative NewChat history only.
func TestServiceSnapshotMissingTmuxResumePreservesChatThreadIdentity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current := "brain_1786013210655596422"
	historical := []string{"brain_1783911734080561361", "brain_1784200700958214019", current}
	if err := store.SetChatState(ChatState{ThreadID: current, ThreadIDs: historical}); err != nil {
		t.Fatal(err)
	}
	beforeRaw, err := os.ReadFile(store.ChatStatePath())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-1786013209881380707:@7750"
	providerSessionID := "019fd6ae-d6df-7341-bedc-706f7c4977bf"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/rollout-"+providerSessionID+".jsonl", "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ChatThreadID != current {
		t.Fatalf("thread rotated: %q", snapshot.ChatThreadID)
	}
	afterRaw, err := os.ReadFile(store.ChatStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeRaw, afterRaw) {
		t.Fatalf("chat_state mutated:\nbefore=%s\nafter=%s", beforeRaw, afterRaw)
	}
	if len(fw.killed) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("resume must not NewChat-kill or bootstrap: killed=%#v sent=%#v", fw.killed, fw.sentCalls)
	}
}

func TestServiceMissingTmuxFailClosedTable(t *testing.T) {
	tests := []struct {
		name       string
		executorID string
		command    string
		kind       string
		providerID string
		path       string
		createErr  error
		wantCreate bool
	}{
		{name: "unsupported", executorID: "custom", command: "my-custom-agent", kind: "custom", providerID: "custom-1"},
		{name: "opencode non-ses", executorID: "opencode", command: "opencode", kind: "opencode", providerID: "not-a-ses"},
		{name: "claude path-only", executorID: "claude", command: "claude", kind: "claude", path: "/tmp/claude.jsonl"},
		{name: "create fails", executorID: "codex", command: "codex", kind: "codex", providerID: "019fd717-589c-7a11-9966-917f43dc336a", createErr: fmt.Errorf("tmux unavailable")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			oldID := "brain-agent-brain-old:@1"
			if err := store.SetHostSession(oldID, tc.executorID); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostProviderTranscript(tc.providerID, tc.path, ""); err != nil {
				t.Fatal(err)
			}
			before, err := store.HostSession()
			if err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{createErr: tc.createErr}
			service := NewService(store, fw, &work.ExecutorConfig{
				ByName: map[string]work.Executor{
					tc.executorID: {Name: tc.executorID, Command: tc.command, Kind: tc.kind},
					"codex":       {Name: "codex", Command: "codex", Kind: "codex"},
				},
			})
			t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", tc.executorID)

			_, err = service.Snapshot()
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
			if tc.wantCreate != (len(fw.created) > 0) {
				t.Fatalf("created %#v", fw.created)
			}
			after, err := store.HostSession()
			if err != nil {
				t.Fatal(err)
			}
			if after.ID != before.ID || after.ProviderSessionID != before.ProviderSessionID || after.TranscriptPath != before.TranscriptPath {
				t.Fatalf("binding mutated: before=%+v after=%+v", before, after)
			}
			audit, _ := os.ReadFile(store.HostReplacementsPath())
			if !strings.Contains(string(audit), hostReplaceReasonMissingTmuxUnrecoverable) {
				t.Fatalf("audit=%s", audit)
			}
		})
	}
}

func TestServiceSnapshotDoesNotRebindUnrelatedHostAsContinuity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@292"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			"main:@0": {
				ID: "main:@0", Name: "Codex", Cwd: "/other",
				Command: "codex resume", State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions["main:@0"])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 || snapshot.HostAgent == nil || snapshot.HostAgent.ID == "main:@0" {
		t.Fatalf("created=%#v host=%#v", fw.created, snapshot.HostAgent)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) ||
		!strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("audit=%s", audit)
	}
}

func TestStoreReplaceHostSessionBindingAtomic(t *testing.T) {
	storeA, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := storeA.SetHostSession("old:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := storeA.SetHostProviderTranscript("old-token", "/tmp/old.jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	derived := "019fd717-589c-7a11-9966-917f43dc336a"
	path := "/tmp/rollout-2026-08-06T20-40-59-" + derived + ".jsonl"
	if err := storeA.ReplaceHostSessionBinding("new:@2", "codex", derived, path, "/home"); err != nil {
		t.Fatal(err)
	}
	got, err := storeA.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "new:@2" || got.ProviderSessionID != derived || got.TranscriptPath != path || got.ProviderDataRoot != "/home" {
		t.Fatalf("got %+v", got)
	}

	before, err := os.ReadFile(storeA.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	storeA.replaceHostBindingWrite = func(string, any) error {
		return fmt.Errorf("injected atomic rename failure")
	}
	if err := storeA.ReplaceHostSessionBinding("newer:@3", "codex", "other", path, "/home"); err == nil {
		t.Fatal("expected injected write failure")
	}
	afterBytes, err := os.ReadFile(storeA.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterBytes) {
		t.Fatalf("host file mutated on failed write:\nbefore=%s\nafter=%s", before, afterBytes)
	}
	after, err := storeA.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != "new:@2" || after.ProviderSessionID != derived {
		t.Fatalf("old binding lost after failed write: %+v", after)
	}

	// Concurrent sibling Store must not observe A's seam.
	storeB, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := storeB.ReplaceHostSessionBinding("b:@1", "codex", "b-token", "/tmp/b.jsonl", "/b"); err != nil {
		t.Fatal(err)
	}
	hostB, err := storeB.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostB.ID != "b:@1" || hostB.ProviderSessionID != "b-token" {
		t.Fatalf("store B should write normally: %+v", hostB)
	}
	stillA, err := os.ReadFile(storeA.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, stillA) {
		t.Fatalf("store A host file changed while B wrote")
	}
}

func TestServiceSnapshotResumeBindFailureKillsNewHostKeepsOld(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old:@1"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	store.replaceHostBindingWrite = func(string, any) error {
		return fmt.Errorf("injected binding write failure")
	}

	_, err = service.Snapshot()
	if err == nil {
		t.Fatal("expected binding failure")
	}
	if !strings.Contains(err.Error(), "injected binding write failure") {
		t.Fatalf("store error must remain primary: %v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected one CreateSession before bind fail, got %#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) != 1 || fw.killed[0] != newID {
		t.Fatalf("must kill exactly the new agentID %q, killed %#v", newID, fw.killed)
	}
	afterBytes, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterBytes) {
		t.Fatalf("old binding mutated:\nbefore=%s\nafter=%s", before, afterBytes)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("must not audit resume_launched after bind failure: %s", audit)
	}
}

// ProjectionSnapshot must never create/resume/rebind even when the recorded
// host is absent from tmux (continuity stays on intentional Snapshot paths).
func TestProjectionSnapshotAbsentHostDoesNotMutate(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-proj:@1"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{} // recorded host Absent
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	snapshot, err := service.ProjectionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != oldID {
		t.Fatalf("projection must keep recorded host: %#v", snapshot.HostAgent)
	}
	if len(fw.created) != 0 || len(fw.killed) != 0 || len(routes.transfers) != 0 {
		t.Fatalf("created=%#v killed=%#v transfers=%#v", fw.created, fw.killed, routes.transfers)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
}

// Unknown recorded-host ProbeSession must not enter missing_tmux create/kill/transfer.
func TestServiceSnapshotProbeUnknownPreservesBindingCreatesZero(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-probe-unknown:@1"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	beforeHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{probeErr: fmt.Errorf("injected tmux probe transport failure")}
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "liveness unknown") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("probe unknown must create zero sessions: %#v", fw.created)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("probe unknown must not kill: %#v", fw.killed)
	}
	if len(routes.transfers) != 0 {
		t.Fatalf("probe unknown must not transfer routes: %#v", routes.transfers)
	}
	afterHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHost, afterHost) {
		t.Fatalf("host/provider binding mutated:\nbefore=%s\nafter=%s", beforeHost, afterHost)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonMissingTmux) ||
		strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovery/spawn on probe unknown: %s", audit)
	}
}

// Candidate recover ProbeSession Unknown must not fall through to duplicate spawn.
func TestServiceSnapshotRecoverCandidateProbeUnknownCreatesZero(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	beforeHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{},
		agents: []*classifier.Agent{{
			ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
			Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
		}},
		probeErrByID: map[string]error{
			aliveID: fmt.Errorf("injected candidate probe failure"),
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})

	_, err = service.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "candidate liveness unknown") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("candidate probe unknown must not spawn: %#v", fw.created)
	}
	afterHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHost, afterHost) {
		t.Fatalf("binding mutated:\nbefore=%s\nafter=%s", beforeHost, afterHost)
	}
}

func TestServiceSnapshotRecoverLiveMigratesProviderBindingAtomically(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		path      string
		root      string
		wantToken string
		aliveCmd  string
	}{
		{
			name:      "token+path+root",
			token:     "019fd717-589c-7a11-9966-917f43dc336a",
			path:      "/home/daoleno/.codex/sessions/2026/08/06/rollout-019fd717-589c-7a11-9966-917f43dc336a.jsonl",
			root:      "/home/daoleno",
			wantToken: "019fd717-589c-7a11-9966-917f43dc336a",
			aliveCmd:  "codex resume 019fd717-589c-7a11-9966-917f43dc336a",
		},
		{
			name:      "codex path-only derives uuid",
			path:      "/tmp/rollout-2026-08-06T20-40-59-019fd717-589c-7a11-9966-917f43dc336a.jsonl",
			root:      "/home",
			wantToken: "019fd717-589c-7a11-9966-917f43dc336a",
			aliveCmd:  "codex resume 019fd717-589c-7a11-9966-917f43dc336a",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			deadID := "brain-agent-brain-dead:@1"
			aliveID := "brain-agent-brain-alive:@2"
			if err := store.SetHostSession(deadID, "codex"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostProviderTranscript(tc.token, tc.path, tc.root); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{
				sessions: map[string]*classifier.Agent{
					aliveID: {
						ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
						Command: tc.aliveCmd, State: classifier.StateRunning, Hidden: true,
					},
				},
			}
			fw.agents = append(fw.agents, fw.sessions[aliveID])
			service := NewService(store, fw, &work.ExecutorConfig{
				ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
			})

			snapshot, err := service.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(fw.created) != 0 || len(fw.killed) != 0 {
				t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
			}
			if snapshot.HostAgent == nil || snapshot.HostAgent.ID != aliveID {
				t.Fatalf("host=%#v", snapshot.HostAgent)
			}
			host, err := store.HostSession()
			if err != nil {
				t.Fatal(err)
			}
			if host.ID != aliveID || host.ProviderSessionID != tc.wantToken ||
				host.TranscriptPath != tc.path || host.ProviderDataRoot != tc.root {
				t.Fatalf("binding=%+v want id=%s token=%s path=%s root=%s",
					host, aliveID, tc.wantToken, tc.path, tc.root)
			}
			audit, _ := os.ReadFile(store.HostReplacementsPath())
			if !strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
				t.Fatalf("audit=%s", audit)
			}
		})
	}
}

func TestServiceSnapshotRecoverLiveBindFailureKeepsOldDoesNotKill(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID:      aliveID,
				Name:    "Brain (" + aliveID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "codex resume " + providerSessionID,
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	store.replaceHostBindingWrite = func(string, any) error {
		return fmt.Errorf("injected recover bind failure")
	}

	_, err = service.Snapshot()
	if err == nil {
		t.Fatal("expected recover bind failure")
	}
	if len(fw.killed) != 0 {
		t.Fatalf("recover-live bind failure must not kill recovered host: %#v", fw.killed)
	}
	afterBytes, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterBytes) {
		t.Fatalf("old binding mutated:\nbefore=%s\nafter=%s", before, afterBytes)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovered_alive on bind failure: %s", audit)
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
		"Keep lifecycle principles in Markdown, prompts, and agent instructions",
		"Treat a direct Work Event input as one claimed actionable delta",
		"Research discoverable environment facts with tools or delegated agents",
		"every currently independent required decision in one small numbered round with a recommended default",
		"remaining unknowns have safe defaults and completion is checkable",
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
	if pathExists(timelineMessagesPath(root)) {
		t.Fatalf("fresh Brain store created an empty timeline ledger")
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
	if !strings.Contains(string(instructions), "Use policies/ for stable Brain lifecycle rules") {
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
	if !strings.Contains(string(instructions), "Keep lifecycle principles in Markdown, prompts, and agent instructions") {
		t.Fatalf("workspace instructions do not describe prompt-first lifecycle:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Treat a direct Work Event input as one claimed actionable delta") {
		t.Fatalf("workspace instructions do not describe Work event handling:\n%s", instructions)
	}
	for _, want := range []string{
		"Research discoverable environment facts with tools or delegated agents",
		"every currently independent required decision in one small numbered round",
		"remaining unknowns have safe defaults",
		"checkable completion conditions",
	} {
		if !strings.Contains(string(instructions), want) {
			t.Fatalf("workspace instructions missing alignment contract %q:\n%s", want, instructions)
		}
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

func TestStoreContextAndHousekeepingDoNotCreateButFailClosedOnCorruptTimeline(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			root := t.TempDir()
			path := timelineMessagesPath(root)
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
			_, contextErr := service.Context()
			_, housekeepingErr := service.Housekeeping()

			if !existing {
				if contextErr != nil || housekeepingErr != nil {
					t.Fatalf("empty timeline context=%v housekeeping=%v", contextErr, housekeepingErr)
				}
				if pathExists(path) {
					t.Fatal("Brain context/housekeeping created an empty timeline ledger")
				}
				return
			}
			if contextErr == nil || housekeepingErr == nil ||
				!strings.Contains(contextErr.Error(), "decode timeline line 1") ||
				!strings.Contains(housekeepingErr.Error(), "decode timeline line 1") {
				t.Fatalf("corrupt timeline context=%v housekeeping=%v", contextErr, housekeepingErr)
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
				t.Fatalf("corrupt timeline changed: bytes=%q mode=%v mtime=%v", got, after.Mode(), after.ModTime())
			}
		})
	}
}

func timelineMessagesPath(root string) string {
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
		"## Brain Lifecycle Rules",
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
