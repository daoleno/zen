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
	ID                string
	AcceptedAt        time.Time
	ProcessIdentity   string
	PaneGeneration    string
	Receipt           string
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
	// TurnID permanently binds a delegated receipt to the lifecycle identity
	// that actually accepted it. Ambiguous pre-mutation markers carry the
	// fresh candidate ID so an uncertain send never loses its candidate across
	// owner restart; accepted steering rewrites it to the existing TurnID.
	TurnID string `json:"turn_id,omitempty"`
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
		result.TurnID = strings.TrimSpace(entry.TurnID)
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
	return owner.submitWithTurn(sessionID, expected, resolver, command, payload, turn.ID, &turn, confirm)
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
			if entry, found := ledger.entry(result.Receipt); found {
				if entry.PayloadSHA256 != payloadDigest {
					return definitelyNotSubmitted(result.Receipt, fmt.Errorf("receipt already belongs to different input"))
				}
				result.Outcome = entry.Outcome
				result.TurnID = strings.TrimSpace(entry.TurnID)
				if entry.Outcome == InputAccepted {
					if turn != nil && result.TurnID == "" {
						return ambiguousSubmission(
							result.Receipt,
							fmt.Errorf("input was already accepted but its original canonical turn identity is missing"),
						)
					}
					result.Duplicate = true
					return nil
				}
				return ambiguousSubmission(result.Receipt, fmt.Errorf("the prior attempt may already have submitted"))
			}
		}
		buffer := fmt.Sprintf("zen-session-input-%d-%d", os.Getpid(), sessionInputBufferSequence.Add(1))
		if err := owner.io.loadBuffer(socket, buffer, payload); err != nil {
			return definitelyNotSubmitted(result.Receipt, err)
		}
		defer owner.io.deleteBuffer(socket, buffer)

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

		originalLedger := ledger
		markedLedger := ledger
		if result.Receipt != "" {
			var markErr error
			markedLedger, markErr = ledger.withAmbiguous(
				result.Receipt,
				payloadDigest,
				result.TurnID,
			)
			if markErr != nil {
				return definitelyNotSubmitted(result.Receipt, markErr)
			}
			if markErr := owner.persistReceiptLedger(
				socket,
				current.paneID,
				markedLedger,
				result.Receipt,
				payloadDigest,
				InputAmbiguous,
			); markErr != nil {
				return owner.rollbackReceiptMarker(
					socket,
					current.paneID,
					originalLedger,
					result.Receipt,
					fmt.Errorf("persist pre-mutation ambiguity: %w", markErr),
				)
			}
			if err := guardTargetIdentity(resolver, sessionID, expected); err != nil {
				return owner.rollbackReceiptMarker(socket, current.paneID, originalLedger, result.Receipt, err)
			}
			afterMarker := owner.io.pane(socket, current.paneID)
			if err := validateSameSessionInputPane(current, afterMarker); err != nil {
				return owner.rollbackReceiptMarker(socket, current.paneID, originalLedger, result.Receipt, err)
			}
		}
		var admissionBaseline delegatedAdmissionEvidence
		var providerBaseline ProviderActivityObservation
		if requiresConfirmation {
			if confirm.baseline == nil || confirm.confirm == nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						socket,
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
			captured, baselineErr := confirm.baseline()
			if baselineErr != nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						socket,
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
			admissionBaseline = captured.Admission
			providerBaseline = captured.Provider
		}
		reuse := delegatedReuseDecision{}
		admitBeforeMutation := turn != nil
		if turn != nil {
			existing, exists, turnErr := owner.ledgerTurn(sessionID)
			if turnErr != nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						socket,
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("read canonical delegated turn: %w", turnErr),
					)
				}
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
					if result.Receipt != "" {
						return owner.rollbackReceiptMarker(
							socket,
							current.paneID,
							originalLedger,
							result.Receipt,
							reconcileErr,
						)
					}
					return definitelyNotSubmitted(result.Receipt, reconcileErr)
				}
				if reuse.Mode == delegatedReuseConditionalSteer {
					// The ambiguity receipt already durably carries the fresh
					// candidate. Do not replace the live canonical row until the
					// post-mutation admission says the provider created a different
					// activity; equally, do not discard the candidate yet.
					admitBeforeMutation = false
				}
			}
		}
		if admitBeforeMutation {
			// Persist A: the canonical Admitted record is durable before the
			// submit queue runs. Conditional steering is the sole exception:
			// its fresh candidate is carried by the durable ambiguity receipt
			// until post-submit activity identity decides which Turn owns it.
			admitErr := owner.admitDelegatedTurn(
				sessionID,
				*turn,
				current.generation,
				payloadDigest,
				result.Receipt,
			)
			if admitErr != nil {
				if result.Receipt != "" {
					return owner.rollbackReceiptMarker(
						socket,
						current.paneID,
						originalLedger,
						result.Receipt,
						fmt.Errorf("persist delegated turn admission: %w", admitErr),
					)
				}
				return definitelyNotSubmitted(result.Receipt, fmt.Errorf("persist delegated turn admission: %w", admitErr))
			}
		}

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
			if result.Receipt != "" {
				return owner.rollbackReceiptMarker(
					socket,
					current.paneID,
					originalLedger,
					result.Receipt,
					fmt.Errorf("start target-bound tmux command queue: %w", queueErr),
				)
			}
			return definitelyNotSubmitted(result.Receipt, fmt.Errorf("start target-bound tmux command queue: %w", queueErr))
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
		}

		acceptedTurn := turn
		deliveryTurnID := result.TurnID
		if reuse.Mode == delegatedReuseConditionalSteer {
			confirmedActivity := strings.TrimSpace(confirmation.ProviderActivity)
			if confirmedActivity == "" {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("provider admission did not identify the activity that accepted the input"),
				)
			}
			if confirmedActivity == reuse.BaselineActivity {
				// Same exact provider activity: the input is steering for the
				// existing turn. Only now may the result/receipt adopt that TurnID.
				acceptedTurn = nil
				deliveryTurnID = reuse.ExistingTurn.TurnID
			} else {
				// The provider finished/advanced between baseline and admission.
				// The ambiguity receipt already preserves this candidate across
				// restart; admit it now and bind the newly confirmed activity.
				if admitErr := owner.admitDelegatedTurn(
					sessionID,
					*turn,
					current.generation,
					payloadDigest,
					result.Receipt,
				); admitErr != nil {
					result.Outcome = InputAmbiguous
					return ambiguousSubmission(
						result.Receipt,
						fmt.Errorf("provider admitted a fresh activity but its canonical turn could not be persisted: %w", admitErr),
					)
				}
				deliveryTurnID = turn.ID
			}
		}
		result.TurnID = strings.TrimSpace(deliveryTurnID)

		if acceptedTurn != nil && confirmation.Outcome == InputAccepted {
			// Persist B: the correlated admission tuple promotes the durable
			// Admitted record to Accepted. The receipt state is part of the
			// deterministic FactID, so ambiguous → accepted promotion is a
			// distinct fact and same-state retries dedupe.
			admission := confirmation.Admission
			receipt := firstNonEmptyString(acceptedTurn.Receipt, result.Receipt)
			if receipt == "" {
				receipt = acceptedTurn.ID
			}
			_, _, applyErr := owner.ledger.ApplyTurnFact(TurnFact{
				SessionID: sessionID,
				TurnID:    acceptedTurn.ID,
				Class:     EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + receipt + "\x00accepted\x00" + payloadDigest,
				Admission: TurnAdmission{
					Stream: strings.TrimSpace(admission.Stream),
					ID:     strings.TrimSpace(admission.ID),
					Cursor: admission.Cursor,
					SHA256: strings.TrimSpace(admission.InputSHA256),
					At:     admission.StartedAt.UTC(),
				},
				ActivityID: strings.TrimSpace(confirmation.ProviderActivity),
				At:         time.Now().UTC(),
				Summary:    "Delegated input accepted",
			})
			if applyErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(
					result.Receipt,
					fmt.Errorf("submit succeeded but canonical turn acceptance was not confirmed: %w", applyErr),
				)
			}
		}
		if result.Receipt != "" {
			acceptedLedger, acceptErr := markedLedger.withOutcome(
				result.Receipt,
				payloadDigest,
				InputAccepted,
				result.TurnID,
			)
			if acceptErr != nil {
				result.Outcome = InputAmbiguous
				return ambiguousSubmission(result.Receipt, acceptErr)
			}
			if acceptErr := owner.persistReceiptLedger(
				socket,
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

func (owner *sessionInputOwner) admitDelegatedTurn(
	sessionID string,
	turn delegatedTurnDraft,
	paneGeneration string,
	payloadDigest string,
	receipt string,
) error {
	if owner == nil || owner.ledger == nil {
		return fmt.Errorf("canonical turn ledger is unavailable")
	}
	admitter, ok := owner.ledger.(TurnLedgerAdmitter)
	if !ok {
		return fmt.Errorf("canonical turn ledger cannot durably admit turns")
	}
	return admitter.AdmitTurn(AdmittedTurn{
		SessionID:         sessionID,
		TurnID:            turn.ID,
		Receipt:           firstNonEmptyString(turn.Receipt, receipt),
		AcceptedAt:        turn.AcceptedAt,
		ProcessIdentity:   turn.ProcessIdentity,
		PaneGeneration:    firstNonEmptyString(turn.PaneGeneration, paneGeneration),
		PayloadSHA256:     payloadDigest,
		TranscriptBinding: turn.TranscriptBinding,
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
//     which the pending submission owns its freshly minted turn.
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
		if len(entry.TurnID) > sessionInputReceiptMaxBytes || !utf8.ValidString(entry.TurnID) {
			return fmt.Errorf("Session input receipt %q has an invalid turn id", entry.Receipt)
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
	turnID string,
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
		TurnID:        strings.TrimSpace(turnID),
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
	turnID string,
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
		entries[index].TurnID = strings.TrimSpace(turnID)
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
