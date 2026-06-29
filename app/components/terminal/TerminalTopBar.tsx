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
  menuAnchorRef,
  codexRenderMode,
  gitDiffDisabled,
  isStructuredChatAgent,
  delegated,
  onBack,
  onOpenPicker,
  onOpenGitDiff,
  onOpenMenu,
  onToggleCodexRenderMode,
}: TerminalTopBarProps) {
  const rightActions = useMemo(() => {
    const actions: Array<{
      key: string;
      icon: React.ComponentProps<typeof import("@expo/vector-icons").Ionicons>["name"];
      accessibilityLabel: string;
      disabled?: boolean;
      onPress: () => void;
    }> = [];

    if (isStructuredChatAgent) {
      actions.push({
        key: "render-mode",
        icon: codexRenderMode === "chat" ? "terminal-outline" : "chatbubble-outline",
        accessibilityLabel:
          codexRenderMode === "chat"
            ? "Open terminal renderer"
            : "Open chat renderer",
        onPress: onToggleCodexRenderMode,
      });
    }

    actions.push({
      key: "git-diff",
      icon: "git-branch-outline",
      accessibilityLabel: "Open Git diff",
      disabled: gitDiffDisabled,
      onPress: onOpenGitDiff,
    });

    actions.push({
      key: "menu",
      icon: "ellipsis-vertical",
      accessibilityLabel: "Session actions",
      onPress: onOpenMenu,
    });

    return actions;
  }, [
    codexRenderMode,
    gitDiffDisabled,
    isStructuredChatAgent,
    onOpenGitDiff,
    onOpenMenu,
    onToggleCodexRenderMode,
  ]);

  const resolvedSubtitle =
    subtitle ??
    (delegated ? "Brain session" : undefined);

  return (
    <TelegramChatHeader
      title={title}
      subtitle={resolvedSubtitle}
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