package server

import "github.com/gorilla/websocket"

func (s *Server) handleServiceTunnel(conn *websocket.Conn, raw clientMessage) {
	if s.watcher == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "service_tunnel_failed", "Service discovery is unavailable")
		return
	}
	state, err := s.watcher.ServiceTunnelAction(raw.ServiceID, raw.ServiceGeneration, raw.TunnelAction)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "service_tunnel_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{"type": "service_tunnel", "request_id": raw.RequestID, "service_id": raw.ServiceID, "tunnel": state})
}
