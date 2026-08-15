package modelprofiles

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

const MaxRouteRequestBodyBytes = 8 << 20

var hopByHopHeaderNames = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
	"Proxy-Connection",
}

// decodeBodyEncoding applies the request Content-Encoding to a bounded body.
// Supported codings match what real clients ship — Codex Desktop sends
// zstd-compressed request bodies, others use gzip/x-gzip, deflate, or br:
//
//	gzip, x-gzip, deflate (zlib per RFC 9110 with raw-DEFLATE fallback),
//	br (brotli), zstd, zst (zstd alias)
//
// Stacked codings (e.g. "gzip, zstd") are listed in application order and are
// decoded in reverse, with every stage — final output and intermediate
// products — bounded by max so compressed bombs are cut off at the budget
// instead of expanded fully. Unknown or unsupported codings are rejected
// explicitly (fail closed): the router must rewrite the JSON body, so a
// compressed body it cannot decode must never be passed through untouched.
// decoded reports whether the caller must strip Content-Encoding before
// forwarding upstream.
func decodeBodyEncoding(encoding string, raw []byte, max int64) (body []byte, decoded bool, err error) {
	codings := splitCodings(encoding)
	if len(codings) == 0 {
		return raw, false, nil
	}
	for _, coding := range codings {
		if !isSupportedCoding(coding) {
			return nil, false, fmt.Errorf("%w: unsupported content-encoding %q", ErrRequestBodyMalformed, encoding)
		}
	}
	if max <= 0 {
		max = MaxRouteRequestBodyBytes
	}
	// RFC 9110 §8.4: codings are listed in application order, so the last one
	// applied comes off first.
	data := raw
	for i := len(codings) - 1; i >= 0; i-- {
		out, err := decodeSingleCoding(codings[i], data, max)
		if err != nil {
			return nil, false, err
		}
		data = out
	}
	return data, true, nil
}

// splitCodings splits a Content-Encoding value into trimmed, lowercased
// codings, dropping empties and identity. Repeated headers are already joined
// by net/http with commas, so a single split covers both forms.
func splitCodings(encoding string) []string {
	var out []string
	for _, part := range strings.Split(encoding, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" || part == "identity" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func isSupportedCoding(coding string) bool {
	switch coding {
	case "gzip", "x-gzip", "deflate", "br", "zstd", "zst":
		return true
	default:
		return false
	}
}

func decodeSingleCoding(coding string, raw []byte, max int64) ([]byte, error) {
	switch coding {
	case "gzip", "x-gzip":
		return decodeGzipBody(raw, max)
	case "deflate":
		return decodeDeflateBody(raw, max)
	case "br":
		return decodeBrotliBody(raw, max)
	case "zstd", "zst":
		return decodeZstdBody(raw, max)
	default:
		// Unreachable: decodeBodyEncoding validates every coding first.
		return nil, fmt.Errorf("%w: unsupported content-encoding %q", ErrRequestBodyMalformed, coding)
	}
}

func decodeGzipBody(raw []byte, max int64) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrRequestBodyMalformed, err)
	}
	defer r.Close()
	return boundedReadAll(r, max, "gzip")
}

func decodeZstdBody(raw []byte, max int64) ([]byte, error) {
	r, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: zstd: %v", ErrRequestBodyMalformed, err)
	}
	defer r.Close()
	return boundedReadAll(r, max, "zstd")
}

func decodeBrotliBody(raw []byte, max int64) ([]byte, error) {
	return boundedReadAll(brotli.NewReader(bytes.NewReader(raw)), max, "br")
}

func decodeDeflateBody(raw []byte, max int64) ([]byte, error) {
	// RFC 9110 §8.4.1: deflate means the zlib wrapper. Some clients send raw
	// DEFLATE streams instead, so fall back to raw flate before rejecting —
	// mirrors cc-switch. A genuine zlib bomb keeps its 413: the raw fallback
	// sees the zlib wrapper as corruption, but the limit abort is the more
	// honest diagnosis.
	zr, zlibErr := zlib.NewReader(bytes.NewReader(raw))
	zlibTooLarge := false
	if zlibErr == nil {
		out, readErr := boundedReadAll(zr, max, "deflate(zlib)")
		_ = zr.Close()
		if readErr == nil {
			return out, nil
		}
		if errors.Is(readErr, ErrRequestBodyTooLarge) {
			zlibTooLarge = true
		}
	}
	fr := flate.NewReader(bytes.NewReader(raw))
	out, rawErr := boundedReadAll(fr, max, "deflate(raw)")
	if rawErr == nil {
		return out, nil
	}
	if zlibTooLarge {
		return nil, ErrRequestBodyTooLarge
	}
	// A limit abort is authoritative under either interpretation: the request
	// is rejected regardless of which codec the stream actually is.
	if errors.Is(rawErr, ErrRequestBodyTooLarge) {
		return nil, rawErr
	}
	if zlibErr != nil {
		// The stream never parsed as zlib; the header check is the diagnosis.
		return nil, fmt.Errorf("%w: deflate: %v", ErrRequestBodyMalformed, zlibErr)
	}
	return nil, rawErr
}

func boundedReadAll(r io.Reader, max int64, codec string) ([]byte, error) {
	out, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRequestBodyMalformed, codec, err)
	}
	if int64(len(out)) > max {
		return nil, ErrRequestBodyTooLarge
	}
	return out, nil
}

// requestModelFromBody extracts the top-level JSON "model" field (the CLI's
// own model identity). Fail closed on malformed/non-object bodies or a missing
// model: the router never guesses an identity.
func requestModelFromBody(body []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return "", err
	}
	if obj == nil {
		return "", fmt.Errorf("body must be a json object")
	}
	raw, ok := obj["model"]
	if !ok {
		return "", fmt.Errorf("body model is required")
	}
	var model string
	if err := json.Unmarshal(raw, &model); err != nil {
		return "", fmt.Errorf("body model must be a string")
	}
	return strings.TrimSpace(model), nil
}

// requestEffortFromBody extracts the top-level JSON "reasoning.effort" field.
// present=false when the body has no reasoning object or no effort key.
func requestEffortFromBody(body []byte) (effort string, present bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", false
	}
	raw, ok := obj["reasoning"]
	if !ok {
		return "", false
	}
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		return "", false
	}
	effortRaw, ok := reasoning["effort"]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(effortRaw, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// requestHasModelSwitchSignal reports Codex's reserved model-switch fragment
// only when it appears in the current developer-owned input suffix. Historical
// fragments before any model-produced response item are ignored, as are user
// messages, so neither retained history nor prompt text can authorize a fresh
// runtime mutation.
func requestHasModelSwitchSignal(body []byte) (bool, error) {
	obj, err := decodeJSONObject(body)
	if err != nil {
		return false, err
	}
	rawInput, ok := obj["input"]
	if !ok {
		return false, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawInput, &items); err != nil {
		return false, nil
	}
	currentSuffixStart := 0
	for index, item := range items {
		var header struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(item, &header); err != nil {
			continue
		}
		itemType := normalizeID(header.Type)
		role := normalizeID(header.Role)
		if itemType != "message" || (role != "user" && role != "developer" && role != "system") {
			currentSuffixStart = index + 1
		}
	}
	for _, item := range items[currentSuffixStart:] {
		var message struct {
			Type    string            `json:"type"`
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(item, &message); err != nil || normalizeID(message.Type) != "message" {
			continue
		}
		if normalizeID(message.Role) != "developer" {
			continue
		}
		for _, part := range message.Content {
			var textPart struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(part, &textPart); err == nil && normalizeID(textPart.Type) == "input_text" && hasCompleteModelSwitchFragment(textPart.Text) {
				return true, nil
			}
		}
	}
	return false, nil
}

func hasCompleteModelSwitchFragment(value string) bool {
	const open = "<model_switch>"
	const close = "</model_switch>"
	const preamble = "The user was previously using a different model."
	start := strings.Index(value, open)
	if start < 0 {
		return false
	}
	body := value[start+len(open):]
	end := strings.Index(body, close)
	return end >= 0 && strings.Contains(body[:end], preamble)
}

// rewriteRequestModel replaces only the top-level JSON "model" field.
func rewriteRequestModel(body []byte, upstreamModel string) ([]byte, error) {
	if err := ValidateModelID(upstreamModel); err != nil {
		return nil, fmt.Errorf("%w: upstream model: %v", ErrRequestBodyMalformed, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing junk after json object", ErrRequestBodyMalformed)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}
	modelBytes, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, fmt.Errorf("%w: model encode: %v", ErrRequestBodyMalformed, err)
	}
	obj["model"] = json.RawMessage(modelBytes)
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode: %v", ErrRequestBodyMalformed, err)
	}
	return out, nil
}

// rewriteRequestEffort replaces only the top-level JSON "reasoning.effort"
// field, preserving every other key of the reasoning object (e.g. summary).
// The value must be in the daemon-owned Codex effort vocabulary (fail closed —
// an unknown value is never forwarded upstream).
func rewriteRequestEffort(body []byte, effort string) ([]byte, error) {
	effort = normalizeID(effort)
	if effort == "" {
		return body, nil
	}
	if !isCodexReasoningEffortValue(effort) {
		return nil, fmt.Errorf("%w: unknown reasoning effort %q", ErrRequestBodyMalformed, effort)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing junk after json object", ErrRequestBodyMalformed)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}
	reasoning := map[string]json.RawMessage{}
	if raw, ok := obj["reasoning"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &reasoning); err != nil {
			return nil, fmt.Errorf("%w: reasoning: %v", ErrRequestBodyMalformed, err)
		}
	}
	effortBytes, err := json.Marshal(effort)
	if err != nil {
		return nil, fmt.Errorf("%w: effort encode: %v", ErrRequestBodyMalformed, err)
	}
	reasoning["effort"] = json.RawMessage(effortBytes)
	obj["reasoning"] = mustRawMessage(reasoning)
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode: %v", ErrRequestBodyMalformed, err)
	}
	return out, nil
}

// clearRequestEffort removes the top-level reasoning.effort override while
// preserving other reasoning fields.
func clearRequestEffort(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing junk after json object", ErrRequestBodyMalformed)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}
	raw, ok := obj["reasoning"]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return body, nil
	}
	reasoning := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		return nil, fmt.Errorf("%w: reasoning: %v", ErrRequestBodyMalformed, err)
	}
	delete(reasoning, "effort")
	if len(reasoning) == 0 {
		delete(obj, "reasoning")
	} else {
		obj["reasoning"] = mustRawMessage(reasoning)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode: %v", ErrRequestBodyMalformed, err)
	}
	return out, nil
}

func mustRawMessage(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err) // unreachable for map[string]json.RawMessage
	}
	return raw
}

func stripInboundAuthAndHopByHop(h http.Header) {
	for _, conn := range h.Values("Connection") {
		for _, name := range strings.Split(conn, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopByHopHeaderNames {
		h.Del(name)
	}
	h.Del("Authorization")
	h.Del("Proxy-Authorization")
	h.Del("X-Api-Key")
	h.Del("Api-Key")
}

func copySafeResponseHeaders(dst, src http.Header) {
	// Dual filter: (1) conservative end-to-end allowlist — never forward arbitrary
	// X-* / custom headers, because Go's Transport may consume Connection and
	// drop knowledge of hop-named headers; (2) also strip any Connection tokens
	// that are still visible on the response.
	denied := map[string]struct{}{}
	for _, name := range hopByHopHeaderNames {
		denied[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	for _, conn := range src.Values("Connection") {
		for _, tok := range strings.Split(conn, ",") {
			tok = http.CanonicalHeaderKey(strings.TrimSpace(tok))
			if tok != "" {
				denied[tok] = struct{}{}
			}
		}
	}
	for name, values := range src {
		canon := http.CanonicalHeaderKey(name)
		if _, block := denied[canon]; block {
			continue
		}
		if !isAllowedResponseHeader(canon) {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

// isAllowedResponseHeader is a conservative allowlist for upstream→client headers.
// Unknown / arbitrary X-* headers are denied even if Connection tokens were lost.
func isAllowedResponseHeader(name string) bool {
	canon := http.CanonicalHeaderKey(name)
	switch canon {
	case "Content-Type", "Content-Encoding", "Content-Length", "Content-Language",
		"Cache-Control", "Expires", "Pragma", "Vary",
		"Retry-After", "Date", "Etag", "Last-Modified", "Age",
		"Accept-Ranges", "Content-Range":
		return true
	}
	lower := strings.ToLower(canon)
	switch {
	case strings.HasPrefix(lower, "x-request-id"),
		strings.HasPrefix(lower, "x-ratelimit"),
		strings.HasPrefix(lower, "ratelimit-"),
		strings.HasPrefix(lower, "openai-"),
		strings.HasPrefix(lower, "anthropic-"),
		lower == "request-id",
		lower == "x-openai-request-id":
		return true
	default:
		return false
	}
}

func isHopByHopHeaderName(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailers", "Transfer-Encoding", "Upgrade", "Proxy-Connection":
		return true
	default:
		return false
	}
}

type capturedInboundAuth struct {
	authorization string
	xAPIKey       string
	apiKey        string
}

func captureInboundAuth(h http.Header) capturedInboundAuth {
	return capturedInboundAuth{
		authorization: h.Get("Authorization"),
		xAPIKey:       h.Get("X-Api-Key"),
		apiKey:        h.Get("Api-Key"),
	}
}

// applyUpstreamAuth injects upstream credentials per AuthMode. Env values and
// stored secrets are never returned to callers beyond the request header mutation.
func applyUpstreamAuth(dst http.Header, binding RouteBinding, inbound capturedInboundAuth, lookup func(string) (string, bool), store CredentialStore) error {
	mode := normalizeID(binding.AuthMode)
	if mode == "" {
		mode = AuthModeNone
	}
	switch mode {
	case AuthModeNone:
		return nil
	case AuthModeBearerEnv:
		token, err := resolveProviderSecret(binding.CredentialRef, binding.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		dst.Set("Authorization", "Bearer "+token)
		return nil
	case AuthModeXAPIKeyEnv:
		token, err := resolveProviderSecret(binding.CredentialRef, binding.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		dst.Set("X-Api-Key", token)
		return nil
	case AuthModeNativePassthrough:
		host, err := upstreamHostname(binding.UpstreamBaseURL)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUpstreamInvalid, err)
		}
		if !isNativePassthroughHost(host) {
			return fmt.Errorf("%w: native_passthrough host not allowed", ErrUpstreamInvalid)
		}
		if inbound.authorization != "" {
			dst.Set("Authorization", inbound.authorization)
		}
		if inbound.xAPIKey != "" {
			dst.Set("X-Api-Key", inbound.xAPIKey)
		}
		if inbound.apiKey != "" {
			dst.Set("Api-Key", inbound.apiKey)
		}
		if inbound.authorization == "" && inbound.xAPIKey == "" && inbound.apiKey == "" {
			return fmt.Errorf("%w: native_passthrough requires inbound auth headers", ErrCredentialNotReady)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown auth_mode", ErrInvalid)
	}
}

func resolveSecretEnv(envName string, lookup func(string) (string, bool)) (string, error) {
	envName = normalizeSpace(envName)
	if err := ValidateCredentialEnv(envName); err != nil {
		return "", err
	}
	if lookup == nil {
		lookup = lookupEnv
	}
	value, ok := lookup(envName)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s", ErrCredentialNotReady, envName)
	}
	return value, nil
}

func withRawQuery(upstreamURL, rawQuery string) (string, error) {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamInvalid, err)
	}
	u.RawQuery = rawQuery
	u.Fragment = ""
	return u.String(), nil
}
