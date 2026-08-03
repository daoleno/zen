package brain

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestOrchestrationSchemaV0MigratesDeterministically(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "orchestration.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"schema_version\":0}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	database, migrated, err := decodeOrchestrationDatabase(first)
	if err != nil {
		t.Fatal(err)
	}
	if migrated || database.SchemaVersion != orchestrationSchemaVersion {
		t.Fatalf("database = %#v, migrated=%v", database, migrated)
	}
	if database.BrainWork == nil || len(database.BrainWork) != 0 ||
		database.BrainWorkEvents == nil || len(database.BrainWorkEvents) != 0 {
		t.Fatalf("migrated tables = %#v / %#v", database.BrainWork, database.BrainWorkEvents)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("second open rewrote deterministic migration:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestDelegatedSessionMigrationIsOneWay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return fixed }

	migrated, err := store.MigrateDelegatedSessionsV1([]Work{{
		Title:            "Existing delegated session",
		Objective:        "Preserve existing durable execution ownership.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-existing:@1",
		CompletionPolicy: CompletionBounded,
		WaitFor:          "Session brain-agent-existing:@1",
	}})
	if err != nil || !migrated {
		t.Fatalf("first migration migrated=%v err=%v", migrated, err)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != legacySessionWorkID("brain-agent-existing:@1") {
		t.Fatalf("migrated Work = %#v", items)
	}

	migrated, err = store.MigrateDelegatedSessionsV1([]Work{{
		Title:            "Late legacy session",
		Objective:        "Must not enter through a permanent fallback.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-late:@2",
		CompletionPolicy: CompletionBounded,
	}})
	if err != nil || migrated {
		t.Fatalf("second migration migrated=%v err=%v", migrated, err)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err = reopened.MigrateDelegatedSessionsV1(nil)
	if err != nil || migrated {
		t.Fatalf("reopened migration migrated=%v err=%v", migrated, err)
	}
	items, err = reopened.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OwnerSessionID != "brain-agent-existing:@1" {
		t.Fatalf("one-way migration was replayed: %#v", items)
	}
}

func TestWorkEventDedupeAndClaimAreAtomic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Atomic event",
		Objective:        "Consume one external fact at most once.",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	var created atomic.Int32
	var appendErrors atomic.Int32
	var appendWG sync.WaitGroup
	for range 32 {
		appendWG.Add(1)
		go func() {
			defer appendWG.Done()
			_, wasCreated, appendErr := store.AppendWorkEvent(WorkEvent{
				WorkID:     item.ID,
				Kind:       "session.done",
				DedupeKey:  "session:worker:@1:done:42",
				Actionable: true,
			})
			if appendErr != nil {
				appendErrors.Add(1)
			}
			if wasCreated {
				created.Add(1)
			}
		}()
	}
	appendWG.Wait()
	if appendErrors.Load() != 0 || created.Load() != 1 {
		t.Fatalf("append errors=%d created=%d", appendErrors.Load(), created.Load())
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("deduplicated events = %#v", events)
	}

	var claimed atomic.Int32
	var claimErrors atomic.Int32
	var claimWG sync.WaitGroup
	for range 32 {
		claimWG.Add(1)
		go func() {
			defer claimWG.Done()
			_, ok, claimErr := store.ClaimNextActionableEvent()
			if claimErr != nil {
				claimErrors.Add(1)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	claimWG.Wait()
	if claimErrors.Load() != 0 || claimed.Load() != 1 {
		t.Fatalf("claim errors=%d claimed=%d", claimErrors.Load(), claimed.Load())
	}

	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.ClaimNextActionableEvent(); err != nil || ok {
		t.Fatalf("durable claim replayed after restart: ok=%v err=%v", ok, err)
	}
}

func TestActiveWorkProjectsMultipleItemsAndUnreadResults(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateWork(Work{
		Title:            "Work A",
		Objective:        "Keep A running.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-a:@1",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateWork(Work{
		Title:            "Work C",
		Objective:        "Start C independently.",
		Status:           WorkDone,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstAfterStartingSecond, err := store.Work(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstAfterStartingSecond != first {
		t.Fatalf("starting Work C mutated Work A:\nbefore=%#v\nafter=%#v", first, firstAfterStartingSecond)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     second.ID,
		Kind:       "session.done",
		DedupeKey:  "session:c:done",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	active, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("active Work = %#v", active)
	}
	byID := map[string]ActiveWork{}
	for _, item := range active {
		byID[item.ID] = item
	}
	if byID[first.ID].Status != WorkRunning || byID[first.ID].UnreadResult {
		t.Fatalf("Work A projection = %#v", byID[first.ID])
	}
	if byID[second.ID].Status != WorkDone || !byID[second.ID].UnreadResult {
		t.Fatalf("Work C projection = %#v", byID[second.ID])
	}
	if err := store.MarkWorkRead(second.ID); err != nil {
		t.Fatal(err)
	}
	active, err = store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("read terminal Work should leave Active projection: %#v", active)
	}
}

func TestOneSessionCannotOwnTwoActiveWorkRecords(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := "brain-agent-worker:@1"
	first, err := store.CreateWork(Work{
		Title:            "First",
		Objective:        "Keep one canonical Session owner.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Second",
		Objective:        "Must not duplicate active Session ownership.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	}); err == nil {
		t.Fatal("duplicate active Session ownership was accepted")
	}
	status := WorkDone
	if _, err := store.UpdateWork(first.ID, WorkUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Successor",
		Objective:        "Reuse the Session after prior Work is terminal.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatalf("terminal Work should release owner uniqueness: %v", err)
	}
}

func TestDispatchRequiresActionableEventEvenForUntilDoneWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(Work{
		Title:            "Invalid completion",
		Objective:        "Must name evidence.",
		CompletionPolicy: CompletionUntilDone,
	}); err == nil {
		t.Fatal("until_done Work without done_criteria_ref was accepted")
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Verified completion",
		Objective:        "Continue only when a real fact arrives.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef:  "worklog/verified.md#done",
		NextAction:       "Wait for evidence.",
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 20 {
		if woke, err := service.DispatchPendingEvent(); err != nil || woke {
			t.Fatalf("idle until_done Work woke: woke=%v err=%v", woke, err)
		}
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.progress",
		DedupeKey:  "progress:1",
		Actionable: false,
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("passive event woke: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 0 {
		t.Fatalf("idle scheduler sent %#v", fw.sentCalls)
	}

	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "done:1",
		PayloadRef: "session:worker:@1",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || !woke {
		t.Fatalf("actionable event did not wake: woke=%v err=%v", woke, err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("consumed event replayed: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("sends = %#v", fw.sentCalls)
	}
}

func TestDispatchReleasesClaimWhenHostSendFails(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
		sendErr: os.ErrDeadlineExceeded,
	}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Retry delivery",
		Objective:        "Do not lose a failed host send.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "external.changed",
		DedupeKey:  "external:send-failure",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	if woke, err := service.DispatchPendingEvent(); err == nil || woke {
		t.Fatalf("failed send woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt != nil || events[0].ConsumedAt != nil {
		t.Fatalf("failed send retained claim: %#v", events)
	}
	fw.sendErr = nil
	if woke, err := service.DispatchPendingEvent(); err != nil || !woke {
		t.Fatalf("released event was not retryable: woke=%v err=%v", woke, err)
	}
	events, err = store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].ConsumedAt == nil {
		t.Fatalf("successful retry was not consumed: %#v", events)
	}
}

func TestUserSteeringPreemptsUnclaimedWorkEvent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{
		Title:            "Background result",
		Objective:        "Preserve the result while the user is steering.",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "done:user-precedence",
		Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}

	if !service.NoteUserSteering(hostID) {
		t.Fatal("host user input was not recognized")
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("internal event preempted foreground: woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ClaimedAt != nil || events[0].ConsumedAt != nil {
		t.Fatalf("preempted event was not preserved unclaimed: %#v", events)
	}

	woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  hostID,
		NewState: string(classifier.StateDone),
		Agent:    &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
	})
	if err != nil || !woke {
		t.Fatalf("queued event did not run after foreground turn: woke=%v err=%v", woke, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("sends = %#v", fw.sentCalls)
	}
}

func TestConversationOnlyUserSteeringDoesNotCreateOrPauseWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	running, err := store.CreateWork(Work{
		Title:            "Work A",
		Objective:        "Continue in the background.",
		Status:           WorkRunning,
		OwnerSessionID:   "brain-agent-a:@2",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !service.NoteUserSteering(hostID) {
		t.Fatal("conversation-only Brain input was not recognized")
	}
	service.CancelUserSteering(hostID)
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0] != running {
		t.Fatalf("conversation-only input changed durable Work: %#v", items)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("conversation-only input created Events: %#v", events)
	}
}

func TestDelegatedSessionTransitionsDedupeToOneTurn(t *testing.T) {
	for _, state := range []classifier.AgentState{
		classifier.StateDone,
		classifier.StateFailed,
		classifier.StateBlocked,
	} {
		t.Run(string(state), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-agent-brain-hidden:@1"
			sessionID := "brain-agent-worker:@2"
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
			}}
			service := NewService(store, fw, nil)
			if _, err := store.CreateWork(Work{
				Title:            "Delegated change",
				Objective:        "Handle one terminal transition.",
				Status:           WorkRunning,
				OwnerSessionID:   sessionID,
				CompletionPolicy: CompletionBounded,
			}); err != nil {
				t.Fatal(err)
			}
			agent := &classifier.Agent{
				ID:        sessionID,
				Name:      "Worker",
				State:     state,
				Delegated: true,
				UpdatedAt: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
			}
			event := watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  sessionID,
				OldState: string(classifier.StateRunning),
				NewState: string(state),
				Agent:    agent,
			}
			first, err := service.RouteSessionEvent(event)
			if err != nil || !first {
				t.Fatalf("first transition woke=%v err=%v", first, err)
			}
			restartedProjection := event
			restartedAgent := *agent
			restartedAgent.UpdatedAt = agent.UpdatedAt.Add(time.Hour)
			restartedProjection.Agent = &restartedAgent
			restartedProjection.OldState = ""
			second, err := service.RouteSessionEvent(restartedProjection)
			if err != nil || second {
				t.Fatalf("duplicate transition woke=%v err=%v", second, err)
			}
			events, err := store.ListWorkEvents("")
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || len(fw.sentCalls) != 1 {
				t.Fatalf("events=%#v sends=%#v", events, fw.sentCalls)
			}
		})
	}
}

func TestDelegatedSessionDedupeAllowsANewLifecycleEpisode(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	if _, err := store.CreateWork(Work{
		Title:            "Lifecycle episodes",
		Objective:        "Dedupe repeated facts without suppressing a later blocker.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	route := func(oldState, newState classifier.AgentState, updated time.Time) bool {
		t.Helper()
		woke, routeErr := service.RouteSessionEvent(watcher.SessionEvent{
			Type:     "agent_state_change",
			AgentID:  sessionID,
			OldState: string(oldState),
			NewState: string(newState),
			Agent: &classifier.Agent{
				ID:        sessionID,
				State:     newState,
				Delegated: true,
				UpdatedAt: updated,
			},
		})
		if routeErr != nil {
			t.Fatal(routeErr)
		}
		return woke
	}
	if !route(classifier.StateRunning, classifier.StateBlocked, at) {
		t.Fatal("first blocker did not wake")
	}
	if route(classifier.StateBlocked, classifier.StateRunning, at.Add(time.Minute)) {
		t.Fatal("running progress unexpectedly woke")
	}
	if !route(classifier.StateRunning, classifier.StateBlocked, at.Add(2*time.Minute)) {
		t.Fatal("new blocker episode was over-deduplicated")
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || len(fw.sentCalls) != 2 {
		t.Fatalf("events=%#v sends=%#v", events, fw.sentCalls)
	}
}

func TestDelegatedSessionReconciliationWaitsForLeaseAndStalesOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	hostID := "brain-agent-brain-hidden:@1"
	sessionID := "brain-agent-worker:@2"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	service.now = func() time.Time { return now }
	if _, err := store.CreateWork(Work{
		Title:            "Leased work",
		Objective:        "Wait for a healthy lease.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
	}); err != nil {
		t.Fatal(err)
	}
	progressAt := now.Add(-time.Minute)
	nextCheck := now.Add(time.Minute)
	agent := &classifier.Agent{
		ID:                  sessionID,
		State:               classifier.StateRunning,
		Delegated:           true,
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &nextCheck,
		UpdatedAt:           progressAt,
	}

	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("healthy lease polled Brain: events=%#v sends=%#v", events, fw.sentCalls)
	}

	expired := now.Add(-time.Second)
	agent.ExpectedNextCheckAt = &expired
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	events, err = store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "session.stale" || len(fw.sentCalls) != 1 {
		t.Fatalf("stale reconciliation events=%#v sends=%#v", events, fw.sentCalls)
	}
}

func TestNonDelegatedSessionCannotBeClaimedByBrain(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	woke, err := service.RouteSessionEvent(watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  "user-session:@1",
		NewState: string(classifier.StateDone),
		Agent: &classifier.Agent{
			ID:        "user-session:@1",
			State:     classifier.StateDone,
			Delegated: false,
		},
	})
	if err != nil || woke {
		t.Fatalf("non-delegated Session routed: woke=%v err=%v", woke, err)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || len(events) != 0 {
		t.Fatalf("non-delegated Session created scheduler state: Work=%#v Events=%#v", items, events)
	}
}

func TestCalendarScheduledActionProjectsIdempotentlyWithoutOwningDelivery(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	scheduledFor := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	sourceThreadID := "brain-thread-immutable"
	item := calendar.Item{
		ID:                "calendar-item-1",
		Title:             "Morning report",
		Kind:              calendar.KindScheduledAction,
		ActionInstruction: "Generate the report.",
		SourceThreadID:    sourceThreadID,
		Runs: []calendar.Run{{
			ID:             "calendar-run-1",
			Title:          "Morning report",
			SourceThreadID: sourceThreadID,
			ScheduledFor:   scheduledFor,
			Status:         calendar.StatusRunning,
			AgentSession:   "brain-agent-calendar:@2",
		}},
	}

	if woke, err := service.RouteCalendarEvent(calendar.Event{Item: item}); err != nil || woke {
		t.Fatalf("launch projection woke=%v err=%v", woke, err)
	}
	sessionDone := watcher.SessionEvent{
		Type:     "agent_state_change",
		AgentID:  item.Runs[0].AgentSession,
		OldState: string(classifier.StateRunning),
		NewState: string(classifier.StateDone),
		Agent: &classifier.Agent{
			ID:        item.Runs[0].AgentSession,
			State:     classifier.StateDone,
			Delegated: true,
			UpdatedAt: scheduledFor.Add(30 * time.Second),
		},
	}
	if woke, err := service.RouteSessionEvent(sessionDone); err != nil || woke {
		t.Fatalf("Calendar-owned raw Session result woke=%v err=%v", woke, err)
	}
	item.Runs[0].Status = calendar.StatusCompleted
	finished := scheduledFor.Add(time.Minute)
	item.Runs[0].FinishedAt = &finished
	result := &calendar.ScheduledResult{
		ID:             "calendar-result-1",
		ThreadID:       sourceThreadID,
		Body:           "Canonical Calendar result",
		CreatedAt:      finished,
		Status:         calendar.StatusCompleted,
		Title:          item.Title,
		CalendarItemID: item.ID,
		CalendarRunID:  item.Runs[0].ID,
		ScheduledFor:   scheduledFor,
	}
	terminal := calendar.Event{Item: item, ScheduledResult: result}
	if woke, err := service.RouteCalendarEvent(terminal); err != nil || !woke {
		t.Fatalf("result projection woke=%v err=%v", woke, err)
	}
	if woke, err := service.RouteCalendarEvent(terminal); err != nil || woke {
		t.Fatalf("duplicate Calendar result woke=%v err=%v", woke, err)
	}

	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != WorkDone ||
		items[0].OwnerSessionID != "brain-agent-calendar:@2" {
		t.Fatalf("Calendar Work = %#v", items)
	}
	if len(events) != 3 || events[0].Kind != "calendar.launched" ||
		events[1].Kind != "session.done" || events[1].Actionable ||
		events[2].Kind != "calendar.result" || events[2].PayloadRef != result.ID {
		t.Fatalf("Calendar Events = %#v", events)
	}
	if terminal.Item.SourceThreadID != sourceThreadID ||
		terminal.Item.Runs[0].SourceThreadID != sourceThreadID ||
		terminal.ScheduledResult.ThreadID != sourceThreadID {
		t.Fatalf("Brain projection retargeted Calendar delivery: %#v", terminal)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("Calendar result turns = %#v", fw.sentCalls)
	}
}
