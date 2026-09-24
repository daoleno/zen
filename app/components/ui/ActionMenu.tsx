import React from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, Radii, TouchTarget, useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { BottomSheetFrame } from "./BottomSheetFrame";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export interface ActionMenuItem {
  key: string;
  label: string;
  icon: IoniconName;
  detail?: string;
  destructive?: boolean;
  disabled?: boolean;
  onPress(): void;
}

interface ActionMenuProps {
  visible: boolean;
  title?: string;
  items: readonly ActionMenuItem[];
  onClose(): void;
}

/** Contextual action list presented as a bottom sheet. Closes before acting. */
export function ActionMenu({ visible, title, items, onClose }: ActionMenuProps) {
  const { colors, theme } = useAppTheme();
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="70%">
      {title ? (
        <AppText variant="label" tone="tertiary" accessibilityRole="header" style={styles.title}>
          {title}
        </AppText>
      ) : null}
      <View style={[styles.group, { backgroundColor: theme.materials.thin }]}>
        {items.map((item, index) => {
          const ink = item.disabled
            ? colors.disabledText
            : item.destructive
              ? colors.dangerText
              : colors.textPrimary;
          return (
            <Pressable
              key={item.key}
              disabled={item.disabled}
              accessibilityRole="button"
              accessibilityLabel={item.label}
              accessibilityHint={item.detail}
              accessibilityState={{ disabled: Boolean(item.disabled) }}
              android_ripple={{ color: colors.surfacePressed }}
              onPress={() => {
                void Haptics.selectionAsync();
                onClose();
                item.onPress();
              }}
              style={({ pressed }) => [
                styles.item,
                index > 0 && {
                  borderTopWidth: StyleSheet.hairlineWidth,
                  borderTopColor: theme.materials.separator,
                },
                pressed && { backgroundColor: colors.surfacePressed },
              ]}
            >
              <Ionicons
                name={item.icon}
                size={20}
                color={item.destructive && !item.disabled ? colors.dangerText : item.disabled ? colors.disabledText : colors.accentStrong}
              />
              <View style={styles.copy}>
                <AppText variant="body" numberOfLines={1} style={{ color: ink }}>
                  {item.label}
                </AppText>
                {item.detail ? (
                  <AppText variant="caption" tone="tertiary" numberOfLines={2}>
                    {item.detail}
                  </AppText>
                ) : null}
              </View>
            </Pressable>
          );
        })}
      </View>
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  title: {
    paddingHorizontal: 6,
    paddingBottom: 10,
  },
  group: {
    borderRadius: Radii.xl,
    ...ContinuousCorners,
    overflow: "hidden",
  },
  item: {
    minHeight: Math.max(TouchTarget, 54),
    flexDirection: "row",
    alignItems: "center",
    gap: 14,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
});
