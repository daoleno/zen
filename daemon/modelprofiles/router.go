package modelprofiles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Router is the Zen-owned same-protocol loopback routing runtime.
type Router struct {
	table   *RouteTable
	client  *http.Client
	lookup  func(string) (string, bool)
	creds   CredentialStore
	maxBody int64
}

// RouterOption configures Router construction.
type RouterOption func(*Router)

// WithRouterClient overrides the upstream HTTP client (tests).
func WithRouterClient(client *http.Client) RouterOption {
	return func(r *Router) {
		if client != nil {
			r.client = client
		}
	}
}

// WithRouterLookup overrides credential resolution (tests).
func WithRouterLookup(lookup func(string) (string, bool)) RouterOption {
	return func(r *Router) {
		if lookup != nil {
			r.lookup = lookup
		}
	}
}

// WithRouterCredentials installs the Provider credential store for route auth.
func WithRouterCredentials(store CredentialStore) RouterOption {
	return func(r *Router) {
		r.creds = store
	}
}

// WithRouterMaxBody overrides the bounded request body size (tests).
func WithRouterMaxBody(n int64) RouterOption {
	return func(r *Router) {
		if n > 0 {
			r.maxBody = n
		}
	}
}

// NewRouter constructs a loopback routing runtime over table.
func NewRouter(table *RouteTable, opts ...RouterOption) *Router {
	r := &Router{
		table:   table,
		client:  NewSafeHTTPClient(5 * time.Minute),
		maxBody: MaxRouteRequestBodyBytes,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Router) credentialLookup() func(string) (string, bool) {
	if r != nil && r.lookup != nil {
		return r.lookup
	}
	if r != nil && r.table != nil {
		return r.table.credentialLookup
	}
	return lookupEnv
}

// Handler returns the HTTP handler for loopback admission.
func (r *Router) Handler() http.Handler {
	return http.HandlerFunc(r.ServeHTTP)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.table == nil {
		writeRouteError(w, http.StatusServiceUnavailable, ErrRouteNotFound)
		return
	}
	if !isLoopbackRemoteAddr(req.RemoteAddr) {
		writeRouteError(w, http.StatusForbidden, ErrRouteAdmissionDenied)
		return
	}

	// Reject WebSocket Upgrade with 501 so Codex can fall back to POST /v1/responses
	// when the installed CLI probe confirms that behavior.
	if isWebSocketUpgrade(req) {
		writeRouteError(w, http.StatusNotImplemented, ErrRouteWebSocket)
		return
	}

	if req.Method != http.MethodPost {
		writeRouteError(w, http.StatusMethodNotAllowed, ErrRouteMethodMismatch)
		return
	}

	parsed, err := ParseRouteRequestPath(req.URL.Path)
	if err != nil {
		writeRouteError(w, http.StatusNotFound, ErrRoutePathMismatch)
		return
	}

	binding, flightToken, err := r.table.BeginRouteFlight(parsed.RouteID)
	if err != nil {
		writeRouteError(w, http.StatusNotFound, ErrRouteNotFound)
		return
	}
	completed := false
	defer func() {
		if !completed {
			_ = r.table.EndRouteFlight(parsed.RouteID, flightToken, false)
		}
	}()

	if !EndpointAllowedForProtocol(binding.RouteProtocol, parsed.Endpoint) {
		writeRouteError(w, http.StatusBadRequest, ErrRouteProtocolMismatch)
		return
	}

	inboundAuth := captureInboundAuth(req.Header)

	body, err := readBoundedBody(req, r.maxBody)
	if err != nil {
		if errors.Is(err, ErrRequestBodyTooLarge) {
			writeRouteError(w, http.StatusRequestEntityTooLarge, ErrRequestBodyTooLarge)
			return
		}
		writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
		return
	}
	rewritten := body
	if len(body) > 0 && (parsed.Endpoint == EndpointResponses || parsed.Endpoint == EndpointAnthropicMessages || parsed.Endpoint == EndpointAnthropicCountTokens) {
		rewritten, err = rewriteRequestModel(body, binding.UpstreamModel)
		if err != nil {
			writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
			return
		}
		if normalizeID(binding.HistoryPortability) == HistoryPortabilityStripOpaque {
			rewritten, _, err = normalizePortableHistoryBody(binding.RouteProtocol, rewritten)
			if err != nil {
				if errors.Is(err, ErrRequestBodyNotPortable) {
					writeRouteError(w, http.StatusBadRequest, ErrRequestBodyNotPortable)
					return
				}
				writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
				return
			}
		}
		if parsed.Endpoint == EndpointResponses && isDeepSeekResponsesUpstream(binding) {
			// Trusted DeepSeek Responses envelope (not the ClientModelContract
			// UpstreamEnvelope mapping used for Activate admission).
			rewritten, err = sanitizeDeepSeekResponsesRequest(rewritten, envelopeDeepSeekV4Flash())
			if err != nil {
				if errors.Is(err, ErrResponsesFeatureUnsupported) {
					writeRouteError(w, http.StatusBadRequest, ErrResponsesFeatureUnsupported)
					return
				}
				writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
				return
			}
		}
	}

	upstreamURL, err := UpstreamRequestURL(binding.UpstreamBaseURL, parsed.APIPath)
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
		return
	}
	upstreamURL, err = withRawQuery(upstreamURL, req.URL.RawQuery)
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
		return
	}

	upReq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, upstreamURL, bytes.NewReader(rewritten))
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
		return
	}
	copyInboundHeaders(upReq.Header, req.Header)
	if err := applyUpstreamAuth(upReq.Header, binding, inboundAuth, r.credentialLookup(), r.creds); err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrCredentialNotReady)
		return
	}
	if len(rewritten) > 0 {
		upReq.Header.Set("Content-Type", firstNonEmpty(req.Header.Get("Content-Type"), "application/json"))
		upReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
		upReq.ContentLength = int64(len(rewritten))
	}

	resp, err := r.client.Do(upReq)
	if err != nil {
		status, typed := classifyUpstreamDoError(err)
		writeRouteError(w, status, typed)
		return
	}
	defer resp.Body.Close()

	// Only a successful 2xx upstream response may mark opaque history, and only
	// after headers are about to be forwarded (never on local/network failure).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := r.table.EndRouteFlight(parsed.RouteID, flightToken, true); err != nil {
			writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
			return
		}
		completed = true
	} else {
		_ = r.table.EndRouteFlight(parsed.RouteID, flightToken, false)
		completed = true
	}

	copySafeResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if err := streamCopyFlush(w, resp.Body); err != nil {
		return
	}
}

func isWebSocketUpgrade(req *http.Request) bool {
	if req == nil {
		return false
	}
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range strings.Split(req.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "Upgrade") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade")
}

func streamCopyFlush(w http.ResponseWriter, src io.Reader) error {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func readBoundedBody(req *http.Request, max int64) ([]byte, error) {
	if max <= 0 {
		max = MaxRouteRequestBodyBytes
	}
	limited := io.LimitReader(req.Body, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if int64(len(body)) > max {
		return nil, ErrRequestBodyTooLarge
	}
	return body, nil
}

func copyInboundHeaders(dst, src http.Header) {
	for name, values := range src {
		canon := http.CanonicalHeaderKey(name)
		if canon == "Authorization" || canon == "Proxy-Authorization" ||
			canon == "X-Api-Key" || canon == "Api-Key" ||
			isHopByHopHeaderName(canon) || canon == "Content-Length" || canon == "Host" {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
	stripInboundAuthAndHopByHop(dst)
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func writeRouteError(w http.ResponseWriter, status int, err error) {
	code := "route_error"
	switch {
	case errors.Is(err, ErrRouteNotFound):
		code = "route_not_found"
	case errors.Is(err, ErrRouteAdmissionDenied):
		code = "route_admission_denied"
	case errors.Is(err, ErrRoutePathMismatch):
		code = "route_path_mismatch"
	case errors.Is(err, ErrRouteProtocolMismatch):
		code = "route_protocol_mismatch"
	case errors.Is(err, ErrRouteMethodMismatch):
		code = "route_method_mismatch"
	case errors.Is(err, ErrRouteWebSocket):
		code = "route_websocket_rejected"
	case errors.Is(err, ErrRequestBodyTooLarge):
		code = "request_body_too_large"
	case errors.Is(err, ErrRequestBodyMalformed):
		code = "request_body_malformed"
	case errors.Is(err, ErrRequestBodyNotPortable):
		code = "request_body_not_portable"
	case errors.Is(err, ErrResponsesFeatureUnsupported):
		code = "responses_feature_unsupported"
	case errors.Is(err, ErrCredentialNotReady):
		code = "credential_not_ready"
	case errors.Is(err, ErrUpstreamInvalid):
		code = "upstream_invalid"
	case errors.Is(err, ErrUpstreamSSRF):
		code = "upstream_ssrf"
	case errors.Is(err, ErrUpstreamRedirect):
		code = "upstream_redirect"
	case errors.Is(err, ErrBindingBusy):
		code = "route_busy"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":{"type":"`+code+`","message":"request failed"}}`)
}

func classifyUpstreamDoError(err error) (int, error) {
	if err == nil {
		return http.StatusBadGateway, ErrUpstreamInvalid
	}
	if errors.Is(err, ErrUpstreamRedirect) {
		return http.StatusBadGateway, ErrUpstreamRedirect
	}
	if errors.Is(err, ErrUpstreamSSRF) {
		return http.StatusBadGateway, ErrUpstreamSSRF
	}
	if errors.Is(err, context.Canceled) {
		return http.StatusBadGateway, ErrUpstreamInvalid
	}
	msg := err.Error()
	if strings.Contains(msg, "blocked") || strings.Contains(msg, "ssrf") {
		return http.StatusBadGateway, ErrUpstreamSSRF
	}
	return http.StatusBadGateway, ErrUpstreamInvalid
}
