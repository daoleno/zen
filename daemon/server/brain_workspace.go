package server

import "github.com/gorilla/websocket"

func (s *Server) handleBrainWorkspaceTree(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	tree, err := s.brain.WorkspaceTree(raw.Path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_workspace_tree_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":           "brain_workspace_tree",
		"request_id":     raw.RequestID,
		"workspace_tree": tree,
	})
}

func (s *Server) handleBrainWorkspaceFile(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	file, err := s.brain.ReadWorkspaceFile(raw.Path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_workspace_file_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_workspace_file",
		"request_id": raw.RequestID,
		"file":       file,
	})
}
