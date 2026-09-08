package server

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Server) handleDesktop(w http.ResponseWriter, r *http.Request) {
	// Unlike legacy clients, native desktop never puts credentials in URLs.
	if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Authorization") == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	device, ok := s.authenticateRequest(w, r, "zen-desktop")
	if !ok {
		return
	}
	u := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.desktop.Serve(conn, device.ID, device.Name, func() bool { return s.auth.IsDeviceTrusted(device.ID) })
}
