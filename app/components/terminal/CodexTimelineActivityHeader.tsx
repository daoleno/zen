import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { TimelineActivityIconName } from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityHeaderProps {
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  detail?: string;
  canExpand: boolean;
  expanded: boolean;
  toneColor: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

export function CodexTimelineActivityHeader({
  title,
  tone,
  icon,
  detail,
  canExpand,
  expanded,
  toneColor,
  chrome,
  onPress,
}: CodexTimelineActivityHeaderProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={title}
      style={styles.row}
      onPress={onPress}
      disabled={!canExpand}
      activeOpacity={0.76}
    >
      {tone === "running" ? (
        <ActivityIndicator size="small" color={toneColor} />
      ) : (
        <Ionicons name={icon} size={13} color={toneColor} />
      )}
      <Text style={[styles.title, { color: chrome.textSubtle }]} numberOfLines={1}>
        {title}
      </Text>
      {detail ? (
        <Text
          style={[styles.detail, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
          {detail}
        </Text>
      ) : null}
      {canExpand ? (
        <Ionicons
          name={expanded ? "chevron-up" : "chevron-down"}
          size={12}
          color={chrome.textSubtle}
        />
      ) : null}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    alignSelf: "flex-start",
    minHeight: 24,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    opacity: 0.78,
  },
  title: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  detail: {
    flexShrink: 1,
    maxWidth: 210,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
