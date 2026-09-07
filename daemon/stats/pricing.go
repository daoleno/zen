package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	pricingCacheRelPath = ".zen/pricing-cache.json"
	pricingSyncEvery    = 24 * time.Hour
	pricingCacheVersion = 5
	pricingMaxBytes     = 32 << 20
)

var (
	pricingSyncURL    = "https://models.dev/api.json"
	pricingHTTPClient = http.DefaultClient
	pricingProviders  = map[string]bool{"anthropic": true, "openai": true, "xai": true}
)

type pricingCacheFile struct {
	Version   int                                     `json:"version,omitempty"`
	UpdatedAt time.Time                               `json:"updatedAt"`
	Source    string                                  `json:"source"`
	Providers map[string]map[string]pricingCacheEntry `json:"providers,omitempty"`
	Models    map[string]pricingCacheEntry            `json:"models,omitempty"`
}

type pricingCacheEntry struct {
	DisplayName  string             `json:"displayName"`
	Input        float64            `json:"input"`
	Output       float64            `json:"output"`
	CacheRead    float64            `json:"cacheRead"`
	CacheCreate  float64            `json:"cacheCreate"`
	Present      uint8              `json:"present"`
	Tiered       bool               `json:"tiered,omitempty"`
	ContextTiers []contextPriceTier `json:"contextTiers,omitempty"`
	Provider     string             `json:"provider,omitempty"`
	Source       string             `json:"source"`
	UpdatedAt    time.Time          `json:"updatedAt,omitempty"`
}

type modelsDevCost struct {
	Input        *float64        `json:"input"`
	Output       *float64        `json:"output"`
	CacheRead    *float64        `json:"cache_read"`
	CacheWrite   *float64        `json:"cache_write"`
	CacheWrite5m *float64        `json:"cache_write_5m"`
	Tiers        json.RawMessage `json:"tiers"`
	ContextTier  json.RawMessage `json:"context_over_200k"`
}

func (c modelsDevCost) hasRates() bool {
	return c.Input != nil || c.Output != nil || c.CacheRead != nil || c.CacheWrite != nil || c.CacheWrite5m != nil
}

type pricingRegistry struct {
	mu        sync.RWMutex
	models    map[string]map[string]modelPricing
	updatedAt time.Time
	source    string
}

var prices = &pricingRegistry{
	models: staticProviderPricing(),
	source: "built-in",
}

func clonePricingMap(src map[string]modelPricing) map[string]modelPricing {
	out := make(map[string]modelPricing, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneProviderPricing(src map[string]map[string]modelPricing) map[string]map[string]modelPricing {
	out := make(map[string]map[string]modelPricing, len(src))
	for provider, models := range src {
		out[provider] = clonePricingMap(models)
	}
	return out
}

func staticProviderPricing() map[string]map[string]modelPricing {
	out := map[string]map[string]modelPricing{"anthropic": {}, "openai": {}, "xai": {}}
	for modelID, price := range staticPricing {
		out[inferOfficialProvider(modelID)][modelID] = price
	}
	return out
}

func pricingCachePath(home string) string {
	return filepath.Join(home, pricingCacheRelPath)
}

func currentProviderPricing(provider, modelID string) (modelPricing, bool) {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	p, ok := prices.models[normalizePricingProvider(provider)][pricingModelID(modelID)]
	return p, ok
}

// currentPricing remains a test/display convenience for models whose official
// provider is unambiguous. Cost calculation always uses currentProviderPricing.
func currentPricing(modelID string) (modelPricing, bool) {
	return currentProviderPricing(inferOfficialProvider(modelID), modelID)
}

func normalizePricingProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek", "deepseek-api":
		return "deepseek"
	case "openai", "openai-api", "codex":
		return "openai"
	case "anthropic", "anthropic-api", "claude":
		return "anthropic"
	case "xai", "x-ai":
		return "xai"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func inferOfficialProvider(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "claude-"):
		return "anthropic"
	case strings.HasPrefix(modelID, "grok-"):
		return "xai"
	case strings.HasPrefix(modelID, "deepseek-"):
		return "deepseek"
	default:
		return "openai"
	}
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
	if len(cache.Providers) == 0 && len(cache.Models) == 0 {
		return
	}
	// Older caches lost rate presence and context tiers; they cannot establish prices.
	if cache.Version != pricingCacheVersion {
		return
	}
	loaded := make(map[string]map[string]modelPricing)
	loadEntry := func(provider, id string, item pricingCacheEntry) bool {
		if id == "" || item.Present > 15 || !validContextPriceTiers(item.ContextTiers) || (item.Source != "built-in" && item.Source != "models.dev") {
			return false
		}
		for _, rate := range []float64{item.Input, item.Output, item.CacheRead, item.CacheCreate} {
			if rate < 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
				return false
			}
		}
		provider = normalizePricingProvider(provider)
		if loaded[provider] == nil {
			loaded[provider] = make(map[string]modelPricing)
		}
		loaded[provider][id] = modelPricing{
			displayName: item.DisplayName,
			input:       item.Input,
			output:      item.Output,
			cacheRead:   item.CacheRead,
			cacheCreate: item.CacheCreate,
			present:     item.Present, tiered: item.Tiered, provider: item.Provider, source: item.Source,
			contextTiers: item.ContextTiers,
			updatedAt:    item.UpdatedAt,
		}
		return true
	}
	for provider, models := range cache.Providers {
		for id, item := range models {
			if !loadEntry(provider, id, item) {
				return
			}
		}
	}
	for id, item := range cache.Models {
		if !loadEntry(inferOfficialProvider(id), id, item) {
			return
		}
	}
	prices.mu.Lock()
	prices.models = mergeProviderPricing(staticProviderPricing(), loaded)
	prices.updatedAt = cache.UpdatedAt
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

func mergeProviderPricing(base, override map[string]map[string]modelPricing) map[string]map[string]modelPricing {
	out := cloneProviderPricing(base)
	for provider, models := range override {
		if out[provider] == nil {
			out[provider] = make(map[string]modelPricing)
		}
		for id, price := range models {
			out[provider][id] = price
		}
	}
	return out
}

func pricingIsStale() bool {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	return prices.updatedAt.IsZero() || time.Since(prices.updatedAt) >= pricingSyncEvery
}

func syncPricing(ctx context.Context, home string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, pricingMaxBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > pricingMaxBytes {
		return fmt.Errorf("pricing catalog exceeds size limit")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}

	prices.mu.RLock()
	updated := cloneProviderPricing(prices.models)
	prices.mu.RUnlock()
	count := 0
	now := time.Now().UTC()
	for provider, providerData := range payload {
		if !pricingProviders[provider] {
			continue
		}
		for modelID, modelData := range providerData.Models {
			if modelID == "" {
				return fmt.Errorf("pricing catalog contains an empty model ID")
			}
			cost := modelData.Cost
			if !cost.hasRates() {
				continue
			}
			localID := modelID
			if updated[provider] == nil {
				updated[provider] = make(map[string]modelPricing)
			}
			current := modelPricing{displayName: modelDisplayName(localID, modelData.Name), source: "models.dev", provider: provider, updatedAt: now,
				tiered: meaningfulTier(cost.Tiers) || meaningfulTier(cost.ContextTier)}
			var tierErr error
			current.contextTiers, tierErr = parseContextPriceTiers(cost)
			if tierErr != nil {
				return fmt.Errorf("model %q: %w", localID, tierErr)
			}
			for _, rate := range []*float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.CacheWrite5m} {
				if rate != nil && (math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0) {
					return fmt.Errorf("invalid pricing rate for model %q", localID)
				}
			}
			if cost.Input != nil {
				current.input = *cost.Input
				current.present |= rateInput
			}
			if cost.Output != nil {
				current.output = *cost.Output
				current.present |= rateOutput
			}
			if cost.CacheRead != nil {
				current.cacheRead = *cost.CacheRead
				current.present |= rateCacheRead
			}
			if cost.CacheWrite != nil {
				current.cacheCreate = *cost.CacheWrite
				current.present |= rateCacheCreate
			} else if cost.CacheWrite5m != nil {
				current.cacheCreate = *cost.CacheWrite5m
				current.present |= rateCacheCreate
			}
			count++
			updated[provider][localID] = current
		}
	}

	if count == 0 {
		return fmt.Errorf("pricing catalog has no supported rates")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if home != "" {
		cacheProviders := make(map[string]map[string]pricingCacheEntry, len(updated))
		for provider, models := range updated {
			cacheProviders[provider] = make(map[string]pricingCacheEntry, len(models))
			for id, item := range models {
				cacheProviders[provider][id] = pricingCacheEntry{
					DisplayName: item.displayName,
					Input:       item.input,
					Output:      item.output,
					CacheRead:   item.cacheRead,
					CacheCreate: item.cacheCreate,
					Present:     item.ratePresence(), Tiered: item.tiered, Provider: item.provider, Source: item.priceSource(), UpdatedAt: item.updatedAt,
					ContextTiers: item.contextTiers,
				}
			}
		}
		if err := persistPricingCache(home, pricingCacheFile{
			Version:   pricingCacheVersion,
			UpdatedAt: now,
			Source:    "models.dev",
			Providers: cacheProviders,
		}); err != nil {
			return fmt.Errorf("persist pricing cache: %w", err)
		}
	}
	prices.mu.Lock()
	prices.models = updated
	prices.updatedAt = now
	prices.source = "models.dev"
	prices.mu.Unlock()
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
	f, err := os.CreateTemp(filepath.Dir(path), ".pricing-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func meaningfulTier(raw json.RawMessage) bool {
	var value any
	if len(raw) == 0 {
		return false
	}
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// Only the curated built-in table uses these provider families, not arbitrary IDs.
func builtinPricingProvider(id string) string {
	if strings.HasPrefix(id, "claude-") {
		return "anthropic"
	}
	if strings.HasPrefix(id, "grok-") {
		return "xai"
	}
	return "openai"
}

const (
	rateInput uint8 = 1 << iota
	rateOutput
	rateCacheRead
	rateCacheCreate
)

func (p modelPricing) priceSource() string {
	if p.source != "" {
		return p.source
	}
	return "built-in"
}

func (p modelPricing) ratePresence() uint8 {
	if p.source != "" {
		return p.present
	}
	var mask uint8
	for i, rate := range []float64{p.input, p.output, p.cacheRead, p.cacheCreate} {
		if rate > 0 {
			mask |= 1 << i
		}
	}
	return mask
}

type httpStatusError struct {
	statusCode int
}

func (e *httpStatusError) Error() string {
	return http.StatusText(e.statusCode)
}
