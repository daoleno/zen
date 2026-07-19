package server

import (
	"context"
	"errors"
	"strings"
	"unicode"

	skillmgmt "github.com/daoleno/zen/daemon/skills"
	"github.com/gorilla/websocket"
)

type skillsSearchRequest struct {
	requestID  string
	generation int64
	cancel     context.CancelFunc
}

type skillsInventoryRequest struct {
	requestID  string
	generation int64
	cancel     context.CancelFunc
}

type skillsInventoryResponse struct {
	Type       string              `json:"type"`
	RequestID  string              `json:"request_id"`
	Generation int64               `json:"generation"`
	Inventory  skillmgmt.Inventory `json:"inventory"`
}

type skillsSearchResponse struct {
	Type       string                  `json:"type"`
	RequestID  string                  `json:"request_id"`
	Generation int64                   `json:"generation"`
	Result     skillmgmt.CatalogResult `json:"result"`
}

type skillsCommandResponse struct {
	Type      string                    `json:"type"`
	RequestID string                    `json:"request_id"`
	Command   skillmgmt.MutationCommand `json:"command"`
}

type skillsErrorResponse struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Generation int64  `json:"generation,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (s *Server) handleSkillsInventory(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) {
		s.sendSkillsError(conn, "skills_inventory_error", raw, "invalid_request", "Invalid Skills inventory request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := skillsInventoryRequest{requestID: raw.RequestID, generation: raw.Generation, cancel: cancel}
	previous, hadPrevious := s.replaceSkillsInventory(conn, next)
	if hadPrevious {
		s.sendJSON(conn, skillsErrorResponse{
			Type:       "skills_inventory_error",
			RequestID:  previous.requestID,
			Generation: previous.generation,
			Code:       "superseded",
			Message:    "A newer Skills inventory request replaced this request.",
		})
	}
	go func() {
		inventory, err := skillmgmt.DiscoverInventory(skillmgmt.InventoryOptions{Context: ctx, CWD: raw.Cwd})
		if !s.claimSkillsInventory(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendSkillsError(conn, "skills_inventory_error", raw, "inventory_failed", err.Error())
			return
		}
		s.sendJSON(conn, skillsInventoryResponse{
			Type:       "skills_inventory",
			RequestID:  raw.RequestID,
			Generation: raw.Generation,
			Inventory:  inventory,
		})
	}()
}

func (s *Server) replaceSkillsInventory(conn *websocket.Conn, next skillsInventoryRequest) (skillsInventoryRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.skillsInventories[conn]
	if ok {
		previous.cancel()
	}
	s.skillsInventories[conn] = next
	return previous, ok
}

func (s *Server) claimSkillsInventory(conn *websocket.Conn, expected skillsInventoryRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsInventories[conn]
	if !ok || current.requestID != expected.requestID || current.generation != expected.generation {
		return false
	}
	delete(s.skillsInventories, conn)
	return true
}

func (s *Server) handleSkillsSearch(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) {
		s.sendSkillsError(conn, "skills_search_error", raw, "invalid_request", "Invalid Skills search request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := skillsSearchRequest{requestID: raw.RequestID, generation: raw.Generation, cancel: cancel}

	s.mu.Lock()
	previous, hadPrevious := s.skillsSearches[conn]
	s.skillsSearches[conn] = next
	s.mu.Unlock()
	if hadPrevious {
		previous.cancel()
		s.sendJSON(conn, skillsErrorResponse{
			Type:       "skills_search_error",
			RequestID:  previous.requestID,
			Generation: previous.generation,
			Code:       "superseded",
			Message:    "A newer Skills search replaced this request.",
		})
	}

	go func() {
		result, err := s.skillsSearcher.Search(ctx, raw.Prompt, raw.Limit)
		if !s.claimSkillsSearch(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendSkillsError(conn, "skills_search_error", raw, "search_failed", err.Error())
			return
		}
		s.sendJSON(conn, skillsSearchResponse{
			Type:       "skills_search",
			RequestID:  raw.RequestID,
			Generation: raw.Generation,
			Result:     result,
		})
	}()
}

func (s *Server) handleSkillsSearchCancel(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) {
		s.sendSkillsError(conn, "skills_search_cancel_error", raw, "invalid_request", "Invalid Skills search cancellation request.")
		return
	}
	canceled, ok := s.cancelSkillsSearch(conn, raw.Generation)
	if ok {
		s.sendJSON(conn, skillsErrorResponse{
			Type:       "skills_search_error",
			RequestID:  canceled.requestID,
			Generation: canceled.generation,
			Code:       "canceled",
			Message:    "The Skills search was canceled.",
		})
	}
	s.sendJSON(conn, map[string]any{
		"type":       "skills_search_canceled",
		"request_id": raw.RequestID,
		"generation": raw.Generation,
	})
}

func (s *Server) cancelSkillsSearch(conn *websocket.Conn, generation int64) (skillsSearchRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsSearches[conn]
	if !ok || current.generation != generation {
		return skillsSearchRequest{}, false
	}
	delete(s.skillsSearches, conn)
	current.cancel()
	return current, true
}

func (s *Server) claimSkillsSearch(conn *websocket.Conn, expected skillsSearchRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsSearches[conn]
	if !ok || current.requestID != expected.requestID || current.generation != expected.generation {
		return false
	}
	delete(s.skillsSearches, conn)
	return true
}

func (s *Server) handleSkillsCommand(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequestID(raw.RequestID) {
		s.sendSkillsError(conn, "skills_command_error", raw, "invalid_request", "Invalid Skills command request.")
		return
	}
	agents := make([]skillmgmt.Agent, 0, len(raw.Agents))
	for _, agent := range raw.Agents {
		agents = append(agents, skillmgmt.Agent(agent))
	}
	request := skillmgmt.MutationRequest{
		Operation: skillmgmt.MutationOperation(raw.Operation),
		CWD:       raw.Cwd,
		SkillID:   raw.SkillID,
		Source:    raw.Source,
		SkillName: raw.SkillName,
		Scope:     skillmgmt.Scope(raw.Scope),
		Agents:    agents,
	}
	go func() {
		command, err := skillmgmt.BuildMutationCommand(skillmgmt.InventoryOptions{}, request)
		if err != nil {
			s.sendSkillsError(conn, "skills_command_error", raw, "command_rejected", err.Error())
			return
		}
		s.sendJSON(conn, skillsCommandResponse{
			Type:      "skills_command",
			RequestID: raw.RequestID,
			Command:   command,
		})
	}()
}

func (s *Server) sendSkillsError(conn *websocket.Conn, responseType string, raw clientMessage, code, message string) {
	s.sendJSON(conn, skillsErrorResponse{
		Type:       responseType,
		RequestID:  raw.RequestID,
		Generation: raw.Generation,
		Code:       code,
		Message:    message,
	})
}

func validSkillsRequest(requestID string, generation int64) bool {
	return validSkillsRequestID(requestID) && generation > 0
}

func validSkillsRequestID(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, current := range value {
		if current > unicode.MaxASCII || unicode.IsControl(current) || unicode.IsSpace(current) {
			return false
		}
	}
	return true
}
