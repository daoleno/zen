package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	skillmgmt "github.com/daoleno/zen/daemon/skills"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

// mutationProbe stands in for the production command executor so handler
// tests can prove the ownership protocol: execution starts while the request
// still owns the mutation slot, superseding or disconnect cancels the
// in-flight context, and only the still-current request emits a terminal
// result.
type mutationProbe struct {
	started   chan struct{}
	release   chan struct{}
	executed  atomic.Int32
	cancelled atomic.Int32
}

func newMutationProbe() *mutationProbe {
	return &mutationProbe{
		started: make(chan struct{}, 16),
		release: make(chan struct{}),
	}
}

func (p *mutationProbe) executeSkills(ctx context.Context, command skillmgmt.MutationCommand, options skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error) {
	p.executed.Add(1)
	p.started <- struct{}{}
	select {
	case <-p.release:
		return skillmgmt.MutationExecution{Success: true, ExitCode: 0, Output: "ok", DurationMS: 1}, nil
	case <-ctx.Done():
		p.cancelled.Add(1)
		return skillmgmt.MutationExecution{}, skillmgmt.ErrMutationCancelled
	}
}

func TestSkillsMutationSupersedeCancelsInFlightCommandAndSuppressesStaleResult(t *testing.T) {
	probe := newMutationProbe()
	srv, conn, closeConn := newSkillsMutationTestServer(t, probe)
	defer closeConn()
	messages := messageSink(t, conn)

	send := func(requestID string) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{
			"type":       "skills_mutation",
			"request_id": requestID,
			"operation":  "import",
			"skill_id":   "owner/repo/skill",
			"source":     "owner/repo",
			"skill_name": "skill",
			"scope":      "global",
			"agents":     []string{"codex"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	send("req-first")
	waitProbeStarted(t, probe)

	// The first request still owns the slot while its command is executing:
	// the ownership must NOT have been consumed by the start of execution.
	if !srv.isCurrentSkillsMutation(serverSideConnection(t, srv), skillsMutationRequest{requestID: "req-first"}) {
		t.Fatal("first request lost mutation ownership while its command was executing")
	}

	// A newer request replaces and cancels the in-flight command.
	send("req-second")
	waitProbeStarted(t, probe)
	waitFor(t, func() bool { return probe.cancelled.Load() == 1 }, "first command was not canceled by supersede")

	payload := readUntil(t, messages, "skills_mutation_error", "req-first")
	if payload["code"] != "superseded" {
		t.Fatalf("first request error = %v, want superseded", payload["code"])
	}

	// Disconnect cancels the second in-flight command too; its stale result
	// must be suppressed (the slot was deleted, claim fails).
	closeConn()
	waitFor(t, func() bool { return probe.cancelled.Load() == 2 }, "second command was not canceled by disconnect")

	if probe.executed.Load() != 2 {
		t.Fatalf("executions = %d, want 2", probe.executed.Load())
	}
	expectNoMessage(t, messages, "skills_mutation_result")
	expectNoMessage(t, messages, "skills_mutation_error")
}

func TestSkillsMutationOnlyCurrentRequestClaimsOnceAndEmitsResult(t *testing.T) {
	probe := newMutationProbe()
	srv, conn, closeConn := newSkillsMutationTestServer(t, probe)
	defer closeConn()
	messages := messageSink(t, conn)

	send := func(requestID string) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{
			"type":       "skills_mutation",
			"request_id": requestID,
			"operation":  "import",
			"skill_id":   "owner/repo/skill",
			"source":     "owner/repo",
			"skill_name": "skill",
			"scope":      "global",
			"agents":     []string{"codex"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// First request runs to completion and claims exactly once.
	send("req-one")
	waitProbeStarted(t, probe)
	close(probe.release)
	first := readUntil(t, messages, "skills_mutation_result", "req-one")
	if first["success"] != true {
		t.Fatalf("first result success = %v, want true", first["success"])
	}
	if srv.isCurrentSkillsMutation(serverSideConnection(t, srv), skillsMutationRequest{requestID: "req-one"}) {
		t.Fatal("completed request still owns the mutation slot")
	}
	expectNoMessage(t, messages, "skills_mutation_error")

	// A second request claims and emits exactly once as well.
	send("req-two")
	waitProbeStarted(t, probe)
	second := readUntil(t, messages, "skills_mutation_result", "req-two")
	if second["success"] != true {
		t.Fatalf("second result success = %v, want true", second["success"])
	}
	expectNoMessage(t, messages, "skills_mutation_result")
	expectNoMessage(t, messages, "skills_mutation_error")
}

func newSkillsMutationTestServer(t *testing.T, probe *mutationProbe) (*Server, *websocket.Conn, func()) {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-skills", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.skillsMutationExecuteOverride = probe.executeSkills
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-skills", "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	return srv, conn, func() {
		_ = conn.Close()
		httpServer.Close()
	}
}

// serverSideConnection returns the server-side websocket for the single
// connected test client, which is the key used in the ownership maps.
func serverSideConnection(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for conn := range srv.clients {
		return conn
	}
	t.Fatal("no server-side connection registered")
	return nil
}

// messageSink drains the client websocket into a channel so assertions never
// depend on gorilla read deadlines.
func messageSink(t *testing.T, conn *websocket.Conn) <-chan map[string]any {
	t.Helper()
	messages := make(chan map[string]any, 128)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				close(messages)
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				continue
			}
			messages <- payload
		}
	}()
	return messages
}

func waitProbeStarted(t *testing.T, probe *mutationProbe) {
	t.Helper()
	select {
	case <-probe.started:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation execution never started")
	}
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}

// readUntil consumes sink messages, skipping unrelated server traffic, until
// the wanted type for the wanted request id arrives.
func readUntil(t *testing.T, messages <-chan map[string]any, wantType, wantRequestID string) map[string]any {
	t.Helper()
	for {
		select {
		case payload, ok := <-messages:
			if !ok {
				t.Fatalf("connection closed before %s for %s arrived", wantType, wantRequestID)
			}
			if payload["type"] == wantType && payload["request_id"] == wantRequestID {
				return payload
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s for %s", wantType, wantRequestID)
		}
	}
}

// expectNoMessage asserts no message of the given type arrives within the
// grace window; unrelated server traffic is skipped.
func expectNoMessage(t *testing.T, messages <-chan map[string]any, messageType string) {
	t.Helper()
	for {
		select {
		case payload, ok := <-messages:
			if !ok {
				return // connection closed: nothing more can arrive
			}
			if payload["type"] == messageType {
				t.Fatalf("unexpected %s arrived", messageType)
			}
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}
