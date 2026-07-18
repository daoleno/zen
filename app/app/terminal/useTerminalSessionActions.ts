import { useCallback, type Dispatch, type SetStateAction } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import type { ConnectionState } from "../../store/agents";
import type { WorkItem } from "../../store/work";
import {
  getServerById,
  markAgentOpened,
  setAgentAlias,
  setCodexRenderMode,
  type StoredAgentAliases,
  type StoredCodexRenderMode,
  type StoredCodexRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
} from "../../services/storage";
import { makeSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";

interface CreateTerminalInput {
  cwd: string;
  command: string;
  name: string;
}

interface UseTerminalSessionActionsInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  connectionState: ConnectionState;
  creatingSession: boolean;
  codexRenderMode: StoredCodexRenderMode;
  linkedWork?: WorkItem;
  renameDraft: string;
  closeMenu(): void;
  setNewTerminalVisible(value: boolean): void;
  setCreatingSession(value: boolean): void;
  setRenameVisible(value: boolean): void;
  setAgentAliases(value: StoredAgentAliases): void;
  setCodexRenderModes: Dispatch<SetStateAction<StoredCodexRenderModes>>;
  setRecentAgentOpens: Dispatch<SetStateAction<StoredRecentAgentOpens>>;
  setServer: Dispatch<SetStateAction<StoredServer | null>>;
}

export function useTerminalSessionActions({
  serverId,
  agentId,
  sessionKey,
  connectionState,
  creatingSession,
  codexRenderMode,
  linkedWork,
  renameDraft,
  closeMenu,
  setNewTerminalVisible,
  setCreatingSession,
  setRenameVisible,
  setAgentAliases,
  setCodexRenderModes,
  setRecentAgentOpens,
  setServer,
}: UseTerminalSessionActionsInput) {
  const router = useRouter();

  const handleSaveRename = useCallback(async () => {
    if (!sessionKey) return;
    const nextAliases = await setAgentAlias(sessionKey, renameDraft);
    setAgentAliases(nextAliases);
    setRenameVisible(false);
  }, [renameDraft, sessionKey, setAgentAliases, setRenameVisible]);

  const applyCodexRenderMode = useCallback(
    (mode: StoredCodexRenderMode) => {
      if (!sessionKey) return;
      setCodexRenderModes((current) => {
        if (current[sessionKey] === mode) {
          return current;
        }
        return {
          ...current,
          [sessionKey]: mode,
        };
      });
      closeMenu();
      void setCodexRenderMode(sessionKey, mode).catch((error) => {
        console.log("Failed to persist Codex render mode:", error);
      });
    },
    [closeMenu, sessionKey, setCodexRenderModes],
  );

  const toggleCodexRenderMode = useCallback(() => {
    void applyCodexRenderMode(
      codexRenderMode === "chat" ? "terminal" : "chat",
    );
  }, [applyCodexRenderMode, codexRenderMode]);

  const createTerminal = useCallback(
    async (input: CreateTerminalInput) => {
      if (!serverId || connectionState !== "connected" || creatingSession) {
        if (connectionState !== "connected") {
          Alert.alert(
            "Daemon unavailable",
            "Reconnect to that daemon before creating a new terminal.",
          );
        }
        return;
      }

      setNewTerminalVisible(false);
      closeMenu();
      setCreatingSession(true);
      try {
        const startedAt = Date.now();
        const nextAgentId = await wsClient.createSession(serverId, {
          targetId: agentId,
          cwd: input.cwd,
          command: input.command,
          name: input.name,
        });
        const nextSessionKey = makeSessionKey(serverId, nextAgentId);
        const openedAt = Date.now();
        void markAgentOpened(nextSessionKey, openedAt);
        setRecentAgentOpens((previous) => ({
          ...previous,
          [nextSessionKey]: openedAt,
        }));
        router.replace({
          pathname: "/terminal/[id]",
          params: {
            id: nextAgentId,
            serverId,
            cwd: input.cwd,
            command: input.command,
            name: input.name,
            startedAt: String(startedAt),
          },
        });
      } catch (error: any) {
        Alert.alert(
          "Could not create terminal",
          error?.message || "Try reconnecting to that daemon first.",
        );
      } finally {
        setCreatingSession(false);
      }
    },
    [
      agentId,
      closeMenu,
      connectionState,
      creatingSession,
      router,
      serverId,
      setCreatingSession,
      setNewTerminalVisible,
      setRecentAgentOpens,
    ],
  );

  const openNewTerminal = useCallback(() => {
    if (connectionState !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before creating a new terminal.",
      );
      return;
    }
    setNewTerminalVisible(true);
  }, [connectionState, setNewTerminalVisible]);

  const openLinkedWork = useCallback(() => {
    if (!linkedWork) return;
    closeMenu();
    router.push({
      pathname: "/work/[id]",
      params: { id: linkedWork.id, serverId: linkedWork.serverId },
    });
  }, [closeMenu, linkedWork, router]);

  const retryServerConnection = useCallback(async () => {
    if (!serverId) return;
    const storedServer = await getServerById(serverId);
    if (!storedServer) return;

    setServer(storedServer);
    wsClient.connectServer(storedServer);
  }, [serverId, setServer]);

  return {
    applyCodexRenderMode,
    createTerminal,
    handleSaveRename,
    openLinkedWork,
    openNewTerminal,
    retryServerConnection,
    toggleCodexRenderMode,
  };
}
