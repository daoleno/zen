import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Animated,
  AppState,
  AppStateStatus,
  Easing,
  Keyboard,
  KeyboardAvoidingView,
  KeyboardEvent,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  useWindowDimensions,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { Agent, useAgents } from "../../store/agents";
import {
  AgentStatus,
  Colors,
  Typography,
  statusColor,
} from "../../constants/tokens";
import {
  DefaultTerminalThemeName,
  TerminalThemeName,
} from "../../constants/terminalThemes";
import {
  closeOtherTerminalTabs,
  closeTerminalTab,
  getAgentAliases,
  getRecentAgentOpens,
  getServerById,
  getTerminalTabs,
  getTerminalTheme,
  markAgentOpened,
  setAgentAlias,
  setTerminalTabPinned,
  StoredAgentAliases,
  StoredRecentAgentOpens,
  StoredServer,
  StoredTerminalTabs,
  syncTerminalTabsWithLiveSessions,
  touchTerminalTab,
} from "../../services/storage";
import { makeSessionKey, parseSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";
import { connectionIssueAccent } from "../../services/connectionIssue";
import {
  TerminalSurface,
  TerminalSurfaceHandle,
} from "../../components/terminal/TerminalSurface";
import { TerminalAccessoryBar } from "../../components/terminal/TerminalAccessoryBar";
import { AgentKindIcon } from "../../components/terminal/AgentKindIcon";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { presentAgent } from "../../services/agentPresentation";

const EMPTY_TABS: StoredTerminalTabs = { order: [], pinned: [] };
const MENU_POPOVER_WIDTH = 168;

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

interface TerminalTabDescriptor {
  id: string;
  name: string;
  status: AgentStatus;
  kind: "terminal" | "claude" | "codex";
  pinned: boolean;
  active: boolean;
}

export default function TerminalScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const { state } = useAgents();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const [themeName, setThemeName] = useState<TerminalThemeName>(
    DefaultTerminalThemeName,
  );
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] =
    useState<StoredRecentAgentOpens>({});
  const [terminalTabs, setTerminalTabs] =
    useState<StoredTerminalTabs>(EMPTY_TABS);
  const [server, setServer] = useState<StoredServer | null>(null);
  const [pickerVisible, setPickerVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<{
    x: number;
    y: number;
    width: number;
    height: number;
  } | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [keyboardInset, setKeyboardInset] = useState(0);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const [newTerminalVisible, setNewTerminalVisible] = useState(false);
  const [creatingSession, setCreatingSession] = useState(false);
  const [showTerminalFallback, setShowTerminalFallback] = useState(
    !Boolean(sessionKey && serverId && agentId),
  );
  const [screenFocused, setScreenFocused] = useState(false);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);
  const tabScrollRef = useRef<ScrollView>(null);
  const tabLayoutsRef = useRef<Map<string, { x: number; width: number }>>(
    new Map(),
  );
  const tabSwipeTranslateX = useRef(new Animated.Value(0)).current;
  const tabSwipeAnimatingRef = useRef(false);
  const keyboardHeightRef = useRef(0);
  const baseWindowHeightRef = useRef(windowHeight);
  const menuAnchorRef = useRef<View | null>(null);
  const reconnectFallbackTimerRef = useRef<ReturnType<
    typeof setTimeout
  > | null>(null);

  const agentByKey = useMemo(
    () => new Map(state.agents.map((agent) => [agent.key, agent])),
    [state.agents],
  );
  const hydratedServerIds = useMemo(
    () =>
      Object.entries(state.hydratedServers)
        .filter(([, hydrated]) => hydrated)
        .map(([serverId]) => serverId),
    [state.hydratedServers],
  );
  const hydratedServerIdSet = useMemo(
    () => new Set(hydratedServerIds),
    [hydratedServerIds],
  );
  const agent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const activePinned = sessionKey
    ? terminalTabs.pinned.includes(sessionKey)
    : false;
  const displayName = useMemo(
    () =>
      presentAgent(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? agentAliases[sessionKey] : undefined,
      ).title,
    [agent, agentAliases, sessionKey],
  );
  const connectionState = serverId
    ? state.serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? state.serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && agentId);
  const canRenderTerminal = hasTerminalRoute && !showTerminalFallback;
  const shouldMountTerminalSurface = hasTerminalRoute && screenFocused;
  const terminalStateAccent = connectionIssue
    ? connectionIssueAccent(connectionIssue)
    : connectionState === "connecting"
      ? "#E7B65C"
      : "#65758A";
  const terminalStateBusy =
    hasTerminalRoute && connectionState === "connecting" && !connectionIssue;
  const terminalStateTitle = !hasTerminalRoute
    ? "Terminal unavailable"
    : connectionIssue?.title ||
      (connectionState === "connecting"
        ? "Reconnecting to daemon"
        : "Daemon unavailable");
  const terminalStateDetail = !hasTerminalRoute
    ? "Open this terminal again from the Agents tab."
    : connectionIssue?.detail ||
      (connectionState === "connecting"
        ? "zen is reconnecting before reopening this terminal."
        : "Start zen-daemon on that machine, or bring the network or tunnel back.");
  const terminalStateHint = !hasTerminalRoute
    ? "The app kept your route, but the live terminal is not ready yet."
    : connectionIssue?.hint ||
      "This terminal will reopen automatically once the daemon is reachable again.";

  const syncActiveTerminal = React.useCallback(
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
    React.useCallback(() => {
      setScreenFocused(true);
      syncActiveTerminal();

      const appStateSub = AppState.addEventListener("change", (nextState) => {
        syncActiveTerminal(nextState);
      });

      return () => {
        appStateSub.remove();
        setScreenFocused(false);
        setCtrlArmed(false);
        syncActiveTerminal("background");
      };
    }, [syncActiveTerminal]),
  );

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [storedTheme, storedRecentOpens, storedAliases] = await Promise.all(
        [getTerminalTheme(), getRecentAgentOpens(), getAgentAliases()],
      );
      const storedServer = serverId ? await getServerById(serverId) : null;

      if (!sessionKey) {
        const storedTabs = await getTerminalTabs();
        if (!cancelled) {
          setThemeName(storedTheme);
          setAgentAliases(storedAliases);
          setRecentAgentOpens(storedRecentOpens);
          setTerminalTabs(storedTabs);
          setServer(storedServer);
        }
        return;
      }

      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(sessionKey);
      void markAgentOpened(sessionKey, openedAt);

      if (!cancelled) {
        setThemeName(storedTheme);
        setAgentAliases(storedAliases);
        setRecentAgentOpens({
          ...storedRecentOpens,
          [sessionKey]: openedAt,
        });
        setTerminalTabs(nextTabs);
        setServer(storedServer);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [sessionKey]);

  useEffect(() => {
    setMenuVisible(false);
    setMenuAnchor(null);
  }, [sessionKey]);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

  useEffect(() => {
    if (hydratedServerIds.length === 0) return;

    let cancelled = false;

    (async () => {
      const nextTabs = await syncTerminalTabsWithLiveSessions(
        state.agents.map((currentAgent) => currentAgent.key),
        hydratedServerIds,
      );
      if (!cancelled) {
        setTerminalTabs(nextTabs);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [hydratedServerIds, state.agents]);

  useEffect(() => {
    const handleShow = (event: KeyboardEvent) => {
      if (Platform.OS === "android") {
        keyboardHeightRef.current = event.endCoordinates.height;
      }
      setKeyboardVisible(true);
    };
    const handleHide = () => {
      setKeyboardVisible(false);
      keyboardHeightRef.current = 0;
      setKeyboardInset(0);
    };

    const showSub = Keyboard.addListener("keyboardDidShow", handleShow);
    const hideSub = Keyboard.addListener("keyboardDidHide", handleHide);
    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  // Track base window height when keyboard is hidden
  useEffect(() => {
    if (!keyboardVisible) {
      baseWindowHeightRef.current = windowHeight;
    }
  }, [keyboardVisible, windowHeight]);

  // Compute Android keyboard inset: keyboardHeight minus what adjustResize handled.
  // windowHeight is reactive — when adjustResize completes, it updates and this
  // re-runs, converging to the correct padding automatically.
  useEffect(() => {
    if (!keyboardVisible || Platform.OS !== "android") return;
    const kbHeight = keyboardHeightRef.current;
    if (!kbHeight) return;

    const adjustResizeHandled = Math.max(
      0,
      baseWindowHeightRef.current - windowHeight,
    );
    const remaining = Math.max(0, kbHeight - adjustResizeHandled);
    setKeyboardInset((prev) =>
      Math.abs(prev - remaining) <= 1 ? prev : remaining,
    );
  }, [keyboardVisible, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible) {
      setCtrlArmed(false);
    }
  }, [keyboardVisible]);

  useEffect(() => {
    setCtrlArmed(false);
  }, [sessionKey]);

  useEffect(() => {
    tabSwipeAnimatingRef.current = false;
    tabSwipeTranslateX.stopAnimation();
    tabSwipeTranslateX.setValue(0);
  }, [sessionKey, tabSwipeTranslateX]);

  useEffect(() => {
    if (renameVisible) {
      setCtrlArmed(false);
    }
  }, [renameVisible]);

  useEffect(() => {
    if (reconnectFallbackTimerRef.current) {
      clearTimeout(reconnectFallbackTimerRef.current);
      reconnectFallbackTimerRef.current = null;
    }

    if (!hasTerminalRoute) {
      setShowTerminalFallback(true);
      return;
    }

    if (connectionState === "connected") {
      setShowTerminalFallback(false);
      return;
    }

    if (connectionIssue) {
      setShowTerminalFallback(true);
      return;
    }

    setShowTerminalFallback(false);
    reconnectFallbackTimerRef.current = setTimeout(() => {
      setShowTerminalFallback(true);
      reconnectFallbackTimerRef.current = null;
    }, 1500);

    return () => {
      if (reconnectFallbackTimerRef.current) {
        clearTimeout(reconnectFallbackTimerRef.current);
        reconnectFallbackTimerRef.current = null;
      }
    };
  }, [connectionIssue, connectionState, hasTerminalRoute]);

  const tabs = useMemo(() => {
    const order = buildDisplayTabOrder(sessionKey, terminalTabs);
    return order
      .filter((currentSessionKey) => {
        if (currentSessionKey === sessionKey) return true;
        if (agentByKey.has(currentSessionKey)) return true;

        const parsed = parseSessionKey(currentSessionKey);
        return parsed ? !hydratedServerIdSet.has(parsed.serverId) : false;
      })
      .map((currentSessionKey) => {
        const tabAgent = agentByKey.get(currentSessionKey);
        const parsed = parseSessionKey(currentSessionKey);
        const presented = presentAgent(
          tabAgent || {
            name: parsed?.agentId || "",
            summary: "",
            last_output_lines: [],
          },
          currentSessionKey ? agentAliases[currentSessionKey] : undefined,
        );
        return {
          id: currentSessionKey,
          name: presented.cwdBase || presented.shortTitle,
          status: tabAgent?.status || "unknown",
          kind: presented.kind,
          pinned: terminalTabs.pinned.includes(currentSessionKey),
          active: currentSessionKey === sessionKey,
        } satisfies TerminalTabDescriptor;
      });
  }, [agentAliases, agentByKey, hydratedServerIdSet, sessionKey, terminalTabs]);

  // Auto-scroll to keep the active tab visible
  useEffect(() => {
    if (!sessionKey) return;
    const layout = tabLayoutsRef.current.get(sessionKey);
    if (layout && tabScrollRef.current) {
      const scrollTo = Math.max(0, layout.x - 40);
      tabScrollRef.current.scrollTo({ x: scrollTo, animated: true });
    }
  }, [sessionKey]);

  const previousTab = useMemo(
    () => getAdjacentTab(tabs, sessionKey, "prev"),
    [sessionKey, tabs],
  );
  const nextTab = useMemo(
    () => getAdjacentTab(tabs, sessionKey, "next"),
    [sessionKey, tabs],
  );

  const sortedAgents = useMemo(() => {
    const openTabs = new Set(terminalTabs.order);
    const pinnedTabs = new Set(terminalTabs.pinned);

    return [...state.agents].sort((left, right) => {
      const leftPinned = pinnedTabs.has(left.key) ? 0 : 1;
      const rightPinned = pinnedTabs.has(right.key) ? 0 : 1;
      if (leftPinned !== rightPinned) return leftPinned - rightPinned;

      const leftOpen = openTabs.has(left.key) ? 0 : 1;
      const rightOpen = openTabs.has(right.key) ? 0 : 1;
      if (leftOpen !== rightOpen) return leftOpen - rightOpen;

      const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;

      return (right.updated_at || 0) - (left.updated_at || 0);
    });
  }, [recentAgentOpens, state.agents, terminalTabs]);

  const menuPosition = useMemo(
    () => buildMenuPosition(menuAnchor, windowWidth),
    [menuAnchor, windowWidth],
  );
  const previousHintOpacity = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [0, 14, 80],
        outputRange: [0, 0.3, 1],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );
  const nextHintOpacity = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [-80, -14, 0],
        outputRange: [1, 0.3, 0],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );
  const previousHintTranslate = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [0, 80],
        outputRange: [-12, 0],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );
  const terminalSwipeScale = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [-180, 0, 180],
        outputRange: [0.982, 1, 0.982],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );
  const terminalSwipeOpacity = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [-180, 0, 180],
        outputRange: [0.96, 1, 0.96],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );
  const nextHintTranslate = useMemo(
    () =>
      tabSwipeTranslateX.interpolate({
        inputRange: [-80, 0],
        outputRange: [0, 12],
        extrapolate: "clamp",
      }),
    [tabSwipeTranslateX],
  );

  const accessoryVisible = canRenderTerminal && screenFocused;

  const closeMenu = () => {
    setMenuVisible(false);
    setMenuAnchor(null);
  };

  const openRenameModal = () => {
    closeMenu();
    setRenameDraft(displayName);
    setRenameVisible(true);
  };

  const openAgentTab = async (agentId: string) => {
    setPickerVisible(false);
    closeMenu();

    if (!agentId || agentId === sessionKey) return;
    const parsed = parseSessionKey(agentId);
    if (!parsed) return;
    if (!agentByKey.has(agentId) && hydratedServerIdSet.has(parsed.serverId)) {
      const nextTabs = await closeTerminalTab(agentId);
      setTerminalTabs(nextTabs);
      return;
    }

    router.replace({
      pathname: "/terminal/[id]",
      params: { id: parsed.agentId, serverId: parsed.serverId },
    });
  };

  const goToInbox = () => {
    setPickerVisible(false);
    closeMenu();
    router.replace("/");
  };

  const openMenu = () => {
    const anchor = menuAnchorRef.current;
    if (!anchor) {
      setMenuAnchor(null);
      setMenuVisible(true);
      return;
    }

    anchor.measureInWindow((x, y, width, height) => {
      setMenuAnchor({ x, y, width, height });
      setMenuVisible(true);
    });
  };

  const handleTogglePinned = async () => {
    if (!sessionKey) return;
    const nextTabs = await setTerminalTabPinned(sessionKey, !activePinned);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleCloseCurrentTab = async () => {
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
  };

  const handleCloseOtherTabs = async () => {
    if (!sessionKey) return;

    const nextTabs = await closeOtherTerminalTabs(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleTerminateAgent = () => {
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
        (agent?.serverName || serverId) +
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
  };

  const performTerminateAgent = async () => {
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
  };

  const handleSaveRename = async () => {
    if (!sessionKey) return;
    const nextAliases = await setAgentAlias(sessionKey, renameDraft);
    setAgentAliases(nextAliases);
    setRenameVisible(false);
  };

  const handleCtrlArmedChange = (next: boolean) => {
    setCtrlArmed(next);
  };

  const createTerminal = async (input: {
    cwd: string;
    command: string;
    name: string;
  }) => {
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
    setPickerVisible(false);
    closeMenu();
    setCreatingSession(true);
    try {
      const nextAgentId = await wsClient.createSession(serverId, {
        targetId: agentId,
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
      const nextSessionKey = makeSessionKey(serverId, nextAgentId);
      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(nextSessionKey);
      setTerminalTabs(nextTabs);
      void markAgentOpened(nextSessionKey, openedAt);
      setRecentAgentOpens((previous) => ({
        ...previous,
        [nextSessionKey]: openedAt,
      }));
      router.replace({
        pathname: "/terminal/[id]",
        params: { id: nextAgentId, serverId },
      });
    } catch (error: any) {
      Alert.alert(
        "Could not create terminal",
        error?.message || "Try reconnecting to that daemon first.",
      );
    } finally {
      setCreatingSession(false);
    }
  };

  const openNewTerminal = () => {
    if (connectionState !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before creating a new terminal.",
      );
      return;
    }
    setNewTerminalVisible(true);
  };

  const retryServerConnection = async () => {
    if (!serverId) return;
    const storedServer = await getServerById(serverId);
    if (!storedServer) return;

    setServer(storedServer);
    wsClient.connectServer(storedServer);
  };

  const animateTabSwipeBack = () => {
    tabSwipeAnimatingRef.current = false;
    Animated.spring(tabSwipeTranslateX, {
      toValue: 0,
      useNativeDriver: true,
      damping: 22,
      stiffness: 240,
      mass: 0.8,
    }).start();
  };

  const handleTabSwipeProgress = (deltaX: number, active: boolean) => {
    if (tabSwipeAnimatingRef.current) return;

    if (!active) {
      animateTabSwipeBack();
      return;
    }

    const direction = deltaX < 0 ? "next" : "prev";
    const targetExists =
      direction === "next" ? Boolean(nextTab) : Boolean(previousTab);
    const maxOffset = targetExists ? Math.min(windowWidth * 0.24, 110) : 56;
    const resistance = targetExists ? 0.34 : 0.16;
    const previewOffset = clamp(deltaX * resistance, -maxOffset, maxOffset);

    tabSwipeTranslateX.setValue(previewOffset);
  };

  const handleTabSwipe = (direction: "next" | "prev") => {
    if (
      tabSwipeAnimatingRef.current ||
      pickerVisible ||
      menuVisible ||
      renameVisible ||
      !sessionKey ||
      tabs.length <= 1
    )
      return;

    const targetTab = direction === "next" ? nextTab : previousTab;
    if (!targetTab) {
      animateTabSwipeBack();
      return;
    }

    tabSwipeAnimatingRef.current = true;
    setCtrlArmed(false);
    Animated.timing(tabSwipeTranslateX, {
      toValue: direction === "next" ? -(windowWidth + 24) : windowWidth + 24,
      duration: 230,
      easing: Easing.bezier(0.22, 1, 0.36, 1),
      useNativeDriver: true,
    }).start(() => {
      void openAgentTab(targetTab.id);
    });
  };

  return (
    <SafeAreaView style={styles.container} edges={["top"]}>
      <View style={styles.topBar}>
        <TouchableOpacity
          onPress={goToInbox}
          style={styles.chromeButton}
          activeOpacity={0.84}
        >
          <Ionicons name="chevron-back" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>

        <ScrollView
          ref={tabScrollRef}
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.tabScroller}
          contentContainerStyle={styles.tabScrollerContent}
        >
          {tabs.map((tab) => (
            <View
              key={tab.id}
              style={[
                styles.tabPill,
                tab.active && styles.tabPillActive,
                tab.pinned && styles.tabPillPinned,
              ]}
              onLayout={(e) => {
                const { x, width } = e.nativeEvent.layout;
                tabLayoutsRef.current.set(tab.id, { x, width });
              }}
            >
              <TouchableOpacity
                style={styles.tabMainButton}
                onPress={() => openAgentTab(tab.id)}
                activeOpacity={0.84}
              >
                <AgentKindIcon kind={tab.kind} size={13} />
                <Text
                  style={[styles.tabLabel, tab.active && styles.tabLabelActive]}
                  numberOfLines={1}
                >
                  {tab.name}
                </Text>
                {tab.pinned ? (
                  <Ionicons
                    name="bookmark"
                    size={12}
                    color={tab.active ? Colors.textPrimary : "#73839A"}
                  />
                ) : null}
                <View
                  style={[
                    styles.tabStatusDot,
                    { backgroundColor: statusColor(tab.status) },
                  ]}
                />
              </TouchableOpacity>

              {tab.active ? (
                <View ref={menuAnchorRef} collapsable={false}>
                  <TouchableOpacity
                    style={styles.tabMenuButton}
                    onPress={openMenu}
                    activeOpacity={0.84}
                  >
                    <Ionicons
                      name="ellipsis-vertical"
                      size={17}
                      color={Colors.textPrimary}
                    />
                  </TouchableOpacity>
                </View>
              ) : null}
            </View>
          ))}
        </ScrollView>

        <TouchableOpacity
          onPress={() => setPickerVisible(true)}
          style={styles.chromeButton}
          activeOpacity={0.84}
        >
          <Ionicons name="add" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>
      </View>

      <View style={styles.terminalStage}>
        {previousTab ? (
          <Animated.View
            pointerEvents="none"
            style={[
              styles.swipeHint,
              styles.swipeHintLeft,
              {
                opacity: previousHintOpacity,
                transform: [{ translateX: previousHintTranslate }],
              },
            ]}
          >
            <Ionicons name="chevron-back" size={15} color="#DDE5F2" />
            <Text style={styles.swipeHintText} numberOfLines={1}>
              {previousTab?.name}
            </Text>
          </Animated.View>
        ) : null}

        {nextTab ? (
          <Animated.View
            pointerEvents="none"
            style={[
              styles.swipeHint,
              styles.swipeHintRight,
              {
                opacity: nextHintOpacity,
                transform: [{ translateX: nextHintTranslate }],
              },
            ]}
          >
            <Text style={styles.swipeHintText} numberOfLines={1}>
              {nextTab?.name}
            </Text>
            <Ionicons name="chevron-forward" size={15} color="#DDE5F2" />
          </Animated.View>
        ) : null}

        <Animated.View
          style={[
            styles.terminalShell,
            Platform.OS === "android" && keyboardInset > 0
              ? { paddingBottom: keyboardInset }
              : null,
            {
              opacity: terminalSwipeOpacity,
              transform: [
                { translateX: tabSwipeTranslateX },
                { scale: terminalSwipeScale },
              ],
            },
          ]}
        >
          <KeyboardAvoidingView
            style={styles.terminalContent}
            behavior={Platform.OS === "ios" ? "padding" : undefined}
          >
            <View style={styles.output}>
              {shouldMountTerminalSurface && sessionKey && serverId && agentId ? (
                <TerminalSurface
                  key={sessionKey}
                  ref={terminalRef}
                  serverId={serverId}
                  targetId={agentId}
                  themeName={themeName}
                  ctrlArmed={ctrlArmed}
                  onCtrlArmedChange={handleCtrlArmedChange}
                  onTabSwipeProgress={handleTabSwipeProgress}
                  onTabSwipe={handleTabSwipe}
                />
              ) : null}
              {canRenderTerminal ? null : (
                <View style={styles.terminalState}>
                  <View
                    style={[
                      styles.terminalStateCard,
                      { borderColor: terminalStateAccent },
                    ]}
                  >
                    {terminalStateBusy ? (
                      <ActivityIndicator color={terminalStateAccent} />
                    ) : (
                      <View
                        style={[
                          styles.terminalStateDot,
                          { backgroundColor: terminalStateAccent },
                        ]}
                      />
                    )}
                    <Text style={styles.terminalStateTitle}>
                      {terminalStateTitle}
                    </Text>
                    <Text style={styles.terminalStateDetail}>
                      {terminalStateDetail}
                    </Text>
                    <Text style={styles.terminalStateHint}>
                      {terminalStateHint}
                    </Text>
                    {hasTerminalRoute ? (
                      <TouchableOpacity
                        style={styles.terminalStateAction}
                        onPress={() => void retryServerConnection()}
                        activeOpacity={0.84}
                      >
                        <Text style={styles.terminalStateActionText}>
                          Retry Connection
                        </Text>
                      </TouchableOpacity>
                    ) : null}
                  </View>
                </View>
              )}
            </View>

            {accessoryVisible ? (
              <View
                style={[
                  styles.inputShell,
                  {
                    paddingBottom: keyboardVisible
                      ? 6
                      : Math.max(insets.bottom + 8, 12),
                    marginBottom: keyboardVisible ? 4 : 0,
                  },
                ]}
              >
                <TerminalAccessoryBar
                  terminalRef={terminalRef}
                  serverUrl={server?.url || ""}
                  daemonId={server?.daemonId || ""}
                  ctrlArmed={ctrlArmed}
                  onCtrlArmedChange={handleCtrlArmedChange}
                />
              </View>
            ) : null}
          </KeyboardAvoidingView>
        </Animated.View>
      </View>

      <Modal
        visible={pickerVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setPickerVisible(false)}
      >
        <View style={styles.modalRoot}>
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setPickerVisible(false)}
          />

          <View style={styles.sheetCard}>
            <View style={styles.sheetHandle} />

            <ScrollView
              style={styles.sheetScroll}
              contentContainerStyle={styles.sheetScrollContent}
              showsVerticalScrollIndicator={false}
            >
              {sortedAgents.length === 0 ? (
                <Text style={styles.sheetEmpty}>No agents available.</Text>
              ) : (
                sortedAgents.map((item) => {
                  const isActive = item.key === sessionKey;
                  const presented = presentAgent(item, agentAliases[item.key]);

                  return (
                    <TouchableOpacity
                      key={item.key}
                      style={[
                        styles.agentRow,
                        isActive && styles.agentRowActive,
                      ]}
                      onPress={() => openAgentTab(item.key)}
                      activeOpacity={0.84}
                    >
                      <AgentKindIcon kind={presented.kind} size={15} />
                      <Text style={styles.agentRowTitle} numberOfLines={1}>
                        {presented.cwdBase || presented.title}
                      </Text>
                      {item.serverName ? (
                        <Text style={styles.agentRowMeta} numberOfLines={1}>
                          {item.serverName}
                        </Text>
                      ) : null}
                      <View
                        style={[
                          styles.agentRowStatusDot,
                          { backgroundColor: statusColor(item.status) },
                        ]}
                      />
                    </TouchableOpacity>
                  );
                })
              )}
            </ScrollView>

            <TouchableOpacity
              style={[
                styles.sheetCreateButton,
                creatingSession && styles.sheetCreateButtonDisabled,
              ]}
              onPress={openNewTerminal}
              disabled={creatingSession}
              activeOpacity={0.84}
            >
              <Ionicons name="add" size={16} color={Colors.textSecondary} />
              <Text style={styles.sheetCreateButtonText}>
                {creatingSession ? "Starting…" : "New Terminal"}
              </Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

      <Modal
        visible={menuVisible}
        transparent
        animationType="none"
        onRequestClose={closeMenu}
      >
        <View style={styles.popoverRoot}>
          <TouchableOpacity
            style={styles.popoverBackdrop}
            activeOpacity={1}
            onPress={closeMenu}
          />

          <View
            style={[
              styles.menuPopover,
              {
                left: menuPosition.left,
                top: menuPosition.top,
                width: MENU_POPOVER_WIDTH,
              },
            ]}
          >
            <MenuAction
              label={creatingSession ? "Starting Terminal…" : "New Terminal"}
              onPress={openNewTerminal}
              disabled={creatingSession || connectionState !== "connected"}
            />
            <MenuAction label="Rename" onPress={openRenameModal} />
            <MenuAction
              label={activePinned ? "Unpin Tab" : "Pin Tab"}
              onPress={handleTogglePinned}
            />
            <MenuAction
              label="Close Other Tabs"
              onPress={handleCloseOtherTabs}
              disabled={tabs.length <= 1}
            />
            <MenuAction label="Close Tab" onPress={handleCloseCurrentTab} />
            <MenuAction
              label="Terminate"
              onPress={handleTerminateAgent}
              destructive
            />
          </View>
        </View>
      </Modal>

      <NewTerminalSheet
        visible={newTerminalVisible}
        title="New Terminal"
        subtitle="Start a plain shell here, or launch Claude/Codex in the current project."
        initialCwd={agent?.cwd || ""}
        selectedServerId={serverId}
        submitting={creatingSession}
        onClose={() => setNewTerminalVisible(false)}
        onSubmit={(input) => {
          void createTerminal({
            cwd: input.cwd,
            command: input.command,
            name: input.name,
          });
        }}
      />

      <Modal
        visible={renameVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setRenameVisible(false)}
      >
        <KeyboardAvoidingView
          style={styles.renameRoot}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setRenameVisible(false)}
          />

          <View style={styles.renameCard}>
            <Text style={styles.renameTitle}>Rename Terminal</Text>
            <Text style={styles.renameHint}>
              Only changes the local display name on this device.
            </Text>
            <TextInput
              style={styles.renameInput}
              value={renameDraft}
              onChangeText={setRenameDraft}
              placeholder={agent?.name || agentId}
              placeholderTextColor="#6E7D90"
              autoFocus
              autoCorrect={false}
              autoCapitalize="none"
              returnKeyType="done"
              onSubmitEditing={handleSaveRename}
            />
            <View style={styles.renameActions}>
              <TouchableOpacity
                style={styles.renameButton}
                onPress={() => setRenameVisible(false)}
                activeOpacity={0.84}
              >
                <Text style={styles.renameButtonText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.renameButton, styles.renameButtonPrimary]}
                onPress={handleSaveRename}
                activeOpacity={0.84}
              >
                <Text
                  style={[
                    styles.renameButtonText,
                    styles.renameButtonTextPrimary,
                  ]}
                >
                  Save
                </Text>
              </TouchableOpacity>
            </View>
          </View>
        </KeyboardAvoidingView>
      </Modal>
    </SafeAreaView>
  );
}

function MenuAction({
  label,
  onPress,
  disabled = false,
  destructive = false,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  destructive?: boolean;
}) {
  return (
    <TouchableOpacity
      style={[styles.menuAction, disabled && styles.menuActionDisabled]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.84}
    >
      <Text
        style={[
          styles.menuActionText,
          destructive && styles.menuActionTextDestructive,
          disabled && styles.menuActionTextDisabled,
        ]}
      >
        {label}
      </Text>
    </TouchableOpacity>
  );
}

function buildDisplayTabOrder(
  currentId: string | null | undefined,
  tabs: StoredTerminalTabs,
): string[] {
  if (!currentId) return tabs.order;
  return tabs.order.includes(currentId)
    ? tabs.order
    : [...tabs.order, currentId];
}

function getAdjacentTab(
  tabs: TerminalTabDescriptor[],
  currentId: string | null | undefined,
  direction: "next" | "prev",
): TerminalTabDescriptor | null {
  if (!currentId) return null;

  const currentIndex = tabs.findIndex((tab) => tab.id === currentId);
  if (currentIndex === -1) return null;

  return (
    tabs[direction === "next" ? currentIndex + 1 : currentIndex - 1] ?? null
  );
}

function buildMenuPosition(
  anchor: { x: number; y: number; width: number; height: number } | null,
  windowWidth: number,
): { left: number; top: number } {
  const top = Math.max(12, (anchor?.y ?? 12) + (anchor?.height ?? 38) + 16);
  const preferredLeft =
    (anchor?.x ?? windowWidth - 14) + (anchor?.width ?? 0) - MENU_POPOVER_WIDTH;
  const maxLeft = Math.max(12, windowWidth - MENU_POPOVER_WIDTH - 12);

  return {
    left: clamp(preferredLeft, 12, maxLeft),
    top,
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function pickNextTabAfterClose(
  closedId: string,
  currentTabs: StoredTerminalTabs,
  nextTabs: StoredTerminalTabs,
): string | null {
  const currentOrder = buildDisplayTabOrder(null, currentTabs);
  const nextOrder = buildDisplayTabOrder(null, nextTabs);
  const closedIndex = currentOrder.indexOf(closedId);

  if (closedIndex === -1) return nextOrder[0] || null;

  return currentOrder[closedIndex + 1] || currentOrder[closedIndex - 1] || null;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0B1118",
  },
  topBar: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    paddingTop: 4,
    paddingBottom: 8,
    borderBottomWidth: 1,
    borderBottomColor: "#161E2A",
    backgroundColor: "#11161F",
  },
  terminalStage: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    justifyContent: "center",
  },
  terminalShell: {
    flex: 1,
    minHeight: 0,
  },
  swipeHint: {
    position: "absolute",
    top: "50%",
    zIndex: 1,
    maxWidth: "46%",
    minHeight: 40,
    paddingHorizontal: 12,
    borderRadius: 16,
    backgroundColor: "rgba(20, 30, 44, 0.92)",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.08)",
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    shadowColor: "#000",
    shadowOpacity: 0.18,
    shadowRadius: 14,
    shadowOffset: { width: 0, height: 8 },
  },
  swipeHintLeft: {
    left: 12,
  },
  swipeHintRight: {
    right: 12,
  },
  swipeHintText: {
    flexShrink: 1,
    color: "#DDE5F2",
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  terminalContent: {
    flex: 1,
    minHeight: 0,
  },
  chromeButton: {
    width: 38,
    height: 38,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#1B2230",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.05)",
  },
  tabScroller: {
    flex: 1,
    marginHorizontal: 8,
  },
  tabScrollerContent: {
    paddingRight: 2,
  },
  tabPill: {
    minWidth: 140,
    maxWidth: 220,
    height: 38,
    borderRadius: 13,
    paddingLeft: 10,
    paddingRight: 6,
    marginRight: 6,
    flexDirection: "row",
    alignItems: "center",
    backgroundColor: "#262633",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.04)",
  },
  tabPillActive: {
    backgroundColor: "#5A5A67",
    borderColor: "rgba(255,255,255,0.08)",
  },
  tabPillPinned: {
    shadowColor: "#5B9DFF",
    shadowOpacity: 0.12,
    shadowRadius: 5,
  },
  tabMainButton: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  tabStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    marginLeft: 8,
  },
  tabLabel: {
    flex: 1,
    color: "#C6CDDA",
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    marginRight: 6,
  },
  tabLabelActive: {
    color: "#F4F6FA",
  },
  tabMenuButton: {
    width: 24,
    height: 24,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  output: {
    flex: 1,
    minHeight: 0,
    paddingTop: 4,
  },
  terminalState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    paddingBottom: 32,
  },
  terminalStateCard: {
    width: "100%",
    maxWidth: 360,
    alignItems: "center",
    paddingHorizontal: 18,
    paddingVertical: 20,
    borderRadius: 20,
    borderWidth: 1,
    backgroundColor: "rgba(17,22,31,0.9)",
  },
  terminalStateDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  terminalStateTitle: {
    marginTop: 12,
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  terminalStateDetail: {
    marginTop: 8,
    color: "#D6DFEC",
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateHint: {
    marginTop: 8,
    color: "#8E9DB2",
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateAction: {
    marginTop: 16,
    minHeight: 38,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  terminalStateActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  inputShell: {
    paddingHorizontal: 12,
    paddingTop: 6,
    backgroundColor: "#0B1118",
  },
  modalRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  popoverRoot: {
    flex: 1,
  },
  popoverBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "transparent",
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(6, 8, 12, 0.58)",
  },
  sheetCard: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: "#121A25",
    borderTopWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    maxHeight: "82%",
  },
  sheetHandle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: "#3A475B",
    marginBottom: 14,
  },
  sheetCreateButton: {
    marginTop: 12,
    height: 40,
    borderRadius: 12,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    borderStyle: "dashed" as const,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  sheetCreateButtonDisabled: {
    opacity: 0.5,
  },
  sheetCreateButtonText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  sheetScroll: {
    marginTop: 4,
  },
  sheetScrollContent: {
    paddingBottom: 8,
  },
  sheetEmpty: {
    color: "#7D8CA0",
    fontSize: 13,
    fontFamily: Typography.uiFont,
    paddingVertical: 12,
  },
  agentRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: "rgba(255,255,255,0.04)",
  },
  agentRowActive: {
    opacity: 1,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  agentRowTitle: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  agentRowMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },
  menuPopover: {
    position: "absolute",
    borderRadius: 14,
    paddingVertical: 4,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    shadowColor: "#000",
    shadowOpacity: 0.2,
    shadowRadius: 8,
    elevation: 8,
  },
  menuAction: {
    minHeight: 38,
    justifyContent: "center",
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  menuActionDisabled: {
    opacity: 0.52,
  },
  menuActionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  menuActionTextDisabled: {
    color: "#556176",
  },
  menuActionTextDestructive: {
    color: "#F09999",
  },
  renameRoot: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 20,
  },
  renameCard: {
    borderRadius: 18,
    padding: 16,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
  },
  renameTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
  },
  renameHint: {
    marginTop: 4,
    color: "#7D8CA0",
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  renameInput: {
    marginTop: 14,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: "#263345",
    backgroundColor: "#111923",
    color: Colors.textPrimary,
    paddingHorizontal: 12,
    paddingVertical: 11,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  renameActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    marginTop: 14,
    gap: 10,
  },
  renameButton: {
    minWidth: 72,
    height: 38,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#202A38",
  },
  renameButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  renameButtonText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  renameButtonTextPrimary: {
    color: "#07111E",
  },
});
