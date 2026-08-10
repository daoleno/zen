import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  brainWorkEventAccessibilityLabel,
  brainWorkEventReviewLabel,
  brainWorkEventSessionLabel,
  brainWorkEventSourceLabel,
  brainWorkEventSummary,
  brainWorkEventWorkTitle,
} from "./brainWorkEventPresentation";

export type BrainWorkEventTimelineItem = {
  type: "brain-work-event";
  id: string;
  timestamp: string;
  event: BrainWorkResultEvent;
  onPress?: () => void;
};

export function BrainWorkEventCard({
  item,
  chrome,
}: {
  item: BrainWorkEventTimelineItem;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const presentation = resolvePresentationColors(
    resultPresentation(item.event.kind),
    chrome,
  );
  const workTitle = brainWorkEventWorkTitle(item.event);
  const summary = brainWorkEventSummary(item.event);
  const source = brainWorkEventSourceLabel(item.event);
  const reviewLabel = brainWorkEventReviewLabel(item.event);
  const sessionLabel = brainWorkEventSessionLabel(item.event);
  const time = formatChatBubbleTime(item.event.occurred_at);
  const accessibilityTime = new Date(
    item.event.occurred_at,
  ).toLocaleString();
  const accessibilityLabel = brainWorkEventAccessibilityLabel({
    event: item.event,
    statusLabel: presentation.label,
    occurredAtLabel: accessibilityTime,
  });

  return (
    <Pressable
      accessibilityRole={item.onPress ? "button" : undefined}
      accessibilityLabel={accessibilityLabel}
      disabled={!item.onPress}
      onPress={item.onPress}
      style={({ pressed }) => [
        styles.wrap,
        item.event.unread ? styles.wrapUnread : null,
        pressed ? styles.wrapPressed : null,
      ]}
    >
      <View style={styles.header}>
        <View
          style={[
            styles.iconWrap,
            { backgroundColor: presentation.background },
          ]}
        >
          <Ionicons
            name={presentation.icon}
            size={15}
            color={presentation.color}
          />
        </View>
        <Text style={[styles.kind, { color: presentation.color }]}>
          {presentation.label}
        </Text>
        {item.event.unread ? (
          <View accessibilityElementsHidden style={styles.unreadDot} />
        ) : null}
      </View>

      <Text style={styles.title}>{workTitle}</Text>
      <Text numberOfLines={5} style={styles.summary}>
        {summary}
      </Text>

      <View style={styles.lifecycleRow}>
        <View style={styles.lifecycleBadge}>
          <Ionicons
            name={
              item.event.review_state === "queued"
                ? "time-outline"
                : item.event.review_state === "reviewing"
                  ? "eye-outline"
                  : "checkmark-done-outline"
            }
            size={12}
            color={chrome.textMuted}
          />
          <Text style={styles.lifecycleText}>{reviewLabel}</Text>
        </View>
        {sessionLabel ? (
          <Text style={styles.sessionLifecycleText}>{sessionLabel}</Text>
        ) : null}
        {!item.event.current_result ? (
          <Text style={styles.supersededText}>Superseded result</Text>
        ) : null}
      </View>

      <View style={styles.footer}>
        {source ? (
          <>
            <Ionicons
              name="git-branch-outline"
              size={13}
              color={chrome.textSubtle}
            />
            <Text numberOfLines={1} style={styles.source}>
              {source}
            </Text>
          </>
        ) : null}
        <Text style={styles.time}>{time}</Text>
      </View>
    </Pressable>
  );
}

function resultPresentation(kind: BrainWorkResultEvent["kind"]) {
  switch (kind) {
    case "session.done":
      return {
        label: "Completed",
        icon: "checkmark-circle-outline" as const,
        colorKey: "accent" as const,
      };
    case "session.failed":
      return {
        label: "Failed",
        icon: "alert-circle-outline" as const,
        colorKey: "danger" as const,
      };
    case "session.needs_input":
      return {
        label: "Needs input",
        icon: "help-circle-outline" as const,
        colorKey: "danger" as const,
      };
    case "session.stale":
      return {
        label: "Session unavailable",
        icon: "cloud-offline-outline" as const,
        colorKey: "textMuted" as const,
      };
    case "session.uncertain":
      return {
        label: "Outcome uncertain",
        icon: "help-circle-outline" as const,
        colorKey: "textMuted" as const,
      };
  }
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 1,
      marginBottom: 8,
      paddingHorizontal: 14,
      paddingVertical: 12,
      borderRadius: 14,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: chrome.border,
      backgroundColor: chrome.surfaceMuted,
    },
    wrapUnread: {
      borderColor: chrome.accent,
      backgroundColor: chrome.accentSoft,
    },
    wrapPressed: {
      opacity: 0.72,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      marginBottom: 8,
    },
    iconWrap: {
      width: 26,
      height: 26,
      borderRadius: 13,
      alignItems: "center",
      justifyContent: "center",
      marginRight: 8,
    },
    kind: {
      ...TypeScale.caption,
      fontWeight: "700",
      textTransform: "uppercase",
      letterSpacing: 0.45,
      flexShrink: 1,
    },
    unreadDot: {
      width: 7,
      height: 7,
      borderRadius: 4,
      marginLeft: "auto",
      backgroundColor: chrome.accent,
    },
    title: {
      ...TypeScale.body,
      color: chrome.text,
      fontWeight: "700",
    },
    summary: {
      ...TypeScale.compact,
      color: chrome.text,
      marginTop: 4,
      lineHeight: 20,
    },
    lifecycleRow: {
      marginTop: 8,
      flexDirection: "row",
      alignItems: "center",
      flexWrap: "wrap",
      gap: 6,
    },
    lifecycleBadge: {
      minHeight: 22,
      paddingHorizontal: 8,
      borderRadius: 11,
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      backgroundColor: chrome.surfaceActive,
    },
    lifecycleText: {
      ...TypeScale.caption,
      color: chrome.textMuted,
      fontWeight: "600",
    },
    sessionLifecycleText: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
    },
    supersededText: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
      fontStyle: "italic",
    },
    footer: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 9,
    },
    source: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
      flexShrink: 1,
    },
    time: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
      marginLeft: "auto",
    },
  });
}

type ResultPresentationBase = ReturnType<typeof resultPresentation>;

function resolvePresentationColors(
  presentation: ResultPresentationBase,
  chrome: TerminalThemeChrome,
) {
  const color = chrome[presentation.colorKey];
  return {
    ...presentation,
    color,
    background:
      presentation.colorKey === "danger"
        ? chrome.dangerSoft
        : presentation.colorKey === "accent"
          ? chrome.accentSoft
          : chrome.surfaceActive,
  };
}
