package stats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncPricingUpdatesRegistryAndCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai": {"models": {
				"gpt-5.4-mini": {"cost": {"input": 0.75, "output": 4.5, "cache_read": 0.075}},
				"gpt-5.5": {"name": "GPT-5.5", "cost": {"input": 5, "output": 30, "cache_read": 0.5}},
				"gpt-5.6-sol": {"name": "GPT-5.6 Sol", "cost": {
					"input": 5,
					"output": 30,
					"cache_read": 0.5,
					"cache_write": 6.25,
					"tiers": [{"input": 10, "output": 45}],
					"context_over_200k": {"input": 10, "output": 45}
				}},
				"nested-only": {"name": "Nested Metadata Only", "cost": {
					"tiers": [{"input": 10, "output": 45}],
					"context_over_200k": {"input": 10, "output": 45}
				}},
				"o3-mini": {"cost": {"input": 1.1, "output": 4.4, "cache_read": 0.55}}
			}},
			"anthropic": {"models": {
				"claude-sonnet-4-6": {"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75}}
			}},
			"xai": {"models": {
				"grok-4.5": {"name": "Grok 4.5", "cost": {"input": 2, "output": 6, "cache_read": 0.5}},
				"grok-build-0.1": {"name": "Grok Build 0.1", "cost": {"input": 1, "output": 2, "cache_read": 0.2}}
			}},
			"openrouter": {"models": {
				"gpt-5.6": {"name": "GPT-5.6", "cost": {"input": 1, "output": 2}}
			}}
		}`))
	}))
	defer srv.Close()

	prevURL := pricingSyncURL
	prevClient := pricingHTTPClient
	prevModels := clonePricingMap(prices.models)
	prevSource := prices.source
	prevUpdated := prices.updatedAt
	pricingSyncURL = srv.URL
	pricingHTTPClient = srv.Client()
	t.Cleanup(func() {
		pricingSyncURL = prevURL
		pricingHTTPClient = prevClient
		prices.mu.Lock()
		prices.models = prevModels
		prices.source = prevSource
		prices.updatedAt = prevUpdated
		prices.mu.Unlock()
	})

	home := t.TempDir()
	if err := syncPricing(context.Background(), home); err != nil {
		t.Fatalf("syncPricing: %v", err)
	}

	if got, ok := currentPricing("gpt-5.4-mini"); !ok || got.input != 0.75 || got.output != 4.5 || got.cacheRead != 0.075 {
		t.Fatalf("unexpected gpt-5.4-mini pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("gpt-5.5"); !ok || got.input != 5 || got.output != 30 || got.displayName != "GPT-5.5" {
		t.Fatalf("unexpected gpt-5.5 pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("gpt-5.6-sol"); !ok || got.input != 5 || got.output != 30 || got.cacheRead != 0.5 || got.cacheCreate != 6.25 || got.displayName != "GPT-5.6 Sol" {
		t.Fatalf("unexpected gpt-5.6-sol pricing: %+v ok=%v", got, ok)
	}
	if _, ok := currentPricing("nested-only"); ok {
		t.Fatal("nested cost metadata without numeric rates should not register pricing")
	}
	if _, ok := currentPricing("gpt-5.6"); ok {
		t.Fatal("unexpected pricing from unsupported provider")
	}
	if got, ok := currentPricing("o3-mini"); !ok || got.cacheRead != 0.55 {
		t.Fatalf("unexpected o3-mini pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("claude-sonnet-4-6"); !ok || got.cacheCreate != 3.75 {
		t.Fatalf("unexpected claude-sonnet-4-6 pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("grok-4.5"); !ok || got.input != 2 || got.output != 6 || got.cacheRead != 0.5 {
		t.Fatalf("unexpected grok-4.5 pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("grok-build"); !ok || got.input != 1 || got.output != 2 || got.cacheRead != 0.2 || got.displayName != "Grok Build 0.1" {
		t.Fatalf("unexpected grok-build alias pricing: %+v ok=%v", got, ok)
	}

	prices.mu.RLock()
	updatedAt := prices.updatedAt
	source := prices.source
	prices.mu.RUnlock()
	if updatedAt.IsZero() || time.Since(updatedAt) > time.Minute {
		t.Fatalf("unexpected updatedAt: %v", updatedAt)
	}
	if source != "models.dev" {
		t.Fatalf("unexpected source: %s", source)
	}

	// Reset registry to built-in snapshot, then ensure disk cache restores synced values.
	prices.mu.Lock()
	prices.models = clonePricingMap(staticPricing)
	prices.updatedAt = time.Time{}
	prices.source = "built-in"
	prices.mu.Unlock()

	loadPricingCache(home)
	if got, ok := currentPricing("gpt-5.4-mini"); !ok || got.input != 0.75 {
		t.Fatalf("cache reload failed: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("gpt-5.5"); !ok || got.output != 30 {
		t.Fatalf("cache reload failed for dynamic model: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("gpt-5.6-sol"); !ok || got.cacheCreate != 6.25 {
		t.Fatalf("cache reload failed for gpt-5.6-sol: %+v ok=%v", got, ok)
	}
	if pricingIsStale() {
		t.Fatal("fresh cache should not be stale")
	}
}

func TestLoadPreviousPricingCacheForcesResync(t *testing.T) {
	prevModels := clonePricingMap(prices.models)
	prevSource := prices.source
	prevUpdated := prices.updatedAt
	t.Cleanup(func() {
		prices.mu.Lock()
		prices.models = prevModels
		prices.source = prevSource
		prices.updatedAt = prevUpdated
		prices.mu.Unlock()
	})

	home := t.TempDir()
	if err := persistPricingCache(home, pricingCacheFile{
		Version:   pricingCacheVersion - 1,
		UpdatedAt: time.Now().UTC(),
		Source:    "models.dev",
		Models: map[string]pricingCacheEntry{
			"gpt-5.5": {DisplayName: "GPT-5.5", Input: 5, Output: 30, CacheRead: 0.5},
		},
	}); err != nil {
		t.Fatalf("persist previous cache: %v", err)
	}

	loadPricingCache(home)
	if got, ok := currentPricing("gpt-5.5"); !ok || got.input != 5 {
		t.Fatalf("previous cache values should still load before resync: %+v ok=%v", got, ok)
	}
	if !pricingIsStale() {
		t.Fatal("previous cache should force a fresh models.dev sync")
	}
}
