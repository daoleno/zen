import React, {
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { CodexChatBodyProps } from "./CodexChatBody";
import { useCodexChatController } from "./CodexChatController";
import { isCodexRequestRunning } from "./CodexChatControllerModel";
import { useCodexChatSession } from "./CodexChatSession";
import { CodexSkillsSheet } from "./CodexSkillsSheet";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useCodexChatBodyProps } from "./useCodexChatBodyProps";
import {
  useCodexComposerInput,
  useCodexComposerPresentation,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";

interface UseCodexChatSurfaceStateInput {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  onSwitchToTerminal(): void;
}

interface CodexChatSurfaceState {
  bodyProps: CodexChatBodyProps;
}

export function useCodexChatSurfaceState({
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  onSwitchToTerminal,
}: UseCodexChatSurfaceStateInput): CodexChatSurfaceState {
  const insets = useSafeAreaInsets();
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused,
  });
  const [actionMenuPinned, setActionMenuPinned] = useState(false);
  const [skillsSheetVisible, setSkillsSheetVisible] = useState(false);
  const session = useCodexChatSession({
    serverId,
    agentId,
    agent,
    connectionState,
    screenFocused,
  });
  const {
    cacheKey: conversationCacheKey,
    conversation,
    localChatState,
    loading,
    error,
    draft,
    setDraft,
    attachments,
    setAttachments,
    pendingUserMessages,
    pendingAssistantMessages,
    addPendingUserMessage,
    removePendingUserMessage,
    startPendingAssistantMessage,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    refreshConversation,
  } = session;
  const composerInput = useCodexComposerInput({
    enabled: screenFocused && connectionState === "connected",
  });
  useEffect(() => {
    if (connectionState !== "connected" && actionMenuPinned) {
      setActionMenuPinned(false);
    }
  }, [actionMenuPinned, connectionState]);

  const toggleActionMenu = useCallback(() => {
    if (connectionState !== "connected") {
      composerInput.focus();
      return;
    }
    if (!actionMenuPinned) {
      composerInput.blur();
    }
    setActionMenuPinned(!actionMenuPinned);
  }, [actionMenuPinned, composerInput.blur, composerInput.focus, connectionState]);

  const dismissActionMenu = useCallback(() => {
    setActionMenuPinned(false);
  }, []);
  const openSkillsSheet = useCallback(() => {
    setActionMenuPinned(false);
    setSkillsSheetVisible(true);
  }, []);
  const closeSkillsSheet = useCallback(() => {
    setSkillsSheetVisible(false);
  }, []);
  const events = conversation?.events ?? [];
  const timeline = usePinnedTimeline(
    events.length +
      pendingUserMessages.length +
      pendingAssistantMessages.length,
  );
  const controller = useCodexChatController({
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    conversation,
    events,
    draft,
    setDraft,
    attachments,
    setAttachments,
    slashCommands,
    addPendingUserMessage,
    removePendingUserMessage,
    startPendingAssistantMessage,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    refreshConversation,
    scrollToLatest: timeline.scrollToLatest,
    focusComposer: composerInput.focus,
    dismissActionMenu,
    openSkillsSheet,
  });

  useEffect(() => {
    timeline.resetForConversation();
  }, [conversationCacheKey, timeline.resetForConversation]);

  const requestRunning =
    (isCodexRequestRunning({
      agent,
      conversation,
      events,
    }) &&
      localChatState === "idle") ||
    pendingAssistantMessages.some((message) => !message.settledAt);
  const composerPresentation = useCodexComposerPresentation({
    draft,
    slashCommands,
    connectionState,
    requestRunning,
    attachmentCount: attachments.length,
    sending: controller.sending,
    startingNewChat: controller.startingNewChat,
    interrupting: controller.interrupting,
    canSend: controller.canSend,
    composerFocused: composerInput.focused,
    actionMenuPinned,
    safeAreaTop: insets.top,
    safeAreaBottom: insets.bottom,
  });
  const skillsSheet = useMemo(
    () =>
      React.createElement(CodexSkillsSheet, {
        visible: skillsSheetVisible,
        serverId,
        cwd: agent?.cwd,
        chrome,
        theme,
        onSelectSkill: (skill) => {
          closeSkillsSheet();
          controller.insertSkillMention(skill);
        },
        onClose: closeSkillsSheet,
      }),
    [
      agent?.cwd,
      chrome,
      closeSkillsSheet,
      controller.insertSkillMention,
      serverId,
      skillsSheetVisible,
      theme,
    ],
  );
  const bodyProps = useCodexChatBodyProps({
    screenFocused,
    serverId,
    agent,
    connectionState,
    conversation,
    events,
    pendingUserMessages,
    pendingAssistantMessages,
    loading,
    localChatState,
    error,
    draft,
    attachments,
    composerPresentation,
    timeline,
    composerInput,
    controller,
    chrome,
    theme,
    onSwitchToTerminal,
    setDraft,
    onToggleActionMenu: toggleActionMenu,
    onDismissActionMenu: dismissActionMenu,
    skillsSheet,
  });
  return {
    bodyProps,
  };
}
