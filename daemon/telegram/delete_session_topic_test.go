package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

// markAbsent makes a fixture Session invisible. confirmed=false models an
// unreadable/unreachable canonical probe that must never authorize deletion.
func markAbsent(owner *fakeBrain, sessionID string, confirmed bool) {
	owner.sessions = nil
	if owner.projections == nil {
		owner.projections = map[string]brain.SessionProjection{}
	}
	owner.projections[sessionID] = brain.SessionProjection{SessionID: sessionID, Present: false}
	if !confirmed {
		if owner.absenceUnconfirmed == nil {
			owner.absenceUnconfirmed = map[string]bool{}
		}
		owner.absenceUnconfirmed[sessionID] = true
	}
}

func deleteOpFor(state durableState, sessionID string) (topicOpRecord, bool) {
	for _, op := range state.TopicOps {
		if op.Kind == topicOpDelete && op.SessionID == sessionID {
			return op, true
		}
	}
	return topicOpRecord{}, false
}

func TestPresentSessionTopicIsNeverDeleted(t *testing.T) {
	tests := []struct {
		name       string
		projection brain.SessionProjection
	}{
		{name: "running", projection: brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running"}},
		{name: "done-but-present", projection: brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "idle", TurnStatus: "done"}},
		{name: "terminal-work-but-present", projection: brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", WorkStatus: "done"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, owner, api, _ := topicFixture(t)
			createTopicFor(t, manager)
			owner.projections["sess-a"] = test.projection
			if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
				t.Fatal(err)
			}
			if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
				t.Fatal(err)
			}
			if len(api.deletedTopics) != 0 {
				t.Fatalf("present Session deleted: %+v", api.deletedTopics)
			}
			if state := manager.store.snapshot(); len(state.Topics) != 1 {
				t.Fatalf("present mapping lost: %+v", state.Topics)
			}
		})
	}
}

func TestUncertainAndErroredAbsencePreserveTopic(t *testing.T) {
	// Absence that is not strictly confirmed never retires or deletes.
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	markAbsent(owner, "sess-a", false)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateActive {
		t.Fatalf("uncertain absence retired mapping: %+v", state.Topics)
	}
	if _, found := deleteOpFor(state, "sess-a"); found {
		t.Fatalf("uncertain absence queued delete: %+v", state.TopicOps)
	}
	if len(api.deletedTopics) != 0 {
		t.Fatalf("uncertain absence deleted: %+v", api.deletedTopics)
	}

	// A canonical projection error fails the reconcile without touching state.
	manager2, owner2, api2, _ := topicFixture(t)
	createTopicFor(t, manager2)
	owner2.projectionErrors = map[string]error{"sess-a": errors.New("watcher inventory unavailable")}
	if err := manager2.projectSessionTopics(context.Background(), "token"); err == nil {
		t.Fatal("projection error was ignored")
	}
	state = manager2.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateActive {
		t.Fatalf("projection error changed mapping: %+v", state.Topics)
	}
	if len(api2.deletedTopics) != 0 {
		t.Fatalf("projection error reached delete: %+v", api2.deletedTopics)
	}
}

func TestReappearanceBeforeDispatchCancelsDelete(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	markAbsent(owner, "sess-a", true)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if op, found := deleteOpFor(state, "sess-a"); !found || op.State != "pending" {
		t.Fatalf("delete op not queued: %+v", state.TopicOps)
	}

	// The exact Session identity is user-visible again before dispatch.
	owner.sessions = []brain.WorkerRef{{ID: "sess-a", Name: "Session A", Delegated: true, Status: "running"}}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state = manager.store.snapshot()
	if state.Topics[0].State != topicStateActive {
		t.Fatalf("revived mapping state=%s", state.Topics[0].State)
	}
	if _, found := deleteOpFor(state, "sess-a"); found {
		t.Fatalf("pending delete survived revival: %+v", state.TopicOps)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.deletedTopics) != 0 || len(api.editedTopics) != 0 {
		t.Fatalf("reappeared Session deleted: deletes=%+v edits=%+v", api.deletedTopics, api.editedTopics)
	}
	if got := topicThreadFor(t, manager, "sess-a"); got != threadID {
		t.Fatal("exact topic identity lost")
	}
}

func TestDeleteRefusesWrongBindingOrMissingMapping(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	markAbsent(owner, "sess-a", true)

	queue := func() {
		t.Helper()
		if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	deliver := func() {
		t.Helper()
		if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
			t.Fatal(err)
		}
	}
	mutateDelete := func(fn func(*topicOpRecord)) {
		t.Helper()
		if err := manager.store.mutate(func(state *durableState) error {
			for i := range state.TopicOps {
				if state.TopicOps[i].Kind == topicOpDelete {
					fn(&state.TopicOps[i])
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Wrong chat binding is refused and cancels the op.
	queue()
	mutateDelete(func(op *topicOpRecord) { op.ChatID = 999 })
	deliver()
	if len(api.deletedTopics) != 0 {
		t.Fatalf("wrong chat deleted: %+v", api.deletedTopics)
	}
	state := manager.store.snapshot()
	if op, found := deleteOpFor(state, "sess-a"); !found || op.State != "cancelled" {
		t.Fatalf("wrong-chat op not cancelled: %+v", state.TopicOps)
	}
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("wrong-chat delete removed tombstone: %+v", state.Topics)
	}

	// Wrong bot binding is refused; a cancelled attempt is re-queued once the
	// exact binding exists again and then succeeds.
	queue()
	mutateDelete(func(op *topicOpRecord) { op.BotID = 42 })
	deliver()
	if len(api.deletedTopics) != 0 {
		t.Fatalf("wrong bot deleted: %+v", api.deletedTopics)
	}
	queue()
	deliver()
	if len(api.deletedTopics) != 1 {
		t.Fatalf("re-queued exact delete not dispatched: %+v", api.deletedTopics)
	}

	// A replaced/removed mapping refuses the stale op.
	manager2, owner2, api2, _ := topicFixture(t)
	createTopicFor(t, manager2)
	markAbsent(owner2, "sess-a", true)
	if err := manager2.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager2.store.mutate(func(state *durableState) error {
		state.Topics = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager2.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api2.deletedTopics) != 0 {
		t.Fatalf("missing mapping deleted: %+v", api2.deletedTopics)
	}
}

func TestDeleteRetryableAndAmbiguousOutcomesRetainRecoverableState(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	markAbsent(owner, "sess-a", true)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}

	// A definite flood wait returns to pending with a bounded retry and never
	// removes the tombstone.
	api.topicOpErrs = map[string]error{"delete": &APIError{Code: 429, Retryable: true, RetryAfter: 3 * time.Second, description: "Too Many Requests: retry after 3"}}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	op, found := deleteOpFor(state, "sess-a")
	if !found || op.State != "pending" || op.AttemptAt.IsZero() {
		t.Fatalf("flood-wait delete not recoverable: %+v", state.TopicOps)
	}
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("flood-wait delete removed mapping: %+v", state.Topics)
	}
	// Reconcile must not duplicate the queued delete.
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.deletedTopics) != 1 {
		t.Fatalf("flood wait retried early: %+v", api.deletedTopics)
	}

	// After the wait, a transport-indeterminate outcome becomes durable
	// ambiguous: never replayed, mapping retained, status truthful.
	manager.now = func() time.Time { return time.Date(2026, 8, 24, 12, 0, 5, 0, time.UTC) }
	api.topicOpErrs = map[string]error{"delete": errors.New("connection closed after request write")}
	if err := manager.deliverTopicOpOne(context.Background(), "token"); err == nil {
		t.Fatal("ambiguous delete returned success")
	}
	state = manager.store.snapshot()
	op, found = deleteOpFor(state, "sess-a")
	if !found || op.State != "ambiguous" {
		t.Fatalf("transport delete not ambiguous: %+v", state.TopicOps)
	}
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("ambiguous delete removed mapping: %+v", state.Topics)
	}
	attempts := len(api.deletedTopics)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.deletedTopics) != attempts {
		t.Fatalf("ambiguous delete replayed: %d -> %d", attempts, len(api.deletedTopics))
	}
	if status := manager.Status(); status.TopicAmbiguousOps != 1 || status.State != StateDegraded {
		t.Fatalf("ambiguous delete status=%+v", status)
	}

	// A reappearing exact Session cannot revive over an unresolved deletion.
	owner.sessions = []brain.WorkerRef{{ID: "sess-a", Name: "Session A", Delegated: true, Status: "running"}}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err == nil {
		t.Fatal("ambiguous deletion revived")
	}
	if state := manager.store.snapshot(); state.Topics[0].State != topicStateStale {
		t.Fatalf("ambiguous deletion reactivated mapping: %+v", state.Topics)
	}
}

func TestDeleteAlreadyMissingConvergesAndDefiniteFailureRetainsTombstone(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	markAbsent(owner, "sess-a", true)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	api.topicOpErrs = map[string]error{"delete": &APIError{Code: 400, description: "Bad Request: TOPIC_ID_INVALID"}}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if len(api.deletedTopics) != 1 {
		t.Fatalf("already-missing not attempted: %+v", api.deletedTopics)
	}
	if len(state.Topics) != 0 {
		t.Fatalf("authoritative already-missing kept mapping: %+v", state.Topics)
	}

	// Any other definite rejection is not already-missing: the exact tombstone
	// stays and the failure is reported.
	manager2, owner2, api2, _ := topicFixture(t)
	createTopicFor(t, manager2)
	markAbsent(owner2, "sess-a", true)
	if err := manager2.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	api2.topicOpErrs = map[string]error{"delete": &APIError{Code: 400, description: "Bad Request: not enough rights to delete the topic"}}
	if err := manager2.deliverTopicOps(context.Background(), "token", 8); err == nil {
		t.Fatal("definite delete rejection returned success")
	}
	state = manager2.store.snapshot()
	if op, found := deleteOpFor(state, "sess-a"); !found || op.State != "failed" {
		t.Fatalf("definite rejection state=%+v", state.TopicOps)
	}
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("definite rejection removed tombstone: %+v", state.Topics)
	}
	if status := manager2.Status(); status.State != StateDegraded || status.TopicFailedOps == 0 {
		t.Fatalf("failure not reported: %+v", status)
	}
}

func TestLegacyUnboundDeleteIsCancelledButBoundDeleteProceeds(t *testing.T) {
	manager, _, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	thread := topicThreadFor(t, manager, "sess-a")
	if err := manager.store.mutate(func(state *durableState) error {
		enqueueTopicOp(state, topicOpRecord{ID: "legacy:delete", Kind: topicOpDelete, SessionID: "sess-a", MessageThreadID: thread})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for manager.hasDeliverableTopicOp() {
		if err := manager.deliverTopicOpOne(t.Context(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	if len(api.deletedTopics) != 0 {
		t.Fatalf("unbound legacy delete dispatched: %+v", api.deletedTopics)
	}
	for _, op := range manager.store.snapshot().TopicOps {
		if op.ID == "legacy:delete" && op.State != "cancelled" {
			t.Fatalf("legacy delete op=%+v", op)
		}
	}
	if got := topicThreadFor(t, manager, "sess-a"); got != thread {
		t.Fatal("topic identity lost")
	}
}
