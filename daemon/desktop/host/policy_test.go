package host

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

type testAgent struct {
	writes int
	closed bool
	err    error
}

func (a *testAgent) Write(desktop.Command) error { a.writes++; return a.err }
func (a *testAgent) Close() error                { a.closed = true; return nil }

func fixture(t *testing.T) (*Gate, Request, Session) {
	t.Helper()
	e := HostConfig{Version: 1, HostID: "host", OwnerUID: 1000, Seat: "seat0"}
	trust := func(id string, key [32]byte) bool { return id == "phone" && key == [32]byte{1} }
	g, err := NewGate(e, trust, trust)
	if err != nil {
		t.Fatal(err)
	}
	s := Session{BootID: "boot", ID: "user-session", Seat: "seat0", UID: 1000, Surface: Desktop, Backend: "x11", DisplayGeneration: 1, Active: true, Ready: true, Control: true}
	r := Request{HostID: "host", DeviceID: "phone", Fingerprint: [32]byte{1}, ConnectionID: "connection", Generation: g.Observe(s), Mode: Unattended, Control: true, TLS: true}
	return g, r, s
}

func TestUnattendedAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Gate, *Request, *Session)
		allowed bool
	}{
		{"desktop", func(*Gate, *Request, *Session) {}, true},
		{"paired-default", func(_ *Gate, r *Request, _ *Session) { r.Mode = "" }, true},
		{"locked", func(_ *Gate, _ *Request, s *Session) { s.Surface = Locked }, true},
		{"greeter", func(_ *Gate, _ *Request, s *Session) { s.Surface = Greeter; s.UID = 959 }, true},
		{"plain-even-approved", func(_ *Gate, r *Request, _ *Session) { r.TLS = false; r.LANApproved = true }, false},
		{"other-host", func(_ *Gate, r *Request, _ *Session) { r.HostID = "other" }, false},
		{"other-device", func(_ *Gate, r *Request, _ *Session) { r.DeviceID = "other" }, false},
		{"rotated-key", func(_ *Gate, r *Request, _ *Session) { r.Fingerprint = [32]byte{2} }, false},
		{"legacy-pairing", func(g *Gate, _ *Request, _ *Session) { g.scoped = func(string, [32]byte) bool { return false } }, false},
		{"wrong-uid", func(_ *Gate, _ *Request, s *Session) { s.UID = 1001 }, false},
		{"other-seat", func(_ *Gate, _ *Request, s *Session) { s.Seat = "seat1" }, false},
		{"inactive", func(_ *Gate, _ *Request, s *Session) { s.Active = false }, false},
		{"no-backend-proof", func(_ *Gate, _ *Request, s *Session) { s.Ready = false }, false},
		{"unknown-backend", func(_ *Gate, _ *Request, s *Session) { s.Backend = "unknown" }, false},
		{"portal-greeter", func(_ *Gate, _ *Request, s *Session) { s.Backend = "wayland-portal"; s.Surface = Greeter }, false},
		{"portal-lock", func(_ *Gate, _ *Request, s *Session) { s.Backend = "wayland-portal"; s.Surface = Locked }, false},
		{"preboot", func(_ *Gate, _ *Request, s *Session) { s.Surface = "preboot" }, false},
		{"unknown-mode", func(_ *Gate, r *Request, _ *Session) { r.Mode = "auto" }, false},
		{"view-backend", func(_ *Gate, _ *Request, s *Session) { s.Control = false }, false},
		{"legacy-prelogin", func(g *Gate, _ *Request, s *Session) {
			g.scoped = func(string, [32]byte) bool { return false }
			s.Surface = Greeter
		}, false},
		{"no-lock-grant", func(g *Gate, _ *Request, s *Session) {
			g.scoped = func(string, [32]byte) bool { return false }
			s.Surface = Locked
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, r, s := fixture(t)
			tt.change(g, &r, &s)
			r.Generation = g.Observe(s)
			a := &testAgent{}
			_, err := g.Open(r, nil, a, time.Now())
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v error=%v", tt.allowed, err)
			}
			if a.writes != 0 || a.closed {
				t.Fatal("Open performed input or consumed rejected agent")
			}
		})
	}
}

func TestAttendedConsentBindingAndConsumption(t *testing.T) {
	g, r, s := fixture(t)
	r.Mode, r.TLS, r.LANApproved = Attended, false, true
	c := &Consent{Request: r}
	if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
		t.Fatal("missing consent")
	}
	other := r
	other.ConnectionID = "other"
	if _, err := g.Open(other, c, &testAgent{}, time.Now()); err == nil {
		t.Fatal("consent crossed connection")
	}
	lease, err := g.Open(r, c, &testAgent{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g.Disconnect(lease)
	if _, err := g.Open(r, c, &testAgent{}, time.Now()); err == nil {
		t.Fatal("consent reused")
	}
	s.Surface = Locked
	r.Generation = g.Observe(s)
	if _, err := g.Open(r, &Consent{Request: r}, &testAgent{}, time.Now()); err == nil {
		t.Fatal("attended entered lock screen")
	}
}

func TestSessionChangesRetireBeforeHandoff(t *testing.T) {
	changes := []func(*Session){
		func(s *Session) { s.Surface = Locked },
		func(s *Session) { s.Surface = Greeter; s.ID = "greeter"; s.UID = 959 },
		func(s *Session) { s.ID = "other"; s.UID = 1001 },
		func(s *Session) { s.BootID = "next-boot" },
		func(s *Session) { s.DisplayGeneration++ },
		func(s *Session) { s.Active = false },
		func(s *Session) { s.Ready = false },
	}
	for i, change := range changes {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			g, r, s := fixture(t)
			a := &testAgent{}
			lease, err := g.Open(r, nil, a, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if g.Observe(s) != r.Generation || a.closed {
				t.Fatal("unchanged observation retired")
			}
			change(&s)
			next := g.Observe(s)
			if next == r.Generation || !a.closed {
				t.Fatal("old agent survived transition")
			}
			if g.Input(lease, desktop.Command{Type: "key", Code: 65, Down: true}) == nil {
				t.Fatal("old input accepted")
			}
			if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
				t.Fatal("stale generation accepted")
			}
		})
	}
}

func TestGreeterUserHandoffAndCrossGateLease(t *testing.T) {
	g, r, s := fixture(t)
	s.Surface, s.ID, s.UID = Greeter, "greeter", 959
	r.Generation = g.Observe(s)
	a := &testAgent{}
	old, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.Surface, s.ID, s.UID = Desktop, "new-user", 1000
	r.Generation = g.Observe(s)
	nextAgent := &testAgent{}
	next, err := g.Open(r, nil, nextAgent, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g.Disconnect(old)
	if nextAgent.closed || !a.closed {
		t.Fatal("late disconnect crossed generation")
	}
	g2, r2, _ := fixture(t)
	foreign, err := g2.Open(r2, nil, &testAgent{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if g.Input(foreign, desktop.Command{Type: "release"}) == nil {
		t.Fatal("lease crossed gates")
	}
	if g.Input(next, desktop.Command{Type: "release"}) != nil {
		t.Fatal("new lease unavailable")
	}
}

func TestRevokeDenyAndTrustRecheck(t *testing.T) {
	for _, action := range []string{"revoke", "deny", "untrust", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			g, r, _ := fixture(t)
			a := &testAgent{}
			trusted := true
			g.trusted = func(string, [32]byte) bool { return trusted }
			lease, err := g.Open(r, nil, a, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "revoke":
				trusted = false
				g.Revoke(r.DeviceID)
			case "deny":
				g.Deny(true)
			case "untrust":
				trusted = false
			case "disconnect":
				g.Disconnect(lease)
			}
			if g.Input(lease, desktop.Command{Type: "release"}) == nil || !a.closed || a.writes != 0 {
				t.Fatal("retired input admitted")
			}
			if action != "disconnect" {
				if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
					t.Fatal("reconnect bypassed retirement")
				}
			}
		})
	}
}

func TestViewOnlyAndRedactedAgentFailure(t *testing.T) {
	g, r, _ := fixture(t)
	r.Control = false
	a := &testAgent{}
	lease, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if g.Input(lease, desktop.Command{Type: "key", Code: 65}) == nil || a.writes != 0 {
		t.Fatal("view-only input")
	}
	g.Disconnect(lease)
	r.Control = true
	a = &testAgent{err: errors.New("synthetic secret must never escape")}
	lease, err = g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = g.Input(lease, desktop.Command{Type: "key", Code: 65})
	if err == nil || err.Error() != "agent_failed" || !a.closed {
		t.Fatalf("unredacted failure: %v", err)
	}
}

type blockingAgent struct {
	entered chan struct{}
	release chan struct{}
	closed  atomic.Bool
}

func (a *blockingAgent) Write(desktop.Command) error { close(a.entered); <-a.release; return nil }
func (a *blockingAgent) Close() error                { a.closed.Store(true); return nil }

func TestRevokeWaitsForAdmittedWrite(t *testing.T) {
	g, r, _ := fixture(t)
	a := &blockingAgent{entered: make(chan struct{}), release: make(chan struct{})}
	lease, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() { written <- g.Input(lease, desktop.Command{Type: "release"}) }()
	<-a.entered
	done := make(chan struct{})
	go func() { g.Revoke(r.DeviceID); close(done) }()
	select {
	case <-done:
		t.Fatal("revocation passed active writer")
	case <-time.After(10 * time.Millisecond):
	}
	close(a.release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	<-done
	if !a.closed.Load() || g.Input(lease, desktop.Command{Type: "release"}) == nil {
		t.Fatal("revocation barrier failed")
	}
}

func TestBoundedRateAndHostConfiguration(t *testing.T) {
	g, r, _ := fixture(t)
	now := time.Now()
	bad := r
	bad.Mode = "unknown"
	for range 10 {
		if _, err := g.Open(bad, nil, &testAgent{}, now); err == nil {
			t.Fatal("bad request admitted")
		}
	}
	if _, err := g.Open(r, nil, &testAgent{}, now); err == nil || err.Error() != "rate_limited" {
		t.Fatal("rate limit missing")
	}
	if _, err := g.Open(r, nil, &testAgent{}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(g.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadHostConfig(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{string(data) + "{}", `{"version":1,"hostId":"host","osPassword":"synthetic"}`, strings.Repeat(" ", 32769), `{"version":2,"hostId":"host"}`} {
		if _, err := ReadHostConfig(strings.NewReader(invalid)); err == nil {
			t.Fatal("invalid enrollment accepted")
		}
	}
	invalidConfig := g.config
	invalidConfig.OwnerUID = 0
	if invalidConfig.Validate() == nil {
		t.Fatal("root desktop owner accepted")
	}
}

type failingCloseAgent struct{ testAgent }

func (a *failingCloseAgent) Close() error {
	a.closed = true
	return errors.New("synthetic cleanup failure")
}

func TestFailedRetirementCannotBeClearedByDenyToggle(t *testing.T) {
	g, r, s := fixture(t)
	a := &failingCloseAgent{}
	lease, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g.Disconnect(lease)
	g.Deny(false)
	s.DisplayGeneration++
	r.Generation = g.Observe(s)
	if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
		t.Fatal("failed cleanup resumed without rebuilding runtime owner")
	}
}
