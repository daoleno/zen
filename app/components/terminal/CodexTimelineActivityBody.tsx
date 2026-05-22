import React from "react";
import {
  StyleSheet,
  Text,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface CodexTimelineActivityBodyProps {
  body: string;
  chrome: TerminalThemeChrome;
  textSelectable: boolean;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityBody({
  body,
  chrome,
  textSelectable,
  truncateBody,
}: CodexTimelineActivityBodyProps) {
  return (
    <Text
      selectable={textSelectable}
      style={[styles.body, { color: chrome.textSubtle }]}
    >
      {truncateBody(body, 1800)}
    </Text>
  );
}

const styles = StyleSheet.create({
  body: {
    marginTop: 6,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
  },
});
