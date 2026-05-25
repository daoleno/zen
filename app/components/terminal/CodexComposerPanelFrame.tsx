import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexComposerPanelFrameProps {
  focused: boolean;
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}

export function CodexComposerPanelFrame({
  focused,
  chrome,
  children,
}: CodexComposerPanelFrameProps) {
  return (
    <View
      collapsable={false}
      style={[
        styles.panel,
        {
          backgroundColor: chrome.surface,
          borderColor: focused ? chrome.borderStrong : chrome.border,
        },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    minHeight: 48,
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 3,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
});
