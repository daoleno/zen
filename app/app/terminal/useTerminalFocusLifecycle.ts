import { useCallback, type Dispatch, type SetStateAction } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useFocusEffect } from "expo-router";
import { wsClient } from "../../services/websocket";

interface UseTerminalFocusLifecycleInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  setScreenFocused: Dispatch<SetStateAction<boolean>>;
  onInactive(): void;
}

export function useTerminalFocusLifecycle({
  serverId,
  agentId,
  sessionKey,
  setScreenFocused,
  onInactive,
}: UseTerminalFocusLifecycleInput) {
  const syncActiveTerminal = useCallback(
    (appState: AppStateStatus = "active") => {
      if (
        appState !== "active" ||
        !sessionKey ||
        !serverId ||
        !agentId
      ) {
        wsClient.clearActiveAgentsExcept(null);
        return;
      }

      wsClient.clearActiveAgentsExcept({ serverId, agentId });
    },
    [agentId, serverId, sessionKey],
  );

  useFocusEffect(
    useCallback(() => {
      setScreenFocused(true);
      syncActiveTerminal();

      const appStateSub = AppState.addEventListener("change", (nextState) => {
        syncActiveTerminal(nextState);
      });

      return () => {
        appStateSub.remove();
        setScreenFocused(false);
        onInactive();
        syncActiveTerminal("background");
      };
    }, [onInactive, setScreenFocused, syncActiveTerminal]),
  );
}
