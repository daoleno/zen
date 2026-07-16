package chatthread

import (
	"fmt"
	"testing"
)

const (
	fixtureThreadID    ThreadID    = "sanitized-thread"
	fixtureExecutionID ExecutionID = "sanitized-execution-01"
	fixtureStateDigest             = "89a5734cf4c7411256cd65df291e126d35dc980860dd391516bc30c8b39cc60a"
)

type fixtureOperation struct {
	name   string
	accept *AcceptSubmissionCommand
	fact   ProviderFact
}

func sanitizedFixtureOperations() []fixtureOperation {
	return []fixtureOperation{
		{
			name: "accept repeated submission 1",
			accept: &AcceptSubmissionCommand{
				SubmissionID: "submission-01",
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: "same body"},
			},
		},
		{
			name: "one provider activity starts",
			fact: ActivityStartedFact{
				Key:         "fixture/activity/start",
				ExecutionID: fixtureExecutionID,
			},
		},
		{
			name: "input boundary 1",
			fact: InputAdmittedFact{
				Key:          "fixture/input/01",
				ExecutionID:  fixtureExecutionID,
				SubmissionID: "submission-01",
				Ordinal:      1,
			},
		},
		{
			name: "assistant event 1 partial",
			fact: EventUpsertFact{
				Key:                "fixture/event/01/partial",
				EventID:            "event-01",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-01",
				Kind:               EventAssistant,
				Payload:            "assistant update one",
			},
		},
		{
			name: "assistant event 1 final",
			fact: EventUpsertFact{
				Key:                "fixture/event/01/final",
				EventID:            "event-01",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-01",
				Kind:               EventAssistant,
				Final:              true,
				Payload:            "assistant update one complete",
			},
		},
		{
			name: "accept repeated submission 2",
			accept: &AcceptSubmissionCommand{
				SubmissionID: "submission-02",
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: "same body"},
			},
		},
		{
			name: "input boundary 2",
			fact: InputAdmittedFact{
				Key:          "fixture/input/02",
				ExecutionID:  fixtureExecutionID,
				SubmissionID: "submission-02",
				Ordinal:      2,
			},
		},
		{
			name: "tool event 2",
			fact: EventUpsertFact{
				Key:                "fixture/event/02/final",
				EventID:            "event-02",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-02",
				Kind:               EventTool,
				Final:              true,
				Payload:            "tool update two",
			},
		},
		{
			name: "accept repeated submission 3",
			accept: &AcceptSubmissionCommand{
				SubmissionID: "submission-03",
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: "same body"},
			},
		},
		{
			name: "input boundary 3",
			fact: InputAdmittedFact{
				Key:          "fixture/input/03",
				ExecutionID:  fixtureExecutionID,
				SubmissionID: "submission-03",
				Ordinal:      3,
			},
		},
		{
			name: "assistant event 3",
			fact: EventUpsertFact{
				Key:                "fixture/event/03/final",
				EventID:            "event-03",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-03",
				Kind:               EventAssistant,
				Final:              true,
				Payload:            "assistant update three",
			},
		},
		{
			name: "accept distinct submission 4",
			accept: &AcceptSubmissionCommand{
				SubmissionID: "submission-04",
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: "different body"},
			},
		},
		{
			name: "input boundary 4",
			fact: InputAdmittedFact{
				Key:          "fixture/input/04",
				ExecutionID:  fixtureExecutionID,
				SubmissionID: "submission-04",
				Ordinal:      4,
			},
		},
		{
			name: "tool event 4",
			fact: EventUpsertFact{
				Key:                "fixture/event/04/final",
				EventID:            "event-04",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-04",
				Kind:               EventTool,
				Final:              true,
				Payload:            "tool update four",
			},
		},
		{
			name: "accept distinct submission 5",
			accept: &AcceptSubmissionCommand{
				SubmissionID: "submission-05",
				Origin:       OriginApp,
				Payload:      SubmissionPayload{Body: "final body"},
			},
		},
		{
			name: "input boundary 5",
			fact: InputAdmittedFact{
				Key:          "fixture/input/05",
				ExecutionID:  fixtureExecutionID,
				SubmissionID: "submission-05",
				Ordinal:      5,
			},
		},
		{
			name: "assistant event 5 partial",
			fact: EventUpsertFact{
				Key:                "fixture/event/05/partial",
				EventID:            "event-05",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-05",
				Kind:               EventAssistant,
				Payload:            "assistant update five",
			},
		},
		{
			name: "assistant event 5 final",
			fact: EventUpsertFact{
				Key:                "fixture/event/05/final",
				EventID:            "event-05",
				ExecutionID:        fixtureExecutionID,
				CausalSubmissionID: "submission-05",
				Kind:               EventAssistant,
				Final:              true,
				Payload:            "assistant update five complete",
			},
		},
		{
			name: "one matching terminal fact",
			fact: ActivityTerminalFact{
				Key:           "fixture/activity/terminal",
				ExecutionID:   fixtureExecutionID,
				TerminalState: ActivityCompleted,
				Reason:        "provider completed",
			},
		},
	}
}

func TestSanitizedOneActivityFiveInputFixture(t *testing.T) {
	projector := newTestProjector(t, fixtureThreadID)
	operations := sanitizedFixtureOperations()

	starts, inputs, terminals := 0, 0, 0
	for _, operation := range operations {
		switch operation.fact.(type) {
		case ActivityStartedFact:
			starts++
		case InputAdmittedFact:
			inputs++
		case ActivityTerminalFact:
			terminals++
		}
	}
	if starts != 1 || inputs != 5 || terminals != 1 {
		t.Fatalf("fixture cardinality = starts %d, inputs %d, terminals %d; want 1, 5, 1", starts, inputs, terminals)
	}

	submissionPositions := make(map[SubmissionID]Position)
	eventPositions := make(map[EventID]Position)
	for _, operation := range operations {
		before := projector.Snapshot()
		result := applyFixtureOperation(t, projector, operation)
		if !result.Changed {
			t.Fatalf("%s: first application was a no-op", operation.name)
		}
		after := projector.Snapshot()
		assertRevisionTransition(t, operation.name, before.Revision, result, after.Revision)
		assertInvariantState(t, operation.name, after)
		assertStablePositions(t, submissionPositions, eventPositions, after)

		if input, ok := operation.fact.(InputAdmittedFact); ok {
			beforeSubmission := requireSubmission(t, before, input.SubmissionID)
			if beforeSubmission.Delivery != DeliveryQueued || !containsSubmissionID(before.QueuedSubmissionIDs, input.SubmissionID) {
				t.Fatalf("%s: Submission was not independently queued before admission: %#v", operation.name, beforeSubmission)
			}
			afterSubmission := requireSubmission(t, after, input.SubmissionID)
			if afterSubmission.Delivery != DeliveryDelivered || afterSubmission.InputOrdinal != input.Ordinal ||
				afterSubmission.ExecutionID != fixtureExecutionID {
				t.Fatalf("%s: admission projection = %#v", operation.name, afterSubmission)
			}
			if containsSubmissionID(after.QueuedSubmissionIDs, input.SubmissionID) {
				t.Fatalf("%s: admitted Submission remains queued", operation.name)
			}
			if len(after.ExecutionActivities) != 1 || after.CurrentExecutionID != fixtureExecutionID {
				t.Fatalf("%s: input admission fabricated Activity promotion: %#v", operation.name, after.ExecutionActivities)
			}
		}
	}

	state := projector.Snapshot()
	if len(state.Submissions) != 5 {
		t.Fatalf("Submission count = %d, want 5", len(state.Submissions))
	}
	if len(state.ExecutionActivities) != 1 {
		t.Fatalf("ExecutionActivity count = %d, want 1", len(state.ExecutionActivities))
	}
	if len(state.Events) != 5 {
		t.Fatalf("ThreadEvent count = %d, want 5", len(state.Events))
	}
	if len(state.QueuedSubmissionIDs) != 0 {
		t.Fatalf("terminal queue = %v, want empty", state.QueuedSubmissionIDs)
	}
	if state.CurrentExecutionID != "" {
		t.Fatalf("current ExecutionActivity = %q, want authoritative clear", state.CurrentExecutionID)
	}

	wantSubmissionPositions := []Position{1, 3, 5, 7, 9}
	repeatedBodies := 0
	for index, submission := range state.Submissions {
		if submission.ID != SubmissionID(fmt.Sprintf("submission-%02d", index+1)) {
			t.Fatalf("Submission[%d] ID = %q", index, submission.ID)
		}
		if submission.Position != wantSubmissionPositions[index] {
			t.Fatalf("Submission %q position = %d, want %d", submission.ID, submission.Position, wantSubmissionPositions[index])
		}
		if submission.InputOrdinal != InputOrdinal(index+1) {
			t.Fatalf("Submission %q ordinal = %d, want %d", submission.ID, submission.InputOrdinal, index+1)
		}
		if submission.Delivery != DeliveryDelivered || submission.ExecutionID != fixtureExecutionID {
			t.Fatalf("Submission %q delivery binding = %#v", submission.ID, submission)
		}
		if submission.Payload.Body == "same body" {
			repeatedBodies++
		}
	}
	if repeatedBodies != 3 {
		t.Fatalf("identical body records = %d, want 3 distinct Submissions", repeatedBodies)
	}

	activity := state.ExecutionActivities[0]
	if activity.ID != fixtureExecutionID || activity.State != ActivityCompleted || activity.InputCount != 5 ||
		activity.ConsumedThroughPosition != 9 || activity.TerminalFactKey != "fixture/activity/terminal" {
		t.Fatalf("final ExecutionActivity = %#v", activity)
	}

	wantEventPositions := []Position{2, 4, 6, 8, 10}
	for index, event := range state.Events {
		cause := requireSubmission(t, state, SubmissionID(fmt.Sprintf("submission-%02d", index+1)))
		if event.Position != wantEventPositions[index] {
			t.Fatalf("ThreadEvent %q position = %d, want %d", event.ID, event.Position, wantEventPositions[index])
		}
		if event.CausalSubmissionID != cause.ID || event.CausalSubmissionPosition != cause.Position ||
			event.ExecutionID != cause.ExecutionID || event.Position <= cause.Position {
			t.Fatalf("ThreadEvent %q has invalid causal frontier: event=%#v cause=%#v", event.ID, event, cause)
		}
	}
	if state.Events[0].Revision != 2 || state.Events[4].Revision != 2 {
		t.Fatalf("streaming Event revisions = %d and %d, want 2 and 2", state.Events[0].Revision, state.Events[4].Revision)
	}
	if state.Revision != Revision(len(operations)) {
		t.Fatalf("Thread revision = %d, want %d effective transitions", state.Revision, len(operations))
	}
	if state.NextPosition != 11 {
		t.Fatalf("next position = %d, want 11", state.NextPosition)
	}

	if digest := projector.Digest(); digest != fixtureStateDigest {
		t.Fatalf("sanitized fixture state digest = %s, want %s", digest, fixtureStateDigest)
	}
}

func TestSanitizedFixtureWholeStreamReplayIsDigestIdentical(t *testing.T) {
	projector := newTestProjector(t, fixtureThreadID)
	operations := sanitizedFixtureOperations()
	for _, operation := range operations {
		result := applyFixtureOperation(t, projector, operation)
		if !result.Changed {
			t.Fatalf("%s: first application was a no-op", operation.name)
		}
	}

	beforeDigest := projector.Digest()
	beforeRevision := projector.Snapshot().Revision
	for _, operation := range operations {
		before := projector.Snapshot()
		result := applyFixtureOperation(t, projector, operation)
		if result.Changed {
			t.Fatalf("%s: replay changed canonical state", operation.name)
		}
		after := projector.Snapshot()
		assertRevisionTransition(t, operation.name+" replay", before.Revision, result, after.Revision)
		assertInvariantState(t, operation.name+" replay", after)
	}

	afterDigest := projector.Digest()
	if afterDigest != beforeDigest {
		t.Fatalf("whole-stream replay digest changed: before %s, after %s", beforeDigest, afterDigest)
	}
	if projector.Snapshot().Revision != beforeRevision {
		t.Fatalf("whole-stream replay revision changed: before %d, after %d", beforeRevision, projector.Snapshot().Revision)
	}
}

func applyFixtureOperation(t *testing.T, projector *Projector, operation fixtureOperation) ApplyResult {
	t.Helper()
	var (
		result ApplyResult
		err    error
	)
	if operation.accept != nil {
		result, err = projector.Accept(*operation.accept)
	} else {
		result, err = projector.Apply(operation.fact)
	}
	if err != nil {
		t.Fatalf("%s: %v", operation.name, err)
	}
	return result
}

func assertStablePositions(
	t *testing.T,
	submissionPositions map[SubmissionID]Position,
	eventPositions map[EventID]Position,
	state Thread,
) {
	t.Helper()
	for _, submission := range state.Submissions {
		if position, exists := submissionPositions[submission.ID]; exists && position != submission.Position {
			t.Fatalf("Submission %q moved from position %d to %d", submission.ID, position, submission.Position)
		}
		submissionPositions[submission.ID] = submission.Position
	}
	for _, event := range state.Events {
		if position, exists := eventPositions[event.ID]; exists && position != event.Position {
			t.Fatalf("ThreadEvent %q moved from position %d to %d", event.ID, position, event.Position)
		}
		eventPositions[event.ID] = event.Position
	}
}
