package brain

import (
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
	if !service.NoteUserSteering(hostID) {
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
	go func() { steered <- service.NoteUserSteering(hostID) }()
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
