import React from "react";
import { StyleSheet } from "react-native";
import { StatusBar } from "expo-status-bar";
import { SafeAreaView } from "react-native-safe-area-context";
import { TerminalTopBar } from "../../components/terminal/TerminalTopBar";
import type { TerminalTopBarProps } from "../../components/terminal/TerminalTopBar";
import { TerminalViewport } from "../../components/terminal/TerminalViewport";
import type { TerminalViewportProps } from "../../components/terminal/TerminalViewport";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalScreenOverlays } from "./TerminalScreenOverlays";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";

interface TerminalScreenLayoutProps {
  chrome: TerminalThemeChrome;
  statusBarStyle: "auto" | "inverted" | "light" | "dark";
  topBarProps: TerminalTopBarProps;
  viewportProps: TerminalViewportProps;
  overlayProps: TerminalScreenOverlaysProps;
}

export function TerminalScreenLayout({
  chrome,
  statusBarStyle,
  topBarProps,
  viewportProps,
  overlayProps,
}: TerminalScreenLayoutProps) {
  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chrome.appBackground }]}
      edges={["top"]}
    >
      <StatusBar style={statusBarStyle} />
      <TerminalTopBar {...topBarProps} />

      <TerminalViewport {...viewportProps} />

      <TerminalScreenOverlays {...overlayProps} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0D0C0C",
  },
});
