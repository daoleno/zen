import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  AppState,
  StyleSheet,
  View,
  type ScrollView,
  type AppStateStatus,
  useColorScheme,
  useWindowDimensions,
} from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAgents } from "../../store/agents";
import { useWork } from "../../store/work";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemePreference,
} from "../../constants/terminalThemes";
import {
  closeOtherTerminalTabs,
  closeTerminalTab,
  DefaultCodexRenderMode,
  getServerById,
  markAgentOpened,
  setAgentAlias,
  setCodexRenderMode,
  setTerminalTabPinned,
  touchTerminalTab,
  type StoredCodexRenderMode,
} from "../../services/storage";
import { makeSessionKey, parseSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";
import { GitDiffSheet } from "../../components/terminal/GitDiffSheet";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { TerminalAgentPickerSheet } from "../../components/terminal/TerminalAgentPickerSheet";
import {
  TerminalActionPopover,
  TERMINAL_ACTION_POPOVER_WIDTH,
} from "../../components/terminal/TerminalActionPopover";
import { TerminalRenameModal } from "../../components/terminal/TerminalRenameModal";
import { TerminalTopBar } from "../../components/terminal/TerminalTopBar";
import { TerminalViewport } from "../../components/terminal/TerminalViewport";
import { useTerminalAccessoryLayout } from "../../components/terminal/useTerminalAccessoryLayout";
import { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import { presentAgent } from "../../services/agentPresentation";
import {
  filterAgentsByPreferredServers,
  groupAgentsByDirectory,
} from "../../services/serverSelection";
import {
  buildMenuPosition,
  buildTerminalTabs,
  pickNextTabAfterClose,
  shouldShowPickerServerNames,
  sortTerminalAgents,
  type MenuAnchorLayout,
} from "./TerminalScreenModel";
import { useTerminalScreenStorage } from "./useTerminalScreenStorage";

export default function TerminalScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const { state } = useAgents();
  const { state: workState } = useWork();
  const router = useRouter();
  const colorScheme = useColorScheme();
  const { width: windowWidth } = useWindowDimensions();
  const [pickerVisible, setPickerVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<MenuAnchorLayout | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
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
  const liveAgentKeys = useMemo(
    () => state.agents.map((currentAgent) => currentAgent.key),
    [state.agents],
  );
  const hydratedServerIdSet = useMemo(
    () => new Set(hydratedServerIds),
    [hydratedServerIds],
  );
  const {
    themePreference,
    agentAliases,
    setAgentAliases,
    codexRenderModes,
    setCodexRenderModes,
    recentAgentOpens,
    setRecentAgentOpens,
    terminalTabs,
    setTerminalTabs,
    server,
    setServer,
    servers,
  } = useTerminalScreenStorage({
    serverId,
    sessionKey,
    hydratedServerIds,
    liveAgentKeys,
  });
  const themeName = useMemo(
    () => resolveTerminalThemePreference(themePreference, colorScheme),
    [themePreference, colorScheme],
  );
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(themeName),
    [themeName],
  );
  const chromeColors = useMemo(
    () => buildTerminalChrome(terminalTheme),
    [terminalTheme],
  );
  const agent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const gitDiffCwd = typeof agent?.cwd === "string" ? agent.cwd.trim() : "";
  const presentedAgent = useMemo(
    () =>
      presentAgent(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? agentAliases[sessionKey] : undefined,
      ),
    [agent, agentAliases, sessionKey],
  );

  const linkedWork = useMemo(
    () =>
      Object.values(workState.byKey)
        .filter((current) => current.serverId === serverId && current.frontmatter.agent_session === agentId)
        .sort((left, right) => {
          const leftTime = Date.parse(left.frontmatter.started || left.frontmatter.created || "");
          const rightTime = Date.parse(right.frontmatter.started || right.frontmatter.created || "");
          return (Number.isNaN(rightTime) ? 0 : rightTime) - (Number.isNaN(leftTime) ? 0 : leftTime);
        })[0],
    [agentId, workState.byKey, serverId],
  );
  const activePinned = sessionKey
    ? terminalTabs.pinned.includes(sessionKey)
    : false;
  const displayName = presentedAgent.title;
  const connectionState = serverId
    ? state.serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? state.serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && agentId);
  const isCodexAgent = presentedAgent.kind === "codex";
  const codexRenderMode: StoredCodexRenderMode = sessionKey
    ? codexRenderModes[sessionKey] ?? DefaultCodexRenderMode
    : DefaultCodexRenderMode;
  const showCodexChat = hasTerminalRoute && isCodexAgent && codexRenderMode === "chat";
  const canRenderTerminal = hasTerminalRoute && !showTerminalFallback && !showCodexChat;
  const shouldMountTerminalSurface = canRenderTerminal && screenFocused;
  const terminalStateAccent = connectionIssue
    ? terminalTheme.red
    : connectionState === "connecting"
      ? terminalTheme.yellow
      : chromeColors.textSubtle;
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
        ? "Zen is reconnecting before reopening this terminal."
        : "Start zen-daemon on that machine, or bring the network or tunnel back.");
  const terminalStateHint = !hasTerminalRoute
    ? "The app kept your route, but the live terminal is not ready yet."
    : connectionIssue?.hint ||
      "This terminal will reopen automatically once the daemon is reachable again.";
  const gitDiff = useTerminalGitDiff({
    serverId,
    agentId,
    cwd: gitDiffCwd,
    connectionState,
    hasTerminalRoute,
    screenFocused,
  });
  const accessoryVisible = canRenderTerminal && screenFocused;
  const {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  } = useTerminalAccessoryLayout({
    accessoryVisible,
    ctrlResetKey: sessionKey,
    ctrlDisabled: renameVisible,
  });

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
        handleCtrlArmedChange(false);
        syncActiveTerminal("background");
      };
    }, [handleCtrlArmedChange, syncActiveTerminal]),
  );

  useEffect(() => {
    setMenuVisible(false);
    setMenuAnchor(null);
  }, [sessionKey]);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

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
    return buildTerminalTabs({
      sessionKey,
      terminalTabs,
      agentByKey,
      hydratedServerIdSet,
      agentAliases,
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
      sortTerminalAgents({
        agents: displayAgents,
        terminalTabs,
        recentAgentOpens,
      }),
    [displayAgents, recentAgentOpens, terminalTabs],
  );

  const showPickerServerNames = useMemo(
    () => shouldShowPickerServerNames(sortedAgents),
    [sortedAgents],
  );
  const pickerSections = useMemo(
    () => groupAgentsByDirectory(sortedAgents, {
      showServerName: showPickerServerNames,
    }),
    [showPickerServerNames, sortedAgents],
  );

  const menuPosition = useMemo(
    () =>
      buildMenuPosition(
        menuAnchor,
        windowWidth,
        TERMINAL_ACTION_POPOVER_WIDTH,
      ),
    [menuAnchor, windowWidth],
  );

  const closeMenu = () => {
    setMenuVisible(false);
    setMenuAnchor(null);
  };

  const openGitDiff = () => {
    closeMenu();
    gitDiff.open();
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

  const applyCodexRenderMode = useCallback(
    async (mode: StoredCodexRenderMode) => {
      if (!sessionKey) return;
      const nextModes = await setCodexRenderMode(sessionKey, mode);
      setCodexRenderModes(nextModes);
      closeMenu();
    },
    [sessionKey],
  );

  const toggleCodexRenderMode = () => {
    void applyCodexRenderMode(
      codexRenderMode === "chat" ? "terminal" : "chat",
    );
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

  const openLinkedWork = () => {
    if (!linkedWork) return;
    closeMenu();
    router.push({
      pathname: "/work/[id]",
      params: { id: linkedWork.id, serverId: linkedWork.serverId },
    });
  };

  const retryServerConnection = async () => {
    if (!serverId) return;
    const storedServer = await getServerById(serverId);
    if (!storedServer) return;

    setServer(storedServer);
    wsClient.connectServer(storedServer);
  };

  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chromeColors.appBackground }]}
      edges={["top"]}
    >
      <StatusBar
        style={isLightTerminalTheme(terminalTheme) ? "dark" : "light"}
      />
      <TerminalTopBar
        tabs={tabs}
        backgroundColor={terminalTheme.background}
        chrome={chromeColors}
        tabScrollRef={tabScrollRef}
        menuAnchorRef={menuAnchorRef}
        onBack={goToInbox}
        onOpenTab={openAgentTab}
        onOpenMenu={openMenu}
        onNewTerminal={openNewTerminal}
        onTabLayout={(tabId, layout) => {
          tabLayoutsRef.current.set(tabId, layout);
        }}
      />

      <TerminalViewport
        showCodexChat={showCodexChat}
        sessionKey={sessionKey}
        serverId={serverId}
        agentId={agentId}
        agent={agent}
        connectionState={connectionState}
        connectionIssue={connectionIssue}
        theme={terminalTheme}
        chrome={chromeColors}
        themeName={themeName}
        screenFocused={screenFocused}
        gitDiff={gitDiff.chip}
        terminalRef={terminalRef}
        ctrlArmed={ctrlArmed}
        onCtrlArmedChange={handleCtrlArmedChange}
        canRenderTerminal={canRenderTerminal}
        shouldMountTerminalSurface={shouldMountTerminalSurface}
        terminalStateAccent={terminalStateAccent}
        terminalStateBusy={terminalStateBusy}
        terminalStateTitle={terminalStateTitle}
        terminalStateDetail={terminalStateDetail}
        terminalStateHint={terminalStateHint}
        hasTerminalRoute={hasTerminalRoute}
        isCodexAgent={isCodexAgent}
        outputBottomInset={outputBottomInset}
        accessoryVisible={accessoryVisible}
        accessoryBottomOffset={accessoryBottomOffset}
        serverUrl={server?.url || ""}
        daemonId={server?.daemonId || ""}
        keyboardVisible={keyboardVisible}
        onSwitchToTerminal={() => {
          void applyCodexRenderMode("terminal");
        }}
        onSwitchToChat={() => {
          void applyCodexRenderMode("chat");
        }}
        onOpenGitDiff={openGitDiff}
        onRetryConnection={() => {
          void retryServerConnection();
        }}
        onAccessoryLayout={handleAccessoryLayout}
      />

      <TerminalAgentPickerSheet
        visible={pickerVisible}
        sections={pickerSections}
        agentCount={sortedAgents.length}
        activeSessionKey={sessionKey}
        showServerNames={showPickerServerNames}
        agentAliases={agentAliases}
        creatingSession={creatingSession}
        chrome={chromeColors}
        onClose={() => setPickerVisible(false)}
        onOpenAgent={openAgentTab}
        onNewTerminal={openNewTerminal}
      />

      <TerminalActionPopover
        visible={menuVisible}
        left={menuPosition.left}
        top={menuPosition.top}
        creatingSession={creatingSession}
        newTerminalLabel={creatingSession ? "Starting Terminal…" : "New Terminal"}
        newTerminalDisabled={connectionState !== "connected"}
        gitDiffDisabled={gitDiff.actionDisabled}
        activePinned={activePinned}
        closeOtherTabsDisabled={tabs.length <= 1}
        codexRenderAction={
          isCodexAgent
            ? {
                icon: codexRenderMode === "chat" ? "terminal-outline" : "sparkles-outline",
                label: codexRenderMode === "chat" ? "Use Terminal" : "Use Codex Chat",
                onPress: toggleCodexRenderMode,
              }
            : null
        }
        showLinkedWork={Boolean(linkedWork)}
        chrome={chromeColors}
        theme={terminalTheme}
        onClose={closeMenu}
        onNewTerminal={openNewTerminal}
        onOpenGitDiff={openGitDiff}
        onRename={openRenameModal}
        onTogglePinned={handleTogglePinned}
        onCloseOtherTabs={handleCloseOtherTabs}
        onCloseTab={handleCloseCurrentTab}
        onOpenLinkedWork={openLinkedWork}
        onTerminate={handleTerminateAgent}
      />

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

      <GitDiffSheet
        theme={terminalTheme}
        {...gitDiff.sheetProps}
      />

      <TerminalRenameModal
        visible={renameVisible}
        draft={renameDraft}
        placeholder={agent?.name || agentId}
        chrome={chromeColors}
        theme={terminalTheme}
        onDraftChange={setRenameDraft}
        onClose={() => setRenameVisible(false)}
        onSave={handleSaveRename}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0D0C0C",
  },
});
