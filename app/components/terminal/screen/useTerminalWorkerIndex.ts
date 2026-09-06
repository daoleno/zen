import { useMemo } from "react";
import type { Worker } from "../../../store/workers";

interface UseTerminalWorkerIndexInput {
  agents: Worker[];
  hydratedServers: Record<string, boolean>;
}

export function useTerminalWorkerIndex({
  agents,
  hydratedServers,
}: UseTerminalWorkerIndexInput) {
  const workerByKey = useMemo(
    () => new Map(agents.map((agent) => [agent.key, agent])),
    [agents],
  );
  const hydratedServerIds = useMemo(
    () =>
      Object.entries(hydratedServers)
        .filter(([, hydrated]) => hydrated)
        .map(([serverId]) => serverId),
    [hydratedServers],
  );
  const liveWorkerKeys = useMemo(
    () => agents.map((currentWorker) => currentWorker.key),
    [agents],
  );
  const hydratedServerIdSet = useMemo(
    () => new Set(hydratedServerIds),
    [hydratedServerIds],
  );

  return {
    workerByKey,
    hydratedServerIds,
    hydratedServerIdSet,
    liveWorkerKeys,
  };
}
