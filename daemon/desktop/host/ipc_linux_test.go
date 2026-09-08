package host

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func localPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*net.UnixConn, 2)
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "test-local-peer")
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		connections[i] = c.(*net.UnixConn)
		t.Cleanup(func() { _ = c.Close() })
		if err := AuthenticateLocalPeer(connections[i], uint32(os.Getuid())); err != nil {
			t.Fatal(err)
		}
	}
	return connections[0], connections[1]
}

func TestLocalCapabilityTransfer(t *testing.T) {
	a, b := localPair(t)
	f, err := os.CreateTemp(t.TempDir(), "capability")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := SendCapability(a, []byte(`{"generation":1}`), f); err != nil {
		t.Fatal(err)
	}
	data, got, err := ReceiveCapability(b, uint32(os.Getuid()), true)
	if err != nil || got == nil || !bytes.Equal(data, []byte(`{"generation":1}`)) {
		t.Fatalf("transfer failed: %v", err)
	}
	defer got.Close()
	before, _ := f.Stat()
	after, _ := got.Stat()
	if !os.SameFile(before, after) {
		t.Fatal("descriptor identity changed")
	}
	flags, err := unix.FcntlInt(got.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("received descriptor is inheritable")
	}
}

func TestLocalCapabilityRejectsWrongPeer(t *testing.T) {
	a, b := localPair(t)
	wrong := uint32(os.Getuid()) + 1
	if AuthenticateLocalPeer(a, wrong) == nil {
		t.Fatal("accepted wrong kernel peer")
	}
	if err := SendCapability(a, []byte("metadata"), nil); err != nil {
		t.Fatal(err)
	}
	if _, f, err := ReceiveCapability(b, wrong, false); err == nil || f != nil {
		t.Fatal("accepted wrong message sender")
	}
}

func TestLocalCapabilityRejectsMalformedMessagesWithoutFDLeaks(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		fds  int
		want bool
	}{
		{"empty", 0, 0, false},
		{"oversized", maxCapabilityMessage + 1, 1, true},
		{"missing", 1, 0, true},
		{"unexpected", 1, 1, false},
		{"multiple", 1, 2, true},
		{"truncated-rights", 1, 32, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := localPair(t)
			f, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			rights := make([]int, tc.fds)
			for i := range rights {
				rights[i] = int(f.Fd())
			}
			var oob []byte
			if len(rights) > 0 {
				oob = unix.UnixRights(rights...)
			}
			before, _ := os.ReadDir("/proc/self/fd")
			if _, _, err := a.WriteMsgUnix(make([]byte, tc.size), oob, nil); err != nil {
				t.Fatal(err)
			}
			if _, got, err := ReceiveCapability(b, uint32(os.Getuid()), tc.want); err == nil || got != nil {
				t.Fatal("accepted malformed capability")
			}
			after, _ := os.ReadDir("/proc/self/fd")
			if len(after) != len(before) {
				t.Fatalf("descriptor leak: before=%d after=%d", len(before), len(after))
			}
		})
	}
}

func TestRootFileRejectsUnownedAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", "/", file, link, "/dev/null", "/etc/../etc/passwd"} {
		if f, err := OpenRootFile(path, 0600); err == nil {
			f.Close()
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
}
