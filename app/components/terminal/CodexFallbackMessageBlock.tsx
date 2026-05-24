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
  messageBlockLast: {
    marginBottom: 0,
  },
});
