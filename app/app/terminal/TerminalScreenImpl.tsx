import React from "react";
import { useAgents } from "../../store/agents";
import { useWork } from "../../store/work";
import { TerminalScreenLayout } from "./TerminalScreenLayout";
import { useTerminalAgentIndex } from "./useTerminalAgentIndex";
import { useTerminalDirectoryModel } from "./useTerminalDirectoryModel";
import { useTerminalScreenActions } from "./useTerminalScreenActions";
import { useTerminalScreenAccessory } from "./useTerminalScreenAccessory";
import { useTerminalScreenChrome } from "./useTerminalScreenChrome";
import { useTerminalScreenLocalState } from "./useTerminalScreenLocalState";
import { useTerminalScreenLayoutProps } from "./useTerminalScreenLayoutProps";
import { useTerminalScreenModels } from "./useTerminalScreenModels";
import { useTerminalScreenStorage } from "./useTerminalScreenStorage";
import { useTerminalSessionActions } from "./useTerminalSessionActions";
import { useTerminalNavigationActions } from "./useTerminalNavigationActions";

export default function TerminalScreen() {
  const { state } = useAgents();
  const { state: workState } = useWork();
  const {
    agentId,
    serverId,
    sessionKey,
    routeSessionHint,
    pickerVisible,
    setPickerVisible,
    renameVisible,
    setRenameVisible,
    renameDraft,
    setRenameDraft,
    newTerminalVisible,
    setNewTerminalVisible,
    creatingSession,
    setCreatingSession,
    screenFocused,
    setScreenFocused,
    terminalRef,
  } = useTerminalScreenLocalState();
  const chromeLayout = useTerminalScreenChrome({ sessionKey });
  const {
    closeMenu,
    menuPosition,
    menuVisible,
  } = chromeLayout;
  const {
    agentByKey,
  } = useTerminalAgentIndex({
    agents: state.agents,
    hydratedServers: state.hydratedServers,
  });
  const {
    agentAliases,
    setAgentAliases,
    codexRenderModes,
    setCodexRenderModes,
    recentAgentOpens,
    setRecentAgentOpens,
    server,
    setServer,
    servers,
  } = useTerminalScreenStorage({
    serverId,
    sessionKey,
  });
  const {
    gitDiff,
    route,
    theme,
    viewport: viewportModel,
  } = useTerminalScreenModels({
    serverId,
    agentId,
    sessionKey,
    routeSessionHint,
    screenFocused,
    agentByKey,
    workByKey: workState.byKey,
    agentAliases,
    serverConnections: state.serverConnections,
    serverConnectionIssues: state.serverConnectionIssues,
    codexRenderModes,
  });
  const { chromeColors, statusBarStyle, terminalTheme } = theme;
  const {
    agent,
    codexRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    hasTerminalRoute,
    isCodexAgent,
    linkedWork,
    presentedAgent,
    showCodexChat,
  } = route;
  const {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  } = useTerminalScreenAccessory({
    serverId,
    agentId,
    sessionKey,
    accessoryVisible: viewportModel.accessoryVisible,
    ctrlDisabled: renameVisible,
    setScreenFocused,
  });

  const {
    pickerSections,
    showPickerServerNames,
    sortedAgents,
  } = useTerminalDirectoryModel({
    agents: state.agents,
    servers,
    connectionStates: state.serverConnections,
    latencyById: state.serverLatencyById,
    recentAgentOpens,
  });

  const {
    closePicker,
    openGitDiff,
    openRenameModal,
  } = useTerminalScreenActions({
    displayName,
    closeMenu,
    openGitDiffSheet: gitDiff.open,
    setPickerVisible,
    setRenameDraft,
    setRenameVisible,
  });

  const navigationActions = useTerminalNavigationActions({
    sessionKey,
    serverId,
    agentId,
    agentByKey,
    connectionState,
    displayName,
    agentServerName: agent?.serverName,
    closeMenu,
    closePicker,
  });

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
    setRecentAgentOpens,
    setServer,
  });
  const {
    openNewTerminal,
  } = sessionActions;
  const {
    overlayProps,
    topBarProps,
    viewportProps,
  } = useTerminalScreenLayoutProps({
    agent,
    agentAliases,
    agentId,
    accessoryBottomOffset,
    chrome: chromeColors,
    chromeLayout,
    codexRenderMode,
    connectionIssue,
    connectionState,
    creatingSession,
    ctrlArmed,
    daemonId: server?.daemonId,
    displayName,
    gitDiff,
    handleAccessoryLayout,
    handleCtrlArmedChange,
    hasLinkedWork: Boolean(linkedWork),
    hasTerminalRoute,
    isCodexAgent,
    keyboardVisible,
    menuPosition,
    menuVisible,
    newTerminalVisible,
    outputBottomInset,
    presentedAgent,
    pickerSections,
    pickerVisible,
    renameDraft,
    renamePlaceholder: agent?.name || agentId,
    renameVisible,
    screenFocused,
    selectedServerId: serverId,
    serverId,
    serverUrl: server?.url,
    sessionKey,
    setNewTerminalVisible,
    setPickerVisible,
    setRenameDraft,
    setRenameVisible,
    showCodexChat,
    showPickerServerNames,
    sortedAgentCount: sortedAgents.length,
    navigationActions,
    terminalRef,
    terminalTheme,
    viewportModel,
    openGitDiff,
    openNewTerminal,
    openRenameModal,
    sessionActions,
  });

  return (
    <TerminalScreenLayout
      chrome={chromeColors}
      statusBarStyle={statusBarStyle}
      topBarProps={topBarProps}
      viewportProps={viewportProps}
      overlayProps={overlayProps}
    />
  );
}
