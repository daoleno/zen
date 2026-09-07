package work

import (
	"encoding/json"
	"io"
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
	// Normalize invalid legacy strings once, so every emitted envelope passes
	// its own strict parser even when JSON replaces damaged UTF-8.
	var normalized DirectWorkEventInput
	_ = json.Unmarshal(raw, &normalized)
	raw, _ = json.Marshal(normalized)
	return directWorkEventInputOpen + string(raw) + directWorkEventInputClose
}

// IsDirectWorkEventPresentationInput recognizes only a complete reserved
// envelope, including serializer-normalized historical deliveries. This is
// deliberately separate from admission's byte-exact canonical parser.
func IsDirectWorkEventPresentationInput(value string) bool {
	if !strings.HasPrefix(value, directWorkEventInputOpen) || !strings.HasSuffix(value, directWorkEventInputClose) {
		return false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(value, directWorkEventInputOpen), directWorkEventInputClose)
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return false
		}
		var field json.RawMessage
		if decoder.Decode(&field) != nil {
			return false
		}
		fields[key] = field
	}
	if _, err = decoder.Token(); err != nil {
		return false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return false
	}
	var input DirectWorkEventInput
	strict := json.NewDecoder(strings.NewReader(raw))
	strict.DisallowUnknownFields()
	if strict.Decode(&input) != nil {
		return false
	}
	canonical := FormatDirectWorkEventInput(input)
	if _, ok := ParseCanonicalDirectWorkEventInput(canonical); !ok {
		return false
	}
	var expected map[string]json.RawMessage
	encoded, _ := json.Marshal(input)
	_ = json.Unmarshal(encoded, &expected)
	if len(fields) != len(expected) {
		return false
	}
	for key, expectedValue := range expected {
		var actual any
		fieldDecoder := json.NewDecoder(strings.NewReader(string(fields[key])))
		fieldDecoder.UseNumber()
		if fieldDecoder.Decode(&actual) != nil {
			return false
		}
		encodedActual, _ := json.Marshal(actual)
		if string(encodedActual) != string(expectedValue) {
			return false
		}
	}
	return true
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
		strings.TrimSpace(input.ProviderTurnID) == "" {
		return DirectWorkEventInput{}, false
	}
	if value != FormatDirectWorkEventInput(input) {
		return DirectWorkEventInput{}, false
	}
	return input, true
}
