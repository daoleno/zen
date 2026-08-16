package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/codexctl"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

// SetModelProfiles installs the production Model Profiles owner.
func (s *Server) SetModelProfiles(owner *modelprofiles.Owner) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = owner
}

func (s *Server) modelProfiles() *modelprofiles.Owner {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profiles
}

func (s *Server) handleModelProfileMessage(conn *websocket.Conn, raw clientMessage) bool {
	switch raw.Type {
	case "list_providers":
		s.handleListProviders(conn, raw)
		return true
	case "upsert_provider_connection":
		s.handleUpsertProviderConnection(conn, raw)
		return true
	case "delete_provider_connection":
		s.handleDeleteProviderConnection(conn, raw)
		return true
	case "set_provider_default":
		s.handleSetProviderDefault(conn, raw)
		return true
	case "switch_provider":
		s.handleSwitchProvider(conn, raw)
		return true
	case "set_provider_models":
		s.handleSetProviderModels(conn, raw)
		return true
	case "discover_provider_models":
		s.handleDiscoverProviderModels(conn, raw)
		return true
	case "test_provider_connection":
		s.handleTestProviderConnection(conn, raw)
		return true
	case "codex_gateway_status":
		s.handleCodexGatewayStatus(conn, raw)
		return true
	case "codex_gateway_enable":
		s.handleCodexGatewayEnable(conn, raw)
		return true
	case "codex_gateway_disable":
		s.handleCodexGatewayDisable(conn, raw)
		return true
	case "codex_gateway_restore_backup":
		s.handleCodexGatewayRestoreBackup(conn, raw)
		return true
	case "get_thread_runtime":
		s.handleGetThreadRuntime(conn, raw)
		return true
	case "set_thread_runtime":
		s.handleSetThreadRuntime(conn, raw)
		return true
	case "set_provider_credential":
		s.handleSetProviderCredential(conn, raw)
		return true
	case "clear_provider_credential":
		s.handleClearProviderCredential(conn, raw)
		return true
	// Legacy Profile message types are rejected — Provider-first wire only.
	case "list_model_profiles", "get_model_profile", "upsert_model_profile",
		"delete_model_profile", "set_model_profile_default",
		"get_session_route", "activate_session_route":
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfileInvalid,
			"profile wire removed; use provider connection APIs")
		return true
	default:
		return false
	}
}

func (s *Server) handleListProviders(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	proj, err := owner.ProjectProviders()
	if err != nil {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, s.providersCatalogPayload(raw.RequestID, proj, modelprofiles.PersistResult{Applied: true, Durable: true}, nil))
}

func (s *Server) handleUpsertProviderConnection(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	in, err := providerInputFromMessage(raw)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfileInvalid, err.Error())
		return
	}
	create := strings.EqualFold(strings.TrimSpace(raw.Operation), "create")
	if !create && in.ID != "" {
		if _, err := owner.GetProfile(in.ID); errors.Is(err, modelprofiles.ErrNotFound) {
			create = true
		}
	}
	apiKey := strings.TrimSpace(raw.Credential)
	// Never accept body aliases for secrets and never retain the value.
	raw.Credential = ""
	proj, err := owner.UpsertProviderConnection(in, apiKey, raw.Revision, create)
	apiKey = ""
	s.sendProvidersMutation(conn, raw.RequestID, proj, err)
}

func (s *Server) handleDeleteProviderConnection(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	id := strings.TrimSpace(raw.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(raw.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(raw.ID)
	}
	proj, err := owner.DeleteProviderConnection(id, raw.Revision)
	s.sendProvidersMutation(conn, raw.RequestID, proj, err)
}

func (s *Server) handleSetProviderDefault(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	executorID := strings.TrimSpace(raw.ExecutorID)
	if executorID == "" {
		executorID = strings.TrimSpace(raw.Executor)
	}
	if executorID == "" {
		executorID = strings.TrimSpace(raw.Client)
	}
	connectionID := strings.TrimSpace(raw.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(raw.ProfileID)
	}
	proj, err := owner.SetProviderDefault(executorID, connectionID, raw.ModelID, raw.Revision)
	s.sendProvidersMutation(conn, raw.RequestID, proj, err)
}

func (s *Server) handleSwitchProvider(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	executorID := strings.TrimSpace(raw.ExecutorID)
	if executorID == "" {
		executorID = strings.TrimSpace(raw.Executor)
	}
	if executorID == "" {
		executorID = strings.TrimSpace(raw.Client)
	}
	connectionID := strings.TrimSpace(raw.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(raw.ProfileID)
	}
	proj, err := owner.SwitchProvider(executorID, connectionID, raw.Revision)
	s.sendProvidersMutation(conn, raw.RequestID, proj, err)
}

func (s *Server) handleSetProviderModels(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	connectionID := strings.TrimSpace(raw.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(raw.ProfileID)
	}
	if connectionID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfileInvalid, "connection_id is required")
		return
	}
	proj, persist, err := owner.SetProviderModelSupport(connectionID, raw.ModelIDs)
	if !persist.Applied {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, s.providersCatalogPayload(raw.RequestID, proj, persist, err))
}

func (s *Server) handleDiscoverProviderModels(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	id := strings.TrimSpace(raw.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(raw.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(raw.ID)
	}
	res, err := owner.DiscoverProviderModelsDetailed(id, true)
	if err != nil && len(res.Entries) == 0 {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	payload := map[string]any{
		"type":                "provider_models",
		"request_id":          raw.RequestID,
		"connection_id":       id,
		"models":              res.Entries,
		"persistence_durable": res.PersistenceDurable,
	}
	if res.PersistenceWarning != "" {
		payload["persistence_warning"] = res.PersistenceWarning
	}
	if err != nil {
		payload["discovery_warning"] = err.Error()
	}
	s.sendJSON(conn, payload)
}


// handleCodexGatewayStatus reports the machine-level Codex gateway takeover
// state (active | inactive | drifted | broken) plus backup/restore info.
func (s *Server) handleCodexGatewayStatus(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	status := owner.GatewayStatus()
	s.sendJSON(conn, map[string]any{
		"type":         "codex_gateway_status",
		"request_id":   raw.RequestID,
		"ok":           true,
		"codex_gateway": status,
	})
}

// handleCodexGatewayEnable activates the machine-level takeover with an exact
// config backup and an atomic projection to the stable gateway endpoint.
func (s *Server) handleCodexGatewayEnable(conn *websocket.Conn, raw clientMessage) {
	s.handleCodexGatewayMutation(conn, raw, "codex_gateway_enable")
}

// handleCodexGatewayDisable removes only the Zen-owned projection.
func (s *Server) handleCodexGatewayDisable(conn *websocket.Conn, raw clientMessage) {
	s.handleCodexGatewayMutation(conn, raw, "codex_gateway_disable")
}

// handleCodexGatewayRestoreBackup rolls the exact pre-takeover config back.
func (s *Server) handleCodexGatewayRestoreBackup(conn *websocket.Conn, raw clientMessage) {
	s.handleCodexGatewayMutation(conn, raw, "codex_gateway_restore_backup")
}

func (s *Server) handleCodexGatewayMutation(conn *websocket.Conn, raw clientMessage, responseType string) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	var status modelprofiles.TakeoverStatus
	var err error
	switch responseType {
	case "codex_gateway_enable":
		status, err = owner.EnableCodexGateway(modelprofiles.DefaultGatewayListenAddr)
	case "codex_gateway_disable":
		status, err = owner.DisableCodexGateway()
	default:
		status, err = owner.RestoreCodexGatewayBackup()
	}
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.ControlErrorCode(err), err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         responseType,
		"request_id":   raw.RequestID,
		"ok":           true,
		"codex_gateway": status,
	})
}

func (s *Server) handleTestProviderConnection(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	// Saved-connection test: resolve the persisted Base URL, compiled protocol
	// and active stored credential ref daemon-side by stable Provider ID. The
	// App never supplies or receives the secret.
	id := strings.TrimSpace(raw.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(raw.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(raw.ID)
	}
	if id != "" {
		result, err := owner.TestSavedProviderConnection(id)
		if err != nil {
			s.sendModelProfileError(conn, raw.RequestID, err)
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":          "provider_connection_test",
			"request_id":    raw.RequestID,
			"connection_id": id,
			"client":        result.Client,
			"model_count":   result.ModelCount,
			"latency_ms":    result.LatencyMS,
		})
		return
	}
	in, err := providerInputFromMessage(raw)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfileInvalid, err.Error())
		return
	}
	secret := strings.TrimSpace(raw.Credential)
	raw.Credential = ""
	result, err := owner.TestProviderConnection(modelprofiles.ProviderConnectionTestInput{
		Client:     in.Client,
		BaseURL:    in.BaseURL,
		Credential: secret,
	})
	secret = ""
	if err != nil {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":        "provider_connection_test",
		"request_id":  raw.RequestID,
		"client":      result.Client,
		"model_count": result.ModelCount,
		"latency_ms":  result.LatencyMS,
	})
}

func (s *Server) handleGetThreadRuntime(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	sessionID := strings.TrimSpace(raw.AgentID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(raw.SessionID)
	}
	sel, ok := owner.ThreadRuntime(sessionID)
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeBindingNotFound, "thread runtime not found")
		return
	}
	snap, _ := owner.SessionSnapshot(sessionID)
	payload := map[string]any{
		"type":       "thread_runtime",
		"request_id": raw.RequestID,
		"agent_id":   sessionID,
		"runtime":    sel,
	}
	if snap.Launched != nil {
		payload["launched"] = snap.Launched
	}
	s.sendJSON(conn, payload)
}

func (s *Server) handleSetThreadRuntime(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	sessionID := strings.TrimSpace(raw.AgentID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(raw.SessionID)
	}
	if raw.Runtime == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfileInvalid, "runtime is required")
		return
	}
	snap, persist, err := s.SetThreadRuntime(sessionID, *raw.Runtime)
	if !persist.Applied {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	if snap.Current == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeBindingNotFound, "thread runtime not found after switch")
		return
	}
	outcome, durable := modelprofiles.WirePersistFields(persist)
	payload := map[string]any{
		"type":       "thread_runtime_set",
		"request_id": raw.RequestID,
		"agent_id":   sessionID,
		"runtime": map[string]any{
			"session_id":               snap.Current.SessionID,
			"client":                   snap.Current.Client,
			"connection_id":            snap.Current.ConnectionID,
			"connection_name":          snap.Current.ConnectionName,
			"provider_label":           snap.Current.ProviderLabel,
			"model_id":                 snap.Current.ModelID,
			"reasoning_effort":         snap.Current.ReasoningEffort,
			"reasoning_effort_default": snap.Current.ReasoningEffortDefault,
			"reasoning_efforts":        snap.Current.ReasoningEfforts,
			"credential_ready":         snap.Current.CredentialReady,
			"hot_switchable":           snap.Current.HotSwitchable,
		},
		"persistence_outcome": outcome,
		"persistence_durable": durable,
	}
	if snap.Launched != nil {
		payload["launched"] = snap.Launched
	}
	if err != nil {
		payload["persistence_warning"] = err.Error()
	}
	s.sendJSON(conn, payload)
}

// SetThreadRuntime is the single daemon transaction for an acknowledged
// current-thread runtime. For live-control Codex Sessions (app-server mode)
// it applies the native thread/settings/update FIRST and commits the Zen
// route binding only after the native applied-settings acknowledgement; on
// native failure or timeout the route is untouched, and on route-commit
// failure the native side is reverted. For embedded Sessions it changes only
// the route binding and durable runtime projection; the existing process and
// conversation remain untouched.
func (s *Server) SetThreadRuntime(sessionID string, choice modelprofiles.ThreadRuntimeChoice) (modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error) {
	owner := s.modelProfiles()
	if owner == nil {
		return modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{}, fmt.Errorf("%w: Providers are not available", modelprofiles.ErrInvalid)
	}
	prepared, err := owner.PrepareThreadRuntime(sessionID, choice)
	if err != nil {
		return modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{}, err
	}
	// Honest pre-feature Sessions: an embedded Codex TUI has no reachable
	// app-server control surface, so an Interface mutation could never reach
	// the native thread without restarting the Codex process. Reject the
	// switch instead of acknowledging a state the native side cannot hold.
	if s.codexLiveDial != nil {
		if socket := owner.CodexControlSocket(sessionID); socket == "" {
			if state, ok := owner.Table().Get(sessionID); ok && strings.EqualFold(strings.TrimSpace(state.Binding.ExecutorID), modelprofiles.ExecutorCodex) {
				return modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{}, fmt.Errorf("%w: this Codex session predates live-control launches; restart the session to enable synchronized model/effort switching (one-time migration)", modelprofiles.ErrInvalid)
			}
		}
	}
	// Native-first: apply the exact resolved model+effort to the live Codex
	// thread and wait for the native acknowledgement before publishing the
	// Zen route. The prepare/commit split keeps the Owner lock free during
	// the network round-trip; Commit re-validates the generation CAS.
	if socket := owner.CodexControlSocket(sessionID); socket != "" && s.codexLiveDial != nil {
		revert, cleanup, liveErr := s.applyLiveNativeThreadRuntime(socket, sessionID, prepared)
		if liveErr != nil {
			return modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{}, liveErr
		}
		_, snap, persist, commitErr := owner.CommitThreadRuntime(prepared)
		if !persist.Applied || commitErr != nil {
			if revert != nil {
				// Route publish failed after native applied: revert the native
				// thread to the previous model/effort (best-effort; the native
				// update is idempotent).
				rctx, rcancel := context.WithTimeout(context.Background(), codexLiveRollbackBudget)
				_ = revert(rctx)
				rcancel()
			}
			cleanup()
			return snap, persist, commitErr
		}
		cleanup()
		return snap, persist, nil
	}
	_, snap, persist, err := owner.SetThreadRuntime(sessionID, choice)
	if !persist.Applied {
		return modelprofiles.WireSessionSnapshot{}, persist, err
	}
	return snap, persist, err
}

// codexLiveRollbackBudget bounds the native rollback after a failed route
// commit. Rollback is best-effort: a timed-out rollback still returns the
// route-commit failure to the caller, and the next request is normalized by
// the router to the committed binding. Apply + rollback stay below the App's
// 20s set_thread_runtime wait.
const codexLiveRollbackBudget = 5 * time.Second

// applyLiveNativeThreadRuntime opens the Session's Codex app-server control
// socket, resolves the native thread, applies the prepared target model+effort
// and waits for the native acknowledgement. The returned revert re-applies
// the previous native identity on the same open connection; the returned
// cleanup closes it (idempotent, safe to call after revert).
func (s *Server) applyLiveNativeThreadRuntime(socket, sessionID string, prepared modelprofiles.PreparedThreadRuntime) (revert func(context.Context) error, cleanup func(), err error) {
	ctx, cancel := context.WithTimeout(context.Background(), codexLiveApplyBudget)
	defer cancel()
	live, liveErr := s.codexLiveDial(ctx, socket)
	if liveErr != nil {
		return nil, nil, fmt.Errorf("%w: dial %s: %v", modelprofiles.ErrInvalid, socket, liveErr)
	}
	threadID, resolveErr := live.ResolveThread(ctx, s.codexSessionCwd(sessionID))
	if resolveErr != nil {
		_ = live.Close()
		return nil, nil, fmt.Errorf("%w: resolve native thread: %v", modelprofiles.ErrInvalid, resolveErr)
	}
	target := prepared.Target()
	previous := prepared.Previous()
	nativeRevert, applyErr := live.ApplySettings(
		ctx,
		threadID,
		target.ModelID,
		codexEffortPointer(target.Effect),
		codexctl.Settings{ThreadID: threadID, Model: previous.ModelID, Effort: previous.Effect},
		codexctl.DefaultAckTimeout,
	)
	if applyErr != nil {
		_ = live.Close()
		return nil, nil, fmt.Errorf("%w: native thread settings apply: %v", modelprofiles.ErrInvalid, applyErr)
	}
	revert = func(rctx context.Context) error {
		return nativeRevert(rctx)
	}
	cleanup = func() { _ = live.Close() }
	return revert, cleanup, nil
}

// codexLiveApplyBudget bounds dial + thread resolution + settings apply + ack
// for one Interface runtime mutation. The App-side set_thread_runtime wait is
// 20s; apply + rollback stay safely below it.
const codexLiveApplyBudget = 13 * time.Second

func codexEffortPointer(effort string) *string {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return nil
	}
	return &effort
}

// codexSessionCwd returns the watcher-observed cwd for a Session ("" when
// unknown; thread resolution then falls back to the app server's primary
// loaded thread).
func (s *Server) codexSessionCwd(sessionID string) string {
	if s == nil {
		return ""
	}
	if s.getAgentOverride != nil {
		if agent := s.getAgentOverride(sessionID); agent != nil {
			return strings.TrimSpace(agent.Cwd)
		}
		return ""
	}
	if s.watcher == nil {
		return ""
	}
	if agent := s.watcher.GetAgent(sessionID); agent != nil {
		return strings.TrimSpace(agent.Cwd)
	}
	return ""
}

func (s *Server) handleSetProviderCredential(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	id := strings.TrimSpace(raw.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(raw.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(raw.ID)
	}
	secret := strings.TrimSpace(raw.Credential)
	// Never accept text/body aliases for secrets. Never echo secret in errors/logs.
	res, err := owner.SetProviderCredential(id, secret)
	secret = ""
	raw.Credential = ""
	if err != nil {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":                "provider_credential",
		"request_id":          raw.RequestID,
		"connection_id":       res.ConnectionID,
		"credential_ready":    res.CredentialReady,
		"persistence_outcome": res.PersistenceOutcome,
		"persistence_durable": res.PersistenceDurable,
	})
}

func (s *Server) handleClearProviderCredential(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	id := strings.TrimSpace(raw.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(raw.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(raw.ID)
	}
	res, err := owner.ClearProviderCredential(id)
	if err != nil {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":                "provider_credential",
		"request_id":          raw.RequestID,
		"connection_id":       res.ConnectionID,
		"credential_ready":    res.CredentialReady,
		"persistence_outcome": res.PersistenceOutcome,
		"persistence_durable": res.PersistenceDurable,
	})
}

func (s *Server) sendProvidersMutation(conn *websocket.Conn, requestID string, proj modelprofiles.ProviderCatalogProjection, err error) {
	persist := modelprofiles.PersistResultFromError(err)
	if !persist.Applied {
		s.sendModelProfileError(conn, requestID, err)
		return
	}
	s.sendJSON(conn, s.providersCatalogPayload(requestID, proj, persist, err))
}

func (s *Server) providersCatalogPayload(requestID string, proj modelprofiles.ProviderCatalogProjection, persist modelprofiles.PersistResult, warn error) map[string]any {
	payload := map[string]any{
		"type":        "providers",
		"request_id":  requestID,
		"revision":    proj.Revision,
		"connections": proj.Connections,
		"defaults":    proj.Defaults,
		"presets":     proj.Presets,
		"models":      proj.Models,
	}
	if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
		payload["persistence_outcome"] = outcome
		payload["persistence_durable"] = durable
	}
	if warn != nil {
		payload["persistence_warning"] = warn.Error()
	}
	return payload
}

func providerInputFromMessage(raw clientMessage) (modelprofiles.ProviderConnectionInput, error) {
	if raw.ProviderConnection != nil {
		return *raw.ProviderConnection, nil
	}
	return modelprofiles.ProviderConnectionInput{}, fmt.Errorf("provider_connection is required")
}

func (s *Server) sendModelProfileError(conn *websocket.Conn, requestID string, err error) {
	code := modelprofiles.ControlErrorCode(err)
	if code == "" {
		code = modelprofiles.CodeProfilesUnavailable
	}
	s.sendErrorWithRequestID(conn, requestID, code, err.Error())
}

func (s *Server) createSessionWithProfiles(preferredTarget string, opts watcher.CreateSessionOptions, profileID string) (string, *modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error) {
	owner := s.modelProfiles()
	if owner == nil {
		agentID, err := s.watcher.CreateSession(preferredTarget, opts)
		return agentID, nil, modelprofiles.PersistResult{Applied: true, Durable: true}, err
	}

	executorID := strings.TrimSpace(profileExecutorHint(opts.Command, profileID))
	plan, err := owner.PrepareLaunch(executorID, profileID, opts.Command)
	if err != nil && !plan.Persist.Applied && !plan.Bypass {
		return "", nil, plan.Persist, err
	}
	if plan.Bypass || !plan.Applied {
		agentID, err := s.watcher.CreateSession(preferredTarget, opts)
		return agentID, nil, modelprofiles.PersistResult{Applied: true, Durable: true}, err
	}

	opts.Command = plan.Command
	opts.Env = mergeSessionEnv(opts.Env, plan.Env)
	agentID, err := s.watcher.CreateSession(preferredTarget, opts)
	if err != nil {
		abortPersist, abortErr := owner.AbortLaunch(plan.ProvisionalID)
		return "", nil, abortPersist, errors.Join(err, abortErr)
	}
	_, snap, persist, commitErr := owner.CommitLaunch(plan.ProvisionalID, agentID)
	if !persist.Applied {
		cleanup := modelprofiles.CleanupFailedLaunch(owner, plan.ProvisionalID, agentID, s.watcher.KillSession, s.sessionLivenessProbe)
		return "", nil, cleanup.Persist, errors.Join(commitErr, cleanup.Err)
	}
	persist = modelprofiles.CombinePersistResults(plan.Persist, persist)
	if !persist.Durable && commitErr == nil && !plan.Persist.Durable {
		commitErr = modelprofiles.ErrPersistDirSync
	}
	return agentID, &snap, persist, commitErr
}

func profileExecutorHint(command, profileID string) string {
	if hint := work.ProfileClientExecutor(command); hint != "" {
		return hint
	}
	if strings.TrimSpace(profileID) != "" {
		return ""
	}
	return ""
}

func mergeSessionEnv(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func (s *Server) killSession(sessionID string) error {
	if s != nil && s.killSessionOverride != nil {
		return s.killSessionOverride(sessionID)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("watcher unavailable")
	}
	return s.watcher.KillSession(sessionID)
}

func (s *Server) sessionLivenessProbe(sessionID string) (modelprofiles.SessionLiveness, error) {
	if s != nil && s.probeSessionOverride != nil {
		presence, err := s.probeSessionOverride(sessionID)
		if err != nil {
			return modelprofiles.SessionLivenessUnknown, err
		}
		switch presence {
		case watcher.SessionPresencePresent:
			return modelprofiles.SessionLivenessPresent, nil
		case watcher.SessionPresenceAbsent:
			return modelprofiles.SessionLivenessAbsent, nil
		default:
			return modelprofiles.SessionLivenessUnknown, nil
		}
	}
	if s == nil || s.watcher == nil {
		return modelprofiles.SessionLivenessUnknown, fmt.Errorf("watcher unavailable")
	}
	presence, err := s.watcher.ProbeSession(sessionID)
	if err != nil {
		return modelprofiles.SessionLivenessUnknown, err
	}
	switch presence {
	case watcher.SessionPresencePresent:
		return modelprofiles.SessionLivenessPresent, nil
	case watcher.SessionPresenceAbsent:
		return modelprofiles.SessionLivenessAbsent, nil
	default:
		return modelprofiles.SessionLivenessUnknown, nil
	}
}

// teardownAgentSession applies the route-aware kill/release rule for kill_agent
// and other Session destruction paths. Returns a non-nil error whenever cleanup
// is incomplete or applied with uncertain durability.
func (s *Server) teardownAgentSession(agentID string) modelprofiles.SessionTeardownResult {
	var release func(string) (modelprofiles.PersistResult, error)
	controlSocket := ""
	if owner := s.modelProfiles(); owner != nil {
		release = owner.ReleaseSession
		controlSocket = owner.CodexControlSocket(agentID)
	}
	result := modelprofiles.TeardownSession(agentID, s.killSession, s.sessionLivenessProbe, release)
	if result.Err == nil && controlSocket != "" {
		// The Session is confirmed dead: kill any orphaned Codex app-server
		// (via its recorded pid) and remove daemon-owned socket/pid/log files.
		if cleanupErr := modelprofiles.CleanupCodexControlArtifacts(controlSocket); cleanupErr != nil {
			log.Printf("cleanup codex control artifacts for %s: %v", agentID, cleanupErr)
		}
	}
	return result
}
