package lifecycle

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDueRetryIgnoresUnrelatedInputAndWakesExactlyOnce(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC)
	setNow(e, now)
	define(t, e, "w-due-retry", PolicyBounded)
	opened, err := e.OpenReviewEvent("w-due-retry", "external_check", "run:49dc23f4", "event:external-check")
	if err != nil {
		t.Fatal(err)
	}
	dueAt := now.Add(5 * time.Minute)
	waiting, err := e.ResolveReview("w-due-retry", opened.Review.EventID, ResolveReviewInput{
		Disposition:   DispositionWait,
		WakeKind:      WakeDueRetry,
		WakeRef:       "external-run:49dc23f4",
		NextAttemptAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != StatusWaiting || waiting.Review != nil || waiting.Wake == nil ||
		waiting.Wake.Kind != WakeDueRetry || waiting.Wake.NextAttemptAt == nil || !waiting.Wake.NextAttemptAt.Equal(dueAt) {
		t.Fatalf("durable due wait=%+v", waiting)
	}

	revision := waiting.Revision
	e.mu.Lock()
	eventCount := len(e.events)
	e.mu.Unlock()
	for i := 0; i < 32; i++ {
		unrelated, clearErr := e.ClearWait("w-due-retry", WakeUserInput, "brain-thread:unrelated", fmt.Sprintf("message:%d", i))
		if clearErr != nil {
			t.Fatal(clearErr)
		}
		if unrelated.Revision != revision {
			t.Fatalf("unrelated input revised Work: %d -> %d", revision, unrelated.Revision)
		}
	}
	e.mu.Lock()
	if len(e.events) != eventCount {
		t.Fatalf("unrelated input appended lifecycle Events: %d -> %d", eventCount, len(e.events))
	}
	e.mu.Unlock()

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	setNow(reopened, dueAt.Add(-time.Nanosecond))
	if next, ok := reopened.NextWakeAt(); !ok || !next.Equal(dueAt) {
		t.Fatalf("reloaded next wake=(%v,%v), want %v", next, ok, dueAt)
	}
	if err := reopened.Sweep(); err != nil {
		t.Fatal(err)
	}
	beforeDue, _ := reopened.State("w-due-retry")
	if beforeDue.Revision != revision || beforeDue.Review != nil {
		t.Fatalf("retry woke before due: %+v", beforeDue)
	}

	setNow(reopened, dueAt)
	for i := 0; i < 32; i++ {
		if err := reopened.Sweep(); err != nil {
			t.Fatal(err)
		}
	}
	due, err := reopened.State("w-due-retry")
	if err != nil {
		t.Fatal(err)
	}
	if due.Revision != revision+1 || due.Status != StatusQueued || due.Wake != nil ||
		due.Review == nil || due.Review.Reason != "retry_due" || due.Review.Ref != "external-run:49dc23f4" {
		t.Fatalf("due retry state=%+v", due)
	}
	if cards := reopened.Cards(); len(cards) != 1 || !cards[0].Actionable || cards[0].Reason != "retry_due" {
		t.Fatalf("due retry cards=%+v", cards)
	}
	dueSource := fmt.Sprintf("due-retry:%s:%d", "external-run:49dc23f4", dueAt.UnixNano())
	if got := countEngineEvents(reopened, "w-due-retry", KReviewOpened, dueSource); got != 1 {
		t.Fatalf("due retry actionable Events=%d, want 1", got)
	}
}

func TestExpiredClaimReusesExactEventID(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-claim", PolicyBounded)
	opened, err := e.OpenReviewEvent("w-claim", "needs_input", "question", "event-exact")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := e.ClaimReview("w-claim", "parallel-handler-id", "host-turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Review.EventID != "event-exact" ||
		claimed.Review.Handler.HandlerID != "parallel-handler-id" {
		t.Fatalf("event identity split: %+v", claimed.Review)
	}
	setNow(e, claimed.Review.Handler.ClaimExpiresAt)
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	expired, _ := e.State("w-claim")
	if expired.Review == nil || expired.Review.EventID != opened.Review.EventID || expired.Review.Handler != nil {
		t.Fatalf("claim expiry replaced event: %+v", expired.Review)
	}
	reclaimed, err := e.ClaimReview("w-claim", "ignored", "host-turn-2")
	if err != nil || reclaimed.Review.EventID != "event-exact" {
		t.Fatalf("same event not reclaimable: review=%+v err=%v", reclaimed.Review, err)
	}
	delivered, err := e.MarkReviewDelivered("w-claim", "host-turn-2")
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Review.Handler.DeliveredAt == nil {
		t.Fatalf("same Review delivery not recorded: %+v", delivered.Review)
	}
}

func TestAttemptSessionTokenFenceAndHeartbeatSemantics(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-fence", PolicyUntilDone)
	st := admit(t, e, "w-fence", "turn-current", "session-current")
	base := st.Revision
	if _, err := e.Heartbeat("w-fence", attemptID("session-current", "turn-current", st.Attempt.Generation), 300); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := e.State("w-fence")
	if unchanged.Revision != base {
		t.Fatalf("identical heartbeat advanced Work revision: %d -> %d", base, unchanged.Revision)
	}
	if _, err := e.Heartbeat("w-fence", attemptID("session-wrong", "turn-current", st.Attempt.Generation), 300); !errors.Is(err, ErrStaleInput) {
		t.Fatalf("wrong Session accepted: %v", err)
	}
	if _, err := e.ReportTurnDone("w-fence", attemptID("session-current", "turn-old", st.Attempt.Generation), DoneInput{OK: true}); !errors.Is(err, ErrStaleInput) {
		t.Fatalf("old token accepted: %v", err)
	}
	if _, err := e.ReportTurnDone("w-fence", attemptID("session-current", "turn-current", st.Attempt.Generation+1), DoneInput{OK: true}); !errors.Is(err, ErrStaleInput) {
		t.Fatalf("wrong fence accepted: %v", err)
	}
	completed, err := e.ReportTurnDone("w-fence", attemptID("session-current", "turn-current", st.Attempt.Generation), DoneInput{OK: true, Final: true})
	if err != nil {
		t.Fatal(err)
	}
	revision := completed.Revision
	again, err := e.ReportTurnDone("w-fence", attemptID("session-current", "turn-current", st.Attempt.Generation), DoneInput{OK: true, Final: true})
	if err != nil || again.Revision != revision {
		t.Fatalf("exact duplicate completion mutated state: rev=%d -> %d err=%v", revision, again.Revision, err)
	}
}

func TestAttemptTokenCannotBeReusedByAnotherSession(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-token-session", PolicyUntilDone)
	admit(t, e, "w-token-session", "turn-exact", "session-1")
	applied, _, err := e.AdmitTurn("w-token-session", AdmitTurnInput{SessionID: "session-2", TurnToken: "turn-exact", Delegated: true})
	if applied || !errors.Is(err, ErrStaleInput) {
		t.Fatalf("token reused by another Session: applied=%v err=%v", applied, err)
	}
}

func TestLostAttemptDoesNotCompleteAndNextAttemptCanContinue(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-lost", PolicyUntilDone)
	admit(t, e, "w-lost", "turn-lost", "session-1")
	lost, err := e.ReportTurnLost("w-lost", attemptID("session-1", "turn-lost", 1), "process disappeared")
	if err != nil {
		t.Fatal(err)
	}
	if lost.Status == StatusDone || lost.Attempt != nil {
		t.Fatalf("lost Attempt completed or stayed active: %+v", lost)
	}
	applied, nextAttempt, err := e.AdmitTurn("w-lost", AdmitTurnInput{
		SessionID: "session-2", TurnToken: "turn-next", FollowUpOf: "turn-lost", Delegated: true,
	})
	if err != nil || !applied || nextAttempt.Attempt == nil || nextAttempt.Attempt.TurnToken != "turn-next" {
		t.Fatalf("next Attempt admission: applied=%v state=%+v err=%v", applied, nextAttempt, err)
	}
}

func TestLateExactTerminalUpgradesStableLeaseEventAndBrainCanContinue(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-late-terminal", PolicyUntilDone)
	admitted := admit(t, e, "w-late-terminal", "turn-old", "session-1")
	identity := attemptID("session-1", "turn-old", admitted.Attempt.Generation)
	setNow(e, admitted.Attempt.LeaseDeadline.Add(LostGrace+time.Minute))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	lost, _ := e.State("w-late-terminal")
	if lost.Attempt != nil || lost.Review == nil || lost.Review.Reason != "lease_expired" {
		t.Fatalf("provisional lease result=%+v", lost)
	}
	eventID, lostRevision := lost.Review.EventID, lost.Revision

	stronger, err := e.ReportTurnDone("w-late-terminal", identity, DoneInput{
		OK: true, Summary: "provider later confirmed the exact turn completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stronger.Revision != lostRevision+1 || stronger.Review == nil ||
		stronger.Review.EventID != eventID || stronger.Review.Reason != "turn_done" {
		t.Fatalf("stronger evidence replaced stable Event: %+v", stronger)
	}
	if cards := e.Cards(); len(cards) != 1 || !cards[0].Actionable || cards[0].Reason != "turn_done" {
		t.Fatalf("stronger evidence cards=%+v", cards)
	}

	claimed, err := e.ClaimReview("w-late-terminal", "brain-handler", "brain-turn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkReviewDelivered("w-late-terminal", "brain-turn"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.PrepareAdmission("w-late-terminal", PrepareAdmissionInput{
		SessionID: "session-2", TurnToken: "turn-next", Receipt: "scoped follow-up",
		PayloadSHA256: "digest-next", ProcessIdentity: "process-2", PaneGeneration: "pane-2",
		Mode: AdmissionFresh, Purpose: AdmissionPurposeReview,
		PurposeID: claimed.Review.Handler.HandlerID, AttemptedAt: time.Now().UTC(), SignalProtocol: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptAdmissionBySignal("w-late-terminal", "turn-next", "session-2"); err != nil {
		t.Fatal(err)
	}
	continued, err := e.AcceptReviewFollowUp("w-late-terminal", eventID, "session-2", "turn-next")
	if err != nil {
		t.Fatal(err)
	}
	if continued.Review != nil || continued.Attempt == nil || continued.Attempt.TurnToken != "turn-next" {
		t.Fatalf("Brain could not disposition stable Event from stronger evidence: %+v", continued)
	}
}
