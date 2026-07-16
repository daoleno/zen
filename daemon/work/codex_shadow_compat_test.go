package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/daoleno/zen/daemon/chatthread"
	"github.com/daoleno/zen/daemon/codexshadow"
)

func TestCodexShadowReaderLeavesLegacyParserProjectionByteIdentical(t *testing.T) {
	fixturePath := filepath.Join("..", "codexshadow", "testdata", "one_task_five_inputs.jsonl")
	rawBefore, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	legacyBefore, err := parseCodexConversation(fixturePath)
	if err != nil {
		t.Fatalf("parse legacy conversation: %v", err)
	}
	encodedBefore, err := json.Marshal(legacyBefore)
	if err != nil {
		t.Fatal(err)
	}
	assertLegacyOneTaskFiveInputs(t, legacyBefore)

	store, err := chatthread.InitializeShadowStore(filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("InitializeShadowStore: %v", err)
	}
	reader, err := codexshadow.NewReader(store, []string{"agent:fixture"})
	if err != nil {
		t.Fatal(err)
	}
	legacyTurn := chatthread.LegacyShadowTurn{ID: legacyBefore.Turn.ID, State: legacyBefore.Turn.Status}
	shadow, err := reader.ObserveRollout(context.Background(), codexshadow.Observation{
		OwnerKey:    "agent:fixture",
		RolloutPath: fixturePath,
		SessionID:   legacyBefore.SessionID,
		Legacy: chatthread.LegacyShadowProjection{
			OrderedTurns:  []chatthread.LegacyShadowTurn{legacyTurn},
			TerminalState: legacyBefore.Turn.Status,
		},
	})
	if err != nil {
		t.Fatalf("ObserveRollout: %v", err)
	}
	if len(shadow.Thread.ExecutionActivities) != 1 || len(shadow.Thread.Submissions) != 5 ||
		len(shadow.Thread.Events) != 5 || shadow.Thread.CurrentExecutionID != "" ||
		len(shadow.Thread.QueuedSubmissionIDs) != 0 {
		t.Fatalf("shared fixture shadow cardinality/lifecycle = %#v", shadow.Thread)
	}

	legacyAfter, err := parseCodexConversation(fixturePath)
	if err != nil {
		t.Fatalf("parse legacy conversation after shadow: %v", err)
	}
	encodedAfter, err := json.Marshal(legacyAfter)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encodedBefore, encodedAfter) {
		t.Fatalf("legacy JSON projection changed after shadow read\nbefore: %s\nafter:  %s", encodedBefore, encodedAfter)
	}
	rawAfter, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rawBefore, rawAfter) {
		t.Fatalf("shadow reader changed source rollout bytes")
	}
}

func assertLegacyOneTaskFiveInputs(t *testing.T, conversation CodexConversation) {
	t.Helper()
	if conversation.Turn == nil || conversation.Turn.Status != CodexConversationTurnCompleted ||
		len(conversation.ProviderTurns) != 1 {
		t.Fatalf("legacy lifecycle projection = turn %#v provider turns %#v", conversation.Turn, conversation.ProviderTurns)
	}
	users := 0
	repeated := 0
	for _, event := range conversation.Events {
		if event.Kind != "user_message" {
			continue
		}
		users++
		if event.Body == "same body" {
			repeated++
		}
	}
	// Rendering must preserve every provider input boundary. Equal bodies are
	// independent Submissions; the paired response_item echo is the duplicate,
	// never another event_msg/user_message with the same text.
	if users != 5 || repeated != 3 {
		t.Fatalf("visible user projection = %d users / %d repeated, want 5 / 3", users, repeated)
	}
}
