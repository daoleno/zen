import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import { buildTerminalChrome } from "../../constants/terminalThemes";
import type {
  GitDiffFileInfo,
  GitDiffStatusSnapshot,
} from "../../services/gitDiff";
import { describeGitDiffFile } from "./gitDiffPresentation";
import { DiffIconButton } from "./GitDiffReviewControls";

export type GitDiffSheetView = "overview" | "reader" | "files" | "file";

interface GitDiffSheetTopChromeProps {
  chrome: ReturnType<typeof buildTerminalChrome>;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  view: GitDiffSheetView;
  file: GitDiffFileInfo | null;
  repoTitle: string;
  browserFilePath: string | null;
  fileFilterOpen: boolean;
  diffSearchOpen: boolean;
  diffOptionsOpen: boolean;
  onClose(): void;
  onBack(): void;
  onRefresh(): void;
  onBrowseFiles(): void;
  onToggleFileFilter(): void;
  onToggleDiffSearch(): void;
  onToggleDiffOptions(): void;
}

export function GitDiffSheetTopChrome({
  chrome,
  snapshot,
  loading,
  view,
  file,
  repoTitle,
  browserFilePath,
  fileFilterOpen,
  diffSearchOpen,
  diffOptionsOpen,
  onClose,
  onBack,
  onRefresh,
  onBrowseFiles,
  onToggleFileFilter,
  onToggleDiffSearch,
  onToggleDiffOptions,
}: GitDiffSheetTopChromeProps) {
  const filePresentation = file ? describeGitDiffFile(file, "all") : null;
  const browserName =
    browserFilePath?.slice(browserFilePath.lastIndexOf("/") + 1) ??
    browserFilePath;
  const backLabel =
    view === "reader"
      ? "Changed files"
      : view === "files"
        ? "Changes"
        : view === "file"
          ? "Files"
          : "Close Git diff";

  const title =
    view === "reader" && filePresentation
      ? filePresentation.name
      : view === "file"
        ? browserName || repoTitle
        : view === "files"
          ? "Files"
          : "Changes";
  const subtitle =
    view === "reader" && filePresentation
      ? [filePresentation.statusLabel, filePresentation.directory || null]
          .filter(Boolean)
          .join("  ·  ")
      : view === "file"
        ? browserFilePath || repoTitle
        : buildSubtitle(snapshot, repoTitle);

  return (
    <View style={[styles.header, { borderBottomColor: chrome.border }]}>
      <DiffIconButton
        icon={view === "overview" ? "close" : "arrow-back"}
        label={backLabel}
        chrome={chrome}
        onPress={view === "overview" ? onClose : onBack}
      />

      <View style={styles.headerCopy}>
        <View style={styles.titleRow}>
          <Ionicons
            name={
              view === "overview" || view === "reader"
                ? "git-branch-outline"
                : "folder-open-outline"
            }
            size={14}
            color={chrome.textSubtle}
          />
          <Text
            style={[styles.title, { color: chrome.text }]}
            numberOfLines={1}
          >
            {title}
          </Text>
          {view === "reader" && file ? (
            <StatusPill file={file} chrome={chrome} />
          ) : null}
        </View>
        <Text
          style={[styles.subtitle, { color: chrome.textMuted }]}
          numberOfLines={1}
          ellipsizeMode="head"
        >
          {subtitle}
        </Text>
      </View>

      {view === "overview" ? (
        <>
          <DiffIconButton
            icon="filter-outline"
            label="Filter changed paths"
            chrome={chrome}
            selected={fileFilterOpen}
            onPress={onToggleFileFilter}
          />
          <DiffIconButton
            icon="folder-open-outline"
            label="Browse repository files"
            chrome={chrome}
            onPress={onBrowseFiles}
          />
        </>
      ) : null}

      {view === "reader" ? (
        <>
          <DiffIconButton
            icon="search"
            label="Find in diff"
            chrome={chrome}
            selected={diffSearchOpen}
            onPress={onToggleDiffSearch}
          />
          <DiffIconButton
            icon="options-outline"
            label="Diff options"
            chrome={chrome}
            selected={diffOptionsOpen}
            onPress={onToggleDiffOptions}
          />
        </>
      ) : null}

      {view === "overview" || view === "reader" || view === "files" ? (
        <DiffIconButton
          icon="refresh"
          label="Refresh Git diff"
          chrome={chrome}
          disabled={loading}
          busy={loading}
          onPress={onRefresh}
        />
      ) : null}
    </View>
  );
}

function StatusPill({
  file,
  chrome,
}: {
  file: GitDiffFileInfo;
  chrome: ReturnType<typeof buildTerminalChrome>;
}) {
  const label = file.status.charAt(0).toUpperCase() + file.status.slice(1);
  return (
    <View style={[styles.statusPill, { backgroundColor: chrome.surfaceMuted }]}>
      <Text
        style={[styles.statusPillText, { color: chrome.textMuted }]}
        numberOfLines={1}
      >
        {label}
      </Text>
    </View>
  );
}

function buildSubtitle(
  snapshot: GitDiffStatusSnapshot | null,
  repoTitle: string,
): string {
  if (!snapshot?.available) {
    return "Diff and files";
  }
  if (snapshot.branch) {
    return `${repoTitle} · ${snapshot.branch}`;
  }
  return repoTitle || "Repository";
}

const styles = StyleSheet.create({
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 4,
    paddingVertical: 4,
    gap: 2,
    borderBottomWidth: StyleSheet.hairlineWidth,
    minHeight: 52,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
    paddingHorizontal: 2,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  title: {
    flexShrink: 1,
    minWidth: 0,
    fontSize: 15,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  statusPill: {
    borderRadius: 6,
    paddingHorizontal: 6,
    paddingVertical: 2,
    flexShrink: 0,
  },
  statusPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
});
