package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestBDD_ZEN009_LostOriginalResultSurvivesWaitAndRestart(t *testing.T) {
	for _, disposition := range []string{"wait", "cancel", "superseded"} {
		t.Run(disposition, func(t *testing.T) {
			// Given an admitted original producer classified lost, without a replay.
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{Title: "original result", Objective: "retain producer eligibility"})
			if err != nil {
				t.Fatal(err)
			}
			base := time.Now().UTC().Add(-time.Minute)
			candidate := delegatedSubmissionCandidate(item.ID, "worker", "original", "original input", base)
			candidate.SignalProtocol = true
			if _, _, err := store.PrepareInputAdmission(candidate); err != nil {
				t.Fatal(err)
			}
			fact := watcher.TurnFact{SessionID: "worker", TurnID: "original", Class: watcher.EvidenceControl, Kind: "running", SourceID: "running", At: base.Add(time.Second)}
			if result, err := store.ApplyDelegatedTurnProgress(fact); err != nil || !result.Matched {
				t.Fatalf("admit: %+v %v", result, err)
			}
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{SessionID: "worker", TurnID: "original", Class: watcher.EvidenceLiveness, Kind: "uncertain", ProcessDead: true, SourceID: "lost", At: time.Now()}); err != nil {
				t.Fatal(err)
			}
			lost, err := store.FSM().State(lifecycle.WorkID(item.ID))
			if err != nil || lost.Review == nil || lost.Review.Reason != "turn_lost" {
				t.Fatalf("loss: %+v %v", lost, err)
			}
			// When Brain explicitly waits and the Store restarts, the review is gone.
			decision := lifecycle.DispositionWait
			if disposition == "cancel" {
				decision = lifecycle.DispositionCancel
			}
			if _, err := store.FSM().ResolveReview(lost.ID, lost.Review.EventID, lifecycle.ResolveReviewInput{Disposition: decision, WakeKind: lifecycle.WakeSessionTerminal, WakeRef: SessionTerminalWakeRef("worker", "original")}); err != nil {
				t.Fatal(err)
			}
			if disposition == "superseded" {
				next := delegatedSubmissionCandidate(item.ID, "worker", "new-turn", "scoped followup", time.Now())
				next.SignalProtocol = true
				if _, _, err := store.PrepareInputAdmission(next); err != nil {
					t.Fatal(err)
				}
				if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "new-turn", Class: watcher.EvidenceControl, Kind: "running", SourceID: "new-running", At: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			store, err = NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fact.Kind, fact.SourceID, fact.Summary, fact.At = "done", "exact-terminal", "original verified result", time.Now()
			result, err := store.ApplyDelegatedTurnProgress(fact)
			if err != nil {
				t.Fatal(err)
			}
			if disposition != "wait" {
				if result.Matched {
					t.Fatal("closed or superseded producer revived")
				}
				return
			}
			// Then exact supplied progress persists, requires a new decision, and dedupes.
			if !result.Matched || !result.Changed {
				t.Fatalf("original terminal rejected: %+v", result)
			}
			store, err = NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			state, _ := store.FSM().State(lost.ID)
			if state.Review == nil || state.Review.Reason != "turn_done" || state.LastSummary != fact.Summary || state.Wake != nil || state.Status == lifecycle.StatusDone {
				t.Fatalf("durable result: %+v", state)
			}
			if duplicate, err := store.ApplyDelegatedTurnProgress(fact); err != nil || !duplicate.Matched || duplicate.Changed {
				t.Fatalf("duplicate: %+v %v", duplicate, err)
			}
		})
	}
}

func TestBDD_ZEN010_IncompleteInventoryDoesNotDeclareOriginalLost(t *testing.T) {
	for _, mode := range []string{"present", "probe-error", "absent"} {
		t.Run(mode, func(t *testing.T) {
			// Given a restarted inventory missing a still-owned original execution.
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item := createSignalTestWork(t, store, "original execution", "worker")
			fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Worker{"worker": {ID: "worker", Delegated: true, ProcessID: 123, PaneAlive: true}}}
			if _, err := fw.SubmitDelegatedWorkInput("worker", "original input", item.ID, "original", "", "", time.Now()); err != nil {
				t.Fatal(err)
			}
			if mode == "probe-error" {
				fw.probeErr = errors.New("transport unavailable")
			}
			if mode == "absent" {
				delete(fw.sessions, "worker")
			}
			service := NewService(store, fw, nil)
			// When the inventory snapshot is incomplete, consult authoritative presence.
			service.ReconcileDelegatedSessions(nil)
			state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			if mode == "absent" {
				if state.Attempt != nil || state.Review == nil || state.Review.Reason != "turn_lost" {
					t.Fatalf("absence not recorded: %+v", state)
				}
				return
			}
			// Then neither a live Session nor an unreadable transport is process death.
			if state.Attempt == nil || state.Review != nil {
				t.Fatalf("false loss: %+v", state)
			}
			result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "original", Class: watcher.EvidenceControl, Kind: "done", SourceID: "original-done", Summary: "original execution completed", At: time.Now()})
			if err != nil || !result.Matched || !result.Changed {
				t.Fatalf("original progress rejected: %+v %v", result, err)
			}
			if len(fw.sentCalls) != 1 {
				t.Fatal("business input replayed")
			}
		})
	}
}
