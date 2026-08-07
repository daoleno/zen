import { StyleSheet } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { interfaceInlineCodeStyle } from "./InterfaceInlineCodeStyle";

export {
  INTERFACE_MARKDOWN_MOBILE_RENDERER,
  prepareInterfaceMarkdown,
} from "./InterfaceNativeMarkdownBodyPrepare";

type MarkdownStyle = Record<string, Record<string, unknown>>;

export function interfaceMarkdownStyle(
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
  compact: boolean,
): MarkdownStyle {
  const bodyFontSize = compact ? 14 : 15;
  const bodyLineHeight = compact ? 21 : 23;
  const blockMarginBottom = compact ? 7 : 9;
  const text = {
    color: chrome.text,
    fontFamily: Typography.chatFont,
    fontSize: bodyFontSize,
    lineHeight: bodyLineHeight,
    letterSpacing: 0,
    marginTop: 0,
    marginBottom: blockMarginBottom + 1,
  };
  const heading = {
    color: chrome.text,
    fontFamily: Typography.chatFontMedium,
    lineHeight: bodyLineHeight,
    letterSpacing: 0,
    marginTop: compact ? 0 : 1,
    marginBottom: compact ? 7 : 8,
  };
  return {
    paragraph: text,
    h1: {
      ...heading,
      fontSize: compact ? 15 : 17,
      lineHeight: compact ? 22 : 25,
    },
    h2: {
      ...heading,
      fontSize: compact ? 15 : 16,
      lineHeight: compact ? 22 : 24,
    },
    h3: { ...heading, fontSize: bodyFontSize, lineHeight: bodyLineHeight },
    h4: { ...heading, fontSize: bodyFontSize, lineHeight: bodyLineHeight },
    h5: {
      ...heading,
      fontSize: compact ? 13 : 14,
      lineHeight: compact ? 20 : 22,
    },
    h6: {
      ...heading,
      fontSize: compact ? 13 : 14,
      lineHeight: compact ? 20 : 22,
      color: chrome.textMuted,
    },
    strong: {
      color: chrome.text,
      fontFamily: Typography.chatFontMedium,
      fontWeight: "normal",
    },
    em: {
      color: chrome.text,
      fontFamily: Typography.chatFont,
      fontStyle: "italic",
    },
    link: {
      color: chrome.link,
      fontFamily: Typography.chatFontMedium,
      underline: true,
    },
    code: interfaceInlineCodeStyle(theme, compact),
    codeBlock: {
      color: chrome.text,
      backgroundColor: compact ? chrome.surfaceMuted : chrome.surface,
      borderColor: chrome.border,
      borderRadius: 8,
      borderWidth: StyleSheet.hairlineWidth,
      fontFamily: Typography.chatMonoFont,
      fontSize: compact ? 12 : 13,
      lineHeight: compact ? 18 : 20,
      letterSpacing: 0,
      marginTop: 2,
      marginBottom: compact ? 8 : 10,
      padding: compact ? 10 : 12,
    },
    blockquote: {
      color: chrome.textMuted,
      backgroundColor: "transparent",
      borderColor: chrome.borderStrong,
      borderWidth: 2,
      fontFamily: Typography.chatFont,
      fontSize: compact ? 13 : 14,
      gapWidth: 10,
      lineHeight: compact ? 20 : 22,
      letterSpacing: 0,
      marginTop: 0,
      marginBottom: blockMarginBottom,
    },
    list: {
      color: chrome.text,
      bulletColor: chrome.textSubtle,
      bulletSize: compact ? 5 : 6,
      markerColor: chrome.textSubtle,
      markerMinWidth: compact ? 20 : 22,
      markerFontWeight: "normal",
      fontFamily: Typography.chatFont,
      fontSize: bodyFontSize,
      gapWidth: compact ? 7 : 8,
      lineHeight: bodyLineHeight,
      letterSpacing: 0,
      marginLeft: compact ? 18 : 22,
      marginTop: 0,
      marginBottom: blockMarginBottom,
    },
    table: {
      color: chrome.text,
      borderColor: chrome.border,
      borderRadius: 8,
      borderWidth: StyleSheet.hairlineWidth,
      cellPaddingHorizontal: 9,
      cellPaddingVertical: 6,
      fontFamily: Typography.chatFont,
      fontSize: 13,
      headerBackgroundColor: chrome.surfaceMuted,
      headerFontFamily: Typography.chatFontMedium,
      headerTextColor: chrome.text,
      lineHeight: 19,
      marginTop: 2,
      marginBottom: compact ? 8 : 10,
      rowEvenBackgroundColor: chrome.surface,
      rowOddBackgroundColor: chrome.surfaceMuted,
    },
    taskList: {
      borderColor: chrome.borderStrong,
      checkboxBorderRadius: 4,
      checkboxSize: 15,
      checkedColor: theme.green,
      checkedStrikethrough: true,
      checkedTextColor: chrome.textMuted,
      checkmarkColor: theme.background,
    },
    thematicBreak: {
      color: chrome.border,
      height: StyleSheet.hairlineWidth,
      marginTop: 8,
      marginBottom: 10,
    },
    math: {
      color: chrome.text,
      backgroundColor: chrome.surfaceMuted,
      fontSize: 13,
      marginTop: 4,
      marginBottom: 8,
      padding: 8,
      textAlign: "left",
    },
    inlineMath: {
      color: theme.cyan,
    },
    spoiler: {
      color: chrome.surfaceMuted,
      solid: { borderRadius: 4 },
    },
  };
}
