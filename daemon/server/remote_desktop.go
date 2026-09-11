package server

import (
	"net/http"
	"time"

	"github.com/daoleno/zen/daemon/desktop/host"
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
	mode := r.Header.Get("X-Zen-Desktop-Mode")
	if mode != "" && mode != "unattended" && mode != "attended" {
		http.Error(w, "invalid_desktop_mode", http.StatusBadRequest)
		return
	}
	if mode != "attended" {
		if !actualRequestTLS(r) {
			http.Error(w, "desktop_tls_required", http.StatusForbidden)
			return
		}
		if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) {
			http.Error(w, "desktop_scope_required", http.StatusForbidden)
			return
		}
		readiness := inspectHostReadiness()
		if !readiness.Broker && !readiness.CurrentSession {
			http.Error(w, "host_setup_required", http.StatusForbidden)
			return
		}
	}
	u := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if mode == "attended" {
		s.desktop.Serve(conn, device.ID, device.Name, func() bool { return s.auth.IsDeviceTrusted(device.ID) })
	} else {
		readiness := inspectHostReadiness()
		if readiness.Broker {
			s.desktop.ServeExternal(conn, device.ID, func(bind func(func())) {
				host.ServeUnattended(conn, s.auth, device, actualRequestTLS(r), bind)
			})
			return
		}
		s.desktop.ServePairedSession(conn, device.ID, device.Name, func() bool { return s.auth.IsDeviceTrusted(device.ID) })
	}
}
