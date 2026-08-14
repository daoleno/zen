import React, { useCallback } from "react";
import { useAgents } from "../../../store/agents";
import { useBrain } from "../../../store/brain";
import { useWork } from "../../../store/work";
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
import { useSessionProviderSheet } from "./useSessionProviderSheet";

export default function TerminalScreen() {
  const { state } = useAgents();
  const { state: brainState } = useBrain();
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
    createDurabilityWarning,
    dismissCreateDurabilityWarning,
    screenFocused,
    setScreenFocused,
    terminalRef,
    skillsHandoffToken,
  } = useTerminalScreenLocalState();
  const chromeLayout = useTerminalScreenChrome({ sessionKey });
  const { closeMenu, menuPosition, menuVisible } = chromeLayout;
  const { agentByKey } = useTerminalAgentIndex({
    agents: state.agents,
    hydratedServers: state.hydratedServers,
  });
  const brainForServer = serverId ? brainState.byServer[serverId] : undefined;
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
    brainHostAgent: brainForServer?.host_agent ?? null,
    brainHostServerId: brainForServer?.serverId ?? null,
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

  const routeSheet = useSessionProviderSheet({
    serverId,
    agentId,
    capabilities: agent?.capabilities ?? null,
    connectionConnected: connectionState === "connected",
    eagerLoad: true,
  });

  const openModel = useCallback(() => {
    closeMenu();
    // The top-bar Model action opens the native bottom sheet; no anchor is
    // needed — the sheet positions itself.
    routeSheet.open();
  }, [closeMenu, routeSheet]);

  // The popover Model action exists only when this exact Session can switch
  // models right now — the same truth as the Composer control.
  const modelActionAvailable = routeSheet.composerControl != null;

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
      routeSheetVisible: routeSheet.visible,
      routeSheetLoading: routeSheet.loading,
      routeSheetActivating: routeSheet.activating,
      routeSheetError: routeSheet.error,
      routeSheetSelection: routeSheet.selection,
      routeSheetGroups: routeSheet.groups,
      createDurabilityWarning,
      onDismissCreateDurabilityWarning: dismissCreateDurabilityWarning,
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
      skillsHandoffToken,
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
      openModel: modelActionAvailable
        ? openModel
        : undefined,
      modelActionAvailable,
      composerModelControl: routeSheet.composerControl,
      onComposerModelControlPress: () => {
        routeSheet.open();
      },
      closeRouteSheet: routeSheet.close,
      retryRouteSheet: routeSheet.retry,
      activateSessionModel: (choice) => {
        void routeSheet.activate(choice);
      },
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
