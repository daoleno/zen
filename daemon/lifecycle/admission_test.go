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
	if accepted.Attempt == nil || accepted.Attempt.TurnToken != "turn-1" || len(accepted.Attempts) != 1 ||
		accepted.Admission("turn-1").Status != AdmissionAccepted {
		t.Fatalf("accepted state=%+v", accepted)
	}
	again, err := e.AcceptAdmission("w-admission-restart", "turn-1", AcceptAdmissionInput{
		SessionID: "session-1", Receipt: "turn-1", PayloadSHA256: "digest-turn-1",
		ActivityID: "activity-1", AdmissionID: "provider-admission-1", AdmissionSHA256: "digest-turn-1",
	})
	if err != nil || len(again.Attempts) != 1 || again.Fence != accepted.Fence {
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
			admission := prepared.Admission("turn-reviewed")
			if prepared.Attempt != nil || prepared.Review == nil || admission == nil ||
				admission.Status != AdmissionAccepted || admission.PurposeID != claimed.Review.Handler.HandlerID || len(prepared.Attempts) != 1 {
				t.Fatalf("accepted review admission escaped its disposition: %+v", prepared)
			}
			resolved, err := e.AcceptReviewFollowUp(
				"w-review-without-attempt", prepared.Review.EventID, "session-1", "turn-reviewed",
			)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Attempt == nil || resolved.Attempt.TurnToken != "turn-reviewed" ||
				resolved.Review != nil || len(resolved.Attempts) != 2 {
				t.Fatalf("review disposition did not admit exact next Attempt: %+v", resolved)
			}
		})
	}
}

func TestSameSessionTerminalFollowUpSettlesAndPreparesAtomically(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-same-session", PolicyUntilDone)
	admit(t, e, "w-same-session", "turn-old", "session-1")
	st, err := e.PrepareTerminalFollowUp("w-same-session", PrepareTerminalFollowUpInput{
		AttemptSessionID: "session-1", AttemptToken: "turn-old", AttemptFence: 1, TerminalEvidenceID: "provider-completed-17",
		Admission: PrepareAdmissionInput{
			SessionID: "session-1", TurnToken: "turn-new", Receipt: "turn-new",
			PayloadSHA256: "digest-new", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
			Mode: AdmissionFresh, AttemptedAt: time.Now().UTC(), SignalProtocol: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt != nil || st.Attempts[0].SettledAt == nil || st.ActiveAdmission() == nil ||
		st.ActiveAdmission().TurnToken != "turn-new" || st.ActiveAdmission().ExistingTurnToken != "turn-old" {
		t.Fatalf("atomic terminal follow-up state=%+v", st)
	}
	if _, err := e.AcceptAdmissionBySignal("w-same-session", "turn-new", "session-1"); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w-same-session")
	if st.Attempt == nil || st.Attempt.TurnToken != "turn-new" || st.Attempt.SessionID != "session-1" {
		t.Fatalf("new follow-up not admitted: %+v", st)
	}
}

func TestNeedsInputTerminalFollowUpResolvesExactEventAndPreparesAtomically(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-reviewed-followup", PolicyUntilDone)
	admit(t, e, "w-reviewed-followup", "turn-old", "session-1")
	if _, err := e.OpenReviewEvent("w-reviewed-followup", "session.needs_input", "signal", "event-exact"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("w-reviewed-followup", "handler-exact", "host-turn"); err != nil {
		t.Fatal(err)
	}
	if applied, _, err := e.PrepareAdmission("w-reviewed-followup", PrepareAdmissionInput{
		SessionID: "host-session", TurnToken: "host-turn", Receipt: "event-exact", ClaimToken: "handler-exact",
		PayloadSHA256: "handler-digest", ProcessIdentity: "host-process", PaneGeneration: "host-pane",
		Mode: AdmissionFresh, AttemptedAt: time.Now().UTC(),
	}); err != nil || !applied {
		t.Fatalf("prepare exact review delivery applied=%v err=%v", applied, err)
	}
	if _, err := e.MarkAdmissionAmbiguous("w-reviewed-followup", "host-turn", "provider outcome uncertain"); err != nil {
		t.Fatal(err)
	}
	st, err := e.PrepareTerminalReviewFollowUp("w-reviewed-followup", PrepareTerminalReviewFollowUpInput{
		AttemptSessionID: "session-1", AttemptToken: "turn-old", AttemptFence: 1, TerminalEvidenceID: "provider-terminal-exact",
		EventID: "rev-invalid", HandlerID: "handler-exact", HandlerToken: "host-turn",
		Admission: PrepareAdmissionInput{SessionID: "session-1", TurnToken: "turn-new", Receipt: "turn-new",
			PayloadSHA256: "digest-new", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
			Mode: AdmissionFresh, AttemptedAt: time.Now().UTC(), SignalProtocol: true},
	})
	if err != ErrReviewLease {
		t.Fatalf("wrong Event accepted: state=%+v err=%v", st, err)
	}
	before, _ := e.State("w-reviewed-followup")
	st, err = e.PrepareTerminalReviewFollowUp("w-reviewed-followup", PrepareTerminalReviewFollowUpInput{
		AttemptSessionID: "session-1", AttemptToken: "turn-old", AttemptFence: 1, TerminalEvidenceID: "provider-terminal-exact",
		EventID: before.Review.EventID, HandlerID: "handler-exact", HandlerToken: "host-turn",
		Admission: PrepareAdmissionInput{SessionID: "session-1", TurnToken: "turn-new", Receipt: "turn-new",
			PayloadSHA256: "digest-new", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
			Mode: AdmissionFresh, AttemptedAt: time.Now().UTC(), SignalProtocol: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt != nil || st.Review != nil || st.Attempts[0].SettledAt == nil || st.ActiveAdmission() == nil ||
		st.ActiveAdmission().TurnToken != "turn-new" {
		t.Fatalf("reviewed terminal follow-up state=%+v", st)
	}
	if handler := st.Admission("host-turn"); handler == nil || handler.Status != AdmissionAborted {
		t.Fatalf("consumed review delivery admission=%+v, want aborted", handler)
	}
}

func TestTerminalReviewFollowUpAdmissionOutcomesSurviveRestart(t *testing.T) {
	for _, outcome := range []string{"accepted", "ambiguous", "aborted"} {
		t.Run(outcome, func(t *testing.T) {
			root := t.TempDir()
			e, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			define(t, e, "w-outcome", PolicyUntilDone)
			admit(t, e, "w-outcome", "turn-old", "session-1")
			if _, err := e.OpenReviewEvent("w-outcome", "session.needs_input", "signal", "event-exact"); err != nil {
				t.Fatal(err)
			}
			claimed, err := e.ClaimReview("w-outcome", "handler-exact", "host-turn")
			if err != nil {
				t.Fatal(err)
			}
			attemptedAt := time.Now().UTC()
			command := PrepareTerminalReviewFollowUpInput{
				AttemptSessionID: "session-1", AttemptToken: "turn-old", AttemptFence: 1, TerminalEvidenceID: "provider-terminal-exact",
				EventID: claimed.Review.EventID, HandlerID: "handler-exact", HandlerToken: "host-turn",
				Admission: PrepareAdmissionInput{SessionID: "session-1", TurnToken: "turn-new", Receipt: "turn-new",
					PayloadSHA256: "digest-new", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
					Mode: AdmissionFresh, AttemptedAt: attemptedAt, SignalProtocol: true},
			}
			prepared, err := e.PrepareTerminalReviewFollowUp("w-outcome", command)
			if err != nil {
				t.Fatal(err)
			}
			switch outcome {
			case "accepted":
				if _, err := e.AcceptAdmissionBySignal("w-outcome", "turn-new", "session-1"); err != nil {
					t.Fatal(err)
				}
			case "ambiguous":
				if _, err := e.MarkAdmissionAmbiguous("w-outcome", "turn-new", "transport uncertain"); err != nil {
					t.Fatal(err)
				}
			case "aborted":
				if _, err := e.AbortAdmission("w-outcome", "turn-new", "turn-new", "digest-new", "not submitted"); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			replayed, _ := e.State("w-outcome")
			if replayed.Review != nil || replayed.Attempts[0].SettledAt == nil || replayed.Admission("turn-new") == nil {
				t.Fatalf("restart lost atomic transition: %+v", replayed)
			}
			want := AdmissionStatus(outcome)
			if outcome == "accepted" {
				want = AdmissionAccepted
			}
			if replayed.Admission("turn-new").Status != want {
				t.Fatalf("admission status=%s want=%s", replayed.Admission("turn-new").Status, want)
			}
			duplicate, err := e.PrepareTerminalReviewFollowUp("w-outcome", command)
			if err != nil || duplicate.Revision != replayed.Revision {
				t.Fatalf("exact duplicate changed state: rev=%d want=%d err=%v", duplicate.Revision, replayed.Revision, err)
			}
			stale := command
			stale.TerminalEvidenceID = "different-terminal"
			stale.Admission.TurnToken = "turn-stale"
			stale.Admission.Receipt = "turn-stale"
			if _, err := e.PrepareTerminalReviewFollowUp("w-outcome", stale); err != ErrStaleInput {
				t.Fatalf("stale terminal evidence err=%v", err)
			}
			_ = prepared
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

func TestExactActionableEventIdentityOwnsReviewAndOutbox(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-event", PolicyBounded)
	if _, err := e.OpenReviewEvent("w-event", "session.needs_input", "evidence-ref", "event-exact"); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w-event")
	if st.Review == nil || st.Review.EventID != "event-exact" || len(st.Outbox) != 1 ||
		st.Outbox[0].ID != reviewOutboxID("w-event", "event-exact") {
		t.Fatalf("exact event state=%+v", st)
	}
	if _, err := e.AckNotification("w-event", "event-exact"); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalAttemptReviewHasTotalExactTransitions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disposition Disposition
		wantStatus  Status
		nextAttempt bool
	}{
		{name: "wait", disposition: DispositionWait, wantStatus: StatusWaiting},
		{name: "complete", disposition: DispositionComplete, wantStatus: StatusDone},
		{name: "cancel", disposition: DispositionCancel, wantStatus: StatusCancelled},
		{name: "named next Attempt", disposition: DispositionContinue, wantStatus: StatusRunning, nextAttempt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newTestEngine(t)
			defer e.Close()
			const workID = "780022fd-807d-48bf-b187-ed2f1256eb2e"
			const eventID = "88c7095f526608d9"
			define(t, e, workID, PolicyUntilDone)
			admit(t, e, workID, "turn-attempt-409", "Session @409")
			if _, err := e.OpenReviewEvent(workID, "session.needs_input", "provider-terminal", eventID); err != nil {
				t.Fatal(err)
			}
			_, err := e.ClaimReview(workID, "handling-exact", "host-turn")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.MarkReviewDelivered(workID, "host-turn"); err != nil {
				t.Fatal(err)
			}
			in := ResolveTerminalReviewInput{
				EventID:   eventID,
				HandlerID: "handling-exact", HandlerToken: "host-turn",
				AttemptSessionID: "Session @409",
				AttemptToken:     "turn-attempt-409", AttemptFence: 1,
				TerminalEvidenceID: "provider-complete-409", Disposition: tc.disposition,
				Actor: "brain", WakeKind: WakeUserInput, WakeRef: eventID,
			}
			if tc.nextAttempt {
				in.NextSessionID, in.NextTurnToken = "Session @418", "turn-next-418"
			}
			st, err := e.ResolveTerminalReview(workID, in)
			if err != nil {
				t.Fatal(err)
			}
			if st.Review != nil || st.Status != tc.wantStatus || st.Attempts[0].SettledAt == nil {
				t.Fatalf("terminal review transition state=%+v", st)
			}
			if tc.nextAttempt {
				if st.Attempt == nil || st.Attempt.SessionID != "Session @418" || st.Attempt.TurnToken != "turn-next-418" {
					t.Fatalf("named next Attempt not admitted atomically: %+v", st.Attempt)
				}
			} else if st.Attempt != nil {
				t.Fatalf("old Attempt survived terminal evidence: %+v", st.Attempt)
			}
		})
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
	admission := st.Admission("turn-1")
	if admission == nil || !admission.AttemptedAt.Equal(at) {
		t.Fatalf("historical admission.prepared must inherit event time, got %+v", admission)
	}
}
