package modelprofiles

import (
	"bytes"
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

// Regression: the Codex wire catalog must never fabricate context_window for
// models without explicit metadata. The conn model_catalog_json feeds the
// native CLI's compaction/skill-budget math; a bogus 1-token window forces
// constant "Context compacted", skill-budget warnings, and false tool
// failures. Resolution order: installed Codex cache, daemon-owned pinned
// catalog, then omit the field so native Codex fallback applies.
func TestWireCatalogContextWindowPrefersInstalledCodexCache(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("gpt-5.6-sol", "GPT-5.6-Sol", ReasoningEffortMedium, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh),
	})
	resp := CodexModelsResponseForModels([]string{"gpt-5.6-sol"})
	if len(resp.Models) != 1 {
		t.Fatalf("models=%d want 1", len(resp.Models))
	}
	if got := resp.Models[0].ContextWindow; got != 123456 {
		t.Fatalf("context_window=%d want installed cache value 123456 (never the fabricated 1)", got)
	}
}

func TestWireCatalogContextWindowFallsBackToDaemonOwnedCatalog(t *testing.T) {
	// No installed cache: the daemon-owned pinned catalog still resolves the
	// evidence-based window for known models.
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	resp := CodexModelsResponseForModels([]string{"gpt-5.6-sol"})
	if len(resp.Models) != 1 {
		t.Fatalf("models=%d want 1", len(resp.Models))
	}
	if got := resp.Models[0].ContextWindow; got != 272000 {
		t.Fatalf("context_window=%d want daemon-owned 272000", got)
	}
}

func TestWireCatalogOmitsContextWindowForUnknownModel(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	resp := CodexModelsResponseForModels([]string{"vendor/private-alpha"})
	if len(resp.Models) != 1 {
		t.Fatalf("models=%d want 1", len(resp.Models))
	}
	if resp.Models[0].ContextWindow != 0 {
		t.Fatalf("context_window=%d want 0 (omitted, native fallback)", resp.Models[0].ContextWindow)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("context_window")) {
		t.Fatalf("unknown model must omit context_window so native Codex fallback applies: %s", raw)
	}
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

func TestProviderProjectionSurvivesLegacyCacheEntryWithoutMetadata(t *testing.T) {
	// Entries persisted before the metadata field existed deserialize with a
	// nil Metadata map but non-empty IDs. Projecting them must not panic on
	// assignment to entry in nil map.
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("gpt-5.6", "Local 5.6", ReasoningEffortMedium, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh),
	})
	owner := startBuiltinVerifierOwner(t)
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "legacy", Name: "Legacy", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://gateway.example/v1", Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	// putModels with nil metadata and non-empty ids reproduces the legacy shape.
	owner.discovery.putModels("legacy", []string{"gpt-5.6"}, nil, nil)
	owner.mu.Unlock()

	projection, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProviderModelEntry{}
	for _, entry := range projection.Models["legacy"] {
		byID[entry.ID] = entry
	}
	if byID["gpt-5.6"].DisplayName != "Local 5.6" {
		t.Fatalf("local metadata did not fill legacy gap: %#v", byID["gpt-5.6"])
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
