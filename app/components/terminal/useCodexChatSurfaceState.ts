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
import type { ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { CodexChatBodyProps } from "./CodexChatBody";
import { useCodexChatController } from "./CodexChatController";
import { isCodexRequestRunning } from "./CodexChatControllerModel";
import {
  type CodexChatAgentInfo,
  type PendingSlashCommand,
  type PendingUserMessage,
  useCodexChatSession,
} from "./CodexChatSession";
import { CodexSkillsSheet } from "./CodexSkillsSheet";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useCodexChatBodyProps } from "./useCodexChatBodyProps";
import {
  useCodexComposerInput,
  useCodexComposerPresentation,
  useElapsedDurationLabel,
  usePinnedTimeline,
  useRelativeTimeLabel,
} from "./CodexChatSurfaceHooks";

interface UseCodexChatSurfaceStateInput {
  visible: boolean;
  serverId: string;
  agentId: string;
  agentInfo?: CodexChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  placeholder?: string;
  minimalComposer?: boolean;
  keyboardVerticalOffset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onSwitchToTerminal(): void;
}

interface CodexChatSurfaceState {
  bodyProps: CodexChatBodyProps;
}

export function useCodexChatSurfaceState({
  visible,
  serverId,
  agentId,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  placeholder,
  minimalComposer,
  keyboardVerticalOffset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
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
    agentInfo,
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
    pendingSlashCommands,
    addPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
  } = session;
  const composerInput = useCodexComposerInput({
    enabled: visible && screenFocused && connectionState === "connected",
  });
  useEffect(() => {
    if ((!visible || connectionState !== "connected") && actionMenuPinned) {
      setActionMenuPinned(false);
    }
  }, [actionMenuPinned, connectionState, visible]);

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
  const latestTimelineTimestamp = useMemo(
    () => latestChatTimelineTimestamp(conversation, pendingUserMessages, pendingSlashCommands),
    [conversation, pendingSlashCommands, pendingUserMessages],
  );
  const jumpLabel = useRelativeTimeLabel(latestTimelineTimestamp);
  const timeline = usePinnedTimeline(
    events.length +
      pendingUserMessages.length +
      pendingSlashCommands.length,
    conversationCacheKey,
  );
  const controller = useCodexChatController({
    serverId,
    agentId,
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
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    scrollToLatest: timeline.scrollToLatest,
    focusComposer: composerInput.focus,
    clearComposerNativeText: composerInput.clearNativeText,
    dismissActionMenu,
    openSkillsSheet,
  });

  const requestRunning =
    (isCodexRequestRunning({
      conversation,
      events,
    }) &&
      localChatState === "idle");
  const turnStartedAt = useMemo(
    () => currentTurnStartedAt(conversation, events, pendingUserMessages),
    [conversation, events, pendingUserMessages],
  );
  const turnElapsedLabel = useElapsedDurationLabel(
    turnStartedAt,
    requestRunning && localChatState === "idle",
  );
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
    elapsedLabel: turnElapsedLabel,
    actionMenuPinned,
    safeAreaTop: insets.top,
    safeAreaBottom: insets.bottom,
    placeholder,
    keyboardVerticalOffset,
    minimalComposer,
  });
  const skillsSheet = useMemo(
    () =>
      React.createElement(CodexSkillsSheet, {
        visible: skillsSheetVisible,
        serverId,
        cwd: agentInfo?.cwd,
        chrome,
        theme,
        onSelectSkill: (skill) => {
          closeSkillsSheet();
          controller.insertSkillMention(skill);
        },
        onClose: closeSkillsSheet,
      }),
    [
      agentInfo?.cwd,
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
    agentCwd: agentInfo?.cwd,
    connectionState,
    conversation,
    events,
    pendingUserMessages,
    pendingSlashCommands,
    loading,
    localChatState,
    error,
    draft,
    attachments,
    composerPresentation,
    timeline,
    jumpLabel,
    emptyTitle,
    emptyBody,
    composerInput,
    controller,
    chrome,
    theme,
    onSwitchToTerminal,
    setDraft,
    onToggleActionMenu: toggleActionMenu,
    onDismissActionMenu: dismissActionMenu,
    showUnavailableAction,
    skillsSheet,
  });
  return {
    bodyProps,
  };
}

function latestChatTimelineTimestamp(
  conversation: CodexConversation | null,
  pendingUserMessages: PendingUserMessage[],
  pendingSlashCommands: PendingSlashCommand[],
) {
  let latest = 0;
  conversation?.events.forEach((event: CodexConversationEvent) => {
    const timestamp = new Date(event.timestamp || "").getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  pendingUserMessages.forEach((message) => {
    const timestamp = new Date(message.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  pendingSlashCommands.forEach((command) => {
    const timestamp = new Date(command.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  return latest > 0 ? new Date(latest).toISOString() : undefined;
}

function currentTurnStartedAt(
  conversation: CodexConversation | null,
  events: CodexConversationEvent[],
  pendingUserMessages: PendingUserMessage[],
) {
  const activeUserTimestamp = latestUserMessageTimestamp(conversation, events);
  const pendingTimestamp = latestPendingUserMessageTimestamp(pendingUserMessages);

  if (!activeUserTimestamp) {
    return pendingTimestamp;
  }
  if (!pendingTimestamp) {
    return activeUserTimestamp;
  }

  const activeMs = new Date(activeUserTimestamp).getTime();
  const pendingMs = new Date(pendingTimestamp).getTime();
  if (!Number.isFinite(activeMs)) {
    return pendingTimestamp;
  }
  if (!Number.isFinite(pendingMs)) {
    return activeUserTimestamp;
  }
  return pendingMs > activeMs ? pendingTimestamp : activeUserTimestamp;
}

function latestUserMessageTimestamp(
  conversation: CodexConversation | null,
  events: CodexConversationEvent[],
) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.kind !== "user_message" || !event.timestamp) {
      continue;
    }
    const timestamp = new Date(event.timestamp).getTime();
    if (Number.isFinite(timestamp)) {
      return event.timestamp;
    }
  }
  return conversation?.updated_at;
}

function latestPendingUserMessageTimestamp(
  pendingUserMessages: PendingUserMessage[],
) {
  let latest = 0;
  pendingUserMessages.forEach((message) => {
    const timestamp = new Date(message.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  return latest > 0 ? new Date(latest).toISOString() : undefined;
}
