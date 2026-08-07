// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  OPENCODE_GO_PLAN_LABEL,
  isOfficialOpenCodeGoSubscription,
  openCodeGoLimitLabel,
  openCodeGoWindowLabel,
} from './opencodeGoSubscriptionStats';

describe('OpenCode Go subscription stats presentation', () => {
  test('labels the Go plan from official confirmation only', () => {
    expect(OPENCODE_GO_PLAN_LABEL).toBe('OpenCode Go');
  });

  test('only a confirmed official subscription is a card candidate', () => {
    const candidates = [
      undefined,
      null,
      { authKind: 'absent', state: 'unavailable' },
      { authKind: 'api_key', state: 'available' },
      { authKind: 'unknown', state: 'available' },
      { authKind: 'official', state: 'unavailable' },
      { authKind: 'official', state: 'unavailable', plan: 'go' },
      { authKind: 'official', state: 'available' },
      { authKind: 'official', state: 'available', usageAvailable: true, windows: [] },
    ].filter(isOfficialOpenCodeGoSubscription);

    expect(candidates).toEqual([
      { authKind: 'official', state: 'available' },
      { authKind: 'official', state: 'available', usageAvailable: true, windows: [] },
    ]);
  });

  test('labels the three documented usage windows', () => {
    expect(openCodeGoWindowLabel('rolling')).toBe('5 hours');
    expect(openCodeGoWindowLabel('weekly')).toBe('Weekly');
    expect(openCodeGoWindowLabel('monthly')).toBe('Monthly');
    expect(openCodeGoWindowLabel('<drift>')).toBe('Usage window');
  });

  test('formats documented plan limits without inventing values', () => {
    expect(openCodeGoLimitLabel(12)).toBe('$12');
    expect(openCodeGoLimitLabel(30)).toBe('$30');
    expect(openCodeGoLimitLabel(60)).toBe('$60');
    expect(openCodeGoLimitLabel(undefined)).toBe('');
    expect(openCodeGoLimitLabel(Number.NaN)).toBe('');
  });
});
