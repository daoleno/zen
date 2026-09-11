import React from "react";
import {
  Modal,
  StatusBar,
  StyleSheet,
  useWindowDimensions,
  View,
} from "react-native";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  filterGitDiffFiles,
  type GitDiffPage,
  type GitDiffPageRequest,
  type GitDiffScope,
  type GitDiffStatusSnapshot,
  type GitRepoBrowserEntry,
  type GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { GitDiffRepoBrowser } from "./GitDiffRepoBrowser";
import { GitDiffSheetDiffContent } from "./GitDiffSheetDiffContent";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  GitDiffSheetTopChrome,
  type GitDiffSheetView,
} from "./GitDiffSheetTopChrome";
import { resolveGitDiffBack, type GitDiffViewMode } from "./gitDiffNavigation";

const WIDE_BREAKPOINT = 720;

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  refreshKey: number;
  ownerKey: string;
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
  onCloseRepoFile(): void;
  onBackRepoPath(): void;
}

export function GitDiffSheet({
  visible,
  theme,
  snapshot,
  loading,
  error,
  loadPage,
  refreshKey,
  ownerKey,
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
  onCloseRepoFile,
  onBackRepoPath,
}: GitDiffSheetProps) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const { width } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const wide = width >= WIDE_BREAKPOINT;
  const bottomInset = Math.max(insets.bottom, 0);

  const [view, setView] = React.useState<GitDiffViewMode>("changes");
  const [selectedPath, setSelectedPath] = React.useState<string | null>(null);
  const [scope, setScope] = React.useState<GitDiffScope>("all");
  const [fileFilterOpen, setFileFilterOpen] = React.useState(false);
  const [diffSearchOpen, setDiffSearchOpen] = React.useState(false);
  const [diffOptionsOpen, setDiffOptionsOpen] = React.useState(false);
  const [fileOrigin, setFileOrigin] = React.useState<GitDiffViewMode>("files");

  const files = snapshot?.files ?? [];
  const changedPathSet = React.useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );
  const selectedFile =
    files.find((file) => file.path === selectedPath) ?? null;
  const repoFileContent = repoFilePath
    ? repoFileByPath[repoFilePath]
    : undefined;
  const repoFileLoading = Boolean(
    repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent,
  );

  React.useEffect(() => {
    if (visible) return;
    setView("changes");
    setSelectedPath(null);
    setScope("all");
    setFileFilterOpen(false);
    setDiffSearchOpen(false);
    setDiffOptionsOpen(false);
  }, [visible]);

  React.useEffect(() => {
    setView("changes");
    setSelectedPath(null);
    setScope("all");
    setFileFilterOpen(false);
    setDiffSearchOpen(false);
    setDiffOptionsOpen(false);
  }, [ownerKey]);

  React.useEffect(() => {
    if (selectedPath && !selectedFile && snapshot?.available && !loading) {
      setSelectedPath(null);
    }
  }, [selectedPath, selectedFile, snapshot?.available, loading]);

  const scopeCounts = React.useMemo<Record<GitDiffScope, number>>(
    () => ({
      all: files.length,
      working: filterGitDiffFiles(files, "working", "").length,
      staged: filterGitDiffFiles(files, "staged", "").length,
    }),
    [files],
  );

  const repoTitle =
    snapshot?.repo_name || repoBaseName(snapshot?.repo_root || "") || "repo";

  const handleBack = React.useCallback(() => {
    const action = resolveGitDiffBack({
      view,
      hasSelectedFile: Boolean(selectedFile),
      hasBrowserFile: Boolean(repoFilePath),
      fileOrigin,
    });
    switch (action) {
      case "close":
        onClose();
        break;
      case "deselect-file":
        setSelectedPath(null);
        setDiffSearchOpen(false);
        setDiffOptionsOpen(false);
        break;
      case "close-browser-file-to-reader":
        onCloseRepoFile();
        setView("changes");
        break;
      case "close-browser-file-to-browser":
        onCloseRepoFile();
        break;
      case "browser-to-changes":
        setView("changes");
        break;
    }
  }, [fileOrigin, onClose, onCloseRepoFile, repoFilePath, selectedFile, view]);

  const handleBrowseFiles = React.useCallback(() => {
    setView("files");
    onOpenRepoPath(repoBrowserPath);
  }, [onOpenRepoPath, repoBrowserPath]);

  const handleOpenWorkingFile = React.useCallback(
    (path: string) => {
      setFileOrigin("changes");
      setView("files");
      onOpenRepoFile(path);
    },
    [onOpenRepoFile],
  );

  const handleOpenBrowserFile = React.useCallback(
    (path: string) => {
      setFileOrigin("files");
      onOpenRepoFile(path);
    },
    [onOpenRepoFile],
  );

  const handleRefresh = React.useCallback(() => {
    onRefresh();
    if (view === "files") {
      if (repoFilePath) onOpenRepoFile(repoFilePath);
      else onOpenRepoPath(repoBrowserPath);
    }
  }, [onOpenRepoFile, onOpenRepoPath, onRefresh, repoBrowserPath, repoFilePath, view]);

  const chromeView: GitDiffSheetView = !snapshot?.available
    ? "overview"
    : view === "files"
      ? repoFilePath
        ? "file"
        : "files"
      : // Wide mode keeps the repo/list header anchored to the master pane; the
        // detail pane owns its own file header.
        wide
        ? "overview"
        : selectedFile
          ? "reader"
          : "overview";

  return (
    <Modal
      visible={visible}
      animationType="slide"
      onRequestClose={handleBack}
      statusBarTranslucent
    >
      <SafeAreaView
        style={[styles.root, { backgroundColor: chrome.appBackground }]}
        edges={["top"]}
      >
        <StatusBar
          barStyle={
            isLightTerminalTheme(theme) ? "dark-content" : "light-content"
          }
        />
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
            view={chromeView}
            file={selectedFile}
            repoTitle={repoTitle}
            browserFilePath={repoFilePath}
            workingFileOrigin={fileOrigin}
            fileFilterOpen={fileFilterOpen}
            diffSearchOpen={diffSearchOpen}
            diffOptionsOpen={diffOptionsOpen}
            onClose={onClose}
            onBack={handleBack}
            onRefresh={handleRefresh}
            onBrowseFiles={handleBrowseFiles}
            onToggleFileFilter={() => setFileFilterOpen((value) => !value)}
            onToggleDiffSearch={() => setDiffSearchOpen((value) => !value)}
            onToggleDiffOptions={() => setDiffOptionsOpen((value) => !value)}
          />

          {error ? (
            <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
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
            <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
              <GitDiffStateCard
                icon="sync-outline"
                title="Inspecting repository"
                accent={theme.cursor}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
                busy
              />
            </View>
          ) : !snapshot?.available ? (
            <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
              <GitDiffStateCard
                icon="git-branch-outline"
                title={
                  snapshot?.reason === "no_cwd"
                    ? "No working directory yet"
                    : "Not a git repository"
                }
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
          ) : (
            <View style={styles.body}>
              <View
                style={styles.changesLayer}
                pointerEvents={view === "changes" ? "auto" : "none"}
                importantForAccessibility={
                  view === "changes" ? "auto" : "no-hide-descendants"
                }
                accessibilityElementsHidden={view !== "changes"}
              >
                <GitDiffSheetDiffContent
                  key={ownerKey}
                  files={files}
                  clean={snapshot.clean}
                  loadPage={loadPage}
                  refreshKey={refreshKey}
                  scope={scope}
                  scopeCounts={scopeCounts}
                  selectedFile={selectedFile}
                  wide={wide}
                  bottomInset={bottomInset}
                  fileFilterOpen={fileFilterOpen}
                  diffSearchOpen={diffSearchOpen}
                  diffOptionsOpen={diffOptionsOpen}
                  loading={loading}
                  theme={theme}
                  chrome={chrome}
                  onSelectFile={(path) => {
                    setSelectedPath(path);
                    setDiffSearchOpen(false);
                    setDiffOptionsOpen(false);
                  }}
                  onOpenFile={handleOpenWorkingFile}
                  onScopeChange={(next) => {
                    setScope(next);
                    setSelectedPath(null);
                  }}
                  onClearSelection={() => {
                    setSelectedPath(null);
                    setDiffSearchOpen(false);
                    setDiffOptionsOpen(false);
                  }}
                  onToggleDiffSearch={() => setDiffSearchOpen((value) => !value)}
                  onToggleDiffOptions={() =>
                    setDiffOptionsOpen((value) => !value)
                  }
                  onRefresh={handleRefresh}
                />
              </View>
              {view === "files" ? (
                <View
                  style={[
                    styles.browserLayer,
                    { backgroundColor: chrome.surface },
                  ]}
                >
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
                    bottomInset={bottomInset}
                    onOpenRepoPath={onOpenRepoPath}
                    onOpenRepoFile={handleOpenBrowserFile}
                    onBackRepoPath={onBackRepoPath}
                  />
                </View>
              ) : null}
            </View>
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
  body: {
    flex: 1,
  },
  changesLayer: {
    flex: 1,
  },
  browserLayer: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
});
