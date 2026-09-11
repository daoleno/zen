package host

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// Account scope is the default when no ownerUnit is configured: the kernel peer
// must carry the enrolled UID. The broker socket is owner-only, and the
// signed challenge/generation/device/scope checks still apply on top.
func TestVerifyCanonicalAccount(t *testing.T) {
	if !verifyCanonicalAccount(4242, 1000, 1000) {
		t.Fatal("enrolled owner account rejected")
	}
	cases := map[string]struct {
		pid      int32
		peerUid  uint32
		ownerUID uint32
	}{
		"different uid":  {4242, 1001, 1000},
		"zero pid":       {0, 1000, 1000},
		"negative pid":   {-1, 1000, 1000},
		"zero owner uid": {4242, 1000, 0},
		"root uid":       {4242, 0, 1000},
	}
	for name, tc := range cases {
		if verifyCanonicalAccount(tc.pid, tc.peerUid, tc.ownerUID) {
			t.Fatalf("%s accepted", name)
		}
	}
	// Root is never an accepted owner UID even when the peer matches it.
	if verifyCanonicalAccount(4242, 0, 0) {
		t.Fatal("root account accepted")
	}
}

// Real Unix peer credentials, not a mock: the accepted socket must report this
// process's kernel PID and UID.
func TestPeerCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientDone := make(chan error, 1)
	go func() {
		client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			clientDone <- err
			return
		}
		defer client.Close()
		_, _ = io.Copy(io.Discard, client)
		clientDone <- nil
	}()
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	pid, uid, ok := peerCredentials(peer)
	if !ok {
		t.Fatal("peer credentials unavailable")
	}
	if pid != int32(os.Getpid()) || uid != uint32(os.Getuid()) {
		t.Fatalf("peer credentials = pid %d uid %d, want pid %d uid %d", pid, uid, os.Getpid(), os.Getuid())
	}
	_ = peer.Close()
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
}
