import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Colors, Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

interface TerminalOutputStateCardProps {
  accent: string;
  busy: boolean;
  title: string;
  detail: string;
  hint: string;
  showRetry: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onRetry(): void;
}

export function TerminalOutputStateCard({
  accent,
  busy,
  title,
  detail,
  hint,
  showRetry,
  chrome,
  theme,
  onRetry,
}: TerminalOutputStateCardProps) {
  return (
    <View style={styles.terminalState}>
      <View
        style={[
          styles.terminalStateCard,
          {
            backgroundColor: chrome.surface,
            borderColor: accent,
          },
        ]}
      >
        {busy ? (
          <ActivityIndicator color={accent} />
        ) : (
          <View style={[styles.terminalStateDot, { backgroundColor: accent }]} />
        )}
        <Text style={[styles.terminalStateTitle, { color: chrome.text }]}>
          {title}
        </Text>
        <Text style={[styles.terminalStateDetail, { color: chrome.textMuted }]}>
          {detail}
        </Text>
        <Text style={[styles.terminalStateHint, { color: chrome.textSubtle }]}>
          {hint}
        </Text>
        {showRetry ? (
          <TouchableOpacity
            style={[
              styles.terminalStateAction,
              { backgroundColor: chrome.accent },
            ]}
            onPress={onRetry}
            activeOpacity={0.84}
          >
            <Text
              style={[
                styles.terminalStateActionText,
                { color: theme.background },
              ]}
            >
              Retry Connection
            </Text>
          </TouchableOpacity>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  terminalState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    paddingBottom: 32,
  },
  terminalStateCard: {
    width: "100%",
    maxWidth: 360,
    alignItems: "center",
    paddingHorizontal: 18,
    paddingVertical: 20,
    borderRadius: 20,
    borderWidth: 1,
    backgroundColor: "rgba(17,22,31,0.9)",
  },
  terminalStateDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  terminalStateTitle: {
    marginTop: 12,
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  terminalStateDetail: {
    marginTop: 8,
    color: "#D6DFEC",
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateHint: {
    marginTop: 8,
    color: "#8E9DB2",
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateAction: {
    marginTop: 16,
    minHeight: 38,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  terminalStateActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
