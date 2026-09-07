package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestSupervisorLossProjectsReviewWithoutProviderResultAndDeliversOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "accepted execution lost", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "accepted-turn", AcceptedAt: time.Now().UTC(),
	})
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	identity := lifecycle.AttemptIdentity{SessionID: "worker", TurnToken: "accepted-turn", Fence: state.Attempt.Generation}
	if _, err := store.FSM().ReportTurnLost(state.ID, identity, "evidence_loss"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	projected, err := store.Work(item.ID)
	if err != nil || projected.AttemptSessionID != "" || projected.Status != WorkNeedsInput {
		t.Fatalf("lost projection=%+v err=%v", projected, err)
	}
	if strings.Contains(projected.NextAction, "received the prompt") || !strings.Contains(projected.NextAction, "exit evidence") {
		t.Fatalf("accepted execution described as uncertain delivery: %s", projected.NextAction)
	}
	timeline, err := store.ThreadTimeline(item.SourceThreadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	cards := 0
	for _, event := range timeline {
		if event.WorkID == item.ID {
			cards++
			if event.Status != "session.uncertain" || event.ID != projected.Review.EventID {
				t.Fatalf("canonical loss did not replace card: %+v", event)
			}
		}
	}
	if cards != 1 {
		t.Fatalf("got %d loss cards, want one", cards)
	}
	action, _ := deliverSignalTestEvent(t, store, "host")
	if action.EventID != projected.Review.EventID || action.Kind != "turn_lost" {
		t.Fatalf("wrong exception delivered: %+v", action)
	}
	if _, _, err := store.EndReviewDelivery(item.ID, action.HandlingID, action.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, claimed, err := store.ClaimNextReviewAction("host"); err != nil || claimed {
			t.Fatalf("ended loss handling looped: claimed=%v err=%v", claimed, err)
		}
	}
}

func TestLossCardCannotReuseEarlierAttentionEvidence(t *testing.T) {
	database := presentationDatabase{BrainWorkEvents: []WorkEvent{{
		WorkID: "work", Kind: "session.needs_input", DedupeKey: "session:worker:turn:accepted-turn:session.needs_input",
		Summary: "Earlier request", Attention: "user_input",
	}}}
	card := cardEventForCanonicalReview(database, WorkEvent{
		ID: "loss", WorkID: "work", Kind: "turn_lost", PayloadRef: "accepted-turn",
	})
	if card.Kind != "session.uncertain" || card.Attention != "" || card.Summary == "Earlier request" {
		t.Fatalf("loss reused stale attention: %+v", card)
	}
}
