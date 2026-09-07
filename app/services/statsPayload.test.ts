// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  hasRangeStats,
  normalizeStatsPayload,
  type StatsPayload,
} from './statsPayload';
import { INITIAL_STATS_RANGE } from './statsTabs';

// Clearly synthetic representative OpenCode model rows for the daemon-payload
// merge contract. Model names cover the OpenCode feature surface; every value
// is fictional, and each row's breakdown sums to its totalTokens.
const OPENCODE_MODEL_FIXTURE = [
  {
    name: 'deepseek-v4-flash',
    totalTokens: 290000000,
    totalTokensKnown: true,
    inputTokens: 2500000,
    outputTokens: 520000,
    reasoningTokens: 550000,
    cacheRead: 286420000,
    cacheCreate: 10000,
    tokenBreakdownKnown: true,
    cost: 0.73,
    costKnown: true,
    sessions: 1900,
  },
  {
    name: 'kimi-k2.5-free',
    totalTokens: 4180000,
    totalTokensKnown: true,
    inputTokens: 233000,
    outputTokens: 18000,
    reasoningTokens: 7000,
    cacheRead: 3922000,
    cacheCreate: 0,
    tokenBreakdownKnown: true,
    cost: 0,
    costKnown: true,
    sessions: 117,
  },
];

function wirePayload(ranges): StatsPayload {
  return {
    ranges,
    serverId: 'server-a',
    serverUrl: 'https://daemon-a.test',
    daemonId: 'd'.repeat(64),
    daemonPublicKey: 'k'.repeat(64),
  };
}

function rangeWith(models) {
  return {
    cost: models.reduce((s, m) => s + m.cost, 0),
    costKnown: models.every(m => m.costKnown),
    totalTokens: models.reduce((s, m) => s + m.totalTokens, 0),
    totalTokensKnown: true,
    inputTokens: models.reduce((s, m) => s + m.inputTokens, 0),
    outputTokens: models.reduce((s, m) => s + m.outputTokens, 0),
    reasoningTokens: models.reduce((s, m) => s + m.reasoningTokens, 0),
    cacheRead: models.reduce((s, m) => s + m.cacheRead, 0),
    cacheCreate: models.reduce((s, m) => s + m.cacheCreate, 0),
    tokenBreakdownKnown: true,
    sessions: models.reduce((s, m) => s + m.sessions, 0),
    models,
    projects: [],
    skills: [],
    tools: [],
    days: [],
  };
}

describe('OpenCode local usage contract (daemon payload to App visibility)', () => {
  test('OpenCode model rows survive the transport merge unchanged', () => {
    const merged = normalizeStatsPayload(
      wirePayload({ all: rangeWith(OPENCODE_MODEL_FIXTURE) }),
    );
    expect(merged).not.toBeNull();
    expect(merged.codexSubscriptions).toEqual([]);
    const models = merged.ranges.all.models;
    expect(models).toEqual(OPENCODE_MODEL_FIXTURE);
    expect(models.map(m => m.name)).toContain('deepseek-v4-flash');
    const deepseek = models.find(m => m.name === 'deepseek-v4-flash');
    expect(deepseek.sessions).toBe(1900);
    expect(deepseek.cost).toBeCloseTo(0.73, 6);
    expect(deepseek.costKnown).toBe(true);
  });

  test('the model-usage section shows only when actual usage rows exist', () => {
    expect(hasRangeStats(rangeWith(OPENCODE_MODEL_FIXTURE))).toBe(true);
    expect(hasRangeStats(rangeWith([]))).toBe(false);
    expect(hasRangeStats(null)).toBe(false);
    expect(hasRangeStats(undefined)).toBe(false);
    expect(
      hasRangeStats({
        cost: 0,
        costKnown: true,
        totalTokens: 0,
        sessions: 0,
        models: [],
        projects: [],
        skills: [],
        tools: [],
        days: [],
      }),
    ).toBe(false);
  });

  test('zero-usage payloads merge without inventing rows', () => {
    const merged = normalizeStatsPayload(wirePayload({ all: rangeWith([]) }));
    expect(merged.ranges.all.models).toEqual([]);
    expect(hasRangeStats(merged.ranges.all)).toBe(false);
  });

  test('normalization preserves source rows and refuses a multi-server array', () => {
    const payload = wirePayload({ all: rangeWith(OPENCODE_MODEL_FIXTURE) });
    const merged = normalizeStatsPayload(payload);
    expect(merged.ranges.all.models).toHaveLength(OPENCODE_MODEL_FIXTURE.length);
    expect(merged.ranges.all.models[0]).not.toBe(payload.ranges.all.models[0]);
    expect(normalizeStatsPayload([payload, payload])).toBeNull();
  });
});

describe('Pi owned-session usage contract (daemon payload to App visibility)', () => {
  test('DeepSeek survives the default-week merge within a five-model list', () => {
    const model = (name, totalTokens) => ({
      name,
      totalTokens,
      totalTokensKnown: true,
      inputTokens: totalTokens,
      outputTokens: 0,
      reasoningTokens: 0,
      cacheRead: 0,
      cacheCreate: 0,
      tokenBreakdownKnown: true,
      cost: 0,
      costKnown: true,
      sessions: 1,
    });
    // Fictional values only; the shape mirrors the daemon row derived from
    // Pi's Zen-owned flat JSONL root without carrying any user content.
    const piDeepSeek = model('deepseek-v4-flash', 4_000_000);
    const daemonWeekModels = [
      model('fixture-model-a', 8_000_000),
      model('fixture-model-b', 7_000_000),
      model('fixture-model-c', 6_000_000),
      model('fixture-model-d', 5_000_000),
      piDeepSeek,
    ];

    const merged = normalizeStatsPayload(
      wirePayload({ week: rangeWith(daemonWeekModels) }),
    );
    expect(INITIAL_STATS_RANGE).toBe('week');
    expect(merged.ranges.week.models).toHaveLength(5);
    expect(merged.ranges.week.models).toContainEqual(piDeepSeek);
  });
});

describe('provider-aware current-server costs', () => {
  test('same model through different providers remains separate with provenance', () => {
    const base = {
      name: 'deepseek-v4-flash', totalTokens: 100, inputTokens: 100, outputTokens: 0,
      reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, tokenBreakdownKnown: true,
      totalTokensKnown: true, sessions: 1, costKnown: true,
    };
    const merged = normalizeStatsPayload(wirePayload({ all: rangeWith([
      { ...base, provider: 'deepseek', cost: 0.000022, costReported: 0, costEstimated: 0.000022, costProvenance: 'estimated' as const },
      { ...base, provider: 'opencode-go', cost: 0.00001, costReported: 0.00001, costEstimated: 0, costProvenance: 'reported' as const },
    ]) }));
    expect(merged.ranges.all.models).toHaveLength(2);
    expect(merged.ranges.all.models.map(model => [model.provider, model.costProvenance])).toEqual([
      ['deepseek', 'estimated'], ['opencode-go', 'reported'],
    ]);
  });
});
