import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  TerminalSheetAction,
  type TerminalSheetActionIcon,
} from "./TerminalSheetAction";

export const TERMINAL_ACTION_MENU_WIDTH = 184;
export type { TerminalSheetActionIcon } from "./TerminalSheetAction";

export interface TerminalActionMenuItem {
  key: string;
  icon: TerminalSheetActionIcon;
  label: string;
  disabled?: boolean;
  destructive?: boolean;
  onPress(): void;
}

interface TerminalActionMenuProps {
  left: number;
  top: number;
  actions: TerminalActionMenuItem[];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}

export function TerminalActionMenu({
  left,
  top,
  actions,
  chrome,
  theme,
}: TerminalActionMenuProps) {
  return (
    <View
      style={[
        styles.menuPopover,
        {
          backgroundColor: chrome.surface,
          left,
          top,
          width: TERMINAL_ACTION_MENU_WIDTH,
          borderColor: chrome.border,
        },
      ]}
    >
      {actions.map((action) => (
        <TerminalSheetAction
          key={action.key}
          icon={action.icon}
          label={action.label}
          disabled={action.disabled}
          destructive={action.destructive}
          chrome={chrome}
          theme={theme}
          onPress={action.onPress}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
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
