import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { PatchFileSummary } from "./InterfaceTimelineActivityTypes";
import { useSessionFilePreviewContext } from "./SessionFilePreviewContext";

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
  const filePreview = useSessionFilePreviewContext();
  return (
    <View style={styles.diffFiles}>
      {files.slice(0, 6).map((file) => (
        <Pressable
          key={`${file.operation}:${file.path}`}
          accessibilityRole={filePreview ? "link" : undefined}
          disabled={!filePreview}
          onPress={() => filePreview?.open(file.movePath || file.path)}
          style={styles.diffFileRow}
        >
          <Text
            style={[styles.diffPath, { color: chrome.textMuted }]}
            numberOfLines={2}
          >
            {formatPatchPath(file)}
          </Text>
          {file.added != null ? (
            <Text style={[styles.diffAdded, { color: theme.green }]}>
              +{file.added}
            </Text>
          ) : null}
          {file.removed != null ? (
            <Text style={[styles.diffRemoved, { color: theme.red }]}>
              -{file.removed}
            </Text>
          ) : null}
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  diffFiles: {
    gap: 4,
  },
  diffFileRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
  },
  diffPath: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  diffAdded: {
    paddingTop: 1,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  diffRemoved: {
    paddingTop: 1,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
});
