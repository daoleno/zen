import React from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale, UiTextMetrics } from "../../constants/tokens";
import type {
  GitDiffFileInfo,
  GitDiffScope,
  GitDiffStatusSnapshot,
} from "../../services/gitDiff";
import type { GitDiffViewMode } from "./gitDiffNavigation";
import {
  describeGitDiffFile,
  gitDiffToneColor,
  type GitDiffFilePresentation,
} from "./gitDiffPresentation";
import { DiffIconButton } from "./GitDiffReviewControls";
import { withAlpha } from "./colorWithAlpha";

export type GitDiffSheetView = "overview" | "reader" | "files" | "file";

interface GitDiffSheetTopChromeProps {
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  view: GitDiffSheetView;
  file: GitDiffFileInfo | null;
  scope: GitDiffScope;
  repoTitle: string;
  /** `+12 −3 · 4 files` for the overview's selected comparison. */
  summary: string | null;
  browserPath: string;
  browserFilePath: string | null;
  workingFileOrigin: GitDiffViewMode;
  /** No overflow when the current state has no secondary actions. */
  hasActions: boolean;
  onClose(): void;
  onBack(): void;
  onOpenActions(): void;
}

/**
 * The sheet's single navigation bar: one leading control (Close at the top
 * level, Back everywhere else), a centered title with concise status, and one
 * overflow button that owns every secondary action.
 */
export function GitDiffSheetTopChrome({
  chrome,
  theme,
  snapshot,
  loading,
  view,
  file,
  scope,
  repoTitle,
  summary,
  browserPath,
  browserFilePath,
  workingFileOrigin,
  hasActions,
  onClose,
  onBack,
  onOpenActions,
}: GitDiffSheetTopChromeProps) {
  const filePresentation =
    view === "reader" && file ? describeGitDiffFile(file, scope) : null;
  const backLabel =
    view === "reader"
      ? "Changed files"
      : view === "files"
        ? browserPath
          ? "Parent folder"
          : "Changes"
        : view === "file"
          ? workingFileOrigin === "changes"
            ? file
              ? "Back to diff"
              : "Changed files"
            : "Files"
          : "Close Git diff";

  let title: string;
  let subtitle: string;
  let headTruncate = false;
  if (filePresentation) {
    title = filePresentation.name;
    subtitle = fileSubtitle(filePresentation, repoTitle);
    headTruncate = true;
  } else if (view === "file" && browserFilePath) {
    const index = browserFilePath.lastIndexOf("/");
    title = browserFilePath.slice(index + 1) || repoTitle;
    subtitle = index > 0 ? `${repoTitle}/${browserFilePath.slice(0, index)}` : repoTitle;
    headTruncate = true;
  } else if (view === "files") {
    title = browserPath ? browserPath.slice(browserPath.lastIndexOf("/") + 1) : "Files";
    subtitle = browserPath ? `${repoTitle}/${browserPath}` : repoTitle;
    headTruncate = true;
  } else {
    title = snapshot?.available ? repoTitle : "Changes";
    subtitle = overviewSubtitle(snapshot, summary);
  }

  return (
    <GitDiffHeaderBar
      chrome={chrome}
      leading={
        <DiffIconButton
          icon={view === "overview" ? "close" : "chevron-back"}
          label={backLabel}
          chrome={chrome}
          filled
          onPress={view === "overview" ? onClose : onBack}
        />
      }
      glyph={filePresentation ? <StatusTile presentation={filePresentation} theme={theme} chrome={chrome} /> : null}
      title={title}
      subtitle={subtitle}
      headTruncate={headTruncate}
      loading={loading}
      trailing={
        hasActions ? (
          <DiffIconButton
            icon="ellipsis-horizontal"
            label="More actions"
            chrome={chrome}
            filled
            onPress={onOpenActions}
          />
        ) : null
      }
    />
  );
}

/**
 * Detail-pane header used only on the wide master-detail layout. The list side
 * keeps its own repo header, so selecting a file never hides list filtering.
 */
export function GitDiffDetailHeader({
  chrome,
  theme,
  file,
  scope,
  repoTitle,
  loading,
  onClear,
  onOpenActions,
}: {
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  file: GitDiffFileInfo;
  scope: GitDiffScope;
  repoTitle: string;
  loading: boolean;
  onClear(): void;
  onOpenActions(): void;
}) {
  const presentation = describeGitDiffFile(file, scope);
  return (
    <GitDiffHeaderBar
      chrome={chrome}
      leading={
        <DiffIconButton
          icon="close"
          label="Clear selection"
          chrome={chrome}
          filled
          onPress={onClear}
        />
      }
      glyph={<StatusTile presentation={presentation} theme={theme} chrome={chrome} />}
      title={presentation.name}
      subtitle={fileSubtitle(presentation, repoTitle)}
      headTruncate
      loading={loading}
      trailing={
        <DiffIconButton
          icon="ellipsis-horizontal"
          label="More actions"
          chrome={chrome}
          filled
          onPress={onOpenActions}
        />
      }
    />
  );
}

function GitDiffHeaderBar({
  chrome,
  leading,
  glyph,
  title,
  subtitle,
  headTruncate,
  loading,
  trailing,
}: {
  chrome: TerminalThemeChrome;
  leading: React.ReactNode;
  glyph: React.ReactNode;
  title: string;
  subtitle: string;
  headTruncate: boolean;
  loading: boolean;
  trailing: React.ReactNode;
}) {
  return (
    <View style={[styles.header, { backgroundColor: chrome.appBackground, borderBottomColor: chrome.border }]}>
      <View style={styles.lane}>{leading}</View>
      <View style={styles.headerCopy}>
        <View style={styles.titleRow}>
          {glyph}
          <Text
            accessibilityRole="header"
            style={[styles.title, { color: chrome.text }]}
            numberOfLines={1}
            ellipsizeMode="middle"
          >
            {title}
          </Text>
        </View>
        <View style={styles.subtitleRow}>
          {loading ? (
            <ActivityIndicator
              size="small"
              color={chrome.textSubtle}
              accessibilityLabel="Refreshing"
              style={styles.spinner}
            />
          ) : null}
          <Text
            style={[styles.subtitle, { color: chrome.textMuted }]}
            numberOfLines={1}
            ellipsizeMode={headTruncate ? "head" : "tail"}
          >
            {subtitle}
          </Text>
        </View>
      </View>
      <View style={styles.lane}>{trailing}</View>
    </View>
  );
}

export function StatusTile({
  presentation,
  theme,
  chrome,
  size = 22,
}: {
  presentation: GitDiffFilePresentation;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  size?: number;
}) {
  const ink = gitDiffToneColor(presentation.tone, theme, chrome.textMuted);
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      style={[
        styles.tile,
        {
          width: size,
          height: size,
          borderRadius: Math.round(size * 0.3),
          backgroundColor: withAlpha(ink, 0.14),
        },
      ]}
    >
      <Text
        allowFontScaling={false}
        style={[styles.tileGlyph, { color: ink, fontSize: Math.round(size * 0.52) }]}
      >
        {presentation.glyph}
      </Text>
    </View>
  );
}

function fileSubtitle(presentation: GitDiffFilePresentation, repoTitle: string): string {
  const directory = presentation.directory.replace(/\/$/, "") || repoTitle;
  const change = presentation.binary
    ? "Binary"
    : `+${presentation.additions} −${presentation.deletions}`;
  return [presentation.statusLabel, change, directory].join(" · ");
}

function overviewSubtitle(
  snapshot: GitDiffStatusSnapshot | null,
  summary: string | null,
): string {
  if (!snapshot?.available) return "Diff and files";
  return [snapshot.branch || null, snapshot.clean ? "Clean" : summary]
    .filter(Boolean)
    .join(" · ") || "Repository";
}

const styles = StyleSheet.create({
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 4,
    paddingVertical: 4,
    borderBottomWidth: StyleSheet.hairlineWidth,
    minHeight: 56,
  },
  lane: {
    minWidth: 48,
    alignItems: "center",
    justifyContent: "center",
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
    alignItems: "center",
    paddingHorizontal: 4,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    maxWidth: "100%",
  },
  title: {
    ...TypeScale.heading,
    ...UiTextMetrics,
    flexShrink: 1,
    minWidth: 0,
  },
  subtitleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    maxWidth: "100%",
  },
  spinner: {
    transform: [{ scale: 0.7 }],
    width: 16,
    height: 16,
  },
  subtitle: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    flexShrink: 1,
    minWidth: 0,
    fontVariant: ["tabular-nums"],
  },
  tile: {
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
  },
  tileGlyph: {
    fontFamily: TypeScale.monoStrong.fontFamily,
    includeFontPadding: false,
    textAlign: "center",
  },
});
