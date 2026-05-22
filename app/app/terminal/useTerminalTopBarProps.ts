import { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { TerminalTopBarProps } from "../../components/terminal/TerminalTopBar";
import type { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import type { useTerminalTabActions } from "./useTerminalTabActions";

interface UseTerminalTopBarPropsInput {
  tabs: TerminalTopBarProps["tabs"];
  terminalTheme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  chromeLayout: Pick<
    ReturnType<typeof useTerminalChromeLayout>,
    "handleTabLayout" | "menuAnchorRef" | "openMenu" | "tabScrollRef"
  >;
  tabActions: Pick<
    ReturnType<typeof useTerminalTabActions>,
    "goToInbox" | "openAgentTab"
  >;
  openNewTerminal(): void;
}

export function useTerminalTopBarProps({
  tabs,
  terminalTheme,
  chrome,
  chromeLayout,
  tabActions,
  openNewTerminal,
}: UseTerminalTopBarPropsInput): TerminalTopBarProps {
  return useMemo(
    () => ({
      tabs,
      backgroundColor: terminalTheme.background,
      chrome,
      tabScrollRef: chromeLayout.tabScrollRef,
      menuAnchorRef: chromeLayout.menuAnchorRef,
      onBack: tabActions.goToInbox,
      onOpenTab: tabActions.openAgentTab,
      onOpenMenu: chromeLayout.openMenu,
      onNewTerminal: openNewTerminal,
      onTabLayout: chromeLayout.handleTabLayout,
    }),
    [
      chrome,
      chromeLayout.handleTabLayout,
      chromeLayout.menuAnchorRef,
      chromeLayout.openMenu,
      chromeLayout.tabScrollRef,
      openNewTerminal,
      tabActions.goToInbox,
      tabActions.openAgentTab,
      tabs,
      terminalTheme.background,
    ],
  );
}
