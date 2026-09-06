package relay

import (
	"strings"
	"testing"
	"time"
)

func TestClientReleaseReturnsUnconsumedAdmissionWithCapacity(t *testing.T) {
	server := limitedRelay(t)
	alias, stream := strings.Repeat("a", 32), strings.Repeat("b", 32)
	route := &routeSession{routeID: strings.Repeat("c", 32)}
	server.routes[route.routeID] = route
	server.admissions[alias] = admission{routeID: route.routeID, reservedStream: stream, expiresAt: time.Now().Add(time.Minute)}
	if !server.reserveClient(route) {
		t.Fatal("client reservation failed")
	}
	server.releaseClient(route, alias, stream)
	if server.Snapshot().ActiveClients != 0 || route.activeClients != 0 {
		t.Fatal("client capacity did not return")
	}
	if _, _, _, ok := server.reserveRoute(alias, strings.Repeat("d", 32)); !ok {
		t.Fatal("idle client left admission reserved")
	}
}

func TestClientReleaseDoesNotRecreateConsumedAdmission(t *testing.T) {
	server := limitedRelay(t)
	route := &routeSession{routeID: strings.Repeat("c", 32)}
	if !server.reserveClient(route) {
		t.Fatal("client reservation failed")
	}
	server.releaseClient(route, strings.Repeat("a", 32), strings.Repeat("b", 32))
	if len(server.admissions) != 0 {
		t.Fatal("client release recreated a consumed admission")
	}
}
