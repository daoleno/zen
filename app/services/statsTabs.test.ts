// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import {
  INITIAL_STATS_RANGE,
  STATS_RANGE_TABS,
  statsRangeAt,
  statsRangeIndex,
} from './statsTabs';

describe('Stats range tabs', () => {
  it('keeps the existing Week initial selection', () => {
    expect(INITIAL_STATS_RANGE).toBe('week');
    expect(statsRangeIndex(INITIAL_STATS_RANGE)).toBe(1);
  });

  it('maps every tap and pager index through the same stable order', () => {
    expect(STATS_RANGE_TABS.map((tab) => statsRangeAt(statsRangeIndex(tab.key))))
      .toEqual(STATS_RANGE_TABS.map((tab) => tab.key));
  });

  it('bounds pager indices instead of creating an invalid selection', () => {
    expect(statsRangeAt(-1)).toBe('day');
    expect(statsRangeAt(STATS_RANGE_TABS.length)).toBe('all');
    expect(statsRangeAt(Number.POSITIVE_INFINITY)).toBe('day');
    expect(statsRangeAt(Number.NaN)).toBe('day');
  });
});
