package desktop

import (
	"reflect"
	"testing"
)

func TestConfiguredSourceAutoDetectsCurrentSession(t *testing.T) {
	t.Setenv("ZEN_DESKTOP_BACKEND", "")
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("ZEN_DESKTOP_BUS_ADDRESS", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":42")
	id, name, args, err := configuredSource()
	if err != nil || id != "x11" || name != "Current X11 session" || !reflect.DeepEqual(args, []string{"--display", ":42"}) {
		t.Fatal(id, name, args, err)
	}
	t.Setenv("DISPLAY", "")
	if _, _, _, err := configuredSource(); err == nil {
		t.Fatal("empty session was treated as configured")
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/owned-fixture-never-opened")
	id, name, args, err = configuredSource()
	if err != nil || id != "wayland" || name != "Current Wayland session" || !reflect.DeepEqual(args, []string{"--display", "wayland-0", "--wayland", "--bus-address", "unix:path=/owned-fixture-never-opened"}) {
		t.Fatal(id, name, args, err)
	}
}

func TestConfiguredSourceRequiresExplicitHostSelection(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("ZEN_DESKTOP_BACKEND", "")
	t.Setenv("ZEN_DESKTOP_DISPLAY", ":owned-fixture")
	t.Setenv("ZEN_DESKTOP_BUS_ADDRESS", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	id, _, args, err := configuredSource()
	if err != nil || id != "x11" || !reflect.DeepEqual(args, []string{"--display", ":owned-fixture"}) {
		t.Fatal(id, args, err)
	}
	t.Setenv("ZEN_DESKTOP_BACKEND", "wayland")
	if _, _, _, err := configuredSource(); err == nil {
		t.Fatal("Wayland inherited an unspecified bus")
	}
	t.Setenv("ZEN_DESKTOP_BUS_ADDRESS", "unix:path=/owned/fixture/bus")
	id, _, args, err = configuredSource()
	if err != nil || id != "wayland" || !reflect.DeepEqual(args, []string{"--display", ":owned-fixture", "--wayland", "--bus-address", "unix:path=/owned/fixture/bus"}) {
		t.Fatal(id, args, err)
	}
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	if _, _, _, err := configuredSource(); err == nil {
		t.Fatal("Wayland inherited an unspecified compositor")
	}
	t.Setenv("ZEN_DESKTOP_BACKEND", "headless")
	if _, _, _, err := configuredSource(); err == nil {
		t.Fatal("unsupported backend silently fell back")
	}
}
