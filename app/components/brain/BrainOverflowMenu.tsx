import React, { useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";

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
  const styles = useMemo(() => createStyles(theme), [theme]);

  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="52%">
      <Text style={styles.title}>Actions</Text>
      <View style={styles.list}>
        {actions.map((action, index) => (
          <View key={action.key}>
            {index > 0 ? (
              <View
                style={[styles.separator, { backgroundColor: colors.borderSubtle }]}
              />
            ) : null}
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel={action.label}
              disabled={action.disabled}
              preset="press"
              scale={0.99}
              style={styles.row}
              onPress={() => {
                if (action.disabled) {
                  return;
                }
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                onClose();
                action.onPress();
              }}
            >
              <Ionicons
                name={action.icon}
                size={20}
                color={
                  action.disabled ? colors.disabledText : colors.textSecondary
                }
              />
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
                  <Text
                    style={[
                      styles.rowDetail,
                      action.disabled ? { color: colors.disabledText } : null,
                    ]}
                    numberOfLines={1}
                  >
                    {action.detail}
                  </Text>
                ) : null}
              </View>
            </AnimatedPressable>
          </View>
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
      marginBottom: 8,
    },
    list: {
      marginHorizontal: -4,
    },
    separator: {
      height: StyleSheet.hairlineWidth,
      marginLeft: 44,
    },
    row: {
      minHeight: 54,
      paddingHorizontal: 4,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 14,
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
      fontSize: 16,
      lineHeight: uiLineHeight(16),
    },
    rowDetail: {
      ...UiTextMetrics,
      color: colors.textTertiary,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: uiLineHeight(13),
    },
  });
}
