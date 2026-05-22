import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

export interface TerminalAccessoryGitDiff {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface TerminalAccessoryGitDiffChipProps {
  gitDiff: TerminalAccessoryGitDiff;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}

export function TerminalAccessoryGitDiffChip({
  gitDiff,
  chrome,
  theme,
}: TerminalAccessoryGitDiffChipProps) {
  const dirty = gitDiff.tone === "dirty";
  const backgroundColor = dirty
    ? chrome.accentSoft
    : gitDiff.tone === "clean"
      ? withAlpha(theme.green, 0.14)
      : chrome.surfaceMuted;

  return (
    <TouchableOpacity
      accessibilityLabel="Git diff"
      style={[
        styles.gitDiffChip,
        {
          backgroundColor,
          borderColor: dirty ? chrome.borderStrong : chrome.border,
        },
      ]}
      onPress={gitDiff.onPress}
      activeOpacity={0.75}
    >
      <Ionicons
        name={gitDiff.tone === "loading" ? "sync-outline" : "git-branch-outline"}
        size={14}
        color={dirty ? chrome.accent : chrome.textMuted}
      />
      <Text
        style={[
          styles.gitDiffChipText,
          { color: dirty ? chrome.text : chrome.textMuted },
        ]}
        numberOfLines={1}
      >
        {gitDiff.label}
      </Text>
    </TouchableOpacity>
  );
}

function withAlpha(hex: string, alpha: number): string {
  const normalized = hex.trim().replace("#", "");
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    return hex;
  }

  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${Math.min(Math.max(alpha, 0), 1)})`;
}

const styles = StyleSheet.create({
  gitDiffChip: {
    maxWidth: 220,
    minHeight: 36,
    marginRight: 4,
    paddingHorizontal: 12,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  gitDiffChipText: {
    flexShrink: 1,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
});
