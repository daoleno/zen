package host

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeInhibitorLeg struct {
	acquires   int
	releases   int
	acquireErr error
	aliveFlag  bool
}

func (f *fakeInhibitorLeg) acquire(context.Context) error {
	f.acquires++
	if f.acquireErr != nil {
		return f.acquireErr
	}
	f.aliveFlag = true
	return nil
}

func (f *fakeInhibitorLeg) release() {
	f.releases++
	f.aliveFlag = false
}

func (f *fakeInhibitorLeg) alive() bool { return f.aliveFlag }

func fakeInhibitorSet() (map[string]inhibitorLeg, *fakeInhibitorLeg, *fakeInhibitorLeg, *fakeInhibitorLeg) {
	idle, sleep, lock := &fakeInhibitorLeg{}, &fakeInhibitorLeg{}, &fakeInhibitorLeg{}
	return map[string]inhibitorLeg{legLogindIdle: idle, legLogindSleep: sleep, legScreenSaver: lock}, idle, sleep, lock
}

func TestAuthorizationInhibitorsAcquiresAllLegsAndReleases(t *testing.T) {
	legs, idle, sleep, lock := fakeInhibitorSet()
	inhibitors := newAuthorizationInhibitors(1000, legs)
	status := inhibitors.Reconcile(context.Background(), true)
	if !status.Active || status.Reason != "authorization_active" {
		t.Fatalf("status = %+v", status)
	}
	if !status.IdleInhibited || !status.SuspendInhibited || !status.LockInhibited {
		t.Fatalf("leg flags = %+v", status)
	}
	if idle.acquires != 1 || sleep.acquires != 1 || lock.acquires != 1 {
		t.Fatalf("acquires = %d/%d/%d", idle.acquires, sleep.acquires, lock.acquires)
	}
	// Reconciling an already-held authorization is idempotent.
	if again := inhibitors.Reconcile(context.Background(), true); !again.Active {
		t.Fatalf("second reconcile = %+v", again)
	}
	if idle.acquires != 1 || sleep.acquires != 1 || lock.acquires != 1 {
		t.Fatalf("idempotent acquires = %d/%d/%d", idle.acquires, sleep.acquires, lock.acquires)
	}
	released := inhibitors.Release()
	if released.Active || released.Reason != "authorization_inactive" {
		t.Fatalf("released = %+v", released)
	}
	if idle.releases != 1 || sleep.releases != 1 || lock.releases != 1 {
		t.Fatalf("releases = %d/%d/%d", idle.releases, sleep.releases, lock.releases)
	}
	if idle.aliveFlag || sleep.aliveFlag || lock.aliveFlag {
		t.Fatal("a leg stayed alive after release")
	}
}

func TestAuthorizationInhibitorsReportsUnavailableLegTruthfully(t *testing.T) {
	legs, idle, sleep, lock := fakeInhibitorSet()
	sleep.acquireErr = errors.New("logind_inhibit_rejected")
	inhibitors := newAuthorizationInhibitors(1000, legs)
	status := inhibitors.Reconcile(context.Background(), true)
	if status.Active {
		t.Fatalf("incomplete leg set reported active: %+v", status)
	}
	if !status.IdleInhibited || status.SuspendInhibited || !status.LockInhibited {
		t.Fatalf("leg flags = %+v", status)
	}
	if status.LogindSleep != legStateUnavailable || status.Reason != legLogindSleep+"_unavailable" {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.Error, "logind_inhibit_rejected") {
		t.Fatalf("error = %q", status.Error)
	}
	if idle.aliveFlag == false || lock.aliveFlag == false {
		t.Fatal("partial legs must still be held")
	}
	inhibitors.Release()
	if idle.releases != 1 || sleep.releases != 0 || lock.releases != 1 {
		t.Fatalf("releases = %d/%d/%d", idle.releases, sleep.releases, lock.releases)
	}
}

func TestAuthorizationInhibitorsReacquiresDroppedLeg(t *testing.T) {
	legs, _, _, lock := fakeInhibitorSet()
	inhibitors := newAuthorizationInhibitors(1000, legs)
	if status := inhibitors.Reconcile(context.Background(), true); !status.Active {
		t.Fatalf("status = %+v", status)
	}
	// Simulate the KDE screen-saver connection dropping: the leg is no longer
	// alive, so the next reconcile releases and re-acquires it.
	lock.aliveFlag = false
	status := inhibitors.Reconcile(context.Background(), true)
	if !status.Active {
		t.Fatalf("status after re-acquire = %+v", status)
	}
	if lock.acquires != 2 || lock.releases != 1 {
		t.Fatalf("lock acquires/releases = %d/%d", lock.acquires, lock.releases)
	}
}

func TestOwnerSessionBusAddressValidatesRuntimeBoundary(t *testing.T) {
	root := t.TempDir()
	uid := uint32(os.Getuid())
	dir := filepath.Join(root, strconv.Itoa(int(uid)))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerSessionBusAddressIn(root, uid); err == nil || !strings.Contains(err.Error(), "unsafe_owner_runtime_dir") {
		t.Fatalf("public runtime dir error = %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bus := filepath.Join(dir, "bus")
	if err := os.WriteFile(bus, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerSessionBusAddressIn(root, uid); err == nil || !strings.Contains(err.Error(), "unsafe_owner_session_bus") {
		t.Fatalf("regular bus file error = %v", err)
	}
	_ = os.Remove(bus)
	unixAddr, err := net.ResolveUnixAddr("unix", bus)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", unixAddr)
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	_ = listener.Close()
	address, err := ownerSessionBusAddressIn(root, uid)
	if err != nil || address != "unix:path="+bus {
		t.Fatalf("address = %q err = %v", address, err)
	}
	if _, err := ownerSessionBusAddressIn(root, 0); err == nil {
		t.Fatal("root uid accepted")
	}
}

func TestSplitInhibitorWhat(t *testing.T) {
	got := splitInhibitorWhat("idle:sleep")
	if len(got) != 2 || got[0] != "idle" || got[1] != "sleep" {
		t.Fatalf("split = %v", got)
	}
	if len(splitInhibitorWhat("")) != 0 {
		t.Fatal("empty what produced entries")
	}
}
