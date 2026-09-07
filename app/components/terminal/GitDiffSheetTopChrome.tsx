import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import { buildTerminalChrome } from "../../constants/terminalThemes";
import type { GitDiffStatusSnapshot } from "../../services/gitDiff";
import { withAlpha } from "./colorWithAlpha";

export type GitDiffSheetTab = "diff" | "browser";

interface GitDiffSheetTopChromeProps {
  chrome: ReturnType<typeof buildTerminalChrome>;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  compact?: boolean;
  activeTab: GitDiffSheetTab;
  fileCount: number;
  accentColor: string;
  onClose(): void;
  onRefresh(): void;
  onTabChange(tab: GitDiffSheetTab): void;
}

export function GitDiffSheetTopChrome({
  chrome,
  snapshot,
  loading,
  compact = false,
  activeTab,
  fileCount,
  accentColor,
  onClose,
  onRefresh,
  onTabChange,
}: GitDiffSheetTopChromeProps) {
  return (
    <>
      <View style={[styles.header, { borderBottomColor: chrome.border }]}>
        <TouchableOpacity
          accessibilityRole="button"
          accessibilityLabel="Close Git Diff"
          style={styles.iconButton}
          onPress={onClose}
          activeOpacity={0.82}
        >
          <Ionicons name="close" size={18} color={chrome.textMuted} />
        </TouchableOpacity>

        <View style={styles.headerCopy}>
          <View style={styles.titleRow}>
            <Ionicons
              name="git-branch-outline"
              size={15}
              color={chrome.textMuted}
            />
            <Text
              style={[styles.title, { color: chrome.text }]}
              numberOfLines={1}
            >
              Git Diff
            </Text>
          </View>
          <Text
            style={[styles.subtitle, { color: chrome.textMuted }]}
            numberOfLines={1}
          >
            {buildSubtitle(snapshot)}
          </Text>
        </View>

        <TouchableOpacity
          accessibilityRole="button"
          accessibilityLabel="Refresh Git Diff"
          disabled={loading}
          style={styles.iconButton}
          onPress={onRefresh}
          activeOpacity={0.82}
        >
          {loading ? (
            <ActivityIndicator size="small" color={chrome.accent} />
          ) : (
            <Ionicons name="refresh" size={16} color={chrome.textMuted} />
          )}
        </TouchableOpacity>
      </View>

      {snapshot?.available && !compact ? (
        <View style={[styles.modeBar, { borderBottomColor: chrome.border }]}>
          <View style={styles.modeMetaRow}>
            <View style={styles.modeSwitch}>
              <ModeButton
                label={`Diff ${fileCount}`}
                active={activeTab === "diff"}
                chrome={chrome}
                accentColor={accentColor}
                onPress={() => onTabChange("diff")}
              />
              <ModeButton
                label="Files"
                active={activeTab === "browser"}
                chrome={chrome}
                accentColor={accentColor}
                onPress={() => onTabChange("browser")}
              />
            </View>
            <View style={styles.modeSummaryWrap}>
              <Text
                style={[styles.modeSummary, { color: chrome.textMuted }]}
                numberOfLines={2}
              >
                {buildCompactSummary(snapshot)}
              </Text>
            </View>
          </View>
        </View>
      ) : null}
    </>
  );
}

function ModeButton({
  label,
  active,
  chrome,
  accentColor,
  onPress,
}: {
  label: string;
  active: boolean;
  chrome: ReturnType<typeof buildTerminalChrome>;
  accentColor: string;
  onPress(): void;
}) {
  return (
    <TouchableOpacity
      style={[
        styles.modeButton,
        active
          ? {
              backgroundColor: withAlpha(accentColor, 0.16),
              borderColor: withAlpha(accentColor, 0.36),
            }
          : {
              backgroundColor: "transparent",
              borderColor: "transparent",
            },
      ]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Text
        style={[
          styles.modeButtonText,
          { color: active ? chrome.text : chrome.textMuted },
        ]}
        numberOfLines={1}
      >
        {label}
      </Text>
    </TouchableOpacity>
  );
}

function buildSubtitle(snapshot: GitDiffStatusSnapshot | null): string {
  if (!snapshot?.available) {
    return "Diff and files";
  }
  if (snapshot.repo_name && snapshot.branch) {
    return `${snapshot.repo_name} · ${snapshot.branch}`;
  }
  return snapshot.repo_name || "Repository";
}

function buildCompactSummary(snapshot: GitDiffStatusSnapshot): string {
  return `+${snapshot.additions} -${snapshot.deletions}`;
}

const styles = StyleSheet.create({
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 8,
    paddingTop: 4,
    paddingBottom: 5,
    gap: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  title: {
    flex: 1,
    minWidth: 0,
    fontSize: 15,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 0,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  iconButton: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  modeBar: {
    paddingHorizontal: 8,
    paddingVertical: 5,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  modeSummary: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
    flexShrink: 1,
  },
  modeSummaryWrap: {
    flex: 1,
    minWidth: 0,
    paddingRight: 4,
  },
  modeMetaRow: {
    minHeight: 24,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  modeSwitch: {
    flexDirection: "row",
    alignItems: "center",
    gap: 3,
    flexShrink: 0,
  },
  modeButton: {
    minHeight: 26,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    paddingVertical: 4,
    justifyContent: "center",
  },
  modeButtonText: {
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  collapseAllButton: {
    minHeight: 26,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    paddingVertical: 4,
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
  },
  collapseAllText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
});
