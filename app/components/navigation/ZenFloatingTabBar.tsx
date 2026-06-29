import React, { useMemo } from "react";
import { Platform, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { createThemedSurfaces } from "../../constants/themedSurfaces";
import { Radii, Typography, shadow, useAppTheme } from "../../constants/tokens";

const TAB_BAR_HEIGHT = 54;
const TAB_BAR_HORIZONTAL_INSET = 16;
const TAB_BAR_BOTTOM_GAP = 10;
/** Compact inner highlight — stadium/squircle, not a full-width pill. */
const TAB_SELECTION_RADIUS = 18;

type TabRoute = {
  key: string;
  name: string;
};

type TabDescriptor = {
  options: {
    title?: string;
    tabBarAccessibilityLabel?: string;
    tabBarIcon?: (props: {
      focused: boolean;
      color: string;
      size: number;
    }) => React.ReactNode;
  };
};

export type ZenFloatingTabBarProps = {
  state: {
    index: number;
    routes: TabRoute[];
  };
  descriptors: Record<string, TabDescriptor>;
  navigation: {
    emit: (event: {
      type: "tabPress";
      target: string;
      canPreventDefault?: boolean;
    }) => { defaultPrevented: boolean };
    navigate: (name: string) => void;
  };
};

export function ZenFloatingTabBar({
  state,
  descriptors,
  navigation,
}: ZenFloatingTabBarProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const surfaces = useMemo(() => createThemedSurfaces(theme), [theme]);
  const insets = useSafeAreaInsets();
  const bottom = Math.max(insets.bottom, TAB_BAR_BOTTOM_GAP);
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
          Platform.OS === "android" ? styles.capsuleAndroid : shadow("card", colors.shadowColor),
        ]}
      >
        <View style={styles.row}>
          {state.routes.map((route, index) => {
            const focused = state.index === index;
            const { options } = descriptors[route.key];
            const label =
              options.tabBarAccessibilityLabel
              ?? options.title
              ?? route.name;
            const tint = focused ? colors.accent : colors.textTertiary;

            const onPress = () => {
              const event = navigation.emit({
                type: "tabPress",
                target: route.key,
                canPreventDefault: true,
              });
              if (!focused && !event.defaultPrevented) {
                navigation.navigate(route.name);
              }
            };

            return (
              <AnimatedPressable
                key={route.key}
                accessibilityRole="button"
                accessibilityState={{ selected: focused }}
                accessibilityLabel={label}
                preset="press"
                scale={0.97}
                style={styles.tab}
                onPress={onPress}
              >
                <View
                  style={[
                    styles.tabInner,
                    focused ? styles.tabInnerSelected : null,
                    focused
                      ? { backgroundColor: colors.surfaceActive }
                      : null,
                  ]}
                >
                  {options.tabBarIcon?.({
                    focused,
                    color: tint,
                    size: 22,
                  })}
                  <Text
                    style={[
                      styles.label,
                      {
                        color: tint,
                        fontFamily: focused
                          ? Typography.uiFontMedium
                          : Typography.uiFont,
                      },
                    ]}
                    numberOfLines={1}
                  >
                    {options.title ?? route.name}
                  </Text>
                </View>
              </AnimatedPressable>
            );
          })}
        </View>
      </View>
    </View>
  );
}

function createStyles() {
  return StyleSheet.create({
    host: {
      position: "absolute",
      left: TAB_BAR_HORIZONTAL_INSET,
      right: TAB_BAR_HORIZONTAL_INSET,
    },
    capsule: {
      minHeight: TAB_BAR_HEIGHT,
      borderRadius: Radii.pill,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 4,
      paddingVertical: 4,
      overflow: "hidden",
    },
    capsuleAndroid: {
      elevation: 6,
    },
    row: {
      flexDirection: "row",
      alignItems: "center",
    },
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
      fontSize: 10.5,
      lineHeight: 12,
      textAlign: "center",
    },
  });
}