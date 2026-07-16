import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
import { COMPOSER_OUTER_HORIZONTAL_INSET } from "./composerActionSlot";

interface CodexChatComposerFrameProps {
  bottomPadding: number;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  composerLayout: "chatgpt" | "telegram" | "classic";
  children: React.ReactNode;
}

export function CodexChatComposerFrame({
  bottomPadding,
  chrome,
  theme,
  composerLayout,
  children,
}: CodexChatComposerFrameProps) {
  const ambient = isAmbientChatChrome(chrome);
  const chatgptDock = !ambient && composerLayout === "chatgpt";
  const telegramDock = composerLayout === "telegram";

  return (
    <View
      style={[
        styles.composer,
        ambient ? styles.composerAmbient : null,
        chatgptDock ? styles.composerChatGpt : null,
        telegramDock ? styles.composerTelegram : null,
        {
          paddingBottom: Math.max(bottomPadding, telegramDock ? 8 : 0),
          borderTopWidth: 0,
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
    paddingHorizontal: COMPOSER_OUTER_HORIZONTAL_INSET.classic,
    paddingTop: 8,
  },
  composerAmbient: {
    paddingHorizontal: COMPOSER_OUTER_HORIZONTAL_INSET.ambient,
    paddingTop: 6,
    paddingBottom: 2,
  },
  composerChatGpt: {
    paddingHorizontal: COMPOSER_OUTER_HORIZONTAL_INSET.chatgpt,
    paddingTop: 10,
  },
  composerTelegram: {
    paddingHorizontal: COMPOSER_OUTER_HORIZONTAL_INSET.telegram,
    paddingTop: 6,
  },
});
