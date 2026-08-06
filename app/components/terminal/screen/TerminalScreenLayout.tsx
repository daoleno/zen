import React from "react";
import { StyleSheet, View } from "react-native";
import { StatusBar } from "expo-status-bar";
import { SafeAreaView } from "react-native-safe-area-context";
import { TerminalTopBar } from "../TerminalTopBar";
import type { TerminalTopBarProps } from "../TerminalTopBar";
import { TerminalViewport } from "../TerminalViewport";
import type { TerminalViewportProps } from "../TerminalViewport";
import type { TerminalThemeChrome } from "../../../constants/terminalThemes";
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
  const floatingChatChrome = viewportProps.showInterfaceChat;
  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chrome.appBackground }]}
      edges={["top"]}
    >
      <StatusBar style={statusBarStyle} />
      <View style={styles.stage}>
        {floatingChatChrome ? (
          <View pointerEvents="box-none" style={styles.headerOverlay}>
            <TerminalTopBar {...topBarProps} />
          </View>
        ) : (
          <TerminalTopBar {...topBarProps} />
        )}
        <TerminalViewport {...viewportProps} />
      </View>

      <TerminalScreenOverlays {...overlayProps} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  stage: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  headerOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    zIndex: 20,
  },
});
