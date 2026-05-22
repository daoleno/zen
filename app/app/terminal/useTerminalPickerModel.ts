import { useMemo } from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ServerLatencySample } from "../../services/serverLatency";
import {
  filterAgentsByPreferredServers,
  groupAgentsByDirectory,
} from "../../services/serverSelection";
import type {
  StoredRecentAgentOpens,
  StoredServer,
  StoredTerminalTabs,
} from "../../services/storage";
import {
  shouldShowPickerServerNames,
  sortTerminalAgents,
} from "./TerminalScreenModel";

interface UseTerminalPickerModelInput {
  agents: Agent[];
  servers: StoredServer[];
  connectionStates: Record<string, ConnectionState>;
  latencyById: Record<string, ServerLatencySample | undefined>;
  terminalTabs: StoredTerminalTabs;
  recentAgentOpens: StoredRecentAgentOpens;
}

export function useTerminalPickerModel({
  agents,
  servers,
  connectionStates,
  latencyById,
  terminalTabs,
  recentAgentOpens,
}: UseTerminalPickerModelInput) {
  const displayAgents = useMemo(
    () =>
      filterAgentsByPreferredServers({
        agents,
        servers,
        connectionStates,
        latencyById,
      }),
    [agents, connectionStates, latencyById, servers],
  );

  const sortedAgents = useMemo(
    () =>
      sortTerminalAgents({
        agents: displayAgents,
        terminalTabs,
        recentAgentOpens,
      }),
    [displayAgents, recentAgentOpens, terminalTabs],
  );

  const showPickerServerNames = useMemo(
    () => shouldShowPickerServerNames(sortedAgents),
    [sortedAgents],
  );
  const pickerSections = useMemo(
    () => groupAgentsByDirectory(sortedAgents, {
      showServerName: showPickerServerNames,
    }),
    [showPickerServerNames, sortedAgents],
  );

  return {
    pickerSections,
    showPickerServerNames,
    sortedAgents,
  };
}
