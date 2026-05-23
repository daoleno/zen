import React from "react";
import {
  FlatList,
  StyleSheet,
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
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import type { PatchFileSummary } from "./CodexTimelineActivityTypes";

interface CodexTimelineViewProps {
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  textSelectable: boolean;
  showJumpToLatest: boolean;
  jumpButtonBottom: number;
  streamingAssistantId: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onScrollBeginDrag(): void;
  onScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onMomentumScrollBegin(): void;
  onMomentumScrollEnd(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onContentSizeChange(width: number, height: number): void;
  onJumpToLatest(): void;
  onUnavailableAction(): void;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

const TIMELINE_BOTTOM_PADDING = 18;

export function CodexTimelineView({
  scrollRef,
  items,
  loading,
  error,
  unavailable,
  unavailableReason,
  syncing,
  textSelectable,
  showJumpToLatest,
  jumpButtonBottom,
  streamingAssistantId,
  chrome,
  theme,
  onLayout,
  onScroll,
  onScrollBeginDrag,
  onScrollEndDrag,
  onMomentumScrollBegin,
  onMomentumScrollEnd,
  onContentSizeChange,
  onJumpToLatest,
  onUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: CodexTimelineViewProps) {
  const timelineData = React.useMemo(
    () => [...items].reverse(),
    [items],
  );

  const renderItem = React.useCallback(
    ({ item }: ListRenderItemInfo<ZenTimelineItem>) => (
      <ZenTimelineItemView
        item={item}
        chrome={chrome}
        theme={theme}
        stream={
          item.type === "message" &&
          item.role === "assistant" &&
          item.id === streamingAssistantId
        }
        loadAssetPreview={loadAssetPreview}
        formatPatchPath={formatPatchPath}
        truncateBody={truncateBody}
      />
    ),
    [
      chrome,
      formatPatchPath,
      loadAssetPreview,
      streamingAssistantId,
      theme,
      truncateBody,
    ],
  );

  const listEmptyComponent = React.useMemo(
    () => (
      <CodexTimelineEmptyContent
        items={items}
        loading={loading}
        error={error}
        unavailable={unavailable}
        unavailableReason={unavailableReason}
        syncing={syncing}
        chrome={chrome}
        onUnavailableAction={onUnavailableAction}
      />
    ),
    [
      chrome,
      error,
      items,
      loading,
      onUnavailableAction,
      unavailable,
      unavailableReason,
      syncing,
    ],
  );

  return (
    <TimelineTextSelectableContext.Provider value={textSelectable}>
      <FlatList
        ref={scrollRef}
        data={timelineData}
        keyExtractor={(item) => item.id}
        renderItem={renderItem}
        inverted
        style={styles.timeline}
        contentContainerStyle={[
          styles.timelineContent,
          items.length === 0 ? styles.timelineEmptyContent : null,
        ]}
        scrollIndicatorInsets={{ bottom: TIMELINE_BOTTOM_PADDING }}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
        scrollEventThrottle={80}
        onLayout={onLayout}
        onScroll={onScroll}
        onScrollBeginDrag={onScrollBeginDrag}
        onScrollEndDrag={onScrollEndDrag}
        onMomentumScrollBegin={onMomentumScrollBegin}
        onMomentumScrollEnd={onMomentumScrollEnd}
        onContentSizeChange={onContentSizeChange}
        ListEmptyComponent={listEmptyComponent}
        initialNumToRender={12}
        maxToRenderPerBatch={8}
        updateCellsBatchingPeriod={32}
        windowSize={7}
        removeClippedSubviews
      />

      {showJumpToLatest ? (
        <CodexTimelineJumpButton
          bottom={jumpButtonBottom}
          chrome={chrome}
          onPress={onJumpToLatest}
        />
      ) : null}
    </TimelineTextSelectableContext.Provider>
  );
}

const styles = StyleSheet.create({
  timeline: {
    flex: 1,
    minHeight: 0,
  },
  timelineContent: {
    paddingHorizontal: 16,
    paddingTop: 14,
    paddingBottom: TIMELINE_BOTTOM_PADDING,
  },
  timelineEmptyContent: {
    justifyContent: "center",
  },
});
