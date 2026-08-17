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

type mutationProbe struct {
	started   chan struct{}
	release   chan struct{}
	executed  atomic.Int32
	cancelled atomic.Int32
}

func newMutationProbe() *mutationProbe {
	return &mutationProbe{started: make(chan struct{}, 16), release: make(chan struct{})}
}

func (p *mutationProbe) executeSkills(ctx context.Context, _ skillmgmt.MutationCommand, _ skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error) {
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

func configureSkillsTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZEN_STATE_DIR", filepath.Join(home, ".zen"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func writeServerSkill(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: websocket fixture\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func deleteWireInput(row map[string]any) map[string]any {
	return map[string]any{
		"operation": "delete", "skill_id": row["id"], "skill_name": row["name"],
		"root_path": row["root_path"], "canonical_path": row["canonical_path"],
		"allowed_root": row["allowed_root"],
	}
}

func TestSkillsWebSocketDeleteUsesReviewedExactIdentityAndReconciles(t *testing.T) {
	home := configureSkillsTestHome(t)
	root := filepath.Join(home, ".codex", "skills")
	selected := writeServerSkill(t, root, "delete-proof")
	neighbor := writeServerSkill(t, root, "neighbor")
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
	initial := inventory("inventory-before", 1)
	row := skillInventoryRow(t, initial, "delete-proof")
	input := deleteWireInput(row)
	commandRequest := map[string]any{"type": "skills_command", "request_id": "command-delete"}
	for key, value := range input {
		commandRequest[key] = value
	}
	send(commandRequest)
	commandResponse := readUntil(t, messages, "skills_command", "command-delete")
	command := commandResponse["command"].(map[string]any)
	for _, key := range []string{"copy_id", "skill_name", "root_path", "canonical_path", "allowed_root"} {
		inputKey := key
		if key == "copy_id" {
			inputKey = "skill_id"
		}
		if command[key] != input[inputKey] {
			t.Fatalf("reviewed %s = %v, want %v", key, command[key], input[inputKey])
		}
	}
	if command["destructive"] != true || command["location"] != "Codex global Skills" {
		t.Fatalf("reviewed delete command = %+v", command)
	}
	mutationRequest := map[string]any{"type": "skills_mutation", "request_id": "mutation-delete"}
	for key, value := range input {
		mutationRequest[key] = value
	}
	send(mutationRequest)
	result := readUntil(t, messages, "skills_mutation_result", "mutation-delete")
	if result["success"] != true {
		t.Fatalf("delete result = %+v", result)
	}
	if _, err := os.Lstat(selected); !os.IsNotExist(err) {
		t.Fatalf("selected root remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(neighbor, "SKILL.md")); err != nil {
		t.Fatalf("neighbor was changed: %v", err)
	}
	after := inventory("inventory-after", 2)
	if hasSkillInventoryRow(after, "delete-proof") || !hasSkillInventoryRow(after, "neighbor") {
		t.Fatalf("reconciled inventory = %+v", after)
	}
}

func TestSkillsWebSocketRejectsObsoleteOperations(t *testing.T) {
	configureSkillsTestHome(t)
	_, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	messages := messageSink(t, conn)
	for _, operation := range []string{"migrate", "adopt", "bind", "unbind", "enable", "disable", "uninstall", "forget", "update", "import"} {
		for _, messageType := range []string{"skills_command", "skills_mutation"} {
			requestID := messageType + "-" + operation
			if err := conn.WriteJSON(map[string]any{"type": messageType, "request_id": requestID, "operation": operation}); err != nil {
				t.Fatal(err)
			}
			response := readUntil(t, messages, messageType+"_error", requestID)
			if response["code"] != "unsupported_operation" {
				t.Fatalf("%s %s response = %+v", messageType, operation, response)
			}
		}
	}
}

func TestSkillsDeleteRequiresCompleteIdentity(t *testing.T) {
	configureSkillsTestHome(t)
	_, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	messages := messageSink(t, conn)
	for _, messageType := range []string{"skills_command", "skills_mutation"} {
		requestID := "missing-" + messageType
		if err := conn.WriteJSON(map[string]any{"type": messageType, "request_id": requestID, "operation": "delete", "skill_id": strings.Repeat("a", 24), "skill_name": "demo"}); err != nil {
			t.Fatal(err)
		}
		response := readUntil(t, messages, messageType+"_error", requestID)
		if response["code"] != "invalid_request" || !strings.Contains(response["message"].(string), "complete") {
			t.Fatalf("missing identity response = %+v", response)
		}
	}
}

func TestSkillsMutationSupersedeCancelsInFlightDeleteAndSuppressesStaleResult(t *testing.T) {
	home := configureSkillsTestHome(t)
	writeServerSkill(t, filepath.Join(home, ".codex", "skills"), "first")
	writeServerSkill(t, filepath.Join(home, ".pi", "agent", "skills"), "second")
	inventory, err := skillmgmt.DiscoverInventory(skillmgmt.InventoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]map[string]any{}
	for _, copy := range inventory.Skills {
		if copy.Name == "first" || copy.Name == "second" {
			inputs[copy.Name] = map[string]any{
				"operation": "delete", "skill_id": copy.ID, "skill_name": copy.Name,
				"root_path": copy.RootPath, "canonical_path": copy.CanonicalPath, "allowed_root": copy.AllowedRoot,
			}
		}
	}
	probe := newMutationProbe()
	srv, conn, closeConn := newSkillsMutationTestServer(t, probe)
	messages := messageSink(t, conn)
	send := func(requestID, name string) {
		payload := map[string]any{"type": "skills_mutation", "request_id": requestID}
		for key, value := range inputs[name] {
			payload[key] = value
		}
		if err := conn.WriteJSON(payload); err != nil {
			t.Fatal(err)
		}
	}
	send("req-first", "first")
	waitProbeStarted(t, probe)
	if !srv.isCurrentSkillsMutation(serverSideConnection(t, srv), skillsMutationRequest{requestID: "req-first"}) {
		t.Fatal("first request lost ownership while executing")
	}
	send("req-second", "second")
	waitProbeStarted(t, probe)
	waitFor(t, func() bool { return probe.cancelled.Load() == 1 }, "first delete was not canceled")
	if payload := readUntil(t, messages, "skills_mutation_error", "req-first"); payload["code"] != "superseded" {
		t.Fatalf("superseded response = %+v", payload)
	}
	closeConn()
	waitFor(t, func() bool { return probe.cancelled.Load() == 2 }, "disconnect did not cancel second delete")
	expectNoMessage(t, messages, "skills_mutation_result")
}

func skillInventoryRow(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	inventory := payload["inventory"].(map[string]any)
	for _, raw := range inventory["skills"].([]any) {
		row := raw.(map[string]any)
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("Skill %q not found in %+v", name, payload)
	return nil
}

func hasSkillInventoryRow(payload map[string]any, name string) bool {
	inventory := payload["inventory"].(map[string]any)
	for _, raw := range inventory["skills"].([]any) {
		if raw.(map[string]any)["name"] == name {
			return true
		}
	}
	return false
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
	return srv, conn, func() { _ = conn.Close(); httpServer.Close() }
}

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
			if json.Unmarshal(raw, &payload) == nil {
				messages <- payload
			}
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

func expectNoMessage(t *testing.T, messages <-chan map[string]any, messageType string) {
	t.Helper()
	for {
		select {
		case payload, ok := <-messages:
			if !ok {
				return
			}
			if payload["type"] == messageType {
				t.Fatalf("unexpected %s arrived", messageType)
			}
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}
