package brain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestDataPlatformNeedsInputWakesSourceThreadAndAcceptsNamedFollowUpAtomically(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		workID          = "6e879311-6fe7-4e97-a8d8-2a0caa29f4af"
		eventID         = "abbfd98b-7da6-48f6-b46f-b61e9316f951"
		sourceThread    = "brain-data-platform-source"
		ownerSession    = "Data Platform Session @397"
		ownerTurn       = "turn-data-platform-397"
		nextAttempt     = "Data Platform Session @398"
		nextAttemptTurn = "turn-data-platform-398"
	)
	item, err := store.CreateWork(Work{
		ID: workID, Title: "Data Platform", Objective: "Finish the platform work",
		SourceThreadID: sourceThread, CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef: "All platform acceptance criteria are verified.",
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 22, 5, 0, 0, 0, time.UTC)
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: ownerSession, TurnID: ownerTurn, AcceptedAt: acceptedAt,
		ProcessIdentity: "process-397", PaneGeneration: "pane-397", PayloadSHA256: "payload-397",
	})
	event, created, err := store.AppendWorkEvent(WorkEvent{
		ID: eventID, WorkID: item.ID, Kind: "session.needs_input",
		DedupeKey:  "session:" + ownerSession + ":turn:" + ownerTurn + ":session.needs_input",
		SourceName: ownerSession, Summary: "A named follow-up is required.", Actionable: true,
	})
	if err != nil || !created || event.ID != eventID {
		t.Fatalf("append exact needs_input created=%v event=%+v err=%v", created, event, err)
	}
	if event.Actionable {
		t.Fatalf("observation must remain non-actionable until canonical turn evidence: %+v", event)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: ownerSession, TurnID: ownerTurn, Class: watcher.EvidenceControl,
		Kind: "attention", SourceID: eventID, Summary: "A named follow-up is required.",
		At: acceptedAt.Add(time.Second), LeaseSeconds: 300,
	}); err != nil || !changed {
		t.Fatalf("apply exact needs_input changed=%v err=%v", changed, err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(workID))
	if err != nil || state.Review == nil || state.Review.Ref != eventID {
		t.Fatalf("review state=%+v err=%v", state, err)
	}
	if _, duplicate, err := store.AppendWorkEvent(event); err != nil || duplicate {
		t.Fatalf("duplicate exact event created=%v err=%v", duplicate, err)
	}
	delivered, _ := claimAndDeliverTestReview(t, store, "brain-host:@source")
	if delivered.EventID != eventID || delivered.SourceThreadID != sourceThread {
		t.Fatalf("delivered action=%+v", delivered)
	}
	failed := delegatedSubmissionCandidate(
		workID, "Data Platform Session @failed", "turn-data-platform-failed", "failed spawn",
		acceptedAt.Add(30*time.Second),
	)
	failedPending, created, err := store.PrepareInputAdmission(failed)
	if err != nil || !created {
		t.Fatalf("prepare failed admission created=%v err=%v", created, err)
	}
	if _, err := store.AbortInputAdmission(
		failedPending.SessionID, failedPending.ProposedTurnID, failedPending.Receipt, failedPending.PayloadSHA256,
	); err != nil {
		t.Fatal(err)
	}
	state, _ = store.FSM().State(lifecycle.WorkID(workID))
	if admission := state.AdmissionByToken(lifecycle.TurnToken(failedPending.ProposedTurnID)); admission == nil || admission.Status != lifecycle.AdmissionAborted {
		t.Fatalf("proved admission failure was not durably aborted: %+v", admission)
	}
	pending, created, err := store.PrepareInputAdmission(delegatedSubmissionCandidate(
		workID, nextAttempt, nextAttemptTurn, "named follow-up", acceptedAt.Add(time.Minute),
	))
	if err != nil || !created {
		t.Fatalf("prepare nextAttempt created=%v err=%v", created, err)
	}
	resolveDelegatedSubmission(t, store, pending, "activity-398", acceptedAt.Add(2*time.Minute))
	lease := requireReviewDelivered(t, store, workID)
	_, projected, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: workID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionContinue,
		NextSessionID: nextAttempt, NextTurnToken: nextAttemptTurn, NextAction: "Continue with the named follow-up.",
	})
	if err != nil {
		t.Fatal(err)
	}
	state, _ = store.FSM().State(lifecycle.WorkID(workID))
	if state.Attempt == nil || state.Attempt.SessionID != nextAttempt || state.Attempt.TurnToken != nextAttemptTurn ||
		state.Review != nil || projected.AttemptSessionID != nextAttempt {
		t.Fatalf("atomic acceptance state=%+v projected=%+v", state, projected)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _ := reopened.FSM().State(lifecycle.WorkID(workID))
	if replayed.Attempt == nil || replayed.Attempt.SessionID != nextAttempt || replayed.Review != nil {
		t.Fatalf("restart replay=%+v", replayed)
	}
}

func TestExactControlDoneCompletesUntilDoneWhenCriteriaMet(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "exact criteria completion", Objective: "finish exact acceptance",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "all live criteria pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Now().UTC().Add(-time.Second)
	candidate := delegatedSubmissionCandidate(item.ID, "worker:@1", "turn-exact", "finish", acceptedAt)
	candidate.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare exact signal created=%v err=%v", created, err)
	}
	result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: "worker:@1", TurnID: "turn-exact", Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "control\x00done-exact", Summary: "all criteria verified",
		At: time.Now().UTC(), CriteriaMet: true,
	})
	if err != nil || !result.Owned || !result.Matched || !result.Changed {
		t.Fatalf("exact done result=%+v err=%v", result, err)
	}
	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Status != lifecycle.StatusDone || st.Review != nil || st.Attempt != nil {
		t.Fatalf("exact criteria completion state=%+v err=%v", st, err)
	}
}

func TestIsolatedLifecycleLiveProofRetryReloadSameSessionAndExactCompletion(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const (
		sessionID    = "worker:same-session"
		sourceThread = "brain:lifecycle-regression"
	)
	if err := store.SetChatState(ChatState{ThreadID: sourceThread}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title: "exact same-Session follow-ups", Objective: "finish after reviewed corrections",
		SourceThreadID: sourceThread, CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef: "the final exact prompt reports criteria_met",
	})
	if err != nil {
		t.Fatal(err)
	}

	prepareSignal := func(token, payload string) watcher.InputAdmission {
		candidate := delegatedSubmissionCandidate(item.ID, sessionID, token, payload, now)
		candidate.SignalProtocol = true
		pending, created, prepareErr := store.PrepareInputAdmission(candidate)
		if prepareErr != nil || !created {
			t.Fatalf("prepare %s created=%v err=%v", token, created, prepareErr)
		}
		return pending
	}
	progress := func(token, kind, source, summary string, criteriaMet bool) watcher.TurnProgressResult {
		now = now.Add(time.Second)
		result, progressErr := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
			SessionID: sessionID, TurnID: token, Class: watcher.EvidenceControl,
			Kind: kind, SourceID: "control\x00" + source, Summary: summary,
			At: now, LeaseSeconds: 300, CriteriaMet: criteriaMet,
		})
		if progressErr != nil || !result.Owned || !result.Matched || !result.Changed {
			t.Fatalf("progress %s/%s result=%+v err=%v", token, kind, result, progressErr)
		}
		return result
	}

	const initialToken = "turn:z-initial"
	prepareSignal(initialToken, "start")
	if got := progress(initialToken, "running", "initial-running", "initial work running", false); got.Turn.TurnID != initialToken {
		t.Fatalf("initial progress rebound to %q", got.Turn.TurnID)
	}
	beforeLiveness, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	for range 1000 {
		if _, changed, renewErr := store.ReassertLiveTurnOwnership(item.ID, sessionID, initialToken); renewErr != nil || changed {
			t.Fatalf("identical provider observation changed=%v err=%v", changed, renewErr)
		}
	}
	afterIdentical, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if afterIdentical.Revision != beforeLiveness.Revision {
		t.Fatalf("identical provider observations grew revision %d -> %d", beforeLiveness.Revision, afterIdentical.Revision)
	}
	now = now.Add(lifecycle.LeaseGrace / 2)
	if _, changed, renewErr := store.ReassertLiveTurnOwnership(item.ID, sessionID, initialToken); renewErr != nil || !changed {
		t.Fatalf("material provider renewal changed=%v err=%v", changed, renewErr)
	}
	afterRenewal, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	for range 1000 {
		if _, changed, renewErr := store.ReassertLiveTurnOwnership(item.ID, sessionID, initialToken); renewErr != nil || changed {
			t.Fatalf("coalesced provider observation changed=%v err=%v", changed, renewErr)
		}
	}
	afterCoalesced, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if afterRenewal.Revision != beforeLiveness.Revision+1 || afterCoalesced.Revision != afterRenewal.Revision {
		t.Fatalf("provider renewal revisions before=%d renewed=%d repeated=%d",
			beforeLiveness.Revision, afterRenewal.Revision, afterCoalesced.Revision)
	}
	progress(initialToken, "attention", "needs-input-1", "exact first correction required", false)
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != initialToken || !current.SignalProtocol || current.Status != watcher.TurnBlocked {
		t.Fatalf("first reload current prompt=%+v found=%v err=%v", current, found, err)
	}
	// A definitely unsent claim releases the same Review, which survives a full
	// Store reopen without a delivery scheduler state.
	retryState, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || retryState.Review == nil {
		t.Fatalf("retry review state=%+v err=%v", retryState, err)
	}
	eventID := retryState.Review.EventID
	claimed, err := store.FSM().ClaimReview(lifecycle.WorkID(item.ID), "host:dropped", "host-turn:dropped")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().ReleaseReview(lifecycle.WorkID(item.ID), claimed.Review.Handler.HandlerToken); err != nil {
		t.Fatal(err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	reloadedRetry, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || reloadedRetry.Review == nil || reloadedRetry.Review.EventID != eventID {
		t.Fatalf("retry changed event identity across reload: state=%+v err=%v", reloadedRetry, err)
	}

	finalEventID := ""
	reviewedFollowUp := func(token, hostID string, finish bool) {
		claimAndDeliverTestReview(t, store, hostID)
		beforeResolve, readErr := store.Work(item.ID)
		if readErr != nil || beforeResolve.Review == nil {
			t.Fatalf("review before autonomous handoff=%+v err=%v", beforeResolve.Review, readErr)
		}
		eventID := beforeResolve.Review.EventID
		now = now.Add(time.Second)
		pending := prepareSignal(token, "reviewed follow-up "+token)
		resolveDelegatedSubmission(t, store, pending, "activity-"+token, now.Add(time.Millisecond))
		nextAttemptState, stateErr := store.FSM().State(lifecycle.WorkID(item.ID))
		admission := nextAttemptState.AdmissionByToken(lifecycle.TurnToken(token))
		if stateErr != nil || admission == nil || admission.Status != lifecycle.AdmissionAccepted ||
			admission.Purpose != lifecycle.AdmissionPurposeReview {
			t.Fatalf("aggregate review admission=%+v err=%v", admission, stateErr)
		}
		lease := requireReviewDelivered(t, store, item.ID)
		resolvedEvent, resolvedWork, resolveErr := store.ResolveWorkReview(WorkReviewDispositionRequest{
			WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
			ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionContinue,
			NextSessionID: sessionID, NextTurnToken: token, NextAction: "run next scoped tracer concern",
		})
		if resolveErr != nil {
			t.Fatalf("resolve reviewed follow-up %s: %v", token, resolveErr)
		}
		if resolvedEvent.ID != eventID || resolvedEvent.Disposition != WorkDispositionContinue ||
			resolvedEvent.HandledAt == nil || resolvedWork.NextAction != "run next scoped tracer concern" {
			t.Fatalf("autonomous typed disposition event=%+v work=%+v", resolvedEvent, resolvedWork)
		}
		exact, exactFound, exactErr := store.Turn(sessionID)
		if exactErr != nil || !exactFound || exact.TurnID != token || !exact.SignalProtocol {
			t.Fatalf("reviewed current prompt=%+v found=%v err=%v", exact, exactFound, exactErr)
		}
		if finish {
			progress(token, "done", "done-"+token, "all acceptance criteria verified", true)
			events, eventErr := store.ListWorkEvents(item.ID)
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			for _, event := range events {
				if (event.Kind == "turn_done" || event.Kind == "session.done") && event.Summary == "all acceptance criteria verified" {
					finalEventID = event.ID
				}
			}
			if finalEventID == "" {
				t.Fatalf("final canonical event missing: %+v", events)
			}
		} else {
			progress(token, "attention", "needs-input-"+token, "another exact correction required", false)
		}
	}

	const secondToken = "turn:m-reviewed-second"
	reviewedFollowUp(secondToken, "host:review-1", false)
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	current, found, err = store.Turn(sessionID)
	if err != nil || !found || current.TurnID != secondToken || !current.SignalProtocol || current.Status != watcher.TurnBlocked {
		t.Fatalf("second reload current prompt=%+v found=%v err=%v", current, found, err)
	}

	const finalToken = "turn:a-reviewed-final"
	reviewedFollowUp(finalToken, "host:review-2", true)
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Status != lifecycle.StatusDone || state.Attempt != nil || state.Review != nil {
		t.Fatalf("exact final state=%+v err=%v", state, err)
	}
	projected, err := store.Work(item.ID)
	if err != nil || projected.Status != WorkDone || projected.Revision != state.Revision || projected.Revision > 60 {
		t.Fatalf("bounded final projection=%+v state revision=%d err=%v", projected, state.Revision, err)
	}
	items, err := store.ThreadTimeline(sourceThread, 0)
	if err != nil || len(items) != 1 || items[0].ID != finalEventID {
		t.Fatalf("one-card projection=%+v err=%v", items, err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDueRetryWaitIgnoresUnrelatedBrainConversation(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.FSM().Close()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{
		Title: "bounded external status check", Objective: "check one external source when due",
		SourceThreadID: "brain:source-thread", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.FSM().OpenReviewEvent(
		lifecycle.WorkID(item.ID), "external_check", "external-run:one", "event:external-check",
	)
	if err != nil {
		t.Fatal(err)
	}
	dueAt := now.Add(10 * time.Minute)
	waiting, err := store.FSM().ResolveReview(lifecycle.WorkID(item.ID), opened.Review.EventID, lifecycle.ResolveReviewInput{
		Disposition: lifecycle.DispositionWait, WakeKind: lifecycle.WakeDueRetry,
		WakeRef: "external-run:one", NextAttemptAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}

	pending, created, err := store.PrepareBrainInputAdmission(BrainInputAdmission{
		RequestID: "unrelated-message", ThreadID: item.SourceThreadID,
		HostSessionID: "brain-host:@1", SessionID: "brain-host:@1", DisplayBody: "unrelated conversation",
	})
	if err != nil || !created {
		t.Fatalf("prepare unrelated input created=%v err=%v", created, err)
	}
	_, woken, accepted, err := store.AcceptBrainInputAdmission(pending)
	if err != nil || !accepted || len(woken) != 0 {
		t.Fatalf("unrelated input woke due retry: accepted=%v woken=%+v err=%v", accepted, woken, err)
	}
	afterInput, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if afterInput.Revision != waiting.Revision || afterInput.Review != nil || afterInput.Wake == nil ||
		afterInput.Wake.Kind != lifecycle.WakeDueRetry || afterInput.Wake.NextAttemptAt == nil ||
		!afterInput.Wake.NextAttemptAt.Equal(dueAt) {
		t.Fatalf("unrelated conversation mutated due wait: before=%+v after=%+v", waiting, afterInput)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "user.input" {
			t.Fatalf("unrelated conversation created Work Event: %+v", event)
		}
	}

	now = dueAt
	var sweepWG sync.WaitGroup
	sweepErrors := make(chan error, 32)
	for range 32 {
		sweepWG.Add(1)
		go func() {
			defer sweepWG.Done()
			sweepErrors <- store.SweepLifecycle()
		}()
	}
	sweepWG.Wait()
	close(sweepErrors)
	for err := range sweepErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	action, found, err := store.ClaimNextReviewAction("brain-host:due-check")
	if err != nil || !found || action.Kind != "retry_due" || action.PayloadRef != "external-run:one" {
		t.Fatalf("due bounded check action=%+v found=%v err=%v", action, found, err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Review == nil || state.Review.EventID != action.EventID || state.Wake != nil {
		t.Fatalf("due retry canonical state=%+v err=%v", state, err)
	}
}

func TestTurnSnapshotUsesExactAggregateOwnerDuringProjectionRepair(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "projection repair identity", Objective: "never return the older prompt token",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "the exact owner survives projection repair",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "worker:projection-race"
	acceptedAt := time.Now().UTC().Add(-time.Second)
	initial := delegatedSubmissionCandidate(item.ID, sessionID, "turn:older", "initial", acceptedAt)
	initial.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(initial); err != nil || !created {
		t.Fatalf("prepare initial created=%v err=%v", created, err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: initial.ProposedTurnID, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "control\x00initial", At: acceptedAt.Add(time.Millisecond),
	}); err != nil || !result.Matched {
		t.Fatalf("initial progress=%+v err=%v", result, err)
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	const currentToken = "turn:current"
	if _, _, err := store.FSM().PrepareAdmission(lifecycle.WorkID(item.ID), lifecycle.PrepareAdmissionInput{
		SessionID: sessionID, TurnToken: currentToken, Receipt: currentToken,
		PayloadSHA256: pendingSubmissionDigest("current"), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", Mode: lifecycle.AdmissionConditionalSteer,
		ExistingTurnToken: state.Attempt.TurnToken, BaselineActivityID: "provider-activity",
		SignalProtocol: true, AttemptedAt: acceptedAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().AcceptAdmissionBySignal(lifecycle.WorkID(item.ID), currentToken, sessionID); err != nil {
		t.Fatal(err)
	}

	// The engine has committed the new Attempt while presentation.json still
	// contains only the older Turn row. The read must use the aggregate token.
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != currentToken || !current.SignalProtocol {
		t.Fatalf("repair-window snapshot=%+v found=%v err=%v", current, found, err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.FSM().Close()
	reloaded, found, err := store.Turn(sessionID)
	if err != nil || !found || reloaded.TurnID != currentToken || !reloaded.SignalProtocol {
		t.Fatalf("reloaded snapshot=%+v found=%v err=%v", reloaded, found, err)
	}
}

func TestProjectionRebuildReplacesStaleReviewEvent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "review event projection repair", Objective: "bind only the current review fact",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "the current review survives reload",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.FSM().OpenReviewEvent(
		lifecycle.WorkID(item.ID), "operator_review", "first", "event:first",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	projected, err := store.Work(item.ID)
	if err != nil || projected.Review == nil || projected.Review.EventID != "event:first" {
		t.Fatalf("first projection=%+v err=%v", projected.Review, err)
	}
	if _, err := store.FSM().ResolveReview(lifecycle.WorkID(item.ID), first.Review.EventID, lifecycle.ResolveReviewInput{
		Disposition: lifecycle.DispositionContinue, Actor: "test",
	}); err != nil {
		t.Fatal(err)
	}
	second, err := store.FSM().OpenReviewEvent(
		lifecycle.WorkID(item.ID), "operator_review", "second", "event:second",
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.Review == nil || second.Review.EventID == first.Review.EventID {
		t.Fatalf("new canonical event=%+v previous=%+v", second.Review, first.Review)
	}
	// Simulate a crash before the new engine event reaches presentation.json.
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.FSM().Close()
	reloadedState, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Work(item.ID)
	if err != nil || reloaded.Review == nil ||
		reloaded.Review.EventID != reloadedState.Review.EventID ||
		reloaded.Review.EventID != "event:second" ||
		!reloaded.Review.RequiredAt.Equal(reloadedState.Review.OpenedAt) {
		t.Fatalf("reloaded review=%+v canonical=%+v err=%v", reloaded.Review, reloadedState.Review, err)
	}
}

func TestOldLifecycleSchemaIsRejectedWithoutMigration(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(stateDir, "lifecycle"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"schema_version":12,"next_event_sequence":1,"brain_input_admissions":[],"brain_work":[{"work_id":"cd16a79d-78d8-4c73-b8f5-2d4816171a63","revision":1,"title":"old","objective":"old","status":"open","completion_policy":"bounded","source_thread_id":"old-thread","removed_state":{"session_id":"failed-spawn","event_id":"event-only"},"created_at":"2026-08-22T00:00:00Z","updated_at":"2026-08-22T00:00:00Z"}],"brain_work_events":[],"brain_turns":[]}`
	if err := os.WriteFile(filepath.Join(stateDir, "presentation.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "lifecycle", "events.jsonl"), []byte("partial old log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); !errors.Is(err, ErrSchedulerStateReset) {
		t.Fatalf("old schema was not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "presentation.json")); err != nil {
		t.Fatalf("breaking open mutated old projection: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(stateDir, "scheduler-reset-*")); err != nil || len(matches) != 0 {
		t.Fatalf("unexpected archive created: matches=%v err=%v", matches, err)
	}
}

func TestResolveCanonicalReviewWithoutProjectedEvent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "canonical review", Objective: "resolve without a parallel event row",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().OpenReview(lifecycle.WorkID(item.ID), "operator_review", "canonical-only"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextReviewAction("host:@1")
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v action=%+v", ok, err, claimed)
	}
	delivered, _, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	resolved, work, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID == "" || resolved.HandledAt == nil || work.Status != WorkDone || work.Review != nil {
		t.Fatalf("resolved=%+v work=%+v", resolved, work)
	}
	if delivered, err := store.HasLiveDeliveredReview(); err != nil || delivered {
		t.Fatalf("Host lane remained gated after resolve: delivered=%v err=%v", delivered, err)
	}
	resolvedRevision := work.Revision
	assertNoActionable := func(current *Store) {
		t.Helper()
		events, err := current.ListWorkEvents(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Actionable {
				t.Fatalf("resolved Work retained actionable event: %+v", event)
			}
		}
		if action, claimed, err := current.ClaimNextReviewAction("host:@2"); err != nil || claimed {
			t.Fatalf("resolved Work was reclaimable: action=%+v claimed=%v err=%v", action, claimed, err)
		}
	}
	assertNoActionable(store)
	for i := 0; i < 8; i++ {
		if err := store.SyncWorkProjection(item.ID); err != nil {
			t.Fatal(err)
		}
	}
	stable, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || stable.Revision != resolvedRevision {
		t.Fatalf("projection sync grew revision: state=%+v want=%d err=%v", stable, resolvedRevision, err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.FSM().Close()
	assertNoActionable(reopened)
	reloaded, err := reopened.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || reloaded.Revision != resolvedRevision || reloaded.Review != nil {
		t.Fatalf("reload reopened resolved review: state=%+v want=%d err=%v", reloaded, resolvedRevision, err)
	}
}

func TestOverdueSweepProjectsAndAutomaticallyDeliversOneCanonicalEvent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 2, 59, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	const (
		hostID   = "brain-host:@due-retry"
		threadID = "brain-thread:due-retry"
		wakeRef  = "external-run:due-retry"
	)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "overdue durable check", Objective: "wake exactly once",
		SourceThreadID: threadID, CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef: "external condition is terminal",
	})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), "external_check", wakeRef, "event:initial")
	if err != nil {
		t.Fatal(err)
	}
	dueAt := time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)
	waiting, err := store.FSM().ResolveReview(lifecycle.WorkID(item.ID), opened.Review.EventID, lifecycle.ResolveReviewInput{
		Disposition: lifecycle.DispositionWait, WakeKind: lifecycle.WakeDueRetry,
		WakeRef: wakeRef, NextAttemptAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.FSM().Close()
	now = dueAt
	store.now = func() time.Time { return now }
	if err := store.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || canonical.Review == nil || canonical.Review.Reason != "retry_due" || canonical.Wake != nil ||
		canonical.Revision != waiting.Revision+1 {
		t.Fatalf("overdue canonical state=%+v waiting_revision=%d err=%v", canonical, waiting.Revision, err)
	}
	eventID, openedRevision := canonical.Review.EventID, canonical.Revision
	projected, err := store.Work(item.ID)
	if err != nil || projected.Review == nil || projected.Review.EventID != eventID || projected.Revision != openedRevision {
		t.Fatalf("overdue projection=%+v canonical=%+v err=%v", projected, canonical, err)
	}
	fw := &fakeWatcher{
		sessions:         map[string]*classifier.Agent{hostID: {ID: hostID, Hidden: true, State: classifier.StateDone}},
		ownedGenerations: map[string]string{hostID: "host-generation"},
		outcomes:         map[string]watcher.InputOutcome{}, turnStore: store,
	}
	service := NewService(store, fw, nil)
	if delivered, err := service.ReconcileHostLane(); err != nil || !delivered {
		t.Fatalf("idle Host delivery=%v err=%v", delivered, err)
	}
	after, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || after.Review == nil || after.Review.EventID != eventID ||
		after.Review.Handler == nil || after.Review.Handler.DeliveredAt == nil || len(fw.sentCalls) != 1 {
		t.Fatalf("delivered state=%+v sends=%d err=%v", after, len(fw.sentCalls), err)
	}
	deliveredRevision := after.Revision
	deliveredProjection, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		if err := store.SweepLifecycle(); err != nil {
			t.Fatal(err)
		}
		if delivered, err := service.ReconcileHostLane(); err != nil || delivered {
			t.Fatalf("repeat %d delivery=%v err=%v", i, delivered, err)
		}
	}
	after, _ = store.FSM().State(lifecycle.WorkID(item.ID))
	stableProjection, err := store.Work(item.ID)
	if err != nil || after.Revision != deliveredRevision || !stableProjection.UpdatedAt.Equal(deliveredProjection.UpdatedAt) ||
		len(fw.sentCalls) != 1 || len(fw.created) != 0 {
		t.Fatalf("repeat churn revision=%d want=%d updated=%v want=%v sends=%d sessions=%d err=%v",
			after.Revision, deliveredRevision, stableProjection.UpdatedAt, deliveredProjection.UpdatedAt,
			len(fw.sentCalls), len(fw.created), err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.FSM().Close()
	fw.turnStore = reopened
	if err := reopened.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	if delivered, err := NewService(reopened, fw, nil).ReconcileHostLane(); err != nil || delivered {
		t.Fatalf("reload delivery=%v err=%v", delivered, err)
	}
	reloaded, _ := reopened.FSM().State(lifecycle.WorkID(item.ID))
	if reloaded.Revision != deliveredRevision || len(fw.sentCalls) != 1 {
		t.Fatalf("reload churn revision=%d want=%d sends=%d", reloaded.Revision, deliveredRevision, len(fw.sentCalls))
	}
}

func TestIdleHostDeliversOpenReviewsInOldestFirstOrder(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.FSM().Close()
	now := time.Date(2026, 8, 23, 4, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	const hostID = "brain-host:@fair-review"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	createReview := func(title, eventID string) Work {
		item, createErr := store.CreateWork(Work{
			Title: title, Objective: "deliver fairly", CompletionPolicy: CompletionBounded,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, openErr := store.FSM().OpenReviewEvent(
			lifecycle.WorkID(item.ID), "operator_review", title, eventID,
		); openErr != nil {
			t.Fatal(openErr)
		}
		if syncErr := store.SyncWorkProjection(item.ID); syncErr != nil {
			t.Fatal(syncErr)
		}
		return item
	}
	oldest := createReview("oldest", "event-fair-oldest")
	now = now.Add(time.Second)
	newest := createReview("newest", "event-fair-newest")
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
		ownedGenerations: map[string]string{hostID: "host-generation"},
		outcomes:         map[string]watcher.InputOutcome{}, turnStore: store,
	}
	service := NewService(store, fw, nil)
	if delivered, deliverErr := service.ReconcileHostLane(); deliverErr != nil || !delivered {
		t.Fatalf("oldest delivery=%v err=%v", delivered, deliverErr)
	}
	resolveDeliveredReview(t, store, oldest.ID, WorkDispositionComplete)
	next, claimed, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !claimed || next.WorkID != newest.ID {
		t.Fatalf("next fair claim=%+v claimed=%v err=%v", next, claimed, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("fair delivery order=%+v", fw.sentCalls)
	}
}

func TestReleaseSessionAttemptAfterClose(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "close owner", Objective: "release owner when its Session closes",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := lifecycle.TurnToken("turn-close-owner")
	if _, _, err := store.FSM().AdmitTurn(lifecycle.WorkID(item.ID), lifecycle.AdmitTurnInput{
		SessionID: "worker:@1", Delegated: true, TurnToken: token,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	changed, err := store.ReleaseSessionAttempt("worker:@1", "session_closed")
	if err != nil || !changed {
		t.Fatalf("release changed=%v err=%v", changed, err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempt != nil || projected.AttemptSessionID != "" || projected.AttemptDelegated {
		t.Fatalf("state=%+v projected=%+v", state, projected)
	}
	changed, err = store.ReleaseSessionAttempt("worker:@1", "session_closed")
	if err != nil || changed {
		t.Fatalf("duplicate release changed=%v err=%v", changed, err)
	}
}

func TestMarkDeliveredCanonicalReviewWithoutProjectedEventIsIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "ambiguous canonical review", Objective: "resolve without projected event or submission rows",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().OpenReview(lifecycle.WorkID(item.ID), "operator_review", "canonical-only"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextReviewAction("host:@1")
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v action=%+v", ok, err, claimed)
	}
	if _, found, err := store.ResolveReviewLease(
		item.ID, ReviewLeaseMarkDelivered, "operator", "visible in Host transcript",
	); err != nil || !found {
		t.Fatalf("mark_delivered found=%v err=%v", found, err)
	}
	if _, _, err := store.ResolveReviewLease(
		item.ID, ReviewLeaseMarkDelivered, "operator", "visible in Host transcript",
	); err != nil {
		t.Fatalf("mark_delivered retry: %v", err)
	}
	if gated, err := store.HasLiveDeliveredReview(); err != nil || gated {
		t.Fatalf("Host lane remained gated: gated=%v err=%v", gated, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := reopened.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Review == nil || projected.Review.Lease != nil {
		t.Fatalf("replayed projection retained handler: %+v", projected.Review)
	}
}

func TestUntilDoneTerminalSweepsNeverCreateSessionsAndBrainAdmitsOneScopedAttempt(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "brain-owned continuation", Objective: "finish every required phase",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "all phases verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstToken := lifecycle.TurnToken("initial-turn")
	_, admitted, err := store.FSM().AdmitTurn(lifecycle.WorkID(item.ID), lifecycle.AdmitTurnInput{
		SessionID: "worker:@1", Delegated: true, TurnToken: firstToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := lifecycle.AttemptIdentity{SessionID: "worker:@1", TurnToken: firstToken, Fence: 1}
	if _, err := store.FSM().ReportTurnDone(lifecycle.WorkID(item.ID), identity, lifecycle.DoneInput{
		OK: true, Summary: "phase complete", CriteriaMet: false,
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || pending.Attempt != nil || pending.Review == nil || pending.Revision != admitted.Revision+1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	eventID, terminalRevision := pending.Review.EventID, pending.Revision
	if cards := store.FSM().Cards(); len(cards) != 1 || !cards[0].Actionable || cards[0].Reason != "turn_done" {
		t.Fatalf("terminal cards=%+v", cards)
	}

	fw := &fakeWatcher{turnStore: store}
	service := NewService(store, fw, nil)
	const sweeps = 64
	errs := make(chan error, sweeps)
	var wg sync.WaitGroup
	for i := 0; i < sweeps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				sweepErr := store.FSM().Sweep()
				errs <- sweepErr
				return
			}
			_, duplicateErr := store.FSM().ReportTurnDone(lifecycle.WorkID(item.ID), identity, lifecycle.DoneInput{
				OK: true, Summary: "phase complete", CriteriaMet: false,
			})
			errs <- duplicateErr
		}(i)
	}
	wg.Wait()
	close(errs)
	for sweepErr := range errs {
		if sweepErr != nil {
			t.Fatal(sweepErr)
		}
	}
	service.ReconcileDelegatedSessions(nil)
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != terminalRevision || state.Review == nil || state.Review.EventID != eventID ||
		len(fw.created) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("sweeps changed terminal obligation: state=%+v created=%+v sent=%+v", state, fw.created, fw.sentCalls)
	}
	if unrelated, clearErr := store.FSM().ClearWait(lifecycle.WorkID(item.ID), lifecycle.WakeUserInput, "unrelated", "message:1"); clearErr != nil || unrelated.Revision != terminalRevision {
		t.Fatalf("unrelated conversation changed Work: state=%+v err=%v", unrelated, clearErr)
	}

	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw = &fakeWatcher{turnStore: store}
	service = NewService(store, fw, nil)
	for i := 0; i < sweeps; i++ {
		if err := store.FSM().Sweep(); err != nil {
			t.Fatal(err)
		}
	}
	service.ReconcileDelegatedSessions(nil)
	reloaded, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || reloaded.Revision != terminalRevision || reloaded.Review == nil ||
		reloaded.Review.EventID != eventID || len(fw.created) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("reload changed terminal obligation: state=%+v created=%+v sent=%+v err=%v", reloaded, fw.created, fw.sentCalls, err)
	}

	claimed, err := store.FSM().ClaimReview(lifecycle.WorkID(item.ID), "brain-handler", "brain-turn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().MarkReviewDelivered(lifecycle.WorkID(item.ID), "brain-turn"); err != nil {
		t.Fatal(err)
	}
	const scopedConcern = "Verify the remaining migration edge and report concrete evidence."
	if _, _, err := store.FSM().PrepareAdmission(lifecycle.WorkID(item.ID), lifecycle.PrepareAdmissionInput{
		SessionID: "worker:@2", TurnToken: "turn-scoped", Receipt: scopedConcern,
		PayloadSHA256: "digest-scoped", ProcessIdentity: "process-2", PaneGeneration: "pane-2",
		Mode: lifecycle.AdmissionFresh, Purpose: lifecycle.AdmissionPurposeReview,
		PurposeID: claimed.Review.Handler.HandlerID, AttemptedAt: time.Now().UTC(), SignalProtocol: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().AcceptAdmissionBySignal(lifecycle.WorkID(item.ID), "turn-scoped", "worker:@2"); err != nil {
		t.Fatal(err)
	}
	continued, err := store.FSM().AcceptReviewFollowUp(lifecycle.WorkID(item.ID), eventID, "worker:@2", "turn-scoped")
	if err != nil {
		t.Fatal(err)
	}
	if continued.Review != nil || continued.Attempt == nil || continued.Attempt.SessionID != "worker:@2" ||
		continued.Attempt.TurnToken != "turn-scoped" ||
		len(fw.created) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("explicit Brain continuation=%+v created=%+v sent=%+v", continued, fw.created, fw.sentCalls)
	}
}

func TestLifecycleStoreRejectsAdmissionWithoutAttemptedAt(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "historical admission", Objective: "daemon must start after replay",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 22, 10, 34, 42, 661032855, time.UTC)
	sessionID := "brain-agent-zen-lifecycle-live-admission-review:@409"
	turnID := "turn:87c25477-6589-4b56-b304-6f27728e831a"
	pending, created, err := store.PrepareInputAdmission(delegatedSubmissionCandidate(
		item.ID, sessionID, turnID, "historical payload", acceptedAt,
	))
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	resolveDelegatedSubmission(t, store, pending, "activity-historical", acceptedAt.Add(3*time.Second))
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}

	lifecycleRoot := filepath.Join(root, "state", "lifecycle")
	stripLifecycleAttemptedAt(t, lifecycleRoot)
	if _, err := lifecycle.Open(lifecycleRoot); err == nil {
		t.Fatal("missing attempted_at was accepted")
	}
}

func TestProjectAcceptedAdmissionRejectsZeroAttemptedAt(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "projection repair", Objective: "keep daemon writable",
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	preparedAt := time.Date(2026, 8, 22, 10, 34, 42, 661032855, time.UTC)
	sessionID := "session-projection-repair:@1"
	turnID := "turn:projection-repair"
	digest := pendingSubmissionDigest("projection-repair")
	if err := store.fsmAdmitTurn(item.ID, sessionID, turnID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	st, err := store.fsmState(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	admission := &lifecycle.AdmissionState{
		TurnToken: lifecycle.TurnToken(turnID), SessionID: sessionID, Receipt: turnID,
		PayloadSHA256: digest, ProcessIdentity: "process-1", PaneGeneration: "pane-1",
		Mode: lifecycle.AdmissionFresh, Status: lifecycle.AdmissionAccepted,
		PreparedAt: preparedAt, ResultTurnToken: lifecycle.TurnToken(turnID),
	}
	if err := store.projectAcceptedAdmission(st, admission, watcher.InputAdmissionResolution{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID, PayloadSHA256: digest,
		ActivityID: "activity-1", ResolvedAt: preparedAt.Add(time.Second),
		Admission: watcher.TurnAdmission{Stream: "provider", ID: "admission-1", Cursor: 1, SHA256: digest, At: preparedAt},
	}); err == nil {
		t.Fatal("zero attempted_at was accepted")
	}
}

func stripLifecycleAttemptedAt(t *testing.T, lifecycleRoot string) {
	t.Helper()
	path := filepath.Join(lifecycleRoot, "state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var database map[string]any
	if err := json.Unmarshal(raw, &database); err != nil {
		t.Fatal(err)
	}
	works, _ := database["works"].(map[string]any)
	for _, value := range works {
		work, _ := value.(map[string]any)
		admission, _ := work["admission"].(map[string]any)
		delete(admission, "attempted_at")
	}
	rebuilt, err := json.Marshal(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rebuilt, 0o600); err != nil {
		t.Fatal(err)
	}
}
