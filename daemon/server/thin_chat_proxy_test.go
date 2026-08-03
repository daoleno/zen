package server

import (
	"bytes"
	"crypto/sha256"
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

func TestInboundSendInputProjectsDurablePendingWithRequestReceipt(t *testing.T) {
	var calls atomic.Int32
	var gotReceipt string
	srv := &Server{
		sendInputWithReceiptOverride: func(_ string, _ string, receipt string) error {
			calls.Add(1)
			gotReceipt = receipt
			return &watcher.InputPendingError{TransactionID: "pending-transaction"}
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

func TestInboundSendInputPropagatesUnresolvedCodexArbiterWithoutAck(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the authoritative untracked-target route test")
	}
	helperDir := t.TempDir()
	codexHelper := filepath.Join(helperDir, "codex")
	if err := os.WriteFile(codexHelper, []byte("#!/bin/bash\nexec -a codex /bin/cat\n"), 0o700); err != nil {
		t.Fatalf("write Codex process helper: %v", err)
	}
	sessionID := fmt.Sprintf("zen-ws-untracked-codex-%d", time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, codexHelper).CombinedOutput(); err != nil {
		t.Fatalf("create untracked Codex target: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, currentErr := exec.Command(
			"tmux",
			"display-message",
			"-p",
			"-t",
			sessionID,
			"#{pane_current_command}",
		).Output()
		if currentErr == nil && strings.TrimSpace(string(current)) == "codex" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("untracked Codex process did not become authoritative: %q (%v)", current, currentErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	metadata, err := exec.Command(
		"tmux",
		"display-message",
		"-p",
		"-t",
		sessionID,
		"#{pane_dead}\t#{session_id}\t#{session_created}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}",
	).Output()
	if err != nil {
		t.Fatalf("read untracked target identity: %v", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(metadata), "\n"), "\t")
	if len(fields) != 7 || fields[0] == "1" {
		t.Fatalf("untracked target metadata = %q", metadata)
	}
	generationDigest := sha256.Sum256([]byte(strings.Join(fields[1:7], "\x00")))
	generation := fmt.Sprintf("%x", generationDigest[:])

	stateDir := t.TempDir()
	inputWatcher := watcher.New(time.Second)
	if err := inputWatcher.ConfigureCodexInputState(stateDir); err != nil {
		t.Fatalf("configure durable Codex input: %v", err)
	}
	scopeDigest := sha256.Sum256([]byte(sessionID + "\x00" + generation))
	transactionDir := filepath.Join(stateDir, "codex-input", "transactions")
	recordPath := filepath.Join(
		transactionDir,
		fmt.Sprintf("%x-ws-ambiguous.json", scopeDigest[:16]),
	)
	now := time.Now().UTC()
	record, err := json.MarshalIndent(map[string]any{
		"schema_version":     1,
		"transaction_id":     "ws-ambiguous",
		"session_id":         sessionID,
		"session_generation": generation,
		"action":             "submit_codex_input",
		"phase":              "ambiguous",
		"payload_sha256":     fmt.Sprintf("%x", sha256.Sum256([]byte("owned"))),
		"instruction":        "owned",
		"instruction_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("owned"))),
		"created_at":         now,
		"updated_at":         now,
	}, "", "  ")
	if err != nil {
		t.Fatalf("encode ambiguous transaction: %v", err)
	}
	if err := os.WriteFile(recordPath, append(record, '\n'), 0o600); err != nil {
		t.Fatalf("seed ambiguous transaction: %v", err)
	}
	before, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture untracked target before input: %v", err)
	}

	srv := &Server{watcher: inputWatcher}
	conn := openThinProxyTestSocket(t, srv)
	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "untracked-codex-ambiguous",
		AgentID:   sessionID,
		Text:      "foreign raw input",
	})
	if response.Type != "input_failed" ||
		response.Code != "send_input_failed" ||
		!strings.Contains(response.Message, "unresolved") ||
		response.RequestID != "untracked-codex-ambiguous" {
		t.Fatalf("unresolved arbiter response = %#v", response)
	}
	after, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture untracked target after input: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("WebSocket input bypassed the Codex arbiter\nbefore=%q\nafter=%q", before, after)
	}
}

func TestInboundSendInputIgnoresCachedNonCodexHintForCurrentCodex(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the cached-provider WebSocket route test")
	}
	stateDir := t.TempDir()
	inputWatcher := watcher.New(time.Second)
	if err := inputWatcher.ConfigureCodexInputState(stateDir); err != nil {
		t.Fatalf("configure durable Codex input: %v", err)
	}
	sessionID, err := inputWatcher.CreateSession("", watcher.CreateSessionOptions{
		Name:    "round5 cached provider",
		Command: "exec -a codex /bin/sleep 300",
		Hidden:  true,
	})
	if err != nil {
		t.Fatalf("create cached non-Codex hint target: %v", err)
	}
	t.Cleanup(func() {
		_ = inputWatcher.KillSession(sessionID)
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, currentErr := exec.Command(
			"tmux",
			"display-message",
			"-p",
			"-t",
			sessionID,
			"#{pane_current_command}",
		).Output()
		if currentErr == nil && strings.TrimSpace(string(current)) == "codex" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cached Codex process did not become authoritative: %q (%v)", current, currentErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var metadata []byte
	deadline = time.Now().Add(3 * time.Second)
	for {
		metadata, err = exec.Command(
			"tmux",
			"display-message",
			"-p",
			"-t",
			sessionID,
			"#{pane_dead}\t#{session_id}\t#{session_created}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}",
		).Output()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("read cached target identity: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	fields := strings.Split(strings.TrimSuffix(string(metadata), "\n"), "\t")
	if len(fields) != 7 || fields[0] == "1" {
		t.Fatalf("cached target metadata = %q", metadata)
	}
	generationDigest := sha256.Sum256([]byte(strings.Join(fields[1:7], "\x00")))
	generation := fmt.Sprintf("%x", generationDigest[:])
	scopeDigest := sha256.Sum256([]byte(sessionID + "\x00" + generation))
	recordPath := filepath.Join(
		stateDir,
		"codex-input",
		"transactions",
		fmt.Sprintf("%x-ws-cached-ambiguous.json", scopeDigest[:16]),
	)
	now := time.Now().UTC()
	record, err := json.MarshalIndent(map[string]any{
		"schema_version":     1,
		"transaction_id":     "ws-cached-ambiguous",
		"session_id":         sessionID,
		"session_generation": generation,
		"action":             "submit_codex_input",
		"phase":              "ambiguous",
		"payload_sha256":     fmt.Sprintf("%x", sha256.Sum256([]byte("owned"))),
		"instruction":        "owned",
		"instruction_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("owned"))),
		"created_at":         now,
		"updated_at":         now,
	}, "", "  ")
	if err != nil {
		t.Fatalf("encode cached ambiguous transaction: %v", err)
	}
	if err := os.WriteFile(recordPath, append(record, '\n'), 0o600); err != nil {
		t.Fatalf("seed cached ambiguous transaction: %v", err)
	}
	before, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture cached target before input: %v", err)
	}

	srv := &Server{watcher: inputWatcher}
	conn := openThinProxyTestSocket(t, srv)
	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "cached-command-actual-codex",
		AgentID:   sessionID,
		Text:      "foreign WebSocket input",
	})
	if response.Type != "input_failed" ||
		response.Code != "send_input_failed" ||
		!strings.Contains(response.Message, "unresolved") ||
		response.RequestID != "cached-command-actual-codex" {
		t.Fatalf("cached-provider arbiter response = %#v", response)
	}
	after, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture cached target after input: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("cached provider hint bypassed Codex arbiter\nbefore=%q\nafter=%q", before, after)
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
