package host

import (
	"net"
	"os"
	"strings"
	"time"
)

func InspectReadiness() Readiness {
	result := Readiness{
		Status:  ReadinessSetupRequired,
		Surface: string(Unavailable),
		Session: "unavailable",
	}
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: OwnerSocket, Net: "unixpacket"})
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
		_ = conn.Close()
		result.Broker = true
		result.Status = ReadinessReady
		result.Session = "available"
	}
	if currentSessionAvailable() {
		result.CurrentSession = true
		if !result.Broker {
			result.Status = ReadinessSession
			result.Surface = string(Desktop)
			result.Session = "current"
		}
	}
	return result
}

func currentSessionAvailable() bool {
	if strings.TrimSpace(os.Getenv("ZEN_DESKTOP_DISPLAY")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("DISPLAY")) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "" &&
		strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS")) != ""
}
