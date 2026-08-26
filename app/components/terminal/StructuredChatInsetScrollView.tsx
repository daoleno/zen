import React, { forwardRef, useCallback } from "react";
import { Platform, StyleSheet, type ScrollViewProps } from "react-native";
import { ClippingScrollView } from "react-native-keyboard-controller";
import Reanimated, {
  runOnJS,
  useAnimatedProps,
  useAnimatedReaction,
  useAnimatedRef,
  useAnimatedStyle,
  useScrollOffset,
  useSharedValue,
  type SharedValue,
} from "react-native-reanimated";
import {
  structuredChatEffectiveClearanceForKeyboardLifecycle,
  structuredChatFocusSample,
  type StructuredChatInsetPlatform,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

const INSET_PLATFORM: StructuredChatInsetPlatform =
  Platform.OS === "ios" ? "ios" : Platform.OS === "android" ? "android" : "web";

const ReanimatedClippingScrollView =
  Platform.OS === "android"
    ? Reanimated.createAnimatedComponent(ClippingScrollView)
    : ClippingScrollView;

interface StructuredChatInsetScrollViewProps extends ScrollViewProps {
  clearance: SharedValue<number>;
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>;
  clearanceObservationRequest?: SharedValue<number>;
  inverted?: boolean;
  onClearanceChange(
    intentToken: number,
    clearance: number,
    latestOffset: number,
  ): void;
}

/**
 * Extends the timeline's range without owning keyboard mode or issuing scroll
 * commands. The effective inset retains any portion currently occupied by the
 * reader, so an IME or Composer contraction cannot clamp the visible anchor.
 */
export const StructuredChatInsetScrollView = forwardRef<
  Reanimated.ScrollView,
  React.PropsWithChildren<StructuredChatInsetScrollViewProps>
>(function StructuredChatInsetScrollView(
  {
    children,
    clearance,
    keyboardLifecycleGate,
    clearanceObservationRequest,
    contentContainerStyle,
    contentInset,
    inverted = false,
    onClearanceChange,
    scrollIndicatorInsets,
    ...rest
  },
  forwardedRef,
) {
  const scrollRef = useAnimatedRef<Reanimated.ScrollView>();
  const rawScrollOffset = useScrollOffset(scrollRef);
  const effectiveClearance = useSharedValue(0);
  const setCombinedRef = useCallback(
    (value: Reanimated.ScrollView | null) => {
      scrollRef(value);
      if (!forwardedRef) {
        return;
      }
      if (typeof forwardedRef === "function") {
        forwardedRef(value);
        return;
      }
      forwardedRef.current = value;
    },
    [forwardedRef, scrollRef],
  );

  useAnimatedReaction(
    () => {
      const nextClearance =
        structuredChatEffectiveClearanceForKeyboardLifecycle({
          gate: keyboardLifecycleGate.value,
          platform: INSET_PLATFORM,
          requestedClearance: clearance.value,
          rawOffset: rawScrollOffset.value,
          previousClearance: effectiveClearance.value,
        });
      const intentToken = clearanceObservationRequest?.value ?? 0;
      return {
        clearance: nextClearance,
        focusSample:
          intentToken > 0
            ? structuredChatFocusSample(
                intentToken,
                nextClearance,
                INSET_PLATFORM,
              )
            : null,
      };
    },
    (current, previous) => {
      effectiveClearance.value = current.clearance;
      const currentSample = current.focusSample;
      const previousSample = previous?.focusSample;
      if (
        currentSample == null ||
        (currentSample.intentToken === previousSample?.intentToken &&
          currentSample.clearance === previousSample.clearance &&
          currentSample.latestOffset === previousSample.latestOffset)
      ) {
        return;
      }
      runOnJS(onClearanceChange)(
        currentSample.intentToken,
        currentSample.clearance,
        currentSample.latestOffset,
      );
    },
    [
      clearance,
      clearanceObservationRequest,
      keyboardLifecycleGate,
      onClearanceChange,
    ],
  );

  const animatedProps = useAnimatedProps(() => {
    // A foreground revision must remap every native inset owner even when the
    // contracted numbers equal the previous closed state.
    keyboardLifecycleGate.value.revision;
    const dynamicTop = inverted ? effectiveClearance.value : 0;
    const dynamicBottom = inverted ? 0 : effectiveClearance.value;
    return {
      contentInset: {
        top: dynamicTop + (contentInset?.top || 0),
        bottom: dynamicBottom + (contentInset?.bottom || 0),
        left: contentInset?.left || 0,
        right: contentInset?.right || 0,
      },
      scrollIndicatorInsets: {
        top: dynamicTop + (scrollIndicatorInsets?.top || 0),
        bottom: dynamicBottom + (scrollIndicatorInsets?.bottom || 0),
        left: scrollIndicatorInsets?.left,
        right: scrollIndicatorInsets?.right,
      },
      contentInsetTop: dynamicTop,
      contentInsetBottom: dynamicBottom,
    };
  }, [
    contentInset?.bottom,
    contentInset?.left,
    contentInset?.right,
    contentInset?.top,
    inverted,
    keyboardLifecycleGate,
    scrollIndicatorInsets?.bottom,
    scrollIndicatorInsets?.left,
    scrollIndicatorInsets?.right,
    scrollIndicatorInsets?.top,
  ]);
  const webContentInsetStyle = useAnimatedStyle(() => {
    keyboardLifecycleGate.value.revision;
    return {
      paddingTop: inverted ? effectiveClearance.value : undefined,
      paddingBottom: inverted ? undefined : effectiveClearance.value,
    };
  }, [inverted, keyboardLifecycleGate]);

  const scrollView = (
    <Reanimated.ScrollView
      ref={setCombinedRef}
      animatedProps={animatedProps}
      contentContainerStyle={contentContainerStyle}
      {...rest}
    >
      {Platform.OS === "web" ? (
        <Reanimated.View style={[styles.webContent, webContentInsetStyle]}>
          {children}
        </Reanimated.View>
      ) : (
        children
      )}
    </Reanimated.ScrollView>
  );

  if (Platform.OS === "web") {
    return scrollView;
  }

  return (
    <ReanimatedClippingScrollView
      animatedProps={animatedProps}
      style={styles.container}
    >
      {scrollView}
    </ReanimatedClippingScrollView>
  );
});

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  webContent: {
    flexGrow: 1,
  },
});
