package host

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

func TestMediaCannotCrossRetirement(t *testing.T) {
	for _, action := range []string{"revoke", "generation", "owner", "trust", "scope", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			g, r, s := fixture(t)
			a := &testAgent{}
			lease, err := g.Open(r, nil, a, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			writes := 0
			write := func(byte, []byte) error { writes++; return nil }
			if err := g.Forward(lease, 2, []byte{1}, write); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "revoke":
				g.Revoke(r.DeviceID)
			case "generation":
				s.DisplayGeneration++
				g.Observe(s)
			case "owner":
				s.UID++
				g.Observe(s)
			case "trust":
				g.trusted = func(string, [32]byte) bool { return false }
			case "scope":
				g.scoped = func(string, [32]byte) bool { return false }
			case "disconnect":
				g.Disconnect(lease)
			}
			if g.Forward(lease, 2, []byte{1}, write) == nil || writes != 1 || !a.closed {
				t.Fatal("retired media reached writer")
			}
		})
	}
}

func TestMediaRevocationWaitsForBoundedWriter(t *testing.T) {
	g, r, _ := fixture(t)
	a := &testAgent{}
	lease, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	written := make(chan error, 1)
	go func() {
		written <- g.Forward(lease, 2, []byte{1}, func(byte, []byte) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	go func() { g.Revoke(r.DeviceID); close(done) }()
	select {
	case <-done:
		t.Fatal("revocation crossed active media writer")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	<-done
	if !a.closed {
		t.Fatal("agent survived revocation")
	}
}

func TestInvalidMediaAndWriterFailureRetireAgent(t *testing.T) {
	for _, tc := range []struct {
		kind byte
		size int
		fail bool
	}{
		{3, 1, false}, {2, 0, false}, {2, desktop.MaxFrame, false}, {1, 8193, false}, {2, 1, true},
	} {
		g, r, _ := fixture(t)
		a := &testAgent{}
		lease, err := g.Open(r, nil, a, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		writes := 0
		err = g.Forward(lease, tc.kind, make([]byte, tc.size), func(byte, []byte) error {
			writes++
			return errors.New("private native detail")
		})
		if err == nil || !a.closed || (!tc.fail && writes != 0) {
			t.Fatal("invalid packet did not retire")
		}
		if tc.fail && err.Error() != "media_failed" {
			t.Fatal("writer error not redacted")
		}
	}
}
