import React from "react";
import {
  StyleSheet,
  Text,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface CodexTimelinePlanExplanationProps {
  chrome: TerminalThemeChrome;
  explanation?: string;
}

export function CodexTimelinePlanExplanation({
  chrome,
  explanation,
}: CodexTimelinePlanExplanationProps) {
  const text = explanation?.trim();
  if (!text) {
    return null;
  }

  return (
    <Text style={[styles.planExplanation, { color: chrome.textSubtle }]}>
      {text}
    </Text>
  );
}

const styles = StyleSheet.create({
  planExplanation: {
    marginBottom: 7,
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
});
