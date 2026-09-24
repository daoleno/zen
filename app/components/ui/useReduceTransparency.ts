import { useEffect, useState } from "react";
import { AccessibilityInfo } from "react-native";

/**
 * Follows the system Reduce Transparency setting (iOS; Android reports false).
 * Materials render opaque while it is on.
 */
export function useReduceTransparency(): boolean {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    let cancelled = false;
    AccessibilityInfo.isReduceTransparencyEnabled?.()
      .then((value) => {
        if (!cancelled) setEnabled(value);
      })
      .catch(() => undefined);
    const subscription = AccessibilityInfo.addEventListener?.(
      "reduceTransparencyChanged",
      setEnabled,
    );
    return () => {
      cancelled = true;
      subscription?.remove();
    };
  }, []);
  return enabled;
}
