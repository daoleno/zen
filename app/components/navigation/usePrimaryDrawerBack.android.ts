import { useEffect } from "react";
import { BackHandler } from "react-native";

interface UsePrimaryDrawerBackOptions {
  enabled: boolean;
  onBack(): void;
}

export function usePrimaryDrawerBack({
  enabled,
  onBack,
}: UsePrimaryDrawerBackOptions): void {
  useEffect(() => {
    if (!enabled) {
      return;
    }
    const subscription = BackHandler.addEventListener(
      "hardwareBackPress",
      () => {
        onBack();
        return true;
      },
    );
    return () => subscription.remove();
  }, [enabled, onBack]);
}
