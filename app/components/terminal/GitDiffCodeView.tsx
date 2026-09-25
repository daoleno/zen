import React from "react";
import { ScrollView, StyleSheet, Text, View, useWindowDimensions } from "react-native";
import type { StyleProp, TextStyle } from "react-native";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitDiffContentSnapshot } from "../../services/gitDiff";
import { InlineNotice } from "../ui/InlineNotice";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  highlightCodeLine,
  type HighlightTokenKind,
} from "./gitDiffSyntaxHighlight";

interface CodeSnapshotPanelProps {
  path: string;
  snapshot: GitDiffContentSnapshot | null;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
  bottomInset?: number;
}

export function GitDiffCodeSnapshotPanel({
  path,
  snapshot,
  chrome,
  theme,
  bottomInset = 0,
}: CodeSnapshotPanelProps) {
  const { fontScale } = useWindowDimensions();
  if (!snapshot?.exists || !snapshot.content) {
    return (
      <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
        <GitDiffStateCard
          icon="document-text-outline"
          title="File snapshot unavailable"
          detail={
            snapshot?.reason ||
            "This file could not be read from the working tree."
          }
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  if (snapshot.binary) {
    return (
      <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
        <GitDiffStateCard
          icon="cube-outline"
          title="Binary file"
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  const lines = snapshot.content.split("\n");
  const gutterWidth = (String(lines.length).length * 7.4 + 20) * fontScale;
  return (
    <ScrollView
      style={[styles.codeScroll, { backgroundColor: chrome.surface }]}
      contentContainerStyle={[
        styles.codeScrollContent,
        { paddingBottom: bottomInset + 20 },
      ]}
      showsVerticalScrollIndicator={false}
      nestedScrollEnabled={false}
    >
      {snapshot.truncated ? (
        <InlineNotice
          tone="warning"
          icon="cut-outline"
          title="Showing part of this file"
          detail={`First ${formatByteCount(snapshot.content.length)} of ${formatByteCount(snapshot.byte_count)}.`}
          style={styles.truncationNotice}
        />
      ) : null}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator
        nestedScrollEnabled={false}
      >
        <View style={styles.codeFrame}>
          {lines.map((line, index) => (
            <View key={index} style={styles.codeRow}>
              <Text
                style={[
                  styles.codeLineNumber,
                  {
                    width: gutterWidth,
                    color: chrome.textSubtle,
                    backgroundColor: chrome.surfaceMuted,
                    borderRightColor: chrome.border,
                  },
                ]}
              >
                {index + 1}
              </Text>
              <HighlightedCodeLine
                line={line}
                path={path}
                theme={theme}
                chrome={chrome}
                style={styles.codeLine}
                baseColor={chrome.text || theme.foreground}
              />
            </View>
          ))}
        </View>
      </ScrollView>
    </ScrollView>
  );
}

function HighlightedCodeLine({
  line,
  path,
  theme,
  chrome,
  style,
  baseColor,
}: {
  line: string;
  path: string;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  style: StyleProp<TextStyle>;
  baseColor: string;
}) {
  const text = line || " ";
  return (
    <Text selectable style={[style, { color: baseColor }]}>
      {renderHighlightSegments(text, path, theme, chrome, baseColor)}
    </Text>
  );
}

function renderHighlightSegments(
  text: string,
  path: string,
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
  baseColor: string,
) {
  return highlightCodeLine(text, path).map((segment, index) => (
    <Text
      key={`${index}:${segment.kind}`}
      style={{ color: syntaxColor(segment.kind, theme, chrome, baseColor) }}
    >
      {segment.text}
    </Text>
  ));
}

function syntaxColor(
  kind: HighlightTokenKind,
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
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

function formatByteCount(bytes: number): string {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (bytes >= 1024) {
    return `${Math.round(bytes / 1024)} KB`;
  }
  return `${bytes} B`;
}

const styles = StyleSheet.create({
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  codeScroll: {
    flex: 1,
  },
  codeScrollContent: {
    paddingTop: 0,
    paddingBottom: 20,
  },
  truncationNotice: {
    marginHorizontal: 12,
    marginVertical: 10,
  },
  codeFrame: {
    minWidth: "100%",
  },
  codeRow: {
    flexDirection: "row",
    alignItems: "stretch",
    paddingRight: 16,
  },
  codeLineNumber: {
    textAlign: "right",
    paddingRight: 8,
    paddingVertical: 1,
    marginRight: 10,
    borderRightWidth: StyleSheet.hairlineWidth,
    fontSize: 11,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
    fontVariant: ["tabular-nums"],
  },
  codeLine: {
    paddingVertical: 1,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
});
