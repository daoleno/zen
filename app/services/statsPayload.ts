import {
  isOfficialCodexSubscription,
} from "./codexSubscriptionStats";

// ── Wire payload contract (mirrors daemon/stats/types.go and the daemon
// get_stats handler) ─────────────────────────────────────────

export interface StatsPayload {
  ranges: Record<string, RangeData>;
  codexSubscription?: CodexSubscriptionUsage;
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

export interface StatsView {
  ranges: Record<string, RangeData>;
  codexSubscriptions: CodexSubscriptionUsage[];
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

// Current-server snapshot normalization. Totals remain daemon-owned.

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

export function normalizeStatsPayload(payload: StatsPayload | null | undefined): StatsView | null {
  if (!payload || !payload.ranges || typeof payload.ranges !== "object" || Array.isArray(payload.ranges)) return null;
  const ranges: Record<string, RangeData> = {};
  for (const [key, range] of Object.entries(payload.ranges)) {
    if (!range || typeof range !== "object") continue;
    ranges[key] = {
      ...range,
      models: Array.isArray(range.models) ? range.models.map(row => ({ ...row })) : [],
      projects: Array.isArray(range.projects) ? range.projects.map(row => ({ ...row })) : [],
      skills: Array.isArray(range.skills) ? range.skills.map(row => ({ ...row, projects: Array.isArray(row.projects) ? [...row.projects] : [] })) : [],
      tools: Array.isArray(range.tools) ? range.tools.map(row => ({ ...row })) : [],
      days: Array.isArray(range.days) ? range.days.map(row => ({ ...row })) : [],
    };
  }
  const subscription = payload.codexSubscription;
  return {
    ranges,
    codexSubscriptions: isOfficialCodexSubscription(subscription) ? [{
      ...subscription!,
      windows: Array.isArray(subscription!.windows) ? subscription!.windows.map(window => ({ ...window })) : undefined,
      serverLabel: payload.serverId ?? payload.serverUrl,
    }] : [],
  };
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
