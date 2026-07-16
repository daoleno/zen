import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, Typography } from "../../constants/tokens";
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
  accessibilityLabel?: string;
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
  accessibilityLabel,
  onPress,
}: CodexTimelineActivityHeaderProps) {
  const labelParts = [accessibilityLabel || title];
  if (detail) {
    labelParts.push(detail);
  }
  if (tone === "failed") {
    labelParts.push("failed");
  } else if (tone === "running") {
    labelParts.push("in progress");
  }
  if (canExpand) {
    labelParts.push(expanded ? "expanded" : "collapsed");
  }

  return (
    <TouchableOpacity
      accessibilityLabel={labelParts.join(", ")}
      accessibilityRole="button"
      accessibilityState={{ disabled: !canExpand, expanded: canExpand ? expanded : undefined }}
      style={styles.row}
      onPress={onPress}
      disabled={!canExpand}
      activeOpacity={0.76}
      hitSlop={{ top: 8, bottom: 8, left: 4, right: 4 }}
    >
      <CodexTimelineActivityToneIcon
        tone={tone}
        icon={icon}
        activityKind={activityKind}
        color={toneColor}
      />
      <View style={styles.copy}>
        <Text
          style={[styles.title, { color: chrome.textMuted }]}
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
    minHeight: 28,
    width: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingVertical: 2,
  },
  copy: {
    flex: 1,
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
  },
  title: {
    ...TypeScale.caption,
    flexShrink: 0,
    fontFamily: Typography.uiFontMedium,
  },
  detail: {
    ...TypeScale.caption,
    flex: 1,
    flexShrink: 1,
    minWidth: 0,
    marginLeft: 6,
    fontFamily: Typography.terminalFont,
  },
});
