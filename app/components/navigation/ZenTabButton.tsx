import React, { forwardRef, useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import type { ComponentProps } from "react";
import type { TabTriggerSlotProps } from "expo-router/ui";
import { Ionicons } from "@expo/vector-icons";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";

/** Compact inner highlight — stadium/squircle, not a full-width pill. */
const TAB_SELECTION_RADIUS = 18;

export type ZenTabButtonProps = TabTriggerSlotProps & {
  label: string;
  icon: ComponentProps<typeof Ionicons>["name"];
  iconFocused: ComponentProps<typeof Ionicons>["name"];
};

export const ZenTabButton = forwardRef<View, ZenTabButtonProps>(
  function ZenTabButton(
    { label, icon, iconFocused, isFocused = false, style: _style, ...props },
    ref,
  ) {
    const { theme } = useAppTheme();
    const colors = theme.colors;
    const tint = isFocused ? colors.accent : colors.textTertiary;
    const styles = useMemo(() => createStyles(), []);

    return (
      <AnimatedPressable
        ref={ref}
        accessibilityRole="button"
        accessibilityState={{ selected: isFocused }}
        accessibilityLabel={label}
        preset="press"
        scale={0.97}
        style={styles.tab}
        {...props}
      >
        <View
          style={[
            styles.tabInner,
            isFocused ? styles.tabInnerSelected : null,
            isFocused ? { backgroundColor: colors.surfaceActive } : null,
          ]}
        >
          <Ionicons
            name={isFocused ? iconFocused : icon}
            size={22}
            color={tint}
          />
          <Text
            style={[
              styles.label,
              {
                color: tint,
                fontFamily: isFocused
                  ? Typography.uiFontMedium
                  : Typography.uiFont,
              },
            ]}
            numberOfLines={1}
          >
            {label}
          </Text>
        </View>
      </AnimatedPressable>
    );
  },
);

function createStyles() {
  return StyleSheet.create({
    tab: {
      flex: 1,
      minWidth: 0,
      alignItems: "center",
      justifyContent: "center",
    },
    tabInner: {
      alignSelf: "center",
      alignItems: "center",
      justifyContent: "center",
      gap: 2,
      paddingHorizontal: 10,
      paddingVertical: 6,
    },
    tabInnerSelected: {
      borderRadius: TAB_SELECTION_RADIUS,
      minWidth: 56,
      paddingHorizontal: 14,
      paddingVertical: 7,
    },
    label: {
      ...UiTextMetrics,
      fontSize: 10.5,
      lineHeight: uiLineHeight(10.5),
      textAlign: "center",
    },
  });
}