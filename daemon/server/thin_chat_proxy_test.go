package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

type thinProxyResponse struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	FieldCount int    `json:"-"`
}

type thinProxyInputCall struct {
	agentID string
	text    string
}

func TestInboundSendInputCallsProviderOnceAndAcknowledges(t *testing.T) {
	calls := make(chan thinProxyInputCall, 3)
	srv := &Server{
		sendInputOverride: func(agentID, text string) error {
			calls <- thinProxyInputCall{agentID: agentID, text: text}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "send_input",
		RequestID: "request-1",
		AgentID:   "agent-1",
		Text:      "hello",
	}

	first := sendThinProxyRequest(t, conn, request)
	if first.Type != "input_sent" || first.RequestID != request.RequestID ||
		first.FieldCount != 2 {
		t.Fatalf("first response = %#v", first)
	}
	if call := <-calls; call != (thinProxyInputCall{agentID: "agent-1", text: "hello"}) {
		t.Fatalf("first provider call = %#v", call)
	}
	select {
	case extra := <-calls:
		t.Fatalf("one inbound request caused an extra provider call: %#v", extra)
	default:
	}

	// request_id correlates only this response; it is not a dedupe key.
	second := sendThinProxyRequest(t, conn, request)
	if second.Type != "input_sent" || second.RequestID != request.RequestID {
		t.Fatalf("repeated response = %#v", second)
	}
	if call := <-calls; call != (thinProxyInputCall{agentID: "agent-1", text: "hello"}) {
		t.Fatalf("repeated provider call = %#v", call)
	}
	select {
	case extra := <-calls:
		t.Fatalf("two inbound requests caused an extra provider call: %#v", extra)
	default:
	}
}

func TestInboundSendInputDoesNotReadOrGateOnBusyTranscript(t *testing.T) {
	var transcriptReads atomic.Int32
	var providerCalls atomic.Int32
	srv := &Server{
		providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
			transcriptReads.Add(1)
			return work.CodexConversation{
				Available: true,
				Activity: &work.ProviderActivity{
					ID:        "provider-active",
					Status:    work.ProviderActivityRunning,
					StartedAt: "2026-07-16T06:00:00Z",
				},
			}, nil
		},
		sendInputOverride: func(string, string) error {
			providerCalls.Add(1)
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	for _, requestID := range []string{"busy-request-1", "busy-request-2"} {
		response := sendThinProxyRequest(t, conn, clientMessage{
			Type:      "send_input",
			RequestID: requestID,
			AgentID:   "busy-agent",
			Text:      "provider owns native queuing",
		})
		if response.Type != "input_sent" || response.RequestID != requestID {
			t.Fatalf("busy response = %#v", response)
		}
	}
	if got := providerCalls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	if got := transcriptReads.Load(); got != 0 {
		t.Fatalf("send_input read provider transcript %d times, want 0", got)
	}
}

func TestInboundSendInputFailureReturnsRejectionWithoutAck(t *testing.T) {
	wantErr := errors.New("executor rejected input")
	var providerCalls atomic.Int32
	srv := &Server{
		sendInputOverride: func(string, string) error {
			providerCalls.Add(1)
			return wantErr
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "failed-request",
		AgentID:   "agent-1",
		Text:      "fail",
	})
	if response.Type == "input_sent" {
		t.Fatalf("failed provider call was acknowledged: %#v", response)
	}
	if response.Type != "input_failed" || response.Code != "send_input_failed" ||
		response.Message != wantErr.Error() || response.RequestID != "failed-request" ||
		response.FieldCount != 4 {
		t.Fatalf("failure response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestInboundSendInputProjectsAmbiguousOutcomeAsPending(t *testing.T) {
	var calls atomic.Int32
	var gotReceipt string
	srv := &Server{
		sendInputWithReceiptOverride: func(_ string, _ string, receipt string) error {
			calls.Add(1)
			gotReceipt = receipt
			return &watcher.InputSubmissionError{
				Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
				Cause:  errors.New("tmux queue outcome unknown"),
			}
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-durable-pending",
		AgentID:   "agent-pending",
		Text:      "preserve and resume me",
	})
	if response.Type != "input_pending" ||
		response.RequestID != "request-durable-pending" ||
		response.FieldCount != 2 {
		t.Fatalf("pending response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if gotReceipt != "request-durable-pending" {
		t.Fatalf("receipt = %q, want request ID", gotReceipt)
	}
}

func TestBrainAdmissionFailureAfterProviderAcceptEmitsPending(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@admit-fail"
	if err := store.SetChatState(brain.ChatState{ThreadID: "known-thread"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "persist me" || receipt != "request-admit-fail" {
				t.Fatalf("provider call = %q %q %q", agentID, text, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:                 "send_input",
		RequestID:            "request-admit-fail",
		AgentID:              hostID,
		Text:                 "persist me",
		DisplayBody:          "persist me",
		ConversationScopeKey: "brain-thread:unknown-thread",
	})
	if response.Type == "input_sent" {
		t.Fatalf("admission failure must not emit input_sent: %#v", response)
	}
	if response.Type != "input_pending" ||
		response.RequestID != "request-admit-fail" ||
		response.FieldCount != 2 {
		t.Fatalf("pending response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want exactly 1", got)
	}
	items, err := store.ThreadTimeline("known-thread", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed admission must not invent durable rows: %#v", items)
	}
}

func TestNonBrainSendInputStillAcknowledgesAfterProviderAccept(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "brain-thread"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-host:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != "ordinary-session" || receipt != "request-non-brain" {
				t.Fatalf("provider call = %q %q", agentID, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-non-brain",
		AgentID:   "ordinary-session",
		Text:      "hello ordinary",
	})
	if response.Type != "input_sent" ||
		response.RequestID != "request-non-brain" ||
		response.FieldCount != 2 {
		t.Fatalf("non-Brain response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestServerSendInputWithReceiptSubmitsRawTextThroughRealWatcher(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the real watcher boundary test")
	}
	session := fmt.Sprintf("zen-server-session-input-%d-%d", os.Getpid(), time.Now().UnixNano())
	outputPath := filepath.Join(t.TempDir(), "input.txt")
	command := fmt.Sprintf("IFS= read -r line; printf '%%s' \"$line\" > %q; sleep 5", outputPath)
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", session, command).CombinedOutput(); err != nil {
		t.Fatalf("create tmux Session: %v: %s", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	})
	time.Sleep(100 * time.Millisecond)

	srv := &Server{watcher: watcher.New(time.Second)}
	conn := openThinProxyTestSocket(t, srv)
	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-raw-text",
		AgentID:   session,
		Text:      "hello",
	})
	if response.Type != "input_sent" || response.RequestID != "request-raw-text" {
		t.Fatalf("WebSocket send_input response = %#v", response)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(outputPath)
		if err == nil {
			if string(raw) != "hello" {
				t.Fatalf("provider received %q, want exact raw WebSocket Text", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider did not receive raw WebSocket Text: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestPauseActsDirectlyOnCurrentExecutorState(t *testing.T) {
	var calls atomic.Int32
	actions := make(chan thinProxyInputCall, 1)
	srv := &Server{
		sendActionOverride: func(agentID, action string) error {
			calls.Add(1)
			actions <- thinProxyInputCall{agentID: agentID, text: action}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_action",
		RequestID: "stop-request",
		AgentID:   "agent-1",
		Action:    "pause",
	})
	if response.Type != "action_sent" || response.RequestID != "stop-request" ||
		response.FieldCount != 2 {
		t.Fatalf("pause response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pause provider calls = %d, want 1", got)
	}
	if call := <-actions; call != (thinProxyInputCall{agentID: "agent-1", text: "pause"}) {
		t.Fatalf("pause provider call = %#v", call)
	}
}

func TestPauseFailureReturnsCorrelatedFailureWithoutSentAck(t *testing.T) {
	wantErr := errors.New("executor could not stop")
	var calls atomic.Int32
	srv := &Server{
		sendActionOverride: func(string, string) error {
			calls.Add(1)
			return wantErr
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_action",
		RequestID: "failed-stop",
		AgentID:   "agent-1",
		Action:    "pause",
	})
	if response.Type == "action_sent" {
		t.Fatalf("failed provider action was acknowledged: %#v", response)
	}
	if response.Type != "action_failed" || response.RequestID != "failed-stop" ||
		response.Code != "send_action_failed" || response.Message != wantErr.Error() ||
		response.FieldCount != 4 {
		t.Fatalf("failure response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func openThinProxyTestSocket(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	registered := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srv.mu.Lock()
		if srv.writes == nil {
			srv.writes = make(map[*websocket.Conn]*sync.Mutex)
		}
		if srv.codexSubs == nil {
			srv.codexSubs = make(map[*websocket.Conn]map[string]codexConversationSubscription)
		}
		srv.writes[conn] = &sync.Mutex{}
		srv.mu.Unlock()
		close(registered)
		defer func() {
			srv.mu.Lock()
			for _, subscription := range srv.codexSubs[conn] {
				subscription.cancel()
			}
			delete(srv.codexSubs, conn)
			delete(srv.writes, conn)
			srv.mu.Unlock()
			_ = conn.Close()
		}()
		for {
			_, message, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			srv.handleClientMessage(conn, message)
		}
	}))
	t.Cleanup(httpServer.Close)

	url := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial test WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("test WebSocket was not registered")
	}
	return conn
}

func sendThinProxyRequest(t *testing.T, conn *websocket.Conn, request clientMessage) thinProxyResponse {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("write WebSocket request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read WebSocket response: %v", err)
	}
	var response thinProxyResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode WebSocket response: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode WebSocket response fields: %v", err)
	}
	response.FieldCount = len(fields)
	return response
}
