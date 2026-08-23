package brain

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/work"
)

func TestDirectWorkEventInputIsDeterministicBoundedAndComplete(t *testing.T) {
	event := WorkReviewAction{
		EventID:               "event-direct-1",
		WorkID:                "work-direct-1",
		Kind:                  "session.done",
		SourceName:            "Worker One",
		Summary:               "Completed the exact requested implementation.",
		PayloadRef:            "session:brain-agent-worker:@1",
		HandlingID:            "handling-direct-1",
		ProviderTurnID:        "provider-turn-direct-1",
		DeliveryWorkRevision:  7,
		DeliverySequenceFence: 11,
	}
	item := Work{
		ID:         event.WorkID,
		Title:      "Implement direct delivery",
		Objective:  "This full objective must never be delivered.",
		NextAction: "Review the compact result and decide the Work state.",
		ContextRef: "worklog:direct-delivery.md",
	}

	first, err := marshalDirectWorkEventInput(event, item)
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalDirectWorkEventInput(event, item)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("direct input is not deterministic:\nfirst=%q\nsecond=%q", first, second)
	}
	if len(first) > directWorkEventInputMaxBytes {
		t.Fatalf("direct input bytes=%d, limit=%d", len(first), directWorkEventInputMaxBytes)
	}
	if strings.Contains(first, item.Objective) ||
		strings.Contains(first, "delivery_token") ||
		strings.Contains(first, "zen brain "+"event") {
		t.Fatalf("direct input leaked forbidden content: %q", first)
	}
	got := decodeDirectWorkEventInput(t, first)
	want := work.DirectWorkEventInput{
		EventID:            "event-direct-1",
		WorkID:             item.ID,
		WorkRevision:       event.DeliveryWorkRevision,
		HandlingID:         event.HandlingID,
		ProviderTurnID:     event.ProviderTurnID,
		EventSequenceFence: event.DeliverySequenceFence,
		ResolutionRequired: true,
		ResolveCommand:     "For wait, complete, or cancel: zen brain work resolve --work-id work-direct-1 --handling-id handling-direct-1 --provider-turn-id provider-turn-direct-1 --revision 7 --disposition <wait|complete|cancel> [--wake-kind due_retry --wake-ref <source> --next-attempt-at <RFC3339>]. For continue, first run zen agent send -id <session> -text <scoped-follow-up> --work-id work-direct-1 --event-id event-direct-1 --handling-id handling-direct-1 --provider-turn-id provider-turn-direct-1 --revision 7 --turn-id <one-random-turn-id>; only after exact acceptance run zen brain work resolve --work-id work-direct-1 --handling-id handling-direct-1 --provider-turn-id provider-turn-direct-1 --revision 7 --disposition continue --next-attempt-session-id <session> --next-attempt-turn-token <exact-accepted-turn-token>.",
		WorkTitle:          item.Title,
		Kind:               event.Kind,
		Source:             event.SourceName,
		Summary:            event.Summary,
		NextAction:         item.NextAction,
		ContextRef:         item.ContextRef,
		PayloadRef:         event.PayloadRef,
	}
	if got != want {
		t.Fatalf("direct input = %#v, want %#v", got, want)
	}
}

func decodeDirectWorkEventInput(t *testing.T, payload string) work.DirectWorkEventInput {
	t.Helper()
	input, ok := work.ParseCanonicalDirectWorkEventInput(payload)
	if !ok {
		t.Fatalf("unexpected direct input = %q", payload)
	}
	return input
}

func TestDirectWorkEventInputCompactsDescriptiveFieldsWithoutChangingIdentity(t *testing.T) {
	event := WorkReviewAction{
		EventID: "event-exact-identity", WorkID: "work-exact-identity",
		HandlingID: "handling-exact-identity", ProviderTurnID: "provider-turn-exact-identity",
		DeliveryWorkRevision: 1, Kind: strings.Repeat("kind ", 100),
		SourceName: strings.Repeat("source ", 100), Summary: strings.Repeat("summary ", 200),
		PayloadRef: strings.Repeat("payload/", 200),
	}
	item := Work{
		ID:         event.WorkID,
		Title:      strings.Repeat("title ", 200),
		Objective:  strings.Repeat("secret objective ", 500),
		NextAction: strings.Repeat("next ", 300),
		ContextRef: strings.Repeat("context/", 200),
	}

	payload, err := marshalDirectWorkEventInput(event, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > directWorkEventInputMaxBytes || !utf8.ValidString(payload) {
		t.Fatalf("direct input bytes=%d valid_utf8=%v", len(payload), utf8.ValidString(payload))
	}
	if !strings.Contains(payload, event.EventID) || !strings.Contains(payload, event.WorkID) {
		t.Fatalf("identity changed in compact input: %q", payload)
	}
	if strings.Contains(payload, item.Objective) {
		t.Fatal("full objective leaked into compact input")
	}
}
