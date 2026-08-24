package modelprofiles

// Durable Responses WebSocket proxy shared by the machine Gateway and the
// per-session Router. The downstream Codex connection survives an upstream
// that closes after response.completed; the next response.create resolves and
// binds a fresh immutable upstream turn.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const wsProxyMaxMessage = 64 << 20
const websocketCloseGoingAway = 1001
const wsProxyUpstreamDialTimeout = 15 * time.Second

var wsProxyConnectionSequence atomic.Uint64

type wsConnRegistry struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
	// Shutdown is terminal for one listener generation. A 101 response can
	// reach the client just before the handler calls add, so late adds must
	// receive the same close frame instead of escaping the shutdown snapshot.
	closed      bool
	closeCode   int
	closeReason string
}

func newWSConnRegistry() *wsConnRegistry {
	return &wsConnRegistry{conns: map[*websocket.Conn]struct{}{}}
}
func (r *wsConnRegistry) add(c *websocket.Conn) bool {
	if r == nil || c == nil {
		return false
	}
	r.mu.Lock()
	if r.closed {
		code, reason := r.closeCode, r.closeReason
		r.mu.Unlock()
		closeWebSocket(c, code, reason)
		return false
	}
	r.conns[c] = struct{}{}
	r.mu.Unlock()
	return true
}
func (r *wsConnRegistry) remove(c *websocket.Conn) {
	if r == nil || c == nil {
		return
	}
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}
func (r *wsConnRegistry) closeAll(code int, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.closeCode = code
		r.closeReason = reason
	}
	conns := make([]*websocket.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.conns = map[*websocket.Conn]struct{}{}
	r.mu.Unlock()
	for _, c := range conns {
		closeWebSocket(c, code, reason)
	}
}

func closeWebSocket(c *websocket.Conn, code int, reason string) {
	if c == nil {
		return
	}
	_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
	_ = c.Close()
}

var wsUpstreamDialer = websocket.Dialer{
	HandshakeTimeout:  wsProxyUpstreamDialTimeout,
	NetDialContext:    safeDialContext,
	Proxy:             func(*http.Request) (*url.URL, error) { return nil, nil },
	TLSClientConfig:   &tls.Config{},
	EnableCompression: true,
}

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
	default:
		return "", ErrUpstreamInvalid
	}
	return u.String(), nil
}

func buildWebSocketUpstreamHeaders(inbound http.Header) http.Header {
	h := http.Header{}
	for name, values := range inbound {
		canon := http.CanonicalHeaderKey(name)
		if isHopByHopHeaderName(canon) || strings.HasPrefix(strings.ToLower(canon), "sec-websocket-") {
			continue
		}
		switch canon {
		case "Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key", "Content-Length", "Host", "Origin":
			continue
		}
		for _, value := range values {
			h.Add(name, value)
		}
	}
	return h
}

type wsProxyTarget struct {
	key     string
	url     string
	headers http.Header
	done    func(bool)
}

type wsProxyTargetResolver func(turn bool) (wsProxyTarget, error)

type wsProxyRead struct {
	messageType int
	payload     []byte
	err         error
}

type wsProxyLeg struct {
	target wsProxyTarget
	conn   *websocket.Conn
	read   <-chan wsProxyRead
	stop   chan struct{}
}

func (l *wsProxyLeg) close() {
	if l == nil {
		return
	}
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
	_ = l.conn.Close()
}

func proxyWebSocketToUpstream(ctx context.Context, w http.ResponseWriter, req *http.Request, resolve wsProxyTargetResolver, registry *wsConnRegistry) {
	if registry == nil {
		registry = newWSConnRegistry()
	}
	initial, err := resolve(false)
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, err)
		return
	}
	leg, resp, err := dialWSProxyLeg(ctx, initial)
	if err != nil {
		status := http.StatusBadGateway
		if resp != nil {
			status = http.StatusUpgradeRequired
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		writeRouteError(w, status, ErrUpstreamInvalid)
		return
	}
	upgrader := wsClientUpgrader()
	clientConn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		leg.close()
		return
	}
	connectionID := fmt.Sprintf("ws-%08x", wsProxyConnectionSequence.Add(1))
	clientConn.SetReadLimit(wsProxyMaxMessage)
	defer clientConn.Close()
	if !registry.add(clientConn) {
		leg.close()
		return
	}
	defer registry.remove(clientConn)
	defer func() {
		if leg != nil {
			leg.close()
		}
	}()

	clientStop := make(chan struct{})
	defer close(clientStop)
	clientRead := startWSProxyReader(clientConn, clientStop)
	var turnActive, completedForwarded bool
	var finishTurn func(bool)
	for {
		var upstreamRead <-chan wsProxyRead
		if leg != nil {
			upstreamRead = leg.read
		}
		select {
		case item := <-clientRead:
			if item.err != nil {
				code, reason, category := wsCloseDetails(item.err)
				wsProxyLog(connectionID, "client", code, category, completedForwarded)
				if finishTurn != nil {
					finishTurn(false)
				}
				if leg != nil {
					_ = leg.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
				}
				return
			}
			kind := wsJSONType(item.messageType, item.payload)
			if kind == "response.create" {
				if turnActive {
					if finishTurn != nil {
						finishTurn(false)
					}
					wsProxyClose(clientConn, websocket.CloseProtocolError, "overlapping response.create")
					return
				}
				target, targetErr := resolve(true)
				if targetErr != nil {
					wsProxyClose(clientConn, websocket.CloseInternalServerErr, "upstream unavailable")
					return
				}
				if leg == nil || leg.target.key != target.key {
					if leg != nil {
						_ = leg.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "provider changed"), time.Now().Add(time.Second))
						leg.close()
					}
					var dialErr error
					leg, _, dialErr = dialWSProxyLeg(ctx, target)
					if dialErr != nil {
						if target.done != nil {
							target.done(false)
						}
						wsProxyLog(connectionID, "upstream", websocket.CloseAbnormalClosure, "dial_failed", false)
						wsProxyClose(clientConn, websocket.CloseTryAgainLater, "upstream websocket unavailable")
						return
					}
				}
				finishTurn = target.done
				turnActive = true
				completedForwarded = false
			}
			if kind == "response.processed" && leg == nil {
				continue
			}
			if leg == nil {
				wsProxyClose(clientConn, websocket.CloseTryAgainLater, "upstream websocket unavailable")
				return
			}
			if err := leg.conn.WriteMessage(item.messageType, item.payload); err != nil {
				if finishTurn != nil {
					finishTurn(false)
				}
				wsProxyLog(connectionID, "upstream", closeCode(err), "write_failed", completedForwarded)
				wsProxyClose(clientConn, websocket.CloseInternalServerErr, "upstream write failed")
				return
			}
		case item := <-upstreamRead:
			if item.err != nil {
				code, reason, category := wsCloseDetails(item.err)
				wsProxyLog(connectionID, "upstream", code, category, completedForwarded)
				leg.close()
				leg = nil
				if !turnActive || completedForwarded {
					continue
				}
				if finishTurn != nil {
					finishTurn(false)
					finishTurn = nil
				}
				wsProxyClose(clientConn, code, reason)
				return
			}
			if err := clientConn.WriteMessage(item.messageType, item.payload); err != nil {
				if finishTurn != nil {
					finishTurn(false)
				}
				return
			}
			if wsJSONType(item.messageType, item.payload) == "response.completed" {
				completedForwarded = true
				turnActive = false
				if finishTurn != nil {
					finishTurn(true)
					finishTurn = nil
				}
				// A completed response is the turn boundary. Discard the old leg
				// even when the provider keeps it open so the next response.create
				// resolves and dials the current provider without an EOF race.
				leg.close()
				leg = nil
			}
		}
	}
}

func dialWSProxyLeg(ctx context.Context, target wsProxyTarget) (*wsProxyLeg, *http.Response, error) {
	conn, resp, err := wsUpstreamDialer.DialContext(ctx, target.url, target.headers)
	if err != nil {
		return nil, resp, err
	}
	conn.SetReadLimit(wsProxyMaxMessage)
	stop := make(chan struct{})
	return &wsProxyLeg{target: target, conn: conn, read: startWSProxyReader(conn, stop), stop: stop}, resp, nil
}

func startWSProxyReader(conn *websocket.Conn, stop <-chan struct{}) <-chan wsProxyRead {
	ch := make(chan wsProxyRead, 1)
	// Do not close ch: the single terminal result must disable this select arm,
	// not turn it into an always-ready stream of zero values.
	go func() {
		for {
			messageType, payload, err := conn.ReadMessage()
			select {
			case ch <- wsProxyRead{messageType: messageType, payload: payload, err: err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func wsJSONType(messageType int, payload []byte) string {
	if messageType != websocket.TextMessage {
		return ""
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return ""
	}
	return envelope.Type
}

func wsClientUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := strings.Trim(parsed.Hostname(), "[]")
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}, EnableCompression: true}
}

func wsProxyClose(conn *websocket.Conn, code int, reason string) {
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
}
func closeCode(err error) int        { code, _, _ := wsCloseDetails(err); return code }
func closeCategory(err error) string { _, _, category := wsCloseDetails(err); return category }
func wsCloseDetails(err error) (int, string, string) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		code := closeErr.Code
		if code == websocket.CloseNoStatusReceived {
			code = websocket.CloseNormalClosure
		}
		return code, strings.TrimSpace(closeErr.Text), "close_frame"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return websocket.CloseInternalServerErr, "upstream connection dropped", "transport_eof"
	}
	return websocket.CloseInternalServerErr, "proxy connection error", "transport_error"
}
func wsProxyLog(connectionID, leg string, code int, category string, completed bool) {
	log.Printf("codex websocket connection=%s leg=%s close_code=%d close_category=%s response_completed_forwarded=%t", connectionID, leg, code, category, completed)
}
