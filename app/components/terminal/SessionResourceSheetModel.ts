import {
  formatByteSize,
  formatExactBytesLabel,
  formatUptime,
  type SessionResourceSnapshot,
} from "../../services/sessionResourceSnapshot";

/** Compact sheet projection: labels + optional clamped bar shares. */
export type SessionResourcePresentation = {
  memoryLabel: string | null;
  memoryExact?: string;
  peakLabel?: string;
  tasksLabel?: string;
  qualifier?: string;
  /** Compact copy when Zen does not manage this Session's resources. */
  unmanagedNote?: string;
  /** True when Session memory/peak/tasks exist to justify the hero card. */
  showSessionHero: boolean;

  poolSummary?: string;
  skewNote?: string;
  otherLabel?: string;
  /** Omitted without a trustworthy hard max. Shares are geometry-only. */
  bar?: {
    session: number;
    other: number;
    remaining: number;
    /** cgroup live MemoryHigh/MemoryMax only; marker = "Protection starts". */
    protectionAt?: number;
    split: boolean;
  };

  hostAvailable?: string;
  hostAvailableExact?: string;
  hostStatus?: "ok" | "wait";
  hostChip?: string;
  hostNote?: string;

  metaLine?: string;
  workspace?: string;
  accessibilityLabel: string;
};

export function buildSessionResourceViewModel(
  snapshot?: SessionResourceSnapshot | null,
  nowMs = Date.now(),
): SessionResourcePresentation | null {
  if (!snapshot) return null;

  const { session, pool, host } = snapshot;
  const managed = session.managed === true;
  const backend = pool?.backend ?? session.backend;
  const memoryLabel = formatByteSize(session.memory_current_bytes);
  const memoryExact =
    formatExactBytesLabel(session.memory_current_bytes) ?? undefined;
  const peak = formatByteSize(session.memory_peak_bytes);
  const tasks =
    typeof session.tasks_current === "number" &&
    Number.isFinite(session.tasks_current) &&
    session.tasks_current >= 0
      ? `${Math.round(session.tasks_current)} task${session.tasks_current === 1 ? "" : "s"}`
      : undefined;
  const qualifier =
    managed && session.backend === "cgroup_pool"
      ? "Measured by the system"
      : managed && session.backend === "portable_supervisor"
        ? "Estimated from owned processes"
        : undefined;

  // Defense: never render pool for unmanaged Sessions, even with stale wire.
  const showPool =
    managed &&
    !!pool &&
    (pool.backend != null ||
      pool.memory_current_bytes != null ||
      pool.memory_high_bytes != null ||
      pool.memory_max_bytes != null);
  const usedLbl = showPool ? formatByteSize(pool?.memory_current_bytes) : null;
  const maxLbl = showPool ? formatByteSize(pool?.memory_max_bytes) : null;
  const poolSummary =
    usedLbl && maxLbl
      ? `${usedLbl} used of ${maxLbl}`
      : usedLbl
        ? `${usedLbl} used`
        : maxLbl
          ? `Hard limit ${maxLbl}`
          : undefined;

  const sessionBytes = session.memory_current_bytes;
  const poolCurrent = showPool ? pool?.memory_current_bytes : undefined;
  const skewed =
    typeof sessionBytes === "number" &&
    typeof poolCurrent === "number" &&
    sessionBytes > poolCurrent;
  // Normal composition: Other = max(pool − session, 0). Skew drops the split.
  const otherBytes =
    typeof sessionBytes === "number" && typeof poolCurrent === "number"
      ? Math.max(poolCurrent - sessionBytes, 0)
      : undefined;
  const split =
    showPool &&
    !skewed &&
    typeof sessionBytes === "number" &&
    typeof poolCurrent === "number";

  const { hostStatus, hostChip, hostNote } = hostCopy(host ?? null);
  const hostAvailable = formatByteSize(host?.available_bytes) ?? undefined;
  const hostAvailableExact =
    formatExactBytesLabel(host?.available_bytes) ?? undefined;

  const metaLine = [
    (session.executor || session.command)?.trim() || undefined,
    [session.status, session.phase].filter(Boolean).join(" · ") || undefined,
    formatUptime(session.started_at, nowMs) ?? undefined,
  ]
    .filter(Boolean)
    .join(" · ");

  const unmanagedNote = managed ? undefined : "Not resource-managed by Zen";
  const peakLabel = peak ? `Peak ${peak}` : undefined;
  const showSessionHero =
    managed &&
    (memoryLabel != null || peakLabel != null || tasks != null);
  const skewNote =
    showPool && skewed
      ? "Session and pool readings updated separately"
      : undefined;
  const bar = showPool
    ? poolBar(
        sessionBytes,
        poolCurrent,
        pool?.memory_high_bytes,
        pool?.memory_max_bytes,
        split,
        backend === "cgroup_pool",
      )
    : undefined;
  const otherLabel =
    split && otherBytes != null
      ? `Other Agents · ${formatByteSize(otherBytes) ?? "—"}`
      : undefined;

  const accessibilityLabel = [
    unmanagedNote,
    showSessionHero && memoryLabel
      ? `This Session ${memoryExact ?? memoryLabel}`
      : null,
    showSessionHero ? peakLabel : null,
    showSessionHero ? tasks : null,
    qualifier,
    poolSummary ? `Shared pool ${poolSummary}` : null,
    skewNote,
    hostAvailable
      ? `Host available ${hostAvailableExact ?? hostAvailable}`
      : null,
    hostNote,
    metaLine || null,
  ]
    .filter(Boolean)
    .join(". ");

  return {
    memoryLabel: showSessionHero ? memoryLabel : null,
    memoryExact: showSessionHero ? memoryExact : undefined,
    peakLabel: showSessionHero ? peakLabel : undefined,
    tasksLabel: showSessionHero ? tasks : undefined,
    qualifier,
    unmanagedNote,
    showSessionHero,
    poolSummary,
    skewNote,
    otherLabel,
    bar,
    hostAvailable,
    hostAvailableExact,
    hostStatus,
    hostChip,
    hostNote,
    metaLine: metaLine || undefined,
    workspace: session.cwd?.trim() || undefined,
    accessibilityLabel,
  };
}

function hostCopy(host: SessionResourceSnapshot["host"]): {
  hostStatus?: "ok" | "wait";
  hostChip?: string;
  hostNote?: string;
} {
  if (!host) return {};
  const available = formatByteSize(host.available_bytes);
  if (!available && !host.pressure) return {};
  if (host.pressure === "ok") {
    return {
      hostStatus: "ok",
      hostChip: "Enough memory headroom",
      // Positive headroom keeps the chip only; no redundant reassurance note.
    };
  }
  if (host.pressure === "pressure") {
    return {
      hostStatus: "wait",
      hostChip: "Limited memory headroom",
      hostNote: "Agents may wait for memory headroom",
    };
  }
  return {
    hostChip: "Host headroom",
    hostNote: "Memory headroom state unavailable",
  };
}

/** Geometry shares only; never invent a denominator or skew filler. */
function poolBar(
  sessionBytes: number | undefined,
  poolCurrent: number | undefined,
  poolHigh: number | undefined,
  poolMax: number | undefined,
  split: boolean,
  allowProtection: boolean,
): SessionResourcePresentation["bar"] {
  if (
    typeof poolMax !== "number" ||
    !Number.isFinite(poolMax) ||
    poolMax <= 0 ||
    typeof poolCurrent !== "number" ||
    !Number.isFinite(poolCurrent) ||
    poolCurrent < 0
  ) {
    return undefined;
  }

  let session = 0;
  let other = 0;
  if (split && typeof sessionBytes === "number" && typeof poolCurrent === "number") {
    session = clamp01(sessionBytes / poolMax);
    other = clamp01(Math.max(poolCurrent - sessionBytes, 0) / poolMax);
  } else {
    session = clamp01(poolCurrent / poolMax);
  }

  let remaining = clamp01(1 - session - other);
  const sum = session + other + remaining;
  if (sum > 1) {
    session /= sum;
    other /= sum;
    remaining = Math.max(0, 1 - session - other);
  }

  const protectionAt =
    allowProtection &&
    typeof poolHigh === "number" &&
    Number.isFinite(poolHigh) &&
    poolHigh > 0
      ? clamp01(poolHigh / poolMax)
      : undefined;

  return { session, other, remaining, protectionAt, split };
}

function clamp01(n: number): number {
  if (!Number.isFinite(n) || n <= 0) return 0;
  if (n >= 1) return 1;
  return n;
}
