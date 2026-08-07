import { useMemo } from "react";
import { type AgentKind, presentAgent } from "../../../services/agentPresentation";
import type { ConnectionIssue } from "../../../services/connectionIssue";
import type {
  StoredAgentAliases,
  StoredInterfaceRenderMode,
  StoredInterfaceRenderModes,
} from "../../../services/storage";
import type {
  Agent,
  AgentCapabilities,
  ConnectionState,
} from "../../../store/agents";
import type { BrainAgentRef } from "../../../store/brain";
import type { WorkItem } from "../../../store/work";
import {
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
} from "../../../services/providers/sessionCapabilities";
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
  interfaceRenderModes: StoredInterfaceRenderModes;
  /** Current-server Brain host_agent (hidden from agent_session_list). */
  brainHostAgent?: BrainAgentRef | null;
  /** serverId that owns brainHostAgent — must match route serverId. */
  brainHostServerId?: string | null;
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
  interfaceRenderModes,
  brainHostAgent,
  brainHostServerId,
}: UseTerminalRouteModelInput) {
  const storedAgent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const agent = useMemo(
    () =>
      resolveTerminalRouteAgent({
        storedAgent,
        routeSessionHint,
        sessionKey,
        serverId,
        agentId,
        brainHostAgent,
        brainHostServerId,
      }),
    [
      agentId,
      brainHostAgent,
      brainHostServerId,
      routeSessionHint,
      serverId,
      sessionKey,
      storedAgent,
    ],
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
  const interfaceRenderMode = resolveInterfaceRenderMode({
    kind: presentedAgent.kind,
    capabilities: agent?.capabilities,
    sessionKey,
    storedModes: interfaceRenderModes,
  });
  const showInterfaceChat =
    hasTerminalRoute && isStructuredChatAgent && interfaceRenderMode === "chat";

  return {
    agent,
    interfaceRenderMode,
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
    showInterfaceChat,
  };
}

/** Agents with a structured chat surface (provider-neutral conversation UI). */
export function supportsChatInterface(
  kind: AgentKind | string,
  capabilities?: AgentCapabilities,
): boolean {
  return (
    capabilities?.structured_events === true ||
    kind === "claude" ||
    kind === "codex" ||
    kind === "cursor" ||
    kind === "grok" ||
    kind === "pi" ||
    kind === "opencode"
  );
}

/**
 * Resolve render mode from a persisted per-session preference when present;
 * otherwise derive from chat-interface capability (structured → chat, else terminal).
 */
export function resolveInterfaceRenderMode({
  kind,
  capabilities,
  sessionKey,
  storedModes,
}: {
  kind: AgentKind | string;
  capabilities?: AgentCapabilities;
  sessionKey: string | null;
  storedModes: StoredInterfaceRenderModes;
}): StoredInterfaceRenderMode {
  if (sessionKey) {
    const persisted = storedModes[sessionKey];
    if (persisted === "chat" || persisted === "terminal") {
      return persisted;
    }
  }
  return defaultInterfaceRenderModeForKind(kind, capabilities);
}

export function defaultInterfaceRenderModeForKind(
  kind: AgentKind | string,
  capabilities?: AgentCapabilities,
): StoredInterfaceRenderMode {
  return supportsChatInterface(kind, capabilities) ? "chat" : "terminal";
}

/**
 * True when the route targets the current-server Brain host by exact server+id.
 * Never matches on name, command, or route-param inference.
 */
export function brainHostMatchesRoute(input: {
  brainHostAgent?: BrainAgentRef | null;
  brainHostServerId?: string | null;
  routeServerId: string;
  routeAgentId: string;
}): boolean {
  const hostId = input.brainHostAgent?.id?.trim() || "";
  const brainServer = input.brainHostServerId?.trim() || "";
  const routeServer = input.routeServerId.trim();
  const routeAgent = input.routeAgentId.trim();
  if (!hostId || !brainServer || !routeServer || !routeAgent) {
    return false;
  }
  return brainServer === routeServer && hostId === routeAgent;
}

/**
 * Resolve the Terminal route Agent. When the route targets the current-server
 * Brain host (hidden from agent_session_list), merge host_agent — including
 * daemon-authoritative capabilities — without upserting into the Agent store.
 */
export function resolveTerminalRouteAgent({
  storedAgent,
  routeSessionHint,
  sessionKey,
  serverId,
  agentId,
  brainHostAgent,
  brainHostServerId,
}: {
  storedAgent?: Agent;
  routeSessionHint: TerminalRouteSessionHint;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  brainHostAgent?: BrainAgentRef | null;
  brainHostServerId?: string | null;
}): Agent | undefined {
  const hostMatches = brainHostMatchesRoute({
    brainHostAgent,
    brainHostServerId,
    routeServerId: serverId,
    routeAgentId: agentId,
  });
  const hostCapabilities = hostMatches
    ? brainHostAgent?.capabilities
    : undefined;

  if (storedAgent) {
    // Ordinary visible Agent: unchanged identity. Overlay host capabilities only
    // when this exact server+id is the Brain host (rare overlap; never invent).
    return {
      ...storedAgent,
      name: storedAgent.name || routeSessionHint.name || agentId,
      cwd: storedAgent.cwd || routeSessionHint.cwd,
      command: storedAgent.command || routeSessionHint.command,
      started_at: storedAgent.started_at ?? routeSessionHint.startedAt,
      capabilities: hostCapabilities ?? storedAgent.capabilities,
    };
  }

  if (hostMatches && sessionKey && brainHostAgent) {
    // Hidden Brain host: project host_agent as the route Agent for this screen.
    // Capabilities come only from host_agent — route params never authorize.
    const now = Date.now();
    return {
      key: sessionKey,
      id: brainHostAgent.id,
      serverId,
      serverName: "",
      serverUrl: "",
      name: brainHostAgent.name || agentId,
      status: (brainHostAgent.status as Agent["status"]) || "running",
      project: undefined,
      cwd: brainHostAgent.cwd || routeSessionHint.cwd,
      command: brainHostAgent.command || routeSessionHint.command,
      summary: brainHostAgent.summary || "",
      last_output_lines: [],
      started_at: brainHostAgent.started_at ?? routeSessionHint.startedAt,
      updated_at: brainHostAgent.started_at || now,
      process_id: brainHostAgent.process_id,
      delegated: brainHostAgent.delegated,
      capabilities: hostCapabilities,
    };
  }

  if (
    !sessionKey ||
    !serverId ||
    !agentId ||
    !hasRouteSessionHint(routeSessionHint)
  ) {
    return undefined;
  }

  // Route-hint fallback for non-host Sessions without a store row — no capabilities.
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

/** Model menu visibility derived from resolved route agent capabilities. */
export function routeAgentProviderModelActionState(
  capabilities: AgentCapabilities | null | undefined,
): {
  actionVisible: boolean;
  activationEnabled: boolean;
  managedReadOnly: boolean;
} {
  return {
    actionVisible: sessionSupportsModelProfileAction(capabilities),
    activationEnabled: sessionAllowsModelProfileActivation(capabilities),
    managedReadOnly: sessionIsManagedReadOnlyProfile(capabilities),
  };
}

function hasRouteSessionHint(hint: TerminalRouteSessionHint): boolean {
  return Boolean(hint.name || hint.cwd || hint.command || hint.startedAt);
}
