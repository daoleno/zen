package chatthread

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Projector owns one pure in-memory Thread projection. It has no clock, I/O,
// persistence, provider, or transport dependency.
type Projector struct {
	thread               Thread
	appliedProviderFacts map[ProviderFactKey][sha256.Size]byte
}

func NewProjector(threadID ThreadID) (*Projector, error) {
	if !present(string(threadID)) {
		return nil, fmt.Errorf("%w: thread ID is empty", ErrInvalidArgument)
	}

	projector := &Projector{
		thread: Thread{
			ID:                  threadID,
			NextPosition:        1,
			QueuedSubmissionIDs: []SubmissionID{},
			Submissions:         []Submission{},
			ExecutionActivities: []ExecutionActivity{},
			Events:              []ThreadEvent{},
		},
		appliedProviderFacts: make(map[ProviderFactKey][sha256.Size]byte),
	}
	if err := CheckInvariants(projector.thread); err != nil {
		return nil, err
	}
	return projector, nil
}

// Snapshot returns a deep copy so callers cannot mutate projector state.
func (p *Projector) Snapshot() Thread {
	if p == nil {
		return Thread{}
	}
	return cloneThread(p.thread)
}

func (p *Projector) Digest() string {
	return StateDigest(p.Snapshot())
}

func (p *Projector) CheckInvariants() error {
	if p == nil {
		return fmt.Errorf("%w: nil Projector", ErrInvalidArgument)
	}
	return CheckInvariants(p.thread)
}

// Accept allocates one Submission position before any provider effect. The
// Submission ID is the idempotency key; payload never identifies another row.
func (p *Projector) Accept(command AcceptSubmissionCommand) (ApplyResult, error) {
	if p == nil {
		return ApplyResult{}, fmt.Errorf("%w: nil Projector", ErrInvalidArgument)
	}
	if !present(string(command.SubmissionID)) {
		return p.result(false), fmt.Errorf("%w: Submission ID is empty", ErrInvalidArgument)
	}
	if !validSubmissionOrigin(command.Origin) {
		return p.result(false), fmt.Errorf("%w: unsupported Submission origin %q", ErrInvalidArgument, command.Origin)
	}
	hasWriterMetadata := command.WriterEpoch != "" || command.WriterSeq != 0 || !command.AcceptedAt.IsZero()
	if hasWriterMetadata {
		if command.Origin != OriginApp || !present(string(command.WriterEpoch)) || command.WriterSeq == 0 ||
			command.AcceptedAt.IsZero() {
			return p.result(false), fmt.Errorf(
				"%w: App writer acceptance requires a nonempty epoch and nonzero sequence",
				ErrInvalidArgument,
			)
		}
	}

	if index := findSubmission(p.thread, command.SubmissionID); index >= 0 {
		existing := p.thread.Submissions[index]
		if existing.Origin != command.Origin || existing.WriterEpoch != command.WriterEpoch ||
			existing.WriterSequence != command.WriterSeq || !sameAcceptedAt(existing.AcceptedAt, command.AcceptedAt) ||
			!sameSubmissionPayload(existing.Payload, command.Payload) {
			return p.result(false), fmt.Errorf(
				"%w: Submission %q was already accepted with different immutable fields",
				ErrIDConflict,
				command.SubmissionID,
			)
		}
		if err := CheckInvariants(p.thread); err != nil {
			return p.result(false), err
		}
		return p.result(false), nil
	}

	return p.transition(func(candidate *Thread) (bool, error) {
		position, err := allocatePosition(candidate)
		if err != nil {
			return false, err
		}
		acceptedRevision := Revision(0)
		if hasWriterMetadata {
			if candidate.Revision == ^Revision(0) {
				return false, fmt.Errorf("%w: Thread revision overflow", ErrInvalidTransition)
			}
			acceptedRevision = candidate.Revision + 1
		}
		candidate.Submissions = append(candidate.Submissions, Submission{
			ID:               command.SubmissionID,
			Position:         position,
			Origin:           command.Origin,
			WriterEpoch:      command.WriterEpoch,
			WriterSequence:   command.WriterSeq,
			AcceptedRevision: acceptedRevision,
			AcceptedAt:       acceptedAtPointer(command.AcceptedAt),
			Payload: SubmissionPayload{
				Body:          command.Payload.Body,
				AttachmentIDs: append([]string{}, command.Payload.AttachmentIDs...),
			},
			Delivery: DeliveryQueued,
		})
		candidate.QueuedSubmissionIDs = append(candidate.QueuedSubmissionIDs, command.SubmissionID)
		return true, nil
	})
}

// acceptAndBeginDelivery is the durable-ledger compound transition. It makes
// acceptance and the first dispatch attempt one observable Thread revision;
// no queued intermediate state can be persisted or projected.
func (p *Projector) acceptAndBeginDelivery(
	command AcceptSubmissionCommand,
	attemptID DispatchAttemptID,
) (ApplyResult, error) {
	if p == nil {
		return ApplyResult{}, fmt.Errorf("%w: nil Projector", ErrInvalidArgument)
	}
	if !present(string(command.SubmissionID)) || !present(string(attemptID)) {
		return p.result(false), fmt.Errorf(
			"%w: acceptance with dispatch requires Submission and attempt IDs",
			ErrInvalidArgument,
		)
	}
	if !validSubmissionOrigin(command.Origin) {
		return p.result(false), fmt.Errorf("%w: unsupported Submission origin %q", ErrInvalidArgument, command.Origin)
	}
	hasWriterMetadata := command.WriterEpoch != "" || command.WriterSeq != 0 || !command.AcceptedAt.IsZero()
	if hasWriterMetadata &&
		(command.Origin != OriginApp || !present(string(command.WriterEpoch)) || command.WriterSeq == 0 ||
			command.AcceptedAt.IsZero()) {
		return p.result(false), fmt.Errorf(
			"%w: App writer acceptance requires a nonempty epoch and nonzero sequence",
			ErrInvalidArgument,
		)
	}
	if index := findSubmission(p.thread, command.SubmissionID); index >= 0 {
		existing := p.thread.Submissions[index]
		if existing.Origin != command.Origin || existing.WriterEpoch != command.WriterEpoch ||
			existing.WriterSequence != command.WriterSeq || !sameAcceptedAt(existing.AcceptedAt, command.AcceptedAt) ||
			!sameSubmissionPayload(existing.Payload, command.Payload) {
			return p.result(false), fmt.Errorf(
				"%w: Submission %q was already accepted with different immutable fields",
				ErrIDConflict,
				command.SubmissionID,
			)
		}
		if existing.DispatchAttempt == attemptID {
			switch existing.Delivery {
			case DeliveryDelivering, DeliveryAmbiguous, DeliveryDelivered:
				return p.result(false), nil
			}
		}
		return p.result(false), fmt.Errorf(
			"%w: Submission %q already has disposition %s and attempt %q",
			ErrInvalidTransition,
			command.SubmissionID,
			existing.Delivery,
			existing.DispatchAttempt,
		)
	}

	return p.transition(func(candidate *Thread) (bool, error) {
		position, err := allocatePosition(candidate)
		if err != nil {
			return false, err
		}
		acceptedRevision := Revision(0)
		if hasWriterMetadata {
			if candidate.Revision == ^Revision(0) {
				return false, fmt.Errorf("%w: Thread revision overflow", ErrInvalidTransition)
			}
			acceptedRevision = candidate.Revision + 1
		}
		candidate.Submissions = append(candidate.Submissions, Submission{
			ID:               command.SubmissionID,
			Position:         position,
			Origin:           command.Origin,
			WriterEpoch:      command.WriterEpoch,
			WriterSequence:   command.WriterSeq,
			AcceptedRevision: acceptedRevision,
			AcceptedAt:       acceptedAtPointer(command.AcceptedAt),
			Payload: SubmissionPayload{
				Body:          command.Payload.Body,
				AttachmentIDs: append([]string{}, command.Payload.AttachmentIDs...),
			},
			Delivery:        DeliveryDelivering,
			DispatchAttempt: attemptID,
		})
		return true, nil
	})
}

func cloneProjector(projector *Projector) *Projector {
	if projector == nil {
		return nil
	}
	clone := &Projector{
		thread:               cloneThread(projector.thread),
		appliedProviderFacts: make(map[ProviderFactKey][sha256.Size]byte, len(projector.appliedProviderFacts)),
	}
	for key, fingerprint := range projector.appliedProviderFacts {
		clone.appliedProviderFacts[key] = fingerprint
	}
	return clone
}

// BeginDelivery records a dispatch attempt. Replaying the same attempt is a
// no-op; no command can create a new attempt after an ambiguous outcome.
func (p *Projector) BeginDelivery(command BeginDeliveryCommand) (ApplyResult, error) {
	if p == nil {
		return ApplyResult{}, fmt.Errorf("%w: nil Projector", ErrInvalidArgument)
	}
	if !present(string(command.SubmissionID)) || !present(string(command.AttemptID)) {
		return p.result(false), fmt.Errorf("%w: delivery command requires Submission and attempt IDs", ErrInvalidArgument)
	}

	return p.transition(func(candidate *Thread) (bool, error) {
		index := findSubmission(*candidate, command.SubmissionID)
		if index < 0 {
			return false, fmt.Errorf("%w: Submission %q is missing", ErrInvalidTransition, command.SubmissionID)
		}
		submission := &candidate.Submissions[index]
		if submission.DispatchAttempt == command.AttemptID {
			switch submission.Delivery {
			case DeliveryDelivering, DeliveryAmbiguous, DeliveryDelivered:
				return false, nil
			}
		}

		switch submission.Delivery {
		case DeliveryQueued:
			if submission.DispatchAttempt != "" {
				return false, fmt.Errorf("%w: queued Submission %q already has an attempt", ErrInvalidTransition, submission.ID)
			}
			if err := removeQueuedSubmission(candidate, submission.ID); err != nil {
				return false, err
			}
			submission.Delivery = DeliveryDelivering
			submission.DispatchAttempt = command.AttemptID
			return true, nil
		case DeliveryAmbiguous:
			return false, fmt.Errorf(
				"%w: Submission %q may already have crossed the provider boundary",
				ErrAmbiguousRetry,
				submission.ID,
			)
		default:
			return false, fmt.Errorf(
				"%w: Submission %q is %s and cannot begin another delivery",
				ErrInvalidTransition,
				submission.ID,
				submission.Delivery,
			)
		}
	})
}

// Apply consumes one normalized provider fact. A fact key is globally
// idempotent within the projector: identical replay is a no-op and a different
// payload under the same key is rejected before any state transition.
func (p *Projector) Apply(fact ProviderFact) (ApplyResult, error) {
	if p == nil {
		return ApplyResult{}, fmt.Errorf("%w: nil Projector", ErrInvalidArgument)
	}
	normalized, key, fingerprint, err := normalizeProviderFact(fact)
	if err != nil {
		return p.result(false), err
	}
	if existing, applied := p.appliedProviderFacts[key]; applied {
		if existing != fingerprint {
			return p.result(false), fmt.Errorf(
				"%w: provider fact %q was replayed with different fields",
				ErrFactKeyConflict,
				key,
			)
		}
		if err := CheckInvariants(p.thread); err != nil {
			return p.result(false), err
		}
		return p.result(false), nil
	}

	result, err := p.transition(func(candidate *Thread) (bool, error) {
		return applyProviderFact(candidate, normalized)
	})
	if err != nil {
		return result, err
	}
	p.appliedProviderFacts[key] = fingerprint
	return result, nil
}

func (p *Projector) result(changed bool) ApplyResult {
	if p == nil {
		return ApplyResult{Changed: changed}
	}
	return ApplyResult{Revision: p.thread.Revision, Changed: changed}
}

func (p *Projector) transition(mutate func(*Thread) (bool, error)) (ApplyResult, error) {
	candidate := cloneThread(p.thread)
	changed, err := mutate(&candidate)
	if err != nil {
		return p.result(false), err
	}
	if !changed {
		if err := CheckInvariants(candidate); err != nil {
			return p.result(false), err
		}
		return p.result(false), nil
	}
	if p.thread.Revision == ^Revision(0) {
		return p.result(false), fmt.Errorf("%w: Thread revision overflow", ErrInvalidTransition)
	}
	candidate.Revision = p.thread.Revision + 1
	if err := CheckInvariants(candidate); err != nil {
		return p.result(false), err
	}
	p.thread = candidate
	return p.result(true), nil
}

func applyProviderFact(thread *Thread, fact ProviderFact) (bool, error) {
	switch fact := fact.(type) {
	case ActivityStartedFact:
		return applyActivityStarted(thread, fact)
	case DeliveryAmbiguousFact:
		return applyDeliveryAmbiguous(thread, fact)
	case InputAdmittedFact:
		return applyInputAdmitted(thread, fact)
	case EventUpsertFact:
		return applyEventUpsert(thread, fact)
	case ActivityTerminalFact:
		return applyActivityTerminal(thread, fact)
	default:
		return false, fmt.Errorf("%w: unsupported provider fact %T", ErrInvalidArgument, fact)
	}
}

func applyActivityStarted(thread *Thread, fact ActivityStartedFact) (bool, error) {
	if !present(string(fact.ExecutionID)) {
		return false, fmt.Errorf("%w: Activity start has an empty Execution ID", ErrInvalidArgument)
	}
	if index := findExecutionActivity(*thread, fact.ExecutionID); index >= 0 {
		if terminalActivityState(thread.ExecutionActivities[index].State) {
			return false, fmt.Errorf("%w: ExecutionActivity %q is terminal", ErrTerminalReopen, fact.ExecutionID)
		}
		return false, fmt.Errorf("%w: ExecutionActivity %q already exists", ErrIDConflict, fact.ExecutionID)
	}
	if thread.CurrentExecutionID != "" {
		return false, fmt.Errorf(
			"%w: ExecutionActivity %q already owns current lifecycle",
			ErrInvalidTransition,
			thread.CurrentExecutionID,
		)
	}
	thread.ExecutionActivities = append(thread.ExecutionActivities, ExecutionActivity{
		ID:           fact.ExecutionID,
		State:        ActivityRunning,
		StartFactKey: fact.Key,
	})
	thread.CurrentExecutionID = fact.ExecutionID
	return true, nil
}

func applyDeliveryAmbiguous(thread *Thread, fact DeliveryAmbiguousFact) (bool, error) {
	if !present(string(fact.SubmissionID)) || !present(string(fact.AttemptID)) {
		return false, fmt.Errorf("%w: ambiguous delivery fact requires Submission and attempt IDs", ErrInvalidArgument)
	}
	index := findSubmission(*thread, fact.SubmissionID)
	if index < 0 {
		return false, fmt.Errorf("%w: Submission %q is missing", ErrInvalidTransition, fact.SubmissionID)
	}
	submission := &thread.Submissions[index]
	if submission.Delivery != DeliveryDelivering || submission.DispatchAttempt != fact.AttemptID {
		return false, fmt.Errorf(
			"%w: Submission %q does not have matching in-flight attempt %q",
			ErrInvalidTransition,
			submission.ID,
			fact.AttemptID,
		)
	}
	submission.Delivery = DeliveryAmbiguous
	return true, nil
}

func applyInputAdmitted(thread *Thread, fact InputAdmittedFact) (bool, error) {
	if !present(string(fact.ExecutionID)) || !present(string(fact.SubmissionID)) || fact.Ordinal == 0 {
		return false, fmt.Errorf("%w: input admission requires Execution ID, Submission ID, and ordinal", ErrInvalidArgument)
	}
	activityIndex := findExecutionActivity(*thread, fact.ExecutionID)
	if activityIndex < 0 {
		return false, fmt.Errorf("%w: ExecutionActivity %q is missing", ErrInvalidTransition, fact.ExecutionID)
	}
	activity := &thread.ExecutionActivities[activityIndex]
	if thread.CurrentExecutionID != activity.ID || activity.State != ActivityRunning {
		return false, fmt.Errorf("%w: ExecutionActivity %q is not the current running owner", ErrInvalidTransition, activity.ID)
	}
	submissionIndex := findSubmission(*thread, fact.SubmissionID)
	if submissionIndex < 0 {
		return false, fmt.Errorf("%w: Submission %q is missing", ErrInvalidTransition, fact.SubmissionID)
	}
	submission := &thread.Submissions[submissionIndex]
	if submission.Delivery == DeliveryDelivered {
		return false, fmt.Errorf(
			"%w: Submission %q is already input ordinal %d",
			ErrDuplicateOrdinal,
			submission.ID,
			submission.InputOrdinal,
		)
	}
	if submission.Delivery != DeliveryQueued && submission.Delivery != DeliveryDelivering &&
		submission.Delivery != DeliveryAmbiguous {
		return false, fmt.Errorf("%w: Submission %q cannot be admitted from %s", ErrInvalidTransition, submission.ID, submission.Delivery)
	}
	if activity.InputCount == ^InputOrdinal(0) {
		return false, fmt.Errorf("%w: input ordinal overflow", ErrInvalidTransition)
	}
	expectedOrdinal := activity.InputCount + 1
	if fact.Ordinal != expectedOrdinal {
		if fact.Ordinal <= activity.InputCount {
			return false, fmt.Errorf(
				"%w: ExecutionActivity %q already consumed ordinal %d",
				ErrDuplicateOrdinal,
				activity.ID,
				fact.Ordinal,
			)
		}
		return false, fmt.Errorf(
			"%w: ExecutionActivity %q expected ordinal %d, got %d",
			ErrInvalidTransition,
			activity.ID,
			expectedOrdinal,
			fact.Ordinal,
		)
	}
	if activity.InputCount != 0 && submission.Position <= activity.ConsumedThroughPosition {
		return false, fmt.Errorf(
			"%w: Submission %q position %d is not after consumed frontier %d",
			ErrInvalidTransition,
			submission.ID,
			submission.Position,
			activity.ConsumedThroughPosition,
		)
	}
	if submission.Delivery == DeliveryQueued {
		if err := removeQueuedSubmission(thread, submission.ID); err != nil {
			return false, err
		}
	}

	submission.Delivery = DeliveryDelivered
	submission.ExecutionID = activity.ID
	submission.InputOrdinal = fact.Ordinal
	submission.AdmissionFactKey = fact.Key
	activity.InputCount = fact.Ordinal
	activity.ConsumedThroughPosition = submission.Position
	return true, nil
}

func applyEventUpsert(thread *Thread, fact EventUpsertFact) (bool, error) {
	if !present(string(fact.EventID)) || !present(string(fact.ExecutionID)) ||
		!present(string(fact.CausalSubmissionID)) {
		return false, fmt.Errorf("%w: Event upsert requires Event, Execution, and causal Submission IDs", ErrInvalidArgument)
	}
	if !validEventKind(fact.Kind) {
		return false, fmt.Errorf("%w: unsupported Event kind %q", ErrInvalidArgument, fact.Kind)
	}
	if findExecutionActivity(*thread, fact.ExecutionID) < 0 {
		return false, fmt.Errorf("%w: ExecutionActivity %q is missing", ErrInvalidTransition, fact.ExecutionID)
	}
	causeIndex := findSubmission(*thread, fact.CausalSubmissionID)
	if causeIndex < 0 {
		return false, fmt.Errorf("%w: causal Submission %q is missing", ErrEventBeforeCause, fact.CausalSubmissionID)
	}
	cause := thread.Submissions[causeIndex]
	if cause.Delivery != DeliveryDelivered || cause.ExecutionID != fact.ExecutionID {
		return false, fmt.Errorf(
			"%w: causal Submission %q is not admitted to ExecutionActivity %q",
			ErrEventBeforeCause,
			cause.ID,
			fact.ExecutionID,
		)
	}

	eventIndex := findThreadEvent(*thread, fact.EventID)
	if eventIndex < 0 {
		position, err := allocatePosition(thread)
		if err != nil {
			return false, err
		}
		if position <= cause.Position {
			return false, fmt.Errorf(
				"%w: new Event position %d is not after causal Submission position %d",
				ErrEventBeforeCause,
				position,
				cause.Position,
			)
		}
		thread.Events = append(thread.Events, ThreadEvent{
			ID:                       fact.EventID,
			Position:                 position,
			Revision:                 1,
			ExecutionID:              fact.ExecutionID,
			CausalSubmissionID:       cause.ID,
			CausalSubmissionPosition: cause.Position,
			Kind:                     fact.Kind,
			Final:                    fact.Final,
			Payload:                  fact.Payload,
		})
		return true, nil
	}

	event := &thread.Events[eventIndex]
	if event.ExecutionID != fact.ExecutionID || event.Kind != fact.Kind {
		return false, fmt.Errorf(
			"%w: ThreadEvent %q changed immutable Execution or kind fields",
			ErrIDConflict,
			event.ID,
		)
	}
	if cause.Position < event.CausalSubmissionPosition {
		return false, fmt.Errorf("%w: ThreadEvent %q causal frontier regressed", ErrInvalidTransition, event.ID)
	}
	if event.Position <= cause.Position {
		return false, fmt.Errorf(
			"%w: ThreadEvent %q position %d is not after new causal frontier %d",
			ErrEventBeforeCause,
			event.ID,
			event.Position,
			cause.Position,
		)
	}
	changed := event.CausalSubmissionID != cause.ID || event.CausalSubmissionPosition != cause.Position ||
		event.Final != fact.Final || event.Payload != fact.Payload
	if !changed {
		return false, nil
	}
	if event.Final {
		return false, fmt.Errorf("%w: finalized ThreadEvent %q cannot change", ErrInvalidTransition, event.ID)
	}
	if event.Revision == ^EventRevision(0) {
		return false, fmt.Errorf("%w: ThreadEvent %q revision overflow", ErrInvalidTransition, event.ID)
	}
	event.CausalSubmissionID = cause.ID
	event.CausalSubmissionPosition = cause.Position
	event.Final = fact.Final
	event.Payload = fact.Payload
	event.Revision++
	return true, nil
}

func applyActivityTerminal(thread *Thread, fact ActivityTerminalFact) (bool, error) {
	if !present(string(fact.ExecutionID)) || !terminalActivityState(fact.TerminalState) {
		return false, fmt.Errorf("%w: terminal fact requires Execution ID and terminal state", ErrInvalidArgument)
	}
	activityIndex := findExecutionActivity(*thread, fact.ExecutionID)
	if activityIndex < 0 {
		return false, fmt.Errorf("%w: ExecutionActivity %q is missing", ErrInvalidTransition, fact.ExecutionID)
	}
	activity := &thread.ExecutionActivities[activityIndex]
	if activity.State != ActivityRunning {
		return false, fmt.Errorf("%w: ExecutionActivity %q already settled as %s", ErrInvalidTransition, activity.ID, activity.State)
	}
	if thread.CurrentExecutionID != activity.ID {
		return false, fmt.Errorf("%w: ExecutionActivity %q is not current", ErrInvalidTransition, activity.ID)
	}
	if activity.InputCount == 0 {
		return false, fmt.Errorf("%w: ExecutionActivity %q consumed no Submissions", ErrInvalidTransition, activity.ID)
	}
	activity.State = fact.TerminalState
	activity.TerminalFactKey = fact.Key
	activity.TerminalReason = fact.Reason
	thread.CurrentExecutionID = ""
	return true, nil
}

func normalizeProviderFact(fact ProviderFact) (ProviderFact, ProviderFactKey, [sha256.Size]byte, error) {
	var (
		normalized ProviderFact
		key        ProviderFactKey
		kind       string
	)
	switch fact := fact.(type) {
	case ActivityStartedFact:
		normalized, key, kind = fact, fact.Key, "activity_started"
	case *ActivityStartedFact:
		if fact == nil {
			return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil ActivityStartedFact", ErrInvalidArgument)
		}
		normalized, key, kind = *fact, fact.Key, "activity_started"
	case DeliveryAmbiguousFact:
		normalized, key, kind = fact, fact.Key, "delivery_ambiguous"
	case *DeliveryAmbiguousFact:
		if fact == nil {
			return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil DeliveryAmbiguousFact", ErrInvalidArgument)
		}
		normalized, key, kind = *fact, fact.Key, "delivery_ambiguous"
	case InputAdmittedFact:
		normalized, key, kind = fact, fact.Key, "input_admitted"
	case *InputAdmittedFact:
		if fact == nil {
			return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil InputAdmittedFact", ErrInvalidArgument)
		}
		normalized, key, kind = *fact, fact.Key, "input_admitted"
	case EventUpsertFact:
		normalized, key, kind = fact, fact.Key, "event_upsert"
	case *EventUpsertFact:
		if fact == nil {
			return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil EventUpsertFact", ErrInvalidArgument)
		}
		normalized, key, kind = *fact, fact.Key, "event_upsert"
	case ActivityTerminalFact:
		normalized, key, kind = fact, fact.Key, "activity_terminal"
	case *ActivityTerminalFact:
		if fact == nil {
			return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil ActivityTerminalFact", ErrInvalidArgument)
		}
		normalized, key, kind = *fact, fact.Key, "activity_terminal"
	case nil:
		return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: nil provider fact", ErrInvalidArgument)
	default:
		return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: unsupported provider fact %T", ErrInvalidArgument, fact)
	}
	if !present(string(key)) {
		return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: provider fact key is empty", ErrInvalidArgument)
	}
	encoded, err := json.Marshal(struct {
		Kind string       `json:"kind"`
		Fact ProviderFact `json:"fact"`
	}{Kind: kind, Fact: normalized})
	if err != nil {
		return nil, "", [sha256.Size]byte{}, fmt.Errorf("%w: fingerprint provider fact: %v", ErrInvalidArgument, err)
	}
	return normalized, key, sha256.Sum256(encoded), nil
}

func allocatePosition(thread *Thread) (Position, error) {
	if thread.NextPosition == 0 || thread.NextPosition == ^Position(0) {
		return 0, fmt.Errorf("%w: canonical position overflow", ErrInvalidTransition)
	}
	position := thread.NextPosition
	thread.NextPosition++
	return position, nil
}

func removeQueuedSubmission(thread *Thread, submissionID SubmissionID) error {
	queueIndex := -1
	for index, queuedID := range thread.QueuedSubmissionIDs {
		if queuedID != submissionID {
			continue
		}
		if queueIndex >= 0 {
			return fmt.Errorf("%w: Submission %q occurs twice in queue", ErrInvariant, submissionID)
		}
		queueIndex = index
	}
	if queueIndex < 0 {
		return fmt.Errorf("%w: queued Submission %q is absent from queue", ErrInvariant, submissionID)
	}
	copy(thread.QueuedSubmissionIDs[queueIndex:], thread.QueuedSubmissionIDs[queueIndex+1:])
	thread.QueuedSubmissionIDs = thread.QueuedSubmissionIDs[:len(thread.QueuedSubmissionIDs)-1]
	return nil
}

func findSubmission(thread Thread, submissionID SubmissionID) int {
	for index := range thread.Submissions {
		if thread.Submissions[index].ID == submissionID {
			return index
		}
	}
	return -1
}

func findExecutionActivity(thread Thread, executionID ExecutionID) int {
	for index := range thread.ExecutionActivities {
		if thread.ExecutionActivities[index].ID == executionID {
			return index
		}
	}
	return -1
}

func findThreadEvent(thread Thread, eventID EventID) int {
	for index := range thread.Events {
		if thread.Events[index].ID == eventID {
			return index
		}
	}
	return -1
}

func sameSubmissionPayload(left, right SubmissionPayload) bool {
	if left.Body != right.Body || len(left.AttachmentIDs) != len(right.AttachmentIDs) {
		return false
	}
	for index := range left.AttachmentIDs {
		if left.AttachmentIDs[index] != right.AttachmentIDs[index] {
			return false
		}
	}
	return true
}

func acceptedAtPointer(acceptedAt time.Time) *time.Time {
	if acceptedAt.IsZero() {
		return nil
	}
	canonical := acceptedAt.UTC()
	return &canonical
}

func sameAcceptedAt(existing *time.Time, requested time.Time) bool {
	if existing == nil {
		return requested.IsZero()
	}
	return !requested.IsZero() && existing.Equal(requested)
}
