package desktop

import (
	"errors"
	"os"
	"strings"
)

// Source selection belongs to this zen process's current session. Test overrides
// stay explicit. Never hunt another user's DISPLAY.
func configuredSource() (string, string, []string, error) {
	backend := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_BACKEND"))
	display := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_DISPLAY"))
	bus := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_BUS_ADDRESS"))
	if display == "" {
		display = strings.TrimSpace(os.Getenv("DISPLAY"))
	}
	if backend == "" {
		switch {
		case display != "":
			backend = "x11"
		case strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "":
			backend = "wayland"
			display = strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY"))
		}
	}
	if backend != "x11" && backend != "wayland" {
		return "", "", nil, errors.New("The configured desktop backend is unsupported.")
	}
	if display == "" {
		return "", "", nil, errors.New("No current desktop session on this host.")
	}
	args := []string{"--display", display}
	name := "Current X11 session"
	if backend == "wayland" {
		if bus == "" {
			bus = strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
		}
		if bus == "" {
			return "", "", nil, errors.New("No Wayland portal session bus on this host.")
		}
		args = append(args, "--wayland", "--bus-address", bus)
		name = "Current Wayland session"
	}
	return backend, name, args, nil
}
