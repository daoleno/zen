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

func TestCloseWorkRejectsClaimedOrProviderPendingAuthority(t *testing.T) {
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
		_, err = store.CloseWork(WorkCloseRequest{
			WorkID: item.ID, ExpectedRevision: current.Revision, Status: WorkCancelled,
			Actor: "user", Reason: "must not guess provider admission",
		})
		if !errors.Is(err, ErrWorkCloseConflict) {
			t.Fatalf("pending submission close err=%v want ErrWorkCloseConflict", err)
		}
	})
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
