import React from "react";
import {
  StyleSheet,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ScrollView,
  type TextInput,
} from "react-native";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment, ChatCommandEvent } from "./CodexChatSession";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import { CodexChatComposer } from "./CodexChatComposer";
import { CodexChatTimelineSection } from "./CodexChatTimelineSection";
import { useCodexComposerLayout } from "./useCodexComposerLayout";

interface CodexChatBodyProps {
  screenFocused: boolean;
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  chatCommandEvents: ChatCommandEvent[];
  loading: boolean;
  error?: string | null;
  scrollRef: React.RefObject<ScrollView | null>;
  showJumpToLatest: boolean;
  onTimelineLayout(event: LayoutChangeEvent): void;
  onTimelineScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onTimelineContentSizeChange(width: number, height: number): void;
  onScrollToLatest(animated?: boolean, delay?: number): void;
  onComposerHeightChange(height: number): void;
  onUnavailableAction(): void;
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  composerFocused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sending: boolean;
  attachments: ComposerAttachment[];
  composerPresentation: CodexComposerPresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
  onRemoveAttachment(id: string): void;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onInputStart(): boolean;
  onSubmit(): void;
  onSendPress(): void;
}

export function CodexChatBody({
  screenFocused,
  serverId,
  agentCwd,
  conversation,
  events,
  chatCommandEvents,
  loading,
  error,
  scrollRef,
  showJumpToLatest,
  onTimelineLayout,
  onTimelineScroll,
  onTimelineContentSizeChange,
  onScrollToLatest,
  onComposerHeightChange,
  onUnavailableAction,
  inputRef,
  draft,
  editable,
  composerFocused,
  canAttach,
  uploading,
  sending,
  attachments,
  composerPresentation,
  chrome,
  theme,
  onSelectCommand,
  onRemoveAttachment,
  onDraftChange,
  onUploadPress,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
  onSendPress,
}: CodexChatBodyProps) {
  const { composerHeight, handleComposerLayout } = useCodexComposerLayout({
    onHeightChange: onComposerHeightChange,
  });

  return (
    <KeyboardAvoidingView
      behavior="padding"
      enabled={screenFocused}
      keyboardVerticalOffset={composerPresentation.keyboardVerticalOffset}
      style={styles.chatBody}
    >
      <CodexChatTimelineSection
        serverId={serverId}
        agentCwd={agentCwd}
        conversation={conversation}
        events={events}
        chatCommandEvents={chatCommandEvents}
        loading={loading}
        error={error}
        composerActive={composerPresentation.active}
        composerHeight={composerHeight}
        scrollRef={scrollRef}
        showJumpToLatest={showJumpToLatest}
        chrome={chrome}
        theme={theme}
        onLayout={onTimelineLayout}
        onScroll={onTimelineScroll}
        onContentSizeChange={onTimelineContentSizeChange}
        onScrollToLatest={onScrollToLatest}
        onUnavailableAction={onUnavailableAction}
      />

      <CodexChatComposer
        inputRef={inputRef}
        draft={draft}
        placeholder={composerPresentation.placeholder}
        editable={editable}
        focused={composerFocused}
        floating={composerPresentation.active}
        canAttach={canAttach}
        uploading={uploading}
        sendEnabled={composerPresentation.sendEnabled}
        sending={sending}
        sendIcon={composerPresentation.sendIcon}
        sendLabel={composerPresentation.sendLabel}
        compactSendIcon={composerPresentation.showStopButton}
        bottomPadding={composerPresentation.bottomPadding}
        showCommandMenu={composerPresentation.showCommandMenu}
        commandQuery={composerPresentation.commandQuery}
        commands={composerPresentation.visibleSlashCommands}
        attachments={attachments}
        chrome={chrome}
        theme={theme}
        onLayout={handleComposerLayout}
        onSelectCommand={onSelectCommand}
        onRemoveAttachment={onRemoveAttachment}
        onDraftChange={onDraftChange}
        onUploadPress={onUploadPress}
        onInputPress={onInputPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onInputStart={onInputStart}
        onSubmit={onSubmit}
        onSendPress={onSendPress}
      />
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  chatBody: {
    flex: 1,
    minHeight: 0,
  },
});
