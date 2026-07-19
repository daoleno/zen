import {
  formatByteSize,
  formatExactBytesLabel,
  formatUptime,
  type SessionResourceSnapshot,
} from "../../services/sessionResourceSnapshot";

export type SessionResourceHostSupport = {
  placement: "pool" | "footer";
  label: string;
  accessibilityLabel: string;
};

export type SessionResourceHostPresentation =
  | { state: "missing" }
  | { state: "healthy"; support?: SessionResourceHostSupport }
  | { state: "unavailable"; support: SessionResourceHostSupport }
  | {
      state: "pressure";
      warning: {
        title: "Limited memory headroom";
        available?: string;
        availableExact?: string;
        note: "Agents may wait for memory headroom";
        accessibilityLabel: string;
      };
    };

export type SessionResourceHostSections = {
  poolSupport?: SessionResourceHostSupport;
  footerSupport?: SessionResourceHostSupport;
  warning?: Extract<
    SessionResourceHostPresentation,
    { state: "pressure" }
  >["warning"];
};

export function resolveSessionResourceHostSections(
  host: SessionResourceHostPresentation,
): SessionResourceHostSections {
  if (host.state === "pressure") return { warning: host.warning };
  if (host.state === "missing" || !host.support) return {};
  return host.support.placement === "pool"
    ? { poolSupport: host.support }
    : { footerSupport: host.support };
}

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
  showPoolCard: boolean;
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

  host: SessionResourceHostPresentation;

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
    managed && (memoryLabel != null || peakLabel != null || tasks != null);
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
  const showPoolCard = !!(poolSummary || bar || skewNote);
  const hostPresentation = buildHostPresentation(host ?? null, showPoolCard);

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
    hostAccessibilityLabel(hostPresentation),
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
    showPoolCard,
    skewNote,
    otherLabel,
    bar,
    host: hostPresentation,
    metaLine: metaLine || undefined,
    workspace: session.cwd?.trim() || undefined,
    accessibilityLabel,
  };
}

function buildHostPresentation(
  host: SessionResourceSnapshot["host"],
  showPoolCard: boolean,
): SessionResourceHostPresentation {
  if (!host) return { state: "missing" };
  const available = formatByteSize(host.available_bytes);
  const availableExact =
    formatExactBytesLabel(host.available_bytes) ?? undefined;
  const placement = showPoolCard ? "pool" : "footer";

  if (host.pressure === "ok") {
    return {
      state: "healthy",
      support: available
        ? {
            placement,
            label: `Host · ${available} available`,
            accessibilityLabel: `Host available ${availableExact ?? available}`,
          }
        : undefined,
    };
  }
  if (host.pressure === "pressure") {
    const title = "Limited memory headroom";
    const note = "Agents may wait for memory headroom";
    return {
      state: "pressure",
      warning: {
        title,
        available: available ?? undefined,
        availableExact,
        note,
        accessibilityLabel: [
          title,
          available ? `Host available ${availableExact ?? available}` : null,
          note,
        ]
          .filter(Boolean)
          .join(". "),
      },
    };
  }

  const unavailableLabel = "Memory headroom state unavailable";
  return {
    state: "unavailable",
    support: {
      placement,
      label: available
        ? `Host · ${available} available · Headroom state unavailable`
        : `Host · ${unavailableLabel}`,
      accessibilityLabel: [
        available ? `Host available ${availableExact ?? available}` : null,
        unavailableLabel,
      ]
        .filter(Boolean)
        .join(". "),
    },
  };
}

function hostAccessibilityLabel(
  host: SessionResourceHostPresentation,
): string | null {
  if (host.state === "pressure") return host.warning.accessibilityLabel;
  if (host.state === "healthy" || host.state === "unavailable") {
    return host.support?.accessibilityLabel ?? null;
  }
  return null;
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
  if (
    split &&
    typeof sessionBytes === "number" &&
    typeof poolCurrent === "number"
  ) {
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
