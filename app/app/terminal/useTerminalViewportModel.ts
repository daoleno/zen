import { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { ConnectionState } from "../../store/agents";
import { buildTerminalFallbackPresentation } from "./TerminalScreenModel";
import { useTerminalFallbackState } from "./useTerminalFallbackState";

interface UseTerminalViewportModelInput {
  hasTerminalRoute: boolean;
  showCodexChat: boolean;
  screenFocused: boolean;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  terminalTheme: TerminalThemePalette;
  chromeColors: TerminalThemeChrome;
}

export function useTerminalViewportModel({
  hasTerminalRoute,
  showCodexChat,
  screenFocused,
  connectionState,
  connectionIssue,
  terminalTheme,
  chromeColors,
}: UseTerminalViewportModelInput) {
  const showTerminalFallback = useTerminalFallbackState({
    hasTerminalRoute,
    connectionState,
    connectionIssue,
  });
  const canRenderTerminal =
    hasTerminalRoute && !showTerminalFallback && !showCodexChat;
  const shouldMountTerminalSurface = canRenderTerminal && screenFocused;
  const terminalState = useMemo(
    () =>
      buildTerminalFallbackPresentation({
        hasTerminalRoute,
        connectionState,
        connectionIssue,
        terminalTheme,
        chromeColors,
      }),
    [
      chromeColors,
      connectionIssue,
      connectionState,
      hasTerminalRoute,
      terminalTheme,
    ],
  );

  return {
    accessoryVisible: canRenderTerminal && screenFocused,
    canRenderTerminal,
    shouldMountTerminalSurface,
    terminalState,
  };
}
