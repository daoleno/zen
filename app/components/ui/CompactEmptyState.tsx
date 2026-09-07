import React from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, useAppColors } from "../../constants/tokens";
import { AnimatedPressable } from "./AnimatedPressable";

type Icon = React.ComponentProps<typeof Ionicons>["name"];
export interface EmptyAction {
  label: string;
  icon: Icon;
  onPress(): void;
  disabled?: boolean;
}

export function CompactEmptyState({ title, detail, icon, busy = false, action, secondary }: {
  title: string;
  detail?: string;
  icon: Icon;
  busy?: boolean;
  action?: EmptyAction;
  secondary?: EmptyAction;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.root}>
      <View style={styles.symbol} accessible={false}>
        {busy ? <ActivityIndicator color={colors.accent} /> : <Ionicons name={icon} size={32} color={colors.textSecondary} />}
      </View>
      <Text accessibilityRole="header" accessibilityLiveRegion="polite" style={[styles.title, { color: colors.textPrimary }]}>{title}</Text>
      {detail ? <Text style={[styles.detail, { color: colors.textSecondary }]}>{detail}</Text> : null}
      {action ? (
        <AnimatedPressable accessibilityRole="button" accessibilityLabel={action.label}
          accessibilityState={{ disabled: Boolean(action.disabled), busy: Boolean(action.disabled) }}
          disabled={action.disabled} onPress={action.onPress}
          style={[styles.action, { backgroundColor: colors.accent, opacity: action.disabled ? 0.5 : 1 }]}>
          <Ionicons name={action.icon} size={19} color={colors.textOnAccent} />
          <Text style={[styles.actionText, { color: colors.textOnAccent }]}>{action.label}</Text>
        </AnimatedPressable>
      ) : null}
      {secondary ? (
        <AnimatedPressable accessibilityRole="button" accessibilityLabel={secondary.label}
          onPress={secondary.onPress} style={styles.action}>
          <Ionicons name={secondary.icon} size={18} color={colors.textSecondary} />
          <Text style={[styles.actionText, { color: colors.textSecondary }]}>{secondary.label}</Text>
        </AnimatedPressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { width: "100%", maxWidth: 420, alignSelf: "center", paddingHorizontal: 24, paddingVertical: 28, alignItems: "center", gap: 12 },
  symbol: { width: 48, height: 48, alignItems: "center", justifyContent: "center" },
  title: { ...TypeScale.heading, fontSize: 22, lineHeight: 28, letterSpacing: 0, textAlign: "center" },
  detail: { ...TypeScale.body, textAlign: "center", maxWidth: 320 },
  action: { minHeight: 48, borderRadius: 8, paddingVertical: 12, paddingHorizontal: 18, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 9, maxWidth: "100%" },
  actionText: { ...TypeScale.label, flexShrink: 1, textAlign: "center" },
});
