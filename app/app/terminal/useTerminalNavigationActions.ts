import { useCallback } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import type { Agent, ConnectionState } from "../../store/agents";
import { dismissTerminalToSessions } from "../../services/terminalExitNavigation";
import { wsClient } from "../../services/websocket";

interface UseTerminalNavigationActionsInput {
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  connectionState: ConnectionState;
  displayName: string;
  agentServerName?: string;
  closeMenu(): void;
}

export function useTerminalNavigationActions({
  sessionKey,
  serverId,
  agentId,
  connectionState,
  displayName,
  agentServerName,
  closeMenu,
}: UseTerminalNavigationActionsInput) {
  const router = useRouter();

  const goToInbox = useCallback(() => {
    closeMenu();
    dismissTerminalToSessions(router);
  }, [closeMenu, router]);

  const performTerminateAgent = useCallback(async () => {
    if (!sessionKey || !serverId || !agentId) return;

    wsClient.killAgent(serverId, agentId);
    dismissTerminalToSessions(router);
  }, [agentId, router, serverId, sessionKey]);

  const handleTerminateAgent = useCallback(() => {
    if (!sessionKey || !serverId || !agentId) return;

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
        (displayName || agentId) +
        " on " +
        (agentServerName || serverId) +
        ".",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => {
            void performTerminateAgent();
          },
        },
      ],
    );
  }, [
    agentId,
    agentServerName,
    closeMenu,
    connectionState,
    displayName,
    performTerminateAgent,
    serverId,
    sessionKey,
  ]);

  return {
    goToInbox,
    handleTerminateAgent,
  };
}
