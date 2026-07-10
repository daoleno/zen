import { useCallback } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import type { Agent, ConnectionState } from "../../store/agents";
import { parseSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";

interface UseTerminalNavigationActionsInput {
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  agentByKey: ReadonlyMap<string, Agent>;
  connectionState: ConnectionState;
  displayName: string;
  agentServerName?: string;
  closeMenu(): void;
  closePicker(): void;
}

export function useTerminalNavigationActions({
  sessionKey,
  serverId,
  agentId,
  agentByKey,
  connectionState,
  displayName,
  agentServerName,
  closeMenu,
  closePicker,
}: UseTerminalNavigationActionsInput) {
  const router = useRouter();

  const openAgentSession = useCallback(
    async (nextSessionKey: string) => {
      closePicker();
      closeMenu();

      if (!nextSessionKey || nextSessionKey === sessionKey) return;
      const parsed = parseSessionKey(nextSessionKey);
      if (!parsed) return;
      if (!agentByKey.has(nextSessionKey)) return;

      router.replace({
        pathname: "/terminal/[id]",
        params: { id: parsed.agentId, serverId: parsed.serverId },
      });
    },
    [
      agentByKey,
      closeMenu,
      closePicker,
      router,
      sessionKey,
    ],
  );

  const goToInbox = useCallback(() => {
    closePicker();
    closeMenu();
    router.replace("/list");
  }, [closeMenu, closePicker, router]);

  const performTerminateAgent = useCallback(async () => {
    if (!sessionKey || !serverId || !agentId) return;

    wsClient.killAgent(serverId, agentId);
    router.replace("/list");
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
    openAgentSession,
    handleTerminateAgent,
  };
}
