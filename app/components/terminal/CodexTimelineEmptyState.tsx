import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface CodexTimelineEmptyStateProps {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  actionLabel?: string;
  onAction?: () => void;
}

export function CodexTimelineEmptyState({
  chrome,
  title,
  body,
  busy = false,
  actionLabel,
  onAction,
}: CodexTimelineEmptyStateProps) {
  return (
    <View style={styles.emptyState}>
      {busy ? <BusyGlyph chrome={chrome} /> : null}
      <Text style={[styles.emptyTitle, { color: chrome.text }]}>{title}</Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>{body}</Text>
      ) : null}
      {actionLabel && onAction ? (
        <TouchableOpacity
          accessibilityLabel={actionLabel}
          accessibilityRole="button"
          style={[
            styles.emptyAction,
            { backgroundColor: chrome.composerInput, borderColor: chrome.border },
          ]}
          onPress={onAction}
          activeOpacity={0.82}
        >
          <Ionicons name="terminal-outline" size={15} color={chrome.textMuted} />
          <Text style={[styles.emptyActionText, { color: chrome.textMuted }]}>
            {actionLabel}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function BusyGlyph({
  chrome,
}: {
  chrome: TerminalThemeChrome;
}) {
  return (
    <View
      accessible={false}
      pointerEvents="none"
      style={styles.busyGlyph}
    >
      <ComposerLoadingDots color={chrome.textMuted} size={11} />
    </View>
  );
}

const styles = StyleSheet.create({
  emptyState: {
    minHeight: 220,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 28,
  },
  busyGlyph: {
    marginBottom: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
  },
  emptyTitle: {
    marginTop: 2,
    fontSize: 17,
    lineHeight: 22,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    marginTop: 6,
    fontSize: 13.5,
    lineHeight: 19,
    textAlign: "center",
    fontFamily: Typography.uiFont,
    maxWidth: 260,
    opacity: 0.86,
  },
  emptyAction: {
    marginTop: 14,
    minHeight: 36,
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 7,
  },
  emptyActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
});
