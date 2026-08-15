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
import { Agent, useAgents } from "../../store/agents";
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
import { Enter } from "../../components/ui/Enter";
import { AgentListRowContainer } from "../../components/agents/AgentListRowContainer";
import { AgentSessionSelectionBar } from "../../components/agents/AgentSessionSelectionBar";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { SessionServicesSheet } from "../../components/SessionServicesSheet";
import { usePrimarySelectionBar } from "../../components/navigation/PrimarySelectionBar";
import { usePrimaryDrawerBack } from "../../components/navigation/usePrimaryDrawerBack";
import {
  getAgentAliases,
  getServers,
  markAgentOpened,
  StoredAgentAliases,
  StoredServer,
} from "../../services/storage";
import { connectionIssueAccent } from "../../services/connectionIssue";
import { wsClient } from "../../services/websocket";
import {
  blockCreateAfterAmbiguity,
  bumpAgentSessionListReceipt,
  clearCreateAmbiguityForServer,
  isCreateBlockedByAmbiguity,
  reconcileCreateSessionFailure,
  reconcileCreateSessionSuccess,
  shouldUnlockCreateAfterAmbiguity,
  type CreateAmbiguityGateState,
} from "../../services/providers";
import { isAgentSessionListFreshForConnection } from "../../store/agents";
import { makeSessionKey } from "../../services/sessionKeys";
import { presentAgent } from "../../services/agentPresentation";
import {
  addSessionToSelection,
  countSelectionServers,
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
  filterAgentsByPreferredServers,
  groupAgentsByDirectory,
  type AgentDirectorySection,
} from "../../services/serverSelection";
import {
  serviceProjectLabel,
  type DiscoveredSessionService,
} from "../../services/sessionServicesPresentation";

const AnimatedSectionList = Animated.createAnimatedComponent(
  SectionList<Agent, AgentDirectorySection>,
);
const agentKeyExtractor = (agent: Agent) => agent.key;

export default function InboxScreen() {
  const { state } = useAgents();
  const { state: workState } = useWork();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;
  const { width: viewportWidth } = useWindowDimensions();
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);

  const agentWorkMap = useMemo(() => {
    const map: Record<string, WorkItem> = {};
    for (const current of Object.values(workState.byKey)) {
      if (current.frontmatter.done || !current.frontmatter.agent_session) {
        continue;
      }
      map[`${current.serverId}:${current.frontmatter.agent_session}`] = current;
    }
    return map;
  }, [workState.byKey]);
  const [headerMenuVisible, setHeaderMenuVisible] = useState(false);
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [configuredServerCount, setConfiguredServerCount] = useState(0);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [storageHydrated, setStorageHydrated] = useState(false);
  const [createSheetVisible, setCreateSheetVisible] = useState(false);
  const [selectedCreateServerId, setSelectedCreateServerId] = useState<
    string | null
  >(null);
  const [creatingServerId, setCreatingServerId] = useState<string | null>(null);
  const [createAmbiguityBlocks, setCreateAmbiguityBlocks] =
    useState<CreateAmbiguityGateState>({});
  const [agentSessionListReceiptByServer, setAgentSessionListReceiptByServer] =
    useState<Record<string, number>>({});
  const [serviceSheetVisible, setServiceSheetVisible] = useState(false);
  const [sessionServices, setSessionServices] = useState<
    DiscoveredSessionService[]
  >([]);
  const [servicesLoading, setServicesLoading] = useState(false);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [workObservatoryVisible, setWorkObservatoryVisible] = useState(false);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedKeys, setSelectedKeys] = useState<SessionSelection>(
    EMPTY_SESSION_SELECTION,
  );
  const [terminationRunning, setTerminationRunning] = useState(false);
  const terminationBatchRef = useRef<SessionTerminationBatch | null>(null);
  const submittedKeysRef = useRef<string[]>([]);
  const workObservatoryPullDistance = useSharedValue(0);
  const sessionListScrollOffsetY = useSharedValue(0);
  const workObservatoryTouchStartX = useSharedValue(0);
  const workObservatoryTouchStartY = useSharedValue(0);
  const workObservatoryGestureActivated = useSharedValue(0);
  const agentsHydrated = useMemo(
    () => servers.some((server) => state.hydratedServers[server.id]),
    [servers, state.hydratedServers],
  );

  const agentsByKey = useMemo(() => {
    const byKey: Record<string, Agent> = {};
    for (const agent of state.agents) {
      byKey[agent.key] = agent;
    }
    return byKey;
  }, [state.agents]);
  const agentsByKeyRef = useRef(agentsByKey);
  agentsByKeyRef.current = agentsByKey;
  const agentAliasesRef = useRef(agentAliases);
  agentAliasesRef.current = agentAliases;

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const [storedAliases, storedServers] = await Promise.all([
        getAgentAliases(),
        getServers(),
      ]);
      if (!cancelled) {
        setAgentAliases(storedAliases);
        setConfiguredServerCount(storedServers.length);
        setServers(storedServers);
        setStorageHydrated(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      (async () => {
        const [storedAliases, storedServers] = await Promise.all([
          getAgentAliases(),
          getServers(),
        ]);
        if (!cancelled) {
          setAgentAliases(storedAliases);
          setConfiguredServerCount(storedServers.length);
          setServers(storedServers);
        }
      })();
      return () => {
        cancelled = true;
      };
    }, []),
  );

  const displayAgents = useMemo(
    () =>
      filterAgentsByPreferredServers({
        agents: state.agents,
        servers,
        connectionStates: state.serverConnections,
        latencyById: state.serverLatencyById,
      }),
    [servers, state.agents, state.serverConnections, state.serverLatencyById],
  );

  const sortedAgents = useMemo(
    () =>
      groupAgentsByDirectory(displayAgents).flatMap((section) => section.data),
    [displayAgents],
  );

  const showServerNames = useMemo(
    () => new Set(sortedAgents.map((agent) => agent.serverId)).size > 1,
    [sortedAgents],
  );

  const hasConfiguredServers = configuredServerCount > 0;
  const hasConnection = Object.keys(state.serverConnections).length > 0;
  const anyConnected = Object.values(state.serverConnections).includes(
    "connected",
  );
  const anyConnecting = Object.values(state.serverConnections).includes(
    "connecting",
  );
  const connectedServerIds = useMemo(
    () =>
      servers
        .filter((server) => state.serverConnections[server.id] === "connected")
        .map((server) => server.id),
    [servers, state.serverConnections],
  );
  const waitingForInitialAgentSnapshot =
    storageHydrated &&
    connectedServerIds.some((serverId) => !state.hydratedServers[serverId]);
  const shouldShowInitialLoading =
    (!storageHydrated && sortedAgents.length === 0) ||
    (!agentsHydrated &&
      sortedAgents.length === 0 &&
      hasConfiguredServers &&
      (anyConnecting || waitingForInitialAgentSnapshot));
  const listSections = useMemo(
    () =>
      groupAgentsByDirectory(sortedAgents, { showServerName: showServerNames }),
    [showServerNames, sortedAgents],
  );
  const useSectionHeaders = listSections.length > 1;
  const primaryIssue = useMemo(() => {
    let nextIssue: (typeof state.serverConnectionIssues)[string] | null = null;
    for (const issue of Object.values(state.serverConnectionIssues)) {
      if (!issue) {
        continue;
      }
      if (!nextIssue || issue.checkedAt > nextIssue.checkedAt) {
        nextIssue = issue;
      }
    }
    return nextIssue;
  }, [state.serverConnectionIssues]);

  const connectedServers = useMemo(
    () =>
      servers.filter(
        (server) => state.serverConnections[server.id] === "connected",
      ),
    [servers, state.serverConnections],
  );
  const createServerOptions = useMemo(
    () =>
      connectedServers.map((server) => ({ id: server.id, name: server.name })),
    [connectedServers],
  );

  const openAgent = useCallback(
    (agent: Agent) => {
      const openedAt = Date.now();
      void markAgentOpened(agent.key, openedAt);
      router.push({
        pathname: "/terminal/[id]",
        params: { id: agent.id, serverId: agent.serverId },
      });
    },
    [router],
  );

  const exitSelectionMode = useCallback(() => {
    if (terminationRunning) {
      return;
    }
    setSelectionMode(false);
    setSelectedKeys(EMPTY_SESSION_SELECTION);
  }, [terminationRunning]);

  const enterSelectionMode = useCallback(
    (agent: Agent) => {
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
    (agent: Agent) => {
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
            ? presentAgent(agent, agentAliasesRef.current[entry.sessionKey])
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

  const selectedAgents = useMemo(
    () => state.agents.filter((agent) => selectedKeys.has(agent.key)),
    [selectedKeys, state.agents],
  );

  const runTerminateSelection = useCallback(() => {
    if (terminationRunning || selectedAgents.length === 0) {
      return;
    }
    const entries = createSessionTerminationEntries(
      selectedAgents.map((agent) => ({
        sessionKey: agent.key,
        serverId: agent.serverId,
        agentId: agent.id,
      })),
    );
    submittedKeysRef.current = entries.map((entry) => entry.sessionKey);
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
  }, [handleBatchSettled, selectedAgents, terminationRunning]);

  const confirmTerminateSelection = useCallback(() => {
    if (terminationRunning || selectedAgents.length === 0) {
      return;
    }
    const count = selectedAgents.length;
    const serverCount = countSelectionServers(selectedAgents);
    Alert.alert(
      count === 1 ? "Terminate session?" : "Terminate sessions?",
      sessionTerminationConfirmMessage(count, serverCount),
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => runTerminateSelection(),
        },
      ],
    );
  }, [runTerminateSelection, selectedAgents, terminationRunning]);

  const finishCreateTerminal = async (
    serverId: string,
    agentId: string,
    hint?: {
      cwd: string;
      command: string;
      name: string;
      startedAt: number;
      durabilityWarning?: string | null;
    },
  ) => {
    const sessionKey = makeSessionKey(serverId, agentId);
    const openedAt = Date.now();
    void markAgentOpened(sessionKey, openedAt);
    router.push({
      pathname: "/terminal/[id]",
      params: hint
        ? {
            id: agentId,
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
        : { id: agentId, serverId },
    });
  };

  const findSuggestedCwd = (serverId: string): string => {
    const onServer = sortedAgents.filter(
      (agent) => agent.serverId === serverId && agent.cwd,
    );
    return onServer[0]?.cwd?.trim() || "";
  };

  useEffect(() => {
    const onAgentSessionList = (payload: { serverId?: string }) => {
      const serverId = payload?.serverId?.trim();
      if (!serverId) return;
      setAgentSessionListReceiptByServer((current) =>
        bumpAgentSessionListReceipt(current, serverId),
      );
    };
    wsClient.on("agent_session_list", onAgentSessionList);
    return () => {
      wsClient.off("agent_session_list", onAgentSessionList);
    };
  }, []);

  useEffect(() => {
    setCreateAmbiguityBlocks((current) => {
      let next = current;
      for (const [serverId, block] of Object.entries(current)) {
        const connectionGeneration =
          state.connectionGenerationByServer[serverId] ?? 0;
        const listReceipt = agentSessionListReceiptByServer[serverId] ?? 0;
        const listFresh = isAgentSessionListFreshForConnection(
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
    agentSessionListReceiptByServer,
    state.agentSessionListGenerationByServer,
    state.connectionGenerationByServer,
    state.serverConnections,
  ]);

  const createTerminalOnServer = async (input: {
    serverId: string;
    cwd: string;
    command: string;
    name: string;
  }) => {
    const server = connectedServers.find((item) => item.id === input.serverId);
    if (!server) {
      Alert.alert(
        "Daemon unavailable",
        "Connect to a daemon before creating a new terminal.",
      );
      return;
    }

    setCreateSheetVisible(false);
    const connectionGeneration =
      state.connectionGenerationByServer[server.id] ?? 0;
    const listReceipt = agentSessionListReceiptByServer[server.id] ?? 0;
    const listFresh = isAgentSessionListFreshForConnection(state, server.id);
    if (
      isCreateBlockedByAmbiguity({
        blocks: createAmbiguityBlocks,
        serverId: server.id,
        connectionGeneration,
        listReceipt,
        listFreshForConnection: listFresh,
      })
    ) {
      wsClient.listAgentSessions(server.id);
      Alert.alert(
        "Refresh required",
        "Previous create result was ambiguous. Waiting for a confirmed session list before creating another terminal.",
      );
      return;
    }
    setCreatingServerId(server.id);
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
              listReceipt: agentSessionListReceiptByServer[server.id] ?? 0,
            }),
          );
          wsClient.listAgentSessions(server.id);
        }
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
      await finishCreateTerminal(server.id, reconciled.agentId, {
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
            listReceipt: agentSessionListReceiptByServer[server.id] ?? 0,
          }),
        );
        wsClient.listAgentSessions(server.id);
      }
      Alert.alert(
        reconciled.kind === "ambiguous"
          ? "Refresh required"
          : "Could not create terminal",
        reconciled.kind === "navigable" ? "Create failed." : reconciled.message,
      );
    } finally {
      setCreatingServerId(null);
    }
  };

  const refreshSessionServices = async () => {
    if (connectedServers.length === 0) {
      setSessionServices([]);
      setServicesError(null);
      return;
    }

    setServicesLoading(true);
    setServicesError(null);
    try {
      const results = await Promise.allSettled(
        connectedServers.map(async (server) => {
          const snapshot = await wsClient.listSessionServices(server.id);
          return snapshot.services.map<DiscoveredSessionService>((service) => ({
            ...service,
            serverId: server.id,
            serverName: server.name,
          }));
        }),
      );

      const services = results
        .flatMap((result) =>
          result.status === "fulfilled" ? result.value : [],
        )
        .sort((left, right) => {
          if (left.serverName !== right.serverName)
            return left.serverName.localeCompare(right.serverName);
          const leftProject = serviceProjectLabel(left);
          const rightProject = serviceProjectLabel(right);
          if (leftProject !== rightProject)
            return leftProject.localeCompare(rightProject);
          return left.port - right.port;
        });

      const failures = results.filter((result) => result.status === "rejected");
      setSessionServices(services);
      setServicesError(
        failures.length > 0
          ? `${failures.length} daemon${failures.length === 1 ? "" : "s"} did not return services.`
          : null,
      );
    } catch (error: any) {
      setSessionServices([]);
      setServicesError(error?.message || "Failed to load services.");
    } finally {
      setServicesLoading(false);
    }
  };

  const openSessionServices = () => {
    if (connectedServers.length === 0) {
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
    setServiceSheetVisible(false);
    router.push({
      pathname: "/terminal/[id]",
      params: { id: service.agent_id, serverId: service.serverId },
    });
  };

  const openServiceURL = async (url: string) => {
    try {
      await Linking.openURL(url);
    } catch (error: any) {
      Alert.alert("Could not open URL", error?.message || url);
    }
  };

  const openCreateTerminal = () => {
    if (connectedServers.length === 0) {
      Alert.alert(
        "Daemon unavailable",
        "Connect to a daemon before creating a new terminal.",
      );
      return;
    }
    setSelectedCreateServerId((previous) =>
      previous && connectedServers.some((server) => server.id === previous)
        ? previous
        : connectedServers[0].id,
    );
    setCreateSheetVisible(true);
  };

  useEffect(() => {
    if (!createSheetVisible || !selectedCreateServerId) {
      return;
    }
    wsClient.requestBrainSnapshot(selectedCreateServerId);
  }, [createSheetVisible, selectedCreateServerId]);

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
  const emptyTitle = !hasConfiguredServers
    ? "No servers"
    : anyConnected
      ? "No sessions yet"
      : primaryIssue?.title || (anyConnecting ? "Connecting" : "Offline");
  const emptySubtext = !hasConfiguredServers
    ? "Add a server in Settings."
    : anyConnected
      ? "Start an agent on your daemon, or create a terminal."
      : primaryIssue?.detail ||
        (anyConnecting ? null : "Check server connection in Settings.");

  const renderListAgent = useCallback<ListRenderItem<Agent>>(
    ({ item }) => (
      <AgentListRowContainer
        agent={item}
        alias={agentAliases[item.key]}
        linkedWorkTitle={agentWorkMap[`${item.serverId}:${item.id}`]?.title}
        showServerName={showServerNames}
        selectionMode={selectionMode}
        selected={selectedKeys.has(item.key)}
        selectionDisabled={
          !isSessionTerminable(item, state.serverConnections)
        }
        onOpenAgent={openAgent}
        onEnterSelection={enterSelectionMode}
        onToggleSelection={toggleSelection}
      />
    ),
    [
      agentAliases,
      agentWorkMap,
      enterSelectionMode,
      openAgent,
      selectedKeys,
      selectionMode,
      showServerNames,
      state.serverConnections,
      toggleSelection,
    ],
  );

  const renderListSectionHeader = useCallback(
    ({ section }: { section: AgentDirectorySection }) => {
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
  const authoritativeAgentKeySet = useMemo(
    () => new Set(state.agents.map((agent) => agent.key)),
    [state.agents],
  );

  // Selection survives reorder/live updates by stable key and is pruned only
  // when the authoritative Session row disappears from the daemon store.
  useEffect(() => {
    if (!selectionMode) {
      return;
    }
    setSelectedKeys((current) =>
      pruneSessionSelection(current, authoritativeAgentKeySet),
    );
  }, [authoritativeAgentKeySet, selectionMode]);

  // A Session that disappears while a termination batch is running is already
  // settled: treat it as success so it can never be reported as failed.
  useEffect(() => {
    const batch = terminationBatchRef.current;
    if (!selectionMode || !batch) {
      return;
    }
    for (const sessionKey of selectedKeys) {
      if (!authoritativeAgentKeySet.has(sessionKey)) {
        batch.settleDisappeared(sessionKey);
      }
    }
  }, [authoritativeAgentKeySet, selectedKeys, selectionMode]);

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
        <AgentSessionSelectionBar
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
        ) : sortedAgents.length === 0 ? (
          <Animated.ScrollView
            style={styles.flex}
            contentContainerStyle={styles.emptyScrollContent}
            onScroll={handleContentScroll}
            scrollEventThrottle={16}
            alwaysBounceVertical
            showsVerticalScrollIndicator={false}
          >
            <Enter preset="rise" style={styles.emptyContainer}>
              <Enter preset="pop">
                <View style={styles.emptyBadge}>
                  <Text style={styles.emptyIcon}>☯</Text>
                </View>
              </Enter>
              <Text style={styles.emptyText}>{emptyTitle}</Text>
              {emptySubtext ? (
                <Text style={styles.emptySubtext}>{emptySubtext}</Text>
              ) : null}
              <View style={styles.emptyActions}>
                {connectedServers.length > 0 ? (
                  <AnimatedPressable
                    style={[
                      styles.emptyActionBtn,
                      styles.emptyActionBtnPrimary,
                    ]}
                    preset="press"
                    scale={0.95}
                    onPress={openCreateTerminal}
                    disabled={!!creatingServerId}
                  >
                    <Ionicons
                      name="add"
                      size={18}
                      color={colors.textOnAccent}
                      style={styles.emptyActionIcon}
                    />
                    <Text
                      style={[
                        styles.emptyActionText,
                        styles.emptyActionTextPrimary,
                      ]}
                    >
                      {creatingServerId ? "Starting…" : "New terminal"}
                    </Text>
                  </AnimatedPressable>
                ) : (
                  <AnimatedPressable
                    style={[
                      styles.emptyActionBtn,
                      styles.emptyActionBtnPrimary,
                    ]}
                    preset="press"
                    scale={0.95}
                    onPress={() => openServerSettings(true)}
                  >
                    <Ionicons
                      name="server-outline"
                      size={18}
                      color={colors.textOnAccent}
                      style={styles.emptyActionIcon}
                    />
                    <Text
                      style={[
                        styles.emptyActionText,
                        styles.emptyActionTextPrimary,
                      ]}
                    >
                      Add server
                    </Text>
                  </AnimatedPressable>
                )}
                {hasConfiguredServers ? (
                  <AnimatedPressable
                    style={styles.emptyActionLink}
                    preset="press"
                    scale={0.96}
                    onPress={() => openServerSettings(false)}
                  >
                    <Text style={styles.emptyActionLinkText}>
                      Open Settings
                    </Text>
                  </AnimatedPressable>
                ) : null}
              </View>
            </Enter>
          </Animated.ScrollView>
        ) : (
          <AnimatedSectionList
            sections={listSections}
            key="list"
            keyExtractor={agentKeyExtractor}
            renderItem={renderListAgent}
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
          services={sessionServices}
          loading={servicesLoading}
          error={servicesError}
          showServerSections={connectedServers.length > 1}
          onClose={() => setServiceSheetVisible(false)}
          onRefresh={() => void refreshSessionServices()}
          onOpenTerminal={openServiceTerminal}
          onOpenURL={(url) => void openServiceURL(url)}
        />

        <NewTerminalSheet
          visible={createSheetVisible}
          title="Session"
          subtitle=""
          initialCwd={
            selectedCreateServerId
              ? findSuggestedCwd(selectedCreateServerId)
              : ""
          }
          serverOptions={createServerOptions}
          selectedServerId={selectedCreateServerId}
          onSelectServer={setSelectedCreateServerId}
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

        {sortedAgents.length > 0 && !selectionMode ? (
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
            aliases={agentAliases}
            onClose={closeWorkObservatory}
            onOpenSession={openAgent}
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
    emptyContainer: {
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 36,
    },
    emptyScrollContent: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
      flexGrow: 1,
      justifyContent: "center",
      paddingVertical: 44,
    },
    emptyBadge: {
      width: 88,
      height: 88,
      borderRadius: 44,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accentSoft,
      marginBottom: 22,
    },
    emptyIcon: {
      fontSize: 42,
      color: colors.accent,
      lineHeight: 48,
    },
    emptyText: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: colors.textPrimary,
    },
    emptySubtext: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginTop: 9,
      maxWidth: 280,
      textAlign: "center",
    },
    emptyActions: {
      width: "100%",
      maxWidth: 260,
      gap: 14,
      marginTop: 30,
      alignItems: "center",
    },
    emptyActionLink: {
      minHeight: 44,
      justifyContent: "center",
      paddingHorizontal: 10,
    },
    emptyActionLinkText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.accent,
    },
    emptyActionBtn: {
      width: "100%",
      flexDirection: "row",
      minHeight: 48,
      paddingHorizontal: 18,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: themedBorder,
      backgroundColor: themedSurface,
      gap: 8,
    },
    emptyActionBtnPrimary: {
      backgroundColor: colors.accent,
      borderColor: colors.accent,
      ...shadow("card", colors.shadowColor),
    },
    emptyActionIcon: {
      marginTop: 1,
    },
    emptyActionText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
      textAlign: "center",
    },
    emptyActionTextPrimary: {
      color: colors.textOnAccent,
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
