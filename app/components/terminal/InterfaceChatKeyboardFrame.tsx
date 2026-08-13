import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { AppState, Platform, StyleSheet, View } from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import {
  useGenericKeyboardHandler,
  useKeyboardContext,
} from "react-native-keyboard-controller";
import { runOnUI, runOnUIAsync } from "react-native-worklets";
import Reanimated, {
  useDerivedValue,
  useAnimatedStyle,
  useSharedValue,
  type SharedValue,
} from "react-native-reanimated";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { withAlpha } from "./colorWithAlpha";
import { useStructuredChatWindowMode } from "./chatKeyboardWindowMode";
import { useInterfaceComposerLayout } from "./useInterfaceComposerLayout";
import { StructuredChatContentFade } from "./StructuredChatContentFade";
import {
  createStructuredChatKeyboardLifecycleGate,
  dispatchStructuredChatKeyboardLifecycleEvent,
  reduceStructuredChatKeyboardLifecycleGate,
  resolveInterfaceChatCanvasColor,
  structuredChatGatedOverlayTranslateY,
  structuredChatScrollClearance,
  type StructuredChatKeyboardLifecycleDispatchResult,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

interface InterfaceChatKeyboardFrameProps {
  enabled: boolean;
  composerFocused?: boolean;
  keyboardVerticalOffset: number;
  chrome: TerminalThemeChrome;
  topChromeInset?: number;
  renderTimeline(
    extraContentPadding: SharedValue<number>,
    keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>,
  ): React.ReactNode;
  onKeyboardLifecycleInvalidate?: (reason: "route" | "app") => void;
  composer?: React.ReactNode;
  floatingAction?: React.ReactNode;
  portal?: React.ReactNode;
}

// Mirrors KeyboardProvider's platform cadence: iOS publishes destination
// geometry on start, Android publishes live geometry on move/end, and both
// publish interactive samples.
const KEYBOARD_GEOMETRY_PLATFORM = Platform.OS;

export function InterfaceChatKeyboardFrame({
  enabled,
  composerFocused = false,
  keyboardVerticalOffset,
  chrome,
  topChromeInset = 0,
  renderTimeline,
  onKeyboardLifecycleInvalidate,
  composer,
  floatingAction,
  portal,
}: InterfaceChatKeyboardFrameProps) {
  const overlayEnabled = Boolean(composer);
  const { composerHeight, handleComposerLayout } = useInterfaceComposerLayout({
    enabled: overlayEnabled,
  });
  const { reanimated } = useKeyboardContext();
  const initialKeyboardLifecycleGate =
    createStructuredChatKeyboardLifecycleGate({
      enabled,
      appActive: AppState.currentState === "active",
    });
  // KeyboardController provides raw native samples; this shared value is the
  // sole accepted lifecycle + geometry owner for both layout consumers.
  const keyboardLifecycleGate = useSharedValue(initialKeyboardLifecycleGate);
  // A handler closure captures the epoch in which it was registered. Native
  // callbacks already queued by the previous Activity/focus lifetime can then
  // be rejected instead of being credited to whichever epoch is current when
  // they finally arrive.
  const [nativeCallbackEpoch, setNativeCallbackEpoch] = useState(
    initialKeyboardLifecycleGate.epoch,
  );
  useStructuredChatWindowMode(enabled);
  const disposedRef = useRef(false);
  useEffect(() => {
    disposedRef.current = false;
    return () => {
      disposedRef.current = true;
    };
  }, []);
  const handleGateDispatch = useCallback(
    (result: StructuredChatKeyboardLifecycleDispatchResult) => {
      if (disposedRef.current) {
        return;
      }
      setNativeCallbackEpoch(result.epoch);
      if (result.invalidateReason) {
        onKeyboardLifecycleInvalidate?.(result.invalidateReason);
      }
    },
    [onKeyboardLifecycleInvalidate],
  );

  useLayoutEffect(() => {
    void runOnUIAsync(
      dispatchStructuredChatKeyboardLifecycleEvent,
      keyboardLifecycleGate,
      { type: "set_enabled", enabled },
    ).then(handleGateDispatch);
  }, [enabled, keyboardLifecycleGate, handleGateDispatch]);

  useEffect(() => {
    const updateAppState = (active: boolean) => {
      void runOnUIAsync(
        dispatchStructuredChatKeyboardLifecycleEvent,
        keyboardLifecycleGate,
        { type: "app_state", active },
      ).then(handleGateDispatch);
    };
    updateAppState(AppState.currentState === "active");
    const subscription = AppState.addEventListener("change", (state) => {
      updateAppState(state === "active");
    });
    return () => subscription.remove();
  }, [keyboardLifecycleGate, handleGateDispatch]);

  // The gate is owned by the UI runtime: composer focus and the pre-existing
  // geometry bind are scheduled there so the reduce always sees the live
  // accepted samples instead of a stale JS-side snapshot.
  useLayoutEffect(() => {
    void runOnUIAsync(
      dispatchStructuredChatKeyboardLifecycleEvent,
      keyboardLifecycleGate,
      { type: "composer_focus", focused: enabled && composerFocused },
    ).then(handleGateDispatch);
    if (!enabled || !composerFocused) {
      return;
    }
    runOnUI(dispatchComposerFocusBind)(
      keyboardLifecycleGate,
      reanimated.height,
      reanimated.progress,
    );
  }, [
    composerFocused,
    enabled,
    keyboardLifecycleGate,
    reanimated.height,
    reanimated.progress,
    handleGateDispatch,
  ]);

  useGenericKeyboardHandler(
    {
      onStart: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackEpoch,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "ios",
        );
      },
      onMove: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackEpoch,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "android",
        );
      },
      onInteractive: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackEpoch,
          event.height,
          event.progress,
          true,
        );
      },
      onEnd: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackEpoch,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "android",
        );
      },
    },
    [keyboardLifecycleGate, nativeCallbackEpoch],
  );

  const overlayTranslateY = useDerivedValue(() => {
    return structuredChatGatedOverlayTranslateY({
      gate: keyboardLifecycleGate.value,
      keyboardVerticalOffset,
    });
  }, [keyboardLifecycleGate, keyboardVerticalOffset]);
  const scrollClearance = useDerivedValue(() =>
    structuredChatScrollClearance(
      composerHeight.value,
      overlayTranslateY.value,
    ),
  );
  // The overlay transform reads the gate shared value directly instead of an
  // intermediate numeric derived value: an invalidation that lands while the
  // app is backgrounded can lose its UI->Fabric style apply, and a later
  // same-number write (0 === 0) is deduplicated so the style never re-applies.
  // Reading the gate re-triggers the style mapper on every event (each reduce
  // replaces the gate object), so a resume invalidation re-applies the
  // collapsed translation after the suspension.
  const stickyStyle = useAnimatedStyle(() => {
    return {
      transform: [
        {
          translateY: structuredChatGatedOverlayTranslateY({
            gate: keyboardLifecycleGate.value,
            keyboardVerticalOffset,
          }),
        },
      ],
    };
  }, [keyboardLifecycleGate, keyboardVerticalOffset]);
  const canvasColor = resolveInterfaceChatCanvasColor(
    chrome.appBackground,
    chrome.surface,
  );
  const transparentCanvas = withAlpha(canvasColor, 0);

  return (
    <View
      collapsable={false}
      testID="structured-chat-canvas"
      style={styles.chatCanvas}
    >
      <View collapsable={false} style={styles.timelineCanvas}>
        <StructuredChatContentFade
          canvasColor={canvasColor}
          composerHeight={composerHeight}
          overlayTranslateY={overlayTranslateY}
        >
          {renderTimeline(scrollClearance, keyboardLifecycleGate)}
        </StructuredChatContentFade>
      </View>

      <LinearGradient
        pointerEvents="none"
        colors={[canvasColor, canvasColor, transparentCanvas]}
        locations={[0, 0.68, 1]}
        style={[styles.topFade, { height: Math.max(18, topChromeInset + 18) }]}
      />

      {overlayEnabled ? (
        <Reanimated.View
          accessibilityLabel="Message composer"
          pointerEvents="box-none"
          testID="structured-chat-composer-overlay"
          style={[styles.stickyOverlay, stickyStyle]}
        >
          <View
            collapsable={false}
            onLayout={handleComposerLayout}
            style={styles.overlayContent}
          >
            {floatingAction}
            {composer}
          </View>
        </Reanimated.View>
      ) : null}

      {portal}
    </View>
  );
}

function acceptNativeKeyboardSample(
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>,
  sourceEpoch: number,
  height: number,
  progress: number,
  updatesGeometry: boolean,
) {
  "worklet";
  keyboardLifecycleGate.value = reduceStructuredChatKeyboardLifecycleGate(
    keyboardLifecycleGate.value,
    {
      type: "native_sample",
      sourceEpoch,
      height,
      progress,
      updatesGeometry,
    },
  );
}

function dispatchComposerFocusBind(
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>,
  height: SharedValue<number>,
  progress: SharedValue<number>,
) {
  "worklet";
  // Reads the live native values on the UI runtime: a JS-side read of
  // reanimated.height/progress would see the stale copy and could bind the
  // wrong geometry or miss a just-opened IME entirely.
  keyboardLifecycleGate.value = reduceStructuredChatKeyboardLifecycleGate(
    keyboardLifecycleGate.value,
    {
      type: "composer_focus_bind",
      height: height.value,
      progress: progress.value,
    },
  );
}

const styles = StyleSheet.create({
  chatCanvas: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    position: "relative",
  },
  timelineCanvas: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  stickyOverlay: {
    position: "absolute",
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 5,
  },
  overlayContent: {
    position: "relative",
    zIndex: 2,
  },
  topFade: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    height: 18,
    zIndex: 2,
  },
});
