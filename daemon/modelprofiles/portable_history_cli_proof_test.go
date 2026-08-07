package modelprofiles_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// Demoted: resume/--continue starts a second OS process and is NOT the
// portable-history acceptance boundary. Use TestPortableHistorySamePID* instead
// (ZEN_PORTABLE_HISTORY_SAMEPID=1).
func TestPortableHistoryCLIProofCodex(t *testing.T) {
	t.Skip("rejected acceptance boundary: codex exec resume starts a new process; use TestPortableHistorySamePIDCodex")
}

func TestPortableHistoryCLIProofClaude(t *testing.T) {
	t.Skip("rejected acceptance boundary: claude --continue starts a new process; use TestPortableHistorySamePIDClaude")
}

// assertNoOpaqueResponsesHistory fails if opaque provider state remains after strip.
// Note: Codex may still send include=["reasoning.encrypted_content"] as a preference —
// that string must not be treated as opaque history leakage.
func assertNoOpaqueResponsesHistory(t *testing.T, body []byte) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("B body not json: %v", err)
	}
	if _, ok := obj["previous_response_id"]; ok {
		t.Fatalf("previous_response_id leaked to B")
	}
	rawInput, ok := obj["input"]
	if !ok {
		return
	}
	s := string(rawInput)
	if strings.Contains(s, `"type":"reasoning"`) || strings.Contains(s, `"type":"item_reference"`) ||
		strings.Contains(s, `"encrypted_content"`) {
		t.Fatalf("opaque input items leaked to B: %s", trimTail(s, 800))
	}
}

func assertNoOpaqueAnthropicHistory(t *testing.T, body []byte) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("B body not json: %v", err)
	}
	raw, ok := obj["messages"]
	if !ok {
		return
	}
	s := string(raw)
	if strings.Contains(s, `"type":"thinking"`) || strings.Contains(s, `"type":"redacted_thinking"`) ||
		strings.Contains(s, `"signature"`) {
		t.Fatalf("opaque thinking leaked to B: %s", trimTail(s, 800))
	}
}

func waitHits(t *testing.T, mu *sync.Mutex, sink *[][]byte, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(*sink) > 0
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gateway received no requests within %s", d)
}
