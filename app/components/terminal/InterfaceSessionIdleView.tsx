import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, Typography } from "../../constants/tokens";
import { withAlpha } from "./colorWithAlpha";
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

  if (busy) {
    return (
      <View style={styles.root} accessibilityLabel="Loading conversation">
        <ComposerLoadingDots color={chrome.textMuted} size={11} />
      </View>
    );
  }

  // A fresh Session: say it is ready and where it runs, then let the
  // composer below carry the next step.
  return (
    <View style={styles.root}>
      <Text style={[styles.title, { color: chrome.text }]}>Ready</Text>
      <Text style={[styles.hint, { color: chrome.textMuted }]}>
        Send a message to start.
      </Text>
      {workspace ? (
        <View style={[styles.workspaceChip, { backgroundColor: withAlpha(chrome.textMuted, 0.1) }]}>
          <Ionicons name="folder-outline" size={13} color={chrome.textSubtle} />
          <Text
            style={[styles.workspace, { color: chrome.textSubtle }]}
            numberOfLines={1}
            ellipsizeMode="head"
          >
            {workspace}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    minHeight: 160,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
    gap: 4,
  },
  title: {
    ...TypeScale.title,
    textAlign: "center",
  },
  hint: {
    ...TypeScale.compact,
    textAlign: "center",
  },
  workspaceChip: {
    marginTop: 10,
    maxWidth: 300,
    minHeight: 28,
    borderRadius: 14,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  workspace: {
    flexShrink: 1,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
  },
});
