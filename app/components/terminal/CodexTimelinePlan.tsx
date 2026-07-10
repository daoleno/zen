import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexTimelineExpandedBlock } from "./CodexTimelineExpandedBlock";
import { CodexTimelinePlanExplanation } from "./CodexTimelinePlanExplanation";
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
      <CodexTimelineExpandedBlock chrome={chrome} style={styles.planBlock}>
        <CodexTimelinePlanExplanation
          chrome={chrome}
          explanation={item.explanation}
        />
        <ZenPlanSteps steps={item.steps} chrome={chrome} theme={theme} />
      </CodexTimelineExpandedBlock>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 8,
    paddingLeft: 1,
  },
  planBlock: {
    paddingVertical: 1,
  },
});
