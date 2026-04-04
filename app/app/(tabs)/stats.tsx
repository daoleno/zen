import React, { useCallback, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { useFocusEffect } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Typography } from '../../constants/tokens';
import { wsClient } from '../../services/websocket';

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

const INTENSITY_COLORS = [
  'rgba(91,157,255,0.05)',
  'rgba(91,157,255,0.20)',
  'rgba(91,157,255,0.45)',
  'rgba(91,157,255,0.80)',
];

const MAX_LIST_ITEMS = 5;

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
  if (payloads.length === 0) return null;
  const rangeKeys = new Set<string>();
  for (const p of payloads) for (const k of Object.keys(p.ranges ?? {})) rangeKeys.add(k);
  const ranges: Record<string, RangeData> = {};
  for (const k of rangeKeys) {
    ranges[k] = mergeRangeData(payloads.map(p => p.ranges?.[k] ?? EMPTY_RANGE));
  }
  return { ranges };
}

// ── Component ──────────────────────────────────────────────

export default function StatsScreen() {
  const [range, setRange] = useState<TimeRange>('week');
  const [statsData, setStatsData] = useState<StatsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set());
  const [selectedDay, setSelectedDay] = useState<DayCell | null>(null);

  const toggleSection = (section: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  };

  useFocusEffect(
    useCallback(() => {
      const serverIds = wsClient.connectedServerIds();
      if (serverIds.length === 0) { setStatsData(null); setLoading(false); return; }
      setLoading(true);
      Promise.allSettled(serverIds.map(id => wsClient.getStats(id)))
        .then(results => {
          const payloads = results
            .filter((r): r is PromiseFulfilledResult<StatsPayload> => r.status === 'fulfilled')
            .map(r => r.value);
          setStatsData(mergeStatsPayloads(payloads));
        })
        .catch(() => {})
        .finally(() => setLoading(false));
    }, []),
  );

  const data = statsData?.ranges?.[range] ?? EMPTY_RANGE;
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

  const hasData = data.sessions > 0 || (data.tools?.length ?? 0) > 0;

  return (
    <SafeAreaView style={s.container} edges={['top']}>
      <View style={s.header}>
        <Text style={s.title}>Stats</Text>
        <View style={s.rangeToggle}>
          {RANGE_OPTIONS.map(opt => {
            const active = range === opt.key;
            return (
              <TouchableOpacity
                key={opt.key}
                style={s.rangeBtn}
                onPress={() => setRange(opt.key)}
                activeOpacity={0.82}
              >
                <Text style={[s.rangeBtnText, active && s.rangeBtnTextOn]}>
                  {opt.label}
                </Text>
                {active && <View style={s.rangeBtnBar} />}
              </TouchableOpacity>
            );
          })}
        </View>
      </View>

      {loading ? (
        <View style={s.emptyContainer}>
          <ActivityIndicator color={Colors.textSecondary} />
        </View>
      ) : !hasData ? (
        <View style={s.emptyContainer}>
          <Text style={s.emptyIcon}>{'∷'}</Text>
          <Text style={s.emptyText}>No stats yet</Text>
          <Text style={s.emptySubtext}>Connect to a server to start collecting data.</Text>
        </View>
      ) : (
        <ScrollView contentContainerStyle={s.scroll} showsVerticalScrollIndicator={false}>

          {/* ── Estimated cost ── */}
          <View style={s.card}>
            <View style={s.costRow}>
              <Text style={s.costBig}>{fmtCost(data.cost)}</Text>
              <View style={s.costRight}>
                <Text style={s.costMeta}>Estimated Cost</Text>
                <Text style={s.costMeta}>{fmt(data.totalTokens)} tokens</Text>
                {data.reasoningTokens > 0 && (
                  <Text style={s.costMeta}>{fmt(data.reasoningTokens)} reasoning</Text>
                )}
                <Text style={s.costMeta}>{data.sessions} sessions</Text>
              </View>
            </View>
          </View>

          {/* ── Daily activity bars ── */}
          {days.length > 1 && (
            <View style={s.card}>
              <Text style={s.label}>Activity</Text>
              {days.length <= 14 ? (
                <View style={s.barChartFlex}>
                  {days.map((d, i) => {
                    const intensity = barIntensity(d.cost, maxDayCost);
                    return (
                      <TouchableOpacity key={i} style={s.barColFlex} activeOpacity={0.7} onPress={() => setSelectedDay(d)}>
                        <View style={s.barOuter}>
                          <View
                            style={[s.barInner, {
                              height: `${Math.max(d.cost / maxDayCost * 100, 4)}%`,
                              backgroundColor: INTENSITY_COLORS[intensity],
                            }]}
                          />
                        </View>
                        <Text style={s.barDate}>{shortDate(d.date)}</Text>
                      </TouchableOpacity>
                    );
                  })}
                </View>
              ) : (
                <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                  <View style={s.barChartScroll}>
                    {days.map((d, i) => {
                      const intensity = barIntensity(d.cost, maxDayCost);
                      return (
                        <TouchableOpacity key={i} style={s.barColFixed} activeOpacity={0.7} onPress={() => setSelectedDay(d)}>
                          <View style={s.barOuter}>
                            <View
                              style={[s.barInner, {
                                height: `${Math.max(d.cost / maxDayCost * 100, 4)}%`,
                                backgroundColor: INTENSITY_COLORS[intensity],
                              }]}
                            />
                          </View>
                          <Text style={s.barDate}>{shortDate(d.date)}</Text>
                        </TouchableOpacity>
                      );
                    })}
                  </View>
                </ScrollView>
              )}
            </View>
          )}

          {/* ── Models ── */}
          {(data.models?.length ?? 0) > 0 && (
            <View style={s.card}>
              <Text style={s.label}>Models</Text>
              {(expandedSections.has('models') ? data.models : visibleModels).map((m, i) => (
                <View key={i} style={s.row}>
                  <View style={s.rowInfo}>
                    <Text style={s.rowName} numberOfLines={1}>{m.name}</Text>
                    <Text style={s.rowMeta}>{tokenSummary(m)} · {m.sessions} sessions</Text>
                  </View>
                  <Text style={s.rowCost}>{fmtCost(m.cost)}</Text>
                  <Bar ratio={m.cost / maxModelCost} color={Colors.accent} />
                </View>
              ))}
              {data.models.length > MAX_LIST_ITEMS && (
                <ExpandToggle expanded={expandedSections.has('models')} total={data.models.length} onPress={() => toggleSection('models')} />
              )}
            </View>
          )}

          {/* ── Projects ── */}
          {(data.projects?.length ?? 0) > 0 && (
            <View style={s.card}>
              <Text style={s.label}>Projects</Text>
              {(expandedSections.has('projects') ? data.projects : visibleProjects).map((p, i) => (
                <View key={i} style={s.row}>
                  <View style={s.rowInfo}>
                    <Text style={s.rowName} numberOfLines={1}>{p.name}</Text>
                    <Text style={s.rowMeta}>{fmt(p.totalTokens)} tokens · {p.sessions} sessions</Text>
                  </View>
                  <Text style={s.rowCost}>{p.cost > 0 ? fmtCost(p.cost) : fmt(p.totalTokens)}</Text>
                  <Bar ratio={p.cost > 0 ? p.cost / maxProjectCost : p.totalTokens / maxProjectTokens} color={Colors.accent} />
                </View>
              ))}
              {data.projects.length > MAX_LIST_ITEMS && (
                <ExpandToggle expanded={expandedSections.has('projects')} total={data.projects.length} onPress={() => toggleSection('projects')} />
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
              {(expandedSections.has('skills') ? data.skills : visibleSkills).map((sk, i) => (
                <View key={i} style={s.row}>
                  <View style={s.rowInfo}>
                    <Text style={s.skillCmd}>{sk.name}</Text>
                    <Text style={s.rowMeta}>{sk.projects?.join(' · ')}</Text>
                  </View>
                  <Text style={s.rowCount}>{sk.calls}</Text>
                  <Bar ratio={sk.calls / maxSkillCalls} color={Colors.statusUnknown} />
                </View>
              ))}
              {totalSkills > MAX_LIST_ITEMS && (
                <ExpandToggle expanded={expandedSections.has('skills')} total={totalSkills} onPress={() => toggleSection('skills')} />
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
              {(expandedSections.has('tools') ? data.tools : visibleTools).map((t, i) => (
                <View key={i} style={s.row}>
                  <View style={s.rowInfo}>
                    <Text style={s.rowName}>{t.name}</Text>
                  </View>
                  <Text style={s.rowCount}>{t.calls}</Text>
                  <Bar ratio={t.calls / maxToolCalls} color={Colors.statusRunning} />
                </View>
              ))}
              {(data.tools?.length ?? 0) > MAX_LIST_ITEMS && (
                <ExpandToggle expanded={expandedSections.has('tools')} total={data.tools.length} onPress={() => toggleSection('tools')} />
              )}
            </View>
          )}

          <View style={{ height: 24 }} />
        </ScrollView>
      )}
      {/* ── Day detail modal ── */}
      <Modal
        visible={selectedDay !== null}
        transparent animationType="fade"
        onRequestClose={() => setSelectedDay(null)}
      >
        <View style={s.modalRoot}>
          <TouchableOpacity style={s.modalBg} activeOpacity={1} onPress={() => setSelectedDay(null)} />
          {selectedDay && (
            <View style={s.detailCard}>
              <Text style={s.detailTitle}>{selectedDay.date}</Text>
              <View style={s.detailGrid}>
                <DItem label="Cost" value={fmtCost(selectedDay.cost)} accent />
                <DItem label="Sessions" value={`${selectedDay.sessions}`} />
                <DItem label="Total" value={fmt(selectedDay.totalTokens)} />
                <DItem label="Input" value={fmt(selectedDay.inputTokens)} />
                <DItem label="Cache" value={fmt(selectedDay.cacheRead)} />
                <DItem label="Output" value={fmt(selectedDay.outputTokens)} />
                <DItem label="Reason" value={fmt(selectedDay.reasoningTokens)} />
              </View>
            </View>
          )}
        </View>
      </Modal>
    </SafeAreaView>
  );
}

// ── Small components ───────────────────────────────────────

function DItem({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <View style={s.dItem}>
      <Text style={s.dLabel}>{label}</Text>
      <Text style={[s.dValue, accent && { color: Colors.accent }]}>{value}</Text>
    </View>
  );
}

function Bar({ ratio, color }: { ratio: number; color: string }) {
  return (
    <View style={s.barTrack}>
      <View style={[s.barFill, { width: `${Math.min(ratio, 1) * 100}%`, backgroundColor: color }]} />
    </View>
  );
}

function ExpandToggle({ expanded, total, onPress }: { expanded: boolean; total: number; onPress: () => void }) {
  return (
    <TouchableOpacity style={s.expandBtn} onPress={onPress} activeOpacity={0.7}>
      <Text style={s.expandText}>
        {expanded ? 'less' : `${total - MAX_LIST_ITEMS} more`}
      </Text>
    </TouchableOpacity>
  );
}

// ── Styles ─────────────────────────────────────────────────

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 16, paddingTop: 6, paddingBottom: 8,
  },
  title: {
    color: Colors.textPrimary, fontSize: 22, fontFamily: Typography.uiFontMedium,
    letterSpacing: 1, opacity: 0.9,
  },
  rangeToggle: { flexDirection: 'row', gap: 16 },
  rangeBtn: { alignItems: 'center', paddingVertical: 2 },
  rangeBtnText: {
    color: Colors.textSecondary, fontSize: 13, fontFamily: Typography.uiFont,
    letterSpacing: 0.3, opacity: 0.4,
  },
  rangeBtnTextOn: { color: Colors.textPrimary, opacity: 0.9 },
  rangeBtnBar: {
    width: 12, height: 1.5, borderRadius: 1,
    backgroundColor: Colors.accent, marginTop: 3, opacity: 0.6,
  },

  scroll: { paddingHorizontal: 16, gap: 10 },

  // Empty
  emptyContainer: { flex: 1, justifyContent: 'center', alignItems: 'center', paddingHorizontal: 24 },
  emptyIcon: { fontSize: 44, color: Colors.textSecondary, marginBottom: 16, opacity: 0.6 },
  emptyText: { color: Colors.textPrimary, fontSize: 17, fontFamily: Typography.uiFontMedium, opacity: 0.8 },
  emptySubtext: { color: Colors.textSecondary, fontSize: 13, fontFamily: Typography.uiFont, marginTop: 6, textAlign: 'center', opacity: 0.6 },

  // Card
  card: {
    borderRadius: 12, backgroundColor: 'rgba(255,255,255,0.03)',
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.06)',
    paddingHorizontal: 14, paddingVertical: 12,
  },
  label: {
    color: Colors.textSecondary, fontSize: 10, fontFamily: Typography.uiFont,
    letterSpacing: 0.4, marginBottom: 8, opacity: 0.5,
  },
  labelRow: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8,
  },
  labelCount: {
    color: Colors.textSecondary, fontSize: 10, fontFamily: Typography.terminalFont, opacity: 0.35,
  },

  // Cost hero — horizontal: big number left, meta right-aligned
  costRow: { flexDirection: 'row', alignItems: 'center' },
  costBig: {
    color: Colors.accent, fontSize: 28, fontFamily: Typography.terminalFontBold, lineHeight: 34,
  },
  costRight: { flex: 1, alignItems: 'flex-end', gap: 2 },
  costMeta: {
    color: Colors.textSecondary, fontSize: 11, fontFamily: Typography.terminalFont, opacity: 0.45,
  },

  // Daily bar chart — flex layout (<=14 days, fills width)
  barChartFlex: {
    flexDirection: 'row', gap: 3, alignItems: 'flex-end', paddingVertical: 4,
  },
  barColFlex: { flex: 1, alignItems: 'center' },
  // Daily bar chart — scroll layout (>14 days)
  barChartScroll: {
    flexDirection: 'row', gap: 3, alignItems: 'flex-end', paddingVertical: 4,
  },
  barColFixed: { alignItems: 'center', width: 24 },
  barOuter: {
    width: '100%', maxWidth: 20, height: 48, borderRadius: 3,
    backgroundColor: 'rgba(255,255,255,0.03)',
    justifyContent: 'flex-end', overflow: 'hidden',
    alignSelf: 'center',
  },
  barInner: { width: '100%', borderRadius: 3, minHeight: 2 },
  barDate: {
    color: Colors.textSecondary, fontSize: 7, fontFamily: Typography.uiFont,
    marginTop: 3, opacity: 0.4,
  },

  // Rank rows
  row: {
    flexDirection: 'row', alignItems: 'center', gap: 8,
    paddingVertical: 7,
    borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: 'rgba(255,255,255,0.04)',
  },
  rowInfo: { flex: 1, minWidth: 0 },
  rowName: { color: Colors.textPrimary, fontSize: 12, fontFamily: Typography.terminalFont },
  rowMeta: { color: Colors.textSecondary, fontSize: 9, fontFamily: Typography.uiFont, marginTop: 1, opacity: 0.45 },
  rowCost: { color: Colors.accent, fontSize: 12, fontFamily: Typography.terminalFontBold, minWidth: 42, textAlign: 'right' },
  rowCount: { color: Colors.textSecondary, fontSize: 12, fontFamily: Typography.terminalFontBold, minWidth: 32, textAlign: 'right' },

  // Skill
  skillCmd: { color: Colors.statusUnknown, fontSize: 12, fontFamily: Typography.terminalFontBold },

  // Expand
  expandBtn: { alignItems: 'center', paddingVertical: 8, marginTop: 4 },
  expandText: { color: Colors.accent, fontSize: 11, fontFamily: Typography.uiFontMedium, opacity: 0.7 },

  // Modal
  modalRoot: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  modalBg: { ...StyleSheet.absoluteFillObject, backgroundColor: 'rgba(0,0,0,0.6)' },
  detailCard: {
    width: 240, borderRadius: 14, padding: 16,
    backgroundColor: '#1A1A22', borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.08)',
  },
  detailTitle: {
    color: Colors.textPrimary, fontSize: 14, fontFamily: Typography.uiFontMedium,
    marginBottom: 12, textAlign: 'center',
  },
  detailGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10 },
  dItem: { width: '46%', alignItems: 'center' },
  dLabel: { color: Colors.textSecondary, fontSize: 9, fontFamily: Typography.uiFont, opacity: 0.5, marginBottom: 3 },
  dValue: { color: Colors.textPrimary, fontSize: 16, fontFamily: Typography.terminalFontBold },

  // Bar (inline rank bar)
  barTrack: { width: 40, height: 2.5, borderRadius: 1.5, backgroundColor: 'rgba(255,255,255,0.04)' },
  barFill: { height: 2.5, borderRadius: 1.5, opacity: 0.55 },
});
