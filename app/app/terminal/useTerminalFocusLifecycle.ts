import { useCallback, type Dispatch, type SetStateAction } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useFocusEffect } from "expo-router";
import { wsClient } from "../../services/websocket";
import {
  createTerminalConnectedPresenceHandler,
  currentTerminalPresence,
} from "./terminalPresence";

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
    (appState: AppStateStatus, focused = true) => {
      wsClient.clearActiveAgentsExcept(
        currentTerminalPresence({
          serverId,
          agentId,
          sessionKey,
          appState,
          focused,
        }),
      );
    },
    [agentId, serverId, sessionKey],
  );

  useFocusEffect(
    useCallback(() => {
      setScreenFocused(true);
      syncActiveTerminal(AppState.currentState);

      const onConnected = createTerminalConnectedPresenceHandler(
        serverId,
        () => syncActiveTerminal(AppState.currentState),
      );
      wsClient.on("connected", onConnected);

      const appStateSub = AppState.addEventListener("change", (nextState) => {
        syncActiveTerminal(nextState);
      });

      return () => {
        appStateSub.remove();
        wsClient.off("connected", onConnected);
        setScreenFocused(false);
        onInactive();
        syncActiveTerminal(AppState.currentState, false);
      };
    }, [onInactive, serverId, setScreenFocused, syncActiveTerminal]),
  );
}
