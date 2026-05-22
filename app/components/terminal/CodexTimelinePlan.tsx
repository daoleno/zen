import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { ZenPlanHeader } from "./CodexTimelinePlanHeader";
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
      <ZenPlanHeader accentColor={theme.cyan} chrome={chrome} />
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
