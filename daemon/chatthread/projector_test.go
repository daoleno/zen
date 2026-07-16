package chatthread

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"testing/quick"
)

func TestSubmissionAcceptanceIsIdempotentButConflictingPayloadIsRejected(t *testing.T) {
	projector := newTestProjector(t, "submission-idempotency")
	command := AcceptSubmissionCommand{
		SubmissionID: "submission-01",
		Origin:       OriginApp,
		Payload: SubmissionPayload{
			Body:          "identical immutable payload",
			AttachmentIDs: []string{"attachment-01"},
		},
	}

	first := mustAccept(t, projector, command)
	if !first.Changed {
		t.Fatal("first acceptance was a no-op")
	}
	position := projector.Snapshot().Submissions[0].Position
	replay := mustAccept(t, projector, command)
	if replay.Changed {
		t.Fatal("same Submission ID and payload changed state")
	}
	if projector.Snapshot().Submissions[0].Position != position {
		t.Fatal("idempotent acceptance changed canonical position")
	}

	before := projector.Snapshot()
	beforeDigest := projector.Digest()
	conflict := command
	conflict.Payload.Body = "different payload under same ID"
	result, err := projector.Accept(conflict)
	if !errors.Is(err, ErrIDConflict) {
		t.Fatalf("conflicting Submission payload error = %v, want ErrIDConflict", err)
	}
	assertRejectedStateUnchanged(t, "Submission ID conflict", projector, before, beforeDigest, result)
}

func TestDuplicateProviderFactKeyIsNoOpAndConflictingPayloadIsRejected(t *testing.T) {
	projector := newTestProjector(t, "fact-idempotency")
	start := ActivityStartedFact{Key: "provider/start/01", ExecutionID: "execution-01"}
	first := mustApply(t, projector, start)
	if !first.Changed {
		t.Fatal("first provider fact was a no-op")
	}

	before := projector.Snapshot()
	beforeDigest := projector.Digest()
	replay := mustApply(t, projector, start)
	if replay.Changed {
		t.Fatal("identical provider fact key replay changed state")
	}
	if projector.Digest() != beforeDigest {
		t.Fatal("identical provider fact key replay changed digest")
	}

	conflict := start
	conflict.ExecutionID = "execution-02"
	result, err := projector.Apply(conflict)
	if !errors.Is(err, ErrFactKeyConflict) {
		t.Fatalf("conflicting provider fact key error = %v, want ErrFactKeyConflict", err)
	}
	assertRejectedStateUnchanged(t, "provider fact key conflict", projector, before, beforeDigest, result)
}

func TestDuplicateInputOrdinalAndCanonicalPositionAreRejected(t *testing.T) {
	t.Run("input ordinal", func(t *testing.T) {
		projector := newTestProjector(t, "duplicate-ordinal")
		mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-01", Origin: OriginApp})
		mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-02", Origin: OriginApp})
		mustApply(t, projector, ActivityStartedFact{Key: "start", ExecutionID: "execution-01"})
		mustApply(t, projector, InputAdmittedFact{
			Key:          "input-01",
			ExecutionID:  "execution-01",
			SubmissionID: "submission-01",
			Ordinal:      1,
		})

		before := projector.Snapshot()
		beforeDigest := projector.Digest()
		result, err := projector.Apply(InputAdmittedFact{
			Key:          "input-02-conflicting-ordinal",
			ExecutionID:  "execution-01",
			SubmissionID: "submission-02",
			Ordinal:      1,
		})
		if !errors.Is(err, ErrDuplicateOrdinal) {
			t.Fatalf("duplicate ordinal error = %v, want ErrDuplicateOrdinal", err)
		}
		assertRejectedStateUnchanged(t, "duplicate input ordinal", projector, before, beforeDigest, result)
		if !containsSubmissionID(projector.Snapshot().QueuedSubmissionIDs, "submission-02") {
			t.Fatal("rejected duplicate ordinal cleared the independent queued Submission")
		}
	})

	t.Run("canonical position", func(t *testing.T) {
		projector := newTestProjector(t, "duplicate-position")
		mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-01", Origin: OriginApp})
		mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-02", Origin: OriginApp})
		corruptSnapshot := projector.Snapshot()
		corruptSnapshot.Submissions[1].Position = corruptSnapshot.Submissions[0].Position
		if err := CheckInvariants(corruptSnapshot); !errors.Is(err, ErrDuplicatePosition) {
			t.Fatalf("duplicate position error = %v, want ErrDuplicatePosition", err)
		}
		if err := projector.CheckInvariants(); err != nil {
			t.Fatalf("mutating snapshot corrupted Projector: %v", err)
		}
	})
}

func TestEventBeforeDeliveredCauseAndBeforeAdvancedFrontierIsRejected(t *testing.T) {
	projector := newTestProjector(t, "event-causality")
	mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-01", Origin: OriginApp})
	mustApply(t, projector, ActivityStartedFact{Key: "start", ExecutionID: "execution-01"})

	before := projector.Snapshot()
	beforeDigest := projector.Digest()
	result, err := projector.Apply(EventUpsertFact{
		Key:                "event-before-input",
		EventID:            "event-01",
		ExecutionID:        "execution-01",
		CausalSubmissionID: "submission-01",
		Kind:               EventAssistant,
		Payload:            "too early",
	})
	if !errors.Is(err, ErrEventBeforeCause) {
		t.Fatalf("Event before admission error = %v, want ErrEventBeforeCause", err)
	}
	assertRejectedStateUnchanged(t, "Event before admission", projector, before, beforeDigest, result)

	mustApply(t, projector, InputAdmittedFact{
		Key:          "input-01",
		ExecutionID:  "execution-01",
		SubmissionID: "submission-01",
		Ordinal:      1,
	})
	mustApply(t, projector, EventUpsertFact{
		Key:                "event-01-partial",
		EventID:            "event-01",
		ExecutionID:        "execution-01",
		CausalSubmissionID: "submission-01",
		Kind:               EventAssistant,
		Payload:            "valid event",
	})
	mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-02", Origin: OriginApp})
	mustApply(t, projector, InputAdmittedFact{
		Key:          "input-02",
		ExecutionID:  "execution-01",
		SubmissionID: "submission-02",
		Ordinal:      2,
	})

	before = projector.Snapshot()
	beforeDigest = projector.Digest()
	result, err = projector.Apply(EventUpsertFact{
		Key:                "event-01-impossible-frontier",
		EventID:            "event-01",
		ExecutionID:        "execution-01",
		CausalSubmissionID: "submission-02",
		Kind:               EventAssistant,
		Final:              true,
		Payload:            "would place cause after Event",
	})
	if !errors.Is(err, ErrEventBeforeCause) {
		t.Fatalf("Event before advanced frontier error = %v, want ErrEventBeforeCause", err)
	}
	assertRejectedStateUnchanged(t, "Event before advanced frontier", projector, before, beforeDigest, result)
}

func TestMatchingTerminalSettlesOnceAndTerminalActivityCannotReopen(t *testing.T) {
	projector := newTestProjector(t, "terminal-once")
	mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-01", Origin: OriginApp})
	mustApply(t, projector, ActivityStartedFact{Key: "start", ExecutionID: "execution-01"})
	mustApply(t, projector, InputAdmittedFact{
		Key:          "input",
		ExecutionID:  "execution-01",
		SubmissionID: "submission-01",
		Ordinal:      1,
	})
	terminal := ActivityTerminalFact{
		Key:           "terminal",
		ExecutionID:   "execution-01",
		TerminalState: ActivityCompleted,
	}
	mustApply(t, projector, terminal)
	state := projector.Snapshot()
	if state.CurrentExecutionID != "" || state.ExecutionActivities[0].State != ActivityCompleted {
		t.Fatalf("matching terminal did not atomically clear lifecycle: %#v", state)
	}

	replay := mustApply(t, projector, terminal)
	if replay.Changed {
		t.Fatal("identical terminal fact settled Activity twice")
	}

	before := projector.Snapshot()
	beforeDigest := projector.Digest()
	result, err := projector.Apply(ActivityTerminalFact{
		Key:           "second-terminal-record",
		ExecutionID:   "execution-01",
		TerminalState: ActivityCompleted,
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second terminal transition error = %v, want ErrInvalidTransition", err)
	}
	assertRejectedStateUnchanged(t, "second terminal transition", projector, before, beforeDigest, result)

	result, err = projector.Apply(ActivityStartedFact{Key: "reopen", ExecutionID: "execution-01"})
	if !errors.Is(err, ErrTerminalReopen) {
		t.Fatalf("terminal reopen error = %v, want ErrTerminalReopen", err)
	}
	assertRejectedStateUnchanged(t, "terminal reopen", projector, before, beforeDigest, result)
}

func TestAmbiguousDeliveryCannotAutoRetryButAuthoritativeAdmissionCanResolveIt(t *testing.T) {
	projector := newTestProjector(t, "ambiguous-delivery")
	mustAccept(t, projector, AcceptSubmissionCommand{SubmissionID: "submission-01", Origin: OriginApp})
	attempt := BeginDeliveryCommand{SubmissionID: "submission-01", AttemptID: "attempt-01"}
	firstAttempt := mustBeginDelivery(t, projector, attempt)
	if !firstAttempt.Changed {
		t.Fatal("first delivery attempt was a no-op")
	}
	if state := projector.Snapshot(); state.Submissions[0].Delivery != DeliveryDelivering || len(state.QueuedSubmissionIDs) != 0 {
		t.Fatalf("delivery attempt projection = %#v", state)
	}
	mustApply(t, projector, DeliveryAmbiguousFact{
		Key:          "ambiguous-01",
		SubmissionID: "submission-01",
		AttemptID:    "attempt-01",
	})

	replay := mustBeginDelivery(t, projector, attempt)
	if replay.Changed {
		t.Fatal("same delivery command replay changed ambiguous state")
	}
	before := projector.Snapshot()
	beforeDigest := projector.Digest()
	result, err := projector.BeginDelivery(BeginDeliveryCommand{
		SubmissionID: "submission-01",
		AttemptID:    "attempt-02",
	})
	if !errors.Is(err, ErrAmbiguousRetry) {
		t.Fatalf("ambiguous retry error = %v, want ErrAmbiguousRetry", err)
	}
	assertRejectedStateUnchanged(t, "ambiguous delivery retry", projector, before, beforeDigest, result)

	mustApply(t, projector, ActivityStartedFact{Key: "start", ExecutionID: "execution-01"})
	mustApply(t, projector, InputAdmittedFact{
		Key:          "authoritative-input",
		ExecutionID:  "execution-01",
		SubmissionID: "submission-01",
		Ordinal:      1,
	})
	resolved := projector.Snapshot().Submissions[0]
	if resolved.Delivery != DeliveryDelivered || resolved.DispatchAttempt != "attempt-01" || resolved.InputOrdinal != 1 {
		t.Fatalf("authoritative ambiguity resolution = %#v", resolved)
	}
}

func TestPropertyIdenticalBodiesAlwaysKeepDistinctIDsAndPositions(t *testing.T) {
	property := func(body string, countSeed uint8) bool {
		count := int(countSeed%16) + 1
		projector, err := NewProjector("property-thread")
		if err != nil {
			return false
		}
		commands := make([]AcceptSubmissionCommand, 0, count)
		for index := 0; index < count; index++ {
			command := AcceptSubmissionCommand{
				SubmissionID: SubmissionID(fmt.Sprintf("submission-%02d", index)),
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: body},
			}
			commands = append(commands, command)
			result, applyErr := projector.Accept(command)
			if applyErr != nil || !result.Changed || result.Revision != Revision(index+1) {
				return false
			}
			if invariantErr := projector.CheckInvariants(); invariantErr != nil {
				return false
			}
		}

		state := projector.Snapshot()
		if len(state.Submissions) != count || len(state.QueuedSubmissionIDs) != count {
			return false
		}
		for index, submission := range state.Submissions {
			if submission.ID != commands[index].SubmissionID || submission.Payload.Body != body ||
				submission.Position != Position(index+1) {
				return false
			}
		}

		digest := projector.Digest()
		revision := state.Revision
		for index := len(commands) - 1; index >= 0; index-- {
			result, applyErr := projector.Accept(commands[index])
			if applyErr != nil || result.Changed || result.Revision != revision {
				return false
			}
		}
		return projector.Digest() == digest && projector.Snapshot().Revision == revision
	}

	if err := quick.Check(property, &quick.Config{
		MaxCount: 128,
		Rand:     rand.New(rand.NewSource(0)),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInvariantCheckerRejectsMoreThanOneRunningLifecycleOwner(t *testing.T) {
	projector := newTestProjector(t, "running-owner")
	mustApply(t, projector, ActivityStartedFact{Key: "start-01", ExecutionID: "execution-01"})
	corruptSnapshot := projector.Snapshot()
	corruptSnapshot.ExecutionActivities = append(corruptSnapshot.ExecutionActivities, ExecutionActivity{
		ID:           "execution-02",
		State:        ActivityRunning,
		StartFactKey: "start-02",
	})
	if err := CheckInvariants(corruptSnapshot); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("multiple running owners error = %v, want ErrInvalidTransition", err)
	}
}

func newTestProjector(t *testing.T, threadID ThreadID) *Projector {
	t.Helper()
	projector, err := NewProjector(threadID)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	return projector
}

func mustAccept(t *testing.T, projector *Projector, command AcceptSubmissionCommand) ApplyResult {
	t.Helper()
	before := projector.Snapshot()
	result, err := projector.Accept(command)
	if err != nil {
		t.Fatalf("Accept(%q): %v", command.SubmissionID, err)
	}
	after := projector.Snapshot()
	assertRevisionTransition(t, "Accept "+string(command.SubmissionID), before.Revision, result, after.Revision)
	assertInvariantState(t, "Accept "+string(command.SubmissionID), after)
	return result
}

func mustBeginDelivery(t *testing.T, projector *Projector, command BeginDeliveryCommand) ApplyResult {
	t.Helper()
	before := projector.Snapshot()
	result, err := projector.BeginDelivery(command)
	if err != nil {
		t.Fatalf("BeginDelivery(%q): %v", command.SubmissionID, err)
	}
	after := projector.Snapshot()
	assertRevisionTransition(t, "BeginDelivery "+string(command.SubmissionID), before.Revision, result, after.Revision)
	assertInvariantState(t, "BeginDelivery "+string(command.SubmissionID), after)
	return result
}

func mustApply(t *testing.T, projector *Projector, fact ProviderFact) ApplyResult {
	t.Helper()
	before := projector.Snapshot()
	result, err := projector.Apply(fact)
	if err != nil {
		t.Fatalf("Apply(%T): %v", fact, err)
	}
	after := projector.Snapshot()
	assertRevisionTransition(t, fmt.Sprintf("Apply %T", fact), before.Revision, result, after.Revision)
	assertInvariantState(t, fmt.Sprintf("Apply %T", fact), after)
	return result
}

func assertRevisionTransition(t *testing.T, label string, before Revision, result ApplyResult, after Revision) {
	t.Helper()
	want := before
	if result.Changed {
		want++
	}
	if after != want || result.Revision != after {
		t.Fatalf("%s: revisions before=%d result=%d after=%d, want after=%d", label, before, result.Revision, after, want)
	}
}

func assertInvariantState(t *testing.T, label string, state Thread) {
	t.Helper()
	if err := CheckInvariants(state); err != nil {
		t.Fatalf("%s: invariant failure: %v", label, err)
	}
}

func assertRejectedStateUnchanged(
	t *testing.T,
	label string,
	projector *Projector,
	before Thread,
	beforeDigest string,
	result ApplyResult,
) {
	t.Helper()
	after := projector.Snapshot()
	if result.Changed {
		t.Fatalf("%s: rejected transition reported a state change", label)
	}
	if after.Revision != before.Revision || result.Revision != before.Revision {
		t.Fatalf("%s: rejected transition changed revision from %d to %d", label, before.Revision, after.Revision)
	}
	if projector.Digest() != beforeDigest {
		t.Fatalf("%s: rejected transition changed state digest", label)
	}
	assertInvariantState(t, label, after)
}

func requireSubmission(t *testing.T, state Thread, submissionID SubmissionID) Submission {
	t.Helper()
	for _, submission := range state.Submissions {
		if submission.ID == submissionID {
			return submission
		}
	}
	t.Fatalf("Submission %q is missing", submissionID)
	return Submission{}
}

func containsSubmissionID(ids []SubmissionID, want SubmissionID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
