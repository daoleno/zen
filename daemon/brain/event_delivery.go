package brain

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/work"
)

const (
	directWorkEventInputMaxBytes       = 4096
	directWorkEventTitleRuneLimit      = 200
	directWorkEventKindRuneLimit       = 100
	directWorkEventSourceRuneLimit     = 200
	directWorkEventNextActionRuneLimit = 360
	directWorkEventReferenceRuneLimit  = 512
)

func marshalDirectWorkEventInput(action WorkReviewAction, item Work) (string, error) {
	input := work.DirectWorkEventInput{
		EventID:            strings.TrimSpace(action.EventID),
		WorkID:             strings.TrimSpace(action.WorkID),
		WorkRevision:       action.DeliveryWorkRevision,
		HandlingID:         strings.TrimSpace(action.HandlingID),
		ProviderTurnID:     strings.TrimSpace(action.ProviderTurnID),
		EventSequenceFence: action.DeliverySequenceFence,
		ResolutionRequired: true,
		ResolveCommand: fmt.Sprintf(
			"zen brain work resolve --work-id %s --handling-id %s --provider-turn-id %s --revision %d --disposition <continue|wait|complete|cancel|supersede> [--next-attempt-session-id <session> --next-attempt-turn-token <exact-accepted-turn-token>] [--wake-kind due_retry --wake-ref <source> --next-attempt-at <RFC3339>]",
			action.WorkID, action.HandlingID, action.ProviderTurnID, action.DeliveryWorkRevision,
		),
		WorkTitle:  compactDirectWorkEventField(item.Title, directWorkEventTitleRuneLimit),
		Kind:       compactDirectWorkEventField(action.Kind, directWorkEventKindRuneLimit),
		Source:     compactDirectWorkEventField(action.SourceName, directWorkEventSourceRuneLimit),
		Summary:    compactWorkResultText(action.Summary),
		NextAction: compactDirectWorkEventField(item.NextAction, directWorkEventNextActionRuneLimit),
		ContextRef: compactDirectWorkEventField(item.ContextRef, directWorkEventReferenceRuneLimit),
		PayloadRef: compactDirectWorkEventField(action.PayloadRef, directWorkEventReferenceRuneLimit),
	}
	payload := work.FormatDirectWorkEventInput(input)
	if len(payload) > directWorkEventInputMaxBytes {
		return "", fmt.Errorf("direct Work Event input exceeds %d bytes", directWorkEventInputMaxBytes)
	}
	return payload, nil
}

func compactDirectWorkEventField(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
