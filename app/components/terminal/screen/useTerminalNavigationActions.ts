import { useCallback } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import type { Worker, ConnectionState } from "../../../store/workers";
import { dismissTerminalToSessions } from "../../../services/terminalExitNavigation";
import { wsClient } from "../../../services/websocket";

interface UseTerminalNavigationActionsInput {
  sessionKey: string | null;
  serverId: string;
  workerId: string;
  connectionState: ConnectionState;
  displayName: string;
  workerServerName?: string;
  closeMenu(): void;
}

export function useTerminalNavigationActions({
  sessionKey,
  serverId,
  workerId,
  connectionState,
  displayName,
  workerServerName,
  closeMenu,
}: UseTerminalNavigationActionsInput) {
  const router = useRouter();

  const goToInbox = useCallback(() => {
    closeMenu();
    dismissTerminalToSessions(router);
  }, [closeMenu, router]);

  const performTerminateWorker = useCallback(async () => {
    if (!sessionKey || !serverId || !workerId) return;

    wsClient.killWorker(serverId, workerId);
    dismissTerminalToSessions(router);
  }, [workerId, router, serverId, sessionKey]);

  const handleTerminateWorker = useCallback(() => {
    if (!sessionKey || !serverId || !workerId) return;

    closeMenu();

    if (connectionState !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before terminating the agent.",
      );
      return;
    }

    Alert.alert(
      "Terminate?",
      "This will terminate " +
        (displayName || workerId) +
        " on " +
        (workerServerName || serverId) +
        ".",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => {
            void performTerminateWorker();
          },
        },
      ],
    );
  }, [
    workerId,
    workerServerName,
    closeMenu,
    connectionState,
    displayName,
    performTerminateWorker,
    serverId,
    sessionKey,
  ]);

  return {
    goToInbox,
    handleTerminateWorker,
  };
}
