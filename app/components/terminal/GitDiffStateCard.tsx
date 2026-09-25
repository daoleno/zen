import React from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import { withAlpha } from "./colorWithAlpha";

interface GitDiffStateCardProps {
  icon: keyof typeof Ionicons.glyphMap;
  title: string;
  detail?: string;
  accent: string;
  chromeText: string;
  chromeMuted: string;
  busy?: boolean;
}

/**
 * Quiet in-panel state for content that has no code to show (binary file,
 * unreadable snapshot, empty folder). Sheet-level loading, error and empty
 * states use the shared EmptyState instead.
 */
export function GitDiffStateCard({
  icon,
  title,
  detail,
  accent,
  chromeText,
  chromeMuted,
  busy = false,
}: GitDiffStateCardProps) {
  return (
    <View style={styles.stateCard} accessible accessibilityLiveRegion="polite">
      <View style={[styles.glyph, { backgroundColor: withAlpha(accent, 0.12) }]}>
        {busy ? (
          <ActivityIndicator size="small" color={accent} />
        ) : (
          <Ionicons name={icon} size={20} color={accent} />
        )}
      </View>
      <Text style={[styles.stateTitle, { color: chromeText }]}>{title}</Text>
      {detail ? <Text style={[styles.stateDetail, { color: chromeMuted }]}>{detail}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  stateCard: {
    alignItems: "center",
    paddingHorizontal: 24,
    paddingVertical: 32,
    gap: 6,
  },
  glyph: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 6,
  },
  stateTitle: {
    ...TypeScale.label,
    ...UiTextMetrics,
    textAlign: "center",
  },
  stateDetail: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    textAlign: "center",
    maxWidth: 320,
  },
});
