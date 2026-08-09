package brain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func createSignalTestWork(t *testing.T, store *Store, title, owner string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{
		Title:            title,
		Objective:        "Exercise the durable Brain signal protocol.",
		Status:           WorkRunning,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func appendSignalTestEvent(t *testing.T, store *Store, item Work, suffix string) WorkEvent {
	t.Helper()
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:" + item.OwnerSessionID + ":turn:" + suffix + ":session.done",
		PayloadRef: "session:" + item.OwnerSessionID,
		SourceName: item.OwnerSessionID,
		Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append event created=%v err=%v", created, err)
	}
	return event
}

func deliverSignalTestEvent(t *testing.T, store *Store, hostID string) (WorkEvent, Work) {
	t.Helper()
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	delivered, item, err := store.ConsumeClaimedWorkEvent(claimed.ID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	return delivered, item
}

func TestSignalDeliveryAndHandlingAreSeparateRevisionCheckedTransitions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Delivery boundary", "brain-agent-delivery:@1")
	appendSignalTestEvent(t, store, item, "delivery-1")
	delivered, current := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if delivered.DeliveredAt == nil || delivered.HandledAt != nil || delivered.HandlingID == "" {
		t.Fatalf("delivery incorrectly implied handling: %+v", delivered)
	}
	if delivered.DeliveryWorkRevision != current.Revision {
		t.Fatalf("delivery revision=%d Work revision=%d", delivered.DeliveryWorkRevision, current.Revision)
	}

	resolvedEvent, resolvedWork, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID:              delivered.ID,
		HandlingID:           delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
		Summary:              "Accepted the delegated result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolvedEvent.HandledAt == nil || resolvedEvent.Disposition != WorkDispositionComplete ||
		resolvedWork.Status != WorkDone || resolvedWork.Revision <= current.Revision {
		t.Fatalf("resolution did not atomically handle and terminalize: event=%+v Work=%+v", resolvedEvent, resolvedWork)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); !errors.Is(err, ErrEventHandled) {
		t.Fatalf("duplicate resolution err=%v, want ErrEventHandled", err)
	}
}

func TestSignalResolutionCASConflictLeavesAttentionDurable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "CAS conflict", "brain-agent-cas:@1")
	appendSignalTestEvent(t, store, item, "cas-1")
	delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "review.changed", DedupeKey: "review:cas:changed", Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); !errors.Is(err, ErrWorkRevisionConflict) {
		t.Fatalf("stale resolution err=%v, want ErrWorkRevisionConflict", err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention("brain-agent-brain-hidden:@1"); err != nil || !created {
		t.Fatalf("requeue created=%v err=%v", created, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reconciles int
	for _, event := range events {
		if event.Kind == "brain.reconcile_required" && event.HandledAt == nil {
			reconciles++
		}
	}
	if reconciles != 1 {
		t.Fatalf("unhandled reconcile attentions=%d events=%+v", reconciles, events)
	}
}

func TestTypedWaitOnlyExactProducerWakesWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Typed wait", Objective: "Wait for one exact user input.",
		Status: WorkWaiting, CompletionPolicy: CompletionBounded,
		Wake: &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:thread-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	nonmatch, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "user.input", DedupeKey: "user:thread-2:input:1",
		SourceName: "brain-thread:thread-2", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if nonmatch.Actionable {
		t.Fatalf("wrong producer woke typed wait: %+v", nonmatch)
	}
	match, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "user.input", DedupeKey: "user:thread-1:input:1",
		SourceName: "brain-thread:thread-1", Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !match.Actionable {
		t.Fatalf("exact producer did not wake typed wait: %+v", match)
	}
	current, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Wake != nil {
		t.Fatalf("satisfied wake remained attached: %+v", current.Wake)
	}
}

func TestAdmittedHostUserInputProducesTypedThreadWake(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "User wake producer", Objective: "Wake from admitted input on one exact thread.",
		Status: WorkWaiting, CompletionPolicy: CompletionBounded,
		Wake: &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + threadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	if err := service.AdmitHostUserInput(hostID, "request-user-wake-1", "continue", "brain-thread:"+threadID); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "user.input" || !events[0].Actionable ||
		events[0].SourceName != "brain-thread:"+threadID {
		t.Fatalf("admitted user wake = %+v", events)
	}
}

func TestDirtyWorkRequeuesOnceAtFairTail(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := createSignalTestWork(t, store, "A", "brain-agent-a:@1")
	b := createSignalTestWork(t, store, "B", "brain-agent-b:@1")
	c := createSignalTestWork(t, store, "C", "brain-agent-c:@1")
	appendSignalTestEvent(t, store, a, "a-1")
	deliveredA, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	appendSignalTestEvent(t, store, b, "b-1")
	appendSignalTestEvent(t, store, c, "c-1")
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: a.ID, Kind: "review.changed", DedupeKey: "review:a:changed", Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(deliveredA.DeliveryHostSessionID); err != nil || !created {
		t.Fatalf("requeue created=%v err=%v", created, err)
	}
	for index, wantWorkID := range []string{b.ID, c.ID, a.ID} {
		claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
		if err != nil || !ok || claimed.WorkID != wantWorkID {
			t.Fatalf("claim %d = %+v ok=%v err=%v, want Work %s", index, claimed, ok, err, wantWorkID)
		}
		if index < 2 {
			delivered, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, claimed.DeliveryHostSessionID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
				EventID: delivered.ID, HandlingID: delivered.HandlingID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionComplete,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Requeueing the same ended handling is idempotent.
	if _, created, err := store.RequeueUnhandledHostAttention(deliveredA.DeliveryHostSessionID); err != nil || created {
		t.Fatalf("duplicate requeue created=%v err=%v", created, err)
	}
}

func TestSignalMigrationIsBoundedIdempotentAndDoesNotReplay(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	raw := `{"schema_version":6,"migrations":{},"brain_work":[` +
		`{"work_id":"legacy-a","title":"Legacy A","objective":"Reconcile A","status":"waiting","completion_policy":"bounded","created_at":"` + fixed.Format(time.RFC3339) + `","updated_at":"` + fixed.Format(time.RFC3339) + `"},` +
		`{"work_id":"legacy-b","title":"Legacy B","objective":"Reconcile B","status":"needs_input","completion_policy":"bounded","created_at":"` + fixed.Add(time.Second).Format(time.RFC3339) + `","updated_at":"` + fixed.Add(time.Second).Format(time.RFC3339) + `"}],` +
		`"brain_work_events":[{"event_id":"old-delivered","work_id":"legacy-a","kind":"legacy.result","dedupe_key":"legacy:a:result","actionable":true,"created_at":"` + fixed.Format(time.RFC3339) + `","claimed_at":"` + fixed.Format(time.RFC3339) + `","delivery_host_session_id":"old-host","consumed_at":"` + fixed.Format(time.RFC3339) + `"}],` +
		`"brain_turns":[],"brain_turn_submissions":[]}`
	if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	complete, changed, err := store.MigrateSignalSystemV1(1)
	if err != nil || complete || changed != 1 {
		t.Fatalf("first batch complete=%v changed=%d err=%v", complete, changed, err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	complete, changed, err = store.MigrateSignalSystemV1(1)
	if err != nil || complete || changed != 1 {
		t.Fatalf("second batch complete=%v changed=%d err=%v", complete, changed, err)
	}
	complete, changed, err = store.MigrateSignalSystemV1(1)
	if err != nil || !complete || changed != 0 {
		t.Fatalf("completion batch complete=%v changed=%d err=%v", complete, changed, err)
	}
	if complete, changed, err = store.MigrateSignalSystemV1(1); err != nil || !complete || changed != 0 {
		t.Fatalf("idempotent rerun complete=%v changed=%d err=%v", complete, changed, err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	reconciles := map[string]int{}
	for _, event := range events {
		if event.ID == "old-delivered" && (event.DeliveredAt == nil || event.HandledAt != nil || !event.HistoricalDelivery) {
			t.Fatalf("historical consumption was not migrated to delivery-only: %+v", event)
		}
		if event.Kind == "brain.reconcile_required" {
			reconciles[event.WorkID]++
		}
	}
	if reconciles["legacy-a"] != 1 || reconciles["legacy-b"] != 1 {
		t.Fatalf("reconcile counts=%v events=%+v", reconciles, events)
	}
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
	if err != nil || !ok || claimed.ID == "old-delivered" || claimed.Kind != "brain.reconcile_required" {
		t.Fatalf("migration replay boundary claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	persisted, err := os.ReadFile(filepath.Join(stateDir, "orchestration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "consumed_at") || !strings.Contains(string(persisted), "delivered_at") {
		t.Fatalf("legacy delivery field survived migration: %s", persisted)
	}
}

func TestSignalMigrationNeverBackfillsHistoricalTerminalFinalization(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Historical terminal", Objective: "Remain terminal without bulk cleanup.",
		Status: WorkDone, OwnerSessionID: "brain-agent-historical:@1",
		OwnerDelegated: true, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MigrateSignalSystemV1(8); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MigrateSignalSystemV1(8); err != nil {
		t.Fatal(err)
	}
	current, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != WorkDone || current.Finalization != nil {
		t.Fatalf("historical terminal ownership was bulk-finalized: %+v", current)
	}
}

func TestSignalMigrationMakesNewSilentWorkReadyAtomically(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	complete, processed, err := store.MigrateSignalSystemV1(8)
	if err != nil || !complete || processed != 0 {
		t.Fatalf("empty migration complete=%v processed=%d err=%v", complete, processed, err)
	}
	item, err := store.CreateWork(Work{
		Title: "New ready Work", Objective: "Never persist a silent nonterminal shape.",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "brain.reconcile_required" || !events[0].Actionable {
		t.Fatalf("new Work attention = %+v", events)
	}
}

func TestSignalRestartRequeuesWorkKeyWithoutReplayingDeliveredFact(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Restart recovery", "brain-agent-restart:@1")
	appendSignalTestEvent(t, store, item, "restart-1")
	deliverSignalTestEvent(t, store, hostID)

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(restarted, fw, nil)
	complete, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8)
	if err != nil || !complete {
		t.Fatalf("startup complete=%v err=%v", complete, err)
	}
	if len(fw.sentCalls) != 1 || strings.Contains(fw.sentCalls[0].text, `"kind":"session.done"`) ||
		!strings.Contains(fw.sentCalls[0].text, `"kind":"brain.reconcile_required"`) {
		t.Fatalf("restart replayed delivered fact instead of reconciling Work: %+v", fw.sentCalls)
	}
}

func TestTerminalDispositionFinalizesOnlyDelegatedOwnerAndRetriesFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	delegatedID := "brain-agent-finalize:@1"
	item := createSignalTestWork(t, store, "Finalize delegated", delegatedID)
	appendSignalTestEvent(t, store, item, "finalize-1")
	delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{}}
	fw.sessions[delegatedID] = &classifier.Agent{ID: delegatedID, Delegated: true}
	fw.killErr = errors.New("injected teardown failure")
	fw.killLeavesLive = true
	service := NewService(store, fw, nil)
	_, failedWork, err := service.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err == nil || failedWork.Finalization == nil || failedWork.Finalization.State != SessionFinalizationFailed {
		t.Fatalf("failed finalization Work=%+v err=%v", failedWork, err)
	}
	fw.killErr = nil
	fw.killLeavesLive = false
	retryEvent, retryWork := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if retryEvent.Kind != "brain.finalization_failed" || retryWork.ID != item.ID {
		t.Fatalf("retry attention event=%+v Work=%+v", retryEvent, retryWork)
	}
	_, got, err := service.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: retryEvent.ID, HandlingID: retryEvent.HandlingID,
		ExpectedWorkRevision: retryEvent.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err != nil || got.Finalization == nil || got.Finalization.State != SessionFinalizationComplete || len(fw.killed) != 2 {
		t.Fatalf("retry Work=%+v killed=%v err=%v", got, fw.killed, err)
	}

	nondelegatedID := "external-shell:@1"
	nondelegated := createSignalTestWork(t, store, "Do not finalize external", nondelegatedID)
	appendSignalTestEvent(t, store, nondelegated, "external-1")
	delivered, _ = deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	fw.sessions[nondelegatedID] = &classifier.Agent{ID: nondelegatedID, Delegated: false}
	_, safeWork, err := service.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionCancel,
	})
	if err != nil || safeWork.Finalization == nil || safeWork.Finalization.State != SessionFinalizationSkipped {
		t.Fatalf("nondelegated finalization Work=%+v err=%v", safeWork, err)
	}
	for _, killed := range fw.killed {
		if killed == nondelegatedID {
			t.Fatalf("delegated=false Session was killed: %v", fw.killed)
		}
	}
}

func TestReviewRejectionDispositionKeepsAcceptedSuccessorOwner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := "brain-agent-review:@1"
	item := createSignalTestWork(t, store, "Review rejection", owner)
	appendSignalTestEvent(t, store, item, "review-1")
	delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID: owner, TurnID: owner + ":turn:2", AcceptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	resolvedEvent, resolvedWork, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionContinue, SuccessorSessionID: owner,
		NextAction: "Review the corrected delegated result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolvedEvent.Disposition != WorkDispositionContinue || resolvedWork.Status != WorkRunning ||
		resolvedWork.OwnerSessionID != owner || resolvedWork.Wake != nil {
		t.Fatalf("continuation did not retain successor ownership: event=%+v Work=%+v", resolvedEvent, resolvedWork)
	}
}
