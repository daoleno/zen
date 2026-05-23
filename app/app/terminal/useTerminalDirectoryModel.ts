import type { Agent, ConnectionState } from "../../store/agents";
import type { ServerLatencySample } from "../../services/serverLatency";
import type {
  StoredRecentAgentOpens,
  StoredServer,
} from "../../services/storage";
import { useTerminalPickerModel } from "./useTerminalPickerModel";

interface UseTerminalDirectoryModelInput {
  agents: Agent[];
  servers: StoredServer[];
  connectionStates: Record<string, ConnectionState>;
  latencyById: Record<string, ServerLatencySample | undefined>;
  recentAgentOpens: StoredRecentAgentOpens;
}

export function useTerminalDirectoryModel({
  agents,
  servers,
  connectionStates,
  latencyById,
  recentAgentOpens,
}: UseTerminalDirectoryModelInput) {
  const { pickerSections, showPickerServerNames, sortedAgents } =
    useTerminalPickerModel({
      agents,
      servers,
      connectionStates,
      latencyById,
      recentAgentOpens,
    });

  return {
    pickerSections,
    showPickerServerNames,
    sortedAgents,
  };
}
