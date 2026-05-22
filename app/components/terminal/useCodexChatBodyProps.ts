import { useCallback, useMemo } from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  ChatCommandEvent,
  ComposerAttachment,
} from "./CodexChatSession";
import type { CodexChatBodyProps } from "./CodexChatBody";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import type { useCodexChatController } from "./CodexChatController";
import type {
  useCodexComposerInput,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";

interface UseCodexChatBodyPropsInput {
  screenFocused: boolean;
  serverId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  chatCommandEvents: ChatCommandEvent[];
  loading: boolean;
  error?: string | null;
  draft: string;
  attachments: ComposerAttachment[];
  composerPresentation: CodexComposerPresentation;
  timeline: ReturnType<typeof usePinnedTimeline>;
  composerInput: ReturnType<typeof useCodexComposerInput>;
  controller: ReturnType<typeof useCodexChatController>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSwitchToTerminal(): void;
  setDraft(value: string): void;
}

export function useCodexChatBodyProps({
  screenFocused,
  serverId,
  agent,
  connectionState,
  conversation,
  events,
  chatCommandEvents,
  loading,
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
}: UseCodexChatBodyPropsInput): CodexChatBodyProps {
  const handleComposerHeightChange = useCallback(() => {
    timeline.pinToBottomIfNeeded(false);
  }, [timeline.pinToBottomIfNeeded]);

  const handleUploadPress = useCallback(() => {
    void controller.handleUploadAttachment();
  }, [controller.handleUploadAttachment]);

  const handleSendPress = useCallback(() => {
    if (composerPresentation.showStopButton) {
      controller.interruptCodex();
      return;
    }
    controller.sendDraft();
  }, [
    composerPresentation.showStopButton,
    controller.interruptCodex,
    controller.sendDraft,
  ]);

  return useMemo(
    () => ({
      screenFocused,
      serverId,
      agentCwd: agent?.cwd,
      conversation,
      events,
      chatCommandEvents,
      loading,
      error,
      scrollRef: timeline.scrollRef,
      showJumpToLatest: timeline.showJumpToLatest,
      onTimelineLayout: timeline.handleLayout,
      onTimelineScroll: timeline.handleScroll,
      onTimelineContentSizeChange: timeline.handleContentSizeChange,
      onScrollToLatest: timeline.scrollToLatest,
      onComposerHeightChange: handleComposerHeightChange,
      onUnavailableAction: onSwitchToTerminal,
      inputRef: composerInput.inputRef,
      draft,
      editable: connectionState === "connected",
      composerFocused: composerInput.focused,
      canAttach: controller.canAttach,
      uploading: controller.uploading,
      sending: controller.sending,
      attachments,
      composerPresentation,
      chrome,
      theme,
      onSelectCommand: controller.pickSlashCommand,
      onRemoveAttachment: controller.removeAttachment,
      onDraftChange: setDraft,
      onUploadPress: handleUploadPress,
      onInputPress: composerInput.focus,
      onInputFocus: composerInput.handleFocus,
      onInputBlur: composerInput.handleBlur,
      onInputStart: composerInput.handleInputStart,
      onSubmit: controller.sendDraft,
      onSendPress: handleSendPress,
    }),
    [
      agent?.cwd,
      attachments,
      chatCommandEvents,
      chrome,
      composerInput.focus,
      composerInput.focused,
      composerInput.handleBlur,
      composerInput.handleFocus,
      composerInput.handleInputStart,
      composerInput.inputRef,
      composerPresentation,
      connectionState,
      conversation,
      controller.canAttach,
      controller.interruptCodex,
      controller.pickSlashCommand,
      controller.removeAttachment,
      controller.sendDraft,
      controller.sending,
      controller.uploading,
      draft,
      error,
      events,
      handleComposerHeightChange,
      handleSendPress,
      handleUploadPress,
      loading,
      onSwitchToTerminal,
      screenFocused,
      serverId,
      setDraft,
      theme,
      timeline.handleContentSizeChange,
      timeline.handleLayout,
      timeline.handleScroll,
      timeline.scrollRef,
      timeline.scrollToLatest,
      timeline.showJumpToLatest,
    ],
  );
}
