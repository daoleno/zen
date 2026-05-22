import { useMemo } from "react";
import type { Agent } from "../../store/agents";

interface UseTerminalAgentIndexInput {
  agents: Agent[];
  hydratedServers: Record<string, boolean>;
}

export function useTerminalAgentIndex({
  agents,
  hydratedServers,
}: UseTerminalAgentIndexInput) {
  const agentByKey = useMemo(
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
  const liveAgentKeys = useMemo(
    () => agents.map((currentAgent) => currentAgent.key),
    [agents],
  );
  const hydratedServerIdSet = useMemo(
    () => new Set(hydratedServerIds),
    [hydratedServerIds],
  );

  return {
    agentByKey,
    hydratedServerIds,
    hydratedServerIdSet,
    liveAgentKeys,
  };
}
