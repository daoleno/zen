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
          size={15}
          color={chrome.textSubtle}
        />
        <View
          style={[
            styles.statusDot,
            { backgroundColor: statusTone(file, theme) },
          ]}
        />
        <View style={styles.diffCardTitleWrap}>
          <Text style={[styles.diffFileName, { color: chrome.text }]} numberOfLines={2}>
            {pathBaseName(file.path)}
          </Text>
          <Text style={[styles.diffFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {buildFilePathMeta(file)}
          </Text>
        </View>
        <View style={styles.diffHeaderActions}>
          <FileStats file={file} theme={theme} chrome={chrome} />
          <TouchableOpacity
            style={styles.diffOpenButton}
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
              path={file.path}
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
  path,
  theme,
  chrome,
}: {
  section: GitDiffPatchSection;
  path: string;
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
      <GitDiffBlock path={path} patch={section.patch} theme={theme} />
    </View>
  );
}

function FileStats({
  file,
  theme,
  chrome,
}: {
  file: GitDiffFileInfo;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
}) {
  const additions = normalizedCount(file.additions);
  const deletions = normalizedCount(file.deletions);
  if (additions === 0 && deletions === 0) {
    return (
      <Text style={[styles.statEmpty, { color: chrome.textSubtle }]}>
        {statusLabel(file)}
      </Text>
    );
  }

  return (
    <View style={styles.fileStats}>
      {additions > 0 ? (
        <Text style={[styles.statText, { color: theme.green }]}>
          +{additions}
        </Text>
      ) : null}
      {deletions > 0 ? (
        <Text style={[styles.statText, { color: theme.red }]}>
          -{deletions}
        </Text>
      ) : null}
    </View>
  );
}

function normalizedCount(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(0, value)
    : 0;
}

function buildFilePathMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${statusLabel(file)} · from ${file.old_path}`;
  }
  const directory = pathDirectoryName(file.path);
  const scope = describeGitDiffScope(file);
  const label = statusLabel(file);
  const state = scope === label ? label : `${label} · ${scope}`;
  return [state, directory].filter(Boolean).join(" · ");
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
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  diffCardHeader: {
    minHeight: 42,
    paddingHorizontal: 8,
    paddingVertical: 5,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  diffCardTitleWrap: {
    flex: 1,
    minWidth: 0,
    paddingTop: 1,
  },
  diffOpenButton: {
    width: 28,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
  },
  diffFileName: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.terminalFontBold,
  },
  diffFilePath: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  diffHeaderActions: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    flexShrink: 0,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
    flexShrink: 0,
    marginTop: 7,
  },
  fileStats: {
    minWidth: 46,
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 5,
  },
  statText: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.terminalFontBold,
  },
  statEmpty: {
    maxWidth: 72,
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
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
  inlineStateWrap: {
    padding: 8,
  },
});
