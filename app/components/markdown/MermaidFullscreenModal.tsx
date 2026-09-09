import { Ionicons } from "@expo/vector-icons";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Modal, Pressable, StyleSheet, View } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, {
  useAnimatedStyle,
  useSharedValue,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { MermaidSvgPreview } from "./MermaidSvgPreview";

export function MermaidFullscreenModal({
  visible,
  svg,
  width,
  height,
  chrome,
  onClose,
}: {
  visible: boolean;
  svg: string;
  width: number;
  height: number;
  chrome: TerminalThemeChrome;
  onClose: () => void;
}) {
  const insets = useSafeAreaInsets();
  const scale = useSharedValue(1);
  const savedScale = useSharedValue(1);
  const translateX = useSharedValue(0);
  const translateY = useSharedValue(0);
  const savedX = useSharedValue(0);
  const savedY = useSharedValue(0);
  const [viewport, setViewport] = useState({ width: 1, height: 1 });

  useEffect(() => {
    if (!visible) {
      return;
    }
    scale.value = 1;
    savedScale.value = 1;
    translateX.value = 0;
    translateY.value = 0;
    savedX.value = 0;
    savedY.value = 0;
  }, [savedScale, savedX, savedY, scale, translateX, translateY, visible]);

  const pinch = useMemo(
    () =>
      Gesture.Pinch()
        .onUpdate((event) => {
          scale.value = Math.max(1, Math.min(4, savedScale.value * event.scale));
        })
        .onEnd(() => {
          savedScale.value = scale.value;
        }),
    [savedScale, scale],
  );
  const pan = useMemo(
    () =>
      Gesture.Pan()
        .minDistance(4)
        .onUpdate((event) => {
          const maxX = (viewport.width * (scale.value - 1)) / 2;
          const maxY = (viewport.height * (scale.value - 1)) / 2;
          translateX.value = Math.max(
            -maxX,
            Math.min(maxX, savedX.value + event.translationX),
          );
          translateY.value = Math.max(
            -maxY,
            Math.min(maxY, savedY.value + event.translationY),
          );
        })
        .onEnd(() => {
          savedX.value = translateX.value;
          savedY.value = translateY.value;
        }),
    [savedX, savedY, scale, translateX, translateY, viewport.height, viewport.width],
  );
  const gesture = useMemo(() => Gesture.Simultaneous(pinch, pan), [pan, pinch]);
  const imageStyle = useAnimatedStyle(() => ({
    transform: [
      { translateX: translateX.value },
      { translateY: translateY.value },
      { scale: scale.value },
    ],
  }));
  const fitted = useMemo(() => {
    const availableWidth = Math.max(1, viewport.width);
    const availableHeight = Math.max(1, viewport.height);
    const scaleToFit = Math.min(
      availableWidth / Math.max(width, 1),
      availableHeight / Math.max(height, 1),
      1,
    );
    return {
      width: Math.max(1, width * scaleToFit),
      height: Math.max(1, height * scaleToFit),
    };
  }, [height, viewport.height, viewport.width, width]);

  return (
    <Modal
      visible={visible}
      animationType="fade"
      onRequestClose={onClose}
      supportedOrientations={["portrait", "landscape"]}
    >
      <View
        style={[
          styles.root,
          {
            backgroundColor: chrome.appBackground,
            paddingTop: insets.top,
            paddingBottom: insets.bottom,
          },
        ]}
      >
        <View style={styles.toolbar}>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Close diagram"
            hitSlop={8}
            onPress={onClose}
            style={styles.close}
          >
            <Ionicons name="close" size={22} color={chrome.text} />
          </Pressable>
        </View>
        <GestureDetector gesture={gesture}>
          <View
            style={styles.stage}
            onLayout={(event) => {
              setViewport({
                width: event.nativeEvent.layout.width,
                height: event.nativeEvent.layout.height,
              });
            }}
          >
            <Animated.View style={[styles.canvas, imageStyle]}>
              <MermaidSvgPreview
                svg={svg}
                width={fitted.width}
                height={fitted.height}
                background="transparent"
              />
            </Animated.View>
          </View>
        </GestureDetector>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  toolbar: {
    height: 44,
    paddingHorizontal: 8,
    alignItems: "flex-end",
    justifyContent: "center",
  },
  close: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  stage: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
  },
  canvas: {
    alignItems: "center",
    justifyContent: "center",
  },
});
