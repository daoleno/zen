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
import {
  CHAT_CHROME_HORIZONTAL_INSET,
} from "./chatChromeMetrics";

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
  const telegramDock = composerLayout === "telegram";

  return (
    <View
      onLayout={onLayout}
      style={[
        styles.composer,
        ambient ? styles.composerAmbient : null,
        chatgptDock ? styles.composerChatGpt : null,
        telegramDock ? styles.composerTelegram : null,
        {
          paddingBottom: Math.max(bottomPadding, telegramDock ? 8 : 0),
          borderTopWidth: 0,
          // Never paint a dock plate under the pill — elevation on Android
          // draws an opaque rectangle that shows as ugly white layers.
          backgroundColor: ambient ? "transparent" : telegramDock
            ? "transparent"
            : theme.background === "transparent"
              ? "transparent"
              : chrome.appBackground,
          zIndex: 4,
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
  composerTelegram: {
    paddingHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
    paddingTop: 6,
  },
});
