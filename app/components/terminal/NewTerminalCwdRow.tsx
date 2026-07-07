import React, { useMemo } from "react";
import {
  Pressable,
  StyleSheet,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography, useAppColors } from "../../constants/tokens";
import { compactPathLabel } from "../../services/pathDisplay";
import { AppText } from "../ui";

interface NewTerminalCwdRowProps {
  cwd: string;
  canPickDirectory: boolean;
  onPickDirectory(): void;
}

export function NewTerminalCwdRow({
  cwd,
  canPickDirectory,
  onPickDirectory,
}: NewTerminalCwdRowProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const label = useMemo(() => formatCwdLabel(cwd), [cwd]);

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Choose working directory"
      disabled={!canPickDirectory}
      onPress={onPickDirectory}
      style={({ pressed }) => [
        styles.row,
        pressed && canPickDirectory ? styles.rowPressed : null,
        !canPickDirectory ? styles.rowDisabled : null,
      ]}
    >
      <View style={styles.iconFrame}>
        <Ionicons name="folder-outline" size={16} color={colors.textSecondary} />
      </View>
      <AppText
        variant="body"
        tone="primary"
        style={styles.path}
        numberOfLines={1}
        ellipsizeMode="head"
      >
        {label}
      </AppText>
      {canPickDirectory ? (
        <Ionicons name="chevron-forward" size={14} color={colors.disabledText} />
      ) : null}
    </Pressable>
  );
}

function formatCwdLabel(value: string): string {
  const trimmed = value.trim().replace(/\/+$/, "");
  if (!trimmed) {
    return "Default directory";
  }
  return compactPathLabel(trimmed);
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    row: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      paddingHorizontal: 12,
      paddingVertical: 8,
      borderRadius: 12,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      marginBottom: 14,
    },
    rowPressed: {
      opacity: 0.78,
    },
    rowDisabled: {
      opacity: 0.55,
    },
    iconFrame: {
      width: 28,
      height: 28,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceActive,
    },
    path: {
      flex: 1,
      minWidth: 0,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
    },
  });
}