import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { TypeScale } from "../../constants/tokens";
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
                { color: chrome.textOnAccent },
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
  },
  terminalStateDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  terminalStateTitle: {
    ...TypeScale.heading,
    marginTop: 12,
    textAlign: "center",
  },
  terminalStateDetail: {
    ...TypeScale.compact,
    marginTop: 8,
    textAlign: "center",
  },
  terminalStateHint: {
    ...TypeScale.caption,
    marginTop: 8,
    textAlign: "center",
  },
  terminalStateAction: {
    marginTop: 16,
    minHeight: 44,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
  },
  terminalStateActionText: {
    ...TypeScale.label,
  },
});
