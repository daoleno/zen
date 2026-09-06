package brain

import (
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestWorkerProgressSurvivesHostDeliveryAndRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "Worker authority", Objective: "retain exact execution identity", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "Exact identity survives Host delivery and restart"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := delegatedSubmissionCandidate(item.ID, "worker:@1", "turn-worker", "bounded test", time.Now().UTC().Add(-time.Second))
	candidate.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare: created=%v err=%v", created, err)
	}
	progress := func(owner *Store, token, source string) error {
		t.Helper()
		result, err := owner.ApplyDelegatedTurnProgress(watcher.TurnFact{
			SessionID: candidate.SessionID, TurnID: token, Class: watcher.EvidenceControl,
			Kind: "heartbeat", SourceID: source, At: time.Now().UTC(), LeaseSeconds: 300,
		})
		if err == nil && (!result.Owned || !result.Matched) {
			return fmt.Errorf("progress rejected: owned=%v matched=%v", result.Owned, result.Matched)
		}
		return err
	}
	if err := progress(store, candidate.ProposedTurnID, "initial-progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), "lease_expired", candidate.ProposedTurnID, "worker-review"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	claimAndDeliverTestReview(t, store, "brain-host:@2")
	if err := progress(store, candidate.ProposedTurnID, "after-host-delivery"); err != nil {
		t.Fatalf("Host delivery erased Worker authority: %v", err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := progress(reopened, candidate.ProposedTurnID, "after-restart"); err != nil {
		t.Fatalf("restart erased Worker authority: %v", err)
	}
	if err := progress(reopened, "wrong-token", "wrong-token-progress"); err == nil {
		t.Fatal("unknown prompt gained authority")
	}
	state, err := reopened.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(candidate.ProposedTurnID) || state.Review == nil {
		t.Fatalf("progress changed ownership or dismissed review: state=%+v err=%v", state, err)
	}
}
