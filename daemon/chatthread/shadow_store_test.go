package chatthread

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShadowStoreIsIsolatedAndValidatesStateAtOpenBoundary(t *testing.T) {
	root := t.TempDir()
	productionPath := filepath.Join(root, ledgerStateFileName)
	productionSentinel := []byte("corrupt-production-ledger-must-remain-untouched\n")
	if err := os.WriteFile(productionPath, productionSentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := InitializeShadowStore(root)
	if err != nil {
		t.Fatalf("InitializeShadowStore: %v", err)
	}
	if store.Path() == productionPath || filepath.Base(store.Path()) != shadowStateFileName {
		t.Fatalf("shadow path %q is not isolated from production %q", store.Path(), productionPath)
	}
	assertFileBytes(t, productionPath, productionSentinel)
	cached, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread-a", 1))
	if err != nil {
		t.Fatalf("ApplyShadowBatch: %v", err)
	}
	assertFileBytes(t, productionPath, productionSentinel)

	corrupt := []byte("{not-valid-shadow-state\n")
	if err := os.WriteFile(store.Path(), corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ShadowSnapshot("shadow-thread-a")
	if err != nil {
		t.Fatalf("snapshot from owning handle after external corruption: %v", err)
	}
	if !reflect.DeepEqual(snapshot, cached) {
		t.Fatalf("owning handle stopped serving its validated cache")
	}
	replayed, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread-a", 1))
	if err != nil {
		t.Fatalf("idempotent apply from owning handle after external corruption: %v", err)
	}
	if !reflect.DeepEqual(replayed, cached) {
		t.Fatalf("idempotent apply changed the validated cache")
	}
	assertFileBytes(t, store.Path(), corrupt)
	assertFileBytes(t, productionPath, productionSentinel)
	if _, err := OpenShadowStore(root); !errors.Is(err, ErrShadowCorrupt) {
		t.Fatalf("OpenShadowStore corrupt error = %v", err)
	}
	assertFileBytes(t, store.Path(), corrupt)
	assertFileBytes(t, productionPath, productionSentinel)

	missingRoot := filepath.Join(t.TempDir(), "missing-shadow")
	if _, err := OpenShadowStore(missingRoot); !errors.Is(err, ErrShadowNotInitialized) {
		t.Fatalf("OpenShadowStore missing error = %v", err)
	}
	if _, err := os.Stat(missingRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenShadowStore created missing root: %v", err)
	}
}

func TestShadowStoreWriteFailurePermanentlyFailsHandleClosed(t *testing.T) {
	root := t.TempDir()
	store, err := InitializeShadowStore(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read initial shadow state: %v", err)
	}
	writes := 0
	store.atomicWrite = func(string, []byte) error {
		writes++
		return errors.New("injected persistence failure")
	}
	if _, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread", 1)); !errors.Is(err, ErrShadowUnavailable) {
		t.Fatalf("write failure error = %v", err)
	}
	if writes != 1 {
		t.Fatalf("atomic writes = %d, want 1", writes)
	}
	assertFileBytes(t, store.Path(), before)
	if _, err := store.ShadowSnapshot("shadow-thread"); !errors.Is(err, ErrShadowUnavailable) {
		t.Fatalf("snapshot after write failure error = %v", err)
	}
	if _, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread", 1)); !errors.Is(err, ErrShadowUnavailable) {
		t.Fatalf("second apply after write failure error = %v", err)
	}
	if writes != 1 {
		t.Fatalf("failed handle attempted %d writes, want 1", writes)
	}
}

func TestShadowStoreRejectsV2OwnershipAndPersistsOnlySanitizedProjection(t *testing.T) {
	root := t.TempDir()
	store, err := InitializeShadowStore(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread", 1))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ownership != ShadowOwnershipV1ReadOnly {
		t.Fatalf("ownership = %q", snapshot.Ownership)
	}
	if snapshot.Thread.Submissions[0].Origin != OriginProviderExternal ||
		snapshot.Thread.Submissions[0].Payload.Body != "" ||
		len(snapshot.Thread.Submissions[0].Payload.AttachmentIDs) != 0 ||
		snapshot.Thread.Events != nil && len(snapshot.Thread.Events) != 0 {
		t.Fatalf("shadow projection retained content or wrong origin: %#v", snapshot.Thread)
	}

	ledgerRoot := filepath.Join(t.TempDir(), "v2")
	ledger, err := InitializeLedger(ledgerRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.CreateThread(CreateThreadCommand{
		ThreadID:    "shadow-thread",
		Ownership:   ThreadOwnershipV1,
		WriterEpoch: "writer",
	}); !errors.Is(err, ErrThreadOwnership) {
		t.Fatalf("v2 Ledger accepted v1/shadow ownership: %v", err)
	}
	if _, err := ledger.Snapshot("shadow-thread"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("v2 Ledger mixed in shadow Thread: %v", err)
	}
}

func TestShadowStoreRestartPreservesRecordKeysAndRejectsLateUnseenCursor(t *testing.T) {
	root := t.TempDir()
	store, err := InitializeShadowStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ApplyShadowBatch(simpleShadowBatch(t, "shadow-thread", 10))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenShadowStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reopened.ShadowSnapshot("shadow-thread")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, restarted) {
		t.Fatalf("restart snapshot changed\nfirst: %#v\nrestart: %#v", first, restarted)
	}

	lateFingerprint, err := structuralFingerprint(struct{ Kind string }{Kind: "late"})
	if err != nil {
		t.Fatal(err)
	}
	late := ShadowBatch{
		ThreadID:     "shadow-thread",
		SourceToken:  diagnosticToken("source", "fixture"),
		SourceCursor: restarted.SourceCursor,
		Records: []ShadowRecord{{
			Key:         "record-late",
			Cursor:      5,
			Fingerprint: lateFingerprint,
		}},
		Legacy: LegacyShadowProjection{
			OrderedTurns:  []LegacyShadowTurn{{ID: "legacy", State: "completed"}},
			TerminalState: "completed",
		},
		CorrelationGaps: []ShadowCorrelationGap{{
			SubmissionID: "submission",
			RecordKey:    "record-input",
			Reason:       CorrelationGapNoExplicitAppBinding,
		}},
	}
	if _, err := reopened.ApplyShadowBatch(late); !errors.Is(err, ErrShadowRecordGap) {
		t.Fatalf("late unseen cursor error = %v", err)
	}
	after, err := reopened.ShadowSnapshot("shadow-thread")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted, after) {
		t.Fatalf("rejected late cursor changed state")
	}
}

func TestShadowProviderFactBoundaryRejectsContentAndDispatchState(t *testing.T) {
	for name, fact := range map[string]ProviderFact{
		"event payload": EventUpsertFact{
			Key:                "record-event",
			EventID:            "event",
			ExecutionID:        "execution",
			CausalSubmissionID: "submission",
			Kind:               EventAssistant,
			Payload:            "sensitive body",
		},
		"terminal reason": ActivityTerminalFact{
			Key:           "record-terminal",
			ExecutionID:   "execution",
			TerminalState: ActivityCompleted,
			Reason:        "sensitive provider output",
		},
		"dispatch ambiguity": DeliveryAmbiguousFact{
			Key:          "record-ambiguous",
			SubmissionID: "submission",
			AttemptID:    "attempt",
		},
		"path identifier": ActivityStartedFact{
			Key:         "record-start",
			ExecutionID: "/private/execution",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := validateShadowProviderFact(fact); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("validateShadowProviderFact error = %v", err)
			}
		})
	}
}

func simpleShadowBatch(t *testing.T, threadID ThreadID, baseCursor uint64) ShadowBatch {
	t.Helper()
	startFingerprint, err := structuralFingerprint(struct{ Kind string }{Kind: "start"})
	if err != nil {
		t.Fatal(err)
	}
	inputFingerprint, err := structuralFingerprint(struct{ Kind string }{Kind: "input"})
	if err != nil {
		t.Fatal(err)
	}
	terminalFingerprint, err := structuralFingerprint(struct{ Kind string }{Kind: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	executionID := ExecutionID("execution")
	submissionID := SubmissionID("submission")
	return ShadowBatch{
		ThreadID:     threadID,
		SourceToken:  diagnosticToken("source", "fixture"),
		SourceCursor: baseCursor + 3,
		Records: []ShadowRecord{
			{
				Key:         "record-start",
				Cursor:      baseCursor,
				Fingerprint: startFingerprint,
				Operations: []ShadowOperation{ProviderFactObserved{Fact: ActivityStartedFact{
					Key:         "record-start",
					ExecutionID: executionID,
				}}},
			},
			{
				Key:         "record-input",
				Cursor:      baseCursor + 1,
				Fingerprint: inputFingerprint,
				Operations: []ShadowOperation{
					ProviderExternalSubmissionObserved{SubmissionID: submissionID},
					ProviderFactObserved{Fact: InputAdmittedFact{
						Key:          "record-input",
						ExecutionID:  executionID,
						SubmissionID: submissionID,
						Ordinal:      1,
					}},
				},
			},
			{
				Key:         "record-terminal",
				Cursor:      baseCursor + 2,
				Fingerprint: terminalFingerprint,
				Operations: []ShadowOperation{ProviderFactObserved{Fact: ActivityTerminalFact{
					Key:           "record-terminal",
					ExecutionID:   executionID,
					TerminalState: ActivityCompleted,
				}}},
			},
		},
		Legacy: LegacyShadowProjection{
			OrderedTurns:  []LegacyShadowTurn{{ID: "legacy", State: "completed"}},
			TerminalState: "completed",
		},
		CorrelationGaps: []ShadowCorrelationGap{{
			SubmissionID: submissionID,
			RecordKey:    "record-input",
			Reason:       CorrelationGapNoExplicitAppBinding,
		}},
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s changed: got %q, want %q", filepath.Base(path), got, want)
	}
}
