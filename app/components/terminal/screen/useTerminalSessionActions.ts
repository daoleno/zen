import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import { useCurrentServer } from "../../../store/currentServer";
import {
  isWorkerSessionListFreshForConnection,
  type ConnectionState,
  useWorkers,
} from "../../../store/workers";
import type { WorkItem } from "../../../store/work";
import {
  markWorkerOpened,
  setWorkerAlias,
  setInterfaceRenderMode,
  type StoredWorkerAliases,
  type StoredInterfaceRenderMode,
  type StoredInterfaceRenderModes,
  type StoredRecentWorkerOpens,
} from "../../../services/storage";
import { makeSessionKey } from "../../../services/sessionKeys";
import {
  blockCreateAfterAmbiguity,
  clearCreateAmbiguityForServer,
  isCreateBlockedByAmbiguity,
  reconcileCreateSessionFailure,
  reconcileCreateSessionSuccess,
  shouldUnlockCreateAfterAmbiguity,
  bumpWorkerSessionListReceipt,
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
  workerId: string;
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
  setWorkerAliases(value: StoredWorkerAliases): void;
  setInterfaceRenderModes: Dispatch<SetStateAction<StoredInterfaceRenderModes>>;
  setRecentWorkerOpens: Dispatch<SetStateAction<StoredRecentWorkerOpens>>;
}

export function useTerminalSessionActions({
  serverId,
  workerId,
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
  setWorkerAliases,
  setInterfaceRenderModes,
  setRecentWorkerOpens,
}: UseTerminalSessionActionsInput) {
  const router = useRouter();
  const { refreshServers, isCurrentServer } = useCurrentServer();
  const createInFlightRef = useRef(false);
  const { state } = useWorkers();
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
        bumpWorkerSessionListReceipt(current, id),
      );
    };
    wsClient.on("worker_session_list", onList);
    return () => {
      wsClient.off("worker_session_list", onList);
    };
  }, []);

  useEffect(() => {
    setCreateAmbiguityBlocks((current) => {
      let next = current;
      for (const [id, block] of Object.entries(current)) {
        const connectionGeneration =
          state.connectionGenerationByServer[id] ?? 0;
        const listReceipt = listReceiptByServer[id] ?? 0;
        const listFresh = isWorkerSessionListFreshForConnection(state, id);
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
    state.workerSessionListGenerationByServer,
    state.connectionGenerationByServer,
    state.serverConnections,
  ]);

  const handleSaveRename = useCallback(async () => {
    if (!sessionKey) return;
    const nextAliases = await setWorkerAlias(sessionKey, renameDraft);
    setWorkerAliases(nextAliases);
    setRenameVisible(false);
  }, [renameDraft, sessionKey, setWorkerAliases, setRenameVisible]);

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
      if (!isCurrentServer(serverId) || createInFlightRef.current) return;
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
      const listFresh = isWorkerSessionListFreshForConnection(state, serverId);
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
        wsClient.listWorkerSessions(serverId);
        Alert.alert(
          "Refresh required",
          "Previous create result was ambiguous. Waiting for a confirmed session list before creating another terminal.",
        );
        return;
      }

      setNewTerminalVisible(false);
      closeMenu();
      setCreatingSession(true);
      createInFlightRef.current = true;
      let dispatched = false;
      try {
        const startedAt = Date.now();
        // Carry the client-selected Provider connection + model end-to-end
        // into the launch. The daemon resolves a deterministic supported-model
        // fallback when the selection is stale; failure to load Providers
        // never blocks creation (the daemon falls back to its own state).
        const selection = await resolveLaunchSelection(serverId, input.command);
        if (!isCurrentServer(serverId)) return;
        const pending = wsClient.createSession(serverId, {
          targetId: workerId,
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
            wsClient.listWorkerSessions(serverId);
          }
          if (!isCurrentServer(serverId)) return;
          Alert.alert(
            reconciled.kind === "ambiguous"
              ? "Refresh required"
              : "Could not create terminal",
            reconciled.message,
          );
          return;
        }
        setCreateAmbiguityBlocks((current) => clearCreateAmbiguityForServer(current, serverId));
        if (!isCurrentServer(serverId)) return;
        const nextWorkerId = reconciled.workerId;
        const nextSessionKey = makeSessionKey(serverId, nextWorkerId);
        const openedAt = Date.now();
        void markWorkerOpened(nextSessionKey, openedAt);
        setRecentWorkerOpens((previous) => ({
          ...previous,
          [nextSessionKey]: openedAt,
        }));
        router.replace({
          pathname: "/terminal/[id]",
          params: {
            id: nextWorkerId,
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
          wsClient.listWorkerSessions(serverId);
        }
        if (!isCurrentServer(serverId)) return;
        Alert.alert(
          reconciled.kind === "ambiguous"
            ? "Refresh required"
            : "Could not create terminal",
          reconciled.kind === "navigable" ? "Create failed." : reconciled.message,
        );
      } finally {
        createInFlightRef.current = false;
        setCreatingSession(false);
      }
    },
    [
      workerId,
      closeMenu,
      connectionState,
      createAmbiguityBlocks,
      creatingSession,
      listReceiptByServer,
      isCurrentServer,
      router,
      serverId,
      setCreatingSession,
      setNewTerminalVisible,
      setRecentWorkerOpens,
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

  const retryServerConnection = useCallback(async () => {
    if (!isCurrentServer(serverId)) return;
    await refreshServers();
  }, [serverId, refreshServers, isCurrentServer]);

  return {
    createTerminal,
    openNewTerminal,
    openLinkedWork,
    openRenameModal,
    handleSaveRename,
    toggleInterfaceRenderMode,
    applyInterfaceRenderMode,
    retryServerConnection,
  };
}
