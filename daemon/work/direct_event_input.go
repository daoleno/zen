package work

import (
	"encoding/json"
	"strings"
)

const (
	directWorkEventInputOpen  = "<zen_work_event>\n"
	directWorkEventInputClose = "\n</zen_work_event>"
)

// DirectWorkEventInput is the complete provider-neutral internal input shape.
// It is transport data only; Work and Event remain the durable product owners.
type DirectWorkEventInput struct {
	EventID            string `json:"event_id"`
	WorkID             string `json:"work_id"`
	WorkRevision       uint64 `json:"work_revision"`
	HandlingID         string `json:"handling_id"`
	ProviderTurnID     string `json:"provider_turn_id"`
	EventSequenceFence uint64 `json:"event_sequence_fence,omitempty"`
	ResolutionRequired bool   `json:"resolution_required"`
	ResolveCommand     string `json:"resolve_command"`
	WorkTitle          string `json:"work_title"`
	Kind               string `json:"kind"`
	Source             string `json:"source"`
	Summary            string `json:"summary"`
	NextAction         string `json:"next_action"`
	ContextRef         string `json:"context_ref"`
	PayloadRef         string `json:"payload_ref"`
}

func FormatDirectWorkEventInput(input DirectWorkEventInput) string {
	raw, _ := json.Marshal(input)
	return directWorkEventInputOpen + string(raw) + directWorkEventInputClose
}

func ParseCanonicalDirectWorkEventInput(value string) (DirectWorkEventInput, bool) {
	if !strings.HasPrefix(value, directWorkEventInputOpen) ||
		!strings.HasSuffix(value, directWorkEventInputClose) {
		return DirectWorkEventInput{}, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(value, directWorkEventInputOpen), directWorkEventInputClose)
	var input DirectWorkEventInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil ||
		strings.TrimSpace(input.EventID) == "" ||
		strings.TrimSpace(input.WorkID) == "" || input.WorkRevision == 0 ||
		strings.TrimSpace(input.HandlingID) == "" ||
		strings.TrimSpace(input.ProviderTurnID) == "" ||
		!input.ResolutionRequired || strings.TrimSpace(input.ResolveCommand) == "" {
		return DirectWorkEventInput{}, false
	}
	if value != FormatDirectWorkEventInput(input) {
		return DirectWorkEventInput{}, false
	}
	return input, true
}
