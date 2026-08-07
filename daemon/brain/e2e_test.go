package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// e2eStore builds the real store + service with a fake host watcher, the
// full durable pipeline: canonical ledger → derived Work → outbox event →
// claim → host delivery.
func e2eStore(t *testing.T) (*Store, *Service, *fakeWatcher, string, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	hostWatcher := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
	}
	service := NewService(store, hostWatcher, nil)
	sessionID := "brain-agent-e2e:@1"
	if _, err := store.CreateWork(Work{
		Title:            "E2E wake",
		Objective:        "Brain wakes exactly once on real completion.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	turnID := sessionID + ":turn:1"
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID:       sessionID,
		TurnID:          turnID,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "proc-1",
		PaneGeneration:  "pane-1",
		PayloadSHA256:   "payload",
		PanePID:         100,
		PaneStart:       1700000000000000000,
		ProcessID:       200,
		ProcessStart:    1700000000000000000,
	}); err != nil {
		t.Fatal(err)
	}
	return store, service, hostWatcher, sessionID, turnID
}

func e2eAdmission(t *testing.T, store *Store, sessionID, turnID string, at time.Time) watcher.TurnAdmission {
	t.Helper()
	admission := providerAdmission("stream", "msg-1", 1, "payload", at.Add(2*time.Second))
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class:     watcher.EvidenceReceipt,
		Kind:      "admission",
		SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload",
		Admission: admission,
		At:        at.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	return admission
}

// TestE2EWakeExactlyOnceOnRealCompletion proves Brain is automatically woken
// for a real bound completion exactly once across the whole pipeline and a
// daemon restart, and is never woken by live-turn quiet/prompt frames.
func TestE2EWakeExactlyOnceOnRealCompletion(t *testing.T) {
	store, service, hostWatcher, sessionID, turnID := e2eStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	admission := e2eAdmission(t, store, sessionID, turnID, at)

	// Live-turn quiet/prompt frames (pane evidence) never wake.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidencePane, Kind: "running",
		SourceID: "pane\x00quiet-frame",
		At:       at.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("quiet frame woke Brain: woke=%v err=%v", woke, err)
	}

	// Provider running: still no terminal wake.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  at.Add(4 * time.Second),
		At:         at.Add(5 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("running fact woke Brain: woke=%v err=%v", woke, err)
	}

	// Real completion: the routed state change wakes Brain exactly once.
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID, TurnID: turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       "done",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  at.Add(4 * time.Second),
		SettledAt:  at.Add(9 * time.Second),
		At:         at.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	agent := &classifier.Agent{
		ID: sessionID, Name: "E2E", State: classifier.StateDone,
		Delegated: true, PaneAlive: true,
	}
	woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type: "agent_state_change", AgentID: sessionID, Agent: agent,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: turnID,
	})
	if err != nil || !woke {
		t.Fatalf("completion wake = %v err=%v", woke, err)
	}
	if len(hostWatcher.sentCalls) != 1 {
		t.Fatalf("host deliveries = %d, want exactly one", len(hostWatcher.sentCalls))
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkWaiting || workItem.NextAction == "" {
		t.Fatalf("Work after completion = %v", workItem)
	}

	// Restart: replaying the same durable facts cannot duplicate the wake.
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restartedWatcher := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			"brain-agent-brain-hidden:@1": {ID: "brain-agent-brain-hidden:@1", Hidden: true, State: classifier.StateDone},
		},
	}
	restartedService := NewService(restarted, restartedWatcher, nil)
	restartedService.ReconcileDelegatedSessions(nil)
	if len(restartedWatcher.sentCalls) != 0 {
		t.Fatalf("restart re-delivered the consumed wake: %#v", restartedWatcher.sentCalls)
	}
}

// TestE2EAttentionAndUncertainWakes covers blocked/user-input and the
// session.uncertain wake path through the same pipeline.
func TestE2EAttentionAndUncertainWakes(t *testing.T) {
	t.Run("attention user_input wakes needs_input", func(t *testing.T) {
		store, service, hostWatcher, sessionID, turnID := e2eStore(t)
		at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
		e2eAdmission(t, store, sessionID, turnID, at)
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceControl, Kind: "attention",
			SourceID: "control\x00progress-event-1",
			At:       at.Add(3 * time.Second),
			Summary:  "Awaiting user decision",
		}); err != nil {
			t.Fatal(err)
		}
		agent := &classifier.Agent{
			ID: sessionID, Name: "E2E", State: classifier.StateBlocked,
			Delegated: true, PaneAlive: true, Attention: "user_input",
		}
		woke, err := service.RouteSessionEvent(watcher.SessionEvent{
			Type: "agent_state_change", AgentID: sessionID, Agent: agent,
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateBlocked),
			TurnID: turnID,
		})
		if err != nil || !woke {
			t.Fatalf("attention wake = %v err=%v", woke, err)
		}
		if len(hostWatcher.sentCalls) != 1 {
			t.Fatalf("attention deliveries = %d, want one", len(hostWatcher.sentCalls))
		}
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		if workItem.Status != WorkNeedsInput {
			t.Fatalf("Work after attention = %v", workItem)
		}
	})
	t.Run("end-of-identity wakes uncertain", func(t *testing.T) {
		store, service, hostWatcher, sessionID, turnID := e2eStore(t)
		at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
		e2eAdmission(t, store, sessionID, turnID, at)
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID,
			Class: watcher.EvidenceLiveness, Kind: "uncertain",
			ProcessDead: true,
			SourceID:    "liveness\x00process-dead",
			SettledAt:   at.Add(20 * time.Second),
			At:          at.Add(21 * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
		agent := &classifier.Agent{
			ID: sessionID, Name: "E2E", State: classifier.StateUnknown,
			Delegated: true, PaneAlive: false,
		}
		woke, err := service.RouteSessionEvent(watcher.SessionEvent{
			Type: "agent_state_change", AgentID: sessionID, Agent: agent,
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateUnknown),
			TurnID: turnID,
		})
		if err != nil || !woke {
			t.Fatalf("uncertain wake = %v err=%v", woke, err)
		}
		if len(hostWatcher.sentCalls) != 1 {
			t.Fatalf("uncertain deliveries = %d, want one", len(hostWatcher.sentCalls))
		}
		workItem, _, _ := store.WorkByOwnerSession(sessionID)
		if workItem.Status != WorkNeedsInput ||
			!strings.Contains(workItem.NextAction, "Confirm whether the delegated Session received the prompt") {
			t.Fatalf("Work after uncertain = %v", workItem)
		}
	})
}

// TestE2EMissingHeartbeatWakesStale covers the lease-expiry wake through
// ReconcileDelegatedSessions (the heartbeat path).
func TestE2EMissingHeartbeatWakesStale(t *testing.T) {
	store, service, hostWatcher, sessionID, turnID := e2eStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	e2eAdmission(t, store, sessionID, turnID, at)
	now := at.Add(time.Hour)
	store.now = func() time.Time { return now }
	agent := &classifier.Agent{
		ID:                  sessionID,
		Name:                "E2E",
		State:               classifier.StateRunning,
		Delegated:           true,
		PaneAlive:           true,
		ProcessID:           42,
		LastProgressAt:      &at,
		ExpectedNextCheckAt: &at,
	}
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	if len(hostWatcher.sentCalls) != 1 {
		t.Fatalf("stale deliveries = %d, want one", len(hostWatcher.sentCalls))
	}
	// A second reconcile with the same expiry does not duplicate the wake.
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	if len(hostWatcher.sentCalls) != 1 {
		t.Fatalf("stale duplicate deliveries = %d, want one", len(hostWatcher.sentCalls))
	}
}

// TestE2ERestartAbsentCanonicalSessionWakesUncertain covers P1.3: after a
// daemon restart, a canonical Session absent from the inventory produces
// exactly one actionable uncertain wake instead of silently continuing.
func TestE2ERestartAbsentCanonicalSessionWakesUncertain(t *testing.T) {
	store, service, hostWatcher, sessionID, turnID := e2eStore(t)
	at := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	e2eAdmission(t, store, sessionID, turnID, at)

	// Daemon restart: the inventory contains no agents at all.
	service.ReconcileDelegatedSessions(nil)
	if len(hostWatcher.sentCalls) != 1 {
		t.Fatalf("restart-absent deliveries = %d, want exactly one", len(hostWatcher.sentCalls))
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	if workItem.Status != WorkNeedsInput ||
		!strings.Contains(workItem.NextAction, "Confirm whether the delegated Session received the prompt") {
		t.Fatalf("Work after restart-absent wake = %v", workItem)
	}
	// Reconcile keeps running every heartbeat: no duplicate wake.
	service.ReconcileDelegatedSessions(nil)
	if len(hostWatcher.sentCalls) != 1 {
		t.Fatalf("restart-absent duplicate deliveries = %d, want one", len(hostWatcher.sentCalls))
	}
	snapshot, hasTurn, _ := store.Turn(sessionID)
	if !hasTurn || snapshot.Status != watcher.TurnUnknown {
		t.Fatalf("restart-absent turn = %+v hasTurn=%v, want Unknown", snapshot, hasTurn)
	}
}
