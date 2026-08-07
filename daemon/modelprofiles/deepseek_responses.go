package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// deepSeekRejectHook receives secret-free diagnostics when sanitize rejects.
// Production leaves this nil. Tests may install a bounded capture.
var (
	deepSeekRejectMu   sync.Mutex
	deepSeekRejectHook func(path string, structure map[string]any)
)

// SetDeepSeekSanitizeRejectHook installs a test-only capture for rejected
// DeepSeek Responses requests. structure is secret-free JSON shape only
// (keys/types/enum-ish scalars); never request text or secrets.
func SetDeepSeekSanitizeRejectHook(hook func(path string, structure map[string]any)) {
	deepSeekRejectMu.Lock()
	deepSeekRejectHook = hook
	deepSeekRejectMu.Unlock()
}

func reportDeepSeekSanitizeReject(path string, body []byte) {
	deepSeekRejectMu.Lock()
	hook := deepSeekRejectHook
	deepSeekRejectMu.Unlock()
	if hook == nil {
		return
	}
	hook(path, secretFreeJSONStructure(body))
}

// secretFreeJSONStructure sketches JSON shape without copying string payloads
// that may contain user text or secrets. Arrays become [{…}] samples; objects
// keep keys; numbers/bools/null are kept; strings become "<string:N>".
func secretFreeJSONStructure(body []byte) map[string]any {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return map[string]any{"_decode_error": true}
	}
	sketched, _ := sketchJSONValue(v).(map[string]any)
	if sketched == nil {
		return map[string]any{"_root": sketchJSONValue(v)}
	}
	return sketched
}

func sketchJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sketchJSONValue(t[k])
		}
		return out
	case []any:
		if len(t) == 0 {
			return []any{}
		}
		// One sample element + length — enough to see item types without dumping history text.
		return map[string]any{"_array_len": len(t), "_sample": sketchJSONValue(t[0])}
	case string:
		return fmt.Sprintf("<string:%d>", len(t))
	case json.Number:
		return t.String()
	case bool:
		return t
	case nil:
		return nil
	default:
		return fmt.Sprintf("<%T>", t)
	}
}

// DeepSeek Responses is same-protocol with Codex (wire_api=responses at
// https://api.deepseek.com). No Chat Completions translation. The API is
// stateless: previous_response_id is unsupported and must be stripped so the
// portable-history path can send full portable input.
//
// Policy (official DeepSeek Responses table + observed Codex 0.146.1 envelope):
//   - Strip previous_response_id and non-semantic telemetry/cache hints.
//   - JSON member names are case-sensitive (never lowercase into allow-lists).
//   - Nested reasoning allows only effort + narrowly handled summary; text
//     allows only format + narrowly handled verbosity. Unknown nested keys reject.
//   - Narrow Codex compatibility exceptions (exact shape only — never “strip all
//     unsupported”):
//       1. include:["reasoning.encrypted_content"] — transport preference for
//          opaque encrypted reasoning; Zen's full portable-history path supersedes.
//       2. parallel_tool_calls:false — Codex client default; DeepSeek always
//          enables parallel calls, so false cannot be honored and dropping it
//          does not change token limits/user text/requested output content.
//       3. reasoning.summary:"auto" — Codex default presentation hint; upstream
//          accepts but generates no summary. Explicit values (e.g. "detailed")
//          remain typed rejects.
//   - Explicit/non-default semantic requests still reject: other include shapes,
//     max_tool_calls limits, text.verbosity, truncation/context_management/
//     service_tier, etc.
//   - Preserve supported function tools/calls/results and portable text history.

func isDeepSeekResponsesUpstream(b RouteBinding) bool {
	return normalizeID(b.ProviderID) == "deepseek" &&
		normalizeID(b.RouteProtocol) == RouteProtocolResponses
}

func envelopeDeepSeekV4Flash() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 1000000,
		ReasoningClass:      ReasoningClassExtended,
		ThinkingClass:       ThinkingClassNone,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText},
	}
}

// Supported / classified top-level fields (nested/value rules apply after allow).
var deepSeekResponsesAllowedFields = map[string]struct{}{
	"model": {}, "input": {}, "instructions": {}, "tools": {}, "tool_choice": {},
	"stream": {}, "temperature": {}, "top_p": {}, "max_output_tokens": {},
	"top_logprobs": {}, "reasoning": {}, "text": {}, "user": {},
	"parallel_tool_calls": {}, "max_tool_calls": {}, "include": {},
	// Present so strip classification can run; removed before forward.
	"previous_response_id": {},
	"metadata":             {},
	"prompt_cache_key":     {}, "prompt_cache_retention": {},
	"safety_identifier": {}, "stream_options": {},
	"client_metadata": {},
	// Reject classification runs first for non-default values.
	"service_tier": {}, "truncation": {}, "context_management": {},
	"store": {}, "background": {},
	"conversation": {}, "prompt": {},
}

// Non-semantic telemetry/cache hints + stateless continuation only.
var deepSeekResponsesStripFields = map[string]struct{}{
	"previous_response_id":   {},
	"metadata":               {},
	"prompt_cache_key":       {},
	"prompt_cache_retention": {},
	"safety_identifier":      {},
	"stream_options":         {},
	"client_metadata":        {},
}

// Reject when present with a non-empty / non-default semantic value.
var deepSeekResponsesRejectFields = map[string]struct{}{
	"conversation": {}, "prompt": {},
	"store": {}, "background": {},
	"truncation": {}, "context_management": {}, "service_tier": {},
}

func sanitizeDeepSeekResponsesRequest(body []byte, envelope CapabilityEnvelope) ([]byte, error) {
	out, path, err := sanitizeDeepSeekResponsesRequestInner(body, envelope)
	if err != nil && errors.Is(err, ErrResponsesFeatureUnsupported) {
		reportDeepSeekSanitizeReject(firstNonEmpty(path, unsupportedFieldPath(err)), body)
	}
	return out, err
}

func sanitizeDeepSeekResponsesRequestInner(body []byte, envelope CapabilityEnvelope) ([]byte, string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, "", fmt.Errorf("%w: trailing junk", ErrRequestBodyMalformed)
	}
	if obj == nil {
		return nil, "", fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}

	// JSON member names are case-sensitive. Match allow/strip/reject maps on the
	// exact key; do not lowercase unknown keys into a permitted name.
	for key, raw := range obj {
		if _, reject := deepSeekResponsesRejectFields[key]; reject {
			if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
				delete(obj, key)
				continue
			}
			if key == "store" || key == "background" {
				var b bool
				if err := json.Unmarshal(raw, &b); err == nil && !b {
					delete(obj, key) // default-false is non-semantic
					continue
				}
			}
			return nil, key, fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, key)
		}
		if _, strip := deepSeekResponsesStripFields[key]; strip {
			delete(obj, key)
			continue
		}
		if _, ok := deepSeekResponsesAllowedFields[key]; !ok {
			return nil, key, fmt.Errorf("%w: unknown field %q", ErrResponsesFeatureUnsupported, key)
		}
	}

	if err := classifyDeepSeekIgnoredSemantics(obj); err != nil {
		return nil, unsupportedFieldPath(err), err
	}

	allowsImage := false
	for _, m := range envelope.Modalities {
		if normalizeID(m) == ModalityImage {
			allowsImage = true
			break
		}
	}
	if raw, ok := obj["reasoning"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		cleaned, err := sanitizeDeepSeekTopLevelReasoning(raw)
		if err != nil {
			return nil, unsupportedFieldPath(err), err
		}
		obj["reasoning"] = cleaned
	}
	if raw, ok := obj["text"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		cleaned, err := sanitizeDeepSeekText(raw)
		if err != nil {
			return nil, unsupportedFieldPath(err), err
		}
		obj["text"] = cleaned
	}
	if raw, ok := obj["input"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		cleaned, err := sanitizeDeepSeekResponsesInput(raw, allowsImage)
		if err != nil {
			return nil, unsupportedFieldPath(err), err
		}
		obj["input"] = cleaned
	}
	if raw, ok := obj["tools"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		cleaned, err := sanitizeDeepSeekResponsesTools(raw)
		if err != nil {
			return nil, unsupportedFieldPath(err), err
		}
		obj["tools"] = cleaned
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return nil, "", fmt.Errorf("%w: re-encode: %v", ErrRequestBodyMalformed, err)
	}
	return out, "", nil
}

func unsupportedFieldPath(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, `field "`); i >= 0 {
		rest := msg[i+len(`field "`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	if i := strings.Index(msg, `unknown field "`); i >= 0 {
		rest := msg[i+len(`unknown field "`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	return msg
}

// classifyDeepSeekIgnoredSemantics applies narrow Codex compatibility strips
// and rejects remaining unhonored semantic requests.
func classifyDeepSeekIgnoredSemantics(obj map[string]json.RawMessage) error {
	if raw, ok := obj["include"]; ok {
		if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" || string(raw) == "[]" {
			delete(obj, "include")
		} else if isCodexEncryptedReasoningInclude(raw) {
			// Compat #1: exact Codex opaque-reasoning transport preference.
			// Zen portable-history already carries plain reasoning content.
			delete(obj, "include")
		} else {
			return fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, "include")
		}
	}
	if raw, ok := obj["max_tool_calls"]; ok {
		if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
			delete(obj, "max_tool_calls")
		} else {
			return fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, "max_tool_calls")
		}
	}
	if raw, ok := obj["parallel_tool_calls"]; ok {
		if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
			delete(obj, "parallel_tool_calls")
		} else {
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				return fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, "parallel_tool_calls")
			}
			if !b {
				// Compat #2: exact false — Codex default; DeepSeek always parallel.
				delete(obj, "parallel_tool_calls")
			}
			// true matches fixed upstream behavior — forward.
		}
	}
	return nil
}

// isCodexEncryptedReasoningInclude reports the exact observed Codex 0.146.1
// shape include:["reasoning.encrypted_content"] (sole element).
func isCodexEncryptedReasoningInclude(raw json.RawMessage) bool {
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return false
	}
	return len(items) == 1 && items[0] == "reasoning.encrypted_content"
}

func sanitizeDeepSeekTopLevelReasoning(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: reasoning: %v", ErrRequestBodyMalformed, err)
	}
	for key := range obj {
		switch key {
		case "effort":
			// Officially supported — forward.
		case "summary":
			if deepSeekReasoningSummaryCompatStrip(obj[key]) {
				delete(obj, key)
				continue
			}
			return nil, fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, "reasoning.summary")
		default:
			return nil, fmt.Errorf("%w: unknown field %q", ErrResponsesFeatureUnsupported, "reasoning."+key)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode reasoning: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(out), nil
}

// deepSeekReasoningSummaryCompatStrip allows disabled forms and the exact
// Codex default "auto" (compat #3). Any other non-empty value is a semantic reject.
func deepSeekReasoningSummaryCompatStrip(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.TrimSpace(s) {
		case "", "none", "auto": // "auto" = observed Codex 0.146.1 default hint
			return true
		default:
			return false
		}
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return !b
	}
	return false
}

func sanitizeDeepSeekText(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: text: %v", ErrRequestBodyMalformed, err)
	}
	for key := range obj {
		switch key {
		case "format":
			// Officially supported — forward.
		case "verbosity":
			if deepSeekTextVerbosityDefault(obj[key]) {
				delete(obj, key)
				continue
			}
			return nil, fmt.Errorf("%w: field %q", ErrResponsesFeatureUnsupported, "text.verbosity")
		default:
			return nil, fmt.Errorf("%w: unknown field %q", ErrResponsesFeatureUnsupported, "text."+key)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode text: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(out), nil
}

func deepSeekTextVerbosityDefault(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func sanitizeDeepSeekResponsesInput(raw json.RawMessage, allowsImage bool) (json.RawMessage, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return raw, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%w: input must be string or array: %v", ErrRequestBodyMalformed, err)
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var meta struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &meta); err != nil {
			return nil, fmt.Errorf("%w: input item: %v", ErrRequestBodyMalformed, err)
		}
		switch normalizeID(meta.Type) {
		case "message":
			cleaned, err := sanitizeDeepSeekMessageItem(item, allowsImage)
			if err != nil {
				return nil, err
			}
			out = append(out, cleaned)
		case "function_call", "function_call_output", "web_search_call":
			out = append(out, item)
		case "reasoning":
			cleaned, err := sanitizeDeepSeekReasoningItem(item)
			if err != nil {
				return nil, err
			}
			out = append(out, cleaned)
		case "item_reference":
			return nil, fmt.Errorf("%w: input type %q", ErrResponsesFeatureUnsupported, meta.Type)
		case "":
			return nil, fmt.Errorf("%w: input item missing type", ErrRequestBodyMalformed)
		default:
			return nil, fmt.Errorf("%w: input type %q", ErrResponsesFeatureUnsupported, meta.Type)
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode input: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(encoded), nil
}

func sanitizeDeepSeekMessageItem(item json.RawMessage, allowsImage bool) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return nil, fmt.Errorf("%w: message: %v", ErrRequestBodyMalformed, err)
	}
	rawContent, ok := obj["content"]
	if !ok {
		return item, nil
	}
	var asString string
	if err := json.Unmarshal(rawContent, &asString); err == nil {
		return item, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(rawContent, &parts); err != nil {
		return nil, fmt.Errorf("%w: message content: %v", ErrRequestBodyMalformed, err)
	}
	kept := make([]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		var meta struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(part, &meta); err != nil {
			return nil, fmt.Errorf("%w: content part: %v", ErrRequestBodyMalformed, err)
		}
		switch normalizeID(meta.Type) {
		case "input_text", "output_text", "text":
			kept = append(kept, part)
		case "refusal":
			return nil, fmt.Errorf("%w: content part type %q", ErrResponsesFeatureUnsupported, meta.Type)
		case "input_image", "input_file", "image", "file":
			if !allowsImage {
				return nil, fmt.Errorf("%w: content part type %q", ErrResponsesFeatureUnsupported, meta.Type)
			}
			kept = append(kept, part)
		default:
			return nil, fmt.Errorf("%w: content part type %q", ErrResponsesFeatureUnsupported, meta.Type)
		}
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode content: %v", ErrRequestBodyMalformed, err)
	}
	obj["content"] = json.RawMessage(encoded)
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode message: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(out), nil
}

func sanitizeDeepSeekReasoningItem(item json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return nil, fmt.Errorf("%w: reasoning: %v", ErrRequestBodyMalformed, err)
	}
	if raw, ok := obj["encrypted_content"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" && string(raw) != `""` {
		return nil, fmt.Errorf("%w: reasoning.encrypted_content", ErrResponsesFeatureUnsupported)
	}
	if raw, ok := obj["summary"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" && string(raw) != `[]` && string(raw) != `""` {
		return nil, fmt.Errorf("%w: reasoning.summary", ErrResponsesFeatureUnsupported)
	}
	if _, ok := obj["content"]; !ok {
		return nil, fmt.Errorf("%w: reasoning without portable content", ErrResponsesFeatureUnsupported)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode reasoning: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(out), nil
}

func sanitizeDeepSeekResponsesTools(raw json.RawMessage) (json.RawMessage, error) {
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("%w: tools: %v", ErrRequestBodyMalformed, err)
	}
	out := make([]map[string]any, 0, len(tools))
	var walk func(tool map[string]any) error
	walk = func(tool map[string]any) error {
		typ, _ := tool["type"].(string)
		switch normalizeID(typ) {
		case "function", "web_search", "web_search_2025_08_26":
			out = append(out, tool)
			return nil
		case "custom":
			name, _ := tool["name"].(string)
			if normalizeSpace(name) == "apply_patch" {
				out = append(out, tool)
				return nil
			}
			return fmt.Errorf("%w: custom tool %q", ErrResponsesFeatureUnsupported, name)
		case "namespace":
			nested, _ := tool["tools"].([]any)
			if len(nested) == 0 {
				return nil
			}
			for _, n := range nested {
				child, ok := n.(map[string]any)
				if !ok {
					return fmt.Errorf("%w: namespace child", ErrRequestBodyMalformed)
				}
				if err := walk(child); err != nil {
					return err
				}
			}
			return nil
		case "file_search", "code_interpreter", "computer_use", "mcp":
			return fmt.Errorf("%w: tool type %q", ErrResponsesFeatureUnsupported, typ)
		default:
			if strings.TrimSpace(typ) == "" {
				return fmt.Errorf("%w: tool missing type", ErrRequestBodyMalformed)
			}
			return fmt.Errorf("%w: tool type %q", ErrResponsesFeatureUnsupported, typ)
		}
	}
	for _, tool := range tools {
		if err := walk(tool); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode tools: %v", ErrRequestBodyMalformed, err)
	}
	return json.RawMessage(encoded), nil
}
