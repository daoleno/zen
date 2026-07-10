import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
import { shadow } from "../../constants/tokens";
import { CHAT_COMPOSER_DOCK_RADIUS } from "./chatChromeMetrics";

interface CodexComposerPanelFrameProps {
  focused: boolean;
  chrome: TerminalThemeChrome;
  layout?: "chatgpt" | "telegram" | "classic";
  children: React.ReactNode;
}

export function CodexComposerPanelFrame({
  focused: _focused,
  chrome,
  layout = "classic",
  children,
}: CodexComposerPanelFrameProps) {
  const ambient = isAmbientChatChrome(chrome);
  const telegram = layout === "telegram";
  const chatgpt = layout === "chatgpt";
  const floating = telegram || chatgpt;

  return (
    <View
      collapsable={false}
      style={[
        chatgpt
          ? styles.panelChatGpt
          : telegram
            ? styles.panelTelegram
            : styles.panel,
        ambient && !telegram && !chatgpt ? styles.panelAmbient : null,
        {
          backgroundColor:
            chatgpt || telegram || !ambient
              ? chrome.composerInput
              : chrome.surfaceActive,
          borderColor: chrome.border,
          ...(floating ? shadow("card", chrome.shadowColor) : null),
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
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 5,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  panelAmbient: {
    paddingLeft: 4,
    paddingRight: 4,
  },
  panelTelegram: {
    minHeight: 48,
    borderRadius: CHAT_COMPOSER_DOCK_RADIUS,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 6,
    paddingRight: 6,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 2,
  },
  panelChatGpt: {
    flex: 1,
    minWidth: 0,
    minHeight: 48,
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 6,
    paddingRight: 6,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 2,
  },
});
