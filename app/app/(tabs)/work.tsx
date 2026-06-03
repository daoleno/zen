import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { BottomSheetFrame, IconButton, StateView } from "../../components/ui";
import { AppButton } from "../../components/ui/AppButton";
import { AppText } from "../../components/ui/AppText";
import { CodexChatSurface } from "../../components/terminal/CodexChatSurface";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
} from "../../constants/terminalThemes";
import {
  Colors,
  Typography,
  useAppColors,
  useAppTheme,
} from "../../constants/tokens";
import { getServers, type StoredServer } from "../../services/storage";
import {
  wsClient,
  type BrainNativeThread,
  type BrainNativeThreadGoal,
} from "../../services/websocket";
import { useAgents, type ConnectionState } from "../../store/agents";
import {
  useBrain,
  type BrainAgentRef,
  type BrainAdapterRef,
  type BrainAttentionQueueItem,
  type BrainServerState,
} from "../../store/brain";
import { useWork, type WorkItem } from "../../store/work";

export default function BrainScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const { isLight } = useAppTheme();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(isLight ? "light" : "dark"),
    [isLight],
  );
  const chrome = useMemo(() => buildTerminalChrome(terminalTheme), [terminalTheme]);
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const { state: workState } = useWork();
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
  const [switchingAdapterId, setSwitchingAdapterId] = useState<string | null>(null);
  const [adapterSwitchError, setAdapterSwitchError] = useState<string | null>(null);
  const [newChatLoading, setNewChatLoading] = useState(false);
  const [brainActionError, setBrainActionError] = useState<string | null>(null);
  const [attentionSheetVisible, setAttentionSheetVisible] = useState(false);
  const [threadSheetVisible, setThreadSheetVisible] = useState(false);
  const [threadsLoading, setThreadsLoading] = useState(false);
  const [threadsError, setThreadsError] = useState<string | null>(null);
  const [nativeThreads, setNativeThreads] = useState<BrainNativeThread[]>([]);
  const [nativeThreadsNextCursor, setNativeThreadsNextCursor] = useState<string | undefined>();
  const [selectedNativeThreadId, setSelectedNativeThreadId] = useState("");
  const [threadSearchDraft, setThreadSearchDraft] = useState("");
  const [threadSearchTerm, setThreadSearchTerm] = useState("");
  const [threadAction, setThreadAction] = useState<"archive" | "fork" | "goal" | "pin" | "read" | "resume" | "review" | null>(null);
  const [threadGoal, setThreadGoal] = useState<BrainNativeThreadGoal | null>(null);
  const [threadGoalDraft, setThreadGoalDraft] = useState("");
  const [threadGoalLoading, setThreadGoalLoading] = useState(false);
  const [threadGoalError, setThreadGoalError] = useState<string | null>(null);
  const threadGoalRequestSeq = useRef(0);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      void getServers().then((savedServers) => {
        if (!cancelled) {
          setServers(savedServers);
        }
      });
      return () => {
        cancelled = true;
      };
    }, []),
  );

  const connectedServers = useMemo(
    () =>
      servers.filter(
        (server) => agentState.serverConnections[server.id] === "connected",
      ),
    [agentState.serverConnections, servers],
  );

  const activeServer = useMemo(
    () =>
      resolveActiveServer({
        connectedServers,
        servers,
        brainByServer: brainState.byServer,
        connectionStates: agentState.serverConnections,
      }),
    [
      agentState.serverConnections,
      brainState.byServer,
      connectedServers,
      servers,
    ],
  );

  useEffect(() => {
    if (!activeServer) {
      return;
    }
    wsClient.requestBrainSnapshot(activeServer.id);
  }, [activeServer?.id]);

  const activeBrain = activeServer ? brainState.byServer[activeServer.id] : null;
  const connectionState: ConnectionState = activeServer
    ? agentState.serverConnections[activeServer.id] || "offline"
    : "offline";
  const connectionIssue = activeServer
    ? agentState.serverConnectionIssues[activeServer.id] ?? null
    : null;
  const statusLabel = brainStatusLabel({
    activeServer,
    connectionState,
    activeBrain,
  });
  const hostAgent = activeBrain?.host_agent ?? null;
  const hostAdapter = activeBrain?.host_adapter ?? null;
  const adapterLabel = brainAdapterLabel(activeBrain?.host_adapter);
  const baseAttentionLabel = brainAttentionLabel(activeBrain?.attention);
  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const canUseCodexBrainInterface = Boolean(
    ready && hostAdapter?.provider === "codex",
  );
  const availableAdapters = activeBrain?.adapters ?? [];
  const canUseNativeThreads = Boolean(
    activeServer && hostAdapter?.capabilities?.native_threads,
  );
  const canSearchNativeThreads = Boolean(
    canUseNativeThreads && hostAdapter?.capabilities?.native_search,
  );
  const canArchiveNativeThreads = Boolean(
    canUseNativeThreads && hostAdapter?.capabilities?.native_archive,
  );
  const canForkNativeThreads = Boolean(
    canUseNativeThreads && hostAdapter?.capabilities?.native_fork,
  );
  const canResumeNativeThreads = Boolean(
    canUseNativeThreads &&
      hostAdapter?.capabilities?.interactive_tty &&
      hostAdapter?.capabilities?.native_resume,
  );
  const canUseNativeGoals = Boolean(
    canUseNativeThreads && hostAdapter?.capabilities?.native_goals,
  );
  const selectedNativeThread = useMemo(
    () =>
      nativeThreads.find((thread) => thread.id === selectedNativeThreadId) ??
      nativeThreads[0] ??
      null,
    [nativeThreads, selectedNativeThreadId],
  );
  const nativeThreadById = useMemo(() => {
    const map = new Map<string, BrainNativeThread>();
    for (const thread of nativeThreads) {
      map.set(thread.id, thread);
    }
    return map;
  }, [nativeThreads]);
  const attentionQueue = activeBrain?.attention_queue;
  const hasSnapshotAttentionQueue = Array.isArray(attentionQueue);
  const visibleBrainAgents = activeBrain?.agents ?? [];
  const blockedBrainAgents = useMemo(
    () => visibleBrainAgents.filter((agent) => agent.status === "blocked"),
    [visibleBrainAgents],
  );
  const blockedAttentionItems = useMemo(
    () =>
      hasSnapshotAttentionQueue
        ? (attentionQueue ?? []).filter(
            (item) => item.kind === "blocked_agent",
          )
        : blockedBrainAgents.map(brainBlockedAgentToQueueItem),
    [attentionQueue, blockedBrainAgents, hasSnapshotAttentionQueue],
  );
  const reviewQueueThreads = useMemo(
    () => nativeThreads.filter((thread) => threadNeedsReview(thread)),
    [nativeThreads],
  );
  const reviewAttentionItems = useMemo(
    () =>
      hasSnapshotAttentionQueue
        ? (attentionQueue ?? []).filter(
            (item) => item.kind === "review_thread",
          )
        : reviewQueueThreads.map(brainReviewThreadToQueueItem),
    [attentionQueue, hasSnapshotAttentionQueue, reviewQueueThreads],
  );
  const reviewQueueIndex = useMemo(
    () =>
      reviewAttentionItems.findIndex(
        (item) => attentionQueueItemThreadID(item) === selectedNativeThread?.id,
      ),
    [reviewAttentionItems, selectedNativeThread?.id],
  );
  const reviewAttentionCount = hasSnapshotAttentionQueue
    ? reviewAttentionItems.length
    : Math.max(
        activeBrain?.attention?.review_queue ?? 0,
        reviewAttentionItems.length,
      );
  const workAttentionItems = useMemo(() => {
    if (hasSnapshotAttentionQueue) {
      return (attentionQueue ?? []).filter((item) => item.kind === "work_item");
    }
    if (!activeServer?.id) {
      return [] as BrainAttentionQueueItem[];
    }
    return Object.values(workState.byKey)
      .filter(
        (item) =>
          item.serverId === activeServer.id && brainWorkItemNeedsAttention(item),
      )
      .map(brainWorkItemToQueueItem)
      .sort(compareAttentionQueueItems);
  }, [activeServer?.id, attentionQueue, hasSnapshotAttentionQueue, workState.byKey]);
  const attentionItemCount = hasSnapshotAttentionQueue
    ? (attentionQueue ?? []).length
    : blockedAttentionItems.length + reviewAttentionCount + workAttentionItems.length;
  const workAttentionLabel =
    workAttentionItems.length > 0 ? `${workAttentionItems.length} work` : "";
  const attentionLabel = baseAttentionLabel || workAttentionLabel;
  const subtitleLabel = [statusLabel, adapterLabel, attentionLabel]
    .filter(Boolean)
    .join(" · ");
  const keyboardVerticalOffset = 0;

  const openAdapterSheet = useCallback(() => {
    if (!activeBrain?.adapters?.length || !activeServer) {
      return;
    }
    setAdapterSwitchError(null);
    setAdapterSheetVisible(true);
  }, [activeBrain?.adapters?.length, activeServer]);

  const closeAdapterSheet = useCallback(() => {
    setAdapterSheetVisible(false);
    setAdapterSwitchError(null);
  }, []);

  const startNewBrainChat = useCallback(async () => {
    if (!activeServer || !activeBrain?.hydrated || newChatLoading) {
      return;
    }
    setBrainActionError(null);
    setNewChatLoading(true);
    try {
      await wsClient.startNewBrainChat(activeServer.id);
    } catch (error: any) {
      setBrainActionError(error?.message || "Failed to start a new Brain chat.");
    } finally {
      setNewChatLoading(false);
    }
  }, [activeBrain?.hydrated, activeServer, newChatLoading]);

  const loadNativeThreads = useCallback(
    async (cursor?: string, append = false, searchTerm = threadSearchTerm) => {
      if (!activeServer || !hostAdapter?.id || !hostAdapter?.capabilities?.native_threads) {
        return;
      }
      setThreadsLoading(true);
      try {
        setThreadsError(null);
        const snapshot = await wsClient.listBrainThreads(activeServer.id, {
          adapterId: hostAdapter.id,
          limit: 24,
          cursor,
          searchTerm,
        });
        setNativeThreads((current) => {
          const next = append ? [...current] : [];
          for (const thread of snapshot.threads) {
            const existingIndex = next.findIndex((item) => item.id === thread.id);
            if (existingIndex >= 0) {
              next[existingIndex] = thread;
            } else {
              next.push(thread);
            }
          }
          return sortBrainNativeThreadsForAttention(next);
        });
        setNativeThreadsNextCursor(snapshot.next_cursor);
        setSelectedNativeThreadId((current) => {
          if (append && current) {
            return current;
          }
          if (current && snapshot.threads.some((thread) => thread.id === current)) {
            return current;
          }
          return snapshot.threads[0]?.id ?? (append ? current : "");
        });
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to load Brain threads.");
      } finally {
        setThreadsLoading(false);
      }
    },
    [
      activeServer,
      hostAdapter?.capabilities?.native_threads,
      hostAdapter?.id,
      threadSearchTerm,
    ],
  );

  const openThreadSheet = useCallback(() => {
    if (!canUseNativeThreads) {
      return;
    }
    setThreadSheetVisible(true);
  }, [canUseNativeThreads]);

  const openAttentionSheet = useCallback(() => {
    if (attentionItemCount <= 0) {
      return;
    }
    setAttentionSheetVisible(true);
    if (canUseNativeThreads && reviewAttentionItems.length > 0) {
      void loadNativeThreads(undefined, false, threadSearchTerm);
    }
  }, [
    attentionItemCount,
    canUseNativeThreads,
    loadNativeThreads,
    reviewAttentionItems.length,
    threadSearchTerm,
  ]);

  const closeAttentionSheet = useCallback(() => {
    setAttentionSheetVisible(false);
  }, []);

  const closeThreadSheet = useCallback(() => {
    setThreadSheetVisible(false);
    setThreadsError(null);
    setThreadGoalError(null);
    setThreadAction(null);
  }, []);

  const openBlockedAgent = useCallback(
    (agentId: string) => {
      if (!activeServer?.id || !agentId) {
        return;
      }
      setAttentionSheetVisible(false);
      router.push({
        pathname: "/terminal/[id]",
        params: { id: agentId, serverId: activeServer.id },
      });
    },
    [activeServer?.id, router],
  );

  const openWorkItem = useCallback(
    (item: BrainAttentionQueueItem) => {
      const workItemID = attentionQueueItemWorkID(item);
      if (!activeServer?.id || !workItemID) {
        return;
      }
      setAttentionSheetVisible(false);
      router.push({
        pathname: "/work/[id]",
        params: { id: workItemID, serverId: activeServer.id },
      });
    },
    [activeServer?.id, router],
  );

  const focusReviewThread = useCallback((threadID: string) => {
    if (!threadID) {
      return;
    }
    setSelectedNativeThreadId(threadID);
    setAttentionSheetVisible(false);
    setThreadSheetVisible(true);
  }, []);

  const openReviewQueue = useCallback(() => {
    if (!canUseNativeThreads) {
      return;
    }
    const firstReviewThreadID = attentionQueueItemThreadID(reviewAttentionItems[0]);
    if (firstReviewThreadID) {
      setSelectedNativeThreadId(firstReviewThreadID);
    }
    setAttentionSheetVisible(false);
    setThreadSheetVisible(true);
    void loadNativeThreads(undefined, false, threadSearchTerm);
  }, [
    canUseNativeThreads,
    loadNativeThreads,
    reviewAttentionItems,
    threadSearchTerm,
  ]);

  const selectNextReviewThread = useCallback(() => {
    if (reviewAttentionItems.length === 0) {
      return;
    }
    const currentIndex = selectedNativeThread?.id
      ? reviewAttentionItems.findIndex(
          (item) => attentionQueueItemThreadID(item) === selectedNativeThread.id,
        )
      : -1;
    const nextThread =
      reviewAttentionItems[currentIndex + 1] ?? reviewAttentionItems[0];
    const nextThreadID = attentionQueueItemThreadID(nextThread);
    if (nextThreadID) {
      setSelectedNativeThreadId(nextThreadID);
    }
  }, [reviewAttentionItems, selectedNativeThread?.id]);

  const refreshNativeThreads = useCallback(() => {
    void loadNativeThreads(undefined, false, threadSearchTerm);
  }, [loadNativeThreads, threadSearchTerm]);

  const loadMoreNativeThreads = useCallback(() => {
    if (!nativeThreadsNextCursor) {
      return;
    }
    void loadNativeThreads(nativeThreadsNextCursor, true, threadSearchTerm);
  }, [loadNativeThreads, nativeThreadsNextCursor, threadSearchTerm]);

  const submitNativeThreadSearch = useCallback(() => {
    if (!canSearchNativeThreads) {
      return;
    }
    const nextTerm = threadSearchDraft.trim();
    if (nextTerm === threadSearchTerm) {
      void loadNativeThreads(undefined, false, nextTerm);
      return;
    }
    setThreadSearchTerm(nextTerm);
  }, [
    canSearchNativeThreads,
    loadNativeThreads,
    threadSearchDraft,
    threadSearchTerm,
  ]);

  const clearNativeThreadSearch = useCallback(() => {
    setThreadSearchDraft("");
    if (threadSearchTerm === "") {
      void loadNativeThreads(undefined, false, "");
      return;
    }
    setThreadSearchTerm("");
  }, [loadNativeThreads, threadSearchTerm]);

  const toggleNativeThreadArchive = useCallback(
    async (thread: BrainNativeThread) => {
      if (!activeServer || !hostAdapter?.id || !thread.id) {
        return;
      }
      setThreadsError(null);
      try {
        setThreadAction("archive");
        setThreadsLoading(true);
        await wsClient.archiveBrainThread(activeServer.id, thread.id, {
          adapterId: hostAdapter.id,
          archived: !thread.archived,
        });
        await loadNativeThreads(undefined, false);
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to update thread.");
      } finally {
        setThreadsLoading(false);
        setThreadAction(null);
      }
    },
    [activeServer, hostAdapter?.id, loadNativeThreads],
  );

  const upsertNativeThread = useCallback((thread: BrainNativeThread) => {
    if (!thread.id) {
      return;
    }
    setNativeThreads((current) => {
      const existingIndex = current.findIndex((item) => item.id === thread.id);
      if (existingIndex < 0) {
        return sortBrainNativeThreadsForAttention([thread, ...current]);
      }
      const next = [...current];
      next[existingIndex] = { ...next[existingIndex], ...thread };
      return sortBrainNativeThreadsForAttention(next);
    });
  }, []);

  const toggleNativeThreadPin = useCallback(
    async (thread: BrainNativeThread | null) => {
      if (!activeServer || !thread?.id) {
        return;
      }
      setThreadsError(null);
      try {
        setThreadAction("pin");
        const updated = await wsClient.pinBrainThread(activeServer.id, thread.id, {
          pinned: !thread.pinned,
        });
        upsertNativeThread(updated);
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to update pinned thread.");
      } finally {
        setThreadAction(null);
      }
    },
    [activeServer, upsertNativeThread],
  );

  const toggleNativeThreadReview = useCallback(
    async (thread: BrainNativeThread | null) => {
      if (!activeServer || !thread?.id) {
        return;
      }
      const wasQueued = threadNeedsReview(thread);
      const currentQueue = reviewAttentionItems.slice();
      const currentIndex = currentQueue.findIndex(
        (item) => attentionQueueItemThreadID(item) === thread.id,
      );
      const nextQueuedThread =
        wasQueued && currentIndex >= 0
          ? currentQueue[currentIndex + 1] ?? currentQueue[0] ?? null
          : null;
      setThreadsError(null);
      try {
        setThreadAction("review");
        const updated = await wsClient.setBrainThreadReviewState(
          activeServer.id,
          thread.id,
          {
            reviewState: threadNeedsReview(thread) ? "" : "needs_review",
          },
        );
        upsertNativeThread(updated);
        const nextQueuedThreadID = attentionQueueItemThreadID(nextQueuedThread);
        if (wasQueued && nextQueuedThreadID && nextQueuedThreadID !== thread.id) {
          setSelectedNativeThreadId(nextQueuedThreadID);
        }
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to update review queue.");
      } finally {
        setThreadAction(null);
      }
    },
    [activeServer, reviewAttentionItems, upsertNativeThread],
  );

  const refreshNativeThreadDetail = useCallback(
    async (thread: BrainNativeThread | null) => {
      if (!activeServer || !hostAdapter?.id || !thread?.id) {
        return;
      }
      setThreadsError(null);
      try {
        setThreadAction("read");
        const next = await wsClient.readBrainThread(activeServer.id, thread.id, {
          adapterId: hostAdapter.id,
          includeTurns: false,
        });
        upsertNativeThread(next);
        setSelectedNativeThreadId(next.id || thread.id);
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to refresh thread.");
      } finally {
        setThreadAction(null);
      }
    },
    [activeServer, hostAdapter?.id, upsertNativeThread],
  );

  const forkNativeThread = useCallback(
    async (thread: BrainNativeThread | null) => {
      if (!activeServer || !hostAdapter?.id || !thread?.id) {
        return;
      }
      setThreadsError(null);
      try {
        setThreadAction("fork");
        const forked = await wsClient.forkBrainThread(activeServer.id, thread.id, {
          adapterId: hostAdapter.id,
          cwd: thread.cwd,
        });
        upsertNativeThread(forked);
        threadGoalRequestSeq.current += 1;
        setSelectedNativeThreadId(forked.id || thread.id);
        setThreadGoal(null);
        setThreadGoalDraft("");
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to fork thread.");
      } finally {
        setThreadAction(null);
      }
    },
    [activeServer, hostAdapter?.id, upsertNativeThread],
  );

  const resumeNativeThread = useCallback(
    async (thread: BrainNativeThread | null) => {
      if (!activeServer || !hostAdapter?.id || !thread?.id) {
        return;
      }
      setThreadsError(null);
      try {
        setThreadAction("resume");
        const result = await wsClient.resumeBrainThread(activeServer.id, thread.id, {
          adapterId: hostAdapter.id,
          cwd: thread.cwd,
        });
        upsertNativeThread(result.thread);
        setSelectedNativeThreadId(result.thread.id || thread.id);
        wsClient.requestBrainSnapshot(activeServer.id);
        closeThreadSheet();
      } catch (error: any) {
        setThreadsError(error?.message || "Failed to resume thread in Brain.");
      } finally {
        setThreadAction(null);
      }
    },
    [
      activeServer,
      closeThreadSheet,
      hostAdapter?.id,
      upsertNativeThread,
    ],
  );

  const loadNativeThreadGoal = useCallback(
    async (threadID: string) => {
      if (!activeServer || !hostAdapter?.id || !canUseNativeGoals || !threadID) {
        return;
      }
      const requestSeq = ++threadGoalRequestSeq.current;
      setThreadGoalLoading(true);
      setThreadGoalError(null);
      try {
        const goal = await wsClient.getBrainThreadGoal(activeServer.id, threadID, {
          adapterId: hostAdapter.id,
        });
        if (requestSeq !== threadGoalRequestSeq.current) {
          return;
        }
        setThreadGoal(goal);
        setThreadGoalDraft(goal?.objective ?? "");
      } catch (error: any) {
        if (requestSeq !== threadGoalRequestSeq.current) {
          return;
        }
        setThreadGoal(null);
        setThreadGoalDraft("");
        setThreadGoalError(error?.message || "Failed to load thread goal.");
      } finally {
        if (requestSeq !== threadGoalRequestSeq.current) {
          return;
        }
        setThreadGoalLoading(false);
      }
    },
    [activeServer, canUseNativeGoals, hostAdapter?.id],
  );

  const saveNativeThreadGoal = useCallback(async () => {
    if (!activeServer || !hostAdapter?.id || !selectedNativeThread?.id) {
      return;
    }
    const objective = threadGoalDraft.trim();
    if (!objective) {
      setThreadGoalError("Objective is required.");
      return;
    }
    threadGoalRequestSeq.current += 1;
    setThreadGoalError(null);
    try {
      setThreadAction("goal");
      const goal = await wsClient.setBrainThreadGoal(
        activeServer.id,
        selectedNativeThread.id,
        {
          adapterId: hostAdapter.id,
          objective,
          status: threadGoal?.status || "active",
        },
      );
      setThreadGoal(goal);
      setThreadGoalDraft(goal.objective ?? objective);
    } catch (error: any) {
      setThreadGoalError(error?.message || "Failed to update thread goal.");
    } finally {
      setThreadAction(null);
    }
  }, [
    activeServer,
    hostAdapter?.id,
    selectedNativeThread?.id,
    threadGoal?.status,
    threadGoalDraft,
  ]);

  const clearNativeThreadGoal = useCallback(async () => {
    if (!activeServer || !hostAdapter?.id || !selectedNativeThread?.id) {
      return;
    }
    threadGoalRequestSeq.current += 1;
    setThreadGoalError(null);
    try {
      setThreadAction("goal");
      await wsClient.clearBrainThreadGoal(activeServer.id, selectedNativeThread.id, {
        adapterId: hostAdapter.id,
      });
      setThreadGoal(null);
      setThreadGoalDraft("");
    } catch (error: any) {
      setThreadGoalError(error?.message || "Failed to clear thread goal.");
    } finally {
      setThreadAction(null);
    }
  }, [activeServer, hostAdapter?.id, selectedNativeThread?.id]);

  const switchBrainAdapter = useCallback(
    async (adapter: BrainAdapterRef) => {
      if (!activeServer || !adapter.id || switchingAdapterId) {
        return;
      }
      if (adapter.id === hostAdapter?.id) {
        closeAdapterSheet();
        return;
      }
      setSwitchingAdapterId(adapter.id);
      setAdapterSwitchError(null);
      try {
        await wsClient.setBrainAdapter(activeServer.id, adapter.id);
        closeAdapterSheet();
      } catch (error: any) {
        setAdapterSwitchError(error?.message || "Failed to switch adapter.");
      } finally {
        setSwitchingAdapterId(null);
      }
    },
    [activeServer, closeAdapterSheet, hostAdapter?.id, switchingAdapterId],
  );

  useEffect(() => {
    if (!threadSheetVisible || !canUseNativeThreads) {
      return;
    }
    void loadNativeThreads(undefined, false, threadSearchTerm);
  }, [canUseNativeThreads, loadNativeThreads, threadSearchTerm, threadSheetVisible]);

  useEffect(() => {
    if (!threadSheetVisible || !selectedNativeThread?.id || !canUseNativeGoals) {
      setThreadGoal(null);
      setThreadGoalDraft("");
      setThreadGoalError(null);
      setThreadGoalLoading(false);
      threadGoalRequestSeq.current += 1;
      return;
    }
    void loadNativeThreadGoal(selectedNativeThread.id);
  }, [
    canUseNativeGoals,
    loadNativeThreadGoal,
    selectedNativeThread?.id,
    threadSheetVisible,
  ]);

  useEffect(() => {
    if (!threadSheetVisible) {
      return;
    }
    setThreadSearchDraft(threadSearchTerm);
  }, [threadSearchTerm, threadSheetVisible]);

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <View style={styles.headerTitleBlock}>
          <Text style={styles.title}>Brain</Text>
          {activeBrain?.adapters?.length ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Brain adapter"
              onPress={openAdapterSheet}
              style={({ pressed }) => [
                styles.subtitleChip,
                {
                  borderColor: colors.borderSubtle,
                  backgroundColor: colors.surfaceSubtle,
                },
                pressed ? styles.subtitleChipPressed : null,
              ]}
            >
              <Text style={styles.subtitle} numberOfLines={1}>
                {subtitleLabel}
              </Text>
              <Ionicons
                name="chevron-down"
                size={13}
                color={colors.textSecondary}
              />
            </Pressable>
          ) : (
            <Text style={styles.subtitle} numberOfLines={1}>
              {subtitleLabel}
            </Text>
          )}
        </View>
        <View style={styles.headerActions}>
          {attentionItemCount > 0 ? (
            <IconButton
              icon="alert-circle-outline"
              size={36}
              iconSize={17}
              tone="ghost"
              color={colors.warning}
              accessibilityRole="button"
              accessibilityLabel="Attention queue"
              onPress={openAttentionSheet}
            />
          ) : null}
          <IconButton
            icon="add-circle-outline"
            size={36}
            iconSize={17}
            tone="ghost"
            color={colors.textSecondary}
            accessibilityRole="button"
            accessibilityLabel="New Brain chat"
            onPress={() => void startNewBrainChat()}
            disabled={!activeServer || !activeBrain?.hydrated || newChatLoading}
          />
          {canUseNativeThreads ? (
            <IconButton
              icon="git-branch-outline"
              size={36}
              iconSize={17}
              tone="ghost"
              color={colors.textSecondary}
              accessibilityRole="button"
              accessibilityLabel="Native threads"
              onPress={openThreadSheet}
            />
          ) : null}
        </View>
      </View>
      {brainActionError ? (
        <View style={styles.headerError}>
          <AppText variant="caption" tone="danger">
            {brainActionError}
          </AppText>
        </View>
      ) : null}

      <View style={styles.surface}>
        {canUseCodexBrainInterface ? (
          <CodexChatSurface
            key={`brain-codex-chat:${activeServer?.id}:${hostAgent?.id}`}
            visible
            serverId={activeServer?.id ?? ""}
            agentId={hostAgent?.id ?? ""}
            agentInfo={{
              cwd: hostAgent?.cwd,
              command: hostAgent?.command,
              name: hostAgent?.name,
            }}
            connectionState={connectionState}
            connectionIssue={connectionIssue}
            theme={terminalTheme}
            chrome={chrome}
            screenFocused
            placeholder="Message Brain"
            minimalComposer
            keyboardVerticalOffset={keyboardVerticalOffset}
            showUnavailableAction={false}
            emptyTitle="Ready"
            emptyBody="Message Brain below."
            onSwitchToTerminal={() => {}}
          />
        ) : ready ? (
          <BrainInterfaceUnavailableState
            adapterLabel={adapterLabel}
            provider={hostAdapter?.provider}
          />
        ) : (
          <BrainLoadingState
            connected={connectionState === "connected"}
            hydrated={Boolean(activeBrain?.hydrated)}
            waitingForHost={Boolean(activeBrain?.hydrated && !hostAgent?.id)}
          />
        )}
      </View>

      <BottomSheetFrame
        visible={adapterSheetVisible}
        onClose={closeAdapterSheet}
        keyboardAvoiding
        maxHeight="68%"
      >
        <View style={styles.sheetHeader}>
          <AppText variant="title" tone="primary">
            Adapters
          </AppText>
          <AppText variant="caption" tone="secondary">
            {availableAdapters.length} configured
          </AppText>
        </View>
        <View style={styles.sheetList}>
          {availableAdapters.map((adapter) => {
            const active = adapter.id === hostAdapter?.id;
            const busy = switchingAdapterId === adapter.id;
            return (
              <Pressable
                key={adapter.id}
                accessibilityRole="button"
                onPress={() => void switchBrainAdapter(adapter)}
                disabled={busy}
                style={({ pressed }) => [
                  styles.sheetRow,
                  {
                    borderColor: colors.borderSubtle,
                    backgroundColor: active
                      ? colors.surfaceActive
                      : colors.surfaceSubtle,
                  },
                  pressed && !busy ? styles.sheetRowPressed : null,
                  busy ? styles.sheetRowBusy : null,
                ]}
              >
                <View style={styles.sheetRowMain}>
                  <View style={styles.sheetRowTitleLine}>
                    <AppText variant="body" tone="primary" style={styles.sheetRowTitle}>
                      {adapter.name || adapter.id}
                    </AppText>
                    {active ? (
                      <Ionicons name="checkmark" size={16} color={colors.accent} />
                    ) : null}
                  </View>
                  <AppText variant="caption" tone="secondary">
                    {brainAdapterDetails(adapter)}
                  </AppText>
                </View>
                {busy ? (
                  <ActivityIndicator size="small" color={colors.accent} />
                ) : null}
              </Pressable>
            );
          })}
        </View>
        {adapterSwitchError ? (
          <AppText variant="caption" tone="danger" style={styles.sheetError}>
            {adapterSwitchError}
          </AppText>
        ) : null}
        <View style={styles.sheetFooter}>
          <AppButton label="Close" variant="ghost" onPress={closeAdapterSheet} />
        </View>
      </BottomSheetFrame>

      <BottomSheetFrame
        visible={attentionSheetVisible}
        onClose={closeAttentionSheet}
        keyboardAvoiding
        maxHeight="72%"
      >
        <View style={styles.threadSheetHeader}>
          <View style={styles.threadSheetHeading}>
            <AppText variant="title" tone="primary">
              Attention queue
            </AppText>
            <AppText variant="caption" tone="secondary">
              {attentionItemCount} pending · {activeBrain?.attention?.pressure || "idle"}
            </AppText>
          </View>
          <View style={styles.threadSheetActions}>
            {canUseNativeThreads && reviewAttentionItems.length > 0 ? (
              <IconButton
                icon="arrow-forward-outline"
                size={34}
                iconSize={16}
                tone="ghost"
                color={colors.textSecondary}
                accessibilityRole="button"
                accessibilityLabel="Open review queue"
                onPress={openReviewQueue}
              />
            ) : null}
          </View>
        </View>

        <ScrollView
          style={styles.threadListScroll}
          contentContainerStyle={styles.threadListContent}
          showsVerticalScrollIndicator={false}
        >
          {blockedAttentionItems.length > 0 ? (
            <View style={styles.attentionSection}>
              <AppText variant="caption" tone="secondary">
                Blocked agents
              </AppText>
              <View style={styles.attentionList}>
                {blockedAttentionItems.map((item) => (
                  <Pressable
                    key={item.id}
                    accessibilityRole="button"
                    onPress={() => openBlockedAgent(attentionQueueItemAgentID(item))}
                    style={({ pressed }) => [
                      styles.attentionRow,
                      {
                        borderColor: colors.borderSubtle,
                        backgroundColor: colors.surfaceSubtle,
                      },
                      pressed ? styles.threadRowPressed : null,
                    ]}
                  >
                    <View style={styles.attentionRowMain}>
                      <View style={styles.threadRowTitleLine}>
                        <AppText variant="body" tone="primary" style={styles.threadRowTitle}>
                          {item.title || item.agent_id || item.id}
                        </AppText>
                        <Ionicons name="alert-circle" size={13} color={colors.warning} />
                      </View>
                      <AppText variant="caption" tone="muted" style={styles.threadPreview}>
                        {brainAttentionItemMeta(item)}
                      </AppText>
                    </View>
                    <Ionicons
                      name="open-outline"
                      size={16}
                      color={colors.textSecondary}
                    />
                  </Pressable>
                ))}
              </View>
            </View>
          ) : null}

          {workAttentionItems.length > 0 ? (
            <View style={styles.attentionSection}>
              <View style={styles.attentionSectionHeader}>
                <AppText variant="caption" tone="secondary">
                  Work items
                </AppText>
                <AppText variant="caption" tone="muted">
                  {workAttentionItems.length} pending
                </AppText>
              </View>
              <View style={styles.attentionList}>
                {workAttentionItems.slice(0, 4).map((item) => (
                  <Pressable
                    key={item.id}
                    accessibilityRole="button"
                    onPress={() => openWorkItem(item)}
                    style={({ pressed }) => [
                      styles.attentionRow,
                      {
                        borderColor: colors.borderSubtle,
                        backgroundColor: colors.surfaceSubtle,
                      },
                      pressed ? styles.threadRowPressed : null,
                    ]}
                  >
                    <View style={styles.attentionRowMain}>
                      <View style={styles.threadRowTitleLine}>
                        <AppText variant="body" tone="primary" style={styles.threadRowTitle}>
                          {item.title || item.work_item_id || item.id}
                        </AppText>
                        <Ionicons
                          name={item.status === "failed" ? "close-circle" : "alert-circle"}
                          size={13}
                          color={
                            item.status === "failed"
                              ? colors.statusFailed
                              : colors.warning
                          }
                        />
                      </View>
                      <AppText variant="caption" tone="muted" style={styles.threadPreview}>
                        {brainAttentionItemMeta(item)}
                      </AppText>
                    </View>
                    <Ionicons
                      name="chevron-forward"
                      size={16}
                      color={colors.textSecondary}
                    />
                  </Pressable>
                ))}
              </View>
            </View>
          ) : null}

          {reviewAttentionCount > 0 ? (
            <View style={styles.attentionSection}>
              <View style={styles.attentionSectionHeader}>
                <AppText variant="caption" tone="secondary">
                  Review queue
                </AppText>
                <AppText variant="caption" tone="muted">
                  {reviewAttentionItems.length > 0
                    ? reviewQueueIndex >= 0
                      ? `${reviewQueueIndex + 1} of ${reviewAttentionItems.length}`
                      : `${reviewAttentionItems.length} pending`
                    : `${reviewAttentionCount} pending`}
                </AppText>
              </View>
              {canUseNativeThreads ? (
                reviewAttentionItems.length > 0 ? (
                  <View style={styles.attentionList}>
                    {reviewAttentionItems.slice(0, 4).map((item) => {
                      const threadID = attentionQueueItemThreadID(item);
                      const thread = threadID ? nativeThreadById.get(threadID) : undefined;
                      return (
                        <Pressable
                          key={item.id}
                          accessibilityRole="button"
                          onPress={() => focusReviewThread(threadID)}
                          style={({ pressed }) => [
                            styles.attentionRow,
                            {
                              borderColor:
                                threadID === selectedNativeThread?.id
                                  ? colors.accent
                                  : colors.borderSubtle,
                              backgroundColor:
                                threadID === selectedNativeThread?.id
                                  ? colors.surfaceActive
                                  : colors.surfaceSubtle,
                            },
                            pressed ? styles.threadRowPressed : null,
                          ]}
                        >
                          <View style={styles.attentionRowMain}>
                            <View style={styles.threadRowTitleLine}>
                              <AppText variant="body" tone="primary" style={styles.threadRowTitle}>
                                {thread?.title ||
                                  thread?.native_id ||
                                  item.title ||
                                  threadID ||
                                  item.id}
                              </AppText>
                              <Ionicons name="alert-circle" size={13} color={colors.warning} />
                            </View>
                            <AppText variant="caption" tone="muted" style={styles.threadPreview}>
                              {thread ? brainThreadMeta(thread) : brainAttentionItemMeta(item)}
                            </AppText>
                          </View>
                          <Ionicons
                            name="chevron-forward"
                            size={16}
                            color={colors.textSecondary}
                          />
                        </Pressable>
                      );
                    })}
                  </View>
                ) : (
                  <StateView
                    title="Review queue not loaded"
                    detail="Open native threads to batch through the pending review work."
                    style={styles.threadState}
                  />
                )
              ) : (
                <StateView
                  title="Native threads unavailable"
                  detail="Switch to an adapter that supports native threads to inspect the review queue."
                  style={styles.threadState}
                />
              )}
              {canUseNativeThreads ? (
                <View style={styles.threadMoreRow}>
                  <AppButton
                    label="Open review queue"
                    variant="primary"
                    icon="git-branch-outline"
                    onPress={openReviewQueue}
                  />
                </View>
              ) : null}
            </View>
          ) : null}
        </ScrollView>
      </BottomSheetFrame>

      <BottomSheetFrame
        visible={threadSheetVisible}
        onClose={closeThreadSheet}
        keyboardAvoiding
        maxHeight="76%"
      >
        <View style={styles.threadSheetHeader}>
          <View style={styles.threadSheetHeading}>
            <AppText variant="title" tone="primary">
              Threads
            </AppText>
            <AppText variant="caption" tone="secondary">
              {[brainProviderLabel(hostAdapter?.provider), "native"]
                .filter(Boolean)
                .join(" ")}
            </AppText>
          </View>
          <View style={styles.threadSheetActions}>
            <IconButton
              icon="refresh-outline"
              size={34}
              iconSize={16}
              tone="ghost"
              color={colors.textSecondary}
              accessibilityRole="button"
              accessibilityLabel="Refresh threads"
              onPress={refreshNativeThreads}
              disabled={threadsLoading}
            />
          </View>
        </View>

        {canSearchNativeThreads ? (
          <View style={styles.threadSearchRow}>
            <View
              style={[
                styles.threadSearchField,
                {
                  borderColor: colors.borderSubtle,
                  backgroundColor: colors.inputBackground,
                },
              ]}
            >
              <Ionicons
                name="search-outline"
                size={15}
                color={colors.textSecondary}
              />
              <TextInput
                style={[styles.threadSearchInput, { color: colors.textPrimary }]}
                value={threadSearchDraft}
                onChangeText={setThreadSearchDraft}
                onSubmitEditing={submitNativeThreadSearch}
                placeholder="Search threads"
                placeholderTextColor={colors.textSecondary}
                returnKeyType="search"
                autoCapitalize="none"
                autoCorrect={false}
                editable={!threadsLoading}
              />
              {threadSearchDraft ? (
                <IconButton
                  icon="close-circle-outline"
                  size={28}
                  iconSize={15}
                  tone="ghost"
                  color={colors.textSecondary}
                  accessibilityRole="button"
                  accessibilityLabel="Clear thread search"
                  onPress={clearNativeThreadSearch}
                  disabled={threadsLoading}
                />
              ) : null}
            </View>
            <IconButton
              icon="search-outline"
              size={34}
              iconSize={16}
              tone="ghost"
              color={colors.textSecondary}
              accessibilityRole="button"
              accessibilityLabel="Search threads"
              onPress={submitNativeThreadSearch}
              disabled={threadsLoading}
            />
          </View>
        ) : null}

        <ScrollView
          style={styles.threadListScroll}
          contentContainerStyle={styles.threadListContent}
          showsVerticalScrollIndicator={false}
        >
          {threadsLoading && nativeThreads.length === 0 ? (
            <StateView loading title="Loading threads" style={styles.threadState} />
          ) : threadsError ? (
            <StateView
              danger
              title="Could not load threads"
              detail={threadsError}
              style={styles.threadState}
            />
          ) : nativeThreads.length === 0 ? (
            <StateView
              title="No threads"
              detail={
                threadSearchTerm
                  ? "No native threads matched this search."
                  : "This adapter did not return any native threads yet."
              }
              style={styles.threadState}
            />
          ) : (
            nativeThreads.map((thread) => {
              const selected = thread.id === selectedNativeThreadId;
              return (
                <Pressable
                  key={thread.id}
                  accessibilityRole="button"
                  onPress={() => setSelectedNativeThreadId(thread.id)}
                  style={({ pressed }) => [
                    styles.threadRow,
                    {
                      borderColor: selected
                        ? colors.accent
                        : colors.borderSubtle,
                      backgroundColor: selected
                        ? colors.surfaceActive
                        : colors.surfaceSubtle,
                    },
                    pressed ? styles.threadRowPressed : null,
                  ]}
                >
                  <View style={styles.threadRowMain}>
                    <View style={styles.threadRowTitleLine}>
                      <AppText variant="body" tone="primary" style={styles.threadRowTitle}>
                        {thread.title || thread.native_id || thread.id}
                      </AppText>
                      {thread.pinned ? (
                        <Ionicons name="star" size={13} color={colors.accent} />
                      ) : null}
                      {threadNeedsReview(thread) ? (
                        <Ionicons
                          name="alert-circle"
                          size={13}
                          color={colors.warning}
                        />
                      ) : null}
                      {thread.archived ? (
                        <AppText variant="caption" tone="secondary">
                          Archived
                        </AppText>
                      ) : null}
                    </View>
                    {thread.snippet || thread.preview ? (
                      <AppText variant="caption" tone="secondary" style={styles.threadPreview}>
                        {thread.snippet || thread.preview}
                      </AppText>
                    ) : null}
                    <AppText variant="caption" tone="muted">
                      {brainThreadMeta(thread)}
                    </AppText>
                  </View>
                </Pressable>
              );
            })
          )}

          {nativeThreadsNextCursor ? (
            <View style={styles.threadMoreRow}>
              <AppButton
                label={threadsLoading ? "Loading" : "Load more"}
                variant="ghost"
                onPress={loadMoreNativeThreads}
                disabled={threadsLoading}
              />
            </View>
          ) : null}

          {selectedNativeThread ? (
            <View style={styles.threadDetailCard}>
              <View style={styles.threadDetailHeader}>
                <AppText variant="body" tone="primary" style={styles.threadDetailTitle}>
                  {selectedNativeThread.title ||
                    selectedNativeThread.native_id ||
                    selectedNativeThread.id}
                </AppText>
                <View style={styles.threadDetailActions}>
                  {threadAction ? (
                    <ActivityIndicator size="small" color={colors.accent} />
                  ) : null}
                  <IconButton
                    icon={selectedNativeThread.pinned ? "star" : "star-outline"}
                    size={30}
                    iconSize={15}
                    tone="ghost"
                    color={
                      selectedNativeThread.pinned
                        ? colors.accent
                        : colors.textSecondary
                    }
                    accessibilityRole="button"
                    accessibilityLabel={
                      selectedNativeThread.pinned
                        ? "Unpin thread"
                        : "Pin thread"
                    }
                    onPress={() => void toggleNativeThreadPin(selectedNativeThread)}
                    disabled={threadsLoading || Boolean(threadAction)}
                  />
                  <IconButton
                    icon={
                      threadNeedsReview(selectedNativeThread)
                        ? "alert-circle"
                        : "alert-circle-outline"
                    }
                    size={30}
                    iconSize={15}
                    tone="ghost"
                    color={
                      threadNeedsReview(selectedNativeThread)
                        ? colors.warning
                        : colors.textSecondary
                    }
                    accessibilityRole="button"
                    accessibilityLabel={
                      threadNeedsReview(selectedNativeThread)
                        ? "Remove from review queue"
                        : "Mark for review"
                    }
                    onPress={() => void toggleNativeThreadReview(selectedNativeThread)}
                    disabled={threadsLoading || Boolean(threadAction)}
                  />
                  <IconButton
                    icon="refresh-outline"
                    size={30}
                    iconSize={15}
                    tone="ghost"
                    color={colors.textSecondary}
                    accessibilityRole="button"
                    accessibilityLabel="Refresh thread details"
                    onPress={() => void refreshNativeThreadDetail(selectedNativeThread)}
                    disabled={threadsLoading || Boolean(threadAction)}
                  />
                  {reviewAttentionItems.length > 0 ? (
                    <IconButton
                      icon="arrow-forward-outline"
                      size={30}
                      iconSize={15}
                      tone="ghost"
                      color={colors.textSecondary}
                      accessibilityRole="button"
                      accessibilityLabel="Next review thread"
                      onPress={selectNextReviewThread}
                      disabled={threadsLoading || Boolean(threadAction)}
                    />
                  ) : null}
                  {canResumeNativeThreads ? (
                    <IconButton
                      icon="log-in-outline"
                      size={30}
                      iconSize={15}
                      tone="ghost"
                      color={colors.textSecondary}
                      accessibilityRole="button"
                      accessibilityLabel="Resume thread in Brain"
                      onPress={() => void resumeNativeThread(selectedNativeThread)}
                      disabled={threadsLoading || Boolean(threadAction)}
                    />
                  ) : null}
                  {canForkNativeThreads ? (
                    <IconButton
                      icon="git-branch-outline"
                      size={30}
                      iconSize={15}
                      tone="ghost"
                      color={colors.textSecondary}
                      accessibilityRole="button"
                      accessibilityLabel="Fork thread"
                      onPress={() => void forkNativeThread(selectedNativeThread)}
                      disabled={threadsLoading || Boolean(threadAction)}
                    />
                  ) : null}
                  <AppText
                    variant="caption"
                    tone="secondary"
                    numberOfLines={1}
                    style={styles.threadDetailStatus}
                  >
                    {selectedNativeThread.archived ? "Archived" : "Active"}
                  </AppText>
                  {canArchiveNativeThreads ? (
                    <IconButton
                      icon="archive-outline"
                      size={30}
                      iconSize={15}
                      tone="ghost"
                      color={colors.textSecondary}
                      accessibilityRole="button"
                      accessibilityLabel={
                        selectedNativeThread.archived
                          ? "Unarchive thread"
                          : "Archive thread"
                      }
                      onPress={() =>
                        void toggleNativeThreadArchive(selectedNativeThread)
                      }
                      disabled={threadsLoading || Boolean(threadAction)}
                    />
                  ) : null}
                </View>
              </View>
              {selectedNativeThread.snippet || selectedNativeThread.preview ? (
                <AppText variant="caption" tone="secondary" style={styles.threadDetailPreview}>
                  {selectedNativeThread.snippet || selectedNativeThread.preview}
                </AppText>
              ) : null}
              <AppText variant="caption" tone="muted" style={styles.threadDetailMeta}>
                {brainThreadDetailMeta(selectedNativeThread)}
              </AppText>
              {canUseNativeGoals ? (
                <View style={styles.threadGoalBlock}>
                  <View style={styles.threadGoalHeader}>
                    <AppText variant="caption" tone="secondary">
                      Goal
                    </AppText>
                    {threadGoalLoading ? (
                      <ActivityIndicator size="small" color={colors.textSecondary} />
                    ) : null}
                  </View>
                  <TextInput
                    style={[
                      styles.threadGoalInput,
                      {
                        borderColor: colors.borderSubtle,
                        backgroundColor: colors.inputBackground,
                        color: colors.textPrimary,
                      },
                    ]}
                    value={threadGoalDraft}
                    onChangeText={setThreadGoalDraft}
                    placeholder="Objective"
                    placeholderTextColor={colors.textSecondary}
                    multiline
                    textAlignVertical="top"
                    autoCapitalize="sentences"
                    autoCorrect={false}
                    editable={!threadGoalLoading && threadAction !== "goal"}
                  />
                  {threadGoal ? (
                    <AppText variant="caption" tone="muted" style={styles.threadGoalMeta}>
                      {brainGoalMeta(threadGoal)}
                    </AppText>
                  ) : (
                    <AppText variant="caption" tone="secondary" style={styles.threadGoalMeta}>
                      No native goal set.
                    </AppText>
                  )}
                  {threadGoalError ? (
                    <AppText variant="caption" tone="danger" style={styles.threadGoalError}>
                      {threadGoalError}
                    </AppText>
                  ) : null}
                  <View style={styles.threadGoalActions}>
                    <AppButton
                      label="Set goal"
                      variant="primary"
                      icon="flag-outline"
                      onPress={() => void saveNativeThreadGoal()}
                      disabled={
                        threadGoalLoading ||
                        threadAction === "goal" ||
                        !threadGoalDraft.trim()
                      }
                      style={styles.threadGoalButton}
                    />
                    <AppButton
                      label="Clear"
                      variant="ghost"
                      onPress={() => void clearNativeThreadGoal()}
                      disabled={
                        threadGoalLoading ||
                        threadAction === "goal" ||
                        !threadGoal
                      }
                      style={styles.threadGoalButton}
                    />
                  </View>
                </View>
              ) : null}
            </View>
          ) : null}
        </ScrollView>
        <View style={styles.threadSheetFooter}>
          <AppButton label="Close" variant="ghost" onPress={closeThreadSheet} />
        </View>
      </BottomSheetFrame>
    </SafeAreaView>
  );
}

function BrainLoadingState({
  connected,
  hydrated,
  waitingForHost,
}: {
  connected: boolean;
  hydrated: boolean;
  waitingForHost?: boolean;
}) {
  const colors = useAppColors();
  return (
    <View style={loadingStyles.root}>
      {connected ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Ionicons name="cloud-offline-outline" size={22} color={colors.textSecondary} />
      )}
      <Text style={[loadingStyles.title, { color: colors.textPrimary }]}>
        {connected ? "Starting Brain" : "Offline"}
      </Text>
      <Text style={[loadingStyles.body, { color: colors.textSecondary }]}>
        {connected && waitingForHost
          ? "Getting your assistant ready."
          : connected && hydrated
            ? "Preparing your chat."
          : connected
            ? "Syncing Brain."
            : "Connect to a server to use Brain."}
      </Text>
    </View>
  );
}

function BrainInterfaceUnavailableState({
  adapterLabel,
  provider,
}: {
  adapterLabel: string;
  provider?: string;
}) {
  const colors = useAppColors();
  const label = adapterLabel || brainProviderLabel(provider);
  return (
    <View style={loadingStyles.root}>
      <Ionicons name="layers-outline" size={22} color={colors.textSecondary} />
      <Text style={[loadingStyles.title, { color: colors.textPrimary }]}>
        Interface unavailable
      </Text>
      <Text style={[loadingStyles.body, { color: colors.textSecondary }]}>
        {label
          ? `${label} does not expose a structured Brain interface yet.`
          : "This adapter does not expose a structured Brain interface yet."}
      </Text>
    </View>
  );
}

function resolveActiveServer({
  connectedServers,
  servers,
  brainByServer,
  connectionStates,
}: {
  connectedServers: StoredServer[];
  servers: StoredServer[];
  brainByServer: Record<string, BrainServerState>;
  connectionStates: Record<string, ConnectionState>;
}): StoredServer | null {
  const hydratedConnected = connectedServers.find(
    (server) => brainByServer[server.id]?.hydrated,
  );
  if (hydratedConnected) {
    return hydratedConnected;
  }
  if (connectedServers[0]) {
    return connectedServers[0];
  }
  const connectedByState = servers.find(
    (server) => connectionStates[server.id] === "connected",
  );
  return connectedByState || servers[0] || null;
}

function brainStatusLabel({
  activeServer,
  connectionState,
  activeBrain,
}: {
  activeServer: StoredServer | null;
  connectionState: ConnectionState;
  activeBrain: BrainServerState | null;
}) {
  if (!activeServer) {
    return "Offline";
  }
  if (connectionState !== "connected") {
    return "Offline";
  }
  if (!activeBrain?.hydrated) {
    return "Syncing";
  }
  if (!activeBrain.host_agent?.id) {
    return "Starting";
  }
  return "Ready";
}

function brainAdapterLabel(adapter?: BrainAdapterRef | null) {
  if (!adapter) {
    return "";
  }
  const provider =
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.name || adapter.id;
  const providerLabel = brainProviderLabel(provider);
  const runtime = adapter.runtime?.trim();
  return [providerLabel, runtime].filter(Boolean).join(" · ");
}

function brainAdapterDetails(adapter: BrainAdapterRef) {
  const provider = brainProviderLabel(
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.name || adapter.id,
  );
  const runtime = adapter.runtime?.trim();
  const command = adapter.command?.trim();
  return [provider, runtime, command].filter(Boolean).join(" · ");
}

function brainAttentionLabel(attention?: BrainServerState["attention"]) {
  const parts: string[] = [];
  const blocked = attention?.blocked_agents ?? 0;
  if (blocked > 0) {
    parts.push(`${blocked} blocked`);
  }
  const reviewQueue = attention?.review_queue ?? 0;
  if (reviewQueue > 0) {
    parts.push(`${reviewQueue} review`);
  }
  const active = attention?.active_agents ?? 0;
  if (parts.length < 2 && active > 0) {
    parts.push(`${active} active`);
  }
  if (parts.length > 0) {
    return parts.slice(0, 2).join(" · ");
  }
  const pinned = attention?.pinned ?? 0;
  if (pinned > 0) {
    return `${pinned} pinned`;
  }
  return "";
}

function attentionQueueItemAgentID(item?: BrainAttentionQueueItem | null) {
  if (!item) {
    return "";
  }
  return item.agent_id?.trim() || item.id.replace(/^agent:/, "");
}

function attentionQueueItemThreadID(item?: BrainAttentionQueueItem | null) {
  if (!item) {
    return "";
  }
  return item.thread_id?.trim() || item.id.replace(/^thread:/, "");
}

function attentionQueueItemWorkID(item?: BrainAttentionQueueItem | null) {
  if (!item) {
    return "";
  }
  return item.work_item_id?.trim() || item.id.replace(/^work:/, "");
}

function brainBlockedAgentToQueueItem(agent: BrainAgentRef): BrainAttentionQueueItem {
  return {
    id: `agent:${agent.id}`,
    kind: "blocked_agent",
    title: agent.name || agent.id,
    summary: agent.summary,
    agent_id: agent.id,
    status: agent.status,
    cwd: agent.cwd,
    command: agent.command,
    updated_at: agent.updated_at,
  };
}

function brainReviewThreadToQueueItem(thread: BrainNativeThread): BrainAttentionQueueItem {
  return {
    id: `thread:${thread.id}`,
    kind: "review_thread",
    title: thread.title || thread.native_id || thread.id,
    summary: thread.review_state === "reviewing" ? "Reviewing" : "Needs review",
    thread_id: thread.id,
    review_state: thread.review_state,
    pinned: thread.pinned,
    updated_at: thread.updated_at,
  };
}

function brainWorkItemNeedsAttention(item: WorkItem) {
  if (item.frontmatter.done) {
    return false;
  }
  const status = brainWorkItemAttentionStatus(item);
  return status === "blocked" || status === "failed" || Boolean(item.frontmatter.ai_error?.trim());
}

function brainWorkItemAttentionStatus(item: WorkItem) {
  const status =
    typeof item.frontmatter.status === "string"
      ? item.frontmatter.status.trim().toLowerCase()
      : "";
  if (status === "blocked" || status === "failed") {
    return status;
  }
  if (item.frontmatter.ai_error?.trim()) {
    return "failed";
  }
  return status || "unknown";
}

function brainWorkItemToQueueItem(item: WorkItem): BrainAttentionQueueItem {
  const status = brainWorkItemAttentionStatus(item);
  return {
    id: `work:${item.id}`,
    kind: "work_item",
    title:
      item.frontmatter.title?.trim() ||
      item.title?.trim() ||
      item.frontmatter.summary?.trim() ||
      item.id,
    summary:
      item.frontmatter.ai_error?.trim() ||
      item.frontmatter.friction?.trim() ||
      item.frontmatter.summary?.trim() ||
      item.frontmatter.next?.trim() ||
      undefined,
    work_item_id: item.id,
    status,
    project: item.project,
    cwd: item.frontmatter.cwd,
    command: item.frontmatter.command,
    path: item.path,
    updated_at: item.mtime || item.frontmatter.ai_updated || item.frontmatter.created,
  };
}

function compareAttentionQueueItems(left: BrainAttentionQueueItem, right: BrainAttentionQueueItem) {
  const leftPriority = brainAttentionQueuePriority(left);
  const rightPriority = brainAttentionQueuePriority(right);
  if (leftPriority !== rightPriority) {
    return leftPriority - rightPriority;
  }
  const leftUpdated = Date.parse(left.updated_at || "");
  const rightUpdated = Date.parse(right.updated_at || "");
  return (Number.isNaN(rightUpdated) ? 0 : rightUpdated) -
    (Number.isNaN(leftUpdated) ? 0 : leftUpdated);
}

function brainAttentionQueuePriority(item: BrainAttentionQueueItem) {
  switch (item.kind) {
    case "blocked_agent":
      return 0;
    case "work_item":
      return item.status === "failed" ? 1 : 2;
    case "review_thread":
      return 3;
    default:
      return 9;
  }
}

function brainAttentionItemMeta(item: BrainAttentionQueueItem) {
  const parts: string[] = [];
  if (item.kind === "blocked_agent") {
    if (item.status) {
      parts.push(item.status === "blocked" ? "Blocked" : item.status);
    }
    if (item.summary) {
      parts.push(item.summary);
    }
    if (item.cwd) {
      parts.push(item.cwd);
    }
    if (item.command) {
      parts.push(item.command);
    }
    return parts.join(" · ");
  }
  if (item.kind === "work_item") {
    if (item.status) {
      parts.push(item.status === "failed" ? "Failed" : item.status === "blocked" ? "Blocked" : item.status);
    }
    if (item.summary) {
      parts.push(item.summary);
    }
    if (item.project) {
      parts.push(item.project);
    }
    if (item.cwd) {
      parts.push(item.cwd);
    }
    return parts.join(" · ");
  }
  if (item.review_state) {
    parts.push(item.review_state === "reviewing" ? "Reviewing" : "Needs review");
  } else if (item.summary) {
    parts.push(item.summary);
  }
  if (item.pinned) {
    parts.push("Pinned");
  }
  if (item.thread_id) {
    parts.push(item.thread_id);
  }
  return parts.join(" · ");
}

function brainThreadMeta(thread: BrainNativeThread) {
  const parts: string[] = [];
  if (thread.pinned) {
    parts.push("Pinned");
  }
  if (threadNeedsReview(thread)) {
    parts.push(thread.review_state === "reviewing" ? "Reviewing" : "Needs review");
  }
  const provider = brainProviderLabel(thread.provider || thread.model_provider || thread.source);
  if (provider) {
    parts.push(provider);
  }
  if (thread.cwd) {
    parts.push(thread.cwd);
  }
  if (thread.status) {
    parts.push(thread.status);
  }
  return parts.join(" · ");
}

function brainThreadDetailMeta(thread: BrainNativeThread) {
  const parts = [brainThreadMeta(thread)];
  if (thread.session_id) {
    parts.push(`session ${thread.session_id}`);
  }
  if (thread.path) {
    parts.push(thread.path);
  }
  return parts.filter(Boolean).join(" · ");
}

function brainGoalMeta(goal: BrainNativeThreadGoal) {
  const parts: string[] = [];
  if (goal.status) {
    parts.push(goal.status);
  }
  if (
    typeof goal.tokens_used === "number" ||
    typeof goal.token_budget === "number"
  ) {
    const used = typeof goal.tokens_used === "number" ? goal.tokens_used : 0;
    parts.push(
      typeof goal.token_budget === "number"
        ? `${used}/${goal.token_budget} tokens`
        : `${used} tokens`,
    );
  }
  if (typeof goal.time_used_seconds === "number" && goal.time_used_seconds > 0) {
    parts.push(formatGoalDuration(goal.time_used_seconds));
  }
  return parts.join(" · ");
}

function formatGoalDuration(seconds: number) {
  const rounded = Math.max(0, Math.round(seconds));
  if (rounded < 60) {
    return `${rounded}s`;
  }
  const minutes = Math.floor(rounded / 60);
  const rest = rounded % 60;
  if (minutes < 60) {
    return rest > 0 ? `${minutes}m ${rest}s` : `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  const minuteRest = minutes % 60;
  return minuteRest > 0 ? `${hours}h ${minuteRest}m` : `${hours}h`;
}

function brainProviderLabel(value?: string) {
  const normalized = value?.trim().toLowerCase();
  switch (normalized) {
    case "codex":
      return "Codex";
    case "claude":
      return "Claude Code";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "";
  }
}

function sortBrainNativeThreadsForAttention(threads: BrainNativeThread[]) {
  const pinned: BrainNativeThread[] = [];
  const review: BrainNativeThread[] = [];
  const rest: BrainNativeThread[] = [];
  for (const thread of threads) {
    if (thread.pinned) {
      pinned.push(thread);
    } else if (threadNeedsReview(thread)) {
      review.push(thread);
    } else {
      rest.push(thread);
    }
  }
  return [...pinned, ...review, ...rest];
}

function threadNeedsReview(thread?: BrainNativeThread | null) {
  const state = thread?.review_state?.trim();
  return state === "needs_review" || state === "reviewing";
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    header: {
      minHeight: 58,
      paddingHorizontal: 16,
      paddingTop: 6,
      paddingBottom: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    headerTitleBlock: {
      flex: 1,
      minWidth: 0,
      paddingRight: 12,
    },
    headerActions: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
    },
    headerError: {
      paddingHorizontal: 16,
      paddingVertical: 7,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
      backgroundColor: colors.surfaceSubtle,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 22,
      lineHeight: 27,
      letterSpacing: 0,
    },
    subtitle: {
      marginTop: 0,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: 16,
      flexShrink: 1,
    },
    subtitleChip: {
      marginTop: 2,
      alignSelf: "flex-start",
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      paddingHorizontal: 8,
      paddingVertical: 4,
      borderRadius: 999,
      borderWidth: StyleSheet.hairlineWidth,
    },
    subtitleChipPressed: {
      opacity: 0.7,
    },
    surface: {
      flex: 1,
      minHeight: 0,
      backgroundColor: colors.bgPrimary,
    },
    sheetHeader: {
      marginBottom: 12,
    },
    sheetList: {
      gap: 8,
    },
    sheetRow: {
      minHeight: 56,
      borderRadius: 14,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 14,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 12,
    },
    sheetRowPressed: {
      opacity: 0.75,
    },
    sheetRowBusy: {
      opacity: 0.55,
    },
    sheetRowMain: {
      flex: 1,
      minWidth: 0,
    },
    sheetRowTitleLine: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    sheetRowTitle: {
      flex: 1,
      minWidth: 0,
    },
    sheetError: {
      marginTop: 10,
    },
    sheetFooter: {
      marginTop: 12,
      alignItems: "flex-end",
    },
    threadSheetHeader: {
      marginBottom: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 12,
    },
    threadSheetHeading: {
      flex: 1,
      minWidth: 0,
    },
    threadSheetActions: {
      flexDirection: "row",
      alignItems: "center",
    },
    threadSearchRow: {
      marginBottom: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    threadSearchField: {
      minHeight: 38,
      flex: 1,
      minWidth: 0,
      borderRadius: 10,
      borderWidth: StyleSheet.hairlineWidth,
      paddingLeft: 10,
      paddingRight: 4,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    threadSearchInput: {
      flex: 1,
      minWidth: 0,
      paddingVertical: 8,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 18,
      letterSpacing: 0,
    },
    threadListScroll: {
      maxHeight: 470,
    },
    threadListContent: {
      gap: 8,
      paddingBottom: 8,
    },
    threadState: {
      minHeight: 170,
    },
    attentionSection: {
      gap: 8,
    },
    attentionSectionHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    attentionList: {
      gap: 8,
    },
    attentionRow: {
      minHeight: 66,
      borderRadius: 12,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 12,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 10,
    },
    attentionRowMain: {
      flex: 1,
      minWidth: 0,
    },
    threadRow: {
      minHeight: 68,
      borderRadius: 12,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    threadRowPressed: {
      opacity: 0.75,
    },
    threadRowMain: {
      minWidth: 0,
    },
    threadRowTitleLine: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    threadRowTitle: {
      flex: 1,
      minWidth: 0,
    },
    threadPreview: {
      marginTop: 4,
      maxHeight: 54,
      overflow: "hidden",
    },
    threadMoreRow: {
      alignItems: "center",
      paddingVertical: 4,
    },
    threadDetailCard: {
      marginTop: 4,
      borderRadius: 12,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      backgroundColor: colors.bgElevated,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    threadDetailHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 10,
    },
    threadDetailTitle: {
      flex: 1,
      minWidth: 0,
    },
    threadDetailActions: {
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
    },
    threadDetailStatus: {
      flexShrink: 1,
    },
    threadDetailPreview: {
      marginTop: 6,
    },
    threadDetailMeta: {
      marginTop: 8,
    },
    threadGoalBlock: {
      marginTop: 10,
      paddingTop: 10,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
      gap: 7,
    },
    threadGoalHeader: {
      minHeight: 18,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    threadGoalInput: {
      minHeight: 42,
      maxHeight: 96,
      borderRadius: 10,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 10,
      paddingVertical: 8,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 18,
      letterSpacing: 0,
    },
    threadGoalMeta: {
      marginTop: 1,
    },
    threadGoalError: {
      marginTop: 1,
    },
    threadGoalActions: {
      marginTop: 2,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "flex-end",
      gap: 8,
    },
    threadGoalButton: {
      minHeight: 34,
      paddingHorizontal: 10,
    },
    threadSheetFooter: {
      marginTop: 10,
      alignItems: "flex-end",
    },
  });
}

const loadingStyles = StyleSheet.create({
  root: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 28,
  },
  title: {
    marginTop: 14,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
  },
  body: {
    marginTop: 6,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    lineHeight: 19,
    textAlign: "center",
  },
});
