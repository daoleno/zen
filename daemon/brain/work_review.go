package brain

import (
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

// ---------------------------------------------------------------------------
// Work-centric Brain review model.
//
// Work is the authoritative durable unit. A delegated Session is an executor
// attached to Work. A WorkEvent is an append-only fact and at most a one-shot
// wake/delivery signal. A UI card is a projection of Work state and its
// current required action; it is never owned by a transient Event claim.
//
// Invariants
//
//	I1  Work.Review is the only scheduler truth. Event rows never carry
//	    claim/delivery/lease state; they are append-only facts plus audit
//	    (HandledAt/Disposition for Brain dispositions, Resolution/DiscardedAt
//	    for actor lease closures).
//	I2  At most one Work.Review exists per Work and at most one delivered
//	    review lease exists globally (the Host lane stop gate).
//	I3  Review.RequiredAt is the immutable event birth used for queue
//	    ordering. Review.EventID names the current action fact; a newer
//	    eligible fact replaces it while no lease is in flight (content
//	    refresh, never a second queue item).
//	I4  A disposition CASes the exact lease capability (WorkID, HandlingID,
//	    ProviderTurnID) and the expected Work revision. A stale capability
//	    can never mutate newer state.
//	I5  Work.Review is cleared only by: a typed Brain disposition, an actor
//	    lease resolution (mark_delivered/discard/replay), owner admission
//	    settling an undelivered event, or operator CloseWork. Clearing is
//	    atomic with the Work transition and the fact audit.
//	I6  queued_attention counts review-required Work. Every counted item is
//	    recoverable: re-claimable after lease expiry, or quarantined with an
//	    explicit actor-resolution path (Lease.AmbiguousDelivery). A claim
//	    conflict can therefore never leave queued_attention > 0 with no
//	    recoverable action.
//	I7  A card is active iff its Event ID equals Work.Review.EventID.
//	    Older event cards are history (resolved). Duplicate facts (dedupe
//	    key) never create a second card or a second review.
//	I8  A lease never survives Host death unless the durable submission
//	    ledger proves mutation may have begun (Pending/Resolved exact
//	    submission): that case is quarantined in Work state and only an
//	    explicit actor resolution closes it. Absent/Aborted exact submission
//	    proves the action was never delivered, so the lease is dropped and
//	    the same unresolved action becomes re-claimable.
//	I9  Missing/terminal delegated Sessions never block Work transitions:
//	    owner strings are references validated against the canonical Turn
//	    ledger, and terminal dispositions detach executor ownership
//	    atomically.
//	I10 Projections (queue counts, cards, attention labels) are recomputed
//	    from canonical Work + append-only facts at startup and after every
//	    reducer pass. There is no event/card repair path and no direct DB
//	    mutation.
// ---------------------------------------------------------------------------

// WorkReview is the canonical Brain review obligation of one Work. It is
// durable Work state. EventID is the sole identity from actionable fact
// through claim, notification, card, and resolution.
type WorkReview struct {
	EventID string `json:"event_id"`
	// RequiredAt is the immutable Event birth. Queue order is oldest first.
	RequiredAt time.Time `json:"required_at"`
	// Lease is nil while the review is pending/claimable.
	Lease *WorkReviewLease `json:"lease,omitempty"`
	// Resolution audit for actor-closed reviews (discard / settled by owner
	// admission). Brain dispositions clear the review without an actor trail;
	// the fact row records the disposition audit instead.
	Resolution string     `json:"resolution,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// WorkReviewLease is the disposable delivery lease of one review event. It
// exists only while a Host is (or was) delivering the current action and is
// re-derived from Work + the Lifecycle admission state at startup; it is never
// append-only truth.
type WorkReviewLease struct {
	HostSessionID string `json:"host_session_id"`
	// HandlingID and ProviderTurnID are the exact delivery capability minted
	// at claim time. They never change for the lifetime of the lease.
	HandlingID     string `json:"handling_id"`
	ProviderTurnID string `json:"provider_turn_id"`
	// DeliveryWorkRevision and DeliverySequenceFence freeze the exact state
	// the disposition must CAS.
	DeliveryWorkRevision  uint64     `json:"delivery_work_revision"`
	DeliverySequenceFence uint64     `json:"delivery_sequence_fence"`
	ClaimedAt             time.Time  `json:"claimed_at"`
	DeliveredAt           *time.Time `json:"delivered_at,omitempty"`
	HandlingEndedAt       *time.Time `json:"handling_ended_at,omitempty"`
	// AmbiguousDelivery quarantines the lease: the exact Lifecycle admission state
	// proves mutation may have begun while the lease Host is gone. Only an
	// explicit actor resolution (mark_delivered/discard/replay) may close it.
	AmbiguousDelivery bool `json:"ambiguous_delivery,omitempty"`
}

// WorkReviewAction is the delivery value of one review event: the current
// action content (from the fact) plus the exact lease capability. It is the
// value the Host lane claims, consumes, and resolves.
type WorkReviewAction struct {
	WorkID         string `json:"work_id"`
	EventID        string `json:"event_id"`
	Kind           string `json:"kind"`
	PayloadRef     string `json:"payload_ref,omitempty"`
	SourceName     string `json:"source_name,omitempty"`
	Summary        string `json:"summary,omitempty"`
	WorkTitle      string `json:"work_title,omitempty"`
	SourceThreadID string `json:"source_thread_id"`

	ClaimedAt             *time.Time `json:"claimed_at,omitempty"`
	DeliveryHostSessionID string     `json:"delivery_host_session_id,omitempty"`
	HandlingID            string     `json:"handling_id,omitempty"`
	ProviderTurnID        string     `json:"provider_turn_id,omitempty"`
	DeliveryWorkRevision  uint64     `json:"delivery_work_revision,omitempty"`
	DeliverySequenceFence uint64     `json:"delivery_sequence_fence,omitempty"`
	DeliveredAt           *time.Time `json:"delivered_at,omitempty"`
	HandlingEndedAt       *time.Time `json:"handling_ended_at,omitempty"`
	AmbiguousDelivery     bool       `json:"ambiguous_delivery,omitempty"`
}

// WorkReviewDispositionRequest is Brain's exact handling transaction for a
// review event. The lease capability and expected Work revision came from the
// delivered compact input, preventing an old Host turn from overwriting newer
// durable state.
type WorkReviewDispositionRequest struct {
	WorkID               string          `json:"work_id"`
	HandlingID           string          `json:"handling_id"`
	ProviderTurnID       string          `json:"provider_turn_id"`
	ExpectedWorkRevision uint64          `json:"expected_work_revision"`
	Disposition          WorkDisposition `json:"disposition"`
	NextSessionID        string          `json:"next_session_id,omitempty"`
	NextTurnToken        string          `json:"next_turn_token,omitempty"`
	AttemptSessionID     string          `json:"attempt_session_id,omitempty"`
	AttemptTurnToken     string          `json:"attempt_turn_token,omitempty"`
	AttemptFence         uint64          `json:"attempt_fence,omitempty"`
	TerminalEvidenceID   string          `json:"terminal_evidence_id,omitempty"`
	Wake                 *WorkWake       `json:"wake,omitempty"`
	NextAction           string          `json:"next_action,omitempty"`
	Summary              string          `json:"summary,omitempty"`
}

// ReviewLeaseResolution values for explicit actor closure of a held review
// lease (the C.2.6 path, now Work-scoped).
type ReviewLeaseResolution string

const (
	ReviewLeaseMarkDelivered ReviewLeaseResolution = "mark_delivered"
	ReviewLeaseDiscard       ReviewLeaseResolution = "discard"
	ReviewLeaseReplay        ReviewLeaseResolution = "replay"
)

// ---------------------------------------------------------------------------
// Transition table
//
// Review event state is (Review, Lease): absent, pending, leased, delivered,
// ended, quarantined.
//
//	absent       Review == nil
//	pending      Review != nil, Lease == nil
//	leased       Lease != nil, DeliveredAt == nil, AmbiguousDelivery == false
//	delivered    Lease.DeliveredAt != nil, HandlingEndedAt == nil
//	ended        Lease.HandlingEndedAt != nil
//	quarantined  Lease.AmbiguousDelivery
//
//	#    From        Event                                            To            Guard / effect
//	1    absent      eligible actionable fact appended                 pending       RequiredAt=now; EventID=fact.ID; card materialized
//	2    pending     newer eligible fact while no lease in flight      pending       EventID := fact.ID (content refresh, same event)
//	3    pending     lane claims at the idle boundary                  leased        lease minted (capability + revision fence)
//	4    leased      receipt proves non-submission                     pending       lease cleared (I8)
//	5    leased      receipt accepted; provider Turn canonical         delivered      DeliveredAt set
//	6    leased      Host gone; no/Aborted exact submission            pending       lease cleared (I8); same action re-claimable
//	7    leased      Host gone; Pending/Resolved exact submission      quarantined   AmbiguousDelivery set; delivery.uncertain note
//	8    delivered   typed disposition resolves                        absent        Work transitioned; fact audited; owner detached if terminal
//	9    delivered   Host turn ended without disposition               ended         HandlingEndedAt set; audit note appended
//	10   ended       lane claims at the idle boundary                  leased        same action re-claimed (no new queue item)
//	11   quarantined actor mark_delivered                              pending       delivery proven; same action re-claimable
//	12   quarantined actor replay                                     pending       lease cleared; same action re-claimable
//	13   quarantined actor discard                                    absent        audit; if Work non-terminal a fresh reconcile fact re-requires (1)
//	14   pending     initial delegated owner admission                absent        event settled (owner executes instead; I9)
//	15   any         operator CloseWork                               absent        audit; event discarded
//	16   delivered   Host turn ends (startup recompute)               ended         requeue; lane resumes
//	17   leased      startup recompute, Host gone, no evidence        pending       re-derivation of I8
//	18   leased      startup recompute, Host gone, evidence           quarantined   re-derivation of I7
//	19   any         disposition clears; eligible fact appended       pending       during the lease (sequence > fence) re-requires a fresh event
//
// Exactly-once: rows 3-5 happen at most once per lease; a disposition (8)
// clears the review, so a second disposition of the same event is refused by
// the capability CAS (I4). Host death re-delivers the same unresolved action
// (6, 9, 10, 17) but can never create or preserve a second queue item.
// ---------------------------------------------------------------------------

func reviewDeliveredAwaitingDisposition(review *WorkReview) bool {
	return review != nil && review.Lease != nil &&
		review.Lease.DeliveredAt != nil && review.Lease.HandlingEndedAt == nil
}

// reviewActionFromReview projects the delivery value of a review event.
func reviewActionFromReview(database presentationDatabase, review *WorkReview) (WorkReviewAction, bool) {
	if review == nil {
		return WorkReviewAction{}, false
	}
	fact, found := workEventByID(database.BrainWorkEvents, review.EventID)
	if !found {
		return WorkReviewAction{}, false
	}
	itemIndex := workIndex(database.BrainWork, fact.WorkID)
	if itemIndex < 0 {
		return WorkReviewAction{}, false
	}
	action := WorkReviewAction{
		WorkID:         fact.WorkID,
		EventID:        fact.ID,
		Kind:           fact.Kind,
		PayloadRef:     fact.PayloadRef,
		SourceName:     fact.SourceName,
		Summary:        fact.Summary,
		WorkTitle:      database.BrainWork[itemIndex].Title,
		SourceThreadID: database.BrainWork[itemIndex].SourceThreadID,
	}
	if lease := review.Lease; lease != nil {
		action.ClaimedAt = &lease.ClaimedAt
		action.DeliveryHostSessionID = lease.HostSessionID
		action.HandlingID = lease.HandlingID
		action.ProviderTurnID = lease.ProviderTurnID
		action.DeliveryWorkRevision = lease.DeliveryWorkRevision
		action.DeliverySequenceFence = lease.DeliverySequenceFence
		action.DeliveredAt = lease.DeliveredAt
		action.HandlingEndedAt = lease.HandlingEndedAt
		action.AmbiguousDelivery = lease.AmbiguousDelivery
	}
	return action, true
}

func workEventByID(events []WorkEvent, eventID string) (WorkEvent, bool) {
	for _, event := range events {
		if event.ID == eventID {
			return event, true
		}
	}
	return WorkEvent{}, false
}

func cloneWorkReview(review *WorkReview) *WorkReview {
	if review == nil {
		return nil
	}
	copy := *review
	copy.EventID = strings.TrimSpace(copy.EventID)
	copy.Resolution = strings.TrimSpace(copy.Resolution)
	copy.ResolvedBy = strings.TrimSpace(copy.ResolvedBy)
	if review.Lease != nil {
		lease := *review.Lease
		lease.HostSessionID = strings.TrimSpace(lease.HostSessionID)
		lease.HandlingID = strings.TrimSpace(lease.HandlingID)
		lease.ProviderTurnID = strings.TrimSpace(lease.ProviderTurnID)
		copy.Lease = &lease
	} else {
		copy.Lease = nil
	}
	return &copy
}

// reviewEligibleFact gates which actionable facts may create or refresh a
// review obligation. Delegated lifecycle rows without the canonical
// turn-scoped identity stay append-only evidence and can never become a
// current action.
func reviewEligibleFact(database presentationDatabase, item Work, event WorkEvent) bool {
	if !event.Actionable {
		return false
	}
	if isSessionLifecycleKind(event.Kind) &&
		!isTurnScopedSessionDedupeKey(event.DedupeKey) && !isCanonicalSessionWakeDedupeKey(event.DedupeKey) {
		return false
	}
	if item.Status != WorkDone && item.Status != WorkCancelled {
		return true
	}
	// TerminalRevision is the immutable causal boundary written by the
	// transition that first terminalized the Work. Late producer results
	// (for example a Calendar result arriving after terminalization) are born
	// after that fence and remain reviewable exactly once; earlier facts are
	// pre-terminal history.
	return item.TerminalRevision != 0 && event.WorkRevision >= item.TerminalRevision
}

// validateWorkReview enforces I1-I4 on the canonical review record.
func validateWorkReview(database presentationDatabase, item Work) error {
	review := item.Review
	if review == nil {
		return nil
	}
	if review.RequiredAt.IsZero() || strings.TrimSpace(review.EventID) == "" {
		return fmt.Errorf("review requires required_at and event_id")
	}
	fact, found := workEventByID(database.BrainWorkEvents, review.EventID)
	if !found || fact.WorkID != item.ID {
		return fmt.Errorf("review event_id %q does not name a fact of Work %s", review.EventID, item.ID)
	}
	if !reviewEligibleFact(database, item, fact) {
		return fmt.Errorf("review Event %q is not eligible to obligate Work %s", fact.ID, item.ID)
	}
	lease := review.Lease
	if lease == nil {
		return nil
	}
	if lease.ClaimedAt.IsZero() || strings.TrimSpace(lease.HandlingID) == "" ||
		strings.TrimSpace(lease.ProviderTurnID) == "" || strings.TrimSpace(lease.HostSessionID) == "" ||
		lease.HandlingID == lease.ProviderTurnID || lease.DeliveryWorkRevision == 0 ||
		lease.DeliverySequenceFence == 0 {
		return fmt.Errorf("review lease requires distinct handling and provider Turn identities, Work revision, and sequence fence")
	}
	if lease.DeliveredAt != nil && lease.ClaimedAt.After(*lease.DeliveredAt) {
		return fmt.Errorf("review lease delivered_at precedes claimed_at")
	}
	if lease.HandlingEndedAt != nil {
		if lease.DeliveredAt == nil {
			return fmt.Errorf("review lease handling cannot end before delivery")
		}
		if lease.HandlingEndedAt.Before(*lease.DeliveredAt) {
			return fmt.Errorf("review lease handling_ended_at precedes delivered_at")
		}
	}
	return nil
}

// databaseHasExactReviewLease matches a Host submission transaction against
// the canonical review lease: the five-part capability (fact receipt, claim
// token, Work, Host Session, provider Turn) is the admission authority.
func databaseHasExactReviewLease(database presentationDatabase, submission watcher.InputAdmission) bool {
	itemIndex := workIndex(database.BrainWork, submission.WorkID)
	if itemIndex < 0 {
		return false
	}
	review := database.BrainWork[itemIndex].Review
	if review == nil || review.Lease == nil {
		return false
	}
	lease := review.Lease
	return lease.HandlingID == submission.ClaimToken &&
		lease.ProviderTurnID == submission.ProposedTurnID &&
		lease.HostSessionID == submission.SessionID &&
		review.EventID == submission.Receipt &&
		lease.DeliveredAt == nil && lease.HandlingEndedAt == nil
}

// reviewDeliveredInFlightIndex returns the Work index whose review lease is
// delivered and awaiting disposition, or -1.
func reviewDeliveredInFlightIndex(database presentationDatabase) int {
	for index := range database.BrainWork {
		if reviewDeliveredAwaitingDisposition(database.BrainWork[index].Review) {
			return index
		}
	}
	return -1
}

// reviewLeaseByCapability finds the Work index whose review lease matches the
// exact delivery capability, or -1.
func reviewLeaseByCapability(database presentationDatabase, workID, handlingID, providerTurnID string) int {
	itemIndex := workIndex(database.BrainWork, workID)
	if itemIndex < 0 {
		return -1
	}
	lease := database.BrainWork[itemIndex].Review
	if lease == nil || lease.Lease == nil {
		return -1
	}
	if !leaseCapabilityMatches(lease.Lease, workID, handlingID, providerTurnID) {
		return -1
	}
	return itemIndex
}

func leaseCapabilityMatches(lease *WorkReviewLease, workID, handlingID, providerTurnID string) bool {
	return lease != nil && strings.TrimSpace(lease.HandlingID) == strings.TrimSpace(handlingID) &&
		strings.TrimSpace(lease.ProviderTurnID) == strings.TrimSpace(providerTurnID) &&
		strings.TrimSpace(lease.HostSessionID) != ""
}

// RecoverReviewLease recovers one review lease whose DeliveryHost Session the
// caller proved is gone, using the durable Lifecycle admission state as mutation
// evidence (I8). An absent exact submission (or one Aborted before mutation)
// proves the action was never delivered to the old Host: the lease is dropped
// and the same unresolved action becomes re-claimable by the current Host. A
// Pending/Resolved exact submission means mutation may have begun: the lease
// is quarantined in Work state (AmbiguousDelivery) and only an explicit actor
// resolution (mark_delivered/discard/replay) closes it. The recovery is
// evidence-based, never time-based, and never touches a delivered lease.
func (s *Store) RecoverReviewLease(workID, handlingID, providerTurnID string) (bool, error) {
	workID = strings.TrimSpace(workID)
	handlingID = strings.TrimSpace(handlingID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if workID == "" || handlingID == "" || providerTurnID == "" {
		return false, fmt.Errorf("work_id, handling_id, and provider_turn_id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return false, err
	}
	itemIndex := reviewLeaseByCapability(database, workID, handlingID, providerTurnID)
	if itemIndex < 0 {
		return false, nil
	}
	review := database.BrainWork[itemIndex].Review
	lease := review.Lease
	if lease.DeliveredAt != nil || lease.HandlingEndedAt != nil {
		// A delivered lease is owned by the delivered-handling recovery path
		// (EndReviewDelivery), never by this evidence check.
		return false, nil
	}
	if _, err := s.fsm.ReleaseReview(lifecycle.WorkID(workID), lifecycle.TurnToken(providerTurnID)); err != nil {
		return false, err
	}
	if err := s.fsmSyncWorkLocked(&database, workID, s.nowUTC()); err != nil {
		return false, err
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return false, err
	}
	s.broadcastWorkChange(workID)
	return true, nil
}
