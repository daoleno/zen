package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Codex input is one generation-bound transaction. The exact user payload is
// persisted before mutation, then paste and Enter are submitted in one tmux
// command queue after proving a stable, empty provider composer. Transaction
// identity stays in Zen's journal and is never added to model-visible input.
type codexInputIO interface {
	capture(sessionID string) codexPaneCapture
	probeSubmissionReadiness(sessionID, generation string, rollout codexRolloutIdentity) error
	submitIfEmpty(sessionID, generation string, rollout codexRolloutIdentity, transactionID, body string) error
	advanceStartup(sessionID, generation string) error
	releaseStaleInputSuppression(sessionID string, suppression codexPaneInputSuppression) error
	persistedUserMessage(rollout codexRolloutIdentity, payload string, notBefore time.Time) (bool, error)
	sleep(time.Duration)
	now() time.Time
}

type codexSubmitConfig struct {
	startupStallTimeout time.Duration
	confirmationReserve time.Duration
	confirmationWindow  time.Duration
	totalTimeout        time.Duration
	pollInterval        time.Duration
	stableReadyPolls    int
}

func defaultCodexSubmitConfig() codexSubmitConfig {
	return codexSubmitConfig{
		startupStallTimeout: codexInputStartupStallTimeout,
		confirmationReserve: 5 * time.Second,
		confirmationWindow:  8 * time.Second,
		totalTimeout:        3 * time.Minute,
		pollInterval:        150 * time.Millisecond,
		stableReadyPolls:    2,
	}
}

var codexInputBufferSequence atomic.Uint64

const codexPayloadSpoolThreshold = 256

type codexComposerState uint8

const (
	codexComposerUnknown codexComposerState = iota
	codexComposerEmpty
	codexComposerHasDraft
)

type codexPaneCapture struct {
	content     string
	alive       bool
	composer    codexComposerState
	generation  string
	rollout     codexRolloutIdentity
	inputOff    bool
	suppression codexPaneInputSuppression
}

type codexRolloutIdentity struct {
	Path      string
	SessionID string
}

func (identity codexRolloutIdentity) valid() bool {
	return strings.TrimSpace(identity.SessionID) != ""
}

func (identity codexRolloutIdentity) equal(other codexRolloutIdentity) bool {
	if !identity.valid() || !other.valid() || identity.SessionID != other.SessionID {
		return false
	}
	if strings.TrimSpace(identity.Path) == "" || strings.TrimSpace(other.Path) == "" {
		return true
	}
	return filepath.Clean(identity.Path) == filepath.Clean(other.Path)
}

type codexStartupProgress uint8

const (
	codexStartupSawIdentity codexStartupProgress = 1 << iota
	codexStartupSawModelLoading
	codexStartupSawComposer
)

func (progress *codexStartupProgress) observe(content string) bool {
	current := latestCodexPaneContent(content)
	var observed codexStartupProgress
	if strings.Contains(strings.ToLower(current), "openai codex") {
		observed |= codexStartupSawIdentity
	}
	if codexModelLoadingRe.MatchString(current) {
		observed |= codexStartupSawModelLoading
	}
	if codexInputPromptRe.MatchString(current) &&
		!codexModelLoadingRe.MatchString(current) &&
		!codexStartupContinueRe.MatchString(current) {
		observed |= codexStartupSawComposer
	}
	advanced := observed &^ *progress
	*progress |= observed
	return advanced != 0
}

type realCodexInputIO struct {
	targetGuard func() error
}

var errCodexMutationConflict = errors.New("Codex composer changed at the conditional mutation point")
var errCodexResumeOnPaneChange = errors.New("Codex pending input requires a pane change before resumption")

// InputPendingError means Zen durably owns the exact input transaction, but
// provider input ownership is not currently safe. Retrying the same receipt
// resumes that transaction; it must not be presented as a failed send.
type InputPendingError struct {
	TransactionID string
	cause         error
}

func (err *InputPendingError) Error() string {
	if err == nil {
		return "Codex input is durably pending"
	}
	if err.cause == nil {
		return fmt.Sprintf("Codex transaction %s is durably pending", err.TransactionID)
	}
	return fmt.Sprintf("Codex transaction %s is durably pending: %v", err.TransactionID, err.cause)
}

func (err *InputPendingError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// IsInputPending reports whether err is a durable, non-terminal input wait.
func IsInputPending(err error) bool {
	var pending *InputPendingError
	return errors.As(err, &pending)
}

type codexTmuxPaneMetadata struct {
	alive       bool
	generation  string
	panePID     int
	paneID      string
	paneTTY     string
	cursorX     int
	cursorY     int
	inputOff    bool
	suppression codexPaneInputSuppression
}

type codexPaneInputSuppression struct {
	Generation    string
	TransactionID string
	Operation     string
	ProcessID     int
	ProcessStart  int64
}

const (
	codexPaneLockOperationSubmit      = "submit"
	codexPaneLockOperationStartup     = "startup"
	codexPaneLockNoTransaction        = "-"
	codexPaneLockOperationLegacyPaste = "paste"
	codexPaneLockOperationLegacyEnter = "enter"
	codexPaneLockOperationLegacyClear = "clear"
)

var codexPaneLockOptionNames = []string{
	"@zen_codex_input_lock_generation",
	"@zen_codex_input_lock_transaction",
	"@zen_codex_input_lock_operation",
	"@zen_codex_input_lock_pid",
	"@zen_codex_input_lock_started",
}

func (suppression codexPaneInputSuppression) valid() bool {
	return suppression.Generation != "" &&
		suppression.TransactionID != "" &&
		suppression.Operation != "" &&
		suppression.ProcessID > 0 &&
		suppression.ProcessStart > 0
}

func (suppression codexPaneInputSuppression) equal(other codexPaneInputSuppression) bool {
	return suppression.valid() &&
		other.valid() &&
		suppression == other
}

func (suppression codexPaneInputSuppression) ownerLive() bool {
	if !suppression.valid() {
		return false
	}
	process, ok := snapshotProcesses()[suppression.ProcessID]
	return ok && process.startedAt.UnixNano() == suppression.ProcessStart
}

func readCodexTmuxPaneMetadata(sessionID string) codexTmuxPaneMetadata {
	out, err := exec.Command(
		"tmux",
		"display-message",
		"-p",
		"-t",
		sessionID,
		"#{pane_dead}\t#{session_id}\t#{session_created}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}\t#{pane_width}\t#{pane_input_off}\t#{@zen_codex_input_lock_generation}\t#{@zen_codex_input_lock_transaction}\t#{@zen_codex_input_lock_operation}\t#{@zen_codex_input_lock_pid}\t#{@zen_codex_input_lock_started}\t#{pane_tty}\t#{cursor_x}\t#{cursor_y}",
	).Output()
	if err != nil {
		return codexTmuxPaneMetadata{}
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 17 || fields[0] == "1" {
		return codexTmuxPaneMetadata{}
	}
	panePID, _ := strconv.Atoi(fields[5])
	lockPID, _ := strconv.Atoi(fields[12])
	lockStarted, _ := strconv.ParseInt(fields[13], 10, 64)
	cursorX, cursorXErr := strconv.Atoi(fields[15])
	cursorY, cursorYErr := strconv.Atoi(fields[16])
	if cursorXErr != nil || cursorYErr != nil {
		return codexTmuxPaneMetadata{}
	}
	identity := strings.Join(fields[1:7], "\x00")
	digest := sha256.Sum256([]byte(identity))
	return codexTmuxPaneMetadata{
		alive:      true,
		generation: fmt.Sprintf("%x", digest[:]),
		panePID:    panePID,
		paneID:     fields[4],
		paneTTY:    fields[14],
		cursorX:    cursorX,
		cursorY:    cursorY,
		inputOff:   fields[8] == "1",
		suppression: codexPaneInputSuppression{
			Generation:    fields[9],
			TransactionID: fields[10],
			Operation:     fields[11],
			ProcessID:     lockPID,
			ProcessStart:  lockStarted,
		},
	}
}

type codexPaneInputLock struct {
	paneID      string
	generation  string
	suppression codexPaneInputSuppression
	held        bool
}

func lockCodexPaneInput(sessionID, generation string) (*codexPaneInputLock, error) {
	return lockCodexPaneInputOwned(
		sessionID,
		generation,
		codexPaneLockNoTransaction,
		codexPaneLockOperationStartup,
	)
}

func lockCodexPaneInputOwned(
	sessionID string,
	generation string,
	transactionID string,
	operation string,
) (*codexPaneInputLock, error) {
	current := readCodexTmuxPaneMetadata(sessionID)
	if !current.alive ||
		current.generation != generation ||
		current.paneID == "" ||
		current.inputOff {
		return nil, fmt.Errorf("%w: exact target pane could not be exclusively locked", errCodexMutationConflict)
	}
	process, ok := snapshotProcesses()[os.Getpid()]
	if !ok || process.startedAt.IsZero() {
		return nil, fmt.Errorf("%w: lock owner process identity could not be proven", errCodexMutationConflict)
	}
	transactionID = strings.TrimSpace(transactionID)
	operation = strings.TrimSpace(operation)
	if transactionID == "" {
		transactionID = codexPaneLockNoTransaction
	}
	for _, value := range []string{generation, transactionID, operation} {
		if value == "" || strings.ContainsAny(value, "\t\r\n") {
			return nil, fmt.Errorf("%w: invalid durable pane-input owner", errCodexMutationConflict)
		}
	}
	suppression := codexPaneInputSuppression{
		Generation:    generation,
		TransactionID: transactionID,
		Operation:     operation,
		ProcessID:     os.Getpid(),
		ProcessStart:  process.startedAt.UnixNano(),
	}
	lock := &codexPaneInputLock{
		paneID:      current.paneID,
		generation:  generation,
		suppression: suppression,
	}
	args := codexPaneLockCommandArgs(lock.paneID, suppression)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("lock Codex pane input: %w%s", err, commandOutputSuffix(out))
	}
	lock.held = true
	locked := readCodexTmuxPaneMetadata(lock.paneID)
	if !locked.alive ||
		locked.generation != generation ||
		locked.paneID != lock.paneID ||
		!locked.inputOff ||
		!locked.suppression.equal(suppression) {
		lock.release()
		return nil, fmt.Errorf("%w: target pane generation changed while acquiring input lock", errCodexMutationConflict)
	}
	return lock, nil
}

func codexPaneLockCommandArgs(paneID string, suppression codexPaneInputSuppression) []string {
	values := []string{
		suppression.Generation,
		suppression.TransactionID,
		suppression.Operation,
		strconv.Itoa(suppression.ProcessID),
		strconv.FormatInt(suppression.ProcessStart, 10),
	}
	var args []string
	for index, name := range codexPaneLockOptionNames {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, "set-option", "-p", "-t", paneID, name, values[index])
	}
	args = append(args, ";", "select-pane", "-d", "-t", paneID)
	return args
}

func codexPaneUnlockCommandArgs(paneID string) []string {
	args := []string{"select-pane", "-e", "-t", paneID}
	for _, name := range codexPaneLockOptionNames {
		args = append(args, ";", "set-option", "-p", "-u", "-t", paneID, name)
	}
	return args
}

func (lock *codexPaneInputLock) release() {
	if lock == nil || !lock.held || lock.paneID == "" {
		return
	}
	current := readCodexTmuxPaneMetadata(lock.paneID)
	if current.alive &&
		current.generation == lock.generation &&
		current.inputOff &&
		current.suppression.equal(lock.suppression) {
		_ = exec.Command("tmux", codexPaneUnlockCommandArgs(lock.paneID)...).Run()
	}
	lock.held = false
}

func (lock *codexPaneInputLock) mutate(command ...string) error {
	return lock.mutateGuarded(nil, command...)
}

func (lock *codexPaneInputLock) mutateGuarded(guard func() error, command ...string) error {
	if lock == nil || !lock.held {
		return fmt.Errorf("%w: Codex pane input lock is not held", errCodexMutationConflict)
	}
	current := readCodexTmuxPaneMetadata(lock.paneID)
	if !current.alive ||
		current.generation != lock.generation ||
		current.paneID != lock.paneID ||
		!current.inputOff ||
		!current.suppression.equal(lock.suppression) {
		lock.release()
		return fmt.Errorf("%w: target pane generation changed at the mutation point", errCodexMutationConflict)
	}
	if err := ensureCodexPaneInputConsumed(current); err != nil {
		return err
	}
	if guard != nil {
		if err := guard(); err != nil {
			return fmt.Errorf("%w: %v", errCodexMutationConflict, err)
		}
	}
	args := codexPaneUnlockCommandArgs(lock.paneID)
	args = append(args, ";")
	args = append(args, command...)
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		lock.release()
		return fmt.Errorf("mutate exclusively locked Codex pane: %w%s", err, commandOutputSuffix(out))
	}
	lock.held = false
	return nil
}

func ensureCodexPaneInputConsumed(metadata codexTmuxPaneMetadata) error {
	if !metadata.alive || metadata.paneTTY == "" {
		return fmt.Errorf("%w: target pane PTY identity could not be proven", errCodexMutationConflict)
	}
	pending, err := pendingPTYInputBytes(metadata.paneTTY)
	if err != nil {
		return fmt.Errorf("%w: inspect target pane PTY input queue: %v", errCodexMutationConflict, err)
	}
	if pending != 0 {
		return fmt.Errorf(
			"%w: target application has %d unconsumed PTY input bytes",
			errCodexMutationConflict,
			pending,
		)
	}
	return nil
}

func (io realCodexInputIO) checkTarget() error {
	if io.targetGuard == nil {
		return nil
	}
	if err := io.targetGuard(); err != nil {
		return fmt.Errorf("%w: %v", errCodexMutationConflict, err)
	}
	return nil
}

func (io realCodexInputIO) capture(sessionID string) codexPaneCapture {
	if io.checkTarget() != nil {
		return codexPaneCapture{}
	}
	before := readCodexTmuxPaneMetadata(sessionID)
	if !before.alive || before.generation == "" {
		return codexPaneCapture{}
	}
	out, err := exec.Command(
		"tmux",
		"capture-pane",
		"-e",
		"-J",
		"-t",
		sessionID,
		"-p",
		"-S",
		"-1000",
	).Output()
	if err != nil {
		return codexPaneCapture{}
	}
	after := readCodexTmuxPaneMetadata(sessionID)
	if !after.alive || before.generation != after.generation {
		return codexPaneCapture{}
	}
	styled := string(out)
	return codexPaneCapture{
		content:     stripCodexTerminalEscapes(styled),
		alive:       true,
		composer:    codexComposerStateFromStyledPane(styled),
		generation:  before.generation,
		rollout:     findCodexRolloutIdentity(after.panePID, after.paneID),
		inputOff:    after.inputOff,
		suppression: after.suppression,
	}
}

func (io realCodexInputIO) probeSubmissionReadiness(
	sessionID string,
	generation string,
	rollout codexRolloutIdentity,
) error {
	if err := io.checkTarget(); err != nil {
		return err
	}
	current := io.capture(sessionID)
	if !current.alive || current.generation != generation {
		return fmt.Errorf("%w: target Codex generation is unavailable", errCodexResumeOnPaneChange)
	}
	if !current.rollout.equal(rollout) {
		return fmt.Errorf("%w: target Codex rollout changed", errCodexResumeOnPaneChange)
	}
	if current.inputOff ||
		current.composer != codexComposerEmpty ||
		!isAgentInputReady("codex", current.content) {
		return fmt.Errorf("%w: target Codex composer is not ready and empty", errCodexResumeOnPaneChange)
	}
	if err := ensureCodexPaneInputConsumed(readCodexTmuxPaneMetadata(sessionID)); err != nil {
		return err
	}
	return nil
}

func (io realCodexInputIO) submitIfEmpty(
	sessionID string,
	generation string,
	rollout codexRolloutIdentity,
	transactionID string,
	body string,
) error {
	if err := io.checkTarget(); err != nil {
		return err
	}
	buffer := fmt.Sprintf("zen-codex-input-%d-%d", os.Getpid(), codexInputBufferSequence.Add(1))
	load := exec.Command("tmux", "load-buffer", "-b", buffer, "-")
	load.Stdin = strings.NewReader(body)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("load Codex payload into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	defer func() {
		_ = exec.Command("tmux", "delete-buffer", "-b", buffer).Run()
	}()
	lock, err := lockCodexPaneInputOwned(
		sessionID,
		generation,
		transactionID,
		codexPaneLockOperationSubmit,
	)
	if err != nil {
		return err
	}
	defer lock.release()
	if err := io.checkTarget(); err != nil {
		return err
	}
	current := io.capture(sessionID)
	if !current.alive ||
		current.generation != generation ||
		!current.rollout.equal(rollout) ||
		current.composer != codexComposerEmpty ||
		!isAgentInputReady("codex", current.content) {
		return fmt.Errorf("%w: exact empty target composer was not preserved before paste", errCodexMutationConflict)
	}
	if err := ensureCodexPaneInputConsumed(readCodexTmuxPaneMetadata(lock.paneID)); err != nil {
		return err
	}
	if err := io.checkTarget(); err != nil {
		return err
	}
	command := codexAtomicSubmitCommand(buffer, lock.paneID)
	if err := lock.mutateGuarded(io.targetGuard, command...); err != nil {
		return fmt.Errorf("atomically submit Codex payload: %w", err)
	}
	return nil
}

func codexAtomicSubmitCommand(buffer, paneID string) []string {
	return []string{
		"paste-buffer",
		"-p",
		"-b",
		buffer,
		"-t",
		paneID,
		";",
		"send-keys",
		"-t",
		paneID,
		"Enter",
		";",
		"delete-buffer",
		"-b",
		buffer,
	}
}

func (io realCodexInputIO) advanceStartup(sessionID, generation string) error {
	if err := io.checkTarget(); err != nil {
		return err
	}
	lock, err := lockCodexPaneInputOwned(
		sessionID,
		generation,
		codexPaneLockNoTransaction,
		codexPaneLockOperationStartup,
	)
	if err != nil {
		return err
	}
	defer lock.release()
	current := io.capture(sessionID)
	if !current.alive ||
		current.generation != generation ||
		!isCodexStartupContinuePrompt("codex", current.content) {
		return fmt.Errorf("%w: Codex startup prompt changed before Enter", errCodexMutationConflict)
	}
	if err := ensureCodexPaneInputConsumed(readCodexTmuxPaneMetadata(lock.paneID)); err != nil {
		return err
	}
	if err := io.checkTarget(); err != nil {
		return err
	}
	if err := lock.mutateGuarded(io.targetGuard, "send-keys", "-t", lock.paneID, "Enter"); err != nil {
		return fmt.Errorf("advance Codex startup prompt: %w", err)
	}
	return nil
}

func (io realCodexInputIO) releaseStaleInputSuppression(
	sessionID string,
	suppression codexPaneInputSuppression,
) error {
	if err := io.checkTarget(); err != nil {
		return err
	}
	current := readCodexTmuxPaneMetadata(sessionID)
	if !current.alive ||
		current.generation != suppression.Generation ||
		!current.inputOff ||
		!current.suppression.equal(suppression) {
		return fmt.Errorf("%w: stale pane-input owner changed before recovery", errCodexMutationConflict)
	}
	if suppression.ownerLive() {
		return fmt.Errorf("%w: pane-input owner process is still live", errCodexMutationConflict)
	}
	if out, err := exec.Command(
		"tmux",
		codexPaneUnlockCommandArgs(current.paneID)...,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("release stale Codex pane-input owner: %w%s", err, commandOutputSuffix(out))
	}
	recovered := readCodexTmuxPaneMetadata(current.paneID)
	if !recovered.alive ||
		recovered.generation != suppression.Generation ||
		recovered.inputOff ||
		recovered.suppression != (codexPaneInputSuppression{}) {
		return fmt.Errorf("%w: stale pane-input release was not proven", errCodexMutationConflict)
	}
	return nil
}

func (realCodexInputIO) persistedUserMessage(
	rollout codexRolloutIdentity,
	instruction string,
	notBefore time.Time,
) (bool, error) {
	return codexRolloutContainsExactUserMessage(rollout, instruction, notBefore)
}

func (realCodexInputIO) sleep(delay time.Duration) { time.Sleep(delay) }
func (realCodexInputIO) now() time.Time            { return time.Now() }

type codexPreparedInput struct {
	transactionID string
	payload       string
	envelopePath  string
	rollout       codexRolloutIdentity
	generation    string
}

type codexSubmissionSession struct {
	mu sync.Mutex
}

type codexInputCoordinator struct {
	mu       sync.Mutex
	sessions map[string]*codexSubmissionSession
	store    codexTransactionStore
}

func newCodexInputCoordinator() *codexInputCoordinator {
	return newCodexInputCoordinatorWithStore(newMemoryCodexTransactionStore())
}

func newPersistentCodexInputCoordinator(stateDir string) (*codexInputCoordinator, error) {
	store, err := newFileCodexTransactionStore(stateDir)
	if err != nil {
		return nil, err
	}
	return newCodexInputCoordinatorWithStore(store), nil
}

func newCodexInputCoordinatorWithStore(store codexTransactionStore) *codexInputCoordinator {
	if store == nil {
		store = newMemoryCodexTransactionStore()
	}
	return &codexInputCoordinator{
		sessions: make(map[string]*codexSubmissionSession),
		store:    store,
	}
}

func (coordinator *codexInputCoordinator) session(sessionID string) *codexSubmissionSession {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.sessions == nil {
		coordinator.sessions = make(map[string]*codexSubmissionSession)
	}
	session := coordinator.sessions[sessionID]
	if session == nil {
		session = &codexSubmissionSession{}
		coordinator.sessions[sessionID] = session
	}
	return session
}

func (coordinator *codexInputCoordinator) withSession(sessionID string, action func() error) error {
	session := coordinator.session(sessionID)
	session.mu.Lock()
	defer session.mu.Unlock()
	return action()
}

func (coordinator *codexInputCoordinator) recoverStaleInputSuppression(
	io codexInputIO,
	sessionID string,
	current codexPaneCapture,
) (codexPaneCapture, error) {
	if !current.inputOff {
		return current, nil
	}
	suppression := current.suppression
	if !suppression.valid() || suppression.Generation != current.generation {
		return codexPaneCapture{}, fmt.Errorf(
			"Codex pane input is suppressed without a valid generation-bound Zen owner; mutation was not sent",
		)
	}
	if suppression.ownerLive() {
		return codexPaneCapture{}, fmt.Errorf(
			"Codex pane input is owned by a live Zen mutation; concurrent mutation was not sent",
		)
	}
	records, err := coordinator.store.Active(sessionID, current.generation)
	if err != nil {
		return codexPaneCapture{}, err
	}
	switch suppression.Operation {
	case codexPaneLockOperationStartup:
		if suppression.TransactionID != codexPaneLockNoTransaction || len(records) != 0 {
			return codexPaneCapture{}, fmt.Errorf(
				"stale Codex startup suppression does not match durable transaction state",
			)
		}
	case codexPaneLockOperationSubmit,
		codexPaneLockOperationLegacyPaste,
		codexPaneLockOperationLegacyEnter,
		codexPaneLockOperationLegacyClear:
		var owner *codexTransactionRecord
		for index := range records {
			if records[index].TransactionID == suppression.TransactionID {
				owner = &records[index]
				break
			}
		}
		if owner == nil || !codexSuppressionMatchesRecord(suppression.Operation, owner.Phase) {
			return codexPaneCapture{}, fmt.Errorf(
				"stale Codex pane suppression does not match its durable transaction owner",
			)
		}
	default:
		return codexPaneCapture{}, fmt.Errorf("stale Codex pane suppression operation is unsupported")
	}
	if err := io.releaseStaleInputSuppression(sessionID, suppression); err != nil {
		return codexPaneCapture{}, err
	}
	recovered := io.capture(sessionID)
	if !recovered.alive ||
		recovered.generation != current.generation ||
		recovered.inputOff {
		return codexPaneCapture{}, fmt.Errorf(
			"Codex stale pane-input recovery did not preserve an enabled target generation",
		)
	}
	return recovered, nil
}

func codexSuppressionMatchesRecord(operation string, phase codexTransactionPhase) bool {
	switch operation {
	case codexPaneLockOperationSubmit:
		return phase == codexTransactionEnterPending
	case codexPaneLockOperationLegacyPaste:
		return phase == codexTransactionPrepared
	case codexPaneLockOperationLegacyEnter:
		return phase == codexTransactionEnterPending
	case codexPaneLockOperationLegacyClear:
		return phase == codexTransactionPrepared || phase == codexTransactionDraftAcknowledged
	default:
		return false
	}
}

func (coordinator *codexInputCoordinator) mutateIfUnowned(
	io codexInputIO,
	sessionID string,
	action func() error,
) error {
	return coordinator.withSession(sessionID, func() error {
		current := io.capture(sessionID)
		if !current.alive || current.generation == "" {
			return fmt.Errorf("Codex session generation could not be proven; terminal mutation was not sent")
		}
		current, err := coordinator.recoverStaleInputSuppression(io, sessionID, current)
		if err != nil {
			return err
		}
		records, err := coordinator.store.Active(sessionID, current.generation)
		if err != nil {
			return err
		}
		if len(records) > 0 {
			record := records[0]
			return fmt.Errorf(
				"Codex transaction %s is unresolved in phase %s for this session generation; unrelated terminal mutation was not sent",
				record.TransactionID,
				record.Phase,
			)
		}
		return action()
	})
}

func (coordinator *codexInputCoordinator) submit(io codexInputIO, sessionID, body string, cfg codexSubmitConfig) error {
	return coordinator.submitWithReceipt(io, sessionID, body, "", cfg)
}

func (coordinator *codexInputCoordinator) submitWithReceipt(
	io codexInputIO,
	sessionID, body, receipt string,
	cfg codexSubmitConfig,
) error {
	return coordinator.withSession(sessionID, func() error {
		return coordinator.submitLocked(io, sessionID, body, receipt, cfg)
	})
}

func (coordinator *codexInputCoordinator) submitLocked(
	io codexInputIO,
	sessionID, body, receipt string,
	cfg codexSubmitConfig,
) error {
	payload, err := normalizeCodexPayload(body)
	if err != nil {
		return err
	}
	transactionDeadline := io.now().Add(cfg.totalTimeout)
	preSubmitDeadline := transactionDeadline.Add(-cfg.confirmationReserve)
	initial := io.capture(sessionID)
	if !initial.alive {
		return fmt.Errorf("Codex session exited before input arbitration")
	}
	if initial.generation == "" {
		return fmt.Errorf("Codex session generation could not be proven; input was not changed")
	}
	initial, err = coordinator.recoverStaleInputSuppression(io, sessionID, initial)
	if err != nil {
		return err
	}
	payloadHash := codexSHA256(payload)
	if receipt != "" {
		existing, found, receiptErr := coordinator.store.Receipt(sessionID, initial.generation, receipt)
		if receiptErr != nil {
			return receiptErr
		}
		if found {
			if existing.PayloadSHA256 != payloadHash {
				return fmt.Errorf(
					"Codex acceptance receipt %q already belongs to different input",
					receipt,
				)
			}
			if existing.Phase == codexTransactionConfirmed {
				return nil
			}
		}
	}
	resolved, resumed, err := coordinator.reconcileActive(
		io,
		sessionID,
		initial,
		payloadHash,
		receipt,
	)
	if err != nil {
		return err
	}
	if resolved {
		return nil
	}

	var prepared codexPreparedInput
	var record codexTransactionRecord
	baseline := initial
	if resumed != nil {
		record = *resumed
		prepared = preparedCodexInputFromRecord(record)
		baseline.rollout = prepared.rollout
		baseline, err = waitForStableCodexComposer(io, sessionID, baseline, cfg, preSubmitDeadline)
		if err != nil {
			if baseline.composer == codexComposerHasDraft {
				return pendingCodexInput(record, err)
			}
			return err
		}
		if !baseline.rollout.equal(prepared.rollout) {
			return fmt.Errorf("Codex target rollout changed while its exact payload was pending; provider input was not changed")
		}
	} else {
		baseline, err = waitForStableCodexComposer(io, sessionID, initial, cfg, preSubmitDeadline)
		if err != nil {
			if baseline.composer != codexComposerHasDraft || !baseline.rollout.valid() {
				return err
			}
			pendingCause := err
			prepared, record, err = coordinator.prepareInput(
				io,
				sessionID,
				baseline,
				payload,
				payloadHash,
				receipt,
			)
			if err != nil {
				return err
			}
			record.Detail = "durably pending behind a preserved foreign draft"
			if err := coordinator.store.Save(record); err != nil {
				return err
			}
			return pendingCodexInput(record, pendingCause)
		}
		if !baseline.rollout.valid() {
			return fmt.Errorf("Codex target rollout identity could not be proven; input was not changed")
		}
		prepared, record, err = coordinator.prepareInput(
			io,
			sessionID,
			baseline,
			payload,
			payloadHash,
			receipt,
		)
		if err != nil {
			return err
		}
		if err := coordinator.store.Save(record); err != nil {
			return err
		}
	}
	if !io.now().Before(preSubmitDeadline) {
		record.Phase = codexTransactionPrepared
		record.Detail = "durably pending after the bounded pre-submit wait"
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return pendingCodexInput(record, fmt.Errorf("bounded pre-submit wait elapsed"))
	}
	record.Phase = codexTransactionEnterPending
	record.Detail = "atomic paste-and-Enter intent persisted before provider mutation"
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		return fmt.Errorf("persist Codex submission intent: %w; provider input was not changed", err)
	}
	err = io.submitIfEmpty(
		sessionID,
		baseline.generation,
		baseline.rollout,
		prepared.transactionID,
		prepared.payload,
	)
	if err != nil {
		if !errors.Is(err, errCodexMutationConflict) {
			record.Phase = codexTransactionAmbiguous
			record.Detail = err.Error()
			record.UpdatedAt = io.now()
			_ = coordinator.store.Save(record)
			return durableCodexAmbiguity(record, err)
		}
		record.Phase = codexTransactionPrepared
		record.Detail = "preflight deferred without provider mutation: " + err.Error()
		record.UpdatedAt = io.now()
		if saveErr := coordinator.store.Save(record); saveErr != nil {
			return fmt.Errorf("persist deferred Codex submission: %w", saveErr)
		}
		return pendingCodexInput(record, err)
	}
	record.Phase = codexTransactionAmbiguous
	record.Detail = "atomic paste and Enter submitted once; awaiting provider confirmation"
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		// enter_pending was durably written before the atomic mutation and is
		// itself ambiguous after restart, so this can never authorize replay.
		return durableCodexAmbiguity(record, fmt.Errorf("persist post-submit ambiguity: %w", err))
	}

	if err := confirmCodexSubmission(io, sessionID, prepared, record.CreatedAt, cfg, transactionDeadline); err != nil {
		record.Detail = err.Error()
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return durableCodexAmbiguity(record, err)
	}
	record.Phase = codexTransactionConfirmed
	record.Detail = ""
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		return fmt.Errorf("Codex submission was confirmed but its durable transaction could not be settled: %w", err)
	}
	return nil
}

func (coordinator *codexInputCoordinator) reconcileActive(
	io codexInputIO,
	sessionID string,
	current codexPaneCapture,
	payloadHash string,
	receipt string,
) (bool, *codexTransactionRecord, error) {
	records, err := coordinator.store.Active(sessionID, current.generation)
	if err != nil {
		return false, nil, err
	}
	if len(records) == 0 {
		return false, nil, nil
	}
	if len(records) > 1 {
		return false, nil, fmt.Errorf("multiple active Codex input transactions exist for this session generation; refusing input")
	}
	record := records[0]
	if record.PayloadSHA256 != payloadHash {
		return false, nil, fmt.Errorf(
			"Codex session generation has active transaction %s for different input; refusing to mutate its composer",
			record.TransactionID,
		)
	}
	if record.AcceptanceReceipt != receipt {
		return false, nil, fmt.Errorf(
			"Codex session generation has active transaction %s for a different acceptance receipt; refusing input",
			record.TransactionID,
		)
	}
	rollout := codexRolloutIdentity{Path: record.RolloutPath, SessionID: record.RolloutSessionID}
	persisted, reconcileErr := io.persistedUserMessage(rollout, record.Instruction, record.CreatedAt)
	if reconcileErr != nil {
		return false, nil, fmt.Errorf("reconcile durable Codex transaction %s: %w", record.TransactionID, reconcileErr)
	}
	if persisted {
		record.Phase = codexTransactionConfirmed
		record.Detail = "reconciled from exact persisted Codex user message"
		record.UpdatedAt = io.now()
		if err := coordinator.store.Save(record); err != nil {
			return false, nil, err
		}
		return true, nil, nil
	}
	switch record.Phase {
	case codexTransactionDraftAcknowledged, codexTransactionEnterPending, codexTransactionAmbiguous:
		return false, nil, durableCodexAmbiguity(
			record,
			fmt.Errorf("no exact persisted Codex user message is observable yet"),
		)
	case codexTransactionPrepared:
		return false, &record, nil
	default:
		return false, nil, nil
	}
}

func preparedCodexInputFromRecord(record codexTransactionRecord) codexPreparedInput {
	return codexPreparedInput{
		transactionID: record.TransactionID,
		payload:       record.Instruction,
		envelopePath:  record.EnvelopePath,
		rollout: codexRolloutIdentity{
			Path:      record.RolloutPath,
			SessionID: record.RolloutSessionID,
		},
		generation: record.SessionGeneration,
	}
}

func pendingCodexInput(record codexTransactionRecord, cause error) error {
	return &InputPendingError{TransactionID: record.TransactionID, cause: cause}
}

func (coordinator *codexInputCoordinator) hasAcceptedReceipt(
	io codexInputIO,
	sessionID, receipt string,
) (bool, error) {
	var accepted bool
	err := coordinator.withSession(sessionID, func() error {
		current := io.capture(sessionID)
		if !current.alive || current.generation == "" {
			return fmt.Errorf("Codex session generation could not be proven")
		}
		record, found, err := coordinator.store.Receipt(sessionID, current.generation, receipt)
		if err != nil || !found {
			return err
		}
		if record.Phase == codexTransactionConfirmed {
			accepted = true
			return nil
		}
		rollout := codexRolloutIdentity{
			Path:      record.RolloutPath,
			SessionID: record.RolloutSessionID,
		}
		persisted, err := io.persistedUserMessage(rollout, record.Instruction, record.CreatedAt)
		if err != nil {
			return fmt.Errorf("reconcile Codex acceptance receipt: %w", err)
		}
		if !persisted {
			return nil
		}
		record.Phase = codexTransactionConfirmed
		record.Detail = "reconciled acceptance receipt from exact persisted Codex user message"
		record.UpdatedAt = io.now()
		if err := coordinator.store.Save(record); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	return accepted, err
}

func (coordinator *codexInputCoordinator) prepareInput(
	io codexInputIO,
	sessionID string,
	baseline codexPaneCapture,
	payload string,
	payloadHash string,
	receipt string,
) (codexPreparedInput, codexTransactionRecord, error) {
	transactionID, err := newCodexTransactionID()
	if err != nil {
		return codexPreparedInput{}, codexTransactionRecord{}, err
	}
	prepared := codexPreparedInput{
		transactionID: transactionID,
		payload:       payload,
		rollout:       baseline.rollout,
		generation:    baseline.generation,
	}
	if codexPayloadNeedsSpool(payload) {
		envelopePath, err := coordinator.store.WriteEnvelope(payloadHash, []byte(payload))
		if err != nil {
			return codexPreparedInput{}, codexTransactionRecord{}, err
		}
		prepared.envelopePath = envelopePath
	}
	now := io.now().UTC()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     transactionID,
		SessionID:         sessionID,
		SessionGeneration: baseline.generation,
		AcceptanceReceipt: receipt,
		Action:            "submit_codex_input",
		Phase:             codexTransactionPrepared,
		PayloadSHA256:     payloadHash,
		Instruction:       payload,
		InstructionSHA256: payloadHash,
		EnvelopePath:      prepared.envelopePath,
		RolloutPath:       prepared.rollout.Path,
		RolloutSessionID:  prepared.rollout.SessionID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return prepared, record, nil
}

func durableCodexAmbiguity(record codexTransactionRecord, cause error) error {
	return fmt.Errorf(
		"Codex transaction %s is durably ambiguous for session generation %s; input will not be pasted or submitted again until an exact persisted user message reconciles it: %w",
		record.TransactionID,
		record.SessionGeneration,
		cause,
	)
}

var defaultCodexInputCoordinator = newCodexInputCoordinator()
var defaultCodexInputCoordinatorMu sync.RWMutex

func configureDefaultCodexInputCoordinator(stateDir string) (*codexInputCoordinator, error) {
	coordinator, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		return nil, err
	}
	defaultCodexInputCoordinatorMu.Lock()
	defaultCodexInputCoordinator = coordinator
	defaultCodexInputCoordinatorMu.Unlock()
	return coordinator, nil
}

func currentDefaultCodexInputCoordinator() *codexInputCoordinator {
	defaultCodexInputCoordinatorMu.RLock()
	coordinator := defaultCodexInputCoordinator
	defaultCodexInputCoordinatorMu.RUnlock()
	return coordinator
}

func submitCoordinatedCodexInput(io codexInputIO, sessionID, body string, cfg codexSubmitConfig) error {
	return currentDefaultCodexInputCoordinator().submit(io, sessionID, body, cfg)
}

// submitCodexInput is the isolated test boundary. Production callers use the
// configured persistent coordinator through submitCoordinatedCodexInput.
func submitCodexInput(io codexInputIO, sessionID, body string, cfg codexSubmitConfig) error {
	return newCodexInputCoordinator().submit(io, sessionID, body, cfg)
}

func normalizeCodexPayload(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("Codex input must be valid UTF-8")
	}
	if value == "" {
		return "", fmt.Errorf("Codex input is empty")
	}
	return value, nil
}

func codexPayloadNeedsSpool(value string) bool {
	// Size changes only Zen's internal durable storage. Every payload uses the
	// same provider submission path and remains byte-for-byte model-visible.
	return len(value) > codexPayloadSpoolThreshold
}

func confirmCodexSubmission(
	io codexInputIO,
	sessionID string,
	prepared codexPreparedInput,
	notBefore time.Time,
	cfg codexSubmitConfig,
	transactionDeadline time.Time,
) error {
	deadline := codexEarlierDeadline(io.now().Add(cfg.confirmationWindow), transactionDeadline)
	for {
		persisted, err := io.persistedUserMessage(prepared.rollout, prepared.payload, notBefore)
		if err != nil {
			return fmt.Errorf("inspect target Codex rollout for exact submitted payload: %w", err)
		}
		if persisted {
			return nil
		}
		capture := io.capture(sessionID)
		if !capture.alive {
			return fmt.Errorf("Codex session exited before payload submission was confirmed")
		}
		if capture.generation != prepared.generation {
			return fmt.Errorf("Codex session generation changed before payload submission was confirmed")
		}
		if !capture.rollout.equal(prepared.rollout) {
			return fmt.Errorf("Codex target rollout changed before payload submission was confirmed")
		}
		if !io.now().Before(deadline) {
			break
		}
		io.sleep(cfg.pollInterval)
	}
	return fmt.Errorf(
		"Codex payload received one atomic paste-and-Enter but the exact user message was not persisted in target rollout %s within %s",
		prepared.rollout.SessionID,
		cfg.confirmationWindow,
	)
}

func waitForStableCodexComposer(
	io codexInputIO,
	sessionID string,
	initial codexPaneCapture,
	cfg codexSubmitConfig,
	transactionDeadline time.Time,
) (codexPaneCapture, error) {
	deadline := codexEarlierDeadline(io.now().Add(cfg.startupStallTimeout), transactionDeadline)
	stable := 0
	advancedStartupPrompt := false
	var startupProgress codexStartupProgress
	capture := initial
	for {
		if !capture.alive {
			return codexPaneCapture{}, fmt.Errorf("Codex session exited before its composer became ready")
		}
		if capture.generation != initial.generation {
			return codexPaneCapture{}, fmt.Errorf("Codex session generation changed before its composer became ready")
		}
		content := capture.content
		if startupProgress.observe(content) {
			deadline = codexEarlierDeadline(io.now().Add(cfg.startupStallTimeout), transactionDeadline)
		}
		if !advancedStartupPrompt && isCodexStartupContinuePrompt("codex", content) {
			if err := io.advanceStartup(sessionID, initial.generation); err != nil {
				return codexPaneCapture{}, fmt.Errorf("advance Codex startup prompt: %w", err)
			}
			advancedStartupPrompt = true
			stable = 0
			deadline = codexEarlierDeadline(io.now().Add(cfg.startupStallTimeout), transactionDeadline)
		} else if isAgentInputReady("codex", content) {
			switch capture.composer {
			case codexComposerEmpty:
				stable++
				if stable >= cfg.stableReadyPolls {
					return capture, nil
				}
			case codexComposerHasDraft:
				return capture, fmt.Errorf("Codex composer is occupied by a foreign draft; it was not cleared, pasted into, or submitted")
			default:
				return codexPaneCapture{}, fmt.Errorf("Codex composer ownership is unknown; it was not cleared, pasted into, or submitted")
			}
		} else {
			stable = 0
		}
		if !io.now().Before(deadline) {
			return codexPaneCapture{}, fmt.Errorf("Codex composer made no recognized startup progress for %s", cfg.startupStallTimeout)
		}
		io.sleep(cfg.pollInterval)
		capture = io.capture(sessionID)
	}
}

func codexEarlierDeadline(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func codexComposerStateFromStyledPane(content string) codexComposerState {
	state := codexComposerUnknown
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(stripCodexTerminalEscapes(line), "›") {
			continue
		}
		promptIndex := strings.Index(line, "›")
		if promptIndex < 0 {
			continue
		}
		dim, found := codexFirstVisibleRuneStyle(line[promptIndex+len("›"):])
		if !found {
			state = codexComposerUnknown
			continue
		}
		if dim {
			state = codexComposerEmpty
		} else {
			state = codexComposerHasDraft
		}
	}
	return state
}

func codexFirstVisibleRuneStyle(value string) (dim bool, found bool) {
	for index := 0; index < len(value); {
		if value[index] == 0x1b {
			next, params, final := codexEscapeSequence(value, index)
			if next <= index {
				index++
				continue
			}
			if final == 'm' {
				dim = codexApplySGRDim(params, dim)
			}
			index = next
			continue
		}
		current, size := utf8.DecodeRuneInString(value[index:])
		index += size
		if !unicode.IsSpace(current) {
			return dim, true
		}
	}
	return false, false
}

func codexApplySGRDim(params string, dim bool) bool {
	if params == "" {
		return false
	}
	values := make([]int, 0, strings.Count(params, ";")+1)
	for _, raw := range strings.Split(params, ";") {
		value, err := strconv.Atoi(raw)
		if err == nil {
			values = append(values, value)
		}
	}
	for index := 0; index < len(values); index++ {
		switch values[index] {
		case 0, 22:
			dim = false
		case 2:
			dim = true
		case 38, 48, 58:
			if index+1 >= len(values) {
				continue
			}
			switch values[index+1] {
			case 2:
				index += min(4, len(values)-index-1)
			case 5:
				index += min(2, len(values)-index-1)
			}
		}
	}
	return dim
}

func codexEscapeSequence(value string, start int) (next int, params string, final byte) {
	if start+1 >= len(value) {
		return len(value), "", 0
	}
	switch value[start+1] {
	case '[':
		index := start + 2
		paramsStart := index
		for index < len(value) {
			current := value[index]
			index++
			if current >= 0x40 && current <= 0x7e {
				return index, value[paramsStart : index-1], current
			}
		}
		return len(value), "", 0
	case ']':
		index := start + 2
		for index < len(value) {
			if value[index] == 0x07 {
				return index + 1, "", 0
			}
			if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
				return index + 2, "", 0
			}
			index++
		}
		return len(value), "", 0
	default:
		return start + 2, "", 0
	}
}

func stripCodexTerminalEscapes(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); {
		if value[index] != 0x1b {
			output.WriteByte(value[index])
			index++
			continue
		}
		next, _, _ := codexEscapeSequence(value, index)
		if next <= index {
			index++
		} else {
			index = next
		}
	}
	return output.String()
}
