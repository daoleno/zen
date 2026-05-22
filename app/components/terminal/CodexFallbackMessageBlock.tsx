import React, { useContext } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { MessageBlock } from "./CodexMessageBodyModel";
import { tokenizeInlineMessage } from "./CodexMessageBodyModel";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";

interface CodexFallbackMessageBlockProps {
  block: MessageBlock;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact: boolean;
  isLast: boolean;
}

export function CodexFallbackMessageBlock({
  block,
  chrome,
  theme,
  compact,
  isLast,
}: CodexFallbackMessageBlockProps) {
  const textSelectable = useContext(TimelineTextSelectableContext);

  switch (block.type) {
    case "heading":
      return (
        <Text
          selectable={textSelectable}
          style={[
            styles.messageHeading,
            block.level <= 2 ? styles.messageHeadingLarge : null,
            { color: chrome.text },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          {renderInlineMessage(block.text, chrome, theme)}
        </Text>
      );
    case "list":
      return (
        <View style={[styles.messageList, isLast ? styles.messageBlockLast : null]}>
          {block.items.map((item, itemIndex) => (
            <View key={itemIndex} style={styles.messageListItem}>
              <Text
                selectable={textSelectable}
                style={[styles.messageBullet, { color: chrome.textSubtle }]}
              >
                {"\u2022"}
              </Text>
              <Text
                selectable={textSelectable}
                style={[
                  styles.messageText,
                  styles.messageBlockLast,
                  { color: chrome.text },
                ]}
              >
                {renderInlineMessage(item, chrome, theme)}
              </Text>
            </View>
          ))}
        </View>
      );
    case "code":
      return (
        <Text
          selectable={textSelectable}
          style={[
            styles.messageCodeBlock,
            {
              color: chrome.text,
              backgroundColor: compact ? chrome.surface : theme.black,
              borderColor: chrome.border,
            },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          {block.text}
        </Text>
      );
    case "quote":
      return (
        <View
          style={[
            styles.messageQuote,
            { borderLeftColor: chrome.borderStrong },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <Text
            selectable={textSelectable}
            style={[styles.messageQuoteText, { color: chrome.textMuted }]}
          >
            {renderInlineMessage(block.text, chrome, theme)}
          </Text>
        </View>
      );
    case "paragraph":
    default:
      return (
        <Text
          selectable={textSelectable}
          style={[
            styles.messageText,
            { color: chrome.text },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          {renderInlineMessage(block.text, chrome, theme)}
        </Text>
      );
  }
}

function renderInlineMessage(
  text: string,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  return tokenizeInlineMessage(text).map((part, index) => {
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
            { color: theme.green, backgroundColor: chrome.surfaceMuted },
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
  });
}

const styles = StyleSheet.create({
  messageText: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageHeading: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  messageHeadingLarge: {
    fontSize: 15,
    lineHeight: 20,
  },
  messageList: {
    marginBottom: 8,
    gap: 4,
  },
  messageListItem: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  messageBullet: {
    width: 9,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageCodeBlock: {
    marginTop: 2,
    marginBottom: 9,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingHorizontal: 10,
    paddingVertical: 8,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  messageQuote: {
    marginBottom: 8,
    borderLeftWidth: 2,
    paddingLeft: 9,
  },
  messageQuoteText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  messageBold: {
    fontFamily: Typography.uiFontMedium,
  },
  messageInlineCode: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 18,
  },
  messageLink: {
    fontFamily: Typography.uiFontMedium,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
});
