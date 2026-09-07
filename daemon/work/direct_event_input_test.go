package work

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDirectWorkPresentationNormalizationDoesNotRelaxAdmission(t *testing.T) {
	input := DirectWorkEventInput{EventID: "event-1", WorkID: "work-1", WorkRevision: 9007199254740993, HandlingID: "handling-1", ProviderTurnID: "turn-1", Summary: "中文�..."}
	canonical := FormatDirectWorkEventInput(input)
	raw, _ := json.Marshal(input)
	pretty, _ := json.MarshalIndent(input, "", "  ")
	variants := []string{canonical, strings.ReplaceAll(canonical, "�", `\ufffd`), directWorkEventInputOpen + string(pretty) + directWorkEventInputClose, strings.Replace(canonical, `{"event_id":"event-1","work_id":"work-1"`, `{"work_id":"work-1","event_id":"event-1"`, 1), strings.ReplaceAll(canonical, "中文", `\u4e2d\u6587`)}
	for _, value := range variants {
		if !IsDirectWorkEventPresentationInput(value) {
			t.Fatalf("normalized internal input not recognized: %q", value)
		}
		if _, ok := ParseCanonicalDirectWorkEventInput(value); ok != (value == canonical) {
			t.Fatal("admission lost byte exactness")
		}
	}
	negative := []string{
		"Example:\n" + canonical, "```json\n" + canonical + "\n```", canonical + "\nPlease explain", "\n" + canonical,
		strings.Replace(canonical, `"summary":`, `"extra":true,"summary":`, 1),
		strings.Replace(canonical, `"event_id":"event-1",`, "", 1),
		strings.Replace(canonical, `"event_id":"event-1",`, `"event_id":"event-1","event_id":"event-1",`, 1),
		strings.Replace(canonical, `"summary":"中文�..."`, `"summary":null`, 1),
		directWorkEventInputOpen + string(raw) + " {}" + directWorkEventInputClose,
	}
	for _, value := range negative {
		if IsDirectWorkEventPresentationInput(value) {
			t.Fatalf("user or ambiguous input hidden: %q", value)
		}
		got := SanitizeConversationProjection(CodexConversation{Events: []CodexConversationEvent{{Kind: "user_message", Body: value}, {Kind: "assistant_message", Body: value}}})
		if len(got.Events) != 2 {
			t.Fatalf("legitimate content removed: %q", value)
		}
	}
}

func TestFormatterRoundTripsLegacyInvalidUTF8(t *testing.T) {
	input := DirectWorkEventInput{EventID: "event", WorkID: "work", WorkRevision: 1, HandlingID: "handling", ProviderTurnID: "turn", Summary: "prefix\xe4\xb8..."}
	value := FormatDirectWorkEventInput(input)
	parsed, ok := ParseCanonicalDirectWorkEventInput(value)
	if !ok || FormatDirectWorkEventInput(parsed) != value {
		t.Fatal("formatter rejected its own legacy input")
	}
}
