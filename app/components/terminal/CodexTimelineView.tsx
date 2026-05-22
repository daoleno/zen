import React from "react";
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TimelineTextSelectableContext } from "./CodexMessageBody";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import type { PatchFileSummary } from "./CodexTimelineActivity";

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
          <EmptyState chrome={chrome} title="Loading Codex transcript" busy />
        ) : error && items.length === 0 ? (
          <EmptyState chrome={chrome} title="Transcript unavailable" body={error} />
        ) : unavailable ? (
          <EmptyState
            chrome={chrome}
            title="Native transcript unavailable"
            body={unavailableReason}
            actionLabel="Terminal"
            onAction={onUnavailableAction}
          />
        ) : items.length === 0 ? (
          <EmptyState chrome={chrome} title="Waiting for Codex transcript" />
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
        <TouchableOpacity
          accessibilityLabel="Jump to latest"
          style={[
            styles.jumpButton,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.borderStrong,
              bottom: jumpButtonBottom,
            },
          ]}
          onPress={onJumpToLatest}
          activeOpacity={0.82}
        >
          <Ionicons name="arrow-down" size={15} color={chrome.accent} />
          <Text style={[styles.jumpButtonText, { color: chrome.textMuted }]}>Latest</Text>
        </TouchableOpacity>
      ) : null}
    </TimelineTextSelectableContext.Provider>
  );
}

function EmptyState({
  chrome,
  title,
  body,
  busy = false,
  actionLabel,
  onAction,
}: {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <View style={styles.emptyState}>
      {busy ? <ActivityIndicator color={chrome.accent} /> : null}
      <Text style={[styles.emptyTitle, { color: chrome.text }]}>{title}</Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>{body}</Text>
      ) : null}
      {actionLabel && onAction ? (
        <TouchableOpacity
          style={[
            styles.emptyAction,
            { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
          ]}
          onPress={onAction}
          activeOpacity={0.82}
        >
          <Ionicons name="terminal-outline" size={15} color={chrome.textMuted} />
          <Text style={[styles.emptyActionText, { color: chrome.textMuted }]}>
            {actionLabel}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
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
  jumpButton: {
    position: "absolute",
    right: 14,
    minHeight: 32,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    zIndex: 4,
  },
  jumpButtonText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  emptyState: {
    minHeight: 260,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  emptyTitle: {
    marginTop: 10,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    marginTop: 7,
    fontSize: 12,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
  emptyAction: {
    marginTop: 14,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 7,
  },
  emptyActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
});
