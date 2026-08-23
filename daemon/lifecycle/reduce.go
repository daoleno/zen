package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// Reduce applies one event to a state and returns the next state. It is pure:
// no clock, no IO, no randomness. Callers assign Seq/At and mint IDs before
// invocation so replay is byte-deterministic.
//
// Unknown or stale events reduce to the identical state (idempotent reject),
// except that they still bump Revision: the log records that the fact was
// considered, and Revision remains "count of applied events".
func Reduce(prev *State, ev Event) *State {
	s := prev.Clone()
	if s == nil {
		s = &State{}
	}
	if s.SeenSources == nil {
		s.SeenSources = map[string]bool{}
	}
	if ev.SourceID != "" {
		if s.SeenSources[ev.SourceID] {
			return s // I4: duplicate source applies once (first wins)
		}
		s.SeenSources[ev.SourceID] = true
	}
	s.UpdatedAt = ev.At

	switch ev.Kind {
	case KWorkDefined:
		p := payload[DefinedPayload](ev)
		s.ID = ev.WorkID
		s.Status = StatusQueued
		s.Title = p.Title
		s.Objective = p.Objective
		s.Policy = p.Policy
		if s.Policy == "" {
			s.Policy = PolicyBounded
		}
		s.DoneCriteriaRef = p.DoneCriteriaRef
		s.SourceThreadID = p.SourceThreadID
		s.CreatedAt = ev.At

	case KWorkAmended:
		if terminal(s) {
			return noop(s, ev)
		}
		p := payload[AmendedPayload](ev)
		if p.Title != nil {
			s.Title = *p.Title
		}
		if p.Objective != nil {
			s.Objective = *p.Objective
		}
		if p.DoneCriteriaRef != nil {
			s.DoneCriteriaRef = *p.DoneCriteriaRef
		}
		if p.NextAction != nil {
			s.NextAction = *p.NextAction
		}

	case KWorkCancelled:
		if terminal(s) {
			return noop(s, ev)
		}
		p := payload[CancelledPayload](ev)
		_ = p
		releaseAttempt(s, ev.At)
		s.Status = StatusCancelled
		now := ev.At
		s.TerminalAt = &now
		s.Outbox = nil
		s.Review = nil

	case KWorkCompleted:
		if terminal(s) {
			return noop(s, ev)
		}
		p := payload[CancelledPayload](ev)
		_ = p
		releaseAttempt(s, ev.At)
		s.Status = StatusDone
		now := ev.At
		s.TerminalAt = &now
		s.Outbox = nil
		s.Review = nil

	case KAdmissionPrepared:
		if terminal(s) || ev.TurnToken == "" || s.Admission(ev.TurnToken) != nil || s.ActiveAdmission() != nil {
			return noop(s, ev)
		}
		p := payload[AdmissionPreparedPayload](ev)
		if p.SessionID == "" || p.Receipt == "" || p.PayloadSHA256 == "" ||
			p.ProcessIdentity == "" || p.PaneGeneration == "" {
			return noop(s, ev)
		}
		attemptedAt := p.AttemptedAt
		if attemptedAt.IsZero() {
			// Historical admission.prepared events omitted attempted_at. The
			// event timestamp is the durable prepare instant and keeps replay
			// deterministic.
			attemptedAt = ev.At
		}
		s.Admissions = append(s.Admissions, &AdmissionState{
			TurnToken: ev.TurnToken, SessionID: p.SessionID, Receipt: p.Receipt,
			ClaimToken: p.ClaimToken, PayloadSHA256: p.PayloadSHA256,
			ProcessIdentity: p.ProcessIdentity, PaneGeneration: p.PaneGeneration,
			Mode: p.Mode, ExistingTurnToken: p.ExistingTurnToken,
			BaselineActivityID: p.BaselineActivityID, SignalProtocol: p.SignalProtocol,
			AttemptedAt:        attemptedAt,
			TranscriptProvider: p.TranscriptProvider, TranscriptFlag: p.TranscriptFlag, TranscriptPath: p.TranscriptPath,
			Purpose: p.Purpose, PurposeID: p.PurposeID,
			Status: AdmissionPrepared, PreparedAt: ev.At,
		})

	case KAdmissionAmbiguous:
		a := s.Admission(ev.TurnToken)
		if a == nil || a.Status != AdmissionPrepared {
			return noop(s, ev)
		}
		p := payload[AdmissionAmbiguousPayload](ev)
		a.Status = AdmissionAmbiguous
		a.Reason = p.Reason

	case KAdmissionRearmed:
		a := s.Admission(ev.TurnToken)
		if a == nil || a.Status != AdmissionAborted {
			return noop(s, ev)
		}
		p := payload[AdmissionPreparedPayload](ev)
		a.Status = AdmissionPrepared
		a.Reason = ""
		a.PreparedAt = ev.At
		a.AttemptedAt = p.AttemptedAt

	case KAdmissionAccepted:
		a := s.Admission(ev.TurnToken)
		if a == nil || (a.Status != AdmissionPrepared && a.Status != AdmissionAmbiguous &&
			!(a.Status == AdmissionAccepted && a.ActivityID == "")) {
			return noop(s, ev)
		}
		p := payload[AdmissionAcceptedPayload](ev)
		now := ev.At
		a.Status = AdmissionAccepted
		if a.SettledAt == nil {
			a.SettledAt = &now
		}
		a.ActivityID, a.AdmissionStream, a.AdmissionID = p.ActivityID, p.AdmissionStream, p.AdmissionID
		a.AdmissionCursor, a.AdmissionAt, a.ResultTurnToken = p.AdmissionCursor, p.AdmissionAt, p.ResultTurnToken

	case KAdmissionAborted:
		a := s.Admission(ev.TurnToken)
		if a == nil || (a.Status != AdmissionPrepared && a.Status != AdmissionAmbiguous) {
			return noop(s, ev)
		}
		p := payload[AdmissionAbortedPayload](ev)
		now := ev.At
		a.Status, a.SettledAt, a.Reason = AdmissionAborted, &now, p.Reason

	case KTurnAdmitted:
		if terminal(s) {
			return noop(s, ev)
		}
		p := payload[AdmittedPayload](ev)
		token := ev.TurnToken
		if token == "" || p.SessionID == "" {
			return noop(s, ev)
		}
		if s.turnByToken(token) != nil {
			return noop(s, ev) // I5: token admits once
		}
		if s.Attempt != nil {
			// One active Attempt (I2). Admission is rejected unless it
			// is the exact prepared next Attempt Session taking over a released
			// takes an empty slot, which cannot happen until release clears Attempt.
			return noop(s, ev)
		}
		if s.Wake != nil {
			// Admission implies the wait is satisfied by execution resuming.
			s.Wake = nil
		}
		s.Fence++
		s.Attempt = &Attempt{
			SessionID:     p.SessionID,
			Delegated:     p.Delegated,
			Generation:    s.Fence,
			TurnToken:     token,
			LeaseDeadline: ev.At.Add(LeaseGrace),
			LeaseEpoch:    0,
		}
		s.Attempts = append(s.Attempts, &AttemptRecord{
			Token:      token,
			SessionID:  p.SessionID,
			Generation: ev.Fence,
			FollowUpOf: p.FollowUpOf,
			AdmittedAt: ev.At,
		})
		s.Status = StatusRunning

	case KTurnHeartbeat:
		if !eventMatchesAttempt(s, ev) {
			return noop(s, ev)
		}
		p := payload[HeartbeatPayload](ev)
		if p.LeaseSeconds > 0 {
			deadline := ev.At.Add(time.Duration(p.LeaseSeconds) * time.Second)
			if deadline.After(s.Attempt.LeaseDeadline) { // monotonic extension only
				s.Attempt.LeaseDeadline = deadline
			}
		}

	case KTurnProgress:
		if !eventMatchesAttempt(s, ev) {
			return noop(s, ev)
		}
		s.Attempt.LeaseDeadline = ev.At.Add(LeaseGrace)

	case KTurnDone:
		upgrade := false
		if !eventMatchesAttempt(s, ev) {
			t := s.turnByToken(ev.TurnToken)
			latest := len(s.Attempts) > 0 && s.Attempts[len(s.Attempts)-1].Token == ev.TurnToken
			if t == nil || t.Outcome != "lost" || !latest || s.Attempt != nil || terminal(s) {
				return noop(s, ev)
			}
			// Authoritative terminal upgrades a provisional loss (C.2.4): the
			// provisional actionable Event remains stable and is strengthened by
			// the exact evidence below.
			upgrade = true
		}
		p := payload[DonePayload](ev)
		settleTurnUpgrade(s, ev, "done", p.OK, p.Summary, p.CriteriaMet, upgrade)
		releaseAttempt(s, ev.At)
		if upgrade && s.Review != nil {
			// Keep the first unresolved actionable Event as the stable review/card
			// identity. Later exact terminal evidence strengthens what Brain reviews;
			// it must not manufacture a second obligation or notification.
			if p.OK {
				s.Review.Reason = "turn_done"
			} else {
				s.Review.Reason = "turn_failed"
			}
			s.Review.Ref = string(ev.TurnToken)
		}
		applyCompletionRule(s, ev, p)

	case KTurnRelinquished:
		if !eventMatchesAttempt(s, ev) {
			return noop(s, ev)
		}
		p := payload[RelinquishedPayload](ev)
		settleTurn(s, ev, "reviewed", true, p.Reason, false)
		releaseAttempt(s, ev.At)
		s.Status = StatusQueued

	case KTurnLost:
		if !eventMatchesAttempt(s, ev) {
			return noop(s, ev)
		}
		p := payload[LostPayload](ev)
		settleTurn(s, ev, "lost", false, p.Reason, false)
		releaseAttempt(s, ev.At)
		s.ConsecutiveLost++
		if s.ConsecutiveLost >= MaxConsecutiveLost {
			s.Status = StatusBlocked
			openReview(s, ev, "turn_lost", string(ev.TurnToken))
		} else {
			// Automatic re-takeover: queued work is immediately eligible for a
			// fresh admission by the dispatcher/supervisor.
			s.Status = StatusQueued
			openReview(s, ev, "turn_lost", string(ev.TurnToken))
		}

	case KLeaseExpired:
		if !eventMatchesAttempt(s, ev) {
			return noop(s, ev)
		}
		s.Attempt.LeaseEpoch++
		openReview(s, ev, "lease_expired", string(ev.TurnToken))

	case KWakeSet:
		if terminal(s) {
			return noop(s, ev)
		}
		if s.Attempt != nil {
			return noop(s, ev) // waits apply only without an active Attempt
		}
		p := payload[WakePayload](ev)
		s.Wake = &WakeState{Kind: p.WakeKind, Ref: p.Ref, Since: ev.At, NextAttemptAt: p.NextAttemptAt}
		s.Status = StatusWaiting

	case KWakeCleared:
		if s.Wake == nil {
			return noop(s, ev)
		}
		p := payload[WakeClearedPayload](ev)
		if s.Wake.Kind != p.WakeKind || s.Wake.Ref != p.Ref {
			return noop(s, ev)
		}
		s.Wake = nil
		if s.Status == StatusWaiting {
			s.Status = StatusQueued
		}

	case KReviewOpened:
		p := payload[ReviewOpenedPayload](ev)
		if p.EventID == "" {
			return noop(s, ev)
		}
		outboxID := reviewOutboxID(s.ID, p.EventID)
		if outboxFind(s, outboxID) == nil {
			s.Outbox = append(s.Outbox, &OutboxEntry{
				ID: outboxID, Reason: "review", TargetThreadID: s.SourceThreadID, CreatedAt: ev.At,
			})
		}
		if terminal(s) || s.Review != nil {
			return noop(s, ev) // terminal notification only; otherwise first actionable Event wins
		}
		s.Review = &ReviewState{EventID: p.EventID, OutboxID: outboxID, Reason: p.Reason, Ref: p.Ref, OpenedAt: ev.At}

	case KReviewClaimed:
		if s.Review == nil {
			return noop(s, ev)
		}
		p := payload[ReviewClaimedPayload](ev)
		if p.EventID != s.Review.EventID || s.Review.Handler != nil {
			return noop(s, ev)
		}
		s.Review.Handler = &ReviewHandler{
			HandlerID:      p.HandlerID,
			HandlerToken:   p.HandlerToken,
			ClaimedAt:      ev.At,
			ClaimExpiresAt: p.ExpiresAt,
		}

	case KReviewDelivered:
		if s.Review == nil || s.Review.Handler == nil {
			return noop(s, ev)
		}
		p := payload[ReviewDeliveredPayload](ev)
		if p.EventID != s.Review.EventID || p.HandlerToken != s.Review.Handler.HandlerToken {
			return noop(s, ev)
		}
		now := ev.At
		s.Review.Handler.DeliveredAt = &now
		if entry := outboxFind(s, s.Review.OutboxID); entry != nil {
			entry.DispatchedAt = &now
			entry.DispatchResult = DispatchSuccess
			entry.DispatchError = ""
			entry.LastError = ""
			entry.NextAttemptAt = nil
		}

	case KReviewReleased:
		if s.Review == nil || s.Review.Handler == nil {
			return noop(s, ev)
		}
		p := payload[ReviewReleasedPayload](ev)
		if p.EventID != s.Review.EventID || p.HandlerToken != s.Review.Handler.HandlerToken {
			return noop(s, ev)
		}
		s.Review.Handler = nil

	case KReviewDeliveryResolved:
		if s.Review == nil || s.Review.Handler == nil {
			return noop(s, ev)
		}
		p := payload[ReviewDeliveryResolvedPayload](ev)
		if p.EventID != s.Review.EventID || p.HandlerToken != s.Review.Handler.HandlerToken ||
			s.Review.Handler.DeliveredAt != nil {
			return noop(s, ev)
		}
		if p.Action == ReviewDeliveryDiscard {
			s.Review = nil
		} else {
			s.Review.Handler = nil
		}

	case KReviewResolved:
		if terminal(s) || s.Review == nil {
			return noop(s, ev)
		}
		p := payload[ReviewResolvedPayload](ev)
		if p.EventID != s.Review.EventID {
			return noop(s, ev)
		}
		resolvedOutboxID := s.Review.OutboxID
		s.Review = nil
		removeOutbox(s, resolvedOutboxID)
		switch p.Disposition {
		case DispositionComplete:
			releaseAttempt(s, ev.At)
			s.Status = StatusDone
			now := ev.At
			s.TerminalAt = &now
			s.Outbox = nil
			s.Wake = nil
		case DispositionCancel:
			releaseAttempt(s, ev.At)
			s.Status = StatusCancelled
			now := ev.At
			s.TerminalAt = &now
			s.Outbox = nil
		case DispositionWait:
			if s.Attempt == nil {
				s.Wake = &WakeState{
					Kind: wakeOr(p.WakeKind, WakeUserInput), Ref: p.WakeRef,
					Since: ev.At, NextAttemptAt: p.NextAttemptAt,
				}
				s.Status = StatusWaiting
			}
		case DispositionContinue:
			if s.Attempt == nil && s.Status != StatusWaiting {
				s.Status = StatusQueued
			}
		}

	case KOutboxDispatch:
		p := payload[OutboxDispatchPayload](ev)
		entry := outboxFind(s, p.EntryID)
		if entry == nil {
			return noop(s, ev)
		}
		now := ev.At
		entry.DispatchedAt = &now
		entry.DispatchResult = p.Result
		entry.DispatchError = p.Error
		entry.LastError = p.Error
		entry.NextAttemptAt = nil
		if p.Result == DispatchRetryable {
			entry.Attempts++
			next := ev.At.Add(dispatchBackoff(entry.Attempts))
			entry.NextAttemptAt = &next
		}

	default:
		// Unknown kinds are audit-only.
	}

	s.Revision++
	return s
}

// ---- reduction helpers ----

func payload[T any](ev Event) T {
	p, _ := ev.Payload.(T)
	return p
}

func noop(s *State, ev Event) *State {
	s.Revision++
	return s
}

func terminal(s *State) bool {
	return s.Status.Terminal()
}

func eventMatchesAttempt(s *State, ev Event) bool {
	if s.Attempt == nil || ev.TurnToken == "" {
		return false
	}
	return s.Attempt.TurnToken == ev.TurnToken && s.Attempt.Generation == ev.Fence
}

func currentAttempt(s *State, identity AttemptIdentity) bool {
	if s.Attempt == nil || identity.SessionID == "" || identity.TurnToken == "" || identity.Fence == 0 {
		return false
	}
	return s.Attempt.SessionID == identity.SessionID &&
		s.Attempt.TurnToken == identity.TurnToken && s.Attempt.Generation == identity.Fence
}

func settleTurn(s *State, ev Event, outcome string, ok bool, summary string, criteriaMet bool) {
	settleTurnUpgrade(s, ev, outcome, ok, summary, criteriaMet, false)
}

// settleTurnUpgrade applies first-terminal-wins (I5) unless upgrade names an
// authoritative rewrite of a provisional loss.
func settleTurnUpgrade(s *State, ev Event, outcome string, ok bool, summary string, criteriaMet bool, upgrade bool) {
	t := s.turnByToken(ev.TurnToken)
	if t == nil {
		return
	}
	if t.SettledAt != nil {
		if !upgrade || t.Outcome != "lost" {
			return // I5: first terminal wins
		}
	}
	now := ev.At
	if t.SettledAt == nil {
		t.SettledAt = &now
	}
	t.Outcome = outcome
	t.OK = ok
	t.Summary = summary
	t.CriteriaMet = criteriaMet
}

func releaseAttempt(s *State, at time.Time) {
	if s.Attempt == nil {
		return
	}
	s.Fence++ // I3: fence strictly increases on every Attempt transition
	s.Attempt = nil
}

func openReview(s *State, ev Event, reason, ref string) {
	if s.Review != nil {
		return
	}
	eventID := stableID("actionable-event", string(s.ID), ev.SourceID, reason, ref)
	outboxID := reviewOutboxID(s.ID, eventID)
	s.Review = &ReviewState{
		EventID:  eventID,
		OutboxID: outboxID,
		Reason:   reason,
		Ref:      ref,
		OpenedAt: ev.At,
	}
	if outboxFind(s, outboxID) == nil {
		s.Outbox = append(s.Outbox, &OutboxEntry{
			ID: outboxID, Reason: "review", TargetThreadID: s.SourceThreadID, CreatedAt: ev.At,
		})
	}
}

func reviewOutboxID(workID WorkID, ref string) string {
	return stableID("review-outbox", string(workID), ref)
}

func applyCompletionRule(s *State, ev Event, p DonePayload) {
	s.ConsecutiveLost = 0 // a settled done proves the pipeline worked
	switch {
	case !p.OK:
		s.Status = StatusBlocked
		openReview(s, ev, "turn_failed", string(ev.TurnToken))
	case p.Final || (p.OK && p.CriteriaMet):
		// Strong completion authority: the reporter's evidence class affirms
		// the outcome outright (bounded signal worker terminal, criteria met).
		s.Status = StatusDone
		now := ev.At
		s.TerminalAt = &now
		s.Outbox = nil
		s.Review = nil
	default:
		// An unaffirmed result awaits Brain judgment. until_done changes only
		// whether this result can implicitly complete the Work; it never queues
		// execution, invents a task, or creates a Session.
		s.Status = StatusQueued
		openReview(s, ev, "turn_done", string(ev.TurnToken))
	}
}

func outboxFind(s *State, id string) *OutboxEntry {
	for _, e := range s.Outbox {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func removeOutbox(s *State, id string) {
	for i, entry := range s.Outbox {
		if entry.ID == id {
			s.Outbox = append(s.Outbox[:i], s.Outbox[i+1:]...)
			return
		}
	}
}

func wakeOr(k WakeKind, fallback WakeKind) WakeKind {
	if k == "" {
		return fallback
	}
	return k
}

func stableID(parts ...string) string {
	h := sha256.Sum256([]byte(fmt.Sprint(parts)))
	return hex.EncodeToString(h[:8])
}

// TurnHistory returns settled turns oldest-first (read model).
func (s *State) TurnHistory() []*AttemptRecord {
	out := append([]*AttemptRecord(nil), s.Attempts...)
	sort.Slice(out, func(i, j int) bool { return out[i].AdmittedAt.Before(out[j].AdmittedAt) })
	return out
}
