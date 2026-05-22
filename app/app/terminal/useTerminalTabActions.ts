import { useCallback } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import type { Agent, ConnectionState } from "../../store/agents";
import {
  closeOtherTerminalTabs,
  closeTerminalTab,
  setTerminalTabPinned,
  type StoredTerminalTabs,
} from "../../services/storage";
import { parseSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";
import { pickNextTabAfterClose } from "./TerminalScreenModel";

interface UseTerminalTabActionsInput {
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  activePinned: boolean;
  terminalTabs: StoredTerminalTabs;
  agentByKey: ReadonlyMap<string, Agent>;
  hydratedServerIdSet: ReadonlySet<string>;
  connectionState: ConnectionState;
  displayName: string;
  agentServerName?: string;
  setTerminalTabs(tabs: StoredTerminalTabs): void;
  closeMenu(): void;
  closePicker(): void;
}

export function useTerminalTabActions({
  sessionKey,
  serverId,
  agentId,
  activePinned,
  terminalTabs,
  agentByKey,
  hydratedServerIdSet,
  connectionState,
  displayName,
  agentServerName,
  setTerminalTabs,
  closeMenu,
  closePicker,
}: UseTerminalTabActionsInput) {
  const router = useRouter();

  const openAgentTab = useCallback(
    async (nextSessionKey: string) => {
      closePicker();
      closeMenu();

      if (!nextSessionKey || nextSessionKey === sessionKey) return;
      const parsed = parseSessionKey(nextSessionKey);
      if (!parsed) return;
      if (!agentByKey.has(nextSessionKey) && hydratedServerIdSet.has(parsed.serverId)) {
        const nextTabs = await closeTerminalTab(nextSessionKey);
        setTerminalTabs(nextTabs);
        return;
      }

      router.replace({
        pathname: "/terminal/[id]",
        params: { id: parsed.agentId, serverId: parsed.serverId },
      });
    },
    [
      agentByKey,
      closeMenu,
      closePicker,
      hydratedServerIdSet,
      router,
      sessionKey,
      setTerminalTabs,
    ],
  );

  const goToInbox = useCallback(() => {
    closePicker();
    closeMenu();
    router.replace("/");
  }, [closeMenu, closePicker, router]);

  const handleTogglePinned = useCallback(async () => {
    if (!sessionKey) return;
    const nextTabs = await setTerminalTabPinned(sessionKey, !activePinned);
    setTerminalTabs(nextTabs);
    closeMenu();
  }, [activePinned, closeMenu, sessionKey, setTerminalTabs]);

  const handleCloseCurrentTab = useCallback(async () => {
    if (!sessionKey) return;

    const nextTabs = await closeTerminalTab(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();

    const nextSessionKey = pickNextTabAfterClose(
      sessionKey,
      terminalTabs,
      nextTabs,
    );
    if (nextSessionKey) {
      const parsed = parseSessionKey(nextSessionKey);
      if (parsed) {
        router.replace({
          pathname: "/terminal/[id]",
          params: { id: parsed.agentId, serverId: parsed.serverId },
        });
        return;
      }
      return;
    }

    router.replace("/");
  }, [closeMenu, router, sessionKey, setTerminalTabs, terminalTabs]);

  const handleCloseOtherTabs = useCallback(async () => {
    if (!sessionKey) return;

    const nextTabs = await closeOtherTerminalTabs(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();
  }, [closeMenu, sessionKey, setTerminalTabs]);

  const performTerminateAgent = useCallback(async () => {
    if (!sessionKey || !serverId || !agentId) return;

    const currentTabs = terminalTabs;
    const nextTabs = await closeTerminalTab(sessionKey);
    setTerminalTabs(nextTabs);
    wsClient.killAgent(serverId, agentId);

    const nextSessionKey = pickNextTabAfterClose(
      sessionKey,
      currentTabs,
      nextTabs,
    );
    if (nextSessionKey) {
      const parsed = parseSessionKey(nextSessionKey);
      if (parsed) {
        router.replace({
          pathname: "/terminal/[id]",
          params: { id: parsed.agentId, serverId: parsed.serverId },
        });
        return;
      }
    }

    router.replace("/");
  }, [agentId, router, serverId, sessionKey, setTerminalTabs, terminalTabs]);

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
        ". It does more than closing the tab.",
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
    openAgentTab,
    handleCloseCurrentTab,
    handleCloseOtherTabs,
    handleTerminateAgent,
    handleTogglePinned,
  };
}
