import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import { withAlpha } from "./gitDiffColor";

interface GitDiffStateCardProps {
  icon: keyof typeof Ionicons.glyphMap;
  title: string;
  detail: string;
  accent: string;
  chromeText: string;
  chromeMuted: string;
  busy?: boolean;
}

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
    <View
      style={[
        styles.stateCard,
        { borderColor: withAlpha(accent, 0.24), backgroundColor: withAlpha(accent, 0.08) },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={accent} />
      ) : (
        <Ionicons name={icon} size={18} color={accent} />
      )}
      <Text style={[styles.stateTitle, { color: chromeText }]}>{title}</Text>
      <Text style={[styles.stateDetail, { color: chromeMuted }]}>{detail}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  stateCard: {
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 12,
    alignItems: "flex-start",
    gap: 6,
  },
  stateTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  stateDetail: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
});
