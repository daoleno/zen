package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			"operation":  "migrate",
			"scope":      "global",
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
			"operation":  "migrate",
			"scope":      "global",
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

func TestSkillsImportOperationIsNotExposedOverWebSocket(t *testing.T) {
	_, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	messages := messageSink(t, conn)
	for _, messageType := range []string{"skills_command", "skills_mutation"} {
		requestID := "removed-" + messageType
		if err := conn.WriteJSON(map[string]any{
			"type":       messageType,
			"request_id": requestID,
			"operation":  "import",
			"skill_name": "remote-skill",
			"skill_id":   "owner/repo/remote-skill",
			"source":     "owner/repo",
			"scope":      "global",
			"agents":     []string{"codex"},
		}); err != nil {
			t.Fatal(err)
		}
		response := readUntil(t, messages, messageType+"_error", requestID)
		if response["code"] != "unsupported_operation" {
			t.Fatalf("%s import response = %+v", messageType, response)
		}
	}
}

func TestSkillsWebSocketLifecycleUsesReviewedPlansAndReconcilesInventory(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".zen")
	t.Setenv("HOME", home)
	t.Setenv("ZEN_STATE_DIR", stateDir)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	source := filepath.Join(home, ".codex", "skills", "lifecycle-proof")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: lifecycle-proof\ndescription: websocket lifecycle fixture\n---\nproof\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	messages := messageSink(t, conn)
	send := func(payload map[string]any) {
		t.Helper()
		if err := conn.WriteJSON(payload); err != nil {
			t.Fatal(err)
		}
	}
	inventory := func(id string, generation int) map[string]any {
		send(map[string]any{"type": "skills_inventory", "request_id": id, "generation": generation})
		return readUntil(t, messages, "skills_inventory", id)
	}
	initial := inventory("inventory-initial", 1)
	if rows := initial["inventory"].(map[string]any)["skills"].([]any); len(rows) != 1 {
		t.Fatalf("initial Skills inventory = %d rows, want one local row", len(rows))
	}

	sequence := []map[string]any{
		{"operation": "migrate", "scope": "global"},
		{"operation": "adopt", "skill_name": "lifecycle-proof", "scope": "global", "agents": []string{"codex"}},
		{"operation": "bind", "skill_name": "lifecycle-proof", "scope": "global", "agents": []string{"cursor"}},
		{"operation": "disable", "skill_name": "lifecycle-proof", "scope": "global", "agents": []string{"cursor"}},
		{"operation": "enable", "skill_name": "lifecycle-proof", "scope": "global", "agents": []string{"cursor"}},
		{"operation": "unbind", "skill_name": "lifecycle-proof", "scope": "global", "agents": []string{"cursor"}},
		{"operation": "uninstall", "skill_name": "lifecycle-proof", "scope": "global"},
	}
	for _, input := range sequence {
		operation := input["operation"].(string)
		commandID := "command-" + operation
		commandRequest := map[string]any{"type": "skills_command", "request_id": commandID}
		for key, value := range input {
			commandRequest[key] = value
		}
		send(commandRequest)
		commandResponse := readUntil(t, messages, "skills_command", commandID)
		command := commandResponse["command"].(map[string]any)
		if command["operation"] != operation || command["scope"] != "global" {
			t.Fatalf("%s reviewed command contract = %+v", operation, command)
		}

		mutationID := "mutation-" + operation
		mutationRequest := map[string]any{"type": "skills_mutation", "request_id": mutationID}
		for key, value := range input {
			mutationRequest[key] = value
		}
		send(mutationRequest)
		result := readUntil(t, messages, "skills_mutation_result", mutationID)
		if result["success"] != true {
			t.Fatalf("%s execution failed: %+v", operation, result)
		}

		if operation == "adopt" {
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
			send(map[string]any{"type": "skills_inspect", "request_id": "inspect-adopted", "generation": 1, "skill_name": "lifecycle-proof"})
			inspection := readUntil(t, messages, "skills_inspect_result", "inspect-adopted")
			if inspection["detail"].(map[string]any)["skill_name"] != "lifecycle-proof" {
				t.Fatalf("inspect response = %+v", inspection)
			}
		}
	}
	final := inventory("inventory-final", 2)
	if rows := final["inventory"].(map[string]any)["skills"].([]any); len(rows) != 0 {
		t.Fatalf("final Skills inventory = %d rows, want empty reconciliation", len(rows))
	}
}

func TestSkillsWebSocketExternalLifecycleUsesReviewedPlansAndReconcilesInventory(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".zen")
	t.Setenv("HOME", home)
	t.Setenv("ZEN_STATE_DIR", stateDir)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	externalRoot := filepath.Join(home, ".claude", "skills")
	writeExternal := func(name string) string {
		t.Helper()
		path := filepath.Join(externalRoot, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: websocket external lifecycle fixture\n---\nproof\n"
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	adoptSource := writeExternal("adopt-proof")
	forgetSource := writeExternal("forget-proof")

	_, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	messages := messageSink(t, conn)
	send := func(payload map[string]any) {
		t.Helper()
		if err := conn.WriteJSON(payload); err != nil {
			t.Fatal(err)
		}
	}
	inventory := func(id string, generation int) map[string]any {
		send(map[string]any{"type": "skills_inventory", "request_id": id, "generation": generation})
		return readUntil(t, messages, "skills_inventory", id)
	}
	run := func(input map[string]any) {
		t.Helper()
		operation := input["operation"].(string)
		commandID := "external-command-" + operation
		commandRequest := map[string]any{"type": "skills_command", "request_id": commandID}
		for key, value := range input {
			commandRequest[key] = value
		}
		send(commandRequest)
		commandResponse := readUntil(t, messages, "skills_command", commandID)
		command := commandResponse["command"].(map[string]any)
		if command["operation"] != operation || command["scope"] != "global" {
			t.Fatalf("%s reviewed command contract = %+v", operation, command)
		}

		mutationID := "external-mutation-" + operation
		mutationRequest := map[string]any{"type": "skills_mutation", "request_id": mutationID}
		for key, value := range input {
			mutationRequest[key] = value
		}
		send(mutationRequest)
		result := readUntil(t, messages, "skills_mutation_result", mutationID)
		if result["success"] != true {
			t.Fatalf("%s execution failed: %+v", operation, result)
		}
	}

	initial := inventory("external-inventory-initial", 1)
	if rows := initial["inventory"].(map[string]any)["skills"].([]any); len(rows) != 2 {
		t.Fatalf("initial external Skills inventory = %d rows, want 2", len(rows))
	}
	send(map[string]any{"type": "skills_inspect", "request_id": "inspect-external", "generation": 1, "skill_name": "adopt-proof"})
	inspection := readUntil(t, messages, "skills_inspect_result", "inspect-external")
	if inspection["detail"].(map[string]any)["manager"] != "external" {
		t.Fatalf("external inspect response = %+v", inspection)
	}

	run(map[string]any{"operation": "migrate", "scope": "global"})
	run(map[string]any{"operation": "adopt", "skill_name": "adopt-proof", "scope": "global"})
	run(map[string]any{"operation": "forget", "skill_name": "forget-proof", "scope": "global"})

	// Adopt and forget intentionally leave external source files untouched.
	// Remove only this disposable fixture content before final reconciliation.
	if err := os.RemoveAll(adoptSource); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(forgetSource); err != nil {
		t.Fatal(err)
	}
	run(map[string]any{"operation": "uninstall", "skill_name": "adopt-proof", "scope": "global"})
	final := inventory("external-inventory-final", 2)
	if rows := final["inventory"].(map[string]any)["skills"].([]any); len(rows) != 0 {
		t.Fatalf("final external Skills inventory = %d rows, want empty reconciliation", len(rows))
	}
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
	if probe != nil {
		srv.skillsMutationExecuteOverride = probe.executeSkills
	}
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
