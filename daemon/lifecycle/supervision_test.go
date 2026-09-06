package lifecycle

import (
	"testing"
	"time"
)

func TestExpiryIsSupervisionOnlyAndSchedulesLossOnce(t *testing.T) {
	e, root := newTestEngine(t)
	define(t, e, "supervised", PolicyUntilDone)
	initial := admit(t, e, "supervised", tok1, "worker")
	deadline := initial.Attempt.LeaseDeadline
	setNow(e, deadline)
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := e.ReportLeaseExpired("supervised", tok1); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ClaimReview("supervised", "host", "handling", "host-turn"); err != ErrNoOpenReview {
			t.Fatalf("check-in admitted Brain: %v", err)
		}
	}
	st, _ := e.State("supervised")
	if st.Revision != initial.Revision+1 || st.Review != nil || st.Attempt == nil {
		t.Fatalf("expiry: %+v", st)
	}
	next, ok := e.NextWakeAt()
	if !ok || !next.Equal(deadline.Add(LostGrace)) {
		t.Fatalf("next=%v, want loss deadline", next)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	setNow(reopened, next)
	if err := reopened.Sweep(); err != nil {
		t.Fatal(err)
	}
	lost, _ := reopened.State("supervised")
	if lost.Attempt != nil || lost.Review == nil || lost.Review.Reason != "turn_lost" {
		t.Fatalf("loss=%+v", lost)
	}
	if _, ok := reopened.NextWakeAt(); ok {
		t.Fatal("settled Attempt retained timer")
	}
	if err := reopened.Sweep(); err != nil {
		t.Fatal(err)
	}
	stable, _ := reopened.State("supervised")
	if stable.Revision != lost.Revision {
		t.Fatal("loss repeated")
	}
}

func TestActiveAttemptWaitAndTerminalSupersession(t *testing.T) {
	for _, resolve := range []bool{false, true} {
		t.Run(map[bool]string{false: "unresolved", true: "wait"}[resolve], func(t *testing.T) {
			e, _ := newTestEngine(t)
			define(t, e, "owned", PolicyUntilDone)
			initial := admit(t, e, "owned", tok1, "worker")
			if _, err := e.OpenReviewEvent("owned", "lease_expired", string(tok1), "old-event"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.ClaimReview("owned", "host", "handling", "host-turn"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.MarkReviewDelivered("owned", "host-turn"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.ResolveReview("owned", "old-event", ResolveReviewInput{Disposition: DispositionWait, WakeKind: WakeSessionTerminal, WakeRef: "worker"}); err == nil {
				t.Fatal("bare producer accepted")
			}
			if resolve {
				st, err := e.ResolveReview("owned", "old-event", ResolveReviewInput{Disposition: DispositionWait, WakeKind: WakeSessionTerminal, WakeRef: "session:worker:turn:" + string(tok1)})
				if err != nil || st.Attempt == nil || st.Attempt.Generation != initial.Attempt.Generation || st.Wake != nil || st.Review != nil {
					t.Fatalf("owned wait=%+v %v", st, err)
				}
			}
			st, err := e.ReportTurnDone("owned", attemptID("worker", tok1, initial.Attempt.Generation), DoneInput{OK: true, Summary: "result"})
			if err != nil || st.Attempt != nil || st.Review == nil || st.Review.EventID == "old-event" || st.Review.Reason != "turn_done" || st.Review.Handler != nil {
				t.Fatalf("terminal=%+v %v", st, err)
			}
			oldRevision := st.Revision
			st, err = e.ResolveReview("owned", "old-event", ResolveReviewInput{Disposition: DispositionComplete})
			if err != nil || st.Revision != oldRevision {
				t.Fatal("stale event altered terminal decision")
			}
		})
	}
}

func TestEndedHandlingIsDurableAndRequiresExplicitReplay(t *testing.T) {
	e, root := newTestEngine(t)
	define(t, e, "decision", PolicyUntilDone)
	if _, err := e.OpenReviewEvent("decision", "turn_failed", "producer", "event"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("decision", "host", "handling", "host-turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkReviewDelivered("decision", "host-turn"); err != nil {
		t.Fatal(err)
	}
	ended, err := e.ReleaseReview("decision", "host-turn")
	if err != nil || ended.Review.Handler.EndedAt == nil {
		t.Fatalf("ended=%+v %v", ended, err)
	}
	for range 20 {
		st, err := e.ReleaseReview("decision", "host-turn")
		if err != nil || st.Revision != ended.Revision {
			t.Fatal("duplicate ending churned")
		}
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	setNow(reopened, time.Now().Add(24*time.Hour))
	if err := reopened.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ClaimReview("decision", "host", "new", "new-turn"); err != ErrReviewLease {
		t.Fatalf("automatic replay: %v", err)
	}
	if _, err := reopened.ResolveReviewDelivery("decision", ReviewDeliveryReplay, "user", "reviewed failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ClaimReview("decision", "host", "new", "new-turn"); err != nil {
		t.Fatal(err)
	}
}

func TestNewDecisionSupersedesEndedHandlingButUnchangedEvidenceCannot(t *testing.T) {
	e, _ := newTestEngine(t)
	define(t, e, "decision", PolicyUntilDone)
	if _, err := e.OpenReviewEvent("decision", "session.needs_input", "question-one", "event-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("decision", "host", "handling", "host-turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkReviewDelivered("decision", "host-turn"); err != nil {
		t.Fatal(err)
	}
	ended, err := e.ReleaseReview("decision", "host-turn")
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := e.OpenReviewEvent("decision", "session.needs_input", "question-one", "different-envelope")
	if err != nil || unchanged.Revision != ended.Revision {
		t.Fatalf("unchanged evidence requeued: %+v %v", unchanged, err)
	}
	changed, err := e.OpenReviewEvent("decision", "session.needs_input", "question-two", "event-two")
	if err != nil || changed.Review == nil || changed.Review.EventID != "event-two" || changed.Review.Handler != nil {
		t.Fatalf("new request hidden: %+v %v", changed, err)
	}
	if _, err := e.ClaimReview("decision", "host", "new-handling", "new-host-turn"); err != nil {
		t.Fatal(err)
	}
}
