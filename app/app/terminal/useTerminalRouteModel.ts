import { useMemo } from "react";
import {
  type AgentKind,
  presentAgent,
} from "../../services/agentPresentation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  StoredAgentAliases,
  StoredCodexRenderMode,
  StoredCodexRenderModes,
} from "../../services/storage";
import type {
  Agent,
  AgentCapabilities,
  ConnectionState,
} from "../../store/agents";
import type { WorkItem } from "../../store/work";
import { findLinkedWork } from "./TerminalScreenModel";
import type { TerminalRouteSessionHint } from "./useTerminalScreenLocalState";

interface UseTerminalRouteModelInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  routeSessionHint: TerminalRouteSessionHint;
  agentByKey: ReadonlyMap<string, Agent>;
  workByKey: Record<string, WorkItem>;
  agentAliases: StoredAgentAliases;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  codexRenderModes: StoredCodexRenderModes;
}

export function useTerminalRouteModel({
  serverId,
  agentId,
  sessionKey,
  routeSessionHint,
  agentByKey,
  workByKey,
  agentAliases,
  serverConnections,
  serverConnectionIssues,
  codexRenderModes,
}: UseTerminalRouteModelInput) {
  const storedAgent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const agent = useMemo(
    () =>
      resolveRouteAgent({
        storedAgent,
        routeSessionHint,
        sessionKey,
        serverId,
        agentId,
      }),
    [agentId, routeSessionHint, serverId, sessionKey, storedAgent],
  );
  const gitDiffCwd = typeof agent?.cwd === "string" ? agent.cwd.trim() : "";
  const presentedAgent = useMemo(
    () =>
      presentAgent(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? agentAliases[sessionKey] : undefined,
      ),
    [agent, agentAliases, sessionKey],
  );
  const linkedWork = useMemo(
    () => findLinkedWork(workByKey, serverId, agentId),
    [agentId, serverId, workByKey],
  );
  const linkedWorkTitle = linkedWork?.title?.trim() || "";
  const displayName =
    presentedAgent.titleSource === "default" && linkedWorkTitle
      ? linkedWorkTitle
      : presentedAgent.title;
  const connectionState = serverId
    ? serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && agentId);
  const isCodexAgent = presentedAgent.kind === "codex";
  const isGrokAgent = presentedAgent.kind === "grok";
  const isStructuredChatAgent = supportsChatInterface(
    presentedAgent.kind,
    agent?.capabilities,
  );
  const codexRenderMode = resolveCodexRenderMode({
    kind: presentedAgent.kind,
    capabilities: agent?.capabilities,
    sessionKey,
    storedModes: codexRenderModes,
  });
  const showCodexChat =
    hasTerminalRoute && isStructuredChatAgent && codexRenderMode === "chat";

  return {
    agent,
    codexRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    gitDiffCwd,
    hasTerminalRoute,
    isCodexAgent,
    isGrokAgent,
    isStructuredChatAgent,
    linkedWork,
    presentedAgent,
    showCodexChat,
  };
}

/** Agents with a structured chat surface (Codex-compatible conversation UI). */
export function supportsChatInterface(
  kind: AgentKind | string,
  capabilities?: AgentCapabilities,
): boolean {
  return (
    capabilities?.structured_events === true ||
    kind === "claude" ||
    kind === "codex" ||
    kind === "cursor" ||
    kind === "grok"
  );
}

/**
 * Resolve render mode from a persisted per-session preference when present;
 * otherwise derive from chat-interface capability (structured → chat, else terminal).
 */
export function resolveCodexRenderMode({
  kind,
  capabilities,
  sessionKey,
  storedModes,
}: {
  kind: AgentKind | string;
  capabilities?: AgentCapabilities;
  sessionKey: string | null;
  storedModes: StoredCodexRenderModes;
}): StoredCodexRenderMode {
  if (sessionKey) {
    const persisted = storedModes[sessionKey];
    if (persisted === "chat" || persisted === "terminal") {
      return persisted;
    }
  }
  return defaultCodexRenderModeForKind(kind, capabilities);
}

export function defaultCodexRenderModeForKind(
  kind: AgentKind | string,
  capabilities?: AgentCapabilities,
): StoredCodexRenderMode {
  return supportsChatInterface(kind, capabilities) ? "chat" : "terminal";
}

function resolveRouteAgent({
  storedAgent,
  routeSessionHint,
  sessionKey,
  serverId,
  agentId,
}: {
  storedAgent?: Agent;
  routeSessionHint: TerminalRouteSessionHint;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
}): Agent | undefined {
  if (storedAgent) {
    return {
      ...storedAgent,
      name: storedAgent.name || routeSessionHint.name || agentId,
      cwd: storedAgent.cwd || routeSessionHint.cwd,
      command: storedAgent.command || routeSessionHint.command,
      started_at: storedAgent.started_at ?? routeSessionHint.startedAt,
    };
  }

  if (!sessionKey || !serverId || !agentId || !hasRouteSessionHint(routeSessionHint)) {
    return undefined;
  }

  const now = Date.now();
  return {
    key: sessionKey,
    id: agentId,
    serverId,
    serverName: "",
    serverUrl: "",
    name: routeSessionHint.name || routeSessionHint.command || agentId,
    status: "running",
    project: undefined,
    cwd: routeSessionHint.cwd,
    command: routeSessionHint.command,
    summary: "",
    last_output_lines: [],
    started_at: routeSessionHint.startedAt,
    updated_at: routeSessionHint.startedAt || now,
  };
}

function hasRouteSessionHint(hint: TerminalRouteSessionHint): boolean {
  return Boolean(hint.name || hint.cwd || hint.command || hint.startedAt);
}
