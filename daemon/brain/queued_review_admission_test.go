package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func TestQueuedReviewPromotionSurvivesRestartWithoutRevisionChurn(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "brain-host:@queued-restart"
	item := createSignalTestWork(t, store, "queued restart", "worker:@queued-restart")
	appendSignalTestEvent(t, store, item, "queued-restart")
	action, claimed, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !claimed {
		t.Fatalf("claim=%v action=%+v err=%v", claimed, action, err)
	}
	payloadDigest := AdmissionDigest("queued Review payload")
	acceptedAt := action.ClaimedAt.UTC()
	pending, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: action.WorkID, SessionID: hostID, ProposedTurnID: action.ProviderTurnID,
		Receipt: action.ProviderTurnID, ClaimToken: action.HandlingID,
		PayloadSHA256: payloadDigest, ProcessIdentity: "process-queued",
		PaneGeneration: "pane-queued", AcceptedAt: acceptedAt,
		Mode: watcher.InputAdmissionFresh, BaselineActivityID: "activity-a",
	})
	if err != nil || !created {
		t.Fatalf("prepare created=%v pending=%+v err=%v", created, pending, err)
	}
	admission := watcher.TurnAdmission{
		Stream: "provider", ID: "input-review", Cursor: 2,
		SHA256: payloadDigest, At: acceptedAt.Add(time.Millisecond),
	}
	if _, err := store.ResolveInputAdmission(watcher.InputAdmissionResolution{
		SessionID: hostID, ProposedTurnID: action.ProviderTurnID,
		Receipt: action.ProviderTurnID, PayloadSHA256: payloadDigest,
		Admission: admission, ResolvedAt: acceptedAt.Add(2 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	queued, found, err := store.TurnByID(hostID, action.ProviderTurnID)
	if err != nil || !found || queued.ActivityID != "" ||
		queued.QueuedBehindActivityID != "activity-a" {
		t.Fatalf("queued Turn found=%v turn=%+v err=%v", found, queued, err)
	}

	promotion := watcher.TurnFact{
		SessionID: hostID, TurnID: action.ProviderTurnID,
		Class: watcher.EvidenceProvider, Kind: "running",
		SourceID: "provider-promotion-review", Cursor: admission.Cursor,
		Admission: admission, ActivityID: "activity-b",
		StartedAt: acceptedAt.Add(time.Second), At: acceptedAt.Add(time.Second),
		Summary: "Provider promoted queued Review",
	}
	promoted, changed, err := store.ApplyTurnFact(promotion)
	if err != nil || !changed || promoted.ActivityID != "activity-b" || promoted.Status != watcher.TurnRunning {
		t.Fatalf("promotion changed=%v turn=%+v err=%v", changed, promoted, err)
	}
	if err := store.FSM().Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.FSM().Close()
	recovered, found, err := reopened.TurnByID(hostID, action.ProviderTurnID)
	if err != nil || !found || recovered.ActivityID != "activity-b" || recovered.Status != watcher.TurnRunning {
		t.Fatalf("restart found=%v turn=%+v err=%v", found, recovered, err)
	}
	beforeReplay, _ := reopened.Work(item.ID)
	if _, changed, err := reopened.ApplyTurnFact(promotion); err != nil || changed {
		t.Fatalf("duplicate promotion changed=%v err=%v", changed, err)
	}
	afterReplay, _ := reopened.Work(item.ID)
	if afterReplay.Revision != beforeReplay.Revision {
		t.Fatalf("duplicate promotion churned Work revision=%d want=%d", afterReplay.Revision, beforeReplay.Revision)
	}
}
