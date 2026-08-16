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

type skillsCatalogRequest struct {
	requestID  string
	generation int64
	cancel     context.CancelFunc
}

type skillsMutationRequest struct {
	requestID string
	cancel    context.CancelFunc
}

type skillsInspectRequest struct {
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

type skillsCatalogResponse struct {
	Type         string                        `json:"type"`
	RequestID    string                        `json:"request_id"`
	Generation   int64                         `json:"generation"`
	Leaderboards skillmgmt.CatalogLeaderboards `json:"leaderboards"`
}

type skillsCommandResponse struct {
	Type      string                    `json:"type"`
	RequestID string                    `json:"request_id"`
	Command   skillmgmt.MutationCommand `json:"command"`
}

type skillsInspectResponse struct {
	Type       string                  `json:"type"`
	RequestID  string                  `json:"request_id"`
	Generation int64                   `json:"generation"`
	Detail     skillmgmt.PackageDetail `json:"detail"`
}

type skillsMutationResultResponse struct {
	Type       string                    `json:"type"`
	RequestID  string                    `json:"request_id"`
	Command    skillmgmt.MutationCommand `json:"command"`
	Success    bool                      `json:"success"`
	ExitCode   int                       `json:"exit_code"`
	Output     string                    `json:"output"`
	DurationMS int64                     `json:"duration_ms"`
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
		inventory, err := skillmgmt.DiscoverInventory(skillsInventoryOptions(s, ctx, raw.Cwd))
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

func (s *Server) handleSkillsCatalog(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) || raw.Limit < 0 || raw.Limit > skillmgmt.MaxLeaderboardLimit {
		s.sendSkillsError(conn, "skills_catalog_error", raw, "invalid_request", "Invalid Skills catalog request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := skillsCatalogRequest{requestID: raw.RequestID, generation: raw.Generation, cancel: cancel}
	previous, hadPrevious := s.replaceSkillsCatalog(conn, next)
	if hadPrevious {
		s.sendJSON(conn, skillsErrorResponse{
			Type:       "skills_catalog_error",
			RequestID:  previous.requestID,
			Generation: previous.generation,
			Code:       "superseded",
			Message:    "A newer Skills catalog request replaced this request.",
		})
	}
	go func() {
		leaderboards, err := s.skillsCatalog.Read(ctx, raw.Limit)
		if !s.claimSkillsCatalog(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendSkillsError(conn, "skills_catalog_error", raw, "catalog_failed", err.Error())
			return
		}
		s.sendJSON(conn, skillsCatalogResponse{
			Type:         "skills_catalog",
			RequestID:    raw.RequestID,
			Generation:   raw.Generation,
			Leaderboards: leaderboards,
		})
	}()
}

func (s *Server) replaceSkillsCatalog(conn *websocket.Conn, next skillsCatalogRequest) (skillsCatalogRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.skillsCatalogs[conn]
	if ok {
		previous.cancel()
	}
	s.skillsCatalogs[conn] = next
	return previous, ok
}

func (s *Server) claimSkillsCatalog(conn *websocket.Conn, expected skillsCatalogRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsCatalogs[conn]
	if !ok || current.requestID != expected.requestID || current.generation != expected.generation {
		return false
	}
	delete(s.skillsCatalogs, conn)
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
	request := skillsMutationWireRequest(raw)
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

func skillsInventoryOptions(s *Server, ctx context.Context, cwd string) skillmgmt.InventoryOptions {
	options := skillmgmt.InventoryOptions{Context: ctx, CWD: cwd}
	if s.execs != nil {
		for name, executor := range s.execs.ByName {
			options.Executors = append(options.Executors, skillmgmt.ExecutorAlias{
				Name: name, Kind: executor.Kind, Command: executor.Command,
			})
		}
	}
	return options
}

func skillsMutationWireRequest(raw clientMessage) skillmgmt.MutationRequest {
	agents := make([]skillmgmt.Agent, 0, len(raw.Agents))
	for _, agent := range raw.Agents {
		agents = append(agents, skillmgmt.Agent(agent))
	}
	scope := skillmgmt.Scope(raw.Scope)
	if scope == "" && raw.Operation == "migrate" {
		scope = skillmgmt.ScopeGlobal
	}
	return skillmgmt.MutationRequest{
		Operation: skillmgmt.MutationOperation(raw.Operation),
		CWD:       raw.Cwd,
		SkillID:   raw.SkillID,
		Source:    raw.Source,
		SkillName: raw.SkillName,
		Ref:       raw.Ref,
		InfoPath:  raw.Path,
		Scope:     scope,
		Agents:    agents,
	}
}

// handleSkillsMutation rebuilds the reviewed command from the same structured
// inputs (authoritative, never trusting the App's displayed text), then
// executes it directly on the daemon host and reports the truthful outcome.
// Each connection runs exactly one mutation at a time; a newer request
// replaces and cancels the previous one.
func (s *Server) handleSkillsMutation(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequestID(raw.RequestID) {
		s.sendSkillsError(conn, "skills_mutation_error", raw, "invalid_request", "Invalid Skills mutation request.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := skillsMutationRequest{requestID: raw.RequestID, cancel: cancel}
	previous, hadPrevious := s.replaceSkillsMutation(conn, next)
	if hadPrevious {
		previous.cancel()
		s.sendJSON(conn, skillsErrorResponse{
			Type:      "skills_mutation_error",
			RequestID: previous.requestID,
			Code:      "superseded",
			Message:   "A newer Skills mutation replaced this request.",
		})
	}
	go func() {
		request := skillsMutationWireRequest(raw)
		buildOptions := skillsInventoryOptions(s, ctx, raw.Cwd)
		command, err := skillmgmt.BuildMutationCommand(buildOptions, request)
		// The mutation stays registered (owned, cancelable) through the whole
		// build + execute lifetime: a newer mutation or a disconnect cancels
		// ctx and replaces/deletes the slot, which both stops the command and
		// suppresses this goroutine's outcome. Only the still-current request
		// may proceed to execution.
		if err == nil && !s.isCurrentSkillsMutation(conn, next) {
			return
		}
		var execution skillmgmt.MutationExecution
		var execErr error
		if err == nil {
			execute := skillmgmt.ExecuteMutationCommand
			if s.skillsMutationExecuteOverride != nil {
				execute = s.skillsMutationExecuteOverride
			}
			// Native Skills operations are cancellable and bounded; the timeout
			// mirrors the old CLI bounds so a hung fetch can never hang Zen.
			execution, execErr = execute(ctx, command, skillmgmt.MutationExecutionOptions{
				CWD:              request.CWD,
				InventoryOptions: buildOptions,
				Timeout:          skillmgmt.MutationTimeoutFor(command),
			})
		}
		// Atomically consume the slot with the emission: a superseded or
		// disconnected request can never claim, so its stale result is
		// suppressed even if the command finished just before the replacement.
		if !s.claimSkillsMutation(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendJSON(conn, skillsErrorResponse{
				Type:      "skills_mutation_error",
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
			s.sendJSON(conn, skillsErrorResponse{
				Type:      "skills_mutation_error",
				RequestID: raw.RequestID,
				Code:      code,
				Message:   execErr.Error(),
			})
			return
		}
		s.sendJSON(conn, skillsMutationResultResponse{
			Type:       "skills_mutation_result",
			RequestID:  raw.RequestID,
			Command:    command,
			Success:    execution.Success,
			ExitCode:   execution.ExitCode,
			Output:     execution.Output,
			DurationMS: execution.DurationMS,
		})
	}()
}

func (s *Server) replaceSkillsMutation(conn *websocket.Conn, next skillsMutationRequest) (skillsMutationRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.skillsMutations[conn]
	if ok {
		previous.cancel()
	}
	s.skillsMutations[conn] = next
	return previous, ok
}

func (s *Server) claimSkillsMutation(conn *websocket.Conn, expected skillsMutationRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsMutations[conn]
	if !ok || current.requestID != expected.requestID {
		return false
	}
	delete(s.skillsMutations, conn)
	return true
}

// isCurrentSkillsMutation reports whether the request still owns the mutation
// slot WITHOUT consuming it. Used to skip execution when a request was
// superseded during command building; the slot itself is consumed atomically
// with the terminal emission via claimSkillsMutation.
func (s *Server) isCurrentSkillsMutation(conn *websocket.Conn, expected skillsMutationRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsMutations[conn]
	return ok && current.requestID == expected.requestID
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

// handleSkillsInspect serves the read-side inspector: rendered SKILL.md
// content, bounded file listing, provenance, hash, bindings, and risk
// signals for one Skill. Like the inventory, inspect is generation-cancelable
// and never mutates state.
func (s *Server) handleSkillsInspect(conn *websocket.Conn, raw clientMessage) {
	if !validSkillsRequest(raw.RequestID, raw.Generation) {
		s.sendSkillsError(conn, "skills_inspect_error", raw, "invalid_request", "Invalid Skills inspect request.")
		return
	}
	name := strings.TrimSpace(raw.SkillName)
	if name == "" {
		s.sendSkillsError(conn, "skills_inspect_error", raw, "invalid_request", "A Skill name is required.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := skillsInspectRequest{requestID: raw.RequestID, generation: raw.Generation, cancel: cancel}
	previous, hadPrevious := s.replaceSkillsInspect(conn, next)
	if hadPrevious {
		s.sendJSON(conn, skillsErrorResponse{
			Type:       "skills_inspect_error",
			RequestID:  previous.requestID,
			Generation: previous.generation,
			Code:       "superseded",
			Message:    "A newer Skills inspect request replaced this request.",
		})
	}
	go func() {
		detail, err := skillmgmt.InspectPackage(skillsInventoryOptions(s, ctx, raw.Cwd), name)
		if !s.claimSkillsInspect(conn, next) {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.sendSkillsError(conn, "skills_inspect_error", raw, "inspect_failed", err.Error())
			return
		}
		s.sendJSON(conn, skillsInspectResponse{
			Type:       "skills_inspect_result",
			RequestID:  raw.RequestID,
			Generation: raw.Generation,
			Detail:     detail,
		})
	}()
}

func (s *Server) replaceSkillsInspect(conn *websocket.Conn, next skillsInspectRequest) (skillsInspectRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.skillsInspects[conn]
	if ok {
		previous.cancel()
	}
	s.skillsInspects[conn] = next
	return previous, ok
}

func (s *Server) claimSkillsInspect(conn *websocket.Conn, expected skillsInspectRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.skillsInspects[conn]
	if !ok || current.requestID != expected.requestID || current.generation != expected.generation {
		return false
	}
	delete(s.skillsInspects, conn)
	return true
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
