import { useCallback, useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import {
  isAgentSessionListFreshForConnection,
  type ConnectionState,
  useAgents,
} from "../../../store/agents";
import type { WorkItem } from "../../../store/work";
import {
  getServerById,
  markAgentOpened,
  setAgentAlias,
  setInterfaceRenderMode,
  type StoredAgentAliases,
  type StoredInterfaceRenderMode,
  type StoredInterfaceRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
} from "../../../services/storage";
import { makeSessionKey } from "../../../services/sessionKeys";
import {
  blockCreateAfterAmbiguity,
  clearCreateAmbiguityForServer,
  isCreateBlockedByAmbiguity,
  reconcileCreateSessionFailure,
  reconcileCreateSessionSuccess,
  shouldUnlockCreateAfterAmbiguity,
  bumpAgentSessionListReceipt,
  type CreateAmbiguityGateState,
} from "../../../services/providers";
import { wsClient } from "../../../services/websocket";
import {
  launchSelectionFromSnapshot,
  providerClientForCommand,
} from "../../../services/providers";

interface CreateTerminalInput {
  cwd: string;
  command: string;
  name: string;
}

/**
 * Resolve the current client-selected Provider connection + model for a new
 * Session launch. Best-effort: any Provider load failure returns null so
 * creation proceeds with the daemon's own resolution.
 */
async function resolveLaunchSelection(
  serverId: string,
  command: string,
): Promise<{ connectionId: string; modelId: string } | null> {
  const client = providerClientForCommand(command);
  if (!client) return null;
  try {
    const snapshot = await wsClient.listProviders(serverId);
    return launchSelectionFromSnapshot(snapshot, client);
  } catch {
    return null;
  }
}

interface UseTerminalSessionActionsInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  connectionState: ConnectionState;
  creatingSession: boolean;
  interfaceRenderMode: StoredInterfaceRenderMode;
  linkedWork?: WorkItem;
  renameDraft: string;
  closeMenu(): void;
  setNewTerminalVisible(value: boolean): void;
  setCreatingSession(value: boolean): void;
  setRenameVisible(value: boolean): void;
  setAgentAliases(value: StoredAgentAliases): void;
  setInterfaceRenderModes: Dispatch<SetStateAction<StoredInterfaceRenderModes>>;
  setRecentAgentOpens: Dispatch<SetStateAction<StoredRecentAgentOpens>>;
  setServer: Dispatch<SetStateAction<StoredServer | null>>;
}

export function useTerminalSessionActions({
  serverId,
  agentId,
  sessionKey,
  connectionState,
  creatingSession,
  interfaceRenderMode,
  linkedWork,
  renameDraft,
  closeMenu,
  setNewTerminalVisible,
  setCreatingSession,
  setRenameVisible,
  setAgentAliases,
  setInterfaceRenderModes,
  setRecentAgentOpens,
  setServer,
}: UseTerminalSessionActionsInput) {
  const router = useRouter();
  const { state } = useAgents();
  const [createAmbiguityBlocks, setCreateAmbiguityBlocks] =
    useState<CreateAmbiguityGateState>({});
  const [listReceiptByServer, setListReceiptByServer] = useState<
    Record<string, number>
  >({});

  useEffect(() => {
    const onList = (payload: { serverId?: string }) => {
      const id = payload?.serverId?.trim();
      if (!id) return;
      setListReceiptByServer((current) =>
        bumpAgentSessionListReceipt(current, id),
      );
    };
    wsClient.on("agent_session_list", onList);
    return () => {
      wsClient.off("agent_session_list", onList);
    };
  }, []);

  useEffect(() => {
    setCreateAmbiguityBlocks((current) => {
      let next = current;
      for (const [id, block] of Object.entries(current)) {
        const connectionGeneration =
          state.connectionGenerationByServer[id] ?? 0;
        const listReceipt = listReceiptByServer[id] ?? 0;
        const listFresh = isAgentSessionListFreshForConnection(state, id);
        if (
          shouldUnlockCreateAfterAmbiguity({
            block,
            connectionGeneration,
            listReceipt,
            listFreshForConnection: listFresh,
          })
        ) {
          next = clearCreateAmbiguityForServer(next, id);
        }
      }
      return next;
    });
  }, [
    listReceiptByServer,
    state.agentSessionListGenerationByServer,
    state.connectionGenerationByServer,
    state.serverConnections,
  ]);

  const handleSaveRename = useCallback(async () => {
    if (!sessionKey) return;
    const nextAliases = await setAgentAlias(sessionKey, renameDraft);
    setAgentAliases(nextAliases);
    setRenameVisible(false);
  }, [renameDraft, sessionKey, setAgentAliases, setRenameVisible]);

  const applyInterfaceRenderMode = useCallback(
    (mode: StoredInterfaceRenderMode) => {
      if (!sessionKey) return;
      setInterfaceRenderModes((current) => {
        if (current[sessionKey] === mode) {
          return current;
        }
        return {
          ...current,
          [sessionKey]: mode,
        };
      });
      closeMenu();
      void setInterfaceRenderMode(sessionKey, mode).catch((error) => {
        console.log("Failed to persist Interface render mode:", error);
      });
    },
    [closeMenu, sessionKey, setInterfaceRenderModes],
  );

  const toggleInterfaceRenderMode = useCallback(() => {
    void applyInterfaceRenderMode(
      interfaceRenderMode === "chat" ? "terminal" : "chat",
    );
  }, [applyInterfaceRenderMode, interfaceRenderMode]);

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
      const connectionGeneration =
        state.connectionGenerationByServer[serverId] ?? 0;
      const listReceipt = listReceiptByServer[serverId] ?? 0;
      const listFresh = isAgentSessionListFreshForConnection(state, serverId);
      if (
        isCreateBlockedByAmbiguity({
          blocks: createAmbiguityBlocks,
          serverId,
          connectionGeneration,
          listReceipt,
          listFreshForConnection: listFresh,
        })
      ) {
        // Request list proof — do not clear the block here.
        wsClient.listAgentSessions(serverId);
        Alert.alert(
          "Refresh required",
          "Previous create result was ambiguous. Waiting for a confirmed session list before creating another terminal.",
        );
        return;
      }

      setNewTerminalVisible(false);
      closeMenu();
      setCreatingSession(true);
      let dispatched = false;
      try {
        const startedAt = Date.now();
        // Carry the client-selected Provider connection + model end-to-end
        // into the launch. The daemon resolves a deterministic supported-model
        // fallback when the selection is stale; failure to load Providers
        // never blocks creation (the daemon falls back to its own state).
        const selection = await resolveLaunchSelection(serverId, input.command);
        const pending = wsClient.createSession(serverId, {
          targetId: agentId,
          cwd: input.cwd,
          command: input.command,
          name: input.name,
          connectionId: selection?.connectionId,
          modelId: selection?.modelId,
        });
        dispatched = true;
        const created = await pending;
        const reconciled = reconcileCreateSessionSuccess(created);
        if (reconciled.kind === "ambiguous" || reconciled.kind === "failed") {
          if (reconciled.requiresReconcileBeforeCreate) {
            setCreateAmbiguityBlocks((current) =>
              blockCreateAfterAmbiguity(current, {
                serverId,
                connectionGeneration:
                  state.connectionGenerationByServer[serverId] ?? 0,
                listReceipt: listReceiptByServer[serverId] ?? 0,
              }),
            );
            wsClient.listAgentSessions(serverId);
          }
          Alert.alert(
            reconciled.kind === "ambiguous"
              ? "Refresh required"
              : "Could not create terminal",
            reconciled.message,
          );
          return;
        }
        const nextAgentId = reconciled.agentId;
        const nextSessionKey = makeSessionKey(serverId, nextAgentId);
        const openedAt = Date.now();
        void markAgentOpened(nextSessionKey, openedAt);
        setRecentAgentOpens((previous) => ({
          ...previous,
          [nextSessionKey]: openedAt,
        }));
        setCreateAmbiguityBlocks((current) =>
          clearCreateAmbiguityForServer(current, serverId),
        );
        router.replace({
          pathname: "/terminal/[id]",
          params: {
            id: nextAgentId,
            serverId,
            cwd: input.cwd,
            command: input.command,
            name: input.name,
            startedAt: String(startedAt),
            initialComposerFocus: "1",
            ...(reconciled.durabilityWarning
              ? { createDurabilityWarning: reconciled.durabilityWarning }
              : {}),
          },
        });
      } catch (error: any) {
        const reconciled = reconcileCreateSessionFailure(error, dispatched);
        if (
          reconciled.kind === "ambiguous" ||
          (reconciled.kind === "failed" &&
            reconciled.requiresReconcileBeforeCreate)
        ) {
          setCreateAmbiguityBlocks((current) =>
            blockCreateAfterAmbiguity(current, {
              serverId,
              connectionGeneration:
                state.connectionGenerationByServer[serverId] ?? 0,
              listReceipt: listReceiptByServer[serverId] ?? 0,
            }),
          );
          // Fire-and-forget list must not clear the block.
          wsClient.listAgentSessions(serverId);
        }
        Alert.alert(
          reconciled.kind === "ambiguous"
            ? "Refresh required"
            : "Could not create terminal",
          reconciled.kind === "navigable" ? "Create failed." : reconciled.message,
        );
      } finally {
        setCreatingSession(false);
      }
    },
    [
      agentId,
      closeMenu,
      connectionState,
      createAmbiguityBlocks,
      creatingSession,
      listReceiptByServer,
      router,
      serverId,
      setCreatingSession,
      setNewTerminalVisible,
      setRecentAgentOpens,
      state,
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
    closeMenu();
    setNewTerminalVisible(true);
  }, [closeMenu, connectionState, setNewTerminalVisible]);

  const openLinkedWork = useCallback(() => {
    if (!linkedWork) return;
    closeMenu();
    router.push({
      pathname: "/work/[id]",
      params: {
        id: linkedWork.id,
        serverId: linkedWork.serverId,
      },
    });
  }, [closeMenu, linkedWork, router]);

  const openRenameModal = useCallback(() => {
    closeMenu();
    setRenameVisible(true);
  }, [closeMenu, setRenameVisible]);

  const refreshServerMeta = useCallback(async () => {
    if (!serverId) return;
    const next = await getServerById(serverId);
    setServer(next);
  }, [serverId, setServer]);

  const retryServerConnection = useCallback(async () => {
    if (!serverId) return;
    const storedServer = await getServerById(serverId);
    if (!storedServer) return;
    setServer(storedServer);
    wsClient.connectServer(storedServer);
  }, [serverId, setServer]);

  return {
    createTerminal,
    openNewTerminal,
    openLinkedWork,
    openRenameModal,
    handleSaveRename,
    toggleInterfaceRenderMode,
    applyInterfaceRenderMode,
    refreshServerMeta,
    retryServerConnection,
  };
}
