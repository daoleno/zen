package host

import (
	"context"
	"errors"
)

// OwnerSession is the enrolled owner's active, unlocked desktop on the
// configured seat, resolved from logind and the owner's protected runtime
// directory. An operator may start the Zen desktop host over SSH; the host
// binary must still attach to that same desktop session instead of creating a
// replacement X11/xrdp session or guessing a DISPLAY from the caller
// environment.
type OwnerSession struct {
	UID        uint32 `json:"uid"`
	ID         string `json:"id"`
	Seat       string `json:"seat"`
	Backend    string `json:"backend"`
	Display    string `json:"display"`
	BusAddress string `json:"-"`
	RuntimeDir string `json:"-"`
}

// inspectOwnerSession and discoverOwnerWaylandTarget are injectable so the
// session decision is unit-testable without a live logind or compositor.
var (
	inspectOwnerSession        = InspectLinux
	discoverOwnerWaylandTarget = discoverWaylandTarget
)

// DiscoverOwnerSession validates the current active seat0 session against the
// enrolled owner. It fails closed for a greeter, a lock surface, a foreign
// session, or an ambiguous/missing compositor; it never touches the caller's
// DISPLAY/WAYLAND_DISPLAY.
func DiscoverOwnerSession(ctx context.Context, ownerUID uint32) (OwnerSession, error) {
	if ownerUID == 0 {
		return OwnerSession{}, errors.New("owner_session_requires_user")
	}
	observation, err := inspectOwnerSession(ctx)
	if err != nil {
		return OwnerSession{}, err
	}
	session := observation.Session
	if observation.Class != "user" || session.UID != ownerUID {
		return OwnerSession{}, errors.New("owner_session_unavailable")
	}
	if !session.Active || session.Seat != "seat0" {
		return OwnerSession{}, errors.New("owner_session_inactive")
	}
	if session.Surface != Desktop {
		return OwnerSession{}, errors.New("owner_session_locked")
	}
	switch session.Backend {
	case "wayland":
		target, err := discoverOwnerWaylandTarget(ownerUID)
		if err != nil {
			return OwnerSession{}, err
		}
		return OwnerSession{
			UID:        ownerUID,
			ID:         session.ID,
			Seat:       session.Seat,
			Backend:    "wayland",
			Display:    target.Display,
			BusAddress: target.BusAddress,
			RuntimeDir: target.RuntimeDir,
		}, nil
	case "x11":
		if !xDisplay.MatchString(observation.Display) {
			return OwnerSession{}, errors.New("owner_session_display_invalid")
		}
		return OwnerSession{
			UID:     ownerUID,
			ID:      session.ID,
			Seat:    session.Seat,
			Backend: "x11",
			Display: observation.Display,
		}, nil
	default:
		return OwnerSession{}, errors.New("owner_session_backend_unsupported")
	}
}

// Environment is the exact session environment the supervised host process
// needs to attach to the same desktop. Entries are appended after the daemon
// environment so they win over an unrelated SSH login environment.
func (s OwnerSession) Environment() []string {
	switch s.Backend {
	case "wayland":
		env := []string{
			"XDG_SESSION_TYPE=wayland",
			"WAYLAND_DISPLAY=" + s.Display,
			"DBUS_SESSION_BUS_ADDRESS=" + s.BusAddress,
		}
		if s.RuntimeDir != "" {
			env = append([]string{"XDG_RUNTIME_DIR=" + s.RuntimeDir}, env...)
		}
		return env
	case "x11":
		return []string{"XDG_SESSION_TYPE=x11", "DISPLAY=" + s.Display}
	default:
		return nil
	}
}
