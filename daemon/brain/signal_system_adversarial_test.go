package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func deliverAdversarialHostEvent(t *testing.T, store *Store, hostID string) (WorkEvent, Work) {
	t.Helper()
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if claimed.HandlingID == "" || claimed.ProviderTurnID == "" || claimed.HandlingID == claimed.ProviderTurnID {
		t.Fatalf("claim did not separate handling and provider turn identities: %+v", claimed)
	}
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID: hostID, TurnID: claimed.ProviderTurnID, Receipt: claimed.ID,
		AcceptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("admit Host provider turn: %v", err)
	}
	delivered, item, err := store.ConsumeClaimedWorkEvent(claimed.ID, hostID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatalf("consume claimed event: %v", err)
	}
	return delivered, item
}

func TestSignalAdversarialSchemasTwoThroughSixMigrateBoundedlyWithoutReplay(t *testing.T) {
	for schema := 2; schema <= 6; schema++ {
		t.Run(fmt.Sprintf("schema-%d", schema), func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
			document := map[string]any{
				"schema_version": schema,
				"migrations":     map[string]any{},
				"brain_work": []any{map[string]any{
					"work_id": "legacy-work", "title": "Legacy Work", "objective": "Reconcile once.",
					"status": "waiting", "completion_policy": "bounded", "created_at": at, "updated_at": at,
				}},
				"brain_work_events": []any{map[string]any{
					"event_id": "historical-delivery", "work_id": "legacy-work", "kind": "legacy.result",
					"dedupe_key": "legacy:result", "actionable": true, "created_at": at,
					"claimed_at": at, "delivery_host_session_id": "old-host", "consumed_at": at,
				}},
			}
			if schema >= 3 {
				document["brain_turns"] = []any{}
			}
			if schema >= 6 {
				document["brain_turn_submissions"] = []any{}
			}
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			complete, processed, err := store.MigrateSignalSystemV1(1)
			if err != nil || complete || processed != 1 {
				t.Fatalf("first bounded batch complete=%v processed=%d err=%v", complete, processed, err)
			}
			complete, processed, err = store.MigrateSignalSystemV1(1)
			if err != nil || !complete || processed != 0 {
				t.Fatalf("completion batch complete=%v processed=%d err=%v", complete, processed, err)
			}
			claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
			if err != nil || !ok || claimed.ID == "historical-delivery" || claimed.Kind != "brain.reconcile_required" {
				t.Fatalf("migration replayed historical bytes: claimed=%+v ok=%v err=%v", claimed, ok, err)
			}
			projected := activeWorkByID(t, store, "legacy-work")
			if projected.ProgressMode != WorkProgressReady {
				t.Fatalf("migrated Work mode=%q projection=%+v", projected.ProgressMode, projected)
			}
			restarted, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := restarted.ListWorkEvents("legacy-work")
			if countUnhandledEventKind(events, "brain.reconcile_required") != 1 {
				t.Fatalf("restart duplicated migration attention: %+v", events)
			}
		})
	}
}

func resolveAdversarialEvent(
	t *testing.T,
	store *Store,
	event WorkEvent,
	disposition WorkDisposition,
	wake *WorkWake,
	successor string,
) (WorkEvent, Work) {
	t.Helper()
	resolved, item, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID:              event.ID,
		HandlingID:           event.HandlingID,
		ProviderTurnID:       event.ProviderTurnID,
		ExpectedWorkRevision: event.DeliveryWorkRevision,
		Disposition:          disposition,
		SuccessorSessionID:   successor,
		Wake:                 wake,
	})
	if err != nil {
		t.Fatalf("resolve event: %v", err)
	}
	return resolved, item
}

func activeWorkByID(t *testing.T, store *Store, workID string) ActiveWork {
	t.Helper()
	items, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == workID {
			return item
		}
	}
	t.Fatalf("active Work %s not found in %+v", workID, items)
	return ActiveWork{}
}

// The Watcher emits changed output before the authoritative state transition.
// Only the latter, bound to the exact accepted provider Turn, may end A. A
// duplicate snapshot must not end the newly delivered B handling.
func TestSignalAdversarialWatcherOutputThenStateChangeClosesOnlyExactHandling(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	a := createSignalTestWork(t, store, "Watcher A", "brain-agent-a:@1")
	b := createSignalTestWork(t, store, "Watcher B", "brain-agent-b:@1")
	appendSignalTestEvent(t, store, a, "watcher-a")
	appendSignalTestEvent(t, store, b, "watcher-b")
	deliveredA, _ := deliverAdversarialHostEvent(t, store, hostID)
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{hostID: host}}
	service := NewService(store, fw, nil)

	if woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "agent_output", AgentID: hostID, Agent: host,
	}); err != nil || woke {
		t.Fatalf("output snapshot closed a handling: woke=%v err=%v", woke, err)
	}
	row, _, _ := store.WorkEvent(deliveredA.ID)
	if row.HandlingEndedAt != nil {
		t.Fatalf("output snapshot ended A: %+v", row)
	}

	terminal := watcher.SessionEvent{
		Type: "agent_state_change", AgentID: hostID, Agent: host,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: deliveredA.ProviderTurnID,
	}
	if _, err := service.ObserveHostSessionEvent(terminal); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	var deliveredB WorkEvent
	for _, event := range events {
		if event.WorkID == b.ID && event.DeliveredAt != nil && event.HandlingEndedAt == nil {
			deliveredB = event
		}
	}
	if deliveredB.ID == "" {
		t.Fatalf("B was not delivered after exact A terminal: %+v", events)
	}
	if _, err := service.ObserveHostSessionEvent(terminal); err != nil {
		t.Fatal(err)
	}
	row, _, _ = store.WorkEvent(deliveredB.ID)
	if row.HandlingEndedAt != nil {
		t.Fatalf("duplicate A terminal ended B: %+v", row)
	}
}

func TestSignalAdversarialOnlyOneDeliveredHostHandlingGlobally(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := createSignalTestWork(t, store, "Global A", "brain-agent-a:@1")
	b := createSignalTestWork(t, store, "Global B", "brain-agent-b:@1")
	appendSignalTestEvent(t, store, a, "global-a")
	appendSignalTestEvent(t, store, b, "global-b")
	deliverAdversarialHostEvent(t, store, "brain-agent-brain-hidden:@1")
	if event, claimed, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1"); err != nil || claimed {
		t.Fatalf("second Work entered Host admission window: event=%+v claimed=%v err=%v", event, claimed, err)
	}
}

func TestSignalAdversarialStartupRecoversOldHostDeliveryAfterBindingReplacement(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	oldHost := "brain-agent-brain-hidden:@1"
	newHost := "brain-agent-brain-hidden:@2"
	item := createSignalTestWork(t, store, "Old Host delivery", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "old-host")
	delivered, _ := deliverAdversarialHostEvent(t, store, oldHost)
	if err := store.SetHostSession(newHost, "codex"); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		newHost: {ID: newHost, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(restarted, fw, nil)
	complete, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8)
	if err != nil || !complete {
		t.Fatalf("startup complete=%v err=%v", complete, err)
	}
	row, _, err := restarted.WorkEvent(delivered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.DeliveredAt == nil || row.HandlingEndedAt == nil || row.HandledAt != nil {
		t.Fatalf("old Host delivery was stranded or replayed: %+v", row)
	}
	events, _ := restarted.ListWorkEvents(item.ID)
	if !containsUnhandledEventKind(events, "brain.reconcile_required") {
		t.Fatalf("old Host Work has no durable reconcile attention: %+v", events)
	}
}

func TestSignalAdversarialWrongReorderedAndDuplicateProviderTurnsAreIgnored(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	item := createSignalTestWork(t, store, "Provider turn CAS", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "provider-cas")
	delivered, _ := deliverAdversarialHostEvent(t, store, hostID)
	if delivered.HandlingID == delivered.ProviderTurnID {
		t.Fatalf("random handling token aliased provider Turn: %+v", delivered)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, "wrong-provider-turn",
	); err != nil || created {
		t.Fatalf("wrong provider Turn changed handling: created=%v err=%v", created, err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, delivered.ProviderTurnID,
	); err != nil || !created {
		t.Fatalf("exact provider Turn did not requeue: created=%v err=%v", created, err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, delivered.ProviderTurnID,
	); err != nil || created {
		t.Fatalf("duplicate provider Turn was not idempotent: created=%v err=%v", created, err)
	}
}

func TestSignalAdversarialProgressModeIsExactlyOneAcrossReadyWaitWakeAndContinue(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	owner := "brain-agent-progress:@1"
	item := createSignalTestWork(t, store, "Progress modes", owner)
	turnID := owner + ":turn:1"
	if err := store.AdmitTurn(watcher.AdmittedTurn{SessionID: owner, TurnID: turnID, AcceptedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, item, "progress-ready")
	delivered, current := deliverAdversarialHostEvent(t, store, hostID)
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressReady {
		t.Fatalf("delivered attention mode=%q projection=%+v", projected.ProgressMode, projected)
	}
	wake := &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + current.SourceThreadID}
	_, waiting := resolveAdversarialEvent(t, store, delivered, WorkDispositionWait, wake, "")
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressWaiting || projected.AttentionPending {
		t.Fatalf("wait mode projection=%+v Work=%+v", projected, waiting)
	}
	if _, err := store.WakeWaitingWork(*wake, "user.input", "input-1", "continue"); err != nil {
		t.Fatal(err)
	}
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressReady || !projected.AttentionPending {
		t.Fatalf("wake mode projection=%+v", projected)
	}
	next, _ := deliverAdversarialHostEvent(t, store, hostID)
	_, owned := resolveAdversarialEvent(t, store, next, WorkDispositionContinue, nil, owner)
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressOwned || projected.AttentionPending || projected.Wake != nil {
		t.Fatalf("continue mode projection=%+v Work=%+v", projected, owned)
	}
}

func TestSignalAdversarialTypedWaitsRequireCanonicalExactProducers(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	producerSession := "brain-agent-producer:@1"
	producer := createSignalTestWork(t, store, "Session producer", producerSession)
	producerTurn := producerSession + ":turn:1"
	acceptedAt := time.Now().UTC().Add(-time.Second)
	if err := store.AdmitTurn(watcher.AdmittedTurn{SessionID: producerSession, TurnID: producerTurn, AcceptedAt: acceptedAt}); err != nil {
		t.Fatal(err)
	}
	calendarProducer, err := store.CreateWork(Work{
		ID:    calendarWorkID("item-1", "run-1"),
		Title: "Calendar producer", Objective: "Produce one exact occurrence.",
		Status: WorkRunning, CompletionPolicy: CompletionBounded,
		ContextRef: "calendar:item-1:run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = calendarProducer

	for _, test := range []struct {
		name string
		wake WorkWake
		ok   bool
	}{
		{name: "session exact", wake: WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(producerSession, producerTurn)}, ok: true},
		{name: "session missing", wake: WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef("missing", "turn")}},
		{name: "calendar exact", wake: WorkWake{Kind: WorkWakeCalendarResult, Ref: "calendar:item-1:run-1"}, ok: true},
		{name: "calendar missing", wake: WorkWake{Kind: WorkWakeCalendarResult, Ref: "calendar:item-2:run-9"}},
		{name: "user exact", wake: WorkWake{Kind: WorkWakeUserInput}, ok: true},
		{name: "user cross thread", wake: WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:not-the-work-thread"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			consumer, err := store.CreateWork(Work{
				Title: "Consumer " + test.name, Objective: "Wait on one canonical producer.",
				Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			appendSignalTestEvent(t, store, consumer, "wait-"+strings.ReplaceAll(test.name, " ", "-"))
			delivered, current := deliverAdversarialHostEvent(t, store, hostID)
			wake := test.wake
			if wake.Kind == WorkWakeUserInput && wake.Ref == "" {
				wake.Ref = "brain-thread:" + current.SourceThreadID
			}
			_, _, err = store.ResolveWorkEvent(WorkEventDispositionRequest{
				EventID: delivered.ID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionWait, Wake: &wake,
			})
			if test.ok && err != nil {
				t.Fatalf("exact producer rejected: %v", err)
			}
			if !test.ok && err == nil {
				t.Fatalf("invalid producer accepted: %+v", wake)
			}
			if !test.ok {
				if _, _, endErr := store.RequeueUnhandledHostAttention(delivered.ID, delivered.HandlingID, delivered.ProviderTurnID); endErr != nil {
					t.Fatal(endErr)
				}
				reconcile, _ := deliverAdversarialHostEvent(t, store, hostID)
				resolveAdversarialEvent(t, store, reconcile, WorkDispositionComplete, nil, "")
			}
		})
	}

	consumer, err := store.CreateWork(Work{
		Title: "Cross-Work Session consumer", Objective: "Wake when the producer Turn settles.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, consumer, "cross-work-session")
	delivered, _ := deliverAdversarialHostEvent(t, store, hostID)
	sessionWake := &WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(producerSession, producerTurn)}
	resolveAdversarialEvent(t, store, delivered, WorkDispositionWait, sessionWake, "")
	fact := watcher.TurnFact{
		SessionID: producerSession, TurnID: producerTurn,
		Class: watcher.EvidenceProvider, Kind: "done", SourceID: "provider-session-producer-done",
		ActivityID: "activity-producer", StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(2 * time.Second), At: acceptedAt.Add(2 * time.Second),
	}
	if _, _, err := store.ApplyTurnFact(fact); err != nil {
		t.Fatal(err)
	}
	if projected := activeWorkByID(t, store, consumer.ID); projected.ProgressMode != WorkProgressReady || projected.Wake != nil {
		t.Fatalf("cross-Work terminal did not wake consumer: %+v producer=%+v", projected, producer)
	}
	if _, _, err := store.ApplyTurnFact(fact); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListWorkEvents(consumer.ID)
	if countUnhandledEventKind(events, "session.done") != 1 {
		t.Fatalf("duplicate producer occurrence woke more than once: %+v", events)
	}
	for {
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if err := store.AdmitTurn(watcher.AdmittedTurn{
			SessionID: hostID, TurnID: claimed.ProviderTurnID, Receipt: claimed.ID, AcceptedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		ready, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, hostID, claimed.ProviderTurnID)
		if err != nil {
			t.Fatal(err)
		}
		resolveAdversarialEvent(t, store, ready, WorkDispositionComplete, nil, "")
	}

	calendarConsumer, err := store.CreateWork(Work{
		Title: "Cross-Work Calendar consumer", Objective: "Wake on the exact Calendar occurrence.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, calendarConsumer, "cross-work-calendar")
	calendarHandling, _ := deliverAdversarialHostEvent(t, store, hostID)
	calendarWake := &WorkWake{Kind: WorkWakeCalendarResult, Ref: calendarProducer.ContextRef}
	resolveAdversarialEvent(t, store, calendarHandling, WorkDispositionWait, calendarWake, "")
	finished := time.Now().UTC()
	calendarEvent := calendar.Event{Item: calendar.Item{
		ID: "item-1", Title: "Calendar producer", Kind: calendar.KindScheduledAction,
		ActionInstruction: "Produce one exact occurrence.",
		Runs: []calendar.Run{{
			ID: "run-1", Title: "Calendar producer", Status: calendar.StatusCompleted,
			FinishedAt: &finished,
		}},
	}}
	calendarService := NewService(store, &fakeWatcher{}, nil)
	writes := 0
	write := store.writeOrchestration
	store.writeOrchestration = func(path string, value any) error {
		writes++
		return write(path, value)
	}
	if woke, err := calendarService.RouteCalendarEvent(calendarEvent); err != nil || !woke {
		t.Fatalf("Calendar producer wake=%v err=%v", woke, err)
	}
	if writes != 1 {
		t.Fatalf("Calendar producer transition and consumer attention used %d persistence writes, want 1", writes)
	}
	store.writeOrchestration = write
	if projected := activeWorkByID(t, store, calendarConsumer.ID); projected.ProgressMode != WorkProgressReady || projected.Wake != nil {
		t.Fatalf("cross-Work Calendar completion did not wake consumer: %+v", projected)
	}
	if woke, err := calendarService.RouteCalendarEvent(calendarEvent); err != nil || woke {
		t.Fatalf("duplicate Calendar occurrence wake=%v err=%v", woke, err)
	}
	calendarEvents, _ := store.ListWorkEvents(calendarConsumer.ID)
	if countUnhandledEventKind(calendarEvents, "calendar.result") != 1 {
		t.Fatalf("duplicate Calendar occurrence woke more than once: %+v", calendarEvents)
	}
}

func TestSignalAdversarialSuccessorReservationSurvivesRequeueRestartAndOwnsFinalization(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	incumbent := "brain-agent-incumbent:@1"
	s1 := "brain-agent-successor:@2"
	s2 := "brain-agent-successor:@3"
	item := createSignalTestWork(t, store, "Exclusive successor", incumbent)
	appendSignalTestEvent(t, store, item, "reserve-s1")
	delivered, _ := deliverAdversarialHostEvent(t, store, hostID)
	if _, err := store.AttachWorkOwner(item.ID, s1); err != nil {
		t.Fatal(err)
	}
	s1Turn := s1 + ":turn:1"
	if err := store.AdmitTurn(watcher.AdmittedTurn{SessionID: s1, TurnID: s1Turn, AcceptedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(delivered.ID, delivered.HandlingID, delivered.ProviderTurnID); err != nil || !created {
		t.Fatalf("requeue created=%v err=%v", created, err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Work(item.ID)
	if err != nil || current.SuccessorReservation == nil || current.SuccessorReservation.SessionID != s1 || current.SuccessorReservation.ProviderTurnID != s1Turn {
		t.Fatalf("S1 reservation did not survive restart: Work=%+v err=%v", current, err)
	}
	if _, err := restarted.AttachWorkOwner(item.ID, s2); !errors.Is(err, ErrWorkOwnerConflict) {
		t.Fatalf("S2 replaced admitted S1: err=%v", err)
	}
	reconcile, _ := deliverAdversarialHostEvent(t, restarted, hostID)
	_, terminal := resolveAdversarialEvent(t, restarted, reconcile, WorkDispositionCancel, nil, "")
	foundS1 := false
	for _, finalization := range terminal.SessionFinalizations {
		if finalization.SessionID == s1 {
			foundS1 = true
		}
	}
	if !foundS1 {
		t.Fatalf("accepted S1 has no terminal finalization owner: %+v", terminal)
	}
}

func TestSignalAdversarialSuccessorReleaseRequiresProvedNonAdmission(t *testing.T) {
	for _, test := range []struct {
		name    string
		proved  bool
		cleared bool
	}{
		{name: "ambiguous input retains reservation", proved: false, cleared: false},
		{name: "proved non-admission releases reservation", proved: true, cleared: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item := createSignalTestWork(t, store, "Successor failure", "brain-agent-incumbent:@1")
			appendSignalTestEvent(t, store, item, "successor-failure")
			handling, _ := deliverAdversarialHostEvent(t, store, "brain-agent-brain-hidden:@1")
			s1 := "brain-agent-successor:@1"
			if _, err := store.AttachWorkOwner(item.ID, s1); err != nil {
				t.Fatal(err)
			}
			current, err := store.RecordSuccessorLaunchFailure(item.ID, s1, "provider submission failed", test.proved)
			if err != nil {
				t.Fatal(err)
			}
			if (current.SuccessorReservation == nil) != test.cleared {
				t.Fatalf("reservation after failure=%+v cleared=%v", current.SuccessorReservation, test.cleared)
			}
			_, secondErr := store.AttachWorkOwner(item.ID, "brain-agent-successor:@2")
			if test.cleared && secondErr != nil {
				t.Fatalf("proved release did not allow a new reservation: %v", secondErr)
			}
			if !test.cleared && !errors.Is(secondErr, ErrWorkOwnerConflict) {
				t.Fatalf("ambiguous reservation allowed replacement: %v", secondErr)
			}
			if _, _, err := store.RequeueUnhandledHostAttention(handling.ID, handling.HandlingID, handling.ProviderTurnID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSignalAdversarialHeldClaimResolutionAndReconcileUseOneWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		apply  func(*Store, string) error
		result string
	}{
		{name: "mark delivered", result: EventResolutionMarkDelivered, apply: func(store *Store, eventID string) error {
			return store.MarkDeliveredClaim(eventID, "user", "visible in Host transcript")
		}},
		{name: "discard", result: EventResolutionDiscard, apply: func(store *Store, eventID string) error {
			return store.DiscardClaim(eventID, "user", "obsolete delivery")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _, event := claimResolutionStore(t)
			writes := 0
			write := store.writeOrchestration
			store.writeOrchestration = func(path string, value any) error {
				writes++
				if writes > 1 {
					return fmt.Errorf("unexpected split persistence")
				}
				return write(path, value)
			}
			if err := test.apply(store, event.ID); err != nil {
				t.Fatalf("claim resolution was not one write: %v", err)
			}
			if writes != 1 {
				t.Fatalf("persistence writes=%d want 1", writes)
			}
			reopened, err := NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := reopened.ListWorkEvents(event.WorkID)
			if len(events) != 2 || events[0].Resolution != test.result || !containsUnhandledEventKind(events, "brain.reconcile_required") {
				t.Fatalf("reopened after-state is incomplete: %+v", events)
			}
		})

		t.Run(test.name+" write failure", func(t *testing.T) {
			store, _, event := claimResolutionStore(t)
			store.writeOrchestration = func(string, any) error { return fmt.Errorf("injected write failure") }
			if err := test.apply(store, event.ID); err == nil {
				t.Fatal("injected persistence failure was ignored")
			}
			reopened, err := NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := reopened.ListWorkEvents(event.WorkID)
			if len(events) != 1 || events[0].Resolution != "" || events[0].DiscardedAt != nil || events[0].DeliveredAt != nil {
				t.Fatalf("write failure exposed a partial after-state: %+v", events)
			}
		})
	}
}

func containsUnhandledEventKind(events []WorkEvent, kind string) bool {
	return countUnhandledEventKind(events, kind) != 0
}

func countUnhandledEventKind(events []WorkEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.HandledAt == nil && event.DiscardedAt == nil {
			count++
		}
	}
	return count
}
