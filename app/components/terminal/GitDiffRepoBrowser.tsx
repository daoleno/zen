import React from "react";
import {
  ActivityIndicator,
  FlatList,
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
  GitRepoBrowserEntry,
  GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { GitDiffCodeSnapshotPanel } from "./GitDiffCodeView";
import { GitDiffStateCard } from "./GitDiffStateCard";
import { withAlpha } from "./gitDiffColor";

interface GitDiffRepoBrowserProps {
  repoTitle: string;
  repoBrowserPath: string;
  repoBrowserEntries: GitRepoBrowserEntry[];
  repoBrowserLoading: boolean;
  repoBrowserError: string | null;
  repoFilePath: string | null;
  repoFileContent?: GitRepoFileContentPayload;
  repoFileLoading: boolean;
  repoFileError: string | null;
  changedPathSet: Set<string>;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onOpenRepoPath(path: string): void;
  onOpenRepoFile(path: string): void;
  onCloseRepoFile(): void;
  onBackRepoPath(): void;
}

export function GitDiffRepoBrowser({
  repoTitle,
  repoBrowserPath,
  repoBrowserEntries,
  repoBrowserLoading,
  repoBrowserError,
  repoFilePath,
  repoFileContent,
  repoFileLoading,
  repoFileError,
  changedPathSet,
  theme,
  chrome,
  onOpenRepoPath,
  onOpenRepoFile,
  onCloseRepoFile,
  onBackRepoPath,
}: GitDiffRepoBrowserProps) {
  const renderRepoEntry = React.useCallback(
    ({ item }: { item: GitRepoBrowserEntry }) => (
      <RepoEntryRow
        entry={item}
        changed={changedPathSet.has(item.path)}
        theme={theme}
        chrome={chrome}
        onPress={() => {
          if (item.kind === "directory") {
            onOpenRepoPath(item.path);
            return;
          }
          onOpenRepoFile(item.path);
        }}
      />
    ),
    [changedPathSet, chrome, onOpenRepoFile, onOpenRepoPath, theme],
  );

  if (repoFilePath) {
    return (
      <RepoFileView
        key={`repo-file:${repoFilePath}`}
        repoTitle={repoTitle}
        path={repoFilePath}
        payload={repoFileContent}
        loading={repoFileLoading}
        error={repoFileError}
        changed={changedPathSet.has(repoFilePath)}
        theme={theme}
        chrome={chrome}
        onBack={onCloseRepoFile}
      />
    );
  }

  return (
    <FlatList
      key={`repo-browser-list:${repoBrowserPath || "root"}`}
      data={repoBrowserEntries}
      keyExtractor={(item) => `${item.kind}:${item.path}`}
      renderItem={renderRepoEntry}
      style={styles.fullList}
      contentContainerStyle={[
        styles.browserContent,
        repoBrowserEntries.length === 0 ? styles.fullListEmpty : null,
      ]}
      ListHeaderComponent={
        <RepoBrowserHeader
          repoTitle={repoTitle}
          path={repoBrowserPath}
          loading={repoBrowserLoading}
          error={repoBrowserError}
          theme={theme}
          chrome={chrome}
          onBack={onBackRepoPath}
          canGoBack={repoBrowserPath !== ""}
        />
      }
      ListEmptyComponent={
        repoBrowserLoading ? null : (
          <GitDiffStateCard
            icon="folder-open-outline"
            title="No files here"
            detail="This folder does not contain visible repository entries."
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        )
      }
      keyboardShouldPersistTaps="handled"
      showsVerticalScrollIndicator={false}
      nestedScrollEnabled={false}
    />
  );
}

function RepoBrowserHeader({
  repoTitle,
  path,
  loading,
  error,
  theme,
  chrome,
  onBack,
  canGoBack,
}: {
  repoTitle: string;
  path: string;
  loading: boolean;
  error: string | null;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onBack(): void;
  canGoBack: boolean;
}) {
  return (
    <View style={styles.browserHeaderWrap}>
      <View
        style={[
          styles.browserPathBar,
          {
            backgroundColor: chrome.surfaceMuted,
            borderColor: chrome.border,
          },
        ]}
      >
        <TouchableOpacity
          style={[
            styles.browserBackButton,
            {
              backgroundColor: canGoBack ? chrome.surface : "transparent",
              borderColor: canGoBack ? chrome.border : "transparent",
              opacity: canGoBack ? 1 : 0.35,
            },
          ]}
          onPress={onBack}
          disabled={!canGoBack}
          activeOpacity={0.82}
        >
          <Ionicons name="arrow-up" size={16} color={chrome.textMuted} />
        </TouchableOpacity>
        <View style={styles.browserPathCopy}>
          <Text style={[styles.browserRepoTitle, { color: chrome.text }]} numberOfLines={1}>
            {repoTitle}
          </Text>
          <Text style={[styles.browserPathText, { color: chrome.textMuted }]} numberOfLines={1}>
            {path ? `/${path}` : "/"}
          </Text>
        </View>
        {loading ? <ActivityIndicator size="small" color={theme.cursor} /> : null}
      </View>

      {error ? (
        <GitDiffStateCard
          icon="warning-outline"
          title="Could not load folder"
          detail={error}
          accent={theme.red}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      ) : null}
    </View>
  );
}

function RepoEntryRow({
  entry,
  changed,
  theme,
  chrome,
  onPress,
}: {
  entry: GitRepoBrowserEntry;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onPress(): void;
}) {
  const isDirectory = entry.kind === "directory";

  return (
    <TouchableOpacity
      style={[
        styles.repoEntryRow,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.border,
        },
      ]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Ionicons
        name={isDirectory ? "folder-outline" : "document-text-outline"}
        size={16}
        color={isDirectory ? theme.yellow : chrome.textSubtle}
      />
      <View style={styles.repoEntryCopy}>
        <Text style={[styles.repoEntryName, { color: chrome.text }]} numberOfLines={1}>
          {entry.name}
        </Text>
      </View>
      {changed ? (
        <View style={[styles.changedPill, { backgroundColor: withAlpha(theme.cursor, 0.12) }]}>
          <Text style={[styles.changedPillText, { color: theme.cursor }]}>Changed</Text>
        </View>
      ) : null}
      <Ionicons
        name={isDirectory ? "chevron-forward" : "open-outline"}
        size={15}
        color={chrome.textSubtle}
      />
    </TouchableOpacity>
  );
}

function RepoFileView({
  repoTitle,
  path,
  payload,
  loading,
  error,
  changed,
  theme,
  chrome,
  onBack,
}: {
  repoTitle: string;
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onBack(): void;
}) {
  return (
    <View style={styles.repoFileRoot}>
      <View style={[styles.repoFileHeader, { borderBottomColor: chrome.border }]}>
        <TouchableOpacity
          style={[
            styles.repoFileBack,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
          onPress={onBack}
          activeOpacity={0.82}
        >
          <Ionicons name="chevron-back" size={17} color={chrome.textMuted} />
        </TouchableOpacity>
        <View style={styles.repoFileCopy}>
          <Text style={[styles.repoFileTitle, { color: chrome.text }]} numberOfLines={1}>
            {pathBaseName(path)}
          </Text>
          <Text style={[styles.repoFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {repoTitle}/{pathDirectoryName(path)}
          </Text>
        </View>
        {changed ? (
          <View style={[styles.changedPill, { backgroundColor: withAlpha(theme.cursor, 0.12) }]}>
            <Text style={[styles.changedPillText, { color: theme.cursor }]}>Changed</Text>
          </View>
        ) : null}
      </View>

      {loading ? (
        <View style={styles.contentPad}>
          <GitDiffStateCard
            icon="sync-outline"
            title="Loading file"
            detail="Fetching the current working tree snapshot."
            accent={theme.cursor}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
            busy
          />
        </View>
      ) : error ? (
        <View style={styles.contentPad}>
          <GitDiffStateCard
            icon="warning-outline"
            title="Could not load file"
            detail={error}
            accent={theme.red}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : (
        <GitDiffCodeSnapshotPanel
          path={path}
          snapshot={payload?.snapshot ?? null}
          chrome={chrome}
          theme={theme}
        />
      )}
    </View>
  );
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function pathDirectoryName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "" : path.slice(0, index);
}

const styles = StyleSheet.create({
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  fullList: {
    flex: 1,
  },
  fullListEmpty: {
    flexGrow: 1,
  },
  browserContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 20,
    gap: 3,
  },
  browserHeaderWrap: {
    gap: 8,
    marginBottom: 2,
  },
  browserPathBar: {
    minHeight: 44,
    borderRadius: 11,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 9,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  browserBackButton: {
    width: 30,
    height: 30,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  browserPathCopy: {
    flex: 1,
    minWidth: 0,
  },
  browserRepoTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  browserPathText: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  repoEntryRow: {
    minHeight: 34,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  repoEntryCopy: {
    flex: 1,
    minWidth: 0,
  },
  repoEntryName: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  repoFileRoot: {
    flex: 1,
  },
  repoFileHeader: {
    minHeight: 48,
    paddingHorizontal: 10,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  repoFileBack: {
    width: 32,
    height: 32,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  repoFileCopy: {
    flex: 1,
    minWidth: 0,
  },
  repoFileTitle: {
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.terminalFontBold,
  },
  repoFilePath: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  changedPill: {
    borderRadius: 999,
    paddingHorizontal: 7,
    paddingVertical: 3,
  },
  changedPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
});
