package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultGatewayListenAddr is the stable machine-level Codex gateway
// endpoint. The takeover projection bakes this address into ~/.codex/config.toml
// exactly once; every daemon restart must bind the same address (or repair the
// projection) before takeover can claim active.
const DefaultGatewayListenAddr = "127.0.0.1:38777"

// GatewayProviderName is the Codex model_provider identity projected into the
// CLI's native config by takeover. It is also the provider table key.
const GatewayProviderName = "zen-gateway"

// GatewayUpstream is the atomic upstream connection state of the machine-level
// gateway. It is derived from one Zen Provider connection (profile) and never
// carries secrets.
type GatewayUpstream struct {
	ProfileID     string
	BaseURL       string
	Protocol      string
	AuthMode      string
	CredentialEnv string
	CredentialRef string
}

// Gateway is Zen's stable loopback Codex endpoint. It proxies requests to the
// currently selected upstream connection, preserving the request bytes and
// client model exactly. Provider switching swaps the upstream atomically; the
// next request from every routed Codex process uses the new connection without
// CLI restart, Session kill/resume, or model substitution.
type Gateway struct {
	mu        sync.RWMutex
	upstream  GatewayUpstream
	creds     CredentialStore
	lookup    func(string) (string, bool)
	client    *http.Client
	maxBody   int64
	addr      string
	ln        net.Listener
	server    *http.Server
	statePath string
	ws        *wsConnRegistry
}

// GatewayOption configures Gateway construction (tests).
type GatewayOption func(*Gateway)

// WithGatewayLookup overrides credential env resolution (tests).
func WithGatewayLookup(lookup func(string) (string, bool)) GatewayOption {
	return func(g *Gateway) {
		if lookup != nil {
			g.lookup = lookup
		}
	}
}

// WithGatewayClient overrides the upstream HTTP client (tests).
func WithGatewayClient(client *http.Client) GatewayOption {
	return func(g *Gateway) {
		if client != nil {
			g.client = client
		}
	}
}

// WithGatewayMaxBody overrides the bounded request body size (tests).
func WithGatewayMaxBody(n int64) GatewayOption {
	return func(g *Gateway) {
		if n > 0 {
			g.maxBody = n
		}
	}
}

// NewGateway constructs the machine-level Codex gateway. addr is the stable
// loopback listen address (DefaultGatewayListenAddr in production).
func NewGateway(addr string, creds CredentialStore, opts ...GatewayOption) *Gateway {
	g := &Gateway{
		creds:     creds,
		client:    NewSafeHTTPClient(5 * time.Minute),
		maxBody:   MaxRouteRequestBodyBytes,
		addr:      strings.TrimSpace(addr),
		statePath: "",
		ws:        newWSConnRegistry(),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// SetGatewayStatePath binds the durable gateway state file (listen address and
// upstream profile id) so restart restores the same endpoint.
func (g *Gateway) SetGatewayStatePath(path string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.statePath = strings.TrimSpace(path)
}

// Addr returns the configured listen address.
func (g *Gateway) Addr() string {
	if g == nil {
		return ""
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.addr
}

// Listen binds the stable loopback address and serves the gateway. It fails
// closed when the address is taken (another daemon, or a stale takeover).
func (g *Gateway) Listen() error {
	if g == nil {
		return fmt.Errorf("%w: gateway not configured", ErrInvalid)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ln != nil {
		return nil
	}
	if g.addr == "" {
		return fmt.Errorf("%w: gateway listen address is required", ErrInvalid)
	}
	ln, err := net.Listen("tcp", g.addr)
	if err != nil {
		return fmt.Errorf("%w: gateway listen %s: %v", ErrInvalid, g.addr, err)
	}
	server := &http.Server{Handler: g}
	g.ln = ln
	g.server = server
	go func() {
		_ = server.Serve(ln)
	}()
	return nil
}

// ActualAddr returns the bound listener address (useful when listening on :0
// in tests). Empty when not listening.
func (g *Gateway) ActualAddr() string {
	if g == nil {
		return ""
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.ln == nil {
		return ""
	}
	return g.ln.Addr().String()
}

// Close stops the gateway listener.
func (g *Gateway) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	ln := g.ln
	server := g.server
	g.ln = nil
	g.server = nil
	g.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
	if g.ws != nil {
		g.ws.closeAll(websocketCloseGoingAway, "gateway shutting down")
	}
	return nil
}

// Listening reports whether the gateway listener is bound.
func (g *Gateway) Listening() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ln != nil
}

// SetUpstream atomically swaps the gateway's upstream connection. The next
// request uses the new connection; the running Codex processes are untouched.
func (g *Gateway) SetUpstream(upstream GatewayUpstream) {
	if g == nil {
		return
	}
	upstream = normalizeGatewayUpstream(upstream)
	g.mu.Lock()
	g.upstream = upstream
	g.mu.Unlock()
	_ = g.persistState()
}

// ClearUpstream removes the upstream; routed requests fail honestly until a
// Provider is selected.
func (g *Gateway) ClearUpstream() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.upstream = GatewayUpstream{}
	g.mu.Unlock()
	_ = g.persistState()
}

// Upstream returns the current upstream connection.
func (g *Gateway) Upstream() (GatewayUpstream, bool) {
	if g == nil {
		return GatewayUpstream{}, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	up := g.upstream
	return up, up.ProfileID != "" && up.BaseURL != ""
}

func normalizeGatewayUpstream(up GatewayUpstream) GatewayUpstream {
	up.ProfileID = normalizeSpace(up.ProfileID)
	up.BaseURL = normalizeSpace(up.BaseURL)
	up.Protocol = normalizeID(up.Protocol)
	up.AuthMode = normalizeID(up.AuthMode)
	up.CredentialEnv = normalizeSpace(up.CredentialEnv)
	up.CredentialRef = normalizeSpace(up.CredentialRef)
	return up
}

// GatewayUpstreamFromProfile derives the gateway upstream connection from a
// Zen Provider profile. Secret-free.
func GatewayUpstreamFromProfile(profile Profile) GatewayUpstream {
	profile = normalizeProfile(profile)
	ref := normalizeSpace(profile.CredentialRef)
	if ref == "" {
		ref = CredentialRefFor(profile.ID)
	}
	return GatewayUpstream{
		ProfileID:     normalizeSpace(profile.ID),
		BaseURL:       normalizeSpace(profile.BaseURL),
		Protocol:      normalizeID(profile.Protocol),
		AuthMode:      normalizeID(profile.AuthMode),
		CredentialEnv: normalizeSpace(profile.CredentialEnv),
		CredentialRef: ref,
	}
}

// ServeHTTP implements the loopback gateway handler. Every /v1/* request is
// proxied to the selected upstream with the request body preserved exactly;
// nothing is rewritten (model, effort, and client payload stay byte-identical).
func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if g == nil {
		writeRouteError(w, http.StatusServiceUnavailable, ErrInvalid)
		return
	}
	if !isLoopbackRemoteAddr(req.RemoteAddr) {
		writeRouteError(w, http.StatusForbidden, ErrRouteAdmissionDenied)
		return
	}
	upstream, ok := g.Upstream()
	if !ok {
		// Honest connection failure: never silently bypass to an old upstream.
		writeRouteError(w, http.StatusServiceUnavailable, fmt.Errorf("%w: gateway has no selected Provider", ErrUpstreamInvalid))
		return
	}
	if !strings.HasPrefix(req.URL.Path, "/v1/") && req.URL.Path != "/v1" {
		writeRouteError(w, http.StatusNotFound, ErrRoutePathMismatch)
		return
	}
	// Codex Responses-over-WebSocket: transparently proxy the Upgrade to the
	// current Provider connection with credentials injected and every frame
	// forwarded byte-for-byte (never a local 501 → WS/HTTPS fallback warning).
	// The upstream must reach 101 before this side completes its handshake;
	// any upstream failure is an honest HTTP error.
	if isWebSocketUpgrade(req) {
		if !strings.HasPrefix(req.URL.Path, "/v1/") {
			writeRouteError(w, http.StatusNotImplemented, ErrRouteWebSocket)
			return
		}
		target, targetErr := gatewayUpstreamRequestURL(upstream.BaseURL, req.URL)
		if targetErr != nil {
			writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
			return
		}
		wsURL, wsErr := wsUpstreamURL(target)
		if wsErr != nil {
			writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
			return
		}
		headers := buildWebSocketUpstreamHeaders(req.Header)
		if authErr := applyGatewayAuth(headers, upstream, req.Header, g.lookup, g.creds); authErr != nil {
			writeRouteError(w, http.StatusBadGateway, ErrCredentialNotReady)
			return
		}
		proxyWebSocketToUpstream(req.Context(), w, req, wsURL, headers, g.ws)
		return
	}
	if req.Method != http.MethodPost && req.Method != http.MethodGet {
		writeRouteError(w, http.StatusMethodNotAllowed, ErrRouteMethodMismatch)
		return
	}
	body, _, err := readBoundedBody(req, g.maxBody)
	if err != nil {
		if errors.Is(err, ErrRequestBodyTooLarge) {
			writeRouteError(w, http.StatusRequestEntityTooLarge, ErrRequestBodyTooLarge)
			return
		}
		writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
		return
	}

	target, err := gatewayUpstreamRequestURL(upstream.BaseURL, req.URL)
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
		return
	}

	upReq, err := http.NewRequestWithContext(req.Context(), req.Method, target, bytes.NewReader(body))
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrUpstreamInvalid)
		return
	}
	copyInboundHeaders(upReq.Header, req.Header)
	if err := applyGatewayAuth(upReq.Header, upstream, req.Header, g.lookup, g.creds); err != nil {
		writeRouteError(w, http.StatusBadGateway, ErrCredentialNotReady)
		return
	}
	if len(body) > 0 {
		upReq.Header.Set("Content-Type", firstNonEmpty(req.Header.Get("Content-Type"), "application/json"))
		upReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		upReq.ContentLength = int64(len(body))
	}
	// The body bytes are preserved exactly, so the original Content-Encoding is
	// forwarded as-is (unlike the rewriting router, which decodes at the edge).

	resp, err := g.client.Do(upReq)
	if err != nil {
		status, typed := classifyUpstreamDoError(err)
		writeRouteError(w, status, typed)
		return
	}
	defer resp.Body.Close()
	copySafeResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if err := streamCopyFlush(w, resp.Body); err != nil {
		return
	}
}

// applyGatewayAuth injects the upstream credential per AuthMode. It mirrors the
// router's auth semantics; env values and stored secrets never leave the
// request header mutation.
func applyGatewayAuth(dst http.Header, upstream GatewayUpstream, inbound http.Header, lookup func(string) (string, bool), store CredentialStore) error {
	mode := normalizeID(upstream.AuthMode)
	if mode == "" {
		mode = AuthModeNone
	}
	switch mode {
	case AuthModeNone:
		return nil
	case AuthModeBearerEnv:
		token, err := resolveProviderSecret(upstream.CredentialRef, upstream.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		dst.Set("Authorization", "Bearer "+token)
		return nil
	case AuthModeXAPIKeyEnv:
		token, err := resolveProviderSecret(upstream.CredentialRef, upstream.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		dst.Set("X-Api-Key", token)
		return nil
	case AuthModeNativePassthrough:
		// Codex is projected with requires_openai_auth = false, so passthrough
		// relies on the client sending its own native auth headers. When they
		// are absent the upstream rejects honestly.
		if value := inbound.Get("Authorization"); value != "" {
			dst.Set("Authorization", value)
		}
		if value := inbound.Get("X-Api-Key"); value != "" {
			dst.Set("X-Api-Key", value)
		}
		if value := inbound.Get("Api-Key"); value != "" {
			dst.Set("Api-Key", value)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown auth_mode", ErrInvalid)
	}
}

// gatewayUpstreamRequestURL joins the Provider base URL with the inbound
// /v1/... path + query, mirroring UpstreamRequestURL semantics so a base that
// already ends in /v1 is not duplicated (Profile bases routinely carry /v1).
func gatewayUpstreamRequestURL(base string, reqURL *url.URL) (string, error) {
	if !strings.HasPrefix(reqURL.Path, "/v1/") {
		target, err := url.Parse(normalizeSpace(base))
		if err != nil || target.Scheme == "" || target.Host == "" {
			return "", ErrUpstreamInvalid
		}
		target.Path = strings.TrimRight(target.Path, "/") + reqURL.Path
		target.RawQuery = reqURL.RawQuery
		target.Fragment = ""
		return target.String(), nil
	}
	target, err := UpstreamRequestURL(normalizeSpace(base), reqURL.Path)
	if err != nil {
		return "", err
	}
	return withRawQuery(target, reqURL.RawQuery)
}

// gatewayStateDocument is the durable gateway state: the stable listen address
// and the selected upstream profile id (never secrets, never the upstream URL —
// restart resolves the profile from the Provider catalog).
type gatewayStateDocument struct {
	ListenAddr       string `json:"listen_addr"`
	UpstreamProfileID string `json:"upstream_profile_id,omitempty"`
}

func (g *Gateway) persistState() error {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	path := g.statePath
	upstream := g.upstream
	addr := g.addr
	g.mu.RUnlock()
	if path == "" {
		return nil
	}
	doc := gatewayStateDocument{ListenAddr: addr, UpstreamProfileID: upstream.ProfileID}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, raw, 0o600)
}

// LoadGatewayState reads the durable gateway state.
func LoadGatewayState(path string) (listenAddr, upstreamProfileID string, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var doc gatewayStateDocument
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return "", "", fmt.Errorf("%w: gateway state: %v", ErrRouteSnapshotInvalid, err)
	}
	return strings.TrimSpace(doc.ListenAddr), strings.TrimSpace(doc.UpstreamProfileID), nil
}

// gatewayStateDirDefault returns the daemon-owned gateway state directory
// under the Zen storage root.
func gatewayStateDirDefault(storageDir string) string {
	return filepath.Join(strings.TrimSpace(storageDir), "codex-gateway")
}
