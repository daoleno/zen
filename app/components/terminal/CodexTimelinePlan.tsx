import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { ZenPlanSteps } from "./CodexTimelinePlanSteps";
import type { ZenPlanTimelineItem } from "./CodexTimelinePlanTypes";

export function ZenPlanUpdate({
  item,
  chrome,
  theme,
}: {
  item: ZenPlanTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  return (
    <View style={styles.wrap}>
      <View style={styles.row}>
        <Ionicons name="checkbox-outline" size={13} color={theme.cyan} />
        <Text
          style={[styles.title, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
          Updated Plan
        </Text>
      </View>
      <View
        style={[
          styles.expanded,
          styles.planBlock,
          { borderColor: chrome.border },
        ]}
      >
        {item.explanation?.trim() ? (
          <Text style={[styles.planExplanation, { color: chrome.textSubtle }]}>
            {item.explanation.trim()}
          </Text>
        ) : null}
        <ZenPlanSteps steps={item.steps} chrome={chrome} theme={theme} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 10,
    paddingLeft: 1,
  },
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
  expanded: {
    marginTop: 6,
    marginLeft: 19,
    maxWidth: "92%",
    borderLeftWidth: StyleSheet.hairlineWidth,
    paddingLeft: 10,
    paddingVertical: 4,
  },
  planBlock: {
    paddingVertical: 2,
  },
  planExplanation: {
    marginBottom: 7,
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
});
