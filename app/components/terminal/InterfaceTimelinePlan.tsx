import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { InterfaceTimelineExpandedBlock } from "./InterfaceTimelineExpandedBlock";
import { InterfaceTimelinePlanExplanation } from "./InterfaceTimelinePlanExplanation";
import { ZenPlanHeader } from "./InterfaceTimelinePlanHeader";
import { ZenPlanSteps } from "./InterfaceTimelinePlanSteps";
import type { ZenPlanTimelineItem } from "./InterfaceTimelinePlanTypes";

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
      <InterfaceTimelineExpandedBlock chrome={chrome} style={styles.planBlock}>
        <InterfaceTimelinePlanExplanation
          chrome={chrome}
          explanation={item.explanation}
        />
        <ZenPlanSteps steps={item.steps} chrome={chrome} theme={theme} />
      </InterfaceTimelineExpandedBlock>
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
