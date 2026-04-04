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
				"o3-mini": {"cost": {"input": 1.1, "output": 4.4, "cache_read": 0.55}}
			}},
			"anthropic": {"models": {
				"claude-sonnet-4-6": {"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75}}
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
	if got, ok := currentPricing("o3-mini"); !ok || got.cacheRead != 0.55 {
		t.Fatalf("unexpected o3-mini pricing: %+v ok=%v", got, ok)
	}
	if got, ok := currentPricing("claude-sonnet-4-6"); !ok || got.cacheCreate != 3.75 {
		t.Fatalf("unexpected claude-sonnet-4-6 pricing: %+v ok=%v", got, ok)
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
}
