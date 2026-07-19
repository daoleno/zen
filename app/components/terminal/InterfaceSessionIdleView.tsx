import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { compactPathLabel } from "../../services/pathDisplay";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface InterfaceSessionIdleViewProps {
  chrome: TerminalThemeChrome;
  busy?: boolean;
  cwd?: string;
}

export function InterfaceSessionIdleView({
  chrome,
  busy = false,
  cwd,
}: InterfaceSessionIdleViewProps) {
  const workspace = compactPathLabel(cwd, { tailSegments: 2, showFullUpTo: 2 });

  return (
    <View style={styles.root}>
      {busy ? (
        <ComposerLoadingDots color={chrome.textMuted} size={11} />
      ) : workspace ? (
        <Text
          style={[styles.workspace, { color: chrome.textSubtle }]}
          numberOfLines={1}
          ellipsizeMode="head"
        >
          {workspace}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    minHeight: 120,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  workspace: {
    maxWidth: 280,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
    textAlign: "center",
    opacity: 0.72,
  },
});
