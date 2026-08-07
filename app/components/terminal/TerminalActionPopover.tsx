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
} from "./TerminalActionMenu";

export const TERMINAL_ACTION_POPOVER_WIDTH = TERMINAL_ACTION_MENU_WIDTH;

interface TerminalActionPopoverProps {
  visible: boolean;
  left: number;
  top: number;
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
  onToggleRenderMode?(): void;
  onTerminate(): void;
}

export function TerminalActionPopover({
  visible,
  left,
  top,
  creatingSession,
  newTerminalLabel,
  newTerminalDisabled,
  showLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel = "Open terminal",
  chrome,
  theme,
  onClose,
  onNewTerminal,
  onRename,
  onOpenLinkedWork,
  onOpenModel,
  onToggleRenderMode,
  onTerminate,
}: TerminalActionPopoverProps) {
  const actions: TerminalActionMenuItem[] = [];

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
      icon: "sparkles-outline",
      label: "Model",
      onPress: onOpenModel,
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
