// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import { SCREENSHOT_STATS_FIXTURE } from './screenshotDemoFixtures';

describe('Stats screenshot fixture coherence', () => {
  test('range totals equal the intended sum of model rows under Stats semantics', () => {
    const range = SCREENSHOT_STATS_FIXTURE.ranges.all;
    const models = range.models;
    const sum = (field) => models.reduce((s, m) => s + (m[field] ?? 0), 0);

    expect(range.cost).toBeCloseTo(sum('cost'), 6);
    expect(range.sessions).toBe(sum('sessions'));
    expect(range.totalTokens).toBe(sum('totalTokens'));
    expect(range.inputTokens).toBe(sum('inputTokens'));
    expect(range.outputTokens).toBe(sum('outputTokens'));
    expect(range.reasoningTokens).toBe(sum('reasoningTokens'));
    expect(range.cacheRead).toBe(sum('cacheRead'));
    expect(range.cacheCreate).toBe(sum('cacheCreate'));

    // The range is known only when every model row is known.
    expect(range.costKnown).toBe(models.every((m) => m.costKnown !== false));
    expect(range.totalTokensKnown).toBe(models.every((m) => m.totalTokensKnown !== false));
    expect(range.tokenBreakdownKnown).toBe(models.every((m) => m.tokenBreakdownKnown !== false));
  });

  test('every model row carries a complete, internally consistent breakdown', () => {
    for (const m of SCREENSHOT_STATS_FIXTURE.ranges.all.models) {
      expect(m.totalTokens).toBe(
        (m.inputTokens ?? 0) +
          (m.outputTokens ?? 0) +
          (m.reasoningTokens ?? 0) +
          (m.cacheRead ?? 0) +
          (m.cacheCreate ?? 0),
      );
      expect(m.costKnown).toBe(true);
      expect(m.totalTokensKnown).toBe(true);
      expect(m.tokenBreakdownKnown).toBe(true);
      expect(m.sessions).toBeGreaterThan(0);
    }
  });

  test('subscription card fixture remains intact', () => {
    expect(SCREENSHOT_STATS_FIXTURE.codexSubscriptions).toHaveLength(1);
    const card = SCREENSHOT_STATS_FIXTURE.codexSubscriptions[0];
    expect(card.authKind).toBe('official');
    expect(card.state).toBe('available');
    expect(card.plan).toBe('plus');
    expect(card.windows).toHaveLength(2);
    expect(card.windows[0].usedPercent).toBe(34);
    expect(card.windows[1].usedPercent).toBe(18);
  });
});
