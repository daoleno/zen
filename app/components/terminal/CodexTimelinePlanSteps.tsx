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

function ZenPlanStepRow({
  step,
  chrome,
  theme,
}: {
  step: CodexPlanStep;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
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
  planSteps: {
    gap: 6,
  },
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
  planEmpty: {
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
});
