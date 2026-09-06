import type { AppStateStatus } from "react-native";

interface TerminalPresenceFacts {
  serverId: string;
  workerId: string;
  sessionKey: string | null;
  appState: AppStateStatus;
  focused: boolean;
}

export function currentTerminalPresence({
  serverId,
  workerId,
  sessionKey,
  appState,
  focused,
}: TerminalPresenceFacts): { serverId: string; workerId: string } | null {
  if (
    !focused ||
    appState !== "active" ||
    !sessionKey ||
    !serverId ||
    !workerId
  ) {
    return null;
  }
  return { serverId, workerId };
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
