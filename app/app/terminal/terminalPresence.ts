import type { AppStateStatus } from "react-native";

interface TerminalPresenceFacts {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  appState: AppStateStatus;
  focused: boolean;
}

export function currentTerminalPresence({
  serverId,
  agentId,
  sessionKey,
  appState,
  focused,
}: TerminalPresenceFacts): { serverId: string; agentId: string } | null {
  if (
    !focused ||
    appState !== "active" ||
    !sessionKey ||
    !serverId ||
    !agentId
  ) {
    return null;
  }
  return { serverId, agentId };
}

export function createTerminalConnectedPresenceHandler(
  serverId: string,
  declareCurrentPresence: () => void,
): (event: unknown) => void {
  return (event) => {
    if (
      !event ||
      typeof event !== "object" ||
      (event as { serverId?: unknown }).serverId !== serverId
    ) {
      return;
    }
    declareCurrentPresence();
  };
}
