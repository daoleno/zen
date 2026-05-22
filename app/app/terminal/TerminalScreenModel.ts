import type { Agent } from "../../store/agents";
import type { AgentStatus } from "../../constants/tokens";
import type {
  StoredAgentAliases,
  StoredRecentAgentOpens,
  StoredTerminalTabs,
} from "../../services/storage";
import { parseSessionKey } from "../../services/sessionKeys";
import { presentAgent } from "../../services/agentPresentation";
import type { TerminalTabDescriptor } from "../../components/terminal/TerminalTopBar";

export const EMPTY_TABS: StoredTerminalTabs = { order: [], pinned: [] };

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

export function buildTerminalTabs({
  sessionKey,
  terminalTabs,
  agentByKey,
  hydratedServerIdSet,
  agentAliases,
}: {
  sessionKey: string | null;
  terminalTabs: StoredTerminalTabs;
  agentByKey: ReadonlyMap<string, Agent>;
  hydratedServerIdSet: ReadonlySet<string>;
  agentAliases: StoredAgentAliases;
}): TerminalTabDescriptor[] {
  const order = buildDisplayTabOrder(sessionKey, terminalTabs);
  return order
    .filter((currentSessionKey) => {
      if (currentSessionKey === sessionKey) return true;
      if (agentByKey.has(currentSessionKey)) return true;

      const parsed = parseSessionKey(currentSessionKey);
      return parsed ? !hydratedServerIdSet.has(parsed.serverId) : false;
    })
    .map((currentSessionKey) => {
      const tabAgent = agentByKey.get(currentSessionKey);
      const parsed = parseSessionKey(currentSessionKey);
      const presented = presentAgent(
        tabAgent || {
          name: parsed?.agentId || "",
          summary: "",
          last_output_lines: [],
        },
        currentSessionKey ? agentAliases[currentSessionKey] : undefined,
      );
      return {
        id: currentSessionKey,
        name: presented.cwdBase || presented.shortTitle,
        status: tabAgent?.status || "unknown",
        kind: presented.kind,
        pinned: terminalTabs.pinned.includes(currentSessionKey),
        active: currentSessionKey === sessionKey,
      } satisfies TerminalTabDescriptor;
    });
}

export function sortTerminalAgents({
  agents,
  terminalTabs,
  recentAgentOpens,
}: {
  agents: Agent[];
  terminalTabs: StoredTerminalTabs;
  recentAgentOpens: StoredRecentAgentOpens;
}) {
  const openTabs = new Set(terminalTabs.order);
  const pinnedTabs = new Set(terminalTabs.pinned);

  return [...agents].sort((left, right) => {
    const leftPinned = pinnedTabs.has(left.key) ? 0 : 1;
    const rightPinned = pinnedTabs.has(right.key) ? 0 : 1;
    if (leftPinned !== rightPinned) return leftPinned - rightPinned;

    const leftOpen = openTabs.has(left.key) ? 0 : 1;
    const rightOpen = openTabs.has(right.key) ? 0 : 1;
    if (leftOpen !== rightOpen) return leftOpen - rightOpen;

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

export function pickNextTabAfterClose(
  closedId: string,
  currentTabs: StoredTerminalTabs,
  nextTabs: StoredTerminalTabs,
): string | null {
  const currentOrder = buildDisplayTabOrder(null, currentTabs);
  const nextOrder = buildDisplayTabOrder(null, nextTabs);
  const closedIndex = currentOrder.indexOf(closedId);

  if (closedIndex === -1) return nextOrder[0] || null;

  return currentOrder[closedIndex + 1] || currentOrder[closedIndex - 1] || null;
}

function buildDisplayTabOrder(
  currentId: string | null | undefined,
  tabs: StoredTerminalTabs,
): string[] {
  if (!currentId) return tabs.order;
  return tabs.order.includes(currentId)
    ? tabs.order
    : [...tabs.order, currentId];
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}
