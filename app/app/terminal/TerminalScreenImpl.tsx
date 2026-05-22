import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  StyleSheet,
  useWindowDimensions,
} from "react-native";
import { useLocalSearchParams } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAgents } from "../../store/agents";
import { useWork } from "../../store/work";
import {
  DefaultCodexRenderMode,
  type StoredCodexRenderMode,
} from "../../services/storage";
import { makeSessionKey } from "../../services/sessionKeys";
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
  buildTerminalFallbackPresentation,
  buildTerminalTabs,
  findLinkedWork,
} from "./TerminalScreenModel";
import { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import { useTerminalFallbackState } from "./useTerminalFallbackState";
import { useTerminalFocusLifecycle } from "./useTerminalFocusLifecycle";
import { useTerminalPickerModel } from "./useTerminalPickerModel";
import { useTerminalScreenStorage } from "./useTerminalScreenStorage";
import { useTerminalSessionActions } from "./useTerminalSessionActions";
import { useTerminalTabActions } from "./useTerminalTabActions";
import { useTerminalThemeChrome } from "./useTerminalThemeChrome";

export default function TerminalScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const { state } = useAgents();
  const { state: workState } = useWork();
  const { width: windowWidth } = useWindowDimensions();
  const [pickerVisible, setPickerVisible] = useState(false);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [newTerminalVisible, setNewTerminalVisible] = useState(false);
  const [creatingSession, setCreatingSession] = useState(false);
  const [screenFocused, setScreenFocused] = useState(false);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);
  const {
    closeMenu,
    handleTabLayout,
    menuAnchorRef,
    menuPosition,
    menuVisible,
    openMenu,
    tabScrollRef,
  } = useTerminalChromeLayout({
    sessionKey,
    windowWidth,
    popoverWidth: TERMINAL_ACTION_POPOVER_WIDTH,
  });

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
  const { chromeColors, statusBarStyle, terminalTheme, themeName } =
    useTerminalThemeChrome(themePreference);
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
    () => findLinkedWork(workState.byKey, serverId, agentId),
    [agentId, serverId, workState.byKey],
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
  const showCodexChat =
    hasTerminalRoute && isCodexAgent && codexRenderMode === "chat";
  const showTerminalFallback = useTerminalFallbackState({
    hasTerminalRoute,
    connectionState,
    connectionIssue,
  });
  const canRenderTerminal =
    hasTerminalRoute && !showTerminalFallback && !showCodexChat;
  const shouldMountTerminalSurface = canRenderTerminal && screenFocused;
  const terminalState = useMemo(
    () =>
      buildTerminalFallbackPresentation({
        hasTerminalRoute,
        connectionState,
        connectionIssue,
        terminalTheme,
        chromeColors,
      }),
    [
      chromeColors,
      connectionIssue,
      connectionState,
      hasTerminalRoute,
      terminalTheme,
    ],
  );
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

  const handleTerminalInactive = useCallback(() => {
    handleCtrlArmedChange(false);
  }, [handleCtrlArmedChange]);
  useTerminalFocusLifecycle({
    serverId,
    agentId,
    sessionKey,
    setScreenFocused,
    onInactive: handleTerminalInactive,
  });

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

  const tabs = useMemo(() => {
    return buildTerminalTabs({
      sessionKey,
      terminalTabs,
      agentByKey,
      hydratedServerIdSet,
      agentAliases,
    });
  }, [agentAliases, agentByKey, hydratedServerIdSet, sessionKey, terminalTabs]);

  const { pickerSections, showPickerServerNames, sortedAgents } =
    useTerminalPickerModel({
      agents: state.agents,
      servers,
      connectionStates: state.serverConnections,
      latencyById: state.serverLatencyById,
      terminalTabs,
      recentAgentOpens,
    });

  const closePicker = useCallback(() => {
    setPickerVisible(false);
  }, []);

  const {
    goToInbox,
    openAgentTab,
    handleCloseCurrentTab,
    handleCloseOtherTabs,
    handleTerminateAgent,
    handleTogglePinned,
  } = useTerminalTabActions({
    sessionKey,
    serverId,
    agentId,
    activePinned,
    terminalTabs,
    agentByKey,
    hydratedServerIdSet,
    connectionState,
    displayName,
    agentServerName: agent?.serverName,
    setTerminalTabs,
    closeMenu,
    closePicker,
  });

  const openGitDiff = () => {
    closeMenu();
    gitDiff.open();
  };

  const openRenameModal = () => {
    closeMenu();
    setRenameDraft(displayName);
    setRenameVisible(true);
  };

  const {
    applyCodexRenderMode,
    createTerminal,
    handleSaveRename,
    openLinkedWork,
    openNewTerminal,
    retryServerConnection,
    toggleCodexRenderMode,
  } = useTerminalSessionActions({
    serverId,
    agentId,
    sessionKey,
    connectionState,
    creatingSession,
    codexRenderMode,
    linkedWork,
    renameDraft,
    closeMenu,
    closePicker,
    setNewTerminalVisible,
    setCreatingSession,
    setRenameVisible,
    setAgentAliases,
    setCodexRenderModes,
    setTerminalTabs,
    setRecentAgentOpens,
    setServer,
  });

  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chromeColors.appBackground }]}
      edges={["top"]}
    >
      <StatusBar style={statusBarStyle} />
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
        onTabLayout={handleTabLayout}
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
        terminalStateAccent={terminalState.accent}
        terminalStateBusy={terminalState.busy}
        terminalStateTitle={terminalState.title}
        terminalStateDetail={terminalState.detail}
        terminalStateHint={terminalState.hint}
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
