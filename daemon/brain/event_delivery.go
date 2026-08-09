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
	input := work.DirectWorkEventInput{
		EventID:            strings.TrimSpace(event.ID),
		WorkID:             strings.TrimSpace(event.WorkID),
		WorkRevision:       event.DeliveryWorkRevision,
		HostTurnID:         strings.TrimSpace(event.HandlingID),
		EventSequenceFence: event.DeliverySequenceFence,
		WorkTitle:          compactDirectWorkEventField(item.Title, directWorkEventTitleRuneLimit),
		Kind:               compactDirectWorkEventField(event.Kind, directWorkEventKindRuneLimit),
		Source:             compactDirectWorkEventField(event.SourceName, directWorkEventSourceRuneLimit),
		Summary:            compactWorkResultText(event.Summary),
		NextAction:         compactDirectWorkEventField(item.NextAction, directWorkEventNextActionRuneLimit),
		ContextRef:         compactDirectWorkEventField(item.ContextRef, directWorkEventReferenceRuneLimit),
		PayloadRef:         compactDirectWorkEventField(event.PayloadRef, directWorkEventReferenceRuneLimit),
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
