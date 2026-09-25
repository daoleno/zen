import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale } from "../../constants/tokens";
import { withAlpha } from "./colorWithAlpha";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface InterfaceTimelineEmptyStateProps {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  /** Error states tint the glyph with the chrome's danger ink. */
  tone?: "default" | "error";
  icon?: IoniconName;
  actionLabel?: string;
  actionIcon?: IoniconName;
  onAction?: () => void;
}

/**
 * Chat-canvas counterpart of the app EmptyState: same halo, title, detail
 * and capsule action, drawn from the terminal-theme chrome so it follows the
 * Session's canvas instead of the app theme.
 */
export function InterfaceTimelineEmptyState({
  chrome,
  title,
  body,
  busy = false,
  tone = "default",
  icon = "chatbubble-ellipses-outline",
  actionLabel,
  actionIcon = "terminal-outline",
  onAction,
}: InterfaceTimelineEmptyStateProps) {
  const ink = tone === "error" ? chrome.danger : chrome.accent;
  return (
    <View style={styles.emptyState} accessibilityLiveRegion="polite">
      <View
        accessible={false}
        style={[styles.halo, { backgroundColor: withAlpha(ink, 0.12) }]}
      >
        {busy ? (
          <ComposerLoadingDots color={ink} size={11} />
        ) : (
          <Ionicons name={icon} size={26} color={ink} />
        )}
      </View>
      <Text style={[styles.emptyTitle, { color: chrome.text }]} accessibilityRole="header">
        {title}
      </Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>
          {body}
        </Text>
      ) : null}
      {actionLabel && onAction ? (
        <Pressable
          accessibilityLabel={actionLabel}
          accessibilityRole="button"
          onPress={() => {
            void Haptics.selectionAsync();
            onAction();
          }}
          style={({ pressed }) => [
            styles.emptyAction,
            {
              backgroundColor: withAlpha(chrome.accent, 0.14),
              opacity: pressed ? 0.75 : 1,
            },
          ]}
        >
          <Ionicons name={actionIcon} size={16} color={chrome.accent} />
          <Text style={[styles.emptyActionText, { color: chrome.accent }]}>
            {actionLabel}
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  emptyState: {
    minHeight: 240,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 32,
  },
  halo: {
    width: 64,
    height: 64,
    borderRadius: 22,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 16,
  },
  emptyTitle: {
    ...TypeScale.title,
    textAlign: "center",
  },
  emptyBody: {
    ...TypeScale.compact,
    marginTop: 6,
    textAlign: "center",
    maxWidth: 300,
  },
  emptyAction: {
    marginTop: 18,
    minHeight: 44,
    borderRadius: 22,
    paddingHorizontal: 18,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  emptyActionText: {
    ...TypeScale.label,
  },
});
