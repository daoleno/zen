import React, { type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

type ChatCanvasProps = {
  chrome: TerminalThemeChrome;
  children: ReactNode;
};

export function ChatCanvas({
  chrome,
  children,
}: ChatCanvasProps) {
  return (
    <View style={[styles.root, { backgroundColor: chrome.appBackground }]}>
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
