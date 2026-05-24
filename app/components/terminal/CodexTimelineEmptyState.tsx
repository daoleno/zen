import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface CodexTimelineEmptyStateProps {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  variant?: "plain" | "session";
  actionLabel?: string;
  onAction?: () => void;
}

export function CodexTimelineEmptyState({
  chrome,
  title,
  body,
  busy = false,
  variant = "plain",
  actionLabel,
  onAction,
}: CodexTimelineEmptyStateProps) {
  if (variant === "session") {
    return (
      <SessionResetState
        chrome={chrome}
        title={title}
        body={body}
        busy={busy}
      />
    );
  }

  return (
    <View style={styles.emptyState}>
      {busy ? <BusyGlyph chrome={chrome} /> : null}
      <Text style={[styles.emptyTitle, { color: chrome.text }]}>{title}</Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>{body}</Text>
      ) : null}
      {actionLabel && onAction ? (
        <TouchableOpacity
          accessibilityLabel={actionLabel}
          accessibilityRole="button"
          style={[
            styles.emptyAction,
            { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
          ]}
          onPress={onAction}
          activeOpacity={0.82}
        >
          <Ionicons name="terminal-outline" size={15} color={chrome.textMuted} />
          <Text style={[styles.emptyActionText, { color: chrome.textMuted }]}>
            {actionLabel}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function SessionResetState({
  chrome,
  title,
  body,
  busy,
}: {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy: boolean;
}) {
  return (
    <View style={styles.sessionState}>
      <View style={styles.sessionHeader}>
        <View
          style={[
            styles.sessionIconFrame,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: busy ? chrome.accent : chrome.borderStrong,
            },
          ]}
        >
          {busy ? (
            <View style={styles.sessionBusyLayer}>
              <BusyGlyph chrome={chrome} size={29} />
            </View>
          ) : (
            <Ionicons
              name="checkmark"
              size={17}
              color={chrome.accent}
            />
          )}
        </View>
        <View style={styles.sessionCopy}>
          <Text style={[styles.sessionTitle, { color: chrome.text }]}>
            {title}
          </Text>
          {body ? (
            <Text style={[styles.sessionBody, { color: chrome.textMuted }]}>
              {body}
            </Text>
          ) : null}
        </View>
      </View>
      <View
        style={[
          styles.sessionStatusLine,
          {
            backgroundColor: chrome.surfaceMuted,
            borderColor: chrome.border,
          },
        ]}
      >
        <View
          style={[
            styles.sessionStatusDot,
            { backgroundColor: chrome.accent },
          ]}
        />
        <Text style={[styles.sessionStatusText, { color: chrome.textMuted }]}>
          {busy ? "Resetting session" : "Ready"}
        </Text>
      </View>
    </View>
  );
}

function BusyGlyph({
  chrome,
  size = 20,
}: {
  chrome: TerminalThemeChrome;
  size?: number;
}) {
  return (
    <View
      accessible={false}
      pointerEvents="none"
      style={[
        styles.busyGlyph,
        {
          width: size,
          height: size,
          borderColor: chrome.border,
        },
      ]}
    >
      <ComposerLoadingDots
        color={chrome.accent}
        size={Math.max(4, Math.round(size * 0.18))}
        gap={Math.max(3, Math.round(size * 0.13))}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  emptyState: {
    minHeight: 260,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  busyGlyph: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 2.5,
  },
  sessionState: {
    minHeight: 300,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 28,
  },
  sessionHeader: {
    width: "100%",
    maxWidth: 360,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  sessionIconFrame: {
    width: 42,
    height: 42,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  sessionBusyLayer: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    alignItems: "center",
    justifyContent: "center",
  },
  sessionCopy: {
    flex: 1,
    minWidth: 0,
  },
  sessionTitle: {
    fontSize: 15,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  sessionBody: {
    marginTop: 3,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  sessionStatusLine: {
    width: "100%",
    maxWidth: 360,
    marginTop: 14,
    minHeight: 30,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    gap: 7,
  },
  sessionStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  sessionStatusText: {
    flex: 1,
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  emptyTitle: {
    marginTop: 10,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    marginTop: 7,
    fontSize: 12,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
  emptyAction: {
    marginTop: 14,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 7,
  },
  emptyActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
});
