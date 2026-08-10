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

func marshalDirectWorkEventInput(event WorkEvent, item Work) (string, error) {
	reviewKind := firstNonEmpty(event.ReviewKind, event.Kind)
	reviewSource := firstNonEmpty(event.ReviewSourceName, event.SourceName)
	reviewSummary := firstNonEmpty(event.ReviewSummary, event.Summary)
	reviewPayloadRef := firstNonEmpty(event.ReviewPayloadRef, event.PayloadRef)
	input := work.DirectWorkEventInput{
		EventID:            strings.TrimSpace(event.ID),
		WorkID:             strings.TrimSpace(event.WorkID),
		WorkRevision:       event.DeliveryWorkRevision,
		HandlingID:         strings.TrimSpace(event.HandlingID),
		ProviderTurnID:     strings.TrimSpace(event.ProviderTurnID),
		EventSequenceFence: event.DeliverySequenceFence,
		ResolutionRequired: true,
		ResolveCommand: fmt.Sprintf(
			"zen brain work resolve --event-id %s --handling-id %s --provider-turn-id %s --revision %d --disposition <continue|wait|complete|cancel|supersede>",
			event.ID, event.HandlingID, event.ProviderTurnID, event.DeliveryWorkRevision,
		),
		WorkTitle:  compactDirectWorkEventField(item.Title, directWorkEventTitleRuneLimit),
		Kind:       compactDirectWorkEventField(reviewKind, directWorkEventKindRuneLimit),
		Source:     compactDirectWorkEventField(reviewSource, directWorkEventSourceRuneLimit),
		Summary:    compactWorkResultText(reviewSummary),
		NextAction: compactDirectWorkEventField(item.NextAction, directWorkEventNextActionRuneLimit),
		ContextRef: compactDirectWorkEventField(item.ContextRef, directWorkEventReferenceRuneLimit),
		PayloadRef: compactDirectWorkEventField(reviewPayloadRef, directWorkEventReferenceRuneLimit),
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
