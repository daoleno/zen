import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { BottomSheetFrame } from "../ui";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface CodexStatusSheetProps {
  visible: boolean;
  event?: CodexConversationEvent | null;
  loading: boolean;
  timedOut: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onRetry(): void;
  onSwitchToTerminal?: () => void;
  onClose(): void;
}

type StatusRow = {
  key: string;
  value: string;
};

export function CodexStatusSheet({
  visible,
  event,
  loading,
  timedOut,
  chrome,
  theme,
  onRetry,
  onSwitchToTerminal,
  onClose,
}: CodexStatusSheetProps) {
  const body = event?.body?.trim() || "";
  const rows = useMemo(() => parseCodexStatusRows(body), [body]);
  const timestamp = formatStatusTimestamp(event?.timestamp);

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="76%"
      cardStyle={styles.sheet}
      onClose={onClose}
    >
      <View style={styles.header}>
        <View
          style={[
            styles.headerIcon,
            { backgroundColor: chrome.accentSoft },
          ]}
        >
          <Ionicons name="pulse-outline" size={18} color={chrome.accent} />
        </View>
        <View style={styles.headerCopy}>
          <Text style={[styles.title, { color: chrome.text }]}>Status</Text>
          <Text style={[styles.subtitle, { color: chrome.textSubtle }]}>
            {timestamp || "Codex /status"}
          </Text>
        </View>
        <TouchableOpacity
          accessibilityLabel="Close status"
          accessibilityRole="button"
          style={[styles.closeButton, { backgroundColor: chrome.surfaceMuted }]}
          onPress={onClose}
          activeOpacity={0.78}
        >
          <Ionicons name="close" size={18} color={chrome.textSubtle} />
        </TouchableOpacity>
      </View>

      {body ? (
        <ScrollView
          style={styles.content}
          contentContainerStyle={styles.contentBody}
          showsVerticalScrollIndicator={false}
        >
          {rows.length > 0 ? (
            <View
              style={[
                styles.rows,
                { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted },
              ]}
            >
              {rows.map((row, index) => (
                <View
                  key={`${row.key}:${index}`}
                  style={[
                    styles.row,
                    index > 0
                      ? {
                          borderTopColor: chrome.border,
                          borderTopWidth: StyleSheet.hairlineWidth,
                        }
                      : null,
                  ]}
                >
                  <Text
                    style={[styles.rowKey, { color: chrome.textSubtle }]}
                    numberOfLines={1}
                  >
                    {row.key}
                  </Text>
                  <Text
                    style={[styles.rowValue, { color: chrome.text }]}
                    selectable
                  >
                    {row.value}
                  </Text>
                </View>
              ))}
            </View>
          ) : null}

          <View style={styles.rawSection}>
            <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
              Output
            </Text>
            <Text
              style={[
                styles.rawOutput,
                {
                  color: chrome.text,
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
              selectable
            >
              {body}
            </Text>
          </View>
        </ScrollView>
      ) : timedOut ? (
        <View style={styles.state}>
          <Ionicons name="warning-outline" size={20} color={theme.yellow} />
          <Text style={[styles.stateTitle, { color: chrome.text }]}>
            No status output received
          </Text>
          <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
            Codex may still be rendering this command in the terminal.
          </Text>
          <View style={styles.actions}>
            <TouchableOpacity
              accessibilityLabel="Retry status"
              accessibilityRole="button"
              style={[styles.actionButton, { backgroundColor: chrome.accent }]}
              onPress={onRetry}
              activeOpacity={0.82}
            >
              <Ionicons name="refresh-outline" size={15} color={theme.cursorAccent} />
              <Text style={[styles.primaryActionText, { color: theme.cursorAccent }]}>
                Retry
              </Text>
            </TouchableOpacity>
            {onSwitchToTerminal ? (
              <TouchableOpacity
                accessibilityLabel="Open terminal"
                accessibilityRole="button"
                style={[
                  styles.actionButton,
                  styles.secondaryActionButton,
                  { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted },
                ]}
                onPress={onSwitchToTerminal}
                activeOpacity={0.78}
              >
                <Ionicons name="terminal-outline" size={15} color={chrome.textSubtle} />
                <Text style={[styles.secondaryActionText, { color: chrome.text }]}>
                  Terminal
                </Text>
              </TouchableOpacity>
            ) : null}
          </View>
        </View>
      ) : (
        <View style={styles.state}>
          <ComposerLoadingDots color={chrome.accent} size={5} gap={4} />
          <Text style={[styles.stateTitle, { color: chrome.text }]}>
            Waiting for Codex status
          </Text>
          {loading ? (
            <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
              /status sent
            </Text>
          ) : null}
        </View>
      )}
    </BottomSheetFrame>
  );
}

function parseCodexStatusRows(body: string): StatusRow[] {
  if (!body.trim()) {
    return [];
  }
  const rows: StatusRow[] = [];
  for (const line of body.split(/\r?\n/)) {
    const cleaned = line.trim().replace(/^[-*]\s+/, "");
    if (!cleaned) {
      continue;
    }
    const match = /^([^:：]{1,48})[:：]\s*(.+)$/.exec(cleaned);
    if (!match) {
      continue;
    }
    const key = match[1].trim();
    const value = match[2].trim();
    if (key && value) {
      rows.push({ key, value });
    }
  }
  return rows;
}

function formatStatusTimestamp(value?: string) {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) {
    return undefined;
  }
  return `Updated ${date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

const styles = StyleSheet.create({
  sheet: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 4,
    paddingBottom: 12,
  },
  headerIcon: {
    width: 34,
    height: 34,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 17,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 1,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  closeButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
  },
  content: {
    maxHeight: 470,
  },
  contentBody: {
    paddingBottom: 8,
  },
  rows: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  row: {
    minHeight: 42,
    paddingHorizontal: 10,
    paddingVertical: 8,
    gap: 3,
  },
  rowKey: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  rowValue: {
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  rawSection: {
    marginTop: 12,
    gap: 6,
  },
  sectionLabel: {
    paddingHorizontal: 2,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  rawOutput: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 9,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  state: {
    minHeight: 190,
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    paddingHorizontal: 20,
  },
  stateTitle: {
    fontSize: 14,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  stateText: {
    fontSize: 12,
    lineHeight: 17,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
  actions: {
    marginTop: 8,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  actionButton: {
    minHeight: 34,
    borderRadius: 8,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  secondaryActionButton: {
    borderWidth: StyleSheet.hairlineWidth,
  },
  primaryActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  secondaryActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
});
