import React, { useMemo } from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentKind } from "../../services/agentPresentation";
import type { TerminalFlavor } from "../../services/terminalFlavor";
import type { StoredInterfaceRenderMode } from "../../services/storage";
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
  interfaceRenderMode: StoredInterfaceRenderMode;
  gitDiffDisabled: boolean;
  gitDiffPresentation: TerminalTopBarGitDiffPresentation;
  isStructuredChatAgent: boolean;
  delegated?: boolean;
  onBack(): void;
  onOpenSessionDetails(): void;
  onOpenGitDiff(): void;
  onOpenMenu(): void;
  onToggleInterfaceRenderMode(): void;
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
  onOpenSessionDetails,
  onOpenGitDiff,
  onOpenMenu,
}: TerminalTopBarProps) {
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
      onPressTitle={onOpenSessionDetails}
      rightActions={rightActions}
      menuAnchorRef={menuAnchorRef}
    />
  );
}
