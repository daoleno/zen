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
	"sync"
)

const (
	shadowSchemaVersion = 1
	shadowStateFileName = "chatthread-shadow-v2.json"
)

type shadowRecordState struct {
	cursor      uint64
	fingerprint [sha256.Size]byte
}

type shadowThread struct {
	ownership    string
	sourceToken  string
	sourceCursor uint64
	projector    *Projector
	records      map[ProviderFactKey]shadowRecordState
	maxCursor    uint64
	diagnostics  ShadowDiagnostics
}

func (thread *shadowThread) clone() *shadowThread {
	if thread == nil {
		return nil
	}
	clone := &shadowThread{
		ownership:    thread.ownership,
		sourceToken:  thread.sourceToken,
		sourceCursor: thread.sourceCursor,
		projector:    cloneProjector(thread.projector),
		records:      make(map[ProviderFactKey]shadowRecordState, len(thread.records)),
		maxCursor:    thread.maxCursor,
		diagnostics:  cloneShadowDiagnostics(thread.diagnostics),
	}
	for key, record := range thread.records {
		clone.records[key] = record
	}
	return clone
}

// ShadowStore owns only the isolated diagnostic file. Its API has no
// AcceptAndDispatch, DispatchBoundary, executor, terminal, or provider-input
// method. A successfully initialized or opened handle owns the validated
// in-memory state until it is reopened. Only OpenShadowStore validates existing
// file contents, so an external change may be detected on the next open. A
// persistence failure permanently fails the handle closed.
type ShadowStore struct {
	mu          sync.Mutex
	path        string
	threads     map[ThreadID]*shadowThread
	atomicWrite atomicWriteFunc
	fatalErr    error
}

func (store *ShadowStore) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

// InitializeShadowStore explicitly creates an empty diagnostic store. It never
// creates, opens, repairs, or replaces chatthread-v2.json.
func InitializeShadowStore(root string) (*ShadowStore, error) {
	store, err := newShadowStoreHandle(root, true)
	if err != nil {
		return nil, err
	}
	raw, err := encodeShadowDocument(store.threads)
	if err != nil {
		return nil, err
	}
	if err := writeLedgerInitial(store.path, raw); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w", ErrShadowAlreadyInitialized)
		}
		return nil, fmt.Errorf("initialize Chat shadow diagnostics: %w", err)
	}
	return store, nil
}

// OpenShadowStore opens an existing diagnostic store. Missing, empty,
// malformed, unsupported, checksum-invalid, or invariant-invalid state fails
// closed. It never initializes a replacement.
func OpenShadowStore(root string) (*ShadowStore, error) {
	store, err := newShadowStoreHandle(root, false)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w", ErrShadowNotInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read diagnostic state", ErrShadowUnavailable)
	}
	if err := os.Chmod(store.path, 0o600); err != nil {
		return nil, fmt.Errorf("%w: secure diagnostic state", ErrShadowUnavailable)
	}
	threads, err := decodeShadowDocument(raw)
	if err != nil {
		return nil, err
	}
	store.threads = threads
	return store, nil
}

func newShadowStoreHandle(root string, createRoot bool) (*ShadowStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("%w: shadow root is required", ErrInvalidArgument)
	}
	root = filepath.Clean(root)
	if createRoot {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, fmt.Errorf("%w: create diagnostic root", ErrShadowUnavailable)
		}
	} else {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w", ErrShadowNotInitialized)
		}
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: diagnostic root unavailable", ErrShadowUnavailable)
		}
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: secure diagnostic root", ErrShadowUnavailable)
	}
	return &ShadowStore{
		path:        filepath.Join(root, shadowStateFileName),
		threads:     make(map[ThreadID]*shadowThread),
		atomicWrite: writeLedgerAtomic,
	}, nil
}

// ApplyShadowBatch is the sole mutation boundary exposed to provider shadow
// adapters. It applies content-free external admissions and closed ProviderFact
// values transactionally, then persists sanitized comparisons in the isolated
// diagnostic file.
func (store *ShadowStore) ApplyShadowBatch(batch ShadowBatch) (ShadowSnapshot, error) {
	if store == nil {
		return ShadowSnapshot{}, fmt.Errorf("%w: nil ShadowStore", ErrInvalidArgument)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.checkUsableLocked(); err != nil {
		return ShadowSnapshot{}, err
	}
	if !validShadowIdentifier(string(batch.ThreadID)) || !validDiagnosticToken(batch.SourceToken) {
		return ShadowSnapshot{}, fmt.Errorf("%w: shadow Thread and source token are required", ErrInvalidArgument)
	}

	current, exists := store.threads[batch.ThreadID]
	if exists && (current.ownership != ShadowOwnershipV1ReadOnly || current.sourceToken != batch.SourceToken) {
		return ShadowSnapshot{}, fmt.Errorf("%w: shadow Thread identity changed", ErrShadowOwnership)
	}
	var candidate *shadowThread
	if exists {
		candidate = current.clone()
	} else {
		projector, err := NewProjector(batch.ThreadID)
		if err != nil {
			return ShadowSnapshot{}, err
		}
		candidate = &shadowThread{
			ownership:   ShadowOwnershipV1ReadOnly,
			sourceToken: batch.SourceToken,
			projector:   projector,
			records:     make(map[ProviderFactKey]shadowRecordState),
		}
	}

	for _, record := range batch.Records {
		if err := applyShadowRecord(candidate, record); err != nil {
			return snapshotShadowThread(candidate), err
		}
	}
	if batch.SourceCursor < candidate.sourceCursor ||
		(candidate.maxCursor > 0 && batch.SourceCursor < candidate.maxCursor) {
		return snapshotShadowThread(candidate), fmt.Errorf("%w: shadow source cursor moved backward", ErrShadowRecordGap)
	}
	candidate.sourceCursor = batch.SourceCursor
	diagnostics, err := buildShadowDiagnostics(
		candidate.projector.Snapshot(),
		candidate.sourceToken,
		batch.Legacy,
		batch.CorrelationGaps,
	)
	if err != nil {
		return snapshotShadowThread(candidate), err
	}
	candidate.diagnostics = diagnostics
	if err := validateShadowThread(batch.ThreadID, candidate); err != nil {
		return snapshotShadowThread(candidate), err
	}

	if exists && shadowThreadsEqual(current, candidate) {
		return snapshotShadowThread(current), nil
	}
	threads := cloneShadowThreads(store.threads)
	threads[batch.ThreadID] = candidate
	if err := store.persistThreadsLocked(threads); err != nil {
		return snapshotShadowThread(candidate), err
	}
	store.threads = threads
	return snapshotShadowThread(candidate), nil
}

func (store *ShadowStore) ShadowSnapshot(threadID ThreadID) (ShadowSnapshot, error) {
	if store == nil {
		return ShadowSnapshot{}, fmt.Errorf("%w: nil ShadowStore", ErrInvalidArgument)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.checkUsableLocked(); err != nil {
		return ShadowSnapshot{}, err
	}
	thread, ok := store.threads[threadID]
	if !ok {
		return ShadowSnapshot{}, fmt.Errorf("%w: shadow Thread", ErrThreadNotFound)
	}
	return snapshotShadowThread(thread), nil
}

func (store *ShadowStore) checkUsableLocked() error {
	if store.fatalErr != nil {
		return fmt.Errorf("%w: %w", ErrShadowUnavailable, store.fatalErr)
	}
	return nil
}

func applyShadowRecord(thread *shadowThread, record ShadowRecord) error {
	if thread == nil || thread.projector == nil || !validShadowIdentifier(string(record.Key)) || record.Cursor == 0 ||
		!validStructuralFingerprint(record.Fingerprint) {
		return fmt.Errorf("%w: malformed shadow record", ErrInvalidArgument)
	}
	fingerprintBytes, _ := hex.DecodeString(record.Fingerprint)
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], fingerprintBytes)
	if existing, ok := thread.records[record.Key]; ok {
		if existing.cursor != record.Cursor || existing.fingerprint != fingerprint {
			return fmt.Errorf("%w", ErrShadowRecordConflict)
		}
		return nil
	}
	if len(thread.records) > 0 && record.Cursor <= thread.maxCursor {
		return fmt.Errorf("%w", ErrShadowRecordGap)
	}

	providerFacts := 0
	var externalSubmission SubmissionID
	var admittedSubmission SubmissionID
	for _, operation := range record.Operations {
		switch operation := operation.(type) {
		case ProviderExternalSubmissionObserved:
			if externalSubmission != "" || !validShadowIdentifier(string(operation.SubmissionID)) {
				return fmt.Errorf("%w: empty provider-external Submission", ErrInvalidArgument)
			}
			externalSubmission = operation.SubmissionID
			if _, err := thread.projector.Accept(AcceptSubmissionCommand{
				SubmissionID: operation.SubmissionID,
				Origin:       OriginProviderExternal,
				Payload:      SubmissionPayload{AttachmentIDs: []string{}},
			}); err != nil {
				return err
			}
		case ProviderFactObserved:
			providerFacts++
			if providerFacts > 1 || operation.Fact == nil {
				return fmt.Errorf("%w: nil shadow ProviderFact", ErrInvalidArgument)
			}
			factKey, inputSubmission, err := validateShadowProviderFact(operation.Fact)
			if err != nil {
				return err
			}
			if factKey != record.Key {
				return fmt.Errorf("%w: shadow fact key differs from its record key", ErrInvalidArgument)
			}
			admittedSubmission = inputSubmission
			if _, err := thread.projector.Apply(operation.Fact); err != nil {
				return err
			}
		case nil:
			return fmt.Errorf("%w: nil shadow operation", ErrInvalidArgument)
		default:
			return fmt.Errorf("%w: unsupported shadow operation %T", ErrInvalidArgument, operation)
		}
	}
	if externalSubmission != admittedSubmission {
		return fmt.Errorf("%w: provider-external admission is not paired to its input fact", ErrInvalidArgument)
	}
	thread.records[record.Key] = shadowRecordState{cursor: record.Cursor, fingerprint: fingerprint}
	thread.maxCursor = record.Cursor
	return nil
}

func validateShadowProviderFact(fact ProviderFact) (ProviderFactKey, SubmissionID, error) {
	invalid := func() (ProviderFactKey, SubmissionID, error) {
		return "", "", fmt.Errorf("%w: unsanitized or unsupported shadow ProviderFact", ErrInvalidArgument)
	}
	switch fact := fact.(type) {
	case ActivityStartedFact:
		if !validShadowIdentifier(string(fact.Key)) || !validShadowIdentifier(string(fact.ExecutionID)) {
			return invalid()
		}
		return fact.Key, "", nil
	case *ActivityStartedFact:
		if fact == nil {
			return invalid()
		}
		return validateShadowProviderFact(*fact)
	case InputAdmittedFact:
		if !validShadowIdentifier(string(fact.Key)) || !validShadowIdentifier(string(fact.ExecutionID)) ||
			!validShadowIdentifier(string(fact.SubmissionID)) || fact.Ordinal == 0 {
			return invalid()
		}
		return fact.Key, fact.SubmissionID, nil
	case *InputAdmittedFact:
		if fact == nil {
			return invalid()
		}
		return validateShadowProviderFact(*fact)
	case EventUpsertFact:
		if !validShadowIdentifier(string(fact.Key)) || !validShadowIdentifier(string(fact.EventID)) ||
			!validShadowIdentifier(string(fact.ExecutionID)) || !validShadowIdentifier(string(fact.CausalSubmissionID)) ||
			fact.Payload != "" {
			return invalid()
		}
		return fact.Key, "", nil
	case *EventUpsertFact:
		if fact == nil {
			return invalid()
		}
		return validateShadowProviderFact(*fact)
	case ActivityTerminalFact:
		if !validShadowIdentifier(string(fact.Key)) || !validShadowIdentifier(string(fact.ExecutionID)) || fact.Reason != "" {
			return invalid()
		}
		return fact.Key, "", nil
	case *ActivityTerminalFact:
		if fact == nil {
			return invalid()
		}
		return validateShadowProviderFact(*fact)
	default:
		// In particular, a shadow path has no delivery attempt to mark ambiguous.
		return invalid()
	}
}

func snapshotShadowThread(thread *shadowThread) ShadowSnapshot {
	if thread == nil || thread.projector == nil {
		return ShadowSnapshot{}
	}
	factKeys := make([]ProviderFactKey, 0, len(thread.projector.appliedProviderFacts))
	for key := range thread.projector.appliedProviderFacts {
		factKeys = append(factKeys, key)
	}
	sort.Slice(factKeys, func(left, right int) bool { return factKeys[left] < factKeys[right] })
	records := make([]AppliedShadowRecord, 0, len(thread.records))
	for key, record := range thread.records {
		records = append(records, AppliedShadowRecord{
			Key:         key,
			Cursor:      record.cursor,
			Fingerprint: hex.EncodeToString(record.fingerprint[:]),
		})
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].Cursor != records[right].Cursor {
			return records[left].Cursor < records[right].Cursor
		}
		return records[left].Key < records[right].Key
	})
	state := thread.projector.Snapshot()
	return ShadowSnapshot{
		Ownership:        thread.ownership,
		SourceToken:      thread.sourceToken,
		SourceCursor:     thread.sourceCursor,
		Thread:           state,
		Digest:           StateDigest(state),
		ProviderFactKeys: factKeys,
		AppliedRecords:   records,
		Diagnostics:      cloneShadowDiagnostics(thread.diagnostics),
	}
}

func cloneShadowThreads(threads map[ThreadID]*shadowThread) map[ThreadID]*shadowThread {
	clone := make(map[ThreadID]*shadowThread, len(threads))
	for threadID, thread := range threads {
		clone[threadID] = thread.clone()
	}
	return clone
}

func shadowThreadsEqual(left, right *shadowThread) bool {
	leftRaw, leftErr := json.Marshal(snapshotShadowThread(left))
	rightRaw, rightErr := json.Marshal(snapshotShadowThread(right))
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func cloneShadowDiagnostics(diagnostics ShadowDiagnostics) ShadowDiagnostics {
	clone := diagnostics
	clone.Cardinality = cloneShadowComparison(diagnostics.Cardinality)
	clone.Chronology = cloneShadowComparison(diagnostics.Chronology)
	clone.CurrentActivity = cloneShadowComparison(diagnostics.CurrentActivity)
	clone.Queue = cloneShadowComparison(diagnostics.Queue)
	clone.TerminalSettlement = cloneShadowComparison(diagnostics.TerminalSettlement)
	clone.CorrelationGaps = append([]SanitizedCorrelationGap{}, diagnostics.CorrelationGaps...)
	return clone
}

func cloneShadowComparison(comparison ShadowComparison) ShadowComparison {
	clone := comparison
	clone.LegacyIDs = cloneStrings(comparison.LegacyIDs)
	clone.ShadowIDs = cloneStrings(comparison.ShadowIDs)
	return clone
}

type shadowDocument struct {
	SchemaVersion int                     `json:"schema_version"`
	Threads       []persistedShadowThread `json:"threads"`
	Checksum      string                  `json:"checksum"`
}

type persistedShadowThread struct {
	Ownership     string                  `json:"ownership"`
	SourceToken   string                  `json:"source_token"`
	SourceCursor  uint64                  `json:"source_cursor"`
	Thread        Thread                  `json:"thread"`
	ThreadDigest  string                  `json:"thread_digest"`
	ProviderFacts []persistedProviderFact `json:"provider_facts"`
	Records       []persistedShadowRecord `json:"records"`
	MaxCursor     uint64                  `json:"max_cursor"`
	Diagnostics   ShadowDiagnostics       `json:"diagnostics"`
}

type persistedShadowRecord struct {
	Key         ProviderFactKey `json:"key"`
	Cursor      uint64          `json:"cursor"`
	Fingerprint string          `json:"structural_sha256"`
}

func (store *ShadowStore) persistThreadsLocked(threads map[ThreadID]*shadowThread) error {
	raw, err := encodeShadowDocument(threads)
	if err != nil {
		return err
	}
	if err := store.atomicWrite(store.path, raw); err != nil {
		wrapped := fmt.Errorf("%w: persist diagnostic state", ErrShadowUnavailable)
		store.fatalErr = wrapped
		return wrapped
	}
	return nil
}

func encodeShadowDocument(threads map[ThreadID]*shadowThread) ([]byte, error) {
	threadIDs := make([]ThreadID, 0, len(threads))
	for threadID := range threads {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Slice(threadIDs, func(left, right int) bool { return threadIDs[left] < threadIDs[right] })

	document := shadowDocument{
		SchemaVersion: shadowSchemaVersion,
		Threads:       make([]persistedShadowThread, 0, len(threadIDs)),
	}
	for _, threadID := range threadIDs {
		thread := threads[threadID]
		if err := validateShadowThread(threadID, thread); err != nil {
			return nil, err
		}
		providerFacts := make([]persistedProviderFact, 0, len(thread.projector.appliedProviderFacts))
		for key, fingerprint := range thread.projector.appliedProviderFacts {
			providerFacts = append(providerFacts, persistedProviderFact{
				Key:         key,
				Fingerprint: hex.EncodeToString(fingerprint[:]),
			})
		}
		sort.Slice(providerFacts, func(left, right int) bool { return providerFacts[left].Key < providerFacts[right].Key })

		records := make([]persistedShadowRecord, 0, len(thread.records))
		for key, record := range thread.records {
			records = append(records, persistedShadowRecord{
				Key:         key,
				Cursor:      record.cursor,
				Fingerprint: hex.EncodeToString(record.fingerprint[:]),
			})
		}
		sort.Slice(records, func(left, right int) bool {
			if records[left].Cursor != records[right].Cursor {
				return records[left].Cursor < records[right].Cursor
			}
			return records[left].Key < records[right].Key
		})
		state := thread.projector.Snapshot()
		document.Threads = append(document.Threads, persistedShadowThread{
			Ownership:     thread.ownership,
			SourceToken:   thread.sourceToken,
			SourceCursor:  thread.sourceCursor,
			Thread:        state,
			ThreadDigest:  StateDigest(state),
			ProviderFacts: providerFacts,
			Records:       records,
			MaxCursor:     thread.maxCursor,
			Diagnostics:   cloneShadowDiagnostics(thread.diagnostics),
		})
	}
	checksum, err := shadowDocumentChecksum(document)
	if err != nil {
		return nil, fmt.Errorf("checksum Chat shadow diagnostics: %w", err)
	}
	document.Checksum = checksum
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Chat shadow diagnostics: %w", err)
	}
	return append(raw, '\n'), nil
}

func decodeShadowDocument(raw []byte) (map[ThreadID]*shadowThread, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: state is empty", ErrShadowCorrupt)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document shadowDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: decode diagnostic state", ErrShadowCorrupt)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing diagnostic state", ErrShadowCorrupt)
	}
	if document.SchemaVersion != shadowSchemaVersion {
		return nil, fmt.Errorf("%w", ErrShadowSchema)
	}
	if !validStructuralFingerprint(document.Checksum) {
		return nil, fmt.Errorf("%w: malformed document checksum", ErrShadowCorrupt)
	}
	wantChecksum, err := shadowDocumentChecksum(document)
	if err != nil {
		return nil, fmt.Errorf("%w: calculate document checksum", ErrShadowCorrupt)
	}
	if subtle.ConstantTimeCompare([]byte(document.Checksum), []byte(wantChecksum)) != 1 {
		return nil, fmt.Errorf("%w: document checksum mismatch", ErrShadowCorrupt)
	}

	threads := make(map[ThreadID]*shadowThread, len(document.Threads))
	previousThreadID := ThreadID("")
	for index, persisted := range document.Threads {
		threadID := persisted.Thread.ID
		if !validShadowIdentifier(string(threadID)) || (index > 0 && threadID <= previousThreadID) {
			return nil, fmt.Errorf("%w: invalid Thread ordering", ErrShadowCorrupt)
		}
		previousThreadID = threadID
		if persisted.Ownership != ShadowOwnershipV1ReadOnly || !validDiagnosticToken(persisted.SourceToken) {
			return nil, fmt.Errorf("%w: %w", ErrShadowCorrupt, ErrShadowOwnership)
		}
		if persisted.ThreadDigest != StateDigest(persisted.Thread) {
			return nil, fmt.Errorf("%w: Thread digest mismatch", ErrShadowCorrupt)
		}

		providerFacts, err := decodeShadowProviderFacts(persisted.ProviderFacts)
		if err != nil {
			return nil, err
		}
		records, maxCursor, err := decodeShadowRecords(persisted.Records)
		if err != nil {
			return nil, err
		}
		if maxCursor != persisted.MaxCursor {
			return nil, fmt.Errorf("%w: record cursor mismatch", ErrShadowCorrupt)
		}
		thread := &shadowThread{
			ownership:    persisted.Ownership,
			sourceToken:  persisted.SourceToken,
			sourceCursor: persisted.SourceCursor,
			projector: &Projector{
				thread:               cloneThread(persisted.Thread),
				appliedProviderFacts: providerFacts,
			},
			records:     records,
			maxCursor:   persisted.MaxCursor,
			diagnostics: cloneShadowDiagnostics(persisted.Diagnostics),
		}
		if err := validateShadowThread(threadID, thread); err != nil {
			return nil, fmt.Errorf("%w: persisted shadow Thread invalid", ErrShadowCorrupt)
		}
		threads[threadID] = thread
	}
	return threads, nil
}

func decodeShadowProviderFacts(persisted []persistedProviderFact) (map[ProviderFactKey][sha256.Size]byte, error) {
	facts := make(map[ProviderFactKey][sha256.Size]byte, len(persisted))
	previousKey := ProviderFactKey("")
	for index, fact := range persisted {
		if !validShadowIdentifier(string(fact.Key)) || (index > 0 && fact.Key <= previousKey) ||
			!validStructuralFingerprint(fact.Fingerprint) {
			return nil, fmt.Errorf("%w: invalid provider fact index", ErrShadowCorrupt)
		}
		previousKey = fact.Key
		decoded, _ := hex.DecodeString(fact.Fingerprint)
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], decoded)
		facts[fact.Key] = fingerprint
	}
	return facts, nil
}

func decodeShadowRecords(persisted []persistedShadowRecord) (map[ProviderFactKey]shadowRecordState, uint64, error) {
	records := make(map[ProviderFactKey]shadowRecordState, len(persisted))
	var previousCursor uint64
	previousKey := ProviderFactKey("")
	for index, record := range persisted {
		if !validShadowIdentifier(string(record.Key)) || record.Cursor == 0 || !validStructuralFingerprint(record.Fingerprint) {
			return nil, 0, fmt.Errorf("%w: malformed shadow record", ErrShadowCorrupt)
		}
		if index > 0 && (record.Cursor < previousCursor || (record.Cursor == previousCursor && record.Key <= previousKey)) {
			return nil, 0, fmt.Errorf("%w: shadow records are not ordered", ErrShadowCorrupt)
		}
		if _, duplicate := records[record.Key]; duplicate {
			return nil, 0, fmt.Errorf("%w: duplicate shadow record", ErrShadowCorrupt)
		}
		decoded, _ := hex.DecodeString(record.Fingerprint)
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], decoded)
		records[record.Key] = shadowRecordState{cursor: record.Cursor, fingerprint: fingerprint}
		previousCursor = record.Cursor
		previousKey = record.Key
	}
	return records, previousCursor, nil
}

func shadowDocumentChecksum(document shadowDocument) (string, error) {
	payload := struct {
		SchemaVersion int                     `json:"schema_version"`
		Threads       []persistedShadowThread `json:"threads"`
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

func validateShadowThread(threadID ThreadID, thread *shadowThread) error {
	if !validShadowIdentifier(string(threadID)) || thread == nil || thread.projector == nil || thread.records == nil {
		return fmt.Errorf("%w: incomplete shadow Thread", ErrInvariant)
	}
	if thread.ownership != ShadowOwnershipV1ReadOnly || !validDiagnosticToken(thread.sourceToken) {
		return fmt.Errorf("%w: %w", ErrInvariant, ErrShadowOwnership)
	}
	if thread.projector.thread.ID != threadID {
		return fmt.Errorf("%w: shadow Thread key mismatch", ErrInvariant)
	}
	if err := thread.projector.CheckInvariants(); err != nil {
		return err
	}
	if err := validateSanitizedShadowProjection(thread.projector.thread); err != nil {
		return err
	}
	var maxCursor uint64
	for key, record := range thread.records {
		if !validShadowIdentifier(string(key)) || record.cursor == 0 {
			return fmt.Errorf("%w: invalid shadow record", ErrInvariant)
		}
		if record.cursor > maxCursor {
			maxCursor = record.cursor
		}
	}
	if maxCursor != thread.maxCursor {
		return fmt.Errorf("%w: shadow max cursor mismatch", ErrInvariant)
	}
	if thread.maxCursor > 0 && thread.sourceCursor < thread.maxCursor {
		return fmt.Errorf("%w: shadow source cursor precedes its records", ErrInvariant)
	}
	if thread.diagnostics.ThreadToken == "" {
		if len(thread.records) != 0 || thread.projector.thread.Revision != 0 {
			return fmt.Errorf("%w: nonempty shadow Thread has no diagnostics", ErrInvariant)
		}
		return nil
	}
	if err := validateShadowDiagnostics(thread.diagnostics); err != nil {
		return err
	}
	if thread.diagnostics.SourceToken != thread.sourceToken ||
		thread.diagnostics.ThreadToken != diagnosticToken("shadow-thread", string(threadID)) ||
		thread.diagnostics.CanonicalRevision != thread.projector.thread.Revision ||
		thread.diagnostics.CanonicalDigest != thread.projector.Digest() {
		return fmt.Errorf("%w: shadow diagnostics do not describe the canonical snapshot", ErrInvariant)
	}
	expectedGaps := make(map[string]struct{})
	for _, submission := range thread.projector.thread.Submissions {
		if submission.Origin != OriginProviderExternal || submission.AdmissionFactKey == "" {
			continue
		}
		key := diagnosticToken("gap-submission", string(submission.ID)) + "\x00" +
			diagnosticToken("gap-record", string(submission.AdmissionFactKey))
		expectedGaps[key] = struct{}{}
	}
	for _, gap := range thread.diagnostics.CorrelationGaps {
		if gap.Reason != CorrelationGapNoExplicitAppBinding {
			continue
		}
		delete(expectedGaps, gap.SubmissionToken+"\x00"+gap.RecordToken)
	}
	if len(expectedGaps) != 0 {
		return fmt.Errorf("%w: provider-external Submission lacks an explicit correlation gap", ErrInvariant)
	}
	return nil
}

func validShadowIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '_', character == '.', character == ':':
		default:
			return false
		}
	}
	return true
}

func validateSanitizedShadowProjection(thread Thread) error {
	if !validShadowIdentifier(string(thread.ID)) {
		return fmt.Errorf("%w: unsanitized shadow Thread ID", ErrInvariant)
	}
	for _, queuedID := range thread.QueuedSubmissionIDs {
		if !validShadowIdentifier(string(queuedID)) {
			return fmt.Errorf("%w: unsanitized shadow queue ID", ErrInvariant)
		}
	}
	for _, submission := range thread.Submissions {
		if !validShadowIdentifier(string(submission.ID)) || submission.Origin != OriginProviderExternal ||
			submission.Payload.Body != "" || len(submission.Payload.AttachmentIDs) != 0 ||
			submission.WriterEpoch != "" || submission.WriterSequence != 0 || submission.AcceptedAt != nil ||
			submission.DispatchAttempt != "" ||
			(submission.ExecutionID != "" && !validShadowIdentifier(string(submission.ExecutionID))) ||
			(submission.AdmissionFactKey != "" && !validShadowIdentifier(string(submission.AdmissionFactKey))) {
			return fmt.Errorf("%w: unsanitized shadow Submission", ErrInvariant)
		}
	}
	for _, activity := range thread.ExecutionActivities {
		if !validShadowIdentifier(string(activity.ID)) || !validShadowIdentifier(string(activity.StartFactKey)) ||
			(activity.TerminalFactKey != "" && !validShadowIdentifier(string(activity.TerminalFactKey))) ||
			activity.TerminalReason != "" {
			return fmt.Errorf("%w: unsanitized shadow ExecutionActivity", ErrInvariant)
		}
	}
	for _, event := range thread.Events {
		if !validShadowIdentifier(string(event.ID)) || !validShadowIdentifier(string(event.ExecutionID)) ||
			!validShadowIdentifier(string(event.CausalSubmissionID)) || event.Payload != "" {
			return fmt.Errorf("%w: unsanitized shadow ThreadEvent", ErrInvariant)
		}
	}
	return nil
}
