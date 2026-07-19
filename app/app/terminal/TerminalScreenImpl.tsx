import React from "react";
import { useAgents } from "../../store/agents";
import { useWork } from "../../store/work";
import { TerminalScreenLayout } from "./TerminalScreenLayout";
import { useTerminalAgentIndex } from "./useTerminalAgentIndex";
import { useTerminalScreenActions } from "./useTerminalScreenActions";
import { useTerminalScreenAccessory } from "./useTerminalScreenAccessory";
import { useTerminalScreenChrome } from "./useTerminalScreenChrome";
import { useTerminalScreenLocalState } from "./useTerminalScreenLocalState";
import { useTerminalScreenLayoutProps } from "./useTerminalScreenLayoutProps";
import { useTerminalScreenModels } from "./useTerminalScreenModels";
import { useTerminalScreenStorage } from "./useTerminalScreenStorage";
import { useTerminalSessionActions } from "./useTerminalSessionActions";
import { useTerminalNavigationActions } from "./useTerminalNavigationActions";
import { useSessionResourceSheet } from "./useSessionResourceSheet";

export default function TerminalScreen() {
  const { state } = useAgents();
  const { state: workState } = useWork();
  const {
    agentId,
    serverId,
    sessionKey,
    initialComposerFocusGrant,
    consumeInitialComposerFocus,
    initialInterfaceRenderMode,
    routeSessionHint,
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
  const { closeMenu, menuPosition, menuVisible } = chromeLayout;
  const { agentByKey } = useTerminalAgentIndex({
    agents: state.agents,
    hydratedServers: state.hydratedServers,
  });
  const {
    agentAliases,
    setAgentAliases,
    interfaceRenderModes,
    setInterfaceRenderModes,
    setRecentAgentOpens,
    server,
    setServer,
  } = useTerminalScreenStorage({
    serverId,
    sessionKey,
    initialInterfaceRenderMode,
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
    interfaceRenderModes,
  });
  const { chromeColors, chatChrome, chatTheme, statusBarStyle, terminalTheme } =
    theme;
  const {
    agent,
    interfaceRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    hasTerminalRoute,
    isStructuredChatAgent,
    linkedWork,
    presentedAgent,
    showInterfaceChat,
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

  const { openGitDiff, openRenameModal } = useTerminalScreenActions({
    displayName,
    closeMenu,
    openGitDiffSheet: gitDiff.open,
    setRenameDraft,
    setRenameVisible,
  });

  const navigationActions = useTerminalNavigationActions({
    sessionKey,
    serverId,
    agentId,
    connectionState,
    displayName,
    agentServerName: agent?.serverName,
    closeMenu,
  });

  const sessionActions = useTerminalSessionActions({
    serverId,
    agentId,
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
    setAgentAliases,
    setInterfaceRenderModes,
    setRecentAgentOpens,
    setServer,
  });
  const { openNewTerminal } = sessionActions;

  const resourceSheet = useSessionResourceSheet({
    serverId,
    agentId,
    connectionConnected: connectionState === "connected",
  });

  const { overlayProps, topBarProps, viewportProps } =
    useTerminalScreenLayoutProps({
      agent,
      agentId,
      accessoryBottomOffset,
      chrome: chromeColors,
      chatChrome,
      chatTheme,
      chromeLayout,
      interfaceRenderMode,
      initialComposerFocusGrant,
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
      isStructuredChatAgent,
      keyboardVisible,
      menuPosition,
      menuVisible,
      newTerminalVisible,
      outputBottomInset,
      presentedAgent,
      renameDraft,
      renamePlaceholder: agent?.name || agentId,
      renameVisible,
      resourceSheetVisible: resourceSheet.visible,
      resourceSheetLoading: resourceSheet.loading,
      resourceSheetError: resourceSheet.error,
      resourceSheetSnapshot: resourceSheet.snapshot,
      screenFocused,
      selectedServerId: serverId,
      serverId,
      serverUrl: server?.url,
      sessionKey,
      setNewTerminalVisible,
      setRenameDraft,
      setRenameVisible,
      showInterfaceChat,
      onConsumeInitialComposerFocus: consumeInitialComposerFocus,
      navigationActions,
      terminalRef,
      terminalTheme,
      viewportModel,
      openGitDiff,
      openNewTerminal,
      openRenameModal,
      openSessionDetails: resourceSheet.open,
      closeResourceSheet: resourceSheet.close,
      retryResourceSheet: resourceSheet.retry,
      sessionActions,
    });

  return (
    <TerminalScreenLayout
      chrome={chatChrome}
      statusBarStyle={statusBarStyle}
      topBarProps={topBarProps}
      viewportProps={viewportProps}
      overlayProps={overlayProps}
    />
  );
}
