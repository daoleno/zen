package stats

import (
	"reflect"
	"testing"
)

// The price-based estimate for 2000 input tokens of gpt-5.5 (input $5/1M):
// 2000 / 1e6 * 5 = 0.01.
const estimateInput2000GPT55 = 0.01

func recordedEntry(modelID string, cost float64, input int64) modelAggEntry {
	return modelAggEntry{
		totalTokens: input,
		inputTokens: input,
		recorded:    &modelRecordedCost{cost: cost, input: input},
	}
}

func unrecordedEntry(modelID string, input int64) modelAggEntry {
	return modelAggEntry{totalTokens: input, inputTokens: input}
}

func TestModelCostMixedRecordedAndEstimatedSources(t *testing.T) {
	// The same model ID used by an exact-cost source (OpenCode) and an
	// unrecorded source (Codex-style, price known): the aggregate must carry
	// the recorded exact cost AND the price estimate for the unrecorded
	// tokens, never one replacing the other.
	recorded := recordedEntry("gpt-5.5", 0.5, 1000)
	estimated := unrecordedEntry("gpt-5.5", 2000)

	recordedFirst := map[string]modelAggEntry{}
	mergeModelAgg(recordedFirst, map[string]modelAggEntry{"gpt-5.5": recorded})
	mergeModelAgg(recordedFirst, map[string]modelAggEntry{"gpt-5.5": estimated})

	estimatedFirst := map[string]modelAggEntry{}
	mergeModelAgg(estimatedFirst, map[string]modelAggEntry{"gpt-5.5": estimated})
	mergeModelAgg(estimatedFirst, map[string]modelAggEntry{"gpt-5.5": recorded})

	if !reflect.DeepEqual(recordedFirst, estimatedFirst) {
		t.Fatalf("merge order must not matter:\nrecorded-first = %#v\nestimated-first = %#v", recordedFirst, estimatedFirst)
	}

	entry := recordedFirst["gpt-5.5"]
	if entry.recorded == nil || entry.recorded.cost != 0.5 || entry.recorded.input != 1000 {
		t.Fatalf("recorded component lost: %#v", entry)
	}
	cost, known := modelCostFor("gpt-5.5", entry)
	if !known || cost != 0.5+estimateInput2000GPT55 {
		t.Fatalf("cost = %v known = %v, want 0.51 known", cost, known)
	}

	stats := buildModelStats(recordedFirst)
	if len(stats) != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if stats[0].Cost != 0.5+estimateInput2000GPT55 || !stats[0].CostKnown {
		t.Fatalf("model stat = %#v", stats[0])
	}
}

func TestModelCostSameModelAcrossDates(t *testing.T) {
	// One date contributes the exact-cost component, another the unrecorded
	// one; range aggregation must keep both contributions.
	byDate := map[string]*dateAgg{
		"2026-08-07": {
			models:   map[string]modelAggEntry{"gpt-5.5": recordedEntry("gpt-5.5", 0.5, 1000)},
			projects: map[string]*projectAggEntry{},
			tools:    map[string]int{},
			skills:   map[string]*skillEntry{},
		},
		"2026-08-06": {
			models:   map[string]modelAggEntry{"gpt-5.5": unrecordedEntry("gpt-5.5", 2000)},
			projects: map[string]*projectAggEntry{},
			tools:    map[string]int{},
			skills:   map[string]*skillEntry{},
		},
	}
	merged := aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99")
	entry := merged["gpt-5.5"]
	cost, known := modelCostFor("gpt-5.5", entry)
	if !known || cost != 0.5+estimateInput2000GPT55 {
		t.Fatalf("across-dates cost = %v known = %v, want 0.51 known", cost, known)
	}
}

func TestModelCostUnpriceableComponentMakesAggregateUnknown(t *testing.T) {
	// A recorded exact-cost component merged with an unrecorded component
	// whose model has no price: the aggregate cost stays unknown.
	recorded := recordedEntry("mystery-model", 0.25, 500)
	estimated := unrecordedEntry("mystery-model", 3000)
	merged := map[string]modelAggEntry{}
	mergeModelAgg(merged, map[string]modelAggEntry{"mystery-model": recorded})
	mergeModelAgg(merged, map[string]modelAggEntry{"mystery-model": estimated})

	cost, known := modelCostFor("mystery-model", merged["mystery-model"])
	if known {
		t.Fatalf("unpriceable component must make the aggregate unknown (cost = %v)", cost)
	}
	stats := buildModelStats(merged)
	if stats[0].CostKnown {
		t.Fatalf("model stat must report unknown: %#v", stats[0])
	}
	if stats[0].Cost != 0.25 {
		t.Fatalf("cost = %v, want recorded exact 0.25", stats[0].Cost)
	}
}

func TestModelCostExactZeroRecordedStaysKnown(t *testing.T) {
	// A free OpenCode model records cost 0 exactly: no estimate is involved
	// and the cost is known, never flagged unknown.
	entry := recordedEntry("deepseek-v4-flash", 0, 1000)
	cost, known := modelCostFor("deepseek-v4-flash", entry)
	if !known || cost != 0 {
		t.Fatalf("zero recorded cost = %v known = %v, want 0 known", cost, known)
	}
	stats := buildModelStats(map[string]modelAggEntry{"deepseek-v4-flash": entry})
	if stats[0].Cost != 0 || !stats[0].CostKnown {
		t.Fatalf("model stat = %#v", stats[0])
	}
}

func TestModelCostDayCellMatchesModelStat(t *testing.T) {
	// DayCell cost (buildDayCells) and ModelStat cost (buildModelStats) use
	// the same formula on the same mixed entry and must agree.
	byDate := map[string]*dateAgg{
		"2026-08-07": {
			models: map[string]modelAggEntry{
				"gpt-5.5": {
					totalTokens:  3000,
					inputTokens:  3000,
					outputTokens: 500,
					reasoning:    100,
					cacheRead:    2000,
					cacheCreate:  400,
					sessions:     3,
					recorded:     &modelRecordedCost{cost: 0.5, input: 1000, output: 200, reasoning: 50, cacheRead: 500, cacheCreate: 100},
				},
			},
			projects: map[string]*projectAggEntry{},
			tools:    map[string]int{},
			skills:   map[string]*skillEntry{},
		},
	}
	modelAgg := aggregateModelsByDate(byDate, "2026-08-07", "9999-99-99")
	modelStats := buildModelStats(modelAgg)
	if len(modelStats) != 1 {
		t.Fatalf("model stats = %#v", modelStats)
	}
	dayCells := buildDayCells(byDate, map[string]map[string]modelAggEntry{}, "2026-08-07", "9999-99-99")
	if len(dayCells) != 1 {
		t.Fatalf("day cells = %#v", dayCells)
	}
	if dayCells[0].Cost != modelStats[0].Cost || dayCells[0].CostKnown != modelStats[0].CostKnown {
		t.Fatalf("day cell %#v disagrees with model stat %#v", dayCells[0], modelStats[0])
	}
}

func TestModelCostSingleSourceUnchanged(t *testing.T) {
	// Unrecorded-only entries keep the exact previous semantics: the cost is
	// the pricing-table estimate of their own tokens.
	entry := modelAggEntry{totalTokens: 7000, inputTokens: 5000, outputTokens: 1000, reasoning: 200, cacheRead: 2000, cacheCreate: 100}
	expected, expectedKnown := computeKnownCost("gpt-5.5", 5000, 1000, 200, 2000, 100)
	cost, known := modelCostFor("gpt-5.5", entry)
	if cost != expected || known != expectedKnown {
		t.Fatalf("single-source cost = %v known = %v, want %v %v", cost, known, expected, expectedKnown)
	}

	// Unpriceable single source stays unknown.
	unpriceable := unrecordedEntry("mystery-model", 3000)
	_, known = modelCostFor("mystery-model", unpriceable)
	if known {
		t.Fatal("unpriceable single source must stay unknown")
	}
}
