import React from "react";
import {
  ScrollView,
  StyleSheet,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexTimelineEmptyState } from "./CodexTimelineEmptyState";
import { CodexTimelineJumpButton } from "./CodexTimelineJumpButton";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import type { PatchFileSummary } from "./CodexTimelineActivityTypes";

interface CodexTimelineViewProps {
  scrollRef: React.RefObject<ScrollView | null>;
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  unavailable: boolean | null;
  unavailableReason?: string;
  textSelectable: boolean;
  showJumpToLatest: boolean;
  jumpButtonBottom: number;
  streamingAssistantId: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
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
  textSelectable,
  showJumpToLatest,
  jumpButtonBottom,
  streamingAssistantId,
  chrome,
  theme,
  onLayout,
  onScroll,
  onContentSizeChange,
  onJumpToLatest,
  onUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: CodexTimelineViewProps) {
  return (
    <TimelineTextSelectableContext.Provider value={textSelectable}>
      <ScrollView
        ref={scrollRef}
        style={styles.timeline}
        contentContainerStyle={styles.timelineContent}
        scrollIndicatorInsets={{ bottom: TIMELINE_BOTTOM_PADDING }}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
        scrollEventThrottle={80}
        onLayout={onLayout}
        onScroll={onScroll}
        onContentSizeChange={onContentSizeChange}
      >
        {loading && items.length === 0 ? (
          <CodexTimelineEmptyState
            chrome={chrome}
            title="Loading Codex transcript"
            busy
          />
        ) : error && items.length === 0 ? (
          <CodexTimelineEmptyState
            chrome={chrome}
            title="Transcript unavailable"
            body={error}
          />
        ) : unavailable ? (
          <CodexTimelineEmptyState
            chrome={chrome}
            title="Native transcript unavailable"
            body={unavailableReason}
            actionLabel="Terminal"
            onAction={onUnavailableAction}
          />
        ) : items.length === 0 ? (
          <CodexTimelineEmptyState
            chrome={chrome}
            title="Waiting for Codex transcript"
          />
        ) : (
          items.map((item) => (
            <ZenTimelineItemView
              key={item.id}
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
          ))
        )}
      </ScrollView>

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
});
