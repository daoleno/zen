import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  type ListRenderItem,
  SectionList,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import { useFocusEffect, useIsFocused, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, {
  runOnJS,
  useAnimatedScrollHandler,
  useSharedValue,
} from "react-native-reanimated";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { Worker, useWorkers } from "../../store/workers";
import { useCurrentServer } from "../../store/currentServer";
import { selectCurrentServerItems } from "../../services/currentServerSelection";
import { useWork, type WorkItem } from "../../store/work";
import {
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppColors,
  useAppTheme,
  shadow,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import { usePrimaryPageAction } from "../../components/navigation/PrimaryPageAction";
import { resolvePrimaryAppBarGeometry } from "../../components/navigation/PrimaryDrawerShell";
import { AnimatedPressable } from "../../components/ui/AnimatedPressable";
import { WorkSignalObservatory } from "../../components/work/WorkSignalObservatory";
import { WorkSignalPullPreview } from "../../components/work/WorkSignalPullPreview";
import {
  createWorkObservatoryAccessibilityProps,
  resolveWorkObservatoryPullIntent,
  shouldRevealWorkObservatory,
  WORK_OBSERVATORY_PULL,
} from "../../components/work/workSignalObservatoryInteraction";
import { RisingSheet } from "../../components/ui/RisingSheet";
import { CompactEmptyState } from "../../components/ui/CompactEmptyState";
import { sessionEmptyState } from "../../services/sessionEmptyState";
import { WorkerListRowContainer } from "../../components/workers/WorkerListRowContainer";
import { WorkerSessionSelectionBar } from "../../components/workers/WorkerSessionSelectionBar";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { SessionServicesSheet } from "../../components/SessionServicesSheet";
import { usePrimarySelectionBar } from "../../components/navigation/PrimarySelectionBar";
import { usePrimaryDrawerBack } from "../../components/navigation/usePrimaryDrawerBack";
import {
  getWorkerAliases,
  markWorkerOpened,
  StoredWorkerAliases,
  setServerAutoConnect,
} from "../../services/storage";
import { connectionIssueAccent } from "../../services/connectionIssue";
import { wsClient } from "../../services/websocket";
import {
  blockCreateAfterAmbiguity,
  bumpWorkerSessionListReceipt,
  clearCreateAmbiguityForServer,
  isCreateBlockedByAmbiguity,
  reconcileCreateSessionFailure,
  reconcileCreateSessionSuccess,
  shouldUnlockCreateAfterAmbiguity,
  type CreateAmbiguityGateState,
} from "../../services/providers";
import { isWorkerSessionListFreshForConnection } from "../../store/workers";
import { makeSessionKey } from "../../services/sessionKeys";
import { presentWorker } from "../../services/workerPresentation";
import {
  addSessionToSelection,
  countSessionSelection,
  EMPTY_SESSION_SELECTION,
  isSessionTerminable,
  pruneSessionSelection,
  removeSessionsFromSelection,
  toggleSessionSelection,
  type SessionSelection,
} from "../../services/sessionSelection";
import {
  createSessionTerminationEntries,
  SessionTerminationBatch,
  sessionTerminationConfirmMessage,
  sessionTerminationSummaryMessage,
  type SessionTerminationSummary,
} from "../../services/sessionBulkTerminate";
import {
  groupWorkersByDirectory,
  type WorkerDirectorySection,
} from "../../services/workerDirectory";
import {
  serviceProjectLabel,
  type DiscoveredSessionService,
} from "../../services/sessionServicesPresentation";

const AnimatedSectionList = Animated.createAnimatedComponent(
  SectionList<Worker, WorkerDirectorySection>,
);
const workerKeyExtractor = (agent: Worker) => agent.key;

export default function InboxScreen() {
  const { state } = useWorkers();
  const { currentServer, currentServerId, hydrated: storageHydrated, isCurrentServer } = useCurrentServer();
  const servicesRequestEpochRef = useRef(0);
  const createInFlightRef = useRef(false);
  const displayWorkers = useMemo(
    () => selectCurrentServerItems(state.workers, currentServerId),
    [state.workers, currentServerId],
  );
  const { state: workState } = useWork();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;
  const { width: viewportWidth } = useWindowDimensions();
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);

  const workerWorkMap = useMemo(() => {
    const map: Record<string, WorkItem> = {};
    for (const current of Object.values(workState.byKey)) {
      if (current.serverId !== currentServerId || current.frontmatter.done || !current.frontmatter.worker_session) {
        continue;
      }
      map[`${current.serverId}:${current.frontmatter.worker_session}`] = current;
    }
    return map;
  }, [workState.byKey, currentServerId]);
  const [headerMenuVisible, setHeaderMenuVisible] = useState(false);
  const [workerAliases, setWorkerAliases] = useState<StoredWorkerAliases>({});
  const [createSheetVisible, setCreateSheetVisible] = useState(false);
  const [creatingServerId, setCreatingServerId] = useState<string | null>(null);
  const [createAmbiguityBlocks, setCreateAmbiguityBlocks] =
    useState<CreateAmbiguityGateState>({});
  const [workerSessionListReceiptByServer, setWorkerSessionListReceiptByServer] =
    useState<Record<string, number>>({});
  const [serviceSheetVisible, setServiceSheetVisible] = useState(false);
  const [sessionServices, setSessionServices] = useState<
    DiscoveredSessionService[]
  >([]);
  const [servicesLoading, setServicesLoading] = useState(false);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const currentServices = useMemo(
    () => selectCurrentServerItems(sessionServices, currentServerId),
    [sessionServices, currentServerId],
  );
  const [workObservatoryVisible, setWorkObservatoryVisible] = useState(false);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedKeys, setSelectedKeys] = useState<SessionSelection>(
    EMPTY_SESSION_SELECTION,
  );
  const [terminationRunning, setTerminationRunning] = useState(false);
  const terminationBatchRef = useRef<SessionTerminationBatch | null>(null);
  const submittedKeysRef = useRef<string[]>([]);
  const submittedServerIdRef = useRef<string | null>(null);
  const workObservatoryPullDistance = useSharedValue(0);
  const sessionListScrollOffsetY = useSharedValue(0);
  const workObservatoryTouchStartX = useSharedValue(0);
  const workObservatoryTouchStartY = useSharedValue(0);
  const workObservatoryGestureActivated = useSharedValue(0);
  const agentsHydrated = Boolean(currentServerId && state.hydratedServers[currentServerId]);

  useEffect(() => {
    servicesRequestEpochRef.current += 1;
    setServiceSheetVisible(false);
    setSessionServices([]);
    setServicesError(null);
    setServicesLoading(false);
    setCreateSheetVisible(false);
    setWorkObservatoryVisible(false);
    setSelectionMode(false);
    setSelectedKeys(EMPTY_SESSION_SELECTION);
  }, [currentServerId]);

  const agentsByKey = useMemo(() => {
    const byKey: Record<string, Worker> = {};
    for (const agent of displayWorkers) {
      byKey[agent.key] = agent;
    }
    return byKey;
  }, [displayWorkers]);
  const agentsByKeyRef = useRef(agentsByKey);
  agentsByKeyRef.current = agentsByKey;
  const workerAliasesRef = useRef(workerAliases);
  workerAliasesRef.current = workerAliases;

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      (async () => {
        const storedAliases = await getWorkerAliases();
        if (!cancelled) {
          setWorkerAliases(storedAliases);
        }
      })();
      return () => {
        cancelled = true;
      };
    }, []),
  );

  const listSections = useMemo(
    () => groupWorkersByDirectory(displayWorkers),
    [displayWorkers],
  );
  const sortedWorkers = useMemo(
    () => listSections.flatMap((section) => section.data),
    [listSections],
  );

  const showServerNames = false;
  const hasConfiguredServers = currentServer !== null;
  const connectionState = currentServerId ? state.serverConnections[currentServerId] : undefined;
  const hasConnection = connectionState !== undefined;
  const anyConnected = connectionState === "connected";
  const anyConnecting = connectionState === "connecting";
  const waitingForInitialWorkerSnapshot =
    storageHydrated &&
    anyConnected && !agentsHydrated;
  const shouldShowInitialLoading =
    (!storageHydrated && sortedWorkers.length === 0) ||
    (!agentsHydrated &&
      sortedWorkers.length === 0 &&
      hasConfiguredServers &&
      waitingForInitialWorkerSnapshot);
  const useSectionHeaders = listSections.length > 1;
  const primaryIssue = currentServerId ? state.serverConnectionIssues[currentServerId] ?? null : null;

  const openWorker = useCallback(
    (agent: Worker) => {
      if (!isCurrentServer(agent.serverId)) return;
      const openedAt = Date.now();
      void markWorkerOpened(agent.key, openedAt);
      router.push({
        pathname: "/terminal/[id]",
        params: { id: agent.id, serverId: agent.serverId },
      });
    },
    [router, isCurrentServer],
  );

  const exitSelectionMode = useCallback(() => {
    if (terminationRunning) {
      return;
    }
    setSelectionMode(false);
    setSelectedKeys(EMPTY_SESSION_SELECTION);
  }, [terminationRunning]);

  const enterSelectionMode = useCallback(
    (agent: Worker) => {
      if (terminationRunning || selectionMode) {
        return;
      }
      if (!isSessionTerminable(agent, state.serverConnections)) {
        return;
      }
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
      setSelectionMode(true);
      setSelectedKeys(
        addSessionToSelection(EMPTY_SESSION_SELECTION, agent.key),
      );
    },
    [selectionMode, state.serverConnections, terminationRunning],
  );

  const toggleSelection = useCallback(
    (agent: Worker) => {
      if (!selectionMode || terminationRunning) {
        return;
      }
      if (!isSessionTerminable(agent, state.serverConnections)) {
        return;
      }
      setSelectedKeys((current) =>
        toggleSessionSelection(current, agent.key),
      );
    },
    [selectionMode, state.serverConnections, terminationRunning],
  );

  const handleBatchSettled = useCallback(
    (summary: SessionTerminationSummary) => {
      if (summary.pending > 0) {
        return;
      }
      terminationBatchRef.current?.dispose();
      terminationBatchRef.current = null;
      setTerminationRunning(false);
      if (!isCurrentServer(submittedServerIdRef.current)) {
        submittedKeysRef.current = [];
        submittedServerIdRef.current = null;
        return;
      }
      // Successes leave selection; failures stay selected for retry.
      setSelectedKeys((current) => {
        let next = removeSessionsFromSelection(
          current,
          submittedKeysRef.current,
        );
        for (const entry of summary.failedEntries) {
          next = addSessionToSelection(next, entry.sessionKey);
        }
        return next;
      });
      submittedKeysRef.current = [];
      if (summary.failed > 0) {
        const failedTitles = summary.failedEntries.map((entry) => {
          const agent = agentsByKeyRef.current[entry.sessionKey];
          return agent
            ? presentWorker(agent, workerAliasesRef.current[entry.sessionKey])
                .title
            : entry.sessionKey;
        });
        Alert.alert(
          summary.succeeded > 0
            ? "Some sessions could not be terminated"
            : "Could not terminate sessions",
          sessionTerminationSummaryMessage(summary, failedTitles),
        );
      } else {
        void Haptics.notificationAsync(
          Haptics.NotificationFeedbackType.Success,
        );
      }
    },
    [],
  );

  const selectedWorkers = useMemo(
    () => displayWorkers.filter((agent) => selectedKeys.has(agent.key)),
    [selectedKeys, displayWorkers],
  );

  const runTerminateSelection = useCallback(() => {
    if (terminationRunning || selectedWorkers.length === 0) {
      return;
    }
    if (selectedWorkers.some((worker) => !isCurrentServer(worker.serverId))) return;
    const entries = createSessionTerminationEntries(
      selectedWorkers.map((agent) => ({
        sessionKey: agent.key,
        serverId: agent.serverId,
        workerId: agent.id,
      })),
    );
    submittedKeysRef.current = entries.map((entry) => entry.sessionKey);
    submittedServerIdRef.current = selectedWorkers[0].serverId;
    const batch = new SessionTerminationBatch({
      transport: wsClient,
      entries,
      onSettled: handleBatchSettled,
    });
    terminationBatchRef.current?.dispose();
    terminationBatchRef.current = batch;
    setTerminationRunning(true);
    let started = false;
    try {
      started = batch.start();
    } catch {
      batch.dispose();
      terminationBatchRef.current = null;
      submittedKeysRef.current = [];
      setTerminationRunning(false);
      return;
    }
    if (!started) {
      // Duplicate-prevention guard: never submit the same batch twice.
      batch.dispose();
      terminationBatchRef.current = null;
      submittedKeysRef.current = [];
      setTerminationRunning(false);
    }
  }, [handleBatchSettled, selectedWorkers, terminationRunning, isCurrentServer]);

  const confirmTerminateSelection = useCallback(() => {
    if (terminationRunning || selectedWorkers.length === 0) {
      return;
    }
    const count = selectedWorkers.length;
    Alert.alert(
      count === 1 ? "Terminate session?" : "Terminate sessions?",
      sessionTerminationConfirmMessage(count),
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => runTerminateSelection(),
        },
      ],
    );
  }, [runTerminateSelection, selectedWorkers, terminationRunning]);

  const finishCreateTerminal = async (
    serverId: string,
    workerId: string,
    hint?: {
      cwd: string;
      command: string;
      name: string;
      startedAt: number;
      durabilityWarning?: string | null;
    },
  ) => {
    if (!isCurrentServer(serverId)) return;
    const sessionKey = makeSessionKey(serverId, workerId);
    const openedAt = Date.now();
    void markWorkerOpened(sessionKey, openedAt);
    router.push({
      pathname: "/terminal/[id]",
      params: hint
        ? {
            id: workerId,
            serverId,
            cwd: hint.cwd,
            command: hint.command,
            name: hint.name,
            startedAt: String(hint.startedAt),
            initialComposerFocus: "1",
            ...(hint.durabilityWarning
              ? { createDurabilityWarning: hint.durabilityWarning }
              : {}),
          }
        : { id: workerId, serverId },
    });
  };

  const findSuggestedCwd = (serverId: string): string => {
    const onServer = sortedWorkers.filter(
      (agent) => agent.serverId === serverId && agent.cwd,
    );
    return onServer[0]?.cwd?.trim() || "";
  };

  useEffect(() => {
    const onWorkerSessionList = (payload: { serverId?: string }) => {
      const serverId = payload?.serverId?.trim();
      if (!serverId) return;
      setWorkerSessionListReceiptByServer((current) =>
        bumpWorkerSessionListReceipt(current, serverId),
      );
    };
    wsClient.on("worker_session_list", onWorkerSessionList);
    return () => {
      wsClient.off("worker_session_list", onWorkerSessionList);
    };
  }, []);

  useEffect(() => {
    setCreateAmbiguityBlocks((current) => {
      let next = current;
      for (const [serverId, block] of Object.entries(current)) {
        const connectionGeneration =
          state.connectionGenerationByServer[serverId] ?? 0;
        const listReceipt = workerSessionListReceiptByServer[serverId] ?? 0;
        const listFresh = isWorkerSessionListFreshForConnection(
          state,
          serverId,
        );
        if (
          shouldUnlockCreateAfterAmbiguity({
            block,
            connectionGeneration,
            listReceipt,
            listFreshForConnection: listFresh,
          })
        ) {
          next = clearCreateAmbiguityForServer(next, serverId);
        }
      }
      return next;
    });
  }, [
    workerSessionListReceiptByServer,
    state.workerSessionListGenerationByServer,
    state.connectionGenerationByServer,
    state.serverConnections,
  ]);

  const createTerminalOnServer = async (input: {
    serverId: string;
    cwd: string;
    command: string;
    name: string;
  }) => {
    if (createInFlightRef.current) return;
    const server = currentServer;
    if (!server || !isCurrentServer(server.id) || server.id !== input.serverId || !anyConnected) {
      Alert.alert(
        "Daemon unavailable",
        "Connect to a daemon before creating a new terminal.",
      );
      return;
    }

    setCreateSheetVisible(false);
    const connectionGeneration =
      state.connectionGenerationByServer[server.id] ?? 0;
    const listReceipt = workerSessionListReceiptByServer[server.id] ?? 0;
    const listFresh = isWorkerSessionListFreshForConnection(state, server.id);
    if (
      isCreateBlockedByAmbiguity({
        blocks: createAmbiguityBlocks,
        serverId: server.id,
        connectionGeneration,
        listReceipt,
        listFreshForConnection: listFresh,
      })
    ) {
      wsClient.listWorkerSessions(server.id);
      Alert.alert(
        "Refresh required",
        "Previous create result was ambiguous. Waiting for a confirmed session list before creating another terminal.",
      );
      return;
    }
    setCreatingServerId(server.id);
    createInFlightRef.current = true;
    let dispatched = false;
    try {
      const startedAt = Date.now();
      const pending = wsClient.createSession(server.id, {
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
      dispatched = true;
      const created = await pending;
      const reconciled = reconcileCreateSessionSuccess(created);
      if (reconciled.kind === "ambiguous" || reconciled.kind === "failed") {
        if (reconciled.requiresReconcileBeforeCreate) {
          setCreateAmbiguityBlocks((current) =>
            blockCreateAfterAmbiguity(current, {
              serverId: server.id,
              connectionGeneration:
                state.connectionGenerationByServer[server.id] ?? 0,
              listReceipt: workerSessionListReceiptByServer[server.id] ?? 0,
            }),
          );
          wsClient.listWorkerSessions(server.id);
        }
        if (!isCurrentServer(server.id)) return;
        Alert.alert(
          reconciled.kind === "ambiguous"
            ? "Refresh required"
            : "Could not create terminal",
          reconciled.message,
        );
        return;
      }
      setCreateAmbiguityBlocks((current) =>
        clearCreateAmbiguityForServer(current, server.id),
      );
      await finishCreateTerminal(server.id, reconciled.workerId, {
        ...input,
        startedAt,
        durabilityWarning: reconciled.durabilityWarning,
      });
    } catch (error: any) {
      const reconciled = reconcileCreateSessionFailure(error, dispatched);
      if (
        reconciled.kind === "ambiguous" ||
        (reconciled.kind === "failed" && reconciled.requiresReconcileBeforeCreate)
      ) {
        setCreateAmbiguityBlocks((current) =>
          blockCreateAfterAmbiguity(current, {
            serverId: server.id,
            connectionGeneration:
              state.connectionGenerationByServer[server.id] ?? 0,
            listReceipt: workerSessionListReceiptByServer[server.id] ?? 0,
          }),
        );
        wsClient.listWorkerSessions(server.id);
      }
      if (!isCurrentServer(server.id)) return;
      Alert.alert(
        reconciled.kind === "ambiguous"
          ? "Refresh required"
          : "Could not create terminal",
        reconciled.kind === "navigable" ? "Create failed." : reconciled.message,
      );
    } finally {
      createInFlightRef.current = false;
      setCreatingServerId(null);
    }
  };

  const refreshSessionServices = async () => {
    const server = currentServer;
    const epoch = ++servicesRequestEpochRef.current;
    if (!server || !isCurrentServer(server.id) || !anyConnected) {
      setSessionServices([]);
      setServicesError(null);
      return;
    }

    setServicesLoading(true);
    setServicesError(null);
    try {
      const snapshot = await wsClient.listSessionServices(server.id);
      if (!isCurrentServer(server.id) || epoch !== servicesRequestEpochRef.current) return;
      const services = snapshot.services
        .map<DiscoveredSessionService>((service) => ({ ...service, serverId: server.id, serverName: server.name }))
        .sort((left, right) => {
          const leftProject = serviceProjectLabel(left);
          const rightProject = serviceProjectLabel(right);
          if (leftProject !== rightProject)
            return leftProject.localeCompare(rightProject);
          return left.port - right.port;
        });

      setSessionServices(services);
    } catch (error: any) {
      if (!isCurrentServer(server.id) || epoch !== servicesRequestEpochRef.current) return;
      setSessionServices([]);
      setServicesError(error?.message || "Failed to load services.");
    } finally {
      if (isCurrentServer(server.id) && epoch === servicesRequestEpochRef.current) setServicesLoading(false);
    }
  };

  const openSessionServices = () => {
    if (!anyConnected || !isCurrentServer(currentServerId)) {
      Alert.alert(
        "Daemon unavailable",
        "Connect to a daemon before viewing session services.",
      );
      return;
    }
    setServiceSheetVisible(true);
    void refreshSessionServices();
  };

  const openWorkObservatory = useCallback(() => {
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setWorkObservatoryVisible(true);
  }, []);

  const closeWorkObservatory = useCallback(() => {
    setWorkObservatoryVisible(false);
  }, []);

  const workObservatoryAccessibilityProps = useMemo(
    () => createWorkObservatoryAccessibilityProps(openWorkObservatory),
    [openWorkObservatory],
  );

  const handleContentScroll = useAnimatedScrollHandler({
    onScroll: (event) => {
      const nextOffset = Math.max(0, event.contentOffset.y);
      sessionListScrollOffsetY.value = nextOffset;
      if (nextOffset > 0 && workObservatoryPullDistance.value > 0) {
        workObservatoryPullDistance.value = 0;
      }
    },
  });

  const workObservatoryPullGesture = useMemo(
    () =>
      Gesture.Pan()
        .enabled(!workObservatoryVisible && !selectionMode)
        .manualActivation(true)
        .maxPointers(1)
        .enableTrackpadTwoFingerGesture(false)
        .shouldCancelWhenOutside(false)
        .onTouchesDown((event, stateManager) => {
          workObservatoryPullDistance.value = 0;
          workObservatoryGestureActivated.value = 0;
          const touch = event.allTouches[0];
          if (!touch) {
            stateManager.fail();
            return;
          }
          const intent = resolveWorkObservatoryPullIntent({
            touchCount: event.numberOfTouches,
            startX: touch.absoluteX,
            dx: 0,
            dy: 0,
            scrollOffsetY: sessionListScrollOffsetY.value,
          });
          if (intent === "fail") {
            stateManager.fail();
            return;
          }
          workObservatoryTouchStartX.value = touch.absoluteX;
          workObservatoryTouchStartY.value = touch.absoluteY;
        })
        .onTouchesMove((event, stateManager) => {
          if (workObservatoryGestureActivated.value === 1) {
            return;
          }
          const touch = event.allTouches[0];
          if (!touch) {
            workObservatoryPullDistance.value = 0;
            stateManager.fail();
            return;
          }
          const intent = resolveWorkObservatoryPullIntent({
            touchCount: event.numberOfTouches,
            startX: workObservatoryTouchStartX.value,
            dx: touch.absoluteX - workObservatoryTouchStartX.value,
            dy: touch.absoluteY - workObservatoryTouchStartY.value,
            scrollOffsetY: sessionListScrollOffsetY.value,
          });
          if (intent === "fail") {
            workObservatoryPullDistance.value = 0;
            stateManager.fail();
            return;
          }
          if (intent === "activate") {
            stateManager.activate();
          }
        })
        .onStart((event) => {
          workObservatoryGestureActivated.value = 1;
          workObservatoryPullDistance.value = Math.max(0, event.translationY);
        })
        .onUpdate((event) => {
          if (sessionListScrollOffsetY.value > 0) {
            workObservatoryPullDistance.value = 0;
            return;
          }
          workObservatoryPullDistance.value = Math.max(0, event.translationY);
        })
        .onEnd(() => {
          const shouldOpen = shouldRevealWorkObservatory(
            workObservatoryPullDistance.value,
          );
          workObservatoryPullDistance.value = 0;
          if (shouldOpen) {
            runOnJS(openWorkObservatory)();
          }
        })
        .onFinalize(() => {
          workObservatoryPullDistance.value = 0;
          workObservatoryGestureActivated.value = 0;
        }),
    [
      openWorkObservatory,
      selectionMode,
      sessionListScrollOffsetY,
      workObservatoryGestureActivated,
      workObservatoryPullDistance,
      workObservatoryTouchStartX,
      workObservatoryTouchStartY,
      workObservatoryVisible,
    ],
  );

  const openServiceTerminal = (service: DiscoveredSessionService) => {
    if (!isCurrentServer(service.serverId)) return;
    // Persistent services outlive their creating Session: there is no live
    // worker target, so no terminal action exists for them.
    if (!service.worker_id?.trim()) return;
    setServiceSheetVisible(false);
    router.push({
      pathname: "/terminal/[id]",
      params: { id: service.worker_id, serverId: service.serverId },
    });
  };

  const openBrain = useCallback(() => {
    router.navigate("/");
  }, [router]);

  const openServiceURL = async (url: string) => {
    try {
      await Linking.openURL(url);
    } catch (error: any) {
      Alert.alert("Could not open URL", error?.message || url);
    }
  };

  const openCreateTerminal = () => {
    if (!anyConnected || !isCurrentServer(currentServerId)) {
      Alert.alert(
        "Daemon unavailable",
        "Connect to a daemon before creating a new terminal.",
      );
      return;
    }
    setCreateSheetVisible(true);
  };

  const openServerSettings = (addServer: boolean) => {
    router.push({
      pathname: "/settings",
      params: addServer ? { addServer: Date.now().toString() } : {},
    });
  };

  const bannerAccent = primaryIssue
    ? connectionIssueAccent(primaryIssue, colors)
    : anyConnecting
      ? colors.statusUnknown
      : colors.disabledText;
  const bannerText =
    primaryIssue?.title || (anyConnecting ? "Connecting" : "Offline");
  const empty = sessionEmptyState(hasConfiguredServers, connectionState);
  const retryCurrentServer = async () => {
    if (!currentServer || !isCurrentServer(currentServer.id)) return;
    try {
      await setServerAutoConnect(currentServer.id, true);
      if (isCurrentServer(currentServer.id)) wsClient.connectServer(currentServer);
    } catch (error) {
      Alert.alert("Connection failed", error instanceof Error ? error.message : "Could not retry this server.");
    }
  };

  const renderListWorker = useCallback<ListRenderItem<Worker>>(
    ({ item }) => (
      <WorkerListRowContainer
        agent={item}
        alias={workerAliases[item.key]}
        linkedWorkTitle={workerWorkMap[`${item.serverId}:${item.id}`]?.title}
        showServerName={showServerNames}
        selectionMode={selectionMode}
        selected={selectedKeys.has(item.key)}
        selectionDisabled={
          !isSessionTerminable(item, state.serverConnections)
        }
        onOpenWorker={openWorker}
        onEnterSelection={enterSelectionMode}
        onToggleSelection={toggleSelection}
      />
    ),
    [
      workerAliases,
      workerWorkMap,
      enterSelectionMode,
      openWorker,
      selectedKeys,
      selectionMode,
      showServerNames,
      state.serverConnections,
      toggleSelection,
    ],
  );

  const renderListSectionHeader = useCallback(
    ({ section }: { section: WorkerDirectorySection }) => {
      if (!useSectionHeaders) {
        return null;
      }
      return (
        <View style={styles.sectionHeader}>
          <Text
            style={styles.sectionTitle}
            numberOfLines={1}
            ellipsizeMode="middle"
          >
            {section.title}
          </Text>
        </View>
      );
    },
    [styles, useSectionHeaders],
  );

  const renderRowSeparator = useCallback(
    () => <View style={styles.rowGap} />,
    [styles],
  );
  const renderSectionSeparator = useCallback(
    () => <View style={styles.sectionGap} />,
    [styles],
  );
  const openHeaderMenu = useCallback(() => {
    setHeaderMenuVisible(true);
  }, []);
  const listPageAction = useMemo(
    () => ({
      accessibilityLabel: "Session options",
      ...workObservatoryAccessibilityProps,
      onPress: openHeaderMenu,
    }),
    [openHeaderMenu, workObservatoryAccessibilityProps],
  );
  usePrimaryPageAction(listPageAction);

  const focused = useIsFocused();
  const authoritativeWorkerKeySet = useMemo(
    () => new Set(displayWorkers.map((agent) => agent.key)),
    [displayWorkers],
  );

  // Selection survives reorder/live updates by stable key and is pruned only
  // when the authoritative Session row disappears from the daemon store.
  useEffect(() => {
    if (!selectionMode) {
      return;
    }
    setSelectedKeys((current) =>
      pruneSessionSelection(current, authoritativeWorkerKeySet),
    );
  }, [authoritativeWorkerKeySet, selectionMode]);

  // Only a fresh snapshot from the batch's original server proves removal.
  // Switching the current server is not evidence that a Session terminated.
  useEffect(() => {
    const batch = terminationBatchRef.current;
    const serverId = submittedServerIdRef.current;
    if (!batch || !serverId || !isWorkerSessionListFreshForConnection(state, serverId)) {
      return;
    }
    const batchKeys = new Set(state.workers.filter((worker) => worker.serverId === serverId).map((worker) => worker.key));
    for (const sessionKey of submittedKeysRef.current) {
      if (!batchKeys.has(sessionKey)) {
        batch.settleDisappeared(sessionKey);
      }
    }
  }, [state, terminationRunning]);

  // Last deselection (or all-settled removal) exits selection mode.
  useEffect(() => {
    if (
      selectionMode &&
      !terminationRunning &&
      countSessionSelection(selectedKeys) === 0
    ) {
      exitSelectionMode();
    }
  }, [exitSelectionMode, selectedKeys, selectionMode, terminationRunning]);

  // Leaving the Sessions tab closes selection so the chrome can never show a
  // selection bar without its owning list.
  useEffect(() => {
    if (!focused && selectionMode) {
      exitSelectionMode();
    }
  }, [exitSelectionMode, focused, selectionMode]);

  // Back exits selection without touching Sessions; disabled while a batch is
  // in flight so the acknowledgement flow is never orphaned.
  usePrimaryDrawerBack({
    enabled: selectionMode && !terminationRunning,
    onBack: exitSelectionMode,
  });

  // Unmount safety net: stop listening and drop timers of any in-flight batch.
  useEffect(
    () => () => {
      terminationBatchRef.current?.dispose();
      terminationBatchRef.current = null;
    },
    [],
  );

  // Telegram-style selection chrome: Cancel + selected count + Terminate.
  const selectionBar = useMemo(
    () =>
      selectionMode ? (
        <WorkerSessionSelectionBar
          count={countSessionSelection(selectedKeys)}
          terminating={terminationRunning}
          onCancel={exitSelectionMode}
          onTerminate={confirmTerminateSelection}
        />
      ) : null,
    [
      confirmTerminateSelection,
      exitSelectionMode,
      selectedKeys,
      selectionMode,
      terminationRunning,
    ],
  );
  usePrimarySelectionBar(selectionBar);
  const listContentContainerStyle = useMemo(
    () => [
      styles.promptContent,
      { paddingBottom: Math.max(insets.bottom, 16) + 76 },
    ],
    [insets.bottom, styles],
  );
  return (
    <GestureDetector gesture={workObservatoryPullGesture}>
      <SafeAreaView
        style={[styles.container, { marginTop: topChromeInset }]}
        edges={[]}
      >
        <WorkSignalPullPreview
          pullDistance={workObservatoryPullDistance}
          threshold={WORK_OBSERVATORY_PULL.threshold}
        />

        {hasConnection && !anyConnected && (
          <View style={styles.bannerWrap}>
            <View style={styles.banner}>
              <View
                style={[styles.bannerDot, { backgroundColor: bannerAccent }]}
              />
              <Text style={styles.bannerText}>{bannerText}</Text>
            </View>
          </View>
        )}

        {shouldShowInitialLoading ? (
          <Animated.ScrollView
            style={styles.flex}
            contentContainerStyle={styles.loadingContainer}
            onScroll={handleContentScroll}
            scrollEventThrottle={16}
            alwaysBounceVertical
            showsVerticalScrollIndicator={false}
          >
            <ActivityIndicator color={colors.accent} />
          </Animated.ScrollView>
        ) : sortedWorkers.length === 0 ? (
          <Animated.ScrollView
            style={styles.flex}
            contentContainerStyle={styles.emptyScrollContent}
            onScroll={handleContentScroll}
            scrollEventThrottle={16}
            alwaysBounceVertical
            showsVerticalScrollIndicator={false}
          >
            <CompactEmptyState title={empty.title} icon={empty.icon} busy={empty.busy}
              detail={primaryIssue?.detail}
              action={empty.action ? {
                label: creatingServerId ? "Starting..." : empty.label,
                icon: empty.action === "retry" ? "refresh-outline" : empty.action === "terminal" ? "add" : "qr-code-outline",
                onPress: empty.action === "retry" ? () => void retryCurrentServer() : empty.action === "terminal" ? openCreateTerminal : () => openServerSettings(true),
                disabled: Boolean(creatingServerId),
              } : undefined}
              secondary={hasConfiguredServers && !anyConnected ? {
                label: "Server settings", icon: "settings-outline", onPress: () => openServerSettings(false),
              } : undefined}
            />
          </Animated.ScrollView>
        ) : (
          <AnimatedSectionList
            sections={listSections}
            key="list"
            keyExtractor={workerKeyExtractor}
            renderItem={renderListWorker}
            renderSectionHeader={renderListSectionHeader}
            stickySectionHeadersEnabled={false}
            contentContainerStyle={listContentContainerStyle}
            onScroll={handleContentScroll}
            scrollEventThrottle={16}
            alwaysBounceVertical
            removeClippedSubviews={false}
            windowSize={15}
            showsVerticalScrollIndicator={false}
            ItemSeparatorComponent={renderRowSeparator}
            SectionSeparatorComponent={renderSectionSeparator}
          />
        )}

        <SessionServicesSheet
          visible={serviceSheetVisible}
          services={currentServices}
          loading={servicesLoading}
          error={servicesError}
          showServerSections={false}
          onClose={() => setServiceSheetVisible(false)}
          onRefresh={() => void refreshSessionServices()}
          onOpenTerminal={openServiceTerminal}
          onOpenURL={(url) => void openServiceURL(url)}
        />

        <NewTerminalSheet
          key={currentServerId ?? "no-server"}
          visible={createSheetVisible}
          title="Session"
          initialCwd={
            currentServerId
              ? findSuggestedCwd(currentServerId)
              : ""
          }
          serverId={currentServerId}
          submitting={!!creatingServerId}
          onClose={() => setCreateSheetVisible(false)}
          onSubmit={(input) => {
            if (!input.serverId) return;
            void createTerminalOnServer({
              serverId: input.serverId,
              cwd: input.cwd,
              command: input.command,
              name: input.name,
            });
          }}
        />

        {sortedWorkers.length > 0 && !selectionMode ? (
          <AnimatedPressable
            style={[
              styles.listFab,
              {
                bottom: Math.max(insets.bottom, 16) + 8,
                right: Math.max(16, (viewportWidth - 760) / 2 + 16),
              },
              (!anyConnected || !!creatingServerId) && styles.listFabDisabled,
            ]}
            preset="press"
            scale={0.92}
            onPress={openCreateTerminal}
            disabled={!!creatingServerId || !anyConnected}
            accessibilityLabel="New terminal"
            accessibilityRole="button"
            accessibilityState={{
              disabled: !!creatingServerId || !anyConnected,
            }}
          >
            <Ionicons
              name={creatingServerId ? "hourglass-outline" : "add"}
              size={28}
              color={
                !anyConnected || !!creatingServerId
                  ? colors.disabledText
                  : colors.textOnAccent
              }
            />
          </AnimatedPressable>
        ) : null}

        {workObservatoryVisible ? (
          <WorkSignalObservatory
            visible={workObservatoryVisible}
            aliases={workerAliases}
            onClose={closeWorkObservatory}
            onOpenSession={openWorker}
            onOpenBrain={openBrain}
          />
        ) : null}

        <RisingSheet
          visible={headerMenuVisible}
          onClose={() => setHeaderMenuVisible(false)}
          cardStyle={styles.menuCard}
          align="bottom"
        >
          <Text style={styles.menuTitle}>Sessions</Text>

          <AnimatedPressable
            style={styles.menuItem}
            preset="press"
            scale={0.98}
            disabled={!anyConnected}
            onPress={() => {
              setHeaderMenuVisible(false);
              openSessionServices();
            }}
          >
            <Ionicons
              name="globe-outline"
              size={16}
              color={anyConnected ? colors.textPrimary : colors.disabledText}
            />
            <Text
              style={[
                styles.menuItemText,
                !anyConnected && { color: colors.disabledText },
              ]}
            >
              Session services
            </Text>
          </AnimatedPressable>
        </RisingSheet>

      </SafeAreaView>
    </GestureDetector>
  );
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const {
    surface: themedSurface,
    border: themedBorder,
    sectionLabel,
  } = surfacesFromTheme(theme);

  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    flex: {
      flex: 1,
    },

    bannerWrap: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
    },
    banner: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 7,
      paddingVertical: 8,
      marginHorizontal: 18,
      marginTop: 6,
      borderRadius: Radii.pill,
      backgroundColor: themedSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: themedBorder,
    },
    bannerDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
    },
    bannerText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
    },

    listFab: {
      position: "absolute",
      width: 56,
      height: 56,
      borderRadius: 28,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
      ...shadow("float", colors.shadowColor),
      zIndex: 4,
    },
    listFabDisabled: {
      backgroundColor: colors.disabledSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    promptContent: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
      paddingTop: 4,
    },
    sectionHeader: {
      paddingTop: 18,
      paddingBottom: 8,
      paddingHorizontal: 16,
    },
    sectionTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: sectionLabel,
    },
    sectionGap: {
      height: 8,
    },
    rowGap: {
      height: 0,
    },
    loadingContainer: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
      flexGrow: 1,
      minHeight: 420,
      alignItems: "center",
      justifyContent: "center",
    },
    emptyScrollContent: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
      flexGrow: 1,
      justifyContent: "center",
      paddingVertical: 44,
    },

    menuCard: {
      borderRadius: 8,
      backgroundColor: colors.modalSurfaceAlt,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
      overflow: "hidden",
      ...shadow("float", colors.shadowColor),
    },
    menuTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textTertiary,
      paddingHorizontal: 18,
      paddingTop: 16,
      paddingBottom: 10,
    },
    menuItem: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 18,
      minHeight: 48,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    menuItemText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    menuItemTextDestructive: {
      color: colors.dangerText,
    },


  });
}
