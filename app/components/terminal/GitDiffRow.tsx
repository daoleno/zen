import React from "react";
import { StyleSheet, Text, View, useWindowDimensions } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { GitDiffRow as Row, GitDiffScope } from "../../services/gitDiff";
import { withAlpha } from "./colorWithAlpha";

const GUTTER_DIGITS = 5;

/** Width of the two-column line-number gutter, shared with the reader's no-wrap width. */
export function gitDiffGutterWidth(fontSize: number, fontScale: number): number {
  const gutterFont = Math.max(10, fontSize - 2);
  return 2 * (gutterFont * 0.62 * GUTTER_DIGITS + 8) * fontScale + StyleSheet.hairlineWidth;
}

export const GitDiffRow = React.memo(function GitDiffRow({ row, chrome, theme, wrap, fontSize, query, scope, showHeaders }: {
  row: Row; chrome: TerminalThemeChrome; theme: TerminalThemePalette;
  wrap: boolean; fontSize: number; query: string; scope: GitDiffScope; showHeaders: boolean;
}) {
  const { fontScale } = useWindowDimensions();
  if (row.kind === "scope" && scope !== "all") return null;
  if (!showHeaders && !query && row.kind === "meta" && /^(diff --git |index |--- |\+\+\+ )/.test(row.text)) return null;
  const lineHeight = Math.round(fontSize * 1.5);

  if (row.kind === "scope") {
    return (
      <View style={styles.scopeRow} accessibilityRole="header">
        <Text style={[styles.scopeText, { color: chrome.textSubtle }]}>
          {row.text === "staged" ? "Staged" : row.text === "untracked" ? "Untracked" : "Working tree"}
        </Text>
      </View>
    );
  }

  const change = row.kind === "add" ? theme.green : row.kind === "delete" ? theme.red : null;
  const code = row.kind === "add" || row.kind === "delete" || row.kind === "context" || row.kind === "marker";
  const hunk = row.kind === "hunk";
  const ink = code && row.kind !== "marker" ? chrome.text : chrome.textSubtle;
  const gutterFont = Math.max(10, fontSize - 2);
  const column = (gutterFont * 0.62 * GUTTER_DIGITS + 8) * fontScale;
  // Only the first slice of a line carries the +/- marker; continuation rows
  // are raw content split by the daemon.
  const markerLength = change && !row.continuation ? 1 : 0;

  return (
    <View
      style={[
        styles.row,
        hunk && [styles.hunkRow, { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border }],
        change ? { backgroundColor: withAlpha(change, 0.08) } : null,
      ]}
    >
      <View
        accessible={code}
        accessibilityLabel={row.continuation ? "Line continuation" : code ? `Old line ${row.old ?? ""}, new line ${row.new ?? ""}` : undefined}
        importantForAccessibility={code ? "yes" : "no-hide-descendants"}
        style={[
          styles.gutter,
          {
            borderRightColor: chrome.border,
            backgroundColor: change ? withAlpha(change, 0.1) : hunk ? "transparent" : chrome.surfaceMuted,
          },
        ]}
      >
        {code ? (
          <>
            <Text style={[styles.lineNumber, { width: column, color: chrome.textSubtle, fontSize: gutterFont, lineHeight }]}>
              {row.continuation ? "" : row.old ?? ""}
            </Text>
            <Text style={[styles.lineNumber, { width: column, color: chrome.textSubtle, fontSize: gutterFont, lineHeight }]}>
              {row.continuation ? "↪" : row.new ?? ""}
            </Text>
          </>
        ) : (
          <View style={{ width: column * 2 }} />
        )}
      </View>
      <Text
        selectable
        style={[
          styles.code,
          {
            color: ink,
            fontSize: hunk ? gutterFont : fontSize,
            lineHeight,
            fontStyle: row.kind === "marker" ? "italic" : "normal",
          },
          wrap ? styles.codeWrap : null,
        ]}
      >
        {renderSegments(row.text || " ", query, markerLength, change, theme, chrome)}
      </Text>
    </View>
  );
});

function renderSegments(
  text: string,
  query: string,
  markerLength: number,
  markerColor: string | null,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
): React.ReactNode {
  const match = query ? text.toLowerCase().indexOf(query.toLowerCase()) : -1;
  const pieces: { text: string; start: number; highlight: boolean }[] =
    match < 0
      ? [{ text, start: 0, highlight: false }]
      : [
          { text: text.slice(0, match), start: 0, highlight: false },
          { text: text.slice(match, match + query.length), start: match, highlight: true },
          { text: text.slice(match + query.length), start: match + query.length, highlight: false },
        ];
  const out: React.ReactNode[] = [];
  pieces.forEach((piece, index) => {
    if (!piece.text) return;
    const highlight = piece.highlight
      ? { backgroundColor: withAlpha(theme.yellow, 0.45), color: chrome.text }
      : null;
    if (markerLength && markerColor && piece.start === 0) {
      out.push(
        <Text key={`${index}m`} style={[{ color: markerColor }, highlight]}>
          {piece.text.slice(0, markerLength)}
        </Text>,
      );
      const rest = piece.text.slice(markerLength);
      if (rest) out.push(<Text key={index} style={highlight}>{rest}</Text>);
      return;
    }
    out.push(highlight ? <Text key={index} style={highlight}>{piece.text}</Text> : piece.text);
  });
  return out;
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "stretch" },
  hunkRow: {
    marginTop: 6,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  scopeRow: { paddingHorizontal: 16, paddingTop: 14, paddingBottom: 6 },
  scopeText: { fontSize: 12, lineHeight: 16, fontFamily: Typography.uiFontMedium, letterSpacing: 0.4 },
  gutter: {
    flexDirection: "row",
    borderRightWidth: StyleSheet.hairlineWidth,
  },
  lineNumber: {
    textAlign: "right",
    paddingRight: 8,
    paddingVertical: 1,
    fontFamily: Typography.terminalFont,
    fontVariant: ["tabular-nums"],
  },
  code: {
    fontFamily: Typography.terminalFont,
    paddingLeft: 10,
    paddingRight: 12,
    paddingVertical: 1,
  },
  codeWrap: { flex: 1 },
});
