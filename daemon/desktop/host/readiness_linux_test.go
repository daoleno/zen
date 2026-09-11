package host

import "testing"

func TestInspectReadinessDetectsCurrentSessionWithoutBroker(t *testing.T) {
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("DISPLAY", ":42")
	got := InspectReadiness()
	if !got.CurrentSession {
		t.Fatalf("DISPLAY was not treated as this process session: %#v", got)
	}
	if got.Broker {
		if got.Status != ReadinessReady {
			t.Fatalf("broker present but status=%q", got.Status)
		}
		return
	}
	if got.Status != ReadinessSession || got.Session != "current" {
		t.Fatalf("session-only readiness=%#v", got)
	}
}

func TestInspectReadinessRequiresWaylandBus(t *testing.T) {
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if InspectReadiness().CurrentSession {
		t.Fatal("Wayland without a session bus was treated as ready")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/owned-fixture-never-opened")
	if !InspectReadiness().CurrentSession {
		t.Fatal("Wayland with a session bus was ignored")
	}
}

func TestInspectReadinessEmptySessionIsSetup(t *testing.T) {
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	got := InspectReadiness()
	if got.CurrentSession {
		t.Fatalf("empty process env reported a session: %#v", got)
	}
	if got.Broker {
		return
	}
	if got.Status != ReadinessSetupRequired {
		t.Fatalf("no session and no broker status=%q", got.Status)
	}
}
