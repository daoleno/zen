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

func createSignalTestWork(t *testing.T, store *Store, title, owner string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{
		Title:            title,
		Objective:        "Exercise the durable Brain signal protocol.",
		Status:           WorkWaiting,
		OwnerSessionID:   owner,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Review the delegated Session result.",
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

func deliverSignalTestEvent(t *testing.T, store *Store, hostID string) (WorkReviewAction, Work) {
	t.Helper()
	claimed, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if claimed.HandlingID == "" || claimed.ProviderTurnID == "" || claimed.HandlingID == claimed.ProviderTurnID {
		t.Fatalf("claim did not separate handling and provider turn identities: %+v", claimed)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, item, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatalf("consume claimed event: %v", err)
	}
	return delivered, item
}

func resolveClaimedHostTurnForTest(t *testing.T, store *Store, claimed WorkReviewAction) {
	t.Helper()
	existingTurnID := ""
	if current, found, err := store.Turn(claimed.DeliveryHostSessionID); err != nil {
		t.Fatal(err)
	} else if found {
		existingTurnID = current.TurnID
		if !watcher.TurnImmutable(current.Status) {
			settleCanonicalHostTurnForTest(t, store, current.SessionID, current.TurnID)
		}
	}
	acceptedAt := time.Now().UTC()
	payloadDigest := pendingSubmissionDigest("claimed Host review " + claimed.FactEventID)
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.FactEventID, ClaimToken: claimed.HandlingID, PayloadSHA256: payloadDigest,
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh, ExistingTurnID: existingTurnID,
	})
	if err != nil || !created {
		t.Fatalf("prepare Host provider turn created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.FactEventID, PayloadSHA256: pending.PayloadSHA256,
		ActivityID: "host-activity-" + claimed.ProviderTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + claimed.ProviderTurnID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatalf("resolve Host provider turn: %v", err)
	}
}

func settleCanonicalHostTurnForTest(t *testing.T, store *Store, sessionID, turnID string) watcher.TurnSnapshot {
	t.Helper()
	current, found, err := store.TurnByID(sessionID, turnID)
	if err != nil || !found {
		t.Fatalf("canonical Host Turn %s found=%v err=%v", turnID, found, err)
	}
	if watcher.TurnImmutable(current.Status) {
		return current
	}
	settledAt := time.Now().UTC()
	if !settledAt.After(current.AcceptedAt) {
		settledAt = current.AcceptedAt.Add(time.Second)
	}
	settled, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: current.SessionID, TurnID: current.TurnID,
		Class: watcher.EvidenceProvider, Kind: "done", Bound: true,
		SourceID:  "provider\x00test-host\x00" + current.TurnID + "\x00done",
		Admission: current.Admission, ActivityID: current.ActivityID,
		StartedAt: current.AcceptedAt, SettledAt: settledAt, At: settledAt,
	})
	if err != nil || !changed || !watcher.TurnImmutable(settled.Status) {
		t.Fatalf("settle Host provider turn: turn=%+v changed=%v err=%v", settled, changed, err)
	}
	return settled
}

func TestSignalDeliveryAndHandlingAreSeparateRevisionCheckedTransitions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Delivery boundary", "brain-agent-delivery:@1")
	appendSignalTestEvent(t, store, item, "delivery-1")
	delivered, current := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if delivered.DeliveredAt == nil || delivered.HandlingID == "" {
		t.Fatalf("delivery incorrectly implied handling: %+v", delivered)
	}
	if fact, found, err := store.WorkEvent(delivered.FactEventID); err != nil || !found || fact.HandledAt != nil {
		t.Fatalf("delivery incorrectly implied handling: fact=%+v found=%v err=%v", fact, found, err)
	}
	if delivered.DeliveryWorkRevision != current.Revision {
		t.Fatalf("delivery revision=%d Work revision=%d", delivered.DeliveryWorkRevision, current.Revision)
	}

	resolvedEvent, resolvedWork, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID:               delivered.WorkID,
		HandlingID:           delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
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
	if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); !errors.Is(err, ErrEventHandled) {
		t.Fatalf("duplicate resolution err=%v, want ErrEventHandled", err)
	}
}

func TestSignalResolutionSerializesNextAttentionRevision(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "CAS conflict", "brain-agent-cas:@1")
	appendSignalTestEvent(t, store, item, "cas-1")
	delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	before, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	later, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "review.changed", DedupeKey: "review:cas:changed", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append next attention created=%v event=%+v err=%v", created, later, err)
	}
	afterAppend, err := store.Work(item.ID)
	if err != nil || afterAppend.Revision != before.Revision || later.WorkRevision != before.Revision+1 {
		t.Fatalf("later fact stole current revision: before=%+v after=%+v event=%+v err=%v", before, afterAppend, later, err)
	}
	resolved, terminal, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone || terminal.Revision != before.Revision+1 {
		t.Fatalf("current disposition event=%+v Work=%+v err=%v", resolved, terminal, err)
	}
	durableLater, found, err := store.WorkEvent(later.ID)
	if err != nil || !found || durableLater.HandledAt != nil || durableLater.WorkRevision != terminal.Revision {
		t.Fatalf("next attention was consumed with prior epoch: event=%+v found=%v err=%v", durableLater, found, err)
	}
	next, ok, err := store.ClaimNextReviewAction(delivered.DeliveryHostSessionID)
	if err != nil || !ok || next.FactEventID != later.ID || next.DeliveryWorkRevision != terminal.Revision {
		t.Fatalf("next epoch claim=%+v ok=%v err=%v", next, ok, err)
	}
}

func TestAdmittedHostUserInputIsOnlyTypedThreadWakeAuthority(t *testing.T) {
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
	wake := WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + threadID}
	item, err := store.CreateWork(Work{
		Title: "Typed wait", Objective: "Wait for one exact user input.",
		Status: WorkWaiting, CompletionPolicy: CompletionBounded,
		Wake: &wake,
	})
	if err != nil {
		t.Fatal(err)
	}
	forged, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "user.input", DedupeKey: "user:" + threadID + ":input:forged",
		SourceName: wake.Ref, Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forged.Actionable {
		t.Fatalf("generic matching strings acquired producer authority: %+v", forged)
	}
	current, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !workWakeEqual(current.Wake, &wake) {
		t.Fatalf("generic matching strings cleared typed wait: %+v", current)
	}
	service := NewService(store, nil, nil)
	prepared, created, err := service.PrepareHostUserInput(hostID, "admitted-input-1", "continue", wake.Ref)
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	current, err = store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Wake != nil {
		t.Fatalf("satisfied wake remained attached: %+v", current.Wake)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Actionable || events[1].Kind != "user.input" || !events[1].Actionable ||
		events[1].SourceName != "brain-thread:"+threadID {
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
	if _, created, err := store.EndReviewDelivery(deliveredA.WorkID, deliveredA.HandlingID, deliveredA.ProviderTurnID); err != nil || !created {
		t.Fatalf("requeue created=%v err=%v", created, err)
	}
	// FIFO by append sequence: B and C (older heads) discharge before the
	// requeued dirty key A, whose new head was appended after them. The
	// requeue never jumps the fair tail.
	for index, wantWorkID := range []string{b.ID, c.ID, a.ID} {
		claimed, ok, err := store.ClaimNextReviewAction("brain-agent-brain-hidden:@1")
		if err != nil || !ok || claimed.WorkID != wantWorkID {
			t.Fatalf("claim %d = %+v ok=%v err=%v, want Work %s", index, claimed, ok, err, wantWorkID)
		}
		if index < 2 {
			resolveClaimedHostTurnForTest(t, store, claimed)
			delivered, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
				WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionComplete,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Requeueing the same ended handling is idempotent.
	if _, created, err := store.EndReviewDelivery(deliveredA.WorkID, deliveredA.HandlingID, deliveredA.ProviderTurnID); err != nil || created {
		t.Fatalf("duplicate requeue created=%v err=%v", created, err)
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
	current, found, err := store.Turn(hostID)
	if err != nil || !found {
		t.Fatalf("current Host Turn=%+v found=%v err=%v", current, found, err)
	}
	settleCanonicalHostTurnForTest(t, store, hostID, current.TurnID)

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: restarted, sessions: map[string]*classifier.Agent{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(restarted, fw, nil)
	complete, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8)
	if err != nil || !complete {
		t.Fatalf("startup complete=%v err=%v", complete, err)
	}
	// The unresolved action is re-delivered to the current Host exactly once
	// (I8): same fact identity, no second queue item, no replay of the old
	// Host's consumed turn.
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, `"kind":"session.done"`) {
		t.Fatalf("restart did not re-deliver the unresolved action exactly once: %+v", fw.sentCalls)
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
	_, failedWork, err := service.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err == nil || len(failedWork.SessionFinalizations) != 1 || failedWork.SessionFinalizations[0].State != SessionFinalizationFailed {
		t.Fatalf("failed finalization Work=%+v err=%v", failedWork, err)
	}
	fw.killErr = nil
	fw.killLeavesLive = false
	retryEvent, retryWork := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if retryEvent.Kind != "brain.finalization_failed" || retryWork.ID != item.ID {
		t.Fatalf("retry attention event=%+v Work=%+v", retryEvent, retryWork)
	}
	_, got, err := service.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: retryEvent.WorkID, HandlingID: retryEvent.HandlingID,
		ProviderTurnID:       retryEvent.ProviderTurnID,
		ExpectedWorkRevision: retryEvent.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err != nil || len(got.SessionFinalizations) != 1 || got.SessionFinalizations[0].State != SessionFinalizationComplete || len(fw.killed) != 2 {
		t.Fatalf("retry Work=%+v killed=%v err=%v", got, fw.killed, err)
	}

	nondelegatedID := "external-shell:@1"
	nondelegated := createSignalTestWork(t, store, "Do not finalize external", nondelegatedID)
	appendSignalTestEvent(t, store, nondelegated, "external-1")
	delivered, _ = deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	fw.sessions[nondelegatedID] = &classifier.Agent{ID: nondelegatedID, Delegated: false}
	_, safeWork, err := service.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionCancel,
	})
	if err != nil || len(safeWork.SessionFinalizations) != 1 || safeWork.SessionFinalizations[0].State != SessionFinalizationSkipped {
		t.Fatalf("nondelegated finalization Work=%+v err=%v", safeWork, err)
	}
	for _, killed := range fw.killed {
		if killed == nondelegatedID {
			t.Fatalf("delegated=false Session was killed: %v", fw.killed)
		}
	}
}

func TestTerminalDispositionSkipsReusedUnownedRuntimeIdentity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-finalize-reused:@1"
	item, err := store.CreateWork(Work{
		Title: "Skip reused runtime identity", Objective: "Never kill an ambient replacement window.",
		Status: WorkWaiting, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded, NextAction: "Review the delegated Session result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, item, "reused-unowned-runtime")
	delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{},
		killErr:  fmt.Errorf("%w: %s", watcher.ErrUnownedTmuxTarget, sessionID),
	}
	_, terminal, err := NewService(store, fw, nil).ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	})
	if err != nil || terminal.Status != WorkDone || len(terminal.SessionFinalizations) != 1 ||
		terminal.SessionFinalizations[0].State != SessionFinalizationSkipped {
		t.Fatalf("unowned runtime finalization Work=%+v killed=%v err=%v", terminal, fw.killed, err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != sessionID {
		t.Fatalf("finalizer did not probe the exact historical runtime: %v", fw.killed)
	}
	events, listErr := store.ListWorkEvents(item.ID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, event := range events {
		if event.Kind == "brain.finalization_failed" && event.Actionable {
			t.Fatalf("unowned replacement created an unrecoverable retry obligation: %+v", event)
		}
	}
}

func TestTerminalFinalizationAttentionSurvivesMetadataUpdateAndReopen(t *testing.T) {
	for _, test := range []struct {
		name                string
		claimBeforeMetadata bool
	}{
		{name: "queued retry", claimBeforeMetadata: false},
		{name: "claimed but not delivered retry", claimBeforeMetadata: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			base := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
			now := base
			store.now = func() time.Time { return now }
			delegatedID := "brain-agent-finalization-fence:@1"
			hostID := "brain-agent-brain-hidden:@finalization-fence"
			item := createSignalTestWork(t, store, "Finalization retry fence", delegatedID)
			now = base.Add(time.Minute)
			appendSignalTestEvent(t, store, item, "finalization-fence")
			delivered, _ := deliverSignalTestEvent(t, store, hostID)

			fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
				delegatedID: {ID: delegatedID, Delegated: true},
			}, killErr: errors.New("injected teardown failure"), killLeavesLive: true}
			service := NewService(store, fw, nil)
			_, failed, err := service.ResolveWorkReview(WorkReviewDispositionRequest{
				WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionComplete,
			})
			if err == nil || len(failed.SessionFinalizations) != 1 ||
				failed.SessionFinalizations[0].State != SessionFinalizationFailed {
				t.Fatalf("failed finalization Work=%+v err=%v", failed, err)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if countUnhandledEventKind(events, "brain.finalization_failed") != 1 {
				t.Fatalf("finalization retry was not exactly one durable obligation: %+v", events)
			}

			var claim WorkReviewAction
			if test.claimBeforeMetadata {
				var ok bool
				claim, ok, err = store.ClaimNextReviewAction(hostID)
				if err != nil || !ok || claim.Kind != "brain.finalization_failed" {
					t.Fatalf("pre-metadata claim=%+v ok=%v err=%v", claim, ok, err)
				}
			}

			now = base.Add(2 * time.Minute)
			contextRef := "worklog/finalization-retry-after-terminal-metadata.md"
			updated, updateErr := store.UpdateWork(item.ID, WorkUpdate{ContextRef: &contextRef})
			if test.claimBeforeMetadata {
				if !errors.Is(updateErr, ErrWorkConflict) {
					t.Fatalf("metadata update during held claim err=%v want ErrWorkConflict", updateErr)
				}
				current, err := store.Work(item.ID)
				if err != nil || current.ContextRef == contextRef {
					t.Fatalf("rejected metadata update changed Work: Work=%+v err=%v", current, err)
				}
			} else if updateErr != nil || updated.ContextRef != contextRef {
				t.Fatalf("queued metadata update=%+v err=%v", updated, updateErr)
			}

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			if test.claimBeforeMetadata {
				blockers, err := reopened.LeasedReviewActions()
				if err != nil || len(blockers) != 1 || blockers[0].FactEventID != claim.FactEventID {
					t.Fatalf("reopened held claim=%+v err=%v", blockers, err)
				}
				if err := reopened.ReleaseReviewLease(claim.WorkID, claim.HandlingID, claim.ProviderTurnID); err != nil {
					t.Fatalf("release proved-undelivered retry claim: %v", err)
				}
			}
			claim, ok, err := reopened.ClaimNextReviewAction(hostID)
			if err != nil || !ok || claim.Kind != "brain.finalization_failed" {
				t.Fatalf("post-metadata retry claim=%+v ok=%v err=%v", claim, ok, err)
			}
			resolveClaimedHostTurnForTest(t, reopened, claim)
			retry, _, err := reopened.ConsumeReviewDelivery(claim.WorkID, claim.HandlingID, claim.ProviderTurnID)
			if err != nil {
				t.Fatalf("deliver finalization retry: %v", err)
			}

			fw.killErr = nil
			fw.killLeavesLive = false
			reopenedService := NewService(reopened, fw, nil)
			_, finalized, err := reopenedService.ResolveWorkReview(WorkReviewDispositionRequest{
				WorkID: retry.WorkID, HandlingID: retry.HandlingID,
				ProviderTurnID:       retry.ProviderTurnID,
				ExpectedWorkRevision: retry.DeliveryWorkRevision,
				Disposition:          WorkDispositionComplete,
			})
			if err != nil || len(finalized.SessionFinalizations) != 1 ||
				finalized.SessionFinalizations[0].State != SessionFinalizationComplete || len(fw.killed) != 2 {
				t.Fatalf("finalized Work=%+v killed=%v err=%v", finalized, fw.killed, err)
			}

			reopenedAgain, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			if replay, claimed, err := reopenedAgain.ClaimNextReviewAction(hostID); err != nil || claimed {
				t.Fatalf("finalization retry replayed after completion: event=%+v claimed=%v err=%v", replay, claimed, err)
			}
			events, err = reopenedAgain.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if countUnhandledEventKind(events, "brain.finalization_failed") != 0 {
				t.Fatalf("handled finalization retry remained actionable: %+v", events)
			}
		})
	}
}

func TestContinueDispositionAtomicallyAttachesStagedSuccessorSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	incumbent := "brain-agent-first-attempt:@1"
	successor := "brain-agent-correction:@2"
	item := createSignalTestWork(t, store, "Delegated correction", incumbent)
	appendSignalTestEvent(t, store, item, "correction-1")
	delivered, current := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")

	stagedWork, err := store.ReserveWorkSuccessor(item.ID, successor)
	if err != nil {
		t.Fatal(err)
	}
	if stagedWork.Revision != current.Revision || stagedWork.OwnerSessionID != incumbent {
		t.Fatalf("staging changed Work before disposition: before=%+v after=%+v", current, stagedWork)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stagedWork.SuccessorReservation == nil || stagedWork.SuccessorReservation.SessionID != successor ||
		stagedWork.SuccessorReservation.EventID != delivered.FactEventID || len(events) != 1 {
		t.Fatalf("staged successor Work=%+v Events=%+v", stagedWork, events)
	}
	acceptedAt := time.Now().UTC()
	turnID := successor + ":turn:1"
	pending := prepareInitialSubmission(t, store, successor, turnID, "correct the result", acceptedAt)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: successor, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "activity-correction",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-correction", Cursor: 1,
			SHA256: pending.PayloadSHA256, At: acceptedAt.Add(time.Second),
		},
		ResolvedAt: acceptedAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	beforeDisposition, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if beforeDisposition.Revision != current.Revision || beforeDisposition.OwnerSessionID != incumbent {
		t.Fatalf("accepted successor bypassed disposition: before=%+v after=%+v", current, beforeDisposition)
	}
	resolvedEvent, resolvedWork, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: delivered.WorkID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionContinue, SuccessorSessionID: successor,
		NextAction: "Review the correction.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolvedEvent.Disposition != WorkDispositionContinue || resolvedWork.OwnerSessionID != successor ||
		!resolvedWork.OwnerDelegated || resolvedWork.Revision != current.Revision+1 {
		t.Fatalf("successor was not atomically attached: event=%+v Work=%+v", resolvedEvent, resolvedWork)
	}
}
