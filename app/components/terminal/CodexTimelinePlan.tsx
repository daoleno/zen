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
import type { CodexPlanStep } from "../../services/codexConversation";

export interface ZenPlanTimelineItem {
  type: "plan";
  id: string;
  timestamp?: string;
  explanation?: string;
  steps: CodexPlanStep[];
}

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
        {item.steps.length > 0 ? (
          <View style={styles.planSteps}>
            {item.steps.map((step, index) => (
              <ZenPlanStepRow
                key={`${index}:${step.step}`}
                step={step}
                chrome={chrome}
                theme={theme}
              />
            ))}
          </View>
        ) : (
          <Text style={[styles.planEmpty, { color: chrome.textSubtle }]}>
            (no steps provided)
          </Text>
        )}
      </View>
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
