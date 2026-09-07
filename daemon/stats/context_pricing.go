package stats

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// A tier is an explicit reference tariff above a request input-context size.
// Missing rates remain missing, including when the base tariff has that rate.
type contextPriceTier struct {
	Above       int64   `json:"above"`
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheRead   float64 `json:"cache_read"`
	CacheCreate float64 `json:"cache_create"`
	Present     uint8   `json:"present"`
}

type modelsDevContextTier struct {
	modelsDevCost
	Tier struct {
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tier"`
}

func parseContextPriceTiers(cost modelsDevCost) ([]contextPriceTier, error) {
	var entries []modelsDevContextTier
	if meaningfulTier(cost.Tiers) {
		if err := json.Unmarshal(cost.Tiers, &entries); err != nil {
			return nil, nil // Unsupported schema cannot establish an estimate.
		}
	} else if meaningfulTier(cost.ContextTier) {
		entries = make([]modelsDevContextTier, 1)
		if err := json.Unmarshal(cost.ContextTier, &entries[0].modelsDevCost); err != nil {
			return nil, nil
		}
		entries[0].Tier.Type, entries[0].Tier.Size = "context", 200000
	}
	if len(entries) > 32 {
		return nil, nil
	}
	tiers := make([]contextPriceTier, 0, len(entries))
	for _, entry := range entries {
		if entry.Tier.Type != "context" || entry.Tier.Size <= 0 || meaningfulTier(entry.Tiers) || meaningfulTier(entry.ContextTier) {
			return nil, nil
		}
		tier := contextPriceTier{Above: entry.Tier.Size}
		write := entry.CacheWrite
		if write == nil {
			write = entry.CacheWrite5m
		}
		for i, pair := range []struct {
			from *float64
			to   *float64
		}{
			{entry.Input, &tier.Input}, {entry.Output, &tier.Output}, {entry.CacheRead, &tier.CacheRead}, {write, &tier.CacheCreate},
		} {
			if pair.from == nil {
				continue
			}
			if *pair.from < 0 || math.IsNaN(*pair.from) || math.IsInf(*pair.from, 0) {
				return nil, fmt.Errorf("invalid context tier rate")
			}
			*pair.to = *pair.from
			tier.Present |= 1 << i
		}
		tiers = append(tiers, tier)
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Above < tiers[j].Above })
	for i := 1; i < len(tiers); i++ {
		if tiers[i].Above == tiers[i-1].Above {
			return nil, nil
		}
	}
	return tiers, nil
}

func validContextPriceTiers(tiers []contextPriceTier) bool {
	if len(tiers) > 32 {
		return false
	}
	for i, tier := range tiers {
		if tier.Above <= 0 || tier.Present > 15 || (i > 0 && tiers[i-1].Above >= tier.Above) {
			return false
		}
		for _, rate := range []float64{tier.Input, tier.Output, tier.CacheRead, tier.CacheCreate} {
			if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
				return false
			}
		}
	}
	return true
}

func requestPricing(p modelPricing, inputContext int64) (modelPricing, bool) {
	if !p.tiered {
		return p, true
	}
	if inputContext < 0 || len(p.contextTiers) == 0 {
		return p, false
	}
	for _, tier := range p.contextTiers {
		if inputContext <= tier.Above {
			break
		}
		p.input, p.output, p.cacheRead, p.cacheCreate, p.present = tier.Input, tier.Output, tier.CacheRead, tier.CacheCreate, tier.Present
	}
	return p, true
}

func computeCodexContextCost(modelID string, usage codexUsage) (float64, bool) {
	if len(usage.byContext) == 0 {
		return computeKnownCost(modelID, usage.inputTokens, usage.outputTokens, usage.reasoningTokens, usage.cacheRead, 0)
	}
	price, found := currentProviderPricing("openai", modelID)
	if !found {
		return 0, false
	}
	total, known := 0.0, true
	for context, bucket := range usage.byContext {
		p, applicable := requestPricing(price, context)
		if !applicable {
			known = false
			continue
		}
		cost, complete := computeTariffCost(p, bucket.inputTokens, bucket.outputTokens, bucket.cacheRead, 0)
		if !complete {
			known = false
			continue
		}
		total += cost
	}
	return total, known
}
