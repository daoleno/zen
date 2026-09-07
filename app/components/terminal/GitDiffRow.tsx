import React from "react";
import { StyleSheet, Text, View, useWindowDimensions } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { GitDiffRow as Row, GitDiffScope } from "../../services/gitDiff";
import { withAlpha } from "./colorWithAlpha";

export const GitDiffRow = React.memo(function GitDiffRow({ row, chrome, theme, wrap, fontSize, query, scope, showHeaders }: {
  row: Row; chrome: TerminalThemeChrome; theme: TerminalThemePalette;
  wrap: boolean; fontSize: number; query: string; scope: GitDiffScope; showHeaders: boolean;
}) {
  const { fontScale } = useWindowDimensions();
  if (row.kind === "scope" && scope !== "all") return null;
  if (!showHeaders && !query && row.kind === "meta" && /^(diff --git |index |--- |\+\+\+ )/.test(row.text)) return null;
  const color = row.kind === "add" ? theme.green : row.kind === "delete" ? theme.red : row.kind === "hunk" ? theme.cyan : chrome.text;
  const backgroundColor = ["add", "delete", "hunk"].includes(row.kind) ? withAlpha(color, 0.09) : "transparent";
  const code = ["add", "delete", "context", "marker"].includes(row.kind);
  const match = query ? row.text.toLowerCase().indexOf(query.toLowerCase()) : -1;
  return (
    <View style={[styles.row, { backgroundColor }]}>
      {code ? <Text
        accessibilityLabel={row.continuation ? "Line continuation" : `Old line ${row.old ?? ""}, new line ${row.new ?? ""}`}
        style={[styles.gutter, {
          width: (Math.max(10, fontSize - 2) * 6.8 + 12) * fontScale,
          color: chrome.textSubtle, fontSize: Math.max(10, fontSize - 2), lineHeight: fontSize * 1.5,
        }]}
      >
        {row.continuation ? "..." : `${String(row.old ?? "").padStart(5)} ${String(row.new ?? "").padStart(5)}`}
      </Text> : null}
      <Text selectable style={{ color, fontFamily: Typography.terminalFont, fontSize, lineHeight: fontSize * 1.5, ...(wrap ? { flex: 1 } : {}) }}>
        {row.kind === "scope" ? row.text === "staged" ? "Staged" : row.text === "untracked" ? "Untracked" : "Working"
          : match < 0 ? row.text || " " : <>
            {row.text.slice(0, match)}
            <Text style={{ backgroundColor: theme.yellow, color: chrome.appBackground }}>{row.text.slice(match, match + query.length)}</Text>
            {row.text.slice(match + query.length)}
          </>}
      </Text>
    </View>
  );
});

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "flex-start", paddingHorizontal: 8, paddingVertical: 2 },
  gutter: { textAlign: "right", paddingRight: 8, fontFamily: Typography.terminalFont },
});
