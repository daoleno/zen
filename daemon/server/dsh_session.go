package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

type dshModels struct {
	Current struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Effort   string `json:"reasoningEffort"`
	} `json:"current"`
	Routable bool `json:"routable"`
	Groups   []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Models []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Reasoning struct {
				Default string `json:"defaultEffort"`
				Efforts []struct {
					ID string `json:"id"`
				} `json:"efforts"`
			} `json:"reasoning"`
		} `json:"models"`
	} `json:"groups"`
}

func (s *Server) dshSessionID(workerID string) string {
	worker := s.lookupWorker(workerID)
	if worker == nil || s.structuredProviderForWorker(worker) != work.WorkerProviderDSH {
		return ""
	}
	return work.DSHSessionID(worker.Command)
}

func (s *Server) handleDSHThreadRuntime(conn *websocket.Conn, raw clientMessage, mutation bool) bool {
	workerID := raw.WorkerID
	if workerID == "" {
		workerID = raw.SessionID
	}
	id := s.dshSessionID(workerID)
	if id == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if mutation {
		if raw.Runtime == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "profile_invalid", "runtime is required")
			return true
		}
		choice := map[string]any{"provider": raw.Runtime.ConnectionID, "model": raw.Runtime.ModelID}
		if raw.Runtime.Effect != "" && !raw.Runtime.UseDefaultEffect {
			choice["reasoningEffort"] = raw.Runtime.Effect
		}
		if err := work.CallDSH(ctx, id, "session.selectModel", choice, nil); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "runtime_switch_failed", err.Error())
			return true
		}
	}
	var models dshModels
	if err := work.CallDSH(ctx, id, "session.models", map[string]any{}, &models); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "binding_not_found", err.Error())
		return true
	}
	label := models.Current.Provider
	choices := []map[string]any{}
	var efforts []string
	defaultEffort := ""
	for _, group := range models.Groups {
		if group.ID != models.Current.Provider {
			continue
		}
		label = group.Name
		for _, model := range group.Models {
			values := []string{}
			for _, effort := range model.Reasoning.Efforts {
				values = append(values, effort.ID)
			}
			choices = append(choices, map[string]any{"id": model.ID, "display_name": model.Name, "available": true, "source": "dsh_native", "reasoning_efforts": values, "reasoning_effort_default": model.Reasoning.Default})
			if model.ID == models.Current.Model {
				efforts = values
				defaultEffort = model.Reasoning.Default
			}
		}
	}
	selection := map[string]any{"session_id": workerID, "client": "dsh", "connection_id": models.Current.Provider, "connection_name": label, "provider_label": label, "model_id": models.Current.Model, "reasoning_effort": models.Current.Effort, "reasoning_efforts": efforts, "reasoning_effort_default": defaultEffort, "credential_ready": models.Routable, "hot_switchable": true, "native_models": choices}
	messageType := "thread_runtime"
	if mutation {
		messageType = "thread_runtime_set"
	}
	// Native acknowledgement applies the selection. Native event persistence is
	// asynchronous, so do not claim a daemon fsync that did not occur.
	payload := map[string]any{"type": messageType, "request_id": raw.RequestID, "worker_id": workerID, "runtime": selection}
	if mutation {
		payload["persistence_outcome"] = "applied"
		payload["persistence_durable"] = false
		payload["persistence_warning"] = "DSH accepted the choice; its native session log saves asynchronously."
	}
	s.sendJSON(conn, payload)
	return true
}

func (s *Server) handleDSHImage(conn *websocket.Conn, raw clientMessage) {
	worker := s.currentSessionFileWorker(raw.WorkerID)
	if err := validateSessionFileIdentity(worker, raw); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_file_changed", err.Error())
		return
	}
	id := work.DSHSessionID(worker.Command)
	attachmentID := strings.TrimPrefix(raw.Path, "dsh-attachment:")
	if id == "" || attachmentID == raw.Path || attachmentID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", "Unsupported image owner")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var image struct {
		Data       string `json:"data"`
		Attachment struct {
			MediaType string `json:"mediaType"`
			Bytes     int64  `json:"bytes"`
			Width     int64  `json:"width"`
			Height    int64  `json:"height"`
		} `json:"attachment"`
	}
	if err := work.CallDSH(ctx, id, "session.attachment", map[string]any{"attachmentId": attachmentID}, &image); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", err.Error())
		return
	}
	if image.Attachment.Bytes > 5<<20 || len(image.Data) > 8<<20 || image.Attachment.Width*image.Attachment.Height > 40_000_000 || !strings.HasPrefix(image.Attachment.MediaType, "image/") {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", "Image exceeds preview limits")
		return
	}
	s.sendJSON(conn, map[string]any{"type": "session_image", "request_id": raw.RequestID, "data_url": "data:" + image.Attachment.MediaType + ";base64," + image.Data})
}

func (s *Server) handleDSHInteraction(conn *websocket.Conn, raw clientMessage) {
	worker := s.currentSessionFileWorker(raw.WorkerID)
	if err := validateSessionFileIdentity(worker, raw); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_changed", err.Error())
		return
	}
	id := work.DSHSessionID(worker.Command)
	if id == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "unsupported_session", "DSH Session required")
		return
	}
	method := "session.interactions"
	payload := map[string]any{}
	if len(raw.DSHAnswer) > 0 {
		method = "session.respond"
		if json.Unmarshal(raw.DSHAnswer, &payload) != nil || payload == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_answer", "Invalid answer")
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var result json.RawMessage
	if err := work.CallDSH(ctx, id, method, payload, &result); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "dsh_interaction_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{"type": "dsh_interaction", "request_id": raw.RequestID, "result": result})
}
