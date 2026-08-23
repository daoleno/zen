import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventCardModel } from "./brainWorkEventCardModel";
import {
  BRAIN_WORK_CARD_FACT_LINES,
  BRAIN_WORK_CARD_GAP,
  BRAIN_WORK_CARD_HORIZONTAL_PADDING,
  BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
  BRAIN_WORK_CARD_SUMMARY_LINES,
  BRAIN_WORK_CARD_TITLE_LINES,
} from "./brainWorkEventCardLayout";
import {
  brainWorkEventAccessibilityLabel,
  brainCurrentWorkLifecycle,
  brainWorkEventLifecycle,
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
  const card = brainWorkEventCardModel(item.event);
  const time = formatChatBubbleTime(item.event.occurred_at);
  const accessibilityTime = new Date(item.event.occurred_at).toLocaleString();
  const accessibilityLabel = brainWorkEventAccessibilityLabel({
    event: item.event,
    statusLabel: presentation.label,
    occurredAtLabel: accessibilityTime,
    description: [card.summary, ...card.facts].filter(
      (value): value is string => Boolean(value),
    ),
  });

  if (card.density === "minimal") {
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
        <Text
          numberOfLines={1}
          style={[styles.compactStatus, { color: presentation.color }]}
        >
          {presentation.label}
        </Text>
        <Text
          numberOfLines={BRAIN_WORK_CARD_TITLE_LINES}
          style={styles.compactTitle}
        >
          {workTitle}
        </Text>
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
        <Text numberOfLines={BRAIN_WORK_CARD_TITLE_LINES} style={styles.title}>
          {workTitle}
        </Text>
      </View>
      {card.summary ? (
        <Text
          numberOfLines={BRAIN_WORK_CARD_SUMMARY_LINES}
          style={styles.summary}
        >
          {card.summary}
        </Text>
      ) : null}
      {card.facts.length > 0 ? (
        <View style={styles.facts}>
          {card.facts.map((fact) => (
            <View key={fact} style={styles.factRow}>
              <View accessibilityElementsHidden style={styles.factDot} />
              <Text
                numberOfLines={BRAIN_WORK_CARD_FACT_LINES}
                style={styles.fact}
              >
                {fact}
              </Text>
            </View>
          ))}
        </View>
      ) : null}

      <View style={styles.footer}>
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
      paddingHorizontal: BRAIN_WORK_CARD_HORIZONTAL_PADDING,
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
      gap: BRAIN_WORK_CARD_GAP,
    },
    compactTitle: {
      ...TypeScale.compact,
      color: chrome.text,
      flex: 1,
      minWidth: BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
      fontWeight: "600",
    },
    compactStatus: {
      ...TypeScale.caption,
      fontWeight: "700",
      flexShrink: 0,
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
      gap: BRAIN_WORK_CARD_GAP,
    },
    summary: {
      ...TypeScale.compact,
      color: chrome.text,
      marginTop: 4,
      lineHeight: 20,
    },
    facts: {
      gap: 4,
      marginTop: 8,
    },
    factRow: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: BRAIN_WORK_CARD_GAP,
    },
    factDot: {
      width: 4,
      height: 4,
      borderRadius: 2,
      marginTop: 7,
      backgroundColor: chrome.textSubtle,
    },
    fact: {
      ...TypeScale.caption,
      color: chrome.textMuted,
      flex: 1,
      minWidth: 0,
    },
    footer: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 11,
      minHeight: 18,
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
