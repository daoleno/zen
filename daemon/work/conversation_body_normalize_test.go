package work

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeConversationEventBody_MessagesUncappedToolsCapped(t *testing.T) {
	const suffix = "DISTINCT_MESSAGE_SUFFIX_OK"
	long := strings.Repeat("x", maxCodexConversationBody+400) + "\n4. **Verdict owner & effect**\n" + suffix

	for _, kind := range []string{"user_message", "assistant_message"} {
		got := normalizeConversationEventBody(kind, long)
		if strings.HasSuffix(got, "...") {
			t.Fatalf("%s body ends with silent ellipsis", kind)
		}
		if !strings.HasSuffix(got, suffix) {
			t.Fatalf("%s body lost suffix %q; tail=%q", kind, suffix, got[max(0, len(got)-80):])
		}
		if utf8.RuneCountInString(got) <= maxCodexConversationBody {
			t.Fatalf("%s runes = %d, want > %d", kind, utf8.RuneCountInString(got), maxCodexConversationBody)
		}
	}

	for _, kind := range []string{"tool", "command", "status", "patch", "web_search", "commentary"} {
		got := normalizeConversationEventBody(kind, long)
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("%s body should stay payload-capped with ellipsis; got tail=%q", kind, got[max(0, len(got)-48):])
		}
		if utf8.RuneCountInString(got) > maxCodexConversationBody {
			t.Fatalf("%s runes = %d, want <= %d", kind, utf8.RuneCountInString(got), maxCodexConversationBody)
		}
		if strings.Contains(got, suffix) {
			t.Fatalf("%s body unexpectedly retained uncapped suffix", kind)
		}
	}
}

// longCompletedAssistantMarkdown builds a completed Markdown body past the old
// 8000-rune silent-ellipsis cutoff, ending with an exact distinctive suffix.
func longCompletedAssistantMarkdown(suffix string) string {
	return strings.Repeat("m", maxCodexConversationBody-40) +
		"\n\n## Smallest product decisions before implementation\n\n" +
		"4. **Verdict owner & effect** — who persists?\n" +
		"5. **Intent & evidence sources** — which facts?\n" +
		"6. **Files browser role** — context vs demoted?\n" +
		"7. **Patch section contract** — aggregate vs split?\n\n" +
		"## Recommended first vertical slice (no code)\n\n" +
		suffix
}

func assertUncappedAssistantMarkdown(t *testing.T, events []CodexConversationEvent, suffix string) {
	t.Helper()
	var body string
	for _, event := range events {
		if event.Kind == "assistant_message" && strings.Contains(event.Body, suffix) {
			body = event.Body
			break
		}
	}
	if body == "" {
		t.Fatalf("missing uncapped assistant_message with suffix %q: %#v", suffix, events)
	}
	if strings.HasSuffix(body, "...") {
		t.Fatalf("assistant body still ends with silent ellipsis: %q", body[max(0, len(body)-64):])
	}
	if !strings.HasSuffix(body, suffix) {
		t.Fatalf("assistant body lost exact suffix %q; tail=%q", suffix, body[max(0, len(body)-96):])
	}
	for _, needle := range []string{
		"4. **Verdict owner & effect**",
		"5. **Intent & evidence sources**",
		"Recommended first vertical slice",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("assistant body missing %q", needle)
		}
	}
	if utf8.RuneCountInString(body) <= maxCodexConversationBody {
		t.Fatalf("assistant runes = %d, want uncapped > %d", utf8.RuneCountInString(body), maxCodexConversationBody)
	}
}
