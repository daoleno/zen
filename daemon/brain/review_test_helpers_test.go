package brain

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func pendingSubmissionDigest(payload string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
}

// reviewTestHelpers.go: Work-centric assertion helpers for the review-event
// scheduler. Tests must never read claim/delivery state from fact rows (I1):
// the lease lives on Work.Review.

// reviewLeaseOf returns the current review lease of a Work, or nil.
func reviewLeaseOf(t *testing.T, store *Store, workID string) *WorkReviewLease {
	t.Helper()
	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Review == nil {
		return nil
	}
	return item.Review.Lease
}

func requireReviewDelivered(t *testing.T, store *Store, workID string) *WorkReviewLease {
	t.Helper()
	lease := reviewLeaseOf(t, store, workID)
	if lease == nil || lease.DeliveredAt == nil {
		t.Fatalf("Work %s review lease is not delivered: %+v", workID, lease)
	}
	return lease
}

func requireReviewPending(t *testing.T, store *Store, workID string) {
	t.Helper()
	lease := reviewLeaseOf(t, store, workID)
	if lease != nil {
		t.Fatalf("Work %s review lease is not pending: %+v", workID, lease)
	}
	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Review == nil {
		t.Fatalf("Work %s has no review obligation", workID)
	}
}

// claimAndDeliverTestReview claims the next review action for the Host,
// prepares+resolves the exact Host submission, and consumes the delivery.
func claimAndDeliverTestReview(t *testing.T, store *Store, hostID string) (WorkReviewAction, Work) {
	t.Helper()
	claimed, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok {
		t.Fatalf("claim next review ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, item, err := store.ConsumeReviewDelivery(
		claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
	)
	if err != nil {
		t.Fatalf("consume review delivery: %v", err)
	}
	return delivered, item
}

// resolveDeliveredReview resolves the delivered review of one Work with a
// typed disposition.
func resolveDeliveredReview(t *testing.T, store *Store, workID string, disposition WorkDisposition) (WorkEvent, Work) {
	t.Helper()
	lease := requireReviewDelivered(t, store, workID)
	event, item, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: workID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: disposition,
	})
	if err != nil {
		t.Fatalf("resolve Work %s review: %v", workID, err)
	}
	return event, item
}

// reviewLeaseDelivered reports whether the Work's review lease is delivered.
func reviewLeaseDelivered(t *testing.T, store *Store, workID string) bool {
	t.Helper()
	lease := reviewLeaseOf(t, store, workID)
	return lease != nil && lease.DeliveredAt != nil
}

// storeWorkID returns the Work ID owned by a session, fatal on absence.
func storeWorkID(t *testing.T, store *Store, sessionID string) string {
	t.Helper()
	item, _, err := store.WorkByAttemptSession(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}

// factIDOf returns the current review fact ID of a Work, fatal on absence.
func factIDOf(t *testing.T, store *Store, workID string) string {
	t.Helper()
	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Review == nil {
		t.Fatalf("Work %s has no review", workID)
	}
	return item.Review.EventID
}

func createSignalTestWork(t *testing.T, store *Store, title, owner string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{
		Title: title, Objective: "Exercise the durable Brain signal protocol.",
		Status: WorkWaiting, AttemptSessionID: owner,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Review the delegated Session result.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func appendSignalTestEvent(t *testing.T, store *Store, item Work, suffix string) WorkEvent {
	t.Helper()
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:" + item.AttemptSessionID + ":turn:" + suffix + ":session.done",
		PayloadRef: "session:" + item.AttemptSessionID, SourceName: item.AttemptSessionID,
		Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("append event created=%v err=%v", created, err)
	}
	if _, err := store.FSM().OpenReviewEvent(lifecycle.WorkID(item.ID), event.Kind, event.ID, event.ID); err != nil {
		t.Fatalf("open canonical test Review: %v", err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatalf("project canonical test Review: %v", err)
	}
	return event
}

func deliverSignalTestEvent(t *testing.T, store *Store, hostID string) (WorkReviewAction, Work) {
	t.Helper()
	claimed, ok, err := store.ClaimNextReviewAction(hostID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	resolveClaimedHostTurnForTest(t, store, claimed)
	delivered, item, err := store.ConsumeReviewDelivery(claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatalf("consume claimed event: %v", err)
	}
	return delivered, item
}

func resolveClaimedHostTurnForTest(t *testing.T, store *Store, claimed WorkReviewAction) {
	t.Helper()
	existingTurnID := ""
	if current, found, err := store.Turn(claimed.DeliveryHostSessionID); err != nil {
		t.Fatal(err)
	} else if found {
		existingTurnID = current.TurnID
		if !watcher.TurnImmutable(current.Status) {
			settleCanonicalHostTurnForTest(t, store, current.SessionID, current.TurnID)
		}
	}
	acceptedAt := time.Now().UTC()
	payloadDigest := pendingSubmissionDigest("claimed Host review " + claimed.EventID)
	pending, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID,
		ProposedTurnID: claimed.ProviderTurnID, Receipt: claimed.EventID,
		ClaimToken: claimed.HandlingID, PayloadSHA256: payloadDigest,
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.InputAdmissionFresh, ExistingTurnID: existingTurnID,
	})
	if err != nil || !created {
		t.Fatalf("prepare Host provider turn created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveInputAdmission(watcher.InputAdmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.EventID, PayloadSHA256: pending.PayloadSHA256,
		ActivityID: "host-activity-" + claimed.ProviderTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + claimed.ProviderTurnID,
			Cursor: 1, SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatalf("resolve Host provider turn: %v", err)
	}
}

func settleCanonicalHostTurnForTest(t *testing.T, store *Store, sessionID, turnID string) watcher.TurnSnapshot {
	t.Helper()
	current, found, err := store.TurnByID(sessionID, turnID)
	if err != nil || !found {
		t.Fatalf("canonical Host Turn %s found=%v err=%v", turnID, found, err)
	}
	if watcher.TurnImmutable(current.Status) {
		return current
	}
	settledAt := time.Now().UTC()
	if !settledAt.After(current.AcceptedAt) {
		settledAt = current.AcceptedAt.Add(time.Second)
	}
	settled, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: current.SessionID, TurnID: current.TurnID,
		Class: watcher.EvidenceProvider, Kind: "done", Bound: true,
		SourceID:  "provider\x00test-host\x00" + current.TurnID + "\x00done",
		Admission: current.Admission, ActivityID: current.ActivityID,
		StartedAt: current.AcceptedAt, SettledAt: settledAt, At: settledAt,
	})
	if err != nil || !changed || !watcher.TurnImmutable(settled.Status) {
		t.Fatalf("settle Host provider turn: turn=%+v changed=%v err=%v", settled, changed, err)
	}
	return settled
}

func bootstrapAdmittedTurnFixture(t *testing.T, store *Store, workID string, admitted watcher.AdmittedTurn) {
	t.Helper()
	workID = strings.TrimSpace(workID)
	admitted.SessionID = strings.TrimSpace(admitted.SessionID)
	admitted.TurnID = strings.TrimSpace(admitted.TurnID)
	if workID == "" || admitted.SessionID == "" || admitted.TurnID == "" || admitted.AcceptedAt.IsZero() {
		t.Fatal("exact Work, Session, Turn, and acceptance identities are required")
	}
	// Establish canonical ownership before persisting the compatibility turn
	// projection. Database validation intentionally rejects an admitted turn
	// whose Session is not the aggregate's current owner.
	if err := store.fsmAdmitTurn(workID, admitted.SessionID, admitted.TurnID, true); err != nil {
		t.Fatalf("canonical fixture admission: %v", err)
	}
	if err := store.SyncWorkProjection(workID); err != nil {
		t.Fatalf("canonical fixture projection: %v", err)
	}
	store.mu.Lock()
	database, err := store.loadPresentationLocked()
	if err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		store.mu.Unlock()
		t.Fatalf("fixture Work %s not found", workID)
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == admitted.SessionID && turn.TurnID == admitted.TurnID {
			store.mu.Unlock()
			return
		}
	}
	database.BrainTurns = append(database.BrainTurns, TurnRecord{
		SessionID: admitted.SessionID, TurnID: admitted.TurnID, WorkID: workID,
		Status: watcher.TurnAdmitted, Receipt: strings.TrimSpace(admitted.Receipt),
		PaneGeneration:  strings.TrimSpace(admitted.PaneGeneration),
		ProcessIdentity: strings.TrimSpace(admitted.ProcessIdentity),
		PayloadSHA256:   strings.TrimSpace(admitted.PayloadSHA256),
		AcceptedAt:      admitted.AcceptedAt.UTC(), Facts: []TurnFactRecord{},
		TranscriptBinding: admitted.TranscriptBinding,
		LeaseDeadline:     admitted.AcceptedAt.Add(turnLeaseGrace).UTC(), UpdatedAt: store.nowUTC(),
	})
	if err := store.persistPresentationLocked(database); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
}

func ledgerTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-test:@1"
	item, err := store.CreateWork(Work{
		Title: "Canonical turn test", Objective: "Exercise the single reducer.",
		Status: WorkRunning, AttemptSessionID: sessionID,
		CompletionPolicy: CompletionBounded, NextAction: "Wait for the delegated Session.",
		WaitFor: "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "proc-identity-1", PaneGeneration: "pane-gen-1",
		PayloadSHA256: "payload-digest",
	})
	return store, sessionID, turnID
}

func delegatedSubmissionCandidate(workID, sessionID, turnID, payload string, acceptedAt time.Time) watcher.InputAdmission {
	return watcher.InputAdmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pendingSubmissionDigest(payload), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", AcceptedAt: acceptedAt, Mode: watcher.InputAdmissionFresh,
	}
}

func resolveDelegatedSubmission(t *testing.T, store *Store, pending watcher.InputAdmission, activityID string, at time.Time) watcher.InputAdmission {
	t.Helper()
	resolved, err := store.ResolveInputAdmission(watcher.InputAdmissionResolution{
		SessionID: pending.SessionID, ProposedTurnID: pending.ProposedTurnID,
		Receipt: pending.Receipt, PayloadSHA256: pending.PayloadSHA256, ActivityID: activityID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-" + activityID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: at.UTC(),
		},
		ResolvedAt: at.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func providerAdmission(stream, id string, cursor uint64, sha string, at time.Time) watcher.TurnAdmission {
	return watcher.TurnAdmission{Stream: stream, ID: id, Cursor: cursor, SHA256: sha, At: at}
}

func turnEvent(t *testing.T, store *Store, workID, dedupeKey string) (WorkEvent, bool) {
	t.Helper()
	events, err := store.ListWorkEvents(workID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.DedupeKey == dedupeKey {
			return event, true
		}
	}
	return WorkEvent{}, false
}

func countUnhandledEventKind(events []WorkEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.HandledAt == nil && event.DiscardedAt == nil {
			count++
		}
	}
	return count
}
