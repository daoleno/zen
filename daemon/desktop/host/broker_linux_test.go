package host

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func brokerTestConn(t *testing.T) *net.UnixConn {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(fds[1]), "broker-test-peer")
	conn, err := net.FileConn(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn.(*net.UnixConn)
}

// A new admission must proceed once the previous owner retires within the
// bounded wait (the real reconnect case where retirement takes ~2s).
func TestWaitOwnerClearRetiresWithinDeadline(t *testing.T) {
	b := &broker{}
	b.owner = brokerTestConn(t)
	b.ownerDone = make(chan struct{})
	go func() {
		time.Sleep(2 * time.Second)
		b.mu.Lock()
		b.retireLocked()
		b.mu.Unlock()
	}()
	b.mu.Lock()
	ok := b.waitOwnerClear(context.Background(), time.Now().Add(ownerRetirementWait))
	b.mu.Unlock()
	if !ok {
		t.Fatal("admission did not proceed after the owner retired")
	}
}

// Cancellation must abort immediately without proceeding or spinning.
func TestWaitOwnerClearCancelsPromptly(t *testing.T) {
	b := &broker{}
	b.owner = brokerTestConn(t)
	b.ownerDone = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	b.mu.Lock()
	start := time.Now()
	ok := b.waitOwnerClear(ctx, time.Now().Add(ownerRetirementWait))
	elapsed := time.Since(start)
	b.mu.Unlock()
	if ok {
		t.Fatal("admission proceeded after cancellation")
	}
	if elapsed > time.Second {
		t.Fatalf("cancellation took %s, expected a prompt abort", elapsed)
	}
}

// A still-active owner past the deadline must fail closed, not spin.
func TestWaitOwnerClearDeadlineFailsClosed(t *testing.T) {
	b := &broker{}
	b.owner = brokerTestConn(t)
	b.ownerDone = make(chan struct{})
	b.mu.Lock()
	start := time.Now()
	ok := b.waitOwnerClear(context.Background(), time.Now().Add(300*time.Millisecond))
	elapsed := time.Since(start)
	b.mu.Unlock()
	if ok {
		t.Fatal("admission proceeded with an active owner")
	}
	if elapsed > time.Second {
		t.Fatalf("deadline wait took %s, expected ~300ms", elapsed)
	}
}

// Each admission charges the host quota exactly once, and the window resets.
func TestChargeAdmissionOncePerAttempt(t *testing.T) {
	b := &broker{}
	now := time.Now()
	for i := 1; i <= 10; i++ {
		if !b.chargeAdmission(now) {
			t.Fatalf("attempt %d rejected before the limit", i)
		}
	}
	if b.attempts != 10 {
		t.Fatalf("attempts=%d, want 10", b.attempts)
	}
	if b.chargeAdmission(now) {
		t.Fatal("eleventh attempt was not rejected")
	}
	b.window = now.Add(-time.Minute - time.Second)
	if !b.chargeAdmission(now) {
		t.Fatal("window did not reset")
	}
	if b.attempts != 1 {
		t.Fatalf("attempts=%d after reset, want 1", b.attempts)
	}
}
