// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  codexAuthSummary,
  codexRemainingPercent,
  codexPlanLabel,
  codexWindowLabel,
  isOfficialCodexSubscription,
  normalizeCodexUsedPercent,
} from './codexSubscriptionStats';

describe('Codex subscription stats presentation', () => {
  test('treats backend percentages as consumed and derives remaining', () => {
    expect(codexRemainingPercent(0)).toBe(100);
    expect(codexRemainingPercent(25.5)).toBe(74.5);
    expect(codexRemainingPercent(100)).toBe(0);
  });

  test('bounds malformed percentage values for layout safety', () => {
    expect(normalizeCodexUsedPercent(-5)).toBe(0);
    expect(normalizeCodexUsedPercent(120)).toBe(100);
    expect(normalizeCodexUsedPercent(Number.NaN)).toBe(0);
  });

  test('labels common rolling windows', () => {
    expect(codexWindowLabel(300)).toBe('5 hours');
    expect(codexWindowLabel(10_080)).toBe('1 week');
  });

  test('uses polished plan labels without trusting arbitrary backend text', () => {
    expect(codexPlanLabel('plus')).toBe('ChatGPT Plus');
    expect(codexPlanLabel('enterprise')).toBe('ChatGPT Enterprise');
    expect(codexPlanLabel('<unknown>')).toBe('ChatGPT plan');
  });

  test('does not present API keys or unknown auth as subscriptions', () => {
    expect(codexAuthSummary('official')).toBe('ChatGPT plan');
    expect(codexAuthSummary('api_key')).toBe('API key authentication');
    expect(codexAuthSummary('unknown')).toBe('Authentication unavailable');
  });

  test('only official auth produces a subscription card candidate', () => {
    const candidates = [
      undefined,
      { authKind: 'absent', state: 'unavailable' },
      { authKind: 'api_key', state: 'unavailable' },
      { authKind: 'unknown', state: 'unavailable' },
      { authKind: 'official', state: 'unavailable' },
    ].filter(isOfficialCodexSubscription);

    expect(candidates).toEqual([{ authKind: 'official', state: 'unavailable' }]);
  });
});
