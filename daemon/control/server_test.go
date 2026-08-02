package control

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu       sync.Mutex
	requests []Request
	response Response
}

func (h *testHandler) HandleControlRequest(req Request) Response {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	h.mu.Unlock()
	return h.response
}

func TestServerCallRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatalf("DefaultSocketPath returned error: %v", err)
	}

	handler := &testHandler{
		response: Response{
			OK:    true,
			Text:  "hello",
			Agent: &Agent{ID: "main:@42", Name: "Franklin", Status: "running"},
		},
	}
	server := &Server{Path: socketPath, Handler: handler}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForSocketPath(t, socketPath)

	resp, err := Call(socketPath, Request{Type: "agent_capture", AgentID: "main:@42"})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if !resp.OK || resp.Text != "hello" || resp.Agent == nil || resp.Agent.ID != "main:@42" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	handler.mu.Lock()
	if len(handler.requests) != 1 {
		t.Fatalf("handler requests = %#v", handler.requests)
	}
	if handler.requests[0].Type != "agent_capture" || handler.requests[0].AgentID != "main:@42" {
		t.Fatalf("handler request = %#v", handler.requests[0])
	}
	handler.mu.Unlock()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}
}

func TestServerShutdownClosesAndJoinsAcceptedConnections(t *testing.T) {
	stateDir := t.TempDir()
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Path:    socketPath,
		Handler: &testHandler{response: Response{OK: true}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForSocketPath(t, socketPath)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server returned before joining accepted connection")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("accepted connection remained open after server shutdown")
	}
}

func TestRemoveStaleSocketRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen.sock")
	if err := os.WriteFile(path, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("expected error for non-socket path")
	}
}

func waitForSocketPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for control socket at %s", path)
}
