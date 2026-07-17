import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type FlatList,
  type TextInput,
} from "react-native";
import type {
  CodexConversation,
  CodexConversationEvent,
  ProviderActivity,
} from "../../services/codexConversation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessage,
} from "./CodexChatSession";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import { CodexChatComposerSection } from "./CodexChatComposerSection";
import { CodexChatKeyboardFrame } from "./CodexChatKeyboardFrame";
import { CodexChatTimelineSection } from "./CodexChatTimelineSection";
import { TerminalActionPromptCard } from "./TerminalActionPromptCard";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
import { CodexTimelineJumpButton } from "./CodexTimelineJumpButton";

export interface CodexChatBodyProps {
  screenFocused: boolean;
  readOnly?: boolean;
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  runningActivity?: ProviderActivity;
  loading: boolean;
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
  onTimelineTouchActiveChange(active: boolean): void;
  onTimelineContentSizeChange(width: number, height: number): void;
  onTimelineLatestOffsetChange(offset: number): void;
  onTimelineTextSelectionGestureStart(): void;
  onTimelineTextSelectionGestureEnd(): void;
  onScrollToLatest(animated?: boolean, delay?: number): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  composerFocused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sending: boolean;
  operationalError?: string;
  attachments: ComposerAttachment[];
  composerPresentation: CodexComposerPresentation;
  topChromeInset?: number;
  terminalActionPrompt?: TerminalActionPrompt | null;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
  onRemoveAttachment(id: string): void;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
  onRetryPendingUserMessage(id: string): void;
  onTerminalActionKey(key: string): Promise<void> | void;
  composerAccessory?: React.ReactNode;
  skillsSheet?: React.ReactNode;
}

export function CodexChatBody({
  screenFocused,
  readOnly = false,
  serverId,
  agentCwd,
  conversation,
  events,
  pendingUserMessages,
  runningActivity,
  loading,
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
  onTimelineTouchActiveChange,
  onTimelineContentSizeChange,
  onTimelineLatestOffsetChange,
  onTimelineTextSelectionGestureStart,
  onTimelineTextSelectionGestureEnd,
  onScrollToLatest,
  onUnavailableAction,
  showUnavailableAction,
  inputRef,
  draft,
  editable,
  composerFocused,
  canAttach,
  uploading,
  sending,
  operationalError,
  attachments,
  composerPresentation,
  topChromeInset = 0,
  terminalActionPrompt,
  chrome,
  theme,
  onSelectCommand,
  onToggleActionMenu,
  onDismissActionMenu,
  onRemoveAttachment,
  onDraftChange,
  onUploadPress,
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
  onRetryPendingUserMessage,
  onTerminalActionKey,
  composerAccessory,
  skillsSheet,
}: CodexChatBodyProps) {
  const composer = !readOnly ? (
    <>
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
        operationalError={operationalError}
        attachments={attachments}
        presentation={composerPresentation}
        chrome={chrome}
        theme={theme}
        onSelectCommand={onSelectCommand}
        onToggleActionMenu={onToggleActionMenu}
        onDismissActionMenu={onDismissActionMenu}
        onRemoveAttachment={onRemoveAttachment}
        onDraftChange={onDraftChange}
        onUploadPress={onUploadPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onSendPress={onSendPress}
        onStopPress={onStopPress}
      />
    </>
  ) : undefined;
  const floatingAction = showJumpToLatest ? (
    <View style={styles.jumpSlot}>
      <CodexTimelineJumpButton
        bottom={0}
        chrome={chrome}
        label={jumpLabel}
        onPress={() => onScrollToLatest(false, 0)}
      />
    </View>
  ) : undefined;

  return (
    <CodexChatKeyboardFrame
      enabled={screenFocused}
      keyboardVerticalOffset={composerPresentation.keyboardVerticalOffset}
      chrome={chrome}
      topChromeInset={topChromeInset}
      composer={composer}
      floatingAction={floatingAction}
      portal={skillsSheet}
      renderTimeline={(extraContentPadding) => (
        <CodexChatTimelineSection
          serverId={serverId}
          agentCwd={agentCwd}
          conversation={conversation}
          events={events}
          pendingUserMessages={pendingUserMessages}
          onRetryPendingUserMessage={onRetryPendingUserMessage}
          runningActivity={runningActivity}
          loading={loading}
          error={error}
          commandMenuOpen={composerPresentation.showCommandMenu}
          scrollRef={scrollRef}
          textSelectable={timelineTextSelectable}
          extraContentPadding={extraContentPadding}
          topChromeInset={topChromeInset}
          chrome={chrome}
          theme={theme}
          onLayout={onTimelineLayout}
          onScroll={onTimelineScroll}
          onScrollBeginDrag={onTimelineScrollBeginDrag}
          onScrollEndDrag={onTimelineScrollEndDrag}
          onMomentumScrollBegin={onTimelineMomentumScrollBegin}
          onMomentumScrollEnd={onTimelineMomentumScrollEnd}
          onTouchActiveChange={onTimelineTouchActiveChange}
          onContentSizeChange={onTimelineContentSizeChange}
          onLatestOffsetChange={onTimelineLatestOffsetChange}
          onTextSelectionGestureStart={onTimelineTextSelectionGestureStart}
          onTextSelectionGestureEnd={onTimelineTextSelectionGestureEnd}
          onUnavailableAction={onUnavailableAction}
          showUnavailableAction={showUnavailableAction}
          emptyTitle={emptyTitle}
          emptyBody={emptyBody}
        />
      )}
    />
  );
}

const styles = StyleSheet.create({
  jumpSlot: {
    height: 52,
    position: "relative",
  },
});
