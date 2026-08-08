package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
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

func TestWorkCardMaterializesOnceChronologically(t *testing.T) {
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
	card1, materialized, err := store.MaterializeWorkCard(item, first)
	if err != nil || !materialized {
		t.Fatalf("card1 = %#v materialized=%v err=%v", card1, materialized, err)
	}
	card1Again, materialized, err := store.MaterializeWorkCard(item, first)
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
	card2, materialized, err := store.MaterializeWorkCard(item, second)
	if err != nil || !materialized {
		t.Fatalf("card2 = %#v materialized=%v err=%v", card2, materialized, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := TimelineItemsToConversationEvents(items)
	if len(visible) != 2 || visible[0].ID != card1.ID || visible[1].ID != card2.ID {
		t.Fatalf("immutable chronological cards = %#v", visible)
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
	if _, _, err := store.MaterializeWorkCard(item, event); err != nil {
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
		hostID := "brain-agent-brain-hidden:@1"
		sessionID := "brain-agent-stale-card:@2"
		if err := store.SetHostSession(hostID, "codex"); err != nil {
			t.Fatal(err)
		}
		fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		}}
		service := NewService(store, fw, nil)
		service.now = func() time.Time { return now }
		item, err := store.CreateWork(Work{
			Title:            "zen-telegram-performance-publish",
			Objective:        "Prove stale materializes a card",
			Status:           WorkRunning,
			OwnerSessionID:   sessionID,
			CompletionPolicy: CompletionBounded,
		})
		if err != nil {
			t.Fatal(err)
		}
		acceptedAt := now.Add(-2 * time.Hour)
		if err := store.AdmitTurn(watcher.AdmittedTurn{
			SessionID: sessionID, TurnID: sessionID + ":turn:1", AcceptedAt: acceptedAt,
		}); err != nil {
			t.Fatal(err)
		}
		// Expire the current turn's own lease, then reconcile with a live
		// pane: exactly one actionable session.stale materializes a card.
		if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
			SessionID: sessionID, TurnID: sessionID + ":turn:1",
			Class: watcher.EvidenceControl, Kind: "running",
			SourceID: "control\x00heartbeat-1", LeaseSeconds: 1,
			At: now.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		service.ReconcileDelegatedSessions([]*classifier.Agent{{
			ID: sessionID, State: classifier.StateRunning, Delegated: true,
			PaneAlive: true, ProcessID: 4242,
		}})
		service.ReconcileDelegatedSessions([]*classifier.Agent{{
			ID: sessionID, State: classifier.StateRunning, Delegated: true,
			PaneAlive: true, ProcessID: 4242,
		}})
		events, err := store.ListWorkEvents(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		var recorded WorkEvent
		for _, event := range events {
			if event.Kind == "session.stale" {
				recorded = event
				break
			}
		}
		if recorded.ID == "" {
			t.Fatalf("missing session.stale in %#v", events)
		}
		items, err := store.ThreadTimeline(threadID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != recorded.ID || items[0].Kind != timelineKindWorkCard || items[0].EventKind != "session.stale" {
			t.Fatalf("stale materialize = %#v recorded=%#v", items, recorded)
		}
	})
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
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := sessionID + ":turn:1"
	if err := store.AdmitTurn(watcher.AdmittedTurn{
		SessionID: sessionID, TurnID: turnID, AcceptedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
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
	if len(items) != 1 || items[0].ID != recorded.ID || items[0].Kind != timelineKindWorkCard || items[0].EventKind != kind {
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
		EventID:    "1aa90ab5-cf46-4643-9985-f6fd26c9526b",
		WorkID:     "ae621005-929b-49b5-9d42-fa476d42d3f3",
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

func TestOrchestrationSchemaV3BindsCurrentThreadWithoutBulkCards(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	threadID := "thread-current"
	chatState := `{"thread_id":"` + threadID + `","thread_ids":["` + threadID + `"]}` + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, "chat_state.json"), []byte(chatState), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "schema_version": 3,
  "migrations": {},
  "brain_work": [{
    "work_id": "work-active",
    "title": "Active legacy",
    "objective": "Bind ownership then accept new cards",
    "status": "waiting",
    "completion_policy": "bounded",
    "created_at": "` + fixed.Format(time.RFC3339Nano) + `",
    "updated_at": "` + fixed.Format(time.RFC3339Nano) + `"
  }, {
    "work_id": "work-calendar",
    "title": "Calendar legacy",
    "objective": "Keep explicit source when already present",
    "status": "running",
    "source_thread_id": "thread-scheduled",
    "completion_policy": "bounded",
    "context_ref": "calendar:item-1:run-1",
    "created_at": "` + fixed.Format(time.RFC3339Nano) + `",
    "updated_at": "` + fixed.Format(time.RFC3339Nano) + `"
  }],
  "brain_work_events": []
}`
	if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), []byte(legacy+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Work("work-active")
	if err != nil {
		t.Fatal(err)
	}
	if active.SourceThreadID != threadID {
		t.Fatalf("active legacy bind = %q want %q", active.SourceThreadID, threadID)
	}
	calendarWork, err := store.Work("work-calendar")
	if err != nil {
		t.Fatal(err)
	}
	if calendarWork.SourceThreadID != "thread-scheduled" {
		t.Fatalf("explicit scheduled source overwritten: %#v", calendarWork)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "orchestration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "terminal_at") {
		t.Fatalf("persisted schema must not retain terminal_at: %s", raw)
	}
	database, migrated, err := decodeOrchestrationDatabase(raw)
	if err != nil || migrated || database.SchemaVersion != orchestrationSchemaVersion {
		t.Fatalf("schema after upgrade incomplete: migrated=%v schema=%d err=%v", migrated, database.SchemaVersion, err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("must not bulk-materialize historical cards: %#v", items)
	}

	service := NewService(store, nil, nil)
	event, created, err := service.AppendWorkEvent(WorkEvent{
		ID: "post-upgrade-card", WorkID: active.ID, Kind: "session.needs_input",
		DedupeKey: "session:upgrade:turn:one:session.needs_input", Actionable: false, Summary: "first post-upgrade event",
	})
	if err != nil || !created {
		t.Fatalf("append = %#v created=%v err=%v", event, created, err)
	}
	items, err = store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "post-upgrade-card" || items[0].ThreadID != threadID {
		t.Fatalf("post-upgrade card = %#v", items)
	}
	other, err := store.ThreadTimeline("thread-scheduled", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("card leaked onto scheduled thread: %#v", other)
	}
}
