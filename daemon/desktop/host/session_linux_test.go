package host

import (
	"context"
	"strings"
	"testing"
)

func stubOwnerSessionInspection(t *testing.T, observation LinuxObservation, err error) {
	t.Helper()
	previousInspect, previousTarget := inspectOwnerSession, discoverOwnerWaylandTarget
	inspectOwnerSession = func(context.Context) (LinuxObservation, error) { return observation, err }
	discoverOwnerWaylandTarget = func(uint32) (waylandTarget, error) {
		return waylandTarget{RuntimeDir: "/run/user/1000", BusAddress: "unix:path=/run/user/1000/bus", Display: "wayland-0"}, nil
	}
	t.Cleanup(func() {
		inspectOwnerSession, discoverOwnerWaylandTarget = previousInspect, previousTarget
	})
}

func ownerWaylandObservation(uid uint32) LinuxObservation {
	return LinuxObservation{
		Session: Session{ID: "2", Seat: "seat0", UID: uid, Backend: "wayland", Surface: Desktop, Active: true, BootID: "boot"},
		Class:   "user",
		Display: "wayland-0",
	}
}

func TestDiscoverOwnerSessionBindsToOwnerDesktop(t *testing.T) {
	stubOwnerSessionInspection(t, ownerWaylandObservation(1000), nil)
	session, err := DiscoverOwnerSession(context.Background(), 1000)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if session.Backend != "wayland" || session.Display != "wayland-0" || session.BusAddress != "unix:path=/run/user/1000/bus" || session.RuntimeDir != "/run/user/1000" {
		t.Fatalf("session = %+v", session)
	}
	env := strings.Join(session.Environment(), " ")
	for _, want := range []string{"XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY=wayland-0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus", "XDG_SESSION_TYPE=wayland"} {
		if !strings.Contains(env, want) {
			t.Fatalf("environment missing %q: %v", want, env)
		}
	}
}

// TestDiscoverOwnerSessionIgnoresCallerEnvironment locks the SSH-started
// contract: the operator's login environment (DISPLAY, WAYLAND_DISPLAY,
// DBUS_SESSION_BUS_ADDRESS, XDG_RUNTIME_DIR, XDG_SESSION_ID) must never leak
// into the owner session resolution or the supervised host environment. Only
// the logind/runtime directory facts for the enrolled owner UID are used.
func TestDiscoverOwnerSessionIgnoresCallerEnvironment(t *testing.T) {
	for name, value := range map[string]string{
		"DISPLAY":                  "attacker:9",
		"WAYLAND_DISPLAY":          "attacker-wayland-9",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/tmp/attacker-bus",
		"XDG_RUNTIME_DIR":          "/tmp/attacker-runtime",
		"XDG_SESSION_ID":           "999",
	} {
		t.Setenv(name, value)
	}
	stubOwnerSessionInspection(t, ownerWaylandObservation(1000), nil)
	session, err := DiscoverOwnerSession(context.Background(), 1000)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if session.Display != "wayland-0" || session.BusAddress != "unix:path=/run/user/1000/bus" || session.RuntimeDir != "/run/user/1000" {
		t.Fatalf("caller environment leaked into owner session: %+v", session)
	}
	env := strings.Join(session.Environment(), " ")
	if strings.Contains(env, "attacker") {
		t.Fatalf("caller environment leaked into session environment: %v", env)
	}
	for _, want := range []string{"XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY=wayland-0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"} {
		if !strings.Contains(env, want) {
			t.Fatalf("environment missing %q: %v", want, env)
		}
	}
}

func TestDiscoverOwnerSessionFailsClosed(t *testing.T) {
	cases := []struct {
		name        string
		observation LinuxObservation
		uid         uint32
		want        string
	}{
		{"foreign-session", ownerWaylandObservation(2000), 1000, "owner_session_unavailable"},
		{"greeter", func() LinuxObservation { o := ownerWaylandObservation(1000); o.Class = "greeter"; return o }(), 1000, "owner_session_unavailable"},
		{"locked", func() LinuxObservation {
			o := ownerWaylandObservation(1000)
			o.Session.Surface = Locked
			return o
		}(), 1000, "owner_session_locked"},
		{"inactive", func() LinuxObservation {
			o := ownerWaylandObservation(1000)
			o.Session.Active = false
			return o
		}(), 1000, "owner_session_inactive"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubOwnerSessionInspection(t, testCase.observation, nil)
			if _, err := DiscoverOwnerSession(context.Background(), testCase.uid); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v want %s", err, testCase.want)
			}
		})
	}
	if _, err := DiscoverOwnerSession(context.Background(), 0); err == nil {
		t.Fatal("root uid accepted")
	}
}

func TestDiscoverOwnerSessionX11(t *testing.T) {
	observation := LinuxObservation{
		Session: Session{ID: "4", Seat: "seat0", UID: 1000, Backend: "x11", Surface: Desktop, Active: true, BootID: "boot"},
		Class:   "user",
		Display: ":0",
	}
	stubOwnerSessionInspection(t, observation, nil)
	session, err := DiscoverOwnerSession(context.Background(), 1000)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if session.Backend != "x11" || session.Display != ":0" {
		t.Fatalf("session = %+v", session)
	}
	if env := strings.Join(session.Environment(), " "); !strings.Contains(env, "DISPLAY=:0") || !strings.Contains(env, "XDG_SESSION_TYPE=x11") {
		t.Fatalf("environment = %v", env)
	}
	observation.Display = "guessed-display"
	stubOwnerSessionInspection(t, observation, nil)
	if _, err := DiscoverOwnerSession(context.Background(), 1000); err == nil || !strings.Contains(err.Error(), "display_invalid") {
		t.Fatalf("invalid display error = %v", err)
	}
}
