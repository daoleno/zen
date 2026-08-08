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
