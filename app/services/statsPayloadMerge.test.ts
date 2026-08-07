// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  hasRangeStats,
  mergeStatsPayloads,
  type StatsPayload,
} from './statsPayloadMerge';

// The exact model rows the daemon emits for the real local account after the
// zero-config OpenCode collection (captured live on 2026-08-07, redacted:
// model IDs and observed numbers only, no credentials or message content).
const LIVE_OPENCODE_MODEL_ROWS = [
  {
    name: 'deepseek-v4-flash',
    totalTokens: 290582798,
    totalTokensKnown: true,
    inputTokens: 2536836,
    outputTokens: 517844,
    reasoningTokens: 553830,
    cacheRead: 286572288,
    cacheCreate: 0,
    tokenBreakdownKnown: true,
    cost: 0.728814,
    costKnown: true,
    sessions: 1902,
  },
  {
    name: 'kimi-k2.5-free',
    totalTokens: 4179180,
    totalTokensKnown: true,
    inputTokens: 233019,
    outputTokens: 18647,
    reasoningTokens: 7386,
    cacheRead: 3920128,
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
  test('live OpenCode model rows survive the transport merge unchanged', () => {
    const merged = mergeStatsPayloads([
      wirePayload({ all: rangeWith(LIVE_OPENCODE_MODEL_ROWS) }),
    ]);
    expect(merged).not.toBeNull();
    expect(merged.codexSubscriptions).toEqual([]);
    const models = merged.ranges.all.models;
    expect(models).toEqual(LIVE_OPENCODE_MODEL_ROWS);
    expect(models.map(m => m.name)).toContain('deepseek-v4-flash');
    const deepseek = models.find(m => m.name === 'deepseek-v4-flash');
    expect(deepseek.sessions).toBe(1902);
    expect(deepseek.cost).toBeCloseTo(0.728814, 6);
    expect(deepseek.costKnown).toBe(true);
  });

  test('the model-usage section shows only when actual usage rows exist', () => {
    expect(hasRangeStats(rangeWith(LIVE_OPENCODE_MODEL_ROWS))).toBe(true);
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
    const payload = wirePayload({ all: rangeWith(LIVE_OPENCODE_MODEL_ROWS) });
    const merged = mergeStatsPayloads([payload, { ...payload }, { ...payload }]);
    expect(merged.ranges.all.models).toHaveLength(LIVE_OPENCODE_MODEL_ROWS.length);
  });
});
