package stats

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContextTiersUseRequestInputNotDailyTotal(t *testing.T) {
	isolatePricing(t)
	// Fictional tariffs and threshold. Explicit tiers supersede legacy aliases.
	payload := `{"openai":{"models":{"context-model":{"cost":{"input":1,"output":2,"cache_read":0,"tiers":[{"input":3,"output":4,"cache_read":0.5,"tier":{"type":"context","size":100}}],"context_over_200k":{"input":99,"output":99}}}}}}`
	pricingServer(t, &payload)
	home := t.TempDir()
	if err := syncPricing(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	p, _ := currentPricing("context-model")
	for _, tc := range []struct {
		context int64
		input   float64
		known   bool
	}{{-1, 1, false}, {100, 1, true}, {101, 3, true}} {
		got, known := requestPricing(p, tc.context)
		if known != tc.known || got.input != tc.input {
			t.Fatalf("context=%d: %+v %v", tc.context, got, known)
		}
	}
	usage := codexUsage{inputTokens: 140, cacheRead: 60, outputTokens: 20, byContext: map[int64]codexUsage{
		100: {inputTokens: 140, cacheRead: 60, outputTokens: 20},
	}}
	cost, known := computeCodexContextCost("context-model", usage)
	if !known || math.Abs(cost-0.00018) > 1e-12 {
		t.Fatalf("daily aggregate crossed request tier: %v %v", cost, known)
	}
	usage.byContext[101] = codexUsage{inputTokens: 81, cacheRead: 20, outputTokens: 10}
	cost, known = computeCodexContextCost("context-model", usage)
	if !known || math.Abs(cost-0.000473) > 1e-12 {
		t.Fatalf("high tier: %v %v", cost, known)
	}
	usage.byContext[-1] = codexUsage{inputTokens: 10}
	partial, known := computeCodexContextCost("context-model", usage)
	if known || partial != cost {
		t.Fatalf("unknown request must preserve partial estimate: %v %v", partial, known)
	}
	prices.mu.Lock()
	prices.models = staticProviderPricing()
	prices.mu.Unlock()
	loadPricingCache(home)
	p, _ = currentPricing("context-model")
	if len(p.contextTiers) != 1 || p.contextTiers[0].Above != 100 {
		t.Fatalf("cache lost tiers: %+v", p)
	}
	// Cached token contexts are repriced, not a cached dollar total.
	prices.mu.Lock()
	p.input = 2
	prices.models["openai"]["context-model"] = p
	prices.mu.Unlock()
	repriced, _ := computeCodexContextCost("context-model", usage)
	if math.Abs(repriced-partial-0.00014) > 1e-12 {
		t.Fatalf("refresh did not reprice historical requests: %v", repriced)
	}
}

func TestCodexContextRequiresExactLastUsageAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := func(totalInput, totalOutput, lastInput, lastOutput int64) string {
		usage := func(input, output int64) map[string]int64 {
			return map[string]int64{"input_tokens": input, "output_tokens": output, "total_tokens": input + output}
		}
		value := map[string]any{"timestamp": "2026-09-07T00:00:00Z", "type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": usage(totalInput, totalOutput), "last_token_usage": usage(lastInput, lastOutput)}}}
		b, _ := json.Marshal(value)
		return string(b) + "\n"
	}
	contents := line(100, 10, 100, 10) + line(100, 10, 100, 10) + line(201, 20, 101, 10) + line(401, 40, 100, 10)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewCollector()
	byDate, err := c.readCodexUsageByDate(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	u := byDate["2026-09-07"]
	if u.totalTokens != 441 || u.byContext[100].totalTokens != 110 || u.byContext[101].totalTokens != 111 || u.byContext[-1].totalTokens != 220 {
		t.Fatalf("context buckets: %+v", u)
	}
	u.byContext[100] = codexUsage{totalTokens: 999}
	again, _ := c.readCodexUsageByDate(path, time.UTC)
	if again["2026-09-07"].byContext[100].totalTokens != 110 {
		t.Fatal("caller corrupted cached contexts")
	}
}

func TestUnsupportedContextTierAndMissingRatesRemainUnknown(t *testing.T) {
	for _, raw := range []string{`{"tiers":[{"tier":{"type":"region","size":100},"input":1}]}`, `{"tiers":[{"input":1}]}`} {
		var cost modelsDevCost
		json.Unmarshal([]byte(raw), &cost)
		tiers, err := parseContextPriceTiers(cost)
		if err != nil || len(tiers) != 0 {
			t.Fatalf("unsupported tier accepted: %v %v", tiers, err)
		}
	}
	if validContextPriceTiers([]contextPriceTier{{Above: 100, Input: -1}}) {
		t.Fatal("invalid cached rate accepted")
	}
	p := modelPricing{source: "models.dev", present: rateInput | rateOutput | rateCacheRead, input: 1, output: 2, cacheRead: 0, tiered: true, contextTiers: []contextPriceTier{{Above: 100, Input: 3, Output: 4, Present: rateInput | rateOutput}}}
	selected, ok := requestPricing(p, 101)
	if !ok {
		t.Fatal("tier selection failed")
	}
	if _, known := computeTariffCost(selected, 81, 10, 20, 0); known {
		t.Fatal("missing cache rate became free")
	}
}
