package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

// validateWaylandOwnerSession requires an active, unlocked, owner Wayland
// desktop on the configured seat. Greeter and lock surfaces stay excluded from
// the host contract exactly like broker admission, so a locked session is an
// explicit capability result rather than a misleading ready state.
func validateWaylandOwnerSession(observation LinuxObservation, config HostConfig) error {
	s := observation.Session
	if !s.Active || s.Seat != config.Seat || s.Backend != "wayland" || s.ID == "" || s.BootID == "" {
		return errors.New("active_seat0_wayland_required")
	}
	if observation.Class != "user" || s.UID != config.OwnerUID {
		return errors.New("active_session_is_not_enrolled_owner")
	}
	if s.Surface != Desktop {
		return errors.New("wayland_session_locked")
	}
	return nil
}

// waylandHostQualification is the owner-side precheck for --init-config. It
// discovers the one running compositor and confirms the logged-in session
// exports the portal interfaces; it never creates a portal session.
var waylandHostQualification = func(uid uint32) error {
	target, err := discoverWaylandTarget(uid)
	if err != nil {
		return err
	}
	return verifyWaylandPortal(target.BusAddress)
}

// verifyWaylandPortal performs a read-only version query on the owner session
// bus. It creates no session, shows no dialog and captures no frame. The
// portal is not started or restarted by Zen.
var verifyWaylandPortal = func(busAddress string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dbus.Connect(busAddress, dbus.WithContext(ctx))
	if err != nil {
		return errors.New("the Wayland session bus is unreachable")
	}
	defer conn.Close()
	for _, iface := range []string{"org.freedesktop.portal.RemoteDesktop", "org.freedesktop.portal.ScreenCast"} {
		version, err := portalInterfaceVersion(ctx, conn, iface)
		if err != nil || version == 0 {
			return fmt.Errorf("xdg-desktop-portal does not offer %s in this session", iface)
		}
	}
	return nil
}

func portalInterfaceVersion(ctx context.Context, conn *dbus.Conn, iface string) (uint32, error) {
	var variant dbus.Variant
	err := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, iface, "version").Store(&variant)
	if err != nil {
		return 0, err
	}
	version, ok := variant.Value().(uint32)
	if !ok {
		return 0, errors.New("invalid portal version")
	}
	return version, nil
}

// waylandRegistrationCheck is the Wayland counterpart of the X11 current
// display registration. Wayland admission discovers the compositor on every
// observation, so qualification verifies the owner desktop and the single
// running compositor instead of writing a display registration. Consent is
// requested later, when the paired phone connects.
var waylandRegistrationCheck = func(config HostConfig) (probeResult, error) {
	observation, err := InspectLinux(context.Background())
	if err != nil {
		return probeResult{Error: "registered_session_unavailable"}, err
	}
	if err := validateWaylandOwnerSession(observation, config); err != nil {
		return probeResult{Error: "registered_session_unavailable"}, err
	}
	target, err := discoverWaylandTarget(config.OwnerUID)
	if err != nil {
		return probeResult{Error: err.Error()}, err
	}
	_ = target
	return probeResult{OK: true, Session: observation.Session.ID, Surface: observation.Session.Surface}, nil
}

// installAndRegisterWaylandCurrent installs and activates the same broker unit
// as X11 while the current session stays Wayland. SDDM X11 hooks remain
// installed so an existing X11 greeter/login path is not broken.
func installAndRegisterWaylandCurrent(config HostConfig, source string, out io.Writer, verbose bool) error {
	preflight, err := waylandRegistrationCheck(config)
	if err != nil {
		return fmt.Errorf("desktop setup preflight: %w (no installation applied)", err)
	}
	lock, err := lockInstaller()
	if err != nil {
		return err
	}
	defer lock.Close()
	_, err = os.Lstat(installJournalPath)
	existing := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	installOp, activateOp, rollbackOp := currentInstallSharedOps(config, source, existing)
	result, err := runCurrentInstall(existing, currentInstallOps{
		install:  installOp,
		activate: activateOp,
		register: func() error {
			_, err := waylandRegistrationCheck(config)
			return err
		},
		probe: func() (probeResult, error) {
			checked, err := waylandRegistrationCheck(config)
			if err != nil {
				return checked, err
			}
			if checked.Session != preflight.Session {
				return probeResult{}, errors.New("broker_probe_session_mismatch")
			}
			return checked, nil
		},
		rollback: rollbackOp,
	})
	if err != nil {
		return err
	}
	writeCurrentInstallSuccess(out, result, result.Session, true, verbose)
	return nil
}
