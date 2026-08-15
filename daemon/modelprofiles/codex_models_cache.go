package modelprofiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxCodexModelsCacheBytes = 32 << 20

type modelPresentationMetadata struct {
	DisplayName              string                       `json:"display_name,omitempty"`
	DefaultReasoningLevel    string                       `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []CodexReasoningEffortPreset `json:"supported_reasoning_levels,omitempty"`
	ContextWindow            int64                        `json:"context_window,omitempty"`
}

func metadataFromWire(entry CodexModelCatalogWireEntry) modelPresentationMetadata {
	return normalizeModelPresentationMetadata(modelPresentationMetadata{
		DisplayName:              entry.DisplayName,
		DefaultReasoningLevel:    entry.DefaultReasoningLevel,
		SupportedReasoningLevels: entry.SupportedReasoningLevels,
		ContextWindow:            entry.ContextWindow,
	})
}

func normalizeModelPresentationMetadata(metadata modelPresentationMetadata) modelPresentationMetadata {
	metadata.DisplayName = normalizeSpace(metadata.DisplayName)
	metadata.DefaultReasoningLevel = normalizeID(metadata.DefaultReasoningLevel)
	seen := map[string]struct{}{}
	efforts := make([]CodexReasoningEffortPreset, 0, len(metadata.SupportedReasoningLevels))
	for _, preset := range metadata.SupportedReasoningLevels {
		effort := normalizeID(preset.Effort)
		if !isCodexReasoningEffortValue(effort) {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		efforts = append(efforts, CodexReasoningEffortPreset{
			Effort:      effort,
			Description: normalizeSpace(preset.Description),
		})
	}
	metadata.SupportedReasoningLevels = efforts
	if metadata.DefaultReasoningLevel != "" {
		if _, ok := seen[metadata.DefaultReasoningLevel]; !ok {
			metadata.DefaultReasoningLevel = ""
		}
	}
	if metadata.ContextWindow < 0 {
		metadata.ContextWindow = 0
	}
	return metadata
}

func mergeModelPresentationMetadata(primary, fallback modelPresentationMetadata) modelPresentationMetadata {
	primary = normalizeModelPresentationMetadata(primary)
	fallback = normalizeModelPresentationMetadata(fallback)
	if primary.DisplayName == "" {
		primary.DisplayName = fallback.DisplayName
	}
	if primary.DefaultReasoningLevel == "" {
		primary.DefaultReasoningLevel = fallback.DefaultReasoningLevel
	}
	if len(primary.SupportedReasoningLevels) == 0 {
		primary.SupportedReasoningLevels = append([]CodexReasoningEffortPreset(nil), fallback.SupportedReasoningLevels...)
	}
	if primary.ContextWindow == 0 {
		primary.ContextWindow = fallback.ContextWindow
	}
	return normalizeModelPresentationMetadata(primary)
}

func codexModelsCacheCandidates() []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		add(filepath.Join(home, "models_cache.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		add(filepath.Join(home, ".codex", "models_cache.json"))
	}
	return out
}

func loadInstalledCodexModelCatalog() ([]string, map[string]modelPresentationMetadata, error) {
	var errs []error
	for _, path := range codexModelsCacheCandidates() {
		ids, metadata, err := loadCodexModelCatalogFile(path)
		if err == nil {
			return ids, metadata, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return nil, map[string]modelPresentationMetadata{}, nil
}

func loadCodexModelCatalogFile(path string) ([]string, map[string]modelPresentationMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxCodexModelsCacheBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > maxCodexModelsCacheBytes {
		return nil, nil, fmt.Errorf("codex models cache exceeds %d bytes", maxCodexModelsCacheBytes)
	}
	var response CodexModelsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, nil, fmt.Errorf("decode codex models cache %s: %w", path, err)
	}
	ids := make([]string, 0, len(response.Models))
	metadata := make(map[string]modelPresentationMetadata, len(response.Models))
	seen := map[string]struct{}{}
	for _, entry := range response.Models {
		slug := normalizeSpace(entry.Slug)
		if err := ValidateModelID(slug); err != nil {
			continue
		}
		if _, ok := seen[slug]; !ok {
			seen[slug] = struct{}{}
			ids = append(ids, slug)
		}
		metadata[slug] = mergeModelPresentationMetadata(metadata[slug], metadataFromWire(entry))
	}
	return ids, metadata, nil
}

// resolveCodexContextWindow returns the evidence-based context window for a
// wire catalog entry in resolution order: explicit projection metadata, the
// installed Codex CLI catalog cache (the running binary's own metadata), then
// the daemon-owned pinned catalog. Unknown models resolve to 0, which omits
// the field so the native CLI applies its own fallback — the daemon never
// fabricates a context window (a bogus value such as 1 drives constant
// Context compacted / skill-budget warnings / false tool failures).
func resolveCodexContextWindow(model string, metadata modelPresentationMetadata, installed map[string]modelPresentationMetadata) int64 {
	if metadata.ContextWindow > 0 {
		return metadata.ContextWindow
	}
	if entry, ok := installed[normalizeSpace(model)]; ok && entry.ContextWindow > 0 {
		return entry.ContextWindow
	}
	if entry, ok := lookupCodexModelMetadata(model); ok && entry.Envelope.ContextWindowTokens > 0 {
		return entry.Envelope.ContextWindowTokens
	}
	return 0
}

func codexWireEntryForModelMetadata(model string, metadata modelPresentationMetadata, installed map[string]modelPresentationMetadata) (CodexModelCatalogWireEntry, bool) {
	model = normalizeSpace(model)
	if err := ValidateModelID(model); err != nil {
		return CodexModelCatalogWireEntry{}, false
	}
	metadata = normalizeModelPresentationMetadata(metadata)
	displayName := metadata.DisplayName
	if displayName == "" {
		displayName = model
	}
	contextWindow := resolveCodexContextWindow(model, metadata, installed)
	return CodexModelCatalogWireEntry{
		Slug:                       model,
		DisplayName:                displayName,
		DefaultReasoningLevel:      metadata.DefaultReasoningLevel,
		SupportedReasoningLevels:   append([]CodexReasoningEffortPreset{}, metadata.SupportedReasoningLevels...),
		ContextWindow:              contextWindow,
		ShellType:                  "shell_command",
		Visibility:                 "list",
		SupportedInAPI:             true,
		Priority:                   10,
		BaseInstructions:           codexCatalogBaseInstructions,
		SupportVerbosity:           false,
		TruncationPolicy:           CodexTruncationPolicyConfig{Mode: "tokens", Limit: 10_000},
		SupportsParallelToolCalls:  true,
		ExperimentalSupportedTools: []string{},
	}, true
}
