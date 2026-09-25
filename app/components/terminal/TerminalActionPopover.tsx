import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";

/** Anchor width the chrome layout reserves for the menu button. */
export const TERMINAL_ACTION_POPOVER_WIDTH = 184;

interface TerminalActionPopoverProps {
  visible: boolean;
  /** Anchor geometry is kept for the chrome layout; the menu is a sheet. */
  left: number;
  top: number;
  title?: string;
  creatingSession: boolean;
  newTerminalLabel: string;
  newTerminalDisabled: boolean;
  showLinkedWork: boolean;
  showToggleRenderMode?: boolean;
  toggleRenderModeLabel?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onClose(): void;
  onNewTerminal(): void;
  onRename(): void;
  onOpenLinkedWork(): void;
  onOpenModel?(): void;
  onOpenDSHWeb?(): void;
  onToggleRenderMode?(): void;
  onTerminate(): void;
}

/**
 * Session actions, presented through the shared ActionMenu sheet so every
 * overflow in the app has one shape. Terminate stays last and destructive.
 */
export function TerminalActionPopover({
  visible,
  title,
  creatingSession,
  newTerminalLabel,
  newTerminalDisabled,
  showLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel = "Open terminal",
  onClose,
  onNewTerminal,
  onRename,
  onOpenLinkedWork,
  onOpenModel,
  onOpenDSHWeb,
  onToggleRenderMode,
  onTerminate,
}: TerminalActionPopoverProps) {
  const actions: ActionMenuItem[] = [];

  if (showToggleRenderMode && onToggleRenderMode) {
    actions.push({
      key: "toggle-render-mode",
      icon:
        toggleRenderModeLabel.toLowerCase().includes("chat")
          ? "chatbubble-outline"
          : "terminal-outline",
      label: toggleRenderModeLabel,
      onPress: onToggleRenderMode,
    });
  }

  actions.push(
    {
      key: "new-terminal",
      icon: "add",
      label: newTerminalLabel,
      onPress: onNewTerminal,
      disabled: creatingSession || newTerminalDisabled,
    },
    {
      key: "rename",
      icon: "create-outline",
      label: "Rename",
      onPress: onRename,
    },
  );

  if (onOpenModel) {
    actions.push({
      key: "model",
      icon: "hardware-chip-outline",
      label: "Model",
      onPress: onOpenModel,
    });
  }

  if (onOpenDSHWeb) {
    actions.push({
      key: "dsh-web",
      icon: "globe-outline",
      label: "Open Web",
      onPress: onOpenDSHWeb,
    });
  }

  if (showLinkedWork) {
    actions.push({
      key: "linked-work",
      icon: "reader-outline",
      label: "Open Brain",
      onPress: onOpenLinkedWork,
    });
  }

  actions.push({
    key: "terminate",
    icon: "stop-circle-outline",
    label: "Terminate",
    onPress: onTerminate,
    destructive: true,
  });

  return (
    <ActionMenu
      visible={visible}
      title={title}
      items={actions}
      onClose={onClose}
    />
  );
}
