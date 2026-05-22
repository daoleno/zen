import { useMemo } from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ServerLatencySample } from "../../services/serverLatency";
import type {
  StoredAgentAliases,
  StoredRecentAgentOpens,
  StoredServer,
  StoredTerminalTabs,
} from "../../services/storage";
import { buildTerminalTabs } from "./TerminalScreenModel";
import { useTerminalPickerModel } from "./useTerminalPickerModel";

interface UseTerminalDirectoryModelInput {
  sessionKey: string | null;
  agents: Agent[];
  servers: StoredServer[];
  connectionStates: Record<string, ConnectionState>;
  latencyById: Record<string, ServerLatencySample | undefined>;
  terminalTabs: StoredTerminalTabs;
  recentAgentOpens: StoredRecentAgentOpens;
  agentByKey: ReadonlyMap<string, Agent>;
  hydratedServerIdSet: ReadonlySet<string>;
  agentAliases: StoredAgentAliases;
}

export function useTerminalDirectoryModel({
  sessionKey,
  agents,
  servers,
  connectionStates,
  latencyById,
  terminalTabs,
  recentAgentOpens,
  agentByKey,
  hydratedServerIdSet,
  agentAliases,
}: UseTerminalDirectoryModelInput) {
  const tabs = useMemo(() => {
    return buildTerminalTabs({
      sessionKey,
      terminalTabs,
      agentByKey,
      hydratedServerIdSet,
      agentAliases,
    });
  }, [agentAliases, agentByKey, hydratedServerIdSet, sessionKey, terminalTabs]);

  const { pickerSections, showPickerServerNames, sortedAgents } =
    useTerminalPickerModel({
      agents,
      servers,
      connectionStates,
      latencyById,
      terminalTabs,
      recentAgentOpens,
    });

  return {
    pickerSections,
    showPickerServerNames,
    sortedAgents,
    tabs,
  };
}
