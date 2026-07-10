import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
} from "react";
import {
  Platform,
  Pressable,
  StyleSheet,
  Text,
  View,
  type PressableProps,
} from "react-native";
import { useTabTrigger } from "expo-router/ui";
import Animated, {
  Easing,
  ReduceMotion,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";
import { Typography, useAppColors } from "../../constants/tokens";
import {
  beginInteraction,
  type PrimaryRouteName,
  type ZenInteractionToken,
} from "../../services/interactionTrace";

const SWITCH_OPTION_WIDTH = 72;
const SWITCH_INDICATOR_INSET = 18;

interface PendingSwitchTrace {
  activated: boolean;
  target: PrimaryRouteName;
  token: ZenInteractionToken<"primary.switch">;
}

interface PrimarySwitchOptionProps {
  href?: string;
  isFocused: boolean;
  label: string;
  onLongPress: PressableProps["onLongPress"];
  onPress: PressableProps["onPress"];
  onPressIn(): void;
}

function PrimarySwitchOption({
  href,
  isFocused,
  label,
  onLongPress,
  onPress,
  onPressIn,
}: PrimarySwitchOptionProps) {
  const colors = useAppColors();
  const webHrefProps = Platform.OS === "web" ? { href } : {};
  return (
    <Pressable
      {...webHrefProps}
      onLongPress={onLongPress}
      onPress={onPress}
      onPressIn={onPressIn}
      accessibilityRole="tab"
      accessibilityLabel={label}
      accessibilityState={{ selected: isFocused }}
      aria-selected={isFocused}
      style={styles.switchButton}
    >
      <Text
        style={[
          styles.switchLabel,
          {
            color: isFocused ? colors.textPrimary : colors.textTertiary,
            fontFamily: isFocused
              ? Typography.uiFontMedium
              : Typography.uiFont,
          },
        ]}
      >
        {label}
      </Text>
    </Pressable>
  );
}

export function PrimaryTopSwitch() {
  const colors = useAppColors();
  const reducedMotion = useReducedMotion();
  const brain = useTabTrigger({ name: "brain" });
  const list = useTabTrigger({ name: "list" });
  const brainFocused = Boolean(brain.trigger?.isFocused);
  const activeRoute: PrimaryRouteName = brainFocused ? "brain" : "list";
  const indicatorPosition = useSharedValue(brainFocused ? 0 : 1);
  const pendingTraceRef = useRef<PendingSwitchTrace | null>(null);
  const outerFrameRef = useRef<number | null>(null);
  const innerFrameRef = useRef<number | null>(null);

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

  const beginSwitch = useCallback(
    (target: PrimaryRouteName) => {
      if (target === activeRoute) {
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
    [activeRoute, cancelAfterPaintFrames],
  );

  const activateSwitch = useCallback(
    (target: PrimaryRouteName) => {
      if (target === activeRoute) {
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
    [activeRoute, beginSwitch],
  );

  useEffect(() => {
    if (reducedMotion) {
      indicatorPosition.value = brainFocused ? 0 : 1;
      return;
    }
    indicatorPosition.value = withTiming(brainFocused ? 0 : 1, {
      duration: 140,
      easing: Easing.out(Easing.cubic),
      reduceMotion: ReduceMotion.System,
    });
  }, [brainFocused, indicatorPosition, reducedMotion]);

  useLayoutEffect(() => {
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
      cancelAfterPaintFrames();
      pendingTraceRef.current?.token.cancel();
      pendingTraceRef.current = null;
    },
    [cancelAfterPaintFrames],
  );

  const indicatorStyle = useAnimatedStyle(() => ({
    transform: [
      { translateX: indicatorPosition.value * SWITCH_OPTION_WIDTH },
    ],
  }));

  return (
    <View accessibilityRole="tablist" style={styles.switchRoot}>
      <Animated.View
        pointerEvents="none"
        style={[
          styles.switchIndicator,
          { backgroundColor: colors.accent },
          indicatorStyle,
        ]}
      />
      <PrimarySwitchOption
        href={brain.trigger?.resolvedHref}
        isFocused={brainFocused}
        label="Brain"
        onPressIn={() => beginSwitch("brain")}
        onPress={(event) => {
          activateSwitch("brain");
          brain.triggerProps.onPress?.(event);
        }}
        onLongPress={(event) => {
          activateSwitch("brain");
          brain.triggerProps.onLongPress?.(event);
        }}
      />
      <PrimarySwitchOption
        href={list.trigger?.resolvedHref}
        isFocused={Boolean(list.trigger?.isFocused)}
        label="List"
        onPressIn={() => beginSwitch("list")}
        onPress={(event) => {
          activateSwitch("list");
          list.triggerProps.onPress?.(event);
        }}
        onLongPress={(event) => {
          activateSwitch("list");
          list.triggerProps.onLongPress?.(event);
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
  switchIndicator: {
    position: "absolute",
    left: SWITCH_INDICATOR_INSET,
    bottom: 5,
    width: SWITCH_OPTION_WIDTH - SWITCH_INDICATOR_INSET * 2,
    height: 2,
    borderRadius: 1,
  },
});
