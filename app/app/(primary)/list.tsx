import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  type ListRenderItem,
  SectionList,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
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
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { SessionServicesSheet } from "../../components/SessionServicesSheet";
import {
  getAgentAliases,
  getServers,
  markAgentOpened,
  setAgentAlias,
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
  const workObservatoryPullDistance = useSharedValue(0);
  const sessionListScrollOffsetY = useSharedValue(0);
  const workObservatoryTouchStartX = useSharedValue(0);
  const workObservatoryTouchStartY = useSharedValue(0);
  const workObservatoryGestureActivated = useSharedValue(0);
  const agentsHydrated = useMemo(
    () => servers.some((server) => state.hydratedServers[server.id]),
    [servers, state.hydratedServers],
  );

  const [menuAgent, setMenuAgent] = useState<Agent | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [renameAgentKey, setRenameAgentKey] = useState<string | null>(null);

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

  const openContextMenu = useCallback((agent: Agent) => {
    setMenuAgent(agent);
  }, []);

  const closeContextMenu = () => {
    setMenuAgent(null);
  };

  const openRename = () => {
    if (!menuAgent) return;
    setRenameAgentKey(menuAgent.key);
    setRenameDraft(agentAliases[menuAgent.key] || menuAgent.name);
    setMenuAgent(null);
    setRenameVisible(true);
  };

  const handleRename = async () => {
    if (!renameAgentKey) return;
    const updated = await setAgentAlias(renameAgentKey, renameDraft);
    setAgentAliases(updated);
    setRenameVisible(false);
    setRenameAgentKey(null);
  };

  const closeRename = () => {
    setRenameVisible(false);
    setRenameAgentKey(null);
  };

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
        .enabled(!workObservatoryVisible)
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

  const handleTerminateAgent = () => {
    if (!menuAgent) return;

    const target = menuAgent;
    closeContextMenu();

    if (state.serverConnections[target.serverId] !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before terminating the agent.",
      );
      return;
    }

    Alert.alert(
      "Terminate?",
      "This will terminate " +
        presentAgent(target, agentAliases[target.key]).title +
        " on " +
        target.serverName +
        ".",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => {
            wsClient.killAgent(target.serverId, target.id);
          },
        },
      ],
    );
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
        onOpenAgent={openAgent}
        onOpenContextMenu={openContextMenu}
      />
    ),
    [agentAliases, agentWorkMap, openAgent, openContextMenu, showServerNames],
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

        {sortedAgents.length > 0 ? (
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

        <RisingSheet
          visible={menuAgent !== null && !renameVisible}
          onClose={closeContextMenu}
          cardStyle={styles.menuCard}
          align="bottom"
        >
          <Text style={styles.menuTitle} numberOfLines={1}>
            {menuAgent
              ? presentAgent(menuAgent, agentAliases[menuAgent.key]).title
              : ""}
          </Text>

          <AnimatedPressable
            style={styles.menuItem}
            preset="press"
            scale={0.98}
            onPress={openRename}
          >
            <Ionicons
              name="pencil-outline"
              size={16}
              color={colors.textPrimary}
            />
            <Text style={styles.menuItemText}>Rename</Text>
          </AnimatedPressable>

          <AnimatedPressable
            style={styles.menuItem}
            preset="press"
            scale={0.98}
            onPress={() => {
              if (menuAgent) openAgent(menuAgent);
              closeContextMenu();
            }}
          >
            <Ionicons
              name="terminal-outline"
              size={16}
              color={colors.textPrimary}
            />
            <Text style={styles.menuItemText}>Open Terminal</Text>
          </AnimatedPressable>

          <AnimatedPressable
            style={styles.menuItem}
            preset="press"
            scale={0.98}
            onPress={handleTerminateAgent}
          >
            <Ionicons
              name="power-outline"
              size={16}
              color={colors.dangerText}
            />
            <Text style={[styles.menuItemText, styles.menuItemTextDestructive]}>
              Terminate
            </Text>
          </AnimatedPressable>
        </RisingSheet>

        <RisingSheet
          visible={renameVisible}
          onClose={closeRename}
          cardStyle={styles.renameCard}
          avoidKeyboard
        >
          <Text style={styles.renameTitle}>Rename</Text>
          <TextInput
            style={styles.renameInput}
            value={renameDraft}
            onChangeText={setRenameDraft}
            placeholder="Agent name"
            placeholderTextColor={colors.textSecondary}
            autoCapitalize="none"
            autoCorrect={false}
            autoFocus
            selectTextOnFocus
          />
          <View style={styles.renameActions}>
            <AnimatedPressable
              style={styles.renameBtn}
              preset="press"
              scale={0.94}
              onPress={closeRename}
            >
              <Text style={styles.renameBtnText}>Cancel</Text>
            </AnimatedPressable>
            <AnimatedPressable
              style={[styles.renameBtn, styles.renameBtnPrimary]}
              preset="press"
              scale={0.94}
              onPress={handleRename}
            >
              <Text style={[styles.renameBtnText, styles.renameBtnPrimaryText]}>
                Save
              </Text>
            </AnimatedPressable>
          </View>
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

    renameCard: {
      borderRadius: 8,
      padding: 20,
      backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    renameTitle: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: colors.textPrimary,
      marginBottom: 16,
    },
    renameInput: {
      ...UiTextMetrics,
      ...TypeScale.mono,
      backgroundColor: colors.inputBackground,
      borderRadius: 8,
      minHeight: 44,
      paddingHorizontal: 14,
      paddingVertical: 12,
      color: colors.textPrimary,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    renameActions: {
      flexDirection: "row",
      justifyContent: "flex-end",
      gap: 10,
      marginTop: 20,
    },
    renameBtn: {
      minWidth: 76,
      minHeight: 44,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
    },
    renameBtnPrimary: {
      backgroundColor: colors.accent,
    },
    renameBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
    },
    renameBtnPrimaryText: {
      color: colors.textOnAccent,
    },
  });
}
