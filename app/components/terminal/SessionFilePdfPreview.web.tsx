import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { SessionFileBinarySource } from "../../services/sessionFilePreview";

interface SessionFilePdfPreviewProps {
  source: SessionFileBinarySource;
  generation: string;
  expectedBytes: number;
  chrome: TerminalThemeChrome;
  onError(message: string, stale: boolean): void;
}

export function SessionFilePdfPreview({
  chrome,
}: SessionFilePdfPreviewProps) {
  return (
    <View
      accessibilityLabel="PDF preview unavailable on Web"
      style={[styles.surface, { backgroundColor: chrome.surfaceMuted }]}
    >
      <Text style={[styles.label, { color: chrome.textMuted }]}>
        PDF preview is unavailable on Web. Download the file to inspect it.
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  surface: {
    flex: 1,
    minHeight: 0,
    alignItems: "center",
    justifyContent: "center",
    padding: 20,
  },
  label: {
    maxWidth: 320,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    lineHeight: 18,
    textAlign: "center",
  },
});
