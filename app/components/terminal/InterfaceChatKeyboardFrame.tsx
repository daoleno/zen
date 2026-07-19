import React, { useEffect, useLayoutEffect } from "react";
import { AppState, StyleSheet, View } from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import {
  useGenericKeyboardHandler,
  useKeyboardContext,
} from "react-native-keyboard-controller";
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
  reduceStructuredChatKeyboardLifecycleGate,
  resolveInterfaceChatCanvasColor,
  structuredChatGatedOverlayTranslateY,
  structuredChatScrollClearance,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

interface InterfaceChatKeyboardFrameProps {
  enabled: boolean;
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

export function InterfaceChatKeyboardFrame({
  enabled,
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
  // KeyboardController remains the sole geometry owner. This shared value only
  // proves whether that geometry belongs to the current app/route epoch.
  const keyboardLifecycleGate = useSharedValue(
    createStructuredChatKeyboardLifecycleGate({
      enabled,
      appActive: AppState.currentState === "active",
    }),
  );
  useStructuredChatWindowMode(enabled);

  useLayoutEffect(() => {
    const previous = keyboardLifecycleGate.value;
    const next = reduceStructuredChatKeyboardLifecycleGate(previous, {
      type: "set_enabled",
      enabled,
    });
    keyboardLifecycleGate.value = next;
    if (next.epoch !== previous.epoch && !enabled) {
      onKeyboardLifecycleInvalidate?.("route");
    }
  }, [enabled, keyboardLifecycleGate, onKeyboardLifecycleInvalidate]);

  useEffect(() => {
    const updateAppState = (active: boolean) => {
      const previous = keyboardLifecycleGate.value;
      const next = reduceStructuredChatKeyboardLifecycleGate(previous, {
        type: "app_state",
        active,
      });
      keyboardLifecycleGate.value = next;
      if (next.epoch !== previous.epoch && !active) {
        onKeyboardLifecycleInvalidate?.("app");
      }
    };
    updateAppState(AppState.currentState === "active");
    const subscription = AppState.addEventListener("change", (state) => {
      updateAppState(state === "active");
    });
    return () => subscription.remove();
  }, [keyboardLifecycleGate, onKeyboardLifecycleInvalidate]);

  useGenericKeyboardHandler(
    {
      onStart: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          event.height,
          event.progress,
        );
      },
      onMove: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          event.height,
          event.progress,
        );
      },
      onInteractive: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          event.height,
          event.progress,
        );
      },
      onEnd: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          event.height,
          event.progress,
        );
      },
    },
    [keyboardLifecycleGate],
  );

  const overlayTranslateY = useDerivedValue(() => {
    return structuredChatGatedOverlayTranslateY({
      gate: keyboardLifecycleGate.value,
      keyboardTranslation: reanimated.height.value,
      keyboardProgress: reanimated.progress.value,
      keyboardVerticalOffset,
    });
  }, [keyboardLifecycleGate, keyboardVerticalOffset]);
  const scrollClearance = useDerivedValue(() =>
    structuredChatScrollClearance(
      composerHeight.value,
      overlayTranslateY.value,
    ),
  );
  const stickyStyle = useAnimatedStyle(() => ({
    transform: [{ translateY: overlayTranslateY.value }],
  }));
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
  height: number,
  progress: number,
) {
  "worklet";
  keyboardLifecycleGate.value = reduceStructuredChatKeyboardLifecycleGate(
    keyboardLifecycleGate.value,
    {
      type: "native_sample",
      height,
      progress,
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
