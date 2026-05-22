import { useCallback, useEffect, useState } from "react";
import type { LayoutChangeEvent } from "react-native";

const DEFAULT_COMPOSER_HEIGHT = 76;
const COMPOSER_HEIGHT_UPDATE_THRESHOLD = 1;

export function useCodexComposerLayout({
  onHeightChange,
}: {
  onHeightChange(height: number): void;
}) {
  const [composerHeight, setComposerHeight] = useState(DEFAULT_COMPOSER_HEIGHT);

  const handleComposerLayout = useCallback((event: LayoutChangeEvent) => {
    const nextHeight = Math.ceil(event.nativeEvent.layout.height);
    setComposerHeight((previous) => {
      if (Math.abs(previous - nextHeight) <= COMPOSER_HEIGHT_UPDATE_THRESHOLD) {
        return previous;
      }
      return nextHeight;
    });
  }, []);

  useEffect(() => {
    onHeightChange(composerHeight);
  }, [composerHeight, onHeightChange]);

  return {
    composerHeight,
    handleComposerLayout,
  };
}
