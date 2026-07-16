import { useCallback, useMemo, type ReactNode } from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
  StructuredTurn,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  CodexChatLocalState,
  ComposerAttachment,
  PendingSlashCommand,
  PendingUserMessage,
} from "./CodexChatSession";
import type { CodexChatBodyProps } from "./CodexChatBody";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import type { useCodexChatController } from "./CodexChatController";
import type {
  useCodexComposerInput,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";

interface UseCodexChatBodyPropsInput {
  screenFocused: boolean;
  serverId: string;
  agentCwd?: string;
  connectionState: ConnectionState;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
  workingTurn?: StructuredTurn;
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  draft: string;
  attachments: ComposerAttachment[];
  composerPresentation: CodexComposerPresentation;
  topChromeInset?: number;
  terminalActionPrompt?: TerminalActionPrompt | null;
  timeline: ReturnType<typeof usePinnedTimeline>;
  jumpLabel?: string;
  emptyTitle?: string;
  emptyBody?: string;
  composerInput: ReturnType<typeof useCodexComposerInput>;
  controller: ReturnType<typeof useCodexChatController>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSwitchToTerminal?: () => void;
  setDraft(value: string): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
  onTerminalActionKey(key: string): Promise<void> | void;
  showUnavailableAction?: boolean;
  composerAccessory?: ReactNode;
  skillsSheet?: ReactNode;
}

export function useCodexChatBodyProps({
  screenFocused,
  serverId,
  agentCwd,
  connectionState,
  conversation,
  events,
  pendingUserMessages,
  pendingSlashCommands,
  workingTurn,
  loading,
  localChatState,
  error,
  draft,
  attachments,
  composerPresentation,
  topChromeInset,
  terminalActionPrompt,
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
  onToggleActionMenu,
  onDismissActionMenu,
  onTerminalActionKey,
  showUnavailableAction,
  composerAccessory,
  skillsSheet,
}: UseCodexChatBodyPropsInput): CodexChatBodyProps {
  const handleUploadPress = useCallback(() => {
    void controller.handleUploadAttachment();
  }, [controller.handleUploadAttachment]);

  const handleSendPress = useCallback(() => {
    if (controller.startingNewChat) {
      return;
    }
    controller.sendDraft();
  }, [
    controller.sendDraft,
    controller.startingNewChat,
  ]);
  const handleStopPress = useCallback(() => {
    controller.interruptCodex();
  }, [controller.interruptCodex]);

  return useMemo(
    () => ({
      screenFocused,
      serverId,
      agentCwd,
      conversation,
      events,
      pendingUserMessages,
      pendingSlashCommands,
      workingTurn,
      loading,
      localChatState,
      error,
      scrollRef: timeline.scrollRef,
      timelineTextSelectable: timeline.textSelectable,
      showJumpToLatest: timeline.showJumpToLatest,
      jumpLabel,
      emptyTitle,
      emptyBody,
      onTimelineLayout: timeline.handleLayout,
      onTimelineScroll: timeline.handleScroll,
      onTimelineScrollBeginDrag: timeline.handleScrollBeginDrag,
      onTimelineScrollEndDrag: timeline.handleScrollEndDrag,
      onTimelineMomentumScrollBegin: timeline.handleMomentumScrollBegin,
      onTimelineMomentumScrollEnd: timeline.handleMomentumScrollEnd,
      onTimelineContentSizeChange: timeline.handleContentSizeChange,
      onTimelineLatestOffsetChange: timeline.handleLatestOffsetChange,
      onTimelineTextSelectionGestureStart:
        timeline.handleTextSelectionGestureStart,
      onTimelineTextSelectionGestureEnd: timeline.handleTextSelectionGestureEnd,
      onScrollToLatest: timeline.scrollToLatest,
      onUnavailableAction: onSwitchToTerminal,
      showUnavailableAction:
        Boolean(onSwitchToTerminal) && (showUnavailableAction ?? true),
      inputRef: composerInput.inputRef,
      draft,
      editable: connectionState === "connected",
      composerFocused: composerInput.focused,
      canAttach: controller.canAttach,
      uploading: controller.uploading,
      sending: controller.sending || controller.startingNewChat,
      operationalError: controller.operationalError,
      attachments,
      composerPresentation,
      topChromeInset,
      terminalActionPrompt,
      chrome,
      theme,
      onSelectCommand: controller.pickSlashCommand,
      onToggleActionMenu,
      onDismissActionMenu,
      onRemoveAttachment: controller.removeAttachment,
      onDraftChange: setDraft,
      onUploadPress: handleUploadPress,
      onInputFocus: composerInput.handleFocus,
      onInputBlur: composerInput.handleBlur,
      onSendPress: handleSendPress,
      onStopPress: handleStopPress,
      onRetryPendingUserMessage: controller.retryPendingUserMessage,
      onTerminalActionKey,
      composerAccessory,
      skillsSheet,
    }),
    [
      agentCwd,
      attachments,
      chrome,
      composerInput.focused,
      composerInput.handleBlur,
      composerInput.handleFocus,
      composerInput.inputRef,
      composerPresentation,
      topChromeInset,
      terminalActionPrompt,
      connectionState,
      conversation,
      controller.canAttach,
      controller.interruptCodex,
      controller.operationalError,
      controller.pickSlashCommand,
      controller.removeAttachment,
      controller.sendDraft,
      controller.retryPendingUserMessage,
      controller.sending,
      controller.startingNewChat,
      controller.uploading,
      draft,
      error,
      events,
      pendingUserMessages,
      pendingSlashCommands,
      workingTurn,
      handleSendPress,
      handleStopPress,
      handleUploadPress,
      loading,
      localChatState,
      jumpLabel,
      emptyTitle,
      emptyBody,
      onSwitchToTerminal,
      onToggleActionMenu,
      onDismissActionMenu,
      onTerminalActionKey,
      composerAccessory,
      screenFocused,
      showUnavailableAction,
      serverId,
      setDraft,
      skillsSheet,
      theme,
      timeline.handleContentSizeChange,
      timeline.handleLatestOffsetChange,
      timeline.handleLayout,
      timeline.handleScroll,
      timeline.handleScrollBeginDrag,
      timeline.handleScrollEndDrag,
      timeline.handleMomentumScrollBegin,
      timeline.handleMomentumScrollEnd,
      timeline.handleTextSelectionGestureEnd,
      timeline.handleTextSelectionGestureStart,
      timeline.textSelectable,
      timeline.scrollRef,
      timeline.scrollToLatest,
      timeline.showJumpToLatest,
    ],
  );
}
