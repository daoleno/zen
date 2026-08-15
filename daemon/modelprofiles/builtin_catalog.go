package modelprofiles

import "sort"

// Builtin client compatibility catalog for production contract admission.
// Profile TOML never authorizes envelopes.
//
// Codex: the daemon-owned versioned model catalog (model_catalog.go) is the
// metadata source. The exact selected model slug is the Codex session model
// AND the routed upstream model — there is no hidden compatibility model.
// Unknown models remain valid opaque passthrough identities.
//
// Claude: daemon-known ClientModel contracts remain the admission source;
// Claude connections already run the exact selected model identity.
//
// Reasoning Effort contracts live on the same model catalog entries. Only
// models with a pinned contract expose an Effort control; unknown models hide
// it (never speculate). Non-Codex executors (Claude) have no effort contract.

// ClientContractDescriptor is the secret-free App-facing description of one
// daemon-known Codex/Claude client compatibility contract.
type ClientContractDescriptor struct {
	ExecutorID  string             `json:"executor_id"`
	ClientModel string             `json:"client_model"`
	Provenance  string             `json:"provenance"`
	Envelope    CapabilityEnvelope `json:"envelope"`
}

// ProfileEditorSchema is the App-facing vocabulary for the profile editor.
// Provider ID/label, Gateway URL, and model remain freely configurable; the
// catalog is suggestions and metadata, not an identity allowlist.
type ProfileEditorSchema struct {
	SupportedClientContracts []ClientContractDescriptor `json:"supported_client_contracts"`
	FreelyConfigurable       []string                   `json:"freely_configurable"`
}

// envelopeCodexGPT5 etc. are retained as legacy family descriptors for the
// pre-catalog Codex client keys. The model catalog is the authoritative
// source; these only back the editor vocabulary for legacy slugs.

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

// builtinClientKey -> daemon-owned ClientModelContract envelope for Claude.
var builtinClaudeClients = map[string]CapabilityEnvelope{
	"claude-sonnet-4-6": envelopeClaudeSonnet(),
	"claude-sonnet-4-5": envelopeClaudeSonnet(),
	"claude-opus-4-1":   envelopeClaudeOpus(),
	"claude-opus-4":     envelopeClaudeOpus(),
	"claude-haiku-4-5":  envelopeClaudeHaiku(),
}

// lookupBuiltinClient resolves the daemon-owned envelope for one client model
// identity. Codex returns conservative opaque metadata for unknown exact slugs;
// Claude resolves through the daemon-known Claude contracts.
func lookupBuiltinClient(executorID, clientModel string) (CapabilityEnvelope, bool) {
	switch normalizeID(executorID) {
	case ExecutorCodex:
		entry, ok := lookupCodexModelMetadata(clientModel)
		if !ok {
			if err := ValidateModelID(clientModel); err != nil {
				return CapabilityEnvelope{}, false
			}
			return opaqueCodexPassthroughEnvelope(), true
		}
		return entry.Envelope, true
	case ExecutorClaude:
		env, ok := builtinClaudeClients[normalizeSpace(clientModel)]
		return env, ok
	default:
		return CapabilityEnvelope{}, false
	}
}

// BuiltinClientContractDescriptors returns the daemon-owned client contract
// vocabulary for App editors (secret-free). Codex descriptors come from the
// versioned model catalog; Claude descriptors from the Claude contracts.
func BuiltinClientContractDescriptors() []ClientContractDescriptor {
	var out []ClientContractDescriptor
	for _, entry := range CodexModelCatalogEntries() {
		envelope := entry.Envelope
		envelope.Modalities = append([]string(nil), entry.Envelope.Modalities...)
		out = append(out, ClientContractDescriptor{
			ExecutorID:  ExecutorCodex,
			ClientModel: entry.Slug,
			Provenance:  firstNonEmpty(entry.Provenance, ContractProvenanceConfiguredCompatibility),
			Envelope:    envelope,
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
