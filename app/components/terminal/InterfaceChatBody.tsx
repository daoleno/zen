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
import type { SharedValue } from "react-native-reanimated";
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
import type { ActiveAttachmentUpload } from "../../services/uploads";
import type {
  ComposerAttachment,
  PendingUserMessage,
} from "./InterfaceChatSession";
import type { InterfaceComposerPresentation } from "./InterfaceChatSurfaceModel";
import { InterfaceChatComposerSection } from "./InterfaceChatComposerSection";
import { InterfaceChatKeyboardFrame } from "./InterfaceChatKeyboardFrame";
import { InterfaceChatTimelineSection } from "./InterfaceChatTimelineSection";
import { TerminalActionPromptCard } from "./TerminalActionPromptCard";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type { TurnFocusSpacerRequest } from "./turnFocusState";
import { InterfaceTimelineJumpButton } from "./InterfaceTimelineJumpButton";

export interface InterfaceChatBodyProps {
  screenFocused: boolean;
  readOnly?: boolean;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  agentProcessId?: number;
  agentStartedAt?: number;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  turnFocusAnchorAliases?: ReadonlyMap<string, string>;
  runningActivity?: ProviderActivity;
  supplementaryTimelineItems?: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  nativeFollowSuspended: boolean;
  timelineTextSelectable: boolean;
  turnFocusClearanceRequest: SharedValue<number>;
  turnFocusSpacer: SharedValue<TurnFocusSpacerRequest>;
  turnFocusPendingMessageId?: string;
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
  onTimelineItemsMutated(): void;
  onTimelineContentSizeChange(width: number, height: number): void;
  onTimelineClearanceChange(
    intentToken: number,
    clearance: number,
    latestOffset: number,
  ): void;
  onTurnFocusAnchorAvailable(pendingMessageId: string): void;
  onTurnFocusRowLayout(
    pendingMessageId: string,
    height: number,
    newestEdgeOffset: number,
  ): void;
  onTurnFocusSpacerLayout(height: number, requestEpoch: number): void;
  onTimelineTextSelectionGestureStart(): void;
  onTimelineTextSelectionGestureEnd(): void;
  onKeyboardLifecycleInvalidate(reason: "route" | "app"): void;
  onScrollToLatest(animated?: boolean): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  composerFocused: boolean;
  canAttach: boolean;
  uploading: boolean;
  activeUpload: ActiveAttachmentUpload | null;
  sending: boolean;
  operationalError?: string;
  attachments: ComposerAttachment[];
  composerPresentation: InterfaceComposerPresentation;
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
  onCancelUpload(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
  onRetryPendingUserMessage(id: string): void;
  onTerminalActionKey(key: string): Promise<void> | void;
  composerAccessory?: React.ReactNode;
  skillsSheet?: React.ReactNode;
}

export function InterfaceChatBody({
  screenFocused,
  readOnly = false,
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
  supplementaryTimelineItems,
  loading,
  error,
  scrollRef,
  nativeFollowSuspended,
  timelineTextSelectable,
  turnFocusClearanceRequest,
  turnFocusSpacer,
  turnFocusPendingMessageId,
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
  onTimelineItemsMutated,
  onTimelineContentSizeChange,
  onTimelineClearanceChange,
  onTurnFocusAnchorAvailable,
  onTurnFocusRowLayout,
  onTurnFocusSpacerLayout,
  onTimelineTextSelectionGestureStart,
  onTimelineTextSelectionGestureEnd,
  onKeyboardLifecycleInvalidate,
  onScrollToLatest,
  onUnavailableAction,
  showUnavailableAction,
  inputRef,
  draft,
  editable,
  composerFocused,
  canAttach,
  uploading,
  activeUpload,
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
  onCancelUpload,
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
  onRetryPendingUserMessage,
  onTerminalActionKey,
  composerAccessory,
  skillsSheet,
}: InterfaceChatBodyProps) {
  const composer = !readOnly ? (
    <>
      {terminalActionPrompt ? (
        <TerminalActionPromptCard
          key={terminalActionPrompt.id}
          prompt={terminalActionPrompt}
          chrome={chrome}
          theme={theme}
          onSendKey={onTerminalActionKey}
          onSwitchToTerminal={onUnavailableAction}
        />
      ) : null}
      {composerAccessory}
      <InterfaceChatComposerSection
        inputRef={inputRef}
        draft={draft}
        editable={editable}
        focused={composerFocused}
        canAttach={canAttach}
        uploading={uploading}
        activeUpload={activeUpload}
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
        onCancelUpload={onCancelUpload}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onSendPress={onSendPress}
        onStopPress={onStopPress}
      />
    </>
  ) : undefined;
  const floatingAction = showJumpToLatest ? (
    <View style={styles.jumpSlot}>
      <InterfaceTimelineJumpButton
        bottom={0}
        chrome={chrome}
        label={jumpLabel}
        onPress={() => onScrollToLatest(false)}
      />
    </View>
  ) : undefined;

  return (
    <InterfaceChatKeyboardFrame
      enabled={screenFocused}
      keyboardVerticalOffset={composerPresentation.keyboardVerticalOffset}
      chrome={chrome}
      topChromeInset={topChromeInset}
      composer={composer}
      floatingAction={floatingAction}
      portal={skillsSheet}
      onKeyboardLifecycleInvalidate={onKeyboardLifecycleInvalidate}
      renderTimeline={(extraContentPadding, keyboardLifecycleGate) => (
        <InterfaceChatTimelineSection
          serverId={serverId}
          serverUrl={serverUrl}
          daemonId={daemonId}
          agentId={agentId}
          agentProcessId={agentProcessId}
          agentStartedAt={agentStartedAt}
          agentCwd={agentCwd}
          conversation={conversation}
          events={events}
          pendingUserMessages={pendingUserMessages}
          turnFocusAnchorAliases={turnFocusAnchorAliases}
          onRetryPendingUserMessage={onRetryPendingUserMessage}
          runningActivity={runningActivity}
          supplementaryItems={supplementaryTimelineItems}
          loading={loading}
          error={error}
          commandMenuOpen={composerPresentation.showCommandMenu}
          scrollRef={scrollRef}
          nativeFollowSuspended={nativeFollowSuspended}
          textSelectable={timelineTextSelectable}
          extraContentPadding={extraContentPadding}
          keyboardLifecycleGate={keyboardLifecycleGate}
          turnFocusClearanceRequest={turnFocusClearanceRequest}
          turnFocusSpacer={turnFocusSpacer}
          turnFocusPendingMessageId={turnFocusPendingMessageId}
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
          onItemsMutated={onTimelineItemsMutated}
          onContentSizeChange={onTimelineContentSizeChange}
          onClearanceChange={onTimelineClearanceChange}
          onTurnFocusAnchorAvailable={onTurnFocusAnchorAvailable}
          onTurnFocusRowLayout={onTurnFocusRowLayout}
          onTurnFocusSpacerLayout={onTurnFocusSpacerLayout}
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
