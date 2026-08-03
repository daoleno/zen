package watcher

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const codexTransactionSchemaVersion = 1
const codexTerminalTransactionRetention = 7 * 24 * time.Hour
const codexUnreferencedEnvelopeGrace = 24 * time.Hour

var codexShellSnapshotNameRe = regexp.MustCompile(
	`^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.([0-9]{16,19})\.sh$`,
)
var errCodexRolloutSessionIdentityPending = errors.New("target Codex rollout has no session identity yet")

type codexTransactionPhase string

const (
	codexTransactionPrepared          codexTransactionPhase = "prepared"
	codexTransactionDraftAcknowledged codexTransactionPhase = "draft_acknowledged"
	codexTransactionEnterPending      codexTransactionPhase = "enter_pending"
	codexTransactionAmbiguous         codexTransactionPhase = "ambiguous"
	codexTransactionConfirmed         codexTransactionPhase = "confirmed"
	codexTransactionNotSubmitted      codexTransactionPhase = "not_submitted"
	codexTransactionConflict          codexTransactionPhase = "conflict"
)

type codexTransactionRecord struct {
	SchemaVersion     int                   `json:"schema_version"`
	TransactionID     string                `json:"transaction_id"`
	SessionID         string                `json:"session_id"`
	SessionGeneration string                `json:"session_generation"`
	AcceptanceReceipt string                `json:"acceptance_receipt,omitempty"`
	Action            string                `json:"action"`
	Phase             codexTransactionPhase `json:"phase"`
	PayloadSHA256     string                `json:"payload_sha256"`
	Instruction       string                `json:"instruction"`
	InstructionSHA256 string                `json:"instruction_sha256"`
	EnvelopePath      string                `json:"envelope_path,omitempty"`
	RolloutPath       string                `json:"rollout_path"`
	RolloutSessionID  string                `json:"rollout_session_id"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
	Detail            string                `json:"detail,omitempty"`
}

func (record codexTransactionRecord) active() bool {
	switch record.Phase {
	case codexTransactionPrepared,
		codexTransactionDraftAcknowledged,
		codexTransactionEnterPending,
		codexTransactionAmbiguous:
		return true
	default:
		return false
	}
}

type codexTransactionStore interface {
	Save(codexTransactionRecord) error
	Active(sessionID, generation string) ([]codexTransactionRecord, error)
	Receipt(sessionID, generation, receipt string) (codexTransactionRecord, bool, error)
	WriteEnvelope(payloadSHA256 string, payload []byte) (string, error)
	Maintain(time.Time) error
}

type memoryCodexTransactionStore struct {
	mu      sync.Mutex
	records map[string]codexTransactionRecord
}

func newMemoryCodexTransactionStore() *memoryCodexTransactionStore {
	return &memoryCodexTransactionStore{records: make(map[string]codexTransactionRecord)}
}

func (store *memoryCodexTransactionStore) Save(record codexTransactionRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.records == nil {
		store.records = make(map[string]codexTransactionRecord)
	}
	store.records[record.TransactionID] = record
	return nil
}

func (store *memoryCodexTransactionStore) Active(sessionID, generation string) ([]codexTransactionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var records []codexTransactionRecord
	for _, record := range store.records {
		if record.SessionID == sessionID && record.SessionGeneration == generation && record.active() {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, nil
}

func (store *memoryCodexTransactionStore) Receipt(
	sessionID, generation, receipt string,
) (codexTransactionRecord, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, record := range store.records {
		if record.SessionID == sessionID &&
			record.SessionGeneration == generation &&
			record.AcceptanceReceipt == receipt {
			return record, true, nil
		}
	}
	return codexTransactionRecord{}, false, nil
}

func (*memoryCodexTransactionStore) WriteEnvelope(string, []byte) (string, error) { return "", nil }

func (*memoryCodexTransactionStore) Maintain(time.Time) error { return nil }

type fileCodexTransactionStore struct {
	mu             sync.Mutex
	root           string
	transactionDir string
	envelopeDir    string
}

func newFileCodexTransactionStore(stateDir string) (*fileCodexTransactionStore, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, fmt.Errorf("Zen state directory is required for durable Codex input")
	}
	root := filepath.Join(stateDir, "codex-input")
	store := &fileCodexTransactionStore{
		root:           root,
		transactionDir: filepath.Join(root, "transactions"),
		envelopeDir:    filepath.Join(root, "envelopes"),
	}
	for _, dir := range []string{root, store.transactionDir, store.envelopeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create Codex input state directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("protect Codex input state directory: %w", err)
		}
	}
	return store, nil
}

func (store *fileCodexTransactionStore) Save(record codexTransactionRecord) error {
	if store == nil {
		return fmt.Errorf("Codex input transaction store is unavailable")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if record.TransactionID == "" {
		return fmt.Errorf("Codex input transaction ID is required")
	}
	if record.EnvelopePath != "" {
		envelope := store.validEnvelopePath(record.EnvelopePath)
		if envelope == "" {
			return fmt.Errorf("Codex input transaction references an invalid envelope path")
		}
		raw, err := os.ReadFile(envelope)
		if err != nil {
			return fmt.Errorf("read referenced Codex input envelope: %w", err)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != record.PayloadSHA256 {
			return fmt.Errorf("referenced Codex input envelope does not match transaction payload")
		}
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Codex input transaction: %w", err)
	}
	raw = append(raw, '\n')
	path := store.recordPath(record)
	if err := writeCodexAtomicFile(path, raw); err != nil {
		return fmt.Errorf("persist Codex input transaction: %w", err)
	}
	return nil
}

func (store *fileCodexTransactionStore) Active(sessionID, generation string) ([]codexTransactionRecord, error) {
	if store == nil {
		return nil, fmt.Errorf("Codex input transaction store is unavailable")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.maintainLocked(time.Now().UTC()); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.transactionDir)
	if err != nil {
		return nil, fmt.Errorf("read Codex input transactions: %w", err)
	}
	var records []codexTransactionRecord
	scopePrefix := codexTransactionScope(sessionID, generation) + "-"
	for _, entry := range entries {
		if entry.IsDir() ||
			!strings.HasPrefix(entry.Name(), scopePrefix) ||
			!strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(store.transactionDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read Codex input transaction %s: %w", entry.Name(), err)
		}
		var record codexTransactionRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("decode Codex input transaction %s: %w", entry.Name(), err)
		}
		if record.SchemaVersion != codexTransactionSchemaVersion {
			return nil, fmt.Errorf("unsupported Codex input transaction schema %d in %s", record.SchemaVersion, entry.Name())
		}
		if record.SessionID == sessionID && record.SessionGeneration == generation && record.active() {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, nil
}

func (store *fileCodexTransactionStore) Receipt(
	sessionID, generation, receipt string,
) (codexTransactionRecord, bool, error) {
	if store == nil {
		return codexTransactionRecord{}, false, fmt.Errorf("Codex input transaction store is unavailable")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.maintainLocked(time.Now().UTC()); err != nil {
		return codexTransactionRecord{}, false, err
	}
	entries, err := os.ReadDir(store.transactionDir)
	if err != nil {
		return codexTransactionRecord{}, false, fmt.Errorf("read Codex input transactions: %w", err)
	}
	scopePrefix := codexTransactionScope(sessionID, generation) + "-"
	for _, entry := range entries {
		if entry.IsDir() ||
			!strings.HasPrefix(entry.Name(), scopePrefix) ||
			!strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(store.transactionDir, entry.Name()))
		if readErr != nil {
			return codexTransactionRecord{}, false, fmt.Errorf(
				"read Codex input transaction %s: %w",
				entry.Name(),
				readErr,
			)
		}
		var record codexTransactionRecord
		if decodeErr := json.Unmarshal(raw, &record); decodeErr != nil {
			return codexTransactionRecord{}, false, fmt.Errorf(
				"decode Codex input transaction %s: %w",
				entry.Name(),
				decodeErr,
			)
		}
		if record.SchemaVersion != codexTransactionSchemaVersion {
			return codexTransactionRecord{}, false, fmt.Errorf(
				"unsupported Codex input transaction schema %d in %s",
				record.SchemaVersion,
				entry.Name(),
			)
		}
		if record.SessionID == sessionID &&
			record.SessionGeneration == generation &&
			record.AcceptanceReceipt == receipt {
			return record, true, nil
		}
	}
	return codexTransactionRecord{}, false, nil
}

func (store *fileCodexTransactionStore) Maintain(now time.Time) error {
	if store == nil {
		return fmt.Errorf("Codex input transaction store is unavailable")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.maintainLocked(now)
}

func (store *fileCodexTransactionStore) maintainLocked(now time.Time) error {
	entries, err := os.ReadDir(store.transactionDir)
	if err != nil {
		return fmt.Errorf("read Codex input transactions for retention: %w", err)
	}
	temporaryCutoff := now.UTC().Add(-codexUnreferencedEnvelopeGrace)
	temporaryChanged, err := expireCodexAtomicTemps(store.transactionDir, entries, temporaryCutoff)
	if err != nil {
		return err
	}
	cutoff := now.UTC().Add(-codexTerminalTransactionRetention)
	type expiredRecord struct {
		path string
	}
	var expired []expiredRecord
	retainedEnvelopes := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(store.transactionDir, entry.Name())
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var record codexTransactionRecord
		if json.Unmarshal(raw, &record) != nil || record.SchemaVersion != codexTransactionSchemaVersion {
			// A corrupt or future record stays isolated for operator inspection.
			// Its scoped filename prevents it from blocking another Session.
			continue
		}
		updatedAt := record.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = record.CreatedAt
		}
		if !record.active() && !updatedAt.IsZero() && updatedAt.Before(cutoff) {
			expired = append(expired, expiredRecord{path: path})
			continue
		}
		if envelope := store.validEnvelopePath(record.EnvelopePath); envelope != "" {
			retainedEnvelopes[envelope] = struct{}{}
		}
	}
	changed := temporaryChanged
	for _, record := range expired {
		if err := os.Remove(record.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("expire Codex input transaction: %w", err)
		}
		changed = true
	}
	envelopes, err := os.ReadDir(store.envelopeDir)
	if err != nil {
		return fmt.Errorf("read Codex input envelopes for retention: %w", err)
	}
	temporaryChanged, err = expireCodexAtomicTemps(store.envelopeDir, envelopes, temporaryCutoff)
	if err != nil {
		return err
	}
	changed = changed || temporaryChanged
	orphanCutoff := now.UTC().Add(-codexUnreferencedEnvelopeGrace)
	for _, entry := range envelopes {
		if entry.IsDir() || !codexContentAddressedEnvelopeName(entry.Name()) {
			continue
		}
		envelope := filepath.Join(store.envelopeDir, entry.Name())
		if _, retained := retainedEnvelopes[envelope]; retained {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect Codex input envelope for retention: %w", infoErr)
		}
		if !info.ModTime().Before(orphanCutoff) {
			continue
		}
		if err := os.Remove(envelope); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("expire Codex input envelope: %w", err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return syncCodexDirectory(store.transactionDir, store.envelopeDir)
}

func expireCodexAtomicTemps(directory string, entries []os.DirEntry, cutoff time.Time) (bool, error) {
	changed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".codex-input-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return false, fmt.Errorf("inspect Codex input atomic temporary file: %w", err)
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("expire Codex input atomic temporary file: %w", err)
		}
		changed = true
	}
	return changed, nil
}

func (store *fileCodexTransactionStore) recordPath(record codexTransactionRecord) string {
	return filepath.Join(
		store.transactionDir,
		codexTransactionScope(record.SessionID, record.SessionGeneration)+"-"+record.TransactionID+".json",
	)
}

func (store *fileCodexTransactionStore) validEnvelopePath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || filepath.Dir(path) != filepath.Clean(store.envelopeDir) {
		return ""
	}
	return path
}

func codexContentAddressedEnvelopeName(name string) bool {
	if len(name) != sha256.Size*2 || strings.ToLower(name) != name {
		return false
	}
	decoded, err := hex.DecodeString(name)
	return err == nil && len(decoded) == sha256.Size
}

func codexTransactionScope(sessionID, generation string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + generation))
	return hex.EncodeToString(digest[:16])
}

func (store *fileCodexTransactionStore) WriteEnvelope(payloadSHA256 string, payload []byte) (string, error) {
	if store == nil {
		return "", fmt.Errorf("Codex input envelope store is unavailable")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(payloadSHA256) != sha256.Size*2 {
		return "", fmt.Errorf("invalid Codex input envelope SHA-256")
	}
	path := filepath.Join(store.envelopeDir, payloadSHA256)
	if existing, err := os.ReadFile(path); err == nil {
		digest := sha256.Sum256(existing)
		if hex.EncodeToString(digest[:]) != payloadSHA256 || !bytes.Equal(existing, payload) {
			return "", fmt.Errorf("Codex input envelope digest collision at %s", path)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("protect existing Codex input envelope: %w", err)
		}
		now := time.Now()
		if err := os.Chtimes(path, now, now); err != nil {
			return "", fmt.Errorf("refresh existing Codex input envelope grace: %w", err)
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read Codex input envelope: %w", err)
	}
	if err := writeCodexAtomicFile(path, payload); err != nil {
		return "", fmt.Errorf("persist Codex input envelope: %w", err)
	}
	return path, nil
}

func writeCodexAtomicFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".codex-input-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncCodexDirectory(paths ...string) error {
	for _, path := range paths {
		directory, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func newCodexTransactionID() (string, error) {
	raw := make([]byte, 10)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate Codex transaction ID: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func codexSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func codexRolloutContainsExactUserMessage(
	rollout codexRolloutIdentity,
	instruction string,
	notBefore time.Time,
) (bool, error) {
	if !rollout.valid() {
		return false, fmt.Errorf("target Codex rollout identity is unavailable")
	}
	path := strings.TrimSpace(rollout.Path)
	var err error
	if path == "" {
		path, err = codexRolloutPathForSession(rollout.SessionID)
		if err != nil {
			return false, err
		}
		if path == "" {
			return false, nil
		}
	}
	sessionID, err := codexRolloutSessionID(path)
	if err != nil {
		if errors.Is(err, errCodexRolloutSessionIdentityPending) {
			return false, nil
		}
		return false, err
	}
	if sessionID != rollout.SessionID {
		return false, fmt.Errorf("target Codex rollout identity changed")
	}
	return codexRolloutFileContainsExactUserMessage(path, instruction, notBefore)
}

func codexRolloutFileContainsExactUserMessage(path, instruction string, notBefore time.Time) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 && codexRolloutLineMatchesExactUserMessage(line, instruction, notBefore) {
			return true, nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return false, nil
			}
			return false, readErr
		}
	}
}

func findCodexRolloutIdentity(panePID int, paneID string) codexRolloutIdentity {
	if panePID <= 0 {
		return codexRolloutIdentity{}
	}
	processes := snapshotProcesses()
	pids := descendantPIDsIncludingRoot(panePID, processes)
	if len(pids) == 0 {
		pids = []int{panePID}
	}
	identities := make(map[string]codexRolloutIdentity)
	for _, pid := range pids {
		for _, path := range codexOpenRolloutPaths(pid) {
			sessionID, err := codexRolloutSessionID(path)
			if err != nil || sessionID == "" {
				continue
			}
			identity := codexRolloutIdentity{
				Path:      filepath.Clean(path),
				SessionID: sessionID,
			}
			identities[identity.Path+"\x00"+identity.SessionID] = identity
		}
	}
	if len(identities) > 1 {
		return codexRolloutIdentity{}
	}
	for _, identity := range identities {
		return identity
	}
	return findCodexShellSnapshotIdentity(codexHomeDir(), paneID, pids, processes, time.Now())
}

func findCodexShellSnapshotIdentity(
	codexHome string,
	paneID string,
	pids []int,
	processes map[int]processInfo,
	now time.Time,
) codexRolloutIdentity {
	paneID = strings.TrimSpace(paneID)
	if strings.TrimSpace(codexHome) == "" || paneID == "" || len(pids) == 0 {
		return codexRolloutIdentity{}
	}
	var startedAt time.Time
	for _, pid := range pids {
		process, ok := processes[pid]
		if !ok || agentCommandName(agentCommandFromProcess(process)) != "codex" {
			continue
		}
		if startedAt.IsZero() || process.startedAt.Before(startedAt) {
			startedAt = process.startedAt
		}
	}
	if startedAt.IsZero() {
		return codexRolloutIdentity{}
	}

	entries, err := os.ReadDir(filepath.Join(codexHome, "shell_snapshots"))
	if err != nil {
		return codexRolloutIdentity{}
	}
	var selected codexRolloutIdentity
	var selectedAt time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sessionID, createdAt, ok := parseCodexShellSnapshotName(entry.Name())
		if !ok ||
			createdAt.Before(startedAt.Add(-2*time.Second)) ||
			createdAt.After(now.Add(2*time.Second)) {
			continue
		}
		path := filepath.Join(codexHome, "shell_snapshots", entry.Name())
		snapshotPane, readErr := codexShellSnapshotPane(path)
		if readErr != nil {
			return codexRolloutIdentity{}
		}
		if snapshotPane != paneID || createdAt.Before(selectedAt) {
			continue
		}
		if createdAt.Equal(selectedAt) && selected.SessionID != "" && selected.SessionID != sessionID {
			return codexRolloutIdentity{}
		}
		selected = codexRolloutIdentity{SessionID: sessionID}
		selectedAt = createdAt
	}
	return selected
}

func parseCodexShellSnapshotName(name string) (string, time.Time, bool) {
	match := codexShellSnapshotNameRe.FindStringSubmatch(name)
	if len(match) != 3 {
		return "", time.Time{}, false
	}
	nanoseconds, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil || nanoseconds <= 0 {
		return "", time.Time{}, false
	}
	return strings.ToLower(match[1]), time.Unix(0, nanoseconds), true
}

func codexShellSnapshotPane(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "export TMUX_PANE=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "export TMUX_PANE="))
		return strings.Trim(value, `"'`), nil
	}
	return "", scanner.Err()
}

func codexRolloutPathForSession(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if !codexShellSnapshotNameRe.MatchString(sessionID + ".0000000000000001.sh") {
		return "", fmt.Errorf("target Codex session identity is malformed")
	}
	root := filepath.Join(codexHomeDir(), "sessions")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	suffix := "-" + strings.ToLower(sessionID) + ".jsonl"
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			return nil
		}
		matches = append(matches, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple rollouts claim target Codex session identity")
	}
	if len(matches) == 1 {
		actualSessionID, readErr := codexRolloutSessionID(matches[0])
		if errors.Is(readErr, errCodexRolloutSessionIdentityPending) {
			return "", nil
		}
		if readErr != nil {
			return "", readErr
		}
		if !strings.EqualFold(actualSessionID, sessionID) {
			return "", fmt.Errorf("target Codex rollout identity changed")
		}
		return matches[0], nil
	}
	return "", nil
}

func codexHomeDir() string {
	if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func codexOpenRolloutPaths(processID int) []string {
	if paths := codexProcOpenRolloutPaths(processID); len(paths) > 0 {
		return paths
	}
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return nil
	}
	out, err := exec.Command(lsof, "-w", "-p", strconv.Itoa(processID), "-Fn").Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "n"))
			if codexIsRolloutPath(path) {
				paths = append(paths, filepath.Clean(strings.TrimSuffix(path, " (deleted)")))
			}
		}
	}
	return uniqueCodexPaths(paths)
}

func codexProcOpenRolloutPaths(processID int) []string {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(processID), "fd"))
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(processID), "fd", entry.Name()))
		if err == nil && codexIsRolloutPath(path) {
			paths = append(paths, filepath.Clean(strings.TrimSuffix(path, " (deleted)")))
		}
	}
	return uniqueCodexPaths(paths)
}

func codexIsRolloutPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSuffix(strings.TrimSpace(path), " (deleted)"))
	return strings.Contains(path, "/sessions/") &&
		strings.HasPrefix(filepath.Base(path), "rollout-") &&
		strings.HasSuffix(path, ".jsonl")
}

func uniqueCodexPaths(paths []string) []string {
	seen := make(map[string]struct{})
	var unique []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	sort.Strings(unique)
	return unique
}

func codexRolloutSessionID(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				Type    string `json:"type"`
				Payload struct {
					ID string `json:"id"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &envelope) == nil &&
				envelope.Type == "session_meta" &&
				strings.TrimSpace(envelope.Payload.ID) != "" {
				return strings.TrimSpace(envelope.Payload.ID), nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", errCodexRolloutSessionIdentityPending
			}
			return "", readErr
		}
	}
}

func codexRolloutLineMatchesExactUserMessage(line []byte, instruction string, notBefore time.Time) bool {
	var envelope struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil || timestamp.Before(notBefore) {
		return false
	}
	switch envelope.Type {
	case "event_msg":
		var payload struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		return json.Unmarshal(envelope.Payload, &payload) == nil &&
			payload.Type == "user_message" &&
			payload.Message == instruction
	case "response_item":
		var payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(envelope.Payload, &payload) != nil || payload.Type != "message" || payload.Role != "user" {
			return false
		}
		for _, item := range payload.Content {
			if item.Type == "input_text" && item.Text == instruction {
				return true
			}
		}
	}
	return false
}
