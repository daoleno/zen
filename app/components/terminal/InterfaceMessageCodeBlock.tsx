import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { withAlpha } from "./colorWithAlpha";
import {
  highlightCodeLineForLanguage,
  type HighlightTokenKind,
} from "./gitDiffSyntaxHighlight";
import { shouldRenderPlainMonospaceCodeBlock } from "./codeBlockPresentation";
import { createCodeBlockCopyFeedback } from "./InterfaceMessageCodeBlockCopy";
import { PreformattedCodeWebView } from "./PreformattedCodeWebView";
import { useTimelineSelectableTextProps } from "./TimelineTextSelectableContext";

interface InterfaceMessageCodeBlockProps {
  text: string;
  language?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact: boolean;
  isLast: boolean;
}

export function InterfaceMessageCodeBlock({
  text,
  language,
  chrome,
  theme,
  compact,
  isLast,
}: InterfaceMessageCodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const selectableTextProps = useTimelineSelectableTextProps();
  const copyFeedback = useMemo(
    () =>
      createCodeBlockCopyFeedback({
        copyText: Clipboard.setStringAsync,
        onCopiedChange: setCopied,
        scheduleReset: setTimeout,
        cancelReset: clearTimeout,
      }),
    [],
  );
  const prepared = useMemo(
    () => prepareCodeBlockText(text, language),
    [language, text],
  );
  const lines = useMemo(() => splitCodeLines(prepared.text), [prepared.text]);
  const label = prepared.usePlainMonospace
    ? ""
    : formatLanguageLabel(prepared.language);
  const baseColor = chrome.text || theme.foreground;
  const codeTextStyle = [
    styles.codeLine,
    compact ? styles.codeLineCompact : null,
    { color: baseColor, fontFamily: Typography.chatMonoFont },
  ];
  const handleCopy = useCallback(() => {
    void copyFeedback.copy(text);
  }, [copyFeedback, text]);

  useEffect(() => {
    return () => copyFeedback.dispose();
  }, [copyFeedback]);

  return (
    <View
      style={[
        styles.frame,
        compact ? styles.frameCompact : null,
        {
          backgroundColor:
            compact || theme.background === "transparent"
              ? chrome.surface
              : mixCodeSurface(theme),
          borderColor: chrome.border,
        },
        isLast ? styles.blockLast : null,
      ]}
    >
      <View
        style={[
          styles.header,
          {
            borderBottomColor: chrome.border,
            backgroundColor: withAlpha(
              theme.foreground,
              compact ? 0.035 : 0.045,
            ),
          },
        ]}
      >
        <Text
          numberOfLines={1}
          style={[styles.language, { color: chrome.textMuted }]}
        >
          {label}
        </Text>
        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel={copied ? "Code copied" : "Copy code"}
          accessibilityHint="Copies this code block to the clipboard"
          accessibilityState={{ selected: copied }}
          onPress={handleCopy}
          style={styles.copyButton}
          scale={0.9}
        >
          <Ionicons
            name={copied ? "checkmark" : "copy-outline"}
            size={16}
            color={copied ? chrome.accent : chrome.textMuted}
          />
          {copied ? (
            <Text
              accessibilityLiveRegion="polite"
              style={[styles.copiedText, { color: chrome.accent }]}
            >
              Copied
            </Text>
          ) : null}
        </AnimatedPressable>
      </View>
      {prepared.usePlainMonospace ? (
        <PreformattedCodeWebView
          text={prepared.text}
          color={baseColor}
          compact={compact}
        />
      ) : (
        <ScrollView
          horizontal
          nestedScrollEnabled
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={[
            styles.scrollContent,
            compact ? styles.scrollContentCompact : null,
          ]}
        >
          <View style={styles.codeContent}>
            {lines.map((line, index) => (
              <Text key={index} {...selectableTextProps} style={codeTextStyle}>
                {highlightCodeLineForLanguage(
                  line || " ",
                  prepared.language,
                ).map((segment, segmentIndex) => (
                  <Text
                    key={`${segmentIndex}:${segment.kind}`}
                    style={{
                      color: syntaxColor(
                        segment.kind,
                        theme,
                        chrome,
                        baseColor,
                      ),
                      fontFamily: fontFamilyForSegment(segment.kind),
                    }}
                  >
                    {segment.text}
                  </Text>
                ))}
              </Text>
            ))}
          </View>
        </ScrollView>
      )}
    </View>
  );
}

const MAX_JSON_FORMAT_CHARS = 220_000;

function prepareCodeBlockText(
  text: string,
  language: string | undefined,
): { text: string; language?: string; usePlainMonospace: boolean } {
  const normalized = normalizeCodeBlockText(text);
  const normalizedLanguage = normalizeLanguage(language);
  const usePlainMonospace = shouldRenderPlainMonospaceCodeBlock(
    normalized,
    normalizedLanguage,
  );
  if (usePlainMonospace) {
    return {
      text: normalized,
      language: normalizedLanguage,
      usePlainMonospace: true,
    };
  }

  const shouldTryJson =
    isJsonLanguage(normalizedLanguage) ||
    (!normalizedLanguage && looksLikeJsonContainer(normalized));
  if (!shouldTryJson || normalized.length > MAX_JSON_FORMAT_CHARS) {
    return {
      text: normalized,
      language: normalizedLanguage,
      usePlainMonospace: false,
    };
  }

  const formatted = formatJsonContainer(normalized);
  if (!formatted) {
    return {
      text: normalized,
      language: normalizedLanguage,
      usePlainMonospace: false,
    };
  }

  return {
    text: formatted,
    language: isJsonLanguage(normalizedLanguage) ? normalizedLanguage : "json",
    usePlainMonospace: false,
  };
}

function normalizeCodeBlockText(text: string) {
  return text.replace(/\r\n/g, "\n").replace(/\n+$/, "");
}

function splitCodeLines(text: string) {
  const normalized = normalizeCodeBlockText(text);
  const lines = normalized.split("\n");
  return lines.length > 0 ? lines : [" "];
}

function normalizeLanguage(language: string | undefined) {
  const value = language?.trim().toLowerCase();
  return value || undefined;
}

function isJsonLanguage(language: string | undefined) {
  return language === "json" || language === "jsonc";
}

function looksLikeJsonContainer(value: string) {
  const trimmed = value.trim();
  if (trimmed.length < 2) {
    return false;
  }
  return (
    (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]"))
  );
}

function formatJsonContainer(value: string) {
  try {
    const parsed: unknown = JSON.parse(value.trim());
    if (!parsed || typeof parsed !== "object") {
      return null;
    }
    return JSON.stringify(parsed, null, 2);
  } catch {
    return null;
  }
}

function formatLanguageLabel(language: string | undefined) {
  const value = language?.trim();
  if (!value) {
    return "";
  }
  switch (value.toLowerCase()) {
    case "bash":
      return "Bash";
    case "c++":
    case "cpp":
      return "C++";
    case "css":
      return "CSS";
    case "go":
    case "golang":
      return "Go";
    case "html":
      return "HTML";
    case "java":
      return "Java";
    case "js":
    case "javascript":
      return "JavaScript";
    case "jsx":
      return "JSX";
    case "json":
      return "JSON";
    case "jsonc":
      return "JSONC";
    case "kt":
    case "kotlin":
      return "Kotlin";
    case "md":
    case "markdown":
      return "Markdown";
    case "php":
      return "PHP";
    case "ts":
    case "typescript":
      return "TypeScript";
    case "tsx":
      return "TSX";
    case "py":
    case "python":
      return "Python";
    case "rb":
    case "ruby":
      return "Ruby";
    case "rs":
    case "rust":
      return "Rust";
    case "scss":
      return "SCSS";
    case "sh":
    case "shell":
      return "Shell";
    case "sql":
      return "SQL";
    case "swift":
      return "Swift";
    case "toml":
      return "TOML";
    case "yaml":
    case "yml":
      return "YAML";
    default:
      return value;
  }
}

function mixCodeSurface(theme: TerminalThemePalette) {
  return withAlpha(theme.foreground, 0.04);
}

function fontFamilyForSegment(kind: HighlightTokenKind) {
  switch (kind) {
    case "keyword":
    case "tag":
      return Typography.chatMonoFontBold;
    default:
      return Typography.chatMonoFont;
  }
}

function syntaxColor(
  kind: HighlightTokenKind,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
  baseColor: string,
): string {
  switch (kind) {
    case "attribute":
    case "property":
      return theme.cyan;
    case "comment":
      return chrome.textSubtle;
    case "constant":
    case "number":
      return theme.yellow;
    case "function":
      return theme.blue;
    case "keyword":
    case "tag":
      return theme.magenta;
    case "operator":
    case "punctuation":
      return chrome.textMuted;
    case "string":
      return theme.green;
    default:
      return baseColor;
  }
}

const styles = StyleSheet.create({
  frame: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    maxWidth: "100%",
    marginTop: 2,
    marginBottom: 10,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  frameCompact: {
    marginBottom: 8,
  },
  header: {
    borderBottomWidth: StyleSheet.hairlineWidth,
    minHeight: 44,
    paddingLeft: 11,
    paddingRight: 2,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  language: {
    flex: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.chatMonoFontBold,
  },
  copyButton: {
    minWidth: 44,
    height: 44,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 5,
  },
  copiedText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.chatMonoFontBold,
  },
  scrollContent: {
    flexGrow: 1,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  scrollContentCompact: {
    paddingHorizontal: 10,
    paddingVertical: 9,
  },
  codeContent: {
    flexShrink: 0,
    alignSelf: "flex-start",
  },
  codeLine: {
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.chatMonoFont,
    includeFontPadding: false,
    textAlign: "left",
    letterSpacing: 0,
  },
  codeLineCompact: {
    fontSize: 12,
    lineHeight: 18,
  },
  blockLast: {
    marginBottom: 0,
  },
});
