import { useEffect, useMemo, useState } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { workingTurnElapsedLabel } from "./workingTurnElapsed";

export function useElapsedDurationLabel(
  startTimestamp?: string,
  active: boolean = false,
) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!startTimestamp || !active) {
      return;
    }
    const refresh = () => {
      setNow(Date.now());
    };
    refresh();
    const timer = setInterval(refresh, 1000);
    const onAppStateChange = (nextState: AppStateStatus) => {
      if (nextState === "active") {
        refresh();
      }
    };
    const appStateSubscription = AppState.addEventListener(
      "change",
      onAppStateChange,
    );
    return () => {
      clearInterval(timer);
      appStateSubscription.remove();
    };
  }, [active, startTimestamp]);

  return useMemo(
    () =>
      workingTurnElapsedLabel({
        startedAt: startTimestamp,
        nowMs: now,
        active,
      }),
    [active, now, startTimestamp],
  );
}
