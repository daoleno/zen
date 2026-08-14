package modelprofiles

import "time"

// Public Provider-first wire vocabulary. Profile/protocol/auth_mode/client_model/
// envelope/credential_env/generation/history_domain/degradation remain internal.

// ProviderPresetID identifies a curated Provider template.
const (
	ProviderPresetOpenAI     = "openai"
	ProviderPresetOpenRouter = "openrouter"
	ProviderPresetAnthropic  = "anthropic"
	ProviderPresetDeepSeek   = "deepseek"
	ProviderPresetCustom     = "custom"
)

// DiscoveryTTL is the bounded live-model refresh window.
const DiscoveryTTL = 5 * time.Minute

// ProviderPreset is the App-facing curated Provider template (secret-free).
// Base URLs, auth, protocol, and internal provider_id stay daemon-internal.
type ProviderPreset struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Clients  []string `json:"clients"`
	Advanced bool     `json:"advanced,omitempty"`
}

// ProviderConnection is one Settings-managed Provider account connection.
// Curated public shape: {id,name,preset_id,clients?,credential_ready}.
// BaseURL and ManualModelID appear only for Custom/Advanced.
//
// id is the stable internal identity (never derived from name/URL/key); name
// is the primary user-facing identity and is unique case-insensitively across
// the Provider list. Multiple connections may share the same Base URL with
// different API keys; they are distinguished by id and name, never by URL.
//
// CredentialHint is a conservative masked preview of the active stored secret
// (small bounded prefix/suffix, fixed bullet center); the full key never
// leaves the private credential store and no hint is emitted in logs or
// telemetry. The editable API-key input stays logically empty — the hint is
// presentation only and must never be submitted as a credential.
type ProviderConnection struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	PresetID        string   `json:"preset_id,omitempty"`
	Clients         []string `json:"clients,omitempty"`
	BaseURL         string   `json:"base_url,omitempty"`
	ManualModelID   string   `json:"manual_model_id,omitempty"`
	CredentialReady bool     `json:"credential_ready"`
	CredentialHint  string   `json:"credential_hint,omitempty"`
	Advanced        bool     `json:"advanced,omitempty"`
}

// ProviderDefault is the future-launch default for one product client.
type ProviderDefault struct {
	ConnectionID string `json:"connection_id"`
	ModelID      string `json:"model_id,omitempty"`
}

// ProviderModelEntry is a catalog/discovery model id with availability only.
type ProviderModelEntry struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Source    string `json:"source"` // bundled | discovered | lkg | manual
	// Known marks daemon-owned metadata for managed Codex. Unknown gateway-only
	// models are clearly unsupported (never masqueraded under another identity).
	Known bool `json:"known,omitempty"`
}

// ProviderCatalogProjection is the Settings list payload.
type ProviderCatalogProjection struct {
	Revision    int64                           `json:"revision"`
	Connections []ProviderConnection            `json:"connections"`
	Defaults    map[string]ProviderDefault      `json:"defaults"`
	Presets     []ProviderPreset                `json:"presets"`
	Models      map[string][]ProviderModelEntry `json:"models"`
}

// ProviderConnectionInput is the public mutation shape for create/update.
// Every create is scoped to one client. Advanced/Custom may set base_url and
// model_id; protocol and auth details remain internal.
type ProviderConnectionInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PresetID string `json:"preset_id"`
	Client   string `json:"client"`
	BaseURL  string `json:"base_url,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
	Advanced bool   `json:"advanced,omitempty"`
}

// ProviderSessionSelection is the Plus-menu current-Session projection.
// Ordinary public wire omits provider_id. Reasoning Effort fields mirror
// WireBinding: the current override plus the client model's daemon-owned
// effort contract (absent for unsupported clients/models).
type ProviderSessionSelection struct {
	SessionID              string   `json:"session_id"`
	Client                 string   `json:"client"`
	ConnectionID           string   `json:"connection_id"`
	ConnectionName         string   `json:"connection_name"`
	ProviderLabel          string   `json:"provider_label,omitempty"`
	ModelID                string   `json:"model_id"`
	ReasoningEffort        string   `json:"reasoning_effort,omitempty"`
	ReasoningEffortDefault string   `json:"reasoning_effort_default,omitempty"`
	ReasoningEfforts       []string `json:"reasoning_efforts,omitempty"`
	CredentialReady        bool     `json:"credential_ready"`
	HotSwitchable          bool     `json:"hot_switchable"`
}

// ProviderCredentialResult is the write-only credential mutation reply.
type ProviderCredentialResult struct {
	ConnectionID       string `json:"connection_id"`
	CredentialReady    bool   `json:"credential_ready"`
	PersistenceOutcome string `json:"persistence_outcome"`
	PersistenceDurable bool   `json:"persistence_durable"`
	PersistenceWarning string `json:"persistence_warning,omitempty"`
}

// ProviderConnectionTestInput is a transient, write-free connectivity probe.
// Credential is inbound-only and must never be projected or persisted.
type ProviderConnectionTestInput struct {
	Client     string `json:"client"`
	BaseURL    string `json:"base_url"`
	Credential string `json:"credential"`
}

// ProviderConnectionTestResult contains only secret-free probe facts.
type ProviderConnectionTestResult struct {
	Client     string `json:"client"`
	ModelCount int    `json:"model_count"`
	LatencyMS  int64  `json:"latency_ms"`
}

const (
	ModelSourceBundled    = "bundled"
	ModelSourceDiscovered = "discovered"
	ModelSourceLKG        = "lkg"
	ModelSourceManual     = "manual"
)
