package stats

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	pricingCacheRelPath = ".zen/pricing-cache.json"
	pricingSyncEvery    = 7 * 24 * time.Hour
	pricingCacheVersion = 3
)

var (
	pricingSyncURL    = "https://models.dev/api.json"
	pricingHTTPClient = http.DefaultClient
	pricingProviders  = map[string]bool{"anthropic": true, "openai": true, "xai": true}
)

type pricingCacheFile struct {
	Version   int                          `json:"version,omitempty"`
	UpdatedAt time.Time                    `json:"updatedAt"`
	Source    string                       `json:"source"`
	Models    map[string]pricingCacheEntry `json:"models"`
}

type pricingCacheEntry struct {
	DisplayName string  `json:"displayName"`
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheRead   float64 `json:"cacheRead"`
	CacheCreate float64 `json:"cacheCreate"`
}

type modelsDevCost struct {
	Input        *float64 `json:"input"`
	Output       *float64 `json:"output"`
	CacheRead    *float64 `json:"cache_read"`
	CacheWrite   *float64 `json:"cache_write"`
	CacheWrite5m *float64 `json:"cache_write_5m"`
}

func (c modelsDevCost) hasRates() bool {
	return c.Input != nil || c.Output != nil || c.CacheRead != nil || c.CacheWrite != nil || c.CacheWrite5m != nil
}

type pricingRegistry struct {
	mu        sync.RWMutex
	models    map[string]modelPricing
	updatedAt time.Time
	source    string
}

var prices = &pricingRegistry{
	models: clonePricingMap(staticPricing),
	source: "built-in",
}

func clonePricingMap(src map[string]modelPricing) map[string]modelPricing {
	out := make(map[string]modelPricing, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func pricingCachePath(home string) string {
	return filepath.Join(home, pricingCacheRelPath)
}

func currentPricing(modelID string) (modelPricing, bool) {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	p, ok := prices.models[pricingModelID(modelID)]
	return p, ok
}

func pricingModelID(modelID string) string {
	if modelID == "grok-build" {
		return "grok-build-0.1"
	}
	return modelID
}

func loadPricingCache(home string) {
	if home == "" {
		return
	}
	path := pricingCachePath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cache pricingCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}
	if len(cache.Models) == 0 {
		return
	}
	loaded := make(map[string]modelPricing, len(cache.Models))
	for id, item := range cache.Models {
		loaded[id] = modelPricing{
			displayName: item.DisplayName,
			input:       item.Input,
			output:      item.Output,
			cacheRead:   item.CacheRead,
			cacheCreate: item.CacheCreate,
		}
	}
	prices.mu.Lock()
	prices.models = mergePricingMaps(staticPricing, loaded)
	if cache.Version == pricingCacheVersion {
		prices.updatedAt = cache.UpdatedAt
	} else {
		prices.updatedAt = time.Time{}
	}
	prices.source = cache.Source
	prices.mu.Unlock()
}

func mergePricingMaps(base map[string]modelPricing, override map[string]modelPricing) map[string]modelPricing {
	out := clonePricingMap(base)
	for k, v := range override {
		out[k] = v
	}
	return out
}

func pricingIsStale() bool {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	return prices.updatedAt.IsZero() || time.Since(prices.updatedAt) >= pricingSyncEvery
}

func syncPricing(ctx context.Context, home string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pricingSyncURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "zen/0.1.0 (+https://github.com/daoleno/zen)")

	resp, err := pricingHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{statusCode: resp.StatusCode}
	}

	var payload map[string]struct {
		Models map[string]struct {
			Name string        `json:"name"`
			Cost modelsDevCost `json:"cost"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	updated := clonePricingMap(staticPricing)
	for provider, providerData := range payload {
		if !pricingProviders[provider] {
			continue
		}
		for modelID, modelData := range providerData.Models {
			cost := modelData.Cost
			if !cost.hasRates() {
				continue
			}
			localID := modelID
			if provider == "anthropic" {
				// models.dev uses stable Anthropic aliases that match our IDs in practice.
				localID = modelID
			}
			current, ok := updated[localID]
			if !ok {
				current = modelPricing{displayName: modelDisplayName(localID, modelData.Name)}
			} else if modelData.Name != "" {
				current.displayName = modelData.Name
			}
			if cost.Input != nil {
				current.input = *cost.Input
			}
			if cost.Output != nil {
				current.output = *cost.Output
			}
			if cost.CacheRead != nil {
				current.cacheRead = *cost.CacheRead
			}
			if cost.CacheWrite != nil && *cost.CacheWrite > 0 {
				current.cacheCreate = *cost.CacheWrite
			} else if cost.CacheWrite5m != nil && *cost.CacheWrite5m > 0 {
				current.cacheCreate = *cost.CacheWrite5m
			}
			updated[localID] = current
		}
	}

	now := time.Now().UTC()
	prices.mu.Lock()
	prices.models = updated
	prices.updatedAt = now
	prices.source = "models.dev"
	prices.mu.Unlock()

	if home != "" {
		cacheModels := make(map[string]pricingCacheEntry, len(updated))
		for id, item := range updated {
			cacheModels[id] = pricingCacheEntry{
				DisplayName: item.displayName,
				Input:       item.input,
				Output:      item.output,
				CacheRead:   item.cacheRead,
				CacheCreate: item.cacheCreate,
			}
		}
		_ = persistPricingCache(home, pricingCacheFile{
			Version:   pricingCacheVersion,
			UpdatedAt: now,
			Source:    "models.dev",
			Models:    cacheModels,
		})
	}
	return nil
}

func modelDisplayName(modelID, sourceName string) string {
	if sourceName != "" {
		return sourceName
	}
	return modelID
}

func persistPricingCache(home string, cache pricingCacheFile) error {
	path := pricingCachePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type httpStatusError struct {
	statusCode int
}

func (e *httpStatusError) Error() string {
	return http.StatusText(e.statusCode)
}
