package brain

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestCompletionLoopDurableDecisionCleanup(t *testing.T) {
	for _, scenario := range []string{"done", "failed", "accept-before-cleanup-crash", "cleanup-failure", "reused-session"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			const hostID, workerID = "host:@loop", "worker:@loop"
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
				hostID:       {ID: hostID, Hidden: true, State: classifier.StateDone},
				workerID:     {ID: workerID, Delegated: true, State: classifier.StateDone},
				"user:@keep": {ID: "user:@keep", State: classifier.StateDone},
			}, outcomes: map[string]watcher.InputOutcome{}, turnStore: store}
			service := NewService(store, fw, nil)
			item, err := store.CreateWork(Work{Title: scenario, Objective: "Brain decides", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "verified result accepted"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.SubmitDelegatedWorkInput(workerID, "new test task", item.ID, "turn-worker", "", "", time.Now().Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			kind := "done"
			if scenario == "failed" {
				kind = "failed"
			}
			fact := watcher.TurnFact{SessionID: workerID, TurnID: "turn-worker", Class: watcher.EvidenceControl, Kind: kind, SourceID: "report", Summary: "exact " + kind + " result", At: time.Now()}
			id, changes := service.SubscribeWork()
			defer service.UnsubscribeWork(id)
			if result, err := service.ApplyDelegatedTurnProgress(fact); err != nil || !result.Changed {
				t.Fatalf("report: %+v %v", result, err)
			}
			// Production server consumes this durable event, not a timer or status poll.
			select {
			case <-changes:
			case <-time.After(time.Second):
				t.Fatal("terminal persistence emitted no wake")
			}
			if err := service.ReconcileWorkChange(); err != nil {
				t.Fatal(err)
			}
			lease := requireReviewDelivered(t, store, item.ID)
			before, _ := store.Work(item.ID)
			if before.Status == WorkDone || !fw.HasSession(workerID) {
				t.Fatal("provider result was implicitly accepted or cleaned")
			}
			for _, source := range []string{"report", "duplicate-control", "duplicate-provider"} {
				fact.SourceID = source
				var changed bool
				if source == "duplicate-provider" {
					fact.Class, fact.Bound = watcher.EvidenceProvider, true
					_, changed, err = service.ApplyTurnFact(fact)
				} else {
					result, applyErr := service.ApplyDelegatedTurnProgress(fact)
					err, changed = applyErr, result.Changed
					if !result.Matched {
						t.Fatal("duplicate current result rejected")
					}
				}
				if err != nil || changed {
					t.Fatalf("duplicate mutated result: %v %v", changed, err)
				}
			}
			if scenario == "failed" {
				event, _, _ := store.WorkEvent(before.Review.EventID)
				if event.Kind != "turn_failed" || event.Summary != "exact failed result" {
					t.Fatalf("failure misrepresented: %+v", event)
				}
			}
			status := WorkDone
			if scenario == "failed" {
				status = WorkCancelled
			}
			if scenario == "cleanup-failure" {
				fw.killErr, fw.killLeavesLive = errors.New("injected teardown failure"), true
			}
			if scenario == "accept-before-cleanup-crash" || scenario == "reused-session" {
				// Commit exactly the production Store decision, then lose the Service.
				_, err = store.UpdateWork(item.ID, WorkUpdate{Status: &status})
			} else {
				_, err = service.UpdateWork(item.ID, WorkUpdate{Status: &status})
			}
			if scenario == "cleanup-failure" {
				if err == nil || !fw.HasSession(workerID) {
					t.Fatal("cleanup failure hidden")
				}
				fw.killErr, fw.killLeavesLive = nil, false
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario == "reused-session" {
				other, err := store.CreateWork(Work{Title: "new owner", Objective: "must survive old cleanup"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fw.SubmitDelegatedWorkInput(workerID, "new independent execution", other.ID, "new-turn", "", "", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fw.turnStore = reopened
			service = NewService(reopened, fw, nil)
			if err := service.ReconcileCompletedSessions(); err != nil {
				t.Fatal(err)
			}
			if fw.HasSession(workerID) != (scenario == "reused-session") {
				t.Fatal("cleanup did not respect current ownership")
			}
			if !fw.HasSession(hostID) || !fw.HasSession("user:@keep") {
				t.Fatal("cleanup removed unrelated Session")
			}
			if err := service.ReconcileCompletedSessions(); err != nil {
				t.Fatal(err)
			}
			if delivered, err := service.ReconcileHostLane(); err != nil || delivered {
				t.Fatalf("closed result replayed: %v %v", delivered, err)
			}
			current, _ := reopened.Work(item.ID)
			if current.Status != status || current.Review != nil {
				t.Fatalf("saved decision lost: %+v", current)
			}
			wantSends := 2
			if scenario == "reused-session" {
				wantSends++
			}
			if len(fw.sentCalls) != wantSends {
				t.Fatalf("duplicate business input or result: %d sends, handler %s", len(fw.sentCalls), lease.HandlingID)
			}
		})
	}
}

// Transport is scripted; all result, admission and decision persistence is real.
// This reproduces the Sep 6 unfinished handler blocking the Sep 7 result.
func TestCompletionLoopStaleDeliveredHandlerIncident(t *testing.T) {
	t.Run("missed-terminal-edge", func(t *testing.T) { testCompletionLoopStaleDeliveredHandler(t, false) })
	t.Run("transcript-replacement-and-restart", func(t *testing.T) { testCompletionLoopStaleDeliveredHandler(t, true) })
}

func testCompletionLoopStaleDeliveredHandler(t *testing.T, replaced bool) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "host:@incident"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
		hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
	}, outcomes: map[string]watcher.InputOutcome{}, turnStore: store}
	service := NewService(store, fw, nil)
	now := time.Date(2026, 9, 6, 17, 12, 27, 0, time.UTC)
	store.now = func() time.Time { return now }
	service.now = store.now
	old := createSignalTestWork(t, store, "GitDiff review", "old-worker")
	appendSignalTestEvent(t, store, old, "old-review")
	if delivered, err := service.ReconcileHostLane(); err != nil || !delivered {
		t.Fatalf("old delivery: %v %v", delivered, err)
	}
	oldLease := requireReviewDelivered(t, store, old.ID)
	// Never manually end or remove the stale handler.
	if replaced {
		if err := store.SetHostProviderTranscript("replacement-provider", filepath.Join(root, "replacement.jsonl"), root); err != nil {
			t.Fatal(err)
		}
		store, err = NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		fw.turnStore = store
		fw.providerEvidence = map[string]watcher.ProviderActivityObservation{hostID: {ID: "replacement-idle-activity", Status: "completed"}}
		service = NewService(store, fw, nil)
		store.now = func() time.Time { return now }
		service.now = store.now
	} else {
		settleCanonicalHostTurnForTest(t, store, hostID, oldLease.ProviderTurnID)
	}
	now = time.Date(2026, 9, 7, 9, 12, 12, 0, time.UTC)
	item, err := store.CreateWork(Work{Title: "Independent completion", Objective: "Admit terminal result", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "Brain accepts"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SubmitDelegatedWorkInput("worker:@153", "new scoped test input", item.ID, "turn-result", "", "", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, report := range []struct{ kind, summary string }{{"running", "Delegated provider activity running"}, {"done", "Verified terminal result"}} {
		if result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker:@153", TurnID: "turn-result", Class: watcher.EvidenceControl, Kind: report.kind, SourceID: report.kind, Summary: report.summary, At: now}); err != nil || !result.Matched {
			t.Fatalf("report: %+v %v", result, err)
		}
	}
	if delivered, err := service.ReconcileHostLane(); err != nil || !delivered {
		t.Fatalf("independent completion stranded behind stale delivered handler: delivered=%v err=%v", delivered, err)
	}
	lease := requireReviewDelivered(t, store, item.ID)
	current, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	event, found, err := store.WorkEvent(current.Review.EventID)
	if err != nil || !found || event.Summary != "Verified terminal result" {
		t.Fatalf("stale completion projection: %+v %v", event, err)
	}
	if lease.HandlingID == oldLease.HandlingID {
		t.Fatal("independent result reused old handling")
	}
}
