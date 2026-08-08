import React from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
import { shadow } from "../../constants/tokens";
import { CHAT_COMPOSER_DOCK_RADIUS } from "./chatChromeMetrics";
import { COMPOSER_PANEL_METRICS } from "./composerActionSlot";

interface InterfaceComposerPanelFrameProps {
  focused: boolean;
  chrome: TerminalThemeChrome;
  layout?: "chatgpt" | "classic";
  children: React.ReactNode;
}

/**
 * Static capsule frames for legacy layouts. The shared mobile Composer
 * (telegram) uses InterfaceComposerExpandingDock, which owns the animated
 * capsule geometry.
 */
export function InterfaceComposerPanelFrame({
  focused: _focused,
  chrome,
  layout = "classic",
  children,
}: InterfaceComposerPanelFrameProps) {
  const ambient = isAmbientChatChrome(chrome);
  const chatgpt = layout === "chatgpt";
  const floating = chatgpt;

  return (
    <View
      collapsable={false}
      style={[
        chatgpt ? styles.panelChatGpt : styles.panel,
        ambient && !chatgpt ? styles.panelAmbient : null,
        {
          backgroundColor: chatgpt || !ambient
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
    borderRadius: CHAT_COMPOSER_DOCK_RADIUS,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: COMPOSER_PANEL_METRICS.classic.left,
    paddingRight: COMPOSER_PANEL_METRICS.classic.right,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: COMPOSER_PANEL_METRICS.classic.gap,
  },
  panelAmbient: {
    paddingLeft: COMPOSER_PANEL_METRICS.ambient.left,
    paddingRight: COMPOSER_PANEL_METRICS.ambient.right,
  },
  panelChatGpt: {
    flex: 1,
    minWidth: 0,
    minHeight: 48,
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: COMPOSER_PANEL_METRICS.chatgpt.left,
    paddingRight: COMPOSER_PANEL_METRICS.chatgpt.right,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: COMPOSER_PANEL_METRICS.chatgpt.gap,
  },
});
