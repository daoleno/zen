package host

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestOnlyCurrentSeatAndSessionSignalsRetire(t *testing.T) {
	const current = "/org/freedesktop/login1/session/_32"
	property := func(path, name string) *dbus.Signal {
		return &dbus.Signal{Path: dbus.ObjectPath(path), Name: "org.freedesktop.DBus.Properties.PropertiesChanged", Body: []any{"org.freedesktop.login1.Session", map[string]dbus.Variant{name: dbus.MakeVariant(true)}, []string{}}}
	}
	for _, tc := range []struct {
		name   string
		signal *dbus.Signal
		retire bool
	}{
		{"lock", property(current, "LockedHint"), true},
		{"inactive", property(current, "Active"), true},
		{"seat-switch", property("/org/freedesktop/login1/seat/seat0", "ActiveSession"), true},
		{"idle-only", property(current, "IdleHint"), false},
		{"other-session", property("/org/freedesktop/login1/session/_33", "State"), false},
		{"other-seat", property("/org/freedesktop/login1/seat/seat1", "ActiveSession"), false},
		{"ssh-start", &dbus.Signal{Name: "org.freedesktop.login1.Manager.SessionNew", Body: []any{"3", dbus.ObjectPath("/org/freedesktop/login1/session/_33")}}, false},
		{"current-removed", &dbus.Signal{Name: "org.freedesktop.login1.Manager.SessionRemoved", Body: []any{"2"}}, true},
		{"other-removed", &dbus.Signal{Name: "org.freedesktop.login1.Manager.SessionRemoved", Body: []any{"3"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if sessionSignal(tc.signal, current, "2") != tc.retire {
				t.Fatal("wrong retirement decision")
			}
		})
	}
	invalidated := property(current, "IdleHint")
	invalidated.Body[2] = []string{"LockedHint"}
	if !sessionSignal(invalidated, current, "2") {
		t.Fatal("invalidated session property did not retire")
	}
	invalidated.Body = nil
	if !sessionSignal(invalidated, current, "2") {
		t.Fatal("malformed current-session signal did not fail closed")
	}
}
