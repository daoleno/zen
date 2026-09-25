import React from "react";
import { Pressable, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { TouchTarget, shadow, useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  icon?: IoniconName;
  /** Custom glyph such as a brand mark; replaces `icon`. */
  leading?: React.ReactNode;
}

interface SegmentedControlProps<T extends string> {
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange(value: T): void;
  accessibilityLabel: string;
  style?: StyleProp<ViewStyle>;
}

/**
 * Inset segmented control for a small exclusive choice, such as appearance.
 * The selected segment is a raised thumb on a sunken track.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  accessibilityLabel,
  style,
}: SegmentedControlProps<T>) {
  const { colors, theme } = useAppTheme();
  return (
    <View
      accessibilityRole="radiogroup"
      accessibilityLabel={accessibilityLabel}
      style={[styles.track, { backgroundColor: colors.surfaceSubtle }, style]}
    >
      {options.map((option) => {
        const selected = option.value === value;
        const ink = selected ? colors.textPrimary : colors.textSecondary;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="radio"
            accessibilityLabel={option.label}
            accessibilityState={{ checked: selected, selected }}
            onPress={() => {
              if (selected) return;
              void Haptics.selectionAsync();
              onChange(option.value);
            }}
            style={({ pressed }) => [
              styles.segment,
              selected
                ? [
                    styles.selected,
                    {
                      backgroundColor: theme.isLight ? colors.bgElevated : colors.surfaceActive,
                      ...shadow("card", colors.shadowColor),
                    },
                  ]
                : null,
              pressed && !selected ? { opacity: 0.6 } : null,
            ]}
          >
            {option.leading ?? (option.icon ? <Ionicons name={option.icon} size={16} color={ink} /> : null)}
            <AppText variant="label" numberOfLines={1} style={{ color: ink }}>
              {option.label}
            </AppText>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  track: {
    flexDirection: "row",
    padding: 3,
    borderRadius: 999,
    gap: 2,
  },
  segment: {
    flex: 1,
    minHeight: TouchTarget - 6,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    paddingHorizontal: 8,
    borderRadius: 999,
  },
  selected: {},
});
