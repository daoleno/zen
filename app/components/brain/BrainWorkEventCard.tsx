import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  brainWorkEventAccessibilityLabel,
  brainCurrentWorkLifecycle,
  brainWorkAgentCountLabel,
  brainWorkEventLifecycle,
  brainWorkEventSummary,
  brainWorkEventWorkTitle,
} from "./brainWorkEventPresentation";

export type BrainWorkEventTimelineItem = {
  type: "brain-work-event";
  id: string;
  timestamp: string;
  event: BrainWorkResultEvent;
  events: BrainWorkResultEvent[];
  sourceCount?: number;
  currentWork?: import("../../store/brain").BrainCurrentWork;
  onPress?: () => void;
};

export function BrainWorkEventCard({
  item,
  chrome,
  attentionColor,
  attentionBackground,
}: {
  item: BrainWorkEventTimelineItem;
  chrome: TerminalThemeChrome;
  attentionColor: string;
  attentionBackground: string;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const presentation = resolvePresentationColors(
    item.currentWork
      ? brainCurrentWorkLifecycle(item.currentWork, item.event)
      : brainWorkEventLifecycle(item.event),
    chrome,
    attentionColor,
    attentionBackground,
  );
  const workTitle = brainWorkEventWorkTitle(item.event);
  const summary = brainWorkEventSummary(item.event);
  const time = formatChatBubbleTime(item.event.occurred_at);
  const accessibilityTime = new Date(item.event.occurred_at).toLocaleString();
  const accessibilityLabel = brainWorkEventAccessibilityLabel({
    event: item.event,
    statusLabel: presentation.label,
    occurredAtLabel: accessibilityTime,
  });
  const compactStatus =
    presentation.lifecycle === "failed" ||
    presentation.lifecycle === "cancelled"
      ? presentation.label
      : undefined;

  if (presentation.terminal) {
    return (
      <Pressable
        accessibilityRole={item.onPress ? "button" : undefined}
        accessibilityLabel={accessibilityLabel}
        disabled={!item.onPress}
        onPress={item.onPress}
        style={({ pressed }) => [
          styles.wrap,
          styles.wrapCompact,
          item.event.unread ? styles.wrapUnread : null,
          pressed ? styles.wrapPressed : null,
        ]}
      >
        <Ionicons
          name={presentation.icon}
          size={17}
          color={presentation.color}
        />
        <Text numberOfLines={1} style={styles.compactTitle}>
          {workTitle}
        </Text>
        {compactStatus ? (
          <Text
            style={[styles.compactStatus, { color: presentation.color }]}
          >
            {compactStatus}
          </Text>
        ) : null}
        {item.sourceCount ? (
          <Text style={styles.compactMeta}>
            {brainWorkAgentCountLabel(item.sourceCount)}
          </Text>
        ) : null}
        <Text style={styles.compactMeta}>{time}</Text>
        {item.onPress ? (
          <Ionicons
            name="chevron-forward"
            size={14}
            color={chrome.textSubtle}
          />
        ) : null}
      </Pressable>
    );
  }

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
      <View style={[styles.header, styles.headerActive]}>
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

      <View style={styles.titleRow}>
        <Text numberOfLines={2} style={styles.title}>
          {workTitle}
        </Text>
      </View>
      {summary ? (
        <Text numberOfLines={3} style={styles.summary}>
          {summary}
        </Text>
      ) : null}

      <View style={styles.footer}>
        {item.sourceCount ? (
          <Text numberOfLines={1} style={styles.source}>
            {brainWorkAgentCountLabel(item.sourceCount)}
          </Text>
        ) : null}
        <Text style={styles.time}>{time}</Text>
        {item.onPress ? (
          <Ionicons
            name="chevron-forward"
            size={14}
            color={chrome.textSubtle}
          />
        ) : null}
      </View>
    </Pressable>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 1,
      marginBottom: 8,
      paddingHorizontal: 14,
      paddingVertical: 12,
      borderRadius: 8,
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
    wrapCompact: {
      minHeight: 44,
      paddingVertical: 9,
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
    },
    compactTitle: {
      ...TypeScale.compact,
      color: chrome.text,
      flex: 1,
      minWidth: 0,
      fontWeight: "600",
    },
    compactStatus: {
      ...TypeScale.caption,
      fontWeight: "700",
    },
    compactMeta: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      marginBottom: 5,
    },
    headerActive: {
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
      letterSpacing: 0,
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
      flex: 1,
      minWidth: 0,
    },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
    },
    summary: {
      ...TypeScale.compact,
      color: chrome.text,
      marginTop: 4,
      lineHeight: 20,
    },
    footer: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 11,
      minHeight: 18,
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

function resolvePresentationColors(
  presentation: ReturnType<typeof brainWorkEventLifecycle>,
  chrome: TerminalThemeChrome,
  attentionColor: string,
  attentionBackground: string,
) {
  const color =
    presentation.tone === "danger"
      ? chrome.danger
      : presentation.tone === "attention"
        ? attentionColor
        : presentation.tone === "accent"
          ? chrome.accent
          : chrome.textMuted;
  return {
    ...presentation,
    color,
    background:
      presentation.tone === "danger"
        ? chrome.dangerSoft
        : presentation.tone === "attention"
          ? attentionBackground
          : presentation.tone === "accent"
            ? chrome.accentSoft
            : chrome.surfaceActive,
  };
}
