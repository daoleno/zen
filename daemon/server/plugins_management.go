package server

import (
	"context"
	"errors"

	skillmgmt "github.com/daoleno/zen/daemon/skills"
	"github.com/gorilla/websocket"
)

type pluginsInventoryRequest struct {
	requestID  string
	generation int64
	cancel     context.CancelFunc
}

type pluginsMutationRequest struct {
	requestID string
	cancel    context.CancelFunc
}

type pluginsInventoryResponse struct {
	Type       string                    `json:"type"`
	RequestID  string                    `json:"request_id"`
	Generation int64                     `json:"generation"`
	Inventory  skillmgmt.PluginInventory `json:"inventory"`
}

type pluginCommandResponse struct {
	Type      string                          `json:"type"`
	RequestID string                          `json:"request_id"`
	Command   skillmgmt.PluginMutationCommand `json:"command"`
}

type pluginMutationResultResponse struct {
	Type       string                          `json:"type"`
	RequestID  string                          `json:"request_id"`
	Command    skillmgmt.PluginMutationCommand `json:"command"`
	Success    bool                            `json:"success"`
	ExitCode   int                             `json:"exit_code"`
	Output     string                          `json:"output"`
	DurationMS int64                           `json:"duration_ms"`
}

type pluginsErrorResponse struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Generation int64  `json:"generation,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (s *Server) handlePluginsInventory(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) {
		s.sendPluginsError(conn, "plugins_inventory_error", raw, "invalid_request", "Invalid Plugins inventory request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := pluginsInventoryRequest{requestID: raw.RequestID, generation: raw.Generation, cancel: cancel}
	previous, hadPrevious := s.replacePluginsInventory(conn, next)
	if hadPrevious {
		s.sendJSON(conn, pluginsErrorResponse{
			Type:       "plugins_inventory_error",
			RequestID:  previous.requestID,
			Generation: previous.generation,
			Code:       "superseded",
			Message:    "A newer Plugins inventory request replaced this request.",
		})
	}
	go func() {
		inventory, err := skillmgmt.DiscoverPluginInventory(
			skillmgmt.InventoryOptions{Context: ctx},
			s.pluginCatalogCLI,
		)
		if !s.claimPluginsInventory(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendPluginsError(conn, "plugins_inventory_error", raw, "plugins_inventory_failed", err.Error())
			return
		}
		s.sendJSON(conn, pluginsInventoryResponse{
			Type:       "plugins_inventory",
			RequestID:  raw.RequestID,
			Generation: raw.Generation,
			Inventory:  inventory,
		})
	}()
}

func (s *Server) replacePluginsInventory(conn *websocket.Conn, next pluginsInventoryRequest) (pluginsInventoryRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.pluginsInventories[conn]
	if ok {
		previous.cancel()
	}
	s.pluginsInventories[conn] = next
	return previous, ok
}

func (s *Server) claimPluginsInventory(conn *websocket.Conn, expected pluginsInventoryRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.pluginsInventories[conn]
	if !ok || current.requestID != expected.requestID || current.generation != expected.generation {
		return false
	}
	delete(s.pluginsInventories, conn)
	return true
}

func (s *Server) handlePluginCommand(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequestID(raw.RequestID) {
		s.sendPluginsError(conn, "plugin_command_error", raw, "invalid_request", "Invalid Plugin command request.")
		return
	}
	request := skillmgmt.PluginMutationRequest{
		Operation: skillmgmt.PluginMutationOperation(raw.Operation),
		PluginID:  raw.PluginID,
		Scope:     raw.Scope,
	}
	go func() {
		command, err := skillmgmt.BuildPluginMutationCommand(skillmgmt.InventoryOptions{}, request, s.pluginCatalogCLI)
		if err != nil {
			s.sendPluginsError(conn, "plugin_command_error", raw, "command_rejected", err.Error())
			return
		}
		s.sendJSON(conn, pluginCommandResponse{
			Type:      "plugin_command",
			RequestID: raw.RequestID,
			Command:   command,
		})
	}()
}

// handlePluginMutation rebuilds the reviewed plugin command from the same
// structured inputs and executes it directly on the daemon host. Like the
// Skills mutation, each connection runs one plugin mutation at a time.
func (s *Server) handlePluginMutation(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequestID(raw.RequestID) {
		s.sendPluginsError(conn, "plugin_mutation_error", raw, "invalid_request", "Invalid Plugin mutation request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := pluginsMutationRequest{requestID: raw.RequestID, cancel: cancel}
	previous, hadPrevious := s.replacePluginsMutation(conn, next)
	if hadPrevious {
		previous.cancel()
		s.sendJSON(conn, pluginsErrorResponse{
			Type:      "plugin_mutation_error",
			RequestID: previous.requestID,
			Code:      "superseded",
			Message:   "A newer Plugin mutation replaced this request.",
		})
	}
	go func() {
		request := skillmgmt.PluginMutationRequest{
			Operation: skillmgmt.PluginMutationOperation(raw.Operation),
			PluginID:  raw.PluginID,
			Scope:     raw.Scope,
		}
		command, err := skillmgmt.BuildPluginMutationCommand(skillmgmt.InventoryOptions{Context: ctx}, request, s.pluginCatalogCLI)
		if !s.claimPluginsMutation(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendJSON(conn, pluginsErrorResponse{
				Type:      "plugin_mutation_error",
				RequestID: raw.RequestID,
				Code:      "command_rejected",
				Message:   err.Error(),
			})
			return
		}
		execution, execErr := skillmgmt.ExecutePluginMutationCommand(ctx, command, skillmgmt.MutationExecutionOptions{})
		if execErr != nil {
			code := "execution_failed"
			if errors.Is(execErr, skillmgmt.ErrMutationCancelled) {
				return
			}
			if errors.Is(execErr, skillmgmt.ErrMutationTimedOut) {
				code = "timeout"
			}
			s.sendJSON(conn, pluginsErrorResponse{
				Type:      "plugin_mutation_error",
				RequestID: raw.RequestID,
				Code:      code,
				Message:   execErr.Error(),
			})
			return
		}
		s.sendJSON(conn, pluginMutationResultResponse{
			Type:       "plugin_mutation_result",
			RequestID:  raw.RequestID,
			Command:    command,
			Success:    execution.Success,
			ExitCode:   execution.ExitCode,
			Output:     execution.Output,
			DurationMS: execution.DurationMS,
		})
	}()
}

func (s *Server) replacePluginsMutation(conn *websocket.Conn, next pluginsMutationRequest) (pluginsMutationRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.pluginsMutations[conn]
	if ok {
		previous.cancel()
	}
	s.pluginsMutations[conn] = next
	return previous, ok
}

func (s *Server) claimPluginsMutation(conn *websocket.Conn, expected pluginsMutationRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.pluginsMutations[conn]
	if !ok || current.requestID != expected.requestID {
		return false
	}
	delete(s.pluginsMutations, conn)
	return true
}

func (s *Server) sendPluginsError(conn *websocket.Conn, responseType string, raw clientMessage, code, message string) {
	s.sendJSON(conn, pluginsErrorResponse{
		Type:       responseType,
		RequestID:  raw.RequestID,
		Generation: raw.Generation,
		Code:       code,
		Message:    message,
	})
}
