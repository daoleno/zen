package watcher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// InputOutcome describes what Zen knows at the provider-mutation boundary.
// Accepted means a delegated provider turn was observed (or, for direct
// receipt input, that the existing provider transaction completed). Ambiguous
// means the target-bound tmux queue started and may have submitted; callers
// must retain ownership and must not automatically replay it.
type InputOutcome string

const (
	InputAccepted     InputOutcome = "accepted"
	InputNotSubmitted InputOutcome = "not_submitted"
	InputAmbiguous    InputOutcome = "ambiguous"
)

type InputResult struct {
	Outcome   InputOutcome
	Receipt   string
	TurnID    string
	Duplicate bool
}

type delegatedAdmissionEvidence struct {
	Stream      string
	ID          string
	Cursor      uint64
	StartedAt   time.Time
	InputSHA256 string
}

type delegatedInputConfirmation struct {
	Outcome          InputOutcome
	ProviderActivity string
}

type delegatedInputConfirmer struct {
	baseline func() (delegatedAdmissionEvidence, error)
	confirm  func(
		baseline delegatedAdmissionEvidence,
		mutationBoundary time.Time,
		payloadSHA256 string,
	) (delegatedInputConfirmation, error)
}

// InputSubmissionError preserves whether provider mutation was impossible or
// may already have happened.
type InputSubmissionError struct {
	Result InputResult
	Cause  error
}

func (err *InputSubmissionError) Error() string {
	if err == nil {
		return "Session input failed"
	}
	switch err.Result.Outcome {
	case InputAmbiguous:
		return fmt.Sprintf("Session input outcome is unknown and will not be replayed: %v", err.Cause)
	default:
		return fmt.Sprintf("Session input was definitely not submitted: %v", err.Cause)
	}
}

func (err *InputSubmissionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func InputOutcomeFromError(err error) InputOutcome {
	if err == nil {
		return InputAccepted
	}
	var submissionErr *InputSubmissionError
	if errors.As(err, &submissionErr) {
		return submissionErr.Result.Outcome
	}
	return InputNotSubmitted
}

type sessionInputProvider struct {
	submitKey string
	prepare   time.Duration
	settle    time.Duration
}

func sessionInputProviderForCommand(command string) sessionInputProvider {
	return sessionInputProvider{
		submitKey: "Enter",
		prepare:   tmuxPrepareDelay(command),
		settle:    tmuxSubmitDelay(command),
	}
}

type sessionInputPane struct {
	alive      bool
	paneID     string
	generation string
}

type sessionInputIO interface {
	pane(sessionID string) sessionInputPane
	loadBuffer(buffer, payload string) error
	deleteBuffer(buffer string)
	runQueue(args []string) (started bool, err error)
	receiptLedger(target string) (sessionInputReceiptLedger, error)
	writeReceiptLedger(target string, ledger sessionInputReceiptLedger) error
	delegatedTurn(target string) (delegatedTurnRecord, bool, error)
	writeDelegatedTurn(target string, turn delegatedTurnRecord) error
	clearDelegatedTurn(target string) error
	paneContent(target string) (string, error)
}

type realSessionInputIO struct{}

func (realSessionInputIO) pane(sessionID string) sessionInputPane {
	out, err := exec.Command(
		"tmux",
		"display-message",
		"-p",
		"-t",
		sessionID,
		"#{pane_dead}\t#{session_id}\t#{session_created}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}",
	).Output()
	if err != nil {
		return sessionInputPane{}
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 7 || fields[0] == "1" || strings.TrimSpace(fields[4]) == "" {
		return sessionInputPane{}
	}
	digest := sha256.Sum256([]byte(strings.Join(fields[1:7], "\x00")))
	return sessionInputPane{
		alive:      true,
		paneID:     fields[4],
		generation: fmt.Sprintf("%x", digest[:]),
	}
}

func (realSessionInputIO) loadBuffer(buffer, payload string) error {
	command := exec.Command("tmux", "load-buffer", "-b", buffer, "-")
	command.Stdin = strings.NewReader(payload)
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("load payload into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) deleteBuffer(buffer string) {
	_ = exec.Command("tmux", "delete-buffer", "-b", buffer).Run()
}

func (realSessionInputIO) runQueue(args []string) (bool, error) {
	command := exec.Command("tmux", args...)
	if err := command.Start(); err != nil {
		return false, err
	}
	if err := command.Wait(); err != nil {
		return true, err
	}
	return true, nil
}

func (realSessionInputIO) receiptLedger(target string) (sessionInputReceiptLedger, error) {
	value, err := tmuxWindowUserOption(target, sessionInputReceiptOption)
	if err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return decodeSessionInputReceiptLedger(value)
}

func (realSessionInputIO) writeReceiptLedger(target string, ledger sessionInputReceiptLedger) error {
	if err := validateSessionInputReceiptLedger(ledger); err != nil {
		return err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("encode Session input receipt ledger: %w", err)
	}
	out, err := exec.Command(
		"tmux",
		"set-option",
		"-w",
		"-t",
		target,
		"@"+sessionInputReceiptOption,
		string(raw),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("write Session input receipt ledger: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) delegatedTurn(target string) (delegatedTurnRecord, bool, error) {
	value, err := tmuxWindowUserOption(target, delegatedTurnOption)
	if err != nil {
		return delegatedTurnRecord{}, false, err
	}
	return decodeDelegatedTurn(value)
}

func (realSessionInputIO) writeDelegatedTurn(target string, turn delegatedTurnRecord) error {
	if err := validateDelegatedTurn(turn); err != nil {
		return err
	}
	raw, err := json.Marshal(turn)
	if err != nil {
		return fmt.Errorf("encode delegated turn: %w", err)
	}
	out, err := exec.Command(
		"tmux", "set-option", "-w", "-t", target,
		"@"+delegatedTurnOption, string(raw),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("write delegated turn: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) clearDelegatedTurn(target string) error {
	out, err := exec.Command(
		"tmux", "set-option", "-w", "-u", "-t", target,
		"@"+delegatedTurnOption,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clear delegated turn: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) paneContent(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-200").Output()
	if err != nil {
		return "", fmt.Errorf("capture post-dispatch pane baseline: %w", err)
	}
	return string(out), nil
}

type sessionInputSession struct {
	mu sync.Mutex
}

// sessionInputOwner is the sole serialization owner for every terminal
// provider. It has no provider-specific queue, journal, spool, or resume loop.
type sessionInputOwner struct {
	mu       sync.Mutex
	sessions map[string]*sessionInputSession
	io       sessionInputIO
}

func newSessionInputOwner(io sessionInputIO) *sessionInputOwner {
	if io == nil {
		io = realSessionInputIO{}
	}
	return &sessionInputOwner{
		sessions: make(map[string]*sessionInputSession),
		io:       io,
	}
}

func (owner *sessionInputOwner) session(sessionID string) *sessionInputSession {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	session := owner.sessions[sessionID]
	if session == nil {
		session = &sessionInputSession{}
		owner.sessions[sessionID] = session
	}
	return session
}

func (owner *sessionInputOwner) serialized(sessionID string, action func() error) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return definitelyNotSubmitted("", fmt.Errorf("missing session id"))
	}
	session := owner.session(sessionID)
	session.mu.Lock()
	defer session.mu.Unlock()
	return action()
}

const sessionInputReceiptOption = "zen_session_input_receipts"
const sessionInputReceiptLedgerSchema = 1
const sessionInputReceiptLedgerLimit = 64
const sessionInputReceiptMaxBytes = 512

type sessionInputReceiptEntry struct {
	Receipt       string       `json:"receipt"`
	PayloadSHA256 string       `json:"payload_sha256"`
	Outcome       InputOutcome `json:"outcome"`
}

type sessionInputReceiptLedger struct {
	SchemaVersion int                        `json:"schema_version"`
	Entries       []sessionInputReceiptEntry `json:"entries"`
}

var sessionInputBufferSequence atomic.Uint64

func (owner *sessionInputOwner) submit(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	command string,
	payload string,
	receipt string,
) (InputResult, error) {
	return owner.submitWithTurn(
		sessionID,
		expected,
		resolver,
		command,
		payload,
		receipt,
		nil,
		delegatedInputConfirmer{},
	)
}

func (owner *sessionInputOwner) receiptOutcome(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	receipt string,
) (InputResult, bool, error) {
	result := InputResult{Outcome: InputNotSubmitted, Receipt: strings.TrimSpace(receipt)}
	found := false
	err := owner.serialized(sessionID, func() error {
		if result.Receipt == "" || len(result.Receipt) > sessionInputReceiptMaxBytes ||
			!utf8.ValidString(result.Receipt) {
			return fmt.Errorf("input receipt is invalid or exceeds %d bytes", sessionInputReceiptMaxBytes)
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return err
		}
		baseline := owner.io.pane(sessionID)
		if err := validateSessionInputPane(baseline); err != nil {
			return err
		}
		ledger, err := owner.io.receiptLedger(baseline.paneID)
		if err != nil {
			return fmt.Errorf("read durable input receipt ledger: %w", err)
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return err
		}
		current := owner.io.pane(sessionID)
		if err := validateSameSessionInputPane(baseline, current); err != nil {
			return err
		}
		entry, exists := ledger.entry(result.Receipt)
		if !exists {
			return nil
		}
		found = true
		result.Outcome = entry.Outcome
		return nil
	})
	return result, found, err
}

func (owner *sessionInputOwner) submitDelegated(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	command string,
	payload string,
	turn delegatedTurnRecord,
	confirm delegatedInputConfirmer,
) (InputResult, error) {
	return owner.submitWithTurn(sessionID, expected, resolver, command, payload, turn.ID, &turn, confirm)
}

func (owner *sessionInputOwner) submitWithTurn(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	command string,
	payload string,
	receipt string,
	turn *delegatedTurnRecord,
	confirm delegatedInputConfirmer,
) (InputResult, error) {
	result := InputResult{Outcome: InputNotSubmitted, Receipt: strings.TrimSpace(receipt)}
	requiresConfirmation := turn != nil
	err := owner.serialized(sessionID, func() error {
		if !utf8.ValidString(payload) {
			return definitelyNotSubmitted(result.Receipt, fmt.Errorf("input must be valid UTF-8"))
		}
		if payload == "" {
			return definitelyNotSubmitted(result.Receipt, fmt.Errorf("input is empty"))
		}
		payloadDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		baseline := owner.io.pane(sessionID)
		if err := validateSessionInputPane(baseline); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		ledger := emptySessionInputReceiptLedger()
		if result.Receipt != "" {
			if len(result.Receipt) > sessionInputReceiptMaxBytes || !utf8.ValidString(result.Receipt) {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("input receipt is invalid or exceeds %d bytes", sessionInputReceiptMaxBytes))
			}
			var ledgerErr error
			ledger, ledgerErr = owner.io.receiptLedger(baseline.paneID)
			if ledgerErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read durable input receipt ledger: %w", ledgerErr))
			}
			if entry, found := ledger.entry(result.Receipt); found {
				if entry.PayloadSHA256 != payloadDigest {
					return definitelyNotSubmitted(result.Receipt, fmt.Errorf("receipt already belongs to different input"))
				}
				result.Outcome = entry.Outcome
				if entry.Outcome == InputAccepted {
					result.Duplicate = true
					currentTurn, exists, turnErr := owner.io.delegatedTurn(baseline.paneID)
					if turnErr != nil {
						return ambiguousSubmission(
							result.Receipt,
							fmt.Errorf("input was already accepted but its delegated turn could not be read: %w", turnErr),
						)
					}
					if exists {
						result.TurnID = currentTurn.ID
					} else if turn != nil {
						return ambiguousSubmission(
							result.Receipt,
							fmt.Errorf("input was already accepted but its durable delegated turn marker is missing"),
						)
					}
					return nil
				}
				if turn != nil {
					currentTurn, exists, turnErr := owner.io.delegatedTurn(baseline.paneID)
					if turnErr != nil {
						return ambiguousSubmission(
							result.Receipt,
							fmt.Errorf("input was already ambiguous and its delegated turn could not be read: %w", turnErr),
						)
					}
					if exists && !delegatedTurnTerminal(currentTurn.Status) {
						result.TurnID = currentTurn.ID
					}
				}
				return ambiguousSubmission(result.Receipt, fmt.Errorf("the prior attempt may already have submitted"))
			}
		}
		buffer := fmt.Sprintf("zen-session-input-%d-%d", os.Getpid(), sessionInputBufferSequence.Add(1))
		if err := owner.io.loadBuffer(buffer, payload); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		defer owner.io.deleteBuffer(buffer)

		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		current := owner.io.pane(sessionID)
		if err := validateSameSessionInputPane(baseline, current); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		var previousTurn delegatedTurnRecord
		previousTurnExists := false
		if turn != nil {
			existing, exists, turnErr := owner.io.delegatedTurn(current.paneID)
			if turnErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read current delegated turn: %w", turnErr))
			}
			previousTurn, previousTurnExists = existing, exists
			if exists && existing.ID != turn.ID && !delegatedTurnTerminal(existing.Status) {
				// A submitted message while work is active is steering for the
				// current turn. Keep its distinct input receipt, but do not
				// create a second lifecycle identity or reset settlement.
				result.TurnID = existing.ID
				turn = nil
			} else {
				result.TurnID = turn.ID
			}
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}

		originalLedger := ledger
		markedLedger := ledger
		if result.Receipt != "" {
			var markErr error
			markedLedger, markErr = ledger.withAmbiguous(result.Receipt, payloadDigest)
			if markErr != nil {
				return definitelyNotSubmitted(result.Receipt, markErr)
			}
			if markErr := owner.persistReceiptLedger(
				current.paneID,
				markedLedger,
				result.Receipt,
				payloadDigest,
				InputAmbiguous,
			); markErr != nil {
				return owner.rollbackReceiptMarker(
					current.paneID,
					originalLedger,
					result.Receipt,
					fmt.Errorf("persist pre-mutation ambiguity: %w", markErr),
				)
			}
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				return owner.rollbackReceiptMarker(current.paneID, originalLedger, result.Receipt, err)
			}
			afterMarker := owner.io.pane(current.paneID)
			if err := validateSameSessionInputPane(current, afterMarker); err != nil {
				return owner.rollbackReceiptMarker(current.paneID, originalLedger, result.Receipt, err)
			}
		}
		if turn != nil {
			preDispatchPane, paneErr := owner.io.paneContent(current.paneID)
			if paneErr != nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("capture pre-dispatch pane baseline: %w", paneErr),
					)
				}
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("capture pre-dispatch pane baseline: %w", paneErr))
			}
			turn.Status = delegatedTurnAmbiguous
			turn.PaneBaseline = delegatedTurnPaneIdentity(preDispatchPane)
			turn.ComposerObserved = false
			if err := owner.io.writeDelegatedTurn(current.paneID, *turn); err != nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("persist delegated turn ambiguity: %w", err),
					)
				}
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("persist delegated turn ambiguity: %w", err))
			}
		}

		adapter := sessionInputProviderForCommand(command)
		var admissionBaseline delegatedAdmissionEvidence
		if requiresConfirmation {
			if confirm.baseline == nil || confirm.confirm == nil {
				if turn != nil {
					if rollbackErr := owner.restoreDelegatedTurn(
						current.paneID,
						previousTurn,
						previousTurnExists,
					); rollbackErr != nil {
						return definitelyNotSubmitted(
							result.Receipt,
							fmt.Errorf("delegated provider admission observer is unavailable; restore delegated turn: %w", rollbackErr),
						)
					}
				}
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("delegated provider admission observer is unavailable"),
					)
				}
				return definitelyNotSubmitted(
					result.Receipt,
					fmt.Errorf("delegated provider admission observer is unavailable"),
				)
			}
			var baselineErr error
			admissionBaseline, baselineErr = confirm.baseline()
			if baselineErr != nil {
				if turn != nil {
					if rollbackErr := owner.restoreDelegatedTurn(
						current.paneID,
						previousTurn,
						previousTurnExists,
					); rollbackErr != nil {
						return definitelyNotSubmitted(
							result.Receipt,
							fmt.Errorf("capture provider admission baseline: %v; restore delegated turn: %w", baselineErr, rollbackErr),
						)
					}
				}
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("capture provider admission baseline: %w", baselineErr),
					)
				}
				return definitelyNotSubmitted(
					result.Receipt,
					fmt.Errorf("capture provider admission baseline: %w", baselineErr),
				)
			}
		}
		mutationBoundary := time.Now().UTC()
		started, queueErr := owner.io.runQueue(sessionInputSubmitQueue(
			current.paneID,
			buffer,
			adapter,
		))
		if queueErr != nil {
			if started {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, fmt.Errorf("run target-bound tmux command queue: %w", queueErr))
			}
			if turn != nil {
				if rollbackErr := owner.restoreDelegatedTurn(
					current.paneID,
					previousTurn,
					previousTurnExists,
				); rollbackErr != nil {
					return definitelyNotSubmitted(
						result.Receipt,
						fmt.Errorf("start target-bound tmux command queue: %v; restore delegated turn: %w", queueErr, rollbackErr),
					)
				}
			}
			if result.Receipt != "" {
				return owner.rollbackReceiptMarker(
					current.paneID,
					originalLedger,
					result.Receipt,
					fmt.Errorf("start target-bound tmux command queue: %w", queueErr),
				)
			}
			return definitelyNotSubmitted(result.Receipt, fmt.Errorf("start target-bound tmux command queue: %w", queueErr))
		}
		if requiresConfirmation {
			confirmation, confirmErr := confirm.confirm(
				admissionBaseline,
				mutationBoundary,
				payloadDigest,
			)
			if confirmErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, confirmErr)
			}
			if confirmation.Outcome != InputAccepted {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("provider turn start was not authoritatively observed"),
				)
			}
			if turn != nil {
				turn.Status = delegatedTurnRunning
				turn.ProviderActivity = strings.TrimSpace(confirmation.ProviderActivity)
			}
		}
		if result.Receipt != "" {
			acceptedLedger, acceptErr := markedLedger.withOutcome(
				result.Receipt,
				payloadDigest,
				InputAccepted,
			)
			if acceptErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, acceptErr)
			}
			if acceptErr := owner.persistReceiptLedger(
				current.paneID,
				acceptedLedger,
				result.Receipt,
				payloadDigest,
				InputAccepted,
			); acceptErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("submit succeeded but acceptance receipt was not confirmed: %w", acceptErr),
				)
			}
		}
		if turn != nil {
			paneContent, paneErr := owner.io.paneContent(current.paneID)
			if paneErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("submit succeeded but post-dispatch pane baseline was not observed: %w", paneErr),
				)
			}
			turn.PaneBaseline = delegatedTurnPaneIdentity(paneContent)
			if err := owner.io.writeDelegatedTurn(current.paneID, *turn); err != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("submit succeeded but delegated turn acceptance was not confirmed: %w", err),
				)
			}
		}
		result.Outcome = InputAccepted
		return nil
	})
	if err != nil {
		result.Outcome = InputOutcomeFromError(err)
	}
	return result, err
}

func (owner *sessionInputOwner) restoreDelegatedTurn(
	target string,
	previous delegatedTurnRecord,
	existed bool,
) error {
	if existed {
		if err := owner.io.writeDelegatedTurn(target, previous); err != nil {
			return err
		}
	} else if err := owner.io.clearDelegatedTurn(target); err != nil {
		return err
	}
	confirmed, found, err := owner.io.delegatedTurn(target)
	if err != nil {
		return err
	}
	if found != existed {
		return fmt.Errorf("delegated turn rollback existence did not match")
	}
	if existed && (confirmed.ID != previous.ID || confirmed.Status != previous.Status) {
		return fmt.Errorf("delegated turn rollback readback did not match")
	}
	return nil
}

func emptySessionInputReceiptLedger() sessionInputReceiptLedger {
	return sessionInputReceiptLedger{
		SchemaVersion: sessionInputReceiptLedgerSchema,
		Entries:       []sessionInputReceiptEntry{},
	}
}

func decodeSessionInputReceiptLedger(value string) (sessionInputReceiptLedger, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return emptySessionInputReceiptLedger(), nil
	}
	var ledger sessionInputReceiptLedger
	if err := json.Unmarshal([]byte(value), &ledger); err != nil {
		return sessionInputReceiptLedger{}, fmt.Errorf("decode Session input receipt ledger: %w", err)
	}
	if err := validateSessionInputReceiptLedger(ledger); err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return ledger, nil
}

func validateSessionInputReceiptLedger(ledger sessionInputReceiptLedger) error {
	if ledger.SchemaVersion != sessionInputReceiptLedgerSchema {
		return fmt.Errorf("unsupported Session input receipt ledger schema %d", ledger.SchemaVersion)
	}
	if len(ledger.Entries) > sessionInputReceiptLedgerLimit {
		return fmt.Errorf("Session input receipt ledger exceeds %d entries", sessionInputReceiptLedgerLimit)
	}
	seen := make(map[string]struct{}, len(ledger.Entries))
	for _, entry := range ledger.Entries {
		if entry.Receipt == "" ||
			len(entry.Receipt) > sessionInputReceiptMaxBytes ||
			!utf8.ValidString(entry.Receipt) {
			return fmt.Errorf("Session input receipt ledger contains an invalid receipt")
		}
		if _, exists := seen[entry.Receipt]; exists {
			return fmt.Errorf("Session input receipt ledger contains duplicate receipt %q", entry.Receipt)
		}
		seen[entry.Receipt] = struct{}{}
		hash, err := hex.DecodeString(entry.PayloadSHA256)
		if err != nil || len(hash) != sha256.Size {
			return fmt.Errorf("Session input receipt %q has an invalid payload hash", entry.Receipt)
		}
		if entry.Outcome != InputAccepted && entry.Outcome != InputAmbiguous {
			return fmt.Errorf("Session input receipt %q has invalid outcome %q", entry.Receipt, entry.Outcome)
		}
	}
	return nil
}

func (ledger sessionInputReceiptLedger) entry(receipt string) (sessionInputReceiptEntry, bool) {
	for _, entry := range ledger.Entries {
		if entry.Receipt == receipt {
			return entry, true
		}
	}
	return sessionInputReceiptEntry{}, false
}

func (ledger sessionInputReceiptLedger) withAmbiguous(
	receipt string,
	payloadHash string,
) (sessionInputReceiptLedger, error) {
	if _, found := ledger.entry(receipt); found {
		return sessionInputReceiptLedger{}, fmt.Errorf("receipt %q already exists", receipt)
	}
	entries := append([]sessionInputReceiptEntry(nil), ledger.Entries...)
	if len(entries) >= sessionInputReceiptLedgerLimit {
		evict := -1
		for index, entry := range entries {
			if entry.Outcome == InputAccepted {
				evict = index
				break
			}
		}
		if evict < 0 {
			return sessionInputReceiptLedger{}, fmt.Errorf(
				"Session input receipt ledger is full of unresolved ambiguous entries",
			)
		}
		entries = append(entries[:evict], entries[evict+1:]...)
	}
	entries = append(entries, sessionInputReceiptEntry{
		Receipt:       receipt,
		PayloadSHA256: payloadHash,
		Outcome:       InputAmbiguous,
	})
	next := sessionInputReceiptLedger{
		SchemaVersion: sessionInputReceiptLedgerSchema,
		Entries:       entries,
	}
	return next, validateSessionInputReceiptLedger(next)
}

func (ledger sessionInputReceiptLedger) withOutcome(
	receipt string,
	payloadHash string,
	outcome InputOutcome,
) (sessionInputReceiptLedger, error) {
	entries := append([]sessionInputReceiptEntry(nil), ledger.Entries...)
	for index := range entries {
		if entries[index].Receipt != receipt {
			continue
		}
		if entries[index].PayloadSHA256 != payloadHash {
			return sessionInputReceiptLedger{}, fmt.Errorf("receipt %q belongs to different input", receipt)
		}
		entries[index].Outcome = outcome
		next := sessionInputReceiptLedger{
			SchemaVersion: sessionInputReceiptLedgerSchema,
			Entries:       entries,
		}
		return next, validateSessionInputReceiptLedger(next)
	}
	return sessionInputReceiptLedger{}, fmt.Errorf("receipt %q is missing from durable ledger", receipt)
}

func (owner *sessionInputOwner) writeAndConfirmReceiptLedger(
	target string,
	ledger sessionInputReceiptLedger,
) error {
	if err := owner.io.writeReceiptLedger(target, ledger); err != nil {
		return err
	}
	confirmed, err := owner.io.receiptLedger(target)
	if err != nil {
		return fmt.Errorf("read back Session input receipt ledger: %w", err)
	}
	if !sessionInputReceiptLedgersEqual(confirmed, ledger) {
		return fmt.Errorf("Session input receipt ledger readback did not match the written state")
	}
	return nil
}

func (owner *sessionInputOwner) persistReceiptLedger(
	target string,
	ledger sessionInputReceiptLedger,
	receipt string,
	payloadHash string,
	outcome InputOutcome,
) error {
	if err := owner.writeAndConfirmReceiptLedger(target, ledger); err != nil {
		return err
	}
	entry, found := ledger.entry(receipt)
	if !found || entry.PayloadSHA256 != payloadHash || entry.Outcome != outcome {
		return fmt.Errorf("Session input receipt %q was not confirmed as %s", receipt, outcome)
	}
	return nil
}

func (owner *sessionInputOwner) rollbackReceiptMarker(
	target string,
	original sessionInputReceiptLedger,
	receipt string,
	cause error,
) error {
	if err := owner.writeAndConfirmReceiptLedger(target, original); err != nil {
		return definitelyNotSubmitted(
			receipt,
			fmt.Errorf("%v; durable ambiguity rollback could not be confirmed: %w", cause, err),
		)
	}
	return definitelyNotSubmitted(receipt, cause)
}

func sessionInputReceiptLedgersEqual(left, right sessionInputReceiptLedger) bool {
	if left.SchemaVersion != right.SchemaVersion || len(left.Entries) != len(right.Entries) {
		return false
	}
	for index := range left.Entries {
		if left.Entries[index] != right.Entries[index] {
			return false
		}
	}
	return true
}

func validateSessionInputPane(pane sessionInputPane) error {
	switch {
	case !pane.alive || pane.paneID == "" || pane.generation == "":
		return fmt.Errorf("target pane generation could not be proven")
	default:
		return nil
	}
}

func validateSameSessionInputPane(expected, current sessionInputPane) error {
	if err := validateSessionInputPane(current); err != nil {
		return err
	}
	if current.paneID != expected.paneID || current.generation != expected.generation {
		return fmt.Errorf("target pane generation changed before mutation")
	}
	return nil
}

func sessionInputSubmitQueue(
	paneID string,
	buffer string,
	adapter sessionInputProvider,
) []string {
	args := []string{
		// Chat is authoritative over an unsent Terminal draft.
		"send-keys", "-t", paneID, "C-u",
	}
	if adapter.prepare > 0 {
		args = append(args,
			";", "run-shell", "sleep "+strconv.FormatFloat(adapter.prepare.Seconds(), 'f', 3, 64),
		)
	}
	args = append(args,
		// -r is part of the payload contract: without it tmux rewrites every
		// LF byte as CR, which terminal composers interpret as submit keys.
		";", "paste-buffer", "-r", "-p", "-b", buffer, "-t", paneID,
	)
	if adapter.settle > 0 {
		args = append(args,
			";", "run-shell", "sleep "+strconv.FormatFloat(adapter.settle.Seconds(), 'f', 3, 64),
		)
	}
	args = append(args, ";", "send-keys", "-t", paneID, adapter.submitKey)
	args = append(args, ";", "delete-buffer", "-b", buffer)
	return args
}

func definitelyNotSubmitted(receipt string, cause error) error {
	return &InputSubmissionError{
		Result: InputResult{Outcome: InputNotSubmitted, Receipt: receipt},
		Cause:  cause,
	}
}

func ambiguousSubmission(receipt string, cause error) error {
	return &InputSubmissionError{
		Result: InputResult{Outcome: InputAmbiguous, Receipt: receipt},
		Cause:  cause,
	}
}

var defaultSessionInputOwner = newSessionInputOwner(realSessionInputIO{})
