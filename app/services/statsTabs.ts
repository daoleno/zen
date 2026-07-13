export const STATS_RANGE_TABS = [
  { key: 'day', label: 'Day' },
  { key: 'week', label: 'Week' },
  { key: 'month', label: 'Month' },
  { key: 'all', label: 'All' },
] as const;

export type StatsRange = (typeof STATS_RANGE_TABS)[number]['key'];

export const INITIAL_STATS_RANGE: StatsRange = 'week';

export function statsRangeIndex(range: StatsRange): number {
  return STATS_RANGE_TABS.findIndex((tab) => tab.key === range);
}

export function statsRangeAt(index: number): StatsRange {
  const normalizedIndex = Number.isFinite(index) ? Math.trunc(index) : 0;
  const boundedIndex = Math.max(
    0,
    Math.min(STATS_RANGE_TABS.length - 1, normalizedIndex),
  );
  return STATS_RANGE_TABS[boundedIndex].key;
}
