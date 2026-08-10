package brain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestAttentionSchedulerAlternatesOldestNewestUniqueWorkKeys(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := make([]Work, 4)
	for index, name := range []string{"A", "B", "C", "D"} {
		items[index] = createSignalTestWork(t, store, name, "brain-agent-"+strings.ToLower(name)+":@1")
		appendSignalTestEvent(t, store, items[index], "fair-"+strings.ToLower(name))
	}
	hostID := "brain-agent-brain-hidden:@fair"
	for index, want := range []Work{items[0], items[3], items[1], items[2]} {
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil || !ok || claimed.WorkID != want.ID {
			t.Fatalf("claim %d = %+v ok=%v err=%v, want Work %s", index, claimed, ok, err, want.ID)
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		delivered, _, err := store.ConsumeClaimedWorkEvent(
			claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID,
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID,
			ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
			Disposition: WorkDispositionComplete,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAttentionSchedulerAdmitsFreshCompletionWithinTwoClaims(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old-a", "old-b", "old-c"} {
		item := createSignalTestWork(t, store, name, "brain-agent-"+name+":@1")
		appendSignalTestEvent(t, store, item, name)
	}
	hostID := "brain-agent-brain-hidden:@fresh"
	first, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, first)
	delivered, _, err := store.ConsumeClaimedWorkEvent(first.ID, first.HandlingID, first.WorkID, hostID, first.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fresh := createSignalTestWork(t, store, "fresh completion", "brain-agent-fresh:@1")
	appendSignalTestEvent(t, store, fresh, "fresh")
	next, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || next.WorkID != fresh.ID {
		t.Fatalf("fresh result was not admitted at the next bounded opportunity: claim=%+v ok=%v err=%v", next, ok, err)
	}
}

func TestContinuousForegroundSteeringKeepsOneReservationAndFairSuccessor(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@continuous-steering"
	hostGeneration := "host-generation-continuous"
	hostActivity := "host-activity-continuous"
	items := make([]Work, 3)
	for index, name := range []string{"old-a", "old-b", "old-c"} {
		items[index] = createSignalTestWork(t, store, name, "brain-agent-"+name+":@1")
		appendSignalTestEvent(t, store, items[index], name)
	}
	acceptSteering := func(requestID string) {
		t.Helper()
		candidate := BrainInputAdmission{
			RequestID: requestID, ThreadID: "brain-thread-continuous", HostSessionID: hostID,
			HostGeneration: hostGeneration, ProviderActivityID: hostActivity,
			SessionID: "brain-session-continuous", DisplayBody: "continue " + requestID,
		}
		if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
			t.Fatalf("prepare %s created=%v err=%v", requestID, created, err)
		}
		if _, _, accepted, err := store.AcceptBrainInputAdmission(candidate); err != nil || !accepted {
			t.Fatalf("accept %s accepted=%v err=%v", requestID, accepted, err)
		}
	}

	acceptSteering("steer-1")
	firstReservation, created, err := store.ReserveHostAttention(hostID, hostGeneration, hostActivity)
	if err != nil || !created || firstReservation.WorkID != items[0].ID {
		t.Fatalf("first reservation=%+v created=%v err=%v", firstReservation, created, err)
	}
	acceptSteering("steer-2")
	repeated, created, err := store.ReserveHostAttention(hostID, hostGeneration, hostActivity)
	if err != nil || created || repeated != firstReservation {
		t.Fatalf("continuous steering replaced reservation: first=%+v repeated=%+v created=%v err=%v", firstReservation, repeated, created, err)
	}

	fresh := createSignalTestWork(t, store, "fresh-d", "brain-agent-fresh-d:@1")
	appendSignalTestEvent(t, store, fresh, "fresh-d")
	active, _, err := store.HostForegroundState()
	if err != nil || active == nil {
		t.Fatalf("foreground state active=%+v err=%v", active, err)
	}
	claimed, ok, err := store.ConsumeHostAttentionReservation(
		hostID, hostGeneration, active.HostTurnID, hostActivity,
	)
	if err != nil || !ok || claimed.ID != firstReservation.EventID {
		t.Fatalf("consume reservation claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
	next, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || next.WorkID != fresh.ID {
		t.Fatalf("fair successor after continuous steering=%+v ok=%v err=%v, want fresh Work %s", next, ok, err, fresh.ID)
	}
}

func TestClaimPresentsLatestAuthoritativeFactWithoutRewritingAudit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "stale then done", "brain-agent-worker:@1")
	stale, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.stale",
		DedupeKey:  "session:brain-agent-worker:@1:turn:one:session.stale",
		SourceName: item.OwnerSessionID, PayloadRef: "session:" + item.OwnerSessionID,
		Summary: "Lease expired", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append stale created=%v err=%v", created, err)
	}
	done, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:brain-agent-worker:@1:turn:one:session.done",
		SourceName: item.OwnerSessionID, PayloadRef: "session:" + item.OwnerSessionID,
		Summary: "Release completed", Actionable: true,
	})
	if err != nil || !created || done.CoalescedInto != stale.ID {
		t.Fatalf("append done=%+v created=%v err=%v", done, created, err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{stale.ID, done.ID})
	if err != nil || lifecycles[stale.ID].CurrentResult || !lifecycles[done.ID].CurrentResult ||
		lifecycles[stale.ID].ReviewState != "queued" || lifecycles[done.ID].ReviewState != "queued" {
		t.Fatalf("stale/done card lifecycle=%+v err=%v", lifecycles, err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@latest")
	if err != nil || !ok {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	if claimed.ID != stale.ID || claimed.Kind != "session.stale" ||
		claimed.ReviewKind != "session.done" || claimed.ReviewSummary != "Release completed" {
		t.Fatalf("claim did not separate scheduler head from latest fact: %+v", claimed)
	}
	payload, err := marshalDirectWorkEventInput(claimed, item)
	if err != nil || !strings.Contains(payload, `"kind":"session.done"`) || strings.Contains(payload, `"summary":"Lease expired"`) {
		t.Fatalf("direct review did not present latest fact: err=%v payload=%s", err, payload)
	}
	audit, err := store.ListWorkEvents(item.ID)
	if err != nil || len(audit) != 2 || audit[0].Kind != "session.stale" || audit[1].Kind != "session.done" {
		t.Fatalf("append-only audit changed: events=%+v err=%v", audit, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID,
		"brain-agent-brain-hidden:@latest", claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
	audit, err = store.ListWorkEvents(item.ID)
	if err != nil || audit[0].HandledAt == nil || audit[1].HandledAt == nil {
		t.Fatalf("delivery fence did not handle stale and latest facts exactly once: events=%+v err=%v", audit, err)
	}
	if next, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@latest"); err != nil || ok {
		t.Fatalf("coalesced Work replayed after disposition: next=%+v ok=%v err=%v", next, ok, err)
	}
	lifecycles, err = store.WorkResultLifecycles([]string{stale.ID, done.ID})
	if err != nil || lifecycles[stale.ID].ReviewState != WorkReviewResolved ||
		lifecycles[done.ID].ReviewState != WorkReviewResolved {
		t.Fatalf("handled result lifecycle=%+v err=%v", lifecycles, err)
	}
}

func TestLegacyUnscopedAuditCannotCaptureScopedTerminalObligationAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy-head:@1"
	item, err := store.CreateWork(Work{
		Title: "legacy audit then current done", Objective: "Review the scoped terminal result.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	if err == nil {
		workIndex := workIndex(database.BrainWork, item.ID)
		database.BrainWork[workIndex].OwnerSessionID = sessionID
		database.BrainWork[workIndex].OwnerDelegated = true
		database.NextEventSequence++
		database.BrainWorkEvents = append(database.BrainWorkEvents, WorkEvent{
			ID: "legacy-unscoped-stale", WorkID: item.ID, Kind: "session.stale",
			DedupeKey:  "session:" + sessionID + ":session.stale:1",
			SourceName: sessionID, PayloadRef: "session:" + sessionID,
			Summary: "legacy lease audit", Actionable: true, CreatedAt: time.Now().Add(-time.Hour),
			Sequence: database.NextEventSequence, WorkRevision: item.Revision,
		})
		err = store.persistOrchestrationLocked(database)
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	done, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:" + sessionID + ":turn:turn-current:session.done",
		SourceName: sessionID, PayloadRef: "session:" + sessionID,
		Summary: "current scoped completion", Actionable: true,
	})
	if err != nil || !created || done.CoalescedInto != "" {
		t.Fatalf("scoped done coalesced behind legacy audit: done=%+v created=%v err=%v", done, created, err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@legacy-repair")
	if err != nil || !ok || claimed.ID != done.ID || claimed.ReviewKind != "session.done" {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	resolveClaimedHostTurnForTest(t, reopened, claimed)
	delivered, _, err := reopened.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolved, completed, err := reopened.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
	})
	if err != nil || resolved.ID != done.ID || completed.Status != WorkDone || len(completed.SessionFinalizations) != 1 ||
		completed.SessionFinalizations[0].SessionID != sessionID {
		t.Fatalf("terminal disposition/finalization resolved=%+v Work=%+v err=%v", resolved, completed, err)
	}
	finalized, err := reopened.RecordSessionFinalization(item.ID, sessionID, SessionFinalizationComplete, nil)
	if err != nil || len(finalized.SessionFinalizations) != 1 ||
		finalized.SessionFinalizations[0].State != SessionFinalizationComplete {
		t.Fatalf("legacy repair finalization Work=%+v err=%v", finalized, err)
	}
	audit, err := reopened.ListWorkEvents(item.ID)
	if err != nil || len(audit) != 2 || audit[0].HandledAt != nil || audit[1].HandledAt == nil {
		t.Fatalf("legacy/current audit disposition=%+v err=%v", audit, err)
	}
	if next, ok, err := reopened.ClaimNextActionableEvent("brain-agent-brain-hidden:@legacy-repair"); err != nil || ok {
		t.Fatalf("disposed obligation replayed: next=%+v ok=%v err=%v", next, ok, err)
	}
}

func TestFreshInventoryRetiresAbsentPendingOwnerExactlyOnceWithoutReplay(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "pending absent owner", Objective: "Keep ambiguous input as audit only.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-pending-absent:@1"
	turnID := sessionID + ":turn:1"
	acceptedAt := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	submission, created, err := store.PrepareTurnSubmission(delegatedSubmissionCandidate(
		item.ID, sessionID, turnID, "ambiguous pending input", acceptedAt,
	))
	if err != nil || !created || submission.State != watcher.TurnSubmissionPending {
		t.Fatalf("prepare submission=%+v created=%v err=%v", submission, created, err)
	}
	service := NewService(store, nil, nil)
	service.ReconcileDelegatedSessions(nil)
	service.ReconcileDelegatedSessions(nil)
	current, err := store.Work(item.ID)
	if err != nil || current.OwnerSessionID != "" || current.OwnerDelegated || current.Status != WorkNeedsInput {
		t.Fatalf("absent owner remained operational: Work=%+v err=%v", current, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "brain.owner_absent" {
		t.Fatalf("owner repair was not exact-once: events=%+v err=%v", events, err)
	}
	durable, found, err := store.TurnSubmission(sessionID, turnID)
	if err != nil || !found || durable.State != watcher.TurnSubmissionPending {
		t.Fatalf("ambiguous pending input was rewritten or replayed: submission=%+v found=%v err=%v", durable, found, err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	NewService(reopened, nil, nil).ReconcileDelegatedSessions(nil)
	events, _ = reopened.ListWorkEvents(item.ID)
	if len(events) != 1 {
		t.Fatalf("restart duplicated absent-owner attention: %+v", events)
	}
}

func TestExpiredProgressLeaseWithExactLiveProviderActivityKeepsExecutionOwned(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	acceptedAt := now.Add(-20 * time.Minute)
	store.now = func() time.Time { return now }
	sessionID := "brain-agent-live-overdue:@1"
	turnID := "turn-live-overdue"
	item, err := store.CreateWork(Work{
		Title: "live overdue owner", Objective: "Keep exact live execution controllable.",
		Status: WorkRunning, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
		ProcessIdentity: "process-live", PaneGeneration: "pane-live",
	})
	observation := watcher.ProviderActivityObservation{
		ID: "activity-live", Status: "running", StartedAt: acceptedAt.Add(time.Second),
		AdmissionStream: "codex", AdmissionID: "admission-live", AdmissionCursor: 7,
		AdmissionAt: acceptedAt.Add(time.Second), InputSHA256: strings.Repeat("a", 64),
	}
	if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceProvider, Kind: "running",
		SourceID: providerFactSourceID(sessionID, observation), Cursor: observation.AdmissionCursor,
		Admission: admissionFromObservation(observation), ActivityID: observation.ID,
		StartedAt: observation.StartedAt, At: acceptedAt.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	agent := &classifier.Agent{
		ID: sessionID, State: classifier.StateDone, Delegated: true,
		PaneAlive: true, ProcessID: 42,
	}
	service := NewService(store, &fakeWatcher{
		sessions:         map[string]*classifier.Agent{sessionID: agent},
		providerEvidence: map[string]watcher.ProviderActivityObservation{sessionID: observation},
	}, nil)
	service.ReconcileDelegatedSessions([]*classifier.Agent{agent})
	current, err := store.Work(item.ID)
	if err != nil || current.OwnerSessionID != sessionID || !current.OwnerDelegated || current.Status != WorkRunning {
		t.Fatalf("live overdue Work lost owner: Work=%+v err=%v", current, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "session.stale" {
			t.Fatalf("exact live provider activity emitted stale Attention: %+v", events)
		}
	}
}

func TestOwnershipLossAtomicallyDeprojectsAndRemainsProviderRecoverable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-ownership-loss:@1"
	turnID := "turn-ownership-loss"
	acceptedAt := time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC)
	item, err := store.CreateWork(Work{
		Title: "recover ownership loss", Objective: "Reject control without losing provider truth.",
		Status: WorkRunning, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
		ProcessIdentity: "process-owned", PaneGeneration: "pane-owned",
	})
	fact := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceLiveness,
		Kind: "ownership_lost", SourceID: "liveness-owned-generation-lost",
		SessionReplaced: true, At: acceptedAt.Add(time.Minute),
	}
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return fmt.Errorf("injected ownership-loss persistence failure") }
	if _, _, err := store.ApplyTurnFact(fact); err == nil {
		t.Fatal("ownership loss was reported before durable deprojection")
	}
	store.writeOrchestration = originalWrite
	before, err := store.Work(item.ID)
	if err != nil || before.OwnerSessionID != sessionID {
		t.Fatalf("failed atomic deprojection mutated Work=%+v err=%v", before, err)
	}
	if turn, found, err := store.TurnByID(sessionID, turnID); err != nil || !found || turn.Status != watcher.TurnAdmitted {
		t.Fatalf("failed atomic deprojection mutated Turn=%+v found=%v err=%v", turn, found, err)
	}

	turn, changed, err := store.ApplyTurnFact(fact)
	if err != nil || !changed || turn.Status != watcher.TurnOwnershipLost {
		t.Fatalf("ownership loss Turn=%+v changed=%v err=%v", turn, changed, err)
	}
	deprojected, err := store.Work(item.ID)
	if err != nil || deprojected.OwnerSessionID != "" || deprojected.OwnerDelegated || deprojected.Status != WorkNeedsInput {
		t.Fatalf("ownership loss Work=%+v err=%v", deprojected, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "session.ownership_lost" || !events[0].Actionable {
		t.Fatalf("ownership loss obligation=%+v err=%v", events, err)
	}

	doneFact := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceProvider,
		Kind: "done", SourceID: "provider-owned-generation-done", ActivityID: "activity-recovered",
		StartedAt: acceptedAt.Add(time.Second), SettledAt: acceptedAt.Add(2 * time.Minute),
		At: acceptedAt.Add(2 * time.Minute), Summary: "Provider completed after control ownership was lost",
	}
	recovered, changed, err := store.ApplyTurnFact(doneFact)
	if err != nil || !changed || recovered.Status != watcher.TurnDone {
		t.Fatalf("provider recovery Turn=%+v changed=%v err=%v", recovered, changed, err)
	}
	events, err = store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 2 || events[1].Kind != "session.done" ||
		events[1].CoalescedInto != events[0].ID {
		t.Fatalf("provider recovery obligations=%+v err=%v", events, err)
	}
	if _, changed, err := store.ApplyTurnFact(doneFact); err != nil || changed {
		t.Fatalf("provider recovery replay changed=%v err=%v", changed, err)
	}
}

func TestOwnershipLossPreservesImmutableProviderOutcome(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-completed-control-loss:@1"
	turnID := "turn-completed-control-loss"
	acceptedAt := time.Date(2026, 8, 11, 13, 30, 0, 0, time.UTC)
	item, err := store.CreateWork(Work{
		Title: "preserve completed result", Objective: "Keep provider outcome distinct from control ownership.",
		Status: WorkRunning, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
		ProcessIdentity: "process-completed", PaneGeneration: "pane-completed",
	})
	done, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceProvider,
		Kind: "done", SourceID: "provider-completed-before-control-loss",
		ActivityID: "activity-completed", StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(time.Minute), At: acceptedAt.Add(time.Minute),
		Summary: "Provider completed",
	})
	if err != nil || !changed || done.Status != watcher.TurnDone {
		t.Fatalf("provider completion Turn=%+v changed=%v err=%v", done, changed, err)
	}
	lost, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceLiveness,
		Kind: "ownership_lost", SourceID: "completed-control-generation-mismatch",
		SessionReplaced: true, At: acceptedAt.Add(2 * time.Minute),
	})
	if err != nil || !changed || lost.Status != watcher.TurnDone ||
		lost.ControlState != watcher.TurnControlOwnershipLost || lost.Attention != "ownership_lost" {
		t.Fatalf("control loss rewrote provider outcome: Turn=%+v changed=%v err=%v", lost, changed, err)
	}
	current, err := store.Work(item.ID)
	if err != nil || current.OwnerSessionID != "" || current.OwnerDelegated ||
		current.Status != WorkNeedsInput {
		t.Fatalf("completed control loss Work=%+v err=%v", current, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	durable, found, err := reopened.TurnByID(sessionID, turnID)
	if err != nil || !found || durable.Status != watcher.TurnDone ||
		durable.ControlState != watcher.TurnControlOwnershipLost {
		t.Fatalf("reopened completed control loss=%+v found=%v err=%v", durable, found, err)
	}
	candidate := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:2", "must remain unsent", acceptedAt.Add(3*time.Minute),
	)
	candidate.ExistingTurnID = turnID
	if _, created, err := reopened.PrepareTurnSubmission(candidate); err == nil || created {
		t.Fatalf("control-lost Session prepared fresh input: created=%v err=%v", created, err)
	}
}

func TestAbsentOwnerRepairCannotInvalidateAdmittedHandlingRevision(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-absent-during-review:@1"
	item, err := store.CreateWork(Work{
		Title: "absent during review", Objective: "Keep the admitted disposition valid.",
		Status: WorkWaiting, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:" + sessionID + ":turn:one:session.done",
		PayloadRef: "session:" + sessionID, SourceName: sessionID, Actionable: true,
	}); err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@admitted-revision"
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := store.ReconcileAbsentWorkOwner(item.ID, sessionID); err != nil || changed {
		t.Fatalf("in-flight handling repair changed=%v err=%v", changed, err)
	}
	after, err := store.Work(item.ID)
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("in-flight handling revision changed: before=%+v after=%+v err=%v", before, after, err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatalf("fresh inventory invalidated the admitted disposition: %v", err)
	}
	inventory, err := store.ProjectWorkInventory(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range inventory.Current {
		if current.ID == item.ID {
			t.Fatalf("absent historical owner reappeared as current: %+v", current)
		}
	}
}

func TestWorkInventoryBoundsCurrentRelationshipsAndBacklogsAbsentHistory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	absent, err := store.CreateWork(Work{
		Title: "absent owner", Objective: "Never project an absent endpoint.",
		Status: WorkRunning, OwnerSessionID: "brain-agent-absent:@1", OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 6; index++ {
		item, err := store.CreateWork(Work{
			Title: "queued", Objective: "Exercise bounded current attention.",
			Status: WorkOpen, CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID: item.ID, Kind: "brain.reconcile_required",
			DedupeKey: "queued:" + item.ID, Actionable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	historical, err := store.CreateWork(Work{
		Title: "historical", Objective: "Remain durable without being current.",
		Status: WorkDone, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := store.ProjectWorkInventory(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Current) != currentWorkQueuedAttentionLimit {
		t.Fatalf("current attention rows=%d want=%d: %+v", len(inventory.Current), currentWorkQueuedAttentionLimit, inventory.Current)
	}
	for _, current := range inventory.Current {
		if current.ID == absent.ID || current.ID == historical.ID || current.OwnerSessionID != "" || current.AttentionState != WorkAttentionQueued {
			t.Fatalf("non-operational relationship entered current projection: %+v", current)
		}
	}
	if inventory.Backlog.Total != 4 || inventory.Backlog.QueuedAttention != 2 ||
		inventory.Backlog.HistoricalResults != 1 || inventory.Backlog.RepairNeeded != 1 {
		t.Fatalf("backlog=%+v", inventory.Backlog)
	}
}

func TestWorkResultLifecycleSeparatesQueuedReviewDispositionAndFinalization(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-result-lifecycle:@1"
	item, err := store.CreateWork(Work{
		Title: "result lifecycle", Objective: "Keep every lifecycle authority distinct.",
		Status: WorkWaiting, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:" + sessionID + ":turn:one:session.done",
		PayloadRef: "session:" + sessionID, SourceName: sessionID,
		Summary: "Implementation completed", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append created=%v err=%v", created, err)
	}
	assertLifecycle := func(wantReview WorkReviewState, wantSession WorkResultSessionState) WorkResultLifecycle {
		t.Helper()
		lifecycles, err := store.WorkResultLifecycles([]string{event.ID})
		if err != nil {
			t.Fatal(err)
		}
		got := lifecycles[event.ID]
		if got.ReviewState != wantReview || got.SessionState != wantSession || !got.CurrentResult {
			t.Fatalf("lifecycle=%+v want review=%s session=%s current=true", got, wantReview, wantSession)
		}
		return got
	}
	assertLifecycle("queued", "open")

	hostID := "brain-agent-brain-hidden:@lifecycle"
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertLifecycle("reviewing", "open")
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
	assertLifecycle("resolved", "closing")
	if _, err := store.RecordSessionFinalization(item.ID, sessionID, SessionFinalizationComplete, nil); err != nil {
		t.Fatal(err)
	}
	assertLifecycle("resolved", "finalized")
}

type checkpointWatcher struct {
	*fakeWatcher
	entered chan struct{}
	release chan struct{}
}

func (w *checkpointWatcher) SubmitBrainHostInput(
	sessionID, payload, eventID, claimToken, workID, providerTurnID string,
	acceptedAt time.Time,
) (watcher.InputResult, error) {
	close(w.entered)
	<-w.release
	return w.fakeWatcher.SubmitBrainHostInput(
		sessionID, payload, eventID, claimToken, workID, providerTurnID, acceptedAt,
	)
}

func TestForegroundTurnEndReservesAttentionBeforeNextUserAdmission(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@checkpoint"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "checkpoint", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "checkpoint")
	watcherFixture := &checkpointWatcher{
		fakeWatcher: &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		}},
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatal("foreground input was not recognized")
	}
	dispatched := make(chan error, 1)
	go func() {
		_, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
			Type: "agent_state_change", AgentID: hostID,
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
			TurnID: "foreground-turn", Agent: &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
		})
		dispatched <- err
	}()
	<-watcherFixture.entered
	steered := make(chan bool, 1)
	go func() {
		recognized, _ := service.NoteUserSteering(hostID)
		steered <- recognized
	}()
	select {
	case <-steered:
		t.Fatal("new foreground admission overtook the exact turn-end checkpoint")
	case <-time.After(25 * time.Millisecond):
	}
	close(watcherFixture.release)
	if err := <-dispatched; err != nil {
		t.Fatal(err)
	}
	if recognized := <-steered; !recognized {
		t.Fatal("next foreground input was not admitted after the checkpoint")
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("checkpoint sent=%d Work inputs, want exact-once", len(watcherFixture.sentCalls))
	}
}

func TestLongForegroundTurnPersistsFutureAttentionAcrossReopenAndConsumesAtBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@durable-reservation"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-1"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-1", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("note foreground recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, "foreground-request-1", "keep working", "")
	if err != nil || !created {
		t.Fatalf("prepare foreground admission=%+v created=%v err=%v", prepared, created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "durable future review", "brain-agent-worker:@reservation")
	stale, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.stale",
		DedupeKey: "session:brain-agent-worker:@reservation:turn:one:session.stale",
		Summary:   "lease overdue", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append stale=%+v created=%v err=%v", stale, created, err)
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("foreground dispatch woke=%v err=%v", woke, err)
	}
	active, reserved, err := store.HostForegroundState()
	if err != nil || active == nil || reserved == nil || reserved.EventID != stale.ID ||
		reserved.HostTurnID != active.HostTurnID || reserved.ProviderTurnID == "" {
		t.Fatalf("durable foreground state active=%+v reservation=%+v err=%v", active, reserved, err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{stale.ID})
	if err != nil || lifecycles[stale.ID].ReviewState != WorkReviewReserved {
		t.Fatalf("future reservation lifecycle=%+v err=%v", lifecycles, err)
	}
	reservedTurnID := reserved.ProviderTurnID

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened
	restarted := NewService(reopened, watcherFixture, nil)
	if woke, err := restarted.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("reopen dispatch woke=%v err=%v", woke, err)
	}
	_, afterReopen, err := reopened.HostForegroundState()
	if err != nil || afterReopen == nil || afterReopen.ProviderTurnID != reservedTurnID {
		t.Fatalf("reopen replaced reservation=%+v err=%v", afterReopen, err)
	}
	nextAction := "Review the latest durable worker state."
	updated, err := reopened.UpdateWork(item.ID, WorkUpdate{NextAction: &nextAction})
	if err != nil {
		t.Fatal(err)
	}
	_, afterUpdate, err := reopened.HostForegroundState()
	if err != nil || afterUpdate == nil || afterUpdate.WorkRevision != updated.Revision ||
		afterUpdate.ProviderTurnID != reservedTurnID {
		t.Fatalf("Work update did not refresh durable reservation=%+v Work=%+v err=%v", afterUpdate, updated, err)
	}
	done, created, err := reopened.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:brain-agent-worker:@reservation:turn:one:session.done",
		Summary:   "worker completed", Actionable: true,
	})
	if err != nil || !created || done.CoalescedInto != stale.ID {
		t.Fatalf("append terminal done=%+v created=%v err=%v", done, created, err)
	}
	_, refreshed, _ := reopened.HostForegroundState()
	if refreshed == nil || refreshed.ProviderTurnID != reservedTurnID ||
		refreshed.SequenceFence != done.Sequence || refreshed.WorkRevision != done.WorkRevision {
		t.Fatalf("coalesced terminal fact did not refresh reservation: %+v done=%+v", refreshed, done)
	}
	if _, _, err := reopened.ConsumeHostAttentionReservation(
		hostID, "host-generation-1", active.HostTurnID, "",
	); err == nil {
		t.Fatal("a terminal boundary without the bound provider activity consumed the reservation")
	}

	checkpoint := &checkpointWatcher{fakeWatcher: watcherFixture, entered: make(chan struct{}), release: make(chan struct{})}
	checkpoint.turnStore = reopened
	checkpoint.sessions[hostID].State = classifier.StateDone
	checkpoint.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-1", Status: "completed", StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
	}
	restarted.watcher = checkpoint
	boundary := make(chan error, 1)
	go func() {
		_, boundaryErr := restarted.ObserveHostSessionEvent(watcher.SessionEvent{
			Type: "agent_state_change", AgentID: hostID,
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
			TurnID: "foreground-provider-turn",
			Agent:  &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
		})
		boundary <- boundaryErr
	}()
	select {
	case <-checkpoint.entered:
	case err := <-boundary:
		t.Fatalf("terminal boundary failed before reserved delivery: %v", err)
	}
	laterSteering := make(chan error, 1)
	go func() {
		recognized, steeringErr := restarted.NoteUserSteering(hostID)
		if steeringErr == nil && !recognized {
			steeringErr = fmt.Errorf("later foreground steering was not recognized")
		}
		laterSteering <- steeringErr
	}()
	select {
	case err := <-laterSteering:
		t.Fatalf("later steering overtook reserved boundary: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(checkpoint.release)
	if err := <-boundary; err != nil {
		t.Fatal(err)
	}
	if err := <-laterSteering; err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.sentCalls) != 1 || !strings.Contains(checkpoint.sentCalls[0].text, `"kind":"session.done"`) {
		t.Fatalf("reserved boundary delivery=%+v", checkpoint.sentCalls)
	}
	if _, reservation, err := reopened.HostForegroundState(); err != nil || reservation != nil {
		t.Fatalf("terminal boundary did not consume reservation=%+v err=%v", reservation, err)
	}
}

func TestAcceptedForegroundInputReservesAttentionBeforeTimelineProjection(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@projection-reservation"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "projection-independent reservation", "brain-agent-worker:@projection")
	event := appendSignalTestEvent(t, store, item, "projection-independent")
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-projection"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-projection", Status: "running", StartedAt: time.Now().Add(-time.Second),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	requestID := "foreground-projection-failure"
	prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	projectionErr := errors.New("injected timeline projection failure")
	store.projectBrainInputAdmission = func(BrainInputAdmission) error { return projectionErr }
	if err := service.AdmitHostUserInput(prepared); !errors.Is(err, projectionErr) {
		t.Fatalf("projection failure err=%v, want %v", err, projectionErr)
	}
	active, reservation, err := store.HostForegroundState()
	if err != nil || active == nil || reservation == nil || reservation.EventID != event.ID ||
		reservation.HostGeneration != "host-generation-projection" ||
		active.ProviderActivityID != "host-activity-projection" {
		t.Fatalf("accepted foreground state active=%+v reservation=%+v err=%v", active, reservation, err)
	}
	admission, found, err := store.BrainInputAdmission(requestID, activeHostThreadID(t, store))
	if err != nil || !found || admission.State != BrainInputAdmissionAccepted {
		t.Fatalf("accepted admission found=%v admission=%+v err=%v", found, admission, err)
	}
	items, err := store.ThreadTimeline(admission.ThreadID, 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("failed projection materialized timeline=%+v err=%v", items, err)
	}

	store.projectBrainInputAdmission = nil
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatalf("idempotent projection retry: %v", err)
	}
	_, repeated, err := store.HostForegroundState()
	if err != nil || repeated == nil || repeated.EventID != reservation.EventID ||
		repeated.HandlingID != reservation.HandlingID || len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("projection retry changed reservation=%+v sent=%+v err=%v", repeated, watcherFixture.sentCalls, err)
	}
	items, err = store.ThreadTimeline(admission.ThreadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != requestID {
		t.Fatalf("projection retry timeline=%+v err=%v", items, err)
	}
}

type changingHostGenerationWatcher struct {
	*fakeWatcher
	calls int
}

func (w *changingHostGenerationWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	w.calls++
	generation := "host-generation-prepared"
	if w.calls >= 2 {
		generation = "host-generation-replaced"
	}
	return watcher.OwnedGeneration{SessionID: sessionID, Generation: generation}, nil
}

func TestAcceptedForegroundInputGenerationMismatchStillPersistsExactReservation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@generation-reservation"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "generation-bound reservation", "brain-agent-worker:@generation")
	event := appendSignalTestEvent(t, store, item, "generation-bound")
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	base := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "replacement-generation-activity", Status: "running", StartedAt: time.Now(),
		}},
	}
	watcherFixture := &changingHostGenerationWatcher{fakeWatcher: base}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	requestID := "foreground-generation-mismatch"
	prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err == nil ||
		!strings.Contains(err.Error(), "generation") {
		t.Fatalf("generation mismatch was not surfaced: %v", err)
	}
	active, reservation, err := store.HostForegroundState()
	if err != nil || active == nil || reservation == nil || reservation.EventID != event.ID ||
		active.HostGeneration != "host-generation-prepared" ||
		reservation.HostGeneration != active.HostGeneration {
		t.Fatalf("generation-bound state active=%+v reservation=%+v err=%v", active, reservation, err)
	}
	if active.ProviderActivityID != "" {
		t.Fatalf("replacement generation activity was ambiently adopted: %+v", active)
	}
}

func activeHostThreadID(t *testing.T, store *Store) string {
	t.Helper()
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	return threadID
}

func TestHostForegroundLateBindsNewActivityAfterPriorTerminal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@late-bind"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{
		ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true,
	}
	now := time.Date(2026, 8, 11, 3, 40, 0, 0, time.UTC)
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-late-bind"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "prior-terminal-activity", Status: "completed",
			StartedAt: now.Add(-time.Hour), SettledAt: now.Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(
		hostID, "foreground-late-bind", "continue", "",
	)
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	active, reservation, err := store.HostForegroundState()
	if err != nil || active == nil || reservation != nil {
		t.Fatalf("initial foreground state active=%+v reservation=%+v err=%v", active, reservation, err)
	}
	if active.ProviderActivityID != "" {
		t.Fatalf("prior terminal activity was bound to new foreground: %+v", active)
	}

	item := createSignalTestWork(t, store, "late-bound review", "brain-agent-worker:@late-bind")
	event := appendSignalTestEvent(t, store, item, "late-bind")
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "new-running-activity", Status: "running", StartedAt: now.Add(time.Second),
	}
	if woke, err := service.DispatchPendingEvent(); err != nil || woke {
		t.Fatalf("foreground reservation dispatch woke=%v err=%v", woke, err)
	}
	active, reservation, err = store.HostForegroundState()
	if err != nil || active == nil || reservation == nil || reservation.EventID != event.ID {
		t.Fatalf("late-bound state active=%+v reservation=%+v err=%v", active, reservation, err)
	}
	if active.ProviderActivityID != "new-running-activity" {
		t.Fatalf("foreground did not monotonically bind new running activity: %+v", active)
	}

	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "new-running-activity", Status: "completed",
		StartedAt: now.Add(time.Second), SettledAt: now.Add(time.Minute),
	}
	host.State = classifier.StateDone
	terminalEvent := watcher.SessionEvent{
		Type: "agent_state_change", AgentID: hostID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: "provider-terminal-boundary",
		Agent:  &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
	}
	if _, err := service.ObserveHostSessionEvent(terminalEvent); err != nil {
		t.Fatal(err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("terminal boundary deliveries=%d, want one", len(watcherFixture.sentCalls))
	}
	if _, err := service.ObserveHostSessionEvent(terminalEvent); err != nil {
		t.Fatal(err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("replayed terminal boundary deliveries=%d, want one", len(watcherFixture.sentCalls))
	}
	active, reservation, err = store.HostForegroundState()
	if err != nil || active != nil || reservation != nil {
		t.Fatalf("terminal consume left foreground state active=%+v reservation=%+v err=%v", active, reservation, err)
	}
}
