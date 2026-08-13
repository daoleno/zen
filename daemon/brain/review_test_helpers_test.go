package brain

import (
	"testing"
)

// reviewTestHelpers.go: Work-centric assertion helpers for the review-epoch
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
	item, _, err := store.WorkByOwnerSession(sessionID)
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
	return item.Review.FactEventID
}
