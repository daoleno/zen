import React, { useContext } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { MessageBlock } from "./CodexMessageBodyModel";
import { CodexFallbackCodeBlock } from "./CodexFallbackCodeBlock";
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
        <View style={[styles.messageHeadingWrap, isLast ? styles.messageBlockLast : null]}>
          <Text
            selectable={textSelectable}
            style={[
              styles.messageHeading,
              block.level <= 2 ? styles.messageHeadingLarge : null,
              { color: chrome.text },
            ]}
          >
            <CodexFallbackInlineMessage
              text={block.text}
              chrome={chrome}
              theme={theme}
              compact={compact}
            />
          </Text>
        </View>
      );
    case "list":
      return (
        <View style={[styles.messageList, isLast ? styles.messageBlockLast : null]}>
          {block.items.map((item, itemIndex) => (
            <View key={itemIndex} style={styles.messageListItem}>
              <Text
                selectable={textSelectable}
                style={[styles.messageListMarker, { color: chrome.textSubtle }]}
              >
                {item.marker}
              </Text>
              <Text
                selectable={textSelectable}
                style={[
                  styles.messageText,
                  compact ? styles.messageTextCompact : null,
                  styles.messageListText,
                  { color: chrome.text },
                ]}
              >
                <CodexFallbackInlineMessage
                  text={item.text}
                  chrome={chrome}
                  theme={theme}
                  compact={compact}
                />
              </Text>
            </View>
          ))}
        </View>
      );
    case "code":
      return (
        <CodexFallbackCodeBlock
          text={block.text}
          language={block.language}
          chrome={chrome}
          theme={theme}
          compact={compact}
          isLast={isLast}
        />
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
            style={[
              styles.messageQuoteText,
              compact ? styles.messageQuoteTextCompact : null,
              { color: chrome.textMuted },
            ]}
          >
            <CodexFallbackInlineMessage
              text={block.text}
              chrome={chrome}
              theme={theme}
              compact={compact}
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
            compact ? styles.messageTextCompact : null,
            { color: chrome.text },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <CodexFallbackInlineMessage
            text={block.text}
            chrome={chrome}
            theme={theme}
            compact={compact}
          />
        </Text>
      );
  }
}

const styles = StyleSheet.create({
  messageText: {
    marginBottom: 10,
    fontSize: 15,
    lineHeight: 24,
    fontFamily: Typography.chatFont,
    letterSpacing: 0,
  },
  messageTextCompact: {
    marginBottom: 8,
    fontSize: 14,
    lineHeight: 22,
  },
  messageHeadingWrap: {
    marginBottom: 8,
  },
  messageHeading: {
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageHeadingLarge: {
    fontSize: 16,
    lineHeight: 25,
  },
  messageList: {
    marginBottom: 10,
    gap: 6,
  },
  messageListItem: {
    flexDirection: "row",
    alignItems: "flex-start",
    minWidth: 0,
  },
  messageListText: {
    flex: 1,
    minWidth: 0,
    marginBottom: 0,
  },
  messageListMarker: {
    width: 22,
    paddingRight: 8,
    textAlign: "right",
    fontSize: 13,
    lineHeight: 24,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageQuote: {
    marginBottom: 10,
    borderLeftWidth: 2,
    paddingLeft: 10,
    paddingVertical: 1,
  },
  messageQuoteText: {
    fontSize: 14,
    lineHeight: 22,
    fontFamily: Typography.chatFont,
    letterSpacing: 0,
  },
  messageQuoteTextCompact: {
    fontSize: 13,
    lineHeight: 20,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
});
