import { useWindowDimensions } from "react-native";
import {
  TERMINAL_ACTION_POPOVER_WIDTH,
} from "../TerminalActionPopover";
import { useTerminalChromeLayout } from "./useTerminalChromeLayout";

interface UseTerminalScreenChromeInput {
  sessionKey: string | null;
}

export function useTerminalScreenChrome({
  sessionKey,
}: UseTerminalScreenChromeInput) {
  const { width: windowWidth } = useWindowDimensions();
  return useTerminalChromeLayout({
    sessionKey,
    windowWidth,
    popoverWidth: TERMINAL_ACTION_POPOVER_WIDTH,
  });
}
