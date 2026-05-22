import React from "react";
import {
  Modal,
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  TERMINAL_ACTION_MENU_WIDTH,
  TerminalActionMenu,
  type TerminalActionMenuItem,
  type TerminalSheetActionIcon,
} from "./TerminalActionMenu";

export const TERMINAL_ACTION_POPOVER_WIDTH = TERMINAL_ACTION_MENU_WIDTH;

interface TerminalActionPopoverProps {
  visible: boolean;
  left: number;
  top: number;
  creatingSession: boolean;
  newTerminalLabel: string;
  newTerminalDisabled: boolean;
  gitDiffDisabled: boolean;
  activePinned: boolean;
  closeOtherTabsDisabled: boolean;
  codexRenderAction?: {
    icon: TerminalSheetActionIcon;
    label: string;
    onPress(): void;
  } | null;
  showLinkedWork: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onClose(): void;
  onNewTerminal(): void;
  onOpenGitDiff(): void;
  onRename(): void;
  onTogglePinned(): void;
  onCloseOtherTabs(): void;
  onCloseTab(): void;
  onOpenLinkedWork(): void;
  onTerminate(): void;
}

export function TerminalActionPopover({
  visible,
  left,
  top,
  creatingSession,
  newTerminalLabel,
  newTerminalDisabled,
  gitDiffDisabled,
  activePinned,
  closeOtherTabsDisabled,
  codexRenderAction,
  showLinkedWork,
  chrome,
  theme,
  onClose,
  onNewTerminal,
  onOpenGitDiff,
  onRename,
  onTogglePinned,
  onCloseOtherTabs,
  onCloseTab,
  onOpenLinkedWork,
  onTerminate,
}: TerminalActionPopoverProps) {
  const actions: TerminalActionMenuItem[] = [
    {
      key: "new-terminal",
      icon: "add",
      label: newTerminalLabel,
      onPress: onNewTerminal,
      disabled: creatingSession || newTerminalDisabled,
    },
    {
      key: "git-diff",
      icon: "git-branch-outline",
      label: "Git Diff",
      onPress: onOpenGitDiff,
      disabled: gitDiffDisabled,
    },
  ];

  if (codexRenderAction) {
    actions.push({
      key: "codex-render",
      icon: codexRenderAction.icon,
      label: codexRenderAction.label,
      onPress: codexRenderAction.onPress,
    });
  }

  actions.push(
    {
      key: "rename",
      icon: "create-outline",
      label: "Rename",
      onPress: onRename,
    },
    {
      key: "pin",
      icon: activePinned ? "remove-circle-outline" : "pin-outline",
      label: activePinned ? "Unpin Tab" : "Pin Tab",
      onPress: onTogglePinned,
    },
    {
      key: "close-other-tabs",
      icon: "close-circle-outline",
      label: "Close Other Tabs",
      onPress: onCloseOtherTabs,
      disabled: closeOtherTabsDisabled,
    },
    {
      key: "close-tab",
      icon: "close-outline",
      label: "Close Tab",
      onPress: onCloseTab,
    },
  );

  if (showLinkedWork) {
    actions.push({
      key: "linked-work",
      icon: "reader-outline",
      label: "Open Brain",
      onPress: onOpenLinkedWork,
    });
  }

  actions.push(
    {
      key: "terminate",
      icon: "stop-circle-outline",
      label: "Terminate",
      onPress: onTerminate,
      destructive: true,
    },
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="none"
      onRequestClose={onClose}
    >
      <View style={styles.popoverRoot}>
        <TouchableOpacity
          style={styles.popoverBackdrop}
          activeOpacity={1}
          onPress={onClose}
        />

        <TerminalActionMenu
          left={left}
          top={top}
          actions={actions}
          chrome={chrome}
          theme={theme}
        />
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  popoverRoot: {
    flex: 1,
  },
  popoverBackdrop: {
    ...StyleSheet.absoluteFill,
    backgroundColor: "transparent",
  },
});
