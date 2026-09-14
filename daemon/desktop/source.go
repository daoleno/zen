package desktop

import (
	"errors"
	"os"
	"strings"
)

// SourceInfo is the selected host display contract advertised to the phone.
type SourceInfo struct {
	Backend string
	Name    string
	Mode    string
	Reason  string
	Args    []string
}

// ConfiguredSource describes this Zen process's current display. It never
// hunts another user's DISPLAY. A logged-in Wayland session wins over the
// Xwayland DISPLAY because the product route is the KDE portal/PipeWire path.
func ConfiguredSource() (SourceInfo, error) {
	backend := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_BACKEND"))
	display := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_DISPLAY"))
	bus := strings.TrimSpace(os.Getenv("ZEN_DESKTOP_BUS_ADDRESS"))
	if display == "" {
		display = strings.TrimSpace(os.Getenv("DISPLAY"))
	}
	if backend == "" {
		switch {
		case strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "":
			backend = "wayland"
			display = strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY"))
		case display != "":
			backend = "x11"
		}
	}
	if backend != "x11" && backend != "wayland" {
		return SourceInfo{}, errors.New("The configured desktop backend is unsupported.")
	}
	if display == "" {
		return SourceInfo{}, errors.New("No current desktop session on this host.")
	}
	info := SourceInfo{Backend: backend, Mode: "current-session", Args: []string{"--display", display}}
	switch backend {
	case "x11":
		info.Name = "Current X11 session"
	case "wayland":
		if bus == "" {
			bus = strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
		}
		if bus == "" {
			return SourceInfo{}, errors.New("No Wayland portal session bus on this host.")
		}
		info.Args = append(info.Args, "--wayland", "--bus-address", bus)
		info.Name = "Current Wayland session"
	}
	return info, nil
}

// configuredSource is retained as the small internal tuple used by the
// helper/legacy tests.
func configuredSource() (string, string, []string, error) {
	info, err := ConfiguredSource()
	if err != nil {
		return "", "", nil, err
	}
	return info.Backend, info.Name, info.Args, nil
}
