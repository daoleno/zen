package modelprofiles

import (
	"fmt"
	"strings"
)

// Curated Provider presets. Auth mode / credential env / protocol / client
// contract are compiled internally and never appear on the public preset DTO.

type presetSpec struct {
	Public         ProviderPreset
	ProviderID     string // internal durable provider identity; not public
	DefaultBaseURL string // internal only — not projected on ordinary preset DTO
	Protocol       string
	AuthMode       string
	CredentialEnv  string
	ClientModel    map[string]string // executor -> default client contract id
	DefaultModel   map[string]string // executor -> default upstream model id
	TrustedModels  []string          // bundled model ids for discovery intersect
}

var curatedPresets = []presetSpec{
	{
		Public: ProviderPreset{
			ID: ProviderPresetOpenAI, Label: "OpenAI",
			Clients: []string{ClientCodex},
		},
		ProviderID:     "openai",
		DefaultBaseURL: "https://api.openai.com/v1",
		Protocol:       ProtocolOpenAIResponses,
		AuthMode:       AuthModeBearerEnv,
		CredentialEnv:  "OPENAI_API_KEY",
		ClientModel:    map[string]string{ExecutorCodex: "gpt-5"},
		DefaultModel:   map[string]string{ExecutorCodex: "gpt-5"},
		TrustedModels:  []string{"gpt-5", "gpt-5.1", "gpt-5-codex", "o3", "o4-mini"},
	},
	{
		Public: ProviderPreset{
			ID: ProviderPresetOpenRouter, Label: "OpenRouter",
			Clients: []string{ClientCodex},
		},
		ProviderID:     "openrouter",
		DefaultBaseURL: "https://openrouter.ai/api/v1",
		Protocol:       ProtocolOpenAIResponses,
		AuthMode:       AuthModeBearerEnv,
		CredentialEnv:  "OPENROUTER_API_KEY",
		ClientModel:    map[string]string{ExecutorCodex: "gpt-5"},
		DefaultModel:   map[string]string{ExecutorCodex: "openai/gpt-5"},
		TrustedModels:  []string{"openai/gpt-5", "openai/gpt-5.1", "openai/o3", "openai/o4-mini", "anthropic/claude-sonnet-4"},
	},
	{
		Public: ProviderPreset{
			ID: ProviderPresetAnthropic, Label: "Anthropic",
			Clients: []string{ClientClaude},
		},
		ProviderID:     "anthropic",
		DefaultBaseURL: "https://api.anthropic.com",
		Protocol:       ProtocolAnthropicMessages,
		AuthMode:       AuthModeXAPIKeyEnv,
		CredentialEnv:  "ANTHROPIC_API_KEY",
		ClientModel:    map[string]string{ExecutorClaude: "claude-sonnet-4-6"},
		DefaultModel:   map[string]string{ExecutorClaude: "claude-sonnet-4-6"},
		TrustedModels:  []string{"claude-sonnet-4-6", "claude-sonnet-4-5", "claude-opus-4-1", "claude-opus-4", "claude-haiku-4-5"},
	},
	{
		Public: ProviderPreset{
			ID: ProviderPresetDeepSeek, Label: "DeepSeek",
			Clients: []string{ClientCodex, ClientClaude},
		},
		ProviderID:     "deepseek",
		DefaultBaseURL: "https://api.deepseek.com",
		Protocol:       ProtocolOpenAIResponses,
		AuthMode:       AuthModeBearerEnv,
		CredentialEnv:  "DEEPSEEK_API_KEY",
		ClientModel: map[string]string{
			ExecutorCodex:  "gpt-5",
			ExecutorClaude: "claude-sonnet-4-6",
		},
		DefaultModel: map[string]string{
			ExecutorCodex:  "deepseek-v4-flash",
			ExecutorClaude: "deepseek-v4-flash",
		},
		TrustedModels: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
	},
	{
		Public: ProviderPreset{
			ID: ProviderPresetCustom, Label: "Custom Gateway",
			Clients:  []string{ClientCodex, ClientClaude},
			Advanced: true,
		},
		ProviderID:     "custom",
		DefaultBaseURL: "",
		Protocol:       "",
		AuthMode:       AuthModeBearerEnv,
		CredentialEnv:  "ZEN_PROVIDER_API_KEY",
		ClientModel: map[string]string{
			ExecutorCodex:  "gpt-5",
			ExecutorClaude: "claude-sonnet-4-6",
		},
		DefaultModel:  map[string]string{},
		TrustedModels: nil,
	},
}

// ListProviderPresets returns curated public presets (no Base URL / auth / protocol).
func ListProviderPresets() []ProviderPreset {
	out := make([]ProviderPreset, 0, len(curatedPresets))
	for _, p := range curatedPresets {
		out = append(out, p.Public)
	}
	return out
}

func lookupPreset(id string) (presetSpec, bool) {
	id = normalizeID(id)
	for _, p := range curatedPresets {
		if normalizeID(p.Public.ID) == id {
			return p, true
		}
	}
	return presetSpec{}, false
}

func presetSupportsClient(spec presetSpec, client string) bool {
	client = clientFromExecutor(client)
	for _, c := range spec.Public.Clients {
		if normalizeID(c) == client || executorFromClient(c) == executorFromClient(client) {
			return true
		}
	}
	return false
}

// CompileProviderConnection builds a durable account-scoped connection from a
// public input. Per-client Profile targets are compiled via CompileConnectionTarget.
func CompileProviderConnection(in ProviderConnectionInput) (Profile, error) {
	in.ID = normalizeID(in.ID)
	in.Name = normalizeSpace(in.Name)
	in.Client = normalizeID(in.Client)
	in.Executor = normalizeID(in.Executor) // legacy alias → client
	in.PresetID = normalizeID(in.PresetID)
	in.ModelID = normalizeSpace(in.ModelID)
	in.BaseURL = normalizeSpace(in.BaseURL)
	if in.PresetID == "" {
		in.PresetID = ProviderPresetCustom
		in.Advanced = true
	}
	spec, ok := lookupPreset(in.PresetID)
	if !ok {
		return Profile{}, fmt.Errorf("%w: unknown provider preset %q", ErrInvalid, in.PresetID)
	}
	if in.Name == "" {
		in.Name = spec.Public.Label
	}
	if in.Name == "" {
		return Profile{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}

	// Custom/Advanced may require a client hint to pick protocol defaults for
	// the durable base URL shape; curated multi-client presets do not.
	hint := executorFromClient(firstNonEmpty(in.Client, in.Executor))
	if normalizeID(in.PresetID) == ProviderPresetCustom || in.Advanced {
		if hint == "" {
			hint = ExecutorCodex
		}
		if !presetSupportsClient(spec, hint) {
			return Profile{}, fmt.Errorf("%w: preset %q does not support client %s", ErrInvalid, in.PresetID, hint)
		}
	} else if hint != "" && !presetSupportsClient(spec, hint) {
		return Profile{}, fmt.Errorf("%w: preset %q does not support client %s", ErrInvalid, in.PresetID, hint)
	}

	baseURL := spec.DefaultBaseURL
	credEnv := spec.CredentialEnv
	if normalizeID(in.PresetID) == ProviderPresetDeepSeek {
		baseURL = "https://api.deepseek.com"
		credEnv = "DEEPSEEK_API_KEY"
	}
	advanced := in.Advanced || normalizeID(in.PresetID) == ProviderPresetCustom
	if advanced {
		if in.BaseURL == "" {
			return Profile{}, fmt.Errorf("%w: advanced connection requires base_url", ErrInvalid)
		}
		baseURL = in.BaseURL
	} else if in.BaseURL != "" && strings.TrimRight(in.BaseURL, "/") != strings.TrimRight(baseURL, "/") {
		advanced = true
		baseURL = in.BaseURL
	}

	// Curated account identity never owns a model. Advanced/Custom may store a
	// manual model id for discovery/default hints only.
	modelID := ""
	if advanced {
		modelID = in.ModelID
		if modelID != "" {
			if err := ValidateModelID(modelID); err != nil {
				return Profile{}, fmt.Errorf("%w: model: %v", ErrInvalid, err)
			}
		}
	} else if in.ModelID != "" {
		return Profile{}, fmt.Errorf("%w: curated connections do not own model_id; select models via defaults or Session activation", ErrInvalid)
	}

	id := in.ID
	if id == "" {
		id = synthesizeConnectionID("conn", in.PresetID, in.Name)
	}
	label := spec.Public.Label
	providerID := spec.ProviderID
	if advanced {
		if normalizeID(in.PresetID) == ProviderPresetCustom {
			providerID = "custom"
		}
		if label == "" {
			label = "Custom"
		}
	}

	profile := Profile{
		ID:            id,
		Name:          in.Name,
		Scope:         ConnectionScopeAccount,
		ProviderID:    providerID,
		ProviderLabel: label,
		Model:         modelID,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		AuthMode:      AuthModeNone,
		CredentialEnv: credEnv,
	}
	if err := ValidateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// compileProviderConnectionForClient builds an ephemeral executor-scoped Profile
// for routing/launch from public/account inputs.
func compileProviderConnectionForClient(in ProviderConnectionInput, executor string) (Profile, error) {
	executor = executorFromClient(executor)
	in.PresetID = normalizeID(in.PresetID)
	in.ModelID = normalizeSpace(in.ModelID)
	in.BaseURL = normalizeSpace(in.BaseURL)
	in.Name = normalizeSpace(in.Name)
	in.ID = normalizeID(in.ID)
	if in.PresetID == "" {
		in.PresetID = ProviderPresetCustom
		in.Advanced = true
	}
	spec, ok := lookupPreset(in.PresetID)
	if !ok {
		return Profile{}, fmt.Errorf("%w: unknown provider preset %q", ErrInvalid, in.PresetID)
	}
	if !presetSupportsClient(spec, executor) {
		return Profile{}, fmt.Errorf("%w: preset %q does not support client %s", ErrInvalid, in.PresetID, clientFromExecutor(executor))
	}

	protocol := spec.Protocol
	authMode := spec.AuthMode
	credEnv := spec.CredentialEnv
	baseURL := spec.DefaultBaseURL
	if in.BaseURL != "" {
		baseURL = in.BaseURL
	}
	if normalizeID(in.PresetID) == ProviderPresetDeepSeek {
		credEnv = "DEEPSEEK_API_KEY"
		switch executor {
		case ExecutorCodex:
			protocol = ProtocolOpenAIResponses
			authMode = AuthModeBearerEnv
			if baseURL == "" || baseURL == "https://api.deepseek.com" {
				baseURL = "https://api.deepseek.com"
			}
		case ExecutorClaude:
			protocol = ProtocolAnthropicMessages
			authMode = AuthModeXAPIKeyEnv
			root := strings.TrimRight(baseURL, "/")
			if root == "" || root == "https://api.deepseek.com" {
				baseURL = "https://api.deepseek.com/anthropic"
			} else if !strings.HasSuffix(root, "/anthropic") {
				baseURL = root + "/anthropic"
			}
		}
	}
	if normalizeID(in.PresetID) == ProviderPresetCustom || in.Advanced {
		if baseURL == "" {
			return Profile{}, fmt.Errorf("%w: advanced connection requires base_url", ErrInvalid)
		}
		if protocol == "" || normalizeID(in.PresetID) == ProviderPresetCustom {
			switch executor {
			case ExecutorCodex:
				protocol = ProtocolOpenAIResponses
				authMode = AuthModeBearerEnv
			case ExecutorClaude:
				protocol = ProtocolAnthropicMessages
				authMode = AuthModeXAPIKeyEnv
			}
		}
	}

	clientModel := spec.ClientModel[executor]
	if clientModel == "" {
		return Profile{}, fmt.Errorf("%w: preset missing client contract for %s", ErrInvalid, executor)
	}
	modelID := in.ModelID
	if modelID == "" {
		modelID = spec.DefaultModel[executor]
	}
	if modelID == "" {
		return Profile{}, fmt.Errorf("%w: model_id is required", ErrInvalid)
	}
	if !in.Advanced && normalizeID(in.PresetID) != ProviderPresetCustom {
		if err := requireTrustedOrFail(in.PresetID, modelID); err != nil {
			return Profile{}, err
		}
	}
	if normalizeID(in.PresetID) == ProviderPresetDeepSeek && executor == ExecutorCodex &&
		normalizeSpace(modelID) == "deepseek-v4-pro" && !in.Advanced {
		return Profile{}, fmt.Errorf("%w: deepseek-v4-pro is not yet supported on DeepSeek Responses for Codex; use deepseek-v4-flash", ErrInvalid)
	}

	id := in.ID
	if id == "" {
		id = synthesizeConnectionID(executor, in.PresetID, modelID)
	}
	label := spec.Public.Label
	providerID := spec.ProviderID
	if in.Advanced || normalizeID(in.PresetID) == ProviderPresetCustom {
		providerID = "custom"
		if label == "" {
			label = "Custom"
		}
	}
	if in.Name == "" {
		in.Name = label
	}

	profile := Profile{
		ID:                    id,
		Name:                  in.Name,
		ExecutorID:            executor,
		ProviderID:            providerID,
		ProviderLabel:         label,
		Protocol:              protocol,
		ClientModel:           clientModel,
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		Model:                 modelID,
		BaseURL:               strings.TrimRight(baseURL, "/"),
		AuthMode:              authMode,
		CredentialEnv:         credEnv,
	}
	if err := ValidateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func synthesizeConnectionID(executor, preset, model string) string {
	base := normalizeID(executor) + "-" + normalizeID(preset)
	safe := make([]rune, 0, len(model))
	for _, r := range model {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			safe = append(safe, r)
		} else if r == '/' || r == '.' {
			safe = append(safe, '-')
		}
	}
	m := string(safe)
	if len(m) > 48 {
		m = m[:48]
	}
	if m == "" {
		return base
	}
	return base + "-" + normalizeID(m)
}

func requireTrustedOrFail(presetID, modelID string) error {
	spec, ok := lookupPreset(presetID)
	if !ok {
		return fmt.Errorf("%w: unknown preset", ErrInvalid)
	}
	if len(spec.TrustedModels) == 0 {
		return nil
	}
	for _, id := range spec.TrustedModels {
		if normalizeSpace(id) == normalizeSpace(modelID) {
			return nil
		}
	}
	return fmt.Errorf("%w: model %q is not in the trusted catalog for preset %s (use Advanced for manual ids)", ErrInvalid, modelID, presetID)
}

func presetTrustedModels(presetID string) []string {
	spec, ok := lookupPreset(presetID)
	if !ok {
		return nil
	}
	out := make([]string, len(spec.TrustedModels))
	copy(out, spec.TrustedModels)
	return out
}

func inferPresetID(profile Profile) string {
	pid := normalizeID(profile.ProviderID)
	for _, spec := range curatedPresets {
		if normalizeID(spec.ProviderID) == pid && normalizeID(spec.Public.ID) != ProviderPresetCustom {
			if isAccountConnection(profile) {
				return spec.Public.ID
			}
			for _, c := range spec.Public.Clients {
				if executorFromClient(c) == normalizeID(profile.ExecutorID) {
					return spec.Public.ID
				}
			}
		}
	}
	return ProviderPresetCustom
}
