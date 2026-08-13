// Package modelprofiles owns the daemon Model Profiles catalog, per-Session
// RouteBinding state, the pure launch compiler, the thin same-protocol loopback
// routing runtime, and the durable route-state codec. It never stores or
// returns API-key values.
//
// First-slice executors are Codex (Responses) and Claude Code (Anthropic
// Messages). OpenCode is not part of the public capability/compiler/router
// surface. ClientModelContract (immutable CLI catalog model + envelope) is
// separate from UpstreamModel (mutable route target).
package modelprofiles

import (
	"errors"
	"fmt"
)

// Profile protocols (catalog / capability matrix).
const (
	ProtocolOpenAINative      = "openai_native"
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolAnthropicMessages = "anthropic_messages"
)

// Route protocols spoken by the Zen-owned loopback (same-protocol pass-through).
const (
	RouteProtocolResponses         = "responses"
	RouteProtocolAnthropicMessages = "anthropic_messages"
)

// Opaque-history domains are provider/model-specific strings from a trusted
// verifier (see DeriveOpaqueHistoryDomain). There is no protocol-wide default
// compatibility domain.
const HistoryDomainNone = "none"

const (
	ExecutorCodex  = "codex"
	ExecutorClaude = "claude"
)

// Upstream auth injection modes (secret-free; values resolved at request time).
const (
	AuthModeNone              = "none"
	AuthModeBearerEnv         = "bearer_env"
	AuthModeXAPIKeyEnv        = "x_api_key_env"
	AuthModeNativePassthrough = "native_passthrough"
)

const (
	ActivationDefaultSelection = "default_selection"
	ActivationLaunch           = "launch"
	ActivationActiveSession    = "active_session"
)

// ActiveSwitchRouteBinding is advertised only for routed profile protocols.
const ActiveSwitchRouteBinding = "route_binding"

const (
	ProjectionNativeArgs = "native_args"
	ProjectionEnvBaseURL = "env_base_url"
)

// EnvAnthropicBaseURL is the Claude Code gateway override.
const EnvAnthropicBaseURL = "ANTHROPIC_BASE_URL"

// MaxModelIDLength caps opaque model identifiers (including org/model forms).
const MaxModelIDLength = 256

// MaxRouteHistoryEvents bounds in-memory / durable activation history growth.
const MaxRouteHistoryEvents = 64

// ClientModelContract provenance. Strings never prove catalog metadata; an
// explicit verified source is required. Unverified / empty provenance fails closed.
const (
	ContractProvenanceBuiltinCatalog = "builtin_catalog"
	ContractProvenanceVerifiedAlias  = "verified_alias"
	// ContractProvenanceConfiguredCompatibility means UpstreamEnvelope is the
	// daemon-owned ClientModelContract envelope applied as a configured
	// compatibility mapping — not a claim that upstream capabilities were probed.
	ContractProvenanceConfiguredCompatibility = "configured_compatibility"
)

// Official upstream hosts for AuthModeNativePassthrough are decided by
// isNativePassthroughHost (immutable). Do not export a mutable allowlist map.

var (
	ErrNotFound                   = errors.New("model profile not found")
	ErrConflict                   = errors.New("model profile revision conflict")
	ErrInvalid                    = errors.New("invalid model profile")
	ErrUnsupportedExecutor        = errors.New("executor does not support model profiles")
	ErrUnsupportedProtocol        = errors.New("protocol is not supported for executor")
	ErrDuplicateID                = errors.New("model profile id already exists")
	ErrRouteRequired              = errors.New("zen loopback route url is required")
	ErrBindingNotFound            = errors.New("session route binding not found")
	ErrBindingConflict            = errors.New("session route binding generation conflict")
	ErrBindingExecutorMismatch    = errors.New("route binding cannot change executor")
	ErrBindingSessionRequired     = errors.New("session id is required for route binding")
	ErrBindingNotRouted           = errors.New("session is not routed; active switch unsupported")
	ErrBindingProtocolChange      = errors.New("route protocol class cannot change on active switch")
	ErrBindingContractChange      = errors.New("client model contract cannot change on active switch")
	ErrBindingHistoryDomain       = errors.New("opaque-history compatibility domain mismatch")
	ErrBindingHistoryState        = errors.New("opaque-history state prevents domain change")
	ErrRequestBodyNotPortable     = errors.New("request history is not portable across providers")
	ErrBindingBusy                = errors.New("route has in-flight requests; cross-domain activate denied")
	ErrContractUnverified         = errors.New("profile contract provenance is unverified")
	ErrEnvelopeIncompatible       = errors.New("upstream capability envelope cannot support client envelope")
	ErrCredentialNotReady         = errors.New("credential environment variable is not ready")
	ErrCredentialStoreUnavailable = errors.New("credential store unavailable")
	ErrCredentialStoreFailed      = errors.New("credential store operation failed")
	ErrDiscoveryCacheInvalid      = errors.New("provider discovery cache invalid")
	ErrDiscoveryPersistFailed     = errors.New("provider discovery cache persist failed")
	ErrInternalNotWire            = errors.New("modelprofiles: internal type cannot be JSON-marshaled; use ToWire")
	ErrPersistDirSync             = errors.New("model profile file renamed but directory sync failed")

	ErrRouteNotFound               = errors.New("route not found")
	ErrRouteAdmissionDenied        = errors.New("route admission denied")
	ErrRoutePathMismatch           = errors.New("route path mismatch")
	ErrRouteProtocolMismatch       = errors.New("route protocol mismatch")
	ErrRouteMethodMismatch         = errors.New("route method mismatch")
	ErrRouteWebSocket              = errors.New("websocket upgrade rejected")
	ErrRequestBodyTooLarge         = errors.New("request body too large")
	ErrRequestBodyMalformed        = errors.New("request body malformed")
	ErrResponsesFeatureUnsupported = errors.New("responses feature unsupported by upstream capability envelope")
	ErrUpstreamInvalid             = errors.New("upstream invalid")
	ErrUpstreamSSRF                = errors.New("upstream address blocked")
	ErrUpstreamRedirect            = errors.New("upstream redirect rejected")
	ErrRouteSnapshotInvalid        = errors.New("route snapshot invalid")
	ErrProfileInUse                = errors.New("model profile is in use by a session")
	ErrListenerFailed              = errors.New("route listener failed")
	ErrLaunchCleanupIncomplete     = errors.New("launch cleanup incomplete")
	// ErrSessionStillLive means KillSession failed and the Session is still
	// confirmed present — route ownership must be preserved.
	ErrSessionStillLive = errors.New("session still live after kill failure")
	// ErrSessionLivenessUnknown means a post-kill liveness probe failed or was
	// ambiguous — not proof of absence, so the route must be preserved.
	ErrSessionLivenessUnknown = errors.New("session liveness probe ambiguous")
)

// SessionLiveness is the tri-state probe result used after a non-nil KillSession
// error. It never authorizes route release.
type SessionLiveness int

const (
	SessionLivenessUnknown SessionLiveness = iota
	SessionLivenessPresent
	SessionLivenessAbsent
)

// Profile is durable catalog upstream metadata (secret-free values only).
// Account-scoped Provider connections (Scope=account) belong to exactly one
// product client and omit executor/protocol/client_model/auth_mode; those are
// compiled for that client at launch/activate.
type Profile struct {
	ID    string `toml:"id" json:"id"`
	Name  string `toml:"name" json:"name"`
	Scope string `toml:"scope,omitempty" json:"scope,omitempty"`
	// Client scopes an account connection to exactly one product client.
	Client        string `toml:"client,omitempty" json:"client,omitempty"`
	ExecutorID    string `toml:"executor_id,omitempty" json:"executor_id,omitempty"`
	ProviderID    string `toml:"provider_id" json:"provider_id"`
	ProviderLabel string `toml:"provider_label" json:"provider_label"`
	Protocol      string `toml:"protocol,omitempty" json:"protocol,omitempty"`
	// ClientModel is the CLI-visible model id string from durable catalog input.
	// Establishing a ClientModelContract requires daemon ContractAuth — TOML alone
	// never authorizes this field.
	ClientModel string `toml:"client_model,omitempty" json:"client_model,omitempty"`
	// ClientModelProvenance is optional durable description only (builtin_catalog /
	// verified_alias). User-editable TOML may claim either label; that claim is
	// never treated as authorization by ValidateProfile or contract establishment.
	ClientModelProvenance string `toml:"client_model_provenance,omitempty" json:"client_model_provenance,omitempty"`
	// Model is Advanced/Custom manual upstream model only on durable account
	// connections. Curated account connections leave it empty; defaults and
	// Session activation own model selection. Legacy executor-scoped profiles
	// may still store a catalog model.
	Model         string `toml:"model,omitempty" json:"model,omitempty"`
	BaseURL       string `toml:"base_url,omitempty" json:"base_url,omitempty"`
	AuthMode      string `toml:"auth_mode,omitempty" json:"auth_mode,omitempty"`
	CredentialEnv string `toml:"credential_env,omitempty" json:"credential_env,omitempty"`
	// HistoryDomain is optional durable description only — never authorization.
	HistoryDomain string `toml:"history_domain,omitempty" json:"history_domain,omitempty"`
}

// Catalog is the secret-free view of the durable profile store (settings CRUD).
type Catalog struct {
	Revision int64             `json:"revision"`
	Profiles []Profile         `json:"profiles"`
	Defaults map[string]string `json:"defaults"`
}

// ProfileView is catalog profile plus readiness for settings (never secret values).
type ProfileView struct {
	Profile
	CredentialReady bool `json:"credential_ready"`
}

// ProtocolCapability is profile/routing-specific capability advertisement.
type ProtocolCapability struct {
	Protocol      string `json:"protocol"`
	Routed        bool   `json:"routed"`
	RouteProtocol string `json:"route_protocol,omitempty"`
	ActiveSwitch  string `json:"active_switch,omitempty"`
}

// ExecutorCapabilities advertises launch projection and per-protocol switch support.
type ExecutorCapabilities struct {
	ExecutorID     string               `json:"executor_id"`
	Supported      bool                 `json:"supported"`
	Protocols      []ProtocolCapability `json:"protocols,omitempty"`
	RouteProtocols []string             `json:"route_protocols,omitempty"`
	Projection     string               `json:"projection,omitempty"`
}

// CompileOptions configures Compile, including required contract authorization.
// Supply Verifier or VerifiedProfileContract; Profile TOML claims are never sufficient.
// Credentials is consulted for readiness only; secret values never enter launch env.
type CompileOptions struct {
	LoopbackRouteURL        string
	CatalogRevision         int64
	Lookup                  func(string) (string, bool)
	Credentials             CredentialStore
	Verifier                ProfileContractVerifier
	VerifiedProfileContract VerifiedProfileContract
}

// RouteBinding is daemon-internal per-Session route state.
type RouteBinding struct {
	RouteID               string
	SessionID             string
	ExecutorID            string
	ProfileID             string
	ProfileName           string
	RouteProtocol         string
	ProviderID            string
	ProviderLabel         string
	Protocol              string
	ClientModel           string
	ClientModelProvenance string
	UpstreamBaseURL       string
	UpstreamModel         string
	HistoryDomain         string
	HistoryState          string // empty | may_contain_opaque
	// HistoryPortability is sticky once set (CLI may resent old opaque blocks).
	HistoryPortability string
	ClientEnvelope     CapabilityEnvelope
	UpstreamEnvelope   CapabilityEnvelope
	AuthMode           string
	CredentialEnv      string
	// CredentialRef is an opaque private-store reference (never a secret).
	CredentialRef   string
	CredentialReady bool
	Generation      int64
	CatalogRevision int64
	Activation      string
}

// RouteActivationEvent is daemon-internal append-only history (bounded).
type RouteActivationEvent struct {
	Generation         int64
	Activation         string
	HistoryDegradation string
	From               RouteBinding
	To                 RouteBinding
}

// SessionRouteState is daemon-internal Session route ownership.
// Launched is the immutable original launch binding and is never trimmed with
// activation history (MaxRouteHistoryEvents only bounds History).
type SessionRouteState struct {
	Binding    RouteBinding
	Launched   RouteBinding
	Generation int64
	History    []RouteActivationEvent
}

// WireBinding is the App/control-safe Provider-first projection of a RouteBinding.
// Protocol, client_model, envelopes, auth_mode, credential_env, generation,
// history portability/degradation, provider_id, and route internals are never
// exposed here.
type WireBinding struct {
	SessionID       string `json:"session_id"`
	Client          string `json:"client"`
	ConnectionID    string `json:"connection_id"`
	ConnectionName  string `json:"connection_name"`
	ProviderLabel   string `json:"provider_label,omitempty"`
	ModelID         string `json:"model_id"`
	CredentialReady bool   `json:"credential_ready"`
	HotSwitchable   bool   `json:"hot_switchable"`
}

// WireActivationEvent is retained for internal audit helpers only. Ordinary
// Session projections omit activation history (no generation/degradation).
type WireActivationEvent struct {
	Activation string       `json:"activation"`
	From       *WireBinding `json:"from,omitempty"`
	To         WireBinding  `json:"to"`
}

// WireSessionState is the App-safe Session route projection.
type WireSessionState struct {
	Binding WireBinding `json:"binding"`
}

// ResolvedLaunch is a secret-free compiled launch plan for daemon use.
type ResolvedLaunch struct {
	Command       string
	Env           map[string]string
	NeedsRoute    bool
	RouteProtocol string
	Draft         RouteBinding
	Wire          WireBinding
	// CodexWebSocketNote records empirical WS→POST fallback behavior for the
	// installed Codex under test (not a permanent product version lock).
	CodexWebSocketNote string
}

// SupportsExecutor reports whether the executor participates in Model Profiles.
func SupportsExecutor(executorID string) bool {
	return len(SupportedProtocols(executorID)) > 0
}

// SupportedProtocols returns profile protocols valid for an executor.
func SupportedProtocols(executorID string) []string {
	switch normalizeID(executorID) {
	case ExecutorCodex:
		return []string{ProtocolOpenAINative, ProtocolOpenAIResponses}
	case ExecutorClaude:
		return []string{ProtocolAnthropicMessages}
	default:
		return nil
	}
}

// RouteProtocolFor returns the Zen loopback protocol for a profile protocol.
func RouteProtocolFor(profileProtocol string) (routeProtocol string, ok bool) {
	switch normalizeID(profileProtocol) {
	case ProtocolOpenAIResponses:
		return RouteProtocolResponses, true
	case ProtocolAnthropicMessages:
		return RouteProtocolAnthropicMessages, true
	default:
		return "", false
	}
}

// ProfileHotSwitchable reports whether a profile class supports route activation.
func ProfileHotSwitchable(profileProtocol string) bool {
	_, ok := RouteProtocolFor(profileProtocol)
	return ok
}

// CapabilitiesFor returns honest launch/route/switch capabilities.
func CapabilitiesFor(executorID string) ExecutorCapabilities {
	executorID = normalizeID(executorID)
	protocols := SupportedProtocols(executorID)
	out := ExecutorCapabilities{
		ExecutorID: executorID,
		Supported:  len(protocols) > 0,
	}
	if !out.Supported {
		return out
	}
	for _, protocol := range protocols {
		cap := ProtocolCapability{Protocol: protocol}
		if routeProtocol, ok := RouteProtocolFor(protocol); ok {
			cap.Routed = true
			cap.RouteProtocol = routeProtocol
			cap.ActiveSwitch = ActiveSwitchRouteBinding
			out.RouteProtocols = appendUnique(out.RouteProtocols, routeProtocol)
		}
		out.Protocols = append(out.Protocols, cap)
	}
	switch executorID {
	case ExecutorCodex:
		out.Projection = ProjectionNativeArgs
	case ExecutorClaude:
		out.Projection = ProjectionEnvBaseURL
	}
	return out
}

func appendUnique(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

// ContractFromProfile builds the authorized contract view. Self-declared TOML
// provenance/history/capabilities are not authorization.
func ContractFromProfile(profile Profile, auth ContractAuth) (VerifiedProfileContract, error) {
	return AuthorizeProfileContract(profile, auth)
}

// RequireContractProvenance fails closed unless provenance is an allowed authorized source label.
// This checks vocabulary for daemon-admitted identities; it does not trust TOML self-claims.
func RequireContractProvenance(provenance string) error {
	switch normalizeID(provenance) {
	case ContractProvenanceBuiltinCatalog, ContractProvenanceVerifiedAlias, ContractProvenanceConfiguredCompatibility:
		return nil
	case "":
		return fmt.Errorf("%w: client_model_provenance is required", ErrContractUnverified)
	default:
		return fmt.Errorf("%w: unknown client_model_provenance %q", ErrContractUnverified, provenance)
	}
}

// BindingDraftFromProfile builds internal RouteBinding fields from a profile using
// a daemon-admitted VerifiedProfileContract (never raw TOML claims alone).
func BindingDraftFromProfile(profile Profile, catalogRevision int64, activation string, ready bool, admitted VerifiedProfileContract) (RouteBinding, error) {
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return RouteBinding{}, err
	}
	if err := validateVerifiedProfileContract(admitted); err != nil {
		return RouteBinding{}, err
	}
	routeProtocol, needsRoute := RouteProtocolFor(profile.Protocol)
	if needsRoute && normalizeSpace(profile.BaseURL) == "" {
		return RouteBinding{}, fmt.Errorf("%w: upstream base_url required for routed protocol", ErrInvalid)
	}
	return RouteBinding{
		ExecutorID:            profile.ExecutorID,
		ProfileID:             profile.ID,
		ProfileName:           profile.Name,
		RouteProtocol:         routeProtocol,
		ProviderID:            profile.ProviderID,
		ProviderLabel:         profile.ProviderLabel,
		Protocol:              profile.Protocol,
		ClientModel:           admitted.ClientModelID,
		ClientModelProvenance: admitted.Provenance,
		UpstreamBaseURL:       profile.BaseURL,
		UpstreamModel:         admitted.UpstreamModelID,
		HistoryDomain:         admitted.HistoryDomain,
		HistoryState:          HistoryStateEmpty,
		ClientEnvelope:        admitted.ClientEnvelope,
		UpstreamEnvelope:      admitted.UpstreamEnvelope,
		AuthMode:              profile.AuthMode,
		CredentialEnv:         profile.CredentialEnv,
		CredentialRef:         CredentialRefFor(profile.ID),
		CredentialReady:       ready,
		CatalogRevision:       catalogRevision,
		Activation:            activation,
	}, nil
}

// ToWire projects an internal binding to the App/control Provider-first DTO.
func (b RouteBinding) ToWire() WireBinding {
	return WireBinding{
		SessionID:       b.SessionID,
		Client:          clientFromExecutor(b.ExecutorID),
		ConnectionID:    b.ProfileID,
		ConnectionName:  b.ProfileName,
		ProviderLabel:   b.ProviderLabel,
		ModelID:         b.UpstreamModel,
		CredentialReady: b.CredentialReady,
		HotSwitchable:   b.RouteID != "" && b.RouteProtocol != "",
	}
}

// WireHistory projects internal activation history without generation/degradation.
func WireHistory(history []RouteActivationEvent) []WireActivationEvent {
	if len(history) == 0 {
		return nil
	}
	out := make([]WireActivationEvent, 0, len(history))
	for _, event := range history {
		item := WireActivationEvent{
			Activation: event.Activation,
			To:         event.To.ToWire(),
		}
		if event.Activation != ActivationLaunch &&
			(event.From.SessionID != "" || event.From.ProfileID != "" || event.From.Generation != 0 || event.From.RouteID != "") {
			from := event.From.ToWire()
			item.From = &from
		}
		out = append(out, item)
	}
	return out
}

// ToWire projects SessionRouteState to App/control DTO.
func (s SessionRouteState) ToWire() WireSessionState {
	return WireSessionState{
		Binding: s.Binding.ToWire(),
	}
}

// contractsCompatible enforces immutable client envelope/identity and history rules.
func contractsCompatible(current, next RouteBinding) error {
	if normalizeSpace(current.ClientModel) != normalizeSpace(next.ClientModel) ||
		normalizeID(current.ClientModelProvenance) != normalizeID(next.ClientModelProvenance) {
		return fmt.Errorf("%w: client_model %q -> %q", ErrBindingContractChange, current.ClientModel, next.ClientModel)
	}
	if !envelopesEqual(current.ClientEnvelope, next.ClientEnvelope) {
		return fmt.Errorf("%w: client capability envelope changed", ErrBindingContractChange)
	}
	if err := envelopeSupports(next.UpstreamEnvelope, current.ClientEnvelope); err != nil {
		return fmt.Errorf("%w: %v", ErrEnvelopeIncompatible, err)
	}
	if current.RouteProtocol != next.RouteProtocol {
		return fmt.Errorf("%w: %s -> %s", ErrBindingProtocolChange, current.RouteProtocol, next.RouteProtocol)
	}
	if current.Protocol != next.Protocol {
		return fmt.Errorf("%w: profile protocol %s -> %s", ErrBindingProtocolChange, current.Protocol, next.Protocol)
	}
	switch normalizeID(current.HistoryState) {
	case "", HistoryStateEmpty:
		// Domain may change while history is still empty.
	case HistoryStateMayContainOpaque:
		if current.HistoryDomain != next.HistoryDomain {
			// Lightweight same-protocol portable strip only — never weaken the
			// opaque guard into blind forward of provider-specific state.
			if !portableHistoryProtocolsAllowed(current, next) {
				return fmt.Errorf("%w: opaque history present; domain %q -> %q", ErrBindingHistoryState, current.HistoryDomain, next.HistoryDomain)
			}
		}
	default:
		return fmt.Errorf("%w: unknown history_state %q", ErrInvalid, current.HistoryState)
	}
	return nil
}
