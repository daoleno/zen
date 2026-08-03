package watcher

import (
	"crypto/sha256"
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
// Ambiguous means the target-bound tmux queue started and may have submitted;
// callers must retain ownership and must not automatically replay it.
type InputOutcome string

const (
	InputAccepted     InputOutcome = "accepted"
	InputNotSubmitted InputOutcome = "not_submitted"
	InputAmbiguous    InputOutcome = "ambiguous"
)

type InputResult struct {
	Outcome InputOutcome
	Receipt string
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
	settle    time.Duration
}

func sessionInputProviderForCommand(command string) sessionInputProvider {
	adapter := sessionInputProvider{submitKey: "Enter"}
	switch {
	case isCursorAgentCommand(command):
		adapter.settle = 400 * time.Millisecond
	case isGrokCommand(command):
		adapter.settle = 300 * time.Millisecond
	case isClaudeCommand(command):
		adapter.settle = 250 * time.Millisecond
	}
	return adapter
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
	receipt(sessionID string) (string, error)
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

func (realSessionInputIO) receipt(sessionID string) (string, error) {
	return tmuxWindowUserOption(sessionID, sessionInputReceiptOption)
}

type sessionInputSession struct {
	mu sync.Mutex
}

type sessionInputAttempt struct {
	receipt     string
	payloadHash string
	outcome     InputOutcome
}

// sessionInputOwner is the sole serialization owner for every terminal
// provider. It has no provider-specific queue, journal, spool, or resume loop.
type sessionInputOwner struct {
	mu       sync.Mutex
	sessions map[string]*sessionInputSession
	attempts map[string]sessionInputAttempt
	io       sessionInputIO
}

func newSessionInputOwner(io sessionInputIO) *sessionInputOwner {
	if io == nil {
		io = realSessionInputIO{}
	}
	return &sessionInputOwner{
		sessions: make(map[string]*sessionInputSession),
		attempts: make(map[string]sessionInputAttempt),
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

const sessionInputReceiptOption = "zen_session_input_receipt"

var sessionInputBufferSequence atomic.Uint64

func (owner *sessionInputOwner) submit(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	command string,
	payload string,
	receipt string,
) (InputResult, error) {
	result := InputResult{Outcome: InputNotSubmitted, Receipt: strings.TrimSpace(receipt)}
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
		if result.Receipt != "" {
			if attempt, found := owner.attempt(sessionID, result.Receipt); found {
				if attempt.payloadHash != payloadDigest {
					return definitelyNotSubmitted(result.Receipt, fmt.Errorf("receipt already belongs to different input"))
				}
				result.Outcome = attempt.outcome
				if attempt.outcome == InputAccepted {
					return nil
				}
				return ambiguousSubmission(result.Receipt, fmt.Errorf("the prior attempt may already have submitted"))
			}
			stableReceipt, receiptErr := owner.io.receipt(sessionID)
			if receiptErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read stable input receipt: %w", receiptErr))
			}
			if stableReceipt == result.Receipt {
				result.Outcome = InputAccepted
				owner.remember(sessionID, result.Receipt, payloadDigest, InputAccepted)
				return nil
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
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}

		adapter := sessionInputProviderForCommand(command)
		started, queueErr := owner.io.runQueue(sessionInputSubmitQueue(
			current.paneID,
			buffer,
			adapter,
			result.Receipt,
		))
		if queueErr != nil {
			if started {
				result.Outcome = InputAmbiguous
				owner.remember(sessionID, result.Receipt, payloadDigest, InputAmbiguous)
				return ambiguousSubmission(result.Receipt, fmt.Errorf("run target-bound tmux command queue: %w", queueErr))
			}
			return definitelyNotSubmitted(result.Receipt, fmt.Errorf("start target-bound tmux command queue: %w", queueErr))
		}
		result.Outcome = InputAccepted
		owner.remember(sessionID, result.Receipt, payloadDigest, InputAccepted)
		return nil
	})
	if err != nil {
		result.Outcome = InputOutcomeFromError(err)
	}
	return result, err
}

func (owner *sessionInputOwner) attempt(sessionID, receipt string) (sessionInputAttempt, bool) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	attempt, found := owner.attempts[sessionID]
	return attempt, found && attempt.receipt == receipt
}

func (owner *sessionInputOwner) remember(sessionID, receipt, payloadHash string, outcome InputOutcome) {
	if receipt == "" {
		return
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.attempts[sessionID] = sessionInputAttempt{
		receipt:     receipt,
		payloadHash: payloadHash,
		outcome:     outcome,
	}
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
	receipt string,
) []string {
	args := []string{
		// Chat is authoritative over an unsent Terminal draft.
		"send-keys", "-t", paneID, "C-u",
		";", "paste-buffer", "-p", "-b", buffer, "-t", paneID,
	}
	if adapter.settle > 0 {
		args = append(args,
			";", "run-shell", "sleep "+strconv.FormatFloat(adapter.settle.Seconds(), 'f', 3, 64),
		)
	}
	args = append(args, ";", "send-keys", "-t", paneID, adapter.submitKey)
	if receipt != "" {
		// This queue position is after the single submit key.
		args = append(args, ";", "set-option", "-w", "-t", paneID, "@"+sessionInputReceiptOption, receipt)
	}
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
