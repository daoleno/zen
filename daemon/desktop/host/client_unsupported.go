//go:build !linux

package host

import (
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/gorilla/websocket"
)

func ServeUnattended(conn *websocket.Conn, _ *auth.Manager, _ *auth.TrustedDevice, _ bool, _ func(func())) {
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = conn.WriteJSON(map[string]string{"state": "unsupported", "reason": "Unattended hosting is not implemented on this platform."})
}
