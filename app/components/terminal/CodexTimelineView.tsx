import React from "react";
import {
  FlatList,
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type ListRenderItemInfo,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexTimelineEmptyContent } from "./CodexTimelineContent";
import { CodexTimelineJumpButton } from "./CodexTimelineJumpButton";
import type { CodexChatLocalState } from "./CodexChatSession";
import {
  TimelineTextSelectableContext,
  type TimelineTextSelectableContextValue,
} from "./TimelineTextSelectableContext";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import type {
  PatchFileSummary,
} from "./CodexTimelineActivityTypes";

interface CodexTimelineViewProps {
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  items: ZenTimelineItem[];
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  emptyStateSuppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  textSelectable: boolean;
  showJumpToLatest: boolean;
  jumpButtonBottom: number;
  jumpLabel?: string;
  emptyTitle?: string;
  emptyBody?: string;
  agentCwd?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onScrollBeginDrag(): void;
  onScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onMomentumScrollBegin(): void;
  onMomentumScrollEnd(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onContentSizeChange(width: number, height: number): void;
  onTextSelectionGestureStart: TimelineTextSelectableContextValue["onTextSelectionGestureStart"];
  onTextSelectionGestureEnd: TimelineTextSelectableContextValue["onTextSelectionGestureEnd"];
  onJumpToLatest(): void;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

const TIMELINE_BOTTOM_PADDING = 18;

export function CodexTimelineView({
  scrollRef,
  items,
  loading,
  localChatState,
  error,
  emptyStateSuppressed,
  unavailable,
  unavailableReason,
  syncing,
  textSelectable,
  showJumpToLatest,
  jumpButtonBottom,
  jumpLabel,
  emptyTitle,
  emptyBody,
  agentCwd,
  chrome,
  theme,
  onLayout,
  onScroll,
  onScrollBeginDrag,
  onScrollEndDrag,
  onMomentumScrollBegin,
  onMomentumScrollEnd,
  onContentSizeChange,
  onTextSelectionGestureStart,
  onTextSelectionGestureEnd,
  onJumpToLatest,
  onUnavailableAction,
  showUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: CodexTimelineViewProps) {
  const renderItems = React.useMemo(() => [...items].reverse(), [items]);
  const textSelectionContext = React.useMemo(
    () => ({
      selectable: textSelectable,
      onTextSelectionGestureStart,
      onTextSelectionGestureEnd,
    }),
    [
      onTextSelectionGestureEnd,
      onTextSelectionGestureStart,
      textSelectable,
    ],
  );
  const renderItem = React.useCallback(
    ({ item }: ListRenderItemInfo<ZenTimelineItem>) => (
      <ZenTimelineItemView
        item={item}
        chrome={chrome}
        theme={theme}
        loadAssetPreview={loadAssetPreview}
        formatPatchPath={formatPatchPath}
        truncateBody={truncateBody}
      />
    ),
    [
      chrome,
      formatPatchPath,
      loadAssetPreview,
      theme,
      truncateBody,
    ],
  );

  const emptyContent = React.useMemo(
    () => (
      <CodexTimelineEmptyContent
        items={items}
        loading={loading}
        localChatState={localChatState}
        error={error}
        suppressed={emptyStateSuppressed}
        unavailable={unavailable}
        unavailableReason={unavailableReason}
        syncing={syncing}
        chrome={chrome}
        onUnavailableAction={onUnavailableAction}
        showUnavailableAction={showUnavailableAction}
        agentCwd={agentCwd}
        emptyTitle={emptyTitle}
        emptyBody={emptyBody}
      />
    ),
    [
      agentCwd,
      chrome,
      error,
      emptyStateSuppressed,
      items,
      loading,
      localChatState,
      onUnavailableAction,
      showUnavailableAction,
      emptyTitle,
      emptyBody,
      unavailable,
      unavailableReason,
      syncing,
    ],
  );

  return (
    <TimelineTextSelectableContext.Provider value={textSelectionContext}>
      <View style={styles.timelineStage}>
        <FlatList
          ref={scrollRef}
          data={renderItems}
          keyExtractor={(item) => item.id}
          renderItem={renderItem}
          style={styles.timeline}
          contentContainerStyle={styles.timelineContent}
          inverted
          scrollIndicatorInsets={{ bottom: TIMELINE_BOTTOM_PADDING }}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
          scrollEventThrottle={32}
          onLayout={onLayout}
          onScroll={onScroll}
          onScrollBeginDrag={onScrollBeginDrag}
          onScrollEndDrag={onScrollEndDrag}
          onMomentumScrollBegin={onMomentumScrollBegin}
          onMomentumScrollEnd={onMomentumScrollEnd}
          onContentSizeChange={onContentSizeChange}
          initialNumToRender={12}
          maxToRenderPerBatch={8}
          updateCellsBatchingPeriod={32}
          windowSize={7}
          removeClippedSubviews={false}
        />

        {items.length === 0 ? (
          <View style={styles.emptyOverlay}>
            {emptyContent}
          </View>
        ) : null}

        {showJumpToLatest ? (
          <CodexTimelineJumpButton
            bottom={jumpButtonBottom}
            chrome={chrome}
            label={jumpLabel}
            onPress={onJumpToLatest}
          />
        ) : null}
      </View>
    </TimelineTextSelectableContext.Provider>
  );
}

const styles = StyleSheet.create({
  timelineStage: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  timeline: {
    flex: 1,
    minHeight: 0,
  },
  timelineContent: {
    paddingHorizontal: 16,
    paddingTop: TIMELINE_BOTTOM_PADDING,
    paddingBottom: 14,
    flexGrow: 1,
  },
  emptyOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    paddingHorizontal: 16,
    paddingTop: 14,
    paddingBottom: TIMELINE_BOTTOM_PADDING,
    justifyContent: "center",
    pointerEvents: "box-none",
  },
});
