import React, { useCallback } from "react";
import {
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ScrollView,
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
import { conversationUnavailableReason } from "./CodexChatControllerModel";
import type { ChatCommandEvent } from "./CodexChatSession";
import { CodexTimelineView } from "./CodexTimelineView";
import {
  patchDisplayPath,
  truncateRunes,
} from "./CodexTimelineModel";
import { useCodexTimelineItems } from "./useCodexTimelineItems";

interface CodexChatTimelineSectionProps {
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  chatCommandEvents: ChatCommandEvent[];
  loading: boolean;
  error?: string | null;
  composerActive: boolean;
  composerHeight: number;
  scrollRef: React.RefObject<ScrollView | null>;
  showJumpToLatest: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onContentSizeChange(width: number, height: number): void;
  onScrollToLatest(animated?: boolean, delay?: number): void;
  onUnavailableAction(): void;
}

export function CodexChatTimelineSection({
  serverId,
  agentCwd,
  conversation,
  events,
  chatCommandEvents,
  loading,
  error,
  composerActive,
  composerHeight,
  scrollRef,
  showJumpToLatest,
  chrome,
  theme,
  onLayout,
  onScroll,
  onContentSizeChange,
  onScrollToLatest,
  onUnavailableAction,
}: CodexChatTimelineSectionProps) {
  const timelineItems = useCodexTimelineItems({ events, chatCommandEvents });
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

  return (
    <CodexTimelineView
      scrollRef={scrollRef}
      items={timelineItems}
      loading={loading}
      error={error}
      unavailable={conversation && !conversation.available}
      unavailableReason={conversationUnavailableReason(conversation?.reason)}
      textSelectable={!composerActive}
      showJumpToLatest={showJumpToLatest}
      jumpButtonBottom={composerHeight + 12}
      streamingAssistantId=""
      chrome={chrome}
      theme={theme}
      onLayout={onLayout}
      onScroll={onScroll}
      onContentSizeChange={onContentSizeChange}
      onJumpToLatest={() => onScrollToLatest(true)}
      onUnavailableAction={onUnavailableAction}
      loadAssetPreview={loadAssetPreview}
      formatPatchPath={patchDisplayPath}
      truncateBody={truncateRunes}
    />
  );
}
