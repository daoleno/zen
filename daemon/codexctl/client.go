// Package codexctl is the Zen daemon's live-control client for a running
// Codex 0.147 app server.
//
// Codex exposes exactly one supported external mechanism for mutating the
// model/reasoning-effort of a live native thread without restarting the
// process or entering conversation history: the app-server JSON-RPC method
// `thread/settings/update` (with the experimentalApi capability), followed by
// the authoritative `thread/settings/updated` notification carrying the
// applied ThreadSettings snapshot.
//
// This client speaks that protocol over the app-server control transport
// (WebSocket-over-unix-socket). It is deliberately narrow: initialize,
// thread resolution, thread/settings/update, and the applied-settings
// acknowledgement. It never injects terminal keystrokes and never fabricates
// TUI state.
package codexctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Method names on the Codex app-server JSON-RPC surface (camelCase).
const (
	methodInitialize        = "initialize"
	methodThreadLoadedList  = "thread/loaded/list"
	methodThreadList        = "thread/list"
	methodThreadResume      = "thread/resume"
	methodThreadSettingsUpd = "thread/settings/update"
	notifThreadSettingsUpd  = "thread/settings/updated"
)

// Errors surfaced to callers (wrapped with detail).
var (
	ErrConnect    = errors.New("codex app-server connect")
	ErrProtocol   = errors.New("codex app-server protocol")
	ErrNoThread   = errors.New("codex app-server thread")
	ErrNoSettings = errors.New("codex app-server settings")
	ErrApply      = errors.New("codex thread settings apply")
)

// DefaultAckTimeout bounds the wait for the applied-settings acknowledgement
// after `thread/settings/update` succeeds. The RPC response only means the
// update was queued; the notification is the native applied-state proof.
const DefaultAckTimeout = 12 * time.Second

// Settings is the native applied thread-settings snapshot as reported by the
// `thread/settings/updated` notification.
type Settings struct {
	ThreadID      string
	Model         string
	ModelProvider string
	Effort        string // empty when the thread has no explicit effort
}

// Client is a live connection to one Codex app server control socket.
// A Client is safe for concurrent use; JSON-RPC responses are correlated by
// request id on a single reader pump.
type Client struct {
	conn         *websocket.Conn
	mu           sync.Mutex // write lock
	pending      map[string]chan rpcResponse
	notif        chan Notification
	closed       chan struct{}
	closeOnce    sync.Once
	closeErr     error
	nextID       uint64
	clientName   string
	resolveRetry time.Duration
}

// Notification is a server-pushed JSON-RPC notification.
type Notification struct {
	Method string
	Params json.RawMessage
}

type rpcRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// LiveControl is the native thread-settings mutation surface the server layer
// uses for the native-first runtime transaction. *Client implements it.
type LiveControl interface {
	// ResolveThread identifies the app server's primary native thread for the
	// session cwd.
	ResolveThread(ctx context.Context, cwd string) (string, error)
	// ApplySettings applies model+effort and waits for the native
	// thread/settings/updated acknowledgement; the returned revert re-applies
	// previousSettings best-effort.
	ApplySettings(ctx context.Context, threadID, model string, effort *string, previous Settings, ackTimeout time.Duration) (revert func(ctx context.Context) error, err error)
	Close() error
}

// DialOptions configures Open.
type DialOptions struct {
	// ClientName is advertised in initialize (must be a valid HTTP header
	// value; defaults to "zen").
	ClientName string
	// AckTimeout bounds the applied-settings acknowledgement wait.
	AckTimeout time.Duration
	// DialTimeout bounds the socket connect + initialize handshake.
	DialTimeout time.Duration
	// ResolveRetryWindow bounds the thread-list flush retry in ResolveThread
	// (default 6s; tests may set 0 to disable retries).
	ResolveRetryWindow time.Duration
}

// Open dials the app-server control socket and performs the initialize
// handshake with the experimentalApi capability (required for
// thread/settings/update).
func Open(ctx context.Context, socketPath string, opts DialOptions) (*Client, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("%w: empty socket path", ErrConnect)
	}
	if opts.ClientName == "" {
		opts.ClientName = "zen"
	}
	if opts.AckTimeout <= 0 {
		opts.AckTimeout = DefaultAckTimeout
	}
	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: dialTimeout,
		NetDial: func(network, addr string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, _, err := dialer.DialContext(dialCtx, "ws://zen-codex-ctl/", http.Header{
		"User-Agent": []string{"zen/" + opts.ClientName},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConnect, socketPath, err)
	}
	resolveRetry := opts.ResolveRetryWindow
	if resolveRetry <= 0 {
		resolveRetry = 6 * time.Second
	}
	c := &Client{
		conn:         conn,
		pending:      map[string]chan rpcResponse{},
		notif:        make(chan Notification, 512),
		closed:       make(chan struct{}),
		clientName:   opts.ClientName,
		resolveRetry: resolveRetry,
	}
	go c.readPump()
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// initialize performs the JSON-RPC initialize handshake declaring the
// experimentalApi capability.
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    c.clientName,
			"version": "1",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}
	if _, err := c.call(ctx, methodInitialize, params); err != nil {
		return fmt.Errorf("%w: initialize: %v", ErrProtocol, err)
	}
	return nil
}

// Notifications returns the server-pushed notification stream. The channel is
// closed when the Client is closed.
func (c *Client) Notifications() <-chan Notification {
	return c.notif
}

// Close terminates the connection. Safe to call multiple times.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		close(c.closed)
		conn := c.conn
		c.mu.Unlock()
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = conn.Close()
	})
	return c.closeErr
}

// Err returns the terminal connection error once the read pump has stopped.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

func (c *Client) readPump() {
	defer func() {
		c.mu.Lock()
		c.closeErr = errors.New("codex app-server connection closed")
		close(c.notif)
		for id, ch := range c.pending {
			select {
			case ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: "connection closed"}}:
			default:
			}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			ID     *uint64         `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.ID != nil {
			c.mu.Lock()
			ch := c.pending[fmt.Sprintf("%d", *envelope.ID)]
			if ch != nil {
				select {
				case ch <- rpcResponse{ID: *envelope.ID, Result: envelope.Result, Error: envelope.Error}:
				default:
				}
				delete(c.pending, fmt.Sprintf("%d", *envelope.ID))
			}
			c.mu.Unlock()
			continue
		}
		if envelope.Method != "" {
			select {
			case c.notif <- Notification{Method: envelope.Method, Params: envelope.Params}:
			default:
			}
		}
	}
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: closed", ErrConnect)
	default:
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[fmt.Sprintf("%d", id)] = ch
	req := rpcRequest{ID: id, Method: method, Params: raw}
	if err := c.conn.WriteJSON(req); err != nil {
		delete(c.pending, fmt.Sprintf("%d", id))
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: write: %v", ErrProtocol, err)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, &rpcCallError{Method: method, Code: resp.Error.Code, Message: resp.Error.Message}
		}
		return resp.Result, nil
	case <-c.closed:
		return nil, fmt.Errorf("%w: connection closed during %s", ErrConnect, method)
	}
}

type rpcCallError struct {
	Method  string
	Code    int
	Message string
}

func (e *rpcCallError) Error() string {
	return fmt.Sprintf("codex %s failed (code %d): %s", e.Method, e.Code, e.Message)
}

// LoadedThreadIDs lists thread ids currently loaded in the app server.
func (c *Client) LoadedThreadIDs(ctx context.Context) ([]string, error) {
	result, err := c.call(ctx, methodThreadLoadedList, map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("%w: thread/loaded/list: %v", ErrProtocol, err)
	}
	return payload.Data, nil
}

// ThreadInfo is the subset of a native Thread record needed for resolution.
type ThreadInfo struct {
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	ParentID  string `json:"parentThreadId"`
	UpdatedAt int64  `json:"updatedAt"`
	Model     string `json:"model"`
}

// ListThreads returns native threads whose session cwd matches exactly.
func (c *Client) ListThreads(ctx context.Context, cwd string, limit int) ([]ThreadInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	params := map[string]any{
		"cwd":   cwd,
		"limit": limit,
	}
	result, err := c.call(ctx, methodThreadList, params)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []ThreadInfo `json:"data"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("%w: thread/list: %v", ErrProtocol, err)
	}
	return payload.Data, nil
}

// ResolveThread identifies the app server's primary native thread for a
// session cwd. Loaded threads of this app server are preferred; the store
// listing can lag the in-memory registration by a moment after a turn, so the
// listing is retried briefly. An empty cwd resolves the single loaded
// non-subagent thread when unambiguous.
func (c *Client) ResolveThread(ctx context.Context, cwd string) (string, error) {
	loaded, err := c.LoadedThreadIDs(ctx)
	if err != nil {
		return "", err
	}
	if len(loaded) == 0 {
		return "", fmt.Errorf("%w: no native thread loaded", ErrNoThread)
	}
	// The thread store can flush a moment after the thread is live in memory;
	// retry the cwd-filtered listing briefly before falling back.
	deadline := time.Now().Add(c.resolveRetry)
	for {
		candidates, listErr := c.listLoadedThreads(ctx, cwd, loaded)
		if listErr == nil && len(candidates) > 0 {
			return pickPrimaryThread(candidates), nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	// Fallback: a per-session app server loads exactly the pane's threads;
	// a single loaded thread is the pane's primary thread.
	if len(loaded) == 1 {
		return loaded[0], nil
	}
	return "", fmt.Errorf("%w: %d loaded threads for cwd %s (thread listing unavailable)", ErrNoThread, len(loaded), cwd)
}

// listLoadedThreads returns the cwd-filtered store listing restricted to the
// currently loaded thread ids.
func (c *Client) listLoadedThreads(ctx context.Context, cwd string, loaded []string) ([]ThreadInfo, error) {
	listed, err := c.ListThreads(ctx, cwd, 50)
	if err != nil {
		return nil, err
	}
	byID := map[string]bool{}
	for _, id := range loaded {
		byID[id] = true
	}
	candidates := make([]ThreadInfo, 0, len(listed))
	for _, t := range listed {
		if byID[t.ID] {
			candidates = append(candidates, t)
		}
	}
	return candidates, nil
}

// pickPrimaryThread ranks candidates: main (non-subagent) threads before
// subagents, active before idle, most recently updated first.
func pickPrimaryThread(candidates []ThreadInfo) string {
	sort.SliceStable(candidates, func(i, j int) bool {
		ai, aj := threadRank(candidates[i]), threadRank(candidates[j])
		if ai != aj {
			return ai > aj
		}
		return candidates[i].UpdatedAt > candidates[j].UpdatedAt
	})
	for _, t := range candidates {
		if t.ParentID == "" && t.Status != "notLoaded" {
			return t.ID
		}
	}
	return candidates[0].ID
}

func threadRank(t ThreadInfo) int {
	switch strings.ToLower(t.Status) {
	case "active":
		return 3
	case "idle":
		return 2
	default:
		return 1
	}
}

// ApplySettings mutates the live native thread model + reasoning effort via
// `thread/settings/update` and blocks until the `thread/settings/updated`
// notification acknowledges the exact applied settings. The returned revert
// closure re-applies previousSettings best-effort (idempotent; safe to call
// once).
//
// Effort semantics match Codex 0.147 exactly: the native wire value "none"
// (ReasoningEffort::None, shown as "default" in the TUI footer) is the
// supported representation of "model default effort" — it is what the TUI
// itself sends when the user picks the default. A nil effort therefore maps
// to `"effort": "none"` rather than omitting the field: omission leaves the
// thread's current effort unchanged, which would acknowledge a state Zen's
// projection cannot hold.
//
// The applied-settings notification is delivered only to connections attached
// to the thread, so the client first attaches with the native `thread/resume`
// path (the same attach the TUI uses to reconnect to a running thread — it
// does not restart the thread or lose context).
func (c *Client) ApplySettings(ctx context.Context, threadID, model string, effort *string, previousSettings Settings, ackTimeout time.Duration) (revert func(ctx context.Context) error, err error) {
	if strings.TrimSpace(threadID) == "" {
		return nil, fmt.Errorf("%w: thread id required", ErrApply)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("%w: model required", ErrApply)
	}
	if err := c.AttachThread(ctx, threadID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrApply, err)
	}
	params := map[string]any{
		"threadId": threadID,
		"model":    model,
	}
	if effort != nil && strings.TrimSpace(*effort) != "" {
		params["effort"] = strings.TrimSpace(*effort)
	} else {
		// Zen's "model default" is the native ReasoningEffort::None.
		params["effort"] = "none"
	}
	if _, err := c.call(ctx, methodThreadSettingsUpd, params); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrApply, err)
	}
	if err := c.waitSettingsAck(ctx, threadID, model, effort, ackTimeout); err != nil {
		return nil, err
	}
	revert = func(revertCtx context.Context) error {
		return c.applySettingsOnly(revertCtx, threadID, previousSettings.Model, effortPtrOrNil(previousSettings.Effort))
	}
	return revert, nil
}

func effortPtrOrNil(effort string) *string {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return nil
	}
	return &effort
}

// AttachThread subscribes this connection to the native thread using the
// `thread/resume` path (listener attach; no restart, no context loss). This is
// required to receive thread-scoped notifications such as
// thread/settings/updated.
func (c *Client) AttachThread(ctx context.Context, threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("%w: thread id required", ErrApply)
	}
	if _, err := c.call(ctx, methodThreadResume, map[string]any{
		"threadId": threadID,
	}); err != nil {
		return fmt.Errorf("%w: attach: %v", ErrApply, err)
	}
	return nil
}

// applySettingsOnly sends thread/settings/update without waiting for the
// applied-settings acknowledgement (used by best-effort rollback). A nil
// effort maps to the native model-default value "none", exactly like
// ApplySettings.
func (c *Client) applySettingsOnly(ctx context.Context, threadID, model string, effort *string) error {
	params := map[string]any{
		"threadId": threadID,
		"model":    model,
	}
	if effort != nil && strings.TrimSpace(*effort) != "" {
		params["effort"] = strings.TrimSpace(*effort)
	} else {
		params["effort"] = "none"
	}
	if _, err := c.call(ctx, methodThreadSettingsUpd, params); err != nil {
		return fmt.Errorf("%w: %v", ErrApply, err)
	}
	return nil
}

// waitSettingsAck waits for the applied-settings notification matching the
// requested thread/model/effort. The native snapshot is the ack: the RPC
// response only means the update was queued.
func (c *Client) waitSettingsAck(ctx context.Context, threadID, model string, effort *string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultAckTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrNoSettings, ctx.Err())
		case <-timer.C:
			return fmt.Errorf("%w: no thread/settings/updated ack within %s (thread %s model %s)", ErrNoSettings, timeout, threadID, model)
		case n, ok := <-c.notif:
			if !ok {
				return fmt.Errorf("%w: connection closed before ack", ErrNoSettings)
			}
			if n.Method != notifThreadSettingsUpd {
				continue
			}
			var payload struct {
				ThreadID string `json:"threadId"`
				Settings struct {
					Model         string `json:"model"`
					ModelProvider string `json:"modelProvider"`
					Effort        string `json:"effort"`
				} `json:"threadSettings"`
			}
			if err := json.Unmarshal(n.Params, &payload); err != nil {
				continue
			}
			if payload.ThreadID != threadID {
				continue
			}
			if strings.TrimSpace(payload.Settings.Model) != strings.TrimSpace(model) {
				continue
			}
			appliedEffort := strings.TrimSpace(payload.Settings.Effort)
			if effort != nil && strings.TrimSpace(*effort) != "" {
				if appliedEffort != strings.TrimSpace(*effort) {
					continue
				}
			} else if appliedEffort != "" && normalizeEffort(appliedEffort) != "none" {
				// Model-default request: the applied effort must be the native
				// "none" (or an absent/null effort).
				continue
			}
			return nil
		}
	}
}

// normalizeEffort trims and lowercases a native effort value.
func normalizeEffort(effort string) string {
	return strings.ToLower(strings.TrimSpace(effort))
}
