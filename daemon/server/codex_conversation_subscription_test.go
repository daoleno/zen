package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainScopedConversationIncludesBrainCalendarResult(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Daily papers", "thread-1", "Three papers.", "")
	srv := &Server{brain: service, calendar: calendarStore}
	base := work.CodexConversation{Available: true, SessionID: "agent-session", Events: []work.CodexConversationEvent{{ID: "agent:1", Seq: 1, Timestamp: result.CreatedAt.Add(-time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "Earlier"}}}
	got := srv.brainScopedConversation("brain-thread:thread-1", base, time.Now())
	if got.SessionID != "brain-thread:thread-1" || len(got.Events) != 2 {
		t.Fatalf("conversation = %#v", got)
	}
	if event := got.Events[1]; event.ID != result.ID || event.Source != "calendar_result" || event.Kind != "status" || event.Status != "completed" {
		t.Fatalf("result = %#v", event)
	}
}

func TestSubscriptionPublishesProviderTranscriptWithBrainOverlay(t *testing.T) {
	baseTime := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Provider check", "thread-1", "Calendar result from Brain.", "")

	var loads atomic.Int32
	srv := &Server{
		watcher:  watcher.New(time.Second),
		brain:    service,
		calendar: calendarStore,
		providerConversationLoader: func(_ *work.ProviderConversationReader, agentID string) (work.CodexConversation, error) {
			loads.Add(1)
			if agentID != "provider-agent" {
				return work.CodexConversation{}, fmt.Errorf("unexpected provider agent %q", agentID)
			}
			return work.CodexConversation{
				Available: true,
				Source:    "claude_transcript",
				Path:      "/provider/session.jsonl",
				SessionID: "provider-session",
				CWD:       "/provider/workspace",
				Activity: &work.ProviderActivity{
					ID:        "provider-activity",
					Status:    work.ProviderActivityRunning,
					StartedAt: baseTime.Format(time.RFC3339Nano),
				},
				Events: []work.CodexConversationEvent{{
					ID:        "provider-event",
					Seq:       1,
					Timestamp: baseTime.Format(time.RFC3339Nano),
					Kind:      "assistant_message",
					Role:      "assistant",
					Body:      "Provider-owned history.",
					Source:    "claude_transcript",
				}},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:                 "codex_conversation_subscribe",
		RequestID:            "subscription-1",
		TargetID:             "provider-agent",
		Cwd:                  "/provider/workspace",
		Command:              "claude",
		StartedAt:            json.RawMessage(`"2026-07-16T06:00:00Z"`),
		ConversationScopeKey: "brain-thread:thread-1",
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Type         string                 `json:"type"`
		RequestID    string                 `json:"request_id"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "codex_conversation_snapshot" || response.RequestID != request.RequestID {
		t.Fatalf("subscription response = %#v", response)
	}
	conversation := response.Conversation
	if conversation.Source != "brain_chat" || conversation.Path != "/provider/session.jsonl" ||
		conversation.CWD != "/provider/workspace" || conversation.SessionID != "brain-thread:thread-1" {
		t.Fatalf("provider/Brain fields = %#v", conversation)
	}
	if ids := fmt.Sprint(conversationEventIDs(conversation.Events)); ids != fmt.Sprintf("[provider-event %s]", result.ID) {
		t.Fatalf("subscription events = %s", ids)
	}
	if conversation.Events[0].Body != "Provider-owned history." ||
		conversation.Events[1].Source != "calendar_result" {
		t.Fatalf("subscription event sources = %#v", conversation.Events)
	}
	if conversation.Activity != nil {
		t.Fatalf("detached provider transcript retained Activity: %#v", conversation.Activity)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("provider transcript loads before first snapshot = %d, want 1", got)
	}
}

func TestSubscriptionPreservesProviderUnavailableWithoutCapturingPane(t *testing.T) {
	binDir := t.TempDir()
	captureCounter := filepath.Join(t.TempDir(), "pane-capture-called")
	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf capture >> \"$ZEN_PANE_CAPTURE_COUNTER\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_PANE_CAPTURE_COUNTER", captureCounter)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var loads atomic.Int32
	srv := &Server{
		watcher: watcher.New(time.Second),
		providerConversationLoader: func(_ *work.ProviderConversationReader, agentID string) (work.CodexConversation, error) {
			loads.Add(1)
			if agentID != "provider-agent" {
				return work.CodexConversation{}, fmt.Errorf("unexpected provider agent %q", agentID)
			}
			return work.CodexConversation{
				Available: false,
				Reason:    "transcript_not_found",
				Source:    "claude_transcript",
				Events:    []work.CodexConversationEvent{},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-no-transcript",
		TargetID:  "provider-agent",
		Cwd:       "/provider/workspace",
		Command:   "claude",
		StartedAt: json.RawMessage(`"2026-07-16T06:00:00Z"`),
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Type         string                 `json:"type"`
		RequestID    string                 `json:"request_id"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "codex_conversation_snapshot" || response.RequestID != request.RequestID {
		t.Fatalf("subscription response = %#v", response)
	}
	conversation := response.Conversation
	if conversation.Available || conversation.Reason != "transcript_not_found" ||
		conversation.Source != "claude_transcript" || len(conversation.Events) != 0 ||
		conversation.Activity != nil {
		t.Fatalf("provider unavailable result was replaced: %#v", conversation)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("provider transcript loads before first snapshot = %d, want 1", got)
	}
	if _, err := os.Stat(captureCounter); !os.IsNotExist(err) {
		t.Fatalf("Chat subscription invoked tmux pane capture: %v", err)
	}
}

func TestConversationSubscriptionOwnsReaderForExactlyItsLifetime(t *testing.T) {
	loads := make(chan *work.ProviderConversationReader, 128)
	srv := &Server{
		watcher: watcher.New(time.Second),
		providerConversationLoader: func(reader *work.ProviderConversationReader, agentID string) (work.CodexConversation, error) {
			if agentID != "provider-agent" {
				return work.CodexConversation{}, fmt.Errorf("unexpected provider agent %q", agentID)
			}
			loads <- reader
			return work.CodexConversation{
				Available: true,
				Source:    "test_provider",
				Path:      "/test/provider/session.jsonl",
				SessionID: "test-provider-session",
				Events:    []work.CodexConversationEvent{},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	subscribe := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "reader-lifetime",
		TargetID:  "provider-agent",
		Cwd:       "/provider/workspace",
		Command:   "codex",
		StartedAt: json.RawMessage(`"2026-07-17T00:00:00Z"`),
	}

	writeConversationSubscriptionRequest(t, conn, subscribe)
	readConversationSubscriptionSnapshot(t, conn, subscribe.RequestID)
	first := waitForProviderReaderLoad(t, loads)

	writeConversationSubscriptionRequest(t, conn, clientMessage{
		Type:      "codex_conversation_unsubscribe",
		RequestID: subscribe.RequestID,
	})
	waitForConversationSubscriptionCount(t, srv, 0)
	drainProviderReaderLoads(loads)
	assertNoProviderReaderLoad(t, loads)

	writeConversationSubscriptionRequest(t, conn, subscribe)
	readConversationSubscriptionSnapshot(t, conn, subscribe.RequestID)
	second := waitForProviderReaderLoad(t, loads)
	if second == first {
		t.Fatal("later subscription reused the ended subscription's provider reader")
	}

	// Replacing the same subscription ID cancels the old goroutine and starts a
	// fresh ownership lifetime.
	writeConversationSubscriptionRequest(t, conn, subscribe)
	readConversationSubscriptionSnapshot(t, conn, subscribe.RequestID)
	third := waitForDifferentProviderReader(t, loads, second)
	if third == first {
		t.Fatal("replacement subscription reused an earlier provider reader")
	}
	time.Sleep(codexConversationSubscriptionInterval + 40*time.Millisecond)
	for {
		select {
		case reader := <-loads:
			if reader != third {
				t.Fatalf("replaced subscription continued loading with old reader %p; current %p", reader, third)
			}
		default:
			goto replacementChecked
		}
	}

replacementChecked:
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	waitForConversationSubscriptionCount(t, srv, 0)
	drainProviderReaderLoads(loads)
	assertNoProviderReaderLoad(t, loads)
}

func writeConversationSubscriptionRequest(t *testing.T, conn interface{ WriteJSON(any) error }, request clientMessage) {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
}

func readConversationSubscriptionSnapshot(t *testing.T, conn interface {
	SetReadDeadline(time.Time) error
	ReadJSON(any) error
}, requestID string) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "codex_conversation_snapshot" || response.RequestID != requestID {
		t.Fatalf("subscription response = %#v", response)
	}
}

func waitForProviderReaderLoad(t *testing.T, loads <-chan *work.ProviderConversationReader) *work.ProviderConversationReader {
	t.Helper()
	select {
	case reader := <-loads:
		return reader
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider reader load")
		return nil
	}
}

func waitForDifferentProviderReader(t *testing.T, loads <-chan *work.ProviderConversationReader, previous *work.ProviderConversationReader) *work.ProviderConversationReader {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case reader := <-loads:
			if reader != previous {
				return reader
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for fresh replacement provider reader")
			return nil
		}
	}
}

func waitForConversationSubscriptionCount(t *testing.T, srv *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		count := 0
		for _, subscriptions := range srv.codexSubs {
			count += len(subscriptions)
		}
		srv.mu.Unlock()
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("conversation subscription count did not reach %d", want)
}

func drainProviderReaderLoads(loads <-chan *work.ProviderConversationReader) {
	for {
		select {
		case <-loads:
		default:
			return
		}
	}
}

func assertNoProviderReaderLoad(t *testing.T, loads <-chan *work.ProviderConversationReader) {
	t.Helper()
	select {
	case reader := <-loads:
		t.Fatalf("ended subscription loaded provider conversation with reader %p", reader)
	case <-time.After(codexConversationSubscriptionInterval + 40*time.Millisecond):
	}
}

func TestProviderActivityWireLifecycleHasOneCurrentOwner(t *testing.T) {
	running := &work.ProviderActivity{
		ID:        "provider-activity-a",
		Status:    work.ProviderActivityRunning,
		StartedAt: "2026-07-16T06:00:00Z",
	}
	conversation := work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Activity:  running,
		Events:    []work.CodexConversationEvent{},
	}
	snapshot := codexConversationSnapshotPayload(
		"subscription-1",
		"generation-1",
		"provider-agent",
		1,
		conversation,
	)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	wireConversation, ok := wire["conversation"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot conversation = %#v", wire["conversation"])
	}
	wireActivity, ok := wireConversation["activity"].(map[string]any)
	if !ok || wireActivity["id"] != running.ID || wireActivity["status"] != string(work.ProviderActivityRunning) {
		t.Fatalf("snapshot Activity = %#v", wireConversation["activity"])
	}
	for _, rejected := range []string{"active", "turn", "queued_turns", "turn_epoch", "turn_revision"} {
		if _, exists := wireConversation[rejected]; exists {
			t.Fatalf("snapshot published rejected lifecycle field %q", rejected)
		}
	}

	previous := codexConversationSubscriptionSnapshot{
		conversation: conversation,
		revision:     1,
	}
	terminalConversation := conversation
	terminalConversation.Activity = &work.ProviderActivity{
		ID:        running.ID,
		Status:    work.ProviderActivityCompleted,
		StartedAt: running.StartedAt,
		SettledAt: "2026-07-16T06:00:02Z",
	}
	terminal := codexConversationSubscriptionSnapshot{
		conversation: terminalConversation,
		revision:     2,
	}
	delta := codexConversationDeltaPayload(
		"subscription-1",
		"generation-1",
		"provider-agent",
		previous,
		terminal,
		nil,
		nil,
	)
	if got, ok := delta["activity"].(*work.ProviderActivity); !ok || got.Status != work.ProviderActivityCompleted {
		t.Fatalf("terminal delta Activity = %#v", delta["activity"])
	}
	unchanged := codexConversationDeltaPayload(
		"subscription-1",
		"generation-1",
		"provider-agent",
		terminal,
		codexConversationSubscriptionSnapshot{conversation: terminalConversation, revision: 3},
		nil,
		nil,
	)
	if _, exists := unchanged["activity"]; exists {
		t.Fatalf("unchanged delta unexpectedly published Activity: %#v", unchanged["activity"])
	}
	clearedConversation := terminalConversation
	clearedConversation.Activity = nil
	cleared := codexConversationDeltaPayload(
		"subscription-1",
		"generation-1",
		"provider-agent",
		terminal,
		codexConversationSubscriptionSnapshot{conversation: clearedConversation, revision: 3},
		nil,
		nil,
	)
	if _, exists := cleared["activity"]; !exists {
		t.Fatal("clear delta omitted Activity")
	}
	encodedClear, err := json.Marshal(cleared)
	if err != nil {
		t.Fatal(err)
	}
	var wireClear map[string]any
	if err := json.Unmarshal(encodedClear, &wireClear); err != nil {
		t.Fatal(err)
	}
	if value := wireClear["activity"]; value != nil {
		t.Fatalf("clear delta Activity = %#v; want JSON null", value)
	}
	syncStatus := codexConversationSyncStatusPayload(
		"subscription-1",
		"generation-1",
		"provider-agent",
		4,
		"session_not_ready",
	)
	if _, exists := syncStatus["activity"]; exists {
		t.Fatalf("sync status claimed Activity: %#v", syncStatus["activity"])
	}
	if got := conversationForProviderAttachment(conversation, false); got.Activity != nil {
		t.Fatalf("detached conversation retained Activity: %#v", got.Activity)
	}
	if got := conversationForProviderAttachment(conversation, true); got.Activity == nil {
		t.Fatal("attached provider conversation lost Activity")
	}
}

func TestNonCurrentBrainThreadClearsProviderActivity(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-current", "thread-history")
	srv := &Server{brain: service, calendar: calendarStore}
	conversation := work.CodexConversation{
		Available: true,
		Activity: &work.ProviderActivity{
			ID:        "provider-activity-a",
			Status:    work.ProviderActivityRunning,
			StartedAt: "2026-07-16T06:00:00Z",
		},
		Events: []work.CodexConversationEvent{{
			ID: "provider-event", Seq: 1, Kind: "assistant_message",
		}},
	}
	got := srv.brainScopedConversation(
		"brain-thread:thread-history",
		conversation,
		time.Now(),
	)
	if got.Activity != nil {
		t.Fatalf("non-current Brain thread retained Activity: %#v", got.Activity)
	}
	if len(got.Events) != 0 {
		t.Fatalf("non-current Brain thread retained provider events: %#v", got.Events)
	}
}

func TestBrainScopedConversationPreservesPartialProviderEventWithCalendar(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Daily papers", "thread-1", "Three papers.", "")
	baseTime := result.CreatedAt.Add(-time.Minute)

	partial := work.CodexConversationEvent{
		ID:        "grok-session:stream:prompt-1:assistant:1",
		Seq:       200,
		Timestamp: baseTime.Add(2 * time.Minute).Format(time.RFC3339Nano),
		Kind:      "assistant_message",
		Role:      "assistant",
		Body:      "A genuine provider chunk",
		Status:    "running",
		Partial:   true,
		Source:    "grok_updates",
	}
	srv := &Server{brain: service, calendar: calendarStore}
	got := srv.brainScopedConversation("brain-thread:thread-1", work.CodexConversation{
		Available: true,
		SessionID: "grok-session",
		Activity: &work.ProviderActivity{
			ID:        "grok-activity",
			Status:    work.ProviderActivityRunning,
			StartedAt: baseTime.Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{partial},
	}, baseTime.Add(3*time.Minute))

	if ids := fmt.Sprint(conversationEventIDs(got.Events)); ids != fmt.Sprintf("[%s grok-session:stream:prompt-1:assistant:1]", result.ID) {
		t.Fatalf("event order = %s", ids)
	}
	if got.Activity == nil || got.Activity.ID != "grok-activity" || got.Activity.Status != work.ProviderActivityRunning {
		t.Fatalf("Activity = %#v, want provider Activity preserved", got.Activity)
	}
	streamed := got.Events[1]
	if streamed.ID != partial.ID || streamed.Body != partial.Body || !streamed.Partial || streamed.Status != "running" {
		t.Fatalf("partial provider event changed during Brain overlay: %#v", streamed)
	}
	if got.Events[0].Partial {
		t.Fatalf("Calendar result must remain finalized: %#v", got.Events[0])
	}
}

func TestBrainScopedConversationOrdersCalendarResultBeforeLaterChat(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Daily Hacker News", "thread-1", "", "Linked Work is no longer observable.")
	baseTime := result.CreatedAt.Add(-time.Minute)
	srv := &Server{brain: service, calendar: calendarStore}
	base := work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{
		{ID: "user-before", Seq: 40, Timestamp: baseTime.Format(time.RFC3339Nano), Kind: "user_message", Role: "user", Body: "Before"},
		{ID: "assistant-after", Seq: 41, Timestamp: baseTime.Add(2 * time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Role: "assistant", Body: "Later answer"},
	}}

	got := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime.Add(3*time.Minute))
	if ids := conversationEventIDs(got.Events); fmt.Sprint(ids) != fmt.Sprintf("[user-before %s assistant-after]", result.ID) {
		t.Fatalf("event order = %v", ids)
	}
	event := got.Events[1]
	if event.Kind != "status" || event.Title != "Daily Hacker News failed" || event.Body != "Linked Work is no longer observable." || event.Status != "failed" {
		t.Fatalf("calendar result presentation = %#v", event)
	}
	if got.Activity != nil {
		t.Fatalf("calendar rendering synthesized Activity: %#v", got.Activity)
	}

	reloaded := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime.Add(4*time.Minute))
	if fmt.Sprint(conversationEventIDs(reloaded.Events)) != fmt.Sprint(conversationEventIDs(got.Events)) ||
		codexConversationEventsFingerprint(reloaded.Events) != codexConversationEventsFingerprint(got.Events) {
		t.Fatalf("reload changed provider/Brain timeline: first=%#v reload=%#v", got.Events, reloaded.Events)
	}
}

func TestBrainScopedConversationOrdersAndDeduplicatesCalendarResultsDeterministically(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	finishScheduledResult(t, calendarStore, "item-old", "Old", "thread-1", "", "old")
	first := finishScheduledResult(t, calendarStore, "item-a", "First", "thread-1", "first", "")
	finishScheduledResult(t, calendarStore, "item-b", "Second", "thread-1", "second", "")
	canonical := calendarStore.ScheduledResults("thread-1", 0)
	if len(canonical) != 3 {
		t.Fatalf("canonical results = %#v", canonical)
	}
	baseTime := canonical[0].CreatedAt
	latestTime := canonical[len(canonical)-1].CreatedAt
	srv := &Server{brain: service, calendar: calendarStore}
	base := work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{
		{ID: first.ID, Seq: 99, Timestamp: latestTime.Add(time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "stale duplicate"},
		{ID: "normal", Seq: 100, Timestamp: latestTime.Add(time.Minute).Format(time.RFC3339Nano), Kind: "assistant_message", Body: "normal"},
	}}

	got := srv.brainScopedConversation("brain-thread:thread-1", base, baseTime)
	want := make([]string, 0, len(canonical)+1)
	for _, result := range canonical {
		want = append(want, result.ID)
	}
	want = append(want, "normal")
	if ids := conversationEventIDs(got.Events); !reflect.DeepEqual(ids, want) {
		t.Fatalf("event order = %v, want %v", ids, want)
	}
	for idx := range canonical {
		if got.Events[idx].Seq != 0 {
			t.Fatalf("calendar projection gained provider sequence identity: %#v", got.Events)
		}
	}
	if got.Events[len(canonical)].Seq != 100 {
		t.Fatalf("merge mutated provider sequence identity: %#v", got.Events)
	}
}

func newBrainCalendarFixture(t *testing.T, currentThread string, historicalThreads ...string) (*brain.Service, *calendar.Store) {
	t.Helper()
	brainStore, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := brainStore.SetChatState(brain.ChatState{ThreadID: currentThread, ThreadIDs: historicalThreads}); err != nil {
		t.Fatal(err)
	}
	calendarStore, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return brain.NewService(brainStore, nil, nil), calendarStore
}

func finishScheduledResult(t *testing.T, store *calendar.Store, itemID, title, threadID, result, failure string) calendar.ScheduledResult {
	t.Helper()
	due := time.Now().UTC().Add(time.Hour)
	item, err := store.Create(calendar.Item{
		ID: itemID, Title: title, Kind: calendar.KindScheduledAction,
		DueAt: &due, Timezone: "UTC", Recurrence: calendar.RecurrenceNone,
		ActionInstruction: "Run", SourceThreadID: threadID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(item.ID, run.ID, result, failure); err != nil {
		t.Fatal(err)
	}
	wantID := "calendar_result:" + item.ID + ":" + run.ID
	for _, projected := range store.ScheduledResults(threadID, 0) {
		if projected.ID == wantID {
			return projected
		}
	}
	t.Fatalf("missing Calendar projection %q", wantID)
	return calendar.ScheduledResult{}
}

func conversationEventIDs(events []work.CodexConversationEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func TestCodexConversationSubscriptionFingerprintIgnoresUpdatedAt(t *testing.T) {
	firstUpdated := time.Unix(100, 0).UTC()
	secondUpdated := time.Unix(200, 0).UTC()
	base := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Updated:   &firstUpdated,
		Events: []work.CodexConversationEvent{
			{
				ID:   "session-1:10",
				Seq:  10,
				Kind: "assistant_message",
				Role: "assistant",
				Body: "done",
			},
		},
	}
	next := base
	next.Updated = &secondUpdated

	if codexConversationSubscriptionFingerprint(base) != codexConversationSubscriptionFingerprint(next) {
		t.Fatal("updated_at-only changes should not trigger a conversation delta")
	}
}

func TestCodexConversationSubscriptionFingerprintTracksProviderPathWithinLogicalScope(t *testing.T) {
	first := work.CodexConversation{
		Available: true,
		SessionID: "brain-thread:logical",
		Path:      "/rollouts/host-a.jsonl",
		Events:    []work.CodexConversationEvent{},
	}
	second := first
	second.Path = "/rollouts/host-b.jsonl"
	if codexConversationSubscriptionFingerprint(first) == codexConversationSubscriptionFingerprint(second) {
		t.Fatal("provider path replacement inside a logical scope must publish a new snapshot")
	}
}

func TestCodexConversationSubscriptionFingerprintChangesForEventContent(t *testing.T) {
	base := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Events: []work.CodexConversationEvent{
			{
				ID:   "session-1:10",
				Seq:  10,
				Kind: "assistant_message",
				Role: "assistant",
				Body: "done",
			},
		},
	}
	next := base
	next.Events = append([]work.CodexConversationEvent(nil), base.Events...)
	next.Events[0].Body = "done differently"

	if codexConversationSubscriptionFingerprint(base) == codexConversationSubscriptionFingerprint(next) {
		t.Fatal("event content changes should trigger a conversation delta")
	}
}

func TestCodexConversationEventFingerprintTracksFileChangeFacts(t *testing.T) {
	zero := 0
	one := 1
	base := work.CodexConversationEvent{
		ID:   "session-1:patch",
		Seq:  10,
		Kind: "patch",
		FileChanges: []work.CodexConversationFileChange{
			{
				Path:      "src/ledger/quote.ts",
				Operation: "update",
				Additions: &zero,
				Deletions: &zero,
			},
		},
	}
	next := base
	next.FileChanges = append([]work.CodexConversationFileChange(nil), base.FileChanges...)
	next.FileChanges[0].Additions = &one

	if codexConversationEventFingerprint(base) == codexConversationEventFingerprint(next) {
		t.Fatal("file-change facts must update the existing event in place")
	}
	if base.ID != next.ID || base.Seq != next.Seq {
		t.Fatal("file-change facts must not replace stable event identity")
	}
}

func TestCodexConversationSubscriptionFingerprintTracksCurrentActivity(t *testing.T) {
	base := work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events:    []work.CodexConversationEvent{},
	}
	running := base
	running.Activity = &work.ProviderActivity{
		ID:        "provider-activity-a",
		Status:    work.ProviderActivityRunning,
		StartedAt: "2026-07-16T06:00:00Z",
	}
	terminal := running
	terminal.Activity = &work.ProviderActivity{
		ID:        running.Activity.ID,
		Status:    work.ProviderActivityCompleted,
		StartedAt: running.Activity.StartedAt,
		SettledAt: "2026-07-16T06:00:02Z",
	}
	if codexConversationSubscriptionFingerprint(base) == codexConversationSubscriptionFingerprint(running) {
		t.Fatal("starting provider Activity did not change subscription fingerprint")
	}
	if codexConversationSubscriptionFingerprint(running) == codexConversationSubscriptionFingerprint(terminal) {
		t.Fatal("settling provider Activity did not change subscription fingerprint")
	}
}

func TestCodexConversationSubscriptionFingerprintIsCompactForLargeEvents(t *testing.T) {
	conversation := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: "session-1",
		CWD:       "/repo",
		Events: []work.CodexConversationEvent{
			{
				ID:     "session-1:10",
				Seq:    10,
				Kind:   "tool",
				Output: strings.Repeat("x", 1<<20),
			},
		},
	}

	fingerprint := codexConversationSubscriptionFingerprint(conversation)
	if len(fingerprint) > 64 {
		t.Fatalf("fingerprint len = %d, want compact hash", len(fingerprint))
	}
}

func TestCodexConversationDeltaOnlySendsTrimWindowDifference(t *testing.T) {
	previous := make([]work.CodexConversationEvent, 0, 240)
	for index := 1; index <= 240; index++ {
		previous = append(previous, work.CodexConversationEvent{
			ID:   fmt.Sprintf("session-1:%d", index),
			Seq:  index,
			Kind: "assistant_message",
			Role: "assistant",
			Body: "same",
		})
	}
	next := make([]work.CodexConversationEvent, 0, 240)
	for index := 2; index <= 241; index++ {
		next = append(next, work.CodexConversationEvent{
			ID:   fmt.Sprintf("session-1:%d", index),
			Seq:  index,
			Kind: "assistant_message",
			Role: "assistant",
			Body: "same",
		})
	}

	upserts, deletes := codexConversationDelta(codexConversationEventsByID(previous), next)
	if len(upserts) != 1 || upserts[0].Seq != 241 {
		t.Fatalf("upserts = %#v, want only the newly appended event", upserts)
	}
	if len(deletes) != 1 || deletes[0] != previous[0].ID {
		t.Fatalf("deletes = %#v, want only the trimmed first event", deletes)
	}
}

func TestCodexConversationDeltaFinalizesPartialEventInPlace(t *testing.T) {
	partial := work.CodexConversationEvent{
		ID:      "session-1:stream:answer",
		Seq:     10,
		Kind:    "assistant_message",
		Role:    "assistant",
		Body:    "Complete semantic text",
		Partial: true,
	}
	final := partial
	final.Partial = false

	if codexConversationEventFingerprint(partial) == codexConversationEventFingerprint(final) {
		t.Fatal("partial-only finalization must change the event fingerprint")
	}
	upserts, deletes := codexConversationDelta(
		codexConversationEventsByID([]work.CodexConversationEvent{partial}),
		[]work.CodexConversationEvent{final},
	)
	if len(upserts) != 1 || upserts[0].ID != partial.ID || upserts[0].Partial {
		t.Fatalf("upserts = %#v, want one finalized replacement with the same ID", upserts)
	}
	if len(deletes) != 0 {
		t.Fatalf("deletes = %#v, finalization must not delete and recreate the message", deletes)
	}
	transient := final
	transient.Transient = true
	if codexConversationEventFingerprint(final) == codexConversationEventFingerprint(transient) {
		t.Fatal("transient projection changes must participate in the event fingerprint")
	}
}
