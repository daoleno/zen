import React, { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import {
  Animated,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  View,
  type PressableProps,
} from "react-native";
import { Typography, useAppColors } from "../../constants/tokens";
import {
  beginInteraction,
  type PrimaryRouteName,
  type ZenInteractionToken,
} from "../../services/interactionTrace";
import { usePrimaryPagerPosition } from "./primaryPagerPosition";
import {
  applyPrimarySwitchPressIn,
  applyPrimarySwitchTap,
  primaryRoutePagerIndex,
  reconcilePrimarySwitchPending,
} from "./primarySwitchSelection";

const SWITCH_OPTION_WIDTH = 88;
const SWITCH_INDICATOR_INSET = 24;
const PAGER_POSITION_INPUT_RANGE = [0, 1] as const;

interface PendingSwitchTrace {
  activated: boolean;
  target: PrimaryRouteName;
  token: ZenInteractionToken<"primary.switch">;
}

interface PrimarySwitchOptionProps {
  activeOpacity: Animated.AnimatedInterpolation<number>;
  href?: string;
  inactiveColor: string;
  isSelected: boolean;
  label: string;
  onLongPress: PressableProps["onLongPress"];
  onPress: PressableProps["onPress"];
  onPressIn(): void;
  primaryColor: string;
}

function PrimarySwitchOption({
  activeOpacity,
  href,
  inactiveColor,
  isSelected,
  label,
  onLongPress,
  onPress,
  onPressIn,
  primaryColor,
}: PrimarySwitchOptionProps) {
  const webHrefProps = Platform.OS === "web" ? { href } : {};
  return (
    <Pressable
      {...webHrefProps}
      onLongPress={onLongPress}
      onPress={onPress}
      onPressIn={onPressIn}
      accessibilityRole="tab"
      accessibilityLabel={label}
      accessibilityState={{ selected: isSelected }}
      aria-selected={isSelected}
      style={styles.switchButton}
    >
      <Text
        numberOfLines={1}
        maxFontSizeMultiplier={1.15}
        style={[
          styles.switchLabel,
          styles.switchLabelBase,
          {
            color: inactiveColor,
            fontFamily: Typography.uiFont,
          },
        ]}
      >
        {label}
      </Text>
      <Animated.Text
        accessible={false}
        aria-hidden
        numberOfLines={1}
        maxFontSizeMultiplier={1.15}
        style={[
          styles.switchLabel,
          styles.switchLabelActive,
          {
            color: primaryColor,
            fontFamily: Typography.uiFontMedium,
            opacity: activeOpacity,
          },
        ]}
      >
        {label}
      </Animated.Text>
    </Pressable>
  );
}

export function PrimaryTopSwitch({
  activeRoute,
  onSelectRoute,
}: {
  activeRoute: PrimaryRouteName;
  onSelectRoute(route: PrimaryRouteName): void;
}) {
  const colors = useAppColors();
  const pagerPosition = usePrimaryPagerPosition();
  const fallbackPosition = useRef(
    new Animated.Value(primaryRoutePagerIndex(activeRoute)),
  ).current;
  const position = pagerPosition ?? fallbackPosition;
  const brainSelected = activeRoute === "brain";
  const listSelected = activeRoute === "list";
  const pendingRouteRef = useRef<PrimaryRouteName | null>(null);
  const pendingTraceRef = useRef<PendingSwitchTrace | null>(null);
  const outerFrameRef = useRef<number | null>(null);
  const innerFrameRef = useRef<number | null>(null);

  useLayoutEffect(() => {
    if (pagerPosition != null) {
      return;
    }
    fallbackPosition.setValue(primaryRoutePagerIndex(activeRoute));
  }, [activeRoute, fallbackPosition, pagerPosition]);

  const indicatorTranslateX = position.interpolate({
    inputRange: [...PAGER_POSITION_INPUT_RANGE],
    outputRange: [0, SWITCH_OPTION_WIDTH],
    extrapolate: "clamp",
  });
  const brainActiveOpacity = position.interpolate({
    inputRange: [...PAGER_POSITION_INPUT_RANGE],
    outputRange: [1, 0],
    extrapolate: "clamp",
  });
  const listActiveOpacity = position.interpolate({
    inputRange: [...PAGER_POSITION_INPUT_RANGE],
    outputRange: [0, 1],
    extrapolate: "clamp",
  });

  const cancelAfterPaintFrames = useCallback(() => {
    if (outerFrameRef.current != null) {
      cancelAnimationFrame(outerFrameRef.current);
      outerFrameRef.current = null;
    }
    if (innerFrameRef.current != null) {
      cancelAnimationFrame(innerFrameRef.current);
      innerFrameRef.current = null;
    }
  }, []);

  const cancelPendingTrace = useCallback(() => {
    cancelAfterPaintFrames();
    pendingTraceRef.current?.token.cancel();
    pendingTraceRef.current = null;
  }, [cancelAfterPaintFrames]);

  const beginSwitch = useCallback(
    (target: PrimaryRouteName) => {
      const pressIn = applyPrimarySwitchPressIn({
        canonicalRoute: activeRoute,
        pendingRoute: pendingRouteRef.current,
        target,
      });
      if (pressIn.cancelTrace) {
        cancelPendingTrace();
      }
      if (!pressIn.openTrace) {
        return;
      }
      cancelAfterPaintFrames();
      pendingTraceRef.current?.token.cancel();
      pendingTraceRef.current = {
        activated: false,
        target,
        token: beginInteraction("primary.switch", {
          from: activeRoute,
          to: target,
        }),
      };
    },
    [activeRoute, cancelAfterPaintFrames, cancelPendingTrace],
  );

  const activateSwitch = useCallback(
    (target: PrimaryRouteName) => {
      if (target === activeRoute) {
        cancelPendingTrace();
        return;
      }
      if (pendingTraceRef.current?.target !== target) {
        beginSwitch(target);
      }
      const pending = pendingTraceRef.current;
      if (pending == null || pending.activated) {
        return;
      }
      pending.activated = true;
      pending.token.markActivation();
      pending.token.markRelease();
    },
    [activeRoute, beginSwitch, cancelPendingTrace],
  );

  const selectRoute = useCallback(
    (target: PrimaryRouteName) => {
      const result = applyPrimarySwitchTap({
        canonicalRoute: activeRoute,
        pendingRoute: pendingRouteRef.current,
        target,
      });
      pendingRouteRef.current = result.pendingRoute;
      if (result.cancelTrace) {
        cancelPendingTrace();
      }
      if (!result.shouldNavigate) {
        return;
      }
      activateSwitch(target);
      try {
        onSelectRoute(target);
      } catch (error) {
        pendingRouteRef.current = null;
        cancelPendingTrace();
        throw error;
      }
    },
    [activateSwitch, activeRoute, cancelPendingTrace, onSelectRoute],
  );

  useLayoutEffect(() => {
    pendingRouteRef.current = reconcilePrimarySwitchPending(
      activeRoute,
      pendingRouteRef.current,
    );
    const pending = pendingTraceRef.current;
    if (pending == null || pending.target !== activeRoute) {
      return;
    }
    pending.token.markCommit();
    outerFrameRef.current = requestAnimationFrame(() => {
      outerFrameRef.current = null;
      innerFrameRef.current = requestAnimationFrame(() => {
        innerFrameRef.current = null;
        if (pendingTraceRef.current !== pending) {
          return;
        }
        pending.token.markAfterPaint();
        pending.token.end();
        pendingTraceRef.current = null;
      });
    });
  }, [activeRoute]);

  useEffect(
    () => () => {
      cancelPendingTrace();
    },
    [cancelPendingTrace],
  );

  return (
    <View accessibilityRole="tablist" style={styles.switchRoot}>
      <Animated.View
        pointerEvents="none"
        style={[
          styles.switchIndicator,
          {
            backgroundColor: colors.accent,
            transform: [{ translateX: indicatorTranslateX }],
          },
        ]}
      />
      <PrimarySwitchOption
        href="/"
        isSelected={brainSelected}
        label="Brain"
        activeOpacity={brainActiveOpacity}
        inactiveColor={colors.textTertiary}
        primaryColor={colors.textPrimary}
        onPressIn={() => beginSwitch("brain")}
        onPress={(event) => {
          event.preventDefault?.();
          selectRoute("brain");
        }}
        onLongPress={() => {
          selectRoute("brain");
        }}
      />
      <PrimarySwitchOption
        href="/list"
        isSelected={listSelected}
        label="Sessions"
        activeOpacity={listActiveOpacity}
        inactiveColor={colors.textTertiary}
        primaryColor={colors.textPrimary}
        onPressIn={() => beginSwitch("list")}
        onPress={(event) => {
          event.preventDefault?.();
          selectRoute("list");
        }}
        onLongPress={() => {
          selectRoute("list");
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  switchRoot: {
    width: SWITCH_OPTION_WIDTH * 2,
    height: 52,
    flexDirection: "row",
    alignItems: "stretch",
    justifyContent: "center",
  },
  switchButton: {
    width: SWITCH_OPTION_WIDTH,
    minHeight: 52,
    paddingHorizontal: 12,
    alignItems: "center",
    justifyContent: "center",
  },
  switchLabel: {
    fontSize: 14,
    lineHeight: 21,
  },
  switchLabelBase: {
    textAlign: "center",
  },
  switchLabelActive: {
    position: "absolute",
    textAlign: "center",
  },
  switchIndicator: {
    position: "absolute",
    left: SWITCH_INDICATOR_INSET,
    bottom: 5,
    width: SWITCH_OPTION_WIDTH - SWITCH_INDICATOR_INSET * 2,
    height: 2,
    borderRadius: 1,
  },
});
