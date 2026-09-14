package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

// startPrivateBus runs one owned dbus-daemon so the inhibitor legs can be
// exercised against a real D-Bus transport without touching the developer's
// system or session bus.
func startPrivateBus(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("dbus-daemon", "--session", "--fork", "--print-address=1", "--print-pid=1").Output()
	if err != nil {
		t.Skipf("dbus-daemon unavailable: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected dbus-daemon output: %q", output)
	}
	address := strings.TrimSpace(lines[0])
	pid, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		t.Fatalf("invalid dbus-daemon pid: %q", lines[1])
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGTERM) })
	return address
}

type fakeLogindManager struct {
	mu   sync.Mutex
	held []*os.File
}

func (f *fakeLogindManager) Inhibit(what, who, why, mode string) (dbus.UnixFD, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if mode != "block" || who != InhibitorWho || what == "" {
		return 0, dbus.MakeFailedError(fmt.Errorf("unexpected inhibit arguments"))
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	// Keep the read end open: closing it is what releases a logind inhibitor.
	f.held = append(f.held, readEnd)
	_ = writeEnd.Close()
	return dbus.UnixFD(readEnd.Fd()), nil
}

// rejectingLogindManager returns one fixed authorization error, the same shape
// logind returns when polkit rejects a scoped block inhibitor.
type rejectingLogindManager struct {
	err *dbus.Error
}

func (r *rejectingLogindManager) Inhibit(string, string, string, string) (dbus.UnixFD, *dbus.Error) {
	return 0, r.err
}

type fakeScreenSaver struct {
	mu       sync.Mutex
	next     uint32
	released map[uint32]bool
}

func (f *fakeScreenSaver) Inhibit(application, reason string) (uint32, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if application != InhibitorWho || reason == "" {
		return 0, dbus.MakeFailedError(fmt.Errorf("unexpected inhibit arguments"))
	}
	f.next++
	return f.next, nil
}

func (f *fakeScreenSaver) UnInhibit(cookie uint32) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.released == nil {
		f.released = map[uint32]bool{}
	}
	f.released[cookie] = true
	return nil
}

func exportFakeInhibitServices(t *testing.T, conn *dbus.Conn) *fakeScreenSaver {
	t.Helper()
	logind := &fakeLogindManager{}
	saver := &fakeScreenSaver{released: map[uint32]bool{}}
	if err := conn.Export(logind, dbus.ObjectPath("/org/freedesktop/login1"), "org.freedesktop.login1.Manager"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(saver, dbus.ObjectPath("/ScreenSaver"), "org.freedesktop.ScreenSaver"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"org.freedesktop.login1", "org.freedesktop.ScreenSaver"} {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("request %s: reply=%v err=%v", name, reply, err)
		}
	}
	return saver
}

func TestLogindInhibitorHoldsRealDescriptorOverBus(t *testing.T) {
	address := startPrivateBus(t)
	service, err := dbus.Connect(address)
	if err != nil {
		t.Skipf("private bus connect: %v", err)
	}
	defer service.Close()
	exportFakeInhibitServices(t, service)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", address)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leg := &logindInhibitor{what: "idle", fd: -1}
	if err := leg.acquire(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !leg.alive() || leg.fd < 0 {
		t.Fatalf("leg not alive: %+v", leg)
	}
	if _, err := unix.FcntlInt(uintptr(leg.fd), unix.F_GETFD, 0); err != nil {
		t.Fatalf("inhibitor descriptor is not open: %v", err)
	}
	leg.release()
	if leg.alive() || leg.fd != -1 {
		t.Fatalf("leg not released: %+v", leg)
	}
}

// TestLogindInhibitorSurfacesPolicyKitRejection keeps the stable leg code and
// the raw systemd/polkit reason, so an operator status read explains exactly
// why the kernel-level leg was not held instead of a generic rejection.
func TestLogindInhibitorSurfacesPolicyKitRejection(t *testing.T) {
	address := startPrivateBus(t)
	service, err := dbus.Connect(address)
	if err != nil {
		t.Skipf("private bus connect: %v", err)
	}
	defer service.Close()
	rejection := dbus.NewError("org.freedesktop.PolicyKit1.Error.NotAuthorized", []interface{}{"Not authorized"})
	if err := service.Export(&rejectingLogindManager{err: rejection}, dbus.ObjectPath("/org/freedesktop/login1"), "org.freedesktop.login1.Manager"); err != nil {
		t.Fatal(err)
	}
	if reply, err := service.RequestName("org.freedesktop.login1", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name: reply=%v err=%v", reply, err)
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", address)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leg := &logindInhibitor{what: "sleep", fd: -1}
	err = leg.acquire(ctx)
	if err == nil {
		t.Fatal("rejected sleep inhibitor reported success")
	}
	for _, want := range []string{"logind_inhibit_rejected", "org.freedesktop.PolicyKit1.Error.NotAuthorized"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not preserve %q", err, want)
		}
	}
	if leg.alive() {
		t.Fatal("rejected leg reported alive")
	}
}

// TestDBusRejectionErrorMapping is the deterministic companion to the private
// bus rejection tests: the stable leg code must survive, the D-Bus error name
// (the systemd/polkit class) must be appended when present, and a non-D-Bus
// transport failure must keep its wrapped cause.
func TestDBusRejectionErrorMapping(t *testing.T) {
	// The wire error from godbus is a value dbus.Error (conn.go builds it from
	// the reply message), which is the shape production sees. NewError returns
	// a pointer for exporting, not for matching, so the value is used here.
	rejection := dbusRejectionError("logind_inhibit_rejected",
		dbus.Error{Name: "org.freedesktop.PolicyKit1.Error.NotAuthorized", Body: []interface{}{"Not authorized"}})
	for _, want := range []string{"logind_inhibit_rejected", "org.freedesktop.PolicyKit1.Error.NotAuthorized", "Not authorized"} {
		if !strings.Contains(rejection.Error(), want) {
			t.Fatalf("error %q does not preserve %q", rejection, want)
		}
	}
	transport := dbusRejectionError("system_bus_unavailable", fmt.Errorf("connect: %w", os.ErrNotExist))
	for _, want := range []string{"system_bus_unavailable", "connect:"} {
		if !strings.Contains(transport.Error(), want) {
			t.Fatalf("error %q does not preserve %q", transport, want)
		}
	}
	if !errors.Is(transport, os.ErrNotExist) {
		t.Fatalf("wrapped cause was dropped: %v", transport)
	}
}

func TestScreenSaverInhibitorSurfacesRejection(t *testing.T) {
	address := startPrivateBus(t)
	service, err := dbus.Connect(address)
	if err != nil {
		t.Skipf("private bus connect: %v", err)
	}
	defer service.Close()
	rejecting := &rejectingScreenSaver{err: dbus.NewError("org.kde.ScreenSaver.Error.NotAuthorized", []interface{}{"denied"})}
	if err := service.Export(rejecting, dbus.ObjectPath("/ScreenSaver"), "org.freedesktop.ScreenSaver"); err != nil {
		t.Fatal(err)
	}
	if reply, err := service.RequestName("org.freedesktop.ScreenSaver", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name: reply=%v err=%v", reply, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leg := &screenSaverInhibitor{uid: uint32(os.Getuid()), address: address}
	err = leg.acquire(ctx)
	if err == nil {
		t.Fatal("rejected screen-saver inhibitor reported success")
	}
	for _, want := range []string{"screen_saver_inhibit_rejected", "org.kde.ScreenSaver.Error.NotAuthorized"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not preserve %q", err, want)
		}
	}
}

type rejectingScreenSaver struct {
	err *dbus.Error
}

func (r *rejectingScreenSaver) Inhibit(string, string) (uint32, *dbus.Error) {
	return 0, r.err
}

func (r *rejectingScreenSaver) UnInhibit(uint32) *dbus.Error { return nil }

func TestScreenSaverInhibitorHoldsRealCookieOverBus(t *testing.T) {
	address := startPrivateBus(t)
	service, err := dbus.Connect(address)
	if err != nil {
		t.Skipf("private bus connect: %v", err)
	}
	defer service.Close()
	saver := exportFakeInhibitServices(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leg := &screenSaverInhibitor{uid: uint32(os.Getuid()), address: address}
	if err := leg.acquire(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !leg.alive() || leg.cookie == 0 {
		t.Fatalf("leg not alive: %+v", leg)
	}
	cookie := leg.cookie
	leg.release()
	if leg.alive() || leg.cookie != 0 {
		t.Fatalf("leg not released: %+v", leg)
	}
	saver.mu.Lock()
	released := saver.released[cookie]
	saver.mu.Unlock()
	if !released {
		t.Fatalf("UnInhibit was not called for cookie %d", cookie)
	}
}

func TestScreenSaverInhibitorInvalidatesCookieOnServiceRestart(t *testing.T) {
	address := startPrivateBus(t)
	service, err := dbus.Connect(address)
	if err != nil {
		t.Skipf("private bus connect: %v", err)
	}
	defer service.Close()
	exportFakeInhibitServices(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leg := &screenSaverInhibitor{uid: uint32(os.Getuid()), address: address}
	if err := leg.acquire(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !leg.alive() {
		t.Fatal("fresh cookie not alive")
	}
	// A restarted screen-saver service takes the name from a new connection.
	restarted, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if reply, err := service.ReleaseName("org.freedesktop.ScreenSaver"); err != nil || reply != dbus.ReleaseNameReplyReleased {
		t.Fatalf("release name: reply=%v err=%v", reply, err)
	}
	if err := restarted.Export(&fakeScreenSaver{released: map[uint32]bool{}}, dbus.ObjectPath("/ScreenSaver"), "org.freedesktop.ScreenSaver"); err != nil {
		t.Fatal(err)
	}
	if reply, err := restarted.RequestName("org.freedesktop.ScreenSaver", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("re-request name: reply=%v err=%v", reply, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for leg.alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if leg.alive() {
		t.Fatal("stale cookie was still reported alive after the service restarted")
	}
	leg.release()
}
