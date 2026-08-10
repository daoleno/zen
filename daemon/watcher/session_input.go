package watcher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// delegatedTurnDraft is the pre-dispatch turn identity minted by the control
// plane. It is durably admitted to the canonical ledger (persist A) before
// any provider mutation can begin, so a markerless accepted input is
// unrepresentable (C.2 invariant 2).
type delegatedTurnDraft struct {
	WorkID            string
	ID                string
	Receipt           string
	ClaimToken        string
	AcceptedAt        time.Time
	ProcessIdentity   string
	PaneGeneration    string
	TranscriptBinding TranscriptBinding
}

// delegatedAdmissionEvidence is the provider-native admission tuple observed
// before and after the mutation boundary.
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
	Admission        delegatedAdmissionEvidence
}

type delegatedInputBaseline struct {
	Admission delegatedAdmissionEvidence
	Provider  ProviderActivityObservation
}

type delegatedReuseMode uint8

const (
	delegatedReuseFresh delegatedReuseMode = iota
	// delegatedReuseConditionalSteer means the baseline proved the existing
	// activity was running. The candidate must remain durable until post-submit
	// confirmation decides whether the provider kept that activity (steer) or
	// admitted a different activity (fresh canonical turn).
	delegatedReuseConditionalSteer
)

type delegatedReuseDecision struct {
	Mode             delegatedReuseMode
	ExistingTurn     TurnSnapshot
	BaselineActivity string
}

type delegatedInputConfirmer struct {
	baseline func() (delegatedInputBaseline, error)
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
	// socket resolves the target's own tmux server ("" = the user default
	// server). Buffer and queue operations are server-local: the caller
	// threads the target's resolved server so load/paste/cleanup never touch
	// a different server's buffers or panes.
	socket(sessionID string) string
	pane(socket, sessionID string) sessionInputPane
	loadBuffer(socket, buffer, payload string) error
	deleteBuffer(socket, buffer string)
	runQueue(socket string, args []string, beforeStart func() error) (started bool, err error)
	receiptLedger(socket, target string) (sessionInputReceiptLedger, error)
	writeReceiptLedger(socket, target string, ledger sessionInputReceiptLedger) error
	paneContent(socket, target string) (string, error)
}

// realSessionInputIO executes tmux on the target's own server: Zen-owned
// sessions live on the daemon-namespaced socket, user/manual sessions on the
// user's default server. socketFor resolves the per-target server; nil keeps
// the user default (test doubles).
type realSessionInputIO struct {
	socketFor func(sessionID string) string
}

func (io realSessionInputIO) socket(sessionID string) string {
	if io.socketFor != nil {
		return io.socketFor(sessionID)
	}
	return ""
}

func (io realSessionInputIO) pane(socket, sessionID string) sessionInputPane {
	out, err := tmuxCommand(
		socket,
		"display-message",
		"-p",
		"-t",
		sessionID,
		// Immutable pane lifetime only. App Terminal opens a disposable
		// link-window view session; display-message -t %pane_id can therefore
		// report a different session_id/session_created than
		// display-message -t session:window for the same pane. pane_pid and
		// pane_start_command are launch/process metadata, not pane identity.
		// Live provider process identity (PID + start times) remains
		// fail-closed at every mutation boundary via guardTargetIdentity.
		"#{pane_dead}\t#{pane_id}",
	).Output()
	if err != nil {
		return sessionInputPane{}
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 2 || fields[0] == "1" || strings.TrimSpace(fields[1]) == "" {
		return sessionInputPane{}
	}
	paneID := strings.TrimSpace(fields[1])
	return sessionInputPane{
		alive:      true,
		paneID:     paneID,
		generation: sessionInputPaneGeneration(paneID),
	}
}

// sessionInputPaneGeneration is the opaque digest of an immutable tmux pane
// lifetime id. It must not include session_id/session_created (linked App view
// sessions), window linkage metadata that is not pane-exclusive, pane_pid, or
// pane_start_command. Replaced panes get a new pane_id; respawned processes in
// the same pane are rejected by targetProcessIdentity instead.
func sessionInputPaneGeneration(paneID string) string {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(paneID))
	return fmt.Sprintf("%x", digest[:])
}

func (realSessionInputIO) loadBuffer(socket, buffer, payload string) error {
	command := tmuxCommand(socket, "load-buffer", "-b", buffer, "-")
	command.Stdin = strings.NewReader(payload)
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("load payload into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) deleteBuffer(socket, buffer string) {
	_ = tmuxCommand(socket, "delete-buffer", "-b", buffer).Run()
}

func (realSessionInputIO) runQueue(
	socket string,
	args []string,
	beforeStart func() error,
) (bool, error) {
	command := tmuxCommand(socket, args...)
	// This is the last pre-mutation operation: the target-bound tmux command
	// has already been constructed, but Start has not been called. The guard
	// re-proves the provider process lifetime and immutable pane generation.
	// It closes deterministic replacements during baseline/ledger work; it
	// does not pretend an external process can never race after this check.
	if beforeStart != nil {
		if err := beforeStart(); err != nil {
			return false, err
		}
	}
	if err := command.Start(); err != nil {
		return false, err
	}
	if err := command.Wait(); err != nil {
		return true, err
	}
	return true, nil
}

func (io realSessionInputIO) receiptLedger(socket, target string) (sessionInputReceiptLedger, error) {
	value, err := tmuxWindowUserOption(socket, target, sessionInputReceiptOption)
	if err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return decodeSessionInputReceiptLedger(value)
}

func (io realSessionInputIO) writeReceiptLedger(socket, target string, ledger sessionInputReceiptLedger) error {
	if err := validateSessionInputReceiptLedger(ledger); err != nil {
		return err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("encode Session input receipt ledger: %w", err)
	}
	out, err := tmuxCommand(
		socket,
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

func (io realSessionInputIO) paneContent(socket, target string) (string, error) {
	out, err := tmuxCommand(socket, "capture-pane", "-t", target, "-p", "-S", "-200").Output()
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
	ledger   TurnLedger
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
		socket := owner.ioSocket(sessionID)
		if result.Receipt == "" || len(result.Receipt) > sessionInputReceiptMaxBytes ||
			!utf8.ValidString(result.Receipt) {
			return fmt.Errorf("input receipt is invalid or exceeds %d bytes", sessionInputReceiptMaxBytes)
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return err
		}
		baseline := owner.io.pane(socket, sessionID)
		if err := validateSessionInputPane(baseline); err != nil {
			return err
		}
		ledger, err := owner.io.receiptLedger(socket, baseline.paneID)
		if err != nil {
			return fmt.Errorf("read durable input receipt ledger: %w", err)
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return err
		}
		current := owner.io.pane(socket, sessionID)
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
	turn delegatedTurnDraft,
	confirm delegatedInputConfirmer,
) (InputResult, error) {
	receipt := strings.TrimSpace(turn.Receipt)
	if receipt == "" {
		receipt = strings.TrimSpace(turn.ID)
	}
	return owner.submitWithTurn(sessionID, expected, resolver, command, payload, receipt, &turn, confirm)
}

func (owner *sessionInputOwner) submitWithTurn(
	sessionID string,
	expected targetProcessIdentity,
	resolver func(string) (targetProcessIdentity, bool),
	command string,
	payload string,
	receipt string,
	turn *delegatedTurnDraft,
	confirm delegatedInputConfirmer,
) (InputResult, error) {
	result := InputResult{Outcome: InputNotSubmitted, Receipt: strings.TrimSpace(receipt)}
	requiresConfirmation := turn != nil
	if turn != nil {
		result.TurnID = strings.TrimSpace(turn.ID)
	}
	err := owner.serialized(sessionID, func() error {
		socket := owner.ioSocket(sessionID)
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
		baseline := owner.io.pane(socket, sessionID)
		if err := validateSessionInputPane(baseline); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		ledger := emptySessionInputReceiptLedger()
		if result.Receipt != "" {
			if len(result.Receipt) > sessionInputReceiptMaxBytes || !utf8.ValidString(result.Receipt) {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("input receipt is invalid or exceeds %d bytes", sessionInputReceiptMaxBytes))
			}
			var ledgerErr error
			ledger, ledgerErr = owner.io.receiptLedger(socket, baseline.paneID)
			if ledgerErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read durable input receipt ledger: %w", ledgerErr))
			}
		}

		// The Brain ledger, never the tmux receipt, decides the canonical Turn
		// for a delegated duplicate. A pending retry may resolve from exact
		// provider admission evidence, but it never replays provider input.
		if turn != nil {
			submission, found, submissionErr := owner.turnSubmission(sessionID, turn.ID)
			if submissionErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read canonical pending submission: %w", submissionErr))
			}
			if found {
				if submission.Receipt != result.Receipt || submission.PayloadSHA256 != payloadDigest {
					return definitelyNotSubmitted(result.Receipt, fmt.Errorf("submission identity already belongs to different input"))
				}
				result.Duplicate = true
				switch submission.State {
				case TurnSubmissionResolved:
					result.Outcome = InputAccepted
					result.TurnID = submission.ResolvedTurnID
					return nil
				case TurnSubmissionAborted:
					return definitelyNotSubmitted(result.Receipt, fmt.Errorf("the prior submission was permanently aborted before mutation"))
				case TurnSubmissionPending:
					if submission.ProcessIdentity != delegatedTurnIdentity(expected) ||
						submission.PaneGeneration != baseline.generation {
						return ambiguousSubmission(result.Receipt, fmt.Errorf("pending submission target identity no longer matches; input will not be replayed"))
					}
					if _, transportFound := ledger.entry(result.Receipt); transportFound {
						resolved, resolveErr := owner.resolvePendingFromBaseline(submission, confirm)
						if resolveErr == nil {
							result.Outcome = InputAccepted
							result.TurnID = resolved.ResolvedTurnID
							return nil
						}
						return ambiguousSubmission(result.Receipt, resolveErr)
					}
					return ambiguousSubmission(result.Receipt, fmt.Errorf("a ledger-owned pending submission survived restart; input will not be replayed"))
				default:
					return ambiguousSubmission(result.Receipt, fmt.Errorf("pending submission has invalid state %q", submission.State))
				}
			}
		}
		if entry, found := ledger.entry(result.Receipt); found {
			if entry.PayloadSHA256 != payloadDigest {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("receipt already belongs to different input"))
			}
			result.Outcome = entry.Outcome
			if turn != nil {
				return ambiguousSubmission(result.Receipt, fmt.Errorf("transport receipt has no canonical Turn Ledger submission"))
			}
			if entry.Outcome == InputAccepted {
				result.Duplicate = true
				return nil
			}
			return ambiguousSubmission(result.Receipt, fmt.Errorf("the prior attempt may already have submitted"))
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		current := owner.io.pane(socket, sessionID)
		if err := validateSameSessionInputPane(baseline, current); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}

		var admissionBaseline delegatedAdmissionEvidence
		var providerBaseline ProviderActivityObservation
		if requiresConfirmation {
			if confirm.baseline == nil || confirm.confirm == nil {
				return definitelyNotSubmitted(
					result.Receipt,
					fmt.Errorf("delegated provider admission observer is unavailable"),
				)
			}
			captured, baselineErr := confirm.baseline()
			if baselineErr != nil {
				return definitelyNotSubmitted(
					result.Receipt,
					fmt.Errorf("capture provider admission baseline: %w", baselineErr),
				)
			}
			admissionBaseline = captured.Admission
			providerBaseline = captured.Provider
		}
		reuse := delegatedReuseDecision{}
		if turn != nil {
			existing, exists, turnErr := owner.ledgerTurn(sessionID)
			if turnErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("read canonical delegated turn: %w", turnErr))
			}
			if exists && existing.TurnID != turn.ID {
				// Every Session reuse validates the exact provider baseline,
				// including Done/Failed/Unknown ledger rows. Ledger status alone
				// cannot authorize either steering or a fresh mutation.
				var reconcileErr error
				reuse, reconcileErr = owner.reconcileSubmissionActivity(
					sessionID,
					existing,
					providerBaseline,
				)
				if reconcileErr != nil {
					return definitelyNotSubmitted(result.Receipt, reconcileErr)
				}
			} else if exists {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("canonical turn exists without its submission transaction"))
			}
		}

		prepared := false
		if turn != nil {
			mode := TurnSubmissionFresh
			existingTurnID := ""
			baselineActivityID := ""
			if reuse.ExistingTurn.TurnID != "" {
				existingTurnID = reuse.ExistingTurn.TurnID
			}
			if reuse.Mode == delegatedReuseConditionalSteer {
				mode = TurnSubmissionConditionalSteer
				baselineActivityID = reuse.BaselineActivity
			}
			submission, created, prepareErr := owner.prepareTurnSubmission(TurnSubmission{
				WorkID: turn.WorkID, SessionID: sessionID, ProposedTurnID: turn.ID, Receipt: result.Receipt,
				ClaimToken:    turn.ClaimToken,
				PayloadSHA256: payloadDigest, ProcessIdentity: turn.ProcessIdentity,
				PaneGeneration: firstNonEmptyString(turn.PaneGeneration, current.generation),
				AcceptedAt:     turn.AcceptedAt, TranscriptBinding: turn.TranscriptBinding,
				Mode: mode, ExistingTurnID: existingTurnID,
				BaselineActivityID: baselineActivityID,
			})
			if prepareErr != nil {
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("persist pending delegated submission: %w", prepareErr))
			}
			if !created || submission.State != TurnSubmissionPending {
				return ambiguousSubmission(result.Receipt, fmt.Errorf("pending delegated submission was not freshly prepared"))
			}
			prepared = true
		}

		originalLedger := ledger
		markedLedger := ledger
		if result.Receipt != "" {
			var markErr error
			markedLedger, markErr = ledger.withAmbiguous(result.Receipt, payloadDigest)
			if markErr == nil {
				markErr = owner.persistReceiptLedger(socket, current.paneID, markedLedger, result.Receipt, payloadDigest, InputAmbiguous)
			}
			if markErr != nil {
				return owner.abortBeforeMutation(socket, current.paneID, originalLedger, sessionID, result.Receipt, turn, payloadDigest, prepared,
					fmt.Errorf("persist pre-mutation transport receipt: %w", markErr))
			}
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				return owner.abortBeforeMutation(socket, current.paneID, originalLedger, sessionID, result.Receipt, turn, payloadDigest, prepared, err)
			}
			afterMarker := owner.io.pane(socket, current.paneID)
			if err := validateSameSessionInputPane(current, afterMarker); err != nil {
				return owner.abortBeforeMutation(socket, current.paneID, originalLedger, sessionID, result.Receipt, turn, payloadDigest, prepared, err)
			}
		}
		buffer := fmt.Sprintf("zen-session-input-%d-%d", os.Getpid(), sessionInputBufferSequence.Add(1))
		if err := owner.io.loadBuffer(socket, buffer, payload); err != nil {
			return owner.abortBeforeMutation(socket, current.paneID, originalLedger, sessionID, result.Receipt, turn, payloadDigest, prepared, err)
		}
		defer owner.io.deleteBuffer(socket, buffer)

		adapter := sessionInputProviderForCommand(command)
		mutationBoundary := time.Time{}
		started, queueErr := owner.io.runQueue(socket, sessionInputSubmitQueue(
			current.paneID,
			buffer,
			adapter,
		), func() error {
			// Run this inside runQueue immediately before command.Start. The
			// queue is bound to the immutable pane id; the two identity reads
			// bracket the pane-generation proof so deterministic process/pane
			// replacement during provider baseline cannot reach mutation.
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				return err
			}
			boundaryPane := owner.io.pane(socket, current.paneID)
			if err := validateSameSessionInputPane(current, boundaryPane); err != nil {
				return err
			}
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				return err
			}
			mutationBoundary = time.Now().UTC()
			return nil
		})
		if queueErr != nil {
			if started {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, fmt.Errorf("run target-bound tmux command queue: %w", queueErr))
			}
			return owner.abortBeforeMutation(socket, current.paneID, originalLedger, sessionID, result.Receipt, turn, payloadDigest, prepared,
				fmt.Errorf("start target-bound tmux command queue: %w", queueErr))
		}
		var confirmation delegatedInputConfirmation
		if requiresConfirmation {
			var confirmErr error
			confirmation, confirmErr = confirm.confirm(
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
			// Mutation has begun, so a target replacement is Ambiguous. Never
			// let admission from a replacement process/pane claim the pending
			// transaction that was bound before mutation.
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, err)
			}
			confirmedPane := owner.io.pane(socket, current.paneID)
			if err := validateSameSessionInputPane(current, confirmedPane); err != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, err)
			}
		}

		if turn != nil {
			resolved, resolveErr := owner.resolveTurnSubmission(TurnSubmissionResolution{
				SessionID: sessionID, ProposedTurnID: turn.ID, Receipt: result.Receipt,
				PayloadSHA256: payloadDigest,
				ActivityID:    strings.TrimSpace(confirmation.ProviderActivity),
				Admission: TurnAdmission{
					Stream: strings.TrimSpace(confirmation.Admission.Stream),
					ID:     strings.TrimSpace(confirmation.Admission.ID),
					Cursor: confirmation.Admission.Cursor,
					SHA256: strings.TrimSpace(confirmation.Admission.InputSHA256),
					At:     confirmation.Admission.StartedAt.UTC(),
				},
				ResolvedAt: time.Now().UTC(),
			})
			if resolveErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, fmt.Errorf("provider accepted input but canonical submission did not resolve: %w", resolveErr))
			}
			result.TurnID = resolved.ResolvedTurnID
		}
		if result.Receipt != "" {
			acceptedLedger, acceptErr := markedLedger.withOutcome(result.Receipt, payloadDigest, InputAccepted)
			if acceptErr != nil {
				if turn == nil {
					result.Outcome = InputAmbiguous
					return ambiguousSubmission(result.Receipt, acceptErr)
				}
			} else if acceptErr := owner.persistReceiptLedger(
				socket, current.paneID, acceptedLedger, result.Receipt, payloadDigest, InputAccepted,
			); acceptErr != nil && turn == nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, fmt.Errorf("submit succeeded but acceptance receipt was not confirmed: %w", acceptErr))
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

// ioSocket resolves the target's tmux server for the Session input IO
// through the owner's IO implementation.
func (owner *sessionInputOwner) ioSocket(sessionID string) string {
	if owner == nil || owner.io == nil {
		return ""
	}
	return owner.io.socket(sessionID)
}

// ledgerTurn reads the current canonical turn for the session through the
// injected ledger; sessions without a ledger have no canonical turn.
func (owner *sessionInputOwner) ledgerTurn(sessionID string) (TurnSnapshot, bool, error) {
	if owner == nil || owner.ledger == nil {
		return TurnSnapshot{}, false, nil
	}
	return owner.ledger.Turn(sessionID)
}

func (owner *sessionInputOwner) submissionLedger() (TurnSubmissionLedger, error) {
	if owner == nil || owner.ledger == nil {
		return nil, fmt.Errorf("canonical turn ledger is unavailable")
	}
	ledger, ok := owner.ledger.(TurnSubmissionLedger)
	if !ok {
		return nil, fmt.Errorf("canonical turn ledger cannot own pending submissions")
	}
	return ledger, nil
}

func (owner *sessionInputOwner) prepareTurnSubmission(submission TurnSubmission) (TurnSubmission, bool, error) {
	ledger, err := owner.submissionLedger()
	if err != nil {
		return TurnSubmission{}, false, err
	}
	return ledger.PrepareTurnSubmission(submission)
}

func (owner *sessionInputOwner) turnSubmission(sessionID, proposedTurnID string) (TurnSubmission, bool, error) {
	ledger, err := owner.submissionLedger()
	if err != nil {
		return TurnSubmission{}, false, err
	}
	return ledger.TurnSubmission(sessionID, proposedTurnID)
}

func (owner *sessionInputOwner) resolveTurnSubmission(resolution TurnSubmissionResolution) (TurnSubmission, error) {
	ledger, err := owner.submissionLedger()
	if err != nil {
		return TurnSubmission{}, err
	}
	return ledger.ResolveTurnSubmission(resolution)
}

func (owner *sessionInputOwner) abortBeforeMutation(
	socket, target string,
	original sessionInputReceiptLedger,
	sessionID string,
	receipt string,
	turn *delegatedTurnDraft,
	payloadDigest string,
	prepared bool,
	cause error,
) error {
	if prepared && turn != nil {
		ledger, err := owner.submissionLedger()
		if err != nil {
			return ambiguousSubmission(receipt, fmt.Errorf("%v; abort canonical pending submission: %w", cause, err))
		}
		if _, err := ledger.AbortTurnSubmission(sessionID, turn.ID, receipt, payloadDigest); err != nil {
			return ambiguousSubmission(receipt, fmt.Errorf("%v; abort canonical pending submission: %w", cause, err))
		}
	}
	if receipt != "" {
		if err := owner.writeAndConfirmReceiptLedger(socket, target, original); err != nil {
			return definitelyNotSubmitted(receipt, fmt.Errorf("%v; transport receipt rollback was not confirmed: %w", cause, err))
		}
	}
	return definitelyNotSubmitted(receipt, cause)
}

func (owner *sessionInputOwner) resolvePendingFromBaseline(
	submission TurnSubmission,
	confirm delegatedInputConfirmer,
) (TurnSubmission, error) {
	if confirm.baseline == nil {
		return TurnSubmission{}, fmt.Errorf("provider admission observer is unavailable for pending reconciliation")
	}
	baseline, err := confirm.baseline()
	if err != nil {
		return TurnSubmission{}, fmt.Errorf("observe pending provider admission: %w", err)
	}
	provider := baseline.Provider
	switch strings.TrimSpace(provider.Status) {
	case "running", "completed", "failed", "interrupted", "cancelled":
	default:
		return TurnSubmission{}, fmt.Errorf("pending provider admission has no authoritative lifecycle status")
	}
	admission := baseline.Admission
	if strings.TrimSpace(admission.InputSHA256) != submission.PayloadSHA256 {
		return TurnSubmission{}, fmt.Errorf("provider admission digest does not match the pending payload; ownership was rejected")
	}
	return owner.resolveTurnSubmission(TurnSubmissionResolution{
		SessionID: submission.SessionID, ProposedTurnID: submission.ProposedTurnID,
		Receipt: submission.Receipt, PayloadSHA256: submission.PayloadSHA256,
		ActivityID: strings.TrimSpace(provider.ID),
		Admission: TurnAdmission{
			Stream: strings.TrimSpace(admission.Stream), ID: strings.TrimSpace(admission.ID),
			Cursor: admission.Cursor, SHA256: strings.TrimSpace(admission.InputSHA256),
			At: admission.StartedAt.UTC(),
		},
		ResolvedAt: time.Now().UTC(),
	})
}

// reconcileSubmissionActivity validates every reuse whose proposed TurnID is
// different from the current ledger row. The decision is made from
// authoritative provider activity, never from the possibly stale ledger
// status alone:
//
//   - an exact bound running activity makes steering conditional on the
//     post-mutation confirmed ActivityID;
//   - an exact bound terminal is applied through the canonical reducer, after
//     which a fresh candidate may be prepared without replacing that Turn.
//
// Missing, mismatched, or non-lifecycle evidence fails closed before the tmux
// mutation boundary.
func (owner *sessionInputOwner) reconcileSubmissionActivity(
	sessionID string,
	turn TurnSnapshot,
	provider ProviderActivityObservation,
) (delegatedReuseDecision, error) {
	decision := delegatedReuseDecision{ExistingTurn: turn}
	if owner == nil || owner.ledger == nil {
		return decision, fmt.Errorf("canonical turn ledger is unavailable")
	}
	// Historical terminal metadata is valid for poll-time reconciliation, but
	// it cannot authorize a new input transaction while the provider's current
	// projection is a different activity. Reuse must bind the baseline itself.
	if !providerObservationCanBindTurn(turn, provider) {
		return decision, fmt.Errorf(
			"current provider activity is not authoritatively bound to canonical turn %s",
			turn.TurnID,
		)
	}
	fact := activityFactFromObservation(sessionID, turn, provider)
	if fact == nil {
		return decision, fmt.Errorf("current provider activity has no authoritative lifecycle status")
	}
	if strings.TrimSpace(provider.Status) == "running" && TurnTerminal(turn.Status) {
		return decision, fmt.Errorf(
			"terminal canonical turn %s cannot be reused from a running provider baseline",
			turn.TurnID,
		)
	}
	snapshot, _, err := owner.ledger.ApplyTurnFact(*fact)
	if err != nil {
		return decision, fmt.Errorf("reconcile canonical provider activity: %w", err)
	}
	switch strings.TrimSpace(provider.Status) {
	case "running":
		if snapshot.TurnID != turn.TurnID || TurnTerminal(snapshot.Status) ||
			strings.TrimSpace(snapshot.ActivityID) != strings.TrimSpace(provider.ID) {
			return decision, fmt.Errorf("provider running activity did not bind the canonical turn")
		}
		decision.Mode = delegatedReuseConditionalSteer
		decision.ExistingTurn = snapshot
		decision.BaselineActivity = strings.TrimSpace(provider.ID)
		return decision, nil
	case "completed":
		if snapshot.Status != TurnDone {
			return decision, fmt.Errorf("provider completion did not settle the canonical turn")
		}
		decision.ExistingTurn = snapshot
		return decision, nil
	case "failed", "interrupted", "cancelled":
		if snapshot.Status != TurnFailed {
			return decision, fmt.Errorf("provider failure did not settle the canonical turn")
		}
		decision.ExistingTurn = snapshot
		return decision, nil
	default:
		return decision, fmt.Errorf("current provider activity has no authoritative lifecycle status")
	}
}

func providerObservationCanBindTurn(
	turn TurnSnapshot,
	provider ProviderActivityObservation,
) bool {
	activityID := strings.TrimSpace(provider.ID)
	if recorded := strings.TrimSpace(turn.ActivityID); recorded != "" {
		return activityID == recorded
	}
	admission := admissionFromObservation(provider)
	if !turn.Admission.Empty() {
		return admission.Stream == turn.Admission.Stream &&
			strings.TrimSpace(admission.ID) != "" &&
			admission.Cursor >= turn.Admission.Cursor &&
			(strings.TrimSpace(turn.Admission.SHA256) == "" ||
				strings.TrimSpace(admission.SHA256) == turn.Admission.SHA256)
	}
	if turn.Status != TurnAdmitted && turn.Status != TurnAccepted {
		return false
	}
	if provider.StartedAt.IsZero() || provider.StartedAt.Before(turn.AcceptedAt) {
		return false
	}
	return !admission.Empty() || activityID != ""
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
	socket, target string,
	ledger sessionInputReceiptLedger,
) error {
	if err := owner.io.writeReceiptLedger(socket, target, ledger); err != nil {
		return err
	}
	confirmed, err := owner.io.receiptLedger(socket, target)
	if err != nil {
		return fmt.Errorf("read back Session input receipt ledger: %w", err)
	}
	if !sessionInputReceiptLedgersEqual(confirmed, ledger) {
		return fmt.Errorf("Session input receipt ledger readback did not match the written state")
	}
	return nil
}

func (owner *sessionInputOwner) persistReceiptLedger(
	socket, target string,
	ledger sessionInputReceiptLedger,
	receipt string,
	payloadHash string,
	outcome InputOutcome,
) error {
	if err := owner.writeAndConfirmReceiptLedger(socket, target, ledger); err != nil {
		return err
	}
	entry, found := ledger.entry(receipt)
	if !found || entry.PayloadSHA256 != payloadHash || entry.Outcome != outcome {
		return fmt.Errorf("Session input receipt %q was not confirmed as %s", receipt, outcome)
	}
	return nil
}

func (owner *sessionInputOwner) rollbackReceiptMarker(
	socket, target string,
	original sessionInputReceiptLedger,
	receipt string,
	cause error,
) error {
	if err := owner.writeAndConfirmReceiptLedger(socket, target, original); err != nil {
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

// ErrAgentInputNotReady is the retryable readiness outcome of an initial
// handoff: the exact spawned target identity is still attributable, but the
// provider input surface had not reached the adapter's ready evidence when the
// bounded wait expired. The input was definitely not submitted, so a caller may
// retry within the same occurrence. Any other definitely-not-submitted outcome
// (unprovable identity, pane replacement) is terminal.
var ErrAgentInputNotReady = errors.New("agent input not ready")

func agentInputNotReady(command string) error {
	return definitelyNotSubmitted("", fmt.Errorf("%w for %q", ErrAgentInputNotReady, command))
}

func ambiguousSubmission(receipt string, cause error) error {
	return &InputSubmissionError{
		Result: InputResult{Outcome: InputAmbiguous, Receipt: receipt},
		Cause:  cause,
	}
}

var defaultSessionInputOwner = newSessionInputOwner(realSessionInputIO{})
