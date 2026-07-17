import React, { forwardRef, useCallback } from "react";
import {
  Platform,
  StyleSheet,
  type ScrollViewProps,
} from "react-native";
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
  structuredChatEffectiveClearance,
  structuredChatLatestOffset,
  type StructuredChatInsetPlatform,
} from "./chatKeyboardOverlayPolicy";

const INSET_PLATFORM: StructuredChatInsetPlatform =
  Platform.OS === "ios"
    ? "ios"
    : Platform.OS === "android"
      ? "android"
      : "web";

const ReanimatedClippingScrollView =
  Platform.OS === "android"
    ? Reanimated.createAnimatedComponent(ClippingScrollView)
    : ClippingScrollView;

interface StructuredChatInsetScrollViewProps extends ScrollViewProps {
  clearance: SharedValue<number>;
  inverted?: boolean;
  onLatestOffsetChange(offset: number): void;
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
    contentContainerStyle,
    contentInset,
    inverted = false,
    onLatestOffsetChange,
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
    () => ({
      rawOffset: rawScrollOffset.value,
      requestedClearance: clearance.value,
    }),
    ({ rawOffset, requestedClearance }) => {
      effectiveClearance.value = structuredChatEffectiveClearance({
        platform: INSET_PLATFORM,
        requestedClearance,
        rawOffset,
        previousClearance: effectiveClearance.value,
      });
    },
    [clearance],
  );

  useAnimatedReaction(
    () => structuredChatLatestOffset(
      effectiveClearance.value,
      INSET_PLATFORM,
    ),
    (current, previous) => {
      if (current === previous) {
        return;
      }
      runOnJS(onLatestOffsetChange)(current);
    },
    [onLatestOffsetChange],
  );

  const animatedProps = useAnimatedProps(() => {
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
    scrollIndicatorInsets?.bottom,
    scrollIndicatorInsets?.left,
    scrollIndicatorInsets?.right,
    scrollIndicatorInsets?.top,
  ]);
  const webContentInsetStyle = useAnimatedStyle(() => ({
    paddingTop: inverted
      ? effectiveClearance.value
      : undefined,
    paddingBottom: inverted
      ? undefined
      : effectiveClearance.value,
  }), [inverted]);

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
      ) : children}
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
