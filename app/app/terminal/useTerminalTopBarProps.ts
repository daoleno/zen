import { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { AgentKind } from "../../services/agentPresentation";
import type { StoredCodexRenderMode } from "../../services/storage";
import type { TerminalTopBarProps } from "../../components/terminal/TerminalTopBar";
import type { TerminalGitDiffSummary } from "../../components/terminal/useTerminalGitDiff";
import type { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalTopBarPropsInput {
  title: string;
  kind: AgentKind;
  terminalTheme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  chromeLayout: Pick<
    ReturnType<typeof useTerminalChromeLayout>,
    "menuAnchorRef" | "openMenu"
  >;
  navigationActions: Pick<
    ReturnType<typeof useTerminalNavigationActions>,
    "goToInbox"
  >;
  codexRenderMode: StoredCodexRenderMode;
  gitDiffDisabled: boolean;
  gitDiffSummary: TerminalGitDiffSummary | null;
  isCodexAgent: boolean;
  onOpenPicker(): void;
  openGitDiff(): void;
  onToggleCodexRenderMode(): void;
}

export function useTerminalTopBarProps({
  title,
  kind,
  terminalTheme,
  chrome,
  chromeLayout,
  navigationActions,
  codexRenderMode,
  gitDiffDisabled,
  gitDiffSummary,
  isCodexAgent,
  onOpenPicker,
  openGitDiff,
  onToggleCodexRenderMode,
}: UseTerminalTopBarPropsInput): TerminalTopBarProps {
  return useMemo(
    () => ({
      title,
      kind,
      backgroundColor: terminalTheme.background,
      chrome,
      menuAnchorRef: chromeLayout.menuAnchorRef,
      codexRenderMode,
      gitDiffDisabled,
      gitDiffPresentation: buildGitDiffPresentation({
        chrome,
        disabled: gitDiffDisabled,
        summary: gitDiffSummary,
        terminalTheme,
      }),
      isCodexAgent,
      onBack: navigationActions.goToInbox,
      onOpenPicker,
      onOpenGitDiff: openGitDiff,
      onOpenMenu: chromeLayout.openMenu,
      onToggleCodexRenderMode,
    }),
    [
      title,
      kind,
      chrome,
      chromeLayout.menuAnchorRef,
      chromeLayout.openMenu,
      codexRenderMode,
      gitDiffDisabled,
      gitDiffSummary,
      isCodexAgent,
      onOpenPicker,
      openGitDiff,
      onToggleCodexRenderMode,
      navigationActions.goToInbox,
      terminalTheme.background,
      terminalTheme.green,
      terminalTheme.red,
      terminalTheme.yellow,
    ],
  );
}

function buildGitDiffPresentation({
  chrome,
  disabled,
  summary,
  terminalTheme,
}: {
  chrome: TerminalThemeChrome;
  disabled: boolean;
  summary: TerminalGitDiffSummary | null;
  terminalTheme: TerminalThemePalette;
}): TerminalTopBarProps["gitDiffPresentation"] {
  if (!summary) {
    return {
      accessibilityLabel: "Open Git diff",
      backgroundColor: "transparent",
      iconColor: disabled ? chrome.textSubtle : chrome.textMuted,
    };
  }

  const toneColor =
    summary.tone === "clean"
      ? chrome.textMuted
      : summary.tone === "dirty"
        ? terminalTheme.yellow
        : summary.tone === "error"
          ? terminalTheme.red
          : chrome.textMuted;
  const statsLabel = summary.showStats
    ? `+${formatGitDelta(summary.additions)} -${formatGitDelta(summary.deletions)}`
    : "";

  return {
    accessibilityLabel: [
      "Open Git diff",
      summary.label,
      statsLabel,
    ].filter(Boolean).join(", "),
    additionsColor: terminalTheme.green,
    additionsText: summary.showStats
      ? `+${formatGitDelta(summary.additions)}`
      : undefined,
    backgroundColor: "transparent",
    deletionsColor: terminalTheme.red,
    deletionsText: summary.showStats
      ? `-${formatGitDelta(summary.deletions)}`
      : undefined,
    iconColor: disabled ? chrome.textSubtle : toneColor,
  };
}

function formatGitDelta(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0";
  }
  if (value < 1000) {
    return String(value);
  }
  if (value < 10000) {
    return `${(value / 1000).toFixed(1).replace(/\.0$/, "")}k`;
  }
  return `${Math.round(value / 1000)}k`;
}
