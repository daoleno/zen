import { useEffect, useState } from "react";
import { AppState, type AppStateStatus } from "react-native";
import {
  elapsedNowForRender,
  workingTurnElapsedLabels,
} from "./workingTurnElapsed";

export function useElapsedDurationLabels(
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

  // The button instance morphs between Send and Stop while this hook keeps the
  // authoritative turn clock alive. Sampling wall time during render also
  // prevents a stale elapsed frame if React paints before the effect refreshes.
  return workingTurnElapsedLabels({
    startedAt: startTimestamp,
    nowMs: elapsedNowForRender(now, Date.now(), active),
    active,
  });
}
