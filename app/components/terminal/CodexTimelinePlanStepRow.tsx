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

interface ZenPlanStepRowProps {
  step: CodexPlanStep;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}

export function ZenPlanStepRow({
  step,
  chrome,
  theme,
}: ZenPlanStepRowProps) {
  const completed = step.status === "completed";
  const inProgress = step.status === "in_progress";
  const marker = completed ? "\u2714" : "\u25a1";
  const color = completed
    ? chrome.textSubtle
    : inProgress
      ? theme.cyan
      : chrome.textMuted;

  return (
    <View style={styles.planStepRow}>
      <Text style={[styles.planMarker, { color }]}>{marker}</Text>
      <Text
        style={[
          styles.planStepText,
          completed ? styles.planStepCompleted : null,
          inProgress ? styles.planStepActive : null,
          { color },
        ]}
      >
        {step.step}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  planStepRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  planMarker: {
    width: 14,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  planStepText: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  planStepActive: {
    fontFamily: Typography.uiFontMedium,
  },
  planStepCompleted: {
    textDecorationLine: "line-through",
  },
});
