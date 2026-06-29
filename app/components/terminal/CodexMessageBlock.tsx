import React from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  MessageBlock,
  MessageListItem,
  MessageTableAlignment,
} from "./CodexMessageBodyModel";
import { CodexInlineMessage } from "./CodexInlineMessage";
import { CodexMessageCodeBlock } from "./CodexMessageCodeBlock";
import { useTimelineSelectableTextProps } from "./TimelineTextSelectableContext";

interface CodexMessageBlockProps {
  block: MessageBlock;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact: boolean;
  dense?: boolean;
  isLast: boolean;
}

export function CodexMessageBlock({
  block,
  chrome,
  theme,
  compact,
  dense = false,
  isLast,
}: CodexMessageBlockProps) {
  const selectableTextProps = useTimelineSelectableTextProps();

  switch (block.type) {
    case "heading":
      return (
        <View style={[styles.messageHeadingWrap, isLast ? styles.messageBlockLast : null]}>
          <Text
            {...selectableTextProps}
            style={[
              styles.messageHeading,
              block.level <= 2 ? styles.messageHeadingLarge : null,
              { color: chrome.text },
            ]}
          >
            <CodexInlineMessage
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
            <View
              key={itemIndex}
              style={[
                styles.messageListItem,
                item.depth > 0 ? { paddingLeft: listItemIndent(item.depth) } : null,
              ]}
            >
              <View
                style={[
                  styles.messageListMarkerWrap,
                  item.ordered ? styles.messageListMarkerWrapOrdered : null,
                ]}
              >
                {item.taskState ? (
                  <View
                    style={[
                      styles.messageTaskCheckbox,
                      {
                        borderColor: chrome.textSubtle,
                        backgroundColor:
                          item.taskState === "checked" ? theme.green : "transparent",
                      },
                    ]}
                  >
                    {item.taskState === "checked" ? (
                      <View
                        style={[
                          styles.messageTaskCheckboxDot,
                          {
                            backgroundColor:
                              theme.background === "transparent"
                                ? theme.cursorAccent
                                : theme.background,
                          },
                        ]}
                      />
                    ) : null}
                  </View>
                ) : (
                  <Text
                    {...selectableTextProps}
                    style={[styles.messageListMarker, { color: chrome.textSubtle }]}
                  >
                    {listMarkerText(item)}
                  </Text>
                )}
              </View>
              <Text
                {...selectableTextProps}
                style={[
                  styles.messageText,
                  compact ? styles.messageTextCompact : null,
                  dense ? styles.messageTextDense : null,
                  styles.messageListText,
                  { color: chrome.text },
                ]}
              >
                <CodexInlineMessage
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
    case "table":
      return (
        <View style={[styles.messageTableBlock, isLast ? styles.messageBlockLast : null]}>
          <ScrollView
            horizontal
            nestedScrollEnabled
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={styles.messageTableScrollContent}
          >
            <View
              style={[
                styles.messageTable,
                compact ? styles.messageTableCompact : null,
                { backgroundColor: chrome.surface, borderColor: chrome.border },
              ]}
            >
              <View
                style={[
                  styles.messageTableRow,
                  styles.messageTableHeaderRow,
                  {
                    backgroundColor: chrome.surfaceMuted,
                    borderBottomColor: chrome.border,
                  },
                ]}
              >
                {block.headers.map((cell, columnIndex) => (
                  <View
                    key={`header:${columnIndex}`}
                    style={[
                      styles.messageTableCell,
                      compact ? styles.messageTableCellCompact : null,
                      columnIndex === block.headers.length - 1
                        ? styles.messageTableCellLast
                        : null,
                      { borderRightColor: chrome.border },
                    ]}
                  >
                    <Text
                      {...selectableTextProps}
                      style={[
                        styles.messageTableHeaderText,
                        compact ? styles.messageTableTextCompact : null,
                        {
                          color: chrome.text,
                          textAlign: tableCellTextAlign(block.alignments[columnIndex]),
                        },
                      ]}
                    >
                      <CodexInlineMessage
                        text={cell || " "}
                        chrome={chrome}
                        theme={theme}
                        compact={compact}
                      />
                    </Text>
                  </View>
                ))}
              </View>
              {block.rows.map((row, rowIndex) => (
                <View
                  key={`row:${rowIndex}`}
                  style={[
                    styles.messageTableRow,
                    rowIndex < block.rows.length - 1 ? styles.messageTableBodyRow : null,
                    {
                      backgroundColor:
                        rowIndex % 2 === 0 ? "transparent" : chrome.surfaceMuted,
                      borderBottomColor: chrome.border,
                    },
                  ]}
                >
                  {row.map((cell, columnIndex) => (
                    <View
                      key={`cell:${rowIndex}:${columnIndex}`}
                      style={[
                        styles.messageTableCell,
                        compact ? styles.messageTableCellCompact : null,
                        columnIndex === row.length - 1 ? styles.messageTableCellLast : null,
                        { borderRightColor: chrome.border },
                      ]}
                    >
                      <Text
                        {...selectableTextProps}
                        style={[
                          styles.messageTableText,
                          compact ? styles.messageTableTextCompact : null,
                          {
                            color: chrome.text,
                            textAlign: tableCellTextAlign(block.alignments[columnIndex]),
                          },
                        ]}
                      >
                        <CodexInlineMessage
                          text={cell || " "}
                          chrome={chrome}
                          theme={theme}
                          compact={compact}
                        />
                      </Text>
                    </View>
                  ))}
                </View>
              ))}
            </View>
          </ScrollView>
        </View>
      );
    case "code":
      return (
        <CodexMessageCodeBlock
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
            {...selectableTextProps}
            style={[
              styles.messageQuoteText,
              compact ? styles.messageQuoteTextCompact : null,
              { color: chrome.textMuted },
            ]}
          >
            <CodexInlineMessage
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
          {...selectableTextProps}
          style={[
            styles.messageText,
            compact ? styles.messageTextCompact : null,
            dense ? styles.messageTextDense : null,
            { color: chrome.text },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <CodexInlineMessage
            text={block.text}
            chrome={chrome}
            theme={theme}
            compact={compact}
          />
        </Text>
      );
  }
}

function listItemIndent(depth: number) {
  return Math.min(depth, 6) * 16;
}

function listMarkerText(item: MessageListItem) {
  if (item.ordered) {
    return item.marker;
  }
  switch (item.depth % 3) {
    case 1:
      return "\u25E6";
    case 2:
      return "\u25AA";
    default:
      return item.marker;
  }
}

function tableCellTextAlign(alignment: MessageTableAlignment | undefined) {
  switch (alignment) {
    case "center":
      return "center";
    case "right":
      return "right";
    case "left":
    default:
      return "left";
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
  messageTextDense: {
    marginBottom: 6,
    fontSize: 15,
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
  messageListMarkerWrap: {
    width: 22,
    paddingRight: 7,
    alignItems: "flex-end",
  },
  messageListMarkerWrapOrdered: {
    width: 32,
  },
  messageListText: {
    flex: 1,
    minWidth: 0,
    marginBottom: 0,
  },
  messageListMarker: {
    textAlign: "right",
    fontSize: 13,
    lineHeight: 24,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageTaskCheckbox: {
    width: 14,
    height: 14,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 4,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 5,
  },
  messageTaskCheckboxDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  messageTableBlock: {
    marginBottom: 10,
  },
  messageTableScrollContent: {
    paddingRight: 8,
  },
  messageTable: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    overflow: "hidden",
  },
  messageTableCompact: {
    borderRadius: 7,
  },
  messageTableRow: {
    flexDirection: "row",
    minWidth: 0,
  },
  messageTableHeaderRow: {
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  messageTableBodyRow: {
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  messageTableCell: {
    minWidth: 96,
    maxWidth: 220,
    borderRightWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 7,
  },
  messageTableCellCompact: {
    minWidth: 88,
    maxWidth: 190,
    paddingHorizontal: 8,
    paddingVertical: 6,
  },
  messageTableCellLast: {
    borderRightWidth: 0,
  },
  messageTableText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFont,
    letterSpacing: 0,
  },
  messageTableHeaderText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageTableTextCompact: {
    fontSize: 12,
    lineHeight: 18,
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
