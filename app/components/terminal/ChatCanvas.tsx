import React, { type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { useAppTheme } from "../../constants/tokens";
import { ChatWallpaper } from "../ui/ChatWallpaper";

type ChatCanvasProps = {
  chrome: TerminalThemeChrome;
  /** When false, only paints the canvas color (chat timeline renders its own wallpaper). */
  showWallpaper?: boolean;
  children: ReactNode;
};

export function ChatCanvas({
  chrome,
  showWallpaper = true,
  children,
}: ChatCanvasProps) {
  const { theme: zenTheme } = useAppTheme();
  const wallpaperEnabled = showWallpaper && zenTheme.chat.showWallpaper;

  return (
    <View style={[styles.root, { backgroundColor: chrome.appBackground }]}>
      {wallpaperEnabled ? <ChatWallpaper /> : null}
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
});