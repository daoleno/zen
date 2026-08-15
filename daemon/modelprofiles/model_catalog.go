package modelprofiles

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

// codexCatalogBaseInstructions is the Codex CLI 0.147 model base instructions
// reference text (codex-rs models-manager/prompt.md, Apache-2.0). The Codex
// ModelInfo contract requires base_instructions (or model_messages.
// instructions_template) on every model_catalog_json entry — the CLI exits at
// config load when it is missing — and this is the exact fallback text Codex
// itself uses for models without per-model instructions, so managed sessions
// keep the stock Codex agent persona.
//
//go:embed codex_catalog_instructions.md
var codexCatalogBaseInstructions string

// ContractProvenanceCodexCatalog marks model metadata pinned from the Codex
// CLI / OpenAI model catalog contract (versioned, evidence-based).
const ContractProvenanceCodexCatalog = "codex_catalog"

// CodexModelMetadata is the daemon-owned, versioned, evidence-based metadata
// for one known Codex model identity. Entries are pinned from the Codex CLI
// model catalog / OpenAI Responses model contract; unknown models resolve
// nowhere and fail closed for managed Codex (never masquerade under another
// identity).
type CodexModelMetadata struct {
	// Slug is the exact model identity — the Codex session model, the routed
	// upstream model, and the UI-visible model are this one slug.
	Slug string
	// DisplayName is the catalog display label (evidence-based when known).
	DisplayName string
	Envelope    CapabilityEnvelope
	// Effort contract (nil = no configurable Reasoning Effort).
	Effort *codexEffortContract
	// Provenance is the daemon-owned evidence label for this entry.
	Provenance string
}

// Codex Reasoning Effort wire vocabulary — the OpenAI Responses API
// `reasoning.effort` values (model-dependent) that Zen admits for Session
// selection. `none` disables reasoning entirely and `ultra` is an
// undocumented ChatGPT-tier preset; neither is offered as a Session effort.
var codexReasoningEffortVocabulary = []string{
	ReasoningEffortMinimal,
	ReasoningEffortLow,
	ReasoningEffortMedium,
	ReasoningEffortHigh,
	ReasoningEffortXHigh,
	ReasoningEffortMax,
}

// codexEffortContract is the daemon-owned per-model effort contract. Values
// mirror the Codex model catalog (`supported_reasoning_levels` /
// `default_reasoning_level`) for each known model. Conservative pinning: a
// value is listed only when the Codex contract for that model documents it;
// the daemon never guesses unsupported choices.
type codexEffortContract struct {
	defaultEffort string
	supported     []string
}

// codexModelCatalog is the daemon-owned versioned Codex model metadata.
// Entries are pinned from the Codex CLI 0.147 model catalog (runtime-fetched
// `ModelsResponse` cache shape, verified on the daemon's reference install),
// the OpenAI Responses reasoning contract, and Codex openai_model_info
// context windows. The exact slug is the single model identity; there is no
// hidden compatibility model.
var codexModelCatalog = []CodexModelMetadata{
	{
		Slug: "gpt-5.6-sol", DisplayName: "GPT-5.6-Sol",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.6-terra", DisplayName: "GPT-5.6-Terra",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.6-luna", DisplayName: "GPT-5.6-Luna",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.5", DisplayName: "GPT-5.5",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.4", DisplayName: "gpt-5.4",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.4-mini", DisplayName: "GPT-5.4-Mini",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.3-codex", DisplayName: "gpt-5.3-codex",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3-Codex-Spark",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 128000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortHigh, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.2-codex", DisplayName: "gpt-5.2-codex",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.2", DisplayName: "gpt-5.2",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.1-codex", DisplayName: "gpt-5.1-codex",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.1-codex-max", DisplayName: "gpt-5.1-codex-max",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 400000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5.1", DisplayName: "gpt-5.1",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5-codex", DisplayName: "gpt-5-codex",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "gpt-5", DisplayName: "gpt-5",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "o3", DisplayName: "o3",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 200000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		Slug: "o4-mini", DisplayName: "o4-mini",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 200000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceCodexCatalog,
	},
	{
		// OpenRouter canonical aliases for known OpenAI models (gateway slugs
		// pinned by the OpenRouter preset TrustedModels). Same capabilities as
		// the base model; configured-compatibility provenance.
		Slug: "openai/gpt-5", DisplayName: "openai/gpt-5",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceConfiguredCompatibility,
	},
	{
		Slug: "openai/gpt-5.1", DisplayName: "openai/gpt-5.1",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 272000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText, ModalityImage},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		Provenance: ContractProvenanceConfiguredCompatibility,
	},
	{
		Slug: "openai/o3", DisplayName: "openai/o3",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 200000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceConfiguredCompatibility,
	},
	{
		Slug: "openai/o4-mini", DisplayName: "openai/o4-mini",
		Envelope: CapabilityEnvelope{
			ContextWindowTokens: 200000, ReasoningClass: ReasoningClassExtended,
			ThinkingClass: ThinkingClassNone, ToolClass: ToolClassFunction,
			Modalities: []string{ModalityText},
		},
		Effort:     &codexEffortContract{ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		Provenance: ContractProvenanceConfiguredCompatibility,
	},
	{
		// DeepSeek gateway model: daemon-pinned configured-compatibility
		// envelope (not a Codex catalog model; no Reasoning Effort contract —
		// effort is never speculated for it). Identity is the exact slug.
		Slug: "deepseek-v4-flash", DisplayName: "deepseek-v4-flash",
		Envelope:   envelopeDeepSeekV4Flash(),
		Provenance: ContractProvenanceConfiguredCompatibility,
	},
}

var codexModelCatalogIndex = func() map[string]CodexModelMetadata {
	index := make(map[string]CodexModelMetadata, len(codexModelCatalog))
	for _, entry := range codexModelCatalog {
		index[normalizeSpace(entry.Slug)] = entry
	}
	return index
}()

// lookupCodexModelMetadata resolves the daemon-owned metadata for one exact
// Codex model identity. Unknown models resolve nowhere — callers must fail
// closed (never masquerade under another identity).
func lookupCodexModelMetadata(model string) (CodexModelMetadata, bool) {
	entry, ok := codexModelCatalogIndex[normalizeSpace(model)]
	return entry, ok
}

// CodexModelCatalogEntries returns a clone of the daemon-owned catalog
// (secret-free; deterministic order).
func CodexModelCatalogEntries() []CodexModelMetadata {
	out := make([]CodexModelMetadata, 0, len(codexModelCatalog))
	for _, entry := range codexModelCatalog {
		copyEntry := entry
		if entry.Effort != nil {
			effort := *entry.Effort
			effort.supported = append([]string(nil), entry.Effort.supported...)
			copyEntry.Effort = &effort
		}
		copyEntry.Envelope.Modalities = append([]string(nil), entry.Envelope.Modalities...)
		out = append(out, copyEntry)
	}
	return out
}

// CodexEffortContractSnapshot is the secret-free App-facing projection of one
// model's Reasoning Effort contract.
type CodexEffortContractSnapshot struct {
	ClientModel string   `json:"client_model"`
	Default     string   `json:"default"`
	Supported   []string `json:"supported"`
}

// CodexEffortContractSnapshots returns the daemon-owned effort vocabulary for
// every known Codex model with an effort contract (secret-free).
func CodexEffortContractSnapshots() []CodexEffortContractSnapshot {
	entries := CodexModelCatalogEntries()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Slug < entries[j].Slug })
	out := make([]CodexEffortContractSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.Effort == nil {
			continue
		}
		out = append(out, CodexEffortContractSnapshot{
			ClientModel: entry.Slug,
			Default:     entry.Effort.defaultEffort,
			Supported:   append([]string(nil), entry.Effort.supported...),
		})
	}
	return out
}

// isCodexReasoningEffortValue reports whether value is in the daemon-owned
// Codex effort vocabulary (fail closed for everything else).
func isCodexReasoningEffortValue(value string) bool {
	value = normalizeID(value)
	for _, candidate := range codexReasoningEffortVocabulary {
		if candidate == value {
			return true
		}
	}
	return false
}

// codexEffortSupported reports whether the model's daemon-owned contract
// admits the effort value. Unknown models admit nothing (fail closed).
func codexEffortSupported(model, effort string) bool {
	entry, ok := lookupCodexModelMetadata(model)
	if !ok || entry.Effort == nil {
		return false
	}
	effort = normalizeID(effort)
	for _, candidate := range entry.Effort.supported {
		if candidate == effort {
			return true
		}
	}
	return false
}

// codexEffortDefault returns the model's documented default effort ("" when
// the model has no effort contract).
func codexEffortDefault(model string) string {
	entry, ok := lookupCodexModelMetadata(model)
	if !ok || entry.Effort == nil {
		return ""
	}
	return entry.Effort.defaultEffort
}

// codexModelKnown reports whether the exact slug has daemon-owned metadata.
func codexModelKnown(model string) bool {
	_, ok := lookupCodexModelMetadata(model)
	return ok
}

// errUnknownCodexModel builds the fail-closed error for a model without
// daemon-owned metadata.
func errUnknownCodexModel(model string) error {
	return fmt.Errorf("%w: model %q is not in the Zen Codex model catalog; choose a known Codex model", ErrModelUnsupported, model)
}

// CodexReasoningEffortPreset mirrors the Codex model catalog entry shape
// (`supported_reasoning_levels` items: effort + description).
type CodexReasoningEffortPreset struct {
	Effort      string `json:"effort"`
	Description string `json:"description,omitempty"`
}

// CodexTruncationPolicyConfig is the Codex ModelInfo truncation_policy wire
// shape: the tool-output truncation contract of the running model (mode is
// "bytes" or "tokens"). Required by the Codex CLI >= 0.147 deserializer.
type CodexTruncationPolicyConfig struct {
	Mode  string `json:"mode"`
	Limit int64  `json:"limit"`
}

// CodexModelCatalogWireEntry is the Codex ModelsResponse model entry shape
// (codex model_catalog_json / GET /v1/models contract). Unknown models are
// never projected here.
//
// The Codex CLI >= 0.147 ModelInfo serde contract requires every field below
// that has no serde default: supported_in_api, priority, base_instructions,
// support_verbosity, truncation_policy, supports_parallel_tool_calls,
// experimental_supported_tools, and supported_reasoning_levels. Omitting any
// of them makes codex exit at config load ("missing field ..."), which kills
// the host tmux session and drives the brain host replacement loop.
type CodexModelCatalogWireEntry struct {
	Slug                       string                       `json:"slug"`
	DisplayName                string                       `json:"display_name,omitempty"`
	DefaultReasoningLevel      string                       `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels   []CodexReasoningEffortPreset `json:"supported_reasoning_levels"`
	ContextWindow              int64                        `json:"context_window,omitempty"`
	ShellType                  string                       `json:"shell_type,omitempty"`
	Visibility                 string                       `json:"visibility,omitempty"`
	SupportedInAPI             bool                         `json:"supported_in_api"`
	Priority                   int                          `json:"priority"`
	BaseInstructions           string                       `json:"base_instructions"`
	SupportVerbosity           bool                         `json:"support_verbosity"`
	TruncationPolicy           CodexTruncationPolicyConfig  `json:"truncation_policy"`
	SupportsParallelToolCalls  bool                         `json:"supports_parallel_tool_calls"`
	ExperimentalSupportedTools []string                     `json:"experimental_supported_tools"`
}

// CodexModelsResponse is the Codex-expected /v1/models + model_catalog_json
// envelope (`models` array — NOT the OpenAI list `data` shape).
type CodexModelsResponse struct {
	Models []CodexModelCatalogWireEntry `json:"models"`
}

// codexEffortPresetDescription is a concise evidence-based description used in
// the per-route catalog file (mirrors the Codex catalog wording).
func codexEffortPresetDescription(effort string) string {
	switch normalizeID(effort) {
	case ReasoningEffortMinimal:
		return "Fast responses with minimal reasoning"
	case ReasoningEffortLow:
		return "Fast responses with lighter reasoning"
	case ReasoningEffortMedium:
		return "Balances speed and reasoning depth for everyday tasks"
	case ReasoningEffortHigh:
		return "Greater reasoning depth for complex problems"
	case ReasoningEffortXHigh:
		return "Extra high reasoning depth for complex problems"
	case ReasoningEffortMax:
		return "Maximum reasoning depth for the hardest problems"
	default:
		return ""
	}
}

// codexWireEntryForModel projects one known model into the Codex wire shape.
// Values mirror the Codex CLI 0.147 reference catalog: supported_in_api=true,
// tokens-mode truncation at 10k, parallel tool calls for these models, no
// experimental tools, and no verbosity surface (Zen does not route it).
func codexWireEntryForModel(model string) (CodexModelCatalogWireEntry, bool) {
	entry, ok := lookupCodexModelMetadata(model)
	if !ok {
		return CodexModelCatalogWireEntry{}, false
	}
	wire := CodexModelCatalogWireEntry{
		Slug:                       entry.Slug,
		DisplayName:                entry.DisplayName,
		ContextWindow:              entry.Envelope.ContextWindowTokens,
		ShellType:                  "shell_command",
		Visibility:                 "list",
		SupportedInAPI:             true,
		Priority:                   10,
		BaseInstructions:           codexCatalogBaseInstructions,
		SupportVerbosity:           false,
		TruncationPolicy:           CodexTruncationPolicyConfig{Mode: "tokens", Limit: 10_000},
		SupportsParallelToolCalls:  true,
		ExperimentalSupportedTools: []string{},
		// The Codex ModelInfo contract requires supported_reasoning_levels to
		// be present; a nil slice would marshal as JSON null and fail parse,
		// so the field always carries an explicit (possibly empty) sequence.
		SupportedReasoningLevels: []CodexReasoningEffortPreset{},
	}
	if entry.Effort != nil {
		wire.DefaultReasoningLevel = entry.Effort.defaultEffort
		for _, effort := range entry.Effort.supported {
			wire.SupportedReasoningLevels = append(wire.SupportedReasoningLevels, CodexReasoningEffortPreset{
				Effort:      effort,
				Description: codexEffortPresetDescription(effort),
			})
		}
	}
	return wire, true
}

// CodexModelsResponseForModels projects the exact known-model subset.
func CodexModelsResponseForModels(models []string) CodexModelsResponse {
	seen := map[string]struct{}{}
	resp := CodexModelsResponse{Models: []CodexModelCatalogWireEntry{}}
	for _, model := range models {
		model = normalizeSpace(model)
		if model == "" {
			continue
		}
		if _, dup := seen[model]; dup {
			continue
		}
		seen[model] = struct{}{}
		if wire, ok := codexWireEntryForModel(model); ok {
			resp.Models = append(resp.Models, wire)
		}
	}
	return resp
}

// strings.Contains import guard (used by helpers above via normalizeSpace).
var _ = strings.TrimSpace
