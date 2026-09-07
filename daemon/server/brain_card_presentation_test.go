package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainCardPresentationSubscriptionAndReconnect(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const thread = "card-subscription-thread"
	if err := store.SetChatState(brain.ChatState{ThreadID: thread}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "中文研究整理", Objective: "Preserve research", Status: brain.WorkRunning})
	if err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	event, _, err := service.AppendWorkEvent(brain.WorkEvent{WorkID: item.ID, Kind: "session.done", Summary: "中文研究材料\ufffd\ufffd...", DedupeKey: "session:worker:turn:one:session.done", DetailsJSON: `{"offline_tests":22,"replay_rows":5}`})
	if err != nil {
		t.Fatal(err)
	}
	input := work.DirectWorkEventInput{EventID: event.ID, WorkID: item.ID, WorkRevision: 1, HandlingID: "handling", ProviderTurnID: "turn", Summary: event.Summary}
	legacy := strings.ReplaceAll(work.FormatDirectWorkEventInput(input), "\ufffd", `\ufffd`)
	if _, err := store.AppendTimelineItem(brain.TimelineItem{ID: "legacy-raw", ThreadID: thread, SessionID: "host", Role: "user", Kind: "user_message", Body: legacy, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	provider := work.CodexConversation{Available: true, SessionID: "provider-session", Source: "codex", Events: []work.CodexConversationEvent{
		{ID: "live-raw", Kind: "user_message", Body: legacy, Timestamp: "2026-09-08T00:00:00Z"},
		{ID: "quoted", Kind: "user_message", Body: "Please explain:\n" + legacy, Timestamp: "2026-09-08T00:00:01Z"},
		{ID: "actual-result", Kind: "assistant_message", Body: "中文结果保持完整。", Timestamp: "2026-09-08T00:00:02Z"},
	}}
	for reconnect := 0; reconnect < 2; reconnect++ {
		if reconnect > 0 {
			store, err = brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			service = brain.NewService(store, nil, nil)
			provider.Events = nil
		}
		snapshot := provider
		srv := &Server{brain: service, watcher: watcher.New(time.Second), providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) { return snapshot, nil }}
		conn := openThinProxyTestSocket(t, srv)
		if err := conn.WriteJSON(clientMessage{Type: "codex_conversation_subscribe", RequestID: "cards", TargetID: "provider-agent", Command: "codex", StartedAt: json.RawMessage(`"2026-09-08T00:00:00Z"`), ConversationScopeKey: "brain-thread:" + thread}); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var response struct {
			Type         string                 `json:"type"`
			Conversation work.CodexConversation `json:"conversation"`
		}
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if response.Type != "codex_conversation_snapshot" || len(response.Conversation.Events) != 3 {
			t.Fatalf("reconnect=%d response=%+v", reconnect, response)
		}
		cards := 0
		for _, row := range response.Conversation.Events {
			if row.ID == "legacy-raw" || row.ID == "live-raw" {
				t.Fatal("raw envelope leaked over production WS handler")
			}
			if row.Source == "work_result" {
				cards++
				if row.Body != "中文研究材料..." || row.WorkDetailsJSON != event.DetailsJSON {
					t.Fatalf("card=%+v", row)
				}
			}
			if row.ID == "actual-result" && row.Body != "中文结果保持完整。" {
				t.Fatal("assistant result changed")
			}
		}
		if cards != 1 {
			t.Fatalf("cards=%d", cards)
		}
	}
}
