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
import { isAmbientChatChrome } from "../../constants/themedSurfaces";

interface CodexChatComposerFrameProps {
  bottomPadding: number;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  composerLayout: "chatgpt" | "telegram" | "classic";
  children: React.ReactNode;
  onLayout(event: LayoutChangeEvent): void;
}

export function CodexChatComposerFrame({
  bottomPadding,
  chrome,
  theme,
  composerLayout,
  children,
  onLayout,
}: CodexChatComposerFrameProps) {
  const ambient = isAmbientChatChrome(chrome);
  const chatgptDock = !ambient && composerLayout === "chatgpt";

  return (
    <View
      onLayout={onLayout}
      style={[
        styles.composer,
        ambient ? styles.composerAmbient : null,
        chatgptDock ? styles.composerChatGpt : null,
        {
          paddingBottom: bottomPadding,
          borderTopWidth: 0,
          backgroundColor: ambient
            ? chrome.surfaceActive
            : theme.background === "transparent"
              ? "transparent"
              : chrome.appBackground,
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
  composerAmbient: {
    paddingHorizontal: 10,
    paddingTop: 6,
    paddingBottom: 2,
  },
  composerChatGpt: {
    paddingHorizontal: 16,
    paddingTop: 10,
  },
});
