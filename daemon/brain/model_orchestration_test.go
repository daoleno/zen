package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestProviderResultPersistsAndReachesBrainWithoutImplicitAcceptance(t *testing.T) {
	for _, kind := range []string{"done", "failed"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(Work{Title: "provider result", Objective: "Brain accepts", CompletionPolicy: CompletionBounded})
			if err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC().Add(-time.Second)
			candidate := delegatedSubmissionCandidate(item.ID, "worker", "turn-result", "implement", at)
			candidate.SignalProtocol = true
			pending, _, err := store.PrepareInputAdmission(candidate)
			if err != nil {
				t.Fatal(err)
			}
			resolveDelegatedSubmission(t, store, pending, "activity-result", at.Add(time.Millisecond))
			turn, _, _ := store.Turn("worker")
			summary := "Worker ended; Brain must assess the result"
			if kind == "failed" {
				summary = "429 Too Many Requests: provider rate limit, implementation incomplete"
			}
			fact := watcher.TurnFact{SessionID: "worker", TurnID: "turn-result", Class: watcher.EvidenceProvider,
				Kind: kind, SourceID: "provider-result", Summary: summary, Bound: true,
				ActivityID: turn.ActivityID, Admission: turn.Admission, At: at.Add(time.Second), SettledAt: at.Add(time.Second)}
			if _, changed, err := store.ApplyTurnFact(fact); err != nil || !changed {
				t.Fatalf("provider result: %v %v", changed, err)
			}
			store, err = NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			if state.Attempt != nil || state.Review == nil || state.Status == lifecycle.StatusDone || state.LastSummary != summary {
				t.Fatalf("persisted result=%+v", state)
			}
			if kind == "failed" && state.Review.Reason != "turn_failed" {
				t.Fatalf("error became success: %+v", state)
			}
			if err := store.SetHostSession("host", "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{sessions: map[string]*classifier.Worker{"host": {ID: "host", Hidden: true, State: classifier.StateDone}},
				ownedGenerations: map[string]string{"host": "generation"}, outcomes: map[string]watcher.InputOutcome{}, turnStore: store}
			service := NewService(store, fw, nil)
			if delivered, err := service.ReconcileHostLane(); err != nil || !delivered {
				t.Fatalf("delivery=%v %v", delivered, err)
			}
			seen := false
			for _, call := range fw.sentCalls {
				if input, ok := work.ParseCanonicalDirectWorkEventInput(call.text); ok && input.WorkID == item.ID {
					seen = strings.Contains(input.Summary, summary)
					if strings.Contains(call.text, "resolve_command") || strings.Contains(call.text, "resolution_required") {
						t.Fatal("notification contains workflow ceremony")
					}
				}
			}
			if !seen {
				t.Fatal("persisted result did not reach Brain")
			}
			// Brain may accept while the delivery turn is still active, without
			// finding or resolving a handling/revision capability.
			before, _ := store.FSM().State(lifecycle.WorkID(item.ID))
			done := WorkDone
			next := "Brain accepted the reported evidence"
			if _, err := store.UpdateWork(item.ID, WorkUpdate{Status: &done, NextAction: &next}); err != nil {
				t.Fatal(err)
			}
			state, _ = store.FSM().State(lifecycle.WorkID(item.ID))
			if state.Status != lifecycle.StatusDone || state.Review != nil || state.NextAction != next || state.Revision != before.Revision+1 {
				t.Fatalf("Brain decision=%+v", state)
			}
		})
	}
}

func TestUnknownSendRestartAllowsModelRetryAndDoesNotReviveOlderInput(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "unknown send", Objective: "model chooses retry"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Second)
	first := delegatedSubmissionCandidate(item.ID, "worker", "turn-old", "first", at)
	first.SignalProtocol = true
	if _, _, err := store.PrepareInputAdmission(first); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkInputAdmissionAmbiguous("worker", "turn-old", "transport response lost"); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second := delegatedSubmissionCandidate(item.ID, "worker", "turn-retry", "Brain chose retry", at)
	second.SignalProtocol = true
	if _, _, err := store.PrepareInputAdmission(second); err != nil {
		t.Fatal(err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "turn-retry", Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "retry-report", Summary: "retry verified", At: at.Add(time.Second)}); err != nil || !result.Matched {
		t.Fatalf("retry=%+v %v", result, err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "turn-old", Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "late-old-report", Summary: "late original result", At: at.Add(time.Second)}); err != nil || result.Matched {
		t.Fatalf("old input revived: %+v %v", result, err)
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt != nil || state.Review == nil || state.Review.Ref != "turn-retry" || state.LastSummary != "retry verified" {
		t.Fatalf("current model-directed result replaced: %+v", state)
	}
}
