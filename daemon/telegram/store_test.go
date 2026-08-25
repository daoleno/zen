package telegram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreMigratesSchemaOneAndPreservesState(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := durableState{
		Schema: 1, Enabled: true, BotID: 7001, OwnerID: 99, ChatID: 99, NextOffset: 42,
		Processed:    map[string]updateRecord{"41": {Disposition: "accepted", HandledAt: time.Now()}},
		Projection:   map[string]string{"outbox:assistant:one:0": "digest"},
		WorkMessages: map[string]int64{"work-one": 7},
		Outbox:       []outboxRecord{{ID: "legacy", Kind: "send", Text: "legacy text", State: "pending"}},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	state := opened.snapshot()
	if state.Schema != 3 || !state.Enabled || state.BotID != 7001 || state.OwnerID != 99 || state.NextOffset != 42 ||
		state.Processed["41"].Disposition != "accepted" || state.Projection["outbox:assistant:one:0"] != "digest" || state.WorkMessages["work-one"] != 7 {
		t.Fatalf("migration lost state: %+v", state)
	}
	if row := state.Outbox[0]; row.Text != "legacy text" || row.PlainText != "legacy text" || row.Variant != plainVariant || len(row.Entities) != 0 {
		t.Fatalf("legacy row=%+v", row)
	}
	if len(state.Topics) != 0 || len(state.TopicOps) != 0 || state.TopicProjection == nil || state.TopicMessages == nil {
		t.Fatalf("schema-3 topic maps missing: %+v", state)
	}
}

func TestStoreMigratesSchemaTwoPreservingCurrentSlice(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	current := durableState{
		Schema: 2, Enabled: true, BotID: 7001, OwnerID: 77, ChatID: 77, NextOffset: 9,
		Processed:         map[string]updateRecord{"8": {Disposition: "accepted"}},
		Projection:        map[string]string{"work:w1": "d1"},
		WorkMessages:      map[string]int64{"w1": 3},
		DeliveryStartedAt: now,
		Outbox: []outboxRecord{{
			ID: "formatted", Kind: "send", Text: "rich", PlainText: "rich",
			Entities: []MessageEntity{{Type: "bold", Offset: 0, Length: 4}},
			Variant:  formattedVariant, State: "sent", MessageID: 3, CreatedAt: now,
		}},
	}
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	state := opened.snapshot()
	if state.Schema != 3 || state.Enabled != true || state.BotID != 7001 || state.OwnerID != 77 || state.ChatID != 77 ||
		state.NextOffset != 9 || state.DeliveryStartedAt != now ||
		state.Projection["work:w1"] != "d1" || state.WorkMessages["w1"] != 3 ||
		state.Processed["8"].Disposition != "accepted" {
		t.Fatalf("schema-2 migration lost state: %+v", state)
	}
	if row := state.Outbox[0]; row.ID != "formatted" || row.Variant != formattedVariant || row.State != "sent" || row.MessageID != 3 || len(row.Entities) != 1 {
		t.Fatalf("schema-2 row damaged: %+v", row)
	}
}

func TestStorePersistsTopicStateAndMakesDispatchAmbiguousOnRestart(t *testing.T) {
	root := t.TempDir()
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 30, 0, 0, time.UTC)
	if err := opened.mutate(func(state *durableState) error {
		state.Topics = []topicMapping{{
			SessionID: "session-1", ThreadID: "thread-1", WorkID: "work-1", ChatID: 77,
			MessageThreadID: 42, Label: "Session one", State: topicStateActive, CreatedAt: now,
		}}
		state.TopicOps = []topicOpRecord{{
			ID: "topic:create:session-2", Kind: topicOpCreate, SessionID: "session-2",
			State: "dispatching", CreatedAt: now,
		}}
		state.TopicProjection["topic:msg:session-1:e1"] = "digest"
		state.TopicMessages["topic:msg:session-1:e1"] = 9
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].SessionID != "session-1" || state.Topics[0].MessageThreadID != 42 || state.Topics[0].State != topicStateActive {
		t.Fatalf("topic mapping lost: %+v", state.Topics)
	}
	if len(state.TopicOps) != 1 || state.TopicOps[0].State != "ambiguous" || state.TopicOps[0].Kind != topicOpCreate {
		t.Fatalf("dispatching topic op did not become ambiguous: %+v", state.TopicOps)
	}
	if state.TopicProjection["topic:msg:session-1:e1"] != "digest" || state.TopicMessages["topic:msg:session-1:e1"] != 9 {
		t.Fatalf("topic checkpoint maps lost: %+v", state.TopicProjection)
	}
}

func TestStorePersistsFormattedRowAndMakesDispatchAmbiguousOnRestart(t *testing.T) {
	root := t.TempDir()
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	entity := MessageEntity{Type: "bold", Offset: 0, Length: 4}
	if err := opened.mutate(func(state *durableState) error {
		state.Outbox = []outboxRecord{{
			ID: "formatted", Kind: "send", Text: "rich", PlainText: "rich", Entities: []MessageEntity{entity},
			Variant: formattedVariant, State: "dispatching", CreatedAt: time.Now(),
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	row := reopened.snapshot().Outbox[0]
	if row.State != "ambiguous" || row.Variant != formattedVariant || row.PlainText != "rich" || len(row.Entities) != 1 || row.Entities[0] != entity {
		t.Fatalf("restarted row=%+v", row)
	}
}
