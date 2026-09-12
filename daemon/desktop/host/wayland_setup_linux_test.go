package host

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateWaylandOwnerSession(t *testing.T) {
	config := HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"}
	base := func() LinuxObservation {
		return LinuxObservation{Session: Session{BootID: "boot", ID: "27", Seat: "seat0", UID: 1000,
			Backend: "wayland", Surface: Desktop, Active: true}, Class: "user"}
	}
	if err := validateWaylandOwnerSession(base(), config); err != nil {
		t.Fatalf("owner unlocked desktop refused: %v", err)
	}
	for name, change := range map[string]func(*LinuxObservation){
		"locked":   func(o *LinuxObservation) { o.Session.Surface = Locked },
		"greeter":  func(o *LinuxObservation) { o.Session.Surface = Greeter; o.Class = "greeter"; o.Session.UID = 959 },
		"foreign":  func(o *LinuxObservation) { o.Session.UID = 1001 },
		"x11":      func(o *LinuxObservation) { o.Session.Backend = "x11" },
		"inactive": func(o *LinuxObservation) { o.Session.Active = false },
		"other":    func(o *LinuxObservation) { o.Session.Seat = "seat1" },
	} {
		observation := base()
		change(&observation)
		if validateWaylandOwnerSession(observation, config) == nil {
			t.Fatalf("%s session admitted", name)
		}
	}
}

func TestPortalRestoreTokenPathStaysInOwnerState(t *testing.T) {
	if got := portalRestoreTokenPath("/home/owner"); got != "/home/owner/.zen/desktop-portal-restore-token" {
		t.Fatalf("path=%q", got)
	}
	for _, home := range []string{"", "relative/home", "./home"} {
		if portalRestoreTokenPath(home) != "" {
			t.Fatalf("unsafe home %q produced a token path", home)
		}
	}
}

func TestWaylandInstallSuccessNamesPortalConsent(t *testing.T) {
	var out bytes.Buffer
	writeCurrentInstallSuccess(&out, probeResult{Surface: Desktop, Session: "27"}, "27", true, true)
	text := strings.ToLower(out.String())
	for _, want := range []string{"wayland", "portal", "consent", "rollback"} {
		if !strings.Contains(text, want) {
			t.Fatalf("verbose Wayland success missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(text, "h.264") || strings.Contains(text, "xtest") {
		t.Fatalf("Wayland success claims an X11 probe: %q", out.String())
	}
}
