import { useMemo } from "react";
import { type AgentKind, presentWorker } from "../../../services/workerPresentation";
import type { ConnectionIssue } from "../../../services/connectionIssue";
import type {
  StoredWorkerAliases,
  StoredInterfaceRenderMode,
  StoredInterfaceRenderModes,
} from "../../../services/storage";
import type {
  Worker,
  WorkerCapabilities,
  ConnectionState,
} from "../../../store/workers";
import type { BrainWorkerRef } from "../../../store/brain";
import type { WorkItem } from "../../../store/work";
import {
  sessionAllowsModelProfileActivation,
} from "../../../services/providers/sessionCapabilities";
import { findLinkedWork } from "./TerminalScreenModel";
import type { TerminalRouteSessionHint } from "./useTerminalScreenLocalState";

interface UseTerminalRouteModelInput {
  serverId: string;
  workerId: string;
  sessionKey: string | null;
  routeSessionHint: TerminalRouteSessionHint;
  workerByKey: ReadonlyMap<string, Worker>;
  workByKey: Record<string, WorkItem>;
  workerAliases: StoredWorkerAliases;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  interfaceRenderModes: StoredInterfaceRenderModes;
  /** Current-server Brain host_worker (hidden from worker_session_list). */
  brainHostWorker?: BrainWorkerRef | null;
  /** serverId that owns brainHostWorker — must match route serverId. */
  brainHostServerId?: string | null;
}

export function useTerminalRouteModel({
  serverId,
  workerId,
  sessionKey,
  routeSessionHint,
  workerByKey,
  workByKey,
  workerAliases,
  serverConnections,
  serverConnectionIssues,
  interfaceRenderModes,
  brainHostWorker,
  brainHostServerId,
}: UseTerminalRouteModelInput) {
  const storedWorker = sessionKey ? workerByKey.get(sessionKey) : undefined;
  const agent = useMemo(
    () =>
      resolveTerminalRouteWorker({
        storedWorker,
        routeSessionHint,
        sessionKey,
        serverId,
        workerId,
        brainHostWorker,
        brainHostServerId,
      }),
    [
      workerId,
      brainHostWorker,
      brainHostServerId,
      routeSessionHint,
      serverId,
      sessionKey,
      storedWorker,
    ],
  );
  const gitDiffCwd = typeof agent?.cwd === "string" ? agent.cwd.trim() : "";
  const presentedWorker = useMemo(
    () =>
      presentWorker(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? workerAliases[sessionKey] : undefined,
      ),
    [agent, workerAliases, sessionKey],
  );
  const linkedWork = useMemo(
    () => findLinkedWork(workByKey, serverId, workerId),
    [workerId, serverId, workByKey],
  );
  const linkedWorkTitle = linkedWork?.title?.trim() || "";
  const displayName =
    presentedWorker.titleSource === "default" && linkedWorkTitle
      ? linkedWorkTitle
      : presentedWorker.title;
  const connectionState = serverId
    ? serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && workerId);
  const isCodexWorker = presentedWorker.kind === "codex";
  const isGrokWorker = presentedWorker.kind === "grok";
  const isStructuredChatWorker = supportsChatInterface(
    presentedWorker.kind,
    agent?.capabilities,
  );
  const interfaceRenderMode = resolveInterfaceRenderMode({
    kind: presentedWorker.kind,
    capabilities: agent?.capabilities,
    sessionKey,
    storedModes: interfaceRenderModes,
  });
  const showInterfaceChat =
    hasTerminalRoute && isStructuredChatWorker && interfaceRenderMode === "chat";

  return {
    agent,
    interfaceRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    gitDiffCwd,
    hasTerminalRoute,
    isCodexWorker,
    isGrokWorker,
    isStructuredChatWorker,
    linkedWork,
    presentedWorker,
    showInterfaceChat,
  };
}

/** Agents with a structured chat surface (provider-neutral conversation UI). */
export function supportsChatInterface(
  kind: AgentKind | string,
  capabilities?: WorkerCapabilities,
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
  capabilities?: WorkerCapabilities;
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
  capabilities?: WorkerCapabilities,
): StoredInterfaceRenderMode {
  return supportsChatInterface(kind, capabilities) ? "chat" : "terminal";
}

/**
 * True when the route targets the current-server Brain host by exact server+id.
 * Never matches on name, command, or route-param inference.
 */
export function brainHostMatchesRoute(input: {
  brainHostWorker?: BrainWorkerRef | null;
  brainHostServerId?: string | null;
  routeServerId: string;
  routeWorkerId: string;
}): boolean {
  const hostId = input.brainHostWorker?.id?.trim() || "";
  const brainServer = input.brainHostServerId?.trim() || "";
  const routeServer = input.routeServerId.trim();
  const routeWorker = input.routeWorkerId.trim();
  if (!hostId || !brainServer || !routeServer || !routeWorker) {
    return false;
  }
  return brainServer === routeServer && hostId === routeWorker;
}

/**
 * Resolve the Terminal route Agent. When the route targets the current-server
 * Brain host (hidden from worker_session_list), merge host_worker — including
 * daemon-authoritative capabilities — without upserting into the Agent store.
 */
export function resolveTerminalRouteWorker({
  storedWorker,
  routeSessionHint,
  sessionKey,
  serverId,
  workerId,
  brainHostWorker,
  brainHostServerId,
}: {
  storedWorker?: Worker;
  routeSessionHint: TerminalRouteSessionHint;
  sessionKey: string | null;
  serverId: string;
  workerId: string;
  brainHostWorker?: BrainWorkerRef | null;
  brainHostServerId?: string | null;
}): Worker | undefined {
  const hostMatches = brainHostMatchesRoute({
    brainHostWorker,
    brainHostServerId,
    routeServerId: serverId,
    routeWorkerId: workerId,
  });
  const hostCapabilities = hostMatches
    ? brainHostWorker?.capabilities
    : undefined;

  if (storedWorker) {
    // Ordinary visible Agent: unchanged identity. Overlay host capabilities only
    // when this exact server+id is the Brain host (rare overlap; never invent).
    return {
      ...storedWorker,
      name: storedWorker.name || routeSessionHint.name || workerId,
      cwd: storedWorker.cwd || routeSessionHint.cwd,
      command: storedWorker.command || routeSessionHint.command,
      started_at: storedWorker.started_at ?? routeSessionHint.startedAt,
      capabilities: hostCapabilities ?? storedWorker.capabilities,
    };
  }

  if (hostMatches && sessionKey && brainHostWorker) {
    // Hidden Brain host: project host_worker as the route Agent for this screen.
    // Capabilities come only from host_worker — route params never authorize.
    const now = Date.now();
    return {
      key: sessionKey,
      id: brainHostWorker.id,
      serverId,
      serverName: "",
      serverUrl: "",
      name: brainHostWorker.name || workerId,
      status: (brainHostWorker.status as Worker["status"]) || "running",
      project: undefined,
      cwd: brainHostWorker.cwd || routeSessionHint.cwd,
      command: brainHostWorker.command || routeSessionHint.command,
      summary: brainHostWorker.summary || "",
      last_output_lines: [],
      started_at: brainHostWorker.started_at ?? routeSessionHint.startedAt,
      updated_at: brainHostWorker.started_at || now,
      process_id: brainHostWorker.process_id,
      delegated: brainHostWorker.delegated,
      capabilities: hostCapabilities,
    };
  }

  if (
    !sessionKey ||
    !serverId ||
    !workerId ||
    !hasRouteSessionHint(routeSessionHint)
  ) {
    return undefined;
  }

  // Route-hint fallback for non-host Sessions without a store row — no capabilities.
  const now = Date.now();
  return {
    key: sessionKey,
    id: workerId,
    serverId,
    serverName: "",
    serverUrl: "",
    name: routeSessionHint.name || routeSessionHint.command || workerId,
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

/**
 * Model menu visibility follows the same truth as the Composer control: the
 * action exists only when this Session's capability advertises the
 * daemon-acknowledged live switch. Managed read-only and unmanaged Sessions
 * keep the action hidden — never a dead control.
 */
export function routeWorkerProviderModelActionState(
  capabilities: WorkerCapabilities | null | undefined,
): {
  actionVisible: boolean;
  activationEnabled: boolean;
} {
  const activeSwitch = sessionAllowsModelProfileActivation(capabilities);
  return {
    actionVisible: activeSwitch,
    activationEnabled: activeSwitch,
  };
}

function hasRouteSessionHint(hint: TerminalRouteSessionHint): boolean {
  return Boolean(hint.name || hint.cwd || hint.command || hint.startedAt);
}
