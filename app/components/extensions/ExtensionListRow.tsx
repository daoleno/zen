import React from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import { AgentLogoSet } from "../agents/AgentLogoSet";

export interface ExtensionListRowAction {
  accessibilityLabel: string;
  icon: "trash-outline" | "lock-closed-outline";
  destructive?: boolean;
  busy?: boolean;
  disabled?: boolean;
  onPress(): void;
}

export interface ExtensionListRowProps {
  name: string;
  summary: string;
  agents: readonly string[];
  openAccessibilityLabel: string;
  action: ExtensionListRowAction;
  onOpen(): void;
}

export function ExtensionListRow({
  name,
  summary,
  agents,
  openAccessibilityLabel,
  action,
  onOpen,
}: ExtensionListRowProps) {
  const colors = useAppColors();
  const actionColor = action.destructive
    ? colors.dangerText
    : colors.textTertiary;
  return (
    <View style={[styles.row, { borderBottomColor: colors.borderSubtle }]}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={openAccessibilityLabel}
        onPress={onOpen}
        style={({ pressed }) => [
          styles.open,
          { backgroundColor: pressed ? colors.surfacePressed : "transparent" },
        ]}
      >
        <View style={styles.logo}>
          <AgentLogoSet agents={agents} maxVisible={1} size={20} />
        </View>
        <View style={styles.copy}>
          <Text
            numberOfLines={2}
            style={[styles.name, { color: colors.textPrimary }]}
          >
            {name}
          </Text>
          <Text
            numberOfLines={1}
            style={[styles.summary, { color: colors.textSecondary }]}
          >
            {summary}
          </Text>
        </View>
      </Pressable>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={action.accessibilityLabel}
        disabled={action.disabled}
        onPress={action.onPress}
        style={({ pressed }) => [
          styles.action,
          (pressed || action.disabled) && styles.dimmed,
        ]}
      >
        {action.busy ? (
          <ActivityIndicator size="small" color={actionColor} />
        ) : (
          <Ionicons name={action.icon} size={20} color={actionColor} />
        )}
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    height: 88,
    flexDirection: "row",
    alignItems: "center",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  open: {
    flex: 1,
    minWidth: 0,
    height: 88,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 9,
    paddingRight: 8,
  },
  logo: {
    width: 28,
    flexShrink: 0,
    alignItems: "center",
    justifyContent: "center",
  },
  copy: { flex: 1, minWidth: 0, justifyContent: "center" },
  name: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
  },
  summary: { ...TypeScale.compact, marginTop: 2 },
  action: {
    width: 44,
    height: 44,
    flexShrink: 0,
    alignItems: "center",
    justifyContent: "center",
  },
  dimmed: { opacity: 0.45 },
});
