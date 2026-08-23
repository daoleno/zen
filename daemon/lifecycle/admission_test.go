package lifecycle

import (
	"testing"
	"time"
)

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
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
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
			defer e.Close()
			define(t, e, "w-review-without-attempt", PolicyUntilDone)
			admit(t, e, "w-review-without-attempt", "turn-lost", "session-1")
			if _, err := e.ReportTurnLost("w-review-without-attempt", attemptID("session-1", "turn-lost", 1), "true silence"); err != nil {
				t.Fatal(err)
			}
			claimed, err := e.ClaimReview("w-review-without-attempt", "handler-1", "host-turn")
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

func TestAmbiguousAdmissionIsDurableAndNeverReprepared(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
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
	defer e.Close()
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
