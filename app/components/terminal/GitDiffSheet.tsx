import React from "react";
import {
  ActivityIndicator,
  FlatList,
  Modal,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffStatusSnapshot,
  GitRepoBrowserEntry,
  GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { GitDiffCodeSnapshotPanel } from "./GitDiffCodeView";
import { GitDiffFileCard } from "./GitDiffFileCard";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  GitDiffSheetTopChrome,
  type GitDiffSheetTab,
} from "./GitDiffSheetTopChrome";
import { withAlpha } from "./gitDiffColor";

const LARGE_DIFF_FILE_THRESHOLD = 8;

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  patchByPath: Record<string, GitDiffPatchPayload | undefined>;
  patchLoadingByPath: Record<string, boolean>;
  patchErrorByPath: Record<string, string | undefined>;
  repoBrowserPath: string;
  repoBrowserEntries: GitRepoBrowserEntry[];
  repoBrowserLoading: boolean;
  repoBrowserError: string | null;
  repoFilePath: string | null;
  repoFileLoadingPath: string | null;
  repoFileError: string | null;
  repoFileByPath: Record<string, GitRepoFileContentPayload | undefined>;
  onClose(): void;
  onRefresh(): void;
  onOpenRepoPath(path: string): void;
  onOpenRepoFile(path: string): void;
  onLoadDiffPatch(path: string): void;
  onCloseRepoFile(): void;
  onBackRepoPath(): void;
}

export function GitDiffSheet({
  visible,
  theme,
  snapshot,
  loading,
  error,
  patchByPath,
  patchLoadingByPath,
  patchErrorByPath,
  repoBrowserPath,
  repoBrowserEntries,
  repoBrowserLoading,
  repoBrowserError,
  repoFilePath,
  repoFileLoadingPath,
  repoFileError,
  repoFileByPath,
  onClose,
  onRefresh,
  onOpenRepoPath,
  onOpenRepoFile,
  onLoadDiffPatch,
  onCloseRepoFile,
  onBackRepoPath,
}: GitDiffSheetProps) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const [activeTab, setActiveTab] = React.useState<GitDiffSheetTab>("diff");
  const [collapsedDiffPaths, setCollapsedDiffPaths] = React.useState<Set<string>>(
    () => new Set(),
  );

  const files = snapshot?.files ?? [];
  const changedPathSet = React.useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );
  const repoFileContent = repoFilePath ? repoFileByPath[repoFilePath] : undefined;
  const repoFileLoading = Boolean(
    repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent,
  );

  React.useEffect(() => {
    if (visible) {
      return;
    }
    setActiveTab("diff");
    setCollapsedDiffPaths(new Set());
    diffStateSeedRef.current = "";
  }, [visible]);

  const diffPathsSignature = React.useMemo(
    () => files.map((file) => file.path).join("\n"),
    [files],
  );
  const diffStateSeedRef = React.useRef<string>("");

  React.useEffect(() => {
    if (!visible || !snapshot?.available) {
      return;
    }

    const seed = `${diffPathsSignature}|${files.length}`;
    if (diffStateSeedRef.current === seed) {
      return;
    }

    diffStateSeedRef.current = seed;
    if (files.length > LARGE_DIFF_FILE_THRESHOLD) {
      setCollapsedDiffPaths(new Set(files.map((file) => file.path)));
      return;
    }

    setCollapsedDiffPaths(new Set());
  }, [diffPathsSignature, files, snapshot?.available, visible]);

  const allDiffFilesCollapsed = files.length > 0
    && files.every((file) => collapsedDiffPaths.has(file.path));

  const toggleDiffFile = React.useCallback((path: string) => {
    setCollapsedDiffPaths((previous) => {
      const next = new Set(previous);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  const collapseAllDiffFiles = React.useCallback(() => {
    setCollapsedDiffPaths(new Set(files.map((file) => file.path)));
  }, [files]);

  const expandAllDiffFiles = React.useCallback(() => {
    setCollapsedDiffPaths(new Set());
  }, []);

  const renderDiffFile = React.useCallback(
    ({ item }: { item: GitDiffFileInfo }) => (
      <GitDiffFileCard
        file={item}
        patch={patchByPath[item.path]}
        loading={Boolean(patchLoadingByPath[item.path])}
        error={patchErrorByPath[item.path] ?? null}
        expanded={!collapsedDiffPaths.has(item.path)}
        theme={theme}
        chrome={chrome}
        onLoadPatch={() => onLoadDiffPatch(item.path)}
        onToggle={() => toggleDiffFile(item.path)}
        onOpenFile={() => {
          setActiveTab("browser");
          onOpenRepoFile(item.path);
        }}
      />
    ),
    [
      chrome,
      collapsedDiffPaths,
      onOpenRepoFile,
      patchByPath,
      patchErrorByPath,
      patchLoadingByPath,
      theme,
      toggleDiffFile,
    ],
  );

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

  const repoTitle = snapshot?.repo_name || repoBaseName(snapshot?.repo_root || "") || "repo";

  return (
    <Modal
      visible={visible}
      animationType="slide"
      onRequestClose={onClose}
      statusBarTranslucent
    >
      <SafeAreaView
        style={[styles.root, { backgroundColor: chrome.appBackground }]}
        edges={["top", "bottom"]}
      >
        <View
          style={[
            styles.sheet,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          <GitDiffSheetTopChrome
            chrome={chrome}
            snapshot={snapshot}
            loading={loading}
            activeTab={activeTab}
            fileCount={files.length}
            showCollapseAll={
              activeTab === "diff"
              && Boolean(snapshot?.available)
              && !snapshot?.clean
              && files.length > 0
            }
            allDiffFilesCollapsed={allDiffFilesCollapsed}
            accentColor={theme.cursor}
            onClose={onClose}
            onRefresh={onRefresh}
            onTabChange={setActiveTab}
            onToggleAllDiffFiles={allDiffFilesCollapsed ? expandAllDiffFiles : collapseAllDiffFiles}
          />

          {error && !snapshot?.available ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="warning-outline"
                title="Could not load git data"
                detail={error}
                accent={theme.red}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : loading && !snapshot ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="sync-outline"
                title="Inspecting repository"
                detail="Zen is checking the current working tree."
                accent={theme.cursor}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
                busy
              />
            </View>
          ) : !snapshot?.available ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="git-branch-outline"
                title={snapshot?.reason === "no_cwd" ? "No working directory yet" : "Not a git repository"}
                detail={
                  snapshot?.reason === "no_cwd"
                    ? "This terminal has not reported a cwd yet."
                    : "Move this terminal into a git repository and refresh."
                }
                accent={chrome.textSubtle}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : activeTab === "browser" ? (
            repoFilePath ? (
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
            ) : (
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
            )
          ) : snapshot.clean ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="checkmark-done-outline"
                title="Working tree is clean"
                detail="No staged, unstaged, or untracked changes were found."
                accent={theme.green}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : (
            <FlatList
              key="git-diff-list"
              data={files}
              keyExtractor={(item) => item.path}
              renderItem={renderDiffFile}
              style={styles.fullList}
              contentContainerStyle={styles.diffContent}
              showsVerticalScrollIndicator={false}
              removeClippedSubviews={false}
              initialNumToRender={4}
              maxToRenderPerBatch={4}
              windowSize={5}
            />
          )}
        </View>
      </SafeAreaView>
    </Modal>
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

function repoBaseName(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed) return "";
  return pathBaseName(trimmed);
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  sheet: {
    flex: 1,
    borderWidth: 0,
  },
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
  diffContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 18,
    gap: 8,
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
