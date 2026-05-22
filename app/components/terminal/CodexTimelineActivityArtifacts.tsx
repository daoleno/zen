import React from "react";
import {
  ActivityIndicator,
  Image,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { PatchFileSummary } from "./CodexTimelineActivityTypes";

export function ActivityPreview({
  uri,
  failed,
  chrome,
}: {
  uri: string | null;
  failed: boolean;
  chrome: TerminalThemeChrome;
}) {
  if (uri) {
    return (
      <Image
        source={{ uri }}
        style={[styles.image, { borderColor: chrome.border }]}
        resizeMode="cover"
      />
    );
  }
  return (
    <View style={[styles.imagePlaceholder, { borderColor: chrome.border }]}>
      {failed ? (
        <Ionicons name="image-outline" size={16} color={chrome.textSubtle} />
      ) : (
        <ActivityIndicator size="small" color={chrome.textSubtle} />
      )}
    </View>
  );
}

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
            numberOfLines={1}
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

export function ActivityFileList({
  files,
  chrome,
}: {
  files: string[];
  chrome: TerminalThemeChrome;
}) {
  return (
    <View style={styles.files}>
      {files.slice(0, 4).map((file) => (
        <Text
          key={file}
          style={[styles.fileText, { color: chrome.textMuted }]}
          numberOfLines={1}
        >
          {file}
        </Text>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  files: {
    gap: 4,
  },
  fileText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffFiles: {
    gap: 5,
  },
  diffFileRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
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
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffRemoved: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  image: {
    width: "100%",
    height: 150,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
  },
  imagePlaceholder: {
    height: 96,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
});
