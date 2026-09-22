package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestReleasedAttemptDoesNotAdvertiseHistoricalTurnAsSteerable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item, err := store.CreateWork(Work{Title: "live provider after loss", Objective: "read canonical ownership", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	candidate := delegatedSubmissionCandidate(item.ID, "fixture:@1", "turn:old", "input", now)
	candidate.SignalProtocol = true
	if _, _, err = store.PrepareInputAdmission(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: candidate.SessionID, TurnID: candidate.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "running", SourceID: "fixture-running", At: now}); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := store.ReconcileAbsentWorkAttempt(item.ID, candidate.SessionID); err != nil || !changed {
		t.Fatalf("fixture loss changed=%t err=%v", changed, err)
	}
	if _, err := store.UpdateWork(item.ID, WorkUpdate{AttemptSessionID: &candidate.SessionID}); err == nil || strings.Contains(err.Error(), "already has an active Attempt") {
		t.Fatalf("manual owner assignment must explain the actual unsupported operation: %v", err)
	}
	for _, reopen := range []bool{false, true} {
		if reopen {
			store, err = NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
		}
		current, found, err := store.Turn(candidate.SessionID)
		if err != nil || !found || current.Status != watcher.TurnUnknown || !current.SignalProtocol {
			t.Fatalf("released owner still steerable: found=%t status=%s signal=%t err=%v", found, current.Status, current.SignalProtocol, err)
		}
		historical, found, err := store.TurnByID(candidate.SessionID, candidate.ProposedTurnID)
		if err != nil || !found || historical.Status != watcher.TurnRunning {
			t.Fatal("historical activity evidence was rewritten")
		}
	}
	fresh := delegatedSubmissionCandidate(item.ID, candidate.SessionID, "turn:fresh", "explicit new input", now.Add(time.Second))
	fresh.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(fresh); err != nil || !created {
		t.Fatalf("fresh fenced recovery rejected: created=%t err=%v", created, err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: fresh.SessionID, TurnID: fresh.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "running", SourceID: "fresh-running", At: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(fresh.ProposedTurnID) {
		t.Fatal("new input did not become sole owner")
	}
	result, _ := store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: candidate.SessionID, TurnID: candidate.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "done", SourceID: "old-done", At: now.Add(2 * time.Second)})
	after, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || result.Matched || after.Attempt == nil || after.Attempt.TurnToken != state.Attempt.TurnToken || after.Revision != state.Revision {
		t.Fatal("old signal crossed the new fence")
	}
}
