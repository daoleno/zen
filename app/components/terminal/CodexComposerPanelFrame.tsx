import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexComposerPanelFrameProps {
  focused: boolean;
  floating: boolean;
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}

export function CodexComposerPanelFrame({
  focused,
  floating,
  chrome,
  children,
}: CodexComposerPanelFrameProps) {
  return (
    <View
      collapsable={false}
      style={[
        styles.panel,
        floating ? styles.floating : null,
        {
          backgroundColor: focused ? chrome.surfaceActive : chrome.surface,
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
    minHeight: 50,
    borderRadius: 25,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 4,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  floating: {
    shadowColor: "#000000",
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 0.16,
    shadowRadius: 18,
    elevation: 10,
  },
});
