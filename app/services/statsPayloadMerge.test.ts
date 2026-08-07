// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  hasRangeStats,
  mergeStatsPayloads,
  type StatsPayload,
} from './statsPayloadMerge';

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
    const merged = mergeStatsPayloads([
      wirePayload({ all: rangeWith(OPENCODE_MODEL_FIXTURE) }),
    ]);
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
    const merged = mergeStatsPayloads([wirePayload({ all: rangeWith([]) })]);
    expect(merged.ranges.all.models).toEqual([]);
    expect(hasRangeStats(merged.ranges.all)).toBe(false);
  });

  test('duplicate payloads from one daemon merge into a single source', () => {
    const payload = wirePayload({ all: rangeWith(OPENCODE_MODEL_FIXTURE) });
    const merged = mergeStatsPayloads([payload, { ...payload }, { ...payload }]);
    expect(merged.ranges.all.models).toHaveLength(OPENCODE_MODEL_FIXTURE.length);
  });
});
