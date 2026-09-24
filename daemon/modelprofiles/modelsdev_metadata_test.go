package modelprofiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModelsDevMetadataRefreshFixtureAndCachePreservation(t *testing.T) {
	previousURL := modelsDevURL
	t.Cleanup(func() { modelsDevURL = previousURL })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"openai":{"models":{"gpt-live":{"name":"GPT Live","context":128000,"modalities":{"input":["text","image"],"output":["text"]},"temperature":true,"input":1.5,"output":6,"release_date":"2026-01-02"},"models-only":{"name":"Should Not Become Available"}}}}`))
	}))
	defer server.Close()
	modelsDevURL = server.URL
	home := t.TempDir()
	catalog := newModelsDevCatalog(filepath.Join(home, "models-dev.json"))
	catalog.client = server.Client()
	if err := catalog.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	meta, fetched, ok := catalog.lookup("openai", "gpt-live")
	if !ok || meta.DisplayName != "GPT Live" || meta.ContextWindow != 128000 || meta.TemperatureSupported == nil || !*meta.TemperatureSupported || fetched.IsZero() {
		t.Fatalf("metadata=%+v fetched=%v ok=%v", meta, fetched, ok)
	}
	ids := []string{"gpt-live"}
	entries := projectCatalogModelEntries(ids, map[string]modelPresentationMetadata{}, ModelSourceDiscovered, nil, nil, nil, false, catalog, "openai")
	if len(entries) != 1 || entries[0].ID != "gpt-live" || !entries[0].Available || entries[0].MetadataSource != "models_dev" {
		t.Fatalf("entries=%+v", entries)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("invalid")) }))
	defer bad.Close()
	modelsDevURL = bad.URL
	if err := catalog.refresh(context.Background()); err == nil {
		t.Fatal("invalid payload unexpectedly refreshed")
	}
	meta, _, ok = catalog.lookup("openai", "gpt-live")
	if !ok || meta.DisplayName != "GPT Live" {
		t.Fatalf("last good cache was not preserved: %+v ok=%v", meta, ok)
	}
	if _, err := os.Stat(filepath.Join(home, "models-dev.json")); err != nil {
		t.Fatal(err)
	}
}

func TestModelsDevMetadataNeverAddsOrEnablesModels(t *testing.T) {
	now := time.Now().UTC()
	catalog := &modelsDevCatalog{UpdatedAt: now, Models: map[string]map[string]modelPresentationMetadata{"openai": {
		"models-only":   {DisplayName: "Only in models.dev"},
		"disabled-live": {DisplayName: "Disabled"},
	}}}
	entries := projectCatalogModelEntries([]string{"disabled-live"}, map[string]modelPresentationMetadata{}, ModelSourceDiscovered, []string{"disabled-live"}, nil, nil, false, catalog, "openai")
	if len(entries) != 1 || entries[0].Available {
		t.Fatalf("disabled live model changed: %+v", entries)
	}
	entries = projectCatalogModelEntries([]string{"live-only"}, map[string]modelPresentationMetadata{}, ModelSourceDiscovered, nil, nil, nil, false, catalog, "openai")
	if len(entries) != 1 || entries[0].ID != "live-only" {
		t.Fatalf("models.dev-only ID entered projection: %+v", entries)
	}
}

func TestModelsDevMetadataModelIDsAreValidatedAndProviderScoped(t *testing.T) {
	catalog := &modelsDevCatalog{Models: map[string]map[string]modelPresentationMetadata{
		"anthropic": {"claude-sonnet-4-6": {}, "not valid": {}},
		"openai":    {"gpt-5": {}},
	}}
	if got := catalog.modelIDs("anthropic"); len(got) != 1 || got[0] != "claude-sonnet-4-6" {
		t.Fatalf("anthropic ids=%v", got)
	}
	if got := catalog.modelIDs("openai"); len(got) != 1 || got[0] != "gpt-5" {
		t.Fatalf("openai ids=%v", got)
	}
}

func TestModelsDevMetadataFillsOnlyMissingLiveFields(t *testing.T) {
	catalog := &modelsDevCatalog{UpdatedAt: time.Now().UTC(), Models: map[string]map[string]modelPresentationMetadata{"openai": {
		"shared": {DisplayName: "Models Name", ContextWindow: 100, InputPricePerMillion: floatPtr(2), ReleaseDate: "2025-01-01"},
	}}}
	live := map[string]modelPresentationMetadata{"shared": {DisplayName: "Live Name", ContextWindow: 200}}
	entries := projectCatalogModelEntries([]string{"shared"}, live, ModelSourceDiscovered, nil, nil, nil, false, catalog, "openai")
	if len(entries) != 1 || entries[0].DisplayName != "Live Name" || entries[0].ContextWindowTokens != 200 || entries[0].InputPricePerMillion == nil || *entries[0].InputPricePerMillion != 2 || entries[0].MetadataSource != "live+models_dev" {
		t.Fatalf("live precedence/metadata enrichment failed: %+v", entries)
	}
}

func floatPtr(v float64) *float64 { return &v }
