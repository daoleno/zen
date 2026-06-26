import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { useFocusEffect } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import * as Haptics from 'expo-haptics';
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withTiming,
  interpolate,
} from 'react-native-reanimated';
import { Colors, Radii, Typography, useAppColors, shadow } from '../../constants/tokens';
import { useAgents } from '../../store/agents';
import { wsClient } from '../../services/websocket';
import { AnimatedPressable } from '../../components/ui/AnimatedPressable';
import { RisingSheet } from '../../components/ui/RisingSheet';

// ── Types (mirror daemon/stats/types.go) ───────────────────

type TimeRange = 'day' | 'week' | 'month' | 'all';

interface DayCell {
  date: string;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  cost: number;
  sessions: number;
}

interface ModelStat {
  name: string;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  cost: number;
  sessions: number;
}

interface ProjectStat {
  name: string;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  cost: number;
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
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cacheRead: number;
  cacheCreate: number;
  sessions: number;
  models: ModelStat[];
  projects: ProjectStat[];
  skills: SkillStat[];
  tools: ToolStat[];
  days: DayCell[];
}

interface StatsPayload {
  ranges: Record<string, RangeData>;
  serverId?: string;
  serverUrl?: string;
  daemonId?: string;
  daemonPublicKey?: string;
}

// ── Constants ──────────────────────────────────────────────

const EMPTY_RANGE: RangeData = {
  cost: 0, totalTokens: 0, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0,
  sessions: 0, models: [], projects: [], skills: [], tools: [], days: [],
};

const RANGE_OPTIONS: { key: TimeRange; label: string }[] = [
  { key: 'day', label: 'Day' },
  { key: 'week', label: 'Week' },
  { key: 'month', label: 'Month' },
  { key: 'all', label: 'All' },
];

const MAX_LIST_ITEMS = 5;
const EMPTY_STATS_RETRY_MS = 700;
const EMPTY_STATS_MAX_RETRIES = 3;

// ── Helpers ────────────────────────────────────────────────

function barIntensity(cost: number, maxCost: number): number {
  if (cost <= 0) return 0;
  const ratio = cost / maxCost;
  if (ratio < 0.15) return 1;
  if (ratio < 0.5) return 2;
  return 3;
}

function fmt(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(0) + 'K';
  return n.toString();
}

function fmtCost(n: number): string {
  if (n >= 100) return '$' + n.toFixed(0);
  return '$' + n.toFixed(2);
}

function tokenSummary(item: {
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  reasoningTokens: number;
}): string {
  const parts = [
    `${fmt(item.totalTokens)} total`,
    `${fmt(item.inputTokens)} in`,
  ];
  if (item.cacheRead > 0) parts.push(`${fmt(item.cacheRead)} cache`);
  parts.push(`${fmt(item.outputTokens)} out`);
  if (item.reasoningTokens > 0) parts.push(`${fmt(item.reasoningTokens)} reason`);
  return parts.join(' · ');
}

function shortDate(dateStr: string): string {
  // "2026-04-04" -> "4/4"
  const parts = dateStr.split('-');
  if (parts.length < 3) return dateStr;
  return `${parseInt(parts[1])}/${parseInt(parts[2])}`;
}

function topItems<T>(items: T[]): T[] {
  return items.slice(0, MAX_LIST_ITEMS);
}

function mergeModelStats(items: ModelStat[]): ModelStat[] {
  const merged = new Map<string, ModelStat>();
  for (const item of items) {
    const current = merged.get(item.name) ?? {
      name: item.name, totalTokens: 0, inputTokens: 0, outputTokens: 0,
      reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, sessions: 0,
    };
    current.totalTokens += item.totalTokens;
    current.inputTokens += item.inputTokens;
    current.outputTokens += item.outputTokens;
    current.reasoningTokens += item.reasoningTokens;
    current.cacheRead += item.cacheRead;
    current.cacheCreate += item.cacheCreate;
    current.cost += item.cost;
    current.sessions += item.sessions;
    merged.set(item.name, current);
  }
  return [...merged.values()].sort((a, b) => b.cost - a.cost || b.sessions - a.sessions);
}

function mergeProjectStats(items: ProjectStat[]): ProjectStat[] {
  const merged = new Map<string, ProjectStat>();
  for (const item of items) {
    const current = merged.get(item.name) ?? {
      name: item.name, totalTokens: 0, inputTokens: 0, outputTokens: 0,
      reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, sessions: 0,
    };
    current.totalTokens += item.totalTokens;
    current.inputTokens += item.inputTokens;
    current.outputTokens += item.outputTokens;
    current.reasoningTokens += item.reasoningTokens;
    current.cacheRead += item.cacheRead;
    current.cacheCreate += item.cacheCreate;
    current.cost += item.cost;
    current.sessions += item.sessions;
    merged.set(item.name, current);
  }
  return [...merged.values()].sort((a, b) => b.cost - a.cost || b.sessions - a.sessions);
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
        reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, sessions: 0,
      };
      c.totalTokens += d.totalTokens;
      c.inputTokens += d.inputTokens;
      c.outputTokens += d.outputTokens;
      c.reasoningTokens += d.reasoningTokens;
      c.cacheRead += d.cacheRead;
      c.cacheCreate += d.cacheCreate;
      c.cost += d.cost;
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
    totalTokens: items.reduce((s, i) => s + i.totalTokens, 0),
    inputTokens: items.reduce((s, i) => s + i.inputTokens, 0),
    outputTokens: items.reduce((s, i) => s + i.outputTokens, 0),
    reasoningTokens: items.reduce((s, i) => s + i.reasoningTokens, 0),
    cacheRead: items.reduce((s, i) => s + i.cacheRead, 0),
    cacheCreate: items.reduce((s, i) => s + i.cacheCreate, 0),
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
  return { ranges };
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

/** GitHub-style columns: each column is Mon→Sun (7 rows). */
function buildHeatmapColumns(days: DayCell[]): (DayCell | null)[][] {
  if (days.length === 0) return [];
  const sorted = [...days].sort((a, b) => a.date.localeCompare(b.date));
  const columns: (DayCell | null)[][] = [];
  let col: (DayCell | null)[] = Array(7).fill(null);

  for (const day of sorted) {
    const row = dayOfWeekMon0(day.date);
    if (row === 0 && col.some((c) => c !== null)) {
      columns.push(col);
      col = Array(7).fill(null);
    }
    col[row] = day;
  }
  if (col.some((c) => c !== null)) columns.push(col);
  return columns;
}

// ── Component ──────────────────────────────────────────────

export default function StatsScreen() {
  const colors = useAppColors();
  const s = useMemo(() => createStyles(colors), [colors]);
  const intensityColors = useMemo(
    () => [
      `${colors.accent}0D`,
      `${colors.accent}33`,
      `${colors.accent}73`,
      `${colors.accent}CC`,
    ],
    [colors],
  );
  const { state: agentsState } = useAgents();
  const [range, setRange] = useState<TimeRange>('week');
  const [statsData, setStatsData] = useState<StatsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set());
  const [selectedDay, setSelectedDay] = useState<DayCell | null>(null);
  const statsDataRef = useRef<StatsPayload | null>(null);

  useEffect(() => {
    statsDataRef.current = statsData;
  }, [statsData]);

  const toggleSection = (section: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  };

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

  const data = statsData?.ranges?.[range] ?? EMPTY_RANGE;
  const allData = statsData?.ranges?.all ?? EMPTY_RANGE;
  const days = data.days ?? [];

  const maxModelCost = useMemo(() => Math.max(...(data.models?.map(m => m.cost) ?? [0]), 0.01), [data.models]);
  const maxProjectCost = useMemo(() => Math.max(...(data.projects?.map(p => p.cost) ?? [0]), 0.01), [data.projects]);
  const maxProjectTokens = useMemo(() => Math.max(...(data.projects?.map(p => p.totalTokens) ?? [0]), 1), [data.projects]);
  const maxSkillCalls = useMemo(() => Math.max(...(data.skills?.map(s => s.calls) ?? [0]), 1), [data.skills]);
  const maxToolCalls = useMemo(() => Math.max(...(data.tools?.map(t => t.calls) ?? [0]), 1), [data.tools]);
  const totalSkills = data.skills?.length ?? 0;
  const totalSkillCalls = useMemo(() => (data.skills ?? []).reduce((s, v) => s + v.calls, 0), [data.skills]);
  const totalToolCalls = useMemo(() => (data.tools ?? []).reduce((s, v) => s + v.calls, 0), [data.tools]);
  const visibleModels = useMemo(() => topItems(data.models ?? []), [data.models]);
  const visibleProjects = useMemo(() => topItems(data.projects ?? []), [data.projects]);
  const visibleSkills = useMemo(() => topItems(data.skills ?? []), [data.skills]);
  const visibleTools = useMemo(() => topItems(data.tools ?? []), [data.tools]);
  const maxDayCost = useMemo(() => Math.max(...days.map(d => d.cost), 0.01), [days]);

  const hasData = hasRangeStats(data);
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
    <SafeAreaView style={s.container} edges={['top']}>
      <View style={s.header}>
        <Text style={s.title}>Stats</Text>
        <View style={s.rangeRow}>
          {RANGE_OPTIONS.map((opt) => {
            const active = range === opt.key;
            return (
              <AnimatedPressable
                key={opt.key}
                style={s.rangeTab}
                scale={1}
                onPress={() => {
                  if (!active) {
                    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                    setRange(opt.key);
                  }
                }}
              >
                <Text style={[s.rangeTabText, active && s.rangeTabTextActive]}>
                  {opt.label}
                </Text>
                {active ? <View style={s.rangeTabIndicator} /> : null}
              </AnimatedPressable>
            );
          })}
        </View>
      </View>

      {loading && !statsData ? (
        <View style={s.emptyContainer}>
          <ActivityIndicator color={colors.textSecondary} />
        </View>
      ) : !hasData ? (
        <View style={s.emptyContainer}>
          <Text style={s.emptyIcon}>{'∷'}</Text>
          <Text style={s.emptyText}>{emptyTitle}</Text>
          <Text style={s.emptySubtext}>{emptySubtext}</Text>
        </View>
      ) : (
        <ScrollView
          style={s.scrollView}
          contentContainerStyle={s.scroll}
          showsVerticalScrollIndicator={false}
        >
            {/* ── Cost ── */}
            <View style={s.card}>
              <Text style={s.costBig}>{fmtCost(data.cost)}</Text>
              <Text style={s.costMeta}>
                {fmt(data.totalTokens)} tokens · {data.sessions} sessions
              </Text>
            </View>

            {/* ── Activity heatmap ── */}
            {days.length > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Activity</Text>
                <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                  <View style={s.heatmapRow}>
                    {buildHeatmapColumns(days).map((col, colIndex) => (
                      <View key={`col-${colIndex}`} style={s.heatmapColumn}>
                        {col.map((day, rowIndex) => {
                          if (!day) {
                            return (
                              <View
                                key={`empty-${colIndex}-${rowIndex}`}
                                style={s.heatCellEmpty}
                              />
                            );
                          }
                          const intensity = barIntensity(day.cost, maxDayCost);
                          return (
                            <AnimatedPressable
                              key={day.date}
                              style={[
                                s.heatCell,
                                { backgroundColor: intensityColors[intensity] },
                              ]}
                              scale={0.92}
                              onPress={() => {
                                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                                setSelectedDay(day);
                              }}
                            />
                          );
                        })}
                      </View>
                    ))}
                  </View>
                </ScrollView>
                <View style={s.heatmapLegend}>
                  <Text style={s.heatmapLegendLabel}>Less</Text>
                  {intensityColors.slice(1).map((c, i) => (
                    <View key={i} style={[s.heatCellLegend, { backgroundColor: c }]} />
                  ))}
                  <Text style={s.heatmapLegendLabel}>More</Text>
                </View>
              </View>
            )}

            {/* ── Models ── */}
            {(data.models?.length ?? 0) > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Models</Text>
                {(expandedSections.has('models') ? data.models : visibleModels).map((m) => (
                  <View key={m.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{m.name}</Text>
                      <Text style={s.rowMeta}>{fmt(m.totalTokens)} tokens · {m.sessions} sessions</Text>
                    </View>
                    <Text style={s.rowCost}>{fmtCost(m.cost)}</Text>
                    <Bar ratio={m.cost / maxModelCost} color={colors.accent} trackColor={colors.borderSubtle} />
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
                {(expandedSections.has('projects') ? data.projects : visibleProjects).map((p) => (
                  <View key={p.name} style={s.row}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{p.name}</Text>
                      <Text style={s.rowMeta}>{p.sessions} sessions</Text>
                    </View>
                    <Text style={s.rowCost}>{p.cost > 0 ? fmtCost(p.cost) : fmt(p.totalTokens)}</Text>
                    <Bar ratio={p.cost > 0 ? p.cost / maxProjectCost : p.totalTokens / maxProjectTokens} color={colors.accent} trackColor={colors.borderSubtle} />
                  </View>
                ))}
                {data.projects.length > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('projects')} total={data.projects.length} onPress={() => toggleSection('projects')} colors={colors} />
                )}
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
                      <Text style={s.skillCmd}>{sk.name}</Text>
                      <Text style={s.rowMeta}>{sk.projects?.join(' · ')}</Text>
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
                      <Text style={s.rowName}>{t.name}</Text>
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

      {/* ── Day detail modal ── */}
      <RisingSheet
        visible={selectedDay !== null}
        onClose={() => setSelectedDay(null)}
        cardStyle={s.detailCard}
      >
        {selectedDay && (
          <>
            <Text style={s.detailTitle}>{selectedDay.date}</Text>
            <View style={s.detailGrid}>
              <DItem label="Cost" value={fmtCost(selectedDay.cost)} accent colors={colors} styles={s} />
              <DItem label="Sessions" value={`${selectedDay.sessions}`} colors={colors} styles={s} />
              <DItem label="Total" value={fmt(selectedDay.totalTokens)} colors={colors} styles={s} />
              <DItem label="Input" value={fmt(selectedDay.inputTokens)} colors={colors} styles={s} />
              <DItem label="Cache" value={fmt(selectedDay.cacheRead)} colors={colors} styles={s} />
              <DItem label="Output" value={fmt(selectedDay.outputTokens)} colors={colors} styles={s} />
              <DItem label="Reason" value={fmt(selectedDay.reasoningTokens)} colors={colors} styles={s} />
            </View>
          </>
        )}
      </RisingSheet>
    </SafeAreaView>
  );
}

// ── Small components ───────────────────────────────────────

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
    <View style={styles.dItem}>
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
      style={{ alignItems: 'center', paddingVertical: 10, marginTop: 6 }}
      scale={0.97}
      onPress={onPress}
    >
      <Text style={{ color: colors.accent, fontSize: 11.5, fontFamily: Typography.uiFontMedium, opacity: 0.75 }}>
        {expanded ? 'less' : `${total - MAX_LIST_ITEMS} more`}
      </Text>
    </AnimatedPressable>
  );
}

// ── Styles ─────────────────────────────────────────────────

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bgPrimary },
  header: {
    paddingHorizontal: 18,
    paddingTop: 16,
    paddingBottom: 12,
    gap: 14,
    backgroundColor: colors.bgPrimary,
    zIndex: 2,
  },
  title: {
    color: colors.textPrimary,
    fontSize: 30,
    lineHeight: 34,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: -0.6,
  },
  rangeRow: {
    flexDirection: 'row',
    gap: 20,
  },
  rangeTab: {
    alignItems: 'center',
    paddingVertical: 4,
    minWidth: 36,
  },
  rangeTabText: {
    color: colors.textTertiary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  rangeTabTextActive: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
  },
  rangeTabIndicator: {
    marginTop: 5,
    width: 16,
    height: 2,
    borderRadius: 1,
    backgroundColor: colors.accent,
  },

  scrollView: { flex: 1 },
  scroll: { paddingHorizontal: 18, gap: 12, paddingTop: 6, paddingBottom: 36 },

  // Empty
  emptyContainer: { flex: 1, justifyContent: 'center', alignItems: 'center', paddingHorizontal: 36 },
  emptyIcon: {
    fontSize: 40,
    color: colors.accent,
    lineHeight: 46,
    marginBottom: 20,
  },
  emptyText: { color: colors.textPrimary, fontSize: 17, fontFamily: Typography.uiFontMedium },
  emptySubtext: {
    color: colors.textSecondary,
    fontSize: 13.5,
    fontFamily: Typography.uiFont,
    marginTop: 8,
    maxWidth: 300,
    textAlign: 'center',
    lineHeight: 19,
    opacity: 0.8,
  },

  // Card
  card: {
    borderRadius: Radii.md,
    backgroundColor: colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
    paddingHorizontal: 16,
    paddingVertical: 14,
    ...shadow('card', colors.shadowColor),
  },
  label: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    textTransform: 'uppercase',
    marginBottom: 10,
  },
  labelRow: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10,
  },
  labelCount: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },

  // Cost hero
  costBig: {
    color: colors.accent,
    fontSize: 32,
    fontFamily: Typography.terminalFontBold,
    lineHeight: 38,
    marginBottom: 6,
  },
  costMeta: {
    color: colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.terminalFont,
    lineHeight: 18,
  },

  // Activity heatmap
  heatmapRow: {
    flexDirection: 'row',
    gap: 3,
    paddingVertical: 4,
  },
  heatmapColumn: {
    gap: 3,
  },
  heatCell: {
    width: 12,
    height: 12,
    borderRadius: 2,
  },
  heatCellEmpty: {
    width: 12,
    height: 12,
  },
  heatCellLegend: {
    width: 10,
    height: 10,
    borderRadius: 2,
  },
  heatmapLegend: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    gap: 4,
    marginTop: 10,
  },
  heatmapLegendLabel: {
    color: colors.textTertiary,
    fontSize: 10,
    fontFamily: Typography.uiFont,
    marginHorizontal: 2,
  },

  // Rank rows
  row: {
    flexDirection: 'row', alignItems: 'center', gap: 10,
    paddingVertical: 8,
    borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.borderSubtle,
  },
  rowInfo: { flex: 1, minWidth: 0 },
  rowName: { color: colors.textPrimary, fontSize: 12.5, fontFamily: Typography.terminalFont },
  rowMeta: { color: colors.textTertiary, fontSize: 10, fontFamily: Typography.uiFont, marginTop: 2 },
  rowCost: {
    color: colors.accent,
    fontSize: 12.5,
    fontFamily: Typography.terminalFontBold,
    minWidth: 44,
    textAlign: 'right',
  },
  rowCount: {
    color: colors.textSecondary,
    fontSize: 12.5,
    fontFamily: Typography.terminalFontBold,
    minWidth: 34,
    textAlign: 'right',
  },

  // Skill
  skillCmd: { color: colors.statusUnknown, fontSize: 12.5, fontFamily: Typography.terminalFontBold },

  // Modal / day detail
  detailCard: {
    width: 260,
    borderRadius: Radii.lg,
    padding: 20,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  detailTitle: {
    color: colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 16,
    textAlign: 'center',
  },
  detailGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
  dItem: { width: '46%', alignItems: 'center' },
  dLabel: { color: colors.textTertiary, fontSize: 10, fontFamily: Typography.uiFont, marginBottom: 4, letterSpacing: 0.4 },
  dValue: { color: colors.textPrimary, fontSize: 17, fontFamily: Typography.terminalFontBold },
  });
}
