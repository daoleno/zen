package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAuthorizationController(t *testing.T, legs map[string]inhibitorLeg, session OwnerSession, sessionErr error) (*AuthorizationController, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "desktop", "authorization-status.json")
	controller := newAuthorizationController(uint32(os.Getuid()), newAuthorizationInhibitors(uint32(os.Getuid()), legs),
		func(context.Context, uint32) (OwnerSession, error) { return session, sessionErr }, path)
	return controller, path
}

func TestAuthorizationControllerActivePublishesOwnerOnlyStatus(t *testing.T) {
	legs, idle, _, _ := fakeInhibitorSet()
	controller, path := testAuthorizationController(t, legs, OwnerSession{
		UID: uint32(os.Getuid()), ID: "2", Seat: "seat0", Backend: "wayland", Display: "wayland-0",
	}, nil)
	status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 1, HostConfigured: true, Reachable: true})
	if !status.Active || status.Reason != "authorization_active" {
		t.Fatalf("status = %+v", status)
	}
	if status.Session == nil || status.Session.Display != "wayland-0" {
		t.Fatalf("session = %+v", status.Session)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("status mode = %04o", info.Mode().Perm())
	}
	recorded, err := ReadAuthorizationStatus(path)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !recorded.Active || recorded.Inhibitors.LogindIdle != legStateActive {
		t.Fatalf("recorded = %+v", recorded)
	}
	if !AuthorizationStatusFresh(recorded, time.Now()) {
		t.Fatal("live record reported stale")
	}
	if idle.acquires != 1 {
		t.Fatalf("idle acquires = %d", idle.acquires)
	}
}

func TestAuthorizationControllerReleasesWithoutAuthorization(t *testing.T) {
	legs, idle, _, _ := fakeInhibitorSet()
	controller, _ := testAuthorizationController(t, legs, OwnerSession{UID: uint32(os.Getuid()), ID: "2", Seat: "seat0", Backend: "wayland", Display: "wayland-0"}, nil)
	if status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 1, Reachable: true}); !status.Active {
		t.Fatalf("initial = %+v", status)
	}
	status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 0, Reachable: true})
	if status.Active || status.Reason != "authorization_inactive" {
		t.Fatalf("revoked = %+v", status)
	}
	if idle.releases != 1 || idle.aliveFlag {
		t.Fatalf("idle releases = %d alive = %v", idle.releases, idle.aliveFlag)
	}
}

func TestAuthorizationControllerReleasesWhenSessionUnavailable(t *testing.T) {
	legs, _, _, _ := fakeInhibitorSet()
	controller, _ := testAuthorizationController(t, legs, OwnerSession{}, errors.New("owner_session_locked"))
	status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 1, Reachable: true})
	if status.Active || status.Reason != "session_unavailable" {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.Error, "owner_session_locked") {
		t.Fatalf("error = %q", status.Error)
	}
	if !strings.Contains(status.Recovery, "loginctl unlock-session") {
		t.Fatalf("recovery = %q", status.Recovery)
	}
	if status.Inhibitors.LogindIdle != legStateReleased {
		t.Fatalf("inhibitors = %+v", status.Inhibitors)
	}
}

func TestAuthorizationControllerReleasesWhenHostUnreachable(t *testing.T) {
	legs, idle, _, _ := fakeInhibitorSet()
	discovered := 0
	controller := newAuthorizationController(uint32(os.Getuid()), newAuthorizationInhibitors(uint32(os.Getuid()), legs),
		func(context.Context, uint32) (OwnerSession, error) { discovered++; return OwnerSession{}, nil }, filepath.Join(t.TempDir(), "status.json"))
	if status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 1, Reachable: true}); !status.Active {
		t.Fatalf("initial = %+v", status)
	}
	status := controller.Reconcile(context.Background(), AuthorizationInput{AuthorizedDevices: 1, Reachable: false})
	if status.Active || status.Reason != "host_unreachable" || discovered != 1 {
		t.Fatalf("status = %+v discovered = %d", status, discovered)
	}
	if idle.releases != 1 {
		t.Fatalf("idle releases = %d", idle.releases)
	}
}

func TestReadAuthorizationStatusRejectsUnsafeRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAuthorizationStatus(path); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("public record error = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAuthorizationStatus(path); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("version error = %v", err)
	}
}

func TestAuthorizationStatusFreshRequiresLiveProcess(t *testing.T) {
	live := AuthorizationStatus{Version: 1, PID: os.Getpid(), UpdatedAt: time.Now().UTC()}
	if !AuthorizationStatusFresh(live, time.Now()) {
		t.Fatal("live record reported stale")
	}
	dead := live
	dead.PID = 1 << 30
	if AuthorizationStatusFresh(dead, time.Now()) {
		t.Fatal("dead pid reported fresh")
	}
	old := live
	old.UpdatedAt = time.Now().Add(-time.Hour)
	if AuthorizationStatusFresh(old, time.Now()) {
		t.Fatal("old record reported fresh")
	}
}
