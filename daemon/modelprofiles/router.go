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
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/codexctl"
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
	// modelSwitch applies a model/effect identity carried with Codex's reserved
	// explicit model-switch contextual signal, or with an authoritative native
	// settings snapshot for fragment-less changes.
	modelSwitch func(routeID, modelID, effort string, effortPresent bool) error
	// nativeSettings returns the authoritative applied native thread settings
	// for a route (persistent app-server subscription snapshot). Nil disables
	// fragment-less native convergence.
	nativeSettings func(routeID string) (codexctl.NativeSettings, bool)
	// ws tracks hijacked Responses WebSocket connections so shutdown tears
	// them down deterministically.
	ws *wsConnRegistry
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

// WithRouterModelSwitch installs the explicit Terminal /model mutation path.
func WithRouterModelSwitch(apply func(routeID, modelID, effort string, effortPresent bool) error) RouterOption {
	return func(r *Router) {
		r.modelSwitch = apply
	}
}

// WithRouterNativeSettings installs the authoritative native thread-settings
// source (the live-control app-server subscription snapshot). With it, a
// fragment-less request whose model/effort differs from the binding is
// checked against the native thread before deciding converge vs normalize.
func WithRouterNativeSettings(lookup func(routeID string) (codexctl.NativeSettings, bool)) RouterOption {
	return func(r *Router) {
		r.nativeSettings = lookup
	}
}

// NewRouter constructs a loopback routing runtime over table.
func NewRouter(table *RouteTable, opts ...RouterOption) *Router {
	r := &Router{
		table:   table,
		client:  NewSafeHTTPClient(5 * time.Minute),
		maxBody: MaxRouteRequestBodyBytes,
		ws:      newWSConnRegistry(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// CloseWebSocketConnections drops every hijacked Responses WebSocket
// connection. Called by Owner.Close after the HTTP server shutdown so no
// long-lived socket survives the daemon process teardown; hijacked
// connections are not covered by http.Server.Shutdown.
func (r *Router) CloseWebSocketConnections() {
	if r == nil || r.ws == nil {
		return
	}
	r.ws.closeAll(websocketCloseGoingAway, "zen daemon shutting down")
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

	parsed, err := ParseRouteRequestPath(req.URL.Path)
	if err != nil {
		writeRouteError(w, http.StatusNotFound, ErrRoutePathMismatch)
		return
	}
	if isWebSocketUpgrade(req) {
		r.serveRouteWebSocket(w, req, parsed)
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
		requestModel, modelErr := requestModelFromBody(body)
		if modelErr != nil {
			writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
			return
		}

		requestEffort, requestEffortPresent := requestEffortFromBody(body)
		explicitModelSwitch, signalErr := requestHasModelSwitchSignal(body)
		if signalErr != nil {
			writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
			return
		}
		if explicitModelSwitch && parsed.Endpoint == EndpointResponses {
			if r.modelSwitch == nil {
				writeRouteError(w, http.StatusServiceUnavailable, fmt.Errorf("%w: terminal model switch unavailable", ErrInvalid))
				return
			}
			// This lease has not reached an upstream. Release it before applying
			// the explicit mutation so route activation cannot mistake the local
			// Router parse phase for an old-provider in-flight response and enable
			// portable-history degradation unnecessarily.
			if err := r.table.EndRouteFlight(parsed.RouteID, flightToken, false); err != nil {
				writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
				return
			}
			completed = true
			if err := r.modelSwitch(binding.RouteID, requestModel, requestEffort, requestEffortPresent); err != nil {
				writeRouteError(w, http.StatusBadRequest, fmt.Errorf("%w: terminal model switch: %v", ErrInvalid, err))
				return
			}
			binding, flightToken, err = r.table.BeginRouteFlight(parsed.RouteID)
			if err != nil {
				writeRouteError(w, http.StatusNotFound, ErrRouteNotFound)
				return
			}
			completed = false
		}

		// Fragment-less native convergence: a same-model effort change (or any
		// other native thread-settings change that carries no reserved
		// model-switch fragment) is invisible in the request body alone. For
		// live-control sessions the authoritative native settings snapshot
		// (persistent app-server subscription) decides: when the native thread
		// itself confirms the request's model/effort, the route is converged
		// BEFORE forwarding (same mutation path as the reserved signal); when
		// the native thread still matches the binding, the request is a stale
		// in-flight payload and is normalized to the binding. Unavailable or
		// ambiguous evidence fails closed to the binding.
		if !explicitModelSwitch && parsed.Endpoint == EndpointResponses && r.nativeSettings != nil &&
			requestBodyDiffersFromBinding(binding, requestModel, requestEffort, requestEffortPresent) {
			if settings, ok := r.nativeSettings(binding.RouteID); ok {
				if settings.Model != binding.ClientModel ||
					normalizeID(settings.Effort) != normalizeID(binding.ReasoningEffort) {
					// Pending native change: converge the route, then re-begin
					// the flight so the rewrite block normalizes to the new
					// binding. A generation-CAS conflict means a concurrent
					// mutation won; the re-read below fails closed to the
					// authoritative binding.
					if r.modelSwitch != nil {
						if err := r.table.EndRouteFlight(parsed.RouteID, flightToken, false); err != nil {
							writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
							return
						}
						completed = true
						_ = r.modelSwitch(binding.RouteID, settings.Model, settings.Effort, settings.Effort != "")
						binding, flightToken, err = r.table.BeginRouteFlight(parsed.RouteID)
						if err != nil {
							writeRouteError(w, http.StatusNotFound, ErrRouteNotFound)
							return
						}
						completed = false
					}
				}
			}
		}

		// The route binding is the acknowledged Session runtime. A request body
		// is a transport payload from a potentially stale CLI process, not an
		// intent signal. Normalize it to the immutable flight snapshot instead of
		// guessing whether a mismatch came from a stale request or /model.
		bindingModel := normalizeSpace(binding.UpstreamModel)
		bindingEffort := normalizeID(binding.ReasoningEffort)
		if requestModel != bindingModel {
			rewritten, err = rewriteRequestModel(body, bindingModel)
			if err != nil {
				writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
				return
			}
		}
		if parsed.Endpoint == EndpointResponses {
			requestEffort, requestEffortPresent = requestEffortFromBody(rewritten)
			if bindingEffort != "" {
				if !requestEffortPresent || requestEffort != bindingEffort {
					rewritten, err = rewriteRequestEffort(rewritten, bindingEffort)
					if err != nil {
						writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
						return
					}
				}
			} else if requestEffortPresent {
				// An empty binding means the model's default effect. Remove a
				// stale explicit effect rather than letting it mutate the Session.
				rewritten, err = clearRequestEffort(rewritten)
				if err != nil {
					writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
					return
				}
			}
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

// serveRouteWebSocket transparently proxies a Codex Responses-over-WebSocket
// Upgrade for an admitted per-session route. The route binding elects the
// upstream; frames pass byte-for-byte in both directions (no model/effort
// rewrite — the same transparency contract as the machine-level Gateway, and
// the Codex client uses exactly one transport per process). The upstream must
// reach 101 before the client handshake completes; any other outcome is an
// honest HTTP error so Codex falls back to HTTPS POST through the same route.
// GET /v1/models is local-only and never upgraded.
func (r *Router) serveRouteWebSocket(w http.ResponseWriter, req *http.Request, parsed ParsedRouteRequest) {
	if r == nil || r.table == nil {
		writeRouteError(w, http.StatusServiceUnavailable, ErrRouteNotFound)
		return
	}
	if parsed.Endpoint != EndpointResponses {
		// No client speaks WebSocket for other endpoints; keep the honest 501
		// marker instead of silently accepting an unsupported upgrade.
		writeRouteError(w, http.StatusNotImplemented, ErrRouteWebSocket)
		return
	}
	resolve := func(turn bool) (wsProxyTarget, error) {
		var binding RouteBinding
		var flightToken string
		var err error
		if turn {
			binding, flightToken, err = r.table.BeginRouteFlight(parsed.RouteID)
		} else {
			var ok bool
			binding, ok = r.table.GetByRouteID(parsed.RouteID)
			if !ok {
				err = ErrRouteNotFound
			}
		}
		if err != nil {
			return wsProxyTarget{}, err
		}
		release := func(bool) {}
		if turn {
			var once sync.Once
			release = func(markOpaque bool) {
				once.Do(func() { _ = r.table.EndRouteFlight(parsed.RouteID, flightToken, markOpaque) })
			}
		}
		fail := func(err error) (wsProxyTarget, error) {
			release(false)
			return wsProxyTarget{}, err
		}
		if !EndpointAllowedForProtocol(binding.RouteProtocol, parsed.Endpoint) {
			return fail(ErrRouteProtocolMismatch)
		}
		upstreamURL, urlErr := UpstreamRequestURL(binding.UpstreamBaseURL, parsed.APIPath)
		if urlErr != nil {
			return fail(ErrUpstreamInvalid)
		}
		upstreamURL, urlErr = withRawQuery(upstreamURL, req.URL.RawQuery)
		if urlErr != nil {
			return fail(ErrUpstreamInvalid)
		}
		wsURL, urlErr := wsUpstreamURL(upstreamURL)
		if urlErr != nil {
			return fail(ErrUpstreamInvalid)
		}
		headers := buildWebSocketUpstreamHeaders(req.Header)
		if authErr := applyUpstreamAuth(headers, binding, captureInboundAuth(req.Header), r.credentialLookup(), r.creds); authErr != nil {
			return fail(ErrCredentialNotReady)
		}
		return wsProxyTarget{
			key: fmt.Sprintf("%s:%d", binding.ProfileID, binding.Generation),
			url: wsURL, headers: headers, done: release,
		}, nil
	}
	proxyWebSocketToUpstream(req.Context(), w, req, resolve, r.ws)
}

// serveLocalModels answers GET /v1/models with the synced model catalog of
// the route's connection as a standard OpenAI list payload. Only available
// models are listed — the same set the App picker and default binding use.
// serveLocalModels serves the Codex-expected ModelsResponse shape
// (`{"models":[...]}` — NOT the OpenAI list `data` shape): daemon-known
// metadata for the route connection's available models. Unknown models are
// never projected; the Codex picker therefore only ever offers identities the
// daemon can launch and route truthfully.
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
	available := make([]ProviderModelEntry, 0, len(entries)+1)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Available {
			available = append(available, entry)
			seen[normalizeSpace(entry.ID)] = struct{}{}
		}
	}
	// The running identity always appears so the CLI never loses it.
	if model := normalizeSpace(binding.UpstreamModel); model != "" {
		if _, ok := seen[model]; !ok {
			available = append(available, ProviderModelEntry{ID: model, Available: true, Source: ModelSourceManual})
		}
	}
	payload, err := json.Marshal(CodexModelsResponseForEntries(available))
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

// requestBodyDiffersFromBinding reports whether the request's model/effort
// identity differs from the acknowledged route binding. Effort follows Zen
// semantics: absent and the native "none" both mean model default ("").
func requestBodyDiffersFromBinding(binding RouteBinding, requestModel, requestEffort string, requestEffortPresent bool) bool {
	if requestModel != binding.ClientModel {
		return true
	}
	bodyEffort := ""
	if requestEffortPresent {
		bodyEffort = normalizeID(requestEffort)
		if bodyEffort == ReasoningEffortNone {
			bodyEffort = ""
		}
	}
	return bodyEffort != normalizeID(binding.ReasoningEffort)
}
