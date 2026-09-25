import React, { forwardRef, type ReactNode } from "react";
import {
  Pressable,
  StyleSheet,
  View,
  type PressableProps,
  type View as ViewInstance,
} from "react-native";
import { useAppColors } from "../../constants/tokens";
import { GlassSurface } from "../ui/GlassSurface";

export const PRIMARY_CHROME_BUTTON_SIZE = 40;

interface PrimaryChromeButtonProps
  extends Omit<PressableProps, "style" | "children"> {
  children: ReactNode;
  /** Small status dot on the top-trailing edge, e.g. server connection. */
  badgeColor?: string | null;
}

/**
 * Circular glass control for the primary app bar. The glass disc is 40pt;
 * the pressable keeps a 52pt slot so the touch target exceeds 44pt.
 */
export const PrimaryChromeButton = forwardRef<ViewInstance, PrimaryChromeButtonProps>(
  function PrimaryChromeButton({ children, badgeColor, disabled, ...pressableProps }, ref) {
    const colors = useAppColors();
    return (
      <Pressable
        ref={ref}
        hitSlop={6}
        disabled={disabled}
        {...pressableProps}
        style={({ pressed }) => [
          styles.slot,
          pressed && !disabled ? styles.pressed : null,
        ]}
      >
        <GlassSurface
          material="chrome"
          radius={PRIMARY_CHROME_BUTTON_SIZE / 2}
          elevation="card"
          interactive
          style={styles.disc}
        >
          {children}
        </GlassSurface>
        {badgeColor ? (
          <View
            pointerEvents="none"
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
            style={[
              styles.badge,
              { backgroundColor: badgeColor, borderColor: colors.bgPrimary },
            ]}
          />
        ) : null}
      </Pressable>
    );
  },
);

const styles = StyleSheet.create({
  slot: {
    width: 52,
    minWidth: 44,
    minHeight: 52,
    alignItems: "center",
    justifyContent: "center",
  },
  disc: {
    width: PRIMARY_CHROME_BUTTON_SIZE,
    height: PRIMARY_CHROME_BUTTON_SIZE,
    alignItems: "center",
    justifyContent: "center",
  },
  pressed: {
    opacity: 0.6,
    transform: [{ scale: 0.94 }],
  },
  badge: {
    position: "absolute",
    top: 8,
    right: 8,
    width: 10,
    height: 10,
    borderRadius: 5,
    borderWidth: 2,
  },
});
