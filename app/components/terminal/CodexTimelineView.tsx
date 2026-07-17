import React from "react";
import {
  FlatList,
  Platform,
  StyleSheet,
  View,
  type GestureResponderEvent,
  type LayoutChangeEvent,
  type ListRenderItemInfo,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ScrollViewProps,
} from "react-native";
import type { SharedValue } from "react-native-reanimated";
import { useAppTheme } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

import { CodexTimelineEmptyContent } from "./CodexTimelineContent";
import {
  TimelineTextSelectableContext,
  type TimelineTextSelectableContextValue,
} from "./TimelineTextSelectableContext";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import { CodexTimelineDateDivider } from "./CodexTimelineDateDivider";
import {
  buildTimelineRenderItems,
  type TimelineRenderItem,
} from "./CodexTimelineGrouping";
import type {
  PatchFileSummary,
} from "./CodexTimelineActivityTypes";
import { TIMELINE_LIST_STABILITY_PROPS } from "./timelineScrollPolicy";
import { StructuredChatInsetScrollView } from "./StructuredChatInsetScrollView";

interface CodexTimelineViewProps {
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  emptyStateSuppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  textSelectable: boolean;
  extraContentPadding: SharedValue<number>;
  topChromeInset: number;
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
  /** Passively observes touch lifetime without taking the scroll responder. */
  onTouchActiveChange?(active: boolean): void;
  onContentSizeChange(width: number, height: number): void;
  onLatestOffsetChange(offset: number): void;
  onTextSelectionGestureStart: TimelineTextSelectableContextValue["onTextSelectionGestureStart"];
  onTextSelectionGestureEnd: TimelineTextSelectableContextValue["onTextSelectionGestureEnd"];
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineView({
  scrollRef,
  items,
  loading,
  error,
  emptyStateSuppressed,
  unavailable,
  unavailableReason,
  syncing,
  textSelectable,
  extraContentPadding,
  topChromeInset,
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
  onTouchActiveChange,
  onContentSizeChange,
  onLatestOffsetChange,
  onTextSelectionGestureStart,
  onTextSelectionGestureEnd,
  onUnavailableAction,
  showUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: CodexTimelineViewProps) {
  const { theme: zenTheme } = useAppTheme();
  const renderItems = React.useMemo(
    () =>
      buildTimelineRenderItems([...items].reverse(), {
        showDateDividers: zenTheme.chat.showDateDividers,
      }),
    [items, zenTheme.chat.showDateDividers],
  );
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
    ({ item }: ListRenderItemInfo<TimelineRenderItem>) => {
      if (item.type === "date-divider") {
        return <CodexTimelineDateDivider label={item.label} chrome={chrome} />;
      }
      return (
        <ZenTimelineItemView
          item={item}
          presentation={item.type === "message" ? item.presentation : undefined}
          chrome={chrome}
          theme={theme}
          loadAssetPreview={loadAssetPreview}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      );
    },
    [
      chrome,
      formatPatchPath,
      loadAssetPreview,
      theme,
      truncateBody,
    ],
  );
  const renderScrollComponent = React.useCallback(
    (props: ScrollViewProps) => (
      <StructuredChatInsetScrollView
        {...props}
        clearance={extraContentPadding}
        inverted
        onLatestOffsetChange={onLatestOffsetChange}
      />
    ),
    [extraContentPadding, onLatestOffsetChange],
  );
  const handleTouchStart = React.useCallback(() => {
    onTouchActiveChange?.(true);
  }, [onTouchActiveChange]);
  const handleTouchEnd = React.useCallback((event: GestureResponderEvent) => {
    onTouchActiveChange?.(event.nativeEvent.touches.length > 0);
  }, [onTouchActiveChange]);
  const handleTouchCancel = React.useCallback(() => {
    onTouchActiveChange?.(false);
  }, [onTouchActiveChange]);

  const emptyContent = React.useMemo(
    () => (
      <CodexTimelineEmptyContent
        items={items}
        loading={loading}
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
        <FlatList<TimelineRenderItem>
          accessibilityLabel="Conversation timeline"
          testID="structured-chat-timeline"
          ref={scrollRef as React.RefObject<FlatList<TimelineRenderItem> | null>}
          data={renderItems}
          keyExtractor={(item) => item.id}
          renderItem={renderItem}
          style={styles.timeline}
          contentContainerStyle={[
            styles.timelineContent,
            { paddingBottom: Math.max(12, topChromeInset) },
          ]}
          inverted
          renderScrollComponent={renderScrollComponent}
          {...TIMELINE_LIST_STABILITY_PROPS}
          keyboardDismissMode={
            Platform.OS === "ios" ? "interactive" : "on-drag"
          }
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
          scrollEventThrottle={32}
          onLayout={onLayout}
          onScroll={onScroll}
          onScrollBeginDrag={onScrollBeginDrag}
          onScrollEndDrag={onScrollEndDrag}
          onMomentumScrollBegin={onMomentumScrollBegin}
          onMomentumScrollEnd={onMomentumScrollEnd}
          onTouchStart={handleTouchStart}
          onTouchEnd={handleTouchEnd}
          onTouchCancel={handleTouchCancel}
          onContentSizeChange={onContentSizeChange}
          initialNumToRender={8}
          maxToRenderPerBatch={6}
          updateCellsBatchingPeriod={48}
          windowSize={5}
        />

        {items.length === 0 ? (
          <View style={styles.emptyOverlay}>
            {emptyContent}
          </View>
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
    alignItems: "stretch",
    paddingHorizontal: 14,
    paddingBottom: 12,
    flexGrow: 1,
  },
  emptyOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    paddingHorizontal: 14,
    paddingTop: 14,
    justifyContent: "center",
    pointerEvents: "box-none",
  },
});
