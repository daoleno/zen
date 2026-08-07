package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
// keyring secrets are never returned to callers beyond the request header mutation.
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
