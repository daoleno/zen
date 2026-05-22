import { useCallback, type Dispatch, type SetStateAction } from "react";
import { useTerminalFocusLifecycle } from "./useTerminalFocusLifecycle";

interface UseTerminalScreenLifecycleInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  setScreenFocused: Dispatch<SetStateAction<boolean>>;
  onCtrlArmedChange(next: boolean): void;
}

export function useTerminalScreenLifecycle({
  serverId,
  agentId,
  sessionKey,
  setScreenFocused,
  onCtrlArmedChange,
}: UseTerminalScreenLifecycleInput) {
  const handleTerminalInactive = useCallback(() => {
    onCtrlArmedChange(false);
  }, [onCtrlArmedChange]);

  useTerminalFocusLifecycle({
    serverId,
    agentId,
    sessionKey,
    setScreenFocused,
    onInactive: handleTerminalInactive,
  });
}
