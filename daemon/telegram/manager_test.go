package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

type fakeBrain struct {
	mu          sync.Mutex
	receipts    []string
	bodies      []string
	disposition brain.ExternalInputDisposition
	threadID    string
	timeline    []brain.TimelineItem
	newChats    int
}

func (f *fakeBrain) SubmitExternalUserInput(receipt, body string) (brain.ExternalInputDisposition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receipts = append(f.receipts, receipt)
	f.bodies = append(f.bodies, body)
	if f.disposition == "" {
		return brain.ExternalInputAccepted, nil
	}
	return f.disposition, nil
}

func (f *fakeBrain) ChatThreadID() (string, error) {
	return f.threadID, nil
}

func (f *fakeBrain) ThreadTimeline(string, int) ([]brain.TimelineItem, error) {
	return append([]brain.TimelineItem(nil), f.timeline...), nil
}

func (f *fakeBrain) NewChat() (brain.Snapshot, error) {
	f.newChats++
	return brain.Snapshot{ChatThreadID: "new-thread"}, nil
}

type fakeAPI struct {
	mu          sync.Mutex
	bot         User
	webhook     WebhookInfo
	updates     []Update
	sent        []SendRequest
	edited      []EditRequest
	nextSendErr error
	nextEditErr error
	nextID      int64
	blockPoll   bool
	pollStarted chan struct{}
}

func (f *fakeAPI) GetMe(context.Context, string) (User, error) {
	return f.bot, nil
}

func (f *fakeAPI) GetWebhookInfo(context.Context, string) (WebhookInfo, error) {
	return f.webhook, nil
}

func (f *fakeAPI) GetUpdates(ctx context.Context, _ string, offset int64, _ int, _ []string) ([]Update, error) {
	if f.blockPoll {
		if f.pollStarted != nil {
			select {
			case <-f.pollStarted:
			default:
				close(f.pollStarted)
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Update
	for _, update := range f.updates {
		if update.UpdateID >= offset {
			out = append(out, update)
		}
	}
	return out, nil
}

func (f *fakeAPI) SendMessage(_ context.Context, _ string, request SendRequest) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextSendErr != nil {
		err := f.nextSendErr
		f.nextSendErr = nil
		return Message{}, err
	}
	f.nextID++
	f.sent = append(f.sent, request)
	return Message{MessageID: f.nextID, Chat: Chat{ID: request.ChatID, Type: "private"}}, nil
}

func (f *fakeAPI) EditMessage(_ context.Context, _ string, request EditRequest) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextEditErr != nil {
		err := f.nextEditErr
		f.nextEditErr = nil
		return Message{}, err
	}
	f.edited = append(f.edited, request)
	return Message{MessageID: request.MessageID, Chat: Chat{ID: request.ChatID, Type: "private"}}, nil
}

func newTestManager(t *testing.T, root string, owner *fakeBrain, api *fakeAPI) *Manager {
	t.Helper()
	manager, err := NewManagerWithOptions(root, owner, Options{
		API: api,
		Now: func() time.Time {
			return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
		},
		PollTimeout: 1,
		Backoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func configuredManager(t *testing.T) (*Manager, *fakeBrain, *fakeAPI, string) {
	t.Helper()
	root := t.TempDir()
	owner := &fakeBrain{threadID: "thread-1"}
	api := &fakeAPI{bot: User{ID: 7001, IsBot: true, FirstName: "Zen", Username: "zen_test_bot", Topics: true}}
	manager := newTestManager(t, root, owner, api)
	if _, err := manager.Configure(context.Background(), "123456:test-token"); err != nil {
		t.Fatal(err)
	}
	return manager, owner, api, root
}

func bindOwner(t *testing.T, manager *Manager, updateID, ownerID, chatID int64) {
	t.Helper()
	challenge, err := manager.BeginBinding()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(challenge.URL)
	if err != nil {
		t.Fatal(err)
	}
	value := parsed.Query().Get("start")
	update := Update{UpdateID: updateID, Message: &Message{MessageID: updateID, From: &User{ID: ownerID, FirstName: "Owner", Username: "owner_hint"}, Chat: Chat{ID: chatID, Type: "private"}, Text: "/start " + value}}
	if err := manager.handleUpdate(context.Background(), "123456:test-token", update); err != nil {
		t.Fatal(err)
	}
	if manager.Status().State != StateConnected {
		t.Fatalf("status=%+v", manager.Status())
	}
}

func TestConfigureBindingIdentityDedupeAndOffsetRecovery(t *testing.T) {
	manager, owner, _, root := configuredManager(t)
	stateBytes, err := os.ReadFile(filepath.Join(root, "telegram", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateBytes), "test-token") {
		t.Fatal("public state contains bot token")
	}
	for _, name := range []string{"state.json", "token"} {
		info, err := os.Stat(filepath.Join(root, "telegram", name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v", name, info.Mode().Perm())
		}
	}

	challenge, err := manager.BeginBinding()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(challenge.URL)
	if err != nil {
		t.Fatal(err)
	}
	secret := parsed.Query().Get("start")
	wrongGroup := Update{UpdateID: 1, Message: &Message{From: &User{ID: 10}, Chat: Chat{ID: -100, Type: "group"}, Text: "/start " + secret}}
	if err := manager.handleUpdate(context.Background(), "token", wrongGroup); err != nil {
		t.Fatal(err)
	}
	if manager.Status().State == StateConnected {
		t.Fatal("group bound owner")
	}
	valid := Update{UpdateID: 2, Message: &Message{MessageID: 20, From: &User{ID: 10, FirstName: "Owner"}, Chat: Chat{ID: 10, Type: "private"}, Text: "/start " + secret}}
	if err := manager.handleUpdate(context.Background(), "token", valid); err != nil {
		t.Fatal(err)
	}

	wrongSender := Update{UpdateID: 3, Message: &Message{From: &User{ID: 11}, Chat: Chat{ID: 10, Type: "private"}, Text: "ignore me"}}
	wrongChat := Update{UpdateID: 4, Message: &Message{From: &User{ID: 10}, Chat: Chat{ID: 99, Type: "private"}, Text: "ignore me too"}}
	validText := Update{UpdateID: 5, Message: &Message{MessageID: 50, From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Text: "continue", ReplyToMessage: &Message{Text: "the earlier result"}}}
	for _, update := range []Update{wrongSender, wrongChat, validText, validText} {
		if err := manager.handleUpdate(context.Background(), "token", update); err != nil {
			t.Fatal(err)
		}
	}
	if len(owner.receipts) != 1 || owner.receipts[0] != "telegram:update:7001:5" {
		t.Fatalf("receipts=%v", owner.receipts)
	}
	if !strings.Contains(owner.bodies[0], "Replying to: the earlier result") {
		t.Fatalf("body=%q", owner.bodies[0])
	}
	if got := manager.store.snapshot().NextOffset; got != 6 {
		t.Fatalf("offset=%d", got)
	}

	reopened := newTestManager(t, root, owner, &fakeAPI{bot: User{ID: 7001, IsBot: true, Username: "zen_test_bot"}})
	if err := reopened.handleUpdate(context.Background(), "token", validText); err != nil {
		t.Fatal(err)
	}
	if len(owner.receipts) != 1 {
		t.Fatalf("duplicate admission: %v", owner.receipts)
	}
}

func TestBindingChallengeIsPrivateSingleUseAndExpires(t *testing.T) {
	manager, owner, _, _ := configuredManager(t)
	challenge, err := manager.BeginBinding()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(challenge.URL)
	if err != nil {
		t.Fatal(err)
	}
	secret := parsed.Query().Get("start")

	senderChat := Update{UpdateID: 1, Message: &Message{
		From:       &User{ID: 10},
		SenderChat: &Chat{ID: 10, Type: "private"},
		Chat:       Chat{ID: 10, Type: "private"},
		Text:       "/start " + secret,
	}}
	if err := manager.handleUpdate(context.Background(), "token", senderChat); err != nil {
		t.Fatal(err)
	}
	if manager.Status().State == StateConnected {
		t.Fatal("sender_chat identity bound owner")
	}

	valid := Update{UpdateID: 2, Message: &Message{
		From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Text: "/start " + secret,
	}}
	if err := manager.handleUpdate(context.Background(), "token", valid); err != nil {
		t.Fatal(err)
	}
	replay := Update{UpdateID: 3, Message: &Message{
		From: &User{ID: 11}, Chat: Chat{ID: 11, Type: "private"}, Text: "/start " + secret,
	}}
	if err := manager.handleUpdate(context.Background(), "token", replay); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	if state.OwnerID != 10 || state.ChatID != 10 || len(owner.receipts) != 0 {
		t.Fatalf("challenge replay changed authority: owner=%d chat=%d receipts=%v", state.OwnerID, state.ChatID, owner.receipts)
	}

	if err := manager.RevokeOwner(); err != nil {
		t.Fatal(err)
	}
	expiring, err := manager.BeginBinding()
	if err != nil {
		t.Fatal(err)
	}
	expiringURL, err := url.Parse(expiring.URL)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return expiring.ExpiresAt }
	stale := Update{UpdateID: 4, Message: &Message{
		From: &User{ID: 12}, Chat: Chat{ID: 12, Type: "private"}, Text: "/start " + expiringURL.Query().Get("start"),
	}}
	if err := manager.handleUpdate(context.Background(), "token", stale); err != nil {
		t.Fatal(err)
	}
	if manager.store.snapshot().OwnerID != 0 {
		t.Fatal("expired challenge bound owner")
	}
}

func TestUnsupportedMediaCommandsAndNewChat(t *testing.T) {
	manager, owner, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	updates := []Update{
		{UpdateID: 2, Message: &Message{MessageID: 2, From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Photo: []any{map[string]any{"file_id": "fixture"}}}},
		{UpdateID: 3, Message: &Message{MessageID: 3, From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Text: "/status"}},
		{UpdateID: 4, Message: &Message{MessageID: 4, From: &User{ID: 10}, Chat: Chat{ID: 10, Type: "private"}, Text: "/new"}},
	}
	for _, update := range updates {
		if err := manager.handleUpdate(context.Background(), "token", update); err != nil {
			t.Fatal(err)
		}
	}
	if len(owner.receipts) != 0 {
		t.Fatalf("unsupported/commands admitted: %v", owner.receipts)
	}
	if owner.newChats != 1 {
		t.Fatalf("new chats=%d", owner.newChats)
	}
}

func TestOutboxProjectionRetryAmbiguityAndWorkEdit(t *testing.T) {
	manager, owner, api, root := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	// Deliver binding confirmation first.
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	owner.timeline = []brain.TimelineItem{
		{ID: "assistant-1", ThreadID: "thread-1", SessionID: "host", Role: "assistant", Kind: "assistant_message", Body: strings.Repeat("a", 4100)},
		{ID: "event-1", ThreadID: "thread-1", SessionID: "agent", Role: "system", Kind: "work_card", WorkID: "work-1", Title: "Telegram slice", Status: "running", Summary: "Testing"},
	}
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	api.nextSendErr = &APIError{Code: 429, Retryable: true, RetryAfter: time.Millisecond}
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Date(2026, 8, 24, 12, 0, 1, 0, time.UTC) }
	for i := 0; i < 3; i++ {
		if err := manager.deliverOne(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	if len(api.sent) != 4 {
		t.Fatalf("sent=%d want binding + 2 chunks + work", len(api.sent))
	}
	owner.timeline[1].Status = "done"
	owner.timeline[1].Summary = "Verified"
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 1 || api.edited[0].MessageID == 0 {
		t.Fatalf("edits=%+v", api.edited)
	}

	if err := manager.store.mutate(func(state *durableState) error {
		state.Outbox = append(state.Outbox, outboxRecord{
			ID:        "crash-row",
			Kind:      "send",
			Text:      "maybe sent",
			State:     "dispatching",
			CreatedAt: manager.now(),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened := newTestManager(t, root, owner, api)
	if reopened.Status().AmbiguousDelivery != 1 || reopened.Status().State != StateDegraded {
		t.Fatalf("status=%+v", reopened.Status())
	}
}

func TestWorkProjectionCoalescesBeforeInitialSend(t *testing.T) {
	manager, owner, api, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	owner.timeline = []brain.TimelineItem{{
		ID: "event-1", ThreadID: "thread-1", Kind: "work_card", WorkID: "work-1",
		Title: "Telegram slice", Status: "running", Summary: "First revision",
	}}
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	owner.timeline[0].ID = "event-2"
	owner.timeline[0].Status = "waiting"
	owner.timeline[0].Summary = "Latest revision"
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}

	state := manager.store.snapshot()
	pending := 0
	for _, row := range state.Outbox {
		if row.WorkID == "work-1" && row.State == "pending" {
			pending++
			if row.CanonicalID != "event-2" || !strings.Contains(row.Text, "Latest revision") {
				t.Fatalf("pending Work row did not coalesce: %+v", row)
			}
		}
	}
	if pending != 1 {
		t.Fatalf("pending Work rows=%d, want 1", pending)
	}
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 2 || !strings.Contains(api.sent[1].Text, "Latest revision") {
		t.Fatalf("initial Work delivery=%+v", api.sent)
	}
}

func TestAmbiguousWorkSendNeverReplaysOrCreatesRevision(t *testing.T) {
	manager, owner, api, root := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	owner.timeline = []brain.TimelineItem{{
		ID: "event-1", ThreadID: "thread-1", Kind: "work_card", WorkID: "work-1",
		Title: "Telegram slice", Status: "running", Summary: "May commit remotely",
	}}
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	api.nextSendErr = errors.New("connection closed after request write")
	if err := manager.deliverOne(context.Background(), "token"); err == nil {
		t.Fatal("ambiguous transport send succeeded")
	}
	if manager.Status().AmbiguousDelivery != 1 {
		t.Fatalf("status=%+v", manager.Status())
	}
	sendsAfterAmbiguity := len(api.sent)

	owner.timeline[0].ID = "event-2"
	owner.timeline[0].Status = "done"
	owner.timeline[0].Summary = "Later revision"
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := manager.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != sendsAfterAmbiguity || len(api.edited) != 0 {
		t.Fatalf("ambiguous Work replayed before restart: sends=%d edits=%d", len(api.sent), len(api.edited))
	}

	reopened := newTestManager(t, root, owner, api)
	if err := reopened.projectTimeline(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.deliverOne(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if reopened.Status().AmbiguousDelivery != 1 || len(api.sent) != sendsAfterAmbiguity || len(api.edited) != 0 {
		t.Fatalf("ambiguous Work replayed after restart: status=%+v sends=%d edits=%d", reopened.Status(), len(api.sent), len(api.edited))
	}
}

func TestDurableChannelStateBoundsProcessedUpdatesAndOutbox(t *testing.T) {
	state := newDurableState()
	for updateID := int64(1); updateID <= maxProcessedUpdate+100; updateID++ {
		state.Processed[fmt.Sprint(updateID)] = updateRecord{Disposition: "ignored"}
		state.NextOffset = updateID + 1
		trimProcessedUpdates(&state)
	}
	if len(state.Processed) != maxProcessedUpdate {
		t.Fatalf("processed updates=%d, want %d", len(state.Processed), maxProcessedUpdate)
	}
	if _, found := state.Processed["100"]; found {
		t.Fatal("processed-update retention kept an entry behind the bounded offset window")
	}

	for index := 0; index < maxOutboxRows; index++ {
		if !enqueue(&state, outboxRecord{ID: fmt.Sprintf("row-%d", index), Kind: "send"}) {
			t.Fatalf("enqueue failed at row %d", index)
		}
	}
	if enqueue(&state, outboxRecord{ID: "overflow", Kind: "send"}) {
		t.Fatal("full live outbox accepted an unbounded row")
	}
	if len(state.Outbox) != maxOutboxRows || state.LastError == "" {
		t.Fatalf("full outbox rows=%d error=%q", len(state.Outbox), state.LastError)
	}
	state.Outbox[0].State = "sent"
	if !enqueue(&state, outboxRecord{ID: "after-compaction", Kind: "send"}) {
		t.Fatal("terminal outbox history was not compacted")
	}
	if len(state.Outbox) != maxOutboxRows {
		t.Fatalf("compacted outbox rows=%d, want %d", len(state.Outbox), maxOutboxRows)
	}
}

func TestWebhookConflictRotationRevokeAndRemove(t *testing.T) {
	root := t.TempDir()
	owner := &fakeBrain{threadID: "thread"}
	api := &fakeAPI{
		bot:     User{ID: 1, IsBot: true, Username: "one_bot"},
		webhook: WebhookInfo{URL: "https://operator.example/hook"},
	}
	manager := newTestManager(t, root, owner, api)
	if _, err := manager.Configure(context.Background(), "token-one"); err != nil {
		t.Fatal(err)
	}
	if !manager.Status().WebhookConflict || manager.Status().State != StateDegraded {
		t.Fatalf("status=%+v", manager.Status())
	}
	api.webhook = WebhookInfo{}
	api.bot = User{ID: 2, IsBot: true, Username: "two_bot"}
	if _, err := manager.Configure(context.Background(), "token-two"); err != nil {
		t.Fatal(err)
	}
	bindOwner(t, manager, 1, 20, 20)
	api.bot = User{ID: 3, IsBot: true, Username: "three_bot"}
	if _, err := manager.Configure(context.Background(), "token-three"); err != nil {
		t.Fatal(err)
	}
	if manager.Status().OwnerHint != "" || manager.Status().State != StateSetupPending {
		t.Fatalf("rotation retained owner: %+v", manager.Status())
	}
	if err := manager.RevokeOwner(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Disable(); err != nil {
		t.Fatal(err)
	}
	if manager.Status().State != StateDisabled {
		t.Fatalf("status=%+v", manager.Status())
	}
	if err := manager.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "telegram", "token")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token remains: %v", err)
	}
}

func TestWebhookRefreshPreservesUnrelatedHealthError(t *testing.T) {
	manager, _, api, _ := configuredManager(t)
	manager.recordError("Telegram delivery is degraded.")
	if err := manager.refreshWebhookState(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if got := manager.Status().LastError; got != "Telegram delivery is degraded." {
		t.Fatalf("unrelated health error cleared: %q", got)
	}

	api.webhook = WebhookInfo{URL: "https://operator.example/hook"}
	if err := manager.refreshWebhookState(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	api.webhook = WebhookInfo{}
	if err := manager.refreshWebhookState(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.WebhookConflict || status.LastError != "" {
		t.Fatalf("webhook conflict did not clear its own error: %+v", status)
	}
}

func TestAtomicPrivateWriteCleansFailedPartial(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicPrivateWrite(target, []byte("secret")); err == nil {
		t.Fatal("expected rename failure")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".telegram-*.partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial files remain: %v", matches)
	}
}

func TestRunStopsBlockedLongPoll(t *testing.T) {
	manager, _, api, _ := configuredManager(t)
	api.blockPoll = true
	api.pollStarted = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx) }()
	select {
	case <-api.pollStarted:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop")
	}
}
