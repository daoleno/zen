import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { AgentStatus } from "../../constants/tokens";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import type { WorkItem } from "../../store/work";
import type { StoredRecentAgentOpens } from "../../services/storage";

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

export interface MenuAnchorLayout {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TerminalFallbackPresentation {
  accent: string;
  busy: boolean;
  title: string;
  detail: string;
  hint: string;
}

export function findLinkedWork(
  byKey: Record<string, WorkItem>,
  serverId: string,
  agentId: string,
): WorkItem | undefined {
  if (!serverId || !agentId) return undefined;

  return Object.values(byKey)
    .filter(
      (current) =>
        current.serverId === serverId &&
        current.frontmatter.agent_session === agentId,
    )
    .sort((left, right) => getWorkStartedAt(right) - getWorkStartedAt(left))[0];
}

export function buildTerminalFallbackPresentation({
  hasTerminalRoute,
  connectionState,
  connectionIssue,
  terminalTheme,
  chromeColors,
}: {
  hasTerminalRoute: boolean;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  terminalTheme: TerminalThemePalette;
  chromeColors: TerminalThemeChrome;
}): TerminalFallbackPresentation {
  return {
    accent: connectionIssue
      ? terminalTheme.red
      : connectionState === "connecting"
        ? terminalTheme.yellow
        : chromeColors.textSubtle,
    busy:
      hasTerminalRoute && connectionState === "connecting" && !connectionIssue,
    title: !hasTerminalRoute
      ? "Terminal unavailable"
      : connectionIssue?.title ||
        (connectionState === "connecting"
          ? "Reconnecting to daemon"
          : "Daemon unavailable"),
    detail: !hasTerminalRoute
      ? "Open this terminal again from the Agents tab."
      : connectionIssue?.detail ||
        (connectionState === "connecting"
          ? "Zen is reconnecting before reopening this terminal."
          : "Start zen-daemon on that machine, or bring the network or tunnel back."),
    hint: !hasTerminalRoute
      ? "The app kept your route, but the live terminal is not ready yet."
      : connectionIssue?.hint ||
        "This terminal will reopen automatically once the daemon is reachable again.",
  };
}

export function sortTerminalAgents({
  agents,
  recentAgentOpens,
}: {
  agents: Agent[];
  recentAgentOpens: StoredRecentAgentOpens;
}) {
  return [...agents].sort((left, right) => {
    const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
    const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
    if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

    const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
    const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
    if (leftPriority !== rightPriority) return leftPriority - rightPriority;

    return (right.updated_at || 0) - (left.updated_at || 0);
  });
}

export function shouldShowPickerServerNames(agents: Agent[]) {
  return new Set(agents.map((agent) => agent.serverId)).size > 1;
}

export function buildMenuPosition(
  anchor: MenuAnchorLayout | null,
  windowWidth: number,
  popoverWidth: number,
): { left: number; top: number } {
  const top = Math.max(12, (anchor?.y ?? 12) + (anchor?.height ?? 38) + 16);
  const preferredLeft =
    (anchor?.x ?? windowWidth - 14) + (anchor?.width ?? 0) - popoverWidth;
  const maxLeft = Math.max(12, windowWidth - popoverWidth - 12);

  return {
    left: clamp(preferredLeft, 12, maxLeft),
    top,
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function getWorkStartedAt(item: WorkItem): number {
  const startedAt = Date.parse(
    item.frontmatter.started || item.frontmatter.created || "",
  );
  return Number.isNaN(startedAt) ? 0 : startedAt;
}
