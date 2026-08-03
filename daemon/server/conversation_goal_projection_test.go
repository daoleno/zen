package server

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func TestConversationAPIProjectionHidesTypedGoalContext(t *testing.T) {
	srv := &Server{
		providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
			return work.CodexConversation{
				Available: true,
				Events: []work.CodexConversationEvent{
					{
						ID:     "typed-goal",
						Kind:   "status",
						Source: "goal",
						Title:  "codex_internal_context",
						Body:   "hidden objective",
					},
					{
						ID:   "answer",
						Kind: "assistant_message",
						Body: "Visible\n<codex_internal_context source=\"goal\">hidden objective</codex_internal_context>\nAnswer",
					},
				},
			}, nil
		},
	}

	conversation, err := srv.loadProviderConversationSnapshot(
		work.NewProviderConversationReader(),
		resolvedCodexConversationAgent{targetID: "agent-1", ready: true},
		time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.Events) != 1 ||
		conversation.Events[0].ID != "answer" ||
		conversation.Events[0].Body != "Visible\n\nAnswer" {
		t.Fatalf("API conversation = %#v", conversation)
	}
	raw := conversation.Events[0].Body
	if strings.Contains(raw, "codex_internal_context") || strings.Contains(raw, "hidden objective") {
		t.Fatalf("typed Goal context leaked through API: %q", raw)
	}
}
