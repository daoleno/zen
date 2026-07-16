package chatthread

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ledgerSchemaVersion = 1
	ledgerStateFileName = "chatthread-v2.json"
)

type ledgerDocument struct {
	SchemaVersion int                     `json:"schema_version"`
	Threads       []persistedLedgerThread `json:"threads"`
	Checksum      string                  `json:"checksum"`
}

type persistedLedgerThread struct {
	Ownership     ThreadOwnership         `json:"ownership"`
	Writer        WriterState             `json:"writer"`
	Thread        Thread                  `json:"thread"`
	ThreadDigest  string                  `json:"thread_digest"`
	ProviderFacts []persistedProviderFact `json:"provider_facts"`
}

type persistedProviderFact struct {
	Key         ProviderFactKey `json:"key"`
	Fingerprint string          `json:"sha256"`
}

// InitializeLedger explicitly creates a new empty materialized v2 ledger. It
// never replaces an existing state file.
func InitializeLedger(root string) (*Ledger, error) {
	ledger, err := newLedgerHandle(root, true)
	if err != nil {
		return nil, err
	}
	raw, err := encodeLedgerDocument(ledger.threads)
	if err != nil {
		return nil, err
	}
	if err := writeLedgerInitial(ledger.path, raw); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrLedgerAlreadyInitialized, ledger.path)
		}
		return nil, fmt.Errorf("initialize canonical Chat ledger: %w", err)
	}
	return ledger, nil
}

// OpenLedger opens an existing isolated materialized v2 ledger rooted at root.
// A missing, empty, malformed, unsupported, checksum-invalid, or
// invariant-invalid state file fails closed instead of silently returning an
// empty state.
func OpenLedger(root string) (*Ledger, error) {
	ledger, err := newLedgerHandle(root, false)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(ledger.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrLedgerNotInitialized, ledger.path)
	}
	if err != nil {
		return nil, fmt.Errorf("read canonical Chat ledger: %w", err)
	}
	if err := os.Chmod(ledger.path, 0o600); err != nil {
		return nil, fmt.Errorf("secure canonical Chat ledger file: %w", err)
	}

	threads, err := decodeLedgerDocument(raw)
	if err != nil {
		return nil, err
	}
	ledger.threads = threads
	recovered, changed, err := recoverUnknownDispatchAdmissions(threads)
	if err != nil {
		return nil, fmt.Errorf("%w: recover dispatch attempts: %w", ErrLedgerCorrupt, err)
	}
	if changed {
		if err := ledger.persistThreadsLocked(recovered); err != nil {
			return nil, fmt.Errorf("persist ambiguous dispatch recovery: %w", err)
		}
		ledger.threads = recovered
	}
	return ledger, nil
}

func newLedgerHandle(root string, createRoot bool) (*Ledger, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("%w: ledger root is required", ErrInvalidArgument)
	}
	root = filepath.Clean(root)
	if createRoot {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, fmt.Errorf("create canonical Chat ledger root: %w", err)
		}
	} else {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: ledger root %s", ErrLedgerNotInitialized, root)
		}
		if err != nil {
			return nil, fmt.Errorf("inspect canonical Chat ledger root: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: ledger root %s is not a directory", ErrInvalidArgument, root)
		}
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure canonical Chat ledger root: %w", err)
	}

	ledger := &Ledger{
		path:        filepath.Join(root, ledgerStateFileName),
		threads:     make(map[ThreadID]*ledgerThread),
		atomicWrite: writeLedgerAtomic,
		now:         time.Now,
	}
	return ledger, nil
}

func (ledger *Ledger) persistThreadLocked(threadID ThreadID, candidate *ledgerThread) error {
	if err := validateLedgerThread(threadID, candidate); err != nil {
		return err
	}
	threads := make(map[ThreadID]*ledgerThread, len(ledger.threads)+1)
	for existingID, thread := range ledger.threads {
		threads[existingID] = thread
	}
	threads[threadID] = candidate
	if err := ledger.persistThreadsLocked(threads); err != nil {
		if errors.Is(err, ErrDurabilityUncertain) {
			ledger.fatalErr = err
		}
		return err
	}
	return nil
}

func (ledger *Ledger) persistThreadsLocked(threads map[ThreadID]*ledgerThread) error {
	raw, err := encodeLedgerDocument(threads)
	if err != nil {
		return err
	}
	if err := ledger.atomicWrite(ledger.path, raw); err != nil {
		return fmt.Errorf("replace canonical Chat ledger: %w", err)
	}
	return nil
}

func encodeLedgerDocument(threads map[ThreadID]*ledgerThread) ([]byte, error) {
	threadIDs := make([]ThreadID, 0, len(threads))
	for threadID := range threads {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Slice(threadIDs, func(left, right int) bool { return threadIDs[left] < threadIDs[right] })

	document := ledgerDocument{
		SchemaVersion: ledgerSchemaVersion,
		Threads:       make([]persistedLedgerThread, 0, len(threadIDs)),
	}
	for _, threadID := range threadIDs {
		thread := threads[threadID]
		if err := validateLedgerThread(threadID, thread); err != nil {
			return nil, err
		}
		facts := make([]persistedProviderFact, 0, len(thread.projector.appliedProviderFacts))
		for key, fingerprint := range thread.projector.appliedProviderFacts {
			facts = append(facts, persistedProviderFact{
				Key:         key,
				Fingerprint: hex.EncodeToString(fingerprint[:]),
			})
		}
		sort.Slice(facts, func(left, right int) bool { return facts[left].Key < facts[right].Key })
		state := thread.projector.Snapshot()
		document.Threads = append(document.Threads, persistedLedgerThread{
			Ownership:     thread.ownership,
			Writer:        thread.writer,
			Thread:        state,
			ThreadDigest:  StateDigest(state),
			ProviderFacts: facts,
		})
	}
	checksum, err := ledgerDocumentChecksum(document)
	if err != nil {
		return nil, fmt.Errorf("checksum canonical Chat ledger: %w", err)
	}
	document.Checksum = checksum
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode canonical Chat ledger: %w", err)
	}
	return append(raw, '\n'), nil
}

func decodeLedgerDocument(raw []byte) (map[ThreadID]*ledgerThread, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: state file is empty", ErrLedgerCorrupt)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document ledgerDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: decode state file: %v", ErrLedgerCorrupt, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%w: state file contains multiple JSON values", ErrLedgerCorrupt)
		}
		return nil, fmt.Errorf("%w: decode trailing state data: %v", ErrLedgerCorrupt, err)
	}
	if document.SchemaVersion != ledgerSchemaVersion {
		return nil, fmt.Errorf(
			"%w: found schema %d, support only %d",
			ErrLedgerSchema,
			document.SchemaVersion,
			ledgerSchemaVersion,
		)
	}
	if len(document.Checksum) != sha256.Size*2 {
		return nil, fmt.Errorf("%w: document checksum is missing or malformed", ErrLedgerCorrupt)
	}
	wantChecksum, err := ledgerDocumentChecksum(document)
	if err != nil {
		return nil, fmt.Errorf("%w: calculate document checksum: %v", ErrLedgerCorrupt, err)
	}
	if subtle.ConstantTimeCompare([]byte(document.Checksum), []byte(wantChecksum)) != 1 {
		return nil, fmt.Errorf("%w: document checksum mismatch", ErrLedgerCorrupt)
	}

	threads := make(map[ThreadID]*ledgerThread, len(document.Threads))
	previousThreadID := ThreadID("")
	for index, persisted := range document.Threads {
		threadID := persisted.Thread.ID
		if !present(string(threadID)) {
			return nil, fmt.Errorf("%w: Thread entry %d has an empty ID", ErrLedgerCorrupt, index)
		}
		if index > 0 && threadID <= previousThreadID {
			return nil, fmt.Errorf("%w: Thread entries are duplicated or not canonically ordered", ErrLedgerCorrupt)
		}
		previousThreadID = threadID
		if _, exists := threads[threadID]; exists {
			return nil, fmt.Errorf("%w: duplicate Thread %q", ErrLedgerCorrupt, threadID)
		}
		if persisted.Ownership != ThreadOwnershipV2 {
			return nil, fmt.Errorf(
				"%w: %w: persisted Thread %q is owned by %q",
				ErrLedgerCorrupt,
				ErrThreadOwnership,
				threadID,
				persisted.Ownership,
			)
		}
		actualDigest := StateDigest(persisted.Thread)
		if persisted.ThreadDigest != actualDigest {
			return nil, fmt.Errorf("%w: Thread %q digest mismatch", ErrLedgerCorrupt, threadID)
		}

		facts := make(map[ProviderFactKey][sha256.Size]byte, len(persisted.ProviderFacts))
		previousFactKey := ProviderFactKey("")
		for factIndex, fact := range persisted.ProviderFacts {
			if !present(string(fact.Key)) {
				return nil, fmt.Errorf("%w: Thread %q has an empty provider fact key", ErrLedgerCorrupt, threadID)
			}
			if factIndex > 0 && fact.Key <= previousFactKey {
				return nil, fmt.Errorf(
					"%w: Thread %q provider fact keys are duplicated or not canonically ordered",
					ErrLedgerCorrupt,
					threadID,
				)
			}
			previousFactKey = fact.Key
			decoded, decodeErr := hex.DecodeString(fact.Fingerprint)
			if decodeErr != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != fact.Fingerprint {
				return nil, fmt.Errorf(
					"%w: Thread %q provider fact %q has a malformed fingerprint",
					ErrLedgerCorrupt,
					threadID,
					fact.Key,
				)
			}
			var fingerprint [sha256.Size]byte
			copy(fingerprint[:], decoded)
			facts[fact.Key] = fingerprint
		}

		thread := &ledgerThread{
			ownership: persisted.Ownership,
			writer:    persisted.Writer,
			projector: &Projector{
				thread:               cloneThread(persisted.Thread),
				appliedProviderFacts: facts,
			},
		}
		if err := validateLedgerThread(threadID, thread); err != nil {
			return nil, fmt.Errorf("%w: persisted Thread %q: %w", ErrLedgerCorrupt, threadID, err)
		}
		threads[threadID] = thread
	}
	return threads, nil
}

func ledgerDocumentChecksum(document ledgerDocument) (string, error) {
	payload := struct {
		SchemaVersion int                     `json:"schema_version"`
		Threads       []persistedLedgerThread `json:"threads"`
	}{
		SchemaVersion: document.SchemaVersion,
		Threads:       document.Threads,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func validateLedgerThread(threadID ThreadID, thread *ledgerThread) error {
	if !present(string(threadID)) || thread == nil || thread.projector == nil {
		return fmt.Errorf("%w: incomplete durable Thread", ErrInvariant)
	}
	if thread.ownership != ThreadOwnershipV2 {
		return fmt.Errorf(
			"%w: %w: Thread %q has ownership %q",
			ErrInvariant,
			ErrThreadOwnership,
			threadID,
			thread.ownership,
		)
	}
	if thread.projector.thread.ID != threadID {
		return fmt.Errorf(
			"%w: durable key %q contains Thread %q",
			ErrInvariant,
			threadID,
			thread.projector.thread.ID,
		)
	}
	if !present(string(thread.writer.Epoch)) || thread.writer.NextSequence == 0 {
		return fmt.Errorf("%w: Thread %q has an invalid writer frontier", ErrInvariant, threadID)
	}
	if err := thread.projector.CheckInvariants(); err != nil {
		return err
	}

	expectedSequence := WriterSequence(1)
	previousAcceptedRevision := Revision(0)
	attemptOwners := make(map[DispatchAttemptID]SubmissionID)
	for _, submission := range thread.projector.thread.Submissions {
		if submission.DispatchAttempt != "" {
			if owner, exists := attemptOwners[submission.DispatchAttempt]; exists {
				return fmt.Errorf(
					"%w: dispatch attempt %q belongs to both Submissions %q and %q",
					ErrInvariant,
					submission.DispatchAttempt,
					owner,
					submission.ID,
				)
			}
			attemptOwners[submission.DispatchAttempt] = submission.ID
		}
		if submission.Origin != OriginApp {
			continue
		}
		if submission.WriterEpoch != thread.writer.Epoch {
			return fmt.Errorf(
				"%w: Submission %q writer epoch %q differs from active epoch %q",
				ErrInvariant,
				submission.ID,
				submission.WriterEpoch,
				thread.writer.Epoch,
			)
		}
		if submission.AcceptedAt == nil || submission.AcceptedAt.Location() != time.UTC {
			return fmt.Errorf(
				"%w: Submission %q has no canonical UTC acceptance time",
				ErrInvariant,
				submission.ID,
			)
		}
		if submission.WriterSequence != expectedSequence {
			return fmt.Errorf(
				"%w: Submission %q writer sequence is %d, want %d",
				ErrInvariant,
				submission.ID,
				submission.WriterSequence,
				expectedSequence,
			)
		}
		if submission.AcceptedRevision <= previousAcceptedRevision ||
			submission.AcceptedRevision > thread.projector.thread.Revision {
			return fmt.Errorf(
				"%w: Submission %q has invalid acceptance revision %d",
				ErrInvariant,
				submission.ID,
				submission.AcceptedRevision,
			)
		}
		if submission.Delivery != DeliveryQueued && submission.DispatchAttempt == "" {
			return fmt.Errorf(
				"%w: v2 App Submission %q crossed delivery state without a dispatch attempt",
				ErrInvariant,
				submission.ID,
			)
		}
		previousAcceptedRevision = submission.AcceptedRevision
		expectedSequence++
	}
	if expectedSequence != thread.writer.NextSequence {
		return fmt.Errorf(
			"%w: Thread %q writer frontier is %d, want %d",
			ErrInvariant,
			threadID,
			thread.writer.NextSequence,
			expectedSequence,
		)
	}
	return nil
}

func recoverUnknownDispatchAdmissions(
	threads map[ThreadID]*ledgerThread,
) (map[ThreadID]*ledgerThread, bool, error) {
	recovered := make(map[ThreadID]*ledgerThread, len(threads))
	changed := false
	for threadID, current := range threads {
		candidate := current.clone()
		threadChanged := false
		for _, submission := range candidate.projector.Snapshot().Submissions {
			if submission.Delivery != DeliveryDelivering {
				continue
			}
			result, err := candidate.projector.Apply(DeliveryAmbiguousFact{
				Key:          dispatchAmbiguousFactKey("recovery", submission.DispatchAttempt),
				SubmissionID: submission.ID,
				AttemptID:    submission.DispatchAttempt,
			})
			if err != nil {
				return nil, false, err
			}
			if !result.Changed {
				return nil, false, fmt.Errorf(
					"%w: recovered delivering Submission %q without a state change",
					ErrInvariant,
					submission.ID,
				)
			}
			threadChanged = true
		}
		if err := validateLedgerThread(threadID, candidate); err != nil {
			return nil, false, err
		}
		recovered[threadID] = candidate
		changed = changed || threadChanged
	}
	return recovered, changed, nil
}

func writeLedgerAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".zen-chatthread-v2-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("%w: open state directory after rename: %v", ErrDurabilityUncertain, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("%w: sync state directory after rename: %v", ErrDurabilityUncertain, err)
	}
	return nil
}

func writeLedgerInitial(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if _, err := file.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("%w: open state directory after initialization: %v", ErrDurabilityUncertain, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("%w: sync state directory after initialization: %v", ErrDurabilityUncertain, err)
	}
	return nil
}
