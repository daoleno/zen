import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { StyleProp, TextStyle } from "react-native";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitDiffContentSnapshot } from "../../services/gitDiff";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  highlightCodeLine,
  type HighlightTokenKind,
} from "./gitDiffSyntaxHighlight";
import { withAlpha } from "./gitDiffColor";

interface CodeSnapshotPanelProps {
  path: string;
  snapshot: GitDiffContentSnapshot | null;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}

export function GitDiffCodeSnapshotPanel({
  path,
  snapshot,
  chrome,
  theme,
}: CodeSnapshotPanelProps) {
  if (!snapshot?.exists || !snapshot.content) {
    return (
      <View style={styles.contentPad}>
        <GitDiffStateCard
          icon="document-text-outline"
          title="File snapshot unavailable"
          detail={snapshot?.reason || "This file could not be read from the working tree."}
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  if (snapshot.binary) {
    return (
      <View style={styles.contentPad}>
        <GitDiffStateCard
          icon="cube-outline"
          title="Binary file"
          detail="Zen does not render binary file content."
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  const lines = snapshot.content.split("\n");
  return (
    <ScrollView
      style={styles.codeScroll}
      contentContainerStyle={styles.codeScrollContent}
      showsVerticalScrollIndicator={false}
      nestedScrollEnabled={false}
    >
      {snapshot.truncated ? (
        <View
          style={[
            styles.truncationBanner,
            {
              backgroundColor: withAlpha(theme.yellow, 0.1),
              borderColor: withAlpha(theme.yellow, 0.2),
            },
          ]}
        >
          <Text style={[styles.truncationText, { color: theme.yellow }]}>
            Showing the first {formatByteCount(snapshot.content.length)} of {formatByteCount(snapshot.byte_count)}.
          </Text>
        </View>
      ) : null}
      <ScrollView horizontal showsHorizontalScrollIndicator nestedScrollEnabled={false}>
        <View
          style={[
            styles.codeFrame,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
        >
          {lines.map((line, index) => (
            <View key={index} style={styles.codeRow}>
              <Text style={[styles.codeLineNumber, { color: chrome.textSubtle }]}>
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
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 20,
  },
  truncationBanner: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 9,
    marginBottom: 10,
  },
  truncationText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  codeFrame: {
    minWidth: "100%",
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  codeRow: {
    minHeight: 18,
    flexDirection: "row",
    alignItems: "center",
    paddingRight: 12,
  },
  codeLineNumber: {
    width: 36,
    textAlign: "right",
    paddingRight: 8,
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  codeLine: {
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
