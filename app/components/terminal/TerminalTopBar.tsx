import React, { useMemo } from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentKind } from "../../services/agentPresentation";
import type { TerminalFlavor } from "../../services/terminalFlavor";
import type { StoredCodexRenderMode } from "../../services/storage";
import { TelegramChatHeader } from "./TelegramChatHeader";

export interface TerminalTopBarGitDiffPresentation {
  accessibilityLabel: string;
  backgroundColor: string;
  iconColor: string;
  additionsText?: string;
  additionsColor?: string;
  deletionsText?: string;
  deletionsColor?: string;
}

export interface TerminalTopBarProps {
  title: string;
  subtitle?: string;
  kind: AgentKind;
  terminalFlavor?: TerminalFlavor;
  backgroundColor: string;
  chrome: TerminalThemeChrome;
  menuAnchorRef: React.RefObject<import("react-native").View | null>;
  codexRenderMode: StoredCodexRenderMode;
  gitDiffDisabled: boolean;
  gitDiffPresentation: TerminalTopBarGitDiffPresentation;
  isStructuredChatAgent: boolean;
  delegated?: boolean;
  onBack(): void;
  onOpenPicker(): void;
  onOpenGitDiff(): void;
  onOpenMenu(): void;
  onToggleCodexRenderMode(): void;
}

export function TerminalTopBar({
  title,
  subtitle,
  kind,
  terminalFlavor,
  chrome,
  menuAnchorRef,
  gitDiffDisabled,
  gitDiffPresentation,
  delegated,
  onBack,
  onOpenPicker,
  onOpenGitDiff,
  onOpenMenu,
}: TerminalTopBarProps) {
  // Terminal/chat toggle lives in the overflow menu; header keeps Git + menu
  // together in one circular chip for a cleaner Telegram-like chrome.
  const rightActions = useMemo(
    () => [
      {
        key: "git-diff",
        icon: "git-branch-outline" as const,
        accessibilityLabel: gitDiffPresentation.accessibilityLabel,
        disabled: gitDiffDisabled,
        iconColor: gitDiffPresentation.iconColor,
        onPress: onOpenGitDiff,
      },
      {
        key: "menu",
        icon: "ellipsis-vertical" as const,
        accessibilityLabel: "Session actions",
        onPress: onOpenMenu,
      },
    ],
    [
      gitDiffDisabled,
      gitDiffPresentation.accessibilityLabel,
      gitDiffPresentation.iconColor,
      onOpenGitDiff,
      onOpenMenu,
    ],
  );

  return (
    <TelegramChatHeader
      chrome={chrome}
      title={title}
      subtitle={subtitle ?? (delegated ? "Delegated" : undefined)}
      agentKind={kind}
      terminalFlavor={terminalFlavor}
      avatarLabel={title}
      avatarSeed={title}
      onBack={onBack}
      onPressTitle={onOpenPicker}
      rightActions={rightActions}
      menuAnchorRef={menuAnchorRef}
    />
  );
}
