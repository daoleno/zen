/**
 * Transport-owned freshness for live interaction prompts.
 * A full `worker_session_list` (UPSERT_SERVER_WORKERS) must arrive while the
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

export function stampWorkerSessionListGeneration(input: {
  connectionState: TransportConnectionState | undefined;
  connectionGeneration: number;
  workerSessionListGenerationByServer: Record<string, number>;
  serverId: string;
}): Record<string, number> {
  if (input.connectionState !== "connected" || input.connectionGeneration <= 0) {
    return input.workerSessionListGenerationByServer;
  }
  if (
    input.workerSessionListGenerationByServer[input.serverId] ===
    input.connectionGeneration
  ) {
    return input.workerSessionListGenerationByServer;
  }
  return {
    ...input.workerSessionListGenerationByServer,
    [input.serverId]: input.connectionGeneration,
  };
}

export function isWorkerSessionListFreshForConnection(input: {
  connectionState: TransportConnectionState | undefined;
  connectionGeneration: number;
  workerSessionListGeneration: number;
}): boolean {
  return (
    input.connectionState === "connected" &&
    input.connectionGeneration > 0 &&
    input.workerSessionListGeneration === input.connectionGeneration
  );
}

export function liveActionPromptScopeKey(input: {
  workerId: string;
  processId?: number;
  startedAt?: number;
  connectionGeneration: number;
}): string {
  return [
    input.workerId,
    input.processId ?? "",
    input.startedAt ?? "",
    input.connectionGeneration,
  ].join(":");
}
