import type { Dispatch, SetStateAction } from "react";
import { useTerminalAccessoryLayout } from "../useTerminalAccessoryLayout";
import { useTerminalScreenLifecycle } from "./useTerminalScreenLifecycle";

interface UseTerminalScreenAccessoryInput {
  serverId: string;
  workerId: string;
  sessionKey: string | null;
  accessoryVisible: boolean;
  ctrlDisabled: boolean;
  setScreenFocused: Dispatch<SetStateAction<boolean>>;
}

export function useTerminalScreenAccessory({
  serverId,
  workerId,
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
    workerId,
    sessionKey,
    setScreenFocused,
    onCtrlArmedChange: accessory.handleCtrlArmedChange,
  });

  return accessory;
}
