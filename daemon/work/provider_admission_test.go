package work

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
)

func TestProviderAdmissionDigestPreservesEmbeddedWrapperMarkers(t *testing.T) {
	payloads := []string{
		"before </user_query> after",
		"before <user_query> after",
		"before </user_query> and <user_query> after\n",
		"trailing CRLF\r\n",
		"repeated blank lines\n\n\n",
		"trailing spaces and tabs \t",
	}
	for _, provider := range []string{
		AgentProviderCursor,
		AgentProviderClaude,
		AgentProviderGrok,
	} {
		for _, payload := range payloads {
			t.Run(provider+"/"+fmt.Sprintf("%x", sha256.Sum256([]byte(payload))), func(t *testing.T) {
				event := providerAdmissionFixtureEvent(t, provider, payload)
				want := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
				if event.AdmissionSHA256 != want {
					t.Fatalf("%s admission digest = %q, want %q for %q",
						provider, event.AdmissionSHA256, want, payload)
				}
			})
		}
	}
}

func providerAdmissionFixtureEvent(
	t *testing.T,
	provider string,
	payload string,
) CodexConversationEvent {
	t.Helper()
	switch provider {
	case AgentProviderCursor:
		path := filepath.Join(t.TempDir(), "cursor-admission.jsonl")
		writeJSONL(t, path, map[string]any{
			"role": "user",
			"message": map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": "<timestamp>ignored</timestamp>\n<user_query>\n" +
						payload + "\n</user_query>",
				}},
			},
		})
		conversation, err := parseCursorConversation(path)
		if err != nil {
			t.Fatal(err)
		}
		return firstAdmissionFixtureEvent(t, conversation.Events)
	case AgentProviderClaude:
		path := filepath.Join(t.TempDir(), "claude-admission.jsonl")
		writeJSONL(t, path, map[string]any{
			"type":      "user",
			"sessionId": "claude-admission",
			"uuid":      "claude-user",
			"timestamp": "2026-08-05T02:00:00.000Z",
			"message": map[string]any{
				"role":    "user",
				"content": payload,
			},
		})
		conversation, err := parseClaudeConversation(path)
		if err != nil {
			t.Fatal(err)
		}
		return firstAdmissionFixtureEvent(t, conversation.Events)
	case AgentProviderGrok:
		dir := t.TempDir()
		writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
			"info": map[string]any{"id": "grok-admission", "cwd": "/repo"},
		})
		writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
			map[string]any{"type": "user", "content": payload},
		)
		conversation, err := parseGrokConversation(dir)
		if err != nil {
			t.Fatal(err)
		}
		return firstAdmissionFixtureEvent(t, conversation.Events)
	default:
		t.Fatalf("unsupported provider %q", provider)
		return CodexConversationEvent{}
	}
}

func firstAdmissionFixtureEvent(
	t *testing.T,
	events []CodexConversationEvent,
) CodexConversationEvent {
	t.Helper()
	for _, event := range events {
		if event.Kind == "user_message" {
			return event
		}
	}
	t.Fatalf("user admission event missing: %#v", events)
	return CodexConversationEvent{}
}
