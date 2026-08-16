package server

import (
	"context"
	"testing"

	"github.com/gorilla/websocket"
)

func TestPluginsInventoryClaimReplacesStaleGenerationAndConsumesCurrentOnce(t *testing.T) {
	connection := &websocket.Conn{}
	_, cancelPrevious := context.WithCancel(context.Background())
	_, cancelCurrent := context.WithCancel(context.Background())
	previous := pluginsInventoryRequest{requestID: "plugins-previous", generation: 4, cancel: cancelPrevious}
	current := pluginsInventoryRequest{requestID: "plugins-current", generation: 5, cancel: cancelCurrent}
	server := &Server{pluginsInventories: map[*websocket.Conn]pluginsInventoryRequest{connection: previous}}
	t.Cleanup(cancelPrevious)
	t.Cleanup(cancelCurrent)

	if server.claimPluginsInventory(connection, pluginsInventoryRequest{
		requestID:  "plugins-stale",
		generation: 3,
		cancel:     cancelCurrent,
	}) {
		t.Fatal("stale plugin inventory claimed the current generation")
	}
	replaced, hadPrevious := server.replacePluginsInventory(connection, current)
	if !hadPrevious || replaced.requestID != previous.requestID {
		t.Fatalf("replace = %#v, %v", replaced, hadPrevious)
	}
	if server.claimPluginsInventory(connection, previous) {
		t.Fatal("superseded plugin inventory was claimable")
	}
	if !server.claimPluginsInventory(connection, current) {
		t.Fatal("current plugin inventory did not claim its generation")
	}
	if server.claimPluginsInventory(connection, current) {
		t.Fatal("current plugin inventory was claimable more than once")
	}
}

func TestPluginsMutationReplaceCancelsPreviousAndClaimsOnce(t *testing.T) {
	connection := &websocket.Conn{}
	previousContext, cancelPrevious := context.WithCancel(context.Background())
	_, cancelCurrent := context.WithCancel(context.Background())
	previous := pluginsMutationRequest{requestID: "plugins-mutation-previous", cancel: cancelPrevious}
	current := pluginsMutationRequest{requestID: "plugins-mutation-current", cancel: cancelCurrent}
	server := &Server{pluginsMutations: map[*websocket.Conn]pluginsMutationRequest{connection: previous}}
	t.Cleanup(cancelCurrent)

	replaced, hadPrevious := server.replacePluginsMutation(connection, current)
	if !hadPrevious || replaced.requestID != previous.requestID {
		t.Fatalf("replacement = %#v, %v", replaced, hadPrevious)
	}
	select {
	case <-previousContext.Done():
	default:
		t.Fatal("replacement left the previous plugin mutation running")
	}

	if server.claimPluginsMutation(connection, previous) {
		t.Fatal("stale plugin mutation claimed the current slot")
	}
	if !server.claimPluginsMutation(connection, current) {
		t.Fatal("current plugin mutation did not claim its slot")
	}
	if server.claimPluginsMutation(connection, current) {
		t.Fatal("current plugin mutation was claimable more than once")
	}
}

func TestPluginsMutationOwnershipPersistsWithoutConsuming(t *testing.T) {
	connection := &websocket.Conn{}
	_, cancel := context.WithCancel(context.Background())
	current := pluginsMutationRequest{requestID: "plugins-mutation-current", cancel: cancel}
	server := &Server{pluginsMutations: map[*websocket.Conn]pluginsMutationRequest{}}
	t.Cleanup(cancel)

	server.replacePluginsMutation(connection, current)
	if !server.isCurrentPluginsMutation(connection, current) {
		t.Fatal("plugin mutation ownership was not retained while registered")
	}
	if !server.isCurrentPluginsMutation(connection, current) {
		t.Fatal("isCurrent consumed the plugin mutation slot")
	}
	// Disconnect cancels and removes the owned slot so the in-flight command
	// is stopped and its stale result can never claim.
	server.mu.Lock()
	server.cancelSkillsRequestsLocked(connection)
	server.mu.Unlock()
	if server.isCurrentPluginsMutation(connection, current) {
		t.Fatal("disconnected plugin mutation still owns the slot")
	}
	if server.claimPluginsMutation(connection, current) {
		t.Fatal("disconnected plugin mutation claimed the slot")
	}
	if len(server.pluginsMutations) != 0 {
		t.Fatal("plugin mutation slot survived disconnect")
	}
}
