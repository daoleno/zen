import React, { useMemo, type ReactNode } from "react";
import { Platform, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { createThemedSurfaces } from "../../constants/themedSurfaces";
import { Radii, shadow, useAppTheme } from "../../constants/tokens";
import {
  FLOATING_TAB_BAR_BOTTOM_GAP,
  FLOATING_TAB_BAR_HEIGHT,
  FLOATING_TAB_BAR_HORIZONTAL_INSET,
} from "./floatingTabBarMetrics";

type ZenFloatingTabListProps = {
  children: ReactNode;
};

export function ZenFloatingTabList({ children }: ZenFloatingTabListProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const surfaces = useMemo(() => createThemedSurfaces(theme), [theme]);
  const insets = useSafeAreaInsets();
  const bottom = Math.max(insets.bottom, FLOATING_TAB_BAR_BOTTOM_GAP);
  const styles = useMemo(() => createStyles(), []);

  const capsuleBackground =
    theme.colorScheme === "dark"
      ? "rgba(28,28,30,0.92)"
      : "rgba(255,255,255,0.94)";

  return (
    <View pointerEvents="box-none" style={[styles.host, { bottom }]}>
      <View
        style={[
          styles.capsule,
          {
            backgroundColor: capsuleBackground,
            borderColor: surfaces.border,
          },
          Platform.OS === "android"
            ? styles.capsuleAndroid
            : shadow("card", colors.shadowColor),
        ]}
      >
        <View style={styles.row}>{children}</View>
      </View>
    </View>
  );
}

function createStyles() {
  return StyleSheet.create({
    host: {
      position: "absolute",
      left: FLOATING_TAB_BAR_HORIZONTAL_INSET,
      right: FLOATING_TAB_BAR_HORIZONTAL_INSET,
    },
    capsule: {
      minHeight: FLOATING_TAB_BAR_HEIGHT,
      borderRadius: Radii.pill,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 4,
      paddingVertical: 5,
      overflow: "hidden",
    },
    capsuleAndroid: {
      elevation: 6,
    },
    row: {
      flexDirection: "row",
      alignItems: "center",
    },
  });
}