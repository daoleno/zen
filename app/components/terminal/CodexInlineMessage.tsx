import React, { useCallback } from "react";
import { Linking, StyleSheet, Text } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { openSafeMarkdownUrl } from "../markdown/markdownLinks";
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
  const handleLinkPress = useCallback((url: string) => {
    void openSafeMarkdownUrl(url, (safeUrl) => Linking.openURL(safeUrl));
  }, []);

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
        if (part.kind === "italic") {
          return (
            <Text key={index} style={[styles.messageItalic, { color: chrome.text }]}>
              {part.text}
            </Text>
          );
        }
        if (part.kind === "strike") {
          return (
            <Text
              key={index}
              style={[
                styles.messageStrike,
                { color: chrome.textMuted, textDecorationColor: chrome.textSubtle },
              ]}
            >
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
          const url = part.url;
          return (
            <Text
              accessibilityRole="link"
              key={index}
              onPress={url ? () => handleLinkPress(url) : undefined}
              style={[styles.messageLink, { color: chrome.link }]}
            >
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
  messageItalic: {
    fontFamily: Typography.chatFont,
    fontStyle: "italic",
    letterSpacing: 0,
  },
  messageStrike: {
    fontFamily: Typography.chatFont,
    textDecorationLine: "line-through",
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
    textDecorationLine: "underline",
    letterSpacing: 0,
  },
});
