package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Codex input is one generation-bound transaction. Zen never infers task
// identity from a paste length or from terminal history: Enter requires the
// entire current composer to acknowledge the exact short instruction. Long or
// structurally ambiguous task bytes are stored in a durable, mode-0600,
// content-addressed envelope and only its machine-generated instruction enters
// the provider composer.
type codexInputIO interface {
	capture(sessionID string) codexPaneCapture
	pasteIfEmpty(sessionID, generation string, rollout codexRolloutIdentity, transactionID, body string) error
	mutateOwned(sessionID string, prepared codexPreparedInput, mutation codexOwnedMutation) error
	advanceStartup(sessionID, generation string) error
	releaseStaleInputSuppression(sessionID string, suppression codexPaneInputSuppression) error
	persistedUserMessage(rollout codexRolloutIdentity, instruction string, notBefore time.Time) (bool, error)
	sleep(time.Duration)
	now() time.Time
}

type codexSubmitConfig struct {
	startupStallTimeout time.Duration
	draftTimeout        time.Duration
	draftProgressWindow time.Duration
	draftMaxTimeout     time.Duration
	draftBytesPerSecond int
	draftLineTimeout    time.Duration
	recoveryTimeout     time.Duration
	confirmationWindow  time.Duration
	totalTimeout        time.Duration
	pollInterval        time.Duration
	stableReadyPolls    int
}

func defaultCodexSubmitConfig() codexSubmitConfig {
	return codexSubmitConfig{
		startupStallTimeout: codexInputStartupStallTimeout,
		draftTimeout:        8 * time.Second,
		draftProgressWindow: 8 * time.Second,
		draftMaxTimeout:     60 * time.Second,
		draftBytesPerSecond: 1024,
		draftLineTimeout:    25 * time.Millisecond,
		recoveryTimeout:     5 * time.Second,
		confirmationWindow:  8 * time.Second,
		totalTimeout:        3 * time.Minute,
		pollInterval:        150 * time.Millisecond,
		stableReadyPolls:    2,
	}
}

var codexTaskActiveRe = regexp.MustCompile(`(?im)^\s*•\s+(starting mcp servers|working|thinking|running|searching|reading|exploring|executing|waiting)\b`)
var codexNativeQueueRe = regexp.MustCompile(`(?im)^[ \t]*•[ \t]+messages\s+to\s+be\s+submitted\s+after\s+next\s+tool\s+call\b`)
var codexInputBufferSequence atomic.Uint64

type codexComposerState uint8

const (
	codexComposerUnknown codexComposerState = iota
	codexComposerEmpty
	codexComposerHasDraft
)

type codexPaneCapture struct {
	content     string
	styled      string
	alive       bool
	composer    codexComposerState
	generation  string
	width       int
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

type codexOwnedMutation uint8

const (
	codexOwnedMutationEnter codexOwnedMutation = iota
	codexOwnedMutationClear
)

var errCodexMutationConflict = errors.New("Codex composer changed at the conditional mutation point")

type codexTmuxPaneMetadata struct {
	alive       bool
	generation  string
	width       int
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
	codexPaneLockOperationPaste   = "paste"
	codexPaneLockOperationEnter   = "enter"
	codexPaneLockOperationClear   = "clear"
	codexPaneLockOperationStartup = "startup"
	codexPaneLockNoTransaction    = "-"
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
	width, _ := strconv.Atoi(fields[7])
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
		width:      width,
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

func proveCodexApplicationInputOrder(
	io realCodexInputIO,
	sessionID string,
	lock *codexPaneInputLock,
	prepared codexPreparedInput,
) error {
	before := readCodexTmuxPaneMetadata(lock.paneID)
	if !before.alive ||
		before.generation != lock.generation ||
		!before.inputOff ||
		!before.suppression.equal(lock.suppression) {
		return fmt.Errorf("%w: durable pane-input lock changed before ordering proof", errCodexMutationConflict)
	}
	if err := ensureCodexPaneInputConsumed(before); err != nil {
		return err
	}
	left, err := probeCodexComposerCursor(io, sessionID, lock, prepared, "Left", func(current codexTmuxPaneMetadata) bool {
		return current.cursorX != before.cursorX || current.cursorY != before.cursorY
	})
	if err != nil {
		return err
	}
	_, err = probeCodexComposerCursor(io, sessionID, lock, prepared, "Right", func(current codexTmuxPaneMetadata) bool {
		return current.cursorX == before.cursorX &&
			(current.cursorX != left.cursorX || current.cursorY != left.cursorY)
	})
	if err != nil {
		return err
	}
	current := io.capture(sessionID)
	if !codexCaptureMatchesPrepared(current, prepared, prepared.generation) {
		return fmt.Errorf("%w: composer changed during application input-order proof", errCodexMutationConflict)
	}
	return ensureCodexPaneInputConsumed(readCodexTmuxPaneMetadata(lock.paneID))
}

func probeCodexComposerCursor(
	io realCodexInputIO,
	sessionID string,
	lock *codexPaneInputLock,
	prepared codexPreparedInput,
	key string,
	acknowledged func(codexTmuxPaneMetadata) bool,
) (codexTmuxPaneMetadata, error) {
	if err := io.checkTarget(); err != nil {
		return codexTmuxPaneMetadata{}, err
	}
	args := []string{
		"select-pane", "-e", "-t", lock.paneID,
		";", "send-keys", "-t", lock.paneID, key,
		";", "select-pane", "-d", "-t", lock.paneID,
	}
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		return codexTmuxPaneMetadata{}, fmt.Errorf(
			"%w: send application input-order probe: %v%s",
			errCodexMutationConflict,
			err,
			commandOutputSuffix(out),
		)
	}
	deadline := time.Now().Add(750 * time.Millisecond)
	for {
		if err := io.checkTarget(); err != nil {
			return codexTmuxPaneMetadata{}, err
		}
		metadata := readCodexTmuxPaneMetadata(lock.paneID)
		if !metadata.alive ||
			metadata.generation != lock.generation ||
			!metadata.inputOff ||
			!metadata.suppression.equal(lock.suppression) {
			return codexTmuxPaneMetadata{}, fmt.Errorf("%w: durable pane-input lock changed during ordering proof", errCodexMutationConflict)
		}
		if err := ensureCodexPaneInputConsumed(metadata); err != nil {
			return codexTmuxPaneMetadata{}, err
		}
		current := io.capture(sessionID)
		if !codexCaptureMatchesPrepared(current, prepared, prepared.generation) {
			return codexTmuxPaneMetadata{}, fmt.Errorf("%w: composer changed during application input-order proof", errCodexMutationConflict)
		}
		if acknowledged(metadata) {
			return metadata, nil
		}
		if !time.Now().Before(deadline) {
			return codexTmuxPaneMetadata{}, fmt.Errorf(
				"%w: target application did not acknowledge the %s ordering probe",
				errCodexMutationConflict,
				key,
			)
		}
		time.Sleep(5 * time.Millisecond)
	}
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
		styled:      styled,
		alive:       true,
		composer:    codexComposerStateFromStyledPane(styled),
		generation:  before.generation,
		width:       after.width,
		rollout:     findCodexRolloutIdentity(after.panePID, after.paneID),
		inputOff:    after.inputOff,
		suppression: after.suppression,
	}
}

func (io realCodexInputIO) pasteIfEmpty(
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
		return fmt.Errorf("load Codex instruction into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	defer func() {
		_ = exec.Command("tmux", "delete-buffer", "-b", buffer).Run()
	}()
	lock, err := lockCodexPaneInputOwned(
		sessionID,
		generation,
		transactionID,
		codexPaneLockOperationPaste,
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
	if err := lock.mutateGuarded(
		io.targetGuard,
		"paste-buffer",
		"-p",
		"-d",
		"-b",
		buffer,
		"-t",
		lock.paneID,
	); err != nil {
		return fmt.Errorf("paste Codex instruction into composer: %w", err)
	}
	return nil
}

func (io realCodexInputIO) mutateOwned(
	sessionID string,
	prepared codexPreparedInput,
	mutation codexOwnedMutation,
) error {
	if err := io.checkTarget(); err != nil {
		return err
	}
	operation := codexPaneLockOperationEnter
	if mutation == codexOwnedMutationClear {
		operation = codexPaneLockOperationClear
	}
	lock, err := lockCodexPaneInputOwned(
		sessionID,
		prepared.generation,
		prepared.transactionID,
		operation,
	)
	if err != nil {
		return err
	}
	defer lock.release()
	current := io.capture(sessionID)
	if !codexCaptureMatchesPrepared(current, prepared, prepared.generation) {
		return fmt.Errorf("%w: foreign content was preserved", errCodexMutationConflict)
	}
	if err := proveCodexApplicationInputOrder(io, sessionID, lock, prepared); err != nil {
		return err
	}
	key := "Enter"
	action := "submit Codex instruction"
	if mutation == codexOwnedMutationClear {
		key = "C-c"
		action = "clear owned Codex composer"
	}
	if err := io.checkTarget(); err != nil {
		return err
	}
	if err := lock.mutateGuarded(io.targetGuard, "send-keys", "-t", lock.paneID, key); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
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

type codexDraftProofMode uint8

const (
	codexDraftProofExactLine codexDraftProofMode = iota
	codexDraftProofEnvelopeInstruction
)

type codexPreparedInput struct {
	transactionID string
	payload       string
	instruction   string
	payloadHash   string
	envelopePath  string
	proofMode     codexDraftProofMode
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
	case codexPaneLockOperationPaste,
		codexPaneLockOperationEnter,
		codexPaneLockOperationClear:
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
	case codexPaneLockOperationPaste:
		return phase == codexTransactionPrepared
	case codexPaneLockOperationEnter:
		return phase == codexTransactionEnterPending
	case codexPaneLockOperationClear:
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
	return coordinator.withSession(sessionID, func() error {
		return coordinator.submitLocked(io, sessionID, body, cfg)
	})
}

func (coordinator *codexInputCoordinator) submitLocked(io codexInputIO, sessionID, body string, cfg codexSubmitConfig) error {
	payload, err := normalizeCodexPayload(body)
	if err != nil {
		return err
	}
	transactionDeadline := io.now().Add(cfg.totalTimeout)
	preEnterDeadline := transactionDeadline.Add(-cfg.recoveryTimeout)
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
	resolved, err := coordinator.reconcileActive(io, sessionID, initial, payloadHash, cfg, transactionDeadline)
	if err != nil {
		return err
	}
	if resolved {
		return nil
	}

	baseline, err := waitForStableCodexComposer(io, sessionID, initial, cfg, preEnterDeadline)
	if err != nil {
		return err
	}
	if !baseline.rollout.valid() {
		return fmt.Errorf("Codex target rollout identity could not be proven; input was not changed")
	}
	prepared, record, err := coordinator.prepareInput(io, sessionID, baseline, payload, payloadHash)
	if err != nil {
		return err
	}
	if err := coordinator.store.Save(record); err != nil {
		return err
	}
	if !io.now().Before(preEnterDeadline) {
		record.Phase = codexTransactionNotSubmitted
		record.Detail = "bounded transaction deadline elapsed before paste"
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return fmt.Errorf("Codex transaction deadline elapsed before paste; the composer was not changed")
	}
	if err := io.pasteIfEmpty(
		sessionID,
		baseline.generation,
		baseline.rollout,
		prepared.transactionID,
		prepared.instruction,
	); err != nil {
		record.Phase = codexTransactionConflict
		record.Detail = "paste failed without provider acknowledgement: " + err.Error()
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return fmt.Errorf("%w; ownership was not acknowledged, so the composer was not cleared", err)
	}

	_, err = waitForCodexDraft(io, sessionID, baseline, prepared, cfg, preEnterDeadline)
	if err != nil {
		record.Phase = codexTransactionConflict
		record.Detail = err.Error()
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return err
	}
	record.Phase = codexTransactionDraftAcknowledged
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		cleanupErr := clearOwnedCodexDraft(io, sessionID, prepared, baseline.generation, cfg, transactionDeadline)
		if cleanupErr != nil {
			return fmt.Errorf("persist acknowledged Codex draft: %w; owned-draft cleanup failed: %v", err, cleanupErr)
		}
		return fmt.Errorf("persist acknowledged Codex draft: %w; the unchanged owned composer was cleared", err)
	}

	current := io.capture(sessionID)
	if !codexCaptureMatchesPrepared(current, prepared, baseline.generation) {
		record.Phase = codexTransactionConflict
		record.Detail = "composer changed after exact acknowledgement and before Enter"
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return fmt.Errorf("Codex composer changed after exact acknowledgement; foreign content was preserved and Enter was not sent")
	}

	record.Phase = codexTransactionEnterPending
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		cleanupErr := clearOwnedCodexDraft(io, sessionID, prepared, baseline.generation, cfg, transactionDeadline)
		if cleanupErr != nil {
			return fmt.Errorf("persist Codex Enter intent: %w; owned-draft cleanup failed: %v", err, cleanupErr)
		}
		record.Phase = codexTransactionNotSubmitted
		record.Detail = "Enter intent persistence failed; owned draft cleared before Enter"
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return fmt.Errorf("persist Codex Enter intent: %w; Enter was not sent and the owned composer was cleared", err)
	}

	if err := io.mutateOwned(sessionID, prepared, codexOwnedMutationEnter); err != nil {
		if errors.Is(err, errCodexMutationConflict) {
			record.Phase = codexTransactionConflict
			record.Detail = err.Error()
			record.UpdatedAt = io.now()
			_ = coordinator.store.Save(record)
			return fmt.Errorf("%w; Enter was not sent", err)
		}
		record.Phase = codexTransactionAmbiguous
		record.Detail = err.Error()
		record.UpdatedAt = io.now()
		_ = coordinator.store.Save(record)
		return durableCodexAmbiguity(record, err)
	}
	record.Phase = codexTransactionAmbiguous
	record.Detail = "Enter sent once; awaiting provider confirmation"
	record.UpdatedAt = io.now()
	if err := coordinator.store.Save(record); err != nil {
		// enter_pending was durably written before Enter and is itself treated as
		// ambiguous after restart, so this failure can never authorize replay.
		return durableCodexAmbiguity(record, fmt.Errorf("persist post-Enter ambiguity: %w", err))
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
	cfg codexSubmitConfig,
	transactionDeadline time.Time,
) (bool, error) {
	records, err := coordinator.store.Active(sessionID, current.generation)
	if err != nil {
		return false, err
	}
	if len(records) == 0 {
		return false, nil
	}
	if len(records) > 1 {
		return false, fmt.Errorf("multiple active Codex input transactions exist for this session generation; refusing input")
	}
	record := records[0]
	if record.PayloadSHA256 != payloadHash {
		return false, fmt.Errorf(
			"Codex session generation has active transaction %s for different input; refusing to mutate its composer",
			record.TransactionID,
		)
	}
	rollout := codexRolloutIdentity{Path: record.RolloutPath, SessionID: record.RolloutSessionID}
	persisted, reconcileErr := io.persistedUserMessage(rollout, record.Instruction, record.CreatedAt)
	if reconcileErr != nil {
		return false, fmt.Errorf("reconcile durable Codex transaction %s: %w", record.TransactionID, reconcileErr)
	}
	if persisted {
		record.Phase = codexTransactionConfirmed
		record.Detail = "reconciled from exact persisted Codex user message"
		record.UpdatedAt = io.now()
		if err := coordinator.store.Save(record); err != nil {
			return false, err
		}
		return true, nil
	}
	switch record.Phase {
	case codexTransactionEnterPending, codexTransactionAmbiguous:
		return false, durableCodexAmbiguity(
			record,
			fmt.Errorf("no exact persisted Codex user message is observable yet"),
		)
	case codexTransactionPrepared, codexTransactionDraftAcknowledged:
		prepared := codexPreparedInput{
			transactionID: record.TransactionID,
			instruction:   record.Instruction,
			payloadHash:   record.PayloadSHA256,
			envelopePath:  record.EnvelopePath,
			proofMode:     codexProofModeForRecord(record),
			rollout:       rollout,
			generation:    record.SessionGeneration,
		}
		switch {
		case current.composer == codexComposerEmpty && isAgentInputReady("codex", current.content):
			record.Phase = codexTransactionNotSubmitted
			record.Detail = "reconciled before-Enter transaction from proven empty composer"
			record.UpdatedAt = io.now()
			if err := coordinator.store.Save(record); err != nil {
				return false, err
			}
			return false, nil
		case codexCaptureMatchesPrepared(current, prepared, record.SessionGeneration):
			if err := clearOwnedCodexDraft(io, sessionID, prepared, record.SessionGeneration, cfg, transactionDeadline); err != nil {
				return false, err
			}
			record.Phase = codexTransactionNotSubmitted
			record.Detail = "reconciled and cleared unchanged owned pre-Enter composer"
			record.UpdatedAt = io.now()
			if err := coordinator.store.Save(record); err != nil {
				return false, err
			}
			return false, nil
		default:
			return false, fmt.Errorf(
				"Codex composer conflicts with durable pre-Enter transaction %s; foreign content was preserved",
				record.TransactionID,
			)
		}
	default:
		return false, nil
	}
}

func (coordinator *codexInputCoordinator) prepareInput(
	io codexInputIO,
	sessionID string,
	baseline codexPaneCapture,
	payload string,
	payloadHash string,
) (codexPreparedInput, codexTransactionRecord, error) {
	transactionID, err := newCodexTransactionID()
	if err != nil {
		return codexPreparedInput{}, codexTransactionRecord{}, err
	}
	prepared := codexPreparedInput{
		transactionID: transactionID,
		payload:       payload,
		payloadHash:   payloadHash,
		proofMode:     codexDraftProofExactLine,
		instruction:   payload,
		rollout:       baseline.rollout,
		generation:    baseline.generation,
	}
	if !codexCanDirectlyObserve(payload, baseline.width) {
		envelopePath, err := coordinator.store.WriteEnvelope(payloadHash, []byte(payload))
		if err != nil {
			return codexPreparedInput{}, codexTransactionRecord{}, err
		}
		prepared.envelopePath = envelopePath
		prepared.instruction = codexEnvelopeInstruction(transactionID, envelopePath, payloadHash)
		prepared.proofMode = codexDraftProofEnvelopeInstruction
	}
	now := io.now().UTC()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     transactionID,
		SessionID:         sessionID,
		SessionGeneration: baseline.generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionPrepared,
		PayloadSHA256:     payloadHash,
		Instruction:       prepared.instruction,
		InstructionSHA256: codexSHA256(prepared.instruction),
		EnvelopePath:      prepared.envelopePath,
		RolloutPath:       prepared.rollout.Path,
		RolloutSessionID:  prepared.rollout.SessionID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return prepared, record, nil
}

func codexProofModeForRecord(record codexTransactionRecord) codexDraftProofMode {
	if record.EnvelopePath != "" {
		return codexDraftProofEnvelopeInstruction
	}
	return codexDraftProofExactLine
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
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	if value == "" {
		return "", fmt.Errorf("Codex input is empty")
	}
	for _, current := range value {
		if current == 0 || current == 0x7f || (current < 0x20 && current != '\n' && current != '\t') {
			return "", fmt.Errorf("Codex input contains unsupported control character U+%04X", current)
		}
	}
	return value, nil
}

func codexCanDirectlyObserve(value string, paneWidth int) bool {
	if paneWidth <= 4 || strings.ContainsAny(value, "\r\n\t") || strings.TrimSpace(value) != value {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}
	// UTF-8 byte length deliberately overestimates the terminal width of ASCII
	// and most non-ASCII text. Direct mode is allowed only when the complete
	// logical value is guaranteed to remain on the first composer row.
	return len(value) <= paneWidth-4
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
		persisted, err := io.persistedUserMessage(prepared.rollout, prepared.instruction, notBefore)
		if err != nil {
			return fmt.Errorf("inspect target Codex rollout for exact submitted instruction: %w", err)
		}
		if persisted {
			return nil
		}
		capture := io.capture(sessionID)
		if !capture.alive {
			return fmt.Errorf("Codex session exited before instruction submission was confirmed")
		}
		if capture.generation != prepared.generation {
			return fmt.Errorf("Codex session generation changed before instruction submission was confirmed")
		}
		if !capture.rollout.equal(prepared.rollout) {
			return fmt.Errorf("Codex target rollout changed before instruction submission was confirmed")
		}
		if !io.now().Before(deadline) {
			break
		}
		io.sleep(cfg.pollInterval)
	}
	return fmt.Errorf(
		"Codex instruction received one Enter but the exact user message was not persisted in target rollout %s within %s",
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
				return codexPaneCapture{}, fmt.Errorf("Codex composer is occupied by a foreign draft; it was not cleared, pasted into, or submitted")
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

func waitForCodexDraft(
	io codexInputIO,
	sessionID string,
	baseline codexPaneCapture,
	prepared codexPreparedInput,
	cfg codexSubmitConfig,
	transactionDeadline time.Time,
) (codexPaneCapture, error) {
	startedAt := io.now()
	deadline := codexEarlierDeadline(startedAt.Add(codexDraftObservationBudget(prepared.instruction, cfg)), transactionDeadline)
	hardDeadline := codexEarlierDeadline(startedAt.Add(cfg.draftMaxTimeout), transactionDeadline)
	progress := 0
	for {
		capture := io.capture(sessionID)
		if !capture.alive {
			return codexPaneCapture{}, fmt.Errorf("Codex session exited after the instruction was pasted")
		}
		if capture.generation != baseline.generation {
			return codexPaneCapture{}, fmt.Errorf("Codex session generation changed after the instruction was pasted; Enter was not sent")
		}
		if codexCaptureMatchesPrepared(capture, prepared, baseline.generation) {
			return capture, nil
		}
		observed, recognized := codexObservedComposerIdentity(capture, prepared.proofMode)
		if capture.composer == codexComposerHasDraft && recognized {
			expected := codexExpectedComposerIdentity(prepared)
			if !strings.HasPrefix(expected, observed) {
				return codexPaneCapture{}, fmt.Errorf("Codex composer contains foreign or corrupted text after paste; it was preserved and Enter was not sent")
			}
			if len(observed) > progress {
				progress = len(observed)
				deadline = codexEarlierDeadline(io.now().Add(cfg.draftProgressWindow), hardDeadline)
			}
		}
		if !io.now().Before(deadline) {
			return codexPaneCapture{}, fmt.Errorf(
				"Codex instruction was pasted once but exact whole-composer identity was not acknowledged before the adaptive %s deadline (%d/%d exact bytes observed, %s hard limit); the draft was preserved and Enter was not sent",
				codexDraftObservationBudget(prepared.instruction, cfg),
				progress,
				len(codexExpectedComposerIdentity(prepared)),
				cfg.draftMaxTimeout,
			)
		}
		io.sleep(cfg.pollInterval)
	}
}

func clearOwnedCodexDraft(
	io codexInputIO,
	sessionID string,
	prepared codexPreparedInput,
	generation string,
	cfg codexSubmitConfig,
	transactionDeadline time.Time,
) error {
	current := io.capture(sessionID)
	if !codexCaptureMatchesPrepared(current, prepared, generation) {
		return fmt.Errorf("Codex composer no longer exactly matches the acknowledged owned instruction; foreign content was preserved")
	}
	if err := io.mutateOwned(sessionID, prepared, codexOwnedMutationClear); err != nil {
		return err
	}
	deadline := codexEarlierDeadline(io.now().Add(cfg.recoveryTimeout), transactionDeadline)
	stable := 0
	for {
		capture := io.capture(sessionID)
		if !capture.alive || capture.generation != generation {
			return fmt.Errorf("Codex session generation changed before owned-draft cleanup was confirmed")
		}
		if capture.composer == codexComposerEmpty && isAgentInputReady("codex", capture.content) {
			stable++
			if stable >= cfg.stableReadyPolls {
				return nil
			}
		} else {
			stable = 0
		}
		if !io.now().Before(deadline) {
			return fmt.Errorf("an empty Codex composer was not proven within %s after clearing the owned instruction", cfg.recoveryTimeout)
		}
		io.sleep(cfg.pollInterval)
	}
}

func codexEarlierDeadline(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func codexDraftObservationBudget(body string, cfg codexSubmitConfig) time.Duration {
	budget := cfg.draftTimeout
	maximum := cfg.draftMaxTimeout
	if maximum <= 0 || budget >= maximum {
		return maximum
	}
	addCapped := func(extra time.Duration) {
		if extra <= 0 || budget >= maximum {
			return
		}
		remaining := maximum - budget
		if extra >= remaining {
			budget = maximum
			return
		}
		budget += extra
	}
	if cfg.draftBytesPerSecond > 0 && len(body) > cfg.draftBytesPerSecond {
		addCapped(time.Duration((len(body)-1)/cfg.draftBytesPerSecond) * time.Second)
	}
	if cfg.draftLineTimeout > 0 {
		addCapped(time.Duration(strings.Count(strings.ReplaceAll(body, "\r\n", "\n"), "\n")) * cfg.draftLineTimeout)
	}
	return budget
}

func codexDraftVisible(before, after, body string) bool {
	if strings.ContainsAny(body, "\r\n") {
		return false
	}
	beforeText, beforeFound := codexSingleRowComposerText(before)
	afterText, afterFound := codexSingleRowComposerText(after)
	return afterFound && afterText == body && (!beforeFound || beforeText != body)
}

func codexCaptureMatchesPrepared(capture codexPaneCapture, prepared codexPreparedInput, generation string) bool {
	if !capture.alive || capture.generation != generation || capture.composer != codexComposerHasDraft {
		return false
	}
	if !capture.rollout.equal(prepared.rollout) {
		return false
	}
	observed, found := codexObservedComposerIdentity(capture, prepared.proofMode)
	return found && observed == codexExpectedComposerIdentity(prepared)
}

func codexExpectedComposerIdentity(prepared codexPreparedInput) string {
	return prepared.instruction
}

func codexObservedComposerIdentity(capture codexPaneCapture, mode codexDraftProofMode) (string, bool) {
	lines, found := codexComposerTextLinesStyled(capture.content, capture.styled)
	if !found {
		return "", false
	}
	if mode == codexDraftProofExactLine {
		if len(lines) != 1 {
			return "", false
		}
		return lines[0], true
	}
	return strings.Join(lines, ""), true
}

func codexSingleRowComposerText(content string) (string, bool) {
	lines, found := codexComposerTextLinesStyled(content, "")
	if !found || len(lines) != 1 {
		return "", false
	}
	return lines[0], true
}

func codexComposerTextLines(content string) ([]string, bool) {
	return codexComposerTextLinesStyled(content, "")
}

func codexComposerTextLinesStyled(content, styled string) ([]string, bool) {
	rawLines := codexCaptureLines(content)
	var styledLines []string
	if styled != "" {
		styledLines = codexCaptureLines(styled)
		if len(styledLines) != len(rawLines) {
			return nil, false
		}
		for index := range rawLines {
			if stripCodexTerminalEscapes(styledLines[index]) != rawLines[index] {
				return nil, false
			}
		}
	}
	start := -1
	for index, line := range rawLines {
		if line == "›" || strings.HasPrefix(line, "› ") {
			start = index
		}
	}
	if start < 0 {
		return nil, false
	}
	first := strings.TrimPrefix(rawLines[start], "›")
	first = strings.TrimPrefix(first, " ")
	lines := []string{first}
	for index := start + 1; index < len(rawLines); index++ {
		line := rawLines[index]
		if line == "" {
			next := index + 1
			for next < len(rawLines) && rawLines[next] == "" {
				next++
			}
			if next == index+1 &&
				next < len(rawLines) &&
				codexStyledLineIsProviderChrome(styledLines, next) {
				return lines, true
			}
			// Visual wrapping never creates an empty row in the logical
			// instruction. Any non-chrome blank is therefore user-enterable
			// composer state that cannot be losslessly joined into identity.
			return nil, false
		}
		if strings.HasPrefix(line, "›") {
			return nil, false
		}
		if codexStyledLineIsProviderChrome(styledLines, index) {
			return lines, true
		}
		if !strings.HasPrefix(line, "  ") {
			return nil, false
		}
		lines = append(lines, strings.TrimPrefix(line, "  "))
	}
	return lines, true
}

func codexCaptureLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n")
}

func codexStyledLineIsProviderChrome(lines []string, index int) bool {
	if len(lines) == 0 || index < 0 || index >= len(lines) {
		return false
	}
	if !codexStyledLineIsProviderFooter(lines[index]) {
		return false
	}
	for next := index + 1; next < len(lines); next++ {
		if stripCodexTerminalEscapes(lines[next]) != "" {
			return false
		}
	}
	return true
}

var codexProviderFooterModelRe = regexp.MustCompile(
	`^(?:gpt-[A-Za-z0-9][A-Za-z0-9._-]*|o[0-9][A-Za-z0-9._-]*|codex-[A-Za-z0-9][A-Za-z0-9._-]*)(?: (?:default|minimal|low|medium|high|xhigh|max|ultra))?$`,
)

func codexStyledLineIsProviderFooter(line string) bool {
	index := 0
	if next, ok := codexConsumeExactSGR(line, index, "0"); ok {
		index = next
	}
	if !strings.HasPrefix(line[index:], "  ") {
		return false
	}
	index += 2
	modelColor, next, ok := codexConsumeSGRForegroundColor(line, index)
	if !ok {
		return false
	}
	index = next
	modelEnd := strings.IndexByte(line[index:], 0x1b)
	if modelEnd < 0 {
		return false
	}
	model := line[index : index+modelEnd]
	if !codexProviderFooterModelRe.MatchString(model) {
		return false
	}
	index += modelEnd
	if next, ok = codexConsumeExactSGR(line, index, "2"); !ok {
		return false
	}
	index = next
	if next, ok = codexConsumeExactSGR(line, index, "39"); !ok {
		return false
	}
	index = next
	if !strings.HasPrefix(line[index:], " · ") {
		return false
	}
	index += len(" · ")
	if next, ok = codexConsumeExactSGR(line, index, "0"); !ok {
		return false
	}
	index = next
	cwdColor, next, ok := codexConsumeSGRForegroundColor(line, index)
	if !ok || cwdColor == modelColor {
		return false
	}
	index = next
	cwdEnd := strings.IndexByte(line[index:], 0x1b)
	cwd := ""
	switch {
	case cwdEnd < 0:
		cwd = line[index:]
		index = len(line)
	default:
		cwd = line[index : index+cwdEnd]
		index += cwdEnd
		if next, ok = codexConsumeExactSGR(line, index, "0"); !ok {
			return false
		}
		index = next
	}
	if index != len(line) || !codexProviderFooterWorkingDirectory(cwd) {
		return false
	}
	return true
}

func codexConsumeExactSGR(value string, index int, params string) (int, bool) {
	if index < 0 || index >= len(value) || value[index] != 0x1b {
		return index, false
	}
	next, observed, final := codexEscapeSequence(value, index)
	return next, next > index && final == 'm' && observed == params
}

func codexConsumeSGRForegroundColor(value string, index int) (string, int, bool) {
	if index < 0 || index >= len(value) || value[index] != 0x1b {
		return "", index, false
	}
	next, params, final := codexEscapeSequence(value, index)
	if next <= index || final != 'm' {
		return "", index, false
	}
	fields := strings.Split(params, ";")
	if len(fields) != 5 || fields[0] != "38" || fields[1] != "2" {
		return "", index, false
	}
	for _, raw := range fields[2:] {
		component, err := strconv.Atoi(raw)
		if err != nil || component < 0 || component > 255 {
			return "", index, false
		}
	}
	return params, next, true
}

func codexProviderFooterWorkingDirectory(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, " · ") {
		return false
	}
	if value != "~" && !strings.HasPrefix(value, "~/") && !strings.HasPrefix(value, "/") {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}
	return true
}

func codexSubmissionAdvanced(draft, current, instruction string) bool {
	if current == draft {
		return false
	}
	if codexNativeQueueAdvanced(draft, current) {
		return true
	}
	if strings.HasPrefix(current, draft) {
		suffix := current[len(draft):]
		if codexTaskActiveRe.MatchString(suffix) || codexInputPromptRe.MatchString(suffix) {
			return true
		}
	}
	if len(codexTaskActiveRe.FindAllStringIndex(current, -1)) >
		len(codexTaskActiveRe.FindAllStringIndex(draft, -1)) {
		return true
	}
	if len(codexInputPromptRe.FindAllStringIndex(current, -1)) >
		len(codexInputPromptRe.FindAllStringIndex(draft, -1)) {
		return true
	}
	draftBlocks := codexInstructionBlockEnds(draft, instruction)
	currentBlocks := codexInstructionBlockEnds(current, instruction)
	if len(currentBlocks) < len(draftBlocks) || len(currentBlocks) == 0 {
		return false
	}
	suffix := current[currentBlocks[len(currentBlocks)-1]:]
	return codexTaskActiveRe.MatchString(suffix) || codexInputPromptRe.MatchString(suffix)
}

func codexNativeQueueAdvanced(before, after string) bool {
	return len(codexNativeQueueRe.FindAllStringIndex(after, -1)) >
		len(codexNativeQueueRe.FindAllStringIndex(before, -1))
}

func codexInstructionBlockEnds(content, instruction string) []int {
	lines := strings.SplitAfter(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var ends []int
	offset := 0
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSuffix(lines[index], "\n")
		if line != "›" && !strings.HasPrefix(line, "› ") {
			offset += len(lines[index])
			continue
		}
		blockStart := offset
		first := strings.TrimPrefix(line, "›")
		first = strings.TrimPrefix(first, " ")
		parts := []string{first}
		blockEnd := offset + len(lines[index])
		nextOffset := blockEnd
		next := index + 1
		for ; next < len(lines); next++ {
			continuation := strings.TrimSuffix(lines[next], "\n")
			if continuation == "" || !strings.HasPrefix(continuation, "  ") {
				break
			}
			parts = append(parts, strings.TrimPrefix(continuation, "  "))
			blockEnd = nextOffset + len(lines[next])
			nextOffset = blockEnd
		}
		observed := parts[0]
		if strings.HasPrefix(instruction, "ZEN_TX=") {
			observed = strings.Join(parts, "")
		}
		if observed == instruction {
			ends = append(ends, blockEnd)
		}
		offset = blockStart
		for consumed := index; consumed < next; consumed++ {
			offset += len(lines[consumed])
		}
		index = next - 1
	}
	return ends
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
