package server

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainUnifiedTimelineRestoresHistoryAcrossEmptyHost(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-live"}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	srv := &Server{brain: service}

	seed := work.CodexConversation{
		Available: true,
		SessionID: "host-old",
		Events: []work.CodexConversationEvent{
			{ID: "u1", Timestamp: "2026-08-06T04:00:00Z", Kind: "user_message", Role: "user", Body: "ordinary history"},
			{ID: "a1", Timestamp: "2026-08-06T04:00:01Z", Kind: "assistant_message", Role: "assistant", Body: "still here"},
		},
	}
	first := srv.brainScopedConversation("brain-thread:thread-live", seed, time.Now())
	if len(first.Events) != 2 {
		t.Fatalf("seed = %#v", first.Events)
	}

	item, err := store.CreateWork(brain.Work{
		Title:            "zen-pi-opencode-first-class-acceptance",
		Objective:        "acceptance",
		Status:           brain.WorkRunning,
		CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: "645a5a2a-reject", WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:reject:turn:one:session.failed", Actionable: false, Summary: "REJECT",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: "1a6ddd99-accept", WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:accept:turn:one:session.done", Actionable: false, Summary: "ACCEPT",
	}); err != nil {
		t.Fatal(err)
	}

	needs, err := store.CreateWork(brain.Work{
		Title:            "current-needs-input",
		Objective:        "choose",
		Status:           brain.WorkWaiting,
		CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: "current-needs", WorkID: needs.ID, Kind: "session.needs_input",
		DedupeKey: "session:need:turn:one:session.needs_input", Actionable: false, Summary: "go vet ./...",
	}); err != nil {
		t.Fatal(err)
	}

	// Empty provider host after rotation must restore ordinary chat history
	// from the durable ledger and keep every materialized card visible.
	got := srv.brainScopedConversation("brain-thread:thread-live", work.CodexConversation{
		Available: true,
		SessionID: "host-new",
		Events:    nil,
	}, time.Now())
	if !got.Available || got.SessionID != "brain-thread:thread-live" {
		t.Fatalf("scoped = %#v", got)
	}
	ids := conversationEventIDs(got.Events)
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for _, wantID := range []string{"u1", "a1", "645a5a2a-reject", "1a6ddd99-accept", "current-needs"} {
		if !idSet[wantID] {
			t.Fatalf("missing %s in restored timeline %v", wantID, ids)
		}
	}
	var (
		needsCard                 *work.CodexConversationEvent
		userIndex, assistantIndex int
	)
	userIndex, assistantIndex = -1, -1
	for index := range got.Events {
		switch got.Events[index].ID {
		case "current-needs":
			needsCard = &got.Events[index]
		case "u1":
			userIndex = index
		case "a1":
			assistantIndex = index
		}
	}
	if needsCard == nil || needsCard.Source != "work_result" || needsCard.Status != "session.needs_input" || !needsCard.Unread {
		t.Fatalf("needs_input card = %#v", needsCard)
	}
	if userIndex < 0 || assistantIndex < 0 || userIndex > assistantIndex {
		t.Fatalf("chat history order broken: %v", ids)
	}
	if got.Events[userIndex].Body != "ordinary history" || got.Events[assistantIndex].Body != "still here" {
		t.Fatalf("chat history missing under empty host: %#v", got.Events)
	}
}

func TestBrainCardOnlyNewThreadIsValid(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-cards-only"}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	srv := &Server{brain: service}

	item, err := store.CreateWork(brain.Work{
		Title: "only-cards", Objective: "no chat", Status: brain.WorkWaiting,
		CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: "card-1", WorkID: item.ID, Kind: "session.needs_input",
		DedupeKey: "session:n:turn:one:session.needs_input", Actionable: false, Summary: "card only",
	}); err != nil {
		t.Fatal(err)
	}
	got := srv.brainScopedConversation("brain-thread:thread-cards-only", work.CodexConversation{
		Available: true, SessionID: "host-empty", Events: nil,
	}, time.Now())
	found := false
	for _, event := range got.Events {
		if event.Source == "work_result" && event.ID == "card-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("genuine card-only thread must present work_result: %#v", got.Events)
	}
}

func TestBrainWorkEventStaysOnFrozenThreadAcrossScopedReads(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{
		ThreadID:  "thread-a",
		ThreadIDs: []string{"thread-a"},
	}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	srv := &Server{brain: service}

	item, err := store.CreateWork(brain.Work{
		Title: "A work", Objective: "freeze A", Status: brain.WorkRunning,
		CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{
		ThreadID:  "thread-b",
		ThreadIDs: []string{"thread-a", "thread-b"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: "stay-a", WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:stay:turn:one:session.done", Actionable: false, Summary: "belongs to A",
	}); err != nil {
		t.Fatal(err)
	}

	threadA := srv.brainScopedConversation("brain-thread:thread-a", work.CodexConversation{
		Available: true, SessionID: "host", Events: nil,
	}, time.Now())
	threadB := srv.brainScopedConversation("brain-thread:thread-b", work.CodexConversation{
		Available: true, SessionID: "host", Events: nil,
	}, time.Now())
	if !containsConversationEventID(threadA.Events, "stay-a") {
		t.Fatalf("thread A missing card: %#v", threadA.Events)
	}
	if containsConversationEventID(threadB.Events, "stay-a") {
		t.Fatalf("thread B must not receive A's card: %#v", threadB.Events)
	}
}

func containsConversationEventID(events []work.CodexConversationEvent, id string) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}
