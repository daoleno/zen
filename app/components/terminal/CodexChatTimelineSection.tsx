import React, { useCallback } from "react";
import {
  type FlatList,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import {
  conversationUnavailableReason,
  isConversationSyncingReason,
} from "./CodexChatControllerModel";
import type {
  CodexChatLocalState,
  PendingAssistantMessage,
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
  pendingAssistantMessages: PendingAssistantMessage[];
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  composerActive: boolean;
  commandMenuOpen: boolean;
  composerHeight: number;
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  showJumpToLatest: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onScrollBeginDrag(): void;
  onScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onMomentumScrollBegin(): void;
  onMomentumScrollEnd(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onContentSizeChange(width: number, height: number): void;
  onScrollToLatest(animated?: boolean, delay?: number): void;
  onUnavailableAction(): void;
}

export function CodexChatTimelineSection({
  serverId,
  agentCwd,
  conversation,
  events,
  pendingUserMessages,
  pendingAssistantMessages,
  loading,
  localChatState,
  error,
  composerActive,
  commandMenuOpen,
  composerHeight,
  scrollRef,
  showJumpToLatest,
  chrome,
  theme,
  onLayout,
  onScroll,
  onScrollBeginDrag,
  onScrollEndDrag,
  onMomentumScrollBegin,
  onMomentumScrollEnd,
  onContentSizeChange,
  onScrollToLatest,
  onUnavailableAction,
}: CodexChatTimelineSectionProps) {
  const timelineItems = useCodexTimelineItems({
    events,
    pendingUserMessages,
    pendingAssistantMessages,
  });
  const streamingAssistantId = pendingAssistantMessages.find((message) =>
    message.body.trim(),
  )?.id ?? "";
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
      textSelectable={!composerActive}
      showJumpToLatest={showJumpToLatest}
      jumpButtonBottom={composerHeight + 12}
      streamingAssistantId={streamingAssistantId}
      chrome={chrome}
      theme={theme}
      onLayout={onLayout}
      onScroll={onScroll}
      onScrollBeginDrag={onScrollBeginDrag}
      onScrollEndDrag={onScrollEndDrag}
      onMomentumScrollBegin={onMomentumScrollBegin}
      onMomentumScrollEnd={onMomentumScrollEnd}
      onContentSizeChange={onContentSizeChange}
      onJumpToLatest={() => onScrollToLatest(false, 0)}
      onUnavailableAction={onUnavailableAction}
      loadAssetPreview={loadAssetPreview}
      formatPatchPath={patchDisplayPath}
      truncateBody={truncateRunes}
    />
  );
}
