import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated as NativeAnimated,
  type LayoutChangeEvent,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from 'react-native';
import { useFocusEffect } from 'expo-router';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import * as Haptics from 'expo-haptics';
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  useReducedMotion,
  withTiming,
  interpolate,
} from 'react-native-reanimated';
import { TabView } from 'react-native-tab-view';
import {
  Colors,
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppTheme,
} from '../constants/tokens';
import { useAgents } from '../store/agents';
import { wsClient } from '../services/websocket';
import { AnimatedPressable } from '../components/ui/AnimatedPressable';
import { RisingSheet } from '../components/ui/RisingSheet';
import {
  codexAuthSummary,
  codexRemainingPercent,
  codexPlanLabel,
  codexWindowLabel,
  normalizeCodexUsedPercent,
  isOfficialCodexSubscription,
} from '../services/codexSubscriptionStats';
import {
  INITIAL_STATS_RANGE,
  STATS_RANGE_TABS,
  statsRangeAt,
  statsRangeIndex,
  type StatsRange,
} from '../services/statsTabs';

// ── Types (mirror daemon/stats/types.go) ───────────────────

type TimeRange = StatsRange;

interface DayCell {
  date: string;
  totalTokens: number;
  totalTokensKnown?: boolean;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  tokenBreakdownKnown?: boolean;
  cost: number;
  costKnown?: boolean;
  sessions: number;
}

interface ModelStat {
  name: string;
  totalTokens: number;
  totalTokensKnown?: boolean;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  tokenBreakdownKnown?: boolean;
  cost: number;
  costKnown?: boolean;
  sessions: number;
}

interface ProjectStat {
  name: string;
  totalTokens: number;
  totalTokensKnown?: boolean;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  tokenBreakdownKnown?: boolean;
  cost: number;
  costKnown?: boolean;
  sessions: number;
}

interface SkillStat {
  name: string;
  calls: number;
  projects: string[];
}

interface ToolStat {
  name: string;
  calls: number;
}

interface RangeData {
  cost: number;
  costKnown?: boolean;
  totalTokens: number;
  totalTokensKnown?: boolean;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  tokenBreakdownKnown?: boolean;
  sessions: number;
  models: ModelStat[];
  projects: ProjectStat[];
  skills: SkillStat[];
  tools: ToolStat[];
  days: DayCell[];
}

interface StatsPayload {
  ranges: Record<string, RangeData>;
  codexSubscription?: CodexSubscriptionUsage;
  codexSubscriptions?: CodexSubscriptionUsage[];
  serverId?: string;
  serverUrl?: string;
  daemonId?: string;
  daemonPublicKey?: string;
}

interface CodexUsageWindow {
  name: 'primary' | 'secondary' | string;
  usedPercent: number;
  windowMinutes?: number;
  resetsAt?: string;
}

interface CodexSubscriptionUsage {
  authKind: 'official' | 'api_key' | 'absent' | 'unknown';
  state: 'available' | 'unavailable';
  plan?: string;
  windows?: CodexUsageWindow[];
  fetchedAt?: string;
  stale?: boolean;
  serverLabel?: string;
}

// ── Constants ──────────────────────────────────────────────

const EMPTY_RANGE: RangeData = {
  cost: 0, costKnown: true, totalTokens: 0, totalTokensKnown: true, inputTokens: 0, outputTokens: 0,
  reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, tokenBreakdownKnown: true,
  sessions: 0, models: [], projects: [], skills: [], tools: [], days: [],
};

const RANGE_OPTIONS = STATS_RANGE_TABS;
const STATS_TAB_ROUTES: Array<{ key: TimeRange; label: string }> = [
  ...STATS_RANGE_TABS,
];

const MAX_LIST_ITEMS = 5;
const EMPTY_STATS_RETRY_MS = 700;
const EMPTY_STATS_MAX_RETRIES = 3;
const STATS_CONTENT_MAX_WIDTH = 760;
const STATS_TAB_BAR_GAP = 4;
const STATS_TAB_BAR_PADDING = 3;

// ── Helpers ────────────────────────────────────────────────

function barIntensity(value: number, maxValue: number): number {
  if (value <= 0) return 0;
  const ratio = value / maxValue;
  if (ratio < 0.15) return 1;
  if (ratio < 0.5) return 2;
  return 3;
}

function fmtCompact(n: number, units: readonly [number, string][]): string {
  const absolute = Math.abs(n);
  for (const [threshold, suffix] of units) {
    if (absolute >= threshold) {
      return `${(n / threshold).toFixed(1).replace(/\.0$/, '')}${suffix}`;
    }
  }
  return n.toString();
}

function fmt(n: number): string {
  return fmtCompact(n, [
    [1_000_000_000_000, 'T'],
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'K'],
  ]);
}

function fmtCost(n: number): string {
  if (Math.abs(n) >= 1_000) {
    return '$' + fmtCompact(n, [
      [1_000_000_000_000, 'T'],
      [1_000_000_000, 'B'],
      [1_000_000, 'M'],
      [1_000, 'K'],
    ]);
  }
  if (n >= 100) return '$' + n.toFixed(0);
  return '$' + n.toFixed(2);
}

function isCostKnown(item: { costKnown?: boolean }): boolean {
  return item.costKnown !== false;
}

function isTotalTokensKnown(item: { totalTokensKnown?: boolean }): boolean {
  return item.totalTokensKnown !== false;
}

function isTokenBreakdownKnown(item: { tokenBreakdownKnown?: boolean }): boolean {
  return item.tokenBreakdownKnown !== false;
}

function fmtAvailable(value: number, available: boolean | undefined, formatter: (n: number) => string): string {
  if (available === false) return value > 0 ? `${formatter(value)}+` : '—';
  return formatter(value);
}

function fmtAvailableCost(cost: number, costKnown?: boolean): string {
  return fmtAvailable(cost, costKnown, fmtCost);
}

function fmtAvailableTokens(tokens: number, totalTokensKnown?: boolean): string {
  return fmtAvailable(tokens, totalTokensKnown, fmt);
}

function sessionSummary(sessions: number): string {
  return `${sessions} ${sessions === 1 ? 'session' : 'sessions'}`;
}

function rowActivitySummary(item: {
  totalTokens: number;
  totalTokensKnown?: boolean;
  sessions: number;
}): string {
  if (!isTotalTokensKnown(item) && item.totalTokens === 0) return sessionSummary(item.sessions);
  return `${fmtAvailableTokens(item.totalTokens, item.totalTokensKnown)} tokens · ${sessionSummary(item.sessions)}`;
}

type ActivityStat = {
  name: string;
  totalTokens: number;
  totalTokensKnown?: boolean;
  sessions: number;
};

function activityUsesTokens(items: { totalTokens: number; totalTokensKnown?: boolean }[]): boolean {
  return items.every(item => isTotalTokensKnown(item) || item.totalTokens > 0);
}

function activityValue(
  item: { totalTokens: number; sessions: number },
  usesTokens: boolean,
): number {
  return usesTokens ? item.totalTokens : item.sessions;
}

function sortByActivity<T extends ActivityStat>(items: T[]): T[] {
  const usesTokens = activityUsesTokens(items);
  return [...items].sort((a, b) =>
    activityValue(b, usesTokens) - activityValue(a, usesTokens) || a.name.localeCompare(b.name),
  );
}

function dayActivityIntensity(
  day: DayCell,
  maxActivity: number,
  usesTokens: boolean,
): number {
  return barIntensity(activityValue(day, usesTokens), maxActivity);
}

function shortDate(dateStr: string): string {
  // "2026-04-04" -> "4/4"
  const parts = dateStr.split('-');
  if (parts.length < 3) return dateStr;
  return `${parseInt(parts[1])}/${parseInt(parts[2])}`;
}

function weekdayShort(dateStr: string): string {
  const date = parseDateOnly(dateStr);
  if (!date) return '';
  return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][date.getDay()];
}

function monthShort(dateStr: string): string {
  const date = parseDateOnly(dateStr);
  if (!date) return '';
  return ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][date.getMonth()];
}

function accessibleDate(dateStr: string): string {
  const date = parseDateOnly(dateStr);
  if (!date) return dateStr;
  const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  const months = [
    'January', 'February', 'March', 'April', 'May', 'June',
    'July', 'August', 'September', 'October', 'November', 'December',
  ];
  return `${weekdays[date.getDay()]}, ${months[date.getMonth()]} ${date.getDate()}, ${date.getFullYear()}`;
}

function dayAccessibilityValue(day: DayCell): string {
  return [
    `${fmtAvailableTokens(day.totalTokens, day.totalTokensKnown)} tokens`,
    sessionSummary(day.sessions),
    `${fmtAvailableCost(day.cost, day.costKnown)} estimated cost`,
  ].join(', ');
}

function parseDateOnly(dateStr: string): Date | null {
  const parts = dateStr.split('-').map((part) => Number(part));
  if (parts.length < 3 || parts.some((part) => !Number.isFinite(part))) return null;
  return new Date(parts[0], parts[1] - 1, parts[2], 12, 0, 0, 0);
}

function dateOnlyString(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, '0');
  const day = `${date.getDate()}`.padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function todayDateOnly(): Date {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate(), 12, 0, 0, 0);
}

function topItems<T>(items: T[]): T[] {
  return items.slice(0, MAX_LIST_ITEMS);
}

function mergeModelStats(items: ModelStat[]): ModelStat[] {
  const merged = new Map<string, ModelStat>();
  for (const item of items) {
    const current = merged.get(item.name) ?? {
      name: item.name, totalTokens: 0, inputTokens: 0, outputTokens: 0,
      reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, costKnown: true,
      totalTokensKnown: true, tokenBreakdownKnown: true, sessions: 0,
    };
    current.totalTokens += item.totalTokens;
    current.inputTokens += item.inputTokens;
    current.outputTokens += item.outputTokens;
    current.reasoningTokens += item.reasoningTokens;
    current.cacheRead += item.cacheRead;
    current.cacheCreate += item.cacheCreate;
    current.cost += item.cost;
    current.costKnown = isCostKnown(current) && isCostKnown(item);
    current.totalTokensKnown = isTotalTokensKnown(current) && isTotalTokensKnown(item);
    current.tokenBreakdownKnown = isTokenBreakdownKnown(current) && isTokenBreakdownKnown(item);
    current.sessions += item.sessions;
    merged.set(item.name, current);
  }
  return [...merged.values()];
}

function mergeProjectStats(items: ProjectStat[]): ProjectStat[] {
  const merged = new Map<string, ProjectStat>();
  for (const item of items) {
    const current = merged.get(item.name) ?? {
      name: item.name, totalTokens: 0, inputTokens: 0, outputTokens: 0,
      reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, costKnown: true,
      totalTokensKnown: true, tokenBreakdownKnown: true, sessions: 0,
    };
    current.totalTokens += item.totalTokens;
    current.inputTokens += item.inputTokens;
    current.outputTokens += item.outputTokens;
    current.reasoningTokens += item.reasoningTokens;
    current.cacheRead += item.cacheRead;
    current.cacheCreate += item.cacheCreate;
    current.cost += item.cost;
    current.costKnown = isCostKnown(current) && isCostKnown(item);
    current.totalTokensKnown = isTotalTokensKnown(current) && isTotalTokensKnown(item);
    current.tokenBreakdownKnown = isTokenBreakdownKnown(current) && isTokenBreakdownKnown(item);
    current.sessions += item.sessions;
    merged.set(item.name, current);
  }
  return [...merged.values()];
}

function mergeSkillStats(items: SkillStat[]): SkillStat[] {
  const merged = new Map<string, { calls: number; projects: Set<string> }>();
  for (const item of items) {
    const current = merged.get(item.name) ?? { calls: 0, projects: new Set<string>() };
    current.calls += item.calls;
    for (const p of item.projects ?? []) current.projects.add(p);
    merged.set(item.name, current);
  }
  return [...merged.entries()]
    .map(([name, v]) => ({ name, calls: v.calls, projects: [...v.projects].sort() }))
    .sort((a, b) => b.calls - a.calls);
}

function mergeToolStats(items: ToolStat[]): ToolStat[] {
  const merged = new Map<string, number>();
  for (const item of items) merged.set(item.name, (merged.get(item.name) ?? 0) + item.calls);
  return [...merged.entries()]
    .map(([name, calls]) => ({ name, calls }))
    .sort((a, b) => b.calls - a.calls);
}

function mergeDays(arrays: DayCell[][]): DayCell[] {
  const merged = new Map<string, DayCell>();
  for (const arr of arrays) {
    for (const d of arr ?? []) {
      const c = merged.get(d.date) ?? {
        date: d.date, totalTokens: 0, inputTokens: 0, outputTokens: 0,
        reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, costKnown: true,
        totalTokensKnown: true, tokenBreakdownKnown: true, sessions: 0,
      };
      c.totalTokens += d.totalTokens;
      c.inputTokens += d.inputTokens;
      c.outputTokens += d.outputTokens;
      c.reasoningTokens += d.reasoningTokens;
      c.cacheRead += d.cacheRead;
      c.cacheCreate += d.cacheCreate;
      c.cost += d.cost;
      c.costKnown = isCostKnown(c) && isCostKnown(d);
      c.totalTokensKnown = isTotalTokensKnown(c) && isTotalTokensKnown(d);
      c.tokenBreakdownKnown = isTokenBreakdownKnown(c) && isTokenBreakdownKnown(d);
      c.sessions += d.sessions;
      merged.set(d.date, c);
    }
  }
  return [...merged.values()].sort((a, b) => a.date.localeCompare(b.date));
}

function mergeRangeData(items: RangeData[]): RangeData {
  if (items.length === 0) return EMPTY_RANGE;
  return {
    cost: items.reduce((s, i) => s + i.cost, 0),
    costKnown: items.every(isCostKnown),
    totalTokens: items.reduce((s, i) => s + i.totalTokens, 0),
    totalTokensKnown: items.every(isTotalTokensKnown),
    inputTokens: items.reduce((s, i) => s + i.inputTokens, 0),
    outputTokens: items.reduce((s, i) => s + i.outputTokens, 0),
    reasoningTokens: items.reduce((s, i) => s + i.reasoningTokens, 0),
    cacheRead: items.reduce((s, i) => s + i.cacheRead, 0),
    cacheCreate: items.reduce((s, i) => s + i.cacheCreate, 0),
    tokenBreakdownKnown: items.every(isTokenBreakdownKnown),
    sessions: items.reduce((s, i) => s + i.sessions, 0),
    models: mergeModelStats(items.flatMap(i => i.models ?? [])),
    projects: mergeProjectStats(items.flatMap(i => i.projects ?? [])),
    skills: mergeSkillStats(items.flatMap(i => i.skills ?? [])),
    tools: mergeToolStats(items.flatMap(i => i.tools ?? [])),
    days: mergeDays(items.map(i => i.days ?? [])),
  };
}

function mergeStatsPayloads(payloads: StatsPayload[]): StatsPayload | null {
  const uniquePayloads = uniqueStatsPayloads(payloads);
  if (uniquePayloads.length === 0) return null;
  const rangeKeys = new Set<string>();
  for (const p of uniquePayloads) for (const k of Object.keys(p.ranges ?? {})) rangeKeys.add(k);
  const ranges: Record<string, RangeData> = {};
  for (const k of rangeKeys) {
    ranges[k] = mergeRangeData(uniquePayloads.map(p => p.ranges?.[k] ?? EMPTY_RANGE));
  }
  const codexSubscriptions = uniquePayloads
    .filter(p => isOfficialCodexSubscription(p.codexSubscription))
    .map(p => ({
      ...p.codexSubscription!,
      windows: p.codexSubscription!.windows?.map(window => ({ ...window })),
      serverLabel: p.serverId ?? p.serverUrl,
    }));
  return { ranges, codexSubscriptions };
}

function fmtReset(value?: string): string {
  if (!value) return 'Reset time unavailable';
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return 'Reset time unavailable';
  return `Resets ${date.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
}

function uniqueStatsPayloads(payloads: StatsPayload[]): StatsPayload[] {
  const seen = new Set<string>();
  const out: StatsPayload[] = [];
  for (const payload of payloads) {
    const key =
      payload.daemonId && payload.daemonPublicKey
        ? `daemon:${payload.daemonId}:${payload.daemonPublicKey}`
        : payload.serverUrl
          ? `url:${payload.serverUrl}`
          : payload.serverId
            ? `server:${payload.serverId}`
            : `payload:${out.length}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(payload);
  }
  return out;
}

function hasRangeStats(data?: RangeData | null): boolean {
  if (!data) return false;
  return data.sessions > 0 ||
    data.cost > 0 ||
    data.totalTokens > 0 ||
    (data.models?.length ?? 0) > 0 ||
    (data.projects?.length ?? 0) > 0 ||
    (data.skills?.length ?? 0) > 0 ||
    (data.tools?.length ?? 0) > 0 ||
    (data.days?.length ?? 0) > 0;
}

function dayOfWeekMon0(dateStr: string): number {
  const d = new Date(`${dateStr}T12:00:00`);
  return (d.getDay() + 6) % 7;
}

type ActivityDay = {
  date: string;
  day: DayCell | null;
};

function buildDailyActivitySeries(days: DayCell[], range: TimeRange): ActivityDay[] {
  const byDate = new Map(days.map((day) => [day.date, day]));
  const sortedDates = [...byDate.keys()].sort();
  const latestDataDate = sortedDates.length > 0 ? parseDateOnly(sortedDates[sortedDates.length - 1]) : null;
  const today = todayDateOnly();
  const end =
    latestDataDate && latestDataDate > today && range === 'all'
      ? latestDataDate
      : today;
  let start: Date;
  switch (range) {
    case 'day':
      start = end;
      break;
    case 'week':
      start = addDays(end, -6);
      break;
    case 'month':
      start = addDays(end, -30);
      break;
    case 'all':
    default:
      start = sortedDates.length > 0 ? parseDateOnly(sortedDates[0]) ?? end : end;
      break;
  }

  const out: ActivityDay[] = [];
  for (let cursor = start; cursor <= end; cursor = addDays(cursor, 1)) {
    const date = dateOnlyString(cursor);
    out.push({ date, day: byDate.get(date) ?? null });
  }
  return out;
}

function buildActivityCalendarColumns(days: ActivityDay[]): (ActivityDay | null)[][] {
  if (days.length === 0) return [];
  const columns: (ActivityDay | null)[][] = [];
  let col: (ActivityDay | null)[] = Array(7).fill(null);

  for (const day of days) {
    const row = dayOfWeekMon0(day.date);
    if (row === 0 && col.some((cell) => cell !== null)) {
      columns.push(col);
      col = Array(7).fill(null);
    }
    col[row] = day;
  }
  if (col.some((cell) => cell !== null)) columns.push(col);
  return columns;
}

// ── Component ──────────────────────────────────────────────

export default function StatsScreen() {
  const { colors } = useAppTheme();
  const s = useMemo(() => createStyles(colors), [colors]);
  const { width } = useWindowDimensions();
  const reducedMotion = useReducedMotion();
  const { state: agentsState } = useAgents();
  const [range, setRange] = useState<TimeRange>(INITIAL_STATS_RANGE);
  const [statsData, setStatsData] = useState<StatsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set());
  const [selectedDay, setSelectedDay] = useState<DayCell | null>(null);
  const [nestedHorizontalScrollActive, setNestedHorizontalScrollActive] = useState(false);
  const [rangeTabBarWidth, setRangeTabBarWidth] = useState(0);
  const statsDataRef = useRef<StatsPayload | null>(null);

  useEffect(() => {
    statsDataRef.current = statsData;
  }, [statsData]);

  const toggleSection = useCallback((section: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const connectedServerIds = useMemo(
    () =>
      Object.entries(agentsState.serverConnections)
        .filter(([, state]) => state === 'connected')
        .map(([serverId]) => serverId)
        .sort(),
    [agentsState.serverConnections],
  );
  const hasConnectingServer = useMemo(
    () => Object.values(agentsState.serverConnections).includes('connecting'),
    [agentsState.serverConnections],
  );
  const connectedServerIdsKey = useMemo(
    () => connectedServerIds.join('|'),
    [connectedServerIds],
  );

  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      let retryTimer: ReturnType<typeof setTimeout> | null = null;

      const loadStats = (attempt: number) => {
        retryTimer = null;
        const liveServerIds = connectedServerIds.filter((id) => wsClient.isConnected(id));
        if (liveServerIds.length === 0) {
          if (!statsDataRef.current) {
            setStatsData(null);
          }
          setLoading(!statsDataRef.current && hasConnectingServer);
          return;
        }

        setLoading(!statsDataRef.current);
        Promise.allSettled(liveServerIds.map(id => wsClient.getStats(id)))
          .then(results => {
            if (cancelled) return;
            const payloads = results
              .filter((r): r is PromiseFulfilledResult<StatsPayload> => r.status === 'fulfilled')
              .map(r => r.value);
            const merged = mergeStatsPayloads(payloads);
            const rangesReady = Object.keys(merged?.ranges ?? {}).length > 0;

            if (!rangesReady && attempt < EMPTY_STATS_MAX_RETRIES) {
              retryTimer = setTimeout(
                () => loadStats(attempt + 1),
                EMPTY_STATS_RETRY_MS * (attempt + 1),
              );
              return;
            }

            statsDataRef.current = merged;
            setStatsData(merged);
          })
          .catch(() => {})
          .finally(() => {
            if (!cancelled && !retryTimer) setLoading(false);
          });
      };

      loadStats(0);

      return () => {
        cancelled = true;
        if (retryTimer) clearTimeout(retryTimer);
      };
    }, [connectedServerIdsKey, hasConnectingServer]),
  );

  const selectRangeIndex = useCallback((index: number) => {
    const nextRange = statsRangeAt(index);
    if (nextRange === range) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setRange(nextRange);
  }, [range]);
  const beginNestedHorizontalGesture = useCallback(() => {
    setNestedHorizontalScrollActive(true);
  }, []);
  const endNestedHorizontalGesture = useCallback(() => {
    setNestedHorizontalScrollActive(false);
  }, []);
  const handleRangeTabBarLayout = useCallback((event: LayoutChangeEvent) => {
    setRangeTabBarWidth(event.nativeEvent.layout.width);
  }, []);

  return (
    <SafeAreaView style={s.container} edges={[]}>
      <TabView<{ key: TimeRange; label: string }>
        navigationState={{ index: statsRangeIndex(range), routes: STATS_TAB_ROUTES }}
        initialLayout={{ width }}
        animationEnabled={!reducedMotion}
        lazy={false}
        overScrollMode="never"
        swipeEnabled={!nestedHorizontalScrollActive}
        renderTabBar={({ position, jumpTo }) => (
          <StatsRangeTabBar
            position={position}
            range={range}
            width={rangeTabBarWidth}
            onLayout={handleRangeTabBarLayout}
            jumpTo={jumpTo}
            styles={s}
          />
        )}
        onIndexChange={selectRangeIndex}
        renderScene={({ route }) => (
          <StatsRangeScene
            range={route.key}
            active={route.key === range}
            statsData={statsData}
            loading={loading}
            expandedSections={expandedSections}
            toggleSection={toggleSection}
            selectedDay={selectedDay}
            setSelectedDay={setSelectedDay}
            onNestedHorizontalGestureStart={beginNestedHorizontalGesture}
            onNestedHorizontalGestureEnd={endNestedHorizontalGesture}
          />
        )}
        style={s.pager}
      />

      <DayDetailSheet selectedDay={selectedDay} onClose={() => setSelectedDay(null)} />
    </SafeAreaView>
  );
}

function StatsRangeTabBar({
  position,
  range,
  width,
  onLayout,
  jumpTo,
  styles,
}: {
  position: NativeAnimated.AnimatedInterpolation<number>;
  range: TimeRange;
  width: number;
  onLayout(event: LayoutChangeEvent): void;
  jumpTo(key: string): void;
  styles: ReturnType<typeof createStyles>;
}) {
  const tabWidth = Math.max(
    0,
    (width - STATS_TAB_BAR_PADDING * 2 - STATS_TAB_BAR_GAP * (RANGE_OPTIONS.length - 1)) /
      RANGE_OPTIONS.length,
  );
  const translateX = position.interpolate({
    inputRange: RANGE_OPTIONS.map((_, index) => index),
    outputRange: RANGE_OPTIONS.map((_, index) => index * (tabWidth + STATS_TAB_BAR_GAP)),
    extrapolate: 'clamp',
  });

  return (
    <View style={styles.header}>
      <View style={styles.rangeRow} onLayout={onLayout}>
        {tabWidth > 0 && (
          <NativeAnimated.View
            pointerEvents="none"
            style={[
              styles.rangeTabIndicator,
              { width: tabWidth, transform: [{ translateX }] },
            ]}
          />
        )}
        {RANGE_OPTIONS.map((opt, index) => {
          const active = range === opt.key;
          const activeTextOpacity = position.interpolate({
            inputRange: [index - 1, index, index + 1],
            outputRange: [0, 1, 0],
            extrapolate: 'clamp',
          });
          return (
            <AnimatedPressable
              key={opt.key}
              style={styles.rangeTab}
              scale={1}
              accessibilityRole="tab"
              accessibilityState={{ selected: active }}
              accessibilityLabel={`${opt.label} range`}
              onPress={() => jumpTo(opt.key)}
            >
              <Text style={styles.rangeTabText}>{opt.label}</Text>
              <NativeAnimated.Text
                accessible={false}
                aria-hidden
                style={[styles.rangeTabText, styles.rangeTabTextActive, { opacity: activeTextOpacity }]}
              >
                {opt.label}
              </NativeAnimated.Text>
            </AnimatedPressable>
          );
        })}
      </View>
    </View>
  );
}

interface StatsRangeSceneProps {
  range: TimeRange;
  active: boolean;
  statsData: StatsPayload | null;
  loading: boolean;
  expandedSections: Set<string>;
  toggleSection(section: string): void;
  selectedDay: DayCell | null;
  setSelectedDay(day: DayCell | null): void;
  onNestedHorizontalGestureStart(): void;
  onNestedHorizontalGestureEnd(): void;
}

function StatsRangeScene({
  range,
  active,
  statsData,
  loading,
  expandedSections,
  toggleSection,
  selectedDay,
  setSelectedDay,
  onNestedHorizontalGestureStart,
  onNestedHorizontalGestureEnd,
}: StatsRangeSceneProps) {
  const { colors, theme } = useAppTheme();
  const insets = useSafeAreaInsets();
  const s = useMemo(() => createStyles(colors), [colors]);
  const intensityColors = theme.dataVisualization.activityRamp;

  const data = statsData?.ranges?.[range] ?? EMPTY_RANGE;
  const allData = statsData?.ranges?.all ?? EMPTY_RANGE;
  const days = data.days ?? [];

  const rankedModels = useMemo(() => sortByActivity(data.models ?? []), [data.models]);
  const modelActivityUsesTokens = useMemo(() => activityUsesTokens(rankedModels), [rankedModels]);
  const maxModelActivity = useMemo(
    () => Math.max(...rankedModels.map(model => activityValue(model, modelActivityUsesTokens)), 1),
    [modelActivityUsesTokens, rankedModels],
  );
  const rankedProjects = useMemo(() => sortByActivity(data.projects ?? []), [data.projects]);
  const projectActivityUsesTokens = useMemo(() => activityUsesTokens(rankedProjects), [rankedProjects]);
  const maxProjectActivity = useMemo(
    () => Math.max(...rankedProjects.map(project => activityValue(project, projectActivityUsesTokens)), 1),
    [projectActivityUsesTokens, rankedProjects],
  );
  const maxSkillCalls = useMemo(() => Math.max(...(data.skills?.map(s => s.calls) ?? [0]), 1), [data.skills]);
  const maxToolCalls = useMemo(() => Math.max(...(data.tools?.map(t => t.calls) ?? [0]), 1), [data.tools]);
  const totalSkills = data.skills?.length ?? 0;
  const totalSkillCalls = useMemo(() => (data.skills ?? []).reduce((s, v) => s + v.calls, 0), [data.skills]);
  const totalToolCalls = useMemo(() => (data.tools ?? []).reduce((s, v) => s + v.calls, 0), [data.tools]);
  const visibleModels = useMemo(() => topItems(rankedModels), [rankedModels]);
  const visibleProjects = useMemo(() => topItems(rankedProjects), [rankedProjects]);
  const visibleSkills = useMemo(() => topItems(data.skills ?? []), [data.skills]);
  const visibleTools = useMemo(() => topItems(data.tools ?? []), [data.tools]);
  const dayActivityUsesTokens = useMemo(() => activityUsesTokens(days), [days]);
  const maxDayActivity = useMemo(
    () => Math.max(...days.map(day => activityValue(day, dayActivityUsesTokens)), 1),
    [dayActivityUsesTokens, days],
  );
  const activityDays = useMemo(() => buildDailyActivitySeries(days, range), [days, range]);
  const compactDailyActivity = range === 'day' || range === 'week';
  const activityStartLabel = activityDays[0]?.date ? shortDate(activityDays[0].date) : '';
  const activityEndLabel = activityDays[activityDays.length - 1]?.date
    ? shortDate(activityDays[activityDays.length - 1].date)
    : '';
  const heatmapColumns = useMemo(() => buildActivityCalendarColumns(activityDays), [activityDays]);
  const hasAvailabilityGaps = !isCostKnown(data) ||
    !isTotalTokensKnown(data) ||
    !isTokenBreakdownKnown(data);

  const hasData = hasRangeStats(data);
  const codexSubscriptions = (statsData?.codexSubscriptions ?? [])
    .filter(isOfficialCodexSubscription);
  const showStatsContent = hasData || codexSubscriptions.length > 0;
  const hasAnyStats = useMemo(
    () => Object.values(statsData?.ranges ?? {}).some(item => hasRangeStats(item)),
    [statsData],
  );
  const latestDay = allData.days?.[allData.days.length - 1] ?? null;
  const emptyTitle = !hasAnyStats
    ? 'No stats yet'
    : range === 'day'
      ? 'No activity today'
      : `No ${RANGE_OPTIONS.find(opt => opt.key === range)?.label.toLowerCase()} activity`;
  const emptySubtext = !hasAnyStats
    ? 'Connect to a server with Claude Code or Codex history to start collecting data.'
    : latestDay
      ? `Latest activity: ${shortDate(latestDay.date)}. Stats read Claude Code and Codex history from the daemon host.`
      : 'Stats read Claude Code and Codex history from the daemon host.';

  return (
    <View
      style={s.scene}
      accessibilityElementsHidden={!active}
      importantForAccessibility={active ? 'auto' : 'no-hide-descendants'}
      aria-hidden={!active}
    >
      {loading && !statsData ? (
        <View style={s.emptyContainer}>
          <ActivityIndicator color={colors.textSecondary} />
        </View>
      ) : !showStatsContent ? (
        <View style={s.emptyContainer}>
          <Text style={s.emptyIcon}>{'∷'}</Text>
          <Text style={s.emptyText}>{emptyTitle}</Text>
          <Text style={s.emptySubtext}>{emptySubtext}</Text>
        </View>
      ) : (
        <ScrollView
          style={s.scrollView}
          contentContainerStyle={[
            s.scroll,
            { paddingBottom: Math.max(insets.bottom, 20) + 12 },
          ]}
          showsVerticalScrollIndicator={false}
        >
            {codexSubscriptions.map((subscription, index) => (
              <View key={`${subscription.serverLabel ?? 'server'}:${index}`} style={[s.card, s.codexCard]}>
                <View style={s.codexTitleRow}>
                  <View style={s.codexTitleBlock}>
                    <Text style={s.label}>Codex subscription</Text>
                    <Text style={s.codexPlan}>
                      {subscription.authKind === 'official'
                        ? codexPlanLabel(subscription.plan)
                        : codexAuthSummary(subscription.authKind)}
                    </Text>
                  </View>
                  {subscription.stale && <Text style={s.staleBadge}>STALE</Text>}
                </View>
                {subscription.authKind === 'official' && subscription.state === 'available' ? (
                  <View style={s.codexWindows}>
                    {(subscription.windows ?? []).map(window => {
                      const used = normalizeCodexUsedPercent(window.usedPercent);
                      const remaining = codexRemainingPercent(window.usedPercent);
                      return (
                        <View key={window.name} style={s.codexWindow} accessible accessibilityLabel={`${codexWindowLabel(window.windowMinutes)}, ${remaining.toFixed(0)} percent remaining, ${fmtReset(window.resetsAt)}`}>
                          <View style={s.codexWindowHeader}>
                            <Text style={s.codexWindowLabel}>{codexWindowLabel(window.windowMinutes)}</Text>
                            <Text style={s.codexRemaining}>{remaining.toFixed(0)}% left</Text>
                          </View>
                          <View style={s.codexTrack}>
                            <View style={[s.codexFill, { width: `${used}%` }]} />
                          </View>
                          <Text style={s.codexReset}>{fmtReset(window.resetsAt)}</Text>
                        </View>
                      );
                    })}
                    {subscription.stale && (
                      <Text style={s.codexNotice}>Live usage is temporarily unavailable. Showing the last successful update.</Text>
                    )}
                  </View>
                ) : (
                  <Text style={s.codexNotice}>
                    {subscription.authKind === 'api_key'
                      ? 'Subscription limits are only available when Codex is signed in with ChatGPT.'
                      : subscription.authKind === 'official'
                        ? 'Codex usage is temporarily unavailable. Existing activity stats are unaffected.'
                        : subscription.authKind === 'absent'
                          ? 'Sign in to the Codex CLI with ChatGPT to see subscription limits.'
                          : 'Zen could not confidently identify official Codex subscription authentication.'}
                  </Text>
                )}
              </View>
            ))}

            {/* ── Summary ── */}
            {hasData && <View style={[s.card, s.summaryCard]}>
              <View style={s.summaryMetrics}>
                <View style={s.summaryMetric}>
                  <Text style={s.summaryLabel}>Estimated cost</Text>
                  <Text
                    style={[
                      s.summaryValue,
                      s.summaryCost,
                      !isCostKnown(data) && data.cost === 0 && s.summaryUnavailable,
                    ]}
                  >
                    {fmtAvailableCost(data.cost, data.costKnown)}
                  </Text>
                </View>
                <View style={s.summaryDivider} />
                <View style={s.summaryMetric}>
                  <Text style={s.summaryLabel}>Tokens</Text>
                  <Text
                    style={[
                      s.summaryValue,
                      !isTotalTokensKnown(data) && data.totalTokens === 0 && s.summaryUnavailable,
                    ]}
                  >
                    {fmtAvailableTokens(data.totalTokens, data.totalTokensKnown)}
                  </Text>
                </View>
              </View>
              <Text style={s.summarySessions}>{sessionSummary(data.sessions)}</Text>
              {hasAvailabilityGaps && (
                <Text style={s.summaryNote}>
                  Some agents do not report token or billing details.
                </Text>
              )}
            </View>}

            {/* ── Models ── */}
            {(data.models?.length ?? 0) > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Models</Text>
                {(expandedSections.has('models') ? rankedModels : visibleModels).map((m) => (
                  <View key={m.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{m.name}</Text>
                      <Text style={s.rowMeta} numberOfLines={1}>{rowActivitySummary(m)}</Text>
                    </View>
                    <Text
                      style={[
                        s.rowCost,
                        !isCostKnown(m) && m.cost === 0 && s.rowValueUnavailable,
                      ]}
                    >
                      {fmtAvailableCost(m.cost, m.costKnown)}
                    </Text>
                    <Bar
                      ratio={activityValue(m, modelActivityUsesTokens) / maxModelActivity}
                      color={colors.accent}
                      trackColor={colors.borderSubtle}
                    />
                  </View>
                ))}
                {data.models.length > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('models')} total={data.models.length} onPress={() => toggleSection('models')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Projects ── */}
            {(data.projects?.length ?? 0) > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Projects</Text>
                {(expandedSections.has('projects') ? rankedProjects : visibleProjects).map((p) => (
                  <View key={p.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{p.name}</Text>
                      <Text style={s.rowMeta} numberOfLines={1}>{rowActivitySummary(p)}</Text>
                    </View>
                    <Text
                      style={[
                        s.rowCost,
                        !isCostKnown(p) && p.cost === 0 && s.rowValueUnavailable,
                      ]}
                    >
                      {fmtAvailableCost(p.cost, p.costKnown)}
                    </Text>
                    <Bar
                      ratio={activityValue(p, projectActivityUsesTokens) / maxProjectActivity}
                      color={colors.accent}
                      trackColor={colors.borderSubtle}
                    />
                  </View>
                ))}
                {data.projects.length > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('projects')} total={data.projects.length} onPress={() => toggleSection('projects')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Activity heatmap ── */}
            {days.length > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Activity</Text>
                {compactDailyActivity ? (
                  <>
                    <View style={s.activityMonthRow}>
                      <Text style={s.activityMonthText}>{monthShort(activityDays[0]?.date ?? '')}</Text>
                      {activityDays.length > 10 && monthShort(activityDays[0]?.date ?? '') !== monthShort(activityDays[activityDays.length - 1]?.date ?? '') ? (
                        <Text style={s.activityMonthText}>{monthShort(activityDays[activityDays.length - 1]?.date ?? '')}</Text>
                      ) : null}
                    </View>
                    <ScrollView
                      horizontal
                      showsHorizontalScrollIndicator={false}
                      onTouchStart={onNestedHorizontalGestureStart}
                      onTouchEnd={onNestedHorizontalGestureEnd}
                      onTouchCancel={onNestedHorizontalGestureEnd}
                    >
                      <View style={[s.dailyActivityContent, range === 'week' && s.dailyWeekContent]}>
                        <View style={s.dailyHeatmapRow}>
                          {activityDays.map(({ date, day }) => {
                            const intensity = day
                              ? dayActivityIntensity(day, maxDayActivity, dayActivityUsesTokens)
                              : 0;
                            const backgroundColor = day ? intensityColors[intensity] : colors.borderSubtle;
                            const selected = selectedDay?.date === date;
                            return (
                              <AnimatedPressable
                                key={date}
                                disabled={!day}
                                style={[
                                  s.dailyHeatCell,
                                  {
                                    backgroundColor,
                                    opacity: day ? 1 : 0.38,
                                  },
                                  selected && s.heatCellSelected,
                                ]}
                                scale={day ? 0.96 : 1}
                                accessibilityRole="button"
                                accessibilityLabel={accessibleDate(date)}
                                accessibilityValue={{
                                  text: day ? dayAccessibilityValue(day) : 'No recorded activity',
                                }}
                                accessibilityState={{ selected, disabled: !day }}
                                accessibilityHint={day ? 'Shows daily activity details' : undefined}
                                onPress={() => {
                                  if (!day) return;
                                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                                  setSelectedDay(day);
                                }}
                              />
                            );
                          })}
                        </View>
                        {range === 'week' ? (
                          <View style={s.activityWeekdayRow}>
                            {activityDays.map(({ date }) => (
                              <Text key={date} style={s.activityWeekdayText}>
                                {weekdayShort(date).slice(0, 1)}
                              </Text>
                            ))}
                          </View>
                        ) : (
                          <View style={s.activityAxisRow}>
                            <Text style={s.activityAxisLabel}>{activityStartLabel}</Text>
                            <Text style={s.activityAxisLabel}>{activityEndLabel}</Text>
                          </View>
                        )}
                      </View>
                    </ScrollView>
                  </>
                ) : (
                  <ScrollView
                    horizontal
                    showsHorizontalScrollIndicator={false}
                    accessibilityLabel="Daily activity calendar"
                    onTouchStart={onNestedHorizontalGestureStart}
                    onTouchEnd={onNestedHorizontalGestureEnd}
                    onTouchCancel={onNestedHorizontalGestureEnd}
                  >
                    <View>
                      <View style={s.heatmapMonthRow}>
                        {heatmapColumns.map((col, colIndex) => {
                          const firstCell = col.find(Boolean);
                          const previousFirstCell = colIndex > 0 ? heatmapColumns[colIndex - 1].find(Boolean) : null;
                          const label = firstCell && (
                            colIndex === 0 ||
                            monthShort(firstCell.date) !== monthShort(previousFirstCell?.date ?? '')
                          )
                            ? monthShort(firstCell.date)
                            : '';
                          return (
                            <Text key={`month-${colIndex}`} style={s.heatmapMonthLabel}>
                              {label}
                            </Text>
                          );
                        })}
                      </View>
                      <View style={s.heatmapWithAxis}>
                        <View style={s.heatmapWeekdayColumn}>
                          {['M', 'T', 'W', 'T', 'F', 'S', 'S'].map((label, index) => (
                            <Text key={`${label}-${index}`} style={s.heatmapWeekdayLabel}>
                              {label}
                            </Text>
                          ))}
                        </View>
                        <View style={s.heatmapRow}>
                          {heatmapColumns.map((col, colIndex) => (
                            <View key={`col-${colIndex}`} style={s.heatmapColumn}>
                              {col.map((cell, rowIndex) => {
                                if (!cell) {
                                  return (
                                    <View
                                      key={`empty-${colIndex}-${rowIndex}`}
                                      style={s.heatCellTarget}
                                    />
                                  );
                                }
                                if (!cell.day) {
                                  return (
                                    <AnimatedPressable
                                      key={cell.date}
                                      disabled
                                      style={s.heatCellTarget}
                                      scale={1}
                                      accessibilityRole="button"
                                      accessibilityLabel={accessibleDate(cell.date)}
                                      accessibilityValue={{ text: 'No recorded activity' }}
                                      accessibilityState={{ disabled: true, selected: false }}
                                    >
                                      <View style={[s.heatCell, s.heatCellNoActivity]} />
                                    </AnimatedPressable>
                                  );
                                }
                                const day = cell.day;
                                const intensity = dayActivityIntensity(
                                  day,
                                  maxDayActivity,
                                  dayActivityUsesTokens,
                                );
                                const selected = selectedDay?.date === day.date;
                                return (
                                  <AnimatedPressable
                                    key={day.date}
                                    style={s.heatCellTarget}
                                    scale={0.96}
                                    accessibilityRole="button"
                                    accessibilityLabel={accessibleDate(day.date)}
                                    accessibilityValue={{ text: dayAccessibilityValue(day) }}
                                    accessibilityState={{ selected }}
                                    accessibilityHint="Shows daily activity details"
                                    onPress={() => {
                                      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                                      setSelectedDay(day);
                                    }}
                                  >
                                    <View
                                      style={[
                                        s.heatCell,
                                        { backgroundColor: intensityColors[intensity] },
                                        selected && s.heatCellSelected,
                                      ]}
                                    />
                                  </AnimatedPressable>
                                );
                              })}
                            </View>
                          ))}
                        </View>
                      </View>
                    </View>
                  </ScrollView>
                )}
                <View style={s.heatmapLegend}>
                  <Text style={s.heatmapLegendLabel}>Less</Text>
                  {intensityColors.map((c, i) => (
                    <View key={i} style={[s.heatCellLegend, { backgroundColor: c }]} />
                  ))}
                  <Text style={s.heatmapLegendLabel}>More</Text>
                </View>
              </View>
            )}

            {/* ── Skills ── */}
            {totalSkills > 0 && (
              <View style={s.card}>
                <View style={s.labelRow}>
                  <Text style={s.label}>Skills</Text>
                  <Text style={s.labelCount}>{totalSkillCalls} calls</Text>
                </View>
                {(expandedSections.has('skills') ? data.skills : visibleSkills).map((sk) => (
                  <View key={sk.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.skillCmd} numberOfLines={1}>{sk.name}</Text>
                      <Text style={s.rowMeta} numberOfLines={1}>{sk.projects?.join(' · ')}</Text>
                    </View>
                    <Text style={s.rowCount}>{sk.calls}</Text>
                    <Bar ratio={sk.calls / maxSkillCalls} color={colors.statusUnknown} trackColor={colors.borderSubtle} />
                  </View>
                ))}
                {totalSkills > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('skills')} total={totalSkills} onPress={() => toggleSection('skills')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Tools ── */}
            {totalToolCalls > 0 && (
              <View style={s.card}>
                <View style={s.labelRow}>
                  <Text style={s.label}>Tools</Text>
                  <Text style={s.labelCount}>{totalToolCalls} calls</Text>
                </View>
                {(expandedSections.has('tools') ? data.tools : visibleTools).map((t) => (
                  <View key={t.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{t.name}</Text>
                    </View>
                    <Text style={s.rowCount}>{t.calls}</Text>
                    <Bar ratio={t.calls / maxToolCalls} color={colors.statusRunning} trackColor={colors.borderSubtle} />
                  </View>
                ))}
                {(data.tools?.length ?? 0) > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('tools')} total={data.tools.length} onPress={() => toggleSection('tools')} colors={colors} />
                )}
              </View>
            )}

            <View style={{ height: 28 }} />
        </ScrollView>
      )}

    </View>
  );
}

// ── Small components ───────────────────────────────────────

function DayDetailSheet({
  selectedDay,
  onClose,
}: {
  selectedDay: DayCell | null;
  onClose(): void;
}) {
  const { colors } = useAppTheme();
  const s = useMemo(() => createStyles(colors), [colors]);

  return (
    <RisingSheet
      visible={selectedDay !== null}
      onClose={onClose}
      cardStyle={s.detailCard}
    >
      {selectedDay && (
        <>
          <Text
            style={s.detailTitle}
            accessibilityRole="header"
            accessibilityLabel={accessibleDate(selectedDay.date)}
          >
            {selectedDay.date}
          </Text>
          <View style={s.detailGrid}>
            <DItem
              label="Cost"
              value={fmtAvailableCost(selectedDay.cost, selectedDay.costKnown)}
              accent={isCostKnown(selectedDay) || selectedDay.cost > 0}
              colors={colors}
              styles={s}
            />
            <DItem label="Sessions" value={`${selectedDay.sessions}`} colors={colors} styles={s} />
            <DItem
              label="Total"
              value={fmtAvailableTokens(selectedDay.totalTokens, selectedDay.totalTokensKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Input"
              value={fmtAvailableTokens(selectedDay.inputTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Cache"
              value={fmtAvailableTokens(selectedDay.cacheRead, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Output"
              value={fmtAvailableTokens(selectedDay.outputTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Reason"
              value={fmtAvailableTokens(selectedDay.reasoningTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
          </View>
        </>
      )}
    </RisingSheet>
  );
}

function DItem({
  label,
  value,
  accent,
  colors,
  styles,
}: {
  label: string;
  value: string;
  accent?: boolean;
  colors: typeof Colors;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View
      style={styles.dItem}
      accessible
      accessibilityLabel={label}
      accessibilityValue={{ text: value }}
    >
      <Text style={styles.dLabel}>{label}</Text>
      <Text style={[styles.dValue, accent && { color: colors.accent }]}>{value}</Text>
    </View>
  );
}

function Bar({
  ratio,
  color,
  trackColor,
}: {
  ratio: number;
  color: string;
  trackColor: string;
}) {
  const width = useSharedValue(0);
  React.useEffect(() => {
    width.value = withTiming(Math.min(ratio, 1), { duration: 320 });
  }, [ratio, width]);
  const style = useAnimatedStyle(() => ({
    width: `${interpolate(width.value, [0, 1], [0, 100])}%`,
  }));
  return (
    <View style={{ width: 44, height: 2.5, borderRadius: 1.5, backgroundColor: trackColor }}>
      <Animated.View
        style={[{ height: 2.5, borderRadius: 1.5, backgroundColor: color, opacity: 0.7 }, style]}
      />
    </View>
  );
}

function ExpandToggle({
  expanded,
  total,
  onPress,
  colors,
}: {
  expanded: boolean;
  total: number;
  onPress: () => void;
  colors: typeof Colors;
}) {
  return (
    <AnimatedPressable
      style={{
        minHeight: 44,
        alignItems: 'center',
        justifyContent: 'center',
        marginTop: 4,
      }}
      scale={0.97}
      accessibilityRole="button"
      accessibilityState={{ expanded }}
      accessibilityLabel={expanded ? 'Show fewer items' : `Show ${total - MAX_LIST_ITEMS} more items`}
      onPress={onPress}
    >
      <Text style={[TypeScale.label, UiTextMetrics, { color: colors.accent }]}>
        {expanded ? 'less' : `${total - MAX_LIST_ITEMS} more`}
      </Text>
    </AnimatedPressable>
  );
}

// ── Styles ─────────────────────────────────────────────────

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: colors.bgPrimary },
    pager: { flex: 1 },
    scene: { flex: 1, backgroundColor: colors.bgPrimary },
    header: {
      width: '100%',
      maxWidth: STATS_CONTENT_MAX_WIDTH,
      alignSelf: 'center',
      paddingHorizontal: 18,
      paddingTop: 12,
      paddingBottom: 10,
      backgroundColor: colors.bgPrimary,
      zIndex: 2,
    },
    rangeRow: {
      flexDirection: 'row',
      gap: STATS_TAB_BAR_GAP,
      padding: STATS_TAB_BAR_PADDING,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    rangeTabIndicator: {
      position: 'absolute',
      left: STATS_TAB_BAR_PADDING,
      top: STATS_TAB_BAR_PADDING,
      bottom: STATS_TAB_BAR_PADDING,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceActive,
    },
    rangeTab: {
      flex: 1,
      minWidth: 0,
      minHeight: 44,
      alignItems: 'center',
      justifyContent: 'center',
      paddingHorizontal: 4,
      borderRadius: Radii.sm,
      zIndex: 1,
    },
    rangeTabText: {
      ...TypeScale.label,
      ...UiTextMetrics,
      color: colors.textTertiary,
      textAlign: 'center',
    },
    rangeTabTextActive: { position: 'absolute', color: colors.textPrimary },
    scrollView: { flex: 1 },
    scroll: {
      width: '100%',
      maxWidth: STATS_CONTENT_MAX_WIDTH,
      alignSelf: 'center',
      paddingHorizontal: 18,
      gap: 12,
      paddingTop: 6,
    },

    // Empty
    emptyContainer: {
      flex: 1,
      justifyContent: 'center',
      alignItems: 'center',
      paddingHorizontal: 36,
    },
    emptyIcon: {
      fontSize: 40,
      color: colors.accent,
      lineHeight: 46,
      marginBottom: 16,
    },
    emptyText: {
      ...TypeScale.heading,
      ...UiTextMetrics,
      color: colors.textPrimary,
      textAlign: 'center',
    },
    emptySubtext: {
      ...TypeScale.compact,
      ...UiTextMetrics,
      color: colors.textSecondary,
      marginTop: 8,
      maxWidth: 320,
      textAlign: 'center',
    },

    // Sections
    card: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
      paddingVertical: 18,
    },
    label: {
      ...TypeScale.heading,
      ...UiTextMetrics,
      color: colors.textPrimary,
      marginBottom: 12,
    },
    labelRow: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
      gap: 12,
      marginBottom: 12,
    },
    labelCount: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
    },

    // Summary
    summaryCard: {
      paddingHorizontal: 16,
      paddingVertical: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
    },
    summaryMetrics: {
      flexDirection: 'row',
      alignItems: 'stretch',
    },
    summaryMetric: { flex: 1, minWidth: 0 },
    summaryDivider: {
      width: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
      marginHorizontal: 12,
    },
    summaryLabel: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginBottom: 6,
    },
    summaryValue: {
      ...TypeScale.title,
      ...UiTextMetrics,
      color: colors.textPrimary,
      flexShrink: 1,
    },
    summaryCost: { color: colors.accent },
    summaryUnavailable: { color: colors.textTertiary },
    summarySessions: {
      ...TypeScale.label,
      ...UiTextMetrics,
      color: colors.textSecondary,
      marginTop: 12,
    },
    summaryNote: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginTop: 4,
    },

    codexCard: {
      paddingHorizontal: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
    },
    codexTitleRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 12 },
    codexTitleBlock: { flex: 1, minWidth: 0 },
    codexPlan: { ...TypeScale.caption, ...UiTextMetrics, color: colors.textSecondary, marginTop: -8 },
    staleBadge: { ...TypeScale.micro, ...UiTextMetrics, color: colors.textTertiary, marginTop: 3 },
    codexWindows: { gap: 16, marginTop: 18 },
    codexWindow: { gap: 7 },
    codexWindowHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 },
    codexWindowLabel: { ...TypeScale.label, ...UiTextMetrics, color: colors.textPrimary },
    codexRemaining: { ...TypeScale.monoStrong, ...UiTextMetrics, color: colors.accent },
    codexTrack: { height: 7, borderRadius: 4, backgroundColor: colors.borderSubtle, overflow: 'hidden' },
    codexFill: { height: '100%', borderRadius: 4, backgroundColor: colors.accent, opacity: 0.78 },
    codexReset: { ...TypeScale.caption, ...UiTextMetrics, color: colors.textTertiary },
    codexNotice: { ...TypeScale.compact, ...UiTextMetrics, color: colors.textSecondary, marginTop: 14 },

    // Activity heatmap
    activityMonthRow: {
      minHeight: 20,
      marginBottom: 6,
      flexDirection: 'row',
      justifyContent: 'space-between',
    },
    activityMonthText: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      color: colors.textTertiary,
    },
    dailyActivityContent: { minWidth: '100%' },
    dailyWeekContent: { minWidth: 338 },
    dailyHeatmapRow: {
      flexDirection: 'row',
      alignItems: 'stretch',
      gap: 5,
    },
    dailyHeatCell: {
      flex: 1,
      minWidth: 44,
      height: 44,
      borderRadius: Radii.xs,
    },
    activityAxisRow: {
      minHeight: 20,
      marginTop: 6,
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
    },
    activityAxisLabel: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      color: colors.textTertiary,
    },
    activityWeekdayRow: {
      flexDirection: 'row',
      gap: 5,
      marginTop: 6,
    },
    activityWeekdayText: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      flex: 1,
      minWidth: 44,
      color: colors.textTertiary,
      textAlign: 'center',
    },
    heatmapWithAxis: {
      flexDirection: 'row',
      alignItems: 'flex-start',
    },
    heatmapWeekdayColumn: { width: 32 },
    heatmapWeekdayLabel: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      height: 44,
      color: colors.textTertiary,
      textAlignVertical: 'center',
    },
    heatmapMonthRow: {
      flexDirection: 'row',
      paddingLeft: 32,
      marginBottom: 2,
    },
    heatmapMonthLabel: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      width: 44,
      color: colors.textTertiary,
    },
    heatmapRow: { flexDirection: 'row' },
    heatmapColumn: {},
    heatCellTarget: {
      width: 44,
      height: 44,
      alignItems: 'center',
      justifyContent: 'center',
    },
    heatCell: {
      width: 16,
      height: 16,
      borderRadius: 3,
    },
    heatCellNoActivity: {
      backgroundColor: colors.borderSubtle,
      opacity: 0.55,
    },
    heatCellSelected: {
      borderWidth: 2,
      borderColor: colors.focusRing,
    },
    heatCellLegend: {
      width: 12,
      height: 12,
      borderRadius: 3,
    },
    heatmapLegend: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'flex-end',
      gap: 5,
      marginTop: 8,
    },
    heatmapLegendLabel: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginHorizontal: 2,
    },

    // Rank rows
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingVertical: 10,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    rowInfo: { flex: 1, minWidth: 0 },
    rowName: {
      ...TypeScale.mono,
      ...UiTextMetrics,
      color: colors.textPrimary,
    },
    rowMeta: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginTop: 2,
    },
    rowCost: {
      ...TypeScale.monoStrong,
      ...UiTextMetrics,
      color: colors.accent,
      minWidth: 44,
      textAlign: 'right',
    },
    rowValueUnavailable: { color: colors.textTertiary },
    rowCount: {
      ...TypeScale.monoStrong,
      ...UiTextMetrics,
      color: colors.textSecondary,
      minWidth: 34,
      textAlign: 'right',
    },
    skillCmd: {
      ...TypeScale.monoStrong,
      ...UiTextMetrics,
      color: colors.textSecondary,
    },

    // Modal / day detail
    detailCard: {
      width: '100%',
      maxWidth: 320,
      alignSelf: 'center',
      borderRadius: Radii.lg,
      padding: 20,
      backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    detailTitle: {
      ...TypeScale.heading,
      ...UiTextMetrics,
      color: colors.textPrimary,
      marginBottom: 16,
      textAlign: 'center',
    },
    detailGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
    dItem: {
      width: '46%',
      minHeight: 64,
      alignItems: 'center',
      justifyContent: 'center',
    },
    dLabel: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginBottom: 4,
      textAlign: 'center',
    },
    dValue: {
      ...TypeScale.monoStrong,
      ...UiTextMetrics,
      color: colors.textPrimary,
      textAlign: 'center',
    },
  });
}
