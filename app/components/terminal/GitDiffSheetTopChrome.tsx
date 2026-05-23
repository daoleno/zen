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
import { NativeSegmentedControl } from "../ui";
import { withAlpha } from "./gitDiffColor";

export type GitDiffSheetTab = "diff" | "browser";

interface GitDiffSheetTopChromeProps {
  chrome: ReturnType<typeof buildTerminalChrome>;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  activeTab: GitDiffSheetTab;
  fileCount: number;
  showCollapseAll: boolean;
  allDiffFilesCollapsed: boolean;
  accentColor: string;
  onClose(): void;
  onRefresh(): void;
  onTabChange(tab: GitDiffSheetTab): void;
  onToggleAllDiffFiles(): void;
}

export function GitDiffSheetTopChrome({
  chrome,
  snapshot,
  loading,
  activeTab,
  fileCount,
  showCollapseAll,
  allDiffFilesCollapsed,
  accentColor,
  onClose,
  onRefresh,
  onTabChange,
  onToggleAllDiffFiles,
}: GitDiffSheetTopChromeProps) {
  return (
    <>
      <View style={[styles.header, { borderBottomColor: chrome.border }]}>
        <TouchableOpacity
          style={[
            styles.iconButton,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
          onPress={onClose}
          activeOpacity={0.82}
        >
          <Ionicons name="close" size={18} color={chrome.textMuted} />
        </TouchableOpacity>

        <View style={styles.headerCopy}>
          <Text style={[styles.title, { color: chrome.text }]}>Git Diff</Text>
          <Text style={[styles.subtitle, { color: chrome.textMuted }]} numberOfLines={2}>
            {buildSubtitle(snapshot)}
          </Text>
        </View>

        <TouchableOpacity
          style={[
            styles.iconButton,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
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

      {snapshot?.available ? (
        <View style={[styles.modeBar, { borderBottomColor: chrome.border }]}>
          <NativeSegmentedControl
            options={[
              { value: "diff", label: `Diff ${fileCount}` },
              { value: "browser", label: "Files" },
            ]}
            selectedValue={activeTab}
            onSelect={(value) => onTabChange(value as GitDiffSheetTab)}
            tintColor={withAlpha(accentColor, 0.72)}
            appearance="dark"
            style={{
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            }}
          />
          <View style={styles.modeMetaRow}>
            <View style={styles.modeSummaryWrap}>
              <Text style={[styles.modeSummary, { color: chrome.textMuted }]} numberOfLines={2}>
                {buildCompactSummary(snapshot)}
              </Text>
            </View>
            {showCollapseAll ? (
              <TouchableOpacity
                style={[
                  styles.collapseAllButton,
                  {
                    backgroundColor: chrome.surfaceMuted,
                    borderColor: chrome.border,
                  },
                ]}
                onPress={onToggleAllDiffFiles}
                activeOpacity={0.82}
                hitSlop={{ top: 6, right: 6, bottom: 6, left: 6 }}
              >
                <Text style={[styles.collapseAllText, { color: chrome.textMuted }]}>
                  {allDiffFilesCollapsed ? "Expand all" : "Collapse all"}
                </Text>
              </TouchableOpacity>
            ) : null}
          </View>
        </View>
      ) : null}
    </>
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
  if (snapshot.clean) {
    return "working tree clean";
  }

  const parts = [
    `${snapshot.file_count} changed`,
    `${snapshot.staged_file_count} staged`,
    `${snapshot.unstaged_file_count} unstaged`,
  ];
  if (snapshot.untracked_file_count > 0) {
    parts.push(`${snapshot.untracked_file_count} untracked`);
  }
  if (snapshot.additions > 0 || snapshot.deletions > 0) {
    parts.push(`+${snapshot.additions} -${snapshot.deletions}`);
  }
  return parts.join(" · ");
}

const styles = StyleSheet.create({
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    paddingTop: 5,
    paddingBottom: 6,
    gap: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 19,
    lineHeight: 24,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  iconButton: {
    width: 31,
    height: 31,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: StyleSheet.hairlineWidth,
  },
  modeBar: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    gap: 4,
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
    gap: 8,
  },
  collapseAllButton: {
    minHeight: 28,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
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
