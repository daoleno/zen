package brain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/google/uuid"
)

// TurnRecord is the durable canonical per-turn lifecycle row (schema v5,
// "brain_turns" inside orchestration.json). It is the single owner of every
// lifecycle transition for an accepted delegated turn: Work status and outbox
// events are derived from it by the one reducer, never inferred independently.
type TurnRecord struct {
	SessionID       string               `json:"session_id"`
	TurnID          string               `json:"turn_id"`
	WorkID          string               `json:"work_id"`
	Status          watcher.TurnStatus   `json:"status"`
	Receipt         string               `json:"receipt,omitempty"`
	PaneGeneration  string               `json:"pane_generation,omitempty"`
	ProcessIdentity string               `json:"process_identity,omitempty"`
	PayloadSHA256   string               `json:"payload_sha256,omitempty"`
	Admission       watcher.TurnAdmission `json:"admission,omitempty"`
	ActivityID      string               `json:"activity_id,omitempty"`
	Attention       string               `json:"attention,omitempty"`
	AcceptedAt      time.Time            `json:"accepted_at,omitempty"`
	SettledAt       *time.Time           `json:"settled_at,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	Facts           []TurnFactRecord     `json:"facts"`
	Hints           []watcher.TurnHint   `json:"hints,omitempty"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

// TurnFactRecord is one durable applied observation on a turn. FactID is the
// deterministic frozen identity (watcher.TurnFactID), so replayed and
// reordered facts dedupe identically across restart.
type TurnFactRecord struct {
	FactID  string               `json:"fact_id"`
	Kind    string               `json:"kind"`
	Class   watcher.EvidenceClass `json:"evidence_class"`
	Bound   bool                 `json:"bound,omitempty"`
	At      time.Time            `json:"at,omitempty"`
	Summary string               `json:"summary,omitempty"`
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
	return nil
}

func (t TurnRecord) snapshot() watcher.TurnSnapshot {
	snapshot := watcher.TurnSnapshot{
		SessionID:      t.SessionID,
		TurnID:         t.TurnID,
		Status:         t.Status,
		AcceptedAt:     t.AcceptedAt,
		SettledAt:      t.SettledAt,
		Summary:        t.Summary,
		Attention:      t.Attention,
		ActivityID:     t.ActivityID,
		Admission:      t.Admission,
		HasAdmission:   !t.Admission.Empty(),
		Hints:          append([]watcher.TurnHint(nil), t.Hints...),
		PaneGeneration: t.PaneGeneration,
		UpdatedAt:      t.UpdatedAt,
	}
	return snapshot
}

// AdmitTurn durably records the Admitted turn before any provider mutation can
// begin (C.2 invariant 2): a markerless accepted input is unrepresentable.
// The owning Work must exist and be active; otherwise admission fails closed
// as not-submitted. Idempotent per (session, turn).
func (s *Store) AdmitTurn(admitted watcher.AdmittedTurn) error {
	if s == nil {
		return fmt.Errorf("brain store is not configured")
	}
	admitted.SessionID = strings.TrimSpace(admitted.SessionID)
	admitted.TurnID = strings.TrimSpace(admitted.TurnID)
	if admitted.SessionID == "" || admitted.TurnID == "" {
		return fmt.Errorf("session_id and turn_id are required")
	}
	if admitted.AcceptedAt.IsZero() {
		return fmt.Errorf("accepted_at is required")
	}
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	workID := ""
	for _, item := range database.BrainWork {
		if strings.TrimSpace(item.OwnerSessionID) == admitted.SessionID &&
			item.Status != WorkDone && item.Status != WorkCancelled {
			workID = item.ID
			break
		}
	}
	if workID == "" {
		return fmt.Errorf("no active Brain Work owns delegated Session %s; input was not submitted", admitted.SessionID)
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == admitted.SessionID && turn.TurnID == admitted.TurnID {
			return nil
		}
	}
	database.BrainTurns = append(database.BrainTurns, TurnRecord{
		SessionID:       admitted.SessionID,
		TurnID:          admitted.TurnID,
		WorkID:          workID,
		Status:          watcher.TurnAdmitted,
		Receipt:         strings.TrimSpace(admitted.Receipt),
		PaneGeneration:  strings.TrimSpace(admitted.PaneGeneration),
		ProcessIdentity: strings.TrimSpace(admitted.ProcessIdentity),
		PayloadSHA256:   strings.TrimSpace(admitted.PayloadSHA256),
		AcceptedAt:      admitted.AcceptedAt.UTC(),
		Facts:           []TurnFactRecord{},
		UpdatedAt:       now,
	})
	return s.persistOrchestrationLocked(database)
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
	status              watcher.TurnStatus
	attention           string
	settledAt           *time.Time
	summary             string
	recordAdmission     bool
	admission           watcher.TurnAdmission
	activityID          string
	eventKind           string
	eventActionable     bool
	eventSummary        string
	hint                *watcher.TurnHint
	dropHintKind        string
	workUpdate          WorkUpdate
	changed             bool
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
	switch turn.Status {
	case watcher.TurnDone, watcher.TurnFailed:
		// Globally final and immutable: nothing can mutate a terminal turn.
		return mutation, nil
	}

	status := turn.Status
	attention := turn.Attention
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
		case "admission":
			if status != watcher.TurnAdmitted || fact.Admission.Empty() {
				return mutation, nil
			}
			status = watcher.TurnAccepted
			admission = fact.Admission
			activityID = firstNonEmpty(activityID, fact.ActivityID)
			summary = firstNonEmpty(fact.Summary, "Delegated turn accepted")
			mutation.changed = true
		case "running":
			if !binding && !adopts {
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
			mutation.dropHintKind = firstNonEmpty(mutation.dropHintKind, "done")
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
			// began) and never terminalizes.
			switch status {
			case watcher.TurnAccepted, watcher.TurnRunning:
				if fact.Summary != "" && fact.Summary != summary {
					summary = fact.Summary
				}
				attention = ""
				mutation.changed = attention != turn.Attention || summary != turn.Summary
			case watcher.TurnBlocked:
				status = watcher.TurnRunning
				attention = ""
				if fact.Summary != "" {
					summary = fact.Summary
				} else {
					summary = firstNonEmpty(summary, "Delegated turn running")
				}
				mutation.changed = true
			}
		case "attention":
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
			// Control terminal self-report: hint only, never canonical, and
			// only for turns whose admission has been correlated (at >=
			// AcceptedAt). Admitted stays Admitted.
			if status == watcher.TurnAdmitted || fact.At.Before(turn.AcceptedAt) {
				return mutation, nil
			}
			hintOnly(kind, "Delegated Session reported "+fact.Kind+"; awaiting provider confirmation")
			mutation.changed = true
		case "stale":
			// Lease expiry: no canonical change; one actionable session.stale
			// per turn wakes Brain.
			applyEvent("session.stale", true, "Delegated Session lease expired; inspect the Session")
			mutation.changed = true
		}
	case watcher.EvidenceReceipt:
		if fact.Kind == "admission" && status == watcher.TurnAdmitted &&
			!fact.Admission.Empty() {
			status = watcher.TurnAccepted
			admission = fact.Admission
			activityID = firstNonEmpty(activityID, fact.ActivityID)
			summary = firstNonEmpty(fact.Summary, "Delegated input accepted")
			mutation.changed = true
		}
	case watcher.EvidenceLiveness:
		switch fact.Kind {
		case "failed":
			if !fact.AbnormalExit {
				return mutation, fmt.Errorf("liveness failed fact requires abnormal-exit proof")
			}
			if status == watcher.TurnAdmitted {
				// An unproven input cannot fail; end-of-identity on Admitted
				// resolves to Unknown (uncertain, actionable).
				status = watcher.TurnUnknown
				attention = ""
				summary = "Delegated Session ended before input admission was proven"
			} else {
				status = watcher.TurnFailed
				attention = ""
				summary = firstNonEmpty(fact.Summary, "Delegated Session ended abnormally")
			}
			if fact.SettledAt.IsZero() {
				settledAt = &now
			} else {
				settled := fact.SettledAt.UTC()
				settledAt = &settled
			}
			mutation.changed = true
			applyEvent("session.failed", true, summary)
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
	// canonical values and never change Work status.
	rowChanged := status != turn.Status ||
		attention != turn.Attention ||
		!sameTime(settledAt, turn.SettledAt) ||
		summary != turn.Summary ||
		(!admission.Empty() && (turn.Admission.Empty() ||
			admission.Cursor != turn.Admission.Cursor ||
			admission.ID != turn.Admission.ID)) ||
		(activityID != "" && activityID != turn.ActivityID)
	if rowChanged {
		mutation.status = status
		mutation.attention = attention
		mutation.settledAt = settledAt
		mutation.summary = summary
		if mutation.recordAdmission && !admission.Empty() {
			mutation.admission = admission
		}
		if activityID != "" && activityID != turn.ActivityID {
			mutation.activityID = activityID
		}
		if mutation.status != "" {
			mutation.workUpdate = derivedWorkUpdate(status, turn.SessionID, mutation.eventKind)
		}
	} else if mutation.eventKind == "session.stale" {
		mutation.workUpdate = derivedWorkUpdate(status, turn.SessionID, mutation.eventKind)
	}
	if rowChanged || mutation.eventKind != "" || mutation.hint != nil {
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
	switch status {
	case watcher.TurnAccepted, watcher.TurnRunning:
		next := "Wait for the delegated Session."
		return WorkUpdate{Status: &running, NextAction: &next, WaitFor: &sessionWait}
	case watcher.TurnBlocked:
		next := "Resolve the delegated Session request."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait}
	case watcher.TurnDone:
		return terminalSessionWorkUpdate("session.done")
	case watcher.TurnFailed:
		return terminalSessionWorkUpdate("session.failed")
	case watcher.TurnUnknown:
		next := "Confirm whether the delegated Session received the prompt; delivery will not be replayed."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait}
	}
	if eventKind == "session.stale" {
		next := "Inspect the delegated Session lease expiry."
		return WorkUpdate{Status: &needsInput, NextAction: &next, WaitFor: &sessionWait}
	}
	return WorkUpdate{Status: &waiting}
}

// ApplyTurnFact is the single canonical reducer. Under one lock and one
// persist it: dedupes the deterministic FactID, validates the transition,
// mutates the turn, derives the Work update, and appends or upgrades the
// outbox event (non-actionable → actionable in-place flip for corrections).
// A replayed or reordered fact is a no-op; terminal turns are immutable.
func (s *Store) ApplyTurnFact(fact watcher.TurnFact) (watcher.TurnSnapshot, bool, error) {
	if s == nil {
		return watcher.TurnSnapshot{}, false, fmt.Errorf("brain store is not configured")
	}
	fact.SessionID = strings.TrimSpace(fact.SessionID)
	fact.TurnID = strings.TrimSpace(fact.TurnID)
	fact.Kind = strings.TrimSpace(fact.Kind)
	fact.SourceID = strings.TrimSpace(fact.SourceID)
	if fact.SessionID == "" || fact.TurnID == "" {
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
		turn.SettledAt = mutation.settledAt
		turn.Summary = mutation.summary
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

	// Derive the Work update (status only on final-grade facts; hints only
	// adjust next-action text).
	workIndex := workIndex(database.BrainWork, turn.WorkID)
	var workItem Work
	workChanged := false
	if workIndex >= 0 {
		workItem = database.BrainWork[workIndex]
	}
	if mutation.hint != nil && workIndex >= 0 {
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
	if mutation.workUpdate.Status != nil || mutation.workUpdate.NextAction != nil {
		update := mutation.workUpdate
		if workIndex >= 0 && workUpdateChanges(workItem, update) {
			applyWorkUpdate(&workItem, update)
			workItem.UpdatedAt = now
			database.BrainWork[workIndex] = workItem
			workChanged = true
		}
	}

	// Outbox event: exactly one row per (work, dedupe key); corrections flip
	// the existing non-actionable row actionable in place.
	eventCreated := false
	eventID := ""
	workID := turn.WorkID
	if mutation.eventKind != "" {
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
				Actionable: mutation.eventActionable,
				CreatedAt:  now,
			}
			if err := validateWorkEvent(event); err != nil {
				return watcher.TurnSnapshot{}, false, err
			}
			database.BrainWorkEvents = append(database.BrainWorkEvents, event)
			eventID = event.ID
			eventCreated = true
		} else if mutation.eventActionable &&
			!database.BrainWorkEvents[eventIndex].Actionable {
			// In-place correction flip: the same row becomes actionable; the
			// row count never changes, so no second wake is possible.
			database.BrainWorkEvents[eventIndex].Actionable = true
			database.BrainWorkEvents[eventIndex].Summary = mutation.eventSummary
			database.BrainWorkEvents[eventIndex].SourceName = turn.SessionID
			eventID = database.BrainWorkEvents[eventIndex].ID
			eventCreated = true
		}
	}

	database.BrainTurns[turnIndex] = turn
	if err := s.persistOrchestrationLocked(database); err != nil {
		return watcher.TurnSnapshot{}, false, err
	}
	if workChanged || eventCreated {
		s.broadcastWorkChange(workID)
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

// MigrateTurnLedgerV1 performs the one-shot legacy tmux-marker import
// (C.2.8): canonical status is Admitted/Running only; done/failed markers
// attach a Legacy hint that never changes canonical status. All later writes
// go to the ledger. Returns false when already migrated.
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
	if database.Migrations.TurnLedgerV1At != nil {
		return false, nil
	}
	imported := false
	for _, candidate := range imports {
		candidate.SessionID = strings.TrimSpace(candidate.SessionID)
		candidate.TurnID = strings.TrimSpace(candidate.TurnID)
		candidate.WorkID = strings.TrimSpace(candidate.WorkID)
		if candidate.SessionID == "" || candidate.TurnID == "" || candidate.WorkID == "" {
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
		if candidate.Hint != nil {
			record.Hints = []watcher.TurnHint{*candidate.Hint}
		}
		database.BrainTurns = append(database.BrainTurns, record)
		imported = true
	}
	database.Migrations.TurnLedgerV1At = &now
	if err := s.persistOrchestrationLocked(database); err != nil {
		return false, err
	}
	if imported {
		s.broadcastWorkChange("")
	}
	return true, nil
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
				event.ConsumedAt != nil && event.Actionable {
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
// (delivery.ambiguous non-actionable, delivery.uncertain actionable). Returns
// the existing row on duplicate.
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
	if err := validateWorkEvent(event); err != nil {
		return WorkEvent{}, false, err
	}
	database.BrainWorkEvents = append(database.BrainWorkEvents, event)
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, false, err
	}
	s.broadcastWorkChange(workID)
	return event, true, nil
}

// MarkDeliveredClaim closes a held claim by explicit user assertion that the
// host received and acted on the event (C.2.6.1). Actor-recorded, idempotent,
// never time-based.
func (s *Store) MarkDeliveredClaim(eventID, actor, reason string) error {
	return s.resolveClaim(eventID, actor, reason, func(event *WorkEvent, now time.Time) error {
		event.ConsumedAt = &now
		event.Resolution = EventResolutionMarkDelivered
		event.ResolvedBy = actor
		event.ResolvedAt = &now
		return nil
	})
}

// DiscardClaim abandons a held delivery (C.2.6.2). The row leaves the held
// set forever; Brain separately reconciles the owning Work.
func (s *Store) DiscardClaim(eventID, actor, reason string) error {
	return s.resolveClaim(eventID, actor, reason, func(event *WorkEvent, now time.Time) error {
		event.DiscardedAt = &now
		event.Resolution = EventResolutionDiscard
		event.ResolvedBy = actor
		event.ResolvedAt = &now
		return nil
	})
}

// ReplayEvent performs an explicit user-authorized replay as a new event with
// a new identity and key (C.2.6.3). This is the only mechanism that creates a
// second actionable row for one semantic fact, and only with authorization.
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
	if original.ConsumedAt != nil || original.DiscardedAt != nil {
		return WorkEvent{}, fmt.Errorf("event %s is already resolved; no replay", eventID)
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
	if err := validateWorkEvent(replay); err != nil {
		return WorkEvent{}, err
	}
	original.Resolution = EventResolutionReplayed
	original.ResolvedBy = actor
	original.ResolvedAt = &now
	database.BrainWorkEvents[index] = original
	database.BrainWorkEvents = append(database.BrainWorkEvents, replay)
	if err := s.persistOrchestrationLocked(database); err != nil {
		return WorkEvent{}, err
	}
	s.broadcastWorkChange(replay.WorkID)
	return replay, nil
}

func (s *Store) resolveClaim(eventID, actor, reason string, resolve func(*WorkEvent, time.Time) error) error {
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
	if event.ConsumedAt != nil || event.DiscardedAt != nil {
		return fmt.Errorf("event %s is already resolved", eventID)
	}
	if err := resolve(event, now); err != nil {
		return err
	}
	if err := s.persistOrchestrationLocked(database); err != nil {
		return err
	}
	s.broadcastWorkChange(database.BrainWorkEvents[index].WorkID)
	return nil
}

// ErrNoActiveTurn is returned when a session has no canonical turn.
var ErrNoActiveTurn = errors.New("no canonical turn for session")
