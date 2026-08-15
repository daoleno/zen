package codexctl

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeAppServer is a minimal Codex 0.147 app-server protocol double over a
// unix socket. It records every request and can broadcast
// thread/settings/updated notifications exactly like the native server.
type fakeAppServer struct {
	socketPath string
	ln         net.Listener
	srv        *http.Server
	upgrader   websocket.Upgrader
	mu         sync.Mutex
	requests   []map[string]any
	clients    []*websocket.Conn
	// settingsUpdateHook runs after a thread/settings/update request is
	// recorded; it may broadcast acks.
	settingsUpdateHook func(f *fakeAppServer, params map[string]any)
	threads            []ThreadInfo
	loaded             []string
}

func startFakeAppServer(t *testing.T) *fakeAppServer {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "codex-ctl.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAppServer{
		socketPath: socketPath,
		ln:         ln,
		upgrader:   websocket.Upgrader{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := f.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.clients = append(f.clients, ws)
		f.mu.Unlock()
		f.serve(ws)
	})
	f.srv = &http.Server{Handler: mux}
	go f.srv.Serve(ln)
	t.Cleanup(func() {
		_ = f.srv.Close()
		_ = ln.Close()
		_ = os.Remove(socketPath)
	})
	return f
}

func (f *fakeAppServer) record(req map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
}

func (f *fakeAppServer) requestCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, req := range f.requests {
		if req["method"] == method {
			n++
		}
	}
	return n
}

func (f *fakeAppServer) lastParams(method string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i]["method"] == method {
			params, _ := f.requests[i]["params"].(map[string]any)
			return params
		}
	}
	return nil
}

func (f *fakeAppServer) broadcast(method string, params any) {
	raw, _ := json.Marshal(map[string]any{"method": method, "params": params})
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ws := range f.clients {
		_ = ws.WriteMessage(websocket.TextMessage, raw)
	}
}

func (f *fakeAppServer) serve(ws *websocket.Conn) {
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Method == "" {
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(envelope.Params, &params)
		f.record(map[string]any{"id": float64(envelope.ID), "method": envelope.Method, "params": params})
		switch envelope.Method {
		case methodInitialize:
			f.reply(ws, envelope.ID, map[string]any{"userAgent": "codex/0.147.0 fake", "codexHome": "/tmp"})
		case methodThreadResume:
			f.reply(ws, envelope.ID, map[string]any{"thread": map[string]any{"id": "t-main"}})
		case methodThreadLoadedList:
			f.mu.Lock()
			loaded := append([]string{}, f.loaded...)
			f.mu.Unlock()
			f.reply(ws, envelope.ID, map[string]any{"data": loaded})
		case methodThreadList:
			f.mu.Lock()
			threads := append([]ThreadInfo{}, f.threads...)
			f.mu.Unlock()
			filtered := threads
			if cwd, _ := params["cwd"].(string); cwd != "" {
				filtered = nil
				for _, th := range threads {
					if th.Cwd == cwd {
						filtered = append(filtered, th)
					}
				}
			}
			f.reply(ws, envelope.ID, map[string]any{"data": filtered})
		case methodThreadSettingsUpd:
			hook := f.settingsUpdateHook
			if hook != nil {
				hook(f, params)
			}
			f.reply(ws, envelope.ID, map[string]any{})
		default:
			f.replyErr(ws, envelope.ID, -32601, "method not found")
		}
	}
}

func (f *fakeAppServer) reply(ws *websocket.Conn, id uint64, result any) {
	raw, _ := json.Marshal(map[string]any{"id": id, "result": result})
	_ = ws.WriteMessage(websocket.TextMessage, raw)
}

func (f *fakeAppServer) replyErr(ws *websocket.Conn, id uint64, code int, message string) {
	raw, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
	_ = ws.WriteMessage(websocket.TextMessage, raw)
}

func openFake(t *testing.T, f *fakeAppServer) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Open(ctx, f.socketPath, DialOptions{ResolveRetryWindow: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestOpenPerformsInitializeWithExperimentalAPI(t *testing.T) {
	f := startFakeAppServer(t)
	_ = openFake(t, f)
	init := f.lastParams(methodInitialize)
	if init == nil {
		t.Fatal("no initialize request")
	}
	if f.requestCount(methodInitialize) != 1 {
		t.Fatalf("initialize count = %d", f.requestCount(methodInitialize))
	}
	clientInfo, _ := init["clientInfo"].(map[string]any)
	if clientInfo == nil || clientInfo["name"] != "zen" {
		t.Fatalf("clientInfo = %#v", clientInfo)
	}
	caps, _ := init["capabilities"].(map[string]any)
	if caps == nil || caps["experimentalApi"] != true {
		t.Fatalf("capabilities = %#v (experimentalApi required for thread/settings/update)", caps)
	}
}

func TestResolveThreadPrefersLoadedMainActiveThread(t *testing.T) {
	f := startFakeAppServer(t)
	f.loaded = []string{"t-side", "t-main"}
	f.threads = []ThreadInfo{
		{ID: "t-side", Cwd: "/repo/zen", Status: "idle", UpdatedAt: 200},
		{ID: "t-main", Cwd: "/repo/zen", Status: "active", UpdatedAt: 300},
	}
	c := openFake(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.ResolveThread(ctx, "/repo/zen")
	if err != nil {
		t.Fatal(err)
	}
	if got != "t-main" {
		t.Fatalf("ResolveThread = %q, want t-main", got)
	}
	if p := f.lastParams(methodThreadList); p["cwd"] != "/repo/zen" {
		t.Fatalf("thread/list cwd = %#v", p["cwd"])
	}
}

func TestResolveThreadFallsBackToSingleLoadedThread(t *testing.T) {
	// A per-session app server loads exactly the pane's threads; a single
	// loaded thread is the pane's primary thread even while the store listing
	// has not flushed it yet.
	f := startFakeAppServer(t)
	f.loaded = []string{"t-other"}
	f.threads = []ThreadInfo{{ID: "t-other", Cwd: "/elsewhere", Status: "idle"}}
	c := openFake(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.ResolveThread(ctx, "/repo/zen")
	if err != nil {
		t.Fatal(err)
	}
	if got != "t-other" {
		t.Fatalf("ResolveThread = %q, want single loaded thread", got)
	}
}

func TestResolveThreadNoCandidates(t *testing.T) {
	f := startFakeAppServer(t)
	f.loaded = []string{"t-a", "t-b"}
	f.threads = []ThreadInfo{
		{ID: "t-a", Cwd: "/elsewhere", Status: "idle"},
		{ID: "t-b", Cwd: "/elsewhere", Status: "idle"},
	}
	c := openFake(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.ResolveThread(ctx, "/repo/zen")
	if !errors.Is(err, ErrNoThread) {
		t.Fatalf("err = %v, want ErrNoThread", err)
	}
}

func TestApplySettingsSendsExactParamsAndAcks(t *testing.T) {
	f := startFakeAppServer(t)
	c := openFake(t, f)
	effort := "high"
	f.settingsUpdateHook = func(f *fakeAppServer, params map[string]any) {
		f.broadcast(notifThreadSettingsUpd, map[string]any{
			"threadId": "t-main",
			"threadSettings": map[string]any{
				"model":         "gpt-5.5",
				"modelProvider": "openai",
				"effort":        "high",
			},
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	revert, err := c.ApplySettings(ctx, "t-main", "gpt-5.5", &effort, Settings{ThreadID: "t-main", Model: "gpt-5.4", Effort: "medium"}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	params := f.lastParams(methodThreadSettingsUpd)
	if params["threadId"] != "t-main" || params["model"] != "gpt-5.5" || params["effort"] != "high" {
		t.Fatalf("update params = %#v", params)
	}
	if revert == nil {
		t.Fatal("revert closure missing")
	}
	if err := revert(ctx); err != nil {
		t.Fatalf("revert: %v", err)
	}
	revertParams := f.lastParams(methodThreadSettingsUpd)
	if revertParams["model"] != "gpt-5.4" || revertParams["effort"] != "medium" {
		t.Fatalf("revert params = %#v", revertParams)
	}
}

func TestApplySettingsOmitsEffortWhenNil(t *testing.T) {
	f := startFakeAppServer(t)
	c := openFake(t, f)
	f.settingsUpdateHook = func(f *fakeAppServer, params map[string]any) {
		f.broadcast(notifThreadSettingsUpd, map[string]any{
			"threadId": "t-main",
			"threadSettings": map[string]any{
				"model":  "gpt-5.5",
				"effort": nil,
			},
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.ApplySettings(ctx, "t-main", "gpt-5.5", nil, Settings{ThreadID: "t-main", Model: "gpt-5.4"}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	params := f.lastParams(methodThreadSettingsUpd)
	if _, present := params["effort"]; present {
		t.Fatalf("effort must be omitted when nil: %#v", params)
	}
}

func TestApplySettingsWaitsForMatchingAck(t *testing.T) {
	f := startFakeAppServer(t)
	c := openFake(t, f)
	// Server broadcasts an ack for a different model first, then the real one.
	call := 0
	f.settingsUpdateHook = func(f *fakeAppServer, params map[string]any) {
		call++
		if call == 1 {
			f.broadcast(notifThreadSettingsUpd, map[string]any{
				"threadId":       "t-main",
				"threadSettings": map[string]any{"model": "gpt-4", "effort": "low"},
			})
			f.broadcast(notifThreadSettingsUpd, map[string]any{
				"threadId":       "t-main",
				"threadSettings": map[string]any{"model": "gpt-5.5", "effort": "medium"},
			})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	effort := "medium"
	if _, err := c.ApplySettings(ctx, "t-main", "gpt-5.5", &effort, Settings{ThreadID: "t-main", Model: "gpt-5.4"}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestApplySettingsTimeoutKeepsCallerState(t *testing.T) {
	f := startFakeAppServer(t)
	c := openFake(t, f)
	// No ack is ever broadcast.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.ApplySettings(ctx, "t-main", "gpt-5.5", nil, Settings{ThreadID: "t-main", Model: "gpt-5.4"}, 300*time.Millisecond)
	if !errors.Is(err, ErrNoSettings) {
		t.Fatalf("err = %v, want ErrNoSettings", err)
	}
}

func TestApplySettingsRPCErrorPropagates(t *testing.T) {
	f := startFakeAppServer(t)
	c := openFake(t, f)
	// The real server rejects with the request id; replyErr uses the recorded
	// id of the settings request.
	f.settingsUpdateHook = func(f *fakeAppServer, params map[string]any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, req := range f.requests {
			if req["method"] != methodThreadSettingsUpd {
				continue
			}
			for _, ws := range f.clients {
				id, _ := req["id"].(float64)
				f.replyErr(ws, uint64(id), -32602, "invalid thread id")
			}
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.ApplySettings(ctx, "missing", "gpt-5.5", nil, Settings{}, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "invalid thread id") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenFailsOnMissingSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Open(ctx, filepath.Join(t.TempDir(), "nope.sock"), DialOptions{DialTimeout: 300 * time.Millisecond})
	if !errors.Is(err, ErrConnect) {
		t.Fatalf("err = %v, want ErrConnect", err)
	}
}
