import React from "react";
import { StyleSheet, Text } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { tokenizeInlineMessage } from "./CodexMessageBodyModel";

interface CodexInlineMessageProps {
  text: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
}

export function CodexInlineMessage({
  text,
  chrome,
  theme,
  compact = false,
}: CodexInlineMessageProps) {
  return (
    <>
      {tokenizeInlineMessage(text).map((part, index) => {
        if (part.kind === "bold") {
          return (
            <Text key={index} style={[styles.messageBold, { color: chrome.text }]}>
              {part.text}
            </Text>
          );
        }
        if (part.kind === "code") {
          return (
            <Text
              key={index}
              style={[
                styles.messageInlineCode,
                compact ? styles.messageInlineCodeCompact : null,
                { color: theme.cyan, backgroundColor: chrome.surfaceMuted },
              ]}
            >
              {part.text}
            </Text>
          );
        }
        if (part.kind === "link") {
          return (
            <Text key={index} style={[styles.messageLink, { color: chrome.accent }]}>
              {part.text}
            </Text>
          );
        }
        return part.text;
      })}
    </>
  );
}

const styles = StyleSheet.create({
  messageBold: {
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageInlineCode: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 13,
    lineHeight: 20,
    letterSpacing: 0,
  },
  messageInlineCodeCompact: {
    fontSize: 12,
    lineHeight: 18,
  },
  messageLink: {
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
});
