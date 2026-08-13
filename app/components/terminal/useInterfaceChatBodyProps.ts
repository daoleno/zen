import { useCallback, useMemo, type ReactNode } from "react";
import type { MenuAnchorLayout } from "./screen/TerminalScreenModel";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
  ProviderActivity,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  ComposerAttachment,
  PendingUserMessage,
} from "./InterfaceChatSession";
import type { InterfaceChatBodyProps } from "./InterfaceChatBody";
import type { InterfaceComposerPresentation } from "./InterfaceChatSurfaceModel";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import type { useInterfaceChatController } from "./InterfaceChatController";
import type {
  useInterfaceComposerInput,
  usePinnedTimeline,
} from "./InterfaceChatSurfaceHooks";

interface UseInterfaceChatBodyPropsInput {
  screenFocused: boolean;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  agentProcessId?: number;
  agentStartedAt?: number;
  agentCwd?: string;
  connectionState: ConnectionState;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  turnFocusAnchorAliases?: ReadonlyMap<string, string>;
  runningActivity?: ProviderActivity;
  onBrainWorkEventActivate?: (
    event: import("../brain/brainWorkEvent").BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
  openSessionIds?: ReadonlySet<string>;
  loading: boolean;
  error?: string | null;
  draft: string;
  attachments: ComposerAttachment[];
  composerPresentation: InterfaceComposerPresentation;
  topChromeInset?: number;
  terminalActionPrompt?: TerminalActionPrompt | null;
  timeline: ReturnType<typeof usePinnedTimeline>;
  jumpLabel?: string;
  emptyTitle?: string;
  emptyBody?: string;
  composerInput: ReturnType<typeof useInterfaceComposerInput>;
  controller: ReturnType<typeof useInterfaceChatController>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSwitchToTerminal?: () => void;
  setDraft(value: string): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
  onModelControlPress?(anchor: MenuAnchorLayout): void;
  onTerminalActionKey(key: string): Promise<void> | void;
  onKeyboardLifecycleInvalidate(reason: "route" | "app"): void;
  showUnavailableAction?: boolean;
  composerAccessory?: ReactNode;
  skillsSheet?: ReactNode;
}

export function useInterfaceChatBodyProps({
  screenFocused,
  serverId,
  serverUrl,
  daemonId,
  agentId,
  agentProcessId,
  agentStartedAt,
  agentCwd,
  connectionState,
  conversation,
  events,
  pendingUserMessages,
  turnFocusAnchorAliases,
  runningActivity,
  onBrainWorkEventActivate,
  openSessionIds,
  loading,
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
  onModelControlPress,
  onTerminalActionKey,
  onKeyboardLifecycleInvalidate,
  showUnavailableAction,
  composerAccessory,
  skillsSheet,
}: UseInterfaceChatBodyPropsInput): InterfaceChatBodyProps {
  const handleUploadPress = useCallback(() => {
    void controller.handleUploadAttachment();
  }, [controller.handleUploadAttachment]);

  const handleSendPress = useCallback(() => {
    controller.sendDraft();
  }, [controller.sendDraft]);
  const handleStopPress = useCallback(() => {
    controller.interruptInterface();
  }, [controller.interruptInterface]);

  return useMemo(
    () => ({
      screenFocused,
      serverId,
      serverUrl,
      daemonId,
      agentId,
      agentProcessId,
      agentStartedAt,
      agentCwd,
      conversation,
      events,
      pendingUserMessages,
      turnFocusAnchorAliases,
      runningActivity,
      onBrainWorkEventActivate,
      openSessionIds,
      loading,
      error,
      scrollRef: timeline.scrollRef,
      nativeFollowSuspended: timeline.nativeFollowSuspended,
      timelineTextSelectable: timeline.textSelectable,
      turnFocusClearanceRequest: timeline.turnFocusClearanceRequest,
      turnFocusSpacer: timeline.turnFocusSpacer,
      turnFocusPendingMessageId: timeline.turnFocusPendingMessageId,
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
      onTimelineTouchActiveChange: timeline.handleTimelineTouchActiveChange,
      onTimelineItemsMutated: timeline.handleTimelineItemsMutated,
      onTimelineContentSizeChange: timeline.handleContentSizeChange,
      onTimelineClearanceChange: timeline.handleClearanceChange,
      onTurnFocusAnchorAvailable: timeline.handleTurnFocusAnchorAvailable,
      onTurnFocusRowLayout: timeline.handleTurnFocusRowLayout,
      onTurnFocusSpacerLayout: timeline.handleTurnFocusSpacerLayout,
      onTimelineTextSelectionGestureStart:
        timeline.handleTextSelectionGestureStart,
      onTimelineTextSelectionGestureEnd: timeline.handleTextSelectionGestureEnd,
      onKeyboardLifecycleInvalidate,
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
      activeUpload: controller.activeUpload,
      sending: controller.sending,
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
      onCancelUpload: controller.cancelUpload,
      onInputFocus: composerInput.handleFocus,
      onInputBlur: composerInput.handleBlur,
      onSendPress: handleSendPress,
      onStopPress: handleStopPress,
      onModelControlPress,
      onRetryPendingUserMessage: controller.retryPendingUserMessage,
      onTerminalActionKey,
      composerAccessory,
      skillsSheet,
    }),
    [
      agentId,
      agentCwd,
      agentProcessId,
      agentStartedAt,
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
      controller.activeUpload,
      controller.cancelUpload,
      controller.interruptInterface,
      controller.operationalError,
      controller.pickSlashCommand,
      controller.removeAttachment,
      controller.sendDraft,
      controller.retryPendingUserMessage,
      controller.sending,
      controller.uploading,
      draft,
      error,
      events,
      pendingUserMessages,
      turnFocusAnchorAliases,
      runningActivity,
      onBrainWorkEventActivate,
      openSessionIds,
      handleSendPress,
      handleStopPress,
      handleUploadPress,
      loading,
      jumpLabel,
      emptyTitle,
      emptyBody,
      onSwitchToTerminal,
      onToggleActionMenu,
      onDismissActionMenu,
      onModelControlPress,
      onTerminalActionKey,
      onKeyboardLifecycleInvalidate,
      composerAccessory,
      screenFocused,
      showUnavailableAction,
      daemonId,
      serverId,
      serverUrl,
      setDraft,
      skillsSheet,
      theme,
      timeline.handleContentSizeChange,
      timeline.handleClearanceChange,
      timeline.handleLayout,
      timeline.handleScroll,
      timeline.handleScrollBeginDrag,
      timeline.handleScrollEndDrag,
      timeline.handleMomentumScrollBegin,
      timeline.handleMomentumScrollEnd,
      timeline.handleTimelineItemsMutated,
      timeline.handleTimelineTouchActiveChange,
      timeline.handleTurnFocusAnchorAvailable,
      timeline.handleTurnFocusRowLayout,
      timeline.handleTurnFocusSpacerLayout,
      timeline.handleTextSelectionGestureEnd,
      timeline.handleTextSelectionGestureStart,
      timeline.nativeFollowSuspended,
      timeline.textSelectable,
      timeline.scrollRef,
      timeline.scrollToLatest,
      timeline.showJumpToLatest,
      timeline.turnFocusPendingMessageId,
      timeline.turnFocusClearanceRequest,
      timeline.turnFocusSpacer,
    ],
  );
}
