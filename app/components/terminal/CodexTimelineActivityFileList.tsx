import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

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
});
