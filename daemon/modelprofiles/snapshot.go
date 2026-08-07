package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DurableRouteSchemaVersion = 4
const minDurableRouteSchemaVersion = 3

// durableRouteFile may contain upstream URL, auth_mode, and credential env
// *names* only — never values. CredentialReady is never authoritative on disk.
type durableRouteFile struct {
	SchemaVersion int                   `json:"schema_version"`
	Routes        []durableSessionRoute `json:"routes"`
}

type durableEnvelope struct {
	ContextWindowTokens int64    `json:"context_window_tokens"`
	ReasoningClass      string   `json:"reasoning_class"`
	ThinkingClass       string   `json:"thinking_class"`
	ToolClass           string   `json:"tool_class"`
	Modalities          []string `json:"modalities"`
}

type durableSessionRoute struct {
	SessionID             string                `json:"session_id"`
	RouteID               string                `json:"route_id"`
	ExecutorID            string                `json:"executor_id"`
	ProfileID             string                `json:"profile_id"`
	ProfileName           string                `json:"profile_name"`
	RouteProtocol         string                `json:"route_protocol"`
	ProviderID            string                `json:"provider_id"`
	ProviderLabel         string                `json:"provider_label"`
	Protocol              string                `json:"protocol"`
	ClientModel           string                `json:"client_model"`
	ClientModelProvenance string                `json:"client_model_provenance"`
	UpstreamBaseURL       string                `json:"upstream_base_url"`
	UpstreamModel         string                `json:"upstream_model"`
	HistoryDomain         string                `json:"history_domain"`
	HistoryState          string                `json:"history_state"`
	HistoryPortability    string                `json:"history_portability,omitempty"`
	ClientEnvelope        durableEnvelope       `json:"client_envelope"`
	UpstreamEnvelope      durableEnvelope       `json:"upstream_envelope"`
	AuthMode              string                `json:"auth_mode"`
	CredentialEnv         string                `json:"credential_env,omitempty"`
	CredentialRef         string                `json:"credential_ref,omitempty"`
	Generation            int64                 `json:"generation"`
	CatalogRevision       int64                 `json:"catalog_revision"`
	Activation            string                `json:"activation"`
	History               []durableHistoryEvent `json:"history,omitempty"`
	// Launched is the immutable original launch binding (schema v4+).
	Launched *durableRouteFields `json:"launched,omitempty"`
}

type durableHistoryEvent struct {
	Generation         int64               `json:"generation"`
	Activation         string              `json:"activation"`
	HistoryDegradation string              `json:"history_degradation,omitempty"`
	From               *durableRouteFields `json:"from,omitempty"`
	To                 durableRouteFields  `json:"to"`
}

type durableRouteFields struct {
	SessionID             string          `json:"session_id"`
	RouteID               string          `json:"route_id"`
	ExecutorID            string          `json:"executor_id"`
	ProfileID             string          `json:"profile_id"`
	ProfileName           string          `json:"profile_name"`
	RouteProtocol         string          `json:"route_protocol"`
	ProviderID            string          `json:"provider_id"`
	ProviderLabel         string          `json:"provider_label"`
	Protocol              string          `json:"protocol"`
	ClientModel           string          `json:"client_model"`
	ClientModelProvenance string          `json:"client_model_provenance"`
	UpstreamBaseURL       string          `json:"upstream_base_url"`
	UpstreamModel         string          `json:"upstream_model"`
	HistoryDomain         string          `json:"history_domain"`
	HistoryState          string          `json:"history_state"`
	HistoryPortability    string          `json:"history_portability,omitempty"`
	ClientEnvelope        durableEnvelope `json:"client_envelope"`
	UpstreamEnvelope      durableEnvelope `json:"upstream_envelope"`
	AuthMode              string          `json:"auth_mode"`
	CredentialEnv         string          `json:"credential_env,omitempty"`
	CredentialRef         string          `json:"credential_ref,omitempty"`
	Generation            int64           `json:"generation"`
	CatalogRevision       int64           `json:"catalog_revision"`
	Activation            string          `json:"activation"`
}

// EncodeDurableSnapshot encodes SessionRouteState records for daemon-owned 0600
// persistence. Credential values and readiness bools are never included.
func EncodeDurableSnapshot(states []SessionRouteState) ([]byte, error) {
	doc := durableRouteFile{
		SchemaVersion: DurableRouteSchemaVersion,
		Routes:        make([]durableSessionRoute, 0, len(states)),
	}
	for _, state := range states {
		rec, err := sessionStateToDurable(state)
		if err != nil {
			return nil, err
		}
		doc.Routes = append(doc.Routes, rec)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeDurableSnapshot validates and reconstructs SessionRouteState records.
func DecodeDurableSnapshot(raw []byte) ([]SessionRouteState, error) {
	var doc durableRouteFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: durable snapshot: %v", ErrRouteSnapshotInvalid, err)
	}
	if doc.SchemaVersion < minDurableRouteSchemaVersion || doc.SchemaVersion > DurableRouteSchemaVersion {
		return nil, fmt.Errorf("%w: unsupported schema_version %d", ErrRouteSnapshotInvalid, doc.SchemaVersion)
	}
	seenSession := map[string]struct{}{}
	seenRoute := map[string]struct{}{}
	out := make([]SessionRouteState, 0, len(doc.Routes))
	for _, rec := range doc.Routes {
		state, err := durableToSessionState(rec)
		if err != nil {
			return nil, err
		}
		sid := state.Binding.SessionID
		if _, ok := seenSession[sid]; ok {
			return nil, fmt.Errorf("%w: duplicate session_id", ErrRouteSnapshotInvalid)
		}
		seenSession[sid] = struct{}{}
		if rid := state.Binding.RouteID; rid != "" {
			if _, ok := seenRoute[rid]; ok {
				return nil, fmt.Errorf("%w: duplicate route_id", ErrRouteSnapshotInvalid)
			}
			seenRoute[rid] = struct{}{}
		}
		out = append(out, state)
	}
	return out, nil
}

// ReplaceSnapshot replaces all Session route state with a prior Snapshot()
// clone. Used to roll back Owner mutate+persist transactions. In-flight leases
// for routes that no longer exist are dropped; surviving route IDs keep leases.
func (t *RouteTable) ReplaceSnapshot(states []SessionRouteState) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	bySession := map[string]SessionRouteState{}
	byRoute := map[string]string{}
	for _, state := range states {
		state = cloneSessionState(state)
		sid := state.Binding.SessionID
		bySession[sid] = state
		if rid := state.Binding.RouteID; rid != "" {
			byRoute[rid] = sid
		}
	}
	for rid := range t.inFlight {
		if _, ok := byRoute[rid]; !ok {
			delete(t.inFlight, rid)
		}
	}
	t.bySession = bySession
	t.byRoute = byRoute
}

// Snapshot returns a clone of all Session route states for persistence.
func (t *RouteTable) Snapshot() []SessionRouteState {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]SessionRouteState, 0, len(t.bySession))
	for _, state := range t.bySession {
		out = append(out, cloneSessionState(state))
	}
	return out
}

// Restore replaces an empty RouteTable from durable SessionRouteState records.
// Requires a daemon verifier (argument or table-owned) and re-verifies each
// binding's client/upstream IDs, envelopes, and history domain. CredentialReady
// is recalculated; persisted readiness is ignored.
func (t *RouteTable) Restore(states []SessionRouteState, verifier ProfileContractVerifier) error {
	if t == nil {
		return fmt.Errorf("route table is not configured")
	}
	if verifier == nil {
		verifier = t.contractVerifier()
	}
	if verifier == nil {
		return fmt.Errorf("%w: restore requires daemon verifier", ErrContractUnverified)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.bySession) != 0 || len(t.byRoute) != 0 {
		return fmt.Errorf("%w: route table is not empty", ErrRouteSnapshotInvalid)
	}
	bySession := map[string]SessionRouteState{}
	byRoute := map[string]string{}
	for _, state := range states {
		if err := validateRestorableState(state); err != nil {
			return err
		}
		profile := profileFromBinding(state.Binding)
		admitted, err := AuthorizeProfileContract(profile, ContractAuth{Verifier: verifier})
		if err != nil {
			return fmt.Errorf("%w: restore reverify: %v", ErrRouteSnapshotInvalid, err)
		}
		if err := assertPersistedMatchesAdmitted(state.Binding, admitted); err != nil {
			return fmt.Errorf("%w: %v", ErrRouteSnapshotInvalid, err)
		}
		launchedProfile := profileFromBinding(state.Launched)
		launchedAdmitted, err := AuthorizeProfileContract(launchedProfile, ContractAuth{Verifier: verifier})
		if err != nil {
			return fmt.Errorf("%w: launched reverify: %v", ErrRouteSnapshotInvalid, err)
		}
		if err := assertPersistedMatchesAdmitted(state.Launched, launchedAdmitted); err != nil {
			return fmt.Errorf("%w: launched %v", ErrRouteSnapshotInvalid, err)
		}
		state.Binding.CredentialReady = AuthReady(state.Binding.AuthMode, state.Binding.CredentialEnv, t.lookup)
		state.History = trimHistory(state.History)
		sid := state.Binding.SessionID
		if _, ok := bySession[sid]; ok {
			return fmt.Errorf("%w: duplicate session_id", ErrRouteSnapshotInvalid)
		}
		if rid := state.Binding.RouteID; rid != "" {
			if _, ok := byRoute[rid]; ok {
				return fmt.Errorf("%w: duplicate route_id", ErrRouteSnapshotInvalid)
			}
			byRoute[rid] = sid
		}
		bySession[sid] = cloneSessionState(state)
	}
	t.bySession = bySession
	t.byRoute = byRoute
	return nil
}

func profileFromBinding(b RouteBinding) Profile {
	return Profile{
		ID:                    b.ProfileID,
		Name:                  firstNonEmpty(b.ProfileName, b.ProfileID),
		ExecutorID:            b.ExecutorID,
		ProviderID:            b.ProviderID,
		ProviderLabel:         firstNonEmpty(b.ProviderLabel, b.ProviderID),
		Protocol:              b.Protocol,
		ClientModel:           b.ClientModel,
		ClientModelProvenance: b.ClientModelProvenance,
		Model:                 b.UpstreamModel,
		BaseURL:               b.UpstreamBaseURL,
		AuthMode:              b.AuthMode,
		CredentialEnv:         b.CredentialEnv,
	}
}

func assertPersistedMatchesAdmitted(b RouteBinding, admitted VerifiedProfileContract) error {
	if b.ClientModel != admitted.ClientModelID {
		return fmt.Errorf("persisted client_model drift")
	}
	if b.UpstreamModel != admitted.UpstreamModelID {
		return fmt.Errorf("persisted upstream model drift")
	}
	if normalizeID(b.ClientModelProvenance) != normalizeID(admitted.Provenance) {
		return fmt.Errorf("persisted provenance drift")
	}
	if b.HistoryDomain != admitted.HistoryDomain {
		return fmt.Errorf("persisted history_domain drift")
	}
	if !envelopesEqual(b.ClientEnvelope, admitted.ClientEnvelope) {
		return fmt.Errorf("persisted client envelope drift")
	}
	if !envelopesEqual(b.UpstreamEnvelope, admitted.UpstreamEnvelope) {
		return fmt.Errorf("persisted upstream envelope drift")
	}
	return nil
}

func validateRestorableState(state SessionRouteState) error {
	b := state.Binding
	if strings.TrimSpace(b.SessionID) == "" {
		return fmt.Errorf("%w: session_id required", ErrRouteSnapshotInvalid)
	}
	if state.Generation <= 0 || b.Generation <= 0 {
		return fmt.Errorf("%w: generation must be positive", ErrRouteSnapshotInvalid)
	}
	if state.Generation != b.Generation {
		return fmt.Errorf("%w: generation mismatch", ErrRouteSnapshotInvalid)
	}
	if normalizeID(b.ExecutorID) == "" || normalizeID(b.ProfileID) == "" {
		return fmt.Errorf("%w: executor_id and profile_id required", ErrRouteSnapshotInvalid)
	}
	if err := ValidateModelID(b.ClientModel); err != nil {
		return fmt.Errorf("%w: client_model: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := RequireContractProvenance(b.ClientModelProvenance); err != nil {
		return fmt.Errorf("%w: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := ValidateModelID(b.UpstreamModel); err != nil {
		return fmt.Errorf("%w: model: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := ValidateAuthMode(b.AuthMode, b.CredentialEnv, b.UpstreamBaseURL, b.Protocol); err != nil {
		return fmt.Errorf("%w: auth: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := validateEnvelope(b.ClientEnvelope); err != nil {
		return fmt.Errorf("%w: client envelope: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := validateEnvelope(b.UpstreamEnvelope); err != nil {
		return fmt.Errorf("%w: upstream envelope: %v", ErrRouteSnapshotInvalid, err)
	}
	switch normalizeID(b.HistoryState) {
	case HistoryStateEmpty, HistoryStateMayContainOpaque:
	default:
		return fmt.Errorf("%w: invalid history_state", ErrRouteSnapshotInvalid)
	}
	if strings.TrimSpace(b.HistoryDomain) == "" {
		return fmt.Errorf("%w: history_domain required", ErrRouteSnapshotInvalid)
	}
	if err := validateLaunchedAgainstCurrent(state); err != nil {
		return err
	}
	routeProtocol, needsRoute := RouteProtocolFor(b.Protocol)
	if needsRoute {
		if strings.TrimSpace(b.RouteID) == "" {
			return fmt.Errorf("%w: routed binding missing route_id (will not allocate replacement)", ErrRouteSnapshotInvalid)
		}
		if b.RouteProtocol == "" || b.RouteProtocol != routeProtocol {
			return fmt.Errorf("%w: route_protocol mismatch", ErrRouteSnapshotInvalid)
		}
		if err := ValidateUpstreamBaseURL(b.UpstreamBaseURL); err != nil {
			return fmt.Errorf("%w: upstream: %v", ErrRouteSnapshotInvalid, err)
		}
	} else if b.RouteID != "" || b.RouteProtocol != "" {
		return fmt.Errorf("%w: non-routed binding must not carry route identity", ErrRouteSnapshotInvalid)
	}
	return nil
}

// validateLaunchedAgainstCurrent enforces the immutable launch identity shared
// with the current binding (session/route/protocol/executor/client contract)
// while allowing later Provider/Profile/upstream drift on activation.
func validateLaunchedAgainstCurrent(state SessionRouteState) error {
	l := state.Launched
	b := state.Binding
	if strings.TrimSpace(l.ProfileID) == "" || strings.TrimSpace(l.ClientModel) == "" {
		return fmt.Errorf("%w: launched binding required", ErrRouteSnapshotInvalid)
	}
	if strings.TrimSpace(l.SessionID) == "" {
		return fmt.Errorf("%w: launched session_id required", ErrRouteSnapshotInvalid)
	}
	if l.SessionID != b.SessionID {
		return fmt.Errorf("%w: launched session_id drift", ErrRouteSnapshotInvalid)
	}
	if l.RouteID != b.RouteID {
		return fmt.Errorf("%w: launched route_id drift", ErrRouteSnapshotInvalid)
	}
	if normalizeID(l.RouteProtocol) != normalizeID(b.RouteProtocol) {
		return fmt.Errorf("%w: launched route_protocol drift", ErrRouteSnapshotInvalid)
	}
	if normalizeID(l.ExecutorID) != normalizeID(b.ExecutorID) {
		return fmt.Errorf("%w: launched executor drift", ErrRouteSnapshotInvalid)
	}
	if normalizeID(l.Protocol) != normalizeID(b.Protocol) {
		return fmt.Errorf("%w: launched protocol drift", ErrRouteSnapshotInvalid)
	}
	if l.ClientModel != b.ClientModel {
		return fmt.Errorf("%w: launched client_model drift", ErrRouteSnapshotInvalid)
	}
	if normalizeID(l.ClientModelProvenance) != normalizeID(b.ClientModelProvenance) {
		return fmt.Errorf("%w: launched provenance drift", ErrRouteSnapshotInvalid)
	}
	if !envelopesEqual(l.ClientEnvelope, b.ClientEnvelope) {
		return fmt.Errorf("%w: launched client envelope drift", ErrRouteSnapshotInvalid)
	}
	if l.Generation != 1 {
		return fmt.Errorf("%w: launched generation must be 1", ErrRouteSnapshotInvalid)
	}
	if normalizeID(l.Activation) != ActivationLaunch {
		return fmt.Errorf("%w: launched activation must be %s", ErrRouteSnapshotInvalid, ActivationLaunch)
	}
	if err := ValidateModelID(l.ClientModel); err != nil {
		return fmt.Errorf("%w: launched client_model: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := RequireContractProvenance(l.ClientModelProvenance); err != nil {
		return fmt.Errorf("%w: launched provenance: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := ValidateModelID(l.UpstreamModel); err != nil {
		return fmt.Errorf("%w: launched upstream model: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := validateEnvelope(l.ClientEnvelope); err != nil {
		return fmt.Errorf("%w: launched client envelope: %v", ErrRouteSnapshotInvalid, err)
	}
	if err := validateEnvelope(l.UpstreamEnvelope); err != nil {
		return fmt.Errorf("%w: launched upstream envelope: %v", ErrRouteSnapshotInvalid, err)
	}
	return nil
}

func sessionStateToDurable(state SessionRouteState) (durableSessionRoute, error) {
	if err := validateRestorableState(state); err != nil {
		return durableSessionRoute{}, err
	}
	b := state.Binding
	rec := durableSessionRoute{
		SessionID:             b.SessionID,
		RouteID:               b.RouteID,
		ExecutorID:            b.ExecutorID,
		ProfileID:             b.ProfileID,
		ProfileName:           b.ProfileName,
		RouteProtocol:         b.RouteProtocol,
		ProviderID:            b.ProviderID,
		ProviderLabel:         b.ProviderLabel,
		Protocol:              b.Protocol,
		ClientModel:           b.ClientModel,
		ClientModelProvenance: b.ClientModelProvenance,
		UpstreamBaseURL:       b.UpstreamBaseURL,
		UpstreamModel:         b.UpstreamModel,
		HistoryDomain:         b.HistoryDomain,
		HistoryState:          b.HistoryState,
		HistoryPortability:    b.HistoryPortability,
		ClientEnvelope:        envelopeToDurable(b.ClientEnvelope),
		UpstreamEnvelope:      envelopeToDurable(b.UpstreamEnvelope),
		AuthMode:              b.AuthMode,
		CredentialEnv:         b.CredentialEnv,
		CredentialRef:         b.CredentialRef,
		Generation:            b.Generation,
		CatalogRevision:       b.CatalogRevision,
		Activation:            b.Activation,
	}
	for _, event := range trimHistory(state.History) {
		item := durableHistoryEvent{
			Generation:         event.Generation,
			Activation:         event.Activation,
			HistoryDegradation: event.HistoryDegradation,
			To:                 bindingToDurableFields(event.To),
		}
		if event.Activation != ActivationLaunch && (event.From.RouteID != "" || event.From.SessionID != "" || event.From.Generation != 0) {
			from := bindingToDurableFields(event.From)
			item.From = &from
		}
		rec.History = append(rec.History, item)
	}
	launched := bindingToDurableFields(state.Launched)
	rec.Launched = &launched
	return rec, nil
}

func durableToSessionState(rec durableSessionRoute) (SessionRouteState, error) {
	binding := RouteBinding{
		RouteID:               strings.TrimSpace(rec.RouteID),
		SessionID:             strings.TrimSpace(rec.SessionID),
		ExecutorID:            normalizeID(rec.ExecutorID),
		ProfileID:             normalizeID(rec.ProfileID),
		ProfileName:           normalizeSpace(rec.ProfileName),
		RouteProtocol:         normalizeID(rec.RouteProtocol),
		ProviderID:            normalizeID(rec.ProviderID),
		ProviderLabel:         normalizeSpace(rec.ProviderLabel),
		Protocol:              normalizeID(rec.Protocol),
		ClientModel:           normalizeSpace(rec.ClientModel),
		ClientModelProvenance: normalizeID(rec.ClientModelProvenance),
		UpstreamBaseURL:       normalizeSpace(rec.UpstreamBaseURL),
		UpstreamModel:         normalizeSpace(rec.UpstreamModel),
		HistoryDomain:         normalizeSpace(rec.HistoryDomain),
		HistoryState:          normalizeID(rec.HistoryState),
		HistoryPortability:    normalizeID(rec.HistoryPortability),
		ClientEnvelope:        durableToEnvelope(rec.ClientEnvelope),
		UpstreamEnvelope:      durableToEnvelope(rec.UpstreamEnvelope),
		AuthMode:              normalizeID(rec.AuthMode),
		CredentialEnv:         normalizeSpace(rec.CredentialEnv),
		CredentialRef:         firstNonEmpty(normalizeSpace(rec.CredentialRef), CredentialRefFor(rec.ProfileID)),
		CredentialReady:       false,
		Generation:            rec.Generation,
		CatalogRevision:       rec.CatalogRevision,
		Activation:            normalizeSpace(rec.Activation),
	}
	if binding.AuthMode == "" {
		binding.AuthMode = AuthModeNone
	}
	if binding.HistoryState == "" {
		binding.HistoryState = HistoryStateEmpty
	}
	state := SessionRouteState{Binding: binding, Generation: rec.Generation}
	for _, event := range rec.History {
		item := RouteActivationEvent{
			Generation:         event.Generation,
			Activation:         event.Activation,
			HistoryDegradation: normalizeID(event.HistoryDegradation),
			To:                 durableFieldsToBinding(event.To),
		}
		if event.From != nil {
			item.From = durableFieldsToBinding(*event.From)
		}
		state.History = append(state.History, item)
	}
	switch {
	case rec.Launched != nil:
		state.Launched = durableFieldsToBinding(*rec.Launched)
	default:
		recovered := false
		for _, event := range state.History {
			if normalizeID(event.Activation) == ActivationLaunch {
				state.Launched = event.To
				recovered = true
				break
			}
		}
		if !recovered {
			return SessionRouteState{}, fmt.Errorf("%w: missing durable launched binding", ErrRouteSnapshotInvalid)
		}
	}
	state.History = trimHistory(state.History)
	if err := validateRestorableState(state); err != nil {
		return SessionRouteState{}, err
	}
	return state, nil
}

func envelopeToDurable(e CapabilityEnvelope) durableEnvelope {
	e = normalizeEnvelope(e)
	return durableEnvelope{
		ContextWindowTokens: e.ContextWindowTokens,
		ReasoningClass:      e.ReasoningClass,
		ThinkingClass:       e.ThinkingClass,
		ToolClass:           e.ToolClass,
		Modalities:          append([]string{}, e.Modalities...),
	}
}

func durableToEnvelope(e durableEnvelope) CapabilityEnvelope {
	return normalizeEnvelope(CapabilityEnvelope{
		ContextWindowTokens: e.ContextWindowTokens,
		ReasoningClass:      e.ReasoningClass,
		ThinkingClass:       e.ThinkingClass,
		ToolClass:           e.ToolClass,
		Modalities:          append([]string{}, e.Modalities...),
	})
}

func bindingToDurableFields(b RouteBinding) durableRouteFields {
	return durableRouteFields{
		SessionID:             b.SessionID,
		RouteID:               b.RouteID,
		ExecutorID:            b.ExecutorID,
		ProfileID:             b.ProfileID,
		ProfileName:           b.ProfileName,
		RouteProtocol:         b.RouteProtocol,
		ProviderID:            b.ProviderID,
		ProviderLabel:         b.ProviderLabel,
		Protocol:              b.Protocol,
		ClientModel:           b.ClientModel,
		ClientModelProvenance: b.ClientModelProvenance,
		UpstreamBaseURL:       b.UpstreamBaseURL,
		UpstreamModel:         b.UpstreamModel,
		HistoryDomain:         b.HistoryDomain,
		HistoryState:          b.HistoryState,
		HistoryPortability:    b.HistoryPortability,
		ClientEnvelope:        envelopeToDurable(b.ClientEnvelope),
		UpstreamEnvelope:      envelopeToDurable(b.UpstreamEnvelope),
		AuthMode:              b.AuthMode,
		CredentialEnv:         b.CredentialEnv,
		CredentialRef:         b.CredentialRef,
		Generation:            b.Generation,
		CatalogRevision:       b.CatalogRevision,
		Activation:            b.Activation,
	}
}

func durableFieldsToBinding(f durableRouteFields) RouteBinding {
	auth := normalizeID(f.AuthMode)
	if auth == "" {
		auth = AuthModeNone
	}
	hs := normalizeID(f.HistoryState)
	if hs == "" {
		hs = HistoryStateEmpty
	}
	ref := normalizeSpace(f.CredentialRef)
	if ref == "" {
		ref = CredentialRefFor(f.ProfileID)
	}
	return RouteBinding{
		SessionID:             strings.TrimSpace(f.SessionID),
		RouteID:               strings.TrimSpace(f.RouteID),
		ExecutorID:            normalizeID(f.ExecutorID),
		ProfileID:             normalizeID(f.ProfileID),
		ProfileName:           normalizeSpace(f.ProfileName),
		RouteProtocol:         normalizeID(f.RouteProtocol),
		ProviderID:            normalizeID(f.ProviderID),
		ProviderLabel:         normalizeSpace(f.ProviderLabel),
		Protocol:              normalizeID(f.Protocol),
		ClientModel:           normalizeSpace(f.ClientModel),
		ClientModelProvenance: normalizeID(f.ClientModelProvenance),
		UpstreamBaseURL:       normalizeSpace(f.UpstreamBaseURL),
		UpstreamModel:         normalizeSpace(f.UpstreamModel),
		HistoryDomain:         normalizeSpace(f.HistoryDomain),
		HistoryState:          hs,
		HistoryPortability:    normalizeID(f.HistoryPortability),
		ClientEnvelope:        durableToEnvelope(f.ClientEnvelope),
		UpstreamEnvelope:      durableToEnvelope(f.UpstreamEnvelope),
		AuthMode:              auth,
		CredentialEnv:         normalizeSpace(f.CredentialEnv),
		CredentialRef:         ref,
		Generation:            f.Generation,
		CatalogRevision:       f.CatalogRevision,
		Activation:            normalizeSpace(f.Activation),
	}
}

// RouteStateFile is the daemon-owned durable owner for route snapshots.
//
// Stage 2B integration seam: Session lifecycle Save/Load through this type.
type RouteStateFile struct {
	path    string
	dirSync func(string) error
	hook    func(phase string) error
}

// NewRouteStateFile constructs a route-state persistence owner at path.
func NewRouteStateFile(path string) (*RouteStateFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: route state path is required", ErrInvalid)
	}
	return &RouteStateFile{path: path}, nil
}

// Path returns the durable file path.
func (f *RouteStateFile) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// Save encodes table.Snapshot() to the durable 0600 file.
func (f *RouteStateFile) Save(table *RouteTable) error {
	if f == nil {
		return fmt.Errorf("route state file is not configured")
	}
	return f.SaveStates(table.Snapshot())
}

// SaveStates persists an already-cloned snapshot. Callers that mutate under a
// global Owner lock should snapshot once and pass that clone here so a slow
// write cannot race a newer in-memory generation.
func (f *RouteStateFile) SaveStates(states []SessionRouteState) error {
	if f == nil {
		return fmt.Errorf("route state file is not configured")
	}
	if f.hook != nil {
		if err := f.hook("before_encode"); err != nil {
			return err
		}
	}
	raw, err := EncodeDurableSnapshot(states)
	if err != nil {
		return err
	}
	if f.hook != nil {
		if err := f.hook("after_encode"); err != nil {
			return err
		}
	}
	return f.atomicWrite(raw)
}

// SetPersistHook installs a test failpoint/serialization seam. Phases:
// before_encode, after_encode, before_write, after_write, before_sync, after_sync,
// before_rename, after_rename, before_dirsync, after_dirsync.
func (f *RouteStateFile) SetPersistHook(hook func(phase string) error) {
	if f == nil {
		return
	}
	f.hook = hook
}

// SetDirSync installs a test seam for directory fsync after rename. A failure
// here means the named file already reflects the mutation (ErrPersistDirSync).
func (f *RouteStateFile) SetDirSync(fn func(dir string) error) {
	if f == nil {
		return
	}
	f.dirSync = fn
}

// Load decodes the durable file and Restores into an empty table using the
// table's installed contract verifier.
func (f *RouteStateFile) Load(table *RouteTable) error {
	if f == nil {
		return fmt.Errorf("route state file is not configured")
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		return err
	}
	return table.Restore(states, table.contractVerifier())
}

func (f *RouteStateFile) atomicWrite(data []byte) error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if f.hook != nil {
		if err := f.hook("before_write"); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".route-bindings-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if f.hook != nil {
		if err := f.hook("after_write"); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if f.hook != nil {
		if err := f.hook("before_sync"); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if f.hook != nil {
		if err := f.hook("after_sync"); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if f.hook != nil {
		if err := f.hook("before_rename"); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return err
	}
	cleanup = false
	// From here the named file is in the namespace: failures are applied but
	// durability-unconfirmed (ErrPersistDirSync), never a silent not-applied error.
	if f.hook != nil {
		if err := f.hook("after_rename"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	if f.hook != nil {
		if err := f.hook("before_dirsync"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	if err := f.syncParentDir(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
	}
	if f.hook != nil {
		if err := f.hook("after_dirsync"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	return nil
}

func (f *RouteStateFile) syncParentDir(dir string) error {
	if f != nil && f.dirSync != nil {
		return f.dirSync(dir)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()
	return dirFile.Sync()
}
