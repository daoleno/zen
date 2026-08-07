// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import {
  OPENCODE_GO_PLAN_LABEL,
  isOfficialOpenCodeGoSubscription,
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
    ].filter(isOfficialOpenCodeGoSubscription);

    expect(candidates).toEqual([{ authKind: 'official', state: 'available' }]);
  });
});
