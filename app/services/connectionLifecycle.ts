import type { ConnectionState } from "../store/agents";

/** Why the client reported a disconnect. */
export type DisconnectReason = "intentional" | "transport_closed";

export type DisconnectLifecycleDecision = {
  /** Connection presentation while this disconnect is handled. */
  connectionState: ConnectionState;
  /** Whether Brain/Work (and similar) server caches should be wiped. */
  clearServerCaches: boolean;
};

export type ConnectedReadModelClient = {
  listWorkItems(serverId: string): void;
  listAgentSessions(serverId: string): void;
  requestBrainSnapshot(serverId: string): void;
  listCalendarItems(serverId: string): void;
};

/**
 * Reconnect rebuilds read models with fresh requests. Nothing sent while the
 * socket was unavailable is retained or replayed by the transport.
 */
export function createConnectedReadRefreshHandler(
  client: ConnectedReadModelClient,
) {
  return ({ serverId }: { serverId: string }) => {
    client.listWorkItems(serverId);
    client.listAgentSessions(serverId);
    client.requestBrainSnapshot(serverId);
    client.listCalendarItems(serverId);
  };
}

/**
 * App backgrounding and transient socket close are expected mobile lifecycle
 * events. Only an intentional disconnect (user disable/remove, tear-down)
 * should drop connected presentation and wipe cached server content.
 */
export function decideDisconnectLifecycle(
  reason: DisconnectReason | string | undefined | null,
): DisconnectLifecycleDecision {
  if (reason === "intentional") {
    return {
      connectionState: "offline",
      clearServerCaches: true,
    };
  }
  return {
    connectionState: "connecting",
    clearServerCaches: false,
  };
}

export function isIntentionalDisconnect(
  reason: DisconnectReason | string | undefined | null,
): boolean {
  return reason === "intentional";
}

/**
 * Prefer a hydrated Brain host while the socket is resuming so the timeline
 * stays mounted instead of falling back to an empty/offline card.
 */
export function resolveBrainActiveServerId<T extends { id: string }>({
  servers,
  connectedServerIds,
  brainHydratedByServer,
  connectionStates,
}: {
  servers: T[];
  connectedServerIds: ReadonlySet<string> | string[];
  brainHydratedByServer: Record<string, boolean | undefined>;
  connectionStates: Record<string, ConnectionState | undefined>;
}): string | null {
  const connectedSet =
    connectedServerIds instanceof Set
      ? connectedServerIds
      : new Set(connectedServerIds);

  const hydratedConnected = servers.find(
    (server) =>
      connectedSet.has(server.id) && brainHydratedByServer[server.id],
  );
  if (hydratedConnected) {
    return hydratedConnected.id;
  }

  const connected = servers.find((server) => connectedSet.has(server.id));
  if (connected) {
    return connected.id;
  }

  const hydratedResuming = servers.find(
    (server) => brainHydratedByServer[server.id],
  );
  if (hydratedResuming) {
    return hydratedResuming.id;
  }

  const connectedByState = servers.find(
    (server) => connectionStates[server.id] === "connected",
  );
  if (connectedByState) {
    return connectedByState.id;
  }

  return servers[0]?.id ?? null;
}

/**
 * Brain empty/offline card is only for cold start or intentional wipe.
 * Cached hydrated Brain stays visible while transport resumes.
 */
export function shouldShowBrainLoadingState({
  hydrated,
  hasHostAgent,
}: {
  hydrated: boolean;
  hasHostAgent: boolean;
}): boolean {
  return !(hydrated && hasHostAgent);
}
