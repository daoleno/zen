import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type { LayoutChangeEvent } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

interface CodexChatComposerFrameProps {
  bottomPadding: number;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  children: React.ReactNode;
  onLayout(event: LayoutChangeEvent): void;
}

export function CodexChatComposerFrame({
  bottomPadding,
  chrome,
  theme,
  children,
  onLayout,
}: CodexChatComposerFrameProps) {
  return (
    <View
      onLayout={onLayout}
      style={[
        styles.composer,
        {
          paddingBottom: bottomPadding,
          borderTopColor: chrome.border,
          backgroundColor: theme.background,
        },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  composer: {
    paddingHorizontal: 12,
    paddingTop: 8,
  },
});
