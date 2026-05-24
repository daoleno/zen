import { StyleSheet } from "react-native";
import remend, { type RemendOptions } from "remend";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

type MarkdownStyle = Record<string, Record<string, unknown>>;

const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: true,
  inlineKatex: false,
  linkMode: "text-only",
};

export function prepareCodexMarkdown(value: string, streaming: boolean) {
  let markdown = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!markdown) {
    return "";
  }
  if (streaming) {
    markdown = remend(markdown, STREAMING_REMEND_OPTIONS);
  }
  return stripMarkdownImages(markdown);
}

export function isSafeMarkdownUrl(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export function codexMarkdownStyle(
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
  compact: boolean,
): MarkdownStyle {
  const text = {
    color: chrome.text,
    fontFamily: Typography.uiFont,
    fontSize: 14,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: compact ? 6 : 7,
  };
  const heading = {
    color: chrome.text,
    fontFamily: Typography.uiFontMedium,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: 7,
  };
  return {
    paragraph: text,
    h1: { ...heading, fontSize: 16, lineHeight: 22 },
    h2: { ...heading, fontSize: 15, lineHeight: 21 },
    h3: { ...heading, fontSize: 14, lineHeight: 20 },
    h4: { ...heading, fontSize: 14, lineHeight: 20 },
    h5: { ...heading, fontSize: 13, lineHeight: 19 },
    h6: { ...heading, fontSize: 13, lineHeight: 19, color: chrome.textMuted },
    strong: {
      color: chrome.text,
      fontFamily: Typography.uiFontMedium,
      fontWeight: "normal",
    },
    em: {
      color: chrome.text,
      fontFamily: Typography.uiFont,
      fontStyle: "italic",
    },
    link: {
      color: chrome.accent,
      fontFamily: Typography.uiFontMedium,
      underline: false,
    },
    code: {
      color: theme.green,
      backgroundColor: chrome.surfaceMuted,
      borderColor: chrome.border,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
    },
    codeBlock: {
      color: chrome.text,
      backgroundColor: compact ? chrome.surface : theme.black,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      fontFamily: Typography.terminalFont,
      fontSize: 12,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
      padding: 10,
    },
    blockquote: {
      color: chrome.textMuted,
      backgroundColor: "transparent",
      borderColor: chrome.borderStrong,
      borderWidth: 2,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      gapWidth: 9,
      lineHeight: 19,
      marginTop: 0,
      marginBottom: 8,
    },
    list: {
      color: chrome.text,
      bulletColor: chrome.textSubtle,
      markerColor: chrome.textSubtle,
      markerFontWeight: "normal",
      fontFamily: Typography.uiFont,
      fontSize: 14,
      gapWidth: 7,
      lineHeight: 20,
      marginLeft: 0,
      marginTop: 0,
      marginBottom: 8,
    },
    table: {
      color: chrome.text,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      cellPaddingHorizontal: 8,
      cellPaddingVertical: 6,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      headerBackgroundColor: chrome.surfaceMuted,
      headerFontFamily: Typography.uiFontMedium,
      headerTextColor: chrome.text,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
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

function stripMarkdownImages(value: string) {
  return value.replace(/!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, (_match, alt, url) => {
    const label = String(alt || "").trim();
    const href = String(url || "").trim();
    if (!href) {
      return label;
    }
    return label ? `[${label}](${href})` : href;
  });
}
