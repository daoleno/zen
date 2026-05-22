import React from "react";
import {
  Modal,
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalSheetAction } from "./TerminalSheetAction";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export const TERMINAL_ACTION_POPOVER_WIDTH = 184;

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
    icon: IoniconName;
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

        <View
          style={[
            styles.menuPopover,
            {
              backgroundColor: chrome.surface,
              left,
              top,
              width: TERMINAL_ACTION_POPOVER_WIDTH,
              borderColor: chrome.border,
            },
          ]}
        >
          <TerminalSheetAction
            icon="add"
            label={newTerminalLabel}
            onPress={onNewTerminal}
            disabled={creatingSession || newTerminalDisabled}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          <TerminalSheetAction
            icon="git-branch-outline"
            label="Git Diff"
            onPress={onOpenGitDiff}
            disabled={gitDiffDisabled}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          {codexRenderAction ? (
            <TerminalSheetAction
              icon={codexRenderAction.icon}
              label={codexRenderAction.label}
              onPress={codexRenderAction.onPress}
              textColor={chrome.text}
              disabledTextColor={chrome.textSubtle}
              destructiveColor={theme.red}
            />
          ) : null}
          <TerminalSheetAction
            icon="create-outline"
            label="Rename"
            onPress={onRename}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          <TerminalSheetAction
            icon={activePinned ? "remove-circle-outline" : "pin-outline"}
            label={activePinned ? "Unpin Tab" : "Pin Tab"}
            onPress={onTogglePinned}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          <TerminalSheetAction
            icon="close-circle-outline"
            label="Close Other Tabs"
            onPress={onCloseOtherTabs}
            disabled={closeOtherTabsDisabled}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          <TerminalSheetAction
            icon="close-outline"
            label="Close Tab"
            onPress={onCloseTab}
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
          {showLinkedWork ? (
            <TerminalSheetAction
              icon="reader-outline"
              label="Open Brain"
              onPress={onOpenLinkedWork}
              textColor={chrome.text}
              disabledTextColor={chrome.textSubtle}
              destructiveColor={theme.red}
            />
          ) : null}
          <TerminalSheetAction
            icon="stop-circle-outline"
            label="Terminate"
            onPress={onTerminate}
            destructive
            textColor={chrome.text}
            disabledTextColor={chrome.textSubtle}
            destructiveColor={theme.red}
          />
        </View>
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
  menuPopover: {
    position: "absolute",
    borderRadius: 14,
    paddingVertical: 4,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    shadowColor: "#000",
    shadowOpacity: 0.2,
    shadowRadius: 8,
    elevation: 8,
  },
});
