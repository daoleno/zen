// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import { mergeStatsPayloads, type StatsPayload } from './statsPayloadMerge';
import { isOfficialOpenCodeGoSubscription } from './opencodeGoSubscriptionStats';
import { normalizeCodexUsedPercent } from './codexSubscriptionStats';

// The wire payload the live daemon served for the real subscribed account on
// 2026-08-07 (redacted; contains no credentials by design). This is the exact
// `opencodeGoSubscription` value the daemon get_stats handler emits after
// positive confirmation without dashboard credentials.
const LIVE_DAEMON_OPENGCODE_GO_SUBSCRIPTION = {
  authKind: 'official',
  state: 'available',
  plan: 'go',
  fetchedAt: '2026-08-07T15:27:58Z',
  usageAvailable: false,
};

// The documented server-function window shape (see docs/opencode-go-stats.md).
const WINDOWED_OPENGCODE_GO_SUBSCRIPTION = {
  authKind: 'official',
  state: 'available',
  plan: 'go',
  fetchedAt: '2026-08-07T12:00:00Z',
  usageAvailable: true,
  windows: [
    { name: 'rolling', usedPercent: 12.5, limitUsd: 12, resetInSeconds: 3600, resetsAt: '2026-08-07T13:00:00Z' },
    { name: 'weekly', usedPercent: 25, limitUsd: 30, resetInSeconds: 7200, resetsAt: '2026-08-07T14:00:00Z' },
    { name: 'monthly', usedPercent: 50, limitUsd: 60, resetInSeconds: 10800, resetsAt: '2026-08-07T15:00:00Z' },
  ],
};

function wirePayload(subscription): StatsPayload {
  return {
    ranges: { day: { cost: 0, costKnown: true, totalTokens: 0, sessions: 0, models: [], projects: [], skills: [], tools: [], days: [] } },
    opencodeGoSubscription: subscription,
    serverId: 'server-a',
    serverUrl: 'https://daemon-a.test',
    daemonId: 'd'.repeat(64),
    daemonPublicKey: 'k'.repeat(64),
  };
}

describe('OpenCode Go card end-to-end contract (daemon payload to App visibility)', () => {
  test('the live confirmed subscription survives transport merge and renders as a card candidate', () => {
    const merged = mergeStatsPayloads([wirePayload(LIVE_DAEMON_OPENGCODE_GO_SUBSCRIPTION)]);
    expect(merged).not.toBeNull();
    expect(merged.opencodeGoSubscriptions).toHaveLength(1);
    const card = merged.opencodeGoSubscriptions[0];
    expect(isOfficialOpenCodeGoSubscription(card)).toBe(true);
    expect(card.plan).toBe('go');
    expect(card.serverLabel).toBe('server-a');
    // No dashboard credentials on the live host: the card shows the
    // "subscription confirmed, live usage unavailable" notice branch.
    expect(card.usageAvailable).toBe(false);
    expect(card.windows ?? []).toHaveLength(0);
  });

  test('an API-key-only or uncertain account never renders a card', () => {
    const merged = mergeStatsPayloads([
      wirePayload({ authKind: 'api_key', state: 'available' }),
      wirePayload({ authKind: 'unknown', state: 'available' }),
      wirePayload({ authKind: 'official', state: 'unavailable', plan: 'go' }),
    ]);
    expect(merged.opencodeGoSubscriptions).toHaveLength(0);
  });

  test('a payload without the subscription never renders a card', () => {
    const merged = mergeStatsPayloads([wirePayload(undefined)]);
    expect(merged.opencodeGoSubscriptions).toHaveLength(0);
  });

  test('documented usage windows survive the merge with renderable values', () => {
    const merged = mergeStatsPayloads([wirePayload(WINDOWED_OPENGCODE_GO_SUBSCRIPTION)]);
    expect(merged.opencodeGoSubscriptions).toHaveLength(1);
    const card = merged.opencodeGoSubscriptions[0];
    expect(card.usageAvailable).toBe(true);
    const windows = (card.windows ?? []).filter(w => Number.isFinite(w.usedPercent));
    expect(windows.map(w => w.name)).toEqual(['rolling', 'weekly', 'monthly']);
    // The exact values the Stats screen renders (normalized percent + labels).
    expect(windows.map(w => normalizeCodexUsedPercent(w.usedPercent))).toEqual([12.5, 25, 50]);
    expect(windows.map(w => w.limitUsd)).toEqual([12, 30, 60]);
  });

  test('duplicate payloads from one daemon merge into a single card', () => {
    const payload = wirePayload(LIVE_DAEMON_OPENGCODE_GO_SUBSCRIPTION);
    const merged = mergeStatsPayloads([payload, { ...payload }, { ...payload }]);
    expect(merged.opencodeGoSubscriptions).toHaveLength(1);
  });
});
