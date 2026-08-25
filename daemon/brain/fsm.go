package brain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

// This file is the adapter between the canonical lifecycle engine (the only
// writer of delegated-Work lifecycle state) and Brain's derived read models.
// See docs/work-lifecycle.md: Work is the aggregate root; every
// lifecycle transition is an engine command; presentation.json rows are a
// derived projection refreshed after each commit.

func lifecycleStatusFromWork(status WorkStatus) lifecycle.Status {
	switch status {
	case WorkRunning:
		return lifecycle.StatusRunning
	case WorkWaiting:
		return lifecycle.StatusWaiting
	case WorkNeedsInput:
		return lifecycle.StatusBlocked
	case WorkDone:
		return lifecycle.StatusDone
	case WorkCancelled:
		return lifecycle.StatusCancelled
	default:
		return lifecycle.StatusQueued
	}
}

func workStatusFromLifecycle(status lifecycle.Status) WorkStatus {
	switch status {
	case lifecycle.StatusRunning:
		return WorkRunning
	case lifecycle.StatusWaiting:
		return WorkWaiting
	case lifecycle.StatusBlocked:
		return WorkNeedsInput
	case lifecycle.StatusDone:
		return WorkDone
	case lifecycle.StatusCancelled:
		return WorkCancelled
	default:
		return WorkOpen
	}
}

func lifecyclePolicyFromWork(policy CompletionPolicy) lifecycle.Policy {
	if policy == CompletionUntilDone {
		return lifecycle.PolicyUntilDone
	}
	return lifecycle.PolicyBounded
}

// fsmDefine seeds the canonical aggregate for a newly created Work row.
func (s *Store) fsmDefine(item Work) error {
	if s.fsm == nil {
		return fmt.Errorf("brain store is not configured")
	}
	_, err := s.fsm.DefineWork(lifecycle.WorkID(item.ID), lifecycle.DefineWorkInput{
		Title:           item.Title,
		Objective:       item.Objective,
		Policy:          lifecyclePolicyFromWork(item.CompletionPolicy),
		DoneCriteriaRef: item.DoneCriteriaRef,
		SourceThreadID:  item.SourceThreadID,
	})
	return err
}

// fsmState returns the canonical reduced state for a Work.
func (s *Store) fsmState(workID string) (*lifecycle.State, error) {
	if s.fsm == nil {
		return nil, fmt.Errorf("brain store is not configured")
	}
	return s.fsm.State(lifecycle.WorkID(strings.TrimSpace(workID)))
}

// fsmWorkByAttemptSession locates the Work whose live owner turn runs on the
// given delegated session.
func (s *Store) fsmWorkByAttemptSession(sessionID string) (*lifecycle.State, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s.fsm == nil {
		return nil, false
	}
	for _, st := range s.fsm.ListViews() {
		if st.Attempt != nil && st.Attempt.SessionID == sessionID {
			return st, true
		}
	}
	return nil, false
}

// ReleaseSessionAttempt records an explicitly closed delegated Session as lost
// in the canonical Work aggregate. Session teardown is transport cleanup; this
// command is the lifecycle boundary that releases the owner fence.
func (s *Store) ReleaseSessionAttempt(sessionID, reason string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, fmt.Errorf("session id is required")
	}
	st, found := s.fsmWorkByAttemptSession(sessionID)
	if !found || st.Attempt == nil {
		return false, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "session_closed"
	}
	identity := lifecycle.AttemptIdentity{SessionID: st.Attempt.SessionID, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation}
	if _, err := s.fsm.ReportTurnLost(st.ID, identity, reason); err != nil {
		if current, ok := s.fsmWorkByAttemptSession(sessionID); !ok || current.Attempt == nil {
			return false, nil
		}
		return false, err
	}
	if err := s.SyncWorkProjection(string(st.ID)); err != nil {
		return false, err
	}
	return true, nil
}

// fsmSyncWorkLocked refreshes the derived BrainWork row from canonical engine
// state. Caller holds s.mu. Lifecycle fields are never written anywhere else:
// every mutation path reaches this projector through an engine command first.
func (s *Store) fsmSyncWorkLocked(database *presentationDatabase, workID string, now time.Time) error {
	st, err := s.fsmState(workID)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		return ErrWorkNotFound
	}
	item := database.BrainWork[index]

	// Until the engine has admitted at least one turn for this aggregate, it
	// holds no execution opinion: declared row fields (owner, status) remain
	// exactly as created. From the first canonical admission onward, every
	// lifecycle field below is a pure projection of engine state. Review and
	// next Attempt projections always run: Events are canonical regardless of
	// execution history.
	engineOwnsExecution := st.Fence > 0 || st.TerminalAt != nil || st.Wake != nil || st.Review != nil
	if engineOwnsExecution {
		item.Revision = st.Revision
		item.Status, item.NextAction, item.WaitFor = fsmProjectLifecycle(st, database, item)
		item.AttemptSessionID, item.AttemptDelegated = fsmProjectAttempt(st)
		if st.TerminalAt != nil {
			item.TerminalRevision = st.Revision
		} else if item.Status != WorkDone && item.Status != WorkCancelled {
			// A nonterminal aggregate never retains a terminal marker: the row
			// is a projection, and stale markers are validation failures.
			item.TerminalRevision = 0
		}
		item.Wake = fsmWakeFromState(st)
		item.UpdatedAt = now
	}

	// Review projection: the engine owns event lifecycle and the handler
	// lease; the row keeps its content pointer (EventID) so action
	// rendering finds the fact. An existing content pointer survives: the
	// newest eligible fact refreshes it without changing the event identity.
	if st.Review != nil {
		if strings.TrimSpace(st.Review.EventID) == "" {
			return fmt.Errorf("canonical review %s has no actionable event identity", st.Review.EventID)
		}
		canonicalIndex := workEventIndex(database.BrainWorkEvents, st.Review.EventID)
		if canonicalIndex < 0 {
			projected := canonicalReviewEvent(*database, workID, st)
			database.NextEventSequence++
			projected.Sequence = database.NextEventSequence
			database.BrainWorkEvents = append(database.BrainWorkEvents, projected)
			canonicalIndex = len(database.BrainWorkEvents) - 1
		}
		// Lifecycle owns actionability. Development-era provider rows remain
		// audit evidence, but only the canonical review identity can be claimed.
		for eventIndex := range database.BrainWorkEvents {
			if database.BrainWorkEvents[eventIndex].WorkID == workID {
				database.BrainWorkEvents[eventIndex].Actionable = eventIndex == canonicalIndex
			}
		}
		if item.Review == nil || item.Review.EventID != st.Review.EventID {
			// A new canonical event is a new action identity. A torn projection
			// may still contain the prior event after the engine has closed it and
			// opened another; carrying that fact pointer forward would bind the
			// new handler/nextAttempt to stale review content. Within one event the
			// pointer may still advance to a newer eligible fact elsewhere.
			item.Review = &WorkReview{
				RequiredAt: st.Review.OpenedAt,
				EventID:    st.Review.EventID,
			}
		}
		if st.Review.Handler == nil && item.Review.Lease != nil {
			// The canonical handler was released (end/recovery/actor closure);
			// the disposable delivery lease projection goes with it.
			item.Review.Lease = nil
		}
		if st.Review.Handler != nil {
			if item.Review.Lease == nil || item.Review.Lease.HandlingID != st.Review.Handler.HandlerID {
				hostSessionID := strings.TrimSpace(st.Review.Handler.HostSessionID)
				if hostSessionID == "" {
					// Pre-split claims used HandlerID for both identities. Replay them
					// once so an upgrade can start, but every new claim writes both.
					hostSessionID = st.Review.Handler.HandlerID
				}
				item.Review.Lease = &WorkReviewLease{
					HostSessionID:  hostSessionID,
					HandlingID:     st.Review.Handler.HandlerID,
					ProviderTurnID: string(st.Review.Handler.HandlerToken),
					// The capability freezes the projected revision it was
					// minted against.
					DeliveryWorkRevision:  item.Revision,
					DeliverySequenceFence: database.NextEventSequence,
					ClaimedAt:             st.Review.Handler.ClaimedAt,
					DeliveredAt:           st.Review.Handler.DeliveredAt,
				}
			} else {
				// Same handling: delivery state advances with the engine.
				item.Review.Lease.DeliveredAt = st.Review.Handler.DeliveredAt
			}
		}
	} else {
		item.Review = nil
		// Canonical Review absence means no presentation row for this Work is
		// actionable. This also repairs development-era provider evidence that
		// was independently marked actionable before EventID became the sole
		// claim/card/disposition identity.
		for eventIndex := range database.BrainWorkEvents {
			if database.BrainWorkEvents[eventIndex].WorkID == workID {
				database.BrainWorkEvents[eventIndex].Actionable = false
			}
		}
	}

	database.BrainWork[index] = item
	return nil
}

func canonicalReviewEvent(database presentationDatabase, workID string, st *lifecycle.State) WorkEvent {
	projected := WorkEvent{
		ID: st.Review.EventID, WorkID: workID, Kind: st.Review.Reason,
		DedupeKey: "lifecycle:" + st.Review.EventID, PayloadRef: st.Review.Ref,
		Summary: st.Review.Reason, Actionable: true, CreatedAt: st.Review.OpenedAt,
		WorkRevision: st.Revision,
	}
	for _, evidence := range database.BrainWorkEvents {
		if evidence.WorkID != workID || !isSessionLifecycleKind(evidence.Kind) ||
			!strings.Contains(evidence.DedupeKey, ":turn:"+st.Review.Ref+":") {
			continue
		}
		projected.PayloadRef = evidence.PayloadRef
		projected.SourceName = evidence.SourceName
		projected.Summary = firstNonEmpty(evidence.Summary, projected.Summary)
		if !evidence.CreatedAt.IsZero() {
			projected.CreatedAt = evidence.CreatedAt
		}
		break
	}
	return projected
}

// fsmProjectAttempt derives the presentation fields from the active canonical
// Attempt only. Historical turns may remain visible as result sources, but
// they must never repopulate AttemptSessionID after the fence releases.
func fsmProjectAttempt(st *lifecycle.State) (string, bool) {
	if st.Attempt != nil {
		return st.Attempt.SessionID, st.Attempt.Delegated
	}
	return "", false
}

// fsmProjectLifecycle derives presentation status, next-action prose, and wait
// prose from canonical state plus the evidence ledger. The engine remains the
// only writer of lifecycle truth; this mapping is a read model.
func fsmProjectLifecycle(st *lifecycle.State, database *presentationDatabase, item Work) (WorkStatus, string, string) {
	sessionProse := func(sessionID string) string {
		if sessionID == "" {
			return ""
		}
		return "Session " + sessionID
	}
	switch {
	case st.TerminalAt != nil:
		return workStatusFromLifecycle(st.Status), "", ""
	case st.Wake != nil && st.Attempt == nil:
		return WorkWaiting, firstNonEmpty(item.NextAction, "Wait for the named external condition."), st.Wake.Ref
	}
	if st.Review != nil {
		// An open canonical event is the actionable obligation. Lease expiry
		// and evidence loss keep the owner live (the turn may still be
		// working); their attention still wins over plain execution prose.
		waitFor := ""
		if st.Attempt != nil {
			waitFor = sessionProse(st.Attempt.SessionID)
		}
		switch st.Review.Reason {
		case "turn_failed":
			if st.Attempt == nil {
				return WorkWaiting, "Inspect the delegated Session failure.", waitFor
			}
		case "lease_expired":
			return WorkNeedsInput, "Inspect the delegated Session lease expiry.", waitFor
		case "turn_lost":
			return WorkNeedsInput, "Confirm whether the delegated Session received the prompt; delivery will not be replayed.", waitFor
		case "submission_ambiguous", "submission_failed":
			return WorkNeedsInput, "Confirm whether the delegated Session received the prompt; delivery will not be replayed.", waitFor
		default:
			if st.Attempt == nil {
				return WorkWaiting, "Review the delegated Session result.", ""
			}
		}
	}
	switch {
	case st.Attempt != nil:
		waitFor := sessionProse(st.Attempt.SessionID)
		if fsmAttentionPending(database, st.Attempt.SessionID) {
			return WorkNeedsInput, "Resolve the delegated Session request.", waitFor
		}
		next := firstNonEmpty(item.NextAction, "Wait for the delegated Session.")
		if hint := fsmLiveTurnHint(database, st.Attempt.SessionID); hint != "" {
			next = hint
		}
		return WorkRunning, next, waitFor
	default:
		return workStatusFromLifecycle(st.Status), item.NextAction, ""
	}
}

// fsmAttentionPending reports whether the session's current canonical turn is
// blocked on user input (orthogonal attention, never a second owner).
func fsmAttentionPending(database *presentationDatabase, sessionID string) bool {
	if database == nil || sessionID == "" {
		return false
	}
	turn, found := currentTurnForSession(*database, sessionID)
	return found && turn.Status == watcher.TurnBlocked && turn.Attention == "user_input"
}

// fsmLiveTurnHint projects a provisional provider hint (an unbound terminal
// awaiting confirmation) into next-action prose. Hints are presentation only.
func fsmLiveTurnHint(database *presentationDatabase, sessionID string) string {
	if database == nil || sessionID == "" {
		return ""
	}
	turn, found := currentTurnForSession(*database, sessionID)
	if !found || len(turn.Hints) == 0 || turn.SignalProtocol && watcher.TurnTerminal(turn.Status) {
		return ""
	}
	for _, hint := range turn.Hints {
		switch hint.Kind {
		case "session.done", "session.failed":
			return "Delegated Session reported " + strings.TrimPrefix(hint.Kind, "session.") +
				"; awaiting provider confirmation"
		}
	}
	return ""
}

// fsmAdmitTurn admits one delegated turn as the active Attempt. It is
// idempotent by turn token and rejects while another Attempt is active.
func (s *Store) fsmAdmitTurn(workID, sessionID, turnToken string, delegated bool) error {
	applied, _, err := s.fsm.AdmitTurn(lifecycle.WorkID(workID), lifecycle.AdmitTurnInput{
		SessionID: sessionID,
		Delegated: delegated,
		TurnToken: lifecycle.TurnToken(turnToken),
	})
	if err != nil {
		return err
	}
	_ = applied
	return nil
}

// fsmTranslateCanonicalTransitionLocked routes one canonical ledger
// transition into engine commands (docs/work-lifecycle.md: the engine
// is the only writer of Work lifecycle state). Only transitions that moved
// canonical turn state are translated; hint-only observations stay evidence.
// Every command is an idempotent reject against stale fences/tokens, so
// replaying a durable fact after a crash converges instead of duplicating.
//
// The caller passes finalDone for the one strong-completion path: a bounded
// signal-protocol worker whose exact bound provider terminal arrived.
func (s *Store) fsmTranslateCanonicalTransition(turn *TurnRecord, fact watcher.TurnFact, status watcher.TurnStatus, eventKind string, finalDone bool) error {
	if turn == nil || strings.TrimSpace(turn.WorkID) == "" {
		return nil
	}
	workID := turn.WorkID
	sessionID := turn.SessionID
	token := lifecycle.TurnToken(turn.TurnID)
	attemptState := func() *lifecycle.State {
		st, found := s.fsmWorkByAttemptSession(sessionID)
		if !found || st.Attempt == nil || st.Attempt.TurnToken != token {
			return nil
		}
		workID = string(st.ID)
		return st
	}
	switch {
	case status == watcher.TurnAccepted:
		// Admission promotion (receipt/provider tuple/signal). Idempotent by
		// token: fixtures and submission resolution already admitted it.
		return fsmObservationResult(s.fsmAdmitTurn(workID, sessionID, turn.TurnID, true))
	case status == watcher.TurnRunning && fact.Class == watcher.EvidenceControl:
		if st := attemptState(); st != nil {
			identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: token, Fence: st.Attempt.Generation}
			_, err := s.fsm.Heartbeat(st.ID, identity, fact.LeaseSeconds)
			return fsmObservationResult(err)
		}
	case status == watcher.TurnRunning && fact.Class == watcher.EvidenceProvider:
		if st := attemptState(); st != nil {
			identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: token, Fence: st.Attempt.Generation}
			_, err := s.fsm.Progress(st.ID, identity, fact.Summary)
			return fsmObservationResult(err)
		}
	case status == watcher.TurnBlocked:
		if st := attemptState(); st != nil {
			identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: token, Fence: st.Attempt.Generation}
			if _, err := s.fsm.Heartbeat(st.ID, identity, fact.LeaseSeconds); err != nil {
				return fsmObservationResult(err)
			}
			reason := firstNonEmpty(eventKind, "session.needs_input")
			eventID := strings.TrimSpace(fact.SourceID)
			if eventID == "" {
				return fmt.Errorf("blocked Turn evidence requires an exact Event identity")
			}
			_, err := s.fsm.OpenReviewEvent(st.ID, reason, eventID, eventID)
			return fsmObservationResult(err)
		}
	case status == watcher.TurnDone || status == watcher.TurnFailed:
		ok := status == watcher.TurnDone
		fence := uint64(0)
		if st := attemptState(); st != nil {
			fence = st.Attempt.Generation
		}
		summary := firstNonEmpty(fact.Summary, mutationEventSummary(eventKind))
		identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: token, Fence: fence}
		_, err := s.fsm.ReportTurnDone(lifecycle.WorkID(workID), identity, lifecycle.DoneInput{
			OK: ok, Summary: summary, CriteriaMet: ok && fact.Class == watcher.EvidenceControl && fact.CriteriaMet,
			Final: finalDone,
		})
		if err != nil {
			return fsmObservationResult(err)
		}
		return s.fsmFanoutSessionTerminal(sessionID, turn.TurnID)
	case status == watcher.TurnUnknown:
		reason := "evidence_loss"
		if fact.Class == watcher.EvidenceLiveness && fact.Kind == "ownership_lost" {
			reason = "ownership_lost"
		}
		aggregate, stateErr := s.fsm.State(lifecycle.WorkID(workID))
		if stateErr != nil {
			return fsmObservationResult(stateErr)
		}
		identity := lifecycle.AttemptIdentity{SessionID: sessionID, TurnToken: token}
		if aggregate.Attempt != nil && aggregate.Attempt.SessionID == sessionID && aggregate.Attempt.TurnToken == token {
			identity.Fence = aggregate.Attempt.Generation
		}
		if _, err := s.fsm.ReportTurnLost(lifecycle.WorkID(workID), identity, reason); err != nil {
			return fsmObservationResult(err)
		}
		return s.fsmFanoutSessionTerminal(sessionID, turn.TurnID)
	default:
		if eventKind == "session.stale" {
			// The ledger's per-turn deadline expired; record the idempotent
			// expiry in the engine (shares the sweep's source identity).
			_, err := s.fsm.ReportLeaseExpired(lifecycle.WorkID(workID), token)
			return fsmObservationResult(err)
		}
	}
	return nil
}

// fsmObservationResult classifies canonical idempotent rejection explicitly.
// Late evidence for a settled generation remains audit evidence; all storage,
// validation, and current-authority failures propagate to the caller.
func fsmObservationResult(err error) error {
	if errors.Is(err, lifecycle.ErrStaleInput) || errors.Is(err, lifecycle.ErrTerminal) {
		return nil
	}
	return err
}

func mutationEventSummary(eventKind string) string {
	switch eventKind {
	case "session.failed":
		return "Delegated Session reported failure"
	default:
		return ""
	}
}

// fsmFanoutSessionTerminal releases Works parked on this producer terminal.
func (s *Store) fsmFanoutSessionTerminal(sessionID, turnID string) error {
	if s.fsm == nil {
		return nil
	}
	ref := SessionTerminalWakeRef(sessionID, turnID)
	for _, st := range s.fsm.ListViews() {
		if st.Wake != nil && st.Wake.Kind == lifecycle.WakeSessionTerminal && st.Wake.Ref == ref {
			if _, err := s.fsm.ClearWait(st.ID, lifecycle.WakeSessionTerminal, ref, "turn:"+turnID); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncWorkCardLocked replaces the one timeline slot owned by a Work. The
// visible card ID is the canonical Review.EventID; WorkID selects the lineage
// slot so successive canonical reviews replace rather than duplicate it.
// Caller holds s.mu.
func (s *Store) syncWorkCardLocked(workID string, trigger *WorkEvent) (TimelineItem, bool, error) {
	if trigger == nil || strings.TrimSpace(trigger.ID) == "" || !isProjectedWorkResultEvent(trigger.Kind) {
		return TimelineItem{}, false, nil
	}
	database, err := s.loadPresentationLocked()
	if err != nil {
		return TimelineItem{}, false, err
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		return TimelineItem{}, false, ErrWorkNotFound
	}
	item := database.BrainWork[index]
	projected := workCardTimelineItem(item, *trigger, true)
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return TimelineItem{}, false, err
	}
	out := make([]TimelineItem, 0, len(items)+1)
	found := false
	for _, current := range items {
		if current.Kind != timelineKindWorkCard || current.WorkID != item.ID {
			out = append(out, current)
			continue
		}
		if found {
			continue
		}
		found = true
		if !current.CreatedAt.IsZero() {
			projected.CreatedAt = current.CreatedAt
		}
		projected.Unread = current.Unread || projected.Unread
		out = append(out, projected)
	}
	if !found {
		out = append(out, projected)
	}
	if err := s.rewriteTimelineLocked(out); err != nil {
		return TimelineItem{}, false, err
	}
	return projected, !found, nil
}

// fsmResolveReview applies one typed disposition to the open canonical event.
func (s *Store) fsmResolveReview(workID string, disposition lifecycle.Disposition, wake *WorkWake) error {
	st, err := s.fsmState(workID)
	if err != nil {
		return err
	}
	if st.Review == nil {
		return ErrNoOpenReviewFSM
	}
	in := lifecycle.ResolveReviewInput{Disposition: disposition}
	if wake != nil {
		in.WakeKind = lifecycle.WakeKind(wake.Kind)
		in.WakeRef = wake.Ref
		in.NextAttemptAt = wake.NextAttemptAt
	}
	_, err = s.fsm.ResolveReview(lifecycle.WorkID(workID), st.Review.EventID, in)
	return err
}

// fsmEventID returns the canonical event ID behind a projected review row.
func fsmEventID(item Work) string {
	if item.Review == nil {
		return ""
	}
	return strings.TrimSpace(item.Review.EventID)
}

// fsmWakeFromState projects the typed wait into Brain's read shape.
func fsmWakeFromState(st *lifecycle.State) *WorkWake {
	if st.Wake == nil {
		return nil
	}
	return &WorkWake{Kind: WorkWakeKind(st.Wake.Kind), Ref: st.Wake.Ref, NextAttemptAt: st.Wake.NextAttemptAt}
}

// ErrNoOpenReviewFSM reports a missing canonical event.
var ErrNoOpenReviewFSM = fmt.Errorf("no open canonical review event")

// SyncWorkProjection refreshes one Work's derived row and single lineage card
// from canonical engine state. Safe for concurrent callers.
func (s *Store) SyncWorkProjection(workID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return err
	}
	if err := s.fsmSyncWorkLocked(&database, workID, s.nowUTC()); err != nil {
		return err
	}
	var cardEvent *WorkEvent
	if index := workIndex(database.BrainWork, workID); index >= 0 && database.BrainWork[index].Review != nil {
		if eventIndex := workEventIndex(database.BrainWorkEvents, database.BrainWork[index].Review.EventID); eventIndex >= 0 {
			event := cardEventForCanonicalReview(database, database.BrainWorkEvents[eventIndex])
			cardEvent = &event
		}
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return err
	}
	_, _, err = s.syncWorkCardLocked(workID, cardEvent)
	return err
}

// SweepLifecycle applies every due durable timer and immediately refreshes
// the derived Work rows before Host admission reads them. The engine remains
// the sole lifecycle writer; projection repair is idempotent and cannot
// advance a Work revision.
func (s *Store) SweepLifecycle() error {
	if s == nil || s.fsm == nil {
		return fmt.Errorf("brain store is not configured")
	}
	if err := s.fsm.Sweep(); err != nil {
		return err
	}
	s.mu.Lock()
	database, err := s.loadPresentationLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	revisions := make(map[string]uint64, len(database.BrainWork))
	for _, item := range database.BrainWork {
		revisions[item.ID] = item.Revision
	}
	var firstErr error
	for _, st := range s.fsm.ListViews() {
		revision, found := revisions[string(st.ID)]
		if !found {
			if firstErr == nil {
				firstErr = fmt.Errorf("read Work %s after lifecycle sweep: %w", st.ID, ErrWorkNotFound)
			}
			continue
		}
		if revision == st.Revision {
			continue
		}
		if err := s.SyncWorkProjection(string(st.ID)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("project Work %s after lifecycle sweep: %w", st.ID, err)
		}
	}
	return firstErr
}

func cardEventForCanonicalReview(database presentationDatabase, canonical WorkEvent) WorkEvent {
	for _, evidence := range database.BrainWorkEvents {
		if evidence.WorkID != canonical.WorkID || !isProjectedWorkResultEvent(evidence.Kind) ||
			!strings.Contains(evidence.DedupeKey, ":turn:"+canonical.PayloadRef+":") {
			continue
		}
		canonical.Kind = evidence.Kind
		canonical.PayloadRef = evidence.PayloadRef
		canonical.SourceName = evidence.SourceName
		canonical.Summary = firstNonEmpty(evidence.Summary, canonical.Summary)
		canonical.Phase = evidence.Phase
		canonical.Attention = evidence.Attention
		canonical.EventKind = evidence.EventKind
		canonical.DetailsJSON = evidence.DetailsJSON
		if !evidence.CreatedAt.IsZero() {
			canonical.CreatedAt = evidence.CreatedAt
		}
		break
	}
	return canonical
}

// SyncWorkCard projects the single lineage card for one Work. Safe wrapper.
func (s *Store) SyncWorkCard(workID string, trigger *WorkEvent) (TimelineItem, bool, error) {
	s.mu.Lock()
	item, created, err := s.syncWorkCardLocked(workID, trigger)
	s.mu.Unlock()
	return item, created, err
}
