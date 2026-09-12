package host

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestReadinessProbeClosesWithoutHello reproduces the broker's
// "desktop admission rejected: hello" source locally: InspectReadiness dials
// OwnerSocket and closes without sending the canonical-owner hello capability,
// so ReceiveCapability sees EOF. No host socket, credentials or auth change.
func TestReadinessProbeClosesWithoutHello(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/owner.sock"
	listener, err := net.Listen("unixpacket", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan error, 1)
	go func() {
		accepted, err := listener.Accept()
		if err != nil {
			received <- err
			return
		}
		defer accepted.Close()
		conn, ok := accepted.(*net.UnixConn)
		if !ok {
			received <- errors.New("unexpected connection type")
			return
		}
		_, _, err = ReceiveCapabilityWithin(conn, uint32(os.Getuid()), false, 500*time.Millisecond)
		received <- err
	}()
	conn, err := net.Dial("unixpacket", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-received:
		if err == nil {
			t.Fatal("a probe that closes without hello must not authenticate")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broker receive timed out")
	}
	// Source contract: the readiness probe never sends the owner capability.
	source, err := os.ReadFile("readiness_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "SendCapability") || strings.Contains(text, `"hello"`) {
		t.Fatal("readiness must not send the owner hello capability")
	}
	if !strings.Contains(text, "conn.Close()") {
		t.Fatal("readiness closes the probe socket without hello")
	}
}
