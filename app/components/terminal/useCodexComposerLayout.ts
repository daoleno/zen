import { useCallback, useEffect } from "react";
import type { LayoutChangeEvent } from "react-native";
import { useSharedValue } from "react-native-reanimated";

export const DEFAULT_COMPOSER_OVERLAY_HEIGHT = 76;
const COMPOSER_HEIGHT_UPDATE_THRESHOLD = 1;

export function useCodexComposerLayout({
  enabled,
}: {
  enabled: boolean;
}) {
  const composerHeight = useSharedValue(
    enabled ? DEFAULT_COMPOSER_OVERLAY_HEIGHT : 0,
  );

  const handleComposerLayout = useCallback((event: LayoutChangeEvent) => {
    const nextHeight = Math.ceil(event.nativeEvent.layout.height);
    if (
      Math.abs(composerHeight.value - nextHeight) <=
      COMPOSER_HEIGHT_UPDATE_THRESHOLD
    ) {
      return;
    }
    composerHeight.value = nextHeight;
  }, [composerHeight]);

  useEffect(() => {
    if (!enabled) {
      composerHeight.value = 0;
    }
  }, [composerHeight, enabled]);

  return {
    composerHeight,
    handleComposerLayout,
  };
}
