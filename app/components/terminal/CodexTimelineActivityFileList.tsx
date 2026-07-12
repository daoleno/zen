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
  const visible = files.slice(0, 6);
  const remaining = files.length - visible.length;
  return (
    <View style={styles.files}>
      {visible.map((file) => (
        <Text
          key={file}
          style={[styles.fileText, { color: chrome.textMuted }]}
          numberOfLines={2}
        >
          {file}
        </Text>
      ))}
      {remaining > 0 ? (
        <Text style={[styles.fileText, { color: chrome.textSubtle }]}>
          +{remaining} more
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  files: {
    gap: 4,
  },
  fileText: {
    flexShrink: 1,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
});
