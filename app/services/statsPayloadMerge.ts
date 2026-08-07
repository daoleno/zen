import {
  isOfficialCodexSubscription,
} from "./codexSubscriptionStats";

// ── Wire payload contract (mirrors daemon/stats/types.go and the daemon
// get_stats handler) ─────────────────────────────────────────

export interface StatsPayload {
  ranges: Record<string, RangeData>;
  codexSubscription?: CodexSubscriptionUsage;
  codexSubscriptions?: CodexSubscriptionUsage[];
  serverId?: string;
  serverUrl?: string;
  daemonId?: string;
  daemonPublicKey?: string;
}

export interface CodexUsageWindow {
  name: "primary" | "secondary" | string;
  usedPercent: number;
  windowMinutes?: number;
  resetsAt?: string;
}

export interface CodexSubscriptionUsage {
  authKind: "official" | "api_key" | "absent" | "unknown";
  state: "available" | "unavailable";
  plan?: string;
  windows?: CodexUsageWindow[];
  fetchedAt?: string;
  stale?: boolean;
  serverLabel?: string;
}

export interface DayCell {
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

export interface ModelStat {
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

export interface ProjectStat {
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

export interface SkillStat {
  name: string;
  calls: number;
  projects: string[];
}

export interface ToolStat {
  name: string;
  calls: number;
}

export interface RangeData {
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

// ── Merge ───────────────────────────────────────────────────

export const EMPTY_RANGE: RangeData = {
  cost: 0, costKnown: true, totalTokens: 0, totalTokensKnown: true, inputTokens: 0, outputTokens: 0,
  reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, tokenBreakdownKnown: true,
  sessions: 0, models: [], projects: [], skills: [], tools: [], days: [],
};

export function isCostKnown(item: { costKnown?: boolean }): boolean {
  return item.costKnown !== false;
}

export function isTotalTokensKnown(item: { totalTokensKnown?: boolean }): boolean {
  return item.totalTokensKnown !== false;
}

export function isTokenBreakdownKnown(item: { tokenBreakdownKnown?: boolean }): boolean {
  return item.tokenBreakdownKnown !== false;
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

export function mergeStatsPayloads(payloads: StatsPayload[]): StatsPayload | null {
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

// hasRangeStats reports whether a range carries any observed usage, the
// condition for rendering the model-usage section.
export function hasRangeStats(data?: RangeData | null): boolean {
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
