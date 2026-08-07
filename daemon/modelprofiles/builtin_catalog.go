package modelprofiles

import "sort"

// Builtin client compatibility catalog for production contract admission.
// Profile TOML never authorizes envelopes. Daemon admits known Codex/Claude
// ClientModel contracts only; Provider/Gateway/upstream model IDs may be any
// ValidateProfile-legal values and inherit the selected client contract envelope
// as a configured compatibility mapping (not discovered upstream capability).

// ClientContractDescriptor is the secret-free App-facing description of one
// daemon-known Codex/Claude client compatibility contract.
type ClientContractDescriptor struct {
	ExecutorID  string             `json:"executor_id"`
	ClientModel string             `json:"client_model"`
	Provenance  string             `json:"provenance"`
	Envelope    CapabilityEnvelope `json:"envelope"`
}

// ProfileEditorSchema is the App-facing vocabulary for the profile editor.
// Provider ID/label, Gateway URL, and upstream model remain freely configurable;
// only client_model must choose a daemon-known contract ID for the executor.
type ProfileEditorSchema struct {
	SupportedClientContracts []ClientContractDescriptor `json:"supported_client_contracts"`
	FreelyConfigurable       []string                   `json:"freely_configurable"`
}

func envelopeCodexGPT5() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 400000,
		ReasoningClass:      ReasoningClassExtended,
		ThinkingClass:       ThinkingClassNone,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText, ModalityImage},
	}
}

func envelopeCodexOSeries() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 200000,
		ReasoningClass:      ReasoningClassExtended,
		ThinkingClass:       ThinkingClassNone,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText},
	}
}

func envelopeClaudeSonnet() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 200000,
		ReasoningClass:      ReasoningClassStandard,
		ThinkingClass:       ThinkingClassExtended,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText, ModalityImage},
	}
}

func envelopeClaudeOpus() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 200000,
		ReasoningClass:      ReasoningClassExtended,
		ThinkingClass:       ThinkingClassExtended,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText, ModalityImage},
	}
}

func envelopeClaudeHaiku() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 200000,
		ReasoningClass:      ReasoningClassStandard,
		ThinkingClass:       ThinkingClassNone,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText, ModalityImage},
	}
}

// builtinClientKey -> daemon-owned ClientModelContract envelope for Codex.
var builtinCodexClients = map[string]CapabilityEnvelope{
	"gpt-5":       envelopeCodexGPT5(),
	"gpt-5.1":     envelopeCodexGPT5(),
	"gpt-5-codex": envelopeCodexGPT5(),
	"o3":          envelopeCodexOSeries(),
	"o4-mini":     envelopeCodexOSeries(),
}

// builtinClientKey -> daemon-owned ClientModelContract envelope for Claude Code.
var builtinClaudeClients = map[string]CapabilityEnvelope{
	"claude-sonnet-4-6": envelopeClaudeSonnet(),
	"claude-sonnet-4-5": envelopeClaudeSonnet(),
	"claude-opus-4-1":   envelopeClaudeOpus(),
	"claude-opus-4":     envelopeClaudeOpus(),
	"claude-haiku-4-5":  envelopeClaudeHaiku(),
}

func lookupBuiltinClient(executorID, clientModel string) (CapabilityEnvelope, bool) {
	switch normalizeID(executorID) {
	case ExecutorCodex:
		env, ok := builtinCodexClients[normalizeSpace(clientModel)]
		return env, ok
	case ExecutorClaude:
		env, ok := builtinClaudeClients[normalizeSpace(clientModel)]
		return env, ok
	default:
		return CapabilityEnvelope{}, false
	}
}

// BuiltinClientContractDescriptors returns the daemon-owned client contract
// vocabulary for App editors (secret-free).
func BuiltinClientContractDescriptors() []ClientContractDescriptor {
	out := make([]ClientContractDescriptor, 0, len(builtinCodexClients)+len(builtinClaudeClients))
	for _, id := range sortedClientKeys(builtinCodexClients) {
		out = append(out, ClientContractDescriptor{
			ExecutorID:  ExecutorCodex,
			ClientModel: id,
			Provenance:  ContractProvenanceConfiguredCompatibility,
			Envelope:    normalizeEnvelope(builtinCodexClients[id]),
		})
	}
	for _, id := range sortedClientKeys(builtinClaudeClients) {
		out = append(out, ClientContractDescriptor{
			ExecutorID:  ExecutorClaude,
			ClientModel: id,
			Provenance:  ContractProvenanceConfiguredCompatibility,
			Envelope:    normalizeEnvelope(builtinClaudeClients[id]),
		})
	}
	return out
}

func sortedClientKeys(m map[string]CapabilityEnvelope) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ProfileEditorSchemaSnapshot returns the App-facing editor vocabulary.
func ProfileEditorSchemaSnapshot() ProfileEditorSchema {
	return ProfileEditorSchema{
		SupportedClientContracts: BuiltinClientContractDescriptors(),
		FreelyConfigurable: []string{
			"provider_id",
			"provider_label",
			"base_url",
			"model",
			"auth_mode",
			"credential_env",
			"name",
		},
	}
}
