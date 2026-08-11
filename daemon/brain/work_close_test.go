package brain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func TestCloseWorkSettlesQueuedAttentionAndPersistsAudit(t *testing.T) {
	for _, status := range []WorkStatus{WorkDone, WorkCancelled} {
		t.Run(string(status), func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{
				Title: "obsolete work", Objective: "close without Host delivery",
				Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.AppendWorkEvent(WorkEvent{
				WorkID: item.ID, Kind: "brain.reconcile_required", DedupeKey: "close-fixture",
				Actionable: true,
			}); err != nil {
				t.Fatal(err)
			}
			item, err = store.Work(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeRevision := item.Revision
			closed, err := store.CloseWork(WorkCloseRequest{
				WorkID: item.ID, ExpectedRevision: beforeRevision, Status: status,
				Actor: "brain", Reason: "verified historical work with no live owner",
			})
			if err != nil {
				t.Fatal(err)
			}
			if closed.Status != status || closed.Revision != beforeRevision+1 || closed.Wake != nil ||
				closed.WaitFor != "" || closed.NextAction != "" || closed.SuccessorReservation != nil {
				t.Fatalf("closed Work = %+v", closed)
			}
			events, err := store.ListWorkEvents(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			var audit WorkEvent
			settled := 0
			for _, event := range events {
				if event.Kind == "brain.work_closed" {
					audit = event
					continue
				}
				if event.Actionable && event.Resolution == EventResolutionDiscard &&
					event.ResolvedBy == "brain" && event.DiscardedAt != nil && event.ResolvedAt != nil {
					settled++
				}
			}
			if settled == 0 {
				t.Fatalf("queued Attention was not settled: %#v", events)
			}
			if audit.ID == "" || audit.Actionable || audit.SourceName != "brain" ||
				audit.Summary != "verified historical work with no live owner" ||
				audit.WorkRevision != closed.Revision ||
				!strings.Contains(audit.DedupeKey, string(status)) {
				t.Fatalf("close audit = %#v", audit)
			}
			if _, ok, err := store.ClaimNextActionableEvent("host"); err != nil || ok {
				t.Fatalf("terminal Work remained claimable: ok=%v err=%v", ok, err)
			}

			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			after, err := reopened.Work(item.ID)
			if err != nil || after.Status != status || after.Revision != closed.Revision {
				t.Fatalf("reopened Work = %+v err=%v", after, err)
			}
		})
	}
}

func TestCloseWorkRequiresExactActorRevisionAndNonterminalWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "close contract", Objective: "fail closed", Status: WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := WorkCloseRequest{
		WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled,
		Actor: "user", Reason: "explicit cleanup",
	}
	for _, mutate := range []func(*WorkCloseRequest){
		func(request *WorkCloseRequest) { request.Actor = "" },
		func(request *WorkCloseRequest) { request.Reason = "" },
		func(request *WorkCloseRequest) { request.ExpectedRevision = 0 },
		func(request *WorkCloseRequest) { request.Status = WorkRunning },
	} {
		request := base
		mutate(&request)
		if _, err := store.CloseWork(request); err == nil {
			t.Fatalf("invalid close accepted: %+v", request)
		}
	}
	stale := base
	stale.ExpectedRevision++
	if _, err := store.CloseWork(stale); !errors.Is(err, ErrWorkRevisionConflict) {
		t.Fatalf("stale close err=%v want ErrWorkRevisionConflict", err)
	}
	closed, err := store.CloseWork(base)
	if err != nil {
		t.Fatal(err)
	}
	repeat := base
	repeat.ExpectedRevision = closed.Revision
	if _, err := store.CloseWork(repeat); !errors.Is(err, ErrWorkCloseConflict) {
		t.Fatalf("terminal re-close err=%v want ErrWorkCloseConflict", err)
	}
}

func TestCloseWorkRejectsClaimedAndRetiresProviderPendingAuthority(t *testing.T) {
	t.Run("claimed Attention", func(t *testing.T) {
		store, _, event := claimResolutionStore(t)
		item, err := store.Work(event.WorkID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CloseWork(WorkCloseRequest{
			WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled,
			Actor: "brain", Reason: "must not bypass Host claim",
		})
		if !errors.Is(err, ErrWorkCloseConflict) {
			t.Fatalf("claimed close err=%v want ErrWorkCloseConflict", err)
		}
		row, found, lookupErr := store.WorkEvent(event.ID)
		if lookupErr != nil || !found || row.ClaimedAt == nil || row.Resolution != "" {
			t.Fatalf("claim mutated on rejected close: row=%+v found=%v err=%v", row, found, lookupErr)
		}
	})

	t.Run("delivered Host handling", func(t *testing.T) {
		store, _, claimed := claimResolutionStore(t)
		delivered := admitAndConsumeHostClaim(t, store, claimed)
		item, err := store.Work(delivered.WorkID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CloseWork(WorkCloseRequest{
			WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled,
			Actor: "brain", Reason: "must not bypass admitted Host handling",
		})
		if !errors.Is(err, ErrWorkCloseConflict) {
			t.Fatalf("delivered close err=%v want ErrWorkCloseConflict", err)
		}
		row, found, lookupErr := store.WorkEvent(delivered.ID)
		if lookupErr != nil || !found || row.DeliveredAt == nil || row.HandlingEndedAt != nil || row.HandledAt != nil {
			t.Fatalf("handling mutated on rejected close: row=%+v found=%v err=%v", row, found, lookupErr)
		}
		resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
			Summary: "exact Host handling remains authoritative",
		})
		if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone {
			t.Fatalf("exact disposition after rejected close: event=%+v Work=%+v err=%v", resolved, terminal, err)
		}
	})

	t.Run("pending provider submission", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		item, err := store.CreateWork(Work{
			Title: "provider pending", Objective: "retain ambiguous authority",
			Status: WorkRunning, CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
			WorkID: item.ID, SessionID: "brain-agent-pending:@1", ProposedTurnID: "turn:pending",
			Receipt: "turn:pending", PayloadSHA256: strings.Repeat("a", 64),
			ProcessIdentity: "process", PaneGeneration: "generation",
			AcceptedAt: time.Now().UTC(), Mode: watcher.TurnSubmissionFresh,
		})
		if err != nil || !created {
			t.Fatalf("prepare pending created=%v err=%v", created, err)
		}
		current, err := store.Work(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		closed, err := store.CloseWork(WorkCloseRequest{
			WorkID: item.ID, ExpectedRevision: current.Revision, Status: WorkCancelled,
			Actor: "user", Reason: "explicitly revoke provider admission authority",
		})
		if err != nil || closed.Status != WorkCancelled || len(closed.SessionFinalizations) != 1 ||
			closed.SessionFinalizations[0].SessionID != "brain-agent-pending:@1" ||
			closed.SessionFinalizations[0].State != SessionFinalizationPending {
			t.Fatalf("pending submission close=%+v err=%v", closed, err)
		}
		retired, found, err := store.TurnSubmission("brain-agent-pending:@1", "turn:pending")
		if err != nil || !found || retired.State != watcher.TurnSubmissionRetired {
			t.Fatalf("retired submission=%+v found=%v err=%v", retired, found, err)
		}
		if _, found, err := store.Turn("brain-agent-pending:@1"); err != nil || found {
			t.Fatalf("actor retirement created a canonical Turn: found=%v err=%v", found, err)
		}
		if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
			SessionID: "brain-agent-pending:@1", ProposedTurnID: "turn:pending", Receipt: "turn:pending",
			PayloadSHA256: strings.Repeat("a", 64), ActivityID: "late-activity",
			Admission: watcher.TurnAdmission{
				Stream: "provider", ID: "late-admission", Cursor: 1,
				SHA256: strings.Repeat("a", 64), At: time.Now().UTC(),
			},
			ResolvedAt: time.Now().UTC(),
		}); err == nil || !strings.Contains(err.Error(), "never be adopted") {
			t.Fatalf("retired submission accepted late provider evidence: %v", err)
		}
		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		stable, found, err := reopened.TurnSubmission("brain-agent-pending:@1", "turn:pending")
		if err != nil || !found || stable.State != watcher.TurnSubmissionRetired {
			t.Fatalf("reopened retirement=%+v found=%v err=%v", stable, found, err)
		}
	})
}

func TestUpdateWorkTerminalTransitionRejectsActiveHostLane(t *testing.T) {
	t.Run("claimed", func(t *testing.T) {
		store, workID, claimed := claimResolutionStore(t)
		before, err := store.Work(workID)
		if err != nil {
			t.Fatal(err)
		}
		done := WorkDone
		if _, err := store.UpdateWork(workID, WorkUpdate{Status: &done}); !errors.Is(err, ErrWorkConflict) {
			t.Fatalf("terminal update err=%v want ErrWorkConflict", err)
		}
		after, err := store.Work(workID)
		if err != nil || after.Status != before.Status || after.Revision != before.Revision {
			t.Fatalf("rejected update changed Work: before=%+v after=%+v err=%v", before, after, err)
		}
		row, found, err := store.WorkEvent(claimed.ID)
		if err != nil || !found || row.ClaimedAt == nil || row.DeliveredAt != nil || row.Resolution != "" {
			t.Fatalf("rejected update changed claim: row=%+v found=%v err=%v", row, found, err)
		}
		delivered := admitAndConsumeHostClaim(t, store, claimed)
		resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
			Summary: "claim kept its exact revision authority",
		})
		if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone {
			t.Fatalf("exact disposition after rejected update: event=%+v Work=%+v err=%v", resolved, terminal, err)
		}
	})

	t.Run("delivered", func(t *testing.T) {
		store, workID, claimed := claimResolutionStore(t)
		delivered := admitAndConsumeHostClaim(t, store, claimed)
		before, err := store.Work(workID)
		if err != nil {
			t.Fatal(err)
		}
		cancelled := WorkCancelled
		if _, err := store.UpdateWork(workID, WorkUpdate{Status: &cancelled}); !errors.Is(err, ErrWorkConflict) {
			t.Fatalf("terminal update err=%v want ErrWorkConflict", err)
		}
		after, err := store.Work(workID)
		if err != nil || after.Status != before.Status || after.Revision != before.Revision {
			t.Fatalf("rejected update changed Work: before=%+v after=%+v err=%v", before, after, err)
		}
		row, found, err := store.WorkEvent(delivered.ID)
		if err != nil || !found || row.DeliveredAt == nil || row.HandlingEndedAt != nil || row.HandledAt != nil {
			t.Fatalf("rejected update changed handling: row=%+v found=%v err=%v", row, found, err)
		}
		resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionCancel,
			Summary: "delivered handling kept its exact revision authority",
		})
		if err != nil || resolved.HandledAt == nil || terminal.Status != WorkCancelled {
			t.Fatalf("exact disposition after rejected update: event=%+v Work=%+v err=%v", resolved, terminal, err)
		}
	})
}

func TestUpdateWorkMetadataRejectsActiveHostLane(t *testing.T) {
	t.Run("claimed", func(t *testing.T) {
		store, workID, claimed := claimResolutionStore(t)
		before, err := store.Work(workID)
		if err != nil {
			t.Fatal(err)
		}
		title := "must not invalidate a claimed revision"
		if _, err := store.UpdateWork(workID, WorkUpdate{Title: &title}); !errors.Is(err, ErrWorkConflict) {
			t.Fatalf("metadata update err=%v want ErrWorkConflict", err)
		}
		after, err := store.Work(workID)
		if err != nil || after.Title != before.Title || after.Revision != before.Revision {
			t.Fatalf("rejected update changed Work: before=%+v after=%+v err=%v", before, after, err)
		}
		delivered := admitAndConsumeHostClaim(t, store, claimed)
		resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
			Summary: "claim kept its exact revision authority",
		})
		if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone {
			t.Fatalf("exact disposition after rejected update: event=%+v Work=%+v err=%v", resolved, terminal, err)
		}
	})

	t.Run("delivered", func(t *testing.T) {
		store, workID, claimed := claimResolutionStore(t)
		delivered := admitAndConsumeHostClaim(t, store, claimed)
		before, err := store.Work(workID)
		if err != nil {
			t.Fatal(err)
		}
		nextAction := "must not invalidate a delivered revision"
		if _, err := store.UpdateWork(workID, WorkUpdate{NextAction: &nextAction}); !errors.Is(err, ErrWorkConflict) {
			t.Fatalf("metadata update err=%v want ErrWorkConflict", err)
		}
		after, err := store.Work(workID)
		if err != nil || after.NextAction != before.NextAction || after.Revision != before.Revision {
			t.Fatalf("rejected update changed Work: before=%+v after=%+v err=%v", before, after, err)
		}
		resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionCancel,
			Summary: "handling kept its exact revision authority",
		})
		if err != nil || resolved.HandledAt == nil || terminal.Status != WorkCancelled {
			t.Fatalf("exact disposition after rejected update: event=%+v Work=%+v err=%v", resolved, terminal, err)
		}
	})
}

func TestTerminalWorkPermitsExactHostClaimTransaction(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	if err == nil {
		index := workIndex(database.BrainWork, workID)
		if index < 0 {
			err = ErrWorkNotFound
		} else {
			database.BrainWork[index].Status = WorkDone
			database.BrainWork[index].TerminalRevision = database.BrainWork[index].Revision
			database.BrainWork[index].Wake = nil
			database.BrainWork[index].WaitFor = ""
			database.BrainWork[index].NextAction = ""
			err = store.persistOrchestrationLocked(database)
		}
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	delivered := admitAndConsumeHostClaim(t, store, claimed)
	resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
		Summary: "terminal finalization retry was handled exactly once",
	})
	if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone || terminal.Revision != claimed.DeliveryWorkRevision+1 {
		t.Fatalf("terminal Host transaction: event=%+v Work=%+v err=%v", resolved, terminal, err)
	}
	hostSubmission, found, err := store.TurnSubmission(claimed.DeliveryHostSessionID, claimed.ProviderTurnID)
	if err != nil || !found || hostSubmission.State != watcher.TurnSubmissionResolved {
		t.Fatalf("terminal Host submission=%+v found=%v err=%v", hostSubmission, found, err)
	}
}

func admitAndConsumeHostClaim(t *testing.T, store *Store, claimed WorkEvent) WorkEvent {
	t.Helper()
	acceptedAt := claimed.ClaimedAt.UTC()
	digest := strings.Repeat("6", 64)
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID,
		ProposedTurnID: claimed.ProviderTurnID, Receipt: claimed.ID, ClaimToken: claimed.HandlingID,
		PayloadSHA256: digest, ProcessIdentity: "host-lane-process", PaneGeneration: "host-lane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare Host claim created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, PayloadSHA256: digest, ActivityID: "host-lane-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-lane-admission", Cursor: 1, SHA256: digest, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatal(err)
	}
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return delivered
}

func TestCloseWorkPersistenceFailureExposesNoPartialTerminalState(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "atomic close", Objective: "all or nothing", Status: WorkNeedsInput,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	pendingSessionID := "brain-agent-atomic-close:@1"
	pendingTurnID := "turn:atomic-close"
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: item.ID, SessionID: pendingSessionID, ProposedTurnID: pendingTurnID,
		Receipt: pendingTurnID, PayloadSHA256: strings.Repeat("c", 64),
		ProcessIdentity: "atomic-process", PaneGeneration: "atomic-generation",
		AcceptedAt: time.Now().UTC(), Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare pending created=%v err=%v", created, err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "brain.reconcile_required", DedupeKey: "atomic-close",
		Actionable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.writeOrchestration = func(string, any) error { return errors.New("injected close write failure") }
	_, err = store.CloseWork(WorkCloseRequest{
		WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled,
		Actor: "brain", Reason: "atomic failure fixture",
	})
	if err == nil || !strings.Contains(err.Error(), "injected close write failure") {
		t.Fatalf("close err=%v", err)
	}
	store.writeOrchestration = writeJSONFile
	after, err := store.Work(item.ID)
	if err != nil || after.Status != item.Status || after.Revision != item.Revision {
		t.Fatalf("failed close exposed Work mutation: before=%+v after=%+v err=%v", item, after, err)
	}
	row, found, err := store.WorkEvent(event.ID)
	if err != nil || !found || row.Resolution != "" || row.DiscardedAt != nil {
		t.Fatalf("failed close exposed Event settlement: row=%+v found=%v err=%v", row, found, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range events {
		if candidate.Kind == "brain.work_closed" {
			t.Fatalf("failed close exposed audit row: %#v", events)
		}
	}
	pending, found, err := store.TurnSubmission(pendingSessionID, pendingTurnID)
	if err != nil || !found || pending.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed close exposed submission retirement: pending=%+v found=%v err=%v", pending, found, err)
	}
}

func TestUpdateWorkTerminalTransitionRetiresPendingSubmission(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "terminal update fence", Objective: "revoke pending provider authority",
		Status: WorkRunning, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-terminal-update:@1"
	turnID := "turn:terminal-update"
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: turnID,
		Receipt: turnID, PayloadSHA256: strings.Repeat("7", 64),
		ProcessIdentity: "terminal-update-process", PaneGeneration: "terminal-update-generation",
		AcceptedAt: time.Now().UTC(), Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare pending created=%v err=%v", created, err)
	}
	done := WorkDone
	terminal, err := store.UpdateWork(item.ID, WorkUpdate{Status: &done})
	if err != nil || terminal.Status != WorkDone || len(terminal.SessionFinalizations) != 1 ||
		terminal.SessionFinalizations[0].SessionID != sessionID {
		t.Fatalf("terminal update Work=%+v err=%v", terminal, err)
	}
	retired, found, err := store.TurnSubmission(sessionID, turnID)
	if err != nil || !found || retired.State != watcher.TurnSubmissionRetired {
		t.Fatalf("terminal update retirement=%+v found=%v err=%v", retired, found, err)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	stable, found, err := reopened.TurnSubmission(sessionID, turnID)
	if err != nil || !found || stable.State != watcher.TurnSubmissionRetired {
		t.Fatalf("reopened terminal update retirement=%+v found=%v err=%v", stable, found, err)
	}
}

func TestResolveWorkEventTerminalDispositionRetiresPendingSuccessor(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	acceptedAt := claimed.ClaimedAt.UTC()
	hostDigest := strings.Repeat("d", 64)
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID,
		ProposedTurnID: claimed.ProviderTurnID, Receipt: claimed.ID, ClaimToken: claimed.HandlingID,
		PayloadSHA256: hostDigest, ProcessIdentity: "host-process", PaneGeneration: "host-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare Host claim created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, PayloadSHA256: hostDigest, ActivityID: "host-terminal-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-terminal-admission", Cursor: 1,
			SHA256: hostDigest, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatal(err)
	}
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	successorSessionID := "brain-agent-terminal-successor:@1"
	successorTurnID := "turn:terminal-successor"
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: workID, SessionID: successorSessionID, ProposedTurnID: successorTurnID,
		Receipt: successorTurnID, PayloadSHA256: strings.Repeat("e", 64),
		ProcessIdentity: "successor-process", PaneGeneration: "successor-generation",
		AcceptedAt: resolvedAt.Add(time.Millisecond), Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare successor created=%v err=%v", created, err)
	}
	item, err := store.Work(workID)
	if err != nil || item.SuccessorReservation == nil || item.SuccessorReservation.ProviderTurnID != "" {
		t.Fatalf("unaccepted successor reservation=%+v err=%v", item, err)
	}
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error { return errors.New("injected terminal disposition write failure") }
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
		Summary: "failed atomic terminal disposition",
	}); err == nil || !strings.Contains(err.Error(), "injected terminal disposition write failure") {
		t.Fatalf("terminal disposition write failure=%v", err)
	}
	store.writeOrchestration = originalWrite
	unchanged, err := store.Work(workID)
	if err != nil || unchanged.Status == WorkDone || unchanged.SuccessorReservation == nil {
		t.Fatalf("failed disposition exposed Work mutation=%+v err=%v", unchanged, err)
	}
	unchangedEvent, found, err := store.WorkEvent(delivered.ID)
	if err != nil || !found || unchangedEvent.HandledAt != nil {
		t.Fatalf("failed disposition exposed Event settlement=%+v found=%v err=%v", unchangedEvent, found, err)
	}
	unchangedSubmission, found, err := store.TurnSubmission(successorSessionID, successorTurnID)
	if err != nil || !found || unchangedSubmission.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed disposition exposed submission retirement=%+v found=%v err=%v", unchangedSubmission, found, err)
	}
	resolved, terminal, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID, ProviderTurnID: delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision, Disposition: WorkDispositionComplete,
		Summary: "terminal disposition revokes the staged successor",
	})
	if err != nil || resolved.HandledAt == nil || terminal.Status != WorkDone || terminal.SuccessorReservation != nil {
		t.Fatalf("terminal disposition event=%+v Work=%+v err=%v", resolved, terminal, err)
	}
	pending, found, err := store.TurnSubmission(successorSessionID, successorTurnID)
	if err != nil || !found || pending.State != watcher.TurnSubmissionRetired {
		t.Fatalf("terminal successor retirement=%+v found=%v err=%v", pending, found, err)
	}
	finalized := false
	for _, finalization := range terminal.SessionFinalizations {
		if finalization.SessionID == successorSessionID && finalization.State == SessionFinalizationPending {
			finalized = true
		}
		if finalization.SessionID == claimed.DeliveryHostSessionID {
			t.Fatalf("Host claim Session was scheduled for delegated teardown: %+v", terminal.SessionFinalizations)
		}
	}
	if !finalized {
		t.Fatalf("pending successor has no terminal finalization: %+v", terminal.SessionFinalizations)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	stable, found, err := reopened.TurnSubmission(successorSessionID, successorTurnID)
	if err != nil || !found || stable.State != watcher.TurnSubmissionRetired {
		t.Fatalf("reopened terminal retirement=%+v found=%v err=%v", stable, found, err)
	}
}

func TestCloseWorkSettlesEndedHostHandlingWithoutReplayingIt(t *testing.T) {
	store, workID, claimed := claimResolutionStore(t)
	acceptedAt := claimed.ClaimedAt.UTC()
	digest := strings.Repeat("b", 64)
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID,
		ProposedTurnID: claimed.ProviderTurnID, Receipt: claimed.ID, ClaimToken: claimed.HandlingID,
		PayloadSHA256: digest, ProcessIdentity: "host-process", PaneGeneration: "host-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare Host claim created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, PayloadSHA256: digest, ActivityID: "host-close-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-close-admission", Cursor: 1, SHA256: digest, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatal(err)
	}
	delivered, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	successorSessionID := "brain-agent-ended-successor:@1"
	successorTurnID := "turn:ended-successor"
	if _, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: workID, SessionID: successorSessionID, ProposedTurnID: successorTurnID,
		Receipt: successorTurnID, PayloadSHA256: strings.Repeat("f", 64),
		ProcessIdentity: "ended-successor-process", PaneGeneration: "ended-successor-generation",
		AcceptedAt: resolvedAt.Add(time.Millisecond), Mode: watcher.TurnSubmissionFresh,
	}); err != nil || !created {
		t.Fatalf("prepare ended successor created=%v err=%v", created, err)
	}
	reconcile, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, delivered.ProviderTurnID,
	)
	if err != nil || !created {
		t.Fatalf("requeue=%+v created=%v err=%v", reconcile, created, err)
	}
	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.SuccessorReservation == nil || item.SuccessorReservation.SessionID != successorSessionID ||
		item.SuccessorReservation.ProviderTurnID != "" ||
		item.SuccessorReservation.EventID != "" || item.SuccessorReservation.HandlingID != "" {
		t.Fatalf("requeued unaccepted successor=%+v", item.SuccessorReservation)
	}
	closed, err := store.CloseWork(WorkCloseRequest{
		WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkDone,
		Actor: "brain", Reason: "operator verified the ended handling outcome",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != WorkDone {
		t.Fatalf("closed Work = %+v", closed)
	}
	retired, found, err := store.TurnSubmission(successorSessionID, successorTurnID)
	if err != nil || !found || retired.State != watcher.TurnSubmissionRetired {
		t.Fatalf("ended successor retirement=%+v found=%v err=%v", retired, found, err)
	}
	if closed.SuccessorReservation != nil {
		t.Fatalf("closed Work retained successor reservation=%+v", closed.SuccessorReservation)
	}
	original, found, err := store.WorkEvent(delivered.ID)
	if err != nil || !found || original.HandledAt == nil || original.Disposition != WorkDispositionComplete ||
		original.DispositionSummary != "operator verified the ended handling outcome" {
		t.Fatalf("ended handling settlement = %+v found=%v err=%v", original, found, err)
	}
	queued, found, err := store.WorkEvent(reconcile.ID)
	if err != nil || !found || queued.Resolution != EventResolutionDiscard || queued.ResolvedBy != "brain" {
		t.Fatalf("queued reconciliation settlement = %+v found=%v err=%v", queued, found, err)
	}
}
