import React from "react";
import {
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type FlatList,
  type TextInput,
} from "react-native";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import type {
  CodexChatLocalState,
  ComposerAttachment,
  PendingSlashCommand,
  PendingUserMessage,
} from "./CodexChatSession";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import { CodexChatComposerSection } from "./CodexChatComposerSection";
import { CodexChatKeyboardFrame } from "./CodexChatKeyboardFrame";
import { CodexChatTimelineSection } from "./CodexChatTimelineSection";
import { TerminalActionPromptCard } from "./TerminalActionPromptCard";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
import { useCodexComposerLayout } from "./useCodexComposerLayout";

export interface CodexChatBodyProps {
  screenFocused: boolean;
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  timelineTextSelectable: boolean;
  showJumpToLatest: boolean;
  jumpLabel?: string;
  emptyTitle?: string;
  emptyBody?: string;
  onTimelineLayout(event: LayoutChangeEvent): void;
  onTimelineScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onTimelineScrollBeginDrag(): void;
  onTimelineScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onTimelineMomentumScrollBegin(): void;
  onTimelineMomentumScrollEnd(
    event: NativeSyntheticEvent<NativeScrollEvent>,
  ): void;
  onTimelineContentSizeChange(width: number, height: number): void;
  onTimelineTextSelectionGestureStart(): void;
  onTimelineTextSelectionGestureEnd(): void;
  onScrollToLatest(animated?: boolean, delay?: number): void;
  onComposerHeightChange(height: number): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  composerFocused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sending: boolean;
  attachments: ComposerAttachment[];
  composerPresentation: CodexComposerPresentation;
  terminalActionPrompt?: TerminalActionPrompt | null;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
  onRemoveAttachment(id: string): void;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onInputStart(): boolean;
  onSubmit(): void;
  onSendPress(): void;
  onTerminalActionKey(key: string): Promise<void> | void;
  composerAccessory?: React.ReactNode;
  skillsSheet?: React.ReactNode;
}

export function CodexChatBody({
  screenFocused,
  serverId,
  agentCwd,
  conversation,
  events,
  pendingUserMessages,
  pendingSlashCommands,
  loading,
  localChatState,
  error,
  scrollRef,
  timelineTextSelectable,
  showJumpToLatest,
  jumpLabel,
  emptyTitle,
  emptyBody,
  onTimelineLayout,
  onTimelineScroll,
  onTimelineScrollBeginDrag,
  onTimelineScrollEndDrag,
  onTimelineMomentumScrollBegin,
  onTimelineMomentumScrollEnd,
  onTimelineContentSizeChange,
  onTimelineTextSelectionGestureStart,
  onTimelineTextSelectionGestureEnd,
  onScrollToLatest,
  onComposerHeightChange,
  onUnavailableAction,
  showUnavailableAction,
  inputRef,
  draft,
  editable,
  composerFocused,
  canAttach,
  uploading,
  sending,
  attachments,
  composerPresentation,
  terminalActionPrompt,
  chrome,
  theme,
  onSelectCommand,
  onToggleActionMenu,
  onDismissActionMenu,
  onRemoveAttachment,
  onDraftChange,
  onUploadPress,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
  onSendPress,
  onTerminalActionKey,
  composerAccessory,
  skillsSheet,
}: CodexChatBodyProps) {
  const { handleComposerLayout } = useCodexComposerLayout({
    onHeightChange: onComposerHeightChange,
  });

  return (
    <CodexChatKeyboardFrame
      enabled={screenFocused}
      keyboardVerticalOffset={composerPresentation.keyboardVerticalOffset}
      automaticOffset={composerPresentation.automaticKeyboardOffset}
    >
      <CodexChatTimelineSection
        serverId={serverId}
        agentCwd={agentCwd}
        conversation={conversation}
        events={events}
        pendingUserMessages={pendingUserMessages}
        pendingSlashCommands={pendingSlashCommands}
        loading={loading}
        localChatState={localChatState}
        error={error}
        commandMenuOpen={composerPresentation.showCommandMenu}
        scrollRef={scrollRef}
        textSelectable={timelineTextSelectable}
        showJumpToLatest={showJumpToLatest}
        jumpLabel={jumpLabel}
        chrome={chrome}
        theme={theme}
        onLayout={onTimelineLayout}
        onScroll={onTimelineScroll}
        onScrollBeginDrag={onTimelineScrollBeginDrag}
        onScrollEndDrag={onTimelineScrollEndDrag}
        onMomentumScrollBegin={onTimelineMomentumScrollBegin}
        onMomentumScrollEnd={onTimelineMomentumScrollEnd}
        onContentSizeChange={onTimelineContentSizeChange}
        onTextSelectionGestureStart={onTimelineTextSelectionGestureStart}
        onTextSelectionGestureEnd={onTimelineTextSelectionGestureEnd}
        onScrollToLatest={onScrollToLatest}
        onUnavailableAction={onUnavailableAction}
        showUnavailableAction={showUnavailableAction}
        emptyTitle={emptyTitle}
        emptyBody={emptyBody}
      />

      {terminalActionPrompt ? (
        <TerminalActionPromptCard
          prompt={terminalActionPrompt}
          chrome={chrome}
          theme={theme}
          onSendKey={onTerminalActionKey}
          onSwitchToTerminal={onUnavailableAction}
        />
      ) : null}

      {composerAccessory}
      <CodexChatComposerSection
        inputRef={inputRef}
        draft={draft}
        editable={editable}
        focused={composerFocused}
        canAttach={canAttach}
        uploading={uploading}
        sending={sending}
        attachments={attachments}
        presentation={composerPresentation}
        chrome={chrome}
        theme={theme}
        onLayout={handleComposerLayout}
        onSelectCommand={onSelectCommand}
        onToggleActionMenu={onToggleActionMenu}
        onDismissActionMenu={onDismissActionMenu}
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
      {skillsSheet}
    </CodexChatKeyboardFrame>
  );
}
