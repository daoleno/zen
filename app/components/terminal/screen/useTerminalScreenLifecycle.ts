import { useCallback, type Dispatch, type SetStateAction } from "react";
import { useTerminalFocusLifecycle } from "./useTerminalFocusLifecycle";

interface UseTerminalScreenLifecycleInput {
  serverId: string;
  workerId: string;
  sessionKey: string | null;
  setScreenFocused: Dispatch<SetStateAction<boolean>>;
  onCtrlArmedChange(next: boolean): void;
}

export function useTerminalScreenLifecycle({
  serverId,
  workerId,
  sessionKey,
  setScreenFocused,
  onCtrlArmedChange,
}: UseTerminalScreenLifecycleInput) {
  const handleTerminalInactive = useCallback(() => {
    onCtrlArmedChange(false);
  }, [onCtrlArmedChange]);

  useTerminalFocusLifecycle({
    serverId,
    workerId,
    sessionKey,
    setScreenFocused,
    onInactive: handleTerminalInactive,
  });
}
