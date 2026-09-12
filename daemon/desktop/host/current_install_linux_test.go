package host

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentXSocketUsesOnlyLogindDisplay(t *testing.T) {
	for _, display := range []string{":0", ":91.0", ":234"} {
		got, err := currentXSocket(display)
		if err != nil || !strings.HasPrefix(got, "/tmp/.X11-unix/X") {
			t.Fatal(display, got, err)
		}
	}
	for _, display := range []string{"", "localhost:0", "0", ":0/../../home", ":0\n", ":1;id"} {
		if _, err := currentXSocket(display); err == nil {
			t.Fatalf("accepted %q", display)
		}
	}
}

func TestCurrentXServerAuthorityRejectsAmbiguity(t *testing.T) {
	args := func(parts ...string) []byte { return []byte(strings.Join(parts, "\x00") + "\x00") }
	valid := args("/usr/lib/Xorg", "-displayfd", "16", "-auth", "/run/sddm/xauth-fixture", "-seat", "seat0")
	if got, err := xServerAuthority(valid); err != nil || got != "/run/sddm/xauth-fixture" {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{
		args("Xorg", "-auth", "/run/sddm/a"),
		args("Xorg", "-seat", "seat1", "-auth", "/run/sddm/a"),
		args("Xorg", "-seat", "seat0", "-auth", "relative"),
		args("Xorg", "-seat", "seat0", "-auth", "/run/sddm/a", "-auth", "/run/sddm/b"),
		args("Xorg", "-seat", "seat0", "-seat", "seat0", "-auth", "/run/sddm/a"),
		args("Xorg", "-seat", "seat0", "-auth"),
		[]byte("Xorg"), bytes.Repeat([]byte{'x'}, 65537),
	} {
		if _, err := xServerAuthority(bad); err == nil {
			t.Fatalf("ambiguous argv admitted")
		}
	}
}

func TestCurrentSessionRequiresEnrolledOwnerOrSDDM(t *testing.T) {
	config := HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"}
	greeter := LinuxObservation{Class: "greeter", Session: Session{BootID: "boot", ID: "c1", Seat: "seat0", UID: 959, Backend: "x11", Active: true}}
	if err := validateCurrentSession(greeter, config, 959); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*LinuxObservation){
		func(o *LinuxObservation) { o.Session.UID = 1001 },
		func(o *LinuxObservation) { o.Session.Backend = "wayland" },
		func(o *LinuxObservation) { o.Session.Active = false },
		func(o *LinuxObservation) { o.Session.Seat = "seat1" },
		func(o *LinuxObservation) { o.Class = "user" },
	} {
		copy := greeter
		change(&copy)
		if validateCurrentSession(copy, config, 959) == nil {
			t.Fatal("foreign/unsupported session admitted")
		}
	}
	greeter.Class = "user"
	greeter.Session.UID = 1000
	if validateCurrentSession(greeter, config, 959) != nil {
		t.Fatal("owner X11 denied")
	}
}

func TestCurrentInstallRollsBackOnlyNewChanges(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"", "install", "activate", "register", "probe", "desktop_busy_existing_connection_preserved"} {
			t.Run(strings.Join([]string{map[bool]string{true: "existing", false: "new"}[existing], failure}, "/"), func(t *testing.T) {
				var steps []string
				step := func(name string) error {
					steps = append(steps, name)
					if failure == name {
						return errors.New(name)
					}
					return nil
				}
				_, err := runCurrentInstall(existing, currentInstallOps{
					install: func() error { return step("install") }, activate: func() error { return step("activate") }, register: func() error { return step("register") },
					probe: func() (probeResult, error) {
						if failure == "desktop_busy_existing_connection_preserved" {
							return probeResult{}, errDesktopProbeBusy
						}
						return probeResult{OK: true}, step("probe")
					},
					rollback: func() error { return step("rollback") },
				})
				if (err == nil) != (failure == "") {
					t.Fatal(err)
				}
				wantRollback := !existing && failure != "" && failure != "install" && failure != "desktop_busy_existing_connection_preserved"
				if strings.Contains(strings.Join(steps, ","), "rollback") != wantRollback {
					t.Fatal(steps)
				}
			})
		}
	}
}

func TestCurrentInstallPreservesRollbackFailureEvidence(t *testing.T) {
	_, err := runCurrentInstall(false, currentInstallOps{install: func() error { return nil }, activate: func() error { return errors.New("activation") }, rollback: func() error { return errors.New("administrator_drift") }})
	if err == nil || !strings.Contains(err.Error(), "administrator_drift") {
		t.Fatal(err)
	}
}

func TestProbeRequiresVideoAndCannotEnableInput(t *testing.T) {
	packet := func(kind byte, data []byte) []byte {
		var b bytes.Buffer
		binary.Write(&b, binary.BigEndian, uint32(len(data)+1))
		b.WriteByte(kind)
		b.Write(data)
		return b.Bytes()
	}
	meta := []byte(`{"state":"streaming","codec":"h264","width":1280,"height":720,"control":false}`)
	frame := []byte{0, 0, 0, 1, 0x65, 0x01}
	stream := append(packet(1, meta), packet(2, frame)...)
	if w, h, n, err := readProbeFrame(bytes.NewReader(stream)); err != nil || w != 1280 || h != 720 || n != 6 {
		t.Fatal(w, h, n, err)
	}
	for _, bad := range [][]byte{packet(1, meta), packet(2, frame), append(packet(1, bytes.ReplaceAll(meta, []byte("false"), []byte("true"))), packet(2, frame)...), append(packet(1, meta), packet(2, []byte("not-h264"))...)} {
		if _, _, _, err := readProbeFrame(bytes.NewReader(bad)); err == nil {
			t.Fatal("unverified probe admitted")
		}
	}
}

func TestSameAuthorityRequiresPinnedIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "authority")
	if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Open(p)
	defer a.Close()
	b, _ := os.Open(p)
	defer b.Close()
	if !sameAuthority(a, b) || sameAuthority(a, nil) {
		t.Fatal("descriptor identity mismatch")
	}
	if err := os.Rename(p, p+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	c, _ := os.Open(p)
	defer c.Close()
	if sameAuthority(a, c) {
		t.Fatal("replacement treated as existing registration")
	}
}

func TestRegisterCurrentRequiresExplicitInstallAndActivation(t *testing.T) {
	for _, args := range [][]string{{"--register-current"}, {"--install", "--register-current"}, {"--activate", "--register-current"}, {"--install", "--activate", "--register-current", "--plan"}, {"--install", "--activate", "--register-current", "--register", "start"}} {
		if err := RunLinuxCLI(args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--register-current requires") {
			t.Fatal(args, err)
		}
	}
}

func TestCurrentInstallRefusesForeignUnitOrMask(t *testing.T) {
	p := filepath.Join(t.TempDir(), "zen-desktop-host.service")
	if err := rejectBrokerUnitOverrides(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", p); err != nil {
		t.Fatal(err)
	}
	if err := rejectBrokerUnitOverrides(p); err == nil {
		t.Fatal("unit override or mask accepted")
	}
}
