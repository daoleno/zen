import React from "react";
import {
  FlatList,
  Modal,
  StyleSheet,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
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
import { GitDiffFileCard } from "./GitDiffFileCard";
import { GitDiffRepoBrowser } from "./GitDiffRepoBrowser";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  GitDiffSheetTopChrome,
  type GitDiffSheetTab,
} from "./GitDiffSheetTopChrome";

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
            <GitDiffRepoBrowser
              repoTitle={repoTitle}
              repoBrowserPath={repoBrowserPath}
              repoBrowserEntries={repoBrowserEntries}
              repoBrowserLoading={repoBrowserLoading}
              repoBrowserError={repoBrowserError}
              repoFilePath={repoFilePath}
              repoFileContent={repoFileContent}
              repoFileLoading={repoFileLoading}
              repoFileError={repoFileError}
              changedPathSet={changedPathSet}
              theme={theme}
              chrome={chrome}
              onOpenRepoPath={onOpenRepoPath}
              onOpenRepoFile={onOpenRepoFile}
              onCloseRepoFile={onCloseRepoFile}
              onBackRepoPath={onBackRepoPath}
            />
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

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
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
  diffContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 18,
    gap: 8,
  },
});
