import React, { useContext } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { isLightTerminalTheme } from "../../constants/terminalThemes";
import type { MessageBlock } from "./CodexMessageBodyModel";
import { CodexFallbackInlineMessage } from "./CodexFallbackInlineMessage";
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
          <CodexFallbackInlineMessage
            text={block.text}
            chrome={chrome}
            theme={theme}
          />
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
                <CodexFallbackInlineMessage
                  text={item}
                  chrome={chrome}
                  theme={theme}
                />
              </Text>
            </View>
          ))}
        </View>
      );
    case "code":
      const codeBlockDark = !compact && !isLightTerminalTheme(theme);
      return (
        <Text
          selectable={textSelectable}
          style={[
            styles.messageCodeBlock,
            {
              color: codeBlockDark ? theme.brightWhite : chrome.text,
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
            <CodexFallbackInlineMessage
              text={block.text}
              chrome={chrome}
              theme={theme}
            />
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
          <CodexFallbackInlineMessage
            text={block.text}
            chrome={chrome}
            theme={theme}
          />
        </Text>
      );
  }
}

const styles = StyleSheet.create({
  messageText: {
    marginBottom: 9,
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
  },
  messageHeading: {
    marginBottom: 8,
    fontSize: 15,
    lineHeight: 22,
    fontFamily: Typography.chatFontMedium,
  },
  messageHeadingLarge: {
    fontSize: 16,
    lineHeight: 24,
  },
  messageList: {
    marginBottom: 9,
    gap: 5,
  },
  messageListItem: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
  },
  messageBullet: {
    width: 9,
    fontSize: 13,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
  },
  messageCodeBlock: {
    marginTop: 2,
    marginBottom: 10,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.chatMonoFont,
  },
  messageQuote: {
    marginBottom: 9,
    borderLeftWidth: 2,
    paddingLeft: 10,
  },
  messageQuoteText: {
    fontSize: 14,
    lineHeight: 22,
    fontFamily: Typography.chatFont,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
});
