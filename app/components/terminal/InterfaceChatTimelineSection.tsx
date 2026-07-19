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
  ProviderActivity,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import {
  conversationUnavailableReason,
  isConversationSyncingReason,
} from "./InterfaceChatControllerModel";
import type { PendingUserMessage } from "./InterfaceChatSession";
import { InterfaceTimelineView } from "./InterfaceTimelineView";
import { patchDisplayPath, truncateRunes } from "./InterfaceTimelineModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import { useInterfaceTimelineItems } from "./useInterfaceTimelineItems";
import type { TurnFocusSpacerRequest } from "./turnFocusState";
import type { StructuredChatKeyboardLifecycleGate } from "./chatKeyboardOverlayPolicy";

interface InterfaceChatTimelineSectionProps {
  serverId: string;
  agentCwd?: string;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  turnFocusAnchorAliases?: ReadonlyMap<string, string>;
  runningActivity?: ProviderActivity;
  loading: boolean;
  error?: string | null;
  commandMenuOpen: boolean;
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  textSelectable: boolean;
  extraContentPadding: SharedValue<number>;
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>;
  turnFocusClearanceRequest: SharedValue<number>;
  turnFocusSpacer: SharedValue<TurnFocusSpacerRequest>;
  turnFocusPendingMessageId?: string;
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
  onTouchActiveChange(active: boolean): void;
  onContentSizeChange(width: number, height: number): void;
  onClearanceChange(
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
  onTextSelectionGestureStart(): void;
  onTextSelectionGestureEnd(): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  onRetryPendingUserMessage(id: string): void;
}

export function InterfaceChatTimelineSection({
  serverId,
  agentCwd,
  conversation,
  events,
  pendingUserMessages,
  turnFocusAnchorAliases,
  runningActivity,
  loading,
  error,
  commandMenuOpen,
  scrollRef,
  textSelectable,
  extraContentPadding,
  keyboardLifecycleGate,
  turnFocusClearanceRequest,
  turnFocusSpacer,
  turnFocusPendingMessageId,
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
  onTouchActiveChange,
  onContentSizeChange,
  onClearanceChange,
  onTurnFocusAnchorAvailable,
  onTurnFocusRowLayout,
  onTurnFocusSpacerLayout,
  onTextSelectionGestureStart,
  onTextSelectionGestureEnd,
  onUnavailableAction,
  showUnavailableAction,
  onRetryPendingUserMessage,
}: InterfaceChatTimelineSectionProps) {
  const timelineItems = useInterfaceTimelineItems({
    events,
    pendingUserMessages,
    turnFocusAnchorAliases,
    runningActivity,
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
    Boolean(conversation && !conversation.available) &&
    isConversationSyncingReason(conversation?.reason);
  const emptyConversationReady =
    syncingConversation && conversation?.reason === "transcript_not_found";

  return (
    <InterfaceTimelineView
      scrollRef={scrollRef}
      items={timelineItems}
      loading={loading}
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
      keyboardLifecycleGate={keyboardLifecycleGate}
      turnFocusClearanceRequest={turnFocusClearanceRequest}
      turnFocusSpacer={turnFocusSpacer}
      turnFocusPendingMessageId={turnFocusPendingMessageId}
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
      onTouchActiveChange={onTouchActiveChange}
      onContentSizeChange={onContentSizeChange}
      onClearanceChange={onClearanceChange}
      onTurnFocusAnchorAvailable={onTurnFocusAnchorAvailable}
      onTurnFocusRowLayout={onTurnFocusRowLayout}
      onTurnFocusSpacerLayout={onTurnFocusSpacerLayout}
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
