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
import { makeSessionKey } from "../../services/sessionKeys";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";
import {
  TERMINAL_ACTION_POPOVER_WIDTH,
} from "../../components/terminal/TerminalActionPopover";
import { TerminalTopBar } from "../../components/terminal/TerminalTopBar";
import { TerminalViewport } from "../../components/terminal/TerminalViewport";
import { useTerminalAccessoryLayout } from "../../components/terminal/useTerminalAccessoryLayout";
import { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import { buildTerminalTabs } from "./TerminalScreenModel";
import { TerminalScreenOverlays } from "./TerminalScreenOverlays";
import { useTerminalAgentIndex } from "./useTerminalAgentIndex";
import { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import { useTerminalFocusLifecycle } from "./useTerminalFocusLifecycle";
import { useTerminalRouteModel } from "./useTerminalRouteModel";
import { useTerminalPickerModel } from "./useTerminalPickerModel";
import { useTerminalScreenOverlayProps } from "./useTerminalScreenOverlayProps";
import { useTerminalScreenStorage } from "./useTerminalScreenStorage";
import { useTerminalSessionActions } from "./useTerminalSessionActions";
import { useTerminalTabActions } from "./useTerminalTabActions";
import { useTerminalThemeChrome } from "./useTerminalThemeChrome";
import { useTerminalTopBarProps } from "./useTerminalTopBarProps";
import { useTerminalViewportModel } from "./useTerminalViewportModel";
import { useTerminalViewportProps } from "./useTerminalViewportProps";

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
  const chromeLayout = useTerminalChromeLayout({
    sessionKey,
    windowWidth,
    popoverWidth: TERMINAL_ACTION_POPOVER_WIDTH,
  });
  const {
    closeMenu,
    menuPosition,
    menuVisible,
  } = chromeLayout;

  const {
    agentByKey,
    hydratedServerIds,
    hydratedServerIdSet,
    liveAgentKeys,
  } = useTerminalAgentIndex({
    agents: state.agents,
    hydratedServers: state.hydratedServers,
  });
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
  const {
    activePinned,
    agent,
    codexRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    gitDiffCwd,
    hasTerminalRoute,
    isCodexAgent,
    linkedWork,
    showCodexChat,
  } = useTerminalRouteModel({
    serverId,
    agentId,
    sessionKey,
    agentByKey,
    workByKey: workState.byKey,
    agentAliases,
    terminalTabs,
    serverConnections: state.serverConnections,
    serverConnectionIssues: state.serverConnectionIssues,
    codexRenderModes,
  });
  const viewportModel = useTerminalViewportModel({
    hasTerminalRoute,
    showCodexChat,
    screenFocused,
    connectionState,
    connectionIssue,
    terminalTheme,
    chromeColors,
  });
  const gitDiff = useTerminalGitDiff({
    serverId,
    agentId,
    cwd: gitDiffCwd,
    connectionState,
    hasTerminalRoute,
    screenFocused,
  });
  const {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  } = useTerminalAccessoryLayout({
    accessoryVisible: viewportModel.accessoryVisible,
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

  const tabActions = useTerminalTabActions({
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

  const sessionActions = useTerminalSessionActions({
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
  const {
    openNewTerminal,
  } = sessionActions;
  const topBarProps = useTerminalTopBarProps({
    tabs,
    terminalTheme,
    chrome: chromeColors,
    chromeLayout,
    tabActions,
    openNewTerminal,
  });
  const viewportProps = useTerminalViewportProps({
    showCodexChat,
    sessionKey,
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    theme: terminalTheme,
    chrome: chromeColors,
    themeName,
    screenFocused,
    gitDiff,
    terminalRef,
    ctrlArmed,
    onCtrlArmedChange: handleCtrlArmedChange,
    viewportModel,
    hasTerminalRoute,
    isCodexAgent,
    outputBottomInset,
    accessoryBottomOffset,
    serverUrl: server?.url,
    daemonId: server?.daemonId,
    keyboardVisible,
    sessionActions,
    openGitDiff,
    onAccessoryLayout: handleAccessoryLayout,
  });
  const overlayProps = useTerminalScreenOverlayProps({
    pickerVisible,
    pickerSections,
    sortedAgentCount: sortedAgents.length,
    sessionKey,
    showPickerServerNames,
    agentAliases,
    creatingSession,
    menuVisible,
    menuPosition,
    connectionConnected: connectionState === "connected",
    gitDiff,
    activePinned,
    tabs,
    isCodexAgent,
    codexRenderMode,
    hasLinkedWork: Boolean(linkedWork),
    newTerminalVisible,
    agentCwd: agent?.cwd,
    serverId,
    renameVisible,
    renameDraft,
    renamePlaceholder: agent?.name || agentId,
    chrome: chromeColors,
    theme: terminalTheme,
    setPickerVisible,
    setNewTerminalVisible,
    setRenameVisible,
    setRenameDraft,
    tabActions,
    sessionActions,
    closeMenu,
    openNewTerminal,
    openGitDiff,
    openRenameModal,
  });

  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chromeColors.appBackground }]}
      edges={["top"]}
    >
      <StatusBar style={statusBarStyle} />
      <TerminalTopBar {...topBarProps} />

      <TerminalViewport {...viewportProps} />

      <TerminalScreenOverlays {...overlayProps} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0D0C0C",
  },
});
