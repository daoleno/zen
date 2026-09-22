import React, { useCallback, useEffect, useMemo, useState } from "react";
import { ActivityIndicator, View, StyleSheet, type LayoutChangeEvent } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Reanimated, { useAnimatedStyle, useSharedValue } from "react-native-reanimated";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { SessionFileBinarySource } from "../../services/sessionFilePreview";

export function SessionFileImagePreview({
  source,
  chrome,
  onError,
}: {
  source: SessionFileBinarySource;
  chrome: TerminalThemeChrome;
  onError(): void;
}) {
  const [loading, setLoading] = useState(true);
  const scale = useSharedValue(1);
  const savedScale = useSharedValue(1);
  const translateX = useSharedValue(0);
  const translateY = useSharedValue(0);
  const savedX = useSharedValue(0);
  const savedY = useSharedValue(0);
  const width = useSharedValue(1);
  const height = useSharedValue(1);

  useEffect(() => {
    setLoading(true);
    scale.value = 1;
    savedScale.value = 1;
    translateX.value = 0;
    translateY.value = 0;
    savedX.value = 0;
    savedY.value = 0;
  }, [savedScale, savedX, savedY, scale, source.uri, translateX, translateY]);

  const pinch = useMemo(
    () =>
      Gesture.Pinch()
        .onUpdate((event) => {
          scale.value = Math.max(
            1,
            Math.min(4, savedScale.value * event.scale),
          );
        })
        .onEnd(() => {
          savedScale.value = scale.value;
          const maxX = (width.value * (scale.value - 1)) / 2;
          const maxY = (height.value * (scale.value - 1)) / 2;
          translateX.value = Math.max(-maxX, Math.min(maxX, translateX.value));
          translateY.value = Math.max(-maxY, Math.min(maxY, translateY.value));
          savedX.value = translateX.value;
          savedY.value = translateY.value;
        }),
    [height, savedScale, savedX, savedY, scale, translateX, translateY, width],
  );
  const pan = useMemo(
    () =>
      Gesture.Pan()
        .minDistance(4)
        .onUpdate((event) => {
          const maxX = (width.value * (scale.value - 1)) / 2;
          const maxY = (height.value * (scale.value - 1)) / 2;
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
    [height, savedX, savedY, scale, translateX, translateY, width],
  );
  const doubleTap = useMemo(() => Gesture.Tap().numberOfTaps(2).onEnd(() => {
    scale.value = scale.value > 1 ? 1 : 2;
    savedScale.value = scale.value;
    translateX.value = 0;
    translateY.value = 0;
    savedX.value = 0;
    savedY.value = 0;
  }), [scale, savedScale, translateX, translateY, savedX, savedY]);
  const gesture = useMemo(() => Gesture.Simultaneous(pinch, pan, doubleTap), [pan, pinch, doubleTap]);
  const imageStyle = useAnimatedStyle(() => ({
    transform: [
      { translateX: translateX.value },
      { translateY: translateY.value },
      { scale: scale.value },
    ],
  }));
  const handleLayout = useCallback(
    (event: LayoutChangeEvent) => {
      width.value = Math.max(1, event.nativeEvent.layout.width);
      height.value = Math.max(1, event.nativeEvent.layout.height);
    },
    [height, width],
  );

  return (
    <GestureDetector gesture={gesture}>
      <View
        accessibilityLabel="Zoomable image preview"
        onLayout={handleLayout}
        style={[styles.imageStage, { backgroundColor: chrome.surfaceMuted }]}
      >
        <Reanimated.Image
          source={{ uri: source.uri, headers: source.headers }}
          resizeMode="contain"
          resizeMethod="resize"
          onLoadEnd={() => setLoading(false)}
          onError={() => { setLoading(false); onError(); }}
          style={[styles.image, imageStyle]}
        />
        {loading ? <ActivityIndicator style={StyleSheet.absoluteFill} color={chrome.textMuted} /> : null}
      </View>
    </GestureDetector>
  );
}

const styles = StyleSheet.create({
  imageStage: { flex: 1, minHeight: 0, overflow: "hidden" },
  image: { width: "100%", height: "100%" },
});
