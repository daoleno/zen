package chatthread

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrThreadNotFound           = errors.New("canonical Chat thread not found")
	ErrThreadOwnership          = errors.New("canonical Chat thread ownership conflict")
	ErrWriterEpochMismatch      = errors.New("canonical Chat writer epoch mismatch")
	ErrWriterSequenceGap        = errors.New("canonical Chat writer sequence gap")
	ErrWriterSequenceStale      = errors.New("canonical Chat writer sequence is stale")
	ErrWriterSequenceConflict   = errors.New("canonical Chat writer sequence conflicts with accepted Submission")
	ErrWriterSequenceExhausted  = errors.New("canonical Chat writer sequence exhausted")
	ErrLedgerCorrupt            = errors.New("canonical Chat ledger is corrupt")
	ErrLedgerSchema             = errors.New("unsupported canonical Chat ledger schema")
	ErrLedgerNotInitialized     = errors.New("canonical Chat ledger is not initialized")
	ErrLedgerAlreadyInitialized = errors.New("canonical Chat ledger is already initialized")
	ErrLedgerUnavailable        = errors.New("canonical Chat ledger is unavailable")
	ErrDurabilityUncertain      = errors.New("canonical Chat ledger durability is uncertain")
	ErrDispatchAdmissionUnknown = errors.New("provider dispatch admission is unknown")
	ErrDispatchOrderBlocked     = errors.New("provider dispatch is blocked by an unresolved predecessor")
)

type ThreadOwnership string

const (
	// ThreadOwnershipV1 is only a rejection marker. The durable ledger never
	// creates or imports a v1 Thread.
	ThreadOwnershipV1 ThreadOwnership = "v1"
	ThreadOwnershipV2 ThreadOwnership = "v2"
)

type WriterState struct {
	Epoch        WriterEpoch    `json:"epoch"`
	NextSequence WriterSequence `json:"next_seq"`
}

// CreateThreadCommand is the sole opt-in boundary for a durable v2 Thread.
// Callers must state v2 ownership explicitly; empty or v1 ownership is rejected.
type CreateThreadCommand struct {
	ThreadID    ThreadID
	Ownership   ThreadOwnership
	WriterEpoch WriterEpoch
}

// AppSubmissionRequest contains the App-owned idempotency and FIFO identity.
// Payload is immutable after the Submission ID is accepted.
type AppSubmissionRequest struct {
	ThreadID       ThreadID
	SubmissionID   SubmissionID
	WriterEpoch    WriterEpoch
	WriterSequence WriterSequence
	Payload        SubmissionPayload
}

// DurableThreadSnapshot is the complete durable v2 projection exposed by this
// isolated Slice. Provider fact payloads are not retained, only their keys and
// fingerprints, so only the count is projected here.
type DurableThreadSnapshot struct {
	Ownership         ThreadOwnership
	Writer            WriterState
	Thread            Thread
	Digest            string
	ProviderFactCount int
}

// SubmissionDisposition is returned for both first acceptance and an exact
// retry. AcceptedRevision and Position never change; the delivery fields show
// the currently durable disposition of the already accepted Submission.
type SubmissionDisposition struct {
	ThreadID          ThreadID
	SubmissionID      SubmissionID
	WriterEpoch       WriterEpoch
	WriterSequence    WriterSequence
	Position          Position
	AcceptedRevision  Revision
	AcceptedAt        time.Time
	ThreadRevision    Revision
	Payload           SubmissionPayload
	Delivery          DeliveryState
	DispatchAttemptID DispatchAttemptID
	ExecutionID       ExecutionID
	InputOrdinal      InputOrdinal
	Digest            string
}

// ProviderDispatch is the immutable request passed across the injected effect
// boundary. Its dispatch attempt and acceptance are already durable when the
// boundary receives it.
type ProviderDispatch struct {
	ThreadID         ThreadID
	SubmissionID     SubmissionID
	AttemptID        DispatchAttemptID
	WriterEpoch      WriterEpoch
	WriterSequence   WriterSequence
	Position         Position
	AcceptedRevision Revision
	AcceptedAt       time.Time
	Payload          SubmissionPayload
}

// DispatchBoundary is deliberately provider-neutral and is not registered by
// this Slice. A nil return means only that the call returned; authoritative
// provider facts are still required to prove input admission.
type DispatchBoundary interface {
	Dispatch(context.Context, ProviderDispatch) error
}

type DispatchBoundaryFunc func(context.Context, ProviderDispatch) error

func (dispatch DispatchBoundaryFunc) Dispatch(ctx context.Context, request ProviderDispatch) error {
	return dispatch(ctx, request)
}

type DispatchResult struct {
	Disposition             SubmissionDisposition
	ProviderEffectAttempted bool
}

type ledgerThread struct {
	ownership ThreadOwnership
	writer    WriterState
	projector *Projector
}

func (thread *ledgerThread) clone() *ledgerThread {
	if thread == nil {
		return nil
	}
	return &ledgerThread{
		ownership: thread.ownership,
		writer:    thread.writer,
		projector: cloneProjector(thread.projector),
	}
}

type atomicWriteFunc func(path string, data []byte) error

// Ledger owns a single-process materialized v2 state file. It has no transport,
// App, provider adapter, or production dispatch registration.
type Ledger struct {
	mu          sync.Mutex
	dispatchMu  sync.Mutex
	path        string
	threads     map[ThreadID]*ledgerThread
	atomicWrite atomicWriteFunc
	now         func() time.Time
	fatalErr    error
}

func (ledger *Ledger) Path() string {
	if ledger == nil {
		return ""
	}
	return ledger.path
}

func (ledger *Ledger) CreateThread(command CreateThreadCommand) (DurableThreadSnapshot, error) {
	if ledger == nil {
		return DurableThreadSnapshot{}, fmt.Errorf("%w: nil Ledger", ErrInvalidArgument)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	if err := ledger.checkUsableLocked(); err != nil {
		return DurableThreadSnapshot{}, err
	}
	if !present(string(command.ThreadID)) || !present(string(command.WriterEpoch)) {
		return DurableThreadSnapshot{}, fmt.Errorf("%w: Thread ID and writer epoch are required", ErrInvalidArgument)
	}
	if command.Ownership != ThreadOwnershipV2 {
		return DurableThreadSnapshot{}, fmt.Errorf(
			"%w: Thread %q must explicitly opt in to %q ownership, got %q",
			ErrThreadOwnership,
			command.ThreadID,
			ThreadOwnershipV2,
			command.Ownership,
		)
	}

	if existing, ok := ledger.threads[command.ThreadID]; ok {
		if existing.ownership != ThreadOwnershipV2 {
			return DurableThreadSnapshot{}, fmt.Errorf(
				"%w: Thread %q is owned by %q",
				ErrThreadOwnership,
				command.ThreadID,
				existing.ownership,
			)
		}
		if existing.writer.Epoch != command.WriterEpoch {
			return DurableThreadSnapshot{}, fmt.Errorf(
				"%w: Thread %q has writer epoch %q, got %q",
				ErrWriterEpochMismatch,
				command.ThreadID,
				existing.writer.Epoch,
				command.WriterEpoch,
			)
		}
		return snapshotLedgerThread(existing), nil
	}

	projector, err := NewProjector(command.ThreadID)
	if err != nil {
		return DurableThreadSnapshot{}, err
	}
	candidate := &ledgerThread{
		ownership: ThreadOwnershipV2,
		writer: WriterState{
			Epoch:        command.WriterEpoch,
			NextSequence: 1,
		},
		projector: projector,
	}
	if err := ledger.persistThreadLocked(command.ThreadID, candidate); err != nil {
		return DurableThreadSnapshot{}, err
	}
	ledger.threads[command.ThreadID] = candidate
	return snapshotLedgerThread(candidate), nil
}

func (ledger *Ledger) Snapshot(threadID ThreadID) (DurableThreadSnapshot, error) {
	if ledger == nil {
		return DurableThreadSnapshot{}, fmt.Errorf("%w: nil Ledger", ErrInvalidArgument)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if err := ledger.checkUsableLocked(); err != nil {
		return DurableThreadSnapshot{}, err
	}
	thread, ok := ledger.threads[threadID]
	if !ok {
		return DurableThreadSnapshot{}, fmt.Errorf("%w: Thread %q", ErrThreadNotFound, threadID)
	}
	return snapshotLedgerThread(thread), nil
}

// Accept durably admits one queued App Submission without crossing a provider
// boundary. Exact retry returns the existing disposition without a write.
func (ledger *Ledger) Accept(request AppSubmissionRequest) (SubmissionDisposition, error) {
	if ledger == nil {
		return SubmissionDisposition{}, fmt.Errorf("%w: nil Ledger", ErrInvalidArgument)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	disposition, _, _, err := ledger.acceptLocked(request, "")
	return disposition, err
}

// AcceptAndDispatch atomically persists a new acceptance and dispatch attempt,
// then and only then invokes the injected provider boundary. The boundary's
// return is not treated as proof of provider admission, so the attempt becomes
// ambiguous until a structured provider fact resolves it.
func (ledger *Ledger) AcceptAndDispatch(
	ctx context.Context,
	request AppSubmissionRequest,
	attemptID DispatchAttemptID,
	dispatch DispatchBoundary,
) (DispatchResult, error) {
	if ledger == nil {
		return DispatchResult{}, fmt.Errorf("%w: nil Ledger", ErrInvalidArgument)
	}
	if dispatch == nil {
		return DispatchResult{}, fmt.Errorf("%w: dispatch boundary is required", ErrInvalidArgument)
	}
	if !present(string(attemptID)) {
		return DispatchResult{}, fmt.Errorf("%w: dispatch attempt ID is required", ErrInvalidArgument)
	}
	ledger.dispatchMu.Lock()
	defer ledger.dispatchMu.Unlock()

	ledger.mu.Lock()
	disposition, newlyAccepted, providerRequest, err := ledger.acceptLocked(request, attemptID)
	ledger.mu.Unlock()
	if err != nil {
		return DispatchResult{}, err
	}
	result := DispatchResult{Disposition: disposition}
	if !newlyAccepted {
		return result, nil
	}

	result.ProviderEffectAttempted = true
	boundaryErr := dispatch.Dispatch(ctx, *providerRequest)
	resolved, ambiguityErr := ledger.recordDispatchAmbiguous(request.ThreadID, request.SubmissionID, attemptID)
	if ambiguityErr == nil {
		result.Disposition = resolved
	}
	if ambiguityErr != nil {
		if boundaryErr != nil {
			return result, errors.Join(boundaryErr, ambiguityErr)
		}
		return result, ambiguityErr
	}
	if boundaryErr != nil {
		return result, errors.Join(ErrDispatchAdmissionUnknown, boundaryErr)
	}
	return result, nil
}

// ApplyProviderFact transactionally persists both the materialized projection
// and the provider fact fingerprint. A newly recorded fact that is a visible
// no-op is still written so its key remains idempotent after restart.
func (ledger *Ledger) ApplyProviderFact(threadID ThreadID, fact ProviderFact) (ApplyResult, error) {
	if ledger == nil {
		return ApplyResult{}, fmt.Errorf("%w: nil Ledger", ErrInvalidArgument)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if err := ledger.checkUsableLocked(); err != nil {
		return ApplyResult{}, err
	}
	current, ok := ledger.threads[threadID]
	if !ok {
		return ApplyResult{}, fmt.Errorf("%w: Thread %q", ErrThreadNotFound, threadID)
	}
	candidate := current.clone()
	factsBefore := len(candidate.projector.appliedProviderFacts)
	result, err := candidate.projector.Apply(fact)
	if err != nil {
		return result, err
	}
	if !result.Changed && len(candidate.projector.appliedProviderFacts) == factsBefore {
		return result, nil
	}
	if err := ledger.persistThreadLocked(threadID, candidate); err != nil {
		return ApplyResult{Revision: current.projector.thread.Revision}, err
	}
	ledger.threads[threadID] = candidate
	return result, nil
}

func (ledger *Ledger) acceptLocked(
	request AppSubmissionRequest,
	attemptID DispatchAttemptID,
) (SubmissionDisposition, bool, *ProviderDispatch, error) {
	if err := ledger.checkUsableLocked(); err != nil {
		return SubmissionDisposition{}, false, nil, err
	}
	if !present(string(request.ThreadID)) || !present(string(request.SubmissionID)) ||
		!present(string(request.WriterEpoch)) || request.WriterSequence == 0 {
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread ID, Submission ID, writer epoch, and writer sequence are required",
			ErrInvalidArgument,
		)
	}
	current, ok := ledger.threads[request.ThreadID]
	if !ok {
		return SubmissionDisposition{}, false, nil, fmt.Errorf("%w: Thread %q", ErrThreadNotFound, request.ThreadID)
	}
	if current.ownership != ThreadOwnershipV2 {
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread %q is owned by %q",
			ErrThreadOwnership,
			request.ThreadID,
			current.ownership,
		)
	}

	if index := findSubmission(current.projector.thread, request.SubmissionID); index >= 0 {
		existing := current.projector.thread.Submissions[index]
		if !sameSubmissionPayload(existing.Payload, request.Payload) {
			return SubmissionDisposition{}, false, nil, fmt.Errorf(
				"%w: Submission %q was accepted with a different immutable payload",
				ErrIDConflict,
				request.SubmissionID,
			)
		}
		if existing.Origin != OriginApp || existing.WriterEpoch != request.WriterEpoch {
			return SubmissionDisposition{}, false, nil, fmt.Errorf(
				"%w: Submission %q belongs to writer epoch %q, got %q",
				ErrWriterEpochMismatch,
				request.SubmissionID,
				existing.WriterEpoch,
				request.WriterEpoch,
			)
		}
		if existing.WriterSequence != request.WriterSequence {
			return SubmissionDisposition{}, false, nil, fmt.Errorf(
				"%w: Submission %q belongs to writer sequence %d, got %d",
				ErrWriterSequenceConflict,
				request.SubmissionID,
				existing.WriterSequence,
				request.WriterSequence,
			)
		}
		return dispositionForSubmission(current, existing), false, nil, nil
	}

	if current.writer.Epoch != request.WriterEpoch {
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread %q expects epoch %q, got %q",
			ErrWriterEpochMismatch,
			request.ThreadID,
			current.writer.Epoch,
			request.WriterEpoch,
		)
	}
	switch {
	case request.WriterSequence < current.writer.NextSequence:
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread %q expects sequence %d, got %d",
			ErrWriterSequenceStale,
			request.ThreadID,
			current.writer.NextSequence,
			request.WriterSequence,
		)
	case request.WriterSequence > current.writer.NextSequence:
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread %q expects sequence %d, got %d",
			ErrWriterSequenceGap,
			request.ThreadID,
			current.writer.NextSequence,
			request.WriterSequence,
		)
	}
	if current.writer.NextSequence == ^WriterSequence(0) {
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: Thread %q cannot advance past sequence %d",
			ErrWriterSequenceExhausted,
			request.ThreadID,
			request.WriterSequence,
		)
	}
	if attemptID != "" {
		for _, predecessor := range current.projector.thread.Submissions {
			if predecessor.Delivery == DeliveryDelivered {
				continue
			}
			return SubmissionDisposition{}, false, nil, fmt.Errorf(
				"%w: Submission %q is still %s before writer sequence %d",
				ErrDispatchOrderBlocked,
				predecessor.ID,
				predecessor.Delivery,
				request.WriterSequence,
			)
		}
	}

	candidate := current.clone()
	if ledger.now == nil {
		return SubmissionDisposition{}, false, nil, fmt.Errorf("%w: Ledger clock is unavailable", ErrInvariant)
	}
	acceptedAt := ledger.now().UTC()
	if acceptedAt.IsZero() {
		return SubmissionDisposition{}, false, nil, fmt.Errorf("%w: Ledger clock returned a zero acceptance time", ErrInvariant)
	}
	command := AcceptSubmissionCommand{
		SubmissionID: request.SubmissionID,
		Origin:       OriginApp,
		WriterEpoch:  request.WriterEpoch,
		WriterSeq:    request.WriterSequence,
		AcceptedAt:   acceptedAt,
		Payload: SubmissionPayload{
			Body:          request.Payload.Body,
			AttachmentIDs: append([]string{}, request.Payload.AttachmentIDs...),
		},
	}
	var acceptResult ApplyResult
	var err error
	if attemptID == "" {
		acceptResult, err = candidate.projector.Accept(command)
	} else {
		acceptResult, err = candidate.projector.acceptAndBeginDelivery(command, attemptID)
	}
	if err != nil {
		return SubmissionDisposition{}, false, nil, err
	}
	if !acceptResult.Changed {
		return SubmissionDisposition{}, false, nil, fmt.Errorf(
			"%w: new Submission %q was not admitted",
			ErrInvariant,
			request.SubmissionID,
		)
	}
	candidate.writer.NextSequence++

	if err := ledger.persistThreadLocked(request.ThreadID, candidate); err != nil {
		return SubmissionDisposition{}, false, nil, err
	}
	ledger.threads[request.ThreadID] = candidate
	submission := candidate.projector.thread.Submissions[findSubmission(candidate.projector.thread, request.SubmissionID)]
	disposition := dispositionForSubmission(candidate, submission)
	if attemptID == "" {
		return disposition, true, nil, nil
	}
	providerRequest := &ProviderDispatch{
		ThreadID:         request.ThreadID,
		SubmissionID:     submission.ID,
		AttemptID:        submission.DispatchAttempt,
		WriterEpoch:      submission.WriterEpoch,
		WriterSequence:   submission.WriterSequence,
		Position:         submission.Position,
		AcceptedRevision: submission.AcceptedRevision,
		AcceptedAt:       acceptedAt,
		Payload: SubmissionPayload{
			Body:          submission.Payload.Body,
			AttachmentIDs: append([]string{}, submission.Payload.AttachmentIDs...),
		},
	}
	return disposition, true, providerRequest, nil
}

func (ledger *Ledger) recordDispatchAmbiguous(
	threadID ThreadID,
	submissionID SubmissionID,
	attemptID DispatchAttemptID,
) (SubmissionDisposition, error) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if err := ledger.checkUsableLocked(); err != nil {
		return SubmissionDisposition{}, err
	}
	current, ok := ledger.threads[threadID]
	if !ok {
		return SubmissionDisposition{}, fmt.Errorf("%w: Thread %q", ErrThreadNotFound, threadID)
	}
	index := findSubmission(current.projector.thread, submissionID)
	if index < 0 {
		return SubmissionDisposition{}, fmt.Errorf("%w: Submission %q is missing", ErrInvalidTransition, submissionID)
	}
	submission := current.projector.thread.Submissions[index]
	if submission.DispatchAttempt != attemptID {
		return SubmissionDisposition{}, fmt.Errorf(
			"%w: Submission %q has attempt %q, got %q",
			ErrInvalidTransition,
			submissionID,
			submission.DispatchAttempt,
			attemptID,
		)
	}
	if submission.Delivery == DeliveryAmbiguous || submission.Delivery == DeliveryDelivered {
		return dispositionForSubmission(current, submission), nil
	}
	if submission.Delivery != DeliveryDelivering {
		return SubmissionDisposition{}, fmt.Errorf(
			"%w: Submission %q is %s after dispatch",
			ErrInvalidTransition,
			submissionID,
			submission.Delivery,
		)
	}

	candidate := current.clone()
	_, err := candidate.projector.Apply(DeliveryAmbiguousFact{
		Key:          dispatchAmbiguousFactKey("returned", attemptID),
		SubmissionID: submissionID,
		AttemptID:    attemptID,
	})
	if err != nil {
		return SubmissionDisposition{}, err
	}
	if err := ledger.persistThreadLocked(threadID, candidate); err != nil {
		ledger.fatalErr = fmt.Errorf("record post-effect dispatch ambiguity: %w", err)
		return SubmissionDisposition{}, fmt.Errorf("%w: %v", ErrLedgerUnavailable, ledger.fatalErr)
	}
	ledger.threads[threadID] = candidate
	resolved := candidate.projector.thread.Submissions[findSubmission(candidate.projector.thread, submissionID)]
	return dispositionForSubmission(candidate, resolved), nil
}

func dispatchAmbiguousFactKey(reason string, attemptID DispatchAttemptID) ProviderFactKey {
	return ProviderFactKey("daemon/dispatch/" + reason + "/" + string(attemptID) + "/admission-unknown")
}

func snapshotLedgerThread(thread *ledgerThread) DurableThreadSnapshot {
	snapshot := thread.projector.Snapshot()
	return DurableThreadSnapshot{
		Ownership:         thread.ownership,
		Writer:            thread.writer,
		Thread:            snapshot,
		Digest:            StateDigest(snapshot),
		ProviderFactCount: len(thread.projector.appliedProviderFacts),
	}
}

func dispositionForSubmission(thread *ledgerThread, submission Submission) SubmissionDisposition {
	return SubmissionDisposition{
		ThreadID:         thread.projector.thread.ID,
		SubmissionID:     submission.ID,
		WriterEpoch:      submission.WriterEpoch,
		WriterSequence:   submission.WriterSequence,
		Position:         submission.Position,
		AcceptedRevision: submission.AcceptedRevision,
		AcceptedAt:       dereferenceTime(submission.AcceptedAt),
		ThreadRevision:   thread.projector.thread.Revision,
		Payload: SubmissionPayload{
			Body:          submission.Payload.Body,
			AttachmentIDs: append([]string{}, submission.Payload.AttachmentIDs...),
		},
		Delivery:          submission.Delivery,
		DispatchAttemptID: submission.DispatchAttempt,
		ExecutionID:       submission.ExecutionID,
		InputOrdinal:      submission.InputOrdinal,
		Digest:            thread.projector.Digest(),
	}
}

func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func (ledger *Ledger) checkUsableLocked() error {
	if ledger.fatalErr != nil {
		return fmt.Errorf("%w: %v", ErrLedgerUnavailable, ledger.fatalErr)
	}
	return nil
}
