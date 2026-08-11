package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/google/uuid"
)

// TurnRecord is the durable canonical per-turn lifecycle row (schema v6,
// "brain_turns" inside orchestration.json). It is the single owner of every
// lifecycle transition for an accepted delegated turn: Work status and outbox
// events are derived from it by the one reducer, never inferred independently.
type TurnRecord struct {
	SessionID       string                   `json:"session_id"`
	TurnID          string                   `json:"turn_id"`
	WorkID          string                   `json:"work_id"`
	Status          watcher.TurnStatus       `json:"status"`
	Receipt         string                   `json:"receipt,omitempty"`
	PaneGeneration  string                   `json:"pane_generation,omitempty"`
	ProcessIdentity string                   `json:"process_identity,omitempty"`
	PayloadSHA256   string                   `json:"payload_sha256,omitempty"`
	Admission       watcher.TurnAdmission    `json:"admission,omitempty"`
	ActivityID      string                   `json:"activity_id,omitempty"`
	Attention       string                   `json:"attention,omitempty"`
	ControlState    watcher.TurnControlState `json:"control_state,omitempty"`
	AcceptedAt      time.Time                `json:"accepted_at,omitempty"`
	SettledAt       *time.Time               `json:"settled_at,omitempty"`
	Summary         string                   `json:"summary,omitempty"`
	Facts           []TurnFactRecord         `json:"facts"`
	Hints           []watcher.TurnHint       `json:"hints,omitempty"`
	// TranscriptBinding is the provider-native transcript identity recorded
	// at admission (Pi owned session flag/path), restored on rediscovery;
	// the equivalent tmux option is only an advisory cache.
	TranscriptBinding watcher.TranscriptBinding `json:"transcript_binding,omitempty"`
	// SignalProtocol is true only when this Turn's random identity was carried
	// in its delegated prompt. It is an authority marker, not another identity.
	SignalProtocol bool `json:"signal_protocol,omitempty"`
	// LeaseDeadline is the turn's own expected-next-check time (per-turn
	// liveness): minted fresh at admission, extended only by this turn's
	// Control lease facts, and the sole basis for session.stale. An old turn's
	// expired lease can never make a newer turn stale.
	LeaseDeadline time.Time `json:"lease_deadline,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TurnSubmissionRecord is the Turn Ledger's durable pre-mutation transaction
// row. It is intentionally stored outside brain_turns: Pending never becomes
// the current Turn. Resolve atomically either records exact steering or adds
// the freshly Accepted Turn; Abort is retained as a permanent non-adoptable
// result. Retire is the separate actor-authorized terminal result for a Host
// delivery whose provider outcome is deliberately not adopted.
type TurnSubmissionRecord struct {
	SessionID          string                      `json:"session_id"`
	ProposedTurnID     string                      `json:"proposed_turn_id"`
	WorkID             string                      `json:"work_id"`
	Receipt            string                      `json:"receipt"`
	ClaimToken         string                      `json:"claim_token,omitempty"`
	PayloadSHA256      string                      `json:"payload_sha256"`
	ProcessIdentity    string                      `json:"process_identity"`
	PaneGeneration     string                      `json:"pane_generation"`
	AcceptedAt         time.Time                   `json:"accepted_at"`
	TranscriptBinding  watcher.TranscriptBinding   `json:"transcript_binding,omitempty"`
	Mode               watcher.TurnSubmissionMode  `json:"mode"`
	ExistingTurnID     string                      `json:"existing_turn_id,omitempty"`
	BaselineActivityID string                      `json:"baseline_activity_id,omitempty"`
	SignalProtocol     bool                        `json:"signal_protocol,omitempty"`
	State              watcher.TurnSubmissionState `json:"state"`
	ResolvedTurnID     string                      `json:"resolved_turn_id,omitempty"`
	ResolvedActivityID string                      `json:"resolved_activity_id,omitempty"`
	ResolvedAdmission  watcher.TurnAdmission       `json:"resolved_admission,omitempty"`
	CreatedAt          time.Time                   `json:"created_at"`
	ResolvedAt         *time.Time                  `json:"resolved_at,omitempty"`
	AbortedAt          *time.Time                  `json:"aborted_at,omitempty"`
}

func (r TurnSubmissionRecord) snapshot() watcher.TurnSubmission {
	return watcher.TurnSubmission{
		WorkID: r.WorkID, SessionID: r.SessionID, ProposedTurnID: r.ProposedTurnID,
		Receipt: r.Receipt, ClaimToken: r.ClaimToken, PayloadSHA256: r.PayloadSHA256,
		ProcessIdentity: r.ProcessIdentity, PaneGeneration: r.PaneGeneration,
		AcceptedAt: r.AcceptedAt, TranscriptBinding: r.TranscriptBinding,
		Mode: r.Mode, ExistingTurnID: r.ExistingTurnID,
		BaselineActivityID: r.BaselineActivityID, SignalProtocol: r.SignalProtocol, State: r.State,
		ResolvedTurnID: r.ResolvedTurnID, ResolvedActivityID: r.ResolvedActivityID,
		ResolvedAdmission: r.ResolvedAdmission,
	}
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

// TurnLedgerImport is one legacy tmux marker materialized by the one-shot
// migration. Canonical status is Admitted/Running only; done/failed markers
// attach a Legacy hint that never changes canonical status (C.2.8).
type TurnLedgerImport struct {
	SessionID       string
	TurnID          string
	WorkID          string
	Status          watcher.TurnStatus
	AcceptedAt      time.Time
	ProcessIdentity string
	PaneGeneration  string
	Summary         string
	Hint            *watcher.TurnHint
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
	case watcher.EvidenceAbsent, watcher.EvidenceLegacy, watcher.EvidencePane,
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

func validateTurnSubmissions(submissions []TurnSubmissionRecord, workIDs map[string]struct{}) error {
	keys := make(map[string]struct{}, len(submissions))
	receipts := make(map[string]struct{}, len(submissions))
	pendingSessions := make(map[string]struct{}, len(submissions))
	for index, submission := range submissions {
		prefix := fmt.Sprintf("brain_turn_submissions[%d]", index)
		if strings.TrimSpace(submission.SessionID) == "" || strings.TrimSpace(submission.ProposedTurnID) == "" {
			return fmt.Errorf("%s: session_id and proposed_turn_id are required", prefix)
		}
		if _, ok := workIDs[submission.WorkID]; !ok {
			return fmt.Errorf("%s: unknown work_id %q", prefix, submission.WorkID)
		}
		if strings.TrimSpace(submission.Receipt) == "" || strings.TrimSpace(submission.PayloadSHA256) == "" ||
			strings.TrimSpace(submission.ProcessIdentity) == "" || strings.TrimSpace(submission.PaneGeneration) == "" {
			return fmt.Errorf("%s: receipt, payload_sha256, process_identity, and pane_generation are required", prefix)
		}
		hostClaimSubmission := strings.TrimSpace(submission.ClaimToken) != ""
		if submission.SignalProtocol && hostClaimSubmission {
			return fmt.Errorf("%s: Host submission cannot use the delegated signal protocol", prefix)
		}
		if hostClaimSubmission == (submission.Receipt == submission.ProposedTurnID) {
			return fmt.Errorf("%s: Host submission requires a distinct receipt and claim token", prefix)
		}
		if len(submission.PayloadSHA256) != sha256.Size*2 {
			return fmt.Errorf("%s: payload_sha256 must be a SHA-256 hex digest", prefix)
		}
		if _, err := hex.DecodeString(submission.PayloadSHA256); err != nil {
			return fmt.Errorf("%s: payload_sha256 must be a SHA-256 hex digest", prefix)
		}
		if submission.AcceptedAt.IsZero() || submission.CreatedAt.IsZero() {
			return fmt.Errorf("%s: accepted_at and created_at are required", prefix)
		}
		switch submission.Mode {
		case watcher.TurnSubmissionFresh:
			if strings.TrimSpace(submission.BaselineActivityID) != "" {
				return fmt.Errorf("%s: fresh submission cannot have baseline_activity_id", prefix)
			}
		case watcher.TurnSubmissionConditionalSteer:
			if strings.TrimSpace(submission.ExistingTurnID) == "" || strings.TrimSpace(submission.BaselineActivityID) == "" {
				return fmt.Errorf("%s: conditional steering requires existing_turn_id and baseline_activity_id", prefix)
			}
		default:
			return fmt.Errorf("%s: invalid mode %q", prefix, submission.Mode)
		}
		switch submission.State {
		case watcher.TurnSubmissionPending:
			if submission.ResolvedAt != nil || submission.AbortedAt != nil || submission.ResolvedTurnID != "" ||
				submission.ResolvedActivityID != "" || !submission.ResolvedAdmission.Empty() {
				return fmt.Errorf("%s: pending submission has terminal fields", prefix)
			}
			if _, exists := pendingSessions[submission.SessionID]; exists {
				return fmt.Errorf("%s: session already has a pending submission", prefix)
			}
			pendingSessions[submission.SessionID] = struct{}{}
		case watcher.TurnSubmissionResolved:
			providerResolved := strings.TrimSpace(submission.ResolvedActivityID) != "" &&
				!submission.ResolvedAdmission.Empty() &&
				submission.ResolvedAdmission.SHA256 == submission.PayloadSHA256
			controlResolved := submission.SignalProtocol &&
				submission.ResolvedTurnID == submission.ProposedTurnID &&
				strings.TrimSpace(submission.ResolvedActivityID) == "" && submission.ResolvedAdmission.Empty()
			if submission.ResolvedAt == nil || submission.AbortedAt != nil ||
				strings.TrimSpace(submission.ResolvedTurnID) == "" || (!providerResolved && !controlResolved) {
				return fmt.Errorf("%s: resolved submission requires matching provider admission or exact signal admission", prefix)
			}
		case watcher.TurnSubmissionAborted:
			if submission.AbortedAt == nil || submission.ResolvedAt != nil || submission.ResolvedTurnID != "" ||
				submission.ResolvedActivityID != "" || !submission.ResolvedAdmission.Empty() {
				return fmt.Errorf("%s: aborted submission has invalid terminal fields", prefix)
			}
		case watcher.TurnSubmissionRetired:
			if submission.ResolvedAt == nil || submission.AbortedAt != nil || submission.ResolvedTurnID != "" ||
				submission.ResolvedActivityID != "" || !submission.ResolvedAdmission.Empty() {
				return fmt.Errorf("%s: retired submission has invalid terminal fields", prefix)
			}
			if submission.ResolvedAt.Before(submission.AcceptedAt) {
				return fmt.Errorf("%s: retired submission predates acceptance", prefix)
			}
		default:
			return fmt.Errorf("%s: invalid state %q", prefix, submission.State)
		}
		key := submission.SessionID + "\x00" + submission.ProposedTurnID
		if _, exists := keys[key]; exists {
			return fmt.Errorf("%s: duplicate session/proposed turn", prefix)
		}
		keys[key] = struct{}{}
		receiptKey := submission.SessionID + "\x00" + submission.Receipt + "\x00" + submission.ClaimToken
		if _, exists := receipts[receiptKey]; exists {
			return fmt.Errorf("%s: duplicate session/receipt", prefix)
		}
		receipts[receiptKey] = struct{}{}
		if !submission.TranscriptBinding.Empty() {
			if strings.TrimSpace(submission.TranscriptBinding.Provider) != "pi" ||
				(submission.TranscriptBinding.PiFlag != "--session" && submission.TranscriptBinding.PiFlag != "--session-dir") ||
				!filepath.IsAbs(submission.TranscriptBinding.PiPath) {
				return fmt.Errorf("%s: invalid transcript binding", prefix)
			}
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
		SessionID:         t.SessionID,
		TurnID:            t.TurnID,
		Status:            t.Status,
		AcceptedAt:        t.AcceptedAt,
		SettledAt:         t.SettledAt,
		Summary:           t.Summary,
		Attention:         t.Attention,
		ControlState:      t.ControlState,
		ActivityID:        t.ActivityID,
		Admission:         t.Admission,
		HasAdmission:      !t.Admission.Empty(),
		Hints:             append([]watcher.TurnHint(nil), t.Hints...),
		PaneGeneration:    t.PaneGeneration,
		ProcessIdentity:   t.ProcessIdentity,
		TranscriptBinding: t.TranscriptBinding,
		LeaseDeadline:     t.LeaseDeadline,
		SignalProtocol:    t.SignalProtocol,
		UpdatedAt:         t.UpdatedAt,
	}
	return snapshot
}

// turnLeaseGrace is the fresh per-turn liveness minted at admission: the
// turn's own expected-next-check deadline before its first progress lease
// arrives. It is per-turn, so a newly admitted turn can never inherit an old
// turn's expired lease (the false-stale incident).
const turnLeaseGrace = 10 * time.Minute

// PrepareTurnSubmission persists the sole pre-mutation transaction for the
// exact provider pane/process generation. A stable Session ID is only a UI
// container: when the watcher presents a causally newer, different provider
// generation, any older pending transaction is retired atomically with the
// replacement. It remains non-adoptable audit and can never block the new
// provider generation. The method never appends to brain_turns and therefore
// cannot replace the current running Turn. The provider baseline already
// classified by the watcher is rechecked against the authoritative current row
// under the Store lock.
func (s *Store) PrepareTurnSubmission(candidate watcher.TurnSubmission) (watcher.TurnSubmission, bool, error) {
	if s == nil {
		return watcher.TurnSubmission{}, false, fmt.Errorf("brain store is not configured")
	}
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
	if candidate.SessionID == "" || candidate.ProposedTurnID == "" || candidate.Receipt == "" ||
		candidate.PayloadSHA256 == "" || candidate.ProcessIdentity == "" || candidate.PaneGeneration == "" {
		return watcher.TurnSubmission{}, false, fmt.Errorf("submission identity, receipt, payload digest, process, and pane are required")
	}
	if candidate.AcceptedAt.IsZero() {
		return watcher.TurnSubmission{}, false, fmt.Errorf("accepted_at is required")
	}
	if len(candidate.PayloadSHA256) != sha256.Size*2 {
		return watcher.TurnSubmission{}, false, fmt.Errorf("payload_sha256 must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(candidate.PayloadSHA256); err != nil {
		return watcher.TurnSubmission{}, false, fmt.Errorf("payload_sha256 must be a SHA-256 hex digest")
	}
	hostClaimSubmission := candidate.Receipt != candidate.ProposedTurnID || candidate.ClaimToken != ""
	if hostClaimSubmission && (candidate.WorkID == "" || candidate.ClaimToken == "") {
		return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Host submission requires exact Work and claim token", ErrEventClaim)
	}
	if hostClaimSubmission && candidate.Mode != watcher.TurnSubmissionFresh {
		// Internal Work Events are serialized scheduler transactions, never
		// interactive steering. Allowing one to share a running provider Activity
		// destroys the Event -> Turn ownership fence even if the transport accepts
		// the bytes. Keep this defense below exact Host classification but before
		// every Store mutation.
		return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Host Work Event submission must start a fresh provider Turn", ErrEventClaim)
	}
	now := s.nowUTC()
	s.mu.Lock()
	changedWorkID := ""
	defer func() {
		s.mu.Unlock()
		if changedWorkID != "" {
			s.broadcastWorkChange(changedWorkID)
		}
	}()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSubmission{}, false, err
	}
	pendingIndexes := []int{}
	for index, record := range database.BrainTurnSubmissions {
		if record.SessionID != candidate.SessionID {
			continue
		}
		sameClaimAttempt := record.Receipt == candidate.Receipt && record.ClaimToken == candidate.ClaimToken
		if record.ProposedTurnID == candidate.ProposedTurnID || sameClaimAttempt {
			if candidate.WorkID != "" && record.WorkID != candidate.WorkID {
				return watcher.TurnSubmission{}, false, fmt.Errorf("submission identity already belongs to different Work")
			}
			if !sameTurnSubmissionIdentity(record, candidate) {
				return watcher.TurnSubmission{}, false, fmt.Errorf("submission identity already belongs to different input")
			}
			return record.snapshot(), false, nil
		}
		if record.State == watcher.TurnSubmissionPending {
			pendingIndexes = append(pendingIndexes, index)
		}
	}
	workID := candidate.WorkID
	if hostClaimSubmission {
		if !databaseHasExactHostEventClaim(database, candidate) {
			return watcher.TurnSubmission{}, false, ErrEventClaim
		}
	} else if workID == "" {
		workID = databaseWorkIDForTurnAdmission(database, candidate.SessionID)
	}
	if workID == "" {
		return watcher.TurnSubmission{}, false, fmt.Errorf("no active Brain Work owns delegated Session %s; input was not submitted", candidate.SessionID)
	}
	workIndex := workIndex(database.BrainWork, workID)
	if workIndex < 0 {
		return watcher.TurnSubmission{}, false, ErrWorkNotFound
	}
	item := database.BrainWork[workIndex]
	if !hostClaimSubmission && (item.Status == WorkDone || item.Status == WorkCancelled) {
		return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Work %s is %s", ErrWorkOwnerConflict, item.ID, item.Status)
	}
	if executionWorkID := databaseActiveWorkIDForExecutionSession(database, candidate.SessionID); executionWorkID != "" && executionWorkID != item.ID {
		return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Session %s already executes Work %s", ErrWorkOwnerConflict, candidate.SessionID, executionWorkID)
	}
	current, hasCurrent := currentTurnForSession(database, candidate.SessionID)
	if hasCurrent && !candidate.AcceptedAt.UTC().After(current.AcceptedAt) {
		return watcher.TurnSubmission{}, false, fmt.Errorf("proposed turn acceptance must follow the current canonical turn")
	}
	if hasCurrent && current.ControlState == watcher.TurnControlOwnershipLost {
		return watcher.TurnSubmission{}, false, fmt.Errorf("canonical Session control ownership was lost")
	}
	switch candidate.Mode {
	case watcher.TurnSubmissionFresh:
		if !hasCurrent {
			if candidate.ExistingTurnID != "" {
				return watcher.TurnSubmission{}, false, fmt.Errorf("provider baseline references a missing canonical turn")
			}
		} else if current.TurnID != candidate.ExistingTurnID || !watcher.TurnImmutable(current.Status) {
			return watcher.TurnSubmission{}, false, fmt.Errorf("fresh submission baseline no longer matches an immutable canonical turn")
		}
	case watcher.TurnSubmissionConditionalSteer:
		if !hasCurrent || current.TurnID != candidate.ExistingTurnID ||
			watcher.TurnTerminal(current.Status) || current.ActivityID != candidate.BaselineActivityID {
			return watcher.TurnSubmission{}, false, fmt.Errorf("steering baseline no longer matches the current running provider activity")
		}
	default:
		return watcher.TurnSubmission{}, false, fmt.Errorf("invalid submission mode %q", candidate.Mode)
	}
	if hasCurrent && current.TurnID == candidate.ProposedTurnID {
		return watcher.TurnSubmission{}, false, fmt.Errorf("proposed turn is already canonical without a matching submission transaction")
	}
	for _, index := range pendingIndexes {
		pending := database.BrainTurnSubmissions[index]
		if pending.ProcessIdentity == candidate.ProcessIdentity && pending.PaneGeneration == candidate.PaneGeneration {
			return watcher.TurnSubmission{}, false, fmt.Errorf("provider generation already has an unresolved pending submission")
		}
		if !candidate.AcceptedAt.UTC().After(pending.AcceptedAt) {
			return watcher.TurnSubmission{}, false, fmt.Errorf("replacement provider generation must be accepted after the pending submission")
		}
	}

	ownerID := strings.TrimSpace(item.OwnerSessionID)
	reservation := item.SuccessorReservation
	handlingIndex := inFlightHandlingEventIndex(database, item.ID)
	initialOwnerAdmission := false
	switch {
	case hostClaimSubmission:
		// The exact claimed Event owns this Host provider Turn. Worker
		// ownership and successor reservation remain untouched; Work changes
		// only when the delivered handling commits its typed disposition.
	case reservation != nil:
		if strings.TrimSpace(reservation.SessionID) != candidate.SessionID {
			return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Work %s already reserved successor %s", ErrWorkOwnerConflict, item.ID, reservation.SessionID)
		}
	case handlingIndex >= 0:
		// A delivered review handling awaits its exact typed disposition. A
		// correction admission is staged under that handling (same or new
		// Session) and takes ownership only when the exact continue
		// disposition commits; a queued head never stages or blocks.
		handling := database.BrainWorkEvents[handlingIndex]
		item.SuccessorReservation = &WorkSuccessorReservation{
			SessionID: candidate.SessionID, EventID: handling.ID, HandlingID: handling.HandlingID,
		}
		database.BrainWork[workIndex] = item
		changedWorkID = item.ID
	case ownerID == "":
		if candidate.WorkID == "" {
			return watcher.TurnSubmission{}, false, fmt.Errorf("initial delegated submission requires exact work_id")
		}
		initialOwnerAdmission = true
	case ownerID != candidate.SessionID:
		return watcher.TurnSubmission{}, false, fmt.Errorf("%w: Work %s is owned by %s", ErrWorkOwnerConflict, item.ID, ownerID)
	}

	// Every validation above runs before this mutation. A malformed or stale
	// candidate therefore cannot retire existing provider authority. The old
	// generation retirement and the new pending row share the one atomic Store
	// replacement below, so a crash exposes either side of the transition,
	// never a gap or two live authorities for the current generation.
	for _, index := range pendingIndexes {
		retiredAt := now
		database.BrainTurnSubmissions[index].State = watcher.TurnSubmissionRetired
		database.BrainTurnSubmissions[index].ResolvedAt = &retiredAt
	}

	record := TurnSubmissionRecord{
		SessionID: candidate.SessionID, ProposedTurnID: candidate.ProposedTurnID,
		WorkID: workID, Receipt: candidate.Receipt, ClaimToken: candidate.ClaimToken,
		PayloadSHA256:   candidate.PayloadSHA256,
		ProcessIdentity: candidate.ProcessIdentity, PaneGeneration: candidate.PaneGeneration,
		AcceptedAt: candidate.AcceptedAt.UTC(), TranscriptBinding: candidate.TranscriptBinding,
		Mode: candidate.Mode, ExistingTurnID: candidate.ExistingTurnID,
		BaselineActivityID: candidate.BaselineActivityID,
		SignalProtocol:     candidate.SignalProtocol,
		State:              watcher.TurnSubmissionPending, CreatedAt: now,
	}
	database.BrainTurnSubmissions = append(database.BrainTurnSubmissions, record)
	if initialOwnerAdmission {
		item.OwnerSessionID = candidate.SessionID
		item.OwnerDelegated = true
		item.Status = WorkRunning
		item.NextAction = "Wait for the delegated Session."
		item.WaitFor = "Session " + candidate.SessionID
		item.Wake = nil
		item.UpdatedAt = now
		item.Revision++
		database.BrainWork[workIndex] = item
		settleUndeliveredAttentionForAdmission(&database, item.ID, now)
		changedWorkID = item.ID
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		changedWorkID = ""
		return watcher.TurnSubmission{}, false, err
	}
	return record.snapshot(), true, nil
}

func sameTurnSubmissionIdentity(record TurnSubmissionRecord, candidate watcher.TurnSubmission) bool {
	return record.SessionID == candidate.SessionID &&
		record.ProposedTurnID == candidate.ProposedTurnID &&
		record.Receipt == candidate.Receipt &&
		record.ClaimToken == candidate.ClaimToken &&
		record.PayloadSHA256 == candidate.PayloadSHA256 &&
		record.ProcessIdentity == candidate.ProcessIdentity &&
		record.PaneGeneration == candidate.PaneGeneration &&
		record.AcceptedAt.Equal(candidate.AcceptedAt.UTC()) &&
		record.TranscriptBinding == candidate.TranscriptBinding &&
		record.Mode == candidate.Mode &&
		record.ExistingTurnID == candidate.ExistingTurnID &&
		record.BaselineActivityID == candidate.BaselineActivityID &&
		record.SignalProtocol == candidate.SignalProtocol
}

// TurnSubmission reads one exact ledger-owned submission transaction.
func (s *Store) TurnSubmission(sessionID, proposedTurnID string) (watcher.TurnSubmission, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	proposedTurnID = strings.TrimSpace(proposedTurnID)
	if s == nil || sessionID == "" || proposedTurnID == "" {
		return watcher.TurnSubmission{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSubmission{}, false, err
	}
	for _, record := range database.BrainTurnSubmissions {
		if record.SessionID == sessionID && record.ProposedTurnID == proposedTurnID {
			return record.snapshot(), true, nil
		}
	}
	return watcher.TurnSubmission{}, false, nil
}

// PendingTurnSubmission returns the sole unresolved submission for a Session.
// It lets the normal provider poll resolve a post-mutation crash without
// replaying input or consulting tmux for canonical ownership.
func (s *Store) PendingTurnSubmission(sessionID string) (watcher.TurnSubmission, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return watcher.TurnSubmission{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSubmission{}, false, err
	}
	for _, record := range database.BrainTurnSubmissions {
		if record.SessionID == sessionID && record.State == watcher.TurnSubmissionPending {
			return record.snapshot(), true, nil
		}
	}
	return watcher.TurnSubmission{}, false, nil
}

// AbortTurnSubmission permanently closes a pre-mutation transaction without
// creating a Turn. Only a successfully persisted Abort may be reported as
// NotSubmitted; an abort write failure leaves the outcome ambiguous.
func (s *Store) AbortTurnSubmission(sessionID, proposedTurnID, receipt, payloadSHA256 string) (watcher.TurnSubmission, error) {
	sessionID = strings.TrimSpace(sessionID)
	proposedTurnID = strings.TrimSpace(proposedTurnID)
	receipt = strings.TrimSpace(receipt)
	payloadSHA256 = strings.TrimSpace(payloadSHA256)
	if s == nil {
		return watcher.TurnSubmission{}, fmt.Errorf("brain store is not configured")
	}
	now := s.nowUTC()
	s.mu.Lock()
	changedWorkID := ""
	defer func() {
		s.mu.Unlock()
		if changedWorkID != "" {
			s.broadcastWorkChange(changedWorkID)
		}
	}()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSubmission{}, err
	}
	for index := range database.BrainTurnSubmissions {
		record := &database.BrainTurnSubmissions[index]
		if record.SessionID != sessionID || record.ProposedTurnID != proposedTurnID {
			continue
		}
		if record.Receipt != receipt || record.PayloadSHA256 != payloadSHA256 {
			return watcher.TurnSubmission{}, fmt.Errorf("submission identity belongs to different input")
		}
		switch record.State {
		case watcher.TurnSubmissionAborted:
			return record.snapshot(), nil
		case watcher.TurnSubmissionResolved:
			return watcher.TurnSubmission{}, fmt.Errorf("resolved submission cannot be aborted")
		case watcher.TurnSubmissionRetired:
			return watcher.TurnSubmission{}, fmt.Errorf("retired submission authority cannot be aborted")
		case watcher.TurnSubmissionPending:
			abortedAt := now
			record.State = watcher.TurnSubmissionAborted
			record.AbortedAt = &abortedAt
			if workIndex := workIndex(database.BrainWork, record.WorkID); workIndex >= 0 {
				item := database.BrainWork[workIndex]
				if reservation := item.SuccessorReservation; record.ExistingTurnID != "" && reservation != nil &&
					reservation.SessionID == record.SessionID && strings.TrimSpace(reservation.ProviderTurnID) == "" {
					// Prepare stages a same-Session correction itself. Proved
					// non-submission releases that pre-admission reservation with this
					// same Abort. A newly spawned Session has no predecessor Turn and
					// remains reserved until its explicit cleanup owner succeeds.
					item.SuccessorReservation = nil
					database.BrainWork[workIndex] = item
					changedWorkID = item.ID
				}
			}
			// A fresh submission with no predecessor is also the initial owner
			// admission. Proved non-submission releases that authority and restores
			// one actionable Work obligation in this same replacement; a crash
			// cannot expose an aborted submission beside a naked owner string.
			if record.ClaimToken == "" && record.ExistingTurnID == "" {
				if workIndex := workIndex(database.BrainWork, record.WorkID); workIndex >= 0 {
					item := database.BrainWork[workIndex]
					_, hasCurrent := currentTurnForSession(database, record.SessionID)
					if !hasCurrent && item.OwnerSessionID == record.SessionID && item.SuccessorReservation == nil {
						item.OwnerSessionID = ""
						item.OwnerDelegated = false
						item.Status = WorkNeedsInput
						item.NextAction = "Retry the delegated Session after confirmed non-submission."
						item.WaitFor = ""
						item.Wake = nil
						item.UpdatedAt = now
						database.BrainWork[workIndex] = item
						event := WorkEvent{
							ID: uuid.NewString(), WorkID: item.ID, Kind: "brain.submission_not_admitted",
							DedupeKey:  "brain:submission-abort:" + record.Receipt,
							PayloadRef: "session:" + record.SessionID, SourceName: record.SessionID,
							Summary:    "Delegated input was proved not submitted; choose the next disposition.",
							Actionable: true, CreatedAt: now,
						}
						if _, appendErr := appendWorkEventLocked(&database, workIndex, event, true); appendErr != nil {
							return watcher.TurnSubmission{}, appendErr
						}
						changedWorkID = item.ID
					}
				}
			}
			if err := s.persistOrchestrationLocked(database); err != nil {
				return watcher.TurnSubmission{}, err
			}
			return record.snapshot(), nil
		}
	}
	return watcher.TurnSubmission{}, fmt.Errorf("pending submission not found")
}

// RecordInitialSubmissionNotAdmitted converges a zero-Turn delegated launch
// after the exact initial prompt was proved not submitted. Fresh auto-created
// Work is cancelled and its fact becomes audit-only; an explicitly attached
// Work remains retryable with one actionable fact. The caller must tear down
// the zero-Turn Session before requesting cancellation.
func (s *Store) RecordInitialSubmissionNotAdmitted(
	workID, sessionID, proposedTurnID, summary string,
	cancelAutoWork bool,
) (Work, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	proposedTurnID = strings.TrimSpace(proposedTurnID)
	summary = strings.TrimSpace(summary)
	if workID == "" || sessionID == "" || proposedTurnID == "" {
		return Work{}, fmt.Errorf("initial submission Work, Session, and Turn identities are required")
	}
	if summary == "" {
		summary = "Delegated input was proved not submitted."
	}
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("Work %s not found", workID)
	}
	item := database.BrainWork[itemIndex]
	if turn, found := currentTurnForSession(database, sessionID); found && turn.WorkID == workID {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("accepted delegated Turn %s cannot be recorded as not admitted", turn.TurnID)
	}
	for _, record := range database.BrainTurnSubmissions {
		if record.SessionID != sessionID || record.ProposedTurnID != proposedTurnID {
			continue
		}
		if record.State != watcher.TurnSubmissionAborted {
			s.mu.Unlock()
			return Work{}, fmt.Errorf("delegated submission %s is %s, not aborted", proposedTurnID, record.State)
		}
	}
	if item.Status == WorkDone || item.Status == WorkCancelled {
		if cancelAutoWork && item.Status == WorkCancelled {
			s.mu.Unlock()
			return item, nil
		}
		s.mu.Unlock()
		return Work{}, fmt.Errorf("Work %s is already %s", item.ID, item.Status)
	}
	if owner := strings.TrimSpace(item.OwnerSessionID); owner != "" && owner != sessionID {
		s.mu.Unlock()
		return Work{}, fmt.Errorf("%w: Work %s is owned by %s", ErrWorkOwnerConflict, item.ID, owner)
	}
	item.OwnerSessionID = ""
	item.OwnerDelegated = false
	if reservation := item.SuccessorReservation; reservation != nil && reservation.SessionID == sessionID {
		item.SuccessorReservation = nil
	}
	item.Wake = nil
	item.WaitFor = ""
	if cancelAutoWork {
		item.Status = WorkCancelled
		item.NextAction = "Delegated launch ended before its first Turn was admitted."
	} else {
		item.Status = WorkNeedsInput
		item.NextAction = "Retry the delegated Session after confirmed non-submission."
		item.WaitFor = summary
	}
	item.UpdatedAt = now
	item.Revision++
	database.BrainWork[itemIndex] = item

	dedupeKey := "brain:submission-abort:" + proposedTurnID
	eventIndex := -1
	for index, event := range database.BrainWorkEvents {
		if event.WorkID == workID && event.DedupeKey == dedupeKey {
			eventIndex = index
			break
		}
	}
	if eventIndex < 0 {
		event := WorkEvent{
			ID: uuid.NewString(), WorkID: workID, Kind: "brain.submission_not_admitted",
			DedupeKey: dedupeKey, PayloadRef: "session:" + sessionID, SourceName: sessionID,
			Summary: summary, Actionable: !cancelAutoWork, CreatedAt: now,
		}
		if cancelAutoWork {
			discardedAt := now
			event.DiscardedAt = &discardedAt
			event.Resolution = EventResolutionDiscard
		}
		if _, err := appendWorkEventLocked(&database, itemIndex, event, false); err != nil {
			s.mu.Unlock()
			return Work{}, err
		}
	} else {
		event := &database.BrainWorkEvents[eventIndex]
		event.Summary = summary
		event.SourceName = sessionID
		event.PayloadRef = "session:" + sessionID
		event.WorkRevision = item.Revision
		if cancelAutoWork && event.DeliveredAt == nil && event.HandlingID == "" {
			discardedAt := now
			event.Actionable = false
			event.DiscardedAt = &discardedAt
			event.Resolution = EventResolutionDiscard
		}
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		s.mu.Unlock()
		return Work{}, err
	}
	s.mu.Unlock()
	s.broadcastWorkChange(workID)
	return item, nil
}

// ResolveTurnSubmission atomically resolves provider admission and canonical
// ownership. Same exact baseline Activity resolves as steering to the
// existing Turn. Only a different confirmed Activity promotes the proposed
// fresh Turn. The provider admission digest must exactly match the pending
// payload digest.
func (s *Store) ResolveTurnSubmission(resolution watcher.TurnSubmissionResolution) (watcher.TurnSubmission, error) {
	resolution.SessionID = strings.TrimSpace(resolution.SessionID)
	resolution.ProposedTurnID = strings.TrimSpace(resolution.ProposedTurnID)
	resolution.Receipt = strings.TrimSpace(resolution.Receipt)
	resolution.PayloadSHA256 = strings.TrimSpace(resolution.PayloadSHA256)
	resolution.ActivityID = strings.TrimSpace(resolution.ActivityID)
	resolution.Admission.SHA256 = strings.TrimSpace(resolution.Admission.SHA256)
	if s == nil {
		return watcher.TurnSubmission{}, fmt.Errorf("brain store is not configured")
	}
	if resolution.ActivityID == "" || resolution.Admission.Empty() ||
		resolution.Admission.SHA256 == "" || resolution.Admission.SHA256 != resolution.PayloadSHA256 {
		return watcher.TurnSubmission{}, fmt.Errorf("provider admission does not match the pending payload digest")
	}
	now := s.nowUTC()
	if resolution.ResolvedAt.IsZero() {
		resolution.ResolvedAt = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSubmission{}, err
	}
	index := -1
	for i := range database.BrainTurnSubmissions {
		record := database.BrainTurnSubmissions[i]
		if record.SessionID == resolution.SessionID && record.ProposedTurnID == resolution.ProposedTurnID {
			index = i
			break
		}
	}
	if index < 0 {
		return watcher.TurnSubmission{}, fmt.Errorf("pending submission not found")
	}
	record := database.BrainTurnSubmissions[index]
	if record.Receipt != resolution.Receipt || record.PayloadSHA256 != resolution.PayloadSHA256 {
		return watcher.TurnSubmission{}, fmt.Errorf("submission identity belongs to different input")
	}
	if !resolution.Admission.At.IsZero() && resolution.Admission.At.Before(record.AcceptedAt) {
		return watcher.TurnSubmission{}, fmt.Errorf("provider admission predates the pending submission")
	}
	if resolution.ResolvedAt.Before(record.AcceptedAt) {
		return watcher.TurnSubmission{}, fmt.Errorf("submission resolution predates the pending submission")
	}
	switch record.State {
	case watcher.TurnSubmissionAborted:
		return watcher.TurnSubmission{}, fmt.Errorf("aborted submission can never be adopted")
	case watcher.TurnSubmissionRetired:
		return watcher.TurnSubmission{}, fmt.Errorf("retired submission authority can never be adopted")
	case watcher.TurnSubmissionResolved:
		// A matching Control signal may win the admission race before the
		// provider adapter reports its tuple. Exact later provider evidence
		// enriches that same Turn and submission; it never creates another Turn
		// or lifecycle Event.
		if record.SignalProtocol && record.ResolvedTurnID == record.ProposedTurnID &&
			record.ResolvedActivityID == "" && record.ResolvedAdmission.Empty() {
			for turnIndex := range database.BrainTurns {
				turn := &database.BrainTurns[turnIndex]
				if turn.SessionID != record.SessionID || turn.TurnID != record.ProposedTurnID {
					continue
				}
				turn.Admission = resolution.Admission
				turn.ActivityID = resolution.ActivityID
				turn.UpdatedAt = now
				record.ResolvedActivityID = resolution.ActivityID
				record.ResolvedAdmission = resolution.Admission
				database.BrainTurnSubmissions[index] = record
				if err := s.persistOrchestrationLocked(database); err != nil {
					return watcher.TurnSubmission{}, err
				}
				s.broadcastWorkChange(record.WorkID)
				return record.snapshot(), nil
			}
			return watcher.TurnSubmission{}, fmt.Errorf("resolved signal submission has no canonical Turn")
		}
		return record.snapshot(), nil
	case watcher.TurnSubmissionPending:
	default:
		return watcher.TurnSubmission{}, fmt.Errorf("invalid pending submission state %q", record.State)
	}
	current, hasCurrent := currentTurnForSession(database, record.SessionID)
	if record.ExistingTurnID == "" {
		if hasCurrent {
			return watcher.TurnSubmission{}, fmt.Errorf("canonical Turn changed after pending submission was prepared")
		}
	} else if !hasCurrent || current.TurnID != record.ExistingTurnID {
		return watcher.TurnSubmission{}, fmt.Errorf("canonical Turn changed after pending submission was prepared")
	}
	if record.Mode == watcher.TurnSubmissionConditionalSteer && current.ActivityID != record.BaselineActivityID {
		return watcher.TurnSubmission{}, fmt.Errorf("canonical steering activity changed before resolution")
	}
	resultTurnID := ""
	if !record.SignalProtocol && record.Mode == watcher.TurnSubmissionConditionalSteer && resolution.ActivityID == record.BaselineActivityID {
		resultTurnID = record.ExistingTurnID
		for index := range database.BrainTurns {
			turn := &database.BrainTurns[index]
			if turn.SessionID != record.SessionID || turn.TurnID != record.ExistingTurnID {
				continue
			}
			lease := resolution.ResolvedAt.Add(turnLeaseGrace).UTC()
			if lease.After(turn.LeaseDeadline) {
				turn.LeaseDeadline = lease
			}
			turn.UpdatedAt = now
			break
		}
	} else {
		for _, turn := range database.BrainTurns {
			if turn.SessionID == record.SessionID && turn.TurnID == record.ProposedTurnID {
				return watcher.TurnSubmission{}, fmt.Errorf("proposed turn already exists outside this pending transaction")
			}
		}
		fact := watcher.TurnFact{
			SessionID: record.SessionID, TurnID: record.ProposedTurnID,
			Class: watcher.EvidenceReceipt, Kind: "admission",
			SourceID: "receipt\x00" + record.Receipt + "\x00accepted\x00" + record.PayloadSHA256,
		}
		database.BrainTurns = append(database.BrainTurns, TurnRecord{
			SessionID: record.SessionID, TurnID: record.ProposedTurnID, WorkID: record.WorkID,
			Status: watcher.TurnAccepted, Receipt: record.Receipt,
			PaneGeneration: record.PaneGeneration, ProcessIdentity: record.ProcessIdentity,
			PayloadSHA256: record.PayloadSHA256, Admission: resolution.Admission,
			ActivityID: resolution.ActivityID, AcceptedAt: record.AcceptedAt,
			Summary: "Delegated input accepted",
			Facts: []TurnFactRecord{{
				FactID: fact.TurnFactIDFor(), Kind: fact.Kind, Class: fact.Class,
				At: resolution.ResolvedAt.UTC(), Summary: "Delegated input accepted",
			}},
			TranscriptBinding: record.TranscriptBinding,
			SignalProtocol:    record.SignalProtocol,
			LeaseDeadline:     resolution.ResolvedAt.Add(turnLeaseGrace).UTC(), UpdatedAt: now,
		})
		if workIndex := workIndex(database.BrainWork, record.WorkID); workIndex >= 0 {
			item := database.BrainWork[workIndex]
			if reservation := item.SuccessorReservation; record.ClaimToken == "" && reservation != nil && reservation.SessionID == record.SessionID {
				reservation.ProviderTurnID = record.ProposedTurnID
				item.SuccessorReservation = reservation
				database.BrainWork[workIndex] = item
			}
			// A successor Turn accepted during an in-flight Brain handling is
			// durable in the Turn Ledger, but attachment to Work belongs to the
			// exact continue disposition. Updating Work here would invalidate the
			// delivered revision before Brain could commit that disposition.
			if record.ClaimToken == "" && item.Status != WorkDone && item.Status != WorkCancelled &&
				!workHasInFlightHandling(database, record.WorkID) {
				update := derivedWorkUpdate(watcher.TurnAccepted, record.SessionID, "")
				if workUpdateChanges(item, update) {
					applyWorkUpdate(&item, update)
					item.UpdatedAt = now
					item.Revision++
					database.BrainWork[workIndex] = item
				}
			}
		}
		resultTurnID = record.ProposedTurnID
	}
	resolvedAt := resolution.ResolvedAt.UTC()
	record.State = watcher.TurnSubmissionResolved
	record.ResolvedTurnID = resultTurnID
	record.ResolvedActivityID = resolution.ActivityID
	record.ResolvedAdmission = resolution.Admission
	record.ResolvedAt = &resolvedAt
	database.BrainTurnSubmissions[index] = record
	if err := s.persistOrchestrationLocked(database); err != nil {
		return watcher.TurnSubmission{}, err
	}
	s.broadcastWorkChange(record.WorkID)
	return record.snapshot(), nil
}

func databaseWorkIDForTurnAdmission(database orchestrationDatabase, sessionID string) string {
	if workID := databaseActiveWorkIDForExecutionSession(database, sessionID); workID != "" {
		return workID
	}
	// A terminal or attention-relinquished Turn remains exact lifecycle
	// evidence for a same-Session correction. It may identify the Work for a
	// pending successor, but it does not restore progress ownership; preparation
	// below still requires the exact delivered handling before staging it.
	if current, found := currentTurnForSession(database, sessionID); found {
		if index := workIndex(database.BrainWork, current.WorkID); index >= 0 {
			item := database.BrainWork[index]
			reservationMatches := item.SuccessorReservation != nil && item.SuccessorReservation.SessionID == sessionID
			if item.Status != WorkDone && item.Status != WorkCancelled &&
				(item.OwnerSessionID == sessionID || reservationMatches ||
					(strings.TrimSpace(item.OwnerSessionID) == "" && (workHasAttentionObligation(database, item.ID) || item.Wake != nil))) {
				return item.ID
			}
		}
	}
	return ""
}

func databaseHasExactHostEventClaim(database orchestrationDatabase, submission watcher.TurnSubmission) bool {
	for _, event := range database.BrainWorkEvents {
		if event.ID == submission.Receipt && event.HandlingID == submission.ClaimToken &&
			event.WorkID == submission.WorkID && event.DeliveryHostSessionID == submission.SessionID &&
			event.ProviderTurnID == submission.ProposedTurnID && event.Actionable &&
			event.ClaimedAt != nil && event.DeliveredAt == nil && event.HandlingEndedAt == nil &&
			event.HandledAt == nil && event.DiscardedAt == nil && event.Resolution == "" &&
			!event.HistoricalDelivery {
			return true
		}
	}
	return false
}

func databaseHasResolvedHostEventAdmission(
	database orchestrationDatabase,
	eventID, claimToken, workID, hostSessionID, providerTurnID string,
) bool {
	for _, submission := range database.BrainTurnSubmissions {
		if submission.Receipt != eventID || submission.ClaimToken != claimToken ||
			submission.WorkID != workID || submission.SessionID != hostSessionID ||
			submission.ProposedTurnID != providerTurnID ||
			submission.State != watcher.TurnSubmissionResolved ||
			submission.ResolvedTurnID != providerTurnID {
			continue
		}
		for _, turn := range database.BrainTurns {
			if turn.SessionID == hostSessionID && turn.TurnID == providerTurnID &&
				turn.WorkID == workID && turn.Receipt == eventID && !turn.Admission.Empty() {
				return true
			}
		}
	}
	return false
}

// BackfillTurnTranscriptBinding idempotently records the provider-native
// transcript binding on the current turn when it is still empty (turns
// admitted before the binding existed, or rediscovered sessions whose tmux
// option holds the binding). It never overwrites an existing binding; the
// tmux option is only an advisory cache for sessions without a ledger record.
func (s *Store) BackfillTurnTranscriptBinding(sessionID string, binding watcher.TranscriptBinding) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" || binding.Empty() {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	turn, found := currentTurnForSession(database, sessionID)
	if !found || !turn.TranscriptBinding.Empty() {
		return false, nil
	}
	turn.TranscriptBinding = binding
	turn.UpdatedAt = s.nowUTC()
	for index := range database.BrainTurns {
		if database.BrainTurns[index].SessionID == turn.SessionID && database.BrainTurns[index].TurnID == turn.TurnID {
			database.BrainTurns[index] = turn
			break
		}
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return false, err
	}
	s.broadcastWorkChange(turn.WorkID)
	return true, nil
}

// Turn returns the canonical snapshot for the current turn of the session.
func (s *Store) Turn(sessionID string) (watcher.TurnSnapshot, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return watcher.TurnSnapshot{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	turn, found := currentTurnForSession(database, sessionID)
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
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == sessionID && turn.TurnID == turnID {
			return turn.snapshot(), true, nil
		}
	}
	return watcher.TurnSnapshot{}, false, nil
}

func currentTurnForSession(database orchestrationDatabase, sessionID string) (TurnRecord, bool) {
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
			(!providerFactBinds(turn, fact) && !providerRecoverableAdopts(turn, fact)) {
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
	adopts := !binding && (providerFactAdopts(turn, fact) || providerRecoverableAdopts(turn, fact))

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
				// contradicts any provisional same-kind hint (Phase 1b legacy
				// reconciliation: history showing the turn still running drops
				// the false done hint, C.2.8).
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
	case watcher.EvidenceLegacy:
		switch fact.Kind {
		case "done", "failed":
			kind := "session.done"
			if fact.Kind == "failed" {
				kind = "session.failed"
			}
			hintOnly(kind, "Legacy delegated turn marker reported "+fact.Kind)
			mutation.changed = true
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

// providerRecoverableAdopts is the one-way Unknown/ownership-loss recovery
// gate. Evidence loss
// may occur before a receipt/admission tuple or ActivityID is recorded. A
// later Provider terminal can still resolve the turn, but only when it carries
// a non-empty tuple or ActivityID and its StartedAt is within this turn's
// admission window. Running, attention, stale, liveness, blind and replay
// facts cannot adopt Unknown.
func providerRecoverableAdopts(turn *TurnRecord, fact watcher.TurnFact) bool {
	if fact.Class != watcher.EvidenceProvider ||
		(fact.Kind != "done" && fact.Kind != "failed") ||
		turn.Status != watcher.TurnUnknown ||
		turn.SignalProtocol ||
		!turn.Admission.Empty() || strings.TrimSpace(turn.ActivityID) != "" {
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

// ReassertLiveTurnOwnership repairs only the exact current nonterminal Turn's
// execution projection. It is used when authoritative provider activity stays
// live after a progress lease expires; Attention rows remain untouched and
// orthogonal.
func (s *Store) ReassertLiveTurnOwnership(workID, sessionID, turnID string) (Work, bool, error) {
	workID = strings.TrimSpace(workID)
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if workID == "" || sessionID == "" || turnID == "" {
		return Work{}, false, fmt.Errorf("Work, Session, and Turn identities are required")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return Work{}, false, err
	}
	turn, found := currentTurnForSession(database, sessionID)
	if !found || turn.TurnID != turnID || turn.WorkID != workID || watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(database, turn) {
		return Work{}, false, nil
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		return Work{}, false, ErrWorkNotFound
	}
	item := database.BrainWork[index]
	if item.Status == WorkDone || item.Status == WorkCancelled {
		return item, false, nil
	}
	if item.OwnerSessionID != "" && item.OwnerSessionID != sessionID {
		return Work{}, false, fmt.Errorf("%w: Work %s is owned by %s", ErrWorkOwnerConflict, workID, item.OwnerSessionID)
	}
	update := derivedWorkUpdate(turn.Status, sessionID, "")
	changed := item.OwnerSessionID != sessionID || !item.OwnerDelegated || workUpdateChanges(item, update)
	if !changed {
		return item, false, nil
	}
	item.OwnerSessionID = sessionID
	item.OwnerDelegated = true
	applyWorkUpdate(&item, update)
	item.Revision++
	item.UpdatedAt = now
	database.BrainWork[index] = item
	if err := s.persistOrchestrationLocked(database); err != nil {
		return Work{}, false, err
	}
	s.broadcastWorkChange(workID)
	return item, true, nil
}

// prepareDelegatedSignalTurnLocked validates the one prompt-carried identity
// against authoritative orchestration state. If it names the current pending
// delegated submission, this promotes that exact candidate to Accepted and
// resolves the submission in memory. The caller immediately feeds the same
// Control fact through reduceTurnFact and persists both decisions together.
func prepareDelegatedSignalTurnLocked(database *orchestrationDatabase, fact watcher.TurnFact, now time.Time) error {
	if database == nil {
		return errNoDelegatedSignalContract
	}
	pendingIndex := -1
	for index := range database.BrainTurnSubmissions {
		submission := database.BrainTurnSubmissions[index]
		if submission.SessionID == fact.SessionID && submission.State == watcher.TurnSubmissionPending {
			pendingIndex = index
			break
		}
	}
	if pendingIndex < 0 {
		current, found := currentTurnForSession(*database, fact.SessionID)
		if !found || !current.SignalProtocol {
			return errNoDelegatedSignalContract
		}
		if fact.TurnID == "" || current.TurnID != fact.TurnID {
			return errDelegatedTurnMismatch
		}
		return nil
	}

	record := database.BrainTurnSubmissions[pendingIndex]
	if !record.SignalProtocol || strings.TrimSpace(record.ClaimToken) != "" {
		return errNoDelegatedSignalContract
	}
	if fact.TurnID == "" || fact.TurnID != record.ProposedTurnID || fact.At.Before(record.CreatedAt) {
		return errDelegatedTurnMismatch
	}
	current, hasCurrent := currentTurnForSession(*database, record.SessionID)
	if record.ExistingTurnID == "" {
		if hasCurrent {
			return errDelegatedTurnMismatch
		}
	} else if !hasCurrent || current.TurnID != record.ExistingTurnID {
		return errDelegatedTurnMismatch
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == record.SessionID && turn.TurnID == record.ProposedTurnID {
			return fmt.Errorf("proposed delegated signal Turn already exists outside its pending submission")
		}
	}

	database.BrainTurns = append(database.BrainTurns, TurnRecord{
		SessionID: record.SessionID, TurnID: record.ProposedTurnID, WorkID: record.WorkID,
		Status: watcher.TurnAccepted, Receipt: record.Receipt,
		PaneGeneration: record.PaneGeneration, ProcessIdentity: record.ProcessIdentity,
		PayloadSHA256: record.PayloadSHA256, AcceptedAt: record.AcceptedAt,
		Summary: "Delegated input admitted by matching control signal",
		Facts:   []TurnFactRecord{}, TranscriptBinding: record.TranscriptBinding,
		SignalProtocol: true, LeaseDeadline: now.Add(turnLeaseGrace).UTC(), UpdatedAt: now,
	})
	if index := workIndex(database.BrainWork, record.WorkID); index >= 0 {
		item := database.BrainWork[index]
		if reservation := item.SuccessorReservation; reservation != nil && reservation.SessionID == record.SessionID {
			reservation.ProviderTurnID = record.ProposedTurnID
			item.SuccessorReservation = reservation
			database.BrainWork[index] = item
		}
		if item.Status != WorkDone && item.Status != WorkCancelled && !workHasInFlightHandling(*database, record.WorkID) {
			update := derivedWorkUpdate(watcher.TurnAccepted, record.SessionID, "")
			if workUpdateChanges(item, update) {
				applyWorkUpdate(&item, update)
				item.UpdatedAt = now
				item.Revision++
				database.BrainWork[index] = item
			}
		}
	}
	resolvedAt := fact.At.UTC()
	record.State = watcher.TurnSubmissionResolved
	record.ResolvedTurnID = record.ProposedTurnID
	record.ResolvedAt = &resolvedAt
	database.BrainTurnSubmissions[pendingIndex] = record
	return nil
}

// ApplyTurnFact is the single canonical reducer. Under one lock and one
// persist it: dedupes the deterministic FactID, validates the transition,
// mutates the turn, derives the Work update, and appends or upgrades the
// outbox event (non-actionable → actionable in-place flip for corrections).
// A replayed or reordered fact is a no-op; terminal turns are immutable.
//
// Terminal Work (done/cancelled) is a terminal scheduler decision: a later
// fact may advance the turn row and is retained as non-actionable outbox
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
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	if delegatedSignal {
		if fact.Class != watcher.EvidenceControl {
			return watcher.TurnSnapshot{}, false, fmt.Errorf("delegated progress requires Control evidence")
		}
		if err := prepareDelegatedSignalTurnLocked(&database, fact, now); err != nil {
			return watcher.TurnSnapshot{}, false, err
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
		// prompt Turn. A late previous-turn completion after Session reuse is
		// retained by its provider transcript, but cannot terminate the newer
		// canonical Turn, mutate Work, or wake Brain.
		if current, currentSet := currentTurnForSession(database, fact.SessionID); currentSet && current.TurnID != fact.TurnID {
			return watcher.TurnSnapshot{}, false, nil
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
	if isHostHandlingTurn(database, turn) {
		// Host provider Turns own only the delivery handling. Their lifecycle
		// may close/recover that exact handling, but must never be reinterpreted
		// as delegated Work progress or emit a second scheduler signal.
		database.BrainTurns[turnIndex] = turn
		if err := s.persistOrchestrationLocked(database); err != nil {
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
	workChanged := false
	terminalWork := false
	dispositionRevisionFrozen := false
	if workIndex >= 0 {
		workItem = database.BrainWork[workIndex]
		terminalWork = workItem.Status == WorkDone || workItem.Status == WorkCancelled
		// A delivered Host handling carries the exact Work revision required
		// for its eventual disposition. A newly admitted successor can report
		// progress before that disposition commits; its Turn facts/outbox rows
		// remain durable, but they cannot mutate Work or advance the capability's
		// revision fence. ResolveWorkEvent is the sole owner of that transition.
		dispositionRevisionFrozen = workHasInFlightHandling(database, turn.WorkID)
	}
	if !terminalWork && !dispositionRevisionFrozen && mutation.hint != nil && workIndex >= 0 {
		note := "Delegated Session reported " +
			strings.TrimPrefix(mutation.hint.Kind, "session.") +
			"; awaiting provider confirmation"
		if workUpdateChanges(workItem, WorkUpdate{NextAction: &note}) {
			applyWorkUpdate(&workItem, WorkUpdate{NextAction: &note})
			workItem.UpdatedAt = now
			database.BrainWork[workIndex] = workItem
			workChanged = true
		}
	}
	if !terminalWork && !dispositionRevisionFrozen && (mutation.workUpdate.Status != nil || mutation.workUpdate.NextAction != nil) {
		update := mutation.workUpdate
		if workIndex >= 0 && workUpdateChanges(workItem, update) {
			applyWorkUpdate(&workItem, update)
			workItem.UpdatedAt = now
			database.BrainWork[workIndex] = workItem
			workChanged = true
		}
	}
	if !terminalWork && !dispositionRevisionFrozen && mutation.eventActionable &&
		(turn.ControlState == watcher.TurnControlOwnershipLost ||
			watcher.TurnTerminal(turn.Status) && !watcher.TurnImmutable(turn.Status)) &&
		workIndex >= 0 && strings.TrimSpace(workItem.OwnerSessionID) == turn.SessionID {
		// A recoverable terminal (Unknown) or orthogonal control loss deprojects
		// exact execution ownership. An immutable provider result
		// remains Done/Failed even when its control target is no longer safe.
		// Blocked and lease-overdue live turns retain their owner while Attention
		// remains an orthogonal Brain obligation.
		workItem.OwnerSessionID = ""
		workItem.OwnerDelegated = false
		workItem.UpdatedAt = now
		database.BrainWork[workIndex] = workItem
		workChanged = true
	}

	// Outbox event: exactly one row per (work, dedupe key); corrections flip
	// the existing non-actionable row actionable in place. Terminal Work keeps
	// the late row as non-actionable audit only: it is never claimed and never
	// flips, so no second wake is possible (C.2.9).
	eventCreated := false
	eventID := ""
	workID := turn.WorkID
	revisionBumped := false
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
				ID:         uuid.NewString(),
				WorkID:     workID,
				Kind:       mutation.eventKind,
				DedupeKey:  dedupeKey,
				PayloadRef: "session:" + turn.SessionID,
				SourceName: turn.SessionID,
				Summary:    mutation.eventSummary,
				Actionable: actionable,
				CreatedAt:  now,
			}
			if workIndex >= 0 {
				if workChanged {
					workItem.Revision++
					database.BrainWork[workIndex] = workItem
					revisionBumped = true
				}
				event, err = appendWorkEventLocked(&database, workIndex, event, !revisionBumped && !dispositionRevisionFrozen)
				if err != nil {
					return watcher.TurnSnapshot{}, false, err
				}
				workItem = database.BrainWork[workIndex]
				revisionBumped = true
			} else {
				return watcher.TurnSnapshot{}, false, ErrWorkNotFound
			}
			eventID = event.ID
			eventCreated = true
		} else if actionable &&
			!database.BrainWorkEvents[eventIndex].Actionable {
			// In-place correction flip: the same row becomes actionable; the
			// row count never changes, so no second wake is possible.
			if workIndex >= 0 && !revisionBumped && !dispositionRevisionFrozen {
				workItem.Revision++
				workItem.UpdatedAt = now
				database.BrainWork[workIndex] = workItem
				revisionBumped = true
			}
			database.BrainWorkEvents[eventIndex].Actionable = true
			database.BrainWorkEvents[eventIndex].Summary = mutation.eventSummary
			database.BrainWorkEvents[eventIndex].SourceName = turn.SessionID
			database.BrainWorkEvents[eventIndex].WorkRevision = workItem.Revision
			if readyID := readyAttentionEventID(database, workID); readyID != "" &&
				readyID != database.BrainWorkEvents[eventIndex].ID {
				database.BrainWorkEvents[eventIndex].CoalescedInto = readyID
			}
			eventID = database.BrainWorkEvents[eventIndex].ID
			eventCreated = true
		}
	}
	if workChanged && !revisionBumped && !dispositionRevisionFrozen && workIndex >= 0 {
		workItem.Revision++
		database.BrainWork[workIndex] = workItem
	}

	// Canonical producer terminalization and every matching cross-Work wake
	// share this one orchestration persist. A crash cannot expose the producer
	// terminal without the consumer attention, and FactID makes replay a no-op.
	wokenWorkIDs := []string{}
	if mutation.eventActionable && (mutation.eventKind == "session.done" ||
		mutation.eventKind == "session.failed" || mutation.eventKind == "session.uncertain") {
		_, wokenWorkIDs, err = wakeWaitingWorkLocked(
			&database,
			WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(turn.SessionID, turn.TurnID)},
			mutation.eventKind,
			factID,
			mutation.eventSummary,
			now,
		)
		if err != nil {
			return watcher.TurnSnapshot{}, false, err
		}
	}

	database.BrainTurns[turnIndex] = turn
	if err := s.persistOrchestrationLocked(database); err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	if workChanged || eventCreated {
		s.broadcastWorkChange(workID)
	}
	for _, wokenWorkID := range wokenWorkIDs {
		s.broadcastWorkChange(wokenWorkID)
	}
	if eventCreated && isProjectedWorkResultEvent(mutation.eventKind) {
		if workIndex >= 0 {
			// materializeWorkCardLocked is used directly: we already hold s.mu.
			_, _, _ = s.materializeWorkCardLocked(workItem, WorkEvent{
				ID:         eventID,
				WorkID:     workID,
				Kind:       mutation.eventKind,
				PayloadRef: "session:" + turn.SessionID,
				Summary:    mutation.eventSummary,
				Actionable: mutation.eventActionable,
			})
		}
	}
	return turn.snapshot(), true, nil
}

// MigrateTurnLedgerV1 performs the resumable legacy tmux-marker import
// (C.2.8): canonical status is Admitted/Running only; done/failed markers
// attach a Legacy hint that never changes canonical status. All later writes
// go to the ledger.
//
// The migration is crash-resumable and idempotent: this import phase never
// persists the completion marker — CompleteTurnLedgerV1Migration does that
// only after every later phase (Phase 1b reconciliation, marker cleanup)
// finished. A crash between phases re-runs import (existing rows skipped),
// reconciliation (deterministic FactIDs dedupe), and completion (no-op).
func (s *Store) MigrateTurnLedgerV1(imports []TurnLedgerImport) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("brain store is not configured")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return false, err
	}
	imported := false
	for _, candidate := range imports {
		candidate.SessionID = strings.TrimSpace(candidate.SessionID)
		candidate.TurnID = strings.TrimSpace(candidate.TurnID)
		candidate.WorkID = strings.TrimSpace(candidate.WorkID)
		if candidate.SessionID == "" || candidate.TurnID == "" {
			continue
		}
		if candidate.WorkID == "" {
			for _, item := range database.BrainWork {
				if strings.TrimSpace(item.OwnerSessionID) == candidate.SessionID {
					candidate.WorkID = item.ID
					break
				}
			}
		}
		workExists := false
		for _, item := range database.BrainWork {
			if item.ID == candidate.WorkID {
				workExists = true
				break
			}
		}
		if !workExists {
			// A marker without an owning Work cannot be imported; it is left
			// quarantined rather than failing the whole migration.
			continue
		}
		exists := false
		for _, turn := range database.BrainTurns {
			if turn.SessionID == candidate.SessionID && turn.TurnID == candidate.TurnID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		status := candidate.Status
		if status != watcher.TurnAdmitted && status != watcher.TurnRunning {
			status = watcher.TurnRunning
		}
		record := TurnRecord{
			SessionID:       candidate.SessionID,
			TurnID:          candidate.TurnID,
			WorkID:          candidate.WorkID,
			Status:          status,
			PaneGeneration:  candidate.PaneGeneration,
			ProcessIdentity: candidate.ProcessIdentity,
			AcceptedAt:      candidate.AcceptedAt,
			Summary:         candidate.Summary,
			Facts:           []TurnFactRecord{},
			UpdatedAt:       now,
		}
		if candidate.AcceptedAt.IsZero() {
			record.AcceptedAt = now
		}
		// Per-turn liveness backfill: imported rows get one fresh upgrade
		// grace from migration time, never from their old AcceptedAt. This
		// prevents a live pre-upgrade turn from staling on the first tick.
		record.LeaseDeadline = now.Add(turnLeaseGrace).UTC()
		if candidate.Hint != nil {
			record.Hints = []watcher.TurnHint{*candidate.Hint}
		}
		database.BrainTurns = append(database.BrainTurns, record)
		imported = true
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return false, err
	}
	if imported {
		s.broadcastWorkChange("")
	}
	return imported, nil
}

// CompleteTurnLedgerV1Migration durably records that every migration phase
// finished. It is the ONLY writer of the TurnLedgerV1At marker and must run
// after import, Phase 1b reconciliation, and marker cleanup, so a crash in
// any earlier phase never skips the remaining work. Idempotent.
func (s *Store) CompleteTurnLedgerV1Migration() error {
	if s == nil {
		return fmt.Errorf("brain store is not configured")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	if database.Migrations.TurnLedgerV1At != nil {
		return nil
	}
	database.Migrations.TurnLedgerV1At = &now
	return s.persistOrchestrationLocked(database)
}

// PruneSettledTurns removes closed-turn ledger rows whose terminal events are
// consumed and whose settlement predates olderThan. Held/uncertain turns are
// never pruned. Returns the number of pruned rows.
func (s *Store) PruneSettledTurns(olderThan time.Time) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("brain store is not configured")
	}
	olderThan = olderThan.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return 0, err
	}
	pruned := 0
	kept := database.BrainTurns[:0]
	for _, turn := range database.BrainTurns {
		if turn.Status != watcher.TurnDone && turn.Status != watcher.TurnFailed {
			kept = append(kept, turn)
			continue
		}
		if turn.SettledAt == nil || turn.SettledAt.After(olderThan) {
			kept = append(kept, turn)
			continue
		}
		consumed := false
		eventKind := "session.done"
		if turn.Status == watcher.TurnFailed {
			eventKind = "session.failed"
		}
		dedupeKey := sessionTurnEventDedupeKey(turn.SessionID, turn.TurnID, eventKind)
		for _, event := range database.BrainWorkEvents {
			if event.WorkID == turn.WorkID && event.DedupeKey == dedupeKey &&
				event.HandledAt != nil && event.Actionable {
				consumed = true
				break
			}
		}
		if !consumed {
			kept = append(kept, turn)
			continue
		}
		pruned++
	}
	database.BrainTurns = kept
	if pruned == 0 {
		return 0, nil
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return 0, err
	}
	s.broadcastWorkChange("")
	return pruned, nil
}

// AppendDeliveryNote appends a deduped delivery diagnostic for a held claim
// (delivery.ambiguous non-actionable, delivery.uncertain actionable). It is
// scheduler audit, not a Work-state mutation, so it preserves the revision
// fence carried by the in-flight claim. Returns the existing row on duplicate.
func (s *Store) AppendDeliveryNote(workID, eventID, kind, dedupeKey, summary string, actionable bool) (WorkEvent, bool, error) {
	if s == nil {
		return WorkEvent{}, false, fmt.Errorf("brain store is not configured")
	}
	eventID = strings.TrimSpace(eventID)
	if workID == "" || eventID == "" || kind == "" || dedupeKey == "" {
		return WorkEvent{}, false, fmt.Errorf("delivery note requires work_id, event_id, kind, and dedupe_key")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
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
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(workID)
	return event, true, nil
}

// MarkDeliveredClaim closes a held claim by explicit user assertion that the
// Host received the event (C.2.6.1). It records delivery only, then requeues
// the Work key for a typed disposition. Actor-recorded, never time-based.
func (s *Store) MarkDeliveredClaim(eventID, actor, reason string) error {
	return s.resolveClaimWithReconcile(eventID, actor, reason, func(event *WorkEvent, now time.Time) {
		deliveredAt := now.UTC()
		event.DeliveredAt = &deliveredAt
		event.HandlingEndedAt = &deliveredAt
		event.HistoricalDelivery = true
		event.Resolution = EventResolutionMarkDelivered
		event.ResolvedBy = actor
		event.ResolvedAt = &now
	})
}

// DiscardClaim abandons a held delivery (C.2.6.2). The row leaves the held
// set forever; Brain separately reconciles the owning Work.
func (s *Store) DiscardClaim(eventID, actor, reason string) error {
	return s.resolveClaimWithReconcile(eventID, actor, reason, func(event *WorkEvent, now time.Time) {
		event.DiscardedAt = &now
		event.Resolution = EventResolutionDiscard
		event.ResolvedBy = actor
		event.ResolvedAt = &now
	})
}

// ReplayEvent performs an explicit user-authorized replay as a new event with
// a new identity and key (C.2.6.3). This is the only mechanism that creates a
// second actionable row for one semantic fact, and only with authorization.
//
// Replay is a bounded transition: the original must be an unresolved held
// claim (ClaimedAt set, not consumed, not discarded, never resolved before),
// so a second replay of the same original is rejected and the resolved
// original leaves the held set forever. The replay row retains the audited
// ReplayOf identity; the original records the resolution actor and time.
func (s *Store) ReplayEvent(eventID, actor, reason string) (WorkEvent, error) {
	eventID = strings.TrimSpace(eventID)
	actor = strings.TrimSpace(actor)
	if eventID == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return WorkEvent{}, fmt.Errorf("replay requires event_id, actor, and reason")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return WorkEvent{}, err
	}
	index := workEventIndex(database.BrainWorkEvents, eventID)
	if index < 0 {
		return WorkEvent{}, ErrEventClaim
	}
	original := database.BrainWorkEvents[index]
	if original.ClaimedAt == nil {
		return WorkEvent{}, fmt.Errorf("event %s is not a held claim; no replay", eventID)
	}
	if original.DeliveredAt != nil || original.DiscardedAt != nil {
		return WorkEvent{}, fmt.Errorf("event %s is already resolved; no replay", eventID)
	}
	if original.Resolution != "" {
		return WorkEvent{}, fmt.Errorf("event %s was already replayed; a second replay is not authorized", eventID)
	}
	if err := retireExactHostSubmissionForClaim(&database, original, now); err != nil {
		return WorkEvent{}, err
	}
	nonce := uuid.NewString()
	replay := WorkEvent{
		ID:         uuid.NewString(),
		WorkID:     original.WorkID,
		Kind:       original.Kind,
		DedupeKey:  "delivery:" + eventID + ":replay:" + nonce,
		PayloadRef: original.PayloadRef,
		SourceName: original.SourceName,
		Summary:    original.Summary,
		Actionable: original.Actionable,
		CreatedAt:  now,
		ReplayOf:   eventID,
	}
	original.Resolution = EventResolutionReplayed
	original.ResolvedBy = actor
	original.ResolvedAt = &now
	database.BrainWorkEvents[index] = original
	itemIndex := workIndex(database.BrainWork, replay.WorkID)
	replay, err = appendWorkEventLocked(&database, itemIndex, replay, true)
	if err != nil {
		return WorkEvent{}, err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, err
	}
	s.broadcastWorkChange(replay.WorkID)
	return replay, nil
}

func (s *Store) resolveClaimWithReconcile(eventID, actor, reason string, resolve func(*WorkEvent, time.Time)) error {
	eventID = strings.TrimSpace(eventID)
	actor = strings.TrimSpace(actor)
	if eventID == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("claim resolution requires event_id, actor, and reason")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	index := workEventIndex(database.BrainWorkEvents, eventID)
	if index < 0 {
		return ErrEventClaim
	}
	event := &database.BrainWorkEvents[index]
	if event.ClaimedAt == nil {
		return fmt.Errorf("event %s is not claimed", eventID)
	}
	if event.DeliveredAt != nil || event.DiscardedAt != nil {
		return fmt.Errorf("event %s is already resolved", eventID)
	}
	if err := retireExactHostSubmissionForClaim(&database, *event, now); err != nil {
		return err
	}
	resolve(event, now)
	itemIndex := workIndex(database.BrainWork, event.WorkID)
	if itemIndex < 0 {
		return ErrWorkNotFound
	}
	reconcile := WorkEvent{
		ID: uuid.NewString(), WorkID: event.WorkID, Kind: "brain.reconcile_required",
		DedupeKey: "brain:delivery-resolution:" + event.ID, PayloadRef: "work:" + event.WorkID,
		SourceName: "brain", Summary: "A held Host delivery was resolved; the current Work state requires a disposition.",
		Actionable: true, CreatedAt: now,
	}
	if _, err := appendWorkEventLocked(&database, itemIndex, reconcile, true); err != nil {
		return err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return err
	}
	s.broadcastWorkChange(database.BrainWorkEvents[index].WorkID)
	return nil
}

// retireExactHostSubmissionForClaim closes only the pre-mutation transaction
// authorized by the held Event's complete five-part capability. Manual claim
// resolution is neither proof of non-submission nor provider admission, so it
// gets its own terminal state: future provider evidence cannot adopt it, and a
// replacement Event is no longer blocked by the Session's sole-pending gate.
// The caller persists this mutation together with the Event resolution.
func retireExactHostSubmissionForClaim(database *orchestrationDatabase, event WorkEvent, now time.Time) error {
	if database == nil || event.ID == "" || event.HandlingID == "" || event.WorkID == "" ||
		event.DeliveryHostSessionID == "" || event.ProviderTurnID == "" {
		return nil
	}
	for index := range database.BrainTurnSubmissions {
		submission := &database.BrainTurnSubmissions[index]
		if submission.Receipt != event.ID || submission.ClaimToken != event.HandlingID ||
			submission.WorkID != event.WorkID || submission.SessionID != event.DeliveryHostSessionID ||
			submission.ProposedTurnID != event.ProviderTurnID {
			continue
		}
		switch submission.State {
		case watcher.TurnSubmissionPending:
			retiredAt := now.UTC()
			submission.State = watcher.TurnSubmissionRetired
			submission.ResolvedAt = &retiredAt
			return nil
		case watcher.TurnSubmissionAborted, watcher.TurnSubmissionResolved, watcher.TurnSubmissionRetired:
			return nil
		default:
			return fmt.Errorf("exact Host submission has invalid state %q", submission.State)
		}
	}
	return nil
}

// ErrNoActiveTurn is returned when a session has no canonical turn.
var ErrNoActiveTurn = errors.New("no canonical turn for session")
