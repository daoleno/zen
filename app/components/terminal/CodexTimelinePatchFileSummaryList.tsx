import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { PatchFileSummary } from "./CodexTimelineActivityTypes";

export function PatchFileSummaryList({
  files,
  chrome,
  theme,
  formatPatchPath,
}: {
  files: PatchFileSummary[];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  formatPatchPath(file: PatchFileSummary): string;
}) {
  return (
    <View style={styles.diffFiles}>
      {files.slice(0, 6).map((file) => (
        <View key={`${file.operation}:${file.path}`} style={styles.diffFileRow}>
          <Text style={[styles.diffPrefix, { color: chrome.textSubtle }]}>
            {"\u2514"}
          </Text>
          <Text
            style={[styles.diffPath, { color: chrome.textMuted }]}
          >
            {formatPatchPath(file)}
          </Text>
          <Text style={[styles.diffAdded, { color: theme.green }]}>
            +{file.added}
          </Text>
          <Text style={[styles.diffRemoved, { color: theme.red }]}>
            -{file.removed}
          </Text>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  diffFiles: {
    gap: 5,
  },
  diffFileRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 5,
  },
  diffPrefix: {
    width: 10,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffPath: {
    flex: 1,
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffAdded: {
    paddingTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffRemoved: {
    paddingTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
