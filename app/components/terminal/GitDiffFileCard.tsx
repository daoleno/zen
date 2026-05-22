import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffPatchSection,
} from "../../services/gitDiff";
import { describeGitDiffScope } from "../../services/gitDiff";
import { GitDiffBlock } from "./GitDiffPatchBlock";
import { GitDiffStateCard } from "./GitDiffStateCard";
import { withAlpha } from "./gitDiffColor";

interface GitDiffFileCardProps {
  file: GitDiffFileInfo;
  patch?: GitDiffPatchPayload;
  loading: boolean;
  error: string | null;
  expanded: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onLoadPatch(): void;
  onToggle(): void;
  onOpenFile(): void;
}

export function GitDiffFileCard({
  file,
  patch,
  loading,
  error,
  expanded,
  theme,
  chrome,
  onLoadPatch,
  onToggle,
  onOpenFile,
}: GitDiffFileCardProps) {
  const sections = patch?.sections ?? [];

  React.useEffect(() => {
    if (!expanded || patch || loading || error) {
      return;
    }
    onLoadPatch();
  }, [expanded, error, loading, onLoadPatch, patch]);

  return (
    <View
      style={[
        styles.diffCard,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.border,
        },
      ]}
    >
      <TouchableOpacity
        style={[
          styles.diffCardHeader,
          { borderBottomColor: expanded ? chrome.border : "transparent" },
        ]}
        onPress={onToggle}
        activeOpacity={0.82}
      >
        <Ionicons
          name={expanded ? "chevron-down" : "chevron-forward"}
          size={16}
          color={chrome.textSubtle}
        />
        <View style={styles.diffCardTitleWrap}>
          <Text style={[styles.diffFileName, { color: chrome.text }]} numberOfLines={1}>
            {pathBaseName(file.path)}
          </Text>
          <Text style={[styles.diffFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {buildFilePathMeta(file)}
          </Text>
        </View>
        <View style={styles.diffHeaderBadges}>
          <StatusPill file={file} theme={theme} compact />
          <TouchableOpacity
            style={[styles.diffOpenButton, { borderColor: chrome.border }]}
            onPress={onOpenFile}
            activeOpacity={0.82}
          >
            <Ionicons name="document-text-outline" size={14} color={chrome.textSubtle} />
          </TouchableOpacity>
        </View>
      </TouchableOpacity>

      {!expanded ? null : loading && sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <GitDiffStateCard
            icon="sync-outline"
            title="Loading patch"
            detail="Fetching this file's staged and unstaged hunks."
            accent={theme.cursor}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
            busy
          />
        </View>
      ) : error ? (
        <View style={styles.inlineStateWrap}>
          <GitDiffStateCard
            icon="warning-outline"
            title="Patch unavailable"
            detail={error}
            accent={theme.red}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : patch && sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <GitDiffStateCard
            icon="information-circle-outline"
            title="No patch content"
            detail="Git reports this file as changed, but there are no hunks to display."
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <GitDiffStateCard
            icon="time-outline"
            title="Queued"
            detail="This patch will load shortly."
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : (
        <View style={styles.patchList}>
          {sections.map((section, index) => (
            <PatchSection
              key={`${file.path}:${section.scope}:${index}`}
              section={section}
              theme={theme}
              chrome={chrome}
            />
          ))}
        </View>
      )}
    </View>
  );
}

function PatchSection({
  section,
  theme,
  chrome,
}: {
  section: GitDiffPatchSection;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
}) {
  return (
    <View style={[styles.patchSection, { backgroundColor: chrome.surface, borderColor: chrome.border }]}>
      <View style={styles.patchHeader}>
        <Text style={[styles.patchTitle, { color: chrome.text }]}>
          {section.title}
        </Text>
        <Text style={[styles.patchScope, { color: chrome.textSubtle }]}>
          {section.scope}
        </Text>
      </View>
      <GitDiffBlock patch={section.patch} theme={theme} />
    </View>
  );
}

function StatusPill({
  file,
  theme,
  compact = false,
}: {
  file: GitDiffFileInfo;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const color = statusTone(file, theme);
  return (
    <View
      style={[
        styles.statusPill,
        compact ? styles.statusPillCompact : null,
        { backgroundColor: withAlpha(color, 0.14) },
      ]}
    >
      <Text
        style={[
          styles.statusPillText,
          compact ? styles.statusPillTextCompact : null,
          { color },
        ]}
      >
        {statusLabel(file)}
      </Text>
    </View>
  );
}

function buildFilePathMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${describeGitDiffScope(file)} · ${file.old_path} -> ${file.path}`;
  }
  const directory = pathDirectoryName(file.path);
  return [describeGitDiffScope(file), directory].filter(Boolean).join(" · ");
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function pathDirectoryName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "" : path.slice(0, index);
}

function statusLabel(file: GitDiffFileInfo): string {
  switch (file.status) {
    case "added":
      return "Added";
    case "deleted":
      return "Deleted";
    case "renamed":
      return "Renamed";
    case "copied":
      return "Copied";
    case "conflict":
      return "Conflict";
    case "untracked":
      return "Untracked";
    case "modified":
      return "Modified";
    default:
      return "Changed";
  }
}

function statusTone(file: GitDiffFileInfo, theme: TerminalThemePalette): string {
  switch (file.status) {
    case "added":
    case "untracked":
      return theme.green;
    case "deleted":
      return theme.red;
    case "renamed":
    case "copied":
      return theme.blue;
    case "conflict":
      return theme.magenta;
    case "modified":
      return theme.yellow;
    default:
      return theme.cursor;
  }
}

const styles = StyleSheet.create({
  diffCard: {
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  diffCardHeader: {
    minHeight: 46,
    paddingHorizontal: 9,
    paddingVertical: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  diffCardTitleWrap: {
    flex: 1,
    minWidth: 0,
  },
  diffOpenButton: {
    width: 28,
    height: 28,
    borderRadius: 9,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  diffFileName: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFontBold,
  },
  diffFilePath: {
    marginTop: 2,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  diffHeaderBadges: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  patchList: {
    padding: 5,
    gap: 5,
  },
  patchSection: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  patchHeader: {
    paddingHorizontal: 8,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  patchTitle: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  patchScope: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFont,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  statusPill: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 5,
  },
  statusPillCompact: {
    paddingHorizontal: 7,
    paddingVertical: 3,
  },
  statusPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
  statusPillTextCompact: {
    fontSize: 9,
    lineHeight: 11,
  },
  inlineStateWrap: {
    padding: 8,
  },
});
