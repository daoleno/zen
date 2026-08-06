import type { Dispatch, SetStateAction } from "react";
import { useTerminalAccessoryLayout } from "../useTerminalAccessoryLayout";
import { useTerminalScreenLifecycle } from "./useTerminalScreenLifecycle";

interface UseTerminalScreenAccessoryInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  accessoryVisible: boolean;
  ctrlDisabled: boolean;
  setScreenFocused: Dispatch<SetStateAction<boolean>>;
}

export function useTerminalScreenAccessory({
  serverId,
  agentId,
  sessionKey,
  accessoryVisible,
  ctrlDisabled,
  setScreenFocused,
}: UseTerminalScreenAccessoryInput) {
  const accessory = useTerminalAccessoryLayout({
    accessoryVisible,
    ctrlResetKey: sessionKey,
    ctrlDisabled,
  });

  useTerminalScreenLifecycle({
    serverId,
    agentId,
    sessionKey,
    setScreenFocused,
    onCtrlArmedChange: accessory.handleCtrlArmedChange,
  });

  return accessory;
}
