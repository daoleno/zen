package desktop

import (
	"reflect"
	"testing"
)

func TestConfiguredSourceRequiresExplicitHostSelection(t *testing.T) {
	t.Setenv("ZEN_DESKTOP_BACKEND", "")
	t.Setenv("ZEN_DESKTOP_DISPLAY", ":owned-fixture")
	t.Setenv("ZEN_DESKTOP_BUS_ADDRESS", "")
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
