import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { TimelineActivityIconName } from "./CodexTimelineActivityTypes";
import type { ZenActivityTimelineItem } from "./CodexTimelineActivityTypes";
import { CodexTimelineActivityExpandIcon } from "./CodexTimelineActivityExpandIcon";
import { CodexTimelineActivityToneIcon } from "./CodexTimelineActivityToneIcon";

interface CodexTimelineActivityHeaderProps {
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
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
  activityKind,
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
      accessibilityRole="button"
      accessibilityState={{ disabled: !canExpand, expanded: canExpand ? expanded : undefined }}
      style={styles.row}
      onPress={onPress}
      disabled={!canExpand}
      activeOpacity={0.76}
    >
      <CodexTimelineActivityToneIcon
        tone={tone}
        icon={icon}
        activityKind={activityKind}
        color={toneColor}
      />
      <View style={styles.copy}>
        <Text
          style={[styles.title, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
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
      </View>
      {canExpand ? (
        <CodexTimelineActivityExpandIcon expanded={expanded} chrome={chrome} />
      ) : null}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    alignSelf: "stretch",
    minHeight: 22,
    width: "100%",
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 5,
    opacity: 0.78,
  },
  copy: {
    flex: 1,
    minWidth: 0,
    paddingTop: 1.5,
    flexDirection: "row",
    alignItems: "center",
  },
  title: {
    flexShrink: 0,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  detail: {
    flex: 1,
    flexShrink: 1,
    minWidth: 0,
    marginLeft: 6,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
