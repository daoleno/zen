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

func TestAttentionSchedulerClaimsOldestPendingHeadFifoAcrossWorkKeys(t *testing.T) {
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
	// Fairness is append-sequence FIFO across Work keys: the oldest pending
	// head is selected at every boundary; no counter or last-admitted mirror
	// is persisted.
	for index, want := range items {
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

func TestAttentionSchedulerFifoAdmitsFreshCompletionInAppendOrder(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	oldItems := make([]Work, 0, 3)
	for _, name := range []string{"old-a", "old-b", "old-c"} {
		item := createSignalTestWork(t, store, name, "brain-agent-"+name+":@1")
		appendSignalTestEvent(t, store, item, name)
		oldItems = append(oldItems, item)
	}
	hostID := "brain-agent-brain-hidden:@fresh"
	// Discharge every older head exactly once; FIFO append sequence is the
	// whole fairness contract.
	for _, old := range oldItems {
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil || !ok || claimed.WorkID != old.ID {
			t.Fatalf("old head claim ok=%v err=%v, want Work %s", ok, err, old.ID)
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		delivered, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID)
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
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fresh := createSignalTestWork(t, store, "fresh completion", "brain-agent-fresh:@1")
	appendSignalTestEvent(t, store, fresh, "fresh")
	next, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || next.WorkID != fresh.ID {
		t.Fatalf("fresh result was not admitted at its FIFO turn: claim=%+v ok=%v err=%v", next, ok, err)
	}
}

func TestContinuousForegroundSteeringKeepsOneTurnAndFairSuccessor(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@continuous-steering"
	hostGeneration := "host-generation-continuous"
	hostActivity := "host-activity-continuous"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: hostGeneration},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: hostActivity, Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	acceptSteering := func(requestID string) {
		t.Helper()
		if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
			t.Fatalf("note steering %s recognized=%v err=%v", requestID, recognized, err)
		}
		prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue "+requestID, "")
		if err != nil || !created {
			t.Fatalf("prepare %s created=%v err=%v", requestID, created, err)
		}
		if err := service.AdmitHostUserInput(prepared); err != nil {
			t.Fatalf("admit %s: %v", requestID, err)
		}
	}

	acceptSteering("steer-1")
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID == "" || active.HostGeneration != hostGeneration {
		t.Fatalf("foreground turn active=%+v err=%v", active, err)
	}
	turnID := active.HostTurnID
	items := make([]Work, 3)
	for index, name := range []string{"old-a", "old-b", "old-c"} {
		items[index] = createSignalTestWork(t, store, name, "brain-agent-"+name+":@1")
		appendSignalTestEvent(t, store, items[index], name)
	}
	// The accepted foreground turn owns the lane: no pending Event is claimed
	// or delivered while it is live, however many Events queue behind it.
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("live foreground turn delivered woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("live foreground turn was interrupted: %+v", watcherFixture.sentCalls)
	}

	acceptSteering("steer-2")
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID != turnID {
		t.Fatalf("continuous steering replaced the foreground turn: active=%+v err=%v", active, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("second live foreground pass delivered woke=%v err=%v", woke, err)
	}

	fresh := createSignalTestWork(t, store, "fresh-d", "brain-agent-fresh-d:@1")
	appendSignalTestEvent(t, store, fresh, "fresh-d")
	// The exact bound Activity's terminal evidence closes the turn and admits
	// the oldest pending head exactly once; the fresh Work is the fair
	// successor only after that head's typed disposition.
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: hostActivity, Status: "completed",
		StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
	}
	host.State = classifier.StateDone
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("terminal boundary woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("terminal boundary deliveries=%d, want exact-once", len(watcherFixture.sentCalls))
	}
	events, err := store.ListWorkEvents(items[0].ID)
	if err != nil || len(events) != 1 || events[0].DeliveredAt == nil {
		t.Fatalf("oldest pending head was not delivered exactly once: %+v err=%v", events, err)
	}
	delivered := events[0]
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete,
	}); err != nil {
		t.Fatal(err)
	}
	// FIFO fairness: the remaining older heads (old-b, old-c) discharge
	// before the fresh Work's head is admitted at its append-order turn.
	for _, remaining := range []Work{items[1], items[2]} {
		next, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil || !ok || next.WorkID != remaining.ID {
			t.Fatalf("FIFO successor claim=%+v ok=%v err=%v, want Work %s", next, ok, err, remaining.ID)
		}
		resolveClaimedHostTurnForTest(t, store, next)
		consumed, _, err := store.ConsumeClaimedWorkEvent(next.ID, next.HandlingID, next.WorkID, hostID, next.ProviderTurnID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: consumed.ID, HandlingID: consumed.HandlingID,
			ProviderTurnID: consumed.ProviderTurnID, ExpectedWorkRevision: consumed.DeliveryWorkRevision,
			Disposition: WorkDispositionComplete,
		}); err != nil {
			t.Fatal(err)
		}
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
	if err != nil || !changed || turn.Status != watcher.TurnUnknown ||
		turn.ControlState != watcher.TurnControlOwnershipLost {
		t.Fatalf("ownership loss Turn=%+v changed=%v err=%v", turn, changed, err)
	}
	deprojected, err := store.Work(item.ID)
	if err != nil || deprojected.OwnerSessionID != "" || deprojected.OwnerDelegated || deprojected.Status != WorkNeedsInput {
		t.Fatalf("ownership loss Work=%+v err=%v", deprojected, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "session.uncertain" || !events[0].Actionable {
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
		lost.ControlState != watcher.TurnControlOwnershipLost || lost.Attention != "" {
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

func activeHostThreadID(t *testing.T, store *Store) string {
	t.Helper()
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	return threadID
}

func TestAcceptedForegroundSurvivesIntervalTerminalObservation(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@interval-terminal"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-ee771f"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-running", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)

	// First acceptance creates the durable foreground epoch while nothing is
	// queued, so the lane is idle behind it.
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	requestID := "foreground-interval-terminal"
	prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID == "" || active.ProviderActivityID != "host-activity-running" {
		t.Fatalf("initial foreground state active=%+v err=%v", active, err)
	}

	// A terminal/state observation inside the immutable Prepare-to-Accept
	// interval: ambient Agent state turns terminal, then the server's
	// duplicate-input path re-enters the lane before the idempotent
	// re-admission. The durable turn is bound to a still-running provider
	// Activity, so ambient state can neither close it nor fabricate a
	// boundary.
	host.State = classifier.StateDone
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("retry steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err = service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || created {
		t.Fatalf("duplicate prepare created=%v err=%v", created, err)
	}
	service.CancelUserSteering(hostID)
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}

	// Queued Attention arrives while the accepted durable foreground owns the
	// checkpoint. The pending Event stays pending: no claim, no delivery, no
	// lifecycle promotion.
	item := createSignalTestWork(t, store, "interval terminal review", "brain-agent-worker:@interval-terminal")
	event := appendSignalTestEvent(t, store, item, "interval-terminal")
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("foreground reconcile woke=%v err=%v", woke, err)
	}
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-running" {
		t.Fatalf("accepted foreground state active=%+v err=%v", active, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("foreground turn was interrupted: %+v", watcherFixture.sentCalls)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ClaimedAt != nil || events[0].DeliveredAt != nil {
		t.Fatalf("foreground turn claimed or delivered the pending Event: %+v err=%v", events, err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{event.ID})
	if err != nil || lifecycles[event.ID].ReviewState != WorkReviewQueued {
		t.Fatalf("pending lifecycle=%+v err=%v", lifecycles, err)
	}
}

func TestAlreadyTerminalReopenConvergesExactBoundaryWithoutWatcherEdge(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@reopen-terminal"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-reopen"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-bound", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, "foreground-reopen-terminal", "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "reopen terminal review", "brain-agent-worker:@reopen-terminal")
	event := appendSignalTestEvent(t, store, item, "reopen-terminal")
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-bound" {
		t.Fatalf("pre-reopen foreground state active=%+v err=%v", active, err)
	}
	// Reopen: the provider Activity bound to the durable turn ended while the
	// daemon was down. No duplicate watcher edge will arrive for it; the lane
	// derives the boundary from the durable binding + current observation.
	host.State = classifier.StateDone
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-bound", Status: "completed",
		StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
	}
	watcherFixture.turnStore = nil
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened
	restarted := NewService(reopened, watcherFixture, nil)
	if woke, err := restarted.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("reopen convergence woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("reopen boundary deliveries=%d, want exact-once", len(watcherFixture.sentCalls))
	}
	active, err = reopened.CurrentHostForegroundTurn()
	if err != nil || active != nil {
		t.Fatalf("reopen terminal close left foreground active=%+v err=%v", active, err)
	}
	events, err := reopened.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("reopen delivered events=%+v err=%v", events, err)
	}
	delivered := events[0]
	if delivered.ID != event.ID || delivered.DeliveredAt == nil ||
		delivered.DeliveryHostSessionID != hostID || delivered.DeliverySequenceFence == 0 {
		t.Fatalf("reopen boundary did not deliver the exact pending Event: %+v", delivered)
	}
	lifecycles, err := reopened.WorkResultLifecycles([]string{event.ID})
	if err != nil || lifecycles[event.ID].ReviewState != WorkReviewReviewing {
		t.Fatalf("reopen delivered lifecycle=%+v err=%v", lifecycles, err)
	}

	// Second reopen must not replay the consumed delivery.
	watcherFixture.turnStore = nil
	reopened2, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened2
	restarted2 := NewService(reopened2, watcherFixture, nil)
	if woke, err := restarted2.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("second reopen reconcile woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("second reopen replayed deliveries=%d, want one", len(watcherFixture.sentCalls))
	}
}

func TestDelayedOldTerminalCannotCloseCurrentForegroundTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@delayed-terminal"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-delayed"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-current", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, "foreground-delayed-terminal", "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "delayed terminal review", "brain-agent-worker:@delayed-terminal")
	appendSignalTestEvent(t, store, item, "delayed-terminal")
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-current" {
		t.Fatalf("initial foreground state active=%+v err=%v", active, err)
	}

	// A delayed terminal observation for an OLD Activity arrives: it must not
	// close the current turn, adopt itself as the durable binding, or deliver.
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-old", Status: "completed",
		StartedAt: time.Now().Add(-time.Hour), SettledAt: time.Now().Add(-time.Minute),
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("delayed old terminal reconcile woke=%v err=%v", woke, err)
	}
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-current" {
		t.Fatalf("delayed old terminal closed current foreground: active=%+v err=%v", active, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("delayed old terminal delivered: %+v", watcherFixture.sentCalls)
	}

	// The exact current Activity is still running: the lane holds.
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-current", Status: "running", StartedAt: time.Now().Add(-time.Minute),
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("running foreground reconcile woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("running foreground delivered early: %+v", watcherFixture.sentCalls)
	}

	// Only the exact bound Activity's terminal evidence converges the boundary.
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-current", Status: "completed",
		StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
	}
	host.State = classifier.StateDone
	if woke, err := service.ReconcileHostLane(); err != nil || !woke {
		t.Fatalf("exact terminal reconcile woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("exact terminal boundary deliveries=%d, want one", len(watcherFixture.sentCalls))
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].DeliveredAt == nil {
		t.Fatalf("exact terminal delivery events=%+v err=%v", events, err)
	}
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active != nil {
		t.Fatalf("exact terminal close left foreground active=%+v err=%v", active, err)
	}
}

func TestTerminalMismatchAtReopenFailsClosedWithoutDelivery(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@mismatch-terminal"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-mismatch"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-bound", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, "foreground-mismatch-terminal", "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "mismatch review", "brain-agent-worker:@mismatch-terminal")
	appendSignalTestEvent(t, store, item, "mismatch-terminal")
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-bound" {
		t.Fatalf("initial foreground state active=%+v err=%v", active, err)
	}

	// Reopen with provider evidence that is terminal but names a different
	// Activity than the durable turn's binding: fail closed, never converge,
	// never deliver.
	host.State = classifier.StateDone
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-replacement", Status: "completed",
		StartedAt: time.Now().Add(-time.Hour), SettledAt: time.Now().Add(-time.Minute),
	}
	watcherFixture.turnStore = nil
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened
	restarted := NewService(reopened, watcherFixture, nil)
	if woke, err := restarted.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("mismatch reconcile woke=%v err=%v", woke, err)
	}
	active, err = reopened.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-bound" {
		t.Fatalf("mismatch consumed or adopted foreground: active=%+v err=%v", active, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("mismatch delivered: %+v", watcherFixture.sentCalls)
	}
	events, err := reopened.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].DeliveredAt != nil {
		t.Fatalf("mismatch moved the pending Event: %+v err=%v", events, err)
	}

	// The edge-driven boundary also fails closed on the same mismatch: it is
	// a trigger only, never authority.
	if _, err := restarted.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "agent_state_change", AgentID: hostID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: "foreground-provider-turn", Agent: &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
	}); err != nil {
		t.Fatalf("mismatch terminal edge errored: %v", err)
	}
	active, err = reopened.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-bound" ||
		len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("mismatch edge closed foreground active=%+v sent=%+v err=%v", active, watcherFixture.sentCalls, err)
	}
}

func TestIntervalTerminalProjectionFailureStillKeepsPendingEvent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@interval-projection"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
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
	requestID := "foreground-interval-projection"
	prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	projectionErr := errors.New("injected interval projection failure")
	store.projectBrainInputAdmission = func(BrainInputAdmission) error { return projectionErr }
	if err := service.AdmitHostUserInput(prepared); !errors.Is(err, projectionErr) {
		t.Fatalf("first admission projection err=%v, want %v", err, projectionErr)
	}

	// Interval terminal observation, then the duplicate-input retry path.
	host.State = classifier.StateDone
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("retry steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err = service.PrepareHostUserInput(hostID, requestID, "continue", "")
	if err != nil || created {
		t.Fatalf("duplicate prepare created=%v err=%v", created, err)
	}
	service.CancelUserSteering(hostID)
	if err := service.AdmitHostUserInput(prepared); !errors.Is(err, projectionErr) {
		t.Fatalf("retry admission projection err=%v, want %v", err, projectionErr)
	}

	item := createSignalTestWork(t, store, "interval projection review", "brain-agent-worker:@interval-projection")
	appendSignalTestEvent(t, store, item, "interval-projection")
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("foreground reconcile woke=%v err=%v", woke, err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-projection" {
		t.Fatalf("projection failure lost foreground: active=%+v err=%v", active, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ClaimedAt != nil || events[0].DeliveredAt != nil {
		t.Fatalf("projection failure moved the pending Event: %+v err=%v", events, err)
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
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID == "" || len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("projection retry changed foreground=%+v sent=%+v err=%v", active, watcherFixture.sentCalls, err)
	}
	items, err = store.ThreadTimeline(admission.ThreadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != requestID {
		t.Fatalf("projection retry timeline=%+v err=%v", items, err)
	}
}

func TestForegroundTurnEndSerializesCheckpointBeforeNextUserAdmission(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@checkpoint"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-checkpoint"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-checkpoint", Status: "running", StartedAt: time.Now().Add(-time.Minute),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatal("foreground input was not recognized")
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, "foreground-checkpoint", "continue", "")
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "checkpoint", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "checkpoint")
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-checkpoint", Status: "completed",
		StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
	}
	host.State = classifier.StateDone
	checkpoint := &checkpointWatcher{fakeWatcher: watcherFixture, entered: make(chan struct{}), release: make(chan struct{})}
	service.watcher = checkpoint
	dispatched := make(chan error, 1)
	go func() {
		_, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
			Type: "agent_state_change", AgentID: hostID,
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
			TurnID: "foreground-turn", Agent: &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone},
		})
		dispatched <- err
	}()
	<-checkpoint.entered
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
	close(checkpoint.release)
	if err := <-dispatched; err != nil {
		t.Fatal(err)
	}
	if recognized := <-steered; !recognized {
		t.Fatal("next foreground input was not admitted after the checkpoint")
	}
	if len(checkpoint.sentCalls) != 1 {
		t.Fatalf("checkpoint sent=%d Work inputs, want exact-once", len(checkpoint.sentCalls))
	}
}

func TestLongForegroundTurnPersistsPendingAttentionAcrossReopenAndDeliversAtBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@durable-turn"
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
	item := createSignalTestWork(t, store, "durable future review", "brain-agent-worker:@durable")
	stale, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.stale",
		DedupeKey: "session:brain-agent-worker:@durable:turn:one:session.stale",
		Summary:   "lease overdue", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append stale=%+v created=%v err=%v", stale, created, err)
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("foreground reconcile woke=%v err=%v", woke, err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "host-activity-1" {
		t.Fatalf("durable foreground state active=%+v err=%v", active, err)
	}
	boundTurnID := active.HostTurnID
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ClaimedAt != nil || events[0].DeliveredAt != nil {
		t.Fatalf("pending attention was claimed behind the live turn: %+v err=%v", events, err)
	}
	lifecycles, err := store.WorkResultLifecycles([]string{stale.ID})
	if err != nil || lifecycles[stale.ID].ReviewState != WorkReviewQueued {
		t.Fatalf("pending lifecycle=%+v err=%v", lifecycles, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened
	restarted := NewService(reopened, watcherFixture, nil)
	if woke, err := restarted.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("reopen reconcile woke=%v err=%v", woke, err)
	}
	active, err = reopened.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID != boundTurnID ||
		active.ProviderActivityID != "host-activity-1" {
		t.Fatalf("reopen replaced the durable turn=%+v err=%v", active, err)
	}
	nextAction := "Review the latest durable worker state."
	updated, err := reopened.UpdateWork(item.ID, WorkUpdate{NextAction: &nextAction})
	if err != nil {
		t.Fatal(err)
	}
	done, created, err := reopened.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:brain-agent-worker:@durable:turn:one:session.done",
		Summary:   "worker completed", Actionable: true,
	})
	if err != nil || !created || done.CoalescedInto != stale.ID {
		t.Fatalf("append terminal done=%+v created=%v err=%v", done, created, err)
	}
	if updated.Revision == 0 {
		t.Fatal("Work update did not advance revision")
	}
	events, err = reopened.ListWorkEvents(item.ID)
	if err != nil || len(events) != 2 || events[0].ClaimedAt != nil || events[1].ClaimedAt != nil {
		t.Fatalf("Work/Event mutations disturbed the pending head: %+v err=%v", events, err)
	}
	// A terminal boundary without the bound provider activity never converges.
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "host-activity-old", Status: "completed",
		StartedAt: time.Now().Add(-time.Hour), SettledAt: time.Now().Add(-time.Minute),
	}
	if woke, err := restarted.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("unbound terminal reconcile woke=%v err=%v", woke, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("unbound terminal delivered: %+v", watcherFixture.sentCalls)
	}

	checkpoint := &checkpointWatcher{fakeWatcher: watcherFixture, entered: make(chan struct{}), release: make(chan struct{})}
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
		t.Fatalf("terminal boundary failed before delivery: %v", err)
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
		t.Fatalf("later steering overtook the boundary: %v", err)
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
		t.Fatalf("boundary delivery=%+v", checkpoint.sentCalls)
	}
	active, err = reopened.CurrentHostForegroundTurn()
	if err != nil || active != nil {
		t.Fatalf("terminal boundary did not close the durable turn: active=%+v err=%v", active, err)
	}

	// Second reopen: the delivered Event awaits its typed disposition; the
	// lane must stop without replaying it.
	watcherFixture.turnStore = nil
	reopened2, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	watcherFixture.turnStore = reopened2
	restarted2 := NewService(reopened2, watcherFixture, nil)
	if woke, err := restarted2.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("second reopen reconcile woke=%v err=%v", woke, err)
	}
	if len(checkpoint.sentCalls) != 1 {
		t.Fatalf("second reopen replayed deliveries=%d, want one", len(checkpoint.sentCalls))
	}
}

func TestAcceptedForegroundInputKeepsPendingEventBeforeTimelineProjection(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@projection-turn"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "projection-independent turn", "brain-agent-worker:@projection")
	appendSignalTestEvent(t, store, item, "projection-independent")
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true}
	watcherFixture := &fakeWatcher{
		turnStore: store, sessions: map[string]*classifier.Agent{hostID: host},
		ownedGenerations: map[string]string{hostID: "host-generation-projection"},
		providerEvidence: map[string]watcher.ProviderActivityObservation{hostID: {
			ID: "host-activity-projection", Status: "running", StartedAt: time.Now().Add(-time.Second),
		}},
	}
	service := NewService(store, watcherFixture, nil)
	// The pending internal Event is admitted at the idle boundary BEFORE the
	// user's message is prepared: steering can never overtake it.
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("foreground steering recognized=%v err=%v", recognized, err)
	}
	if len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("idle-boundary delivery=%d, want exact-once", len(watcherFixture.sentCalls))
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].DeliveredAt == nil {
		t.Fatalf("idle-boundary delivery events=%+v err=%v", events, err)
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
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostGeneration != "host-generation-projection" {
		t.Fatalf("accepted foreground state active=%+v err=%v", active, err)
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
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostTurnID == "" || len(watcherFixture.sentCalls) != 1 {
		t.Fatalf("projection retry changed foreground=%+v sent=%+v err=%v", active, watcherFixture.sentCalls, err)
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

func TestAcceptedForegroundInputGenerationMismatchFailsLaneClosed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@generation-turn"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
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
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostGeneration != "host-generation-prepared" {
		t.Fatalf("generation-bound state active=%+v err=%v", active, err)
	}
	if active.ProviderActivityID != "" {
		t.Fatalf("replacement generation activity was ambiently adopted: %+v", active)
	}
	// A pending Event that arrives behind the mismatched turn is safe: the
	// lane fails closed on the same mismatch and never claims or delivers it.
	item := createSignalTestWork(t, store, "generation-bound turn", "brain-agent-worker:@generation")
	appendSignalTestEvent(t, store, item, "generation-bound")
	if woke, err := service.ReconcileHostLane(); err == nil || woke ||
		!strings.Contains(err.Error(), "generation") {
		t.Fatalf("mismatch lane pass woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].ClaimedAt != nil || events[0].DeliveredAt != nil {
		t.Fatalf("generation mismatch moved the pending Event: %+v err=%v", events, err)
	}
	if len(watcherFixture.sentCalls) != 0 {
		t.Fatalf("generation mismatch delivered: %+v", watcherFixture.sentCalls)
	}
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
	now := time.Now()
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
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil {
		t.Fatalf("initial foreground state active=%+v err=%v", active, err)
	}
	if active.ProviderActivityID != "" {
		t.Fatalf("prior terminal activity was bound to new foreground: %+v", active)
	}

	item := createSignalTestWork(t, store, "late-bound review", "brain-agent-worker:@late-bind")
	appendSignalTestEvent(t, store, item, "late-bind")
	watcherFixture.providerEvidence[hostID] = watcher.ProviderActivityObservation{
		ID: "new-running-activity", Status: "running", StartedAt: now.Add(time.Second),
	}
	if woke, err := service.ReconcileHostLane(); err != nil || woke {
		t.Fatalf("foreground reconcile woke=%v err=%v", woke, err)
	}
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.ProviderActivityID != "new-running-activity" {
		t.Fatalf("late-bound state active=%+v err=%v", active, err)
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
	active, err = store.CurrentHostForegroundTurn()
	if err != nil || active != nil {
		t.Fatalf("terminal close left foreground active=%+v err=%v", active, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].DeliveredAt == nil {
		t.Fatalf("terminal boundary delivery events=%+v err=%v", events, err)
	}
}

// TestLiveOwnerCorrectionNotBlockedByQueuedReview is the required regression:
// a correction steer to the live owner Session of a Work with a QUEUED
// (pending, never delivered) attention head is not review and must not be
// rejected. Once that head is DELIVERED, a correction is staged under the
// exact handling (successor reservation) and takes ownership only through
// the typed continue disposition — it never overtakes the review.
func TestLiveOwnerCorrectionNotBlockedByQueuedReview(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-live-correction:@1"
	turnID := "turn-live-correction"
	acceptedAt := time.Date(2026, 8, 11, 15, 0, 0, 0, time.UTC)
	item, err := store.CreateWork(Work{
		Title:            "live correction owner",
		Objective:        "Correction steering must not wait for queued review.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		OwnerDelegated:   true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: acceptedAt,
		ProcessIdentity: "process-live-correction", PaneGeneration: "pane-live-correction",
	})
	// Turn 1 ends with a bound provider failure (the recoverable-error shape
	// from the live incident): one actionable session.failed head is queued
	// and the Work stays open for correction.
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceProvider,
		Kind: "failed", SourceID: "provider\x00" + sessionID + "\x00turn1-failed",
		ActivityID: "activity-turn-1", StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(time.Minute), At: acceptedAt.Add(time.Minute),
		Summary: "Turn 1 failed",
	}); err != nil || !changed {
		t.Fatalf("turn 1 failure changed=%v err=%v", changed, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "session.failed" ||
		!events[0].Actionable || events[0].DeliveredAt != nil {
		t.Fatalf("queued review head=%+v err=%v", events, err)
	}
	// The owner Session may submit correction input: queued review never
	// blocks it.
	candidate := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:2", "correction while live", acceptedAt.Add(2*time.Minute),
	)
	candidate.ExistingTurnID = turnID
	correction, created, err := store.PrepareTurnSubmission(candidate)
	if err != nil || !created {
		t.Fatalf("queued review blocked live-owner correction: created=%v err=%v", created, err)
	}
	if correction.State != watcher.TurnSubmissionPending {
		t.Fatalf("correction submission=%+v", correction)
	}
	// Provider accepts the correction: turn 2 becomes canonical.
	resolveDelegatedSubmission(t, store, correction, "activity-turn-2", acceptedAt.Add(2*time.Minute).Add(time.Second))

	// Once that head is DELIVERED and awaits its typed disposition, a further
	// same-owner correction is staged under the exact handling, never
	// rejected: the successor reservation binds the delivered Event and
	// ownership transfers only when the typed continue disposition commits.
	hostID := "brain-agent-brain-hidden:@live-correction"
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
	if delivered.DeliveredAt == nil {
		t.Fatalf("delivered handling=%+v", delivered)
	}
	staged := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:3", "steer during review", acceptedAt.Add(3*time.Minute),
	)
	staged.ExistingTurnID = correction.ProposedTurnID
	staged.Mode = watcher.TurnSubmissionConditionalSteer
	staged.BaselineActivityID = "activity-turn-2"
	if _, created, err := store.PrepareTurnSubmission(staged); err != nil || !created {
		t.Fatalf("delivered review rejected the staged correction: created=%v err=%v", created, err)
	}
	stagedWork, err := store.Work(item.ID)
	if err != nil || stagedWork.SuccessorReservation == nil ||
		stagedWork.SuccessorReservation.SessionID != sessionID ||
		stagedWork.SuccessorReservation.EventID != delivered.ID ||
		stagedWork.Revision != delivered.DeliveryWorkRevision {
		t.Fatalf("delivered review did not stage the correction under the exact handling: Work=%+v err=%v", stagedWork, err)
	}
	// The staged correction does not overtake the review: the delivered
	// handling still awaits its exact typed disposition.
	if unhandled, err := store.WorkHasInFlightHandling(item.ID); err != nil || !unhandled {
		t.Fatalf("staged correction displaced the delivered review: unhandled=%v err=%v", unhandled, err)
	}
}
