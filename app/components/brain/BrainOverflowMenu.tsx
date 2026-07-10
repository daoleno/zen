import React, { useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  Colors,
  Radii,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { createThemedSurfaces } from "../../constants/themedSurfaces";

type BrainMenuAction = {
  key: string;
  label: string;
  detail?: string;
  icon: React.ComponentProps<typeof Ionicons>["name"];
  disabled?: boolean;
  onPress: () => void;
};

interface BrainOverflowMenuProps {
  visible: boolean;
  actions: BrainMenuAction[];
  onClose: () => void;
}

export function BrainOverflowMenu({
  visible,
  actions,
  onClose,
}: BrainOverflowMenuProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const themed = useMemo(() => createThemedSurfaces(theme), [theme]);
  const styles = useMemo(() => createStyles(theme), [theme]);

  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="52%">
      <Text style={styles.title}>Actions</Text>
      <View style={styles.list}>
        {actions.map((action) => (
          <AnimatedPressable
            key={action.key}
            accessibilityRole="button"
            accessibilityLabel={action.label}
            disabled={action.disabled}
            preset="press"
            scale={0.98}
            style={[
              styles.row,
              {
                borderColor: themed.border,
                backgroundColor: action.disabled
                  ? colors.disabledSurface
                  : themed.surface,
              },
            ]}
            onPress={() => {
              if (action.disabled) {
                return;
              }
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              onClose();
              action.onPress();
            }}
          >
            <View style={[styles.iconWrap, { backgroundColor: colors.surfaceSubtle }]}>
              <Ionicons
                name={action.icon}
                size={18}
                color={action.disabled ? colors.disabledText : colors.textSecondary}
              />
            </View>
            <View style={styles.rowMain}>
              <Text
                style={[
                  styles.rowTitle,
                  action.disabled ? { color: colors.disabledText } : null,
                ]}
              >
                {action.label}
              </Text>
              {action.detail ? (
                <Text style={styles.rowDetail} numberOfLines={1}>
                  {action.detail}
                </Text>
              ) : null}
            </View>
            <Ionicons name="chevron-forward" size={16} color={colors.textTertiary} />
          </AnimatedPressable>
        ))}
      </View>
    </BottomSheetFrame>
  );
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  return StyleSheet.create({
    title: {
      ...UiTextMetrics,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 20,
      lineHeight: uiLineHeight(20),
      marginBottom: 14,
    },
    list: {
      gap: 8,
    },
    row: {
      minHeight: 56,
      borderRadius: Radii.md,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 14,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
    },
    iconWrap: {
      width: 34,
      height: 34,
      borderRadius: 10,
      alignItems: "center",
      justifyContent: "center",
    },
    rowMain: {
      flex: 1,
      minWidth: 0,
      gap: 2,
    },
    rowTitle: {
      ...UiTextMetrics,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    rowDetail: {
      ...UiTextMetrics,
      color: colors.textTertiary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: uiLineHeight(12),
    },
  });
}
