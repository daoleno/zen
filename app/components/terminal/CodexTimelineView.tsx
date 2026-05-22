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
import { CodexTimelineContent } from "./CodexTimelineContent";
import { CodexTimelineJumpButton } from "./CodexTimelineJumpButton";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
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
        <CodexTimelineContent
          items={items}
          loading={loading}
          error={error}
          unavailable={unavailable}
          unavailableReason={unavailableReason}
          streamingAssistantId={streamingAssistantId}
          chrome={chrome}
          theme={theme}
          onUnavailableAction={onUnavailableAction}
          loadAssetPreview={loadAssetPreview}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
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
