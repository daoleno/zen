import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { compactPathLabel } from "../../services/pathDisplay";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface CodexSessionIdleViewProps {
  chrome: TerminalThemeChrome;
  busy?: boolean;
  cwd?: string;
}

export function CodexSessionIdleView({
  chrome,
  busy = false,
  cwd,
}: CodexSessionIdleViewProps) {
  const workspace = compactPathLabel(cwd, { tailSegments: 2, showFullUpTo: 2 });

  return (
    <View style={styles.root}>
      {busy ? (
        <View
          style={[
            styles.busyFrame,
            {
              borderColor: chrome.border,
              backgroundColor: chrome.surfaceMuted,
            },
          ]}
        >
          <ComposerLoadingDots color={chrome.accent} size={5} gap={4} />
        </View>
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
  busyFrame: {
    minWidth: 52,
    minHeight: 36,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 12,
    paddingVertical: 8,
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