package server

import (
	"context"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSkillsSearchClaimRejectsStaleGenerationAndConsumesCurrentOnce(t *testing.T) {
	connection := &websocket.Conn{}
	_, cancel := context.WithCancel(context.Background())
	current := skillsSearchRequest{
		requestID:  "request-current",
		generation: 7,
		cancel:     cancel,
	}
	server := &Server{
		skillsSearches: map[*websocket.Conn]skillsSearchRequest{
			connection: current,
		},
	}
	t.Cleanup(cancel)

	if server.claimSkillsSearch(connection, skillsSearchRequest{
		requestID:  "request-stale",
		generation: 6,
		cancel:     cancel,
	}) {
		t.Fatal("stale search claimed the current generation")
	}
	if !server.claimSkillsSearch(connection, current) {
		t.Fatal("current search did not claim its generation")
	}
	if server.claimSkillsSearch(connection, current) {
		t.Fatal("current search was claimable more than once")
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

func TestSkillsInventoryClaimReplacesStaleGenerationAndConsumesCurrentOnce(t *testing.T) {
	connection := &websocket.Conn{}
	previousContext, cancelPrevious := context.WithCancel(context.Background())
	_, cancelCurrent := context.WithCancel(context.Background())
	previous := skillsInventoryRequest{requestID: "inventory-previous", generation: 8, cancel: cancelPrevious}
	current := skillsInventoryRequest{requestID: "inventory-current", generation: 9, cancel: cancelCurrent}
	server := &Server{skillsInventories: map[*websocket.Conn]skillsInventoryRequest{connection: previous}}
	t.Cleanup(cancelCurrent)
	if replaced, ok := server.replaceSkillsInventory(connection, current); !ok || replaced.requestID != previous.requestID {
		t.Fatalf("replacement = %#v/%v", replaced, ok)
	}
	select {
	case <-previousContext.Done():
	default:
		t.Fatal("rapid refresh left the previous inventory scan active")
	}

	if server.claimSkillsInventory(connection, previous) {
		t.Fatal("stale inventory claimed the current generation")
	}
	if !server.claimSkillsInventory(connection, current) || server.claimSkillsInventory(connection, current) {
		t.Fatal("current inventory was not consumed exactly once")
	}
}

func TestSkillsSearchCancellationIsGenerationCorrelated(t *testing.T) {
	connection := &websocket.Conn{}
	ctx, cancel := context.WithCancel(context.Background())
	current := skillsSearchRequest{requestID: "search-current", generation: 4, cancel: cancel}
	server := &Server{skillsSearches: map[*websocket.Conn]skillsSearchRequest{connection: current}}

	if _, ok := server.cancelSkillsSearch(connection, 3); ok {
		t.Fatal("stale cancellation canceled a newer search")
	}
	if _, ok := server.cancelSkillsSearch(connection, 4); !ok {
		t.Fatal("current search was not canceled")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("search context remained active")
	}
}

func TestSkillsDisconnectCancelsInventoryAndSearchOwners(t *testing.T) {
	connection := &websocket.Conn{}
	inventoryContext, cancelInventory := context.WithCancel(context.Background())
	searchContext, cancelSearch := context.WithCancel(context.Background())
	server := &Server{
		skillsInventories: map[*websocket.Conn]skillsInventoryRequest{
			connection: {requestID: "inventory", generation: 1, cancel: cancelInventory},
		},
		skillsSearches: map[*websocket.Conn]skillsSearchRequest{
			connection: {requestID: "search", generation: 1, cancel: cancelSearch},
		},
	}
	server.mu.Lock()
	server.cancelSkillsRequestsLocked(connection)
	server.mu.Unlock()
	for name, ctx := range map[string]context.Context{"inventory": inventoryContext, "search": searchContext} {
		select {
		case <-ctx.Done():
		default:
			t.Fatalf("%s context remained active after disconnect", name)
		}
	}
	if len(server.skillsInventories) != 0 || len(server.skillsSearches) != 0 {
		t.Fatalf("request owners survived disconnect")
	}
}
