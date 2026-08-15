package modelprofiles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func installTestCodexModelCache(t *testing.T, models []CodexModelCatalogWireEntry) string {
	t.Helper()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("HOME", t.TempDir())
	raw, err := json.Marshal(CodexModelsResponse{Models: models})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "models_cache.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return codexHome
}

func testCodexCacheEntry(slug, display, defaultEffort string, efforts ...string) CodexModelCatalogWireEntry {
	entry := CodexModelCatalogWireEntry{
		Slug:                     slug,
		DisplayName:              display,
		DefaultReasoningLevel:    defaultEffort,
		ContextWindow:            123456,
		SupportedReasoningLevels: []CodexReasoningEffortPreset{},
	}
	for _, effort := range efforts {
		entry.SupportedReasoningLevels = append(entry.SupportedReasoningLevels, CodexReasoningEffortPreset{Effort: effort})
	}
	return entry
}

func TestInstalledCodexCatalogPrefersCODEXHOMEAndNeverUsesRealHome(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("gpt-5.6", "GPT 5.6 Local", ReasoningEffortMedium, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh),
	})
	ids, metadata, err := loadInstalledCodexModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gpt-5.6" {
		t.Fatalf("ids=%#v", ids)
	}
	if metadata["gpt-5.6"].DisplayName != "GPT 5.6 Local" || metadata["gpt-5.6"].DefaultReasoningLevel != ReasoningEffortMedium {
		t.Fatalf("metadata=%#v", metadata["gpt-5.6"])
	}
}

func TestProviderProjectionUsesUpstreamCatalogThenLocalSameSlugMetadata(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("gpt-5.6", "Local 5.6", ReasoningEffortMedium, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh),
		testCodexCacheEntry("local-only", "Local only", "", nil...),
	})
	owner := startBuiltinVerifierOwner(t)
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "dynamic", Name: "Dynamic", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://gateway.example/v1", Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.putModels("dynamic", []string{"gpt-5.6", "upstream-only"}, map[string]modelPresentationMetadata{
		"gpt-5.6": {DisplayName: "Upstream 5.6"},
	}, nil)
	owner.mu.Unlock()

	projection, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProviderModelEntry{}
	for _, entry := range projection.Models["dynamic"] {
		byID[entry.ID] = entry
	}
	if _, ok := byID["local-only"]; ok {
		t.Fatal("non-empty upstream discovery must replace local fallback choices")
	}
	if byID["gpt-5.6"].DisplayName != "Upstream 5.6" {
		t.Fatalf("upstream display metadata did not win: %#v", byID["gpt-5.6"])
	}
	if byID["gpt-5.6"].ReasoningEffortDefault != ReasoningEffortMedium || len(byID["gpt-5.6"].ReasoningEfforts) != 3 {
		t.Fatalf("local same-slug effect metadata did not fill gaps: %#v", byID["gpt-5.6"])
	}
	if !byID["upstream-only"].Available || byID["upstream-only"].Known {
		t.Fatalf("upstream unknown must remain selectable without invented metadata: %#v", byID["upstream-only"])
	}
}

func TestProviderProjectionFallsBackToCodexCacheAndIncludesExactDefaultsAndSessions(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("cache-model", "Cache Model", ReasoningEffortHigh, ReasoningEffortLow, ReasoningEffortHigh),
	})
	owner := startBuiltinVerifierOwner(t)
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "fallback", Name: "Fallback", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://gateway.example/v1", Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	projection, err = owner.SetProviderDefault(ClientCodex, "fallback", "default-exact", projection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorCodex, "fallback", "session-exact", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "session-exact-id"); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.putModels("fallback", nil, nil, errors.New("upstream unavailable"))
	owner.mu.Unlock()

	projection, err = owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProviderModelEntry{}
	for _, entry := range projection.Models["fallback"] {
		byID[entry.ID] = entry
	}
	if byID["cache-model"].Source != ModelSourceCodexCache || byID["cache-model"].DisplayName != "Cache Model" {
		t.Fatalf("cache fallback=%#v", byID["cache-model"])
	}
	for _, exact := range []string{"default-exact", "session-exact"} {
		if !byID[exact].Available {
			t.Fatalf("exact model %q missing: %#v", exact, projection.Models["fallback"])
		}
	}
}
