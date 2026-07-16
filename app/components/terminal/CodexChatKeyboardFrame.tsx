import React from "react";
import { StyleSheet, View } from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import { useKeyboardContext } from "react-native-keyboard-controller";
import Reanimated, {
  useDerivedValue,
  useAnimatedStyle,
  type SharedValue,
} from "react-native-reanimated";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { withAlpha } from "./gitDiffColor";
import { useStructuredChatWindowMode } from "./chatKeyboardWindowMode";
import { useCodexComposerLayout } from "./useCodexComposerLayout";
import { StructuredChatContentFade } from "./StructuredChatContentFade";
import {
  structuredChatOverlayTranslateY,
  structuredChatScrollClearance,
} from "./chatKeyboardOverlayPolicy";

interface CodexChatKeyboardFrameProps {
  enabled: boolean;
  keyboardVerticalOffset: number;
  chrome: TerminalThemeChrome;
  topChromeInset?: number;
  renderTimeline(extraContentPadding: SharedValue<number>): React.ReactNode;
  composer?: React.ReactNode;
  floatingAction?: React.ReactNode;
  portal?: React.ReactNode;
}

export function CodexChatKeyboardFrame({
  enabled,
  keyboardVerticalOffset,
  chrome,
  topChromeInset = 0,
  renderTimeline,
  composer,
  floatingAction,
  portal,
}: CodexChatKeyboardFrameProps) {
  const overlayEnabled = Boolean(composer);
  const { composerHeight, handleComposerLayout } = useCodexComposerLayout({
    enabled: overlayEnabled,
  });
  const { reanimated } = useKeyboardContext();
  useStructuredChatWindowMode(enabled);

  const overlayTranslateY = useDerivedValue(() => {
    if (!enabled) {
      return 0;
    }
    return structuredChatOverlayTranslateY(
      reanimated.height.value,
      reanimated.progress.value,
      keyboardVerticalOffset,
    );
  }, [enabled, keyboardVerticalOffset]);
  const scrollClearance = useDerivedValue(() =>
    structuredChatScrollClearance(
      composerHeight.value,
      overlayTranslateY.value,
    ),
  );
  const stickyStyle = useAnimatedStyle(() => ({
    transform: [{ translateY: overlayTranslateY.value }],
  }));
  const canvasColor = chrome.appBackground === "transparent"
    ? chrome.surface
    : chrome.appBackground;
  const transparentCanvas = withAlpha(canvasColor, 0);

  return (
    <View
      collapsable={false}
      testID="structured-chat-canvas"
      style={styles.chatCanvas}
    >
      <View collapsable={false} style={styles.timelineCanvas}>
        <StructuredChatContentFade
          composerHeight={composerHeight}
          overlayTranslateY={overlayTranslateY}
        >
          {renderTimeline(scrollClearance)}
        </StructuredChatContentFade>
      </View>

      <LinearGradient
        pointerEvents="none"
        colors={[canvasColor, canvasColor, transparentCanvas]}
        locations={[0, 0.68, 1]}
        style={[
          styles.topFade,
          { height: Math.max(18, topChromeInset + 18) },
        ]}
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
