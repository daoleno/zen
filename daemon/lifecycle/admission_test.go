package lifecycle

import (
	"reflect"
	"testing"
	"time"
)

func TestHostAdmissionPreservesWorkerAuthorityAcrossRestart(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	e.SetNow(func() time.Time { return time.Now().UTC() })
	define(t, e, "w-host-worker", PolicyUntilDone)
	prepareAdmissionFixture(t, e, "w-host-worker", "worker", "worker-turn")
	before, err := e.AcceptAdmissionBySignal("w-host-worker", "worker-turn", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.OpenReviewEvent("w-host-worker", "lease_expired", "worker-turn", "review-1"); err != nil {
		t.Fatal(err)
	}
	claim, err := e.ClaimReview("w-host-worker", "host", "handling-1", "host-turn")
	if err != nil {
		t.Fatal(err)
	}
	input := PrepareAdmissionInput{
		SessionID: "host", TurnToken: "host-turn", Receipt: "host-turn", ClaimToken: claim.Review.Handler.HandlerID,
		PayloadSHA256: "host-digest", ProcessIdentity: "host-process", PaneGeneration: "host-pane",
		Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: claim.Review.Handler.HandlerID,
		AttemptedAt: time.Now().UTC(),
	}
	if _, _, err := e.PrepareAdmission("w-host-worker", input); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptAdmission("w-host-worker", "host-turn", AcceptAdmissionInput{
		SessionID: "host", Receipt: "host-turn", PayloadSHA256: "host-digest",
		ActivityID: "host-activity", AdmissionStream: "provider", AdmissionID: "host-input", AdmissionSHA256: "host-digest", AdmissionAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.State("w-host-worker")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Attempt, after.Attempt) ||
		!reflect.DeepEqual(before.AdmissionByToken("worker-turn"), after.AdmissionByToken("worker-turn")) || len(after.Admissions) != 2 {
		t.Fatalf("Host transport changed Worker authority: before=%+v after=%+v", before, after)
	}
	// Operational views are detached; callers cannot mutate retained authority.
	after.AdmissionByToken("worker-turn").SessionID = "forged"
	stored, _ := reopened.State("w-host-worker")
	if stored.AdmissionByToken("worker-turn").SessionID != "worker" {
		t.Fatal("admission map leaked mutable store authority")
	}
	if _, err := reopened.AcceptAdmissionBySignal("w-host-worker", "worker-turn", "worker"); err != nil {
		t.Fatalf("exact Worker signal lost authority: %v", err)
	}
	if _, err := reopened.AcceptAdmissionBySignal("w-host-worker", "worker-turn", "host"); err == nil {
		t.Fatal("wrong Session gained Worker signal authority")
	}
	input.TurnToken, input.Receipt = "duplicate-host-turn", "duplicate-host-turn"
	if _, _, err := reopened.PrepareAdmission("w-host-worker", input); err == nil {
		t.Fatal("accepted Host delivery was automatically replayable")
	}
	// Host delivery and one Worker follow-up have distinct roles under the
	// same review handling. Accepted input is still non-owning until disposition.
	if _, _, err := reopened.PrepareAdmission("w-host-worker", PrepareAdmissionInput{
		SessionID: "worker", TurnToken: "review-follow-up", Receipt: "review-follow-up",
		PayloadSHA256: "follow-up-digest", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
		Mode: AdmissionFresh, SignalProtocol: true, Purpose: AdmissionPurposeReview,
		PurposeID: claim.Review.Handler.HandlerID, AttemptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := reopened.AcceptAdmissionBySignal("w-host-worker", "review-follow-up", "worker")
	if err != nil || accepted.Attempt.TurnToken != "worker-turn" || len(accepted.Admissions) != 3 {
		t.Fatalf("accepted follow-up stole ownership: state=%+v err=%v", accepted, err)
	}
	adopted, err := reopened.AcceptReviewFollowUp("w-host-worker", accepted.Review.EventID, "worker", "review-follow-up")
	if err != nil || adopted.Attempt.TurnToken != "review-follow-up" || adopted.Review != nil {
		t.Fatalf("typed disposition failed to adopt exact input: state=%+v err=%v", adopted, err)
	}
}

func prepareAdmissionFixture(t *testing.T, e *Engine, workID, sessionID string, token TurnToken) {
	t.Helper()
	if _, _, err := e.PrepareAdmission(WorkID(workID), PrepareAdmissionInput{
		SessionID: sessionID, TurnToken: token, Receipt: string(token),
		PayloadSHA256: "digest-" + string(token), ProcessIdentity: "process-1",
		PaneGeneration: "pane-1", Mode: AdmissionFresh, AttemptedAt: time.Now().UTC(),
		SignalProtocol: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedAdmissionSurvivesRestartAndAcceptsExactlyOnce(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w-admission-restart", PolicyUntilDone)
	prepareAdmissionFixture(t, e, "w-admission-restart", "session-1", "turn-1")

	e, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}

	st, _ := e.State("w-admission-restart")
	if st.Attempt != nil || st.ActiveAdmission() == nil || st.ActiveAdmission().Status != AdmissionPrepared {
		t.Fatalf("prepared restart state=%+v", st)
	}
	accepted, err := e.AcceptAdmission("w-admission-restart", "turn-1", AcceptAdmissionInput{
		SessionID: "session-1", Receipt: "turn-1", PayloadSHA256: "digest-turn-1",
		ActivityID: "activity-1", AdmissionID: "provider-admission-1", AdmissionSHA256: "digest-turn-1",
		AdmissionAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Attempt == nil || accepted.Attempt.TurnToken != "turn-1" ||
		accepted.AdmissionByToken("turn-1").Status != AdmissionAccepted {
		t.Fatalf("accepted state=%+v", accepted)
	}
	again, err := e.AcceptAdmission("w-admission-restart", "turn-1", AcceptAdmissionInput{
		SessionID: "session-1", Receipt: "turn-1", PayloadSHA256: "digest-turn-1",
		ActivityID: "activity-1", AdmissionID: "provider-admission-1", AdmissionSHA256: "digest-turn-1",
	})
	if err != nil || again.Fence != accepted.Fence {
		t.Fatalf("duplicate acceptance state=%+v err=%v", again, err)
	}
}

func TestReviewNextAttemptRemainsPreparedUntilDisposition(t *testing.T) {
	for _, acceptBy := range []string{"provider", "signal"} {
		t.Run(acceptBy, func(t *testing.T) {
			e, _ := newTestEngine(t)

			define(t, e, "w-review-without-attempt", PolicyUntilDone)
			admit(t, e, "w-review-without-attempt", "turn-lost", "session-1")
			if _, err := e.ReportTurnLost("w-review-without-attempt", attemptID("session-1", "turn-lost", 1), "true silence"); err != nil {
				t.Fatal(err)
			}
			claimed, err := e.ClaimReview("w-review-without-attempt", "brain-host", "handler-1", "host-turn")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.MarkReviewDelivered("w-review-without-attempt", "host-turn"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.PrepareAdmission("w-review-without-attempt", PrepareAdmissionInput{
				SessionID: "session-1", TurnToken: "turn-reviewed", Receipt: "turn-reviewed",
				PayloadSHA256: "digest-reviewed", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
				Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: claimed.Review.Handler.HandlerID,
				AttemptedAt: time.Now().UTC(), SignalProtocol: true,
			}); err != nil {
				t.Fatal(err)
			}
			switch acceptBy {
			case "provider":
				if _, err := e.AcceptAdmission("w-review-without-attempt", "turn-reviewed", AcceptAdmissionInput{
					SessionID: "session-1", Receipt: "turn-reviewed", PayloadSHA256: "digest-reviewed",
					ActivityID: "activity-reviewed", AdmissionID: "admission-reviewed",
					AdmissionSHA256: "digest-reviewed", AdmissionAt: time.Now().UTC(),
				}); err != nil {
					t.Fatal(err)
				}
			case "signal":
				if _, err := e.AcceptAdmissionBySignal("w-review-without-attempt", "turn-reviewed", "session-1"); err != nil {
					t.Fatal(err)
				}
			}
			prepared, _ := e.State("w-review-without-attempt")
			admission := prepared.AdmissionByToken("turn-reviewed")
			if prepared.Attempt != nil || prepared.Review == nil || admission == nil ||
				admission.Status != AdmissionAccepted || admission.PurposeID != claimed.Review.Handler.HandlerID {
				t.Fatalf("accepted review admission escaped its disposition: %+v", prepared)
			}
			resolved, err := e.AcceptReviewFollowUp(
				"w-review-without-attempt", prepared.Review.EventID, "session-1", "turn-reviewed",
			)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Attempt == nil || resolved.Attempt.TurnToken != "turn-reviewed" || resolved.Review != nil {
				t.Fatalf("review disposition did not admit exact next Attempt: %+v", resolved)
			}
		})
	}
}

func TestClaimedReviewAdmissionMayQueueWithoutAdoptingLiveActivity(t *testing.T) {
	e, _ := newTestEngine(t)

	define(t, e, "w-queued-review", PolicyBounded)
	if _, err := e.OpenReviewEvent("w-queued-review", "session.done", "worker-turn", "event-queued"); err != nil {
		t.Fatal(err)
	}
	claimed, err := e.ClaimReview("w-queued-review", "brain-host", "handling-queued", "turn-queued")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.PrepareAdmission("w-queued-review", PrepareAdmissionInput{
		SessionID: "brain-host", TurnToken: "turn-queued", Receipt: "turn-queued",
		ClaimToken: claimed.Review.Handler.HandlerID, PayloadSHA256: "digest-queued",
		ProcessIdentity: "process-1", PaneGeneration: "pane-1", Mode: AdmissionFresh,
		BaselineActivityID: "activity-a", AttemptedAt: time.Now().UTC(),
		Purpose: AdmissionPurposeReview, PurposeID: claimed.Review.Handler.HandlerID,
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := e.AcceptAdmission("w-queued-review", "turn-queued", AcceptAdmissionInput{
		SessionID: "brain-host", Receipt: "turn-queued", PayloadSHA256: "digest-queued",
		AdmissionStream: "provider", AdmissionID: "input-queued", AdmissionCursor: 2,
		AdmissionSHA256: "digest-queued", AdmissionAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	admission := accepted.AdmissionByToken("turn-queued")
	if admission == nil || admission.Status != AdmissionAccepted || admission.ActivityID != "" ||
		admission.BaselineActivityID != "activity-a" || accepted.Attempt != nil {
		t.Fatalf("queued Review admission=%+v state=%+v", admission, accepted)
	}
	revision := accepted.Revision
	again, err := e.AcceptAdmission("w-queued-review", "turn-queued", AcceptAdmissionInput{
		SessionID: "brain-host", Receipt: "turn-queued", PayloadSHA256: "digest-queued",
		AdmissionStream: "provider", AdmissionID: "input-queued", AdmissionCursor: 2,
		AdmissionSHA256: "digest-queued", AdmissionAt: time.Now().UTC(),
	})
	if err != nil || again.Revision != revision {
		t.Fatalf("duplicate queued acceptance revision=%d want=%d err=%v", again.Revision, revision, err)
	}
}

func TestAmbiguousAdmissionIsDurableAndNeverReprepared(t *testing.T) {
	e, _ := newTestEngine(t)

	define(t, e, "w-ambiguous", PolicyUntilDone)
	prepareAdmissionFixture(t, e, "w-ambiguous", "session-1", "turn-1")
	if _, err := e.MarkAdmissionAmbiguous("w-ambiguous", "turn-1", "queue started"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.PrepareAdmission("w-ambiguous", PrepareAdmissionInput{
		SessionID: "session-1", TurnToken: "turn-2", Receipt: "turn-2", PayloadSHA256: "digest-2",
		ProcessIdentity: "process-1", PaneGeneration: "pane-1", Mode: AdmissionFresh,
		AttemptedAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("ambiguous admission allowed a second mutation transaction")
	}
	st, _ := e.State("w-ambiguous")
	if st.ActiveAdmission() == nil || st.ActiveAdmission().Status != AdmissionAmbiguous || st.Attempt != nil {
		t.Fatalf("ambiguous state=%+v", st)
	}
}

func TestExactActionableEventIdentityOwnsReview(t *testing.T) {
	e, _ := newTestEngine(t)

	define(t, e, "w-event", PolicyBounded)
	if _, err := e.OpenReviewEvent("w-event", "session.needs_input", "evidence-ref", "event-exact"); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w-event")
	if st.Review == nil || st.Review.EventID != "event-exact" {
		t.Fatalf("exact event state=%+v", st)
	}
}

func TestAdmissionPreparedUsesEventTimeWhenAttemptedAtIsOmitted(t *testing.T) {
	at := time.Date(2026, 8, 22, 10, 34, 42, 661032855, time.UTC)
	st := Reduce(&State{}, Event{
		WorkID: "w-historical", Kind: KWorkDefined, At: at,
		Payload: DefinedPayload{Title: "t", Objective: "o", Policy: PolicyBounded},
	})
	st = Reduce(st, Event{
		WorkID: "w-historical", Kind: KAdmissionPrepared, TurnToken: "turn-1", At: at,
		Payload: AdmissionPreparedPayload{
			SessionID: "session-1", Receipt: "turn-1", PayloadSHA256: "digest",
			ProcessIdentity: "proc", PaneGeneration: "pane", Mode: AdmissionFresh,
		},
	})
	admission := st.AdmissionByToken("turn-1")
	if admission == nil || !admission.AttemptedAt.Equal(at) {
		t.Fatalf("historical admission.prepared must inherit event time, got %+v", admission)
	}
}
