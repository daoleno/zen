package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestBDD_ZEN005_PartialResultNeedsScopedFollowup(t *testing.T) {
	// Given a two-part objective and a Worker result containing only part one.
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "two-part result", Objective: "deliver A and B", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "both parts verified"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host":   {ID: "host", Hidden: true, State: classifier.StateDone},
		"worker": {ID: "worker", Delegated: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	if _, err := fw.SubmitDelegatedWorkInput("worker", "deliver A and B", item.ID, "first", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i, turnID := range []string{"first", "followup"} {
		summary := "A verified; B missing"
		if i == 1 {
			summary = "A and B verified"
		}
		if result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: turnID, Class: watcher.EvidenceControl, Kind: "done", SourceID: "result-" + turnID, Summary: summary, At: time.Now()}); err != nil || !result.Changed {
			t.Fatalf("result: %+v %v", result, err)
		}
		if err := service.ReconcileWorkChange(); err != nil {
			t.Fatal(err)
		}
		lease := requireReviewDelivered(t, store, item.ID)
		state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
		if state.Status == lifecycle.StatusDone || !fw.HasSession("worker") || state.LastSummary != summary {
			t.Fatalf("implicit acceptance: %+v", state)
		}
		if i == 0 {
			// When the scripted Brain inspects the incomplete result and asks only
			// for missing B under the delivered review's admission authority.
			if _, err := fw.SubmitDelegatedWorkInput("worker", "deliver missing B only", item.ID, "followup", string(lifecycle.AdmissionPurposeReview), lease.HandlingID, time.Now()); err != nil {
				t.Fatal(err)
			}
			settleCanonicalHostTurnForTest(t, store, "host", lease.ProviderTurnID)
			store, err = NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fw.turnStore = store
			service = NewService(store, fw, nil)
		} else {
			// Then only the later complete evidence is accepted, explicitly.
			done := WorkDone
			if _, err := service.UpdateWork(item.ID, WorkUpdate{Status: &done}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fw.HasSession("worker") || !fw.HasSession("host") {
		t.Fatal("incorrect cleanup")
	}
	if len(fw.sentCalls) != 4 || fw.sentCalls[2].text != "deliver missing B only" {
		t.Fatalf("unexpected replay or followup: %+v", fw.sentCalls)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := store.Work(item.ID)
	if err != nil || closed.Status != WorkDone || closed.Review != nil {
		t.Fatalf("decision lost: %+v %v", closed, err)
	}
}
