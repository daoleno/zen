import React, { useCallback } from "react";
import {
  type FlatList,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import type { SharedValue } from "react-native-reanimated";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  CodexConversation,
  CodexConversationEvent,
  StructuredTurn,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import {
  conversationUnavailableReason,
  isConversationSyncingReason,
} from "./CodexChatControllerModel";
import type {
  CodexChatLocalState,
  PendingSlashCommand,
  PendingUserMessage,
} from "./CodexChatSession";
import { CodexTimelineView } from "./CodexTimelineView";
import {
  patchDisplayPath,
  truncateRunes,
} from "./CodexTimelineModel";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
import { useCodexTimelineItems } from "./useCodexTimelineItems";

interface CodexChatTimelineSectionProps {
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
  workingTurn?: StructuredTurn;
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  commandMenuOpen: boolean;
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  textSelectable: boolean;
  extraContentPadding: SharedValue<number>;
  topChromeInset: number;
  emptyTitle?: string;
  emptyBody?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onScrollBeginDrag(): void;
  onScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onMomentumScrollBegin(): void;
  onMomentumScrollEnd(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onContentSizeChange(width: number, height: number): void;
  onLatestOffsetChange(offset: number): void;
  onTextSelectionGestureStart(): void;
  onTextSelectionGestureEnd(): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  onRetryPendingUserMessage(id: string): void;
}

export function CodexChatTimelineSection({
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
  commandMenuOpen,
  scrollRef,
  textSelectable,
  extraContentPadding,
  topChromeInset,
  emptyTitle,
  emptyBody,
  chrome,
  theme,
  onLayout,
  onScroll,
  onScrollBeginDrag,
  onScrollEndDrag,
  onMomentumScrollBegin,
  onMomentumScrollEnd,
  onContentSizeChange,
  onLatestOffsetChange,
  onTextSelectionGestureStart,
  onTextSelectionGestureEnd,
  onUnavailableAction,
  showUnavailableAction,
  onRetryPendingUserMessage,
}: CodexChatTimelineSectionProps) {
  const timelineItems = useCodexTimelineItems({
    events,
    pendingUserMessages,
    pendingSlashCommands,
    workingTurn,
    onRetryPendingUserMessage,
  });
  const loadAssetPreview = useCallback(
    async (path: string) => {
      const asset = await wsClient.getCodexAsset(serverId, {
        path,
        cwd: conversation?.cwd || agentCwd,
      });
      return asset.data_url || null;
    },
    [agentCwd, conversation?.cwd, serverId],
  );
  const syncingConversation =
    Boolean(conversation && !conversation.available)
    && isConversationSyncingReason(conversation?.reason);
  const emptyConversationReady =
    syncingConversation && conversation?.reason === "transcript_not_found";

  return (
    <CodexTimelineView
      scrollRef={scrollRef}
      items={timelineItems}
      loading={loading}
      localChatState={localChatState}
      error={error}
      emptyStateSuppressed={commandMenuOpen}
      unavailable={
        conversation &&
        !conversation.available &&
        !syncingConversation &&
        !emptyConversationReady
      }
      unavailableReason={conversationUnavailableReason(conversation?.reason)}
      syncing={syncingConversation && !emptyConversationReady}
      textSelectable={textSelectable}
      extraContentPadding={extraContentPadding}
      topChromeInset={topChromeInset}
      chrome={chrome}
      theme={theme}
      agentCwd={agentCwd}
      emptyTitle={emptyTitle}
      emptyBody={emptyBody}
      onLayout={onLayout}
      onScroll={onScroll}
      onScrollBeginDrag={onScrollBeginDrag}
      onScrollEndDrag={onScrollEndDrag}
      onMomentumScrollBegin={onMomentumScrollBegin}
      onMomentumScrollEnd={onMomentumScrollEnd}
      onContentSizeChange={onContentSizeChange}
      onLatestOffsetChange={onLatestOffsetChange}
      onTextSelectionGestureStart={onTextSelectionGestureStart}
      onTextSelectionGestureEnd={onTextSelectionGestureEnd}
      onUnavailableAction={onUnavailableAction}
      showUnavailableAction={showUnavailableAction}
      loadAssetPreview={loadAssetPreview}
      formatPatchPath={patchDisplayPath}
      truncateBody={truncateRunes}
    />
  );
}
