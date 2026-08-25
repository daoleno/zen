package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

// topicFixture returns a configured, bound manager whose fake brain exposes one
// user-visible delegated Session with mapped topic state helpers.
func topicFixture(t *testing.T) (*Manager, *fakeBrain, *fakeAPI, string) {
	t.Helper()
	manager, owner, api, root := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	owner.sessions = []brain.AgentRef{{ID: "sess-a", Name: "Session A", Delegated: true, Status: "running"}}
	owner.sessionWork = map[string]brain.Work{"sess-a": {ID: "work-a", Title: "Work A", Status: brain.WorkRunning, SourceThreadID: "thread-1"}}
	owner.projections = map[string]brain.SessionProjection{
		"sess-a": {SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running", WorkID: "work-a", WorkStatus: "running", WorkTitle: "Work A"},
	}
	return manager, owner, api, root
}

func createTopicFor(t *testing.T, manager *Manager) {
	t.Helper()
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].MessageThreadID == 0 {
		t.Fatalf("topic not created: %+v", state.Topics)
	}
}

func topicUpdate(updateID, threadID int64, text string) Update {
	return Update{UpdateID: updateID, Message: &Message{
		MessageID: updateID, MessageThreadID: threadID, IsTopicMessage: threadID != 0,
		From: &User{ID: 10, FirstName: "Owner"}, Chat: Chat{ID: 10, Type: "private"}, Text: text,
	}}
}

func TestTopicRoutingGeneralToBrainAndMappedSessionOnly(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(10, 0, "hello brain")); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(11, threadID, "hello session")); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(12, 1, "hello general-1")); err != nil {
		t.Fatal(err)
	}

	if len(owner.receipts) != 2 || len(owner.sessionReceipts) != 1 {
		t.Fatalf("brain receipts=%v session receipts=%v", owner.receipts, owner.sessionReceipts)
	}
	for _, body := range owner.bodies {
		if strings.Contains(body, "hello session") {
			t.Fatalf("Session text crossed into Brain: %q", body)
		}
	}
	if owner.bodies[0] != "hello brain" || owner.bodies[1] != "hello general-1" {
		t.Fatalf("brain bodies=%v", owner.bodies)
	}
	if owner.sessionBodies[0] != "hello session" {
		t.Fatalf("session body=%q", owner.sessionBodies[0])
	}
	if !strings.HasPrefix(owner.sessionReceipts[0], "telegram:update:7001:11") {
		t.Fatalf("session receipt=%q", owner.sessionReceipts[0])
	}
	state := manager.store.snapshot()
	if state.Processed["10"].Disposition != "accepted" || state.Processed["11"].Disposition != "session_accepted" || state.Processed["12"].Disposition != "accepted" {
		t.Fatalf("dispositions=%+v", state.Processed)
	}
}

func TestTwoMappedSessionsRemainIsolatedBothDirections(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	owner.sessions = append(owner.sessions, brain.AgentRef{ID: "sess-b", Name: "Session B", Delegated: true, Status: "running"})
	owner.sessionWork["sess-b"] = brain.Work{ID: "work-b", Title: "Work B", Status: brain.WorkRunning, SourceThreadID: "thread-1"}
	owner.projections["sess-b"] = brain.SessionProjection{SessionID: "sess-b", Present: true, Label: "Session B", Status: "running", TurnStatus: "running"}

	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if len(state.Topics) != 2 {
		t.Fatalf("topics=%+v", state.Topics)
	}
	threadA := topicThreadFor(t, manager, "sess-a")
	threadB := topicThreadFor(t, manager, "sess-b")
	if threadA == threadB {
		t.Fatalf("sessions share topic %d", threadA)
	}

	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(20, threadA, "for a")); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(21, threadB, "for b")); err != nil {
		t.Fatal(err)
	}
	// Output projection: each session's assistant text goes only to its own topic.
	owner.mu.Lock()
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running",
		Assistant: []brain.SessionAssistantItem{{ID: "e-a1", Body: "answer for A", Partial: false}}}
	owner.projections["sess-b"] = brain.SessionProjection{SessionID: "sess-b", Present: true, Label: "Session B", Status: "running", TurnStatus: "running",
		Assistant: []brain.SessionAssistantItem{{ID: "e-b1", Body: "answer for B", Partial: false}}}
	owner.mu.Unlock()
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}

	state = manager.store.snapshot()
	rowCount := func(cond func(outboxRecord) bool) int {
		count := 0
		for _, row := range state.Outbox {
			if cond(row) {
				count++
			}
		}
		return count
	}
	if rowCount(func(row outboxRecord) bool {
		return strings.Contains(row.Text, "answer for A") && row.MessageThreadID == threadA
	}) != 1 {
		t.Fatalf("A output not isolated: %+v", state.Outbox)
	}
	if rowCount(func(row outboxRecord) bool {
		return strings.Contains(row.Text, "answer for B") && row.MessageThreadID == threadB
	}) != 1 {
		t.Fatalf("B output not isolated: %+v", state.Outbox)
	}
	if rowCount(func(row outboxRecord) bool {
		return strings.Contains(row.Text, "answer for A") && row.MessageThreadID != threadA
	}) != 0 ||
		rowCount(func(row outboxRecord) bool {
			return strings.Contains(row.Text, "answer for B") && row.MessageThreadID != threadB
		}) != 0 {
		t.Fatalf("cross-session output: %+v", state.Outbox)
	}

	// Inbound isolation is by durable thread mapping: only exact call targets.
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if len(owner.sessionBodies) != 2 || owner.sessionBodies[0] != "for a" || owner.sessionBodies[1] != "for b" {
		t.Fatalf("session inputs=%v", owner.sessionBodies)
	}
}

func topicThreadFor(t *testing.T, manager *Manager, sessionID string) int64 {
	t.Helper()
	state := manager.store.snapshot()
	for _, mapping := range state.Topics {
		if mapping.SessionID == sessionID {
			return mapping.MessageThreadID
		}
	}
	t.Fatalf("no topic for %s", sessionID)
	return 0
}

func TestMultipleSessionTopicMessagesQueueInOrderWhileWorking(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	// The Session is working: direct input must still be accepted in order.
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running"}

	for updateID := int64(30); updateID <= 34; updateID++ {
		if err := manager.handleUpdate(context.Background(), "token", topicUpdate(updateID, threadID, "message-"+string(rune('0'+updateID-30)))); err != nil {
			t.Fatal(err)
		}
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if len(owner.sessionBodies) != 5 {
		t.Fatalf("session inputs=%v", owner.sessionBodies)
	}
	for index, expected := range []string{"message-0", "message-1", "message-2", "message-3", "message-4"} {
		if owner.sessionBodies[index] != expected {
			t.Fatalf("order[%d]=%q want %q", index, owner.sessionBodies[index], expected)
		}
	}
}

func TestSessionTopicFailClosedCases(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	// Unknown Topic: no mapping, fails closed in that Topic.
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(40, 9999, "who is there")); err != nil {
		t.Fatal(err)
	}
	// Not submitted admission failure (definite, actionable, no replay).
	owner.sessionDisposition = brain.ExternalInputNotSubmitted
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(41, threadID, "retry me")); err != nil {
		t.Fatal(err)
	}
	// Uncertain admission: no-replay message, no accepted admission.
	owner.sessionDisposition = brain.ExternalInputUncertain
	owner.sessionDispositionErr = errors.New("provider admission unknown")
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(42, threadID, "unknown outcome")); err != nil {
		t.Fatal(err)
	}
	// Unsupported media in a topic.
	if err := manager.handleUpdate(context.Background(), "token", Update{UpdateID: 43, Message: &Message{
		MessageID: 43, MessageThreadID: threadID, IsTopicMessage: true,
		From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Photo: []any{map[string]any{"file_id": "x"}},
	}}); err != nil {
		t.Fatal(err)
	}
	// Dead Session: mapping exists but the Session is gone; fails closed and
	// the mapping becomes durable stale.
	owner.sessions = nil
	owner.sessionDisposition = brain.ExternalInputAccepted
	owner.sessionDispositionErr = nil
	owner.projections = map[string]brain.SessionProjection{"sess-a": {SessionID: "sess-a", Present: false}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(44, threadID, "still there?")); err != nil {
		t.Fatal(err)
	}

	state := manager.store.snapshot()
	dispositions := map[string]string{}
	for id, record := range state.Processed {
		dispositions[id] = record.Disposition
	}
	for _, id := range []string{"40", "41", "42", "43", "44"} {
		if dispositions[id] == "" {
			t.Fatalf("missing disposition for update %s: %v", id, dispositions)
		}
	}
	if dispositions["40"] != "topic_unknown" || dispositions["41"] != "session_not_submitted" ||
		dispositions["42"] != "session_uncertain" || dispositions["43"] != "unsupported_media" ||
		dispositions["44"] != "topic_stale" {
		t.Fatalf("dispositions=%v", dispositions)
	}
	for _, record := range state.Processed {
		if record.Disposition == "session_accepted" {
			t.Fatalf("fail-closed input accepted: %+v", record)
		}
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	// The provider owner was consulted only for the two real admission attempts
	// (not-submitted and uncertain); unknown/stale/media never crossed it.
	if len(owner.sessionReceipts) != 2 {
		t.Fatalf("admission attempts=%v", owner.sessionReceipts)
	}

	// The actionable replies are scoped to the exact Topic the owner wrote in
	// (the unknown-topic reply stays in the unknown Topic, never General).
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	for _, sent := range api.sent {
		if sent.MessageThreadID == 0 {
			continue // binding/General messages
		}
		if sent.MessageThreadID != threadID && sent.MessageThreadID != 9999 {
			t.Fatalf("reply escaped topics into %d: %q", sent.MessageThreadID, sent.Text)
		}
		if sent.MessageThreadID == 9999 && !strings.Contains(sent.Text, "not mapped") {
			t.Fatalf("unknown-topic reply=%q", sent.Text)
		}
	}
	if manager.Status().TopicMappings != 1 {
		t.Fatalf("status=%+v", manager.Status())
	}
}

func TestTopicCreateLifecyclePersistenceAndCompletionReopen(t *testing.T) {
	manager, owner, api, root := topicFixture(t)
	createTopicFor(t, manager)
	mapping := manager.store.snapshot().Topics[0]
	if mapping.State != topicStateActive || mapping.WorkID != "work-a" || mapping.ThreadID != "thread-1" {
		t.Fatalf("mapping=%+v", mapping)
	}

	// Restart retains the durable mapping and never re-creates the Topic.
	reopened := newTestManager(t, root, owner, api)
	if err := reopened.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := reopened.store.snapshot()
	if len(state.Topics) != 1 || len(api.createdTopics) != 1 {
		t.Fatalf("restart re-created topic: topics=%+v creates=%d", state.Topics, len(api.createdTopics))
	}

	// Completion marks and closes the lifecycle; a still-viable Session reopens
	// on new activity (new input is admitted under the reopen policy).
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running",
		TurnID: "turn-1", TurnStatus: "done", TurnSummary: "Finished A"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state = manager.store.snapshot()
	if state.Topics[0].State != topicStateCompleted {
		t.Fatalf("completion state=%s", state.Topics[0].State)
	}
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	markerSent := false
	for _, sent := range api.sent {
		if sent.MessageThreadID == mapping.MessageThreadID && strings.Contains(sent.Text, "Session completed") {
			markerSent = true
		}
	}
	if !markerSent {
		t.Fatalf("completion marker missing from topic: %+v", api.sent)
	}
	// The private-chat policy never issues closeForumTopic.
	if len(api.closingTopics) != 0 || len(api.deletedTopics) != 0 {
		t.Fatalf("private chat close/delete issued: closings=%d deletes=%d", len(api.closingTopics), len(api.deletedTopics))
	}

	// Reopen policy: completed + still-viable Session accepts exact input and
	// returns to active when a new turn begins.
	owner.sessionDisposition = brain.ExternalInputAccepted
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(50, mapping.MessageThreadID, "continue")); err != nil {
		t.Fatal(err)
	}
	owner.sessionDisposition = ""
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running",
		TurnID: "turn-2", TurnStatus: "running"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state = manager.store.snapshot()
	if state.Topics[0].State != topicStateActive {
		t.Fatalf("reopen state=%s", state.Topics[0].State)
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if len(owner.sessionBodies) != 1 || owner.sessionBodies[0] != "continue" {
		t.Fatalf("reopen input not admitted: %v", owner.sessionBodies)
	}
}

func TestTopicCreateDefiniteRejectionRetriesButAmbiguousNeverRepeats(t *testing.T) {
	manager, _, api, _ := topicFixture(t)
	// Definite rejection proves no Topic exists: the create op retries.
	api.nextTopic = ForumTopic{MessageThreadID: 0}
	api.topicErr = &APIError{Code: 400, description: "Bad Request: title empty"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOpOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.TopicOps[0].State != "pending" {
		t.Fatalf("definite rejection did not stay retryable: %+v", state.TopicOps[0])
	}
	manager.now = func() time.Time { return time.Date(2026, 8, 24, 12, 0, 2, 0, time.UTC) }
	if err := manager.deliverTopicOpOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.createdTopics) != 2 || len(manager.store.snapshot().Topics) != 1 {
		t.Fatalf("rejection retry unresolved: creates=%d", len(api.createdTopics))
	}

	// Transport-indeterminate create becomes ambiguous and is never retried.
	owner2 := &fakeBrain{threadID: "thread-1", sessions: []brain.AgentRef{{ID: "sess-b", Name: "Session B", Delegated: true, Status: "running"}}}
	owner2.projections = map[string]brain.SessionProjection{"sess-b": {SessionID: "sess-b", Present: true, Label: "Session B", Status: "running"}}
	owner2.sessionWork = map[string]brain.Work{"sess-b": {ID: "work-b", SourceThreadID: "thread-1"}}
	manager2, err := NewManagerWithOptions(t.TempDir(), owner2, Options{API: api, Now: manager.now, PollTimeout: 1, Backoff: time.Millisecond,
		TypingInterval: 10 * time.Millisecond, TypingDeadline: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager2.Configure(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	bindOwner(t, manager2, 1, 10, 10)
	api.topicErr = errors.New("connection closed after request write")
	if err := manager2.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager2.deliverTopicOpOne(context.Background(), "token"); err == nil {
		t.Fatal("ambiguous create returned success")
	}
	state = manager2.store.snapshot()
	if state.TopicOps[0].State != "ambiguous" || len(state.Topics) != 0 {
		t.Fatalf("ambiguous create: %+v", state.TopicOps)
	}
	creates := len(api.createdTopics)
	// Re-running the reconcile must not enqueue or attempt a duplicate create.
	if err := manager2.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager2.deliverTopicOps(context.Background(), "token", 8); err != nil && !errors.As(err, new(*APIError)) {
		// No deliverable op exists: deliverTopicOps returns nil.
		t.Fatal(err)
	}
	if len(api.createdTopics) != creates {
		t.Fatalf("ambiguous create retried: %d -> %d", creates, len(api.createdTopics))
	}
	status := manager2.Status()
	if status.TopicAmbiguousOps != 1 || status.State != StateDegraded {
		t.Fatalf("status=%+v", status)
	}
}

func TestTopicRenameLabelAndCapabilityDisabled(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	owner.sessions = []brain.AgentRef{{ID: "sess-a", Name: "Session A renamed (brain-agent-session-a:@2)", Delegated: true, Status: "running"}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.Topics[0].Label != "Session A renamed" {
		t.Fatalf("rename did not land: %+v", state.Topics[0])
	}
	if len(api.editedTopics) != 1 || api.editedTopics[0].MessageThreadID != threadID || api.editedTopics[0].Name != "Session A renamed" {
		t.Fatalf("rename api=%+v", api.editedTopics)
	}

	// Capability disabled: no create ops are enqueued and status is actionable.
	api.bot = User{ID: 7001, IsBot: true, Username: "zen_test_bot", Topics: false}
	if err := manager.refreshTopicCapability(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	owner.sessions = append(owner.sessions, brain.AgentRef{ID: "sess-b", Name: "Session B", Delegated: true, Status: "running"})
	owner.sessionWork["sess-b"] = brain.Work{ID: "work-b", SourceThreadID: "thread-1"}
	before := len(api.createdTopics)
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.createdTopics) != before {
		t.Fatalf("create attempted with capability disabled")
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.TopicsAvailable || status.TopicMappings != 1 || status.State != StateDegraded {
		t.Fatalf("status=%+v", status)
	}
	if !strings.Contains(manager.Status().LastError, "@BotFather") {
		t.Fatalf("capability error not actionable: %q", manager.Status().LastError)
	}
}

func TestTopicLabelMatchesSessionListTitle(t *testing.T) {
	tests := []struct {
		name    string
		session brain.AgentRef
		want    string
	}{
		{
			name:    "canonical identity suffix is hidden",
			session: brain.AgentRef{ID: "session-1", Name: "telegram-topic-smoke (brain-agent-telegram-topic-smoke-1787669753941544264:@42)"},
			want:    "telegram-topic-smoke",
		},
		{
			name:    "plain title is preserved",
			session: brain.AgentRef{ID: "session-2", Name: "Rates research"},
			want:    "Rates research",
		},
		{
			name:    "missing title falls back to session identity",
			session: brain.AgentRef{ID: "session-3"},
			want:    "session-3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := topicLabel(test.session); got != test.want {
				t.Fatalf("topicLabel()=%q want %q", got, test.want)
			}
		})
	}
}

func TestTopicCloseReopenDeleteOperationMachine(t *testing.T) {
	root := t.TempDir()
	owner := &fakeBrain{threadID: "thread-1"}
	api := &fakeAPI{bot: User{ID: 7001, IsBot: true, Username: "zen_test_bot", Topics: true}}
	manager := newTestManager(t, root, owner, api)
	if _, err := manager.Configure(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	bindOwner(t, manager, 1, 10, 10)
	if err := manager.store.mutate(func(state *durableState) error {
		state.Topics = []topicMapping{{SessionID: "sess-a", ThreadID: "thread-1", ChatID: 10,
			MessageThreadID: 42, Label: "Session A", State: topicStateActive, CreatedAt: manager.now(), UpdatedAt: manager.now()}}
		enqueueTopicOp(state, topicOpRecord{ID: "op:close", Kind: topicOpClose, SessionID: "sess-a", MessageThreadID: 42, CreatedAt: manager.now()})
		enqueueTopicOp(state, topicOpRecord{ID: "op:reopen", Kind: topicOpReopen, SessionID: "sess-a", MessageThreadID: 42, CreatedAt: manager.now()})
		enqueueTopicOp(state, topicOpRecord{ID: "op:delete", Kind: topicOpDelete, SessionID: "sess-a", MessageThreadID: 42, CreatedAt: manager.now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.closingTopics) != 1 || len(api.reopenedTopics) != 1 || len(api.deletedTopics) != 1 {
		t.Fatalf("topic ops not delivered: %+v %+v %+v", api.closingTopics, api.reopenedTopics, api.deletedTopics)
	}
	state := manager.store.snapshot()
	for _, op := range state.TopicOps {
		if op.State != "sent" {
			t.Fatalf("op not sent: %+v", op)
		}
	}
	// A transport-indeterminate close becomes ambiguous.
	api.topicOpErrs = map[string]error{"close": errors.New("connection closed after request write")}
	if err := manager.store.mutate(func(state *durableState) error {
		enqueueTopicOp(state, topicOpRecord{ID: "op:close2", Kind: topicOpClose, SessionID: "sess-a", MessageThreadID: 42, CreatedAt: manager.now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverTopicOpOne(context.Background(), "token"); err == nil {
		t.Fatal("ambiguous close succeeded")
	}
	state = manager.store.snapshot()
	found := false
	for _, op := range state.TopicOps {
		if op.ID == "op:close2" && op.State == "ambiguous" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ambiguous close not recorded: %+v", state.TopicOps)
	}
}

func TestDisabledAndRevokedConnectionStopsTopicDeliveryAndInput(t *testing.T) {
	manager, _, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	if err := manager.Disable(); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.State != StateDisabled || status.TopicMappings != 1 {
		t.Fatalf("disable status=%+v", status)
	}
	// Run with a disabled connection never polls.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx) }()
	select {
	case <-done:
		t.Fatal("Run exited on healthy cancel path unexpectedly")
	case <-time.After(75 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	api.mu.Lock()
	polls := api.pollCount
	api.mu.Unlock()
	if polls != 0 {
		t.Fatalf("disabled connection polled %d times", polls)
	}

	// Revoke clears topic state entirely.
	if err := manager.RevokeOwner(); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.TopicMappings != 0 || status.State != StateDisabled {
		t.Fatalf("revoke status=%+v", status)
	}
	if len(manager.store.snapshot().Topics) != 0 || len(manager.store.snapshot().TopicOps) != 0 {
		t.Fatal("revoke retained topic state")
	}
}

func TestSessionTopicDuplicateUpdateAndRestartNoReplay(t *testing.T) {
	manager, owner, api, root := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	update := topicUpdate(60, threadID, "once only")
	if err := manager.handleUpdate(context.Background(), "token", update); err != nil {
		t.Fatal(err)
	}
	// Same transport update again before offset acknowledgement.
	if err := manager.handleUpdate(context.Background(), "token", update); err != nil {
		t.Fatal(err)
	}
	if len(owner.sessionBodies) != 1 {
		t.Fatalf("duplicate update admitted twice: %v", owner.sessionBodies)
	}

	// Restart: mapping and checkpoint survive; the same update is no longer
	// re-admitted (durable disposition).
	reopened := newTestManager(t, root, owner, api)
	state := reopened.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].MessageThreadID != threadID {
		t.Fatalf("restart lost mapping: %+v", state.Topics)
	}
	if err := reopened.handleUpdate(context.Background(), "token", update); err != nil {
		t.Fatal(err)
	}
	if len(owner.sessionBodies) != 1 {
		t.Fatalf("restart replayed accepted input: %v", owner.sessionBodies)
	}
}

func TestSessionTopicUncertainAdmissionIsNeverReplayed(t *testing.T) {
	manager, owner, api, root := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	owner.sessionDisposition = brain.ExternalInputUncertain
	owner.sessionDispositionErr = errors.New("provider admission unknown")
	update := topicUpdate(70, threadID, "maybe lost")
	if err := manager.handleUpdate(context.Background(), "token", update); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.Processed["70"].Disposition != "session_uncertain" {
		t.Fatalf("disposition=%+v", state.Processed["70"])
	}
	// The daemon restarts; the durable update disposition prevents the manager
	// from ever consulting the watcher again for this update, so the unknown
	// admission cannot be re-submitted or upgraded.
	owner.sessionDisposition = brain.ExternalInputAccepted
	owner.sessionDispositionErr = nil
	reopened := newTestManager(t, root, owner, api)
	if err := reopened.handleUpdate(context.Background(), "token", topicUpdate(70, threadID, "maybe lost")); err != nil {
		t.Fatal(err)
	}
	state = reopened.store.snapshot()
	if state.Processed["70"].Disposition != "session_uncertain" {
		t.Fatalf("restart disposition=%+v", state.Processed["70"])
	}
	owner.mu.Lock()
	bodies := append([]string(nil), owner.sessionBodies...)
	owner.mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("uncertain update resubmitted: %v", bodies)
	}
	// The no-replay reply itself is delivered once into the mapped Topic only.
	if err := reopened.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	for _, sent := range api.sent {
		if sent.MessageThreadID == threadID && strings.Contains(sent.Text, "not replayed") {
			return
		}
	}
	t.Fatal("uncertainty ack missing from mapped topic")
}

func TestSessionTopicOutputAndLifecycleOnlyInOwnTopic(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	// Brain assistant output stays in General while Session output targets the topic.
	owner.timeline = []brain.TimelineItem{{
		ID: "brain-1", ThreadID: "thread-1", SessionID: "host", Role: "assistant",
		Kind: "assistant_message", Body: "Brain answer", CreatedAt: manager.now().Add(time.Second),
	}}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running",
		TurnStatus: "running", Assistant: []brain.SessionAssistantItem{{ID: "event-1", Body: "Session answer", CreatedAt: manager.now().Add(time.Second)}}}
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	brainDelivered, sessionDelivered := false, false
	for _, sent := range api.sent {
		switch {
		case strings.Contains(sent.Text, "Brain answer"):
			if sent.MessageThreadID != 0 {
				t.Fatalf("Brain answer escaped General into topic %d", sent.MessageThreadID)
			}
			brainDelivered = true
		case strings.Contains(sent.Text, "Session answer"):
			if sent.MessageThreadID != threadID {
				t.Fatalf("Session answer escaped topic %d into %d", threadID, sent.MessageThreadID)
			}
			sessionDelivered = true
		}
	}
	if !brainDelivered || !sessionDelivered {
		t.Fatalf("delivery missing: brain=%v session=%v", brainDelivered, sessionDelivered)
	}
}

func TestSessionTopicStaleMappingSurvivesRestartAndFailsClosed(t *testing.T) {
	manager, owner, api, root := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	owner.sessions = nil
	owner.projections = map[string]brain.SessionProjection{"sess-a": {SessionID: "sess-a", Present: false}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.Topics[0].State != topicStateStale {
		t.Fatalf("stale state=%s", state.Topics[0].State)
	}

	reopened := newTestManager(t, root, owner, api)
	state = reopened.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("restart lost stale mapping: %+v", state.Topics)
	}
	if err := reopened.handleUpdate(context.Background(), "token", topicUpdate(80, threadID, "anyone?")); err != nil {
		t.Fatal(err)
	}
	if state := reopened.store.snapshot(); state.Processed["80"].Disposition != "topic_stale" {
		t.Fatalf("stale input disposition=%+v", state.Processed["80"])
	}
	if len(owner.sessionBodies) != 0 {
		t.Fatalf("stale topic admitted input: %v", owner.sessionBodies)
	}
	// Stale mappings are never re-created automatically.
	if err := reopened.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state = reopened.store.snapshot()
	if len(state.Topics) != 1 {
		t.Fatalf("stale mapping re-created: %+v", state.Topics)
	}
}

func TestReapedSessionDeletesTopicAndLocalProjectionState(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	if err := manager.store.mutate(func(state *durableState) error {
		state.Outbox = append(state.Outbox, outboxRecord{
			ID: "topic-row", TopicKey: "topic:msg:sess-a:event-1:0", Text: "pending",
			MessageThreadID: threadID, State: "pending", CreatedAt: manager.now(),
		})
		state.TopicProjection["topic:msg:sess-a:event-1:0"] = "digest"
		state.TopicProjection["topic:mark:sess-a:turn-1:done"] = "digest"
		state.TopicMessages["topic:msg:sess-a:event-1:0"] = 99
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	owner.sessions = nil
	owner.projections = map[string]brain.SessionProjection{"sess-a": {SessionID: "sess-a", Present: false}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].State != topicStateStale {
		t.Fatalf("mapping did not fail closed before delete: %+v", state.Topics)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.deletedTopics) != 1 || api.deletedTopics[0].MessageThreadID != threadID {
		t.Fatalf("delete operations=%+v", api.deletedTopics)
	}
	state = manager.store.snapshot()
	if len(state.Topics) != 0 || len(state.TopicOps) != 0 {
		t.Fatalf("deleted topic retained routing state: topics=%+v ops=%+v", state.Topics, state.TopicOps)
	}
	for _, row := range state.Outbox {
		if row.MessageThreadID == threadID {
			t.Fatalf("deleted topic retained outbox row: %+v", row)
		}
	}
	if len(state.TopicProjection) != 0 || len(state.TopicMessages) != 0 {
		t.Fatalf("deleted topic retained checkpoints: projection=%+v messages=%+v", state.TopicProjection, state.TopicMessages)
	}
}

func TestHistoricalStaleMappingWithoutDeleteOpIsReconciled(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID
	if err := manager.store.mutate(func(state *durableState) error {
		state.Topics[0].State = topicStateStale
		ops := state.TopicOps[:0]
		for _, op := range state.TopicOps {
			if op.SessionID != "sess-a" || op.Kind == topicOpCreate {
				ops = append(ops, op)
			}
		}
		state.TopicOps = ops
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	owner.sessions = nil
	owner.projections = map[string]brain.SessionProjection{"sess-a": {SessionID: "sess-a", Present: false}}

	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	foundDelete := false
	for _, op := range state.TopicOps {
		if op.Kind == topicOpDelete && op.SessionID == "sess-a" && op.MessageThreadID == threadID && op.State == "pending" {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Fatalf("historical stale mapping did not get a delete operation: %+v", state.TopicOps)
	}
	if err := manager.deliverTopicOps(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	if len(api.deletedTopics) != 1 || api.deletedTopics[0].MessageThreadID != threadID {
		t.Fatalf("delete operations=%+v", api.deletedTopics)
	}
	if state := manager.store.snapshot(); len(state.Topics) != 0 {
		t.Fatalf("deleted historical mapping remained local: %+v", state.Topics)
	}
}

func TestTopicMappingLimitDegradesWithoutAbortingProjection(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	createTopicFor(t, manager)
	mapping := manager.store.snapshot().Topics[0]

	// Saturate the durable mapping budget.
	prefill := make([]topicMapping, 0, maxTopicMappings-1)
	for index := 0; index < maxTopicMappings-1; index++ {
		prefill = append(prefill, topicMapping{SessionID: fmt.Sprintf("filled-%d", index), ChatID: 10,
			MessageThreadID: 500 + int64(index), Label: "filled", State: topicStateStale,
			CreatedAt: manager.now(), UpdatedAt: manager.now()})
	}
	if err := manager.store.mutate(func(state *durableState) error {
		state.Topics = append(prefill, state.Topics...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A new Session cannot get a Topic but the existing mapping still projects.
	owner.sessions = append(owner.sessions, brain.AgentRef{ID: "sess-over", Name: "Over", Delegated: true, Status: "running"})
	owner.sessionWork["sess-over"] = brain.Work{ID: "work-over", SourceThreadID: "thread-1"}
	owner.projections["sess-over"] = brain.SessionProjection{SessionID: "sess-over", Present: true, Label: "Over", Status: "running"}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running",
		TurnStatus: "running", Assistant: []brain.SessionAssistantItem{{ID: "e-x", Body: "still projected"}}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.State != StateDegraded || !strings.Contains(status.LastError, "mapping limit") {
		t.Fatalf("limit status=%+v", status)
	}
	// The existing mapping still projected: its output row is enqueued even
	// though the new Session cannot get a Topic.
	state := manager.store.snapshot()
	found := false
	for _, row := range state.Outbox {
		if row.MessageThreadID == mapping.MessageThreadID && strings.Contains(row.Text, "still projected") {
			found = true
		}
	}
	if !found {
		t.Fatalf("existing mapping projection aborted by limit: %+v", state.Outbox)
	}
}

func TestTopicStateStateAndFileExcludesSecrets(t *testing.T) {
	manager, _, _, root := topicFixture(t)
	createTopicFor(t, manager)
	stateBytes, err := os.ReadFile(filepath.Join(root, "telegram", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateBytes), "test-token") {
		t.Fatal("topic state leaked bot token")
	}
}

func TestSessionTopicCommandsAreRouteLocal(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(90, threadID, "/status")); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(91, threadID, "/help")); err != nil {
		t.Fatal(err)
	}
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(92, threadID, "/new")); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}

	owner.mu.Lock()
	defer owner.mu.Unlock()
	if len(owner.sessionBodies) != 0 || len(owner.receipts) != 0 || owner.newChats != 0 {
		t.Fatalf("commands admitted: session=%v brain=%v newChats=%d", owner.sessionBodies, owner.receipts, owner.newChats)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	statusSent, newSent, helpSent := false, false, false
	for _, sent := range api.sent {
		if sent.MessageThreadID == 0 {
			continue // binding message is General-scoped
		}
		if sent.MessageThreadID != threadID {
			t.Fatalf("command reply escaped topic: %+v", sent)
		}
		switch {
		case strings.Contains(sent.Text, "Turn: running") && strings.Contains(sent.Text, "Work: running"):
			statusSent = true
		case strings.Contains(sent.Text, "Send text to this Session"):
			helpSent = true
		case strings.Contains(sent.Text, "General topic"):
			newSent = true
		}
	}
	if !statusSent || !helpSent || !newSent {
		t.Fatalf("command replies missing: status=%v help=%v new=%v", statusSent, helpSent, newSent)
	}
}

func TestStaleMappingRevivesWhenSessionReappears(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	owner.sessions = nil
	owner.projections = map[string]brain.SessionProjection{"sess-a": {SessionID: "sess-a", Present: false}}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if state := manager.store.snapshot(); state.Topics[0].State != topicStateStale {
		t.Fatalf("state=%s", state.Topics[0].State)
	}

	// Same exact Session identity is user-visible again: the mapping revives.
	owner.sessions = []brain.AgentRef{{ID: "sess-a", Name: "Session A", Delegated: true, Status: "running"}}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnStatus: "running"}
	if err := manager.projectSessionTopics(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.Topics[0].State != topicStateActive {
		t.Fatalf("revive state=%s", state.Topics[0].State)
	}
	if err := manager.deliverPending(context.Background(), "token", 8); err != nil {
		t.Fatal(err)
	}
	revivalSent := false
	for _, sent := range api.sent {
		if sent.MessageThreadID == threadID && strings.Contains(sent.Text, "available again") {
			revivalSent = true
		}
	}
	if !revivalSent {
		t.Fatalf("revival marker missing: %+v", api.sent)
	}
}

func TestSessionTopicTypingUsesTopicThreadAndStopsOnTerminal(t *testing.T) {
	manager, owner, api, _ := topicFixture(t)
	createTopicFor(t, manager)
	threadID := manager.store.snapshot().Topics[0].MessageThreadID

	owner.sessionDisposition = brain.ExternalInputAccepted
	if err := manager.handleUpdate(context.Background(), "token", topicUpdate(100, threadID, "work on it")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if api.actionCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	api.mu.Lock()
	if len(api.actions) == 0 {
		api.mu.Unlock()
		t.Fatal("no topic chat action sent")
	}
	topicActions := 0
	for _, action := range api.actions {
		if action.MessageThreadID == threadID && action.Action == "typing" {
			topicActions++
		}
	}
	api.mu.Unlock()
	if topicActions == 0 {
		t.Fatal("typing was not topic-scoped")
	}

	// Terminal turn stops typing.
	owner.mu.Lock()
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Status: "running", TurnID: "turn-1", TurnStatus: "done"}
	owner.mu.Unlock()
	before := api.actionCount()
	time.Sleep(60 * time.Millisecond)
	if after := api.actionCount(); after > before+1 {
		t.Fatalf("typing continued past terminal turn: %d -> %d", before, after)
	}
}
