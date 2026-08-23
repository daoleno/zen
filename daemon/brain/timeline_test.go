package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestThreadTimelineSurvivesEmptyProviderHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_timeline"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	provider := work.CodexConversation{
		Available: true,
		SessionID: "host-a",
		Events: []work.CodexConversationEvent{{
			ID:        "user-1",
			Timestamp: "2026-08-06T01:00:00Z",
			Kind:      "user_message",
			Role:      "user",
			Body:      "Remember this across host rotation",
		}, {
			ID:        "assistant-1",
			Timestamp: "2026-08-06T01:00:01Z",
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      "Noted.",
		}},
	}
	if err := store.MaterializeProviderConversation(threadID, provider); err != nil {
		t.Fatal(err)
	}
	emptyHost := work.CodexConversation{
		Available: true,
		SessionID: "host-b",
		Events:    nil,
	}
	if err := store.MaterializeProviderConversation(threadID, emptyHost); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("timeline len = %d, want 2 after empty host: %#v", len(items), items)
	}
	events := TimelineItemsToConversationEvents(items)
	if len(events) != 2 || events[0].Body != "Remember this across host rotation" || events[1].Body != "Noted." {
		t.Fatalf("conversation events = %#v", events)
	}
}

func TestWorkCardProjectionReplacesInPlace(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_cards"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Delegated work",
		Objective:        "Do the thing",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceThreadID != threadID {
		t.Fatalf("frozen thread = %q want %q", item.SourceThreadID, threadID)
	}
	first, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "session:needs:turn:one:session.needs_input",
		Actionable: true,
		Summary:    "Need a decision",
		SourceName: "agent-a",
		PayloadRef: "session:sess-a",
	})
	if err != nil || !created {
		t.Fatalf("first event = %#v created=%v err=%v", first, created, err)
	}
	card1, materialized, err := store.SyncWorkCard(item.ID, &first)
	if err != nil || !materialized {
		t.Fatalf("card1 = %#v materialized=%v err=%v", card1, materialized, err)
	}
	card1Again, materialized, err := store.SyncWorkCard(item.ID, &first)
	if err != nil || materialized || card1Again.ID != card1.ID {
		t.Fatalf("exact-once failed: again=%#v materialized=%v err=%v", card1Again, materialized, err)
	}

	second, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:done:turn:one:session.done",
		Actionable: true,
		Summary:    "Finished",
		SourceName: "agent-a",
		PayloadRef: "session:sess-a",
	})
	if err != nil || !created {
		t.Fatalf("second event = %#v created=%v err=%v", second, created, err)
	}
	card2, materialized, err := store.SyncWorkCard(item.ID, &second)
	if err != nil || materialized || card2.ID != second.ID {
		t.Fatalf("card2 = %#v materialized=%v err=%v", card2, materialized, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := TimelineItemsToConversationEvents(items)
	if len(visible) != 1 || visible[0].ID != second.ID || visible[0].Status != "session.done" {
		t.Fatalf("single Work card projection = %#v", visible)
	}
}

func TestWorkEventStaysOnFrozenThreadAfterSwitch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadA := "thread-a"
	threadB := "thread-b"
	if err := store.SetChatState(ChatState{ThreadID: threadA, ThreadIDs: []string{threadA}}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Owned by A",
		Objective:        "Stay on A",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceThreadID != threadA {
		t.Fatalf("create froze %q want %q", item.SourceThreadID, threadA)
	}
	if err := store.SetChatState(ChatState{ThreadID: threadB, ThreadIDs: []string{threadA, threadB}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "session:switch:turn:one:session.done",
		Actionable: false,
		Summary:    "Still belongs to A",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	itemsA, err := store.ThreadTimeline(threadA, 0)
	if err != nil {
		t.Fatal(err)
	}
	itemsB, err := store.ThreadTimeline(threadB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsA) != 1 || itemsA[0].ID != event.ID || itemsA[0].ThreadID != threadA {
		t.Fatalf("thread A timeline = %#v", itemsA)
	}
	if len(itemsB) != 0 {
		t.Fatalf("thread B must stay empty, got %#v", itemsB)
	}
	reloaded, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SourceThreadID != threadA {
		t.Fatalf("SourceThreadID mutated after switch: %#v", reloaded)
	}
}

func TestMarkWorkReadClearsTimelineUnread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_read"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Needs input",
		Objective:        "Ask user",
		Status:           WorkWaiting,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "session:need:turn:one:session.needs_input",
		Actionable: true,
		Summary:    "Please choose",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SyncWorkCard(item.ID, &event); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	if err := service.MarkWorkRead(item.ID); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Unread {
		t.Fatalf("timeline after read = %#v", items)
	}
}

func TestServiceAppendWorkEventMaterializesCard(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_service_card"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title:            "Service path",
		Objective:        "Materialize on append",
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.needs_input",
		DedupeKey:  "session:svc:turn:one:session.needs_input",
		Actionable: false,
		Summary:    "Current needs input",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != event.ID || items[0].EventKind != "session.needs_input" {
		t.Fatalf("service materialize = %#v", items)
	}
}

func TestReducerFactsMaterializeWorkCards(t *testing.T) {
	// Canonical contract: reducer-derived lifecycle Events materialize the
	// same timeline cards the legacy raw projection used to.
	t.Run("session.done", func(t *testing.T) {
		assertReducerMaterializesKind(t, "session.done", watcher.EvidenceProvider, "done")
	})
	t.Run("session.failed", func(t *testing.T) {
		assertReducerMaterializesKind(t, "session.failed", watcher.EvidenceProvider, "failed")
	})
	t.Run("session.needs_input", func(t *testing.T) {
		assertReducerMaterializesKind(t, "session.needs_input", watcher.EvidenceControl, "attention")
	})
	t.Run("session.stale", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
		store.now = func() time.Time { return now }
		threadID := "brain_thread_stale_card"
		if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
			t.Fatal(err)
		}
		sessionID := "brain-agent-stale-card:@2"
		item, err := store.CreateWork(Work{
			Title:            "zen-telegram-performance-publish",
			Objective:        "Prove stale materializes a card",
			Status:           WorkRunning,
			AttemptSessionID: sessionID,
			CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		acceptedAt := now.Add(-2 * time.Hour)
		bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
			SessionID: sessionID, TurnID: sessionID + ":turn:1", AcceptedAt: acceptedAt,
		})
		// Expire the canonical owner's own deadline. The supervisor command
		// enters Lifecycle directly; it does not create a parallel
		// session.stale event/card materialization.
		before, err := store.FSM().State(lifecycle.WorkID(item.ID))
		if err != nil {
			t.Fatal(err)
		}
		store.now = func() time.Time { return before.Attempt.LeaseDeadline.Add(time.Minute) }
		if err := store.FSM().Sweep(); err != nil {
			t.Fatal(err)
		}
		state, err := store.FSM().State(lifecycle.WorkID(item.ID))
		if err != nil {
			t.Fatal(err)
		}
		if state.Review == nil || state.Review.Reason != "lease_expired" {
			t.Fatalf("canonical lease-expiry review = %+v", state.Review)
		}
		cards := store.FSM().Cards()
		if len(cards) != 1 || cards[0].WorkID != lifecycle.WorkID(item.ID) ||
			!cards[0].Actionable || cards[0].Reason != "lease_expired" {
			t.Fatalf("canonical one-card projection = %+v", cards)
		}
	})
}

func TestMatchingControlDoneProjectsOneExistingWorkResultCard(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	threadID := "brain_thread_control_signal_card"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-signal-card:@1"
	item, err := store.CreateWork(Work{
		Title: "Signal card", Objective: "Project the canonical control result",
		Status: WorkRunning, AttemptSessionID: sessionID, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := "turn:signal-card"
	pending, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pendingSubmissionDigest("card prompt"), ProcessIdentity: "process",
		PaneGeneration: "pane", AcceptedAt: now, Mode: watcher.InputAdmissionFresh,
		SignalProtocol: true,
	})
	if err != nil || !created {
		t.Fatalf("prepare card signal = (%+v, %v, %v)", pending, created, err)
	}
	fact := watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00card-done", At: now.Add(time.Second), Summary: "REVIEW_READY: card",
	}
	if result, err := store.ApplyDelegatedTurnProgress(fact); err != nil || !result.Changed || result.Turn.Status != watcher.TurnDone {
		t.Fatalf("control card result = (%+v, %v)", result, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var providerDone WorkEvent
	for _, event := range events {
		if event.Kind == "session.done" {
			providerDone = event
		}
	}
	projected, err := store.Work(item.ID)
	if err != nil || projected.Review == nil || providerDone.ID == "" || providerDone.Actionable {
		t.Fatalf("canonical done event = %+v", events)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != projected.Review.EventID || items[0].Kind != timelineKindWorkCard ||
		items[0].EventKind != "session.done" || items[0].Summary != fact.Summary {
		t.Fatalf("control result card = %+v event=%+v", items, providerDone)
	}
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := restarted.ApplyDelegatedTurnProgress(fact); err != nil || replay.Changed {
		t.Fatalf("card replay = (%+v, %v)", replay, err)
	}
	items, err = restarted.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != projected.Review.EventID {
		t.Fatalf("replayed control cards = %+v err=%v", items, err)
	}
}

func assertReducerMaterializesKind(t *testing.T, kind string, class watcher.EvidenceClass, factKind string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	threadID := "brain_thread_route_card"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-route-card:@" + kind
	item, err := store.CreateWork(Work{
		Title:            "zen-telegram-performance-publish",
		Objective:        "Prove session lifecycle materializes cards",
		Status:           WorkRunning,
		AttemptSessionID: sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := sessionID + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: now,
	})
	admission := providerAdmission("stream", "msg-1", 1, "sha", now)
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class: watcher.EvidenceReceipt, Kind: "admission",
		SourceID:   "receipt\x00" + turnID + "\x00accepted\x00payload",
		Admission:  admission,
		ActivityID: "activity-1",
		At:         now.Add(time.Second),
	}); err != nil || !changed {
		t.Fatalf("admission apply = (%v, %v)", changed, err)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: turnID,
		Class:        class,
		Kind:         factKind,
		SourceID:     "fact\x00" + turnID + "\x00" + factKind,
		Admission:    admission,
		ActivityID:   "activity-1",
		LeaseSeconds: 300,
		StartedAt:    now.Add(2 * time.Second),
		SettledAt:    now.Add(30 * time.Second),
		Summary:      "Delegated " + factKind + " fact",
	}); err != nil || !changed {
		t.Fatalf("lifecycle fact apply = (%v, %v)", changed, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recorded WorkEvent
	for _, event := range events {
		if event.Kind == kind {
			recorded = event
			break
		}
	}
	if recorded.ID == "" {
		t.Fatalf("missing %s event in %#v", kind, events)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectedID := recorded.ID
	if projected.Review != nil {
		expectedID = projected.Review.EventID
	}
	if len(items) != 1 || items[0].ID != expectedID || items[0].Kind != timelineKindWorkCard || items[0].EventKind != kind {
		t.Fatalf("route materialize %s = %#v recorded=%#v", kind, items, recorded)
	}
	conversation := TimelineItemsToConversationEvents(items)
	if len(conversation) != 1 ||
		conversation[0].Source != workResultConversationSource ||
		conversation[0].Status != kind ||
		conversation[0].WorkID != item.ID {
		t.Fatalf("conversation projection = %#v", conversation)
	}
}

func TestTimelineOmitsCanonicalDirectWorkEventUserRows(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_hide_envelope"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	envelope := work.FormatDirectWorkEventInput(work.DirectWorkEventInput{
		EventID: "1aa90ab5-cf46-4643-9985-f6fd26c9526b", WorkID: "ae621005-929b-49b5-9d42-fa476d42d3f3",
		WorkRevision: 7, HandlingID: "handling-envelope", ProviderTurnID: "provider-turn-envelope",
		ResolutionRequired: true, ResolveCommand: "zen brain work resolve --event-id 1aa90ab5-cf46-4643-9985-f6fd26c9526b --handling-id handling-envelope --provider-turn-id provider-turn-envelope --revision 7 --disposition complete",
		WorkTitle:  "zen-telegram-performance-publish",
		Kind:       "session.failed",
		Source:     "zen-telegram-performance-publish (brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730)",
		Summary:    "Delegated provider process or pane is no longer live",
		NextAction: "Inspect the delegated Session failure.",
		ContextRef: "worklog/2026-08-06-zen-telegram-performance-publish.md",
		PayloadRef: "session:brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730",
	})
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "host",
		Events: []work.CodexConversationEvent{{
			ID:        "provider-envelope-1",
			Timestamp: "2026-08-06T10:19:43.328Z",
			Kind:      "user_message",
			Role:      "user",
			Body:      envelope,
		}, {
			ID:        "user-visible",
			Timestamp: "2026-08-06T10:19:44.000Z",
			Kind:      "user_message",
			Role:      "user",
			Body:      "please continue",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "user-visible" {
		t.Fatalf("materialize leaked envelope: %#v", items)
	}
	// Historical envelope already on disk must not surface through conversation projection.
	store.mu.Lock()
	_, err = store.appendTimelineItemLocked(TimelineItem{
		ID:        "legacy-envelope",
		ThreadID:  threadID,
		SessionID: "host",
		Role:      "user",
		Body:      envelope,
		CreatedAt: time.Date(2026, 8, 6, 10, 19, 43, 0, time.UTC),
		Kind:      timelineKindUserMessage,
	})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	conversation := TimelineItemsToConversationEvents(items)
	if len(conversation) != 1 || conversation[0].ID != "user-visible" || strings.Contains(conversation[0].Body, "zen_work_event") {
		t.Fatalf("conversation leaked envelope: %#v", conversation)
	}
}
