import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";

interface CodexComposerPanelFrameProps {
  focused: boolean;
  chrome: TerminalThemeChrome;
  layout?: "chatgpt" | "telegram" | "classic";
  children: React.ReactNode;
}

export function CodexComposerPanelFrame({
  focused,
  chrome,
  layout = "classic",
  children,
}: CodexComposerPanelFrameProps) {
  const ambient = isAmbientChatChrome(chrome);
  const telegram = layout === "telegram";
  const chatgpt = layout === "chatgpt";

  return (
    <View
      collapsable={false}
      style={[
        chatgpt ? styles.panelChatGpt : telegram ? styles.panelTelegram : styles.panel,
        ambient && !telegram && !chatgpt ? styles.panelAmbient : null,
        telegram ? styles.panelTelegramAmbient : null,
        {
          backgroundColor: chatgpt
            ? chrome.surfaceActive
            : telegram
              ? chrome.surface
              : ambient
                ? chrome.surfaceActive
                : chrome.surfaceMuted,
          borderColor: chatgpt || telegram
            ? "transparent"
            : ambient
              ? "transparent"
              : focused
                ? chrome.borderStrong
                : chrome.border,
        },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    minHeight: 46,
    borderRadius: 23,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 5,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  panelAmbient: {
    borderRadius: 22,
    borderWidth: 0,
    minHeight: 46,
    paddingLeft: 4,
    paddingRight: 4,
  },
  panelTelegram: {
    flex: 1,
    minWidth: 0,
    minHeight: 42,
    borderRadius: 21,
    borderWidth: 0,
    paddingLeft: 4,
    paddingRight: 2,
    paddingVertical: 2,
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 0,
  },
  panelTelegramAmbient: {
    minHeight: 42,
  },
  panelChatGpt: {
    flex: 1,
    minWidth: 0,
    minHeight: 44,
    borderRadius: 24,
    borderWidth: 0,
    paddingLeft: 4,
    paddingRight: 4,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 2,
  },
});