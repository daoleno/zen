package modelprofiles

import (
	"bytes"
	"context"
	"encoding/json"
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
	// models resolves a route's connection to its synced model catalog for the
	// local GET /v1/models surface. Nil means the surface is unavailable.
	models func(profileID string) ([]ProviderModelEntry, error)
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

// WithRouterModelCatalog installs the resolver for the local GET /v1/models
// surface. It receives the route binding's connection id and must return the
// synced (discovery-cache) model entries for that connection.
func WithRouterModelCatalog(models func(profileID string) ([]ProviderModelEntry, error)) RouterOption {
	return func(r *Router) {
		r.models = models
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

	parsed, err := ParseRouteRequestPath(req.URL.Path)
	if err != nil {
		writeRouteError(w, http.StatusNotFound, ErrRoutePathMismatch)
		return
	}

	// GET /v1/models is the local catalog surface; every other admitted
	// endpoint is a POST request path.
	if parsed.Endpoint == EndpointModels {
		if req.Method != http.MethodGet {
			writeRouteError(w, http.StatusMethodNotAllowed, ErrRouteMethodMismatch)
			return
		}
	} else if req.Method != http.MethodPost {
		writeRouteError(w, http.StatusMethodNotAllowed, ErrRouteMethodMismatch)
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

	// The local model catalog is served from the synced discovery cache of the
	// route's connection; nothing is forwarded upstream and no live discovery
	// is triggered. The flight lease is released by the deferred end (no
	// opaque-history marking: nothing upstream ran).
	if parsed.Endpoint == EndpointModels {
		r.serveLocalModels(w, binding)
		return
	}

	inboundAuth := captureInboundAuth(req.Header)

	body, decodedEncoding, err := readBoundedBody(req, r.maxBody)
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
	// The body was decompressed at the router boundary; never forward the
	// original Content-Encoding with plain JSON bytes.
	if decodedEncoding {
		upReq.Header.Del("Content-Encoding")
	}
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

// serveLocalModels answers GET /v1/models with the synced model catalog of
// the route's connection as a standard OpenAI list payload. Only available
// models are listed — the same set the App picker and default binding use.
func (r *Router) serveLocalModels(w http.ResponseWriter, binding RouteBinding) {
	if r.models == nil {
		writeRouteError(w, http.StatusServiceUnavailable, fmt.Errorf("%w: model catalog not configured", ErrInvalid))
		return
	}
	entries, err := r.models(binding.ProfileID)
	if err != nil {
		// The route's connection is gone: the catalog cannot resolve.
		if errors.Is(err, ErrRouteNotFound) || errors.Is(err, ErrNotFound) {
			writeRouteError(w, http.StatusNotFound, ErrRouteNotFound)
			return
		}
		writeRouteError(w, http.StatusServiceUnavailable, fmt.Errorf("%w: model catalog: %v", ErrInvalid, err))
		return
	}
	payload, err := modelsListPayload(entries)
	if err != nil {
		writeRouteError(w, http.StatusInternalServerError, fmt.Errorf("%w: model list encode: %v", ErrInvalid, err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// openAIModelList is the standard OpenAI GET /v1/models response shape that
// Codex's native /model switch reads (data[].id).
type openAIModelList struct {
	Object string           `json:"object"`
	Data   []openAIModelObj `json:"data"`
}

type openAIModelObj struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

func modelsListPayload(entries []ProviderModelEntry) ([]byte, error) {
	data := make([]openAIModelObj, 0, len(entries))
	for _, entry := range entries {
		if !entry.Available {
			continue
		}
		data = append(data, openAIModelObj{
			ID:      entry.ID,
			Object:  "model",
			OwnedBy: "zen",
		})
	}
	return json.Marshal(openAIModelList{Object: "list", Data: data})
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

// readBoundedBody reads the request body with a hard byte bound and applies
// request Content-Encoding decoding at the router boundary. The second return
// reports whether the body was decoded (caller must strip Content-Encoding
// before forwarding the rewritten plain JSON upstream).
func readBoundedBody(req *http.Request, max int64) ([]byte, bool, error) {
	if max <= 0 {
		max = MaxRouteRequestBodyBytes
	}
	limited := io.LimitReader(req.Body, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if int64(len(body)) > max {
		return nil, false, ErrRequestBodyTooLarge
	}
	decoded, isDecoded, err := decodeBodyEncoding(req.Header.Get("Content-Encoding"), body, max)
	if err != nil {
		return nil, false, err
	}
	return decoded, isDecoded, nil
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
