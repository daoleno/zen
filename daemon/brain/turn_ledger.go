package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/google/uuid"
)

// TurnRecord is the provider-facing read model for an accepted delegated turn
// inside presentation.json. Lifecycle State is the sole Work/Attempt/Event
// authority; these rows support transcript and Session presentation only and
// are rebuilt from accepted admissions after restart.
type TurnRecord struct {
	SessionID              string                   `json:"session_id"`
	TurnID                 string                   `json:"turn_id"`
	WorkID                 string                   `json:"work_id"`
	Status                 watcher.TurnStatus       `json:"status"`
	Receipt                string                   `json:"receipt,omitempty"`
	PaneGeneration         string                   `json:"pane_generation,omitempty"`
	ProcessIdentity        string                   `json:"process_identity,omitempty"`
	PayloadSHA256          string                   `json:"payload_sha256,omitempty"`
	Admission              watcher.TurnAdmission    `json:"admission,omitempty"`
	ActivityID             string                   `json:"activity_id,omitempty"`
	QueuedBehindActivityID string                   `json:"queued_behind_activity_id,omitempty"`
	Attention              string                   `json:"attention,omitempty"`
	ControlState           watcher.TurnControlState `json:"control_state,omitempty"`
	AcceptedAt             time.Time                `json:"accepted_at,omitempty"`
	SettledAt              *time.Time               `json:"settled_at,omitempty"`
	Summary                string                   `json:"summary,omitempty"`
	Facts                  []TurnFactRecord         `json:"facts"`
	Hints                  []watcher.TurnHint       `json:"hints,omitempty"`
	// TranscriptBinding is the provider-native transcript identity recorded
	// at admission (Pi owned session flag/path), restored on rediscovery;
	// the equivalent tmux option is only an advisory cache.
	TranscriptBinding watcher.TranscriptBinding `json:"transcript_binding,omitempty"`
	// SignalProtocol is true only when this Turn's random identity was carried
	// in its delegated prompt. It is an authority marker, not another identity.
	SignalProtocol bool `json:"signal_protocol,omitempty"`
	HostHandling   bool `json:"host_handling,omitempty"`
	// LeaseDeadline is the turn's own expected-next-check time (per-turn
	// liveness): minted fresh at admission, extended only by this turn's
	// Control lease facts, and the sole basis for session.stale. An old turn's
	// expired lease can never make a newer turn stale.
	LeaseDeadline time.Time `json:"lease_deadline,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type turnReadCache struct {
	valid   bool
	size    int64
	modTime time.Time
	current map[string]TurnRecord
	exact   map[string]TurnRecord
}

// TurnFactRecord is one durable applied observation on a turn. FactID is the
// deterministic frozen identity (watcher.TurnFactID), so replayed and
// reordered facts dedupe identically across restart.
type TurnFactRecord struct {
	FactID  string                `json:"fact_id"`
	Kind    string                `json:"kind"`
	Class   watcher.EvidenceClass `json:"evidence_class"`
	Bound   bool                  `json:"bound,omitempty"`
	At      time.Time             `json:"at,omitempty"`
	Summary string                `json:"summary,omitempty"`
}

func validTurnStatus(status watcher.TurnStatus) bool {
	switch status {
	case watcher.TurnAdmitted, watcher.TurnAccepted, watcher.TurnRunning,
		watcher.TurnBlocked, watcher.TurnDone, watcher.TurnFailed, watcher.TurnUnknown:
		return true
	default:
		return false
	}
}

func validEvidenceClass(class watcher.EvidenceClass) bool {
	switch class {
	case watcher.EvidenceAbsent, watcher.EvidencePane,
		watcher.EvidenceControl, watcher.EvidenceReceipt, watcher.EvidenceLiveness,
		watcher.EvidenceProvider:
		return true
	default:
		return false
	}
}

func validateTurnLedger(turns []TurnRecord, workIDs map[string]struct{}) error {
	turnKeys := make(map[string]struct{}, len(turns))
	for index, turn := range turns {
		if err := validateTurnRecord(turn); err != nil {
			return fmt.Errorf("brain_turns[%d]: %w", index, err)
		}
		if _, exists := workIDs[turn.WorkID]; !exists {
			return fmt.Errorf("brain_turns[%d]: unknown work_id %q", index, turn.WorkID)
		}
		key := turn.SessionID + "\x00" + turn.TurnID
		if _, exists := turnKeys[key]; exists {
			return fmt.Errorf("brain_turns[%d]: duplicate session_id/turn_id %q/%q", index, turn.SessionID, turn.TurnID)
		}
		turnKeys[key] = struct{}{}
		factIDs := make(map[string]struct{}, len(turn.Facts))
		for factIndex, fact := range turn.Facts {
			if strings.TrimSpace(fact.FactID) == "" || strings.TrimSpace(fact.Kind) == "" {
				return fmt.Errorf("brain_turns[%d].facts[%d]: fact_id and kind are required", index, factIndex)
			}
			if !validEvidenceClass(fact.Class) {
				return fmt.Errorf("brain_turns[%d].facts[%d]: invalid evidence class %q", index, factIndex, fact.Class)
			}
			if _, exists := factIDs[fact.FactID]; exists {
				return fmt.Errorf("brain_turns[%d].facts[%d]: duplicate fact_id %q", index, factIndex, fact.FactID)
			}
			factIDs[fact.FactID] = struct{}{}
		}
		hintKinds := make(map[string]struct{}, len(turn.Hints))
		for hintIndex, hint := range turn.Hints {
			if !validEvidenceClass(hint.Class) {
				return fmt.Errorf("brain_turns[%d].hints[%d]: invalid evidence class %q", index, hintIndex, hint.Class)
			}
			if _, exists := hintKinds[hint.Kind]; exists {
				return fmt.Errorf("brain_turns[%d].hints[%d]: duplicate hint kind %q", index, hintIndex, hint.Kind)
			}
			hintKinds[hint.Kind] = struct{}{}
		}
	}
	return nil
}

func validateTurnRecord(turn TurnRecord) error {
	if strings.TrimSpace(turn.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(turn.TurnID) == "" {
		return fmt.Errorf("turn_id is required")
	}
	if strings.TrimSpace(turn.WorkID) == "" {
		return fmt.Errorf("work_id is required")
	}
	if !validTurnStatus(turn.Status) {
		return fmt.Errorf("invalid status %q", turn.Status)
	}
	if turn.ControlState != watcher.TurnControlOwned &&
		turn.ControlState != watcher.TurnControlOwnershipLost {
		return fmt.Errorf("invalid control_state %q", turn.ControlState)
	}
	if turn.SignalProtocol {
		if turn.Receipt != turn.TurnID || len(turn.PayloadSHA256) != sha256.Size*2 {
			return fmt.Errorf("signal-protocol Turn requires its exact receipt identity and payload digest")
		}
		if _, err := hex.DecodeString(turn.PayloadSHA256); err != nil {
			return fmt.Errorf("signal-protocol Turn requires a SHA-256 payload digest")
		}
	}
	if turn.AcceptedAt.IsZero() || turn.UpdatedAt.IsZero() {
		return fmt.Errorf("accepted_at and updated_at are required")
	}
	switch turn.Status {
	case watcher.TurnDone, watcher.TurnFailed, watcher.TurnUnknown:
		if turn.SettledAt == nil {
			return fmt.Errorf("terminal turn requires settled_at")
		}
	}
	for _, fact := range turn.Facts {
		if fact.Class == watcher.EvidenceAbsent && strings.TrimSpace(fact.FactID) != "" {
			return fmt.Errorf("absent-class facts cannot be applied")
		}
	}
	if !turn.TranscriptBinding.Empty() {
		if strings.TrimSpace(turn.TranscriptBinding.Provider) != "pi" ||
			(turn.TranscriptBinding.PiFlag != "--session" && turn.TranscriptBinding.PiFlag != "--session-dir") ||
			!filepath.IsAbs(turn.TranscriptBinding.PiPath) {
			return fmt.Errorf("invalid transcript binding")
		}
	}
	return nil
}

func (t TurnRecord) snapshot() watcher.TurnSnapshot {
	snapshot := watcher.TurnSnapshot{
		SessionID:              t.SessionID,
		TurnID:                 t.TurnID,
		Status:                 t.Status,
		AcceptedAt:             t.AcceptedAt,
		SettledAt:              cloneTimePointer(t.SettledAt),
		Summary:                t.Summary,
		Attention:              t.Attention,
		ControlState:           t.ControlState,
		ActivityID:             t.ActivityID,
		QueuedBehindActivityID: t.QueuedBehindActivityID,
		Admission:              t.Admission,
		HasAdmission:           !t.Admission.Empty(),
		Hints:                  append([]watcher.TurnHint(nil), t.Hints...),
		PaneGeneration:         t.PaneGeneration,
		ProcessIdentity:        t.ProcessIdentity,
		TranscriptBinding:      t.TranscriptBinding,
		LeaseDeadline:          t.LeaseDeadline,
		SignalProtocol:         t.SignalProtocol,
		UpdatedAt:              t.UpdatedAt,
	}
	return snapshot
}

// turnLeaseGrace is the fresh per-turn liveness minted at admission: the
// turn's own expected-next-check deadline before its first progress lease
// arrives. It is per-turn, so a newly admitted turn can never inherit an old
// turn's expired lease (the false-stale incident).
const turnLeaseGrace = 10 * time.Minute

// PrepareInputAdmission persists one exact pre-mutation input transaction
// for the current provider pane/process generation. A pending row is a
// transaction, not a Session or generation mutex: several distinct pending
// transactions may coexist for the same Session and provider generation, and
// each resolves, aborts, or retires only from its own exact evidence. An
// ambiguous older transaction never blocks a newer one. The method never
// appends to brain_turns and therefore cannot replace the current running
// Turn. The provider baseline already classified by the watcher is rechecked
// against the authoritative current row under the Store lock.
func (s *Store) PrepareInputAdmission(candidate watcher.InputAdmission) (watcher.InputAdmission, bool, error) {
	if s == nil || s.fsm == nil {
		return watcher.InputAdmission{}, false, fmt.Errorf("brain store is not configured")
	}
	candidate = normalizeInputAdmission(candidate)
	if err := validateInputAdmissionCandidate(candidate); err != nil {
		return watcher.InputAdmission{}, false, err
	}
	st, err := s.fsmStateForAdmission(candidate)
	if err != nil {
		return watcher.InputAdmission{}, false, err
	}
	mode := lifecycle.AdmissionFresh
	if candidate.Mode == watcher.InputAdmissionConditionalSteer {
		mode = lifecycle.AdmissionConditionalSteer
	}
	purpose := lifecycle.AdmissionPurpose(strings.TrimSpace(candidate.Purpose))
	purposeID := strings.TrimSpace(candidate.PurposeID)
	if st.Review != nil && st.Review.Handler != nil && candidate.ClaimToken == "" {
		purpose = lifecycle.AdmissionPurposeReview
		purposeID = st.Review.Handler.HandlerID
		// A review-bound correction is a new lifecycle Turn even when the
		// provider implements the input as steering within one Activity.
		mode = lifecycle.AdmissionFresh
	}
	input := lifecycle.PrepareAdmissionInput{
		SessionID: candidate.SessionID, TurnToken: lifecycle.TurnToken(candidate.ProposedTurnID),
		Receipt: candidate.Receipt, ClaimToken: candidate.ClaimToken, PayloadSHA256: candidate.PayloadSHA256,
		ProcessIdentity: candidate.ProcessIdentity, PaneGeneration: candidate.PaneGeneration,
		Mode: mode, ExistingTurnToken: lifecycle.TurnToken(candidate.ExistingTurnID),
		BaselineActivityID: candidate.BaselineActivityID, SignalProtocol: candidate.SignalProtocol,
		AttemptedAt:        candidate.AcceptedAt.UTC(),
		TranscriptProvider: candidate.TranscriptBinding.Provider, TranscriptFlag: candidate.TranscriptBinding.PiFlag,
		TranscriptPath: candidate.TranscriptBinding.PiPath, Purpose: purpose, PurposeID: purposeID,
	}
	applied, next, err := s.fsm.PrepareAdmission(st.ID, input)
	if err != nil {
		return watcher.InputAdmission{}, false, err
	}
	if err := s.SyncWorkProjection(string(st.ID)); err != nil {
		return watcher.InputAdmission{}, false, err
	}
	return admissionSnapshot(next, next.AdmissionByToken(lifecycle.TurnToken(candidate.ProposedTurnID))), applied, nil
}

func normalizeInputAdmission(candidate watcher.InputAdmission) watcher.InputAdmission {
	candidate.WorkID = strings.TrimSpace(candidate.WorkID)
	candidate.SessionID = strings.TrimSpace(candidate.SessionID)
	candidate.ProposedTurnID = strings.TrimSpace(candidate.ProposedTurnID)
	candidate.Receipt = strings.TrimSpace(candidate.Receipt)
	candidate.ClaimToken = strings.TrimSpace(candidate.ClaimToken)
	candidate.PayloadSHA256 = strings.TrimSpace(candidate.PayloadSHA256)
	candidate.ProcessIdentity = strings.TrimSpace(candidate.ProcessIdentity)
	candidate.PaneGeneration = strings.TrimSpace(candidate.PaneGeneration)
	candidate.ExistingTurnID = strings.TrimSpace(candidate.ExistingTurnID)
	candidate.BaselineActivityID = strings.TrimSpace(candidate.BaselineActivityID)
	return candidate
}

func validateInputAdmissionCandidate(candidate watcher.InputAdmission) error {
	if candidate.SessionID == "" || candidate.ProposedTurnID == "" || candidate.Receipt == "" ||
		candidate.PayloadSHA256 == "" || candidate.ProcessIdentity == "" || candidate.PaneGeneration == "" || candidate.AcceptedAt.IsZero() {
		return fmt.Errorf("submission identity, receipt, payload digest, process, pane, and accepted_at are required")
	}
	if len(candidate.PayloadSHA256) != sha256.Size*2 {
		return fmt.Errorf("payload_sha256 must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(candidate.PayloadSHA256); err != nil {
		return fmt.Errorf("payload_sha256 must be a SHA-256 hex digest")
	}
	if candidate.Mode != watcher.InputAdmissionFresh && candidate.Mode != watcher.InputAdmissionConditionalSteer {
		return fmt.Errorf("invalid submission mode %q", candidate.Mode)
	}
	return nil
}

func (s *Store) fsmStateForAdmission(candidate watcher.InputAdmission) (*lifecycle.State, error) {
	if candidate.WorkID != "" {
		st, err := s.fsmState(candidate.WorkID)
		if err != nil {
			return nil, err
		}
		if candidate.ClaimToken != "" {
			if candidate.Receipt != candidate.ProposedTurnID || st.Review == nil || st.Review.Handler == nil ||
				st.Review.Handler.HandlerID != candidate.ClaimToken ||
				st.Review.Handler.HandlerToken != lifecycle.TurnToken(candidate.ProposedTurnID) {
				return nil, ErrEventClaim
			}
		}
		return st, nil
	}
	for _, st := range s.fsm.ListViews() {
		if st.Attempt != nil && st.Attempt.SessionID == candidate.SessionID {
			return st, nil
		}
	}
	return nil, fmt.Errorf("no active Brain Work owns delegated Session %s; input was not submitted", candidate.SessionID)
}

// InputAdmission reads one exact ledger-owned submission transaction.
func (s *Store) InputAdmission(sessionID, proposedTurnID string) (watcher.InputAdmission, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	proposedTurnID = strings.TrimSpace(proposedTurnID)
	if s == nil || sessionID == "" || proposedTurnID == "" {
		return watcher.InputAdmission{}, false, nil
	}
	for _, st := range s.fsm.ListViews() {
		if admission := st.AdmissionByToken(lifecycle.TurnToken(proposedTurnID)); admission != nil && admission.SessionID == sessionID {
			return admissionSnapshot(st, admission), true, nil
		}
	}
	return watcher.InputAdmission{}, false, nil
}

// PendingInputAdmissions returns every unresolved submission for a Session in
// deterministic preparation order (AcceptedAt, then ProposedTurnID). It lets
// the normal provider poll resolve each post-mutation crash transaction from
// its own exact evidence without replaying input or consulting tmux for
// canonical ownership. Several distinct pending transactions may coexist for
// one Session; the caller resolves each only when its exact identity matches.
func (s *Store) PendingInputAdmissions(sessionID string) ([]watcher.InputAdmission, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return nil, nil
	}
	out := make([]watcher.InputAdmission, 0)
	for _, st := range s.fsm.ListViews() {
		admission := st.Admission
		if admission != nil && admission.SessionID == sessionID &&
			(admission.Status == lifecycle.AdmissionPrepared || admission.Status == lifecycle.AdmissionAmbiguous) {
			out = append(out, admissionSnapshot(st, admission))
		}
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].AcceptedAt.Equal(out[right].AcceptedAt) {
			return out[left].ProposedTurnID < out[right].ProposedTurnID
		}
		return out[left].AcceptedAt.Before(out[right].AcceptedAt)
	})
	return out, nil
}

func admissionSnapshot(st *lifecycle.State, admission *lifecycle.AdmissionState) watcher.InputAdmission {
	if st == nil || admission == nil {
		return watcher.InputAdmission{}
	}
	state := watcher.InputAdmissionPending
	switch admission.Status {
	case lifecycle.AdmissionAccepted:
		state = watcher.InputAdmissionResolved
	case lifecycle.AdmissionAborted:
		state = watcher.InputAdmissionAborted
	case lifecycle.AdmissionAmbiguous:
		state = watcher.InputAdmissionPending
	}
	mode := watcher.InputAdmissionFresh
	if admission.Mode == lifecycle.AdmissionConditionalSteer {
		mode = watcher.InputAdmissionConditionalSteer
	}
	return watcher.InputAdmission{
		WorkID: string(st.ID), SessionID: admission.SessionID, ProposedTurnID: string(admission.TurnToken),
		Receipt: admission.Receipt, ClaimToken: admission.ClaimToken, PayloadSHA256: admission.PayloadSHA256,
		ProcessIdentity: admission.ProcessIdentity, PaneGeneration: admission.PaneGeneration,
		AcceptedAt: admission.AttemptedAt, Mode: mode, ExistingTurnID: string(admission.ExistingTurnToken),
		BaselineActivityID: admission.BaselineActivityID, SignalProtocol: admission.SignalProtocol,
		Purpose: string(admission.Purpose), PurposeID: admission.PurposeID,
		State: state, ResolvedTurnID: string(admission.ResultTurnToken), ResolvedActivityID: admission.ActivityID,
		ResolvedAdmission: watcher.TurnAdmission{Stream: admission.AdmissionStream, ID: admission.AdmissionID,
			Cursor: admission.AdmissionCursor, SHA256: admission.PayloadSHA256, At: admission.AdmissionAt},
		TranscriptBinding: watcher.TranscriptBinding{Provider: admission.TranscriptProvider, PiFlag: admission.TranscriptFlag, PiPath: admission.TranscriptPath},
	}
}

// AbortInputAdmission permanently closes a pre-mutation transaction without
// creating a Turn. Only a successfully persisted Abort may be reported as
// NotSubmitted; an abort write failure leaves the outcome ambiguous.
func (s *Store) AbortInputAdmission(sessionID, proposedTurnID, receipt, payloadSHA256 string) (watcher.InputAdmission, error) {
	sessionID = strings.TrimSpace(sessionID)
	proposedTurnID = strings.TrimSpace(proposedTurnID)
	receipt = strings.TrimSpace(receipt)
	payloadSHA256 = strings.TrimSpace(payloadSHA256)
	if s == nil || s.fsm == nil {
		return watcher.InputAdmission{}, fmt.Errorf("brain store is not configured")
	}
	for _, st := range s.fsm.ListViews() {
		admission := st.AdmissionByToken(lifecycle.TurnToken(proposedTurnID))
		if admission == nil || admission.SessionID != sessionID {
			continue
		}
		next, err := s.fsm.AbortAdmission(st.ID, lifecycle.TurnToken(proposedTurnID), receipt, payloadSHA256, "proved_not_submitted")
		if err != nil {
			return watcher.InputAdmission{}, err
		}
		if err := s.SyncWorkProjection(string(st.ID)); err != nil {
			return watcher.InputAdmission{}, err
		}
		return admissionSnapshot(next, next.AdmissionByToken(lifecycle.TurnToken(proposedTurnID))), nil
	}
	return watcher.InputAdmission{}, fmt.Errorf("pending submission not found")
}

// MarkInputAdmissionAmbiguous records that the target-bound mutation queue
// started but provider acceptance is not yet proven. This is canonical
// non-replayable state, not a tmux receipt convention.
func (s *Store) MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason string) error {
	st, _, err := s.fsmAdmission(strings.TrimSpace(sessionID), strings.TrimSpace(proposedTurnID))
	if err != nil {
		return err
	}
	if _, err := s.fsm.MarkAdmissionAmbiguous(st.ID, lifecycle.TurnToken(strings.TrimSpace(proposedTurnID)), strings.TrimSpace(reason)); err != nil {
		return err
	}
	return s.SyncWorkProjection(string(st.ID))
}

// ResolveInputAdmission atomically resolves provider admission and canonical
// ownership. Same exact baseline Activity resolves as steering to the
// existing Turn. Only a different confirmed Activity promotes the proposed
// fresh Turn. The provider admission digest must exactly match the pending
// payload digest.
func (s *Store) ResolveInputAdmission(resolution watcher.InputAdmissionResolution) (watcher.InputAdmission, error) {
	resolution.SessionID = strings.TrimSpace(resolution.SessionID)
	resolution.ProposedTurnID = strings.TrimSpace(resolution.ProposedTurnID)
	resolution.Receipt = strings.TrimSpace(resolution.Receipt)
	resolution.PayloadSHA256 = strings.TrimSpace(resolution.PayloadSHA256)
	resolution.ActivityID = strings.TrimSpace(resolution.ActivityID)
	resolution.Admission.SHA256 = strings.TrimSpace(resolution.Admission.SHA256)
	if s == nil || s.fsm == nil {
		return watcher.InputAdmission{}, fmt.Errorf("brain store is not configured")
	}
	if resolution.Admission.Empty() ||
		resolution.Admission.SHA256 == "" || resolution.Admission.SHA256 != resolution.PayloadSHA256 {
		return watcher.InputAdmission{}, fmt.Errorf("provider admission does not match the pending payload digest")
	}
	st, admission, err := s.fsmAdmission(resolution.SessionID, resolution.ProposedTurnID)
	if err != nil {
		return watcher.InputAdmission{}, err
	}
	if resolution.ActivityID == "" && (admission.ClaimToken == "" || admission.BaselineActivityID == "") {
		return watcher.InputAdmission{}, fmt.Errorf("provider admission has no Activity outside a queued Brain Review")
	}
	next, err := s.fsm.AcceptAdmission(st.ID, lifecycle.TurnToken(resolution.ProposedTurnID), lifecycle.AcceptAdmissionInput{
		SessionID: resolution.SessionID, Receipt: resolution.Receipt, PayloadSHA256: resolution.PayloadSHA256,
		ActivityID: resolution.ActivityID, AdmissionStream: resolution.Admission.Stream,
		AdmissionID: resolution.Admission.ID, AdmissionCursor: resolution.Admission.Cursor,
		AdmissionSHA256: resolution.Admission.SHA256, AdmissionAt: resolution.Admission.At,
	})
	if err != nil {
		return watcher.InputAdmission{}, err
	}
	accepted := next.AdmissionByToken(admission.TurnToken)
	if err := s.SyncWorkProjection(string(st.ID)); err != nil {
		return watcher.InputAdmission{}, err
	}
	if err := s.projectAcceptedAdmission(next, accepted, resolution); err != nil {
		return watcher.InputAdmission{}, err
	}
	// The accepted Turn row now exists, so the canonical review-nextAttempt
	// projection can bind its exact provider token without an inference window.
	if err := s.SyncWorkProjection(string(st.ID)); err != nil {
		return watcher.InputAdmission{}, err
	}
	s.broadcastWorkChange(string(st.ID))
	return admissionSnapshot(next, accepted), nil
}

// rebuildFSMProjections derives every mutable read model from Lifecycle after
// crash/restart. Work rows are projected before provider-facing Turn rows, so
// projection validation never mistakes accepted-but-not-admitted input for an
// active Attempt.
func (s *Store) rebuildFSMProjections() error {
	if s == nil || s.fsm == nil {
		return fmt.Errorf("brain store is not configured")
	}
	states := s.fsm.ListViews()
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, st := range states {
		if err := s.SyncWorkProjection(string(st.ID)); err != nil {
			note(fmt.Errorf("project Work %s: %w", st.ID, err))
		}
	}
	for _, st := range states {
		admission := st.Admission
		if admission != nil && admission.Status == lifecycle.AdmissionAccepted {
			resolvedAt := admission.AttemptedAt
			if admission.SettledAt != nil && !admission.SettledAt.IsZero() {
				resolvedAt = *admission.SettledAt
			}
			resolution := watcher.InputAdmissionResolution{
				SessionID: admission.SessionID, ProposedTurnID: string(admission.TurnToken),
				Receipt: admission.Receipt, PayloadSHA256: admission.PayloadSHA256,
				ActivityID: admission.ActivityID, ResolvedAt: resolvedAt,
				Admission: watcher.TurnAdmission{Stream: admission.AdmissionStream, ID: admission.AdmissionID,
					Cursor: admission.AdmissionCursor, SHA256: admission.PayloadSHA256, At: admission.AdmissionAt},
			}
			if err := s.projectAcceptedAdmission(st, admission, resolution); err != nil {
				note(fmt.Errorf("project admission %s: %w", admission.TurnToken, err))
			}
		}
	}
	// Accepted admission rows are present now. Re-project Work once more so all
	// presentation fields reflect the final canonical aggregate image.
	for _, st := range states {
		if err := s.SyncWorkProjection(string(st.ID)); err != nil {
			note(fmt.Errorf("bind accepted Work projection %s: %w", st.ID, err))
		}
	}
	return firstErr
}

func (s *Store) fsmAdmission(sessionID, proposedTurnID string) (*lifecycle.State, *lifecycle.AdmissionState, error) {
	for _, st := range s.fsm.ListViews() {
		admission := st.AdmissionByToken(lifecycle.TurnToken(proposedTurnID))
		if admission != nil && admission.SessionID == sessionID {
			return st, admission, nil
		}
	}
	return nil, nil, fmt.Errorf("pending submission not found")
}

// projectAcceptedAdmission maintains the provider-facing Turn read model.
// Failure cannot revoke the already-committed Lifecycle fact; restart repair
// deterministically rebuilds this projection from the exact State admission.
func (s *Store) projectAcceptedAdmission(st *lifecycle.State, admission *lifecycle.AdmissionState, resolution watcher.InputAdmissionResolution) error {
	if st == nil || admission == nil {
		return fmt.Errorf("accepted admission projection is missing canonical state")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return err
	}
	resultTurnID := string(admission.ResultTurnToken)
	if resultTurnID == "" {
		return fmt.Errorf("accepted admission %s has no aggregate result token", admission.TurnToken)
	}
	leaseFrom := resolution.ResolvedAt
	if leaseFrom.IsZero() {
		leaseFrom = now
	}
	for index := range database.BrainTurns {
		turn := &database.BrainTurns[index]
		if turn.SessionID != admission.SessionID || turn.TurnID != resultTurnID {
			continue
		}
		turn.ActivityID, turn.Admission, turn.UpdatedAt = resolution.ActivityID, resolution.Admission, now
		turn.QueuedBehindActivityID = admission.BaselineActivityID
		if admission.ResultTurnToken == admission.TurnToken {
			turn.WorkID = string(st.ID)
			turn.Receipt = admission.Receipt
			turn.PaneGeneration = admission.PaneGeneration
			turn.ProcessIdentity = admission.ProcessIdentity
			turn.PayloadSHA256 = admission.PayloadSHA256
			turn.AcceptedAt = admission.AttemptedAt.UTC()
			turn.TranscriptBinding = watcher.TranscriptBinding{Provider: admission.TranscriptProvider,
				PiFlag: admission.TranscriptFlag, PiPath: admission.TranscriptPath}
			turn.SignalProtocol = admission.SignalProtocol
			turn.HostHandling = admission.ClaimToken != ""
		}
		lease := leaseFrom.Add(turnLeaseGrace).UTC()
		if lease.After(turn.LeaseDeadline) {
			turn.LeaseDeadline = lease
		}
		return s.persistPresentationLocked(database)
	}
	if admission.AttemptedAt.IsZero() {
		return fmt.Errorf("accepted admission %s has no attempted_at", admission.TurnToken)
	}
	acceptedAt := admission.AttemptedAt.UTC()
	fact := watcher.TurnFact{SessionID: admission.SessionID, TurnID: resultTurnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID: "receipt\x00" + admission.Receipt + "\x00accepted\x00" + admission.PayloadSHA256}
	database.BrainTurns = append(database.BrainTurns, TurnRecord{
		SessionID: admission.SessionID, TurnID: resultTurnID, WorkID: string(st.ID),
		Status: watcher.TurnAccepted, Receipt: admission.Receipt, PaneGeneration: admission.PaneGeneration,
		ProcessIdentity: admission.ProcessIdentity, PayloadSHA256: admission.PayloadSHA256,
		Admission: resolution.Admission, ActivityID: resolution.ActivityID, AcceptedAt: acceptedAt,
		QueuedBehindActivityID: admission.BaselineActivityID,
		Summary:                "Delegated input accepted", Facts: []TurnFactRecord{{FactID: fact.TurnFactIDFor(),
			Kind: fact.Kind, Class: fact.Class, At: leaseFrom.UTC(), Summary: "Delegated input accepted"}},
		TranscriptBinding: watcher.TranscriptBinding{Provider: admission.TranscriptProvider, PiFlag: admission.TranscriptFlag, PiPath: admission.TranscriptPath},
		SignalProtocol:    admission.SignalProtocol, HostHandling: admission.ClaimToken != "",
		LeaseDeadline: leaseFrom.Add(turnLeaseGrace).UTC(), UpdatedAt: now,
	})
	return s.persistPresentationLocked(database)
}

func databaseWorkIDForTurnAdmission(database presentationDatabase, sessionID string) string {
	if workID := databaseActiveWorkIDForExecutionSession(database, sessionID); workID != "" {
		return workID
	}
	// A terminal or attention-relinquished Turn remains exact lifecycle
	// evidence for a same-Session correction, but it does not restore progress
	// ownership.
	if current, found := currentTurnForSession(database, sessionID); found {
		if index := workIndex(database.BrainWork, current.WorkID); index >= 0 {
			item := database.BrainWork[index]
			if item.Status != WorkDone && item.Status != WorkCancelled &&
				(item.AttemptSessionID == sessionID ||
					(strings.TrimSpace(item.AttemptSessionID) == "" && (workHasReviewObligation(database, item.ID) || item.Wake != nil))) {
				return item.ID
			}
		}
	}
	return ""
}

// Turn returns the canonical snapshot for the current turn of the session.
func (s *Store) Turn(sessionID string) (watcher.TurnSnapshot, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return watcher.TurnSnapshot{}, false, nil
	}
	// A live aggregate owner is the current prompt authority. Never select a
	// different Turn by projection timestamp or lexical token ordering.
	attemptState, hasAttempt := s.fsmWorkByAttemptSession(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	turns, err := s.turnRecordsLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	if hasAttempt && attemptState.Attempt != nil {
		attemptToken := string(attemptState.Attempt.TurnToken)
		if turn, found := turns.exact[turnRecordKey(sessionID, attemptToken)]; found {
			snapshot := turn.snapshot()
			snapshot.LeaseDeadline = attemptState.Attempt.LeaseDeadline
			if admission := attemptState.AdmissionByToken(attemptState.Attempt.TurnToken); admission != nil &&
				admission.ResultTurnToken == attemptState.Attempt.TurnToken {
				snapshot.SignalProtocol = admission.SignalProtocol
			}
			return snapshot, true, nil
		}
		// Admission and ownership commit before the disposable Turn row. If a
		// concurrent read lands in that repair interval, derive the snapshot
		// directly from the exact aggregate token instead of returning an older
		// row or manufacturing an identity.
		current := attemptState.CurrentTurn()
		if current == nil || string(current.TurnToken) != attemptToken {
			return watcher.TurnSnapshot{}, false, fmt.Errorf("canonical Attempt %s has no matching aggregate Turn", attemptToken)
		}
		snapshot := watcher.TurnSnapshot{
			SessionID: sessionID, TurnID: attemptToken, Status: watcher.TurnAccepted,
			AcceptedAt: current.AdmittedAt, LeaseDeadline: attemptState.Attempt.LeaseDeadline,
			UpdatedAt: attemptState.UpdatedAt,
		}
		if admission := attemptState.AdmissionByToken(attemptState.Attempt.TurnToken); admission != nil &&
			admission.ResultTurnToken == attemptState.Attempt.TurnToken {
			snapshot.AcceptedAt = admission.AttemptedAt
			snapshot.ActivityID = admission.ActivityID
			snapshot.QueuedBehindActivityID = admission.BaselineActivityID
			snapshot.Admission = watcher.TurnAdmission{
				Stream: admission.AdmissionStream, ID: admission.AdmissionID, Cursor: admission.AdmissionCursor,
				SHA256: admission.PayloadSHA256, At: admission.AdmissionAt,
			}
			snapshot.HasAdmission = !snapshot.Admission.Empty()
			snapshot.PaneGeneration = admission.PaneGeneration
			snapshot.ProcessIdentity = admission.ProcessIdentity
			snapshot.TranscriptBinding = watcher.TranscriptBinding{
				Provider: admission.TranscriptProvider, PiFlag: admission.TranscriptFlag, PiPath: admission.TranscriptPath,
			}
			snapshot.SignalProtocol = admission.SignalProtocol
		}
		return snapshot, true, nil
	}
	turn, found := turns.current[sessionID]
	if !found {
		return watcher.TurnSnapshot{}, false, nil
	}
	return turn.snapshot(), true, nil
}

// TurnByID reads one exact canonical provider Turn. Host handling recovery
// must never substitute whichever Turn happens to be current for the Session.
func (s *Store) TurnByID(sessionID, turnID string) (watcher.TurnSnapshot, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if s == nil || sessionID == "" || turnID == "" {
		return watcher.TurnSnapshot{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	turns, err := s.turnRecordsLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	turn, found := turns.exact[turnRecordKey(sessionID, turnID)]
	if found {
		return turn.snapshot(), true, nil
	}
	return watcher.TurnSnapshot{}, false, nil
}

func (s *Store) turnRecordsLocked() (*turnReadCache, error) {
	info, err := os.Stat(s.presentationPath())
	if err != nil {
		return nil, err
	}
	if s.turnCache.valid && s.turnCache.size == info.Size() && s.turnCache.modTime.Equal(info.ModTime()) {
		return &s.turnCache, nil
	}
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, err
	}
	next := turnReadCache{
		valid: true, size: info.Size(), modTime: info.ModTime(),
		current: make(map[string]TurnRecord), exact: make(map[string]TurnRecord, len(database.BrainTurns)),
	}
	for _, turn := range database.BrainTurns {
		next.exact[turnRecordKey(turn.SessionID, turn.TurnID)] = turn
		current, found := next.current[turn.SessionID]
		if !found || turn.AcceptedAt.After(current.AcceptedAt) ||
			(turn.AcceptedAt.Equal(current.AcceptedAt) && turn.TurnID > current.TurnID) {
			next.current[turn.SessionID] = turn
		}
	}
	s.turnCache = next
	return &s.turnCache, nil
}

func turnRecordKey(sessionID, turnID string) string {
	return sessionID + "\x00" + turnID
}

func currentTurnForSession(database presentationDatabase, sessionID string) (TurnRecord, bool) {
	best := TurnRecord{}
	bestSet := false
	for _, turn := range database.BrainTurns {
		if turn.SessionID != sessionID {
			continue
		}
		if !bestSet || turn.AcceptedAt.After(best.AcceptedAt) ||
			(turn.AcceptedAt.Equal(best.AcceptedAt) && turn.TurnID > best.TurnID) {
			best = turn
			bestSet = true
		}
	}
	return best, bestSet
}

func exactTurnForSession(database presentationDatabase, sessionID, turnID string) (TurnRecord, bool) {
	for _, turn := range database.BrainTurns {
		if turn.SessionID == sessionID && turn.TurnID == turnID {
			return turn, true
		}
	}
	return TurnRecord{}, false
}

// turnMutation is the pure decision of the single reducer for one fact.
type turnMutation struct {
	status          watcher.TurnStatus
	attention       string
	controlState    watcher.TurnControlState
	settledAt       *time.Time
	summary         string
	leaseDeadline   time.Time
	recordAdmission bool
	admission       watcher.TurnAdmission
	activityID      string
	eventKind       string
	eventActionable bool
	eventSummary    string
	hint            *watcher.TurnHint
	dropHintKind    string
	workUpdate      WorkUpdate
	recordFact      bool
	changed         bool
}

// reduceTurnFact is the canonical transition table (worklog C.2.3). It never
// mutates the input turn; the store applies the mutation atomically.
func reduceTurnFact(turn *TurnRecord, fact watcher.TurnFact, now time.Time) (turnMutation, error) {
	var mutation turnMutation
	if turn == nil || strings.TrimSpace(fact.TurnID) == "" {
		return mutation, nil
	}
	if strings.TrimSpace(fact.SourceID) == "" {
		return mutation, fmt.Errorf("fact source identity is required for deterministic FactID")
	}
	if !validEvidenceClass(fact.Class) {
		return mutation, fmt.Errorf("invalid fact evidence class %q", fact.Class)
	}
	controlOwnershipLoss := fact.Class == watcher.EvidenceLiveness && fact.Kind == "ownership_lost"
	switch turn.Status {
	case watcher.TurnDone, watcher.TurnFailed:
		// Provider outcome is globally final. A later ownership-loss observation
		// is retained only as audit evidence: the completed capability no longer
		// controls admission for the next turn.
		if !controlOwnershipLoss {
			return mutation, nil
		}
	case watcher.TurnUnknown:
		if controlOwnershipLoss {
			break
		}
		// Unknown is final for scheduling until a later authoritative Provider
		// terminal upgrades it (C.2.4) — except for signal-protocol Turns,
		// whose semantic terminal authority is exact Control done/failed only
		// (C.2.10). A terminal may use the recorded tuple or ActivityID, OR it
		// may safely adopt a previously unbound terminal whose non-empty
		// tuple/ActivityID and StartedAt prove it belongs to this turn's
		// admission window. Running, attention, control, liveness, pane,
		// stale, blind and replay-only facts remain ignored.
		if fact.Class != watcher.EvidenceProvider ||
			(fact.Kind != "done" && fact.Kind != "failed") ||
			turn.SignalProtocol ||
			!providerFactBinds(turn, fact) {
			return mutation, nil
		}
	}

	status := turn.Status
	attention := turn.Attention
	controlState := turn.ControlState
	settledAt := turn.SettledAt
	summary := turn.Summary
	admission := turn.Admission
	activityID := turn.ActivityID

	// Bound provider facts carry the turn's admission tuple (monotone cursor)
	// or prove the turn's own activity identity; an Admitted/Accepted turn with
	// no recorded tuple adopts the provider's newest observation that started
	// inside its admission window (C.6 poll-time adoption).
	binding := providerFactBinds(turn, fact)
	adopts := !binding && providerFactAdopts(turn, fact)

	applyEvent := func(kind string, actionable bool, eventSummary string) {
		mutation.eventKind = kind
		mutation.eventActionable = actionable
		mutation.eventSummary = eventSummary
	}
	hintOnly := func(kind, eventSummary string) {
		mutation.hint = &watcher.TurnHint{
			Kind:    kind,
			Class:   fact.Class,
			At:      fact.At,
			Summary: eventSummary,
		}
		applyEvent(kind, false, eventSummary)
	}

	switch fact.Class {
	case watcher.EvidenceProvider:
		switch fact.Kind {
		case "uncertain":
			// Bounded provider-evidence loss (transcript unlocatable or
			// unreadable for the current turn): end-of-evidence resolves
			// Unknown + one actionable session.uncertain — never silent
			// Admitted and never fabricated done/failed. A later readable
			// bound Provider terminal upgrades canonical status and derived
			// Work (C.2.4). Current-turn identity is enforced by
			// ApplyTurnFact before the reducer runs.
			if status != watcher.TurnUnknown {
				status = watcher.TurnUnknown
				attention = ""
				summary = "Delegated Session provider evidence is unavailable; outcome is unknown"
			}
			if fact.SettledAt.IsZero() {
				settledAt = &now
			} else {
				settled := fact.SettledAt.UTC()
				settledAt = &settled
			}
			mutation.changed = true
			applyEvent("session.uncertain", true, summary)
		case "admission":
			// The admission tuple must prove this input began inside the
			// turn's admission window; stale pre-window admissions never
			// promote a newer turn.
			if status != watcher.TurnAdmitted || fact.Admission.Empty() ||
				(!fact.Admission.At.IsZero() && fact.Admission.At.Before(turn.AcceptedAt)) {
				return mutation, nil
			}
			status = watcher.TurnAccepted
			admission = fact.Admission
			activityID = firstNonEmpty(activityID, fact.ActivityID)
			summary = firstNonEmpty(fact.Summary, "Delegated turn accepted")
			mutation.recordAdmission = true
			mutation.changed = true
		case "running":
			inWindow := fact.StartedAt.IsZero() || !fact.StartedAt.Before(turn.AcceptedAt)
			if !binding && !adopts {
				// An unbound running fact is diagnostic evidence only — except
				// that provider running inside the admission window
				// contradicts any provisional same-kind hint (history showing
				// the turn still running drops the false done hint).
				if inWindow {
					mutation.dropHintKind = "session.done"
					mutation.changed = true
				}
				return mutation, nil
			}
			if adopts {
				admission = fact.Admission
			}
			activityID = firstNonEmpty(activityID, fact.ActivityID)
			switch status {
			case watcher.TurnAdmitted:
				// Only an accepted Receipt or a turn-bound Provider admission
				// proves the input began; adoption is that proof.
				status = watcher.TurnAccepted
				mutation.recordAdmission = true
				fallthrough
			case watcher.TurnAccepted, watcher.TurnBlocked:
				status = watcher.TurnRunning
				attention = ""
				summary = firstNonEmpty(fact.Summary, "Delegated turn running")
				mutation.changed = true
			case watcher.TurnRunning:
				if fact.Summary != "" && fact.Summary != summary {
					summary = fact.Summary
					mutation.changed = true
				}
			}
			applyEvent("session.running", false, summary)
			if turn.Admission.Empty() && !admission.Empty() {
				mutation.recordAdmission = true
			}
			if strings.TrimSpace(turn.ActivityID) == "" && activityID != "" {
				mutation.activityID = activityID
			}
			mutation.dropHintKind = firstNonEmpty(mutation.dropHintKind, "session.done")
		case "attention":
			if !binding && !adopts {
				return mutation, nil
			}
			if status != watcher.TurnBlocked {
				status = watcher.TurnBlocked
				attention = "user_input"
				summary = firstNonEmpty(fact.Summary, "Delegated Session needs input")
				mutation.changed = true
			}
			applyEvent("session.needs_input", true, summary)
		case "done", "failed":
			kind := "session.done"
			if fact.Kind == "failed" {
				kind = "session.failed"
			}
			if !binding && !adopts {
				// Unbound provider terminal: provisional hint only. Canonical
				// status is unchanged; the non-actionable row is flipped in
				// place if a bound terminal of that kind arrives later.
				hintOnly(kind, "Delegated provider reported "+fact.Kind+" without turn binding")
				mutation.changed = true
				return mutation, nil
			}
			if turn.SignalProtocol {
				// Signal-protocol terminal authority (C.2.10): only exact
				// Control done/failed is semantic. A bound provider terminal
				// is transport/liveness evidence: it attaches as a provisional
				// hint and never moves canonical status. A later exact Control
				// terminal flips the same dedupe-keyed row actionable.
				hintOnly(kind, "Delegated provider reported "+fact.Kind+"; awaiting exact control completion")
				mutation.changed = true
				return mutation, nil
			}
			if adopts {
				admission = fact.Admission
				mutation.recordAdmission = true
			}
			if status == watcher.TurnUnknown {
				// C.2.4: a later bound Provider terminal upgrades canonical
				// status and derived Work; the uncertain row stays as audit.
			}
			if activityID == "" && fact.ActivityID != "" {
				mutation.activityID = fact.ActivityID
			}
			status = watcher.TurnDone
			attention = ""
			if fact.Kind == "failed" {
				status = watcher.TurnFailed
			}
			if fact.SettledAt.IsZero() {
				settledAt = &now
			} else {
				settled := fact.SettledAt.UTC()
				settledAt = &settled
			}
			summary = firstNonEmpty(fact.Summary, kind)
			mutation.changed = true
			applyEvent(kind, true, summary)
			mutation.dropHintKind = firstNonEmpty(mutation.dropHintKind, kind)
		}
	case watcher.EvidenceControl:
		switch fact.Kind {
		case "running":
			// Attention cleared / lease renewal: refreshes Accepted, Running,
			// and Blocked. Control never promotes Admitted (only an accepted
			// Receipt or a bound Provider admission/activity proves the input
			// began) and never terminalizes. Every heartbeat is a distinct
			// durable fact (its progress_event_id is part of the FactID), so
			// later identical heartbeats still renew the lease record. The
			// caller-declared lease extends the turn's own per-turn deadline;
			// it is monotone (never shrinks), mirroring ApplyProgress.
			if fact.LeaseSeconds > 0 {
				if deadline := fact.At.Add(time.Duration(fact.LeaseSeconds) * time.Second); deadline.After(turn.LeaseDeadline) {
					mutation.leaseDeadline = deadline
				}
			}
			switch status {
			case watcher.TurnAccepted:
				status = watcher.TurnRunning
				if fact.Summary != "" {
					summary = fact.Summary
				} else {
					summary = firstNonEmpty(summary, "Delegated turn running")
				}
				attention = ""
				mutation.recordFact = true
			case watcher.TurnRunning:
				if fact.Summary != "" && fact.Summary != summary {
					summary = fact.Summary
				}
				attention = ""
				mutation.recordFact = true
			case watcher.TurnBlocked:
				status = watcher.TurnRunning
				attention = ""
				if fact.Summary != "" {
					summary = fact.Summary
				} else {
					summary = firstNonEmpty(summary, "Delegated turn running")
				}
				mutation.recordFact = true
			}
		case "attention":
			if fact.LeaseSeconds > 0 {
				if deadline := fact.At.Add(time.Duration(fact.LeaseSeconds) * time.Second); deadline.After(turn.LeaseDeadline) {
					mutation.leaseDeadline = deadline
				}
			}
			if status != watcher.TurnBlocked {
				status = watcher.TurnBlocked
				attention = "user_input"
				summary = firstNonEmpty(fact.Summary, "Delegated Session needs input")
				mutation.changed = true
			}
			applyEvent("session.needs_input", true, summary)
		case "done", "failed":
			kind := "session.done"
			if fact.Kind == "failed" {
				kind = "session.failed"
			}
			// A Turn whose random identity was carried in the delegated prompt
			// has already passed exact identity admission before reaching this
			// reducer. Its terminal Control fact is canonical without provider
			// confirmation. Pre-upgrade/provider-native Turns retain their
			// historical hint behavior because they never received that contract.
			if status == watcher.TurnAdmitted || fact.At.Before(turn.AcceptedAt) {
				return mutation, nil
			}
			if !turn.SignalProtocol {
				hintOnly(kind, "Delegated Session reported "+fact.Kind+"; awaiting provider confirmation")
				mutation.changed = true
				return mutation, nil
			}
			status = watcher.TurnDone
			attention = ""
			if fact.Kind == "failed" {
				status = watcher.TurnFailed
			}
			settled := now
			if !fact.SettledAt.IsZero() {
				settled = fact.SettledAt.UTC()
			}
			settledAt = &settled
			summary = firstNonEmpty(fact.Summary, kind)
			mutation.changed = true
			applyEvent(kind, true, summary)
			mutation.dropHintKind = firstNonEmpty(mutation.dropHintKind, kind)
		case "stale":
			// Lease expiry: no canonical change; one actionable session.stale
			// per turn wakes Brain. The current turn must have exceeded its own
			// expected-next-check time (minted at admission, extended only by
			// this turn's Control lease facts): a freshly admitted turn or a
			// turn with a live per-turn lease is never stale, so an old turn's
			// expired lease cannot make a newer turn stale. Current-turn
			// identity is enforced by ApplyTurnFact before the reducer runs.
			if now.Before(turn.LeaseDeadline) {
				return mutation, nil
			}
			applyEvent("session.stale", true, "Delegated Session lease expired; inspect the Session")
			mutation.changed = true
		}
	case watcher.EvidenceReceipt:
		if fact.Kind == "admission" && status == watcher.TurnAdmitted &&
			!fact.Admission.Empty() &&
			(fact.Admission.At.IsZero() || !fact.Admission.At.Before(turn.AcceptedAt)) {
			status = watcher.TurnAccepted
			admission = fact.Admission
			activityID = firstNonEmpty(activityID, fact.ActivityID)
			summary = firstNonEmpty(fact.Summary, "Delegated input accepted")
			mutation.recordAdmission = true
			mutation.changed = true
		}
	case watcher.EvidenceLiveness:
		switch fact.Kind {
		case "ownership_lost":
			if watcher.TurnImmutable(status) {
				// The provider outcome already ended this turn. Persist the exact
				// liveness fact for audit, but do not create a control gate, mutate
				// Work, or wake Brain. A fresh turn owns its own generation and
				// admission transaction.
				mutation.changed = true
				break
			}
			// Ownership loss is a control capability state only: it never
			// fabricates a terminal outcome. The canonical status becomes
			// Unknown (terminal for scheduling) and the review fact is
			// session.uncertain; only exact Control done/failed (signal
			// turns) or a bound Provider terminal (pre-contract turns) may
			// resolve it later.
			controlState = watcher.TurnControlOwnershipLost
			if !watcher.TurnImmutable(status) {
				status = watcher.TurnUnknown
				attention = ""
				summary = "Delegated Session control ownership was lost; outcome is unknown"
				settledAt = &now
			}
			mutation.changed = true
			applyEvent("session.uncertain", true, summary)
			needsInput := WorkNeedsInput
			next := "Recover or replace the delegated Session ownership before sending more control input."
			sessionWait := "Session " + turn.SessionID
			var noWake *WorkWake
			mutation.workUpdate = WorkUpdate{
				Status: &needsInput, NextAction: &next,
				WaitFor: &sessionWait, Wake: &noWake,
			}
		case "failed":
			// Liveness-derived Failed is removed entirely (Round 4): no
			// production primitive can prove a dead pane's exit status
			// belongs to the exact recorded process lifetime (wrapper panes
			// propagate replaced children, snapshots may be unreadable,
			// dead-pane identity reads fail closed). Such facts are ignored;
			// only a bound Provider terminal may decide Failed.
			return mutation, nil
		case "uncertain":
			// ProcessDead without a readable bound terminal, or SessionReplaced
			// with a different live identity: end-of-identity → Unknown, never
			// Failed. The uncertain event is actionable so Brain reconciles.
			if status != watcher.TurnUnknown {
				status = watcher.TurnUnknown
				attention = ""
				summary = "Delegated Session ended; outcome is unknown"
			}
			if fact.SettledAt.IsZero() {
				settledAt = &now
			} else {
				settled := fact.SettledAt.UTC()
				settledAt = &settled
			}
			mutation.changed = true
			applyEvent("session.uncertain", true, summary)
		}
	case watcher.EvidencePane, watcher.EvidenceAbsent:
		// Pane evidence refreshes only and can never promote Admitted, set
		// attention, or terminalize; PaneAbsent is not death. Absent never
		// acts. No durable mutation.
		return mutation, nil
	}

	// Canonical row mutation: status/attention/settlement/admission/activity
	// move only when the transition changed them. Hint-only facts never move
	// canonical values and never change Work status. The per-turn lease
	// deadline only ever extends (monotone), so renewals always count as a
	// row change.
	rowChanged := status != turn.Status ||
		attention != turn.Attention ||
		controlState != turn.ControlState ||
		!sameTime(settledAt, turn.SettledAt) ||
		summary != turn.Summary ||
		(!admission.Empty() && (turn.Admission.Empty() ||
			admission.Cursor != turn.Admission.Cursor ||
			admission.ID != turn.Admission.ID)) ||
		(activityID != "" && activityID != turn.ActivityID) ||
		(!mutation.leaseDeadline.IsZero() && mutation.leaseDeadline.After(turn.LeaseDeadline))
	if rowChanged {
		mutation.status = status
		mutation.attention = attention
		mutation.controlState = controlState
		mutation.settledAt = settledAt
		mutation.summary = summary
		if mutation.recordAdmission && !admission.Empty() {
			mutation.admission = admission
		}
		if activityID != "" && activityID != turn.ActivityID {
			mutation.activityID = activityID
		}
		if mutation.status != "" && mutation.workUpdate.Status == nil {
			mutation.workUpdate = derivedWorkUpdate(status, turn.SessionID, mutation.eventKind)
		}
	} else if mutation.eventKind == "session.stale" {
		mutation.workUpdate = derivedWorkUpdate(status, turn.SessionID, mutation.eventKind)
	}
	if rowChanged || mutation.eventKind != "" || mutation.hint != nil || mutation.recordFact {
		mutation.changed = true
	}
	return mutation, nil
}

func sameTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// providerFactBinds implements the frozen binding gate (C.1.2): a provider
// fact is turn-bound when it carries the recorded admission tuple with a
// monotone cursor and matching payload digest, or proves the turn's own
// recorded activity identity.
func providerFactBinds(turn *TurnRecord, fact watcher.TurnFact) bool {
	if fact.Class != watcher.EvidenceProvider {
		return false
	}
	if !turn.Admission.Empty() {
		if fact.Admission.Stream == turn.Admission.Stream &&
			strings.TrimSpace(fact.Admission.ID) != "" &&
			fact.Admission.Cursor >= turn.Admission.Cursor &&
			(strings.TrimSpace(turn.Admission.SHA256) == "" ||
				strings.TrimSpace(fact.Admission.SHA256) == turn.Admission.SHA256) {
			return true
		}
	}
	if strings.TrimSpace(turn.ActivityID) != "" &&
		strings.TrimSpace(fact.ActivityID) == turn.ActivityID {
		return true
	}
	return false
}

// providerFactAdopts implements admission uncertainty reconciliation (C.6): an
// Admitted/Accepted turn with no recorded provider identity adopts the
// provider's newest observation that started inside its admission window.
// Blind replay is impossible: adoption only upgrades the turn from provider
// evidence, and stale (pre-admission-window) observations never bind.
func providerFactAdopts(turn *TurnRecord, fact watcher.TurnFact) bool {
	if fact.Class != watcher.EvidenceProvider {
		return false
	}
	if turn.Status != watcher.TurnAdmitted && turn.Status != watcher.TurnAccepted {
		return false
	}
	if !turn.Admission.Empty() {
		return false
	}
	if fact.StartedAt.IsZero() || fact.StartedAt.Before(turn.AcceptedAt) {
		return false
	}
	return !fact.Admission.Empty() || strings.TrimSpace(fact.ActivityID) != ""
}

func derivedWorkUpdate(status watcher.TurnStatus, sessionID, eventKind string) WorkUpdate {
	running := WorkRunning
	needsInput := WorkNeedsInput
	waiting := WorkWaiting
	sessionWait := "Session " + sessionID
	var noWake *WorkWake
	if eventKind == "session.stale" {
		// Lease expiry never moves canonical status; the stale wake is
		// needs_input regardless of the current canonical state.
		next := "Inspect the delegated Session lease expiry."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait, Wake: &noWake}
	}
	switch status {
	case watcher.TurnAccepted, watcher.TurnRunning:
		next := "Wait for the delegated Session."
		return WorkUpdate{Status: &running, NextAction: &next, WaitFor: &sessionWait, Wake: &noWake}
	case watcher.TurnBlocked:
		next := "Resolve the delegated Session request."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait, Wake: &noWake}
	case watcher.TurnDone:
		return terminalSessionWorkUpdate("session.done")
	case watcher.TurnFailed:
		return terminalSessionWorkUpdate("session.failed")
	case watcher.TurnUnknown:
		next := "Confirm whether the delegated Session received the prompt; delivery will not be replayed."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait, Wake: &noWake}
	}
	return WorkUpdate{Status: &waiting}
}

func boundedTerminalWorkUpdate() WorkUpdate {
	status := WorkDone
	empty := ""
	var noWake *WorkWake
	var noAttempt string
	return WorkUpdate{
		Status: &status, AttemptSessionID: &noAttempt,
		NextAction: &empty, WaitFor: &empty, Wake: &noWake,
	}
}

// ReassertLiveTurnOwnership renews only the exact current nonterminal Turn
// from bound provider-running evidence. Lifecycle coalesces the renewal, so
// repeated observations that cannot materially extend the deadline perform no
// durable projection write and do not advance the Work revision. Attention
// rows remain untouched and orthogonal.
func (s *Store) ReassertLiveTurnOwnership(workID, sessionID, turnID string) (Work, bool, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if workID == "" || sessionID == "" || turnID == "" {
		return Work{}, false, fmt.Errorf("Work, Session, and Turn identities are required")
	}
	state, err := s.fsmState(workID)
	if err != nil {
		return Work{}, false, err
	}
	if state.Attempt == nil || state.Attempt.SessionID != sessionID || string(state.Attempt.TurnToken) != turnID {
		item, readErr := s.Work(workID)
		return item, false, readErr
	}
	beforeRevision := state.Revision
	identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: state.Attempt.TurnToken, Fence: state.Attempt.Generation}
	next, err := s.fsm.Progress(state.ID, identity, "provider activity remains running")
	if err != nil {
		return Work{}, false, fsmObservationResult(err)
	}
	if next.Revision == beforeRevision {
		item, readErr := s.Work(workID)
		return item, false, readErr
	}
	if err := s.SyncWorkProjection(workID); err != nil {
		return Work{}, false, err
	}
	item, err := s.Work(workID)
	if err == nil {
		s.broadcastWorkChange(workID)
	}
	return item, err == nil, err
}

// prepareDelegatedSignalTurnLocked validates the one prompt-carried identity
// against authoritative lifecycle state. If it names the current pending
// delegated submission, this promotes that exact candidate to Accepted and
// resolves the submission in memory. A terminal signal for the latest exact
// Turn after lease loss is marked audit-only so the caller persists Turn
// evidence without translating it back into Work lifecycle state.
func (s *Store) prepareDelegatedSignalTurnLocked(database *presentationDatabase, fact watcher.TurnFact, now time.Time) (bool, error) {
	if database == nil {
		return false, errNoDelegatedSignalContract
	}
	var state *lifecycle.State
	var admission *lifecycle.AdmissionState
	for _, candidate := range s.fsm.ListViews() {
		if exact := candidate.AdmissionByToken(lifecycle.TurnToken(fact.TurnID)); exact != nil &&
			exact.SessionID == fact.SessionID && exact.SignalProtocol && exact.ClaimToken == "" {
			state, admission = candidate, exact
			break
		}
	}
	if state == nil || admission == nil {
		for _, candidate := range s.fsm.ListViews() {
			a := candidate.Admission
			if a != nil && a.SessionID == fact.SessionID && a.SignalProtocol && a.ClaimToken == "" {
				return false, errDelegatedTurnMismatch
			}
		}
		return false, errNoDelegatedSignalContract
	}
	if fact.At.Before(admission.AttemptedAt) {
		return false, errDelegatedTurnMismatch
	}
	if admission.Status == lifecycle.AdmissionPrepared || admission.Status == lifecycle.AdmissionAmbiguous {
		next, err := s.fsm.AcceptAdmissionBySignal(state.ID, admission.TurnToken, admission.SessionID)
		if err != nil {
			return false, err
		}
		state, admission = next, next.AdmissionByToken(admission.TurnToken)
	}
	if admission.ResultTurnToken != lifecycle.TurnToken(fact.TurnID) {
		return false, errDelegatedTurnMismatch
	}
	activeAttempt := state.Attempt != nil && state.Attempt.SessionID == fact.SessionID &&
		state.Attempt.TurnToken == lifecycle.TurnToken(fact.TurnID)
	lateTerminalAudit := (fact.Kind == "done" || fact.Kind == "failed") &&
		state.Review != nil && state.Review.Ref == fact.TurnID &&
		(state.Review.Reason == "lease_expired" || state.Review.Reason == "turn_lost")
	if !activeAttempt && !lateTerminalAudit {
		return false, errDelegatedTurnMismatch
	}
	if lateTerminalAudit {
		if current, found := currentTurnForSession(*database, fact.SessionID); !found ||
			current.TurnID != fact.TurnID || !current.SignalProtocol {
			return false, errDelegatedTurnMismatch
		}
	}
	if !lateTerminalAudit {
		if err := s.fsmSyncWorkLocked(database, string(state.ID), now); err != nil {
			return false, err
		}
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == fact.SessionID && turn.TurnID == fact.TurnID {
			return lateTerminalAudit, nil
		}
	}
	database.BrainTurns = append(database.BrainTurns, TurnRecord{
		SessionID: admission.SessionID, TurnID: fact.TurnID, WorkID: string(state.ID),
		Status: watcher.TurnAccepted, Receipt: admission.Receipt,
		PaneGeneration: admission.PaneGeneration, ProcessIdentity: admission.ProcessIdentity,
		PayloadSHA256: admission.PayloadSHA256, AcceptedAt: admission.AttemptedAt,
		Summary: "Delegated input admitted by matching control signal", Facts: []TurnFactRecord{},
		TranscriptBinding: watcher.TranscriptBinding{Provider: admission.TranscriptProvider,
			PiFlag: admission.TranscriptFlag, PiPath: admission.TranscriptPath},
		SignalProtocol: true, LeaseDeadline: now.Add(turnLeaseGrace).UTC(), UpdatedAt: now,
	})
	return lateTerminalAudit, nil
}

// ApplyTurnFact is the single canonical reducer. Under one lock and one
// persist it: dedupes the deterministic FactID, validates the transition,
// mutates the turn, derives the Work update, and appends or upgrades the
// presentation event (non-actionable to actionable in-place flip for corrections).
// A replayed or reordered fact is a no-op; terminal turns are immutable.
//
// Terminal Work (done/cancelled) is a terminal scheduler decision: a later
// fact may advance the turn row and is retained as non-actionable presentation
// audit, but it never moves Work status/next_action/wait_for and never
// creates or flips an actionable wake (C.2.9).
var (
	errNoDelegatedSignalContract = errors.New("Session has no delegated signal contract")
	errDelegatedTurnMismatch     = errors.New("delegated turn identity does not match the current prompt")
)

func (s *Store) ApplyTurnFact(fact watcher.TurnFact) (watcher.TurnSnapshot, bool, error) {
	return s.applyTurnFact(fact, false)
}

// ApplyDelegatedTurnProgress is the identity-gated Control entry point. It
// shares ApplyTurnFact's reducer and persistence transaction; when the first
// matching signal races provider confirmation, the pending submission is
// promoted and the fact is reduced in that same write.
func (s *Store) ApplyDelegatedTurnProgress(fact watcher.TurnFact) (watcher.TurnProgressResult, error) {
	snapshot, changed, err := s.applyTurnFact(fact, true)
	switch {
	case errors.Is(err, errNoDelegatedSignalContract):
		return watcher.TurnProgressResult{}, nil
	case errors.Is(err, errDelegatedTurnMismatch):
		return watcher.TurnProgressResult{Owned: true}, nil
	case err != nil:
		return watcher.TurnProgressResult{}, err
	default:
		return watcher.TurnProgressResult{
			Turn: snapshot, Owned: true, Matched: true, Changed: changed,
		}, nil
	}
}

func (s *Store) applyTurnFact(fact watcher.TurnFact, delegatedSignal bool) (watcher.TurnSnapshot, bool, error) {
	if s == nil {
		return watcher.TurnSnapshot{}, false, fmt.Errorf("brain store is not configured")
	}
	fact.SessionID = strings.TrimSpace(fact.SessionID)
	fact.TurnID = strings.TrimSpace(fact.TurnID)
	fact.Kind = strings.TrimSpace(fact.Kind)
	fact.SourceID = strings.TrimSpace(fact.SourceID)
	if fact.SessionID == "" || (!delegatedSignal && fact.TurnID == "") {
		return watcher.TurnSnapshot{}, false, fmt.Errorf("fact session_id and turn_id are required")
	}
	if fact.Kind == "" || fact.SourceID == "" {
		return watcher.TurnSnapshot{}, false, fmt.Errorf("fact kind and source identity are required")
	}
	factID := fact.TurnFactIDFor()
	now := s.nowUTC()
	if fact.At.IsZero() {
		fact.At = now
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	lateTerminalAudit := false
	if delegatedSignal {
		if fact.Class != watcher.EvidenceControl {
			return watcher.TurnSnapshot{}, false, fmt.Errorf("delegated progress requires Control evidence")
		}
		var prepareErr error
		lateTerminalAudit, prepareErr = s.prepareDelegatedSignalTurnLocked(&database, fact, now)
		if prepareErr != nil {
			return watcher.TurnSnapshot{}, false, prepareErr
		}
	}
	turnIndex := -1
	for index := range database.BrainTurns {
		turn := &database.BrainTurns[index]
		if turn.SessionID == fact.SessionID && turn.TurnID == fact.TurnID {
			turnIndex = index
			break
		}
	}
	if turnIndex < 0 {
		// The fact targets a turn that is not the current (or any) ledger
		// record: per-turn identity means it is ignored, never applied to a
		// different turn.
		return watcher.TurnSnapshot{}, false, nil
	}
	if fact.Class == watcher.EvidenceControl || fact.Class == watcher.EvidenceProvider {
		// Control and provider lifecycle facts may affect only the current
		// aggregate owner. A late previous-turn completion after Session reuse
		// is retained by its provider transcript, but cannot terminate the newer
		// canonical Turn, mutate Work, or wake Brain. Only ownerless historical
		// reconciliation consults the read model.
		attemptState, attemptSet := s.fsmWorkByAttemptSession(fact.SessionID)
		if attemptSet && attemptState.Attempt != nil && string(attemptState.Attempt.TurnToken) != fact.TurnID {
			return watcher.TurnSnapshot{}, false, nil
		}
		if !attemptSet {
			current, currentSet := currentTurnForSession(database, fact.SessionID)
			if currentSet && current.TurnID != fact.TurnID {
				return watcher.TurnSnapshot{}, false, nil
			}
		}
	}
	turn := database.BrainTurns[turnIndex]
	for _, recorded := range turn.Facts {
		if recorded.FactID == factID {
			// Replay / reorder / restart re-read: identical deterministic
			// fact, no-op.
			return turn.snapshot(), false, nil
		}
	}
	mutation, reduceErr := reduceTurnFact(&turn, fact, now)
	if reduceErr != nil {
		return watcher.TurnSnapshot{}, false, reduceErr
	}
	if !mutation.changed && mutation.eventKind == "" {
		return turn.snapshot(), false, nil
	}

	// Strong-completion pre-check: a bounded signal-protocol worker whose
	// exact bound provider terminal arrived carries Final completion
	// authority (bounded Work has no follow-up acceptance gate). Until-done
	// Work deliberately stays on the hint path.
	workIndexPre := workIndex(database.BrainWork, turn.WorkID)
	finalDone := false
	if workIndexPre >= 0 {
		workItemPre := database.BrainWork[workIndexPre]
		if workItemPre.CompletionPolicy == CompletionBounded && turn.SignalProtocol &&
			fact.Class == watcher.EvidenceProvider && fact.Kind == "done" &&
			providerFactBinds(&turn, fact) && mutation.hint != nil &&
			mutation.hint.Kind == "session.done" {
			finalDone = true
		}
	}

	// Canonical lifecycle first (docs/work-lifecycle.md): translate the
	// canonical transition into engine commands. The engine is the only writer
	// of Work status, active Attempt, review, wake, and completion state; the ledger
	// row below remains evidence cache. Every command is an idempotent reject
	// against stale fences/tokens, so crash replays converge.
	effectiveStatus := mutation.status
	if finalDone {
		effectiveStatus = watcher.TurnDone
	}
	if fact.Class == watcher.EvidenceProvider && turn.ActivityID == "" &&
		turn.QueuedBehindActivityID != "" && mutation.activityID != "" &&
		providerFactBinds(&turn, fact) {
		// Provider promotion is canonical admission enrichment, not merely a
		// presentation update. Persist the Activity on Lifecycle first so startup
		// projection repair cannot erase the queued Turn's exact correlation.
		if _, err := s.fsm.AcceptAdmission(lifecycle.WorkID(turn.WorkID), lifecycle.TurnToken(turn.TurnID), lifecycle.AcceptAdmissionInput{
			SessionID: fact.SessionID, Receipt: turn.Receipt, PayloadSHA256: turn.PayloadSHA256,
			ActivityID: mutation.activityID, AdmissionStream: fact.Admission.Stream,
			AdmissionID: fact.Admission.ID, AdmissionCursor: fact.Admission.Cursor,
			AdmissionSHA256: fact.Admission.SHA256, AdmissionAt: fact.Admission.At,
		}); err != nil {
			return watcher.TurnSnapshot{}, false, fmt.Errorf("persist queued provider promotion: %w", err)
		}
	}
	if !lateTerminalAudit {
		if err := s.fsmTranslateCanonicalTransition(&turn, fact, effectiveStatus, mutation.eventKind, finalDone); err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
	}

	// Apply the mutation to the ledger row.
	if !mutation.admission.Empty() {
		turn.Admission = mutation.admission
		turn.ActivityID = firstNonEmpty(turn.ActivityID, mutation.activityID)
	}
	if mutation.activityID != "" {
		turn.ActivityID = mutation.activityID
	}
	if mutation.status != "" {
		turn.Status = mutation.status
		turn.Attention = mutation.attention
		turn.ControlState = mutation.controlState
		turn.SettledAt = mutation.settledAt
		turn.Summary = mutation.summary
	}
	if !mutation.leaseDeadline.IsZero() {
		turn.LeaseDeadline = mutation.leaseDeadline.UTC()
	}
	if mutation.dropHintKind != "" {
		kept := turn.Hints[:0]
		for _, hint := range turn.Hints {
			if hint.Kind == mutation.dropHintKind {
				continue
			}
			kept = append(kept, hint)
		}
		turn.Hints = kept
	}
	if mutation.hint != nil {
		replaced := false
		for index := range turn.Hints {
			if turn.Hints[index].Kind == mutation.hint.Kind {
				turn.Hints[index] = *mutation.hint
				replaced = true
				break
			}
		}
		if !replaced {
			turn.Hints = append(turn.Hints, *mutation.hint)
		}
	}
	turn.Facts = append(turn.Facts, TurnFactRecord{
		FactID:  factID,
		Kind:    fact.Kind,
		Class:   fact.Class,
		Bound:   fact.Bound,
		At:      fact.At,
		Summary: fact.Summary,
	})
	turn.UpdatedAt = now
	if lateTerminalAudit {
		database.BrainTurns[turnIndex] = turn
		if err := s.persistPresentationLocked(database); err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
		return turn.snapshot(), true, nil
	}
	if isHostHandlingTurn(turn) {
		// Host provider Turns own only the delivery handling. Their lifecycle
		// may close/recover that exact handling, but must never be reinterpreted
		// as delegated Work progress or emit a second scheduler signal.
		database.BrainTurns[turnIndex] = turn
		if err := s.persistPresentationLocked(database); err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
		return turn.snapshot(), true, nil
	}

	// Derive the Work update (status only on final-grade facts; hints only
	// adjust next-action text). WorkDone/WorkCancelled are terminal scheduler
	// decisions: no later fact may move the Work's status, next action, or
	// wait condition (C.2.9), so hints and derived updates are suppressed for
	// terminal Work while the turn row keeps the fact as audit.
	workIndex := workIndex(database.BrainWork, turn.WorkID)
	var workItem Work
	terminalWork := false
	dispositionRevisionFrozen := false
	if workIndex >= 0 {
		workItem = database.BrainWork[workIndex]
		terminalWork = workItem.Status == WorkDone || workItem.Status == WorkCancelled
		dispositionRevisionFrozen = workHostLaneOwned(database, turn.WorkID)
		// A bounded signal-protocol worker's exact bound provider terminal is
		// strong completion evidence: promote the provisional hint to the
		// canonical terminal on the ledger row (the engine side already
		// reduced the same Final completion above).
		if !terminalWork && !dispositionRevisionFrozen && finalDone {
			settled := now
			if !fact.SettledAt.IsZero() {
				settled = fact.SettledAt.UTC()
			}
			mutation.status = watcher.TurnDone
			mutation.attention = ""
			mutation.settledAt = &settled
			mutation.summary = firstNonEmpty(fact.Summary, "session.done")
			mutation.eventActionable = true
			mutation.eventSummary = mutation.summary
			mutation.hint = nil
			mutation.dropHintKind = "session.done"
			turn.Status = watcher.TurnDone
			turn.Attention = ""
			turn.SettledAt = &settled
			turn.Summary = mutation.summary
		}
		// A delivered Host handling carries the exact Work capability for its
		// eventual disposition. Facts observed during that window remain
		// durable evidence, but they cannot mutate Work or advance the
		// capability fence: ResolveWorkReview is the sole owner of that
		// transition, and every lifecycle effect routes through the engine.
	}

	// Presentation event: exactly one row per (work, dedupe key); corrections flip
	// the existing non-actionable row actionable in place. Terminal Work keeps
	// the late row as non-actionable audit only: it is never claimed and never
	// flips, so no second wake is possible (C.2.9).
	eventCreated := false
	eventID := ""
	workID := turn.WorkID
	if mutation.eventKind != "" {
		actionable := mutation.eventActionable && !terminalWork
		dedupeKey := sessionTurnEventDedupeKey(turn.SessionID, turn.TurnID, mutation.eventKind)
		eventIndex := -1
		for index := range database.BrainWorkEvents {
			event := database.BrainWorkEvents[index]
			if event.WorkID == workID && event.DedupeKey == dedupeKey {
				eventIndex = index
				break
			}
		}
		if eventIndex < 0 {
			event := WorkEvent{
				ID:          uuid.NewString(),
				WorkID:      workID,
				Kind:        mutation.eventKind,
				DedupeKey:   dedupeKey,
				PayloadRef:  "session:" + turn.SessionID,
				SourceName:  turn.SessionID,
				Summary:     mutation.eventSummary,
				Phase:       fact.Phase,
				Attention:   fact.Attention,
				EventKind:   fact.EventKind,
				DetailsJSON: fact.DetailsJSON,
				Actionable:  actionable,
				CreatedAt:   now,
			}
			if workIndex >= 0 {
				event, err = appendWorkEventLocked(&database, workIndex, event, !dispositionRevisionFrozen)
				if err != nil {
					return watcher.TurnSnapshot{}, false, err
				}
			} else {
				return watcher.TurnSnapshot{}, false, ErrWorkNotFound
			}
			eventID = event.ID
			eventCreated = true
		} else if actionable &&
			!database.BrainWorkEvents[eventIndex].Actionable {
			// In-place correction flip: the same row becomes actionable; the
			// row count never changes, so no second wake or queue item is
			// possible (I7).
			database.BrainWorkEvents[eventIndex].Actionable = true
			database.BrainWorkEvents[eventIndex].Summary = mutation.eventSummary
			database.BrainWorkEvents[eventIndex].SourceName = turn.SessionID
			database.BrainWorkEvents[eventIndex].Phase = fact.Phase
			database.BrainWorkEvents[eventIndex].Attention = fact.Attention
			database.BrainWorkEvents[eventIndex].EventKind = fact.EventKind
			database.BrainWorkEvents[eventIndex].DetailsJSON = fact.DetailsJSON
			database.BrainWorkEvents[eventIndex].WorkRevision = workItem.Revision
			if dispositionRevisionFrozen {
				database.BrainWorkEvents[eventIndex].WorkRevision++
			}
			eventID = database.BrainWorkEvents[eventIndex].ID
			eventCreated = true
		}
	}
	// Canonical producer terminalization and every matching cross-Work wake
	// share this one lifecycle persist. A crash cannot expose the producer
	// terminal without the consumer attention, and FactID makes replay a no-op.
	wokenWorkIDs := []string{}
	if mutation.eventKind == "session.done" ||
		mutation.eventKind == "session.failed" || mutation.eventKind == "session.uncertain" {
		// Waiting Works parked on this producer are released by the canonical
		// engine (wake.cleared), not by inline scheduler writes.
		s.fsmFanoutSessionTerminal(turn.SessionID, turn.TurnID)
	}

	database.BrainTurns[turnIndex] = turn
	if workIndex >= 0 {
		// Refresh the derived Work row from canonical engine state.
		if err := s.fsmSyncWorkLocked(&database, turn.WorkID, now); err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	if eventCreated {
		s.broadcastWorkChange(workID)
	}
	for _, wokenWorkID := range wokenWorkIDs {
		s.broadcastWorkChange(wokenWorkID)
	}
	if eventCreated && isProjectedWorkResultEvent(mutation.eventKind) {
		// One projected card per Work lineage, replaced in place from
		// canonical state; its identity follows the projected fact. Card
		// projection is not notification delivery and never acknowledges the Review
		// success; only confirmed Host delivery does that.
		projectedEvent := WorkEvent{
			ID:          eventID,
			WorkID:      workID,
			Kind:        mutation.eventKind,
			Summary:     mutation.eventSummary,
			SourceName:  turn.SessionID,
			PayloadRef:  "session:" + turn.SessionID,
			Phase:       fact.Phase,
			Attention:   fact.Attention,
			EventKind:   fact.EventKind,
			DetailsJSON: fact.DetailsJSON,
			CreatedAt:   now,
			Actionable:  mutation.eventActionable,
		}
		if workIndex >= 0 && database.BrainWork[workIndex].Review != nil {
			projectedEvent.ID = database.BrainWork[workIndex].Review.EventID
		}
		if _, _, err := s.syncWorkCardLocked(workID, &projectedEvent); err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
	}
	return turn.snapshot(), true, nil
}

// AppendDeliveryNote appends a deduped delivery diagnostic. Delivery notes are
// scheduler audit only: they never carry claim/lease state and never create a
// review obligation (the review event itself is the queue item; the lease
// quarantine is expressed in Work state). Returns the existing row on
// duplicate.
func (s *Store) AppendDeliveryNote(workID, eventID, kind, dedupeKey, summary string, actionable bool) (WorkEvent, bool, error) {
	if s == nil {
		return WorkEvent{}, false, fmt.Errorf("brain store is not configured")
	}
	eventID = strings.TrimSpace(eventID)
	if workID == "" || eventID == "" || kind == "" || dedupeKey == "" {
		return WorkEvent{}, false, fmt.Errorf("delivery note requires work_id, event_id, kind, and dedupe_key")
	}
	// Delivery notes are audit rows; a note must never become a queue item.
	actionable = false
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return WorkEvent{}, false, err
	}
	for _, current := range database.BrainWorkEvents {
		if current.WorkID == workID && current.DedupeKey == dedupeKey {
			return current, false, nil
		}
	}
	event := WorkEvent{
		ID:         uuid.NewString(),
		WorkID:     workID,
		Kind:       kind,
		DedupeKey:  dedupeKey,
		PayloadRef: "delivery:" + eventID,
		Summary:    summary,
		Actionable: actionable,
		CreatedAt:  now,
	}
	itemIndex := workIndex(database.BrainWork, workID)
	event, err = appendWorkEventLocked(&database, itemIndex, event, false)
	if err != nil {
		return WorkEvent{}, false, err
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(workID)
	return event, true, nil
}

// ResolveReviewLease is the explicit actor path (C.2.6, now Work-scoped) that
// closes a held review lease. Actor-recorded, never time-based. The fact row
// records the closure audit; the review event is the same queue item before
// and after.
//
//   - mark_delivered: the actor asserts the Host received the action; the
//     lease is cleared and the same unresolved action is re-claimable (row 11).
//   - replay: the actor authorizes re-delivery; the lease is cleared and the
//     same action identity is re-claimable (row 12) — no second fact is
//     created, so no second card is possible.
//   - discard: the actor abandons the current action; the event is cleared
//     with audit. If Work is still non-terminal, one fresh
//     brain.reconcile_required fact re-requires the same queue item so the
//     Brain still decides the disposition (row 13).
func (s *Store) ResolveReviewLease(workID string, resolution ReviewLeaseResolution, actor, reason string) (WorkReviewAction, bool, error) {
	workID = strings.TrimSpace(workID)
	actor = strings.TrimSpace(actor)
	if workID == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return WorkReviewAction{}, false, fmt.Errorf("lease resolution requires work_id, actor, and reason")
	}
	if resolution != ReviewLeaseMarkDelivered && resolution != ReviewLeaseDiscard && resolution != ReviewLeaseReplay {
		return WorkReviewAction{}, false, fmt.Errorf("lease resolution must be mark_delivered, discard, or replay")
	}
	if _, err := s.fsm.State(lifecycle.WorkID(workID)); errors.Is(err, lifecycle.ErrUnknownWork) {
		return WorkReviewAction{}, false, ErrWorkNotFound
	} else if err != nil {
		return WorkReviewAction{}, false, err
	}
	if _, err := s.fsm.ResolveReviewDelivery(
		lifecycle.WorkID(workID), string(resolution), actor, strings.TrimSpace(reason),
	); err != nil {
		return WorkReviewAction{}, false, err
	}
	if err := s.SyncWorkProjection(workID); err != nil {
		return WorkReviewAction{}, false, err
	}
	s.mu.Lock()
	database, err := s.loadPresentationLocked()
	s.mu.Unlock()
	if err != nil {
		return WorkReviewAction{}, false, err
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		return WorkReviewAction{}, false, ErrWorkNotFound
	}
	action, found := reviewActionFromReview(database, database.BrainWork[index].Review)
	s.broadcastWorkChange(workID)
	return action, found, nil
}

// ErrNoActiveTurn is returned when a session has no canonical turn.
var ErrNoActiveTurn = errors.New("no canonical turn for session")
