import React, {
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  parseMessageBlocks,
  tokenizeInlineMessage,
} from "./CodexMessageBodyModel";
import { CodexNativeMarkdownBody } from "./CodexNativeMarkdownBody";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";

export function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const blocks = useMemo(() => parseMessageBlocks(value), [value]);
  if (blocks.length === 0) {
    return null;
  }
  return (
    <View style={styles.messageBody}>
      {blocks.map((block, index) => {
        const isLast = index === blocks.length - 1;
        switch (block.type) {
          case "heading":
            return (
              <Text
                key={index}
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
              <View
                key={index}
                style={[styles.messageList, isLast ? styles.messageBlockLast : null]}
              >
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
                key={index}
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
                key={index}
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
                key={index}
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
      })}
    </View>
  );
}

export function StreamingMessageBody({
  value,
  chrome,
  theme,
  stream,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  const [visibleChars, setVisibleChars] = useState(stream ? 0 : value.length);

  useEffect(() => {
    if (!stream) {
      setVisibleChars(value.length);
      return;
    }
    setVisibleChars((current) => Math.min(current, value.length));
  }, [stream, value.length]);

  useEffect(() => {
    if (!stream || visibleChars >= value.length) {
      return;
    }
    const timer = setTimeout(() => {
      setVisibleChars((current) => Math.min(value.length, current + 18));
    }, 24);
    return () => clearTimeout(timer);
  }, [stream, value.length, visibleChars]);

  const renderedValue = stream ? value.slice(0, visibleChars) : value;
  return (
    <View style={styles.zenAssistantContent}>
      <CodexNativeMarkdownBody
        value={renderedValue}
        chrome={chrome}
        theme={theme}
        streaming={stream && visibleChars < value.length}
        renderFallback={(fallbackValue) => (
          <MessageBody value={fallbackValue} chrome={chrome} theme={theme} />
        )}
      />
      {stream && visibleChars < value.length ? (
        <View style={[styles.zenStreamCursor, { backgroundColor: chrome.accent }]} />
      ) : null}
    </View>
  );
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
  zenAssistantContent: {
    minWidth: 0,
  },
  zenStreamCursor: {
    width: 6,
    height: 16,
    borderRadius: 3,
    opacity: 0.65,
  },
  messageBody: {
    minWidth: 0,
  },
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
