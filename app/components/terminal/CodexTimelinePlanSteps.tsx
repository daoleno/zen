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
import type { CodexPlanStep } from "../../services/codexConversation";
import { ZenPlanStepRow } from "./CodexTimelinePlanStepRow";

export function ZenPlanSteps({
  steps,
  chrome,
  theme,
}: {
  steps: CodexPlanStep[];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  if (steps.length === 0) {
    return (
      <Text style={[styles.planEmpty, { color: chrome.textSubtle }]}>
        (no steps provided)
      </Text>
    );
  }

  return (
    <View style={styles.planSteps}>
      {steps.map((step, index) => (
        <ZenPlanStepRow
          key={`${index}:${step.step}`}
          step={step}
          chrome={chrome}
          theme={theme}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  planSteps: {
    gap: 6,
  },
  planEmpty: {
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
});
