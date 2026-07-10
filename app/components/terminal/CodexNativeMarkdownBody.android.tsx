import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { prepareCodexMarkdown } from "./CodexNativeMarkdownBodyModel";

interface CodexNativeMarkdownBodyProps {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
  renderFallback(value: string): React.ReactNode;
}

export function CodexNativeMarkdownBody({
  value,
  streaming = false,
  renderFallback,
}: CodexNativeMarkdownBodyProps) {
  const markdown = useMemo(
    () => prepareCodexMarkdown(value, streaming),
    [streaming, value],
  );

  return (
    <View style={styles.messageBody}>
      {renderFallback(markdown || value)}
    </View>
  );
}

const styles = StyleSheet.create({
  messageBody: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
});
