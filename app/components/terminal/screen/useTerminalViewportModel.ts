import { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { ConnectionState } from "../../../store/agents";
import { buildTerminalFallbackPresentation } from "./TerminalScreenModel";
import { useTerminalFallbackState } from "./useTerminalFallbackState";

interface UseTerminalViewportModelInput {
  hasTerminalRoute: boolean;
  showInterfaceChat: boolean;
  screenFocused: boolean;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  terminalTheme: TerminalThemePalette;
  chromeColors: TerminalThemeChrome;
}

/**
 * Mount vs interact policy for the Ghostty WebView surface.
 *
 * Chat must not unmount the surface: tearing it down closes the WS terminal
 * session and remounts a WebView that Android often leaves unpainted until the
 * next touch. Keep the surface mounted under the chat overlay and only gate
 * interaction / accessory chrome.
 */
export function resolveTerminalSurfaceMountPolicy(input: {
  canRenderTerminal: boolean;
  screenFocused: boolean;
  showInterfaceChat: boolean;
}): {
  shouldMountTerminalSurface: boolean;
  terminalSurfaceActive: boolean;
  accessoryVisible: boolean;
} {
  const shouldMountTerminalSurface =
    input.canRenderTerminal && input.screenFocused;
  const terminalSurfaceActive =
    shouldMountTerminalSurface && !input.showInterfaceChat;
  return {
    shouldMountTerminalSurface,
    terminalSurfaceActive,
    accessoryVisible: terminalSurfaceActive,
  };
}

export function useTerminalViewportModel({
  hasTerminalRoute,
  showInterfaceChat,
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
  const canRenderTerminal = hasTerminalRoute && !showTerminalFallback;
  const mountPolicy = resolveTerminalSurfaceMountPolicy({
    canRenderTerminal,
    screenFocused,
    showInterfaceChat,
  });
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
    accessoryVisible: mountPolicy.accessoryVisible,
    canRenderTerminal,
    shouldMountTerminalSurface: mountPolicy.shouldMountTerminalSurface,
    terminalSurfaceActive: mountPolicy.terminalSurfaceActive,
    terminalState,
  };
}
