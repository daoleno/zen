package chatthread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testLedgerThreadID ThreadID    = "durable-thread-01"
	testWriterEpoch    WriterEpoch = "app-writer-epoch-01"
)

func TestLedgerRestartPreservesIdentityOrderRevisionDigestWriterAndQueue(t *testing.T) {
	root := t.TempDir()
	ledger := mustOpenV2Ledger(t, root)
	firstRequest := testAppSubmission(1, "submission-01", "same body")
	firstRequest.Payload.AttachmentIDs = []string{"attachment-01"}
	first := mustLedgerAccept(t, ledger, firstRequest)
	second := mustLedgerAccept(t, ledger, testAppSubmission(2, "submission-02", "same body"))

	before := mustLedgerSnapshot(t, ledger)
	if first.Position != 1 || first.AcceptedRevision != 1 || second.Position != 2 || second.AcceptedRevision != 2 {
		t.Fatalf("acceptance positions/revisions = first %#v, second %#v", first, second)
	}
	if before.Thread.Revision != 2 || before.Thread.NextPosition != 3 || before.Writer.NextSequence != 3 {
		t.Fatalf("durable frontiers = Thread revision %d, position %d, writer %d", before.Thread.Revision, before.Thread.NextPosition, before.Writer.NextSequence)
	}
	if !reflect.DeepEqual(before.Thread.QueuedSubmissionIDs, []SubmissionID{"submission-01", "submission-02"}) {
		t.Fatalf("durable queue = %v", before.Thread.QueuedSubmissionIDs)
	}
	if before.Digest == "" || before.Digest != StateDigest(before.Thread) {
		t.Fatalf("durable digest = %q, calculated %q", before.Digest, StateDigest(before.Thread))
	}
	if got := requireSubmission(t, before.Thread, "submission-01"); got.WriterEpoch != testWriterEpoch ||
		got.WriterSequence != 1 || got.AcceptedRevision != 1 ||
		got.AcceptedAt == nil || got.AcceptedAt.IsZero() || got.AcceptedAt.Location() != time.UTC ||
		!reflect.DeepEqual(got.Payload.AttachmentIDs, []string{"attachment-01"}) {
		t.Fatalf("first durable Submission = %#v", got)
	}

	rawBeforeRestart := readLedgerFile(t, ledger.Path())
	restarted, err := OpenLedger(root)
	if err != nil {
		t.Fatalf("restart OpenLedger: %v", err)
	}
	after := mustLedgerSnapshot(t, restarted)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restart changed durable snapshot:\nbefore=%#v\nafter=%#v", before, after)
	}
	if rawAfterRestart := readLedgerFile(t, restarted.Path()); !bytes.Equal(rawAfterRestart, rawBeforeRestart) {
		t.Fatal("clean restart rewrote the durable state file")
	}

	replay := mustLedgerAccept(t, restarted, firstRequest)
	if replay.Position != first.Position || replay.AcceptedRevision != first.AcceptedRevision {
		t.Fatalf("retry lost stable acceptance disposition: first=%#v replay=%#v", first, replay)
	}
	if replay.ThreadRevision != before.Thread.Revision || replay.Digest != before.Digest {
		t.Fatalf("retry did not return current durable disposition: %#v", replay)
	}
	if got := mustLedgerSnapshot(t, restarted); !reflect.DeepEqual(got, before) {
		t.Fatal("exact retry changed restarted state")
	}
	if rawAfterReplay := readLedgerFile(t, restarted.Path()); !bytes.Equal(rawAfterReplay, rawBeforeRestart) {
		t.Fatal("exact retry rewrote the durable state file")
	}
}

func TestLedgerDuplicateConflictAndWriterRejectionsLeaveStateUnchanged(t *testing.T) {
	ledger := mustOpenV2Ledger(t, t.TempDir())
	request := testAppSubmission(1, "submission-01", "immutable")
	first := mustLedgerAccept(t, ledger, request)
	baseline := mustLedgerSnapshot(t, ledger)
	baselineRaw := readLedgerFile(t, ledger.Path())

	replay := mustLedgerAccept(t, ledger, request)
	if !reflect.DeepEqual(replay, first) {
		t.Fatalf("immediate exact retry disposition changed:\nfirst=%#v\nreplay=%#v", first, replay)
	}
	assertLedgerUnchanged(t, ledger, baseline, baselineRaw)

	tests := []struct {
		name    string
		request AppSubmissionRequest
		want    error
	}{
		{
			name: "same ID different payload",
			request: func() AppSubmissionRequest {
				conflict := request
				conflict.Payload.Body = "changed"
				return conflict
			}(),
			want: ErrIDConflict,
		},
		{
			name: "same ID different writer sequence",
			request: func() AppSubmissionRequest {
				conflict := request
				conflict.WriterSequence = 2
				return conflict
			}(),
			want: ErrWriterSequenceConflict,
		},
		{
			name: "same ID different writer epoch",
			request: func() AppSubmissionRequest {
				conflict := request
				conflict.WriterEpoch = "other-epoch"
				return conflict
			}(),
			want: ErrWriterEpochMismatch,
		},
		{
			name:    "unknown ID stale sequence",
			request: testAppSubmission(1, "submission-stale", "stale"),
			want:    ErrWriterSequenceStale,
		},
		{
			name:    "unknown ID sequence gap",
			request: testAppSubmission(3, "submission-gap", "gap"),
			want:    ErrWriterSequenceGap,
		},
		{
			name: "unknown ID epoch mismatch",
			request: func() AppSubmissionRequest {
				mismatch := testAppSubmission(2, "submission-other-epoch", "epoch")
				mismatch.WriterEpoch = "other-epoch"
				return mismatch
			}(),
			want: ErrWriterEpochMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ledger.Accept(test.request); !errors.Is(err, test.want) {
				t.Fatalf("Accept error = %v, want %v", err, test.want)
			}
			assertLedgerUnchanged(t, ledger, baseline, baselineRaw)
		})
	}
	var blockedCalls atomic.Int64
	if _, err := ledger.AcceptAndDispatch(
		context.Background(),
		testAppSubmission(2, "submission-blocked", "cannot leapfrog queue"),
		"attempt-blocked",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			blockedCalls.Add(1)
			return nil
		}),
	); !errors.Is(err, ErrDispatchOrderBlocked) {
		t.Fatalf("dispatch past queued predecessor error = %v, want ErrDispatchOrderBlocked", err)
	}
	if blockedCalls.Load() != 0 {
		t.Fatal("queued predecessor did not block provider effect")
	}
	assertLedgerUnchanged(t, ledger, baseline, baselineRaw)

	second := mustLedgerAccept(t, ledger, testAppSubmission(2, "submission-02", "next"))
	if second.Position != 2 || second.AcceptedRevision != 2 {
		t.Fatalf("valid successor disposition = %#v", second)
	}
}

func TestLedgerRequiresExplicitExclusiveV2Ownership(t *testing.T) {
	root := t.TempDir()
	ledger, err := InitializeLedger(root)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	emptyRaw := readLedgerFile(t, ledger.Path())
	for _, ownership := range []ThreadOwnership{"", ThreadOwnershipV1} {
		_, err := ledger.CreateThread(CreateThreadCommand{
			ThreadID:    testLedgerThreadID,
			Ownership:   ownership,
			WriterEpoch: testWriterEpoch,
		})
		if !errors.Is(err, ErrThreadOwnership) {
			t.Fatalf("CreateThread ownership %q error = %v, want ErrThreadOwnership", ownership, err)
		}
		if raw := readLedgerFile(t, ledger.Path()); !bytes.Equal(raw, emptyRaw) {
			t.Fatalf("rejected ownership %q changed the state file", ownership)
		}
	}

	created, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    testLedgerThreadID,
		Ownership:   ThreadOwnershipV2,
		WriterEpoch: testWriterEpoch,
	})
	if err != nil {
		t.Fatalf("CreateThread v2: %v", err)
	}
	v2Raw := readLedgerFile(t, ledger.Path())
	replayed, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    testLedgerThreadID,
		Ownership:   ThreadOwnershipV2,
		WriterEpoch: testWriterEpoch,
	})
	if err != nil || !reflect.DeepEqual(replayed, created) {
		t.Fatalf("idempotent v2 claim = %#v, %v", replayed, err)
	}
	if raw := readLedgerFile(t, ledger.Path()); !bytes.Equal(raw, v2Raw) {
		t.Fatal("idempotent v2 claim rewrote the state file")
	}
	if _, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    testLedgerThreadID,
		Ownership:   ThreadOwnershipV2,
		WriterEpoch: "replacement-epoch",
	}); !errors.Is(err, ErrWriterEpochMismatch) {
		t.Fatalf("writer epoch replacement error = %v, want ErrWriterEpochMismatch", err)
	}
	assertLedgerUnchanged(t, ledger, created, v2Raw)
	if _, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    testLedgerThreadID,
		Ownership:   ThreadOwnershipV1,
		WriterEpoch: testWriterEpoch,
	}); !errors.Is(err, ErrThreadOwnership) {
		t.Fatalf("v1 claim over v2 error = %v, want ErrThreadOwnership", err)
	}
	assertLedgerUnchanged(t, ledger, created, v2Raw)

	rewriteLedgerDocument(t, ledger.Path(), func(document *ledgerDocument) {
		document.Threads[0].Ownership = ThreadOwnershipV1
	})
	mixedRaw := readLedgerFile(t, ledger.Path())
	if _, err := OpenLedger(root); !errors.Is(err, ErrLedgerCorrupt) || !errors.Is(err, ErrThreadOwnership) {
		t.Fatalf("opening mixed ownership error = %v, want ErrLedgerCorrupt and ErrThreadOwnership", err)
	}
	if raw := readLedgerFile(t, ledger.Path()); !bytes.Equal(raw, mixedRaw) {
		t.Fatal("failed mixed-ownership open rewrote the ledger")
	}
}

func TestAcceptAndDispatchPersistsAcceptanceAndAttemptBeforeEffect(t *testing.T) {
	root := t.TempDir()
	ledger := mustOpenV2Ledger(t, root)
	request := testAppSubmission(1, "submission-01", "dispatch me")
	request.Payload.AttachmentIDs = []string{"attachment-01"}
	var calls atomic.Int64

	boundary := DispatchBoundaryFunc(func(_ context.Context, dispatched ProviderDispatch) error {
		calls.Add(1)
		threads, err := decodeLedgerDocument(readLedgerFile(t, ledger.Path()))
		if err != nil {
			t.Fatalf("decode durable state from dispatch boundary: %v", err)
		}
		durable := snapshotLedgerThread(threads[testLedgerThreadID])
		submission := requireSubmission(t, durable.Thread, request.SubmissionID)
		if submission.Delivery != DeliveryDelivering || submission.DispatchAttempt != "attempt-01" {
			t.Fatalf("dispatch boundary observed non-durable attempt: %#v", submission)
		}
		if durable.Thread.Revision != 1 || submission.AcceptedRevision != 1 ||
			durable.Writer.NextSequence != 2 || len(durable.Thread.QueuedSubmissionIDs) != 0 {
			t.Fatalf("dispatch boundary observed partial transaction: %#v", durable)
		}
		if dispatched.Position != submission.Position || dispatched.AcceptedRevision != submission.AcceptedRevision ||
			submission.AcceptedAt == nil || !dispatched.AcceptedAt.Equal(*submission.AcceptedAt) ||
			dispatched.AttemptID != submission.DispatchAttempt ||
			!reflect.DeepEqual(dispatched.Payload, submission.Payload) {
			t.Fatalf("dispatch request differs from durable Submission: dispatch=%#v submission=%#v", dispatched, submission)
		}
		return nil
	})

	result, err := ledger.AcceptAndDispatch(context.Background(), request, "attempt-01", boundary)
	if err != nil {
		t.Fatalf("AcceptAndDispatch: %v", err)
	}
	if calls.Load() != 1 || !result.ProviderEffectAttempted {
		t.Fatalf("provider effect calls=%d result=%#v", calls.Load(), result)
	}
	if result.Disposition.Delivery != DeliveryAmbiguous || result.Disposition.DispatchAttemptID != "attempt-01" {
		t.Fatalf("post-boundary disposition = %#v", result.Disposition)
	}
	after := mustLedgerSnapshot(t, ledger)
	if after.Thread.Revision != 2 || after.ProviderFactCount != 1 || after.Writer.NextSequence != 2 {
		t.Fatalf("post-boundary durable state = %#v", after)
	}
	rawBeforeRetry := readLedgerFile(t, ledger.Path())

	retry, err := ledger.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-must-not-replace",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			t.Fatal("exact retry crossed the provider boundary")
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("exact AcceptAndDispatch retry: %v", err)
	}
	if retry.ProviderEffectAttempted || !reflect.DeepEqual(retry.Disposition, result.Disposition) {
		t.Fatalf("exact retry result = %#v, first = %#v", retry, result)
	}
	if rawAfterRetry := readLedgerFile(t, ledger.Path()); !bytes.Equal(rawAfterRetry, rawBeforeRetry) {
		t.Fatal("exact dispatch retry rewrote the ledger")
	}
	conflict := request
	conflict.Payload.Body = "conflicting payload"
	if _, err := ledger.AcceptAndDispatch(
		context.Background(),
		conflict,
		"attempt-conflict",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			t.Fatal("conflicting Submission crossed the provider boundary")
			return nil
		}),
	); !errors.Is(err, ErrIDConflict) {
		t.Fatalf("conflicting dispatch retry error = %v, want ErrIDConflict", err)
	}
	assertLedgerUnchanged(t, ledger, after, rawBeforeRetry)
}

func TestAcceptAndDispatchPersistenceFailurePreventsProviderEffect(t *testing.T) {
	ledger := mustOpenV2Ledger(t, t.TempDir())
	before := mustLedgerSnapshot(t, ledger)
	beforeRaw := readLedgerFile(t, ledger.Path())
	persistErr := errors.New("injected persistence failure")
	ledger.atomicWrite = func(string, []byte) error { return persistErr }
	var calls atomic.Int64

	result, err := ledger.AcceptAndDispatch(
		context.Background(),
		testAppSubmission(1, "submission-01", "must stay local"),
		"attempt-01",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			calls.Add(1)
			return nil
		}),
	)
	if !errors.Is(err, persistErr) {
		t.Fatalf("AcceptAndDispatch error = %v, want injected persistence failure", err)
	}
	if calls.Load() != 0 || result.ProviderEffectAttempted {
		t.Fatalf("provider effect crossed failed durability gate: calls=%d result=%#v", calls.Load(), result)
	}
	assertLedgerUnchanged(t, ledger, before, beforeRaw)
}

func TestCrashAfterPersistBeforeEffectRecoversAmbiguousWithoutAutoDispatch(t *testing.T) {
	root := t.TempDir()
	ledger := mustOpenV2Ledger(t, root)
	request := testAppSubmission(1, "submission-01", "crash window")
	var boundaryEntries atomic.Int64
	var providerEffects atomic.Int64
	crashValue := "simulated daemon crash before provider effect"
	var recovered any

	func() {
		defer func() { recovered = recover() }()
		_, _ = ledger.AcceptAndDispatch(
			context.Background(),
			request,
			"attempt-crash",
			DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
				boundaryEntries.Add(1)
				threads, err := decodeLedgerDocument(readLedgerFile(t, ledger.Path()))
				if err != nil {
					t.Fatalf("decode pre-effect durable state: %v", err)
				}
				persisted := requireSubmission(t, threads[testLedgerThreadID].projector.Snapshot(), request.SubmissionID)
				if persisted.Delivery != DeliveryDelivering || persisted.DispatchAttempt != "attempt-crash" {
					t.Fatalf("crash boundary observed incomplete persistence: %#v", persisted)
				}
				simulateDaemonCrash(crashValue)
				// A real provider effect would begin here.
				providerEffects.Add(1)
				return nil
			}),
		)
	}()
	if recovered != crashValue {
		t.Fatalf("recovered panic = %#v, want %q", recovered, crashValue)
	}
	if boundaryEntries.Load() != 1 || providerEffects.Load() != 0 {
		t.Fatalf("crash simulation entries=%d effects=%d", boundaryEntries.Load(), providerEffects.Load())
	}

	persistedThreads, err := decodeLedgerDocument(readLedgerFile(t, ledger.Path()))
	if err != nil {
		t.Fatalf("decode post-crash file: %v", err)
	}
	preRecovery := snapshotLedgerThread(persistedThreads[testLedgerThreadID])
	if got := requireSubmission(t, preRecovery.Thread, request.SubmissionID); got.Delivery != DeliveryDelivering {
		t.Fatalf("post-crash on-disk delivery = %s, want delivering recovery marker", got.Delivery)
	}

	restarted, err := OpenLedger(root)
	if err != nil {
		t.Fatalf("restart after crash: %v", err)
	}
	afterRecovery := mustLedgerSnapshot(t, restarted)
	recoveredSubmission := requireSubmission(t, afterRecovery.Thread, request.SubmissionID)
	if recoveredSubmission.Delivery != DeliveryAmbiguous || recoveredSubmission.DispatchAttempt != "attempt-crash" {
		t.Fatalf("recovered dispatch disposition = %#v", recoveredSubmission)
	}
	if afterRecovery.Thread.Revision != 2 || afterRecovery.ProviderFactCount != 1 ||
		afterRecovery.Writer.NextSequence != 2 || len(afterRecovery.Thread.QueuedSubmissionIDs) != 0 {
		t.Fatalf("recovered durable state = %#v", afterRecovery)
	}
	if boundaryEntries.Load() != 1 || providerEffects.Load() != 0 {
		t.Fatal("opening the ledger automatically invoked the provider boundary")
	}

	recoveredRaw := readLedgerFile(t, restarted.Path())
	restartedAgain, err := OpenLedger(root)
	if err != nil {
		t.Fatalf("second restart after recovery: %v", err)
	}
	if again := mustLedgerSnapshot(t, restartedAgain); !reflect.DeepEqual(again, afterRecovery) {
		t.Fatalf("ambiguous recovery was not restart-stable:\nfirst=%#v\nsecond=%#v", afterRecovery, again)
	}
	if raw := readLedgerFile(t, restartedAgain.Path()); !bytes.Equal(raw, recoveredRaw) {
		t.Fatal("second restart rewrote already ambiguous state")
	}

	var retryCalls atomic.Int64
	retry, err := restartedAgain.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-retry-forbidden",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			retryCalls.Add(1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("exact retry after ambiguous recovery: %v", err)
	}
	if retryCalls.Load() != 0 || retry.ProviderEffectAttempted || retry.Disposition.Delivery != DeliveryAmbiguous {
		t.Fatalf("ambiguous retry crossed provider boundary: calls=%d result=%#v", retryCalls.Load(), retry)
	}
}

func TestDispatchBoundaryErrorRemainsAmbiguousAcrossRestart(t *testing.T) {
	root := t.TempDir()
	ledger := mustOpenV2Ledger(t, root)
	request := testAppSubmission(1, "submission-01", "unknown admission")
	boundaryErr := errors.New("transport closed during provider write")
	var calls atomic.Int64
	result, err := ledger.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-unknown",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			calls.Add(1)
			return boundaryErr
		}),
	)
	if !errors.Is(err, ErrDispatchAdmissionUnknown) || !errors.Is(err, boundaryErr) {
		t.Fatalf("dispatch error = %v, want admission unknown and boundary cause", err)
	}
	if calls.Load() != 1 || !result.ProviderEffectAttempted || result.Disposition.Delivery != DeliveryAmbiguous {
		t.Fatalf("unknown admission result = %#v calls=%d", result, calls.Load())
	}

	restarted, err := OpenLedger(root)
	if err != nil {
		t.Fatalf("restart ambiguous dispatch: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("restart retried unknown dispatch admission")
	}
	var retryCalls atomic.Int64
	retry, err := restarted.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-02",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			retryCalls.Add(1)
			return nil
		}),
	)
	if err != nil || retryCalls.Load() != 0 || retry.ProviderEffectAttempted || retry.Disposition.Delivery != DeliveryAmbiguous {
		t.Fatalf("duplicate ambiguous dispatch retry = %#v, err=%v, calls=%d", retry, err, retryCalls.Load())
	}
}

func TestProviderFactIdempotencyAndAdmissionResolutionSurviveRestart(t *testing.T) {
	root := t.TempDir()
	ledger := mustOpenV2Ledger(t, root)
	request := testAppSubmission(1, "submission-01", "resolve by fact")
	if _, err := ledger.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-01",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error { return nil }),
	); err != nil {
		t.Fatalf("AcceptAndDispatch: %v", err)
	}
	start := ActivityStartedFact{Key: "provider/activity/start", ExecutionID: "execution-01"}
	if result, err := ledger.ApplyProviderFact(testLedgerThreadID, start); err != nil || !result.Changed {
		t.Fatalf("apply start fact = %#v, %v", result, err)
	}
	admission := InputAdmittedFact{
		Key:          "provider/input/01",
		ExecutionID:  "execution-01",
		SubmissionID: request.SubmissionID,
		Ordinal:      1,
	}
	if result, err := ledger.ApplyProviderFact(testLedgerThreadID, admission); err != nil || !result.Changed {
		t.Fatalf("apply admission fact = %#v, %v", result, err)
	}
	event := EventUpsertFact{
		Key:                "provider/event/01",
		EventID:            "event-01",
		ExecutionID:        "execution-01",
		CausalSubmissionID: request.SubmissionID,
		Kind:               EventAssistant,
		Final:              true,
		Payload:            "authoritative output",
	}
	if result, err := ledger.ApplyProviderFact(testLedgerThreadID, event); err != nil || !result.Changed {
		t.Fatalf("apply Event fact = %#v, %v", result, err)
	}
	visibleBeforeFactOnlyRecord := mustLedgerSnapshot(t, ledger)
	rawBeforeFactOnlyRecord := readLedgerFile(t, ledger.Path())
	redundantEventFact := event
	redundantEventFact.Key = "provider/event/01/redundant-observation"
	factOnlyResult, err := ledger.ApplyProviderFact(testLedgerThreadID, redundantEventFact)
	if err != nil || factOnlyResult.Changed || factOnlyResult.Revision != visibleBeforeFactOnlyRecord.Thread.Revision {
		t.Fatalf("record visible-no-op provider fact = %#v, %v", factOnlyResult, err)
	}
	if raw := readLedgerFile(t, ledger.Path()); bytes.Equal(raw, rawBeforeFactOnlyRecord) {
		t.Fatal("visible-no-op provider fact fingerprint was not durably recorded")
	}

	before := mustLedgerSnapshot(t, ledger)
	delivered := requireSubmission(t, before.Thread, request.SubmissionID)
	if delivered.Delivery != DeliveryDelivered || delivered.DispatchAttempt != "attempt-01" ||
		delivered.ExecutionID != "execution-01" || delivered.InputOrdinal != 1 {
		t.Fatalf("authoritative admission resolution = %#v", delivered)
	}
	if before.ProviderFactCount != 5 || before.Digest != visibleBeforeFactOnlyRecord.Digest ||
		before.Thread.Revision != visibleBeforeFactOnlyRecord.Thread.Revision {
		t.Fatalf("provider fact-only durability = before %#v, prior visible %#v", before, visibleBeforeFactOnlyRecord)
	}

	restarted, err := OpenLedger(root)
	if err != nil {
		t.Fatalf("restart provider fact ledger: %v", err)
	}
	after := mustLedgerSnapshot(t, restarted)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("provider fact restart changed state:\nbefore=%#v\nafter=%#v", before, after)
	}
	rawBeforeReplay := readLedgerFile(t, restarted.Path())
	replay, err := restarted.ApplyProviderFact(testLedgerThreadID, redundantEventFact)
	if err != nil || replay.Changed || replay.Revision != before.Thread.Revision {
		t.Fatalf("replayed provider fact = %#v, %v", replay, err)
	}
	if raw := readLedgerFile(t, restarted.Path()); !bytes.Equal(raw, rawBeforeReplay) {
		t.Fatal("replayed provider fact rewrote durable state")
	}

	conflict := redundantEventFact
	conflict.Payload = "conflicting observation"
	if _, err := restarted.ApplyProviderFact(testLedgerThreadID, conflict); !errors.Is(err, ErrFactKeyConflict) {
		t.Fatalf("conflicting recovered provider fact error = %v, want ErrFactKeyConflict", err)
	}
	assertLedgerUnchanged(t, restarted, before, rawBeforeReplay)

	var retryCalls atomic.Int64
	retry, err := restarted.AcceptAndDispatch(
		context.Background(),
		request,
		"attempt-must-not-run",
		DispatchBoundaryFunc(func(context.Context, ProviderDispatch) error {
			retryCalls.Add(1)
			return nil
		}),
	)
	if err != nil || retryCalls.Load() != 0 || retry.ProviderEffectAttempted || retry.Disposition.Delivery != DeliveryDelivered {
		t.Fatalf("delivered duplicate retry = %#v, err=%v, calls=%d", retry, err, retryCalls.Load())
	}
}

func TestLedgerCorruptionAndSchemaErrorsFailClosed(t *testing.T) {
	t.Run("empty state file", func(t *testing.T) {
		root := t.TempDir()
		ledger, err := InitializeLedger(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ledger.Path(), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		assertOpenFailureLeavesFile(t, root, ledger.Path(), ErrLedgerCorrupt)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		root := t.TempDir()
		ledger, err := InitializeLedger(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ledger.Path(), []byte("{truncated"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertOpenFailureLeavesFile(t, root, ledger.Path(), ErrLedgerCorrupt)
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		root := t.TempDir()
		ledger := mustOpenV2Ledger(t, root)
		mustLedgerAccept(t, ledger, testAppSubmission(1, "submission-01", "original-body"))
		raw := readLedgerFile(t, ledger.Path())
		tampered := bytes.Replace(raw, []byte("original-body"), []byte("tampered-body"), 1)
		if bytes.Equal(tampered, raw) {
			t.Fatal("test did not tamper with ledger body")
		}
		if err := os.WriteFile(ledger.Path(), tampered, 0o600); err != nil {
			t.Fatal(err)
		}
		assertOpenFailureLeavesFile(t, root, ledger.Path(), ErrLedgerCorrupt)
	})

	t.Run("unsupported schema", func(t *testing.T) {
		root := t.TempDir()
		ledger, err := InitializeLedger(root)
		if err != nil {
			t.Fatal(err)
		}
		rewriteLedgerDocument(t, ledger.Path(), func(document *ledgerDocument) {
			document.SchemaVersion++
		})
		assertOpenFailureLeavesFile(t, root, ledger.Path(), ErrLedgerSchema)
	})

	t.Run("writer invariant despite valid checksum", func(t *testing.T) {
		root := t.TempDir()
		ledger := mustOpenV2Ledger(t, root)
		mustLedgerAccept(t, ledger, testAppSubmission(1, "submission-01", "body"))
		rewriteLedgerDocument(t, ledger.Path(), func(document *ledgerDocument) {
			document.Threads[0].Writer.NextSequence = 7
		})
		before := readLedgerFile(t, ledger.Path())
		_, err := OpenLedger(root)
		if !errors.Is(err, ErrLedgerCorrupt) || !errors.Is(err, ErrInvariant) {
			t.Fatalf("invariant-corrupt OpenLedger error = %v", err)
		}
		if after := readLedgerFile(t, ledger.Path()); !bytes.Equal(after, before) {
			t.Fatal("invariant-corrupt open rewrote state")
		}
	})

	t.Run("thread digest mismatch despite valid document checksum", func(t *testing.T) {
		root := t.TempDir()
		ledger := mustOpenV2Ledger(t, root)
		mustLedgerAccept(t, ledger, testAppSubmission(1, "submission-01", "body"))
		rewriteLedgerDocument(t, ledger.Path(), func(document *ledgerDocument) {
			document.Threads[0].Thread.Submissions[0].Payload.Body = "digest-tamper"
		})
		assertOpenFailureLeavesFile(t, root, ledger.Path(), ErrLedgerCorrupt)
	})
}

func TestLedgerUsesPrivateDirectoryAndFileModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ledger")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger, err := InitializeLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("ledger modes directory=%o file=%o", directoryInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

func TestLedgerMissingStateFailsClosedAndInitializationIsExclusive(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ledgerStateFileName)
	if _, err := OpenLedger(root); !errors.Is(err, ErrLedgerNotInitialized) {
		t.Fatalf("open uninitialized ledger error = %v, want ErrLedgerNotInitialized", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenLedger created missing state: %v", err)
	}

	ledger, err := InitializeLedger(root)
	if err != nil {
		t.Fatalf("InitializeLedger: %v", err)
	}
	initializedRaw := readLedgerFile(t, ledger.Path())
	if _, err := InitializeLedger(root); !errors.Is(err, ErrLedgerAlreadyInitialized) {
		t.Fatalf("second initialization error = %v, want ErrLedgerAlreadyInitialized", err)
	}
	if raw := readLedgerFile(t, ledger.Path()); !bytes.Equal(raw, initializedRaw) {
		t.Fatal("rejected initialization replaced existing state")
	}

	if err := os.Remove(ledger.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLedger(root); !errors.Is(err, ErrLedgerNotInitialized) {
		t.Fatalf("open deleted ledger error = %v, want ErrLedgerNotInitialized", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenLedger silently recreated deleted state: %v", err)
	}
}

func TestConcurrentDuplicateAcceptAndDispatchCrossesBoundaryExactlyOnce(t *testing.T) {
	ledger := mustOpenV2Ledger(t, t.TempDir())
	request := testAppSubmission(1, "submission-01", "concurrent duplicate")
	request.Payload.AttachmentIDs = []string{"attachment-01"}
	const callers = 32
	start := make(chan struct{})
	type outcome struct {
		result DispatchResult
		err    error
	}
	outcomes := make(chan outcome, callers)
	var wait sync.WaitGroup
	var boundaryCalls atomic.Int64
	boundary := DispatchBoundaryFunc(func(_ context.Context, dispatched ProviderDispatch) error {
		boundaryCalls.Add(1)
		dispatched.Payload.AttachmentIDs[0] = "boundary-mutation"
		return nil
	})
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := ledger.AcceptAndDispatch(context.Background(), request, "attempt-01", boundary)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)

	attempted := 0
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent AcceptAndDispatch: %v", outcome.err)
		}
		if outcome.result.ProviderEffectAttempted {
			attempted++
		}
		if outcome.result.Disposition.Position != 1 || outcome.result.Disposition.AcceptedRevision != 1 ||
			outcome.result.Disposition.Delivery != DeliveryAmbiguous {
			t.Fatalf("concurrent disposition = %#v", outcome.result.Disposition)
		}
	}
	if attempted != 1 || boundaryCalls.Load() != 1 {
		t.Fatalf("provider attempts in results=%d boundary calls=%d, want exactly one", attempted, boundaryCalls.Load())
	}
	state := mustLedgerSnapshot(t, ledger)
	if len(state.Thread.Submissions) != 1 || state.Writer.NextSequence != 2 ||
		state.Thread.Revision != 2 || state.ProviderFactCount != 1 {
		t.Fatalf("concurrent final state = %#v", state)
	}
	if got := requireSubmission(t, state.Thread, request.SubmissionID); !reflect.DeepEqual(got.Payload.AttachmentIDs, []string{"attachment-01"}) {
		t.Fatalf("boundary mutated durable payload: %#v", got.Payload)
	}
}

func TestConcurrentSequentialWriterCannotCrossAmbiguousPredecessor(t *testing.T) {
	ledger := mustOpenV2Ledger(t, t.TempDir())
	enteredFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	var orderMu sync.Mutex
	var effectOrder []WriterSequence
	boundary := DispatchBoundaryFunc(func(_ context.Context, dispatched ProviderDispatch) error {
		orderMu.Lock()
		effectOrder = append(effectOrder, dispatched.WriterSequence)
		orderMu.Unlock()
		if dispatched.WriterSequence == 1 {
			close(enteredFirst)
			<-releaseFirst
		}
		return nil
	})
	type callResult struct {
		result DispatchResult
		err    error
	}
	firstDone := make(chan callResult, 1)
	secondDone := make(chan callResult, 1)
	go func() {
		result, err := ledger.AcceptAndDispatch(
			context.Background(),
			testAppSubmission(1, "submission-01", "first"),
			"attempt-01",
			boundary,
		)
		firstDone <- callResult{result: result, err: err}
	}()
	<-enteredFirst
	go func() {
		result, err := ledger.AcceptAndDispatch(
			context.Background(),
			testAppSubmission(2, "submission-02", "second"),
			"attempt-02",
			boundary,
		)
		secondDone <- callResult{result: result, err: err}
	}()
	close(releaseFirst)
	first := <-firstDone
	second := <-secondDone
	if first.err != nil || !errors.Is(second.err, ErrDispatchOrderBlocked) {
		t.Fatalf("sequential dispatch results: first=%v second=%v", first.err, second.err)
	}
	orderMu.Lock()
	gotOrder := append([]WriterSequence{}, effectOrder...)
	orderMu.Unlock()
	if !reflect.DeepEqual(gotOrder, []WriterSequence{1}) {
		t.Fatalf("provider effect order = %v, want only the first writer", gotOrder)
	}
	state := mustLedgerSnapshot(t, ledger)
	if state.Writer.NextSequence != 2 || len(state.Thread.Submissions) != 1 {
		t.Fatalf("sequential writer state = %#v", state)
	}
	if submission := state.Thread.Submissions[0]; submission.Position != 1 || submission.WriterSequence != 1 ||
		submission.Delivery != DeliveryAmbiguous {
		t.Fatalf("first Submission = %#v", submission)
	}
	if second.result.ProviderEffectAttempted {
		t.Fatalf("blocked successor reported a provider effect: %#v", second.result)
	}
}

func mustOpenV2Ledger(t *testing.T, root string) *Ledger {
	t.Helper()
	ledger, err := InitializeLedger(root)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    testLedgerThreadID,
		Ownership:   ThreadOwnershipV2,
		WriterEpoch: testWriterEpoch,
	}); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	return ledger
}

func testAppSubmission(sequence WriterSequence, submissionID SubmissionID, body string) AppSubmissionRequest {
	return AppSubmissionRequest{
		ThreadID:       testLedgerThreadID,
		SubmissionID:   submissionID,
		WriterEpoch:    testWriterEpoch,
		WriterSequence: sequence,
		Payload:        SubmissionPayload{Body: body},
	}
}

func mustLedgerAccept(t *testing.T, ledger *Ledger, request AppSubmissionRequest) SubmissionDisposition {
	t.Helper()
	disposition, err := ledger.Accept(request)
	if err != nil {
		t.Fatalf("Accept(%q): %v", request.SubmissionID, err)
	}
	return disposition
}

func mustLedgerSnapshot(t *testing.T, ledger *Ledger) DurableThreadSnapshot {
	t.Helper()
	snapshot, err := ledger.Snapshot(testLedgerThreadID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snapshot
}

func readLedgerFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger %s: %v", path, err)
	}
	return raw
}

func assertLedgerUnchanged(
	t *testing.T,
	ledger *Ledger,
	want DurableThreadSnapshot,
	wantRaw []byte,
) {
	t.Helper()
	if got := mustLedgerSnapshot(t, ledger); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected operation changed snapshot:\nwant=%#v\ngot=%#v", want, got)
	}
	if gotRaw := readLedgerFile(t, ledger.Path()); !bytes.Equal(gotRaw, wantRaw) {
		t.Fatal("rejected operation changed durable bytes")
	}
}

func rewriteLedgerDocument(t *testing.T, path string, mutate func(*ledgerDocument)) {
	t.Helper()
	var document ledgerDocument
	if err := json.Unmarshal(readLedgerFile(t, path), &document); err != nil {
		t.Fatalf("decode ledger document: %v", err)
	}
	mutate(&document)
	checksum, err := ledgerDocumentChecksum(document)
	if err != nil {
		t.Fatalf("recalculate ledger checksum: %v", err)
	}
	document.Checksum = checksum
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode ledger document: %v", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write ledger document: %v", err)
	}
}

func assertOpenFailureLeavesFile(t *testing.T, root, path string, want error) {
	t.Helper()
	before := readLedgerFile(t, path)
	if _, err := OpenLedger(root); !errors.Is(err, want) {
		t.Fatalf("OpenLedger error = %v, want %v", err, want)
	}
	if after := readLedgerFile(t, path); !bytes.Equal(after, before) {
		t.Fatal("failed OpenLedger silently replaced corrupt state")
	}
}

func simulateDaemonCrash(value any) {
	panic(value)
}
