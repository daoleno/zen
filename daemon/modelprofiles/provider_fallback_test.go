package modelprofiles

import "testing"

func TestProjectConnectionModelsClaudeUsesBundledCatalogAfterDiscoveryFailure(t *testing.T) {
	owner := &Owner{modelsDev: &modelsDevCatalog{Models: map[string]map[string]modelPresentationMetadata{
		"anthropic": {"models-dev-claude": {DisplayName: "Cached catalog model"}},
	}}}
	profile := Profile{
		ID:               "claude-provider",
		Client:           ClientClaude,
		ExecutorID:       ExecutorClaude,
		ProviderID:       "anthropic",
		ProviderLabel:    "Anthropic",
		Model:            "configured-claude",
		ModelPlaceholder: false,
	}
	entries := owner.projectConnectionModels(profile, discoveryEntry{
		Err:      "model discovery status 401",
		LastGood: []string{"last-good-claude"},
	})
	if len(entries) == 0 {
		t.Fatal("Claude fallback catalog is empty")
	}
	byID := make(map[string]ProviderModelEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	for _, id := range []string{"last-good-claude", "claude-sonnet-4-6", "claude-opus-4-1", "models-dev-claude", "configured-claude"} {
		entry, ok := byID[id]
		if !ok || !entry.Available {
			t.Fatalf("fallback id %q missing or unavailable: %#v", id, entry)
		}
	}
	if byID["last-good-claude"].Source != ModelSourceLKG {
		t.Fatalf("last good source=%q", byID["last-good-claude"].Source)
	}
	if byID["claude-sonnet-4-6"].Source != ModelSourceBundled {
		t.Fatalf("Claude bundled source=%q", byID["claude-sonnet-4-6"].Source)
	}
	if byID["configured-claude"].Source != ModelSourceManual {
		t.Fatalf("configured source=%q", byID["configured-claude"].Source)
	}
}

func TestProjectConnectionModelsCodexFallbackIncludesCacheAndPinnedIdentities(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("cache-only", "Cache only", ""),
	})
	owner := &Owner{}
	profile := Profile{ID: "codex-provider", ExecutorID: ExecutorCodex, ProviderID: "openai"}
	entries := owner.projectConnectionModels(profile, discoveryEntry{Err: "model discovery status 404"})
	byID := make(map[string]ProviderModelEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	if byID["cache-only"].Source != ModelSourceCodexCache || !byID["cache-only"].Available {
		t.Fatalf("Codex cache fallback=%#v", byID["cache-only"])
	}
	if byID["gpt-5.6-sol"].Source != ModelSourceBundled || !byID["gpt-5.6-sol"].Available {
		t.Fatalf("Codex pinned fallback=%#v", byID["gpt-5.6-sol"])
	}
}
