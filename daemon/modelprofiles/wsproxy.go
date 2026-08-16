package modelprofiles

// Transparent Responses WebSocket upstream proxy shared by the per-session
// Router and the machine-level Gateway.
//
// Codex (responses wire API, supports_websockets=true) opens the Responses
// stream by upgrading GET <base_url>/responses to WebSocket. The frames are a
// fixed sequence of JSON protocol events (`response.create` request frames,
// `response.*` event frames) but Zen never needs to understand them: the
// proxy forwards every Text/Binary frame byte-for-byte in both directions,
// which preserves the user's model, effort, instructions, tools, MCP, and
// payload exactly (the machine-level contract).
//
// Ordering invariant: the upstream dial must reach 101 BEFORE the client
// handshake is completed. An upstream that rejects or cannot reach WebSocket
// therefore surfaces as an honest HTTP failure to the client (Codex then
// falls back to HTTPS POST through the same endpoint) instead of a phantom
// 101 followed by an abort.
//
// Control frames stay local to each leg (gorilla answers pings per
// connection); close frames and abrupt failures propagate to the peer with
// the received code so both sides see the same terminal state.

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsProxyMaxMessage caps one forwarded frame so a misbehaving peer cannot pin
// unbounded memory in the daemon. Codex Responses frames (JSON event batches)
// are far below this bound.
const wsProxyMaxMessage = 64 << 20

// websocketCloseGoingAway is the server-side close code for daemon shutdown.
const websocketCloseGoingAway = 1001

// wsProxyUpstreamDialTimeout bounds a single upstream WebSocket handshake.
const wsProxyUpstreamDialTimeout = 15 * time.Second

// wsConnRegistry tracks hijacked WebSocket connections so daemon shutdown
// (and gateway/router Close) tears them down deterministically; hijacked
// connections are not covered by http.Server.Shutdown.
type wsConnRegistry struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func newWSConnRegistry() *wsConnRegistry {
	return &wsConnRegistry{conns: map[*websocket.Conn]struct{}{}}
}

func (r *wsConnRegistry) add(c *websocket.Conn) {
	if r == nil || c == nil {
		return
	}
	r.mu.Lock()
	r.conns[c] = struct{}{}
	r.mu.Unlock()
}

func (r *wsConnRegistry) remove(c *websocket.Conn) {
	if r == nil || c == nil {
		return
	}
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

// closeAll sends a server-initiated close (daemon shutdown) and drops every
// tracked connection.
func (r *wsConnRegistry) closeAll(code int, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.conns = map[*websocket.Conn]struct{}{}
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
		_ = c.Close()
	}
}

// wsUpstreamDialer is the SSRF-safe upstream WebSocket dialer. It mirrors
// NewSafeHTTPClient's outbound policy: no ambient proxy, safe-host dialing,
// default TLS (system roots, no insecure skip), and pmde compression enabled
// so an upstream that supports permessage-deflate is used transparently.
var wsUpstreamDialer = websocket.Dialer{
	HandshakeTimeout:  wsProxyUpstreamDialTimeout,
	NetDialContext:    safeDialContext,
	Proxy:             func(*http.Request) (*url.URL, error) { return nil, nil },
	TLSClientConfig:   &tls.Config{},
	EnableCompression: true,
}

// wsUpstreamURL swaps an http(s) upstream URL to ws(s), preserving path and
// query. Codex derives its own WebSocket URL the same way.
func wsUpstreamURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrUpstreamInvalid
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrUpstreamInvalid
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already a websocket URL (test callers)
	default:
		return "", ErrUpstreamInvalid
	}
	return u.String(), nil
}

// buildWebSocketUpstreamHeaders forwards the client's own headers
// transparently (excluding WebSocket handshake state and auth, which are
// generated/injected by the proxy) so the upstream still sees user-agent,
// OpenAI-Beta, x-client-request-id, x-codex-*, and so on.
func buildWebSocketUpstreamHeaders(inbound http.Header) http.Header {
	h := http.Header{}
	for name, values := range inbound {
		canon := http.CanonicalHeaderKey(name)
		if isHopByHopHeaderName(canon) || strings.HasPrefix(strings.ToLower(canon), "sec-websocket-") {
			continue
		}
		switch canon {
		case "Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key",
			"Content-Length", "Host", "Origin":
			continue
		}
		for _, value := range values {
			h.Add(name, value)
		}
	}
	return h
}

// proxyWebSocketToUpstream completes the transparent proxy for an admitted
// inbound WebSocket Upgrade. The upstream dial happens first; only a real
// upstream 101 upgrades the client. The registry (never nil) tracks the
// client connection for lifecycle teardown.
func proxyWebSocketToUpstream(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	upstreamWSURL string,
	upstreamHeaders http.Header,
	registry *wsConnRegistry,
) {
	if registry == nil {
		registry = newWSConnRegistry()
	}
	upConn, resp, err := wsUpstreamDialer.DialContext(ctx, upstreamWSURL, upstreamHeaders)
	if err != nil {
		// Honest relay: an upstream handshake that answered with an HTTP status
		// keeps that status (Codex retries then falls back to HTTPS POST); a
		// transport failure is a 502. Never fabricate a client 101.
		status := http.StatusBadGateway
		if resp != nil && resp.StatusCode >= 400 {
			status = resp.StatusCode
		}
		writeRouteError(w, status, ErrUpstreamInvalid)
		return
	}

	upgrader := wsClientUpgrader()
	clientConn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		_ = upConn.Close()
		return
	}
	registry.add(clientConn)
	defer registry.remove(clientConn)
	clientConn.SetReadLimit(wsProxyMaxMessage)
	upConn.SetReadLimit(wsProxyMaxMessage)

	pumpWebSocketPair(clientConn, upConn)
}

// wsClientUpgrader accepts the loopback client handshake with optional
// permessage-deflate. CheckOrigin admits only empty origins or loopback
// origins; admission already restricted the peer address.
func wsClientUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			if err != nil {
				return false
			}
			host := strings.Trim(parsed.Hostname(), "[]")
			if host == "" {
				return false
			}
			if host == "localhost" {
				return true
			}
			ip := net.ParseIP(host)
			return ip != nil && ip.IsLoopback()
		},
		EnableCompression: true,
	}
}

// pumpWebSocketPair forwards frames in both directions until either side
// terminates, then propagates the terminal state to the peer and closes both
// connections. One goroutine per direction keeps writes serialized per conn.
func pumpWebSocketPair(a, b *websocket.Conn) {
	done := make(chan struct{}, 2)
	go pumpWebSocketOneWay(a, b, done)
	go pumpWebSocketOneWay(b, a, done)
	<-done
	<-done
	_ = a.Close()
	_ = b.Close()
}

// pumpWebSocketOneWay copies app frames from src to dst until src ends. The
// terminal close code is propagated to dst (a received close keeps its code;
// an abrupt failure becomes 1011 internal error) before this leg returns.
func pumpWebSocketOneWay(src, dst *websocket.Conn, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for {
		mtype, data, err := src.ReadMessage()
		if err != nil {
			code, reason := wsCloseFromError(err)
			_ = dst.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
			return
		}
		if err := dst.WriteMessage(mtype, data); err != nil {
			_ = src.Close()
			return
		}
	}
}

// wsCloseFromError derives the close code to propagate to the peer.
func wsCloseFromError(err error) (int, string) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		code := closeErr.Code
		if code == websocket.CloseNoStatusReceived { // must not be sent on the wire
			code = websocket.CloseNormalClosure
		}
		return code, strings.TrimSpace(closeErr.Text)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return websocket.CloseInternalServerErr, "upstream connection dropped"
	}
	return websocket.CloseInternalServerErr, "proxy connection error"
}