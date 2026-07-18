/**
 * Transport-owned freshness for live interaction prompts.
 * A full `agent_session_list` (UPSERT_SERVER_AGENTS) must arrive while the
 * server is in the current WebSocket `connected` generation. Watcher
 * `updated_at` ticks are not proof of App receipt.
 */

export type TransportConnectionState = "offline" | "connecting" | "connected";

export function bumpServerConnectionGeneration(
  connectionGenerationByServer: Record<string, number>,
  serverId: string,
  previousState: TransportConnectionState | undefined,
  nextState: TransportConnectionState,
): Record<string, number> {
  if (nextState !== "connected" || previousState === "connected") {
    return connectionGenerationByServer;
  }
  return {
    ...connectionGenerationByServer,
    [serverId]: (connectionGenerationByServer[serverId] ?? 0) + 1,
  };
}

export function stampAgentSessionListGeneration(input: {
  connectionState: TransportConnectionState | undefined;
  connectionGeneration: number;
  agentSessionListGenerationByServer: Record<string, number>;
  serverId: string;
}): Record<string, number> {
  if (input.connectionState !== "connected" || input.connectionGeneration <= 0) {
    return input.agentSessionListGenerationByServer;
  }
  if (
    input.agentSessionListGenerationByServer[input.serverId] ===
    input.connectionGeneration
  ) {
    return input.agentSessionListGenerationByServer;
  }
  return {
    ...input.agentSessionListGenerationByServer,
    [input.serverId]: input.connectionGeneration,
  };
}

export function isAgentSessionListFreshForConnection(input: {
  connectionState: TransportConnectionState | undefined;
  connectionGeneration: number;
  agentSessionListGeneration: number;
}): boolean {
  return (
    input.connectionState === "connected" &&
    input.connectionGeneration > 0 &&
    input.agentSessionListGeneration === input.connectionGeneration
  );
}

export function liveActionPromptScopeKey(input: {
  agentId: string;
  processId?: number;
  startedAt?: number;
  connectionGeneration: number;
}): string {
  return [
    input.agentId,
    input.processId ?? "",
    input.startedAt ?? "",
    input.connectionGeneration,
  ].join(":");
}
