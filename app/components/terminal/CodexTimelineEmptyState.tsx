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

function isAmbientChrome(chrome: TerminalThemeChrome): boolean {
  return chrome.appBackground === "transparent";
}

export function CodexTimelineEmptyState({
  chrome,
  title,
  body,
  busy = false,
  actionLabel,
  onAction,
}: CodexTimelineEmptyStateProps) {
  const ambient = isAmbientChrome(chrome);

  return (
    <View style={[styles.emptyState, ambient && styles.emptyStateAmbient]}>
      {busy ? (
        <BusyGlyph chrome={chrome} />
      ) : ambient ? (
        <View style={[styles.emptyBadge, { backgroundColor: chrome.accentSoft }]}>
          <Text style={[styles.emptyGlyph, { color: chrome.accent }]}>✦</Text>
        </View>
      ) : null}
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
            { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
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
  size = 20,
}: {
  chrome: TerminalThemeChrome;
  size?: number;
}) {
  return (
    <View
      accessible={false}
      pointerEvents="none"
      style={[
        styles.busyGlyph,
        {
          width: size,
          height: size,
          borderColor: chrome.border,
        },
      ]}
    >
      <ComposerLoadingDots
        color={chrome.accent}
        size={Math.max(4, Math.round(size * 0.18))}
        gap={Math.max(3, Math.round(size * 0.13))}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  emptyState: {
    minHeight: 260,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  emptyStateAmbient: {
    minHeight: 320,
    paddingHorizontal: 32,
  },
  emptyBadge: {
    width: 72,
    height: 72,
    borderRadius: 36,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 4,
  },
  emptyGlyph: {
    fontSize: 34,
    lineHeight: 40,
    textAlign: "center",
  },
  busyGlyph: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 2.5,
  },
  emptyTitle: {
    marginTop: 10,
    fontSize: 18,
    lineHeight: 24,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    marginTop: 8,
    fontSize: 14,
    lineHeight: 20,
    textAlign: "center",
    fontFamily: Typography.uiFont,
    maxWidth: 280,
    opacity: 0.88,
  },
  emptyAction: {
    marginTop: 14,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
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
