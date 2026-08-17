package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSkillsInspectClaimReplacesStaleGenerationAndConsumesCurrentOnce(t *testing.T) {
	connection := &websocket.Conn{}
	previousContext, cancelPrevious := context.WithCancel(context.Background())
	_, cancelCurrent := context.WithCancel(context.Background())
	previous := skillsInspectRequest{requestID: "inspect-previous", generation: 2, cancel: cancelPrevious}
	current := skillsInspectRequest{requestID: "inspect-current", generation: 3, cancel: cancelCurrent}
	server := &Server{skillsInspects: map[*websocket.Conn]skillsInspectRequest{connection: previous}}
	t.Cleanup(cancelCurrent)
	if replaced, ok := server.replaceSkillsInspect(connection, current); !ok || replaced.requestID != previous.requestID {
		t.Fatalf("replacement = %#v/%v", replaced, ok)
	}
	select {
	case <-previousContext.Done():
	default:
		t.Fatal("inspect replacement left the previous scan active")
	}
	if server.claimSkillsInspect(connection, previous) || !server.claimSkillsInspect(connection, current) || server.claimSkillsInspect(connection, current) {
		t.Fatal("inspect request ownership was not generation-safe")
	}
}

func TestSkillsInventoryClaimReplacesStaleGenerationAndConsumesCurrentOnce(t *testing.T) {
	connection := &websocket.Conn{}
	previousContext, cancelPrevious := context.WithCancel(context.Background())
	_, cancelCurrent := context.WithCancel(context.Background())
	previous := skillsInventoryRequest{requestID: "inventory-previous", generation: 8, cancel: cancelPrevious}
	current := skillsInventoryRequest{requestID: "inventory-current", generation: 9, cancel: cancelCurrent}
	server := &Server{skillsInventories: map[*websocket.Conn]skillsInventoryRequest{connection: previous}}
	t.Cleanup(cancelCurrent)
	if _, ok := server.replaceSkillsInventory(connection, current); !ok {
		t.Fatal("inventory replacement did not return the previous owner")
	}
	select {
	case <-previousContext.Done():
	default:
		t.Fatal("inventory replacement left the previous scan active")
	}
	if server.claimSkillsInventory(connection, previous) || !server.claimSkillsInventory(connection, current) || server.claimSkillsInventory(connection, current) {
		t.Fatal("inventory request ownership was not generation-safe")
	}
}

func TestSkillsDisconnectCancelsLocalOwners(t *testing.T) {
	connection := &websocket.Conn{}
	inventoryContext, cancelInventory := context.WithCancel(context.Background())
	inspectContext, cancelInspect := context.WithCancel(context.Background())
	mutationContext, cancelMutation := context.WithCancel(context.Background())
	server := &Server{
		skillsInventories: map[*websocket.Conn]skillsInventoryRequest{connection: {requestID: "inventory", generation: 1, cancel: cancelInventory}},
		skillsInspects:    map[*websocket.Conn]skillsInspectRequest{connection: {requestID: "inspect", generation: 1, cancel: cancelInspect}},
		skillsMutations:   map[*websocket.Conn]skillsMutationRequest{connection: {requestID: "mutation", cancel: cancelMutation}},
	}
	server.mu.Lock()
	server.cancelSkillsRequestsLocked(connection)
	server.mu.Unlock()
	for name, ctx := range map[string]context.Context{"inventory": inventoryContext, "inspect": inspectContext, "mutation": mutationContext} {
		select {
		case <-ctx.Done():
		default:
			t.Fatalf("%s context remained active after disconnect", name)
		}
	}
}

func TestSkillsRequestIdentityIsBoundedAndASCII(t *testing.T) {
	for _, valid := range []string{"skills_123", "request:abc-123"} {
		if !validSkillsRequestID(valid) {
			t.Fatalf("valid request id %q was rejected", valid)
		}
	}
	for _, invalid := range []string{"", " leading", "line\nbreak", "snowman-☃"} {
		if validSkillsRequestID(invalid) {
			t.Fatalf("invalid request id %q was accepted", invalid)
		}
	}
}

func TestSkillsDiscoveryMessagesAreNotExposed(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("server.go"))
	if err != nil {
		t.Fatal(err)
	}
	management, err := os.ReadFile(filepath.Join("skills_management.go"))
	if err != nil {
		t.Fatal(err)
	}
	joined := string(source) + string(management)
	for _, removed := range []string{"skills_catalog", "skills_search", "skills_search_cancel"} {
		if strings.Contains(joined, `case "`+removed+`"`) {
			t.Fatalf("removed Discovery message %q is still routed", removed)
		}
	}
}
