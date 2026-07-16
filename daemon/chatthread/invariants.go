package chatthread

import (
	"fmt"
	"strings"
)

// CheckInvariants validates a complete Thread snapshot. The Projector invokes
// it transactionally after every effective transition and before committing.
func CheckInvariants(thread Thread) error {
	if !present(string(thread.ID)) {
		return invariantf(ErrInvalidArgument, "thread ID is empty")
	}

	positions := make(map[Position]string, len(thread.Submissions)+len(thread.Events))
	maxPosition := Position(0)
	addPosition := func(position Position, owner string) error {
		if position == 0 {
			return invariantf(ErrInvalidArgument, "%s has zero position", owner)
		}
		if prior, exists := positions[position]; exists {
			return invariantf(
				ErrDuplicatePosition,
				"position %d belongs to both %s and %s",
				position,
				prior,
				owner,
			)
		}
		positions[position] = owner
		if position > maxPosition {
			maxPosition = position
		}
		return nil
	}

	submissions := make(map[SubmissionID]Submission, len(thread.Submissions))
	previousSubmissionPosition := Position(0)
	for index, submission := range thread.Submissions {
		owner := fmt.Sprintf("submission[%d] %q", index, submission.ID)
		if !present(string(submission.ID)) {
			return invariantf(ErrInvalidArgument, "%s has an empty ID", owner)
		}
		if _, exists := submissions[submission.ID]; exists {
			return invariantf(ErrIDConflict, "duplicate Submission ID %q", submission.ID)
		}
		if err := addPosition(submission.Position, owner); err != nil {
			return err
		}
		if submission.Position <= previousSubmissionPosition {
			return invariantf(
				ErrInvalidTransition,
				"Submission positions are not strictly increasing at %q",
				submission.ID,
			)
		}
		previousSubmissionPosition = submission.Position
		if !validSubmissionOrigin(submission.Origin) {
			return invariantf(ErrInvalidArgument, "%s has origin %q", owner, submission.Origin)
		}
		hasWriterMetadata := submission.WriterEpoch != "" || submission.WriterSequence != 0 ||
			submission.AcceptedRevision != 0 || submission.AcceptedAt != nil
		if hasWriterMetadata {
			if submission.Origin != OriginApp {
				return invariantf(ErrInvalidTransition, "%s has App writer metadata for origin %q", owner, submission.Origin)
			}
			if !present(string(submission.WriterEpoch)) || submission.WriterSequence == 0 ||
				submission.AcceptedRevision == 0 || submission.AcceptedAt == nil || submission.AcceptedAt.IsZero() {
				return invariantf(ErrInvalidArgument, "%s has incomplete App writer metadata", owner)
			}
			if submission.AcceptedRevision > thread.Revision {
				return invariantf(
					ErrInvalidTransition,
					"%s acceptance revision %d is after Thread revision %d",
					owner,
					submission.AcceptedRevision,
					thread.Revision,
				)
			}
		}
		if !validDeliveryState(submission.Delivery) {
			return invariantf(ErrInvalidArgument, "%s has delivery state %q", owner, submission.Delivery)
		}

		switch submission.Delivery {
		case DeliveryQueued:
			if submission.DispatchAttempt != "" || submission.ExecutionID != "" ||
				submission.InputOrdinal != 0 || submission.AdmissionFactKey != "" {
				return invariantf(ErrInvalidTransition, "%s has delivery-only fields while queued", owner)
			}
		case DeliveryDelivering, DeliveryAmbiguous:
			if !present(string(submission.DispatchAttempt)) {
				return invariantf(ErrInvalidArgument, "%s has no dispatch attempt", owner)
			}
			if submission.ExecutionID != "" || submission.InputOrdinal != 0 || submission.AdmissionFactKey != "" {
				return invariantf(ErrInvalidTransition, "%s is bound before provider admission", owner)
			}
		case DeliveryDelivered:
			if !present(string(submission.ExecutionID)) || submission.InputOrdinal == 0 ||
				!present(string(submission.AdmissionFactKey)) {
				return invariantf(ErrInvalidArgument, "%s lacks its provider input binding", owner)
			}
		}

		submissions[submission.ID] = submission
	}

	activities := make(map[ExecutionID]ExecutionActivity, len(thread.ExecutionActivities))
	runningIDs := make([]ExecutionID, 0, 1)
	for index, activity := range thread.ExecutionActivities {
		owner := fmt.Sprintf("execution_activity[%d] %q", index, activity.ID)
		if !present(string(activity.ID)) {
			return invariantf(ErrInvalidArgument, "%s has an empty ID", owner)
		}
		if _, exists := activities[activity.ID]; exists {
			return invariantf(ErrIDConflict, "duplicate ExecutionActivity ID %q", activity.ID)
		}
		if !validActivityState(activity.State) {
			return invariantf(ErrInvalidArgument, "%s has state %q", owner, activity.State)
		}
		if !present(string(activity.StartFactKey)) {
			return invariantf(ErrInvalidArgument, "%s has no start fact key", owner)
		}
		if activity.State == ActivityRunning {
			runningIDs = append(runningIDs, activity.ID)
			if activity.TerminalFactKey != "" || activity.TerminalReason != "" {
				return invariantf(ErrInvalidTransition, "%s is running with terminal fields", owner)
			}
		} else if !present(string(activity.TerminalFactKey)) {
			return invariantf(ErrInvalidArgument, "%s is terminal without a terminal fact key", owner)
		}
		activities[activity.ID] = activity
	}

	if len(runningIDs) > 1 {
		return invariantf(ErrInvalidTransition, "more than one ExecutionActivity is running")
	}
	if thread.CurrentExecutionID == "" {
		if len(runningIDs) != 0 {
			return invariantf(ErrInvalidTransition, "running ExecutionActivity has no current owner")
		}
	} else {
		current, exists := activities[thread.CurrentExecutionID]
		if !exists {
			return invariantf(ErrInvalidTransition, "current ExecutionActivity %q is missing", thread.CurrentExecutionID)
		}
		if current.State != ActivityRunning || len(runningIDs) != 1 || runningIDs[0] != current.ID {
			return invariantf(ErrInvalidTransition, "current ExecutionActivity %q is not the sole running owner", current.ID)
		}
	}

	bindings := make(map[ExecutionID][]Submission, len(thread.ExecutionActivities))
	expectedQueue := make([]SubmissionID, 0, len(thread.QueuedSubmissionIDs))
	for _, submission := range thread.Submissions {
		if submission.Delivery == DeliveryQueued {
			expectedQueue = append(expectedQueue, submission.ID)
		}
		if submission.Delivery != DeliveryDelivered {
			continue
		}
		if _, exists := activities[submission.ExecutionID]; !exists {
			return invariantf(
				ErrInvalidTransition,
				"Submission %q references missing ExecutionActivity %q",
				submission.ID,
				submission.ExecutionID,
			)
		}
		bindings[submission.ExecutionID] = append(bindings[submission.ExecutionID], submission)
	}

	if len(expectedQueue) != len(thread.QueuedSubmissionIDs) {
		return invariantf(ErrInvalidTransition, "queued Submission projection does not match delivery states")
	}
	for index := range expectedQueue {
		if expectedQueue[index] != thread.QueuedSubmissionIDs[index] {
			return invariantf(
				ErrInvalidTransition,
				"queued Submission projection differs at index %d: got %q, want %q",
				index,
				thread.QueuedSubmissionIDs[index],
				expectedQueue[index],
			)
		}
	}

	for _, activity := range thread.ExecutionActivities {
		activityBindings := bindings[activity.ID]
		if activity.InputCount != InputOrdinal(len(activityBindings)) {
			return invariantf(
				ErrDuplicateOrdinal,
				"ExecutionActivity %q input count is %d but has %d bound Submissions",
				activity.ID,
				activity.InputCount,
				len(activityBindings),
			)
		}
		if terminalActivityState(activity.State) && activity.InputCount == 0 {
			return invariantf(ErrInvalidTransition, "terminal ExecutionActivity %q consumed no Submissions", activity.ID)
		}
		if activity.InputCount == 0 {
			if activity.ConsumedThroughPosition != 0 {
				return invariantf(ErrInvalidTransition, "ExecutionActivity %q has an empty nonzero frontier", activity.ID)
			}
			continue
		}

		ordered := make([]Submission, len(activityBindings))
		seen := make([]bool, len(activityBindings))
		for _, submission := range activityBindings {
			if submission.InputOrdinal == 0 || submission.InputOrdinal > activity.InputCount {
				return invariantf(
					ErrDuplicateOrdinal,
					"Submission %q has out-of-range ordinal %d for ExecutionActivity %q",
					submission.ID,
					submission.InputOrdinal,
					activity.ID,
				)
			}
			ordinalIndex := int(submission.InputOrdinal - 1)
			if seen[ordinalIndex] {
				return invariantf(
					ErrDuplicateOrdinal,
					"ExecutionActivity %q repeats input ordinal %d",
					activity.ID,
					submission.InputOrdinal,
				)
			}
			seen[ordinalIndex] = true
			ordered[ordinalIndex] = submission
		}

		previousPosition := Position(0)
		for ordinalIndex, submission := range ordered {
			if !seen[ordinalIndex] {
				return invariantf(
					ErrDuplicateOrdinal,
					"ExecutionActivity %q is missing input ordinal %d",
					activity.ID,
					ordinalIndex+1,
				)
			}
			if submission.Position <= previousPosition {
				return invariantf(
					ErrInvalidTransition,
					"ExecutionActivity %q consumes Submissions out of canonical order",
					activity.ID,
				)
			}
			previousPosition = submission.Position
		}
		if activity.ConsumedThroughPosition != previousPosition {
			return invariantf(
				ErrInvalidTransition,
				"ExecutionActivity %q frontier is %d, want %d",
				activity.ID,
				activity.ConsumedThroughPosition,
				previousPosition,
			)
		}
	}

	eventIDs := make(map[EventID]struct{}, len(thread.Events))
	previousEventPosition := Position(0)
	for index, event := range thread.Events {
		owner := fmt.Sprintf("event[%d] %q", index, event.ID)
		if !present(string(event.ID)) {
			return invariantf(ErrInvalidArgument, "%s has an empty ID", owner)
		}
		if _, exists := eventIDs[event.ID]; exists {
			return invariantf(ErrIDConflict, "duplicate ThreadEvent ID %q", event.ID)
		}
		if err := addPosition(event.Position, owner); err != nil {
			return err
		}
		if event.Position <= previousEventPosition {
			return invariantf(ErrInvalidTransition, "ThreadEvent positions are not strictly increasing at %q", event.ID)
		}
		previousEventPosition = event.Position
		if event.Revision == 0 {
			return invariantf(ErrInvalidArgument, "%s has zero revision", owner)
		}
		if !validEventKind(event.Kind) {
			return invariantf(ErrInvalidArgument, "%s has kind %q", owner, event.Kind)
		}
		if _, exists := activities[event.ExecutionID]; !exists {
			return invariantf(ErrInvalidTransition, "%s references missing ExecutionActivity %q", owner, event.ExecutionID)
		}
		cause, exists := submissions[event.CausalSubmissionID]
		if !exists || cause.Delivery != DeliveryDelivered {
			return invariantf(ErrEventBeforeCause, "%s has no delivered causal Submission", owner)
		}
		if cause.ExecutionID != event.ExecutionID {
			return invariantf(ErrEventBeforeCause, "%s and causal Submission belong to different Activities", owner)
		}
		if cause.Position != event.CausalSubmissionPosition {
			return invariantf(ErrEventBeforeCause, "%s stores a stale causal position", owner)
		}
		if event.Position <= cause.Position {
			return invariantf(
				ErrEventBeforeCause,
				"%s position %d is not after causal Submission position %d",
				owner,
				event.Position,
				cause.Position,
			)
		}
		eventIDs[event.ID] = struct{}{}
	}

	if len(positions) == 0 {
		if thread.NextPosition != 1 {
			return invariantf(ErrInvalidTransition, "empty Thread next position is %d, want 1", thread.NextPosition)
		}
	} else {
		if maxPosition != Position(len(positions)) {
			return invariantf(ErrInvalidTransition, "canonical position sequence has a gap before %d", maxPosition)
		}
		if thread.NextPosition != maxPosition+1 {
			return invariantf(
				ErrInvalidTransition,
				"next position is %d, want %d",
				thread.NextPosition,
				maxPosition+1,
			)
		}
	}

	if thread.Revision == 0 &&
		(len(thread.Submissions) != 0 || len(thread.ExecutionActivities) != 0 || len(thread.Events) != 0) {
		return invariantf(ErrInvalidTransition, "nonempty Thread has zero revision")
	}

	return nil
}

func invariantf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w: %w: %s", ErrInvariant, kind, fmt.Sprintf(format, args...))
}

func present(value string) bool {
	return strings.TrimSpace(value) != ""
}
