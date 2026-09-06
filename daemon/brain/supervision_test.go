package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestActiveReviewWaitPreservesOwnerAndFailedHandlingDoesNotLoop(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact_wait", true: "failed_wait"}[failed], func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item := createSignalTestWork(t, store, "active producer", "worker")
			bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{SessionID: "worker", TurnID: "worker-turn", AcceptedAt: time.Now().UTC()})
			// Exercise an already-admitted exception, not a new lease producer.
			if _, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), "lease_expired", "worker-turn", "lease-event"); err != nil {
				t.Fatal(err)
			}
			if err := store.SyncWorkProjection(item.ID); err != nil {
				t.Fatal(err)
			}
			action, _ := deliverSignalTestEvent(t, store, "host")
			before, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			request := WorkReviewDispositionRequest{WorkID: item.ID, HandlingID: action.HandlingID, ProviderTurnID: action.ProviderTurnID, ExpectedWorkRevision: action.DeliveryWorkRevision, Disposition: WorkDispositionWait, Wake: &WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef("worker", "worker-turn")}}
			wrongRevision := request
			wrongRevision.ExpectedWorkRevision++
			if _, _, err := store.ResolveWorkReview(wrongRevision); !errors.Is(err, ErrEventClaim) {
				t.Fatalf("wrong delivery revision accepted: %v", err)
			}
			if failed {
				request.Wake.Ref = "worker"
			}
			_, _, err = store.ResolveWorkReview(request)
			if failed {
				if !errors.Is(err, ErrWorkAttemptConflict) {
					t.Fatalf("invalid wait: %v", err)
				}
				if _, _, err := store.EndReviewDelivery(item.ID, action.HandlingID, action.ProviderTurnID); err != nil {
					t.Fatal(err)
				}
				for range 30 {
					if _, claimed, err := store.ClaimNextReviewAction("host"); err != nil || claimed {
						t.Fatalf("unchanged exception reentered Brain: %v %v", claimed, err)
					}
				}
				request.Wake.Ref = SessionTerminalWakeRef("worker", "worker-turn")
				if _, _, err := store.ResolveWorkReview(request); !errors.Is(err, ErrEventClaim) {
					t.Fatalf("ended capability accepted: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			if after.Attempt == nil || after.Attempt.Generation != before.Attempt.Generation || after.Wake != nil {
				t.Fatalf("ownership changed: %+v", after)
			}
			if _, err := store.FSM().ReportTurnDone(lifecycle.WorkID(item.ID), lifecycle.AttemptIdentity{SessionID: "worker", TurnToken: "worker-turn", Fence: after.Attempt.Generation}, lifecycle.DoneInput{OK: true, Summary: "completed"}); err != nil {
				t.Fatal(err)
			}
			if err := store.SyncWorkProjection(item.ID); err != nil {
				t.Fatal(err)
			}
			terminal, claimed, err := store.ClaimNextReviewAction("host")
			if err != nil || !claimed || terminal.EventID == action.EventID || terminal.Kind != "turn_done" {
				t.Fatalf("terminal decision=%+v %v %v", terminal, claimed, err)
			}
			if _, _, err := store.ResolveWorkReview(request); err == nil {
				t.Fatal("stale handling resolved replacement event")
			}
		})
	}
}

func TestExactDelegatedTerminalAfterLossSupersedesEndedDecision(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	now := base
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{Title: "late exact result", Objective: "settle without stale attention", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "Brain accepts result"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := delegatedSubmissionCandidate(item.ID, "worker", "exact-turn", "task", base)
	candidate.SignalProtocol = true
	if _, _, err := store.PrepareInputAdmission(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "exact-turn", Class: watcher.EvidenceControl, Kind: "running", SourceID: "running", At: base}); err != nil {
		t.Fatal(err)
	}
	st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	now = st.Attempt.LeaseDeadline.Add(lifecycle.LostGrace + time.Second)
	if err := store.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	lost, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if lost.Review == nil || lost.Review.Reason != "turn_lost" {
		t.Fatalf("loss=%+v", lost)
	}
	action, _ := deliverSignalTestEvent(t, store, "host")
	if _, _, err := store.EndReviewDelivery(item.ID, action.HandlingID, action.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "exact-turn", Class: watcher.EvidenceControl, Kind: "done", SourceID: "exact-result", Summary: "result requires acceptance", At: now})
	if err != nil || !result.Matched || !result.Changed {
		t.Fatalf("late result=%+v %v", result, err)
	}
	final, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if final.Attempt != nil || final.Review == nil || final.Review.Reason != "turn_done" || final.Review.EventID == lost.Review.EventID || final.Review.Handler != nil {
		t.Fatalf("late result swallowed by ended loss decision: %+v", final)
	}
}

func TestSupervisorFreshProgressVersusCachedRunningAndDeadProducer(t *testing.T) {
	for _, mode := range []string{"fresh", "cached", "dead"} {
		t.Run(mode, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			base := time.Now().UTC()
			now := base
			store.now = func() time.Time { return now }
			item := createSignalTestWork(t, store, "supervised producer", "worker")
			bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{SessionID: "worker", TurnID: "worker-turn", AcceptedAt: base})
			initial, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			now = initial.Attempt.LeaseDeadline.Add(time.Second)
			worker := &classifier.Worker{ID: "worker", PaneAlive: true, ProcessID: 123, State: classifier.StateRunning}
			observation := watcher.ProviderActivityObservation{ID: "activity", Status: "running", StartedAt: base, ProgressAt: base}
			if mode == "fresh" {
				observation.ProgressAt = now
			}
			fw := &fakeWatcher{providerEvidence: map[string]watcher.ProviderActivityObservation{"worker": observation}}
			service := NewService(store, fw, nil)
			service.now = func() time.Time { return now }
			workers := []*classifier.Worker{worker}
			if mode == "dead" {
				workers = nil
			}
			service.ReconcileDelegatedSessions(workers)
			st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			if mode == "dead" {
				if st.Attempt != nil || st.Review == nil || st.Status == lifecycle.StatusDone {
					t.Fatalf("dead producer=%+v", st)
				}
				return
			}
			if st.Attempt == nil || st.Review != nil {
				t.Fatalf("expiry requested decision: %+v", st)
			}
			if mode == "fresh" && !st.Attempt.LeaseDeadline.After(now) {
				t.Fatal("fresh source progress did not renew")
			}
			// No new provider facts: the old running status must eventually lose ownership.
			now = st.Attempt.LeaseDeadline.Add(lifecycle.LostGrace + time.Second)
			service.ReconcileDelegatedSessions(workers)
			lost, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			if lost.Attempt != nil || lost.Review == nil || lost.Review.Reason != "turn_lost" {
				t.Fatalf("cached running hid hung producer: %+v", lost)
			}
		})
	}
}
