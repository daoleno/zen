package lifecycle

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	return e, root
}

func define(t *testing.T, e *Engine, id WorkID, policy Policy) *State {
	t.Helper()
	st, err := e.DefineWork(id, DefineWorkInput{
		Title: "T " + string(id), Objective: "obj " + string(id), Policy: policy,
	})
	if err != nil {
		t.Fatalf("define work: %v", err)
	}
	return st
}

const (
	tok1 TurnToken = "turn-1"
	tok2 TurnToken = "turn-2"
)

func admit(t *testing.T, e *Engine, id WorkID, token TurnToken, session string) *State {
	t.Helper()
	applied, st, err := e.AdmitTurn(id, AdmitTurnInput{SessionID: session, Delegated: true, TurnToken: token})
	if err != nil || !applied {
		t.Fatalf("admit %s: applied=%v err=%v", token, applied, err)
	}
	return st
}

func attemptID(session string, token TurnToken, fence uint64) AttemptIdentity {
	return AttemptIdentity{SessionID: session, TurnToken: token, Fence: fence}
}

// 1. Deterministic replay: reducing the same log twice yields identical state.
func TestReplayDeterminism(t *testing.T) {
	e, root := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")
	if _, err := e.Heartbeat("w1", attemptID("s1", tok1, 1), 120); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: true, Summary: "partial"}); err != nil {
		t.Fatal(err)
	}

	want, err := e.State("w1")
	if err != nil {
		t.Fatal(err)
	}

	// Append-only audit Events can still be reduced deterministically, but
	// recovery reads the current rows from the same transaction image.
	e.mu.Lock()
	events := append([]Event(nil), e.events...)
	e.mu.Unlock()
	var replayed *State
	for _, ev := range events {
		replayed = Reduce(replayed, ev)
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(replayed)
	if string(a) != string(b) {
		t.Fatalf("replay mismatch:\nwant %s\ngot  %s", a, b)
	}
	_ = root
}

// 2. Duplicates: same SourceID applies once; duplicate admission is a no-op.
func TestDuplicateEventsIdempotent(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)
	admit(t, e, "w1", tok1, "s1")

	// Duplicate admission of the same token.
	applied, st, err := e.AdmitTurn("w1", AdmitTurnInput{SessionID: "s1", Delegated: true, TurnToken: tok1})
	if err != nil || applied {
		t.Fatalf("duplicate admission: applied=%v err=%v", applied, err)
	}
	if st.Attempt == nil || st.Attempt.TurnToken != tok1 || st.Attempt.Generation != 1 {
		t.Fatalf("Attempt changed by duplicate: %+v", st.Attempt)
	}

	before := st.Revision
	ev := Event{WorkID: "w1", Kind: KTurnDone, TurnToken: tok1, Fence: 1, SourceID: "done:" + string(tok1), At: time.Now(), Payload: DonePayload{OK: true, CriteriaMet: true}}
	once := Reduce(st, ev)
	if once.Revision != before+1 {
		t.Fatalf("first application did not apply: %d -> %d", before, once.Revision)
	}
	twice := Reduce(once, ev)
	if twice.Revision != once.Revision {
		t.Fatalf("duplicate source applied twice: rev %d -> %d", once.Revision, twice.Revision)
	}
	if twice.Status != StatusDone || once.Status != StatusDone {
		t.Fatalf("terminal settle unstable: %s vs %s", once.Status, twice.Status)
	}
}

// 3. Out-of-order arrival reduces to the same state as in-order for facts;
// lease deadlines use monotonic max.
func TestOutOfOrderFacts(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)

	// Heartbeat arriving before its admission event is stale (rejected), then
	// the late done after re-admission cannot touch the newer turn.
	if _, err := e.Heartbeat("w1", attemptID("s1", tok1, 1), 60); err != ErrStaleInput {
		t.Fatalf("heartbeat without active Attempt: err=%v", err)
	}
	admit(t, e, "w1", tok1, "s1")
	deadlineBefore, err := e.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	// A heartbeat with a tiny lease must not shorten the deadline (monotonic).
	if _, err := e.Heartbeat("w1", attemptID("s1", tok1, 1), -5); err != nil {
		t.Fatal(err)
	}
	after, err := e.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	if !after.Attempt.LeaseDeadline.Equal(deadlineBefore.Attempt.LeaseDeadline) {
		t.Fatalf("lease deadline moved backwards: %v -> %v",
			deadlineBefore.Attempt.LeaseDeadline, after.Attempt.LeaseDeadline)
	}
}

// 4. Stale fence: inputs from a released generation are rejected idempotently.
func TestStaleFenceRejected(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")

	// Settle turn 1; fence moves to 2 with no active Attempt.
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: true, CriteriaMet: true}); err != nil {
		t.Fatal(err)
	}
	st, err := e.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusDone {
		t.Fatalf("bounded criteriaMet until_done should be done, got %s", st.Status)
	}

	// An exact duplicate is an idempotent no-op even when its payload differs.
	if duplicate, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: false, Summary: "late failure"}); err != nil || duplicate.Revision != st.Revision {
		t.Fatalf("duplicate result mutated state: state=%+v err=%v", duplicate, err)
	}
	if _, err := e.Heartbeat("w1", attemptID("s1", tok1, 1), 300); err != ErrStaleInput {
		t.Fatalf("late heartbeat accepted: %v", err)
	}
	st, _ = e.State("w1")
	if st.Status != StatusDone {
		t.Fatalf("terminal status disturbed: %s", st.Status)
	}
}

// 5. Lease expiry: sweep emits one expiry per missed deadline and escalates to
// lost only past LostGrace; takeover continues automatically.
func TestLeaseExpiryAndEscalation(t *testing.T) {
	e, root := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")

	st0, _ := e.State("w1")
	deadline := st0.Attempt.LeaseDeadline

	// Simulate time passing just past the deadline but within LostGrace.
	setNow(e, deadline.Add(time.Minute))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w1")
	if st.Status != StatusRunning || st.Attempt == nil {
		t.Fatalf("expiry must not release Attempt: %+v", st)
	}
	if st.Review == nil || st.Review.Reason != "lease_expired" {
		t.Fatalf("expected lease_expired review, got %+v", st.Review)
	}

	// Sweeping again at the same deadline dedupes: no new event applies at all.
	rev := st.Revision
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w1")
	if st.Revision != rev {
		t.Fatalf("duplicate expiry applied: %d -> %d", rev, st.Revision)
	}
	if n := countReviews(st, "lease_expired"); n != 1 {
		t.Fatalf("expected single open Event, got %d", n)
	}

	// Past LostGrace the supervisor escalates to one blocked Review. It never
	// creates an automatic takeover state.
	setNow(e, deadline.Add(LostGrace+time.Minute))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w1")
	if st.Attempt != nil {
		t.Fatalf("Attempt not released after escalation: %+v", st.Attempt)
	}
	if st.Status != StatusBlocked || st.Review == nil {
		t.Fatalf("expected blocked Review after loss, got %+v", st)
	}
	_ = root
}

func TestLeaseExpiryAdmissionIsConcurrentAndRestartDurable(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w-expiry-once", PolicyUntilDone)
	admitted := admit(t, e, "w-expiry-once", "turn:expiry-once", "session-expiry")
	deadline := admitted.Attempt.LeaseDeadline
	setNow(e, deadline.Add(time.Minute))

	const scans = 64
	errs := make(chan error, scans)
	var wg sync.WaitGroup
	for i := 0; i < scans; i++ {
		wg.Add(1)
		go func(reportDirectly bool) {
			defer wg.Done()
			if reportDirectly {
				_, err := e.ReportLeaseExpired("w-expiry-once", "turn:expiry-once")
				errs <- err
				return
			}
			err := e.Sweep()
			errs <- err
		}(i%2 == 0)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent expiry scan: %v", err)
		}
	}

	state, err := e.State("w-expiry-once")
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != admitted.Revision+1 {
		t.Fatalf("expiry revision=%d, want %d", state.Revision, admitted.Revision+1)
	}
	if state.Review == nil || state.Review.Reason != "lease_expired" {
		t.Fatalf("canonical actionable Event=%+v", state.Review)
	}
	eventID := state.Review.EventID
	sourceID := leaseExpiredSourceID("turn:expiry-once", deadline)
	if !state.SeenSources[sourceID] {
		t.Fatalf("durable source admission missing %q", sourceID)
	}
	if got := countEngineEvents(e, "w-expiry-once", KLeaseExpired, sourceID); got != 1 {
		t.Fatalf("expiry audit Events=%d, want 1", got)
	}
	if cards := e.Cards(); len(cards) != 1 || !cards[0].Actionable || cards[0].Reason != "lease_expired" {
		t.Fatalf("actionable cards=%+v", cards)
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	setNow(reopened, deadline.Add(time.Minute))
	for i := 0; i < scans; i++ {
		if err := reopened.Sweep(); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := reopened.State("w-expiry-once")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Revision != state.Revision || reloaded.Review == nil || reloaded.Review.EventID != eventID ||
		!reloaded.SeenSources[sourceID] {
		t.Fatalf("reload changed expiry admission: before=%+v after=%+v", state, reloaded)
	}
	if got := countEngineEvents(reopened, "w-expiry-once", KLeaseExpired, sourceID); got != 1 {
		t.Fatalf("reloaded expiry audit Events=%d, want 1", got)
	}
}

func TestObservedLongRunningProviderPhasePreventsLeaseLoss(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-observed-long-phase", PolicyUntilDone)
	admit(t, e, "w-observed-long-phase", tok1, "s1")

	initial, _ := e.State("w-observed-long-phase")
	oldDeadline := initial.Attempt.LeaseDeadline
	setNow(e, oldDeadline.Add(time.Minute))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	expired, _ := e.State("w-observed-long-phase")
	if expired.Attempt == nil || expired.Review == nil || expired.Review.Reason != "lease_expired" {
		t.Fatalf("expiry must retain observed Attempt pending reconciliation: %+v", expired)
	}

	// The provider is still visibly executing the exact current token. That
	// observation renews its deadline before the loss sweep; elapsed wall time
	// alone cannot terminalize or release the Attempt.
	observedAt := oldDeadline.Add(LostGrace + time.Minute)
	setNow(e, observedAt)
	if _, err := e.Progress("w-observed-long-phase", attemptID("s1", tok1, expired.Attempt.Generation), "provider phase still running"); err != nil {
		t.Fatal(err)
	}
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w-observed-long-phase")
	if st.Attempt == nil || st.Attempt.TurnToken != tok1 ||
		!st.Attempt.LeaseDeadline.After(observedAt) {
		t.Fatalf("live provider phase was mistaken for Attempt loss: %+v", st)
	}
}

func TestProviderRunningRenewalsAreCoalescedAndSilenceStillExpires(t *testing.T) {
	base := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	e, _ := newTestEngine(t)
	defer e.Close()
	setNow(e, base)
	define(t, e, "w-coalesced-live", PolicyUntilDone)
	initial := admit(t, e, "w-coalesced-live", tok1, "s1")
	initialRevision := initial.Revision
	originalDeadline := initial.Attempt.LeaseDeadline

	// Observation frequency is not durable state. Four identical observations
	// cannot materially extend the initial ten-minute lease and append nothing.
	for minute := 1; minute < 5; minute++ {
		setNow(e, base.Add(time.Duration(minute)*time.Minute))
		if _, err := e.Progress("w-coalesced-live", attemptID("s1", tok1, initial.Attempt.Generation), "same running activity"); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := e.State("w-coalesced-live")
	if st.Revision != initialRevision || !st.Attempt.LeaseDeadline.Equal(originalDeadline) {
		t.Fatalf("sub-threshold observations churned state: revision=%d deadline=%v", st.Revision, st.Attempt.LeaseDeadline)
	}

	// At half a lease window the same observation materially extends expiry and
	// produces exactly one durable renewal. Repeating it is a no-op.
	setNow(e, base.Add(5*time.Minute))
	if _, err := e.Progress("w-coalesced-live", attemptID("s1", tok1, initial.Attempt.Generation), "same running activity"); err != nil {
		t.Fatal(err)
	}
	renewed, _ := e.State("w-coalesced-live")
	if renewed.Revision != initialRevision+1 || !renewed.Attempt.LeaseDeadline.Equal(base.Add(15*time.Minute)) {
		t.Fatalf("material renewal=%+v", renewed.Attempt)
	}
	if _, err := e.Progress("w-coalesced-live", attemptID("s1", tok1, renewed.Attempt.Generation), "same running activity"); err != nil {
		t.Fatal(err)
	}
	repeated, _ := e.State("w-coalesced-live")
	if repeated.Revision != renewed.Revision {
		t.Fatalf("identical observation appended twice: %d -> %d", renewed.Revision, repeated.Revision)
	}

	// The renewed Attempt is live after the original deadline.
	setNow(e, originalDeadline.Add(time.Minute))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	active, _ := e.State("w-coalesced-live")
	if active.Attempt == nil || active.Review != nil {
		t.Fatalf("active provider observation expired at old deadline: %+v", active)
	}

	// A truly silent aggregate still follows normal expiry and loss.
	setNow(e, base)
	define(t, e, "w-silent", PolicyUntilDone)
	silent := admit(t, e, "w-silent", tok2, "s2")
	setNow(e, silent.Attempt.LeaseDeadline.Add(time.Second))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	expired, _ := e.State("w-silent")
	if expired.Attempt == nil || expired.Review == nil || expired.Review.Reason != "lease_expired" {
		t.Fatalf("silent Attempt did not expire: %+v", expired)
	}
	setNow(e, silent.Attempt.LeaseDeadline.Add(LostGrace+time.Second))
	if err := e.Sweep(); err != nil {
		t.Fatal(err)
	}
	lost, _ := e.State("w-silent")
	if lost.Attempt != nil || lost.CurrentTurn() != nil || lost.Review == nil {
		t.Fatalf("silent Attempt did not become lost: %+v", lost)
	}
}

func TestSignalSteerPromotesExactPromptToken(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w-signal-steer", PolicyUntilDone)
	old := admit(t, e, "w-signal-steer", "turn:old", "session-1")
	if _, _, err := e.PrepareAdmission("w-signal-steer", PrepareAdmissionInput{
		SessionID: "session-1", TurnToken: "turn:current-prompt", Receipt: "turn:current-prompt",
		PayloadSHA256: "digest-current", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
		Mode: AdmissionConditionalSteer, ExistingTurnToken: "turn:old", BaselineActivityID: "activity-1",
		SignalProtocol: true, AttemptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := e.AcceptAdmissionBySignal("w-signal-steer", "turn:current-prompt", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Attempt == nil || accepted.Attempt.TurnToken != "turn:current-prompt" ||
		accepted.Attempt.Generation <= old.Attempt.Generation || accepted.AdmissionByToken("turn:current-prompt").ResultTurnToken != "turn:current-prompt" {
		t.Fatalf("signal steer did not promote exact prompt token: %+v", accepted)
	}
	if accepted.Attempt.FollowUpOf != "turn:old" {
		t.Fatalf("signal steer lineage=%+v", accepted.Attempt)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, _ := reopened.State("w-signal-steer")
	if replayed.Attempt == nil || replayed.Attempt.TurnToken != "turn:current-prompt" {
		t.Fatalf("reload changed exact Attempt: %+v", replayed)
	}
}

// 6. Next Attempt admission follows the settled predecessor.
func TestNextAttemptAdmission(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")

	// Admission while owned is rejected.
	if _, _, err := e.AdmitTurn("w1", AdmitTurnInput{SessionID: "s2", Delegated: true, TurnToken: tok2}); err != ErrAttemptActive {
		t.Fatalf("admission while owned: %v", err)
	}

	// The active Attempt settles with criteria unmet. No next Attempt exists
	// until Brain explicitly names one after reviewing the stable Event.
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: true, Summary: "step 1"}); err != nil {
		t.Fatal(err)
	}
	st, err := e.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt != nil || st.Review == nil {
		t.Fatalf("terminal result invented continuation state: %+v", st)
	}
	applied, st, err := e.AdmitTurn("w1", AdmitTurnInput{
		SessionID: "s2", Delegated: true, TurnToken: tok2, FollowUpOf: tok1,
	})
	if err != nil || !applied {
		t.Fatalf("follow-up admission: applied=%v err=%v", applied, err)
	}
	if st.Attempt == nil || st.Attempt.TurnToken != tok2 || st.Attempt.Generation != 3 {
		t.Fatalf("follow-up Attempt wrong: %+v", st.Attempt)
	}
	cur := st.CurrentTurn()
	if cur == nil || cur.FollowUpOf != tok1 {
		t.Fatalf("follow-up lineage broken: %+v", cur)
	}

}

func TestAdmissionPurposeRequiresCompleteTag(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w-tagged", PolicyUntilDone)
	if _, _, err := e.PrepareAdmission("w-tagged", PrepareAdmissionInput{
		SessionID: "session-next", TurnToken: "turn-next", Receipt: "turn-next", PayloadSHA256: "digest",
		ProcessIdentity: "process", PaneGeneration: "pane", Mode: AdmissionFresh, AttemptedAt: time.Now(),
		Purpose: AdmissionPurposeReview,
	}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("partial admission purpose accepted: %v", err)
	}
}

func TestReviewAcceptancePersistsRowsAndEventsInOneTransactionImage(t *testing.T) {
	root := t.TempDir()
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w-atomic", PolicyUntilDone)
	admit(t, e, "w-atomic", "turn-old", "session-old")
	if _, err := e.OpenReview("w-atomic", "session.needs_input", "event-exact"); err != nil {
		t.Fatal(err)
	}
	claimed, err := e.ClaimReview("w-atomic", "handling-exact", "brain-turn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkReviewDelivered("w-atomic", "brain-turn"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.PrepareAdmission("w-atomic", PrepareAdmissionInput{
		SessionID: "session-next", TurnToken: "turn-next", Receipt: "turn-next",
		PayloadSHA256: "digest-next", ProcessIdentity: "process-next", PaneGeneration: "pane-next",
		Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: claimed.Review.Handler.HandlerID, AttemptedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptAdmission("w-atomic", "turn-next", AcceptAdmissionInput{
		SessionID: "session-next", Receipt: "turn-next", PayloadSHA256: "digest-next",
		ActivityID: "activity-next", AdmissionID: "admission-next", AdmissionSHA256: "digest-next",
	}); err != nil {
		t.Fatal(err)
	}
	acceptedNextAttempt, _ := e.State("w-atomic")
	if admission := acceptedNextAttempt.AdmissionByToken("turn-next"); admission == nil || admission.Status != AdmissionAccepted ||
		admission.Purpose != AdmissionPurposeReview || admission.PurposeID != claimed.Review.Handler.HandlerID {
		t.Fatalf("accepted admission lacks exact review purpose: %+v", admission)
	}
	if _, _, err := e.PrepareAdmission("w-atomic", PrepareAdmissionInput{
		SessionID: "session-next", TurnToken: "turn-other", Receipt: "turn-other",
		PayloadSHA256: "digest-other", ProcessIdentity: "process-next", PaneGeneration: "pane-next",
		Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: claimed.Review.Handler.HandlerID, AttemptedAt: time.Now(),
	}); !errors.Is(err, ErrAttemptActive) {
		t.Fatalf("second review-purpose admission token accepted: %v", err)
	}
	if _, err := e.AcceptReviewFollowUp("w-atomic", claimed.Review.EventID, "session-next", "turn-next"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	st, err := reopened.State("w-atomic")
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt == nil || st.Attempt.SessionID != "session-next" || st.Review != nil {
		t.Fatalf("atomic transaction image is incomplete: %+v", st)
	}
	database, err := readLifecycleDatabase(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if persisted := database.Works["w-atomic"]; persisted == nil || persisted.Attempt == nil ||
		persisted.Attempt.TurnToken != "turn-next" {
		t.Fatalf("current Work row missing next Attempt: %+v", persisted)
	}
	if last := database.Events[len(database.Events)-1]; last.Kind != KTurnAdmitted || last.TurnToken != "turn-next" {
		t.Fatalf("append-only Event missing from same image: %+v", last)
	}
}

// 7. Provider turn mismatch: a provider terminal for an unknown/old activity
// never settles the current turn.
func TestProviderTurnMismatch(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)
	admit(t, e, "w1", tok1, "s1")

	// Unknown token reports are stale.
	if _, err := e.ReportTurnDone("w1", attemptID("s1", "turn-unknown", 99), DoneInput{OK: true}); err != ErrStaleInput {
		t.Fatalf("unknown token accepted: %v", err)
	}
	// Wrong fence for the right token is stale.
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 7), DoneInput{OK: true}); err != ErrStaleInput {
		t.Fatalf("wrong fence accepted: %v", err)
	}
	st, _ := e.State("w1")
	if st.Status != StatusRunning || st.CurrentTurn() == nil || st.CurrentTurn().TurnToken != tok1 {
		t.Fatalf("mismatched provider fact mutated state: %+v", st)
	}
}

// 8. Restart recovery: reopen from disk reproduces identical current state and
// actionable Review.
func TestRestartRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: true, Summary: "phase1"}); err != nil {
		t.Fatal(err)
	}
	want, _ := e.State("w1")
	wantCards := e.Cards()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer e2.Close()
	got, err := e2.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("restart mismatch:\nwant %s\ngot  %s", a, b)
	}
	gotCards := e2.Cards()
	if len(gotCards) != len(wantCards) {
		t.Fatalf("card projection changed across restart")
	}
	for i := range wantCards {
		if wantCards[i].Actionable != gotCards[i].Actionable || wantCards[i].Reason != gotCards[i].Reason {
			t.Fatalf("card %d changed: %+v vs %+v", i, wantCards[i], gotCards[i])
		}
	}

}

// until_done completion rule: provider done alone never completes; criteria
// met does; failed turns block with a review.
func TestUntilDoneCompletionRule(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")

	// Provider says done but criteria unmet -> queued with exactly one review.
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: true, Summary: "not finished"}); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w1")
	if st.Status != StatusQueued {
		t.Fatalf("until_done provider-done must not complete: %s", st.Status)
	}
	if st.Review == nil || st.Review.Reason != "turn_done" {
		t.Fatalf("missing turn_done review: %+v", st.Review)
	}

	// Follow-up meets criteria → done.
	applied, _, err := e.AdmitTurn("w1", AdmitTurnInput{SessionID: "s2", Delegated: true, TurnToken: tok2, FollowUpOf: tok1})
	if err != nil || !applied {
		t.Fatalf("follow-up admit: %v %v", applied, err)
	}
	if _, err := e.ResolveReview("w1", st.Review.EventID, ResolveReviewInput{Disposition: DispositionContinue, Actor: "brain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReportTurnDone("w1", attemptID("s2", tok2, 3), DoneInput{OK: true, CriteriaMet: true, Summary: "all done"}); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w1")
	if st.Status != StatusDone {
		t.Fatalf("criteriaMet must complete: %s", st.Status)
	}
}

// Cards: exactly one actionable card per lineage, replaced in place.
func TestCardProjectionSingleActionable(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)
	define(t, e, "w2", PolicyBounded)

	cards := e.Cards()
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	for _, c := range cards {
		if c.Actionable {
			t.Fatalf("fresh work actionable: %+v", c)
		}
	}

	admit(t, e, "w1", tok1, "s1")
	if _, err := e.ReportTurnLost("w1", attemptID("s1", tok1, 1), "evidence gone"); err != nil {
		t.Fatal(err)
	}
	// Two historical facts (lost + earlier expiry) still yield ONE card.
	if _, err := e.SetWait("w2", WakeUserInput, "thread-9"); err != nil {
		t.Fatal(err)
	}
	cards = e.Cards()
	byID := map[WorkID]Card{}
	for _, c := range cards {
		byID[c.WorkID] = c
	}
	w1 := byID["w1"]
	if !w1.Actionable || w1.Reason != "turn_lost" {
		t.Fatalf("w1 card wrong: %+v", w1)
	}
	w2 := byID["w2"]
	if w2.Actionable || w2.Status != StatusWaiting {
		t.Fatalf("w2 card wrong: %+v", w2)
	}
}

// Review lifecycle: claim → deliver → resolve; double claim rejected; resolve
// of a superseded Event is a no-op.
func TestReviewLifecycle(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)
	admit(t, e, "w1", tok1, "s1")
	if _, err := e.ReportTurnDone("w1", attemptID("s1", tok1, 1), DoneInput{OK: false, Summary: "boom"}); err != nil {
		t.Fatal(err)
	}
	st, _ := e.State("w1")
	eventID := st.Review.EventID

	if _, err := e.ClaimReview("w1", "host-1", "hturn-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("w1", "host-2", "hturn-2"); err != ErrReviewLease {
		t.Fatalf("double claim: %v", err)
	}
	if _, err := e.MarkReviewDelivered("w1", "hturn-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResolveReview("w1", eventID, ResolveReviewInput{Disposition: DispositionComplete, Actor: "brain"}); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w1")
	if st.Status != StatusDone || st.Review != nil {
		t.Fatalf("resolve complete failed: %+v", st)
	}
	// Resolving again is an idempotent no-op.
	if _, err := e.ResolveReview("w1", eventID, ResolveReviewInput{Disposition: DispositionCancel}); err != nil {
		t.Fatalf("double resolve: %v", err)
	}
}

func TestAmbiguousReviewDeliveryResolutionIsAtomicIdempotentAndReplayable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	e, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	define(t, e, "w1", PolicyBounded)
	if _, err := e.OpenReview("w1", "operator_review", "canonical-only"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("w1", "host-1", "host-turn-1"); err != nil {
		t.Fatal(err)
	}
	before, _ := e.State("w1")
	st, err := e.ResolveReviewDelivery(
		"w1", ReviewDeliveryMarkDelivered, "operator", "visible in Host transcript",
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.Review == nil || st.Review.Handler != nil {
		t.Fatalf("resolution did not release exact handler: %+v", st.Review)
	}
	if st.Revision != before.Revision+1 {
		t.Fatalf("resolution was not one canonical transition: %d -> %d", before.Revision, st.Revision)
	}

	// A lost response may cause the CLI to retry. The retry is a no-op, not an
	// error and not a second audit event.
	retry, err := e.ResolveReviewDelivery(
		"w1", ReviewDeliveryMarkDelivered, "operator", "visible in Host transcript",
	)
	if err != nil || retry.Revision != st.Revision {
		t.Fatalf("idempotent retry state=%+v err=%v", retry, err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, err := reopened.State("w1")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Review == nil || replayed.Review.Handler != nil || replayed.Revision != st.Revision {
		t.Fatalf("resolution changed on replay: %+v", replayed)
	}
}

func TestAmbiguousReviewDiscardClosesExactEvent(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyBounded)
	if _, err := e.OpenReview("w1", "operator_review", "canonical-only"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("w1", "host-1", "host-turn-1"); err != nil {
		t.Fatal(err)
	}
	st, err := e.ResolveReviewDelivery("w1", ReviewDeliveryDiscard, "operator", "obsolete")
	if err != nil {
		t.Fatal(err)
	}
	if st.Review != nil {
		t.Fatalf("discard retained actionable Event: %+v", st.Review)
	}
	if _, err := e.ResolveReviewDelivery("w1", ReviewDeliveryDiscard, "operator", "obsolete"); err != nil {
		t.Fatalf("discard retry: %v", err)
	}
}

// Cancel releases an active Attempt and drops pending continuations.
func TestCancelReleasesAttempt(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	admit(t, e, "w1", tok1, "s1")
	st, err := e.Cancel("w1", 0, "operator", "obsolete")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusCancelled || st.Attempt != nil {
		t.Fatalf("cancel incomplete: %+v", st)
	}
	if _, err := e.Heartbeat("w1", attemptID("s1", tok1, 1), 60); err != ErrStaleInput {
		t.Fatalf("post-terminal heartbeat: %v", err)
	}
	if _, _, err := e.AdmitTurn("w1", AdmitTurnInput{SessionID: "s9", TurnToken: "t9"}); err != ErrTerminal {
		t.Fatalf("post-terminal admit: %v", err)
	}
}

func TestOneLossOpensOneBlockedReview(t *testing.T) {
	e, _ := newTestEngine(t)
	defer e.Close()
	define(t, e, "w1", PolicyUntilDone)
	st := admit(t, e, "w1", tok1, "s1")
	if _, err := e.ReportTurnLost("w1", attemptID("s1", tok1, st.Attempt.Generation), "lost"); err != nil {
		t.Fatal(err)
	}
	st, _ = e.State("w1")
	if st.Status != StatusBlocked || st.Review == nil || st.Review.Reason != "turn_lost" {
		t.Fatalf("loss did not open one blocked Review: %+v", st)
	}
}

// ---- helpers ----

func setNow(e *Engine, at time.Time) {
	e.SetNow(func() time.Time { return at })
}

func countReviews(st *State, reason string) int {
	n := 0
	if st.Review != nil && st.Review.Reason == reason {
		n++
	}
	return n
}

func countEngineEvents(e *Engine, workID WorkID, kind Kind, sourceID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, event := range e.events {
		if event.WorkID == workID && event.Kind == kind && event.SourceID == sourceID {
			n++
		}
	}
	return n
}
