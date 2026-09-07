import { describe, expect, test } from "bun:test";
import { EMPTY_RANGE, normalizeStatsPayload } from "./statsPayload";

describe("pricing reference status", () => {
  test("preserves source, freshness, errors and model cost components", () => {
    const pricing = {
      basis: "Catalog reference estimates, not actual gateway bills.",
      source: "models.dev",
      updatedAt: "2026-09-06T00:00:00Z",
      stale: true,
      lastError: "Pricing refresh failed; retained previous reference estimates.",
    };
    const model = {
      id: "exact-model", name: "Exact model", reportedCost: 2,
      estimatedCost: 3, estimateSource: "models.dev", referenceProvider: "openai",
      unpricedReason: "insufficient_context" as const,
      cost: 5, costKnown: false, totalTokens: 10, inputTokens: 10,
      outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 1,
    };
    const view = normalizeStatsPayload({ ranges: { all: { ...EMPTY_RANGE, models: [model] } }, pricing });
    expect(view?.pricing).toEqual(pricing);
    expect(view?.pricing).not.toBe(pricing);
    expect(view?.ranges.all.models[0]).toEqual(model);
  });

  test("a current-server replacement does not retain the previous pricing status", () => {
    const first = normalizeStatsPayload({ ranges: {}, pricing: {
      basis: "estimate", source: "models.dev", stale: true, lastError: "offline",
    } });
    const next = normalizeStatsPayload({ ranges: {}, pricing: {
      basis: "estimate", source: "built-in", stale: false,
    } });
    expect(first?.pricing?.lastError).toBe("offline");
    expect(next?.pricing?.lastError).toBeUndefined();
    expect(next?.pricing?.source).toBe("built-in");
  });
});
