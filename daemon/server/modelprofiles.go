package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

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
	case "discover_provider_models":
		s.handleDiscoverProviderModels(conn, raw)
		return true
	case "test_provider_connection":
		s.handleTestProviderConnection(conn, raw)
		return true
	case "get_session_provider":
		s.handleGetSessionProvider(conn, raw)
		return true
	case "activate_session_provider":
		s.handleActivateSessionProvider(conn, raw)
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
	proj, err := owner.UpsertProviderConnection(in, raw.Revision, create)
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

func (s *Server) handleTestProviderConnection(conn *websocket.Conn, raw clientMessage) {
	if !s.credentialWriteAllowed(conn) {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeSecureTransportRequired,
			"secure transport required for credential tests")
		return
	}
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
	})
}

func (s *Server) handleGetSessionProvider(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	sessionID := strings.TrimSpace(raw.AgentID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(raw.SessionID)
	}
	sel, ok := owner.SessionProviderSelection(sessionID)
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeBindingNotFound, "session provider binding not found")
		return
	}
	snap, _ := owner.SessionSnapshot(sessionID)
	payload := map[string]any{
		"type":       "session_provider",
		"request_id": raw.RequestID,
		"agent_id":   sessionID,
		"selection":  sel,
	}
	if snap.Launched != nil {
		payload["launched"] = snap.Launched
	}
	s.sendJSON(conn, payload)
}

func (s *Server) handleActivateSessionProvider(conn *websocket.Conn, raw clientMessage) {
	owner := s.modelProfiles()
	if owner == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
		return
	}
	sessionID := strings.TrimSpace(raw.AgentID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(raw.SessionID)
	}
	connectionID := strings.TrimSpace(raw.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(raw.ProfileID)
	}
	_, snap, persist, err := owner.ActivateSessionProvider(sessionID, connectionID, raw.ModelID)
	if !persist.Applied {
		s.sendModelProfileError(conn, raw.RequestID, err)
		return
	}
	if snap.Current == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeBindingNotFound, "session provider binding not found after activate")
		return
	}
	outcome, durable := modelprofiles.WirePersistFields(persist)
	payload := map[string]any{
		"type":       "session_provider_activated",
		"request_id": raw.RequestID,
		"agent_id":   sessionID,
		"selection": map[string]any{
			"session_id":       snap.Current.SessionID,
			"client":           snap.Current.Client,
			"connection_id":    snap.Current.ConnectionID,
			"connection_name":  snap.Current.ConnectionName,
			"provider_label":   snap.Current.ProviderLabel,
			"model_id":         snap.Current.ModelID,
			"credential_ready": snap.Current.CredentialReady,
			"hot_switchable":   snap.Current.HotSwitchable,
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

func (s *Server) handleSetProviderCredential(conn *websocket.Conn, raw clientMessage) {
	if !s.credentialWriteAllowed(conn) {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeSecureTransportRequired,
			"secure transport required for credential writes")
		return
	}
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
	if !s.credentialWriteAllowed(conn) {
		s.sendErrorWithRequestID(conn, raw.RequestID, modelprofiles.CodeSecureTransportRequired,
			"secure transport required for credential writes")
		return
	}
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

func (s *Server) credentialWriteAllowed(conn *websocket.Conn) bool {
	if s == nil || conn == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, ok := s.clients[conn]
	return ok && owner != nil && owner.secureTransport
}

func isSecureCredentialTransport(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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
	if owner := s.modelProfiles(); owner != nil {
		release = owner.ReleaseSession
	}
	return modelprofiles.TeardownSession(agentID, s.killSession, s.sessionLivenessProbe, release)
}
