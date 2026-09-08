package desktop

import (
	"errors"
	"os"
)

// Source selection belongs to the host; never discover another login/session.
func configuredSource() (string, string, []string, error) {
	backend := os.Getenv("ZEN_DESKTOP_BACKEND")
	if backend == "" {
		backend = "x11"
	}
	if backend != "x11" && backend != "wayland" {
		return "", "", nil, errors.New("The configured desktop backend is unsupported.")
	}
	display := os.Getenv("ZEN_DESKTOP_DISPLAY")
	if display == "" {
		return "", "", nil, errors.New("No desktop display selected on this host.")
	}
	args := []string{"--display", display}
	name := "Selected X11 desktop"
	if backend == "wayland" {
		bus := os.Getenv("ZEN_DESKTOP_BUS_ADDRESS")
		if bus == "" {
			return "", "", nil, errors.New("No Wayland portal session bus configured on this host.")
		}
		args = append(args, "--wayland", "--bus-address", bus)
		name = "Wayland portal desktop"
	}
	return backend, name, args, nil
}
