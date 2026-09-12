package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func waylandFixtureDir(t *testing.T, uid uint32) string {
	t.Helper()
	// AF_UNIX socket paths are limited to 108 bytes; the session TMPDIR is long,
	// so bind under a short private root instead.
	root, err := os.MkdirTemp("/tmp", "zwl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	runtime := filepath.Join(root, "1000")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(runtime, int(uid), int(uid)); err != nil {
		t.Skipf("cannot own fixture runtime dir: %v", err)
	}
	listener, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(listener) })
	if err := unix.Bind(listener, &unix.SockaddrUnix{Name: filepath.Join(runtime, "bus")}); err != nil {
		t.Fatal(err)
	}
	return root
}

func waylandSocketFixture(t *testing.T, root string, name string, held bool) {
	t.Helper()
	runtime := filepath.Join(root, "1000")
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrUnix{Name: filepath.Join(runtime, name)}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(runtime, name), 0755); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(runtime, name+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if held {
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		// Keep the lock for the duration of the test; cleanup closes the fd.
		t.Cleanup(func() { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN) })
	}
}

func TestDiscoverWaylandTargetSelectsHeldCompositor(t *testing.T) {
	uid := uint32(os.Getuid())
	root := waylandFixtureDir(t, uid)
	waylandSocketFixture(t, root, "wayland-3", false)
	waylandSocketFixture(t, root, "wayland-0", true)
	target, err := discoverWaylandTargetAt(root, uid)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if target.Display != "wayland-0" {
		t.Fatalf("display=%q, want the lock-held compositor", target.Display)
	}
	if target.RuntimeDir != filepath.Join(root, "1000") {
		t.Fatalf("runtime=%q", target.RuntimeDir)
	}
	if target.BusAddress != "unix:path="+filepath.Join(root, "1000", "bus") {
		t.Fatalf("bus=%q", target.BusAddress)
	}
}

func TestDiscoverWaylandTargetFailsClosed(t *testing.T) {
	uid := uint32(os.Getuid())
	t.Run("missing runtime", func(t *testing.T) {
		if _, err := discoverWaylandTargetAt(t.TempDir(), uid); err == nil || err.Error() != "wayland_runtime_unavailable" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("missing bus", func(t *testing.T) {
		root := t.TempDir()
		runtime := filepath.Join(root, "1000")
		if err := os.Mkdir(runtime, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(runtime, int(uid), int(uid)); err != nil {
			t.Skipf("cannot own fixture runtime dir: %v", err)
		}
		if _, err := discoverWaylandTargetAt(root, uid); err == nil || err.Error() != "wayland_bus_unavailable" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("stale socket only", func(t *testing.T) {
		root := waylandFixtureDir(t, uid)
		waylandSocketFixture(t, root, "wayland-0", false)
		if _, err := discoverWaylandTargetAt(root, uid); err == nil || err.Error() != "wayland_display_unavailable" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("ambiguous compositors", func(t *testing.T) {
		root := waylandFixtureDir(t, uid)
		waylandSocketFixture(t, root, "wayland-0", true)
		waylandSocketFixture(t, root, "wayland-1", true)
		if _, err := discoverWaylandTargetAt(root, uid); err == nil || err.Error() != "wayland_display_ambiguous" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("world-writable runtime", func(t *testing.T) {
		root := waylandFixtureDir(t, uid)
		if err := os.Chmod(filepath.Join(root, "1000"), 0777); err != nil {
			t.Fatal(err)
		}
		if _, err := discoverWaylandTargetAt(root, uid); err == nil || err.Error() != "wayland_runtime_unavailable" {
			t.Fatalf("err=%v", err)
		}
	})
}

func brokerWaylandFixture(t *testing.T) (*broker, waylandTarget) {
	t.Helper()
	target := waylandTarget{RuntimeDir: "/run/user/1000", BusAddress: "unix:path=/run/user/1000/bus", Display: "wayland-0"}
	b := &broker{config: HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"}, monitorReady: true}
	b.inspectLinux = func(context.Context) (LinuxObservation, error) {
		return LinuxObservation{Session: Session{BootID: "boot", ID: "27", Seat: "seat0", UID: 1000, Surface: Desktop, Backend: "wayland", Active: true}, Class: "user"}, nil
	}
	b.discover = func(uid uint32) (waylandTarget, error) {
		if uid != 1000 {
			t.Fatalf("discover uid=%d", uid)
		}
		return target, nil
	}
	return b, target
}

func TestObserveLockedAdmitsWaylandOwnerDesktop(t *testing.T) {
	b, target := brokerWaylandFixture(t)
	if err := b.observeLocked(context.Background()); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if b.session.Backend != "wayland-portal" || !b.session.Ready || !b.session.Control || b.session.Surface != Desktop {
		t.Fatalf("session=%+v", b.session)
	}
	if b.wayland != target || !b.portal {
		t.Fatalf("target=%+v portal=%t", b.wayland, b.portal)
	}
	if b.observeError != "" {
		t.Fatalf("observeError=%q", b.observeError)
	}
}

func TestObserveLockedRefusesWaylandLockedAndGreeter(t *testing.T) {
	for name, tc := range map[string]struct {
		surface Surface
		class   string
		uid     uint32
		want    string
	}{
		"locked":  {surface: Locked, class: "user", uid: 1000, want: "wayland_session_locked"},
		"greeter": {surface: Greeter, class: "greeter", uid: 959, want: "wayland_greeter_unsupported"},
		"other":   {surface: Desktop, class: "user", uid: 1001, want: "session_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := brokerWaylandFixture(t)
			b.inspectLinux = func(context.Context) (LinuxObservation, error) {
				return LinuxObservation{Session: Session{BootID: "boot", ID: "27", Seat: "seat0", UID: tc.uid, Surface: tc.surface, Backend: "wayland", Active: true}, Class: tc.class}, nil
			}
			err := b.observeLocked(context.Background())
			if err == nil || err.Error() != tc.want || b.observeError != tc.want {
				t.Fatalf("err=%v observeError=%q want=%q", err, b.observeError, tc.want)
			}
			if b.session != (Session{}) {
				t.Fatalf("session survived refusal: %+v", b.session)
			}
		})
	}
}

func TestObserveLockedRequiresX11Registration(t *testing.T) {
	b, _ := brokerWaylandFixture(t)
	b.inspectLinux = func(context.Context) (LinuxObservation, error) {
		return LinuxObservation{Session: Session{BootID: "boot", ID: "c1", Seat: "seat0", UID: 959, Surface: Greeter, Backend: "x11", Active: true}, Class: "greeter", Display: ":0"}, nil
	}
	if err := b.observeLocked(context.Background()); err == nil || err.Error() != "session_unavailable" {
		t.Fatalf("x11 without registration must fail closed, got %v", err)
	}
}
