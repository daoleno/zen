package server

import (
	"context"
	"errors"
	"strings"

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
		options := skillsInventoryOptions(s, ctx, "")
		inventory, err := skillmgmt.DiscoverPluginInventory(
			options,
			s.pluginRuntime,
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
	request, err := pluginMutationWireRequest(raw)
	if err != nil {
		s.sendPluginsError(conn, "plugin_command_error", raw, "invalid_request", err.Error())
		return
	}
	go func() {
		command, err := skillmgmt.BuildPluginMutationCommand(skillsInventoryOptions(s, context.Background(), ""), request, s.pluginRuntime)
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
	request, err := pluginMutationWireRequest(raw)
	if err != nil {
		s.sendPluginsError(conn, "plugin_mutation_error", raw, "invalid_request", err.Error())
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
		buildOptions := skillsInventoryOptions(s, ctx, "")
		command, err := skillmgmt.BuildPluginMutationCommand(buildOptions, request, s.pluginRuntime)
		// Ownership stays registered (cancelable) through build + execute, so
		// a newer mutation or a disconnect cancels the in-flight command and
		// suppresses this goroutine's outcome. Only the still-current request
		// proceeds to execution.
		if err == nil && !s.isCurrentPluginsMutation(conn, next) {
			return
		}
		var execution skillmgmt.MutationExecution
		var execErr error
		if err == nil {
			execute := skillmgmt.ExecutePluginMutationCommand
			if s.pluginMutationExecuteOverride != nil {
				execute = s.pluginMutationExecuteOverride
			}
			execution, execErr = execute(ctx, command, skillmgmt.MutationExecutionOptions{
				InventoryOptions: buildOptions,
				PluginRuntime:    s.pluginRuntime,
			})
		}
		// Atomically consume the slot with the emission: a superseded or
		// disconnected request can never claim, so its stale result is
		// suppressed even if the command finished just before the replacement.
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
		if execErr != nil {
			if errors.Is(execErr, skillmgmt.ErrMutationCancelled) {
				return
			}
			code := "execution_failed"
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

func pluginMutationWireRequest(raw clientMessage) (skillmgmt.PluginMutationRequest, error) {
	request := skillmgmt.PluginMutationRequest{
		Operation: skillmgmt.PluginMutationOperation(raw.Operation),
		PluginID:  raw.PluginID,
		Host:      skillmgmt.PluginHost(raw.PluginHost),
		Scope:     raw.Scope,
	}
	switch request.Operation {
	case skillmgmt.PluginOperationInstall:
		return request, nil
	case skillmgmt.PluginOperationUninstall:
		if strings.TrimSpace(raw.PluginCopyID) == "" ||
			strings.TrimSpace(raw.PluginName) == "" ||
			strings.TrimSpace(raw.PluginSource) == "" ||
			strings.TrimSpace(raw.PluginVersion) == "" ||
			strings.TrimSpace(raw.SkillRoot) == "" ||
			strings.TrimSpace(raw.SkillCanonical) == "" ||
			strings.TrimSpace(raw.SkillAllowedRoot) == "" ||
			strings.TrimSpace(raw.PluginRevision) == "" ||
			len(raw.Agents) == 0 {
			return skillmgmt.PluginMutationRequest{}, errors.New("a complete current Plugin copy identity is required")
		}
		request.CopyID = raw.PluginCopyID
		request.Name = raw.PluginName
		request.Source = skillmgmt.PluginSource(raw.PluginSource)
		request.Version = raw.PluginVersion
		request.RootPath = raw.SkillRoot
		request.CanonicalPath = raw.SkillCanonical
		request.AllowedRoot = raw.SkillAllowedRoot
		request.Revision = raw.PluginRevision
		request.Agents = make([]skillmgmt.Agent, 0, len(raw.Agents))
		for _, agent := range raw.Agents {
			request.Agents = append(request.Agents, skillmgmt.Agent(agent))
		}
		return request, nil
	default:
		return skillmgmt.PluginMutationRequest{}, errors.New("this Plugin operation is not supported")
	}
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

// isCurrentPluginsMutation reports whether the request still owns the plugin
// mutation slot WITHOUT consuming it. See isCurrentSkillsMutation.
func (s *Server) isCurrentPluginsMutation(conn *websocket.Conn, expected pluginsMutationRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.pluginsMutations[conn]
	return ok && current.requestID == expected.requestID
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
