package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

func TestCallbackSelectionSurvivesReorderRestartAndDuplicateID(t *testing.T) {
	m, owner, api, root := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	if err := m.store.mutate(func(s *durableState) error { s.TopicsAvailable = false; return nil }); err != nil {
		t.Fatal(err)
	}
	owner.sessions = []brain.WorkerRef{{ID: "a", Name: "Same label", Delegated: true}, {ID: "b", Name: "Same label", Delegated: true}}
	if err := m.handleUpdate(t.Context(), "token", topicUpdate(2, 0, "/sessions")); err != nil {
		t.Fatal(err)
	}
	owner.sessions[0], owner.sessions[1] = owner.sessions[1], owner.sessions[0]
	reopened := newTestManager(t, root, owner, api)
	if err := reopened.handleUpdate(t.Context(), "token", topicUpdate(3, 0, "/use 1")); err != nil {
		t.Fatal(err)
	}
	if reopened.store.snapshot().FallbackSessionID != "a" {
		t.Fatal("number rebound to changed inventory order")
	}
	callback := func(id int64, from int64) Update {
		return Update{UpdateID: id, CallbackQuery: &CallbackQuery{ID: "same-callback", From: &User{ID: from}, Message: &Message{Chat: Chat{ID: 10, Type: "private"}}, Data: "session:" + digestText("b")}}
	}
	if err := reopened.handleUpdate(t.Context(), "token", callback(4, 10)); err != nil {
		t.Fatal(err)
	}
	if err := reopened.handleUpdate(t.Context(), "token", topicUpdate(5, 0, "/brain")); err != nil {
		t.Fatal(err)
	}
	if err := reopened.handleUpdate(t.Context(), "token", callback(6, 10)); err != nil {
		t.Fatal(err)
	}
	state := reopened.store.snapshot()
	if state.FallbackSessionID != "" || state.Processed["6"].Disposition != "callback_duplicate" || state.NextOffset != 7 {
		t.Fatalf("duplicate callback changed recipient/cursor: %+v", state)
	}
	wrong := callback(7, 11)
	wrong.CallbackQuery.ID = "foreign"
	if err := reopened.handleUpdate(t.Context(), "token", wrong); err != nil {
		t.Fatal(err)
	}
	if reopened.store.snapshot().FallbackSessionID != "" {
		t.Fatal("wrong owner selected recipient")
	}
}

func TestStaleFallbackAndReplyNeverFallThroughToBrain(t *testing.T) {
	m, owner, _, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	if err := m.store.mutate(func(s *durableState) error {
		s.TopicsAvailable = false
		s.FallbackSessionID = "gone"
		s.ReplySessions[99] = "gone"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	owner.projections = map[string]brain.SessionProjection{"gone": {SessionID: "gone", Present: false}}
	for _, id := range []int64{2, 3} {
		if err := m.handleUpdate(t.Context(), "token", topicUpdate(id, 0, "continue")); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.handleUpdate(t.Context(), "token", topicUpdate(4, 0, "/brain")); err != nil {
		t.Fatal(err)
	}
	update := topicUpdate(5, 0, "reply to unavailable Session")
	update.Message.ReplyToMessage = &Message{MessageID: 99, From: &User{ID: 7001, IsBot: true}, Text: "Session result"}
	if err := m.handleUpdate(t.Context(), "token", update); err != nil {
		t.Fatal(err)
	}
	if len(owner.bodies) != 0 || len(owner.sessionBodies) != 0 {
		t.Fatal("unavailable destination leaked into live recipient")
	}
	if m.store.snapshot().Processed["5"].Disposition != "fallback_stale" {
		t.Fatal("missing unavailable reply disposition")
	}
}

func TestNativeBrainTopicCreationAndDestinationSurviveRestart(t *testing.T) {
	m, owner, api, root := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	api.bot.UserTopics = true
	if err := m.refreshTopicCapability(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	if err := m.deliverTopicOpOne(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	topic := m.store.snapshot().BrainTopicID
	if topic == 0 || len(m.store.snapshot().Topics) != 0 {
		t.Fatal("Brain topic confused with Session mapping")
	}
	reopened := newTestManager(t, root, owner, api)
	if err := reopened.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	if reopened.hasDeliverableTopicOp() {
		t.Fatal("restart created duplicate Brain topic")
	}
	owner.timeline = []brain.TimelineItem{{ID: "partial", Kind: "assistant_message", Body: "First", CreatedAt: m.now().Add(time.Second)}}
	if err := reopened.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	for reopened.hasDeliverableOutbox() {
		if err := reopened.deliverOne(t.Context(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	owner.timeline[0].Body = "First and final"
	if err := reopened.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.deliverOne(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 1 || api.edited[0].Text != "First and final" {
		t.Fatal("Brain partial result froze at first chunk")
	}
	for _, row := range reopened.store.snapshot().Outbox {
		if row.CanonicalID == "partial" && row.MessageThreadID != topic {
			t.Fatal("Brain output escaped its native topic")
		}
	}
}

func TestUnobservedTopicIsNotAdoptedEvenWithUserTopicCreationEnabled(t *testing.T) {
	m, owner, _, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	if err := m.store.mutate(func(s *durableState) error { s.UsersCreateTopics = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.handleUpdate(t.Context(), "token", topicUpdate(2, 999, "stale message")); err != nil {
		t.Fatal(err)
	}
	if len(owner.bodies) != 0 || m.store.snapshot().Processed["2"].Disposition != "topic_unknown" {
		t.Fatal("unobserved topic adopted as Brain")
	}
}

func TestReconnectSkipsPreBoundarySessionOutputAndKeepsIDs(t *testing.T) {
	m, owner, _, _ := topicFixture(t)
	createTopicFor(t, m)
	before := m.store.snapshot()
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	now := m.now().Add(time.Hour)
	m.now = func() time.Time { return now }
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Assistant: []brain.SessionAssistantItem{{ID: "old", Body: "history", CreatedAt: now.Add(-time.Minute)}, {ID: "undated", Body: "legacy"}, {ID: "fresh", Body: "future", CreatedAt: now.Add(time.Second)}}}
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := m.projectSessionTopics(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	after := m.store.snapshot()
	if after.OwnerID != before.OwnerID || after.ChatID != before.ChatID || after.NextOffset != before.NextOffset || after.Topics[0].MessageThreadID != before.Topics[0].MessageThreadID {
		t.Fatal("reconnect changed binding/cursor/topic")
	}
	var count int
	for _, row := range after.Outbox {
		if row.CanonicalID != "" {
			count++
			if row.CanonicalID != "session:sess-a:fresh" {
				t.Fatalf("replayed history: %s", row.CanonicalID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("new output count=%d", count)
	}
}

func TestFloodWaitBlocksOtherRowsInSamePrivateChat(t *testing.T) {
	m, _, api, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	m.enqueueText("second", "Second", 0)
	api.nextSendErr = &APIError{Code: 429, Retryable: true, RetryAfter: time.Minute}
	if err := m.deliverOne(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	if m.hasDeliverableOutbox() {
		t.Fatal("another row bypassed per-chat flood wait")
	}
	if err := m.deliverOne(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 0 {
		t.Fatal("sent while Telegram retry_after is active")
	}
}

func TestStoreMutationFailureDoesNotExposeUndurableSelection(t *testing.T) {
	m, _, _, _ := configuredManager(t)
	m.store.statePath = m.store.dir
	err := m.store.mutate(func(s *durableState) error { s.FallbackSessionID = "never-saved"; return nil })
	if err == nil || m.store.snapshot().FallbackSessionID != "" {
		t.Fatal("failed store write became routing authority")
	}
}

func TestCallbackDedupeJournalIsBounded(t *testing.T) {
	s := newDurableState()
	ensureDurableMaps(&s)
	for id := int64(1); id < 1000; id++ {
		s.CallbackIDs[fmt.Sprint(id)] = id
		s.NextOffset = id + 1
		trimProcessedUpdates(&s)
	}
	if len(s.CallbackIDs) > maxProcessedUpdate {
		t.Fatal("callback journal grew unbounded")
	}
}

func TestPrivateFallbackDoesNotEditAnOldNativeTopicMessage(t *testing.T) {
	m, owner, api, _ := topicFixture(t)
	createTopicFor(t, m)
	now := m.now()
	if err := m.store.mutate(func(s *durableState) error {
		s.TopicsAvailable = false
		s.FallbackSessionID = "sess-a"
		s.FallbackStartedAt = now
		s.TopicMessages["topic:msg:sess-a:result:0"] = 99
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	owner.projections["sess-a"] = brain.SessionProjection{SessionID: "sess-a", Present: true, Label: "Session A", Assistant: []brain.SessionAssistantItem{{ID: "result", Body: "Fallback reply", CreatedAt: now.Add(time.Second)}}}
	if err := m.projectFallbackSession(); err != nil {
		t.Fatal(err)
	}
	if err := m.projectSessionTopics(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	for m.hasDeliverableOutbox() {
		if err := m.deliverOne(t.Context(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	if len(api.edited) != 0 {
		t.Fatal("fallback edited an old native topic")
	}
	found := false
	for _, message := range api.sent {
		if strings.Contains(message.Text, "Fallback reply") {
			found = true
			if message.MessageThreadID != 0 {
				t.Fatal("fallback escaped private chat")
			}
		}
	}
	if !found {
		t.Fatal("private reply missing")
	}
}

func TestCorrectedLocalLinkRetriesOnlyDefiniteFailureNotAmbiguity(t *testing.T) {
	for _, outcome := range []string{"failed", "ambiguous"} {
		t.Run(outcome, func(t *testing.T) {
			s := newDurableState()
			ensureDurableMaps(&s)
			s.Outbox = []outboxRecord{{ID: "link", Kind: "send", TopicKey: "link", State: outcome, Variant: formattedVariant, Text: "source", Entities: []MessageEntity{{Type: "text_link", Offset: 0, Length: 6, URL: "/workspace/source.go"}}}}
			s.TopicProjection["link"] = "old-digest"
			content := renderMarkdown("[source](/workspace/source.go)")
			candidate := sessionOutputCandidate{Key: "link", Content: content, Digest: digestRichText(content)}
			if !coalesceTopicRow(&s, candidate, 42, time.Now()) {
				t.Fatal("row not coalesced")
			}
			if outcome == "failed" && (s.Outbox[0].State != "pending" || s.Outbox[0].ID != "link" || len(s.Outbox[0].Entities) != 0) {
				t.Fatalf("definite recovery=%+v", s.Outbox[0])
			}
			if outcome == "ambiguous" && (s.Outbox[0].State != "ambiguous" || s.Outbox[0].Text != "source") {
				t.Fatal("ambiguous row was replayed")
			}
		})
	}
}

func TestStartAndTopicServiceMessagesNeverReachProvider(t *testing.T) {
	m, owner, api, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	api.bot.UserTopics = true
	if err := m.refreshTopicCapability(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	created := topicUpdate(2, 222, "")
	created.Message.ForumTopicCreated = &ForumTopicCreated{Name: "Brain"}
	for _, update := range []Update{created, topicUpdate(3, 222, "/start"), topicUpdate(4, 222, "/start repeated")} {
		if err := m.handleUpdate(t.Context(), "token", update); err != nil {
			t.Fatal(err)
		}
	}
	if owner.newChats != 0 || len(owner.bodies) != 0 {
		t.Fatal("start or service message reset/submitted Brain")
	}
	for _, row := range m.store.snapshot().Outbox {
		if strings.HasPrefix(row.ID, "command:") && row.MessageThreadID != 222 {
			t.Fatal("command response escaped native Brain topic")
		}
	}
}

type rotatingPollAPI struct {
	*fakeAPI
	offsets chan int64
	release chan struct{}
	calls   int
	allowed []string
}

func (a *rotatingPollAPI) GetUpdates(ctx context.Context, _ string, offset int64, _ int, allowed []string) ([]Update, error) {
	a.calls++
	if a.calls == 1 {
		a.allowed = append([]string(nil), allowed...)
	}
	a.offsets <- offset
	if a.calls == 1 {
		select {
		case <-a.release:
			return []Update{topicUpdate(9999, 0, "old bot input")}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestBotRotationRejectsOldPollWithoutAdvancingNewCursor(t *testing.T) {
	m, owner, api, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	poller := &rotatingPollAPI{fakeAPI: api, offsets: make(chan int64, 2), release: make(chan struct{})}
	m.api = poller
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	select {
	case <-poller.offsets:
	case <-time.After(time.Second):
		t.Fatal("first poll missing")
	}
	api.bot = User{ID: 8002, IsBot: true, Username: "replacement_bot"}
	if _, err := m.Configure(t.Context(), "replacement-fixture-token"); err != nil {
		t.Fatal(err)
	}
	close(poller.release)
	select {
	case offset := <-poller.offsets:
		if offset != 0 {
			t.Fatalf("new bot inherited old cursor: %d", offset)
		}
	case <-time.After(time.Second):
		t.Fatal("new poll missing")
	}
	cancel()
	<-done
	if len(owner.bodies) != 0 || m.store.snapshot().NextOffset != 0 {
		t.Fatal("old bot update entered new binding")
	}
	if strings.Join(poller.allowed, ",") != "message,callback_query,message_reaction" {
		t.Fatalf("callback updates not requested: %v", poller.allowed)
	}
}
